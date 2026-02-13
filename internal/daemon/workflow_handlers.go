package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/storage"
)

// WorkflowRunStart handles the WorkflowRunStart RPC.
// It creates a new workflow run in the database.
func (s *Server) WorkflowRunStart(ctx context.Context, req *pb.WorkflowRunStartRequest) (*pb.WorkflowRunStartResponse, error) {
	s.touchActivity()

	startedAt := time.Now().UnixMilli()
	if req.StartedAtUnixMs > 0 {
		startedAt = req.StartedAtUnixMs
	}

	run := &storage.WorkflowRun{
		RunID:        req.RunId,
		WorkflowName: req.WorkflowName,
		WorkflowHash: req.WorkflowHash,
		WorkflowPath: req.WorkflowPath,
		Status:       "running",
		StartedAt:    startedAt,
	}

	if err := s.store.CreateWorkflowRun(ctx, run); err != nil {
		s.logger.Warn("failed to create workflow run",
			"run_id", req.RunId,
			"error", err,
		)
		return &pb.WorkflowRunStartResponse{Ok: false, Error: err.Error()}, nil
	}

	s.logger.Debug("workflow run started",
		"run_id", req.RunId,
		"workflow_name", req.WorkflowName,
	)

	return &pb.WorkflowRunStartResponse{Ok: true}, nil
}

// WorkflowStepUpdate handles the WorkflowStepUpdate RPC.
// It creates or updates a workflow step in the database.
func (s *Server) WorkflowStepUpdate(ctx context.Context, req *pb.WorkflowStepUpdateRequest) (*pb.WorkflowStepUpdateResponse, error) {
	s.touchActivity()

	// Check if the step already exists
	existing, err := s.store.GetWorkflowStep(ctx, req.RunId, req.StepId, req.MatrixKey)
	if err != nil && !errors.Is(err, storage.ErrWorkflowStepNotFound) {
		s.logger.Warn("failed to get workflow step",
			"run_id", req.RunId,
			"step_id", req.StepId,
			"error", err,
		)
		return &pb.WorkflowStepUpdateResponse{Ok: false, Error: err.Error()}, nil
	}

	if existing == nil || errors.Is(err, storage.ErrWorkflowStepNotFound) {
		// Create new step
		step := &storage.WorkflowStep{
			RunID:       req.RunId,
			StepID:      req.StepId,
			MatrixKey:   req.MatrixKey,
			Status:      req.Status,
			Command:     req.Command,
			ExitCode:    int(req.ExitCode),
			DurationMs:  req.DurationMs,
			StdoutTail:  req.StdoutTail,
			StderrTail:  req.StderrTail,
			OutputsJSON: req.OutputsJson,
		}

		if err := s.store.CreateWorkflowStep(ctx, step); err != nil {
			s.logger.Warn("failed to create workflow step",
				"run_id", req.RunId,
				"step_id", req.StepId,
				"error", err,
			)
			return &pb.WorkflowStepUpdateResponse{Ok: false, Error: err.Error()}, nil
		}
	} else {
		// Update existing step
		update := &storage.WorkflowStepUpdate{
			RunID:       req.RunId,
			StepID:      req.StepId,
			MatrixKey:   req.MatrixKey,
			Status:      req.Status,
			Command:     req.Command,
			ExitCode:    int(req.ExitCode),
			DurationMs:  req.DurationMs,
			StdoutTail:  req.StdoutTail,
			StderrTail:  req.StderrTail,
			OutputsJSON: req.OutputsJson,
		}

		if err := s.store.UpdateWorkflowStep(ctx, update); err != nil {
			s.logger.Warn("failed to update workflow step",
				"run_id", req.RunId,
				"step_id", req.StepId,
				"error", err,
			)
			return &pb.WorkflowStepUpdateResponse{Ok: false, Error: err.Error()}, nil
		}
	}

	s.logger.Debug("workflow step updated",
		"run_id", req.RunId,
		"step_id", req.StepId,
		"status", req.Status,
	)

	return &pb.WorkflowStepUpdateResponse{Ok: true}, nil
}

