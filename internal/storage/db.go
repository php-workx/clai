package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements the Store interface using SQLite.
type SQLiteStore struct {
	db *sql.DB
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

	store := &SQLiteStore{db: db}

	// Run migrations
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying database connection for advanced use cases.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// migrate runs database migrations to ensure the schema is up to date.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	// Check current schema version
	currentVersion := 0
	row := s.db.QueryRowContext(ctx, `
		SELECT version FROM schema_meta ORDER BY version DESC LIMIT 1
	`)
	if err := row.Scan(&currentVersion); err != nil && err != sql.ErrNoRows {
		// Table might not exist yet, which is fine
		if !isTableNotFoundError(err) {
			// Try to read anyway - if schema_meta doesn't exist, we start from 0
			currentVersion = 0
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
