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

// V1Migrations returns the migration list for V1 database files (suggestions.db).
func V1Migrations() []Migration {
	return []Migration{
		{Version: 1, SQL: schemaV1},
	}
}

// V2Migrations returns the migration list for V2 database files (suggestions_v2.db).
// V2 uses a separate database file and does not migrate from V1.
// The schema starts at version 2 to clearly distinguish from V1 databases.
func V2Migrations() []Migration {
	return []Migration{
		{Version: 2, SQL: schemaV2},
	}
}

// Migrations returns the V1 migration list for backward compatibility.
// New code should use V1Migrations() or V2Migrations() explicitly.
func Migrations() []Migration {
	return V1Migrations()
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

// RunMigrations applies all pending V1 migrations to the database.
// For V2 databases, use RunV2Migrations instead.
// It will refuse to run if the database schema version exceeds V1SchemaVersion.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	return runMigrationList(ctx, db, V1Migrations(), V1SchemaVersion)
}

// RunV2Migrations applies the V2 schema migration to a fresh database.
// V2 uses a separate database file (suggestions_v2.db) and starts fresh.
// It will refuse to run if the database schema version exceeds SchemaVersion.
func RunV2Migrations(ctx context.Context, db *sql.DB) error {
	return runMigrationList(ctx, db, V2Migrations(), SchemaVersion)
}

// runMigrationList applies pending migrations from the given list.
func runMigrationList(ctx context.Context, db *sql.DB, migrations []Migration, maxVersion int) error {
	currentVersion, err := GetSchemaVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	// Refuse to run if DB version is newer than supported
	if currentVersion > maxVersion {
		return fmt.Errorf("%w: database version %d, supported version %d",
			ErrSchemaVersionTooNew, currentVersion, maxVersion)
	}

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

	// Record the migration. Detect the column name since V1 uses
	// applied_ts and V2 uses applied_ms.
	columnName := migrationTimestampColumn(ctx, tx)
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO schema_migrations (version, %s)
		VALUES (?, ?)
	`, columnName), m.Version, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	return nil
}

// migrationTimestampColumn detects the timestamp column name in schema_migrations.
// V1 uses "applied_ts", V2 uses "applied_ms".
func migrationTimestampColumn(ctx context.Context, tx *sql.Tx) string {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(schema_migrations)")
	if err != nil {
		return "applied_ms" // default to V2
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typeName string
		var notNull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == "applied_ts" {
			return "applied_ts"
		}
		if name == "applied_ms" {
			return "applied_ms"
		}
	}
	return "applied_ms" // default to V2
}

// ValidateSchema checks that all expected V1 tables and indexes exist.
// This can be used for health checks after migrations on V1 databases.
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

// ValidateV2Schema checks that all expected V2 tables, indexes, and triggers exist.
// This is the primary validation for V2 database files.
func ValidateV2Schema(ctx context.Context, db *sql.DB) error {
	// Check all V2 tables exist
	for _, table := range V2AllTables {
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE (type='table' OR type='view') AND name=?
		`, table).Scan(&name)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("table %q does not exist", table)
			}
			return fmt.Errorf("failed to check table %q: %w", table, err)
		}
	}

	// Check all V2 indexes exist
	for _, index := range V2AllIndexes {
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

	// Check all V2 triggers exist
	for _, trigger := range V2AllTriggers {
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE type='trigger' AND name=?
		`, trigger).Scan(&name)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("trigger %q does not exist", trigger)
			}
			return fmt.Errorf("failed to check trigger %q: %w", trigger, err)
		}
	}

	return nil
}
