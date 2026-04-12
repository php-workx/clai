package ops

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/google/uuid"

	"github.com/runger/clai/internal/cmdutil"
	"github.com/runger/clai/internal/history"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// HasImportedHistory checks if history has already been imported for the given shell.
func HasImportedHistory(ctx context.Context, db *suggestdb.DB, shell string) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM history_import_meta WHERE shell = ?`, shell).Scan(&exists)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check imported history: %w", err)
	}
	return true, nil
}

// ImportHistory imports shell history entries into the V2 database.
// It replaces any previously imported entries for the same shell.
func ImportHistory(ctx context.Context, db *suggestdb.DB, entries []history.ImportEntry, shell string) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	now := time.Now().UnixMilli()
	sessionID := "imported-" + shell

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	// Delete existing imported commands for this shell
	_, err = tx.ExecContext(ctx, `DELETE FROM command_event WHERE session_id = ?`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old imports: %w", err)
	}

	// Delete existing import session
	_, err = tx.ExecContext(ctx, `DELETE FROM session WHERE id = ?`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old session: %w", err)
	}

	// Create the import session
	sessionStart := now
	if !entries[0].Timestamp.IsZero() {
		sessionStart = entries[0].Timestamp.UnixMilli()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms, os, initial_cwd)
		VALUES (?, ?, ?, ?, ?)
	`, sessionID, shell, sessionStart, runtime.GOOS, "/")
	if err != nil {
		return 0, fmt.Errorf("failed to create import session: %w", err)
	}

	// Prepare insert for command_event
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO command_event (
			session_id, ts_ms, cwd, cmd_raw, cmd_norm, command_id
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	imported := 0
	for _, entry := range entries {
		if entry.Command == "" {
			continue
		}

		tsStart := now + int64(imported)
		if !entry.Timestamp.IsZero() {
			tsStart = entry.Timestamp.UnixMilli()
		}

		norm := cmdutil.NormalizeCommand(entry.Command)

		if _, execErr := stmt.ExecContext(ctx,
			sessionID, tsStart, "/",
			entry.Command, norm, uuid.New().String(),
		); execErr != nil {
			slog.Debug("history import: skipped entry", "error", execErr)
			continue // Skip individual failures
		}
		imported++
	}

	// Record import metadata
	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO history_import_meta (shell, imported_at_ms, entry_count)
		VALUES (?, ?, ?)
	`, shell, now, imported)
	if err != nil {
		return 0, fmt.Errorf("failed to record import metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return imported, nil
}
