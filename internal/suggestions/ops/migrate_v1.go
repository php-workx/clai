package ops

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// MigrateV1Data performs a one-time migration of data from state.db into the
// unified V2/V3 database. It is idempotent and non-fatal on failure.
func MigrateV1Data(ctx context.Context, v2db *suggestdb.DB, v1DBPath string, logger *slog.Logger) error {
	if v1DBPath == "" {
		return nil
	}

	// Check if state.db exists
	if _, err := os.Stat(v1DBPath); os.IsNotExist(err) {
		return nil
	}

	// Check sentinel to avoid re-running
	var exists int
	err := v2db.QueryRowContext(ctx, `
		SELECT 1 FROM schema_migrations WHERE version = -1
	`).Scan(&exists)
	if err == nil {
		// Already migrated
		return nil
	}

	logger.Info("migrating V1 data from state.db", "path", v1DBPath)

	// Open state.db read-only
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", v1DBPath)
	v1db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open V1 database: %w", err)
	}
	defer v1db.Close()

	if err := v1db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to connect to V1 database: %w", err)
	}

	// Migrate sessions
	if err := migrateV1Sessions(ctx, v1db, v2db); err != nil {
		logger.Warn("V1 session migration failed (non-fatal)", "error", err)
	}

	// Migrate commands
	if err := migrateV1Commands(ctx, v1db, v2db); err != nil {
		logger.Warn("V1 command migration failed (non-fatal)", "error", err)
	}

	// Migrate AI cache
	if err := migrateV1Cache(ctx, v1db, v2db); err != nil {
		logger.Warn("V1 cache migration failed (non-fatal)", "error", err)
	}

	// Migrate PTY capture tables
	if err := migrateV1PTY(ctx, v1db, v2db); err != nil {
		logger.Warn("V1 PTY migration failed (non-fatal)", "error", err)
	}

	// Migrate workflow tables
	if err := migrateV1Workflows(ctx, v1db, v2db); err != nil {
		logger.Warn("V1 workflow migration failed (non-fatal)", "error", err)
	}

	// Mark migration complete with sentinel
	_, _ = v2db.ExecContext(ctx, `
		INSERT OR IGNORE INTO schema_migrations (version, applied_ms) VALUES (-1, ?)
	`, 0)

	// Rename state.db to state.db.migrated (backup)
	migratedPath := v1DBPath + ".migrated"
	if err := os.Rename(v1DBPath, migratedPath); err != nil {
		logger.Warn("failed to rename state.db (non-fatal)", "error", err)
	} else {
		logger.Info("V1 data migration complete", "backup", migratedPath)
	}

	return nil
}

func migrateV1Sessions(ctx context.Context, v1db *sql.DB, v2db *suggestdb.DB) error {
	rows, err := v1db.QueryContext(ctx, `
		SELECT session_id, started_at_unix_ms, shell, os, hostname, username, initial_cwd
		FROM sessions
	`)
	if err != nil {
		return fmt.Errorf("failed to query V1 sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, shell string
		var startedAt int64
		var osName, hostname, username, initialCWD sql.NullString

		if err := rows.Scan(&id, &startedAt, &shell, &osName, &hostname, &username, &initialCWD); err != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO session (id, shell, started_at_ms, host, user_name, os, initial_cwd)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, id, shell, startedAt, hostname, username, osName, initialCWD)
	}
	return rows.Err()
}

