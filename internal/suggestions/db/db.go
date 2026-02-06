package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

const (
	// walCheckpointInterval is how often we checkpoint the WAL file
	// to prevent unbounded growth during long-running daemon sessions.
	walCheckpointInterval = 5 * time.Minute
)

// DB is the main database wrapper for the suggestions engine.
// It manages the SQLite connection, migrations, and lifecycle.
type DB struct {
	db        *sql.DB
	lock      *LockFile
	dbPath    string
	stopCh    chan struct{}
	stoppedCh chan struct{}
	closeOnce sync.Once
	closeErr  error

	// Prepared statements (initialized lazily)
	stmtMu sync.RWMutex
	stmts  map[string]*sql.Stmt
}

// Options configures database initialization.
type Options struct {
	// Path is the path to the database file.
	// If empty, defaults to ~/.clai/suggestions_v2.db (V2) or
	// ~/.clai/suggestions.db (V1, when UseV1 is true).
	Path string

	// LockTimeout is how long to wait for the daemon lock.
	// If zero, uses DefaultLockOptions().Timeout
	LockTimeout time.Duration

	// SkipLock skips acquiring the daemon lock.
	// This should only be used for testing or read-only access.
	SkipLock bool

	// ReadOnly opens the database in read-only mode.
	// No migrations will be run and no lock will be acquired.
	ReadOnly bool

	// UseV1 opens the V1 database (suggestions.db) instead of V2.
	// This is for backward compatibility with existing V1 data.
	UseV1 bool

	// EnableRecovery enables automatic corruption recovery for V2 databases.
	// When enabled, if corruption is detected during Open, the database files
	// are rotated to .corrupt.<timestamp> and a fresh database is initialized.
	// This is only supported for V2 databases (ignored when UseV1 is true).
	EnableRecovery bool

	// RunIntegrityCheck runs PRAGMA integrity_check after opening the database.
	// This is only used when EnableRecovery is true; if the integrity check
	// fails, corruption recovery is triggered.
	RunIntegrityCheck bool

	// Logger is the structured logger for recovery events.
	// If nil, slog.Default() is used for recovery logging.
	Logger *slog.Logger
}

// DefaultDBPath returns the default V2 database path (~/.clai/suggestions_v2.db).
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".clai", "suggestions_v2.db"), nil
}

// DefaultV1DBPath returns the default V1 database path (~/.clai/suggestions.db).
// This is retained for backward compatibility with existing V1 data.
func DefaultV1DBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".clai", "suggestions.db"), nil
}

// Open opens the database, acquires the daemon lock, and runs migrations.
// The caller must call Close() when done.
//
// When EnableRecovery is true (V2 only), corruption detected during open or
// migration triggers automatic recovery: corrupt files are rotated to
// .corrupt.<timestamp> and a fresh database is initialized.
func Open(ctx context.Context, opts Options) (*DB, error) {
	// Determine database path
	dbPath := opts.Path
	if dbPath == "" {
		var err error
		if opts.UseV1 {
			dbPath, err = DefaultV1DBPath()
		} else {
			dbPath, err = DefaultDBPath()
		}
		if err != nil {
			return nil, err
		}
	}

	// Ensure the directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Acquire daemon lock (unless skipped or read-only)
	var lock *LockFile
	if !opts.SkipLock && !opts.ReadOnly {
		lockOpts := DefaultLockOptions()
		if opts.LockTimeout > 0 {
			lockOpts.Timeout = opts.LockTimeout
		}

		var err error
		lock, err = AcquireLock(dbDir, lockOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire daemon lock: %w", err)
		}
	}

	// Attempt to open and initialize the database
	sqlDB, err := openAndInit(ctx, dbPath, opts)
	if err != nil {
		// Check if recovery is applicable:
		// - Recovery must be enabled
		// - Must be V2 database (not V1)
		// - Error must be corruption (not permission/disk-full)
		canRecover := opts.EnableRecovery && !opts.UseV1 && !opts.ReadOnly
		if canRecover && isCorruptionError(err) && !isPermissionError(err) && !isDiskFullError(err) {
			logger := opts.Logger
			if logger == nil {
				logger = slog.Default()
			}

			sqlDB, err = recoverAndReopen(ctx, dbPath, sqlDB, err.Error(), logger)
			if err != nil {
				if lock != nil {
					lock.Release()
				}
				return nil, fmt.Errorf("recovery failed: %w", err)
			}
		} else {
			if lock != nil {
				lock.Release()
			}
			return nil, err
		}
	}

	// Run optional integrity check (only when recovery is enabled for V2)
	if opts.EnableRecovery && opts.RunIntegrityCheck && !opts.UseV1 && !opts.ReadOnly {
		if intErr := RunIntegrityCheck(ctx, sqlDB); intErr != nil {
			logger := opts.Logger
			if logger == nil {
				logger = slog.Default()
			}

			sqlDB, err = recoverAndReopen(ctx, dbPath, sqlDB, intErr.Error(), logger)
			if err != nil {
				if lock != nil {
					lock.Release()
				}
				return nil, fmt.Errorf("integrity check recovery failed: %w", err)
			}
		}
	}

	d := &DB{
		db:        sqlDB,
		lock:      lock,
		dbPath:    dbPath,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		stmts:     make(map[string]*sql.Stmt),
	}

	// Start background WAL checkpointing (unless read-only)
	if !opts.ReadOnly {
		go d.walCheckpointLoop()
	} else {
		close(d.stoppedCh) // No background goroutine in read-only mode
	}

	return d, nil
}

