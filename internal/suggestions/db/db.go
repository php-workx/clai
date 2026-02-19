package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// ErrDatabaseClosed is returned when an operation is attempted on a closed database.
var ErrDatabaseClosed = errors.New("database is closed")

const (
	// walCheckpointInterval is how often we checkpoint the WAL file
	// to prevent unbounded growth during long-running daemon sessions.
	walCheckpointInterval = 5 * time.Minute
)

// DB is the main database wrapper for the suggestions engine.
// It manages the SQLite connection, migrations, and lifecycle.
type DB struct {
	closeErr  error
	db        *sql.DB
	lock      *LockFile
	stopCh    chan struct{}
	stoppedCh chan struct{}
	stmts     map[string]*sql.Stmt
	dbPath    string
	stmtMu    sync.RWMutex
	closeOnce sync.Once
}

// Options configures database initialization.
type Options struct {
	Logger            *slog.Logger
	Path              string
	LockTimeout       time.Duration
	SkipLock          bool
	ReadOnly          bool
	UseV1             bool
	EnableRecovery    bool
	RunIntegrityCheck bool
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
	dbPath, err := resolveDBPath(opts)
	if err != nil {
		return nil, err
	}
	dbDir := filepath.Dir(dbPath)
	if mkdirErr := os.MkdirAll(dbDir, 0o750); mkdirErr != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", mkdirErr)
	}

	lock, err := acquireOpenLock(dbDir, opts)
	if err != nil {
		return nil, err
	}
	sqlDB, err := openDatabaseWithRecovery(ctx, dbPath, opts, lock)
	if err != nil {
		return nil, err
	}
	sqlDB, err = runIntegrityRecoveryIfNeeded(ctx, dbPath, sqlDB, opts, lock)
	if err != nil {
		return nil, err
	}
	return buildDB(sqlDB, lock, dbPath, opts.ReadOnly), nil
}

func resolveDBPath(opts Options) (string, error) {
	if opts.Path != "" {
		return opts.Path, nil
	}
	if opts.UseV1 {
		return DefaultV1DBPath()
	}
	return DefaultDBPath()
}

func acquireOpenLock(dbDir string, opts Options) (*LockFile, error) {
	if opts.SkipLock || opts.ReadOnly {
		return nil, nil
	}
	lockOpts := DefaultLockOptions()
	if opts.LockTimeout > 0 {
		lockOpts.Timeout = opts.LockTimeout
	}
	lock, err := AcquireLock(dbDir, lockOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire daemon lock: %w", err)
	}
	return lock, nil
}

func openDatabaseWithRecovery(ctx context.Context, dbPath string, opts Options, lock *LockFile) (*sql.DB, error) {
	sqlDB, err := openAndInit(ctx, dbPath, opts)
	if err == nil {
		return sqlDB, nil
	}
	if !canRecoverFromOpenError(opts, err) {
		releaseLock(lock)
		return nil, err
	}
	logger := resolveRecoveryLogger(opts.Logger)
	sqlDB, recErr := recoverAndReopen(ctx, dbPath, sqlDB, err.Error(), logger)
	if recErr != nil {
		releaseLock(lock)
		return nil, fmt.Errorf("recovery failed: %w", recErr)
	}
	return sqlDB, nil
}

func canRecoverFromOpenError(opts Options, err error) bool {
	canRecover := opts.EnableRecovery && !opts.UseV1 && !opts.ReadOnly
	return canRecover && isCorruptionError(err) && !isPermissionError(err) && !isDiskFullError(err)
}

func runIntegrityRecoveryIfNeeded(
	ctx context.Context,
	dbPath string,
	sqlDB *sql.DB,
	opts Options,
	lock *LockFile,
) (*sql.DB, error) {
	if !opts.EnableRecovery || !opts.RunIntegrityCheck || opts.UseV1 || opts.ReadOnly {
		return sqlDB, nil
	}
	intErr := RunIntegrityCheck(ctx, sqlDB)
	if intErr == nil {
		return sqlDB, nil
	}
	logger := resolveRecoveryLogger(opts.Logger)
	recovered, err := recoverAndReopen(ctx, dbPath, sqlDB, intErr.Error(), logger)
	if err != nil {
		releaseLock(lock)
		return nil, fmt.Errorf("integrity check recovery failed: %w", err)
	}
	return recovered, nil
}

func resolveRecoveryLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func releaseLock(lock *LockFile) {
	if lock != nil {
		lock.Release()
	}
}

func buildDB(sqlDB *sql.DB, lock *LockFile, dbPath string, readOnly bool) *DB {
	d := &DB{
		db:        sqlDB,
		lock:      lock,
		dbPath:    dbPath,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		stmts:     make(map[string]*sql.Stmt),
	}
	if !readOnly {
		go d.walCheckpointLoop()
	} else {
		close(d.stoppedCh)
	}
	return d
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
	if d.stmts == nil {
		d.stmtMu.RUnlock()
		return nil, ErrDatabaseClosed
	}
	if stmt, ok := d.stmts[name]; ok {
		d.stmtMu.RUnlock()
		return stmt, nil
	}
	d.stmtMu.RUnlock()

	// Slow path: prepare and cache
	d.stmtMu.Lock()
	defer d.stmtMu.Unlock()

	if d.stmts == nil {
		return nil, ErrDatabaseClosed
	}

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
