package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzerBuildAnalysisContext_SmallOutput(t *testing.T) {
	a := NewAnalyzer(nil)
	result := a.BuildAnalysisContext("hello", "world", 0)
	assert.Equal(t, "hello\nworld", result)
}

func TestAnalyzerBuildAnalysisContext_StdoutOnly(t *testing.T) {
	a := NewAnalyzer(nil)
	result := a.BuildAnalysisContext("hello", "", 0)
	assert.Equal(t, "hello", result)
}

func TestAnalyzerBuildAnalysisContext_StderrOnly(t *testing.T) {
	a := NewAnalyzer(nil)
	result := a.BuildAnalysisContext("", "error msg", 0)
	assert.Equal(t, "error msg", result)
}

func TestAnalyzerBuildAnalysisContext_Truncated(t *testing.T) {
	a := NewAnalyzer(nil)
	// Create a string larger than 200 bytes.
	long := strings.Repeat("A", 300)
	result := a.BuildAnalysisContext(long, "", 200)

	assert.LessOrEqual(t, len(result), 200)
	assert.Contains(t, result, "...[truncated]...")
	// Head should start with A's.
	assert.True(t, strings.HasPrefix(result, "A"))
	// Tail should end with A's.
	assert.True(t, strings.HasSuffix(result, "A"))
}

func TestAnalyzerBuildAnalysisContext_UTF8Boundary(t *testing.T) {
	a := NewAnalyzer(nil)
	// Create a string with multi-byte runes (€ is 3 bytes in UTF-8).
	// 100 * 3 = 300 bytes.
	long := strings.Repeat("€", 100)
	result := a.BuildAnalysisContext(long, "", 200)

	assert.LessOrEqual(t, len(result), 200)
	assert.Contains(t, result, "...[truncated]...")

	// Verify no broken runes — every rune should be valid.
	for i, r := range result {
		if r == 0xFFFD {
			t.Errorf("found replacement character (broken rune) at index %d", i)
		}
	}
}

func TestAnalyzerBuildAnalysisContext_WithSecretMasking(t *testing.T) {
	// Create a masker with a known secret value using env.
	t.Setenv("TEST_ANALYZE_SECRET", "supersecret123")
	masker := NewSecretMasker([]SecretDef{
		{Name: "TEST_ANALYZE_SECRET", From: "env"},
	})

	a := NewAnalyzer(masker)
	result := a.BuildAnalysisContext("token=supersecret123 done", "", 0)

	assert.NotContains(t, result, "supersecret123")
	assert.Contains(t, result, "***")
	assert.Contains(t, result, "token=")
}

func TestAnalyzerBuildAnalysisContext_NilMasker(t *testing.T) {
	a := NewAnalyzer(nil)
	result := a.BuildAnalysisContext("secret_value", "", 0)
	assert.Equal(t, "secret_value", result)
}

func TestAnalyzerBuildAnalysisContext_DefaultMaxBytes(t *testing.T) {
	a := NewAnalyzer(nil)
	// Passing 0 should use DefaultMaxBytes (100KB).
	small := strings.Repeat("x", 100)
	result := a.BuildAnalysisContext(small, "", 0)
	assert.Equal(t, small, result)
}

func TestAnalyzerBuildPrompt_WithCustomPrompt(t *testing.T) {
	a := NewAnalyzer(nil)
	prompt := a.BuildPrompt("deploy", "high", "output here", "Check for errors")

	assert.Contains(t, prompt, "Step: deploy")
	assert.Contains(t, prompt, "Risk level: high")
	assert.Contains(t, prompt, "Analysis instructions: Check for errors")
	assert.Contains(t, prompt, "output here")
	assert.Contains(t, prompt, "JSON")
}

func TestAnalyzerBuildPrompt_WithoutCustomPrompt(t *testing.T) {
	a := NewAnalyzer(nil)
	prompt := a.BuildPrompt("test", "low", "all passed", "")

	assert.Contains(t, prompt, "Step: test")
	assert.Contains(t, prompt, "Risk level: low")
	assert.NotContains(t, prompt, "Analysis instructions:")
	assert.Contains(t, prompt, "all passed")
}

func TestAnalyzerBuildPrompt_DefaultRiskLevel(t *testing.T) {
	a := NewAnalyzer(nil)
	prompt := a.BuildPrompt("build", "", "ok", "")

	assert.Contains(t, prompt, "Risk level: medium")
}

func TestAnalyzerBuildPrompt_AllRiskLevels(t *testing.T) {
	a := NewAnalyzer(nil)
	for _, risk := range []string{"low", "medium", "high"} {
		prompt := a.BuildPrompt("step", risk, "ctx", "")
		assert.Contains(t, prompt, "Risk level: "+risk)
	}
}

func TestParseAnalysisResponse_ValidJSON(t *testing.T) {
	raw := `{"decision": "approve", "reasoning": "All tests passed", "flags": {"coverage": "95%"}}`
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed
	assert.Equal(t, "All tests passed", result.Reasoning)
	assert.Equal(t, "95%", result.Flags["coverage"])
}

func TestParseAnalysisResponse_ValidJSON_Reject(t *testing.T) {
	raw := `{"decision": "reject", "reasoning": "Build failed"}`
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "halt", result.Decision) // reject normalized to halt
	assert.Equal(t, "Build failed", result.Reasoning)
}

func TestParseAnalysisResponse_ValidJSON_NeedsHuman(t *testing.T) {
	raw := `{"decision": "needs_human", "reasoning": "Ambiguous output"}`
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "needs_human", result.Decision)
}

func TestParseAnalysisResponse_JSONWithSurroundingText(t *testing.T) {
	raw := `Here is my analysis: {"decision": "approve", "reasoning": "looks good"} end.`
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed
}