// WorkflowRunEnd handles the WorkflowRunEnd RPC.
// It updates the run status and end time in the database.
func (s *Server) WorkflowRunEnd(ctx context.Context, req *pb.WorkflowRunEndRequest) (*pb.WorkflowRunEndResponse, error) {
	s.touchActivity()

	endedAt := time.Now().UnixMilli()
	if req.EndedAtUnixMs > 0 {
		endedAt = req.EndedAtUnixMs
	}

	if err := s.store.UpdateWorkflowRun(ctx, req.RunId, req.Status, endedAt, req.DurationMs); err != nil {
		s.logger.Warn("failed to end workflow run",
			"run_id", req.RunId,
			"error", err,
		)
		return &pb.WorkflowRunEndResponse{Ok: false, Error: err.Error()}, nil
	}

	s.logger.Debug("workflow run ended",
		"run_id", req.RunId,
		"status", req.Status,
		"duration_ms", req.DurationMs,
	)

	return &pb.WorkflowRunEndResponse{Ok: true}, nil
}

// AnalyzeStepOutput handles the AnalyzeStepOutput RPC.
// It sends step output to the LLM for analysis and stores the result.
func (s *Server) AnalyzeStepOutput(ctx context.Context, req *pb.AnalyzeStepOutputRequest) (*pb.AnalyzeStepOutputResponse, error) {
	s.touchActivity()

	if s.llm == nil {
		return nil, fmt.Errorf("LLM querier not configured")
	}

	// Always build the full prompt — AnalysisPrompt is custom instructions
	// to include within the prompt, not a standalone prompt.
	prompt := buildAnalysisPrompt(req)

	// Call LLM with 120s timeout
	llmCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	start := time.Now()
	rawResponse, err := s.llm.Query(llmCtx, prompt)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		s.logger.Warn("LLM analysis failed",
			"run_id", req.RunId,
			"step_id", req.StepId,
			"error", err,
		)

		// Store the error analysis record
		analysis := &storage.WorkflowAnalysis{
			RunID:       req.RunId,
			StepID:      req.StepId,
			MatrixKey:   req.MatrixKey,
			Decision:    "error",
			Reasoning:   err.Error(),
			Prompt:      prompt,
			RawResponse: "",
			DurationMs:  durationMs,
			AnalyzedAt:  time.Now().UnixMilli(),
		}
		if storeErr := s.store.CreateWorkflowAnalysis(ctx, analysis); storeErr != nil {
			s.logger.Warn("failed to store error analysis",
				"run_id", req.RunId,
				"step_id", req.StepId,
				"error", storeErr,
			)
		}

		return &pb.AnalyzeStepOutputResponse{
			Decision:  "error",
			Reasoning: fmt.Sprintf("LLM query failed: %v", err),
		}, nil
	}

	// Parse the LLM response
	decision, reasoning, flagsJSON := parseAnalysisResponse(rawResponse)

	// Store analysis record
	analysis := &storage.WorkflowAnalysis{
		RunID:       req.RunId,
		StepID:      req.StepId,
		MatrixKey:   req.MatrixKey,
		Decision:    decision,
		Reasoning:   reasoning,
		FlagsJSON:   flagsJSON,
		Prompt:      prompt,
		RawResponse: rawResponse,
		DurationMs:  durationMs,
		AnalyzedAt:  time.Now().UnixMilli(),
	}

	if err := s.store.CreateWorkflowAnalysis(ctx, analysis); err != nil {
		s.logger.Warn("failed to store analysis",
			"run_id", req.RunId,
			"step_id", req.StepId,
			"error", err,
		)
	}

	s.logger.Debug("workflow step analyzed",
		"run_id", req.RunId,
		"step_id", req.StepId,
		"decision", decision,
		"duration_ms", durationMs,
	)

	return &pb.AnalyzeStepOutputResponse{
		Decision:  decision,
		Reasoning: reasoning,
		FlagsJson: flagsJSON,
	}, nil
}

