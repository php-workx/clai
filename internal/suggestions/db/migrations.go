package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrSchemaVersionTooNew is returned when the database schema version
// exceeds the version supported by this code. This prevents data corruption
// from running old code against a newer schema.
var ErrSchemaVersionTooNew = errors.New("database schema version is newer than supported; upgrade clai")

// Migration represents a single database migration.
type Migration struct {
	Version int
	SQL     string
}

// Migrations returns the list of all migrations in order.
// Migrations are forward-only and must be applied in sequence.
func Migrations() []Migration {
	return []Migration{
		{Version: 1, SQL: schemaV1},
	}
}

// GetSchemaVersion returns the current schema version from the database.
// Returns 0 if no migrations have been applied yet.
func GetSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	// First check if schema_migrations table exists
	var tableName string
	err := db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='schema_migrations'
	`).Scan(&tableName)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to check for schema_migrations table: %w", err)
	}

	// Get the highest applied version
	var version int
	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) FROM schema_migrations
	`).Scan(&version)

	if err != nil {
		return 0, fmt.Errorf("failed to get schema version: %w", err)
	}

	return version, nil
}

// RunMigrations applies all pending migrations to the database.
// It will refuse to run if the database schema version exceeds SchemaVersion.
// Migrations are applied within a transaction for atomicity.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	currentVersion, err := GetSchemaVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	// Refuse to run if DB version is newer than supported
	if currentVersion > SchemaVersion {
		return fmt.Errorf("%w: database version %d, supported version %d",
			ErrSchemaVersionTooNew, currentVersion, SchemaVersion)
	}

	migrations := Migrations()

	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("migration v%d failed: %w", m.Version, err)
		}
	}

	return nil
}

// applyMigration applies a single migration within a transaction.
func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // Best effort rollback on error

	// Execute the migration SQL
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record the migration
	_, err = tx.ExecContext(ctx, `
		INSERT INTO schema_migrations (version, applied_ts)
		VALUES (?, ?)
	`, m.Version, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	return nil
}

// ValidateSchema checks that all expected tables and indexes exist.
// This can be used for health checks after migrations.
func ValidateSchema(ctx context.Context, db *sql.DB) error {
	// Check all tables exist
	for _, table := range AllTables {
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE type='table' AND name=?
		`, table).Scan(&name)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("table %q does not exist", table)
			}
			return fmt.Errorf("failed to check table %q: %w", table, err)
		}
	}

	// Check all indexes exist
	for _, index := range AllIndexes {
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE type='index' AND name=?
		`, index).Scan(&name)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("index %q does not exist", index)
			}
			return fmt.Errorf("failed to check index %q: %w", index, err)
		}
	}

	return nil
}
