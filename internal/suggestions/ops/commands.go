package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// Command represents a command executed in a session.
type Command struct {
	TSEndMs    *int64
	DurationMs *int64
	ExitCode   *int

	GitBranch  *string
	GitRepoKey *string

	CommandID string
	SessionID string
	CWD       string
	CmdRaw    string
	CmdNorm   string
	RepoKey   string
	Branch    string
	ID        int64
	TSStartMs int64
}

// CommandQuery defines parameters for querying commands.
type CommandQuery struct {
	SessionID        *string
	ExcludeSessionID string
	CWD              *string
	Prefix           string
	Substring        string
	Limit            int
	Offset           int
	SuccessOnly      bool
	FailureOnly      bool
	Deduplicate      bool
}

// HistoryRow represents a deduplicated command history entry.
type HistoryRow struct {
	Command     string
	TimestampMs int64
	CWD         string
	ExitCode    *int
}

// ErrCommandNotFound is returned when a command is not found.
var ErrCommandNotFound = errors.New("command not found")

// CreateCommand creates a new command record in the V2 command_event table.
func CreateCommand(ctx context.Context, db *suggestdb.DB, cmd *Command) error {
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
	if cmd.CmdRaw == "" {
		return errors.New("command is required")
	}

	var repoKey, branch interface{}
	if cmd.GitRepoKey != nil {
		repoKey = *cmd.GitRepoKey
	}
	if cmd.GitBranch != nil {
		branch = *cmd.GitBranch
	}

	result, err := db.ExecContext(ctx, `
		INSERT INTO command_event (
			session_id, ts_ms, cwd, repo_key, branch,
			cmd_raw, cmd_norm, command_id, exit_code, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		cmd.SessionID, cmd.TSStartMs, cmd.CWD, repoKey, branch,
		cmd.CmdRaw, cmd.CmdNorm, cmd.CommandID, cmd.ExitCode, cmd.DurationMs)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("command with id %s already exists", cmd.CommandID)
		}
		return fmt.Errorf("failed to create command: %w", err)
	}

	id, idErr := result.LastInsertId()
	if idErr == nil {
		cmd.ID = id
	}
	return nil
}

// UpdateCommandEnd updates a command's duration and exit code.
func UpdateCommandEnd(ctx context.Context, db *suggestdb.DB, commandID string, exitCode int, durationMs int64) error {
	if commandID == "" {
		return errors.New("command_id is required")
	}

	result, err := db.ExecContext(ctx, `
		UPDATE command_event
		SET exit_code = ?, duration_ms = ?
		WHERE command_id = ?
	`, exitCode, durationMs, commandID)
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
//
//nolint:gocritic // hugeParam: value receiver intentional for caller convenience
func QueryCommands(ctx context.Context, db *suggestdb.DB, q CommandQuery) ([]Command, error) {
	query := `
		SELECT id, session_id, ts_ms, cwd, repo_key, branch,
		       cmd_raw, cmd_norm, command_id, exit_code, duration_ms
		FROM command_event
		WHERE command_id IS NOT NULL AND command_id != ''
	`
	args := make([]interface{}, 0)
	query, args = appendQueryFilters(query, args, &q)
	query += " ORDER BY ts_ms DESC"
	query, args = appendLimitOffset(query, args, q.Limit, q.Offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query commands: %w", err)
	}
	defer rows.Close()

	var commands []Command
	for rows.Next() {
		cmd, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating commands: %w", err)
	}
	return commands, nil
}

// QueryHistoryCommands queries deduplicated command history rows.
//
//nolint:gocritic // hugeParam: value receiver intentional for caller convenience
func QueryHistoryCommands(ctx context.Context, db *suggestdb.DB, q CommandQuery) ([]HistoryRow, error) {
	q.Deduplicate = true

	inner := `SELECT cmd_raw, MAX(ts_ms) as latest_ts FROM command_event WHERE 1=1`
	args := make([]interface{}, 0)
	inner, args = appendQueryFilters(inner, args, &q)
	inner += " GROUP BY cmd_raw"

	query := fmt.Sprintf(`
		SELECT e.cmd_raw, e.ts_ms, e.cwd, e.exit_code
		FROM command_event e
		INNER JOIN (%s) g ON e.cmd_raw = g.cmd_raw AND e.ts_ms = g.latest_ts
		ORDER BY e.ts_ms DESC`,
		inner)
	query, args = appendLimitOffset(query, args, q.Limit, q.Offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query history commands: %w", err)
	}
	defer rows.Close()

	var results []HistoryRow
	for rows.Next() {
		var row HistoryRow
		var exitCode sql.NullInt32
		if err := rows.Scan(&row.Command, &row.TimestampMs, &row.CWD, &exitCode); err != nil {
			return nil, fmt.Errorf("failed to scan history row: %w", err)
		}
		if exitCode.Valid {
			ec := int(exitCode.Int32)
			row.ExitCode = &ec
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating history rows: %w", err)
	}
	return results, nil
}

//nolint:gocritic // unnamedResult: internal helper
func appendQueryFilters(query string, args []interface{}, q *CommandQuery) (string, []interface{}) {
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
		query += " AND cmd_norm LIKE ?"
		args = append(args, q.Prefix+"%")
	}
	if q.Substring != "" {
		query += " AND cmd_norm LIKE ?"
		args = append(args, "%"+q.Substring+"%")
	}
	if q.SuccessOnly {
		query += " AND exit_code = 0"
	}
	if q.FailureOnly {
		query += " AND exit_code IS NOT NULL AND exit_code != 0"
	}
	return query, args
}

//nolint:gocritic // unnamedResult: internal helper
func appendLimitOffset(query string, args []interface{}, limit, offset int) (string, []interface{}) {
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	} else {
		query += " LIMIT 1000"
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}
	return query, args
}

func scanCommand(rows *sql.Rows) (Command, error) {
	var cmd Command
	var repoKey, branch, commandID sql.NullString
	var exitCode sql.NullInt32
	var durationMs sql.NullInt64

	err := rows.Scan(
		&cmd.ID, &cmd.SessionID, &cmd.TSStartMs, &cmd.CWD,
		&repoKey, &branch,
		&cmd.CmdRaw, &cmd.CmdNorm, &commandID, &exitCode, &durationMs,
	)
	if err != nil {
		return cmd, fmt.Errorf("failed to scan command: %w", err)
	}
	if repoKey.Valid {
		cmd.RepoKey = repoKey.String
		cmd.GitRepoKey = &repoKey.String
	}
	if branch.Valid {
		cmd.Branch = branch.String
		cmd.GitBranch = &branch.String
	}
	if commandID.Valid {
		cmd.CommandID = commandID.String
	}
	if exitCode.Valid {
		ec := int(exitCode.Int32)
		cmd.ExitCode = &ec
	}
	if durationMs.Valid {
		cmd.DurationMs = &durationMs.Int64
	}
	return cmd, nil
}