// buildAnalysisPrompt constructs the full analysis prompt from the request fields.
func buildAnalysisPrompt(req *pb.AnalyzeStepOutputRequest) string {
	risk := req.RiskLevel
	if risk == "" {
		risk = "medium"
	}

	var b strings.Builder
	b.WriteString("You are analyzing the output of a workflow step.\n\n")
	fmt.Fprintf(&b, "Step: %s\n", req.StepName)
	fmt.Fprintf(&b, "Risk level: %s\n\n", risk)

	if req.AnalysisPrompt != "" {
		fmt.Fprintf(&b, "Analysis instructions: %s\n\n", req.AnalysisPrompt)
	}

	b.WriteString("Output:\n```\n")
	b.WriteString(req.ScrubbedOutput)
	b.WriteString("\n```\n\n")

	b.WriteString("Respond ONLY with a JSON object, no other text:\n")
	b.WriteString(`{"decision": "proceed|halt|needs_human", "reasoning": "...", "flags": {}}` + "\n")
	b.WriteString("Valid decisions: proceed (step looks good), halt (step has problems), needs_human (uncertain)\n")
	return b.String()
}

// analysisResult is used to parse structured JSON responses from the LLM.
type analysisResult struct {
	Decision  string          `json:"decision"`
	Reasoning string          `json:"reasoning"`
	Flags     json.RawMessage `json:"flags"`
}

// formatAnalysisFields converts a parsed analysisResult into the return tuple.
func formatAnalysisFields(result *analysisResult) (decision, reasoning, flagsJSON string) {
	decision = normalizeDecision(result.Decision)
	if decision == "" {
		decision = "needs_human"
	}
	reasoning = result.Reasoning
	if flagsMap := normalizeFlags(result.Flags); len(flagsMap) > 0 {
		if b, err := json.Marshal(flagsMap); err == nil {
			flagsJSON = string(b)
		}
	}
	return decision, reasoning, flagsJSON
}

// tryParseJSON attempts to unmarshal data as an analysisResult.
// Returns the result and true if successful, or nil and false otherwise.
func tryParseJSON(data []byte) (*analysisResult, bool) {
	var result analysisResult
	if err := json.Unmarshal(data, &result); err == nil && result.Decision != "" {
		return &result, true
	}
	return nil, false
}

// parseAnalysisResponse extracts decision, reasoning, and flags from the LLM response.
// It attempts JSON parsing first, then falls back to simple text extraction.
func parseAnalysisResponse(raw string) (decision, reasoning, flagsJSON string) {
	// Try to parse as JSON directly.
	if result, ok := tryParseJSON([]byte(raw)); ok {
		return formatAnalysisFields(result)
	}

	// Try to extract JSON from within the response (e.g., wrapped in markdown code blocks).
	trimmed := strings.TrimSpace(raw)
	if idx := strings.Index(trimmed, "{"); idx >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > idx {
			if result, ok := tryParseJSON([]byte(trimmed[idx : end+1])); ok {
				return formatAnalysisFields(result)
			}
		}
	}

	// Fallback: treat the entire response as reasoning with needs_human decision.
	return "needs_human", trimmed, ""
}

func normalizeDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approve", "proceed":
		return "proceed"
	case "reject", "halt":
		return "halt"
	case "needs_human":
		return "needs_human"
	case "error":
		return "error"
	default:
		return ""
	}
}

func normalizeFlags(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	var flagsMap map[string]string
	if err := json.Unmarshal(raw, &flagsMap); err == nil {
		return flagsMap
	}

	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err == nil {
		flagsMap = make(map[string]string, len(legacy))
		for _, f := range legacy {
			flagsMap[f] = "true"
		}
		return flagsMap
	}

	return nil
}
