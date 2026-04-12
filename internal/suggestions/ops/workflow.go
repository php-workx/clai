package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// WorkflowRun represents a CI workflow execution run.
type WorkflowRun struct {
	RunID        string
	WorkflowName string
	WorkflowHash string
	WorkflowPath string
	Status       string
	StartedAt    int64
	EndedAt      int64
	DurationMs   int64
}

// WorkflowStep represents a single step within a workflow run.
type WorkflowStep struct {
	RunID       string
	StepID      string
	MatrixKey   string
	Status      string
	Command     string
	StdoutTail  string
	StderrTail  string
	OutputsJSON string
	DurationMs  int64
	ExitCode    int
}

// WorkflowStepUpdate contains fields for updating a workflow step.
type WorkflowStepUpdate struct {
	RunID       string
	StepID      string
	MatrixKey   string
	Status      string
	Command     string
	StdoutTail  string
	StderrTail  string
	OutputsJSON string
	DurationMs  int64
	ExitCode    int
}

// WorkflowAnalysis represents an AI analysis of a workflow step.
type WorkflowAnalysis struct {
	RunID       string
	StepID      string
	MatrixKey   string
	Decision    string
	Reasoning   string
	FlagsJSON   string
	Prompt      string
	RawResponse string
	DurationMs  int64
	AnalyzedAt  int64
}

// WorkflowAnalysisRecord is a stored analysis record with an auto-generated ID.
type WorkflowAnalysisRecord struct {
	RunID       string
	StepID      string
	MatrixKey   string
	Decision    string
	Reasoning   string
	FlagsJSON   string
	Prompt      string
	RawResponse string
	ID          int64
	DurationMs  int64
	AnalyzedAt  int64
}

// WorkflowRunQuery defines parameters for querying workflow runs.
type WorkflowRunQuery struct {
	RunID        string
	WorkflowName string
	Status       string
	Limit        int
	Offset       int
}

// Sentinel errors for workflow operations.
var (
	ErrWorkflowRunNotFound  = errors.New("workflow run not found")
	ErrWorkflowStepNotFound = errors.New("workflow step not found")
)

// CreateWorkflowRun creates a new workflow run record.
func CreateWorkflowRun(ctx context.Context, db *suggestdb.DB, run *WorkflowRun) error {
	if run == nil {
		return errors.New("workflow run cannot be nil")
	}
	if run.RunID == "" {
		return errors.New("run_id is required")
	}
	if run.WorkflowName == "" {
		return errors.New("workflow_name is required")
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO ci_workflow_run (
			run_id, workflow_name, workflow_hash, workflow_path,
			status, started_at, ended_at, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, run.RunID, run.WorkflowName, run.WorkflowHash, run.WorkflowPath,
		run.Status, run.StartedAt, run.EndedAt, run.DurationMs)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("workflow run with id %s already exists", run.RunID)
		}
		return fmt.Errorf("failed to create workflow run: %w", err)
	}
	return nil
}

