package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// failDial simulates a daemon that is unavailable.
func failDial(_ time.Duration) (*grpc.ClientConn, error) {
	return nil, errors.New("connection refused")
}

func TestTransport_DaemonUnavailable_FallbackLLM(t *testing.T) {
	analyzer := NewAnalyzer(nil)
	directLLM := func(_ context.Context, _ string) (string, error) {
		return `{"decision": "approve", "reasoning": "all tests passed", "flags": {"coverage": "92%"}}`, nil
	}

	transport := NewAnalysisTransport(analyzer, directLLM)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-1",
		StepID:     "step-1",
		StepName:   "unit-tests",
		RiskLevel:  "low",
		StdoutTail: "PASS\nok  github.com/example/pkg 0.5s",
		StderrTail: "",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed
	assert.Equal(t, "all tests passed", result.Reasoning)
	assert.Equal(t, "92%", result.Flags["coverage"])
}

func TestTransport_DaemonUnavailable_NoFallback(t *testing.T) {
	analyzer := NewAnalyzer(nil)

	transport := NewAnalysisTransport(analyzer, nil)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-2",
		StepID:     "step-2",
		StepName:   "deploy",
		RiskLevel:  "high",
		StdoutTail: "deploying...",
		StderrTail: "",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "needs_human", result.Decision)
	assert.Equal(t, "daemon unavailable and no direct LLM configured", result.Reasoning)
}

func TestTransport_DaemonUnavailable_NilAnalyzer(t *testing.T) {
	transport := NewAnalysisTransport(nil, nil)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-nil-analyzer",
		StepID:     "step",
		StepName:   "lint",
		RiskLevel:  "low",
		StdoutTail: "ok",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, string(DecisionNeedsHuman), result.Decision)
	assert.Contains(t, result.Reasoning, "no sanitizer available")
}

func TestTransport_DaemonUnavailable_NilAnalyzer_DoesNotCallDirectLLM(t *testing.T) {
	called := false
	transport := NewAnalysisTransport(nil, func(_ context.Context, _ string) (string, error) {
		called = true
		return `{"decision":"proceed","reasoning":"should not be called"}`, nil
	})
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-nil-analyzer-direct",
		StepID:     "step",
		StepName:   "lint",
		RiskLevel:  "medium",
		StdoutTail: "sensitive output",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, called, "directLLM must not be called without analyzer sanitization")
	assert.Equal(t, string(DecisionNeedsHuman), result.Decision)
	assert.Contains(t, result.Reasoning, "no sanitizer available")
}

func TestTransport_DirectLLMFailure(t *testing.T) {
	analyzer := NewAnalyzer(nil)
	directLLM := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("LLM service unavailable")
	}

	transport := NewAnalysisTransport(analyzer, directLLM)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-3",
		StepID:     "step-3",
		StepName:   "lint",
		RiskLevel:  "medium",
		StdoutTail: "linting...",
		StderrTail: "warning: unused variable",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "needs_human", result.Decision)
	assert.Contains(t, result.Reasoning, "all analysis paths failed")
	assert.Contains(t, result.Reasoning, "LLM service unavailable")
}

func TestTransport_DirectLLMFailure_NegativeRetries(t *testing.T) {
	analyzer := NewAnalyzer(nil)
	directLLM := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("LLM unavailable")
	}

	transport := NewAnalysisTransport(analyzer, directLLM)
	transport.dialFunc = failDial
	transport.maxRetries = -1

	req := &AnalysisRequest{
		RunID:    "run-negative-retries",
		StepID:   "step",
		StepName: "build",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, string(DecisionNeedsHuman), result.Decision)
	assert.Contains(t, result.Reasoning, "all analysis paths failed")
}

func TestTransport_DaemonUnavailable_FallbackParseError(t *testing.T) {
	analyzer := NewAnalyzer(nil)
	directLLM := func(_ context.Context, _ string) (string, error) {
		return "this is not json or any parseable format", nil
	}

	transport := NewAnalysisTransport(analyzer, directLLM)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-4",
		StepID:     "step-4",
		StepName:   "build",
		RiskLevel:  "medium",
		StdoutTail: "compiling...",
		StderrTail: "",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	// ParseAnalysisResponse returns needs_human for unparseable input (FR-24).
	assert.Equal(t, "needs_human", result.Decision)
	assert.Equal(t, "could not parse LLM response", result.Reasoning)
}

