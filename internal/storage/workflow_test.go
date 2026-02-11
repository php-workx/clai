package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// --- WorkflowRun CRUD ---

func TestCreateWorkflowRun_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	run := &WorkflowRun{
		RunID:        "run-1",
		WorkflowName: "ci-build",
		WorkflowHash: "abc123",
		WorkflowPath: ".clai/workflows/ci.yaml",
		Status:       "running",
		StartedAt:    1000,
	}

	if err := store.CreateWorkflowRun(ctx, run); err != nil {
		t.Fatalf("CreateWorkflowRun() error = %v", err)
	}

	// Verify it was created
	got, err := store.GetWorkflowRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetWorkflowRun() error = %v", err)
	}

	if got.RunID != "run-1" {
		t.Errorf("RunID = %s, want run-1", got.RunID)
	}
	if got.WorkflowName != "ci-build" {
		t.Errorf("WorkflowName = %s, want ci-build", got.WorkflowName)
	}
	if got.WorkflowHash != "abc123" {
		t.Errorf("WorkflowHash = %s, want abc123", got.WorkflowHash)
	}
	if got.Status != "running" {
		t.Errorf("Status = %s, want running", got.Status)
	}
	if got.StartedAt != 1000 {
		t.Errorf("StartedAt = %d, want 1000", got.StartedAt)
	}
}

func TestCreateWorkflowRun_DuplicateID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	run := &WorkflowRun{
		RunID:        "run-dup",
		WorkflowName: "test",
		WorkflowHash: "hash1",
		WorkflowPath: "path1",
		Status:       "running",
		StartedAt:    1000,
	}

	if err := store.CreateWorkflowRun(ctx, run); err != nil {
		t.Fatalf("CreateWorkflowRun() first call error = %v", err)
	}

	// Duplicate should fail
	err := store.CreateWorkflowRun(ctx, run)
	if err == nil {
		t.Fatal("CreateWorkflowRun() expected error for duplicate, got nil")
	}
}

func TestCreateWorkflowRun_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Nil run
	if err := store.CreateWorkflowRun(ctx, nil); err == nil {
		t.Error("CreateWorkflowRun(nil) expected error")
	}

	// Empty RunID
	if err := store.CreateWorkflowRun(ctx, &WorkflowRun{WorkflowName: "test"}); err == nil {
		t.Error("CreateWorkflowRun() with empty RunID expected error")
	}

	// Empty WorkflowName
	if err := store.CreateWorkflowRun(ctx, &WorkflowRun{RunID: "r1"}); err == nil {
		t.Error("CreateWorkflowRun() with empty WorkflowName expected error")
	}
}

func TestUpdateWorkflowRun_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	run := &WorkflowRun{
		RunID:        "run-update",
		WorkflowName: "ci-build",
		WorkflowHash: "abc123",
		WorkflowPath: "path",
		Status:       "running",
		StartedAt:    1000,
	}
	if err := store.CreateWorkflowRun(ctx, run); err != nil {
		t.Fatalf("CreateWorkflowRun() error = %v", err)
	}

	// Update it
	if err := store.UpdateWorkflowRun(ctx, "run-update", "passed", 2000, 1000); err != nil {
		t.Fatalf("UpdateWorkflowRun() error = %v", err)
	}

	got, err := store.GetWorkflowRun(ctx, "run-update")
	if err != nil {
		t.Fatalf("GetWorkflowRun() error = %v", err)
	}

	if got.Status != "passed" {
		t.Errorf("Status = %s, want passed", got.Status)
	}
	if got.EndedAt != 2000 {
		t.Errorf("EndedAt = %d, want 2000", got.EndedAt)
	}
	if got.DurationMs != 1000 {
		t.Errorf("DurationMs = %d, want 1000", got.DurationMs)
	}
}

func TestUpdateWorkflowRun_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	err := store.UpdateWorkflowRun(ctx, "nonexistent", "passed", 2000, 1000)
	if !errors.Is(err, ErrWorkflowRunNotFound) {
		t.Errorf("UpdateWorkflowRun() error = %v, want ErrWorkflowRunNotFound", err)
	}
}

func TestGetWorkflowRun_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	_, err := store.GetWorkflowRun(ctx, "nonexistent")
	if !errors.Is(err, ErrWorkflowRunNotFound) {
		t.Errorf("GetWorkflowRun() error = %v, want ErrWorkflowRunNotFound", err)
	}
}

