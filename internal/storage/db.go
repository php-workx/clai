package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// walCheckpointInterval is how often we checkpoint the WAL file
	// to prevent unbounded growth during long-running daemon sessions.
	walCheckpointInterval = 5 * time.Minute
)

// SQLiteStore implements the Store interface using SQLite.
type SQLiteStore struct {
	db        *sql.DB
	stopCh    chan struct{} // signals background goroutines to stop
	stoppedCh chan struct{} // signals background goroutines have stopped
	closeOnce sync.Once     // ensures Close() is idempotent
	closeErr  error         // stores the error from Close()
}

// DefaultDBPath returns the default database path (~/.clai/state.db).
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".clai", "state.db"), nil
}

// NewSQLiteStore creates a new SQLiteStore with the given database path.
// If the path is empty, it uses the default path (~/.clai/state.db).
// The database is opened with WAL mode enabled for better concurrency.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}

	// Ensure the directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database with pragmas in DSN
	// modernc.org/sqlite uses _pragma=name(value) syntax
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite handles concurrency better with single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't close connections

	// Ping to establish connection and ensure pragmas are applied
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	store := &SQLiteStore{
		db:        db,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}

	// Run migrations
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Start background WAL checkpointing
	go store.walCheckpointLoop()

	return store, nil
}

// Close closes the database connection.
// It is safe to call Close multiple times.
func (s *SQLiteStore) Close() error {
	s.closeOnce.Do(func() {
		// Stop the background checkpoint goroutine
		if s.stopCh != nil {
			close(s.stopCh)
			<-s.stoppedCh // wait for goroutine to finish
		}

		if s.db != nil {
			// Final checkpoint before closing to merge WAL into main db
			_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			s.closeErr = s.db.Close()
		}
	})
	return s.closeErr
}

// DB returns the underlying database connection for advanced use cases.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// walCheckpointLoop periodically checkpoints the WAL file to prevent
// unbounded growth during long-running daemon sessions.
func (s *SQLiteStore) walCheckpointLoop() {
	defer close(s.stoppedCh)

	ticker := time.NewTicker(walCheckpointInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			// TRUNCATE mode: checkpoint and truncate WAL to zero size
			if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
				log.Printf("WAL checkpoint failed: %v", err)
			}
		}
	}
}

// migrate runs database migrations to ensure the schema is up to date.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	// Check current schema version
	currentVersion := 0
	row := s.db.QueryRowContext(ctx, `
		SELECT version FROM schema_meta ORDER BY version DESC LIMIT 1
	`)
	if err := row.Scan(&currentVersion); err != nil {
		if err == sql.ErrNoRows {
			// No version recorded yet, start from 0
			currentVersion = 0
		} else if isTableNotFoundError(err) {
			// Table doesn't exist yet, start from 0
			currentVersion = 0
		} else {
			// Propagate unexpected errors
			return fmt.Errorf("failed to read schema version: %w", err)
		}
	}

	// Run migrations in order
	migrations := []struct {
		version int
		sql     string
	}{
		{
			version: 1,
			sql:     migrationV1,
		},
		{
			version: 2,
			sql:     migrationV2,
		},
		{
			version: 3,
			sql:     migrationV3,
		},
	}

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		if _, err := s.db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("migration v%d failed: %w", m.version, err)
		}

		// Record migration
		_, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO schema_meta (version, applied_at_unix_ms)
			VALUES (?, ?)
		`, m.version, time.Now().UnixMilli())
		if err != nil {
			return fmt.Errorf("failed to record migration v%d: %w", m.version, err)
		}
	}

	return nil
}

// isTableNotFoundError checks if the error indicates a missing table.
func isTableNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "no such table") || contains(errStr, "does not exist")
}

// contains is a simple string contains check to avoid importing strings.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// migrationV1 creates the initial schema.
const migrationV1 = `
-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_meta (
  version INTEGER PRIMARY KEY,
  applied_at_unix_ms INTEGER NOT NULL
);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
  session_id TEXT PRIMARY KEY,
  started_at_unix_ms INTEGER NOT NULL,
  ended_at_unix_ms INTEGER,
  shell TEXT NOT NULL,
  os TEXT NOT NULL,
  hostname TEXT,
  username TEXT,
  initial_cwd TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at_unix_ms DESC);