func TestParseAnalysisResponse_JSONInCodeBlock(t *testing.T) {
	raw := "```json\n{\"decision\": \"approve\", \"reasoning\": \"all clear\"}\n```"
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed
	assert.Equal(t, "all clear", result.Reasoning)
}

func TestParseAnalysisResponse_PlainTextDecision(t *testing.T) {
	raw := "After reviewing, decision: approve because everything is fine."
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "proceed", result.Decision) // approve normalized to proceed
}

func TestParseAnalysisResponse_PlainTextDecisionReject(t *testing.T) {
	raw := "Decision: reject\nThe build had errors."
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "halt", result.Decision) // reject normalized to halt
}

func TestParseAnalysisResponse_GarbageInput(t *testing.T) {
	raw := "This is just random text with no decision"
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "needs_human", result.Decision)
	assert.Equal(t, "could not parse LLM response", result.Reasoning)
}

func TestParseAnalysisResponse_EmptyInput(t *testing.T) {
	result := ParseAnalysisResponse("")

	require.NotNil(t, result)
	assert.Equal(t, "needs_human", result.Decision)
}

func TestParseAnalysisResponse_InvalidJSONDecision(t *testing.T) {
	raw := `{"decision": "maybe", "reasoning": "not sure"}`
	result := ParseAnalysisResponse(raw)

	// "maybe" is not a valid decision so JSON parse returns nil,
	// plain text fallback won't find it either -> needs_human.
	require.NotNil(t, result)
	assert.Equal(t, "needs_human", result.Decision)
}

func TestParseAnalysisResponse_CaseInsensitive(t *testing.T) {
	raw := `{"decision": "APPROVE", "reasoning": "ok"}`
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "proceed", result.Decision) // APPROVE normalized to proceed
}

func TestParseAnalysisResponse_ErrorDecision(t *testing.T) {
	raw := `{"decision": "error", "reasoning": "LLM failed"}`
	result := ParseAnalysisResponse(raw)

	require.NotNil(t, result)
	assert.Equal(t, "error", result.Decision)
}

func TestShouldPromptHuman_ProceedLow(t *testing.T) {
	assert.False(t, ShouldPromptHuman("proceed", "low"))
}

func TestShouldPromptHuman_ProceedMedium(t *testing.T) {
	assert.False(t, ShouldPromptHuman("proceed", "medium"))
}

func TestShouldPromptHuman_ProceedHigh(t *testing.T) {
	assert.True(t, ShouldPromptHuman("proceed", "high"))
}

func TestShouldPromptHuman_HaltLow(t *testing.T) {
	assert.True(t, ShouldPromptHuman("halt", "low"))
}

func TestShouldPromptHuman_HaltMedium(t *testing.T) {
	assert.True(t, ShouldPromptHuman("halt", "medium"))
}

func TestShouldPromptHuman_HaltHigh(t *testing.T) {
	assert.True(t, ShouldPromptHuman("halt", "high"))
}

func TestShouldPromptHuman_NeedsHumanLow(t *testing.T) {
	assert.True(t, ShouldPromptHuman("needs_human", "low"))
}

func TestShouldPromptHuman_NeedsHumanMedium(t *testing.T) {
	assert.True(t, ShouldPromptHuman("needs_human", "medium"))
}

func TestShouldPromptHuman_NeedsHumanHigh(t *testing.T) {
	assert.True(t, ShouldPromptHuman("needs_human", "high"))
}

func TestShouldPromptHuman_ErrorLow(t *testing.T) {
	assert.True(t, ShouldPromptHuman("error", "low"))
}

func TestShouldPromptHuman_ErrorMedium(t *testing.T) {
	assert.True(t, ShouldPromptHuman("error", "medium"))
}

func TestShouldPromptHuman_ErrorHigh(t *testing.T) {
	assert.True(t, ShouldPromptHuman("error", "high"))
}

func TestShouldPromptHuman_UnknownDecision(t *testing.T) {
	assert.True(t, ShouldPromptHuman("unknown", "low"))
}

func TestShouldPromptHuman_DefaultRiskLevel(t *testing.T) {
	// Empty risk level defaults to "medium".
	assert.False(t, ShouldPromptHuman("proceed", ""))
	assert.True(t, ShouldPromptHuman("halt", ""))
}

func TestRuneSliceBytes_NoTruncation(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", runeSliceBytes(s, 100))
}

func TestRuneSliceBytes_ExactBoundary(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", runeSliceBytes(s, 5))
}

func TestRuneSliceBytes_MultiByte(t *testing.T) {
	// "€" is 3 bytes. "a€b" = 5 bytes. Slicing at 3 should give "a" (not "a" + partial €).
	s := "a€b"
	result := runeSliceBytes(s, 3)
	// 3 bytes would cut into the middle of '€' if not rune-aware.
	// The function should back up to just "a" (1 byte) or include full "a€" (4 bytes).
	// Since we want at most 3 bytes: "a" (1 byte).
	assert.Equal(t, "a", result)
	assert.LessOrEqual(t, len(result), 3)
}

func TestRuneSliceBytesFromEnd_NoTruncation(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", runeSliceBytesFromEnd(s, 100))
}

func TestRuneSliceBytesFromEnd_MultiByte(t *testing.T) {
	// "a€b" = 5 bytes. Taking last 3 bytes would start mid-rune.
	s := "a€b"
	result := runeSliceBytesFromEnd(s, 3)
	// start = 5-3 = 2, which is mid-€, so advance to byte 4 -> "b".
	assert.Equal(t, "b", result)
	assert.LessOrEqual(t, len(result), 3)
}