func TestGetWorkflowRun_EmptyID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	_, err := store.GetWorkflowRun(ctx, "")
	if err == nil {
		t.Error("GetWorkflowRun() with empty ID expected error")
	}
}

// --- QueryWorkflowRuns ---

func TestQueryWorkflowRuns_All(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	runs := []WorkflowRun{
		{RunID: "run-q1", WorkflowName: "build", WorkflowHash: "h1", WorkflowPath: "p1", Status: "passed", StartedAt: 1000},
		{RunID: "run-q2", WorkflowName: "test", WorkflowHash: "h2", WorkflowPath: "p2", Status: "failed", StartedAt: 2000},
		{RunID: "run-q3", WorkflowName: "build", WorkflowHash: "h3", WorkflowPath: "p3", Status: "running", StartedAt: 3000},
	}
	for i := range runs {
		if err := store.CreateWorkflowRun(ctx, &runs[i]); err != nil {
			t.Fatalf("CreateWorkflowRun() error = %v", err)
		}
	}

	got, err := store.QueryWorkflowRuns(ctx, WorkflowRunQuery{})
	if err != nil {
		t.Fatalf("QueryWorkflowRuns() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("QueryWorkflowRuns() returned %d runs, want 3", len(got))
	}

	// Should be ordered by started_at DESC
	if got[0].RunID != "run-q3" {
		t.Errorf("first result RunID = %s, want run-q3", got[0].RunID)
	}
}

func TestQueryWorkflowRuns_FilterByName(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	runs := []WorkflowRun{
		{RunID: "run-fn1", WorkflowName: "build", WorkflowHash: "h1", WorkflowPath: "p1", Status: "passed", StartedAt: 1000},
		{RunID: "run-fn2", WorkflowName: "test", WorkflowHash: "h2", WorkflowPath: "p2", Status: "passed", StartedAt: 2000},
		{RunID: "run-fn3", WorkflowName: "build", WorkflowHash: "h3", WorkflowPath: "p3", Status: "failed", StartedAt: 3000},
	}
	for i := range runs {
		if err := store.CreateWorkflowRun(ctx, &runs[i]); err != nil {
			t.Fatalf("CreateWorkflowRun() error = %v", err)
		}
	}

	got, err := store.QueryWorkflowRuns(ctx, WorkflowRunQuery{WorkflowName: "build"})
	if err != nil {
		t.Fatalf("QueryWorkflowRuns() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("QueryWorkflowRuns() returned %d runs, want 2", len(got))
	}
}

func TestQueryWorkflowRuns_FilterByStatus(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	runs := []WorkflowRun{
		{RunID: "run-fs1", WorkflowName: "build", WorkflowHash: "h1", WorkflowPath: "p1", Status: "passed", StartedAt: 1000},
		{RunID: "run-fs2", WorkflowName: "test", WorkflowHash: "h2", WorkflowPath: "p2", Status: "failed", StartedAt: 2000},
		{RunID: "run-fs3", WorkflowName: "lint", WorkflowHash: "h3", WorkflowPath: "p3", Status: "passed", StartedAt: 3000},
	}
	for i := range runs {
		if err := store.CreateWorkflowRun(ctx, &runs[i]); err != nil {
			t.Fatalf("CreateWorkflowRun() error = %v", err)
		}
	}

	got, err := store.QueryWorkflowRuns(ctx, WorkflowRunQuery{Status: "passed"})
	if err != nil {
		t.Fatalf("QueryWorkflowRuns() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("QueryWorkflowRuns() returned %d runs, want 2", len(got))
	}
}

func TestQueryWorkflowRuns_Pagination(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		run := &WorkflowRun{
			RunID:        fmt.Sprintf("run-pg%d", i),
			WorkflowName: "build",
			WorkflowHash: "h",
			WorkflowPath: "p",
			Status:       "passed",
			StartedAt:    int64(1000 + i),
		}
		if err := store.CreateWorkflowRun(ctx, run); err != nil {
			t.Fatalf("CreateWorkflowRun() error = %v", err)
		}
	}

	// Page 1
	got, err := store.QueryWorkflowRuns(ctx, WorkflowRunQuery{Limit: 2})
	if err != nil {
		t.Fatalf("QueryWorkflowRuns() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("QueryWorkflowRuns() page 1 returned %d runs, want 2", len(got))
	}

	// Page 2
	got, err = store.QueryWorkflowRuns(ctx, WorkflowRunQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("QueryWorkflowRuns() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("QueryWorkflowRuns() page 2 returned %d runs, want 2", len(got))
	}
}

// --- WorkflowStep CRUD ---

func TestCreateWorkflowStep_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create parent run first
	run := &WorkflowRun{
		RunID:        "run-step1",
		WorkflowName: "build",
		WorkflowHash: "h",
		WorkflowPath: "p",
		Status:       "running",
		StartedAt:    1000,
	}
	if err := store.CreateWorkflowRun(ctx, run); err != nil {
		t.Fatalf("CreateWorkflowRun() error = %v", err)
	}

	step := &WorkflowStep{
		RunID:       "run-step1",
		StepID:      "step-1",
		MatrixKey:   "",
		Status:      "running",
		Command:     "make build",
		ExitCode:    0,
		DurationMs:  0,
		StdoutTail:  "",
		StderrTail:  "",
		OutputsJSON: "{}",
	}

	if err := store.CreateWorkflowStep(ctx, step); err != nil {
		t.Fatalf("CreateWorkflowStep() error = %v", err)
	}

	// Retrieve it
	got, err := store.GetWorkflowStep(ctx, "run-step1", "step-1", "")
	if err != nil {
		t.Fatalf("GetWorkflowStep() error = %v", err)
	}

	if got.RunID != "run-step1" {
		t.Errorf("RunID = %s, want run-step1", got.RunID)
	}
	if got.StepID != "step-1" {
		t.Errorf("StepID = %s, want step-1", got.StepID)
	}
	if got.Command != "make build" {
		t.Errorf("Command = %s, want make build", got.Command)
	}
}

func TestCreateWorkflowStep_CompositeKey(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create parent run
	run := &WorkflowRun{
		RunID:        "run-comp",
		WorkflowName: "build",
		WorkflowHash: "h",
		WorkflowPath: "p",
		Status:       "running",
		StartedAt:    1000,
	}
	if err := store.CreateWorkflowRun(ctx, run); err != nil {
		t.Fatalf("CreateWorkflowRun() error = %v", err)
	}

	// Same step_id, different matrix_key should both succeed
	step1 := &WorkflowStep{
		RunID:       "run-comp",
		StepID:      "test",
		MatrixKey:   "go1.21",
		Status:      "running",
		OutputsJSON: "{}",
	}
	step2 := &WorkflowStep{
		RunID:       "run-comp",
		StepID:      "test",
		MatrixKey:   "go1.22",
		Status:      "running",
		OutputsJSON: "{}",
	}

	if err := store.CreateWorkflowStep(ctx, step1); err != nil {
		t.Fatalf("CreateWorkflowStep() step1 error = %v", err)
	}
	if err := store.CreateWorkflowStep(ctx, step2); err != nil {
		t.Fatalf("CreateWorkflowStep() step2 error = %v", err)
	}

	// Retrieve each by composite key
	got1, err := store.GetWorkflowStep(ctx, "run-comp", "test", "go1.21")
	if err != nil {
		t.Fatalf("GetWorkflowStep(go1.21) error = %v", err)
	}
	if got1.MatrixKey != "go1.21" {
		t.Errorf("MatrixKey = %s, want go1.21", got1.MatrixKey)
	}

	got2, err := store.GetWorkflowStep(ctx, "run-comp", "test", "go1.22")
	if err != nil {
		t.Fatalf("GetWorkflowStep(go1.22) error = %v", err)
	}
	if got2.MatrixKey != "go1.22" {
		t.Errorf("MatrixKey = %s, want go1.22", got2.MatrixKey)
	}

	// Duplicate composite key should fail
	err = store.CreateWorkflowStep(ctx, step1)
	if err == nil {
		t.Fatal("CreateWorkflowStep() expected error for duplicate composite key, got nil")
	}
}

func TestCreateWorkflowStep_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Nil step
	if err := store.CreateWorkflowStep(ctx, nil); err == nil {
		t.Error("CreateWorkflowStep(nil) expected error")
	}

	// Empty RunID
	if err := store.CreateWorkflowStep(ctx, &WorkflowStep{StepID: "s1"}); err == nil {
		t.Error("CreateWorkflowStep() with empty RunID expected error")
	}

	// Empty StepID
	if err := store.CreateWorkflowStep(ctx, &WorkflowStep{RunID: "r1"}); err == nil {
		t.Error("CreateWorkflowStep() with empty StepID expected error")
	}
}

func TestUpdateWorkflowStep_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create parent run and step
	run := &WorkflowRun{
		RunID:        "run-upd-step",
		WorkflowName: "build",
		WorkflowHash: "h",
		WorkflowPath: "p",
		Status:       "running",
		StartedAt:    1000,
	}
	if err := store.CreateWorkflowRun(ctx, run); err != nil {
		t.Fatalf("CreateWorkflowRun() error = %v", err)
	}

	step := &WorkflowStep{
		RunID:       "run-upd-step",
		StepID:      "step-1",
		MatrixKey:   "",
		Status:      "running",
		Command:     "make test",
		OutputsJSON: "{}",
	}
	if err := store.CreateWorkflowStep(ctx, step); err != nil {
		t.Fatalf("CreateWorkflowStep() error = %v", err)
	}

	// Update the step
	update := &WorkflowStepUpdate{
		RunID:       "run-upd-step",
		StepID:      "step-1",
		MatrixKey:   "",
		Status:      "passed",
		ExitCode:    0,
		DurationMs:  5000,
		StdoutTail:  "PASS",
		StderrTail:  "",
		OutputsJSON: `{"result":"ok"}`,
	}
	if err := store.UpdateWorkflowStep(ctx, update); err != nil {
		t.Fatalf("UpdateWorkflowStep() error = %v", err)
	}

	got, err := store.GetWorkflowStep(ctx, "run-upd-step", "step-1", "")
	if err != nil {
		t.Fatalf("GetWorkflowStep() error = %v", err)
	}

	if got.Status != "passed" {
		t.Errorf("Status = %s, want passed", got.Status)
	}
	if got.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", got.DurationMs)
	}
	if got.StdoutTail != "PASS" {
		t.Errorf("StdoutTail = %s, want PASS", got.StdoutTail)
	}
	if got.OutputsJSON != `{"result":"ok"}` {
		t.Errorf("OutputsJSON = %s, want {\"result\":\"ok\"}", got.OutputsJSON)
	}
}