// openAndInit opens the SQLite database, configures it, pings it, and
// runs migrations. It is extracted from Open to allow recovery to call it.
func openAndInit(ctx context.Context, dbPath string, opts Options) (*sql.DB, error) {
	// Build connection string with pragmas
	// modernc.org/sqlite uses _pragma=name(value) syntax
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	if opts.ReadOnly {
		dsn += "&mode=ro"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	// SQLite handles concurrency better with single writer
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't close connections

	// Ping to establish connection and ensure pragmas are applied
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Run migrations (unless read-only)
	if !opts.ReadOnly {
		var migErr error
		if opts.UseV1 {
			migErr = RunMigrations(ctx, db)
		} else {
			migErr = RunV2Migrations(ctx, db)
		}
		if migErr != nil {
			db.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", migErr)
		}
	}

	return db, nil
}

// Close closes the database connection and releases the daemon lock.
// It is safe to call Close multiple times.
func (d *DB) Close() error {
	d.closeOnce.Do(func() {
		// Stop the background checkpoint goroutine
		if d.stopCh != nil {
			close(d.stopCh)
			<-d.stoppedCh // Wait for goroutine to finish
		}

		// Close all prepared statements
		d.stmtMu.Lock()
		for _, stmt := range d.stmts {
			stmt.Close()
		}
		d.stmts = nil
		d.stmtMu.Unlock()

		if d.db != nil {
			// Final checkpoint before closing to merge WAL into main db
			_, _ = d.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			d.closeErr = d.db.Close()
		}

		// Release the daemon lock
		if d.lock != nil {
			if err := d.lock.Release(); err != nil && d.closeErr == nil {
				d.closeErr = err
			}
		}
	})
	return d.closeErr
}

// DB returns the underlying sql.DB for direct access.
// Use with caution; prefer using prepared statement methods.
func (d *DB) DB() *sql.DB {
	return d.db
}

// Path returns the path to the database file.
func (d *DB) Path() string {
	return d.dbPath
}

// walCheckpointLoop periodically checkpoints the WAL file to prevent
// unbounded growth during long-running daemon sessions.
func (d *DB) walCheckpointLoop() {
	defer close(d.stoppedCh)

	ticker := time.NewTicker(walCheckpointInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			// TRUNCATE mode: checkpoint and truncate WAL to zero size
			if _, err := d.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
				log.Printf("WAL checkpoint failed: %v", err)
			}
		}
	}
}

// PrepareStatement returns a prepared statement, caching it for reuse.
// This improves performance for frequently-used queries.
func (d *DB) PrepareStatement(ctx context.Context, name, query string) (*sql.Stmt, error) {
	// Fast path: check if already prepared
	d.stmtMu.RLock()
	if stmt, ok := d.stmts[name]; ok {
		d.stmtMu.RUnlock()
		return stmt, nil
	}
	d.stmtMu.RUnlock()

	// Slow path: prepare and cache
	d.stmtMu.Lock()
	defer d.stmtMu.Unlock()

	// Double-check after acquiring write lock
	if stmt, ok := d.stmts[name]; ok {
		return stmt, nil
	}

	stmt, err := d.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement %q: %w", name, err)
	}

	d.stmts[name] = stmt
	return stmt, nil
}

// ExecContext executes a query that doesn't return rows.
func (d *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return d.db.ExecContext(ctx, query, args...)
}

// QueryContext executes a query that returns rows.
func (d *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, query, args...)
}

// QueryRowContext executes a query that returns at most one row.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return d.db.QueryRowContext(ctx, query, args...)
}

// BeginTx starts a transaction.
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, opts)
}

// Validate checks that the V1 schema is correctly initialized.
// For V2 databases, use ValidateV2 instead.
func (d *DB) Validate(ctx context.Context) error {
	return ValidateSchema(ctx, d.db)
}

// ValidateV2 checks that the V2 schema is correctly initialized.
// This validates all 23 tables, indexes, and triggers required by V2.
func (d *DB) ValidateV2(ctx context.Context) error {
	return ValidateV2Schema(ctx, d.db)
}

// Version returns the current schema version.
func (d *DB) Version(ctx context.Context) (int, error) {
	return GetSchemaVersion(ctx, d.db)
}
