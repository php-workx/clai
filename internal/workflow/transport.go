package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/ipc"

	"google.golang.org/grpc"
)

// AnalysisTransport manages LLM analysis via daemon RPC or direct fallback.
type AnalysisTransport struct {
	analyzer *Analyzer
	// directLLM is a fallback function for direct LLM queries when daemon is unavailable.
	// Matches the LLMQuerier.Query signature.
	directLLM func(ctx context.Context, prompt string) (string, error)
	// dialFunc allows overriding ipc.Dial for testing.
	dialFunc func(timeout time.Duration) (*grpc.ClientConn, error)
}

// NewAnalysisTransport creates a transport with the given analyzer and optional direct LLM fallback.
func NewAnalysisTransport(analyzer *Analyzer, directLLM func(ctx context.Context, prompt string) (string, error)) *AnalysisTransport {
	return &AnalysisTransport{
		analyzer:  analyzer,
		directLLM: directLLM,
		dialFunc:  ipc.Dial,
	}
}

// AnalysisRequest holds the parameters for an analysis request.
type AnalysisRequest struct {
	RunID          string
	StepID         string
	StepName       string
	MatrixKey      string
	RiskLevel      string
	StdoutTail     string
	StderrTail     string
	AnalysisPrompt string // custom prompt from step def, empty for default
}

// Analyze sends step output for LLM analysis.
// Tries daemon RPC first, falls back to direct LLM if daemon unavailable.
// If both fail, returns AnalysisResult{Decision: "needs_human"}.
func (t *AnalysisTransport) Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error) {
	// Build scrubbed output once -- used by both paths.
	scrubbedOutput := t.analyzer.BuildAnalysisContext(req.StdoutTail, req.StderrTail, 0)

	// 1. Try daemon RPC.
	result, err := t.analyzeViaDaemon(ctx, req, scrubbedOutput)
	if err == nil {
		return result, nil
	}

	slog.Warn("daemon unavailable for analysis, using direct LLM fallback", "error", err)

	// 2. Fallback to direct LLM.
	return t.analyzeViaDirect(ctx, req, scrubbedOutput)
}

// analyzeViaDaemon attempts to send the analysis request via gRPC to the daemon.
func (t *AnalysisTransport) analyzeViaDaemon(ctx context.Context, req *AnalysisRequest, scrubbedOutput string) (*AnalysisResult, error) {
	conn, err := t.dialFunc(2 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial daemon: %w", err)
	}
	defer conn.Close()

	client := pb.NewClaiServiceClient(conn)
	resp, err := client.AnalyzeStepOutput(ctx, &pb.AnalyzeStepOutputRequest{
		RunId:          req.RunID,
		StepId:         req.StepID,
		StepName:       req.StepName,
		MatrixKey:      req.MatrixKey,
		RiskLevel:      req.RiskLevel,
		ScrubbedOutput: scrubbedOutput,
		AnalysisPrompt: req.AnalysisPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("AnalyzeStepOutput RPC: %w", err)
	}

	return protoToResult(resp), nil
}

// protoToResult converts a protobuf AnalyzeStepOutputResponse to an AnalysisResult.
func protoToResult(resp *pb.AnalyzeStepOutputResponse) *AnalysisResult {
	result := &AnalysisResult{
		Decision:  resp.GetDecision(),
		Reasoning: resp.GetReasoning(),
	}

	if flagsJSON := resp.GetFlagsJson(); flagsJSON != "" {
		var flags map[string]string
		if err := json.Unmarshal([]byte(flagsJSON), &flags); err == nil {
			result.Flags = flags
		}
	}

	return result
}

// analyzeViaDirect uses the direct LLM fallback when daemon is unavailable.
func (t *AnalysisTransport) analyzeViaDirect(ctx context.Context, req *AnalysisRequest, scrubbedOutput string) (*AnalysisResult, error) {
	if t.directLLM == nil {
		return &AnalysisResult{
			Decision:  "needs_human",
			Reasoning: "daemon unavailable and no direct LLM configured",
		}, nil
	}

	prompt := t.analyzer.BuildPrompt(req.StepName, req.RiskLevel, scrubbedOutput, req.AnalysisPrompt)

	response, llmErr := t.directLLM(ctx, prompt)
	if llmErr != nil {
		//nolint:nilerr // Intentional: error is captured in AnalysisResult, not propagated as Go error.
		return &AnalysisResult{
			Decision:  "needs_human",
			Reasoning: "all analysis paths failed: " + llmErr.Error(),
		}, nil
	}

	return ParseAnalysisResponse(response), nil
}
