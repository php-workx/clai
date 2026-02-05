package storage

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"time"

	"github.com/google/uuid"

	"github.com/runger/clai/internal/cmdutil"
	"github.com/runger/clai/internal/history"
)

// ImportSessionID returns the session ID used for imported history.
// Format: "imported-<shell>" (e.g., "imported-bash", "imported-zsh").
func ImportSessionID(shell string) string {
	return "imported-" + shell
}

// HasImportedHistory checks if history has already been imported for the given shell.
func (s *SQLiteStore) HasImportedHistory(ctx context.Context, shell string) (bool, error) {
	sessionID := ImportSessionID(shell)
	row := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM commands WHERE session_id = ? LIMIT 1
	`, sessionID)

	var exists int
	err := row.Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check imported history: %w", err)
	}
	return true, nil
}

// ImportHistory imports shell history entries into the database.
// It replaces any previously imported entries for the same shell.
// Returns the number of entries imported.
func (s *SQLiteStore) ImportHistory(ctx context.Context, entries []history.ImportEntry, shell string) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	sessionID := ImportSessionID(shell)
	now := time.Now().UnixMilli()

	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing imported commands for this shell
	_, err = tx.ExecContext(ctx, `DELETE FROM commands WHERE session_id = ?`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old imports: %w", err)
	}

	// Delete existing imported session for this shell (if any)
	_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old session: %w", err)
	}

	// Create the import session
	// Use the oldest entry's timestamp as the session start time
	var sessionStart int64
	if !entries[0].Timestamp.IsZero() {
		sessionStart = entries[0].Timestamp.UnixMilli()
	} else {
		sessionStart = now
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions (
			session_id, started_at_unix_ms, ended_at_unix_ms,
			shell, os, hostname, username, initial_cwd
		) VALUES (?, ?, NULL, ?, ?, NULL, NULL, ?)
	`, sessionID, sessionStart, shell, runtime.GOOS, "/")
	if err != nil {
		return 0, fmt.Errorf("failed to create import session: %w", err)
	}

	// Prepare the insert statement for commands
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO commands (
			command_id, session_id, ts_start_unix_ms, ts_end_unix_ms,
			duration_ms, cwd, command, command_norm, command_hash,
			exit_code, is_success,
			git_branch, git_repo_name, git_repo_root, prev_command_id,
			is_sudo, pipe_count, word_count
		) VALUES (?, ?, ?, NULL, NULL, ?, ?, ?, ?, NULL, 1, NULL, NULL, NULL, NULL, ?, ?, ?)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Insert all entries
	imported := 0
	for _, entry := range entries {
		if entry.Command == "" {
			continue
		}

		// Use entry timestamp or fall back to current time
		var tsStart int64
		if !entry.Timestamp.IsZero() {
			tsStart = entry.Timestamp.UnixMilli()
		} else {
			// Use now + index to preserve ordering for entries without timestamps
			tsStart = now + int64(imported)
		}

		// Normalize and hash the command
		norm := cmdutil.NormalizeCommand(entry.Command)
		hash := cmdutil.HashCommand(norm)

		// Compute derived metadata
		isSudo := cmdutil.IsSudo(entry.Command)
		pipeCount := cmdutil.CountPipes(entry.Command)
		wordCount := cmdutil.CountWords(entry.Command)

		// Generate unique command ID
		cmdID := uuid.New().String()

		_, err = stmt.ExecContext(ctx,
			cmdID,
			sessionID,
			tsStart,
			"/", // CWD unknown for imported commands
			entry.Command,
			norm,
			hash,
			boolToInt(isSudo),
			pipeCount,
			wordCount,
		)
		if err != nil {
			// Skip individual failures (e.g., duplicate commands)
			continue
		}
		imported++
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return imported, nil
}

// boolToInt converts a bool to an int (0 or 1) for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