func TestTransport_FallbackReceivesCorrectPrompt(t *testing.T) {
	analyzer := NewAnalyzer(nil)

	var capturedPrompt string
	directLLM := func(_ context.Context, prompt string) (string, error) {
		capturedPrompt = prompt
		return `{"decision": "approve", "reasoning": "ok"}`, nil
	}

	transport := NewAnalysisTransport(analyzer, directLLM)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:          "run-5",
		StepID:         "step-5",
		StepName:       "integration-tests",
		RiskLevel:      "high",
		StdoutTail:     "all 42 tests passed",
		StderrTail:     "",
		AnalysisPrompt: "Focus on test coverage metrics",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed

	// Verify the prompt was constructed correctly.
	assert.Contains(t, capturedPrompt, "Step: integration-tests")
	assert.Contains(t, capturedPrompt, "Risk level: high")
	assert.Contains(t, capturedPrompt, "Focus on test coverage metrics")
	assert.Contains(t, capturedPrompt, "all 42 tests passed")
}

func TestTransport_FallbackWithSecretMasking(t *testing.T) {
	t.Setenv("TEST_TRANSPORT_SECRET", "mytoken123")
	masker := NewSecretMasker([]SecretDef{
		{Name: "TEST_TRANSPORT_SECRET", From: "env"},
	})
	analyzer := NewAnalyzer(masker)

	var capturedPrompt string
	directLLM := func(_ context.Context, prompt string) (string, error) {
		capturedPrompt = prompt
		return `{"decision": "approve", "reasoning": "ok"}`, nil
	}

	transport := NewAnalysisTransport(analyzer, directLLM)
	transport.dialFunc = failDial

	req := &AnalysisRequest{
		RunID:      "run-6",
		StepID:     "step-6",
		StepName:   "deploy",
		RiskLevel:  "high",
		StdoutTail: "using token mytoken123 for auth",
		StderrTail: "",
	}

	result, err := transport.Analyze(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed

	// Secret should be masked in the prompt sent to the LLM.
	assert.NotContains(t, capturedPrompt, "mytoken123")
	assert.Contains(t, capturedPrompt, "***")
}

func TestProtoToResult_WithFlags(t *testing.T) {
	// Import the proto package to create a response directly.
	resp := &protoResponse{
		decision:  "approve",
		reasoning: "all good",
		flagsJSON: `{"coverage": "95%", "warnings": "0"}`,
	}

	result := protoToResultFromValues(resp.decision, resp.reasoning, resp.flagsJSON)
	assert.Equal(t, "proceed", result.Decision)
	assert.Equal(t, "all good", result.Reasoning)
	assert.Equal(t, "95%", result.Flags["coverage"])
	assert.Equal(t, "0", result.Flags["warnings"])
}

func TestProtoToResult_EmptyFlags(t *testing.T) {
	result := protoToResultFromValues("reject", "build failed", "")
	assert.Equal(t, "halt", result.Decision)
	assert.Equal(t, "build failed", result.Reasoning)
	assert.Nil(t, result.Flags)
}

func TestProtoToResult_InvalidFlagsJSON(t *testing.T) {
	result := protoToResultFromValues("approve", "ok", "not json")
	assert.Equal(t, "proceed", result.Decision)
	assert.Equal(t, "ok", result.Reasoning)
	// Invalid JSON should be silently ignored.
	assert.Nil(t, result.Flags)
}

func TestProtoToResult_LegacyArrayFlags(t *testing.T) {
	result := protoToResultFromValues("proceed", "ok", `["flaky_test","timeout"]`)
	require.NotNil(t, result.Flags)
	assert.Equal(t, "true", result.Flags["flaky_test"])
	assert.Equal(t, "true", result.Flags["timeout"])
}

// protoResponse is a test helper to avoid importing proto directly in tests.
type protoResponse struct {
	decision  string
	reasoning string
	flagsJSON string
}

// protoToResultFromValues mirrors protoToResult logic for test validation
// without requiring a real proto message.
func protoToResultFromValues(decision, reasoning, flagsJSON string) *AnalysisResult {
	result := &AnalysisResult{
		Decision:  normalizeDecision(decision),
		Reasoning: reasoning,
	}

	if flagsJSON != "" {
		if flags, ok := parseFlagsJSON(flagsJSON); ok {
			result.Flags = flags
		}
	}

	return result
}
