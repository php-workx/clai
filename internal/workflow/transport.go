package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/ipc"

	"google.golang.org/grpc"
)

// DefaultMaxRetries is the default number of retry attempts for LLM calls.
const DefaultMaxRetries = 2

// AnalysisTransport manages LLM analysis via daemon RPC or direct fallback.
type AnalysisTransport struct {
	analyzer *Analyzer
	// directLLM is a fallback function for direct LLM queries when daemon is unavailable.
	// Matches the LLMQuerier.Query signature.
	directLLM func(ctx context.Context, prompt string) (string, error)
	// dialFunc allows overriding ipc.Dial for testing.
	dialFunc func(timeout time.Duration) (*grpc.ClientConn, error)
	// maxRetries controls how many times to retry failed LLM calls (default: DefaultMaxRetries).
	maxRetries int
}

// NewAnalysisTransport creates a transport with the given analyzer and optional direct LLM fallback.
func NewAnalysisTransport(analyzer *Analyzer, directLLM func(ctx context.Context, prompt string) (string, error)) *AnalysisTransport {
	return &AnalysisTransport{
		analyzer:   analyzer,
		directLLM:  directLLM,
		dialFunc:   ipc.Dial,
		maxRetries: DefaultMaxRetries,
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
	scrubbedOutput := ""
	if t.analyzer != nil {
		scrubbedOutput = t.analyzer.BuildAnalysisContext(req.StdoutTail, req.StderrTail, 0)
	} else {
		slog.Warn("analysis transport analyzer is nil; sanitized output unavailable")
	}

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
		Decision:  normalizeDecision(strings.ToLower(strings.TrimSpace(resp.GetDecision()))),
		Reasoning: resp.GetReasoning(),
	}

	if flagsJSON := resp.GetFlagsJson(); flagsJSON != "" {
		if flags, ok := parseFlagsJSON(flagsJSON); ok {
			result.Flags = flags
		}
	}

	return result
}

func parseFlagsJSON(raw string) (map[string]string, bool) {
	var flagsMap map[string]string
	if err := json.Unmarshal([]byte(raw), &flagsMap); err == nil {
		return flagsMap, true
	}

	var legacyFlags []string
	if err := json.Unmarshal([]byte(raw), &legacyFlags); err == nil {
		flagsMap = make(map[string]string, len(legacyFlags))
		for _, flag := range legacyFlags {
			flagsMap[flag] = "true"
		}
		return flagsMap, true
	}

	return nil, false
}

// analyzeViaDirect uses the direct LLM fallback when daemon is unavailable.
// Retries transient failures with exponential backoff (up to maxRetries attempts).
func (t *AnalysisTransport) analyzeViaDirect(ctx context.Context, req *AnalysisRequest, scrubbedOutput string) (*AnalysisResult, error) {
	if t.analyzer == nil {
		return &AnalysisResult{
			Decision:  string(DecisionNeedsHuman),
			Reasoning: "daemon unavailable and no sanitizer available for direct LLM fallback",
		}, nil
	}

	if t.directLLM == nil {
		return &AnalysisResult{
			Decision:  string(DecisionNeedsHuman),
			Reasoning: "daemon unavailable and no direct LLM configured",
		}, nil
	}

	prompt := buildFallbackPrompt(t.analyzer, req, scrubbedOutput)

	maxAttempts := t.maxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Info("retrying LLM analysis", "attempt", attempt+1, "backoff", backoff)
			select {
			case <-ctx.Done():
				return &AnalysisResult{
					Decision:  string(DecisionNeedsHuman),
					Reasoning: "analysis cancelled during retry: " + ctx.Err().Error(),
				}, nil
			case <-time.After(backoff):
			}
		}

		response, llmErr := t.directLLM(ctx, prompt)
		if llmErr != nil {
			lastErr = llmErr
			slog.Warn("LLM analysis attempt failed", "attempt", attempt+1, "error", llmErr)
			continue
		}

		return ParseAnalysisResponse(response), nil
	}

	//nolint:nilerr // Intentional: error is captured in AnalysisResult, not propagated as Go error.
	reason := "all analysis paths failed"
	if lastErr != nil {
		reason += ": " + lastErr.Error()
	}
	return &AnalysisResult{
		Decision:  string(DecisionNeedsHuman),
		Reasoning: reason,
	}, nil
}

func buildFallbackPrompt(analyzer *Analyzer, req *AnalysisRequest, scrubbedOutput string) string {
	if analyzer != nil {
		return analyzer.BuildPrompt(req.StepName, req.RiskLevel, scrubbedOutput, req.AnalysisPrompt)
	}

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
	b.WriteString(scrubbedOutput)
	b.WriteString("\n```\n\n")
	b.WriteString("Respond with a JSON object: {\"decision\": \"proceed|halt|needs_human\", \"reasoning\": \"...\", \"flags\": {}}\n")
	b.WriteString("Valid decisions: proceed, halt, needs_human\n")
	return b.String()
}