func TestUpdateWorkflowStep_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	update := &WorkflowStepUpdate{
		RunID:     "nonexistent-run",
		StepID:    "nonexistent-step",
		MatrixKey: "",
		Status:    "passed",
	}
	err := store.UpdateWorkflowStep(ctx, update)
	if !errors.Is(err, ErrWorkflowStepNotFound) {
		t.Errorf("UpdateWorkflowStep() error = %v, want ErrWorkflowStepNotFound", err)
	}
}

func TestGetWorkflowStep_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	_, err := store.GetWorkflowStep(ctx, "nonexistent", "step", "")
	if !errors.Is(err, ErrWorkflowStepNotFound) {
		t.Errorf("GetWorkflowStep() error = %v, want ErrWorkflowStepNotFound", err)
	}
}

func TestGetWorkflowStep_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Empty RunID
	_, err := store.GetWorkflowStep(ctx, "", "step", "")
	if err == nil {
		t.Error("GetWorkflowStep() with empty RunID expected error")
	}

	// Empty StepID
	_, err = store.GetWorkflowStep(ctx, "run", "", "")
	if err == nil {
		t.Error("GetWorkflowStep() with empty StepID expected error")
	}
}

// --- WorkflowAnalysis CRUD ---

func TestCreateWorkflowAnalysis_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	analysis := &WorkflowAnalysis{
		RunID:       "run-an1",
		StepID:      "step-1",
		MatrixKey:   "",
		Decision:    "approve",
		Reasoning:   "All tests passed",
		FlagsJSON:   "{}",
		Prompt:      "Analyze this step",
		RawResponse: "Looks good",
		DurationMs:  250,
		AnalyzedAt:  5000,
	}

	if err := store.CreateWorkflowAnalysis(ctx, analysis); err != nil {
		t.Fatalf("CreateWorkflowAnalysis() error = %v", err)
	}

	// Retrieve it
	records, err := store.GetWorkflowAnalyses(ctx, "run-an1", "step-1", "")
	if err != nil {
		t.Fatalf("GetWorkflowAnalyses() error = %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("GetWorkflowAnalyses() returned %d records, want 1", len(records))
	}

	rec := records[0]
	if rec.ID == 0 {
		t.Error("expected auto-generated ID to be non-zero")
	}
	if rec.RunID != "run-an1" {
		t.Errorf("RunID = %s, want run-an1", rec.RunID)
	}
	if rec.Decision != "approve" {
		t.Errorf("Decision = %s, want approve", rec.Decision)
	}
	if rec.Reasoning != "All tests passed" {
		t.Errorf("Reasoning = %s, want 'All tests passed'", rec.Reasoning)
	}
	if rec.DurationMs != 250 {
		t.Errorf("DurationMs = %d, want 250", rec.DurationMs)
	}
	if rec.AnalyzedAt != 5000 {
		t.Errorf("AnalyzedAt = %d, want 5000", rec.AnalyzedAt)
	}
}