// UpdateWorkflowRun updates a workflow run's status, end time, and duration.
func UpdateWorkflowRun(ctx context.Context, db *suggestdb.DB, runID, status string, endedAt, durationMs int64) error {
	if runID == "" {
		return errors.New("run_id is required")
	}

	result, err := db.ExecContext(ctx, `
		UPDATE ci_workflow_run SET status = ?, ended_at = ?, duration_ms = ?
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
func GetWorkflowRun(ctx context.Context, db *suggestdb.DB, runID string) (*WorkflowRun, error) {
	if runID == "" {
		return nil, errors.New("run_id is required")
	}

	row := db.QueryRowContext(ctx, `
		SELECT run_id, workflow_name, workflow_hash, workflow_path,
		       status, started_at, ended_at, duration_ms
		FROM ci_workflow_run WHERE run_id = ?
	`, runID)

	var run WorkflowRun
	err := row.Scan(&run.RunID, &run.WorkflowName, &run.WorkflowHash,
		&run.WorkflowPath, &run.Status, &run.StartedAt, &run.EndedAt, &run.DurationMs)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorkflowRunNotFound
		}
		return nil, fmt.Errorf("failed to get workflow run: %w", err)
	}
	return &run, nil
}

// QueryWorkflowRuns queries workflow runs based on given criteria.
func QueryWorkflowRuns(ctx context.Context, db *suggestdb.DB, q WorkflowRunQuery) ([]WorkflowRun, error) {
	query := `
		SELECT run_id, workflow_name, workflow_hash, workflow_path,
		       status, started_at, ended_at, duration_ms
		FROM ci_workflow_run WHERE 1=1
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
	query, args = appendLimitOffset(query, args, q.Limit, q.Offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query workflow runs: %w", err)
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var run WorkflowRun
		err := rows.Scan(&run.RunID, &run.WorkflowName, &run.WorkflowHash,
			&run.WorkflowPath, &run.Status, &run.StartedAt, &run.EndedAt, &run.DurationMs)
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
func CreateWorkflowStep(ctx context.Context, db *suggestdb.DB, step *WorkflowStep) error {
	if step == nil {
		return errors.New("workflow step cannot be nil")
	}
	if step.RunID == "" {
		return errors.New("run_id is required")
	}
	if step.StepID == "" {
		return errors.New("step_id is required")
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO ci_workflow_step (
			run_id, step_id, matrix_key, status, command,
			exit_code, duration_ms, stdout_tail, stderr_tail, outputs_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, step.RunID, step.StepID, step.MatrixKey, step.Status, step.Command,
		step.ExitCode, step.DurationMs, step.StdoutTail, step.StderrTail, step.OutputsJSON)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("workflow step (%s, %s, %s) already exists", step.RunID, step.StepID, step.MatrixKey)
		}
		return fmt.Errorf("failed to create workflow step: %w", err)
	}
	return nil
}

// UpdateWorkflowStep updates a workflow step's mutable fields.
func UpdateWorkflowStep(ctx context.Context, db *suggestdb.DB, update *WorkflowStepUpdate) error {
	if update == nil {
		return errors.New("workflow step update cannot be nil")
	}
	if update.RunID == "" {
		return errors.New("run_id is required")
	}
	if update.StepID == "" {
		return errors.New("step_id is required")
	}

	result, err := db.ExecContext(ctx, `
		UPDATE ci_workflow_step
		SET status = ?, command = ?, exit_code = ?, duration_ms = ?,
		    stdout_tail = ?, stderr_tail = ?, outputs_json = ?
		WHERE run_id = ? AND step_id = ? AND matrix_key = ?
	`, update.Status, update.Command, update.ExitCode, update.DurationMs,
		update.StdoutTail, update.StderrTail, update.OutputsJSON,
		update.RunID, update.StepID, update.MatrixKey)
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
func GetWorkflowStep(ctx context.Context, db *suggestdb.DB, runID, stepID, matrixKey string) (*WorkflowStep, error) {
	if runID == "" {
		return nil, errors.New("run_id is required")
	}
	if stepID == "" {
		return nil, errors.New("step_id is required")
	}

	row := db.QueryRowContext(ctx, `
		SELECT run_id, step_id, matrix_key, status, command,
		       exit_code, duration_ms, stdout_tail, stderr_tail, outputs_json
		FROM ci_workflow_step
		WHERE run_id = ? AND step_id = ? AND matrix_key = ?
	`, runID, stepID, matrixKey)

	var step WorkflowStep
	err := row.Scan(&step.RunID, &step.StepID, &step.MatrixKey, &step.Status, &step.Command,
		&step.ExitCode, &step.DurationMs, &step.StdoutTail, &step.StderrTail, &step.OutputsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorkflowStepNotFound
		}
		return nil, fmt.Errorf("failed to get workflow step: %w", err)
	}
	return &step, nil
}

// CreateWorkflowAnalysis creates a new workflow analysis record.
func CreateWorkflowAnalysis(ctx context.Context, db *suggestdb.DB, analysis *WorkflowAnalysis) error {
	if analysis == nil {
		return errors.New("workflow analysis cannot be nil")
	}
	if analysis.RunID == "" {
		return errors.New("run_id is required")
	}
	if analysis.StepID == "" {
		return errors.New("step_id is required")
	}
	if analysis.Decision == "" {
		return errors.New("decision is required")
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO ci_workflow_analysis (
			run_id, step_id, matrix_key, decision, reasoning,
			flags_json, prompt, raw_response, duration_ms, analyzed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, analysis.RunID, analysis.StepID, analysis.MatrixKey,
		analysis.Decision, analysis.Reasoning, analysis.FlagsJSON,
		analysis.Prompt, analysis.RawResponse, analysis.DurationMs, analysis.AnalyzedAt)
	if err != nil {
		return fmt.Errorf("failed to create workflow analysis: %w", err)
	}
	return nil
}

// GetWorkflowAnalyses retrieves all analysis records for a given step.
func GetWorkflowAnalyses(ctx context.Context, db *suggestdb.DB, runID, stepID, matrixKey string) ([]WorkflowAnalysisRecord, error) {
	if runID == "" {
		return nil, errors.New("run_id is required")
	}
	if stepID == "" {
		return nil, errors.New("step_id is required")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, step_id, matrix_key, decision, reasoning,
		       flags_json, prompt, raw_response, duration_ms, analyzed_at
		FROM ci_workflow_analysis
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
		err := rows.Scan(&rec.ID, &rec.RunID, &rec.StepID, &rec.MatrixKey,
			&rec.Decision, &rec.Reasoning, &rec.FlagsJSON,
			&rec.Prompt, &rec.RawResponse, &rec.DurationMs, &rec.AnalyzedAt)
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
