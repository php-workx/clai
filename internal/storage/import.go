package storage

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := resetImportedShellData(ctx, tx, sessionID); err != nil {
		return 0, err
	}
	if err := createImportedSession(ctx, tx, sessionID, importSessionStart(entries, now), shell); err != nil {
		return 0, err
	}

	stmt, err := prepareImportCommandStmt(ctx, tx)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	imported, err := insertImportedEntries(ctx, stmt, entries, sessionID, now)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return imported, nil
}

func resetImportedShellData(ctx context.Context, tx *sql.Tx, sessionID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM commands WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("failed to delete old imports: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("failed to delete old session: %w", err)
	}
	return nil
}

func createImportedSession(ctx context.Context, tx *sql.Tx, sessionID string, sessionStart int64, shell string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (
			session_id, started_at_unix_ms, ended_at_unix_ms,
			shell, os, hostname, username, initial_cwd
		) VALUES (?, ?, NULL, ?, ?, NULL, NULL, ?)
	`, sessionID, sessionStart, shell, runtime.GOOS, "/")
	if err != nil {
		return fmt.Errorf("failed to create import session: %w", err)
	}
	return nil
}

func prepareImportCommandStmt(ctx context.Context, tx *sql.Tx) (*sql.Stmt, error) {
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
		return nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	return stmt, nil
}

func insertImportedEntries(
	ctx context.Context,
	stmt *sql.Stmt,
	entries []history.ImportEntry,
	sessionID string,
	now int64,
) (int, error) {
	imported := 0
	for _, entry := range entries {
		if entry.Command == "" {
			continue
		}

		tsStart := importEntryTimestamp(entry, now+int64(imported))

		norm := cmdutil.NormalizeCommand(entry.Command)
		hash := cmdutil.HashCommand(norm)

		isSudo := cmdutil.IsSudo(entry.Command)
		pipeCount := cmdutil.CountPipes(entry.Command)
		wordCount := cmdutil.CountWords(entry.Command)

		cmdID := uuid.New().String()

		_, err := stmt.ExecContext(ctx,
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
			if isUniqueConstraintError(err) {
				continue
			}
			return 0, fmt.Errorf("failed to insert command: %w", err)
		}
		imported++
	}
	return imported, nil
}

func importSessionStart(entries []history.ImportEntry, fallback int64) int64 {
	if entries[0].Timestamp.IsZero() {
		return fallback
	}
	return entries[0].Timestamp.UnixMilli()
}

func importEntryTimestamp(entry history.ImportEntry, fallback int64) int64 {
	if entry.Timestamp.IsZero() {
		return fallback
	}
	return entry.Timestamp.UnixMilli()
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// boolToInt converts a bool to an int (0 or 1) for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
