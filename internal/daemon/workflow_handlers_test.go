package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/storage"
)

// mockLLMQuerier implements LLMQuerier for testing.
type mockLLMQuerier struct {
	response string
	err      error
}

func (m *mockLLMQuerier) Query(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
}

// workflowMockStore extends mockStore with workflow-aware tracking.
type workflowMockStore struct {
	*mockStore
	runs     map[string]*storage.WorkflowRun
	steps    map[string]*storage.WorkflowStep
	analyses []*storage.WorkflowAnalysis
}

func newWorkflowMockStore() *workflowMockStore {
	return &workflowMockStore{
		mockStore: newMockStore(),
		runs:      make(map[string]*storage.WorkflowRun),
		steps:     make(map[string]*storage.WorkflowStep),
	}
}

func (m *workflowMockStore) CreateWorkflowRun(ctx context.Context, run *storage.WorkflowRun) error {
	m.runs[run.RunID] = run
	return nil
}

func (m *workflowMockStore) UpdateWorkflowRun(ctx context.Context, runID string, status string, endedAt int64, durationMs int64) error {
	if run, ok := m.runs[runID]; ok {
		run.Status = status
		run.EndedAt = endedAt
		run.DurationMs = durationMs
		return nil
	}
	return fmt.Errorf("run not found: %s", runID)
}

func (m *workflowMockStore) GetWorkflowRun(ctx context.Context, runID string) (*storage.WorkflowRun, error) {
	if run, ok := m.runs[runID]; ok {
		return run, nil
	}
	return nil, nil
}

func (m *workflowMockStore) QueryWorkflowRuns(ctx context.Context, q storage.WorkflowRunQuery) ([]storage.WorkflowRun, error) {
	return nil, nil
}

func stepKey(runID, stepID, matrixKey string) string {
	return runID + "/" + stepID + "/" + matrixKey
}

func (m *workflowMockStore) CreateWorkflowStep(ctx context.Context, step *storage.WorkflowStep) error {
	m.steps[stepKey(step.RunID, step.StepID, step.MatrixKey)] = step
	return nil
}

func (m *workflowMockStore) UpdateWorkflowStep(ctx context.Context, update *storage.WorkflowStepUpdate) error {
	key := stepKey(update.RunID, update.StepID, update.MatrixKey)
	if step, ok := m.steps[key]; ok {
		step.Status = update.Status
		step.ExitCode = update.ExitCode
		step.DurationMs = update.DurationMs
		step.StdoutTail = update.StdoutTail
		step.StderrTail = update.StderrTail
		step.OutputsJSON = update.OutputsJSON
		return nil
	}
	return fmt.Errorf("step not found: %s", key)
}

func (m *workflowMockStore) GetWorkflowStep(ctx context.Context, runID, stepID, matrixKey string) (*storage.WorkflowStep, error) {
	if step, ok := m.steps[stepKey(runID, stepID, matrixKey)]; ok {
		return step, nil
	}
	return nil, nil
}

func (m *workflowMockStore) CreateWorkflowAnalysis(ctx context.Context, analysis *storage.WorkflowAnalysis) error {
	m.analyses = append(m.analyses, analysis)
	return nil
}

func (m *workflowMockStore) GetWorkflowAnalyses(ctx context.Context, runID, stepID, matrixKey string) ([]storage.WorkflowAnalysisRecord, error) {
	return nil, nil
}

