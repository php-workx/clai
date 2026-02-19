package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrWorkflowRunNotFound is returned when a workflow run is not found.
var ErrWorkflowRunNotFound = errors.New("workflow run not found")

// ErrWorkflowStepNotFound is returned when a workflow step is not found.
var ErrWorkflowStepNotFound = errors.New("workflow step not found")

// Validation error messages used across multiple methods.
const (
	errRunIDRequired  = "run_id is required"
	errStepIDRequired = "step_id is required"
)

// CreateWorkflowRun creates a new workflow run record.
func (s *SQLiteStore) CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error {
	if run == nil {
		return errors.New("workflow run cannot be nil")
	}
	if run.RunID == "" {
		return errors.New(errRunIDRequired)
	}
	if run.WorkflowName == "" {
		return errors.New("workflow_name is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_runs (
			run_id, workflow_name, workflow_hash, workflow_path,
			status, started_at, ended_at, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.RunID,
		run.WorkflowName,
		run.WorkflowHash,
		run.WorkflowPath,
		run.Status,
		run.StartedAt,
		run.EndedAt,
		run.DurationMs,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("workflow run with id %s already exists", run.RunID)
		}
		return fmt.Errorf("failed to create workflow run: %w", err)
	}
	return nil
}

// UpdateWorkflowRun updates a workflow run's status, end time, and duration.
func (s *SQLiteStore) UpdateWorkflowRun(ctx context.Context, runID, status string, endedAt, durationMs int64) error {
	if runID == "" {
		return errors.New(errRunIDRequired)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE workflow_runs SET status = ?, ended_at = ?, duration_ms = ?
		WHERE run_id = ?
	`, status, endedAt, durationMs, runID)
	if err != nil {
		return fmt.Errorf("failed to update workflow run: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrWorkflowRunNotFound
	}
	return nil
}

// GetWorkflowRun retrieves a workflow run by ID.
func (s *SQLiteStore) GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	if runID == "" {
		return nil, errors.New(errRunIDRequired)
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, workflow_name, workflow_hash, workflow_path,
		       status, started_at, ended_at, duration_ms
		FROM workflow_runs WHERE run_id = ?
	`, runID)

	var run WorkflowRun
	err := row.Scan(
		&run.RunID,
		&run.WorkflowName,
		&run.WorkflowHash,
		&run.WorkflowPath,
		&run.Status,
		&run.StartedAt,
		&run.EndedAt,
		&run.DurationMs,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorkflowRunNotFound
		}
		return nil, fmt.Errorf("failed to get workflow run: %w", err)
	}

	return &run, nil
}

// QueryWorkflowRuns queries workflow runs based on the given criteria.
func (s *SQLiteStore) QueryWorkflowRuns(ctx context.Context, q WorkflowRunQuery) ([]WorkflowRun, error) {
	query := `
		SELECT run_id, workflow_name, workflow_hash, workflow_path,
		       status, started_at, ended_at, duration_ms
		FROM workflow_runs
		WHERE 1=1
	`
	args := make([]interface{}, 0)

	if q.RunID != "" {
		query += " AND run_id = ?"
		args = append(args, q.RunID)
	}
	if q.WorkflowName != "" {
		query += " AND workflow_name = ?"
		args = append(args, q.WorkflowName)
	}
	if q.Status != "" {
		query += " AND status = ?"
		args = append(args, q.Status)
	}

	query += " ORDER BY started_at DESC"

	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	} else {
		query += " LIMIT 1000"
	}

	if q.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, q.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query workflow runs: %w", err)
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var run WorkflowRun
		err := rows.Scan(
			&run.RunID,
			&run.WorkflowName,
			&run.WorkflowHash,
			&run.WorkflowPath,
			&run.Status,
			&run.StartedAt,
			&run.EndedAt,
			&run.DurationMs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow run: %w", err)
		}
		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflow runs: %w", err)
	}

	return runs, nil
}

// CreateWorkflowStep creates a new workflow step record.
func (s *SQLiteStore) CreateWorkflowStep(ctx context.Context, step *WorkflowStep) error {
	if step == nil {
		return errors.New("workflow step cannot be nil")
	}
	if step.RunID == "" {
		return errors.New(errRunIDRequired)
	}
	if step.StepID == "" {
		return errors.New(errStepIDRequired)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_steps (
			run_id, step_id, matrix_key, status, command,
			exit_code, duration_ms, stdout_tail, stderr_tail, outputs_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		step.RunID,
		step.StepID,
		step.MatrixKey,
		step.Status,
		step.Command,
		step.ExitCode,
		step.DurationMs,
		step.StdoutTail,
		step.StderrTail,
		step.OutputsJSON,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("workflow step (%s, %s, %s) already exists", step.RunID, step.StepID, step.MatrixKey)
		}
		if isForeignKeyError(err) {
			return fmt.Errorf("workflow run %s does not exist", step.RunID)
		}
		return fmt.Errorf("failed to create workflow step: %w", err)
	}
	return nil
}