func TestCreateWorkflowAnalysis_MultipleForSameStep(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create two analyses for the same step (e.g., retry scenario)
	for i := 0; i < 3; i++ {
		analysis := &WorkflowAnalysis{
			RunID:      "run-multi",
			StepID:     "step-1",
			MatrixKey:  "",
			Decision:   "approve",
			Reasoning:  fmt.Sprintf("Analysis %d", i),
			FlagsJSON:  "{}",
			DurationMs: int64(100 * (i + 1)),
			AnalyzedAt: int64(1000 + i),
		}
		if err := store.CreateWorkflowAnalysis(ctx, analysis); err != nil {
			t.Fatalf("CreateWorkflowAnalysis() iteration %d error = %v", i, err)
		}
	}

	records, err := store.GetWorkflowAnalyses(ctx, "run-multi", "step-1", "")
	if err != nil {
		t.Fatalf("GetWorkflowAnalyses() error = %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("GetWorkflowAnalyses() returned %d records, want 3", len(records))
	}

	// Should be ordered by analyzed_at ASC
	for i := 0; i < len(records)-1; i++ {
		if records[i].AnalyzedAt > records[i+1].AnalyzedAt {
			t.Errorf("records not in ASC order: %d > %d", records[i].AnalyzedAt, records[i+1].AnalyzedAt)
		}
	}
}

func TestCreateWorkflowAnalysis_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Nil analysis
	if err := store.CreateWorkflowAnalysis(ctx, nil); err == nil {
		t.Error("CreateWorkflowAnalysis(nil) expected error")
	}

	// Empty RunID
	if err := store.CreateWorkflowAnalysis(ctx, &WorkflowAnalysis{StepID: "s", Decision: "approve"}); err == nil {
		t.Error("CreateWorkflowAnalysis() with empty RunID expected error")
	}

	// Empty StepID
	if err := store.CreateWorkflowAnalysis(ctx, &WorkflowAnalysis{RunID: "r", Decision: "approve"}); err == nil {
		t.Error("CreateWorkflowAnalysis() with empty StepID expected error")
	}

	// Empty Decision
	if err := store.CreateWorkflowAnalysis(ctx, &WorkflowAnalysis{RunID: "r", StepID: "s"}); err == nil {
		t.Error("CreateWorkflowAnalysis() with empty Decision expected error")
	}
}

