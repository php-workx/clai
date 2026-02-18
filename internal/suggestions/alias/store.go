package alias

import (
	"context"
	"database/sql"
	"fmt"
)

// Store provides persistence for shell alias snapshots in the session_alias table.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store with the given database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SaveAliases saves a complete alias snapshot for a session.
// This replaces any existing aliases for the session (full re-snapshot).
func (s *Store) SaveAliases(ctx context.Context, sessionID string, aliases AliasMap) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete existing aliases for this session
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_alias WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete old aliases: %w", err)
	}

	// Insert new aliases
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO session_alias (session_id, alias_key, expansion) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for name, expansion := range aliases {
		if _, err := stmt.ExecContext(ctx, sessionID, name, expansion); err != nil {
			return fmt.Errorf("insert alias %q: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// LoadAliases loads the alias map for a session from the database.
// Returns an empty map (not nil) if no aliases are found.
func (s *Store) LoadAliases(ctx context.Context, sessionID string) (AliasMap, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT alias_key, expansion FROM session_alias WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query aliases: %w", err)
	}
	defer rows.Close()

	aliases := make(AliasMap)
	for rows.Next() {
		var key, expansion string
		if err := rows.Scan(&key, &expansion); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		aliases[key] = expansion
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aliases: %w", err)
	}
	return aliases, nil
}

// DeleteAliases removes all aliases for a session.
func (s *Store) DeleteAliases(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM session_alias WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete aliases: %w", err)
	}
	return nil
}

// CountAliases returns the number of aliases stored for a session.
func (s *Store) CountAliases(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_alias WHERE session_id = ?`, sessionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count aliases: %w", err)
	}
	return count, nil
}
