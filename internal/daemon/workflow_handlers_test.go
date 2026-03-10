package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/ops"
)

// mockLLMQuerier implements LLMQuerier for testing.
type mockLLMQuerier struct {
	err      error
	response string
}

func (m *mockLLMQuerier) Query(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
}

func createWorkflowTestServer(t *testing.T, llm LLMQuerier) (*Server, *suggestdb.DB) {
	t.Helper()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "workflow_test.db")
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 DB: %v", err)
	}
	t.Cleanup(func() { v2db.Close() })

	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		LLM:         llm,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server, v2db
}

// --- WorkflowRunStart tests ---

func TestHandler_WorkflowRunStart_Success(t *testing.T) {
	t.Parallel()

	server, v2db := createWorkflowTestServer(t, nil)
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
	run, err := ops.GetWorkflowRun(ctx, v2db, "run-001")
	if err != nil {
		t.Fatalf("failed to get workflow run: %v", err)
	}
	if run == nil {
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

	server, v2db := createWorkflowTestServer(t, nil)
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

	run, err := ops.GetWorkflowRun(ctx, v2db, "run-002")
	if err != nil {
		t.Fatalf("failed to get workflow run: %v", err)
	}
	if run.StartedAt < before || run.StartedAt > after {
		t.Errorf("expected started_at between %d and %d, got %d", before, after, run.StartedAt)
	}
}

// --- WorkflowStepUpdate tests ---

func TestHandler_WorkflowStepUpdate_CreateNew(t *testing.T) {
	t.Parallel()

	server, v2db := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	// Create parent workflow run first (required by FK constraint)
	_, err := server.WorkflowRunStart(ctx, &pb.WorkflowRunStartRequest{
		RunId:        "run-001",
		WorkflowName: "test-workflow",
	})
	if err != nil {
		t.Fatalf("WorkflowRunStart failed: %v", err)
	}

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
	step, err := ops.GetWorkflowStep(ctx, v2db, "run-001", "step-1", "go1.21")
	if err != nil {
		t.Fatalf("failed to get workflow step: %v", err)
	}
	if step == nil {
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

	server, v2db := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	// Create parent workflow run first (required by FK constraint)
	_, err := server.WorkflowRunStart(ctx, &pb.WorkflowRunStartRequest{
		RunId:        "run-001",
		WorkflowName: "test-workflow",
	})
	if err != nil {
		t.Fatalf("WorkflowRunStart failed: %v", err)
	}

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
		Command:    "go test ./... -run TestFast",
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
	step, err := ops.GetWorkflowStep(ctx, v2db, "run-001", "step-1", "go1.21")
	if err != nil {
		t.Fatalf("failed to get workflow step: %v", err)
	}
	if step.Status != "passed" {
		t.Errorf("expected status 'passed', got %q", step.Status)
	}
	if step.DurationMs != 5000 {
		t.Errorf("expected duration_ms 5000, got %d", step.DurationMs)
	}
	if step.StdoutTail != "ok  ./..." {
		t.Errorf("expected stdout_tail 'ok  ./...', got %q", step.StdoutTail)
	}
	if step.Command != "go test ./... -run TestFast" {
		t.Errorf("expected updated command, got %q", step.Command)
	}
}

func TestHandler_WorkflowStepUpdate_EmptyMatrixKey(t *testing.T) {
	t.Parallel()

	server, v2db := createWorkflowTestServer(t, nil)
	ctx := context.Background()

	// Create parent workflow run first (required by FK constraint)
	_, err := server.WorkflowRunStart(ctx, &pb.WorkflowRunStartRequest{
		RunId:        "run-001",
		WorkflowName: "test-workflow",
	})
	if err != nil {
		t.Fatalf("WorkflowRunStart failed: %v", err)
	}

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

	step, err := ops.GetWorkflowStep(ctx, v2db, "run-001", "step-1", "")
	if err != nil {
		t.Fatalf("failed to get workflow step: %v", err)
	}
	if step == nil {
		t.Fatal("step not found in store")
	}
}

// --- WorkflowRunEnd tests ---

func TestHandler_WorkflowRunEnd_Success(t *testing.T) {
	t.Parallel()

	server, v2db := createWorkflowTestServer(t, nil)
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
	run, err := ops.GetWorkflowRun(ctx, v2db, "run-001")
	if err != nil {
		t.Fatalf("failed to get workflow run: %v", err)
	}
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

	server, v2db := createWorkflowTestServer(t, nil)
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

	run, err := ops.GetWorkflowRun(ctx, v2db, "run-003")
	if err != nil {
		t.Fatalf("failed to get workflow run: %v", err)
	}
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
		response: `{"decision": "approve", "reasoning": "All tests passed", "flags": {}}`,
	}
	server, v2db := createWorkflowTestServer(t, llm)
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

	if resp.Decision != "proceed" {
		t.Errorf("expected decision 'proceed', got %q", resp.Decision)
	}
	if resp.Reasoning != "All tests passed" {
		t.Errorf("expected reasoning 'All tests passed', got %q", resp.Reasoning)
	}

	// Verify analysis was stored
	analyses, err := ops.GetWorkflowAnalyses(ctx, v2db, "run-001", "step-1", "go1.21")
	if err != nil {
		t.Fatalf("failed to get workflow analyses: %v", err)
	}
	if len(analyses) != 1 {
		t.Fatalf("expected 1 analysis stored, got %d", len(analyses))
	}

	a := analyses[0]
	if a.Decision != "proceed" {
		t.Errorf("stored decision should be 'proceed', got %q", a.Decision)
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

	if resp.Decision != "halt" {
		t.Errorf("expected decision 'halt', got %q", resp.Decision)
	}
	if resp.FlagsJson != `{"test_failure":"true"}` {
		t.Errorf("expected flags_json '{\"test_failure\":\"true\"}', got %q", resp.FlagsJson)
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
	server, v2db := createWorkflowTestServer(t, llm)
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
	analyses, err := ops.GetWorkflowAnalyses(ctx, v2db, "run-001", "step-1", "")
	if err != nil {
		t.Fatalf("failed to get workflow analyses: %v", err)
	}
	if len(analyses) != 1 {
		t.Fatalf("expected 1 error analysis stored, got %d", len(analyses))
	}
	if analyses[0].Decision != "error" {
		t.Errorf("stored decision should be 'error', got %q", analyses[0].Decision)
	}
}

func TestHandler_AnalyzeStepOutput_CustomPrompt(t *testing.T) {
	t.Parallel()

	llm := &mockLLMQuerier{
		response: `{"decision": "approve", "reasoning": "Looks good"}`,
	}
	server, v2db := createWorkflowTestServer(t, llm)
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

	if resp.Decision != "proceed" {
		t.Errorf("expected decision 'proceed', got %q", resp.Decision)
	}

	// Verify the custom prompt was used (stored in analysis)
	analyses, err := ops.GetWorkflowAnalyses(ctx, v2db, "run-001", "step-1", "")
	if err != nil {
		t.Fatalf("failed to get workflow analyses: %v", err)
	}
	if len(analyses) != 1 {
		t.Fatalf("expected 1 analysis stored, got %d", len(analyses))
	}
	if !strings.Contains(analyses[0].Prompt, customPrompt) {
		t.Errorf("expected stored prompt to contain custom instructions %q, got %q", customPrompt, analyses[0].Prompt)
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

	if resp.Decision != "proceed" {
		t.Errorf("expected decision 'proceed', got %q", resp.Decision)
	}
	if resp.FlagsJson != `{"clean":"true"}` {
		t.Errorf("expected flags_json '{\"clean\":\"true\"}', got %q", resp.FlagsJson)
	}
}

// --- parseAnalysisResponse tests ---

func TestParseAnalysisResponse_ValidJSON(t *testing.T) {
	t.Parallel()

	decision, reasoning, flagsJSON := parseAnalysisResponse(
		`{"decision": "approve", "reasoning": "Tests passed", "flags": ["clean"]}`,
	)

	if decision != "proceed" {
		t.Errorf("expected decision 'proceed', got %q", decision)
	}
	if reasoning != "Tests passed" {
		t.Errorf("expected reasoning 'Tests passed', got %q", reasoning)
	}
	if flagsJSON != `{"clean":"true"}` {
		t.Errorf("expected flags '{\"clean\":\"true\"}', got %q", flagsJSON)
	}
}

func TestParseAnalysisResponse_NoFlags(t *testing.T) {
	t.Parallel()

	decision, reasoning, flagsJSON := parseAnalysisResponse(
		`{"decision": "reject", "reasoning": "Build failed"}`,
	)

	if decision != "halt" {
		t.Errorf("expected decision 'halt', got %q", decision)
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