func migrateV1Commands(ctx context.Context, v1db *sql.DB, v2db *suggestdb.DB) error {
	rows, err := v1db.QueryContext(ctx, `
		SELECT command_id, session_id, ts_start_unix_ms, cwd, command, command_norm,
		       exit_code, duration_ms, git_repo_name, git_branch
		FROM commands
	`)
	if err != nil {
		return fmt.Errorf("failed to query V1 commands: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var commandID, sessionID, cwd, cmdRaw, cmdNorm string
		var tsStart int64
		var exitCode sql.NullInt32
		var durationMs sql.NullInt64
		var repoKey, branch sql.NullString

		if err := rows.Scan(&commandID, &sessionID, &tsStart, &cwd, &cmdRaw, &cmdNorm,
			&exitCode, &durationMs, &repoKey, &branch); err != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO command_event (
				session_id, ts_ms, cwd, cmd_raw, cmd_norm, command_id,
				exit_code, duration_ms, repo_key, branch
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, sessionID, tsStart, cwd, cmdRaw, cmdNorm, commandID,
			exitCode, durationMs, repoKey, branch)
	}
	return rows.Err()
}

func migrateV1Cache(ctx context.Context, v1db *sql.DB, v2db *suggestdb.DB) error {
	rows, err := v1db.QueryContext(ctx, `
		SELECT cache_key, response_json, provider, created_at_unix_ms,
		       expires_at_unix_ms, hit_count
		FROM ai_cache
	`)
	if err != nil {
		return fmt.Errorf("failed to query V1 cache: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, respJSON, prov string
		var createdAt, expiresAt, hitCount int64

		if err := rows.Scan(&key, &respJSON, &prov, &createdAt, &expiresAt, &hitCount); err != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO ai_cache (
				cache_key, response_json, provider, created_at_ms, expires_at_ms, hit_count
			) VALUES (?, ?, ?, ?, ?, ?)
		`, key, respJSON, prov, createdAt, expiresAt, hitCount)
	}
	return rows.Err()
}

func migrateV1PTY(ctx context.Context, v1db *sql.DB, v2db *suggestdb.DB) error {
	// Migrate command_events -> pty_command_event
	rows, err := v1db.QueryContext(ctx, `
		SELECT command_id, session_id, start_ts, end_ts, exit_code, is_sensitive, captured_bytes
		FROM command_events
	`)
	if err != nil {
		// Table may not exist in older V1 schemas
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var cmdID, sessID string
		var startTS, endTS sql.NullInt64
		var exitCode sql.NullInt32
		var isSensitive, capturedBytes int64

		if scanErr := rows.Scan(&cmdID, &sessID, &startTS, &endTS, &exitCode, &isSensitive, &capturedBytes); scanErr != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO pty_command_event (
				command_id, session_id, start_ts, end_ts, exit_code, is_sensitive, captured_bytes
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`, cmdID, sessID, startTS, endTS, exitCode, isSensitive, capturedBytes)
	}

	// Migrate command_output -> pty_command_output
	outRows, err := v1db.QueryContext(ctx, `
		SELECT command_id, stdout_blob, stderr_blob, created_at, expires_at
		FROM command_output
	`)
	if err != nil {
		return nil
	}
	defer outRows.Close()

	for outRows.Next() {
		var cmdID string
		var stdout, stderr []byte
		var createdAt, expiresAt int64

		if scanErr := outRows.Scan(&cmdID, &stdout, &stderr, &createdAt, &expiresAt); scanErr != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO pty_command_output (
				command_id, stdout_blob, stderr_blob, created_at, expires_at
			) VALUES (?, ?, ?, ?, ?)
		`, cmdID, stdout, stderr, createdAt, expiresAt)
	}
	return nil
}

func migrateV1Workflows(ctx context.Context, v1db *sql.DB, v2db *suggestdb.DB) error {
	// Migrate workflow_runs -> ci_workflow_run
	rows, err := v1db.QueryContext(ctx, `
		SELECT run_id, workflow_name, workflow_hash, workflow_path,
		       status, started_at, ended_at, duration_ms
		FROM workflow_runs
	`)
	if err != nil {
		return nil // Table may not exist
	}
	defer rows.Close()

	for rows.Next() {
		var runID, name, hash, path, status string
		var startedAt, endedAt, durationMs int64

		if scanErr := rows.Scan(&runID, &name, &hash, &path, &status, &startedAt, &endedAt, &durationMs); scanErr != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO ci_workflow_run (
				run_id, workflow_name, workflow_hash, workflow_path,
				status, started_at, ended_at, duration_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, runID, name, hash, path, status, startedAt, endedAt, durationMs)
	}

	// Migrate workflow_steps -> ci_workflow_step
	stepRows, err := v1db.QueryContext(ctx, `
		SELECT run_id, step_id, matrix_key, status, command,
		       exit_code, duration_ms, stdout_tail, stderr_tail, outputs_json
		FROM workflow_steps
	`)
	if err != nil {
		return nil
	}
	defer stepRows.Close()

	for stepRows.Next() {
		var runID, stepID, matrixKey, status, command, stdoutTail, stderrTail, outputsJSON string
		var exitCode int
		var durationMs int64

		if scanErr := stepRows.Scan(&runID, &stepID, &matrixKey, &status, &command,
			&exitCode, &durationMs, &stdoutTail, &stderrTail, &outputsJSON); scanErr != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT OR IGNORE INTO ci_workflow_step (
				run_id, step_id, matrix_key, status, command,
				exit_code, duration_ms, stdout_tail, stderr_tail, outputs_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, runID, stepID, matrixKey, status, command, exitCode, durationMs, stdoutTail, stderrTail, outputsJSON)
	}

	// Migrate workflow_analyses -> ci_workflow_analysis
	analysisRows, err := v1db.QueryContext(ctx, `
		SELECT run_id, step_id, matrix_key, decision, reasoning,
		       flags_json, prompt, raw_response, duration_ms, analyzed_at
		FROM workflow_analyses
	`)
	if err != nil {
		return nil
	}
	defer analysisRows.Close()

	for analysisRows.Next() {
		var runID, stepID, matrixKey, decision, reasoning, flagsJSON, prompt, rawResp string
		var durationMs, analyzedAt int64

		if scanErr := analysisRows.Scan(&runID, &stepID, &matrixKey, &decision, &reasoning,
			&flagsJSON, &prompt, &rawResp, &durationMs, &analyzedAt); scanErr != nil {
			continue
		}

		_, _ = v2db.ExecContext(ctx, `
			INSERT INTO ci_workflow_analysis (
				run_id, step_id, matrix_key, decision, reasoning,
				flags_json, prompt, raw_response, duration_ms, analyzed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, runID, stepID, matrixKey, decision, reasoning, flagsJSON, prompt, rawResp, durationMs, analyzedAt)
	}
	return nil
}