func TestGetWorkflowAnalyses_Empty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	records, err := store.GetWorkflowAnalyses(ctx, "nonexistent", "step", "")
	if err != nil {
		t.Fatalf("GetWorkflowAnalyses() error = %v", err)
	}

	if len(records) != 0 {
		t.Errorf("GetWorkflowAnalyses() returned %d records, want 0", len(records))
	}
}

func TestGetWorkflowAnalyses_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Empty RunID
	_, err := store.GetWorkflowAnalyses(ctx, "", "step", "")
	if err == nil {
		t.Error("GetWorkflowAnalyses() with empty RunID expected error")
	}

	// Empty StepID
	_, err = store.GetWorkflowAnalyses(ctx, "run", "", "")
	if err == nil {
		t.Error("GetWorkflowAnalyses() with empty StepID expected error")
	}
}

// --- Migration verification ---

func TestMigrationV3_CreatesWorkflowTables(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Verify workflow tables exist
	tables := []string{"workflow_runs", "workflow_steps", "workflow_analyses"}
	for _, table := range tables {
		_, err := store.DB().ExecContext(ctx, "SELECT 1 FROM "+table+" LIMIT 1")
		if err != nil {
			t.Errorf("Table %s does not exist: %v", table, err)
		}
	}

	// Verify schema_meta has version 3
	var version int
	err := store.DB().QueryRowContext(ctx,
		"SELECT version FROM schema_meta ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read schema version: %v", err)
	}
	if version != 3 {
		t.Errorf("schema version = %d, want 3", version)
	}
}
