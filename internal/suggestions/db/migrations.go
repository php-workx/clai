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
	SQL     string
	Version int
}

// Migrations returns the migration list for the database (suggestions_v2.db).
// The schema starts at version 2 for historical reasons (to distinguish from
// the now-deleted V1 schema). V3 adds unified storage tables.
func Migrations() []Migration {
	return []Migration{
		{Version: 2, SQL: schemaV2},
		{Version: 3, SQL: schemaV3},
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
func RunMigrations(ctx context.Context, db *sql.DB) error {
	return runMigrationList(ctx, db, Migrations(), SchemaVersion)
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
	if _, execErr := tx.ExecContext(ctx, m.SQL); execErr != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", execErr)
	}

	// Record the migration
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
func migrationTimestampColumn(ctx context.Context, tx *sql.Tx) string {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(schema_migrations)")
	if err != nil {
		return "applied_ms"
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
	return "applied_ms"
}

// ValidateSchema checks that all expected tables, indexes, and triggers exist.
func ValidateSchema(ctx context.Context, db *sql.DB) error {
	if err := validateSchemaObjects(ctx, db, "(type='table' OR type='view')", AllTables, "table"); err != nil {
		return err
	}
	if err := validateSchemaObjects(ctx, db, "type='index'", AllIndexes, "index"); err != nil {
		return err
	}
	return validateSchemaObjects(ctx, db, "type='trigger'", AllTriggers, "trigger")
}

func validateSchemaObjects(
	ctx context.Context,
	db *sql.DB,
	typeFilter string,
	names []string,
	kind string,
) error {
	query := fmt.Sprintf(`
		SELECT name FROM sqlite_master
		WHERE %s AND name=?
	`, typeFilter)
	for _, name := range names {
		if err := validateSchemaObject(ctx, db, query, name, kind); err != nil {
			return err
		}
	}
	return nil
}

func validateSchemaObject(ctx context.Context, db *sql.DB, query, name, kind string) error {
	var found string
	err := db.QueryRowContext(ctx, query, name).Scan(&found)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %q does not exist", kind, name)
	}
	return fmt.Errorf("failed to check %s %q: %w", kind, name, err)
}