-- Commands (History)
CREATE TABLE IF NOT EXISTS commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_id TEXT NOT NULL UNIQUE,
  session_id TEXT NOT NULL REFERENCES sessions(session_id),

  -- Timing
  ts_start_unix_ms INTEGER NOT NULL,
  ts_end_unix_ms INTEGER,
  duration_ms INTEGER,

  -- Context
  cwd TEXT NOT NULL,

  -- The Command
  command TEXT NOT NULL,
  command_norm TEXT NOT NULL,
  command_hash TEXT NOT NULL,

  -- Result
  exit_code INTEGER,
  is_success INTEGER DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_commands_session ON commands(session_id, ts_start_unix_ms DESC);
CREATE INDEX IF NOT EXISTS idx_commands_ts ON commands(ts_start_unix_ms DESC);
CREATE INDEX IF NOT EXISTS idx_commands_cwd ON commands(cwd, ts_start_unix_ms DESC);
CREATE INDEX IF NOT EXISTS idx_commands_norm ON commands(command_norm);
CREATE INDEX IF NOT EXISTS idx_commands_hash ON commands(command_hash);

-- AI Response Cache
CREATE TABLE IF NOT EXISTS ai_cache (
  cache_key TEXT PRIMARY KEY,
  response_json TEXT NOT NULL,
  provider TEXT NOT NULL,
  created_at_unix_ms INTEGER NOT NULL,
  expires_at_unix_ms INTEGER NOT NULL,
  hit_count INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_ai_cache_expires ON ai_cache(expires_at_unix_ms);
`

// migrationV2 adds extended command metadata.
const migrationV2 = `
-- Git context
ALTER TABLE commands ADD COLUMN git_branch TEXT;
ALTER TABLE commands ADD COLUMN git_repo_name TEXT;
ALTER TABLE commands ADD COLUMN git_repo_root TEXT;

-- Sequence tracking
ALTER TABLE commands ADD COLUMN prev_command_id TEXT;

-- Derived metadata
ALTER TABLE commands ADD COLUMN is_sudo INTEGER DEFAULT 0;
ALTER TABLE commands ADD COLUMN pipe_count INTEGER DEFAULT 0;
ALTER TABLE commands ADD COLUMN word_count INTEGER DEFAULT 0;

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_commands_git_branch ON commands(git_branch);
CREATE INDEX IF NOT EXISTS idx_commands_git_repo ON commands(git_repo_name);
`

// migrationV3 adds workflow tables.
const migrationV3 = `
-- Workflow runs
CREATE TABLE IF NOT EXISTS workflow_runs (
  run_id TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  workflow_hash TEXT NOT NULL,
  workflow_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'running',
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_name ON workflow_runs(workflow_name);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_started ON workflow_runs(started_at DESC);

-- Workflow steps
CREATE TABLE IF NOT EXISTS workflow_steps (
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id),
  step_id TEXT NOT NULL,
  matrix_key TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'running',
  command TEXT NOT NULL DEFAULT '',
  exit_code INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  stdout_tail TEXT NOT NULL DEFAULT '',
  stderr_tail TEXT NOT NULL DEFAULT '',
  outputs_json TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY (run_id, step_id, matrix_key)
);

CREATE INDEX IF NOT EXISTS idx_workflow_steps_run ON workflow_steps(run_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_status ON workflow_steps(status);

-- Workflow analyses
CREATE TABLE IF NOT EXISTS workflow_analyses (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  matrix_key TEXT NOT NULL DEFAULT '',
  decision TEXT NOT NULL,
  reasoning TEXT NOT NULL DEFAULT '',
  flags_json TEXT NOT NULL DEFAULT '{}',
  prompt TEXT NOT NULL DEFAULT '',
  raw_response TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  analyzed_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_workflow_analyses_step ON workflow_analyses(run_id, step_id, matrix_key);
CREATE INDEX IF NOT EXISTS idx_workflow_analyses_decision ON workflow_analyses(decision);
`