func createWorkflowTestServer(t *testing.T, llm LLMQuerier) (*Server, *workflowMockStore) {
	t.Helper()

	store := newWorkflowMockStore()

	server, err := NewServer(&ServerConfig{
		Store:       store,
		Ranker:      &mockRanker{},
		LLM:         llm,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server, store
}

// --- WorkflowRunStart tests ---

func TestHandler_WorkflowRunStart_Success(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	req := &pb.WorkflowRunStartRequest{
		RunId:           "run-001",
		WorkflowName:    "ci",
		WorkflowHash:    "abc123",
		WorkflowPath:    ".clai/workflows/ci.yaml",
		StartedAtUnixMs: 1700000000000,
	}

	resp, err := server.WorkflowRunStart(ctx, req)
	if err != nil {
		t.Fatalf("WorkflowRunStart failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	// Verify run was stored
	run, ok := store.runs["run-001"]
	if !ok {
		t.Fatal("run not found in store")
	}

	if run.WorkflowName != "ci" {
		t.Errorf("expected workflow_name 'ci', got %q", run.WorkflowName)
	}
	if run.WorkflowHash != "abc123" {
		t.Errorf("expected workflow_hash 'abc123', got %q", run.WorkflowHash)
	}
	if run.Status != "running" {
		t.Errorf("expected status 'running', got %q", run.Status)
	}
	if run.StartedAt != 1700000000000 {
		t.Errorf("expected started_at 1700000000000, got %d", run.StartedAt)
	}
}

func TestHandler_WorkflowRunStart_DefaultTimestamp(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	before := time.Now().UnixMilli()

	req := &pb.WorkflowRunStartRequest{
		RunId:           "run-002",
		WorkflowName:    "test",
		StartedAtUnixMs: 0, // Should use current time
	}

	resp, err := server.WorkflowRunStart(ctx, req)
	if err != nil {
		t.Fatalf("WorkflowRunStart failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	after := time.Now().UnixMilli()

	run := store.runs["run-002"]
	if run.StartedAt < before || run.StartedAt > after {
		t.Errorf("expected started_at between %d and %d, got %d", before, after, run.StartedAt)
	}
}

// --- WorkflowStepUpdate tests ---

func TestHandler_WorkflowStepUpdate_CreateNew(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	req := &pb.WorkflowStepUpdateRequest{
		RunId:       "run-001",
		StepId:      "step-1",
		MatrixKey:   "go1.21",
		Status:      "running",
		Command:     "go test ./...",
		ExitCode:    0,
		DurationMs:  0,
		StdoutTail:  "",
		StderrTail:  "",
		OutputsJson: "",
	}

	resp, err := server.WorkflowStepUpdate(ctx, req)
	if err != nil {
		t.Fatalf("WorkflowStepUpdate failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	// Verify step was created
	key := stepKey("run-001", "step-1", "go1.21")
	step, ok := store.steps[key]
	if !ok {
		t.Fatal("step not found in store")
	}

	if step.Status != "running" {
		t.Errorf("expected status 'running', got %q", step.Status)
	}
	if step.Command != "go test ./..." {
		t.Errorf("expected command 'go test ./...', got %q", step.Command)
	}
}

func TestHandler_WorkflowStepUpdate_UpdateExisting(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	// Create a step first
	createReq := &pb.WorkflowStepUpdateRequest{
		RunId:     "run-001",
		StepId:    "step-1",
		MatrixKey: "go1.21",
		Status:    "running",
		Command:   "go test ./...",
	}
	_, _ = server.WorkflowStepUpdate(ctx, createReq)

	// Update the step
	updateReq := &pb.WorkflowStepUpdateRequest{
		RunId:      "run-001",
		StepId:     "step-1",
		MatrixKey:  "go1.21",
		Status:     "passed",
		ExitCode:   0,
		DurationMs: 5000,
		StdoutTail: "ok  ./...",
		StderrTail: "",
	}

	resp, err := server.WorkflowStepUpdate(ctx, updateReq)
	if err != nil {
		t.Fatalf("WorkflowStepUpdate failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	// Verify step was updated
	key := stepKey("run-001", "step-1", "go1.21")
	step := store.steps[key]
	if step.Status != "passed" {
		t.Errorf("expected status 'passed', got %q", step.Status)
	}
	if step.DurationMs != 5000 {
		t.Errorf("expected duration_ms 5000, got %d", step.DurationMs)
	}
	if step.StdoutTail != "ok  ./..." {
		t.Errorf("expected stdout_tail 'ok  ./...', got %q", step.StdoutTail)
	}
}

func TestHandler_WorkflowStepUpdate_EmptyMatrixKey(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	req := &pb.WorkflowStepUpdateRequest{
		RunId:     "run-001",
		StepId:    "step-1",
		MatrixKey: "", // No matrix
		Status:    "passed",
		Command:   "make lint",
	}

	resp, err := server.WorkflowStepUpdate(ctx, req)
	if err != nil {
		t.Fatalf("WorkflowStepUpdate failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	key := stepKey("run-001", "step-1", "")
	if _, ok := store.steps[key]; !ok {
		t.Fatal("step not found in store")
	}
}

// --- WorkflowRunEnd tests ---

func TestHandler_WorkflowRunEnd_Success(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	// Create a run first
	startReq := &pb.WorkflowRunStartRequest{
		RunId:           "run-001",
		WorkflowName:    "ci",
		StartedAtUnixMs: 1700000000000,
	}
	_, _ = server.WorkflowRunStart(ctx, startReq)

	// End the run
	endReq := &pb.WorkflowRunEndRequest{
		RunId:         "run-001",
		Status:        "passed",
		EndedAtUnixMs: 1700000005000,
		DurationMs:    5000,
	}

	resp, err := server.WorkflowRunEnd(ctx, endReq)
	if err != nil {
		t.Fatalf("WorkflowRunEnd failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	// Verify run was updated
	run := store.runs["run-001"]
	if run.Status != "passed" {
		t.Errorf("expected status 'passed', got %q", run.Status)
	}
	if run.EndedAt != 1700000005000 {
		t.Errorf("expected ended_at 1700000005000, got %d", run.EndedAt)
	}
	if run.DurationMs != 5000 {
		t.Errorf("expected duration_ms 5000, got %d", run.DurationMs)
	}
}

func TestHandler_WorkflowRunEnd_DefaultTimestamp(t *testing.T) {
	t.Parallel()

	server, store := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	// Create a run first
	startReq := &pb.WorkflowRunStartRequest{
		RunId:        "run-003",
		WorkflowName: "ci",
	}
	_, _ = server.WorkflowRunStart(ctx, startReq)

	before := time.Now().UnixMilli()

	endReq := &pb.WorkflowRunEndRequest{
		RunId:         "run-003",
		Status:        "failed",
		EndedAtUnixMs: 0, // Should use current time
		DurationMs:    1000,
	}

	resp, err := server.WorkflowRunEnd(ctx, endReq)
	if err != nil {
		t.Fatalf("WorkflowRunEnd failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}

	after := time.Now().UnixMilli()

	run := store.runs["run-003"]
	if run.EndedAt < before || run.EndedAt > after {
		t.Errorf("expected ended_at between %d and %d, got %d", before, after, run.EndedAt)
	}
}

func TestHandler_WorkflowRunEnd_NotFound(t *testing.T) {
	t.Parallel()

	server, _ := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	endReq := &pb.WorkflowRunEndRequest{
		RunId:         "nonexistent-run",
		Status:        "failed",
		EndedAtUnixMs: time.Now().UnixMilli(),
	}

	resp, err := server.WorkflowRunEnd(ctx, endReq)
	if err != nil {
		t.Fatalf("WorkflowRunEnd returned error: %v", err)
	}

	if resp.Ok {
		t.Error("expected ok=false for nonexistent run")
	}

	if resp.Error == "" {
		t.Error("expected error message")
	}
}

// --- AnalyzeStepOutput tests ---

func TestHandler_AnalyzeStepOutput_Success(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		response: `{"decision": "approve", "reasoning": "All tests passed", "flags": []}`,
	}
	server, store := createWorkflowTestServer(t, llm)
	ctx := context.Background()

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		MatrixKey:      "go1.21",
		StepName:       "unit-tests",
		RiskLevel:      "low",
		ScrubbedOutput: "ok  github.com/runger/clai/...\nPASS",
	}

	resp, err := server.AnalyzeStepOutput(ctx, req)
	if err != nil {
		t.Fatalf("AnalyzeStepOutput failed: %v", err)
	}

	if resp.Decision != "approve" {
		t.Errorf("expected decision 'approve', got %q", resp.Decision)
	}
	if resp.Reasoning != "All tests passed" {
		t.Errorf("expected reasoning 'All tests passed', got %q", resp.Reasoning)
	}

	// Verify analysis was stored
	if len(store.analyses) != 1 {
		t.Fatalf("expected 1 analysis stored, got %d", len(store.analyses))
	}

	a := store.analyses[0]
	if a.Decision != "approve" {
		t.Errorf("stored decision should be 'approve', got %q", a.Decision)
	}
	if a.RunID != "run-001" {
		t.Errorf("stored run_id should be 'run-001', got %q", a.RunID)
	}
	if a.DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", a.DurationMs)
	}
}

func TestHandler_AnalyzeStepOutput_Reject(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		response: `{"decision": "reject", "reasoning": "3 tests failed", "flags": ["test_failure"]}`,
	}
	server, _ := createWorkflowTestServer(t, llm)
	ctx := context.Background()

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		StepName:       "unit-tests",
		RiskLevel:      "high",
		ScrubbedOutput: "FAIL  some/package",
	}

	resp, err := server.AnalyzeStepOutput(ctx, req)
	if err != nil {
		t.Fatalf("AnalyzeStepOutput failed: %v", err)
	}

	if resp.Decision != "reject" {
		t.Errorf("expected decision 'reject', got %q", resp.Decision)
	}
	if resp.FlagsJson != `["test_failure"]` {
		t.Errorf("expected flags_json '[\"test_failure\"]', got %q", resp.FlagsJson)
	}
}

func TestHandler_AnalyzeStepOutput_NilLLM(t *testing.T) {
	t.Parallel()

	server, _ := createWorkflowTestServer(t, nil) // No LLM
	ctx := context.Background()

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		StepName:       "unit-tests",
		ScrubbedOutput: "output",
	}

	_, err := server.AnalyzeStepOutput(ctx, req)
	if err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

func TestHandler_AnalyzeStepOutput_LLMFailure(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		err: fmt.Errorf("connection refused"),
	}
	server, store := createWorkflowTestServer(t, llm)
	ctx := context.Background()

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		StepName:       "unit-tests",
		ScrubbedOutput: "output",
	}

	resp, err := server.AnalyzeStepOutput(ctx, req)
	if err != nil {
		t.Fatalf("AnalyzeStepOutput returned error: %v", err)
	}

	if resp.Decision != "error" {
		t.Errorf("expected decision 'error', got %q", resp.Decision)
	}

	// Verify error analysis was stored
	if len(store.analyses) != 1 {
		t.Fatalf("expected 1 error analysis stored, got %d", len(store.analyses))
	}
	if store.analyses[0].Decision != "error" {
		t.Errorf("stored decision should be 'error', got %q", store.analyses[0].Decision)
	}
}

func TestHandler_AnalyzeStepOutput_CustomPrompt(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		response: `{"decision": "approve", "reasoning": "Looks good"}`,
	}
	server, store := createWorkflowTestServer(t, llm)
	ctx := context.Background()

	customPrompt := "Is this output acceptable? Respond with JSON."

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		StepName:       "deploy",
		ScrubbedOutput: "deployed successfully",
		AnalysisPrompt: customPrompt,
	}

	resp, err := server.AnalyzeStepOutput(ctx, req)
	if err != nil {
		t.Fatalf("AnalyzeStepOutput failed: %v", err)
	}

	if resp.Decision != "approve" {
		t.Errorf("expected decision 'approve', got %q", resp.Decision)
	}

	// Verify the custom prompt was used (stored in analysis)
	if len(store.analyses) != 1 {
		t.Fatalf("expected 1 analysis stored, got %d", len(store.analyses))
	}
	if store.analyses[0].Prompt != customPrompt {
		t.Errorf("expected custom prompt to be stored, got %q", store.analyses[0].Prompt)
	}
}

func TestHandler_AnalyzeStepOutput_NonJSONResponse(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		response: "This output looks suspicious. I recommend a human review.",
	}
	server, _ := createWorkflowTestServer(t, llm)
	ctx := context.Background()

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		StepName:       "unit-tests",
		ScrubbedOutput: "some output",
	}

	resp, err := server.AnalyzeStepOutput(ctx, req)
	if err != nil {
		t.Fatalf("AnalyzeStepOutput failed: %v", err)
	}

	// Non-JSON responses should default to needs_human
	if resp.Decision != "needs_human" {
		t.Errorf("expected decision 'needs_human' for non-JSON response, got %q", resp.Decision)
	}
	if resp.Reasoning == "" {
		t.Error("expected reasoning from non-JSON response")
	}
}

func TestHandler_AnalyzeStepOutput_JSONInCodeBlock(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		response: "Here is my analysis:\n```json\n{\"decision\": \"approve\", \"reasoning\": \"All good\", \"flags\": [\"clean\"]}\n```",
	}
	server, _ := createWorkflowTestServer(t, llm)
	ctx := context.Background()

	req := &pb.AnalyzeStepOutputRequest{
		RunId:          "run-001",
		StepId:         "step-1",
		StepName:       "lint",
		ScrubbedOutput: "no issues found",
	}

	resp, err := server.AnalyzeStepOutput(ctx, req)
	if err != nil {
		t.Fatalf("AnalyzeStepOutput failed: %v", err)
	}

	if resp.Decision != "approve" {
		t.Errorf("expected decision 'approve', got %q", resp.Decision)
	}
	if resp.FlagsJson != `["clean"]` {
		t.Errorf("expected flags_json '[\"clean\"]', got %q", resp.FlagsJson)
	}
}

// --- parseAnalysisResponse tests ---

func TestParseAnalysisResponse_ValidJSON(t *testing.T) {
	t.Parallel()

	decision, reasoning, flagsJSON := parseAnalysisResponse(
		`{"decision": "approve", "reasoning": "Tests passed", "flags": ["clean"]}`,
	)

	if decision != "approve" {
		t.Errorf("expected decision 'approve', got %q", decision)
	}
	if reasoning != "Tests passed" {
		t.Errorf("expected reasoning 'Tests passed', got %q", reasoning)
	}
	if flagsJSON != `["clean"]` {
		t.Errorf("expected flags '[\"clean\"]', got %q", flagsJSON)
	}
}

func TestParseAnalysisResponse_NoFlags(t *testing.T) {
	t.Parallel()

	decision, reasoning, flagsJSON := parseAnalysisResponse(
		`{"decision": "reject", "reasoning": "Build failed"}`,
	)

	if decision != "reject" {
		t.Errorf("expected decision 'reject', got %q", decision)
	}
	if reasoning != "Build failed" {
		t.Errorf("expected reasoning 'Build failed', got %q", reasoning)
	}
	if flagsJSON != "" {
		t.Errorf("expected empty flags, got %q", flagsJSON)
	}
}

func TestParseAnalysisResponse_PlainText(t *testing.T) {
	t.Parallel()

	decision, reasoning, flagsJSON := parseAnalysisResponse("Something went wrong")

	if decision != "needs_human" {
		t.Errorf("expected decision 'needs_human', got %q", decision)
	}
	if reasoning != "Something went wrong" {
		t.Errorf("expected reasoning 'Something went wrong', got %q", reasoning)
	}
	if flagsJSON != "" {
		t.Errorf("expected empty flags, got %q", flagsJSON)
	}
}

func TestParseAnalysisResponse_WrappedJSON(t *testing.T) {
	t.Parallel()

	decision, reasoning, _ := parseAnalysisResponse(
		"Analysis result:\n{\"decision\": \"needs_human\", \"reasoning\": \"Ambiguous output\"}\nEnd.",
	)

	if decision != "needs_human" {
		t.Errorf("expected decision 'needs_human', got %q", decision)
	}
	if reasoning != "Ambiguous output" {
		t.Errorf("expected reasoning 'Ambiguous output', got %q", reasoning)
	}
}

func TestParseAnalysisResponse_EmptyDecision(t *testing.T) {
	t.Parallel()

	// JSON with empty decision should fall through to needs_human
	decision, _, _ := parseAnalysisResponse(`{"decision": "", "reasoning": "test"}`)

	if decision != "needs_human" {
		t.Errorf("expected decision 'needs_human' for empty decision, got %q", decision)
	}
}