// UpdateWorkflowStep updates a workflow step's mutable fields.
func (s *SQLiteStore) UpdateWorkflowStep(ctx context.Context, update *WorkflowStepUpdate) error {
	if update == nil {
		return errors.New("workflow step update cannot be nil")
	}
	if update.RunID == "" {
		return errors.New(errRunIDRequired)
	}
	if update.StepID == "" {
		return errors.New(errStepIDRequired)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE workflow_steps
		SET status = ?, command = ?, exit_code = ?, duration_ms = ?,
		    stdout_tail = ?, stderr_tail = ?, outputs_json = ?
		WHERE run_id = ? AND step_id = ? AND matrix_key = ?
	`,
		update.Status,
		update.Command,
		update.ExitCode,
		update.DurationMs,
		update.StdoutTail,
		update.StderrTail,
		update.OutputsJSON,
		update.RunID,
		update.StepID,
		update.MatrixKey,
	)
	if err != nil {
		return fmt.Errorf("failed to update workflow step: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrWorkflowStepNotFound
	}
	return nil
}

// GetWorkflowStep retrieves a workflow step by its composite key.
func (s *SQLiteStore) GetWorkflowStep(ctx context.Context, runID, stepID, matrixKey string) (*WorkflowStep, error) {
	if runID == "" {
		return nil, errors.New(errRunIDRequired)
	}
	if stepID == "" {
		return nil, errors.New(errStepIDRequired)
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, step_id, matrix_key, status, command,
		       exit_code, duration_ms, stdout_tail, stderr_tail, outputs_json
		FROM workflow_steps
		WHERE run_id = ? AND step_id = ? AND matrix_key = ?
	`, runID, stepID, matrixKey)

	var step WorkflowStep
	err := row.Scan(
		&step.RunID,
		&step.StepID,
		&step.MatrixKey,
		&step.Status,
		&step.Command,
		&step.ExitCode,
		&step.DurationMs,
		&step.StdoutTail,
		&step.StderrTail,
		&step.OutputsJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorkflowStepNotFound
		}
		return nil, fmt.Errorf("failed to get workflow step: %w", err)
	}

	return &step, nil
}

// CreateWorkflowAnalysis creates a new workflow analysis record.
func (s *SQLiteStore) CreateWorkflowAnalysis(ctx context.Context, analysis *WorkflowAnalysis) error {
	if analysis == nil {
		return errors.New("workflow analysis cannot be nil")
	}
	if analysis.RunID == "" {
		return errors.New(errRunIDRequired)
	}
	if analysis.StepID == "" {
		return errors.New(errStepIDRequired)
	}
	if analysis.Decision == "" {
		return errors.New("decision is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_analyses (
			run_id, step_id, matrix_key, decision, reasoning,
			flags_json, prompt, raw_response, duration_ms, analyzed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		analysis.RunID,
		analysis.StepID,
		analysis.MatrixKey,
		analysis.Decision,
		analysis.Reasoning,
		analysis.FlagsJSON,
		analysis.Prompt,
		analysis.RawResponse,
		analysis.DurationMs,
		analysis.AnalyzedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create workflow analysis: %w", err)
	}
	return nil
}

// GetWorkflowAnalyses retrieves all analysis records for a given step.
func (s *SQLiteStore) GetWorkflowAnalyses(ctx context.Context, runID, stepID, matrixKey string) ([]WorkflowAnalysisRecord, error) {
	if runID == "" {
		return nil, errors.New(errRunIDRequired)
	}
	if stepID == "" {
		return nil, errors.New(errStepIDRequired)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, step_id, matrix_key, decision, reasoning,
		       flags_json, prompt, raw_response, duration_ms, analyzed_at
		FROM workflow_analyses
		WHERE run_id = ? AND step_id = ? AND matrix_key = ?
		ORDER BY analyzed_at ASC
	`, runID, stepID, matrixKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query workflow analyses: %w", err)
	}
	defer rows.Close()

	var records []WorkflowAnalysisRecord
	for rows.Next() {
		var rec WorkflowAnalysisRecord
		err := rows.Scan(
			&rec.ID,
			&rec.RunID,
			&rec.StepID,
			&rec.MatrixKey,
			&rec.Decision,
			&rec.Reasoning,
			&rec.FlagsJSON,
			&rec.Prompt,
			&rec.RawResponse,
			&rec.DurationMs,
			&rec.AnalyzedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow analysis: %w", err)
		}
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflow analyses: %w", err)
	}

	return records, nil
}
