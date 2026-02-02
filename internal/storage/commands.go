package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/runger/clai/internal/cmdutil"
)

// ErrCommandNotFound is returned when a command is not found.
var ErrCommandNotFound = errors.New("command not found")

// CreateCommand creates a new command record.
// It automatically normalizes the command and generates a hash.
func (s *SQLiteStore) CreateCommand(ctx context.Context, cmd *Command) error {
	if cmd == nil {
		return errors.New("command cannot be nil")
	}
	if cmd.CommandID == "" {
		return errors.New("command_id is required")
	}
	if cmd.SessionID == "" {
		return errors.New("session_id is required")
	}
	if cmd.CWD == "" {
		return errors.New("cwd is required")
	}
	if cmd.Command == "" {
		return errors.New("command is required")
	}

	// Normalize command if not already set
	if cmd.CommandNorm == "" {
		cmd.CommandNorm = cmdutil.NormalizeCommand(cmd.Command)
	}

	// Generate hash if not already set
	if cmd.CommandHash == "" {
		cmd.CommandHash = cmdutil.HashCommand(cmd.CommandNorm)
	}

	// Determine is_success value: nil = unknown (treated as success), false = failure, true = success
	var isSuccess *int
	if cmd.IsSuccess != nil {
		v := 0
		if *cmd.IsSuccess {
			v = 1
		}
		isSuccess = &v
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO commands (
			command_id, session_id, ts_start_unix_ms, ts_end_unix_ms,
			duration_ms, cwd, command, command_norm, command_hash,
			exit_code, is_success
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		cmd.CommandID,
		cmd.SessionID,
		cmd.TsStartUnixMs,
		cmd.TsEndUnixMs,
		cmd.DurationMs,
		cmd.CWD,
		cmd.Command,
		cmd.CommandNorm,
		cmd.CommandHash,
		cmd.ExitCode,
		isSuccess,
	)
	if err != nil {
		// Check for foreign key violation (invalid session_id)
		if isForeignKeyError(err) {
			return fmt.Errorf("session_id %s does not exist", cmd.SessionID)
		}
		if isDuplicateKeyError(err) {
			return fmt.Errorf("command with id %s already exists", cmd.CommandID)
		}
		return fmt.Errorf("failed to create command: %w", err)
	}

	// Get the auto-generated ID
	id, err := result.LastInsertId()
	if err == nil {
		cmd.ID = id
	}

	return nil
}

// UpdateCommandEnd updates a command's end time, duration, and exit code.
func (s *SQLiteStore) UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error {
	if commandID == "" {
		return errors.New("command_id is required")
	}

	isSuccess := 1
	if exitCode != 0 {
		isSuccess = 0
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE commands
		SET ts_end_unix_ms = ?, duration_ms = ?, exit_code = ?, is_success = ?
		WHERE command_id = ?
	`, endTime, duration, exitCode, isSuccess, commandID)
	if err != nil {
		return fmt.Errorf("failed to update command: %w", err)
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

// QueryCommands queries commands based on the given criteria.
func (s *SQLiteStore) QueryCommands(ctx context.Context, q CommandQuery) ([]Command, error) {
	// Build query dynamically based on provided filters
	query := `
		SELECT id, command_id, session_id, ts_start_unix_ms, ts_end_unix_ms,
		       duration_ms, cwd, command, command_norm, command_hash,
		       exit_code, is_success
		FROM commands
		WHERE 1=1
	`
	args := make([]interface{}, 0)

	if q.SessionID != nil {
		query += " AND session_id = ?"
		args = append(args, *q.SessionID)
	}

	if q.ExcludeSessionID != "" {
		query += " AND session_id != ?"
		args = append(args, q.ExcludeSessionID)
	}

	if q.CWD != nil {
		query += " AND cwd = ?"
		args = append(args, *q.CWD)
	}

	if q.Prefix != "" {
		query += " AND command_norm LIKE ?"
		args = append(args, q.Prefix+"%")
	}

	if q.SuccessOnly {
		query += " AND is_success = 1"
	}

	if q.FailureOnly {
		query += " AND is_success = 0"
	}

	query += " ORDER BY ts_start_unix_ms DESC"

	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	} else {
		// Default limit to prevent unbounded queries
		query += " LIMIT 1000"
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query commands: %w", err)
	}
	defer rows.Close()

	var commands []Command
	for rows.Next() {
		var cmd Command
		var endTime, duration sql.NullInt64
		var exitCode sql.NullInt32
		var isSuccess sql.NullInt32

		err := rows.Scan(
			&cmd.ID,
			&cmd.CommandID,
			&cmd.SessionID,
			&cmd.TsStartUnixMs,
			&endTime,
			&duration,
			&cmd.CWD,
			&cmd.Command,
			&cmd.CommandNorm,
			&cmd.CommandHash,
			&exitCode,
			&isSuccess,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan command: %w", err)
		}

		if endTime.Valid {
			cmd.TsEndUnixMs = &endTime.Int64
		}
		if duration.Valid {
			cmd.DurationMs = &duration.Int64
		}
		if exitCode.Valid {
			ec := int(exitCode.Int32)
			cmd.ExitCode = &ec
		}
		if isSuccess.Valid {
			v := isSuccess.Int32 == 1
			cmd.IsSuccess = &v
		}

		commands = append(commands, cmd)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating commands: %w", err)
	}

	return commands, nil
}

// NormalizeCommand normalizes a command for comparison and deduplication.
// It lowercases the command, trims whitespace, and removes variable arguments.
// This is a re-export of cmdutil.NormalizeCommand for backward compatibility.
var NormalizeCommand = cmdutil.NormalizeCommand

// HashCommand generates a SHA256 hash of a normalized command.
// This is a re-export of cmdutil.HashCommand for backward compatibility.
var HashCommand = cmdutil.HashCommand

// isForeignKeyError checks if the error is a foreign key constraint violation.
func isForeignKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "FOREIGN KEY constraint failed") ||
		contains(errStr, "foreign key constraint")
}
