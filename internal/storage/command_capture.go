package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// UpsertCommandEventStart creates (or refreshes) a command_events row when
// command execution begins.
func (s *SQLiteStore) UpsertCommandEventStart(ctx context.Context, sessionID, commandID string, startTS int64) error {
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	if commandID == "" {
		return errors.New("command_id is required")
	}
	if startTS == 0 {
		startTS = time.Now().UnixMilli()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO command_events (
			session_id, command_id, start_ts, captured_bytes, is_sensitive
		) VALUES (?, ?, ?, 0, 0)
		ON CONFLICT(command_id) DO UPDATE SET
			session_id = excluded.session_id,
			start_ts = excluded.start_ts
	`, sessionID, commandID, startTS)
	if err != nil {
		return fmt.Errorf("failed to upsert command event start: %w", err)
	}
	return nil
}

// FinalizeCommandEvent updates end-of-command fields.
func (s *SQLiteStore) FinalizeCommandEvent(
	ctx context.Context,
	commandID string,
	exitCode int,
	endTS int64,
	isSensitive bool,
	capturedBytes int64,
) error {
	if commandID == "" {
		return errors.New("command_id is required")
	}
	if endTS == 0 {
		endTS = time.Now().UnixMilli()
	}

	sensitive := 0
	if isSensitive {
		sensitive = 1
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE command_events
		SET exit_code = ?, end_ts = ?, is_sensitive = ?, captured_bytes = ?
		WHERE command_id = ?
	`, exitCode, endTS, sensitive, capturedBytes, commandID)
	if err != nil {
		return fmt.Errorf("failed to finalize command event: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrCommandNotFound
	}
	return nil
}

// AppendCommandOutputChunk appends chunk bytes to stdout/stderr blobs for a
// command. It keeps a rolling expiry timestamp for TTL pruning.
func (s *SQLiteStore) AppendCommandOutputChunk(
	ctx context.Context,
	commandID string,
	chunk []byte,
	isStderr bool,
	createdAt,
	expiresAt int64,
) error {
	if commandID == "" {
		return errors.New("command_id is required")
	}
	if len(chunk) == 0 {
		return nil
	}
	if createdAt == 0 {
		createdAt = time.Now().UnixMilli()
	}
	if expiresAt == 0 {
		expiresAt = createdAt + int64((7*24*time.Hour)/time.Millisecond)
	}

	var stdoutChunk []byte
	var stderrChunk []byte
	if isStderr {
		stderrChunk = chunk
	} else {
		stdoutChunk = chunk
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO command_output (
			command_id, stdout_blob, stderr_blob, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(command_id) DO UPDATE SET
			stdout_blob = CASE
				WHEN excluded.stdout_blob IS NULL THEN command_output.stdout_blob
				ELSE COALESCE(command_output.stdout_blob, X'') || excluded.stdout_blob
			END,
			stderr_blob = CASE
				WHEN excluded.stderr_blob IS NULL THEN command_output.stderr_blob
				ELSE COALESCE(command_output.stderr_blob, X'') || excluded.stderr_blob
			END,
			expires_at = excluded.expires_at
	`, commandID, stdoutChunk, stderrChunk, createdAt, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to append command output chunk: %w", err)
	}
	return nil
}

// PruneExpiredCommandOutput removes expired command output blobs.
func (s *SQLiteStore) PruneExpiredCommandOutput(ctx context.Context) (int64, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM command_output WHERE expires_at <= ?
	`, now)
	if err != nil {
		return 0, fmt.Errorf("failed to prune command output: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rows, nil
}
