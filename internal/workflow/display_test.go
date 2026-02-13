package workflow

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplay_TTY_RunStart(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.RunStart("deploy", "run-abc123")

	out := buf.String()
	assert.Contains(t, out, "Running workflow: deploy (run-abc123)")
	assert.Contains(t, out, "\u2550\u2550\u2550") // ═══ border
}

func TestDisplay_TTY_StepStart(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepStart("build", "os=linux")

	out := buf.String()
	assert.Contains(t, out, "\u25D0") // ◐ running icon
	assert.Contains(t, out, "Step: build [os=linux]")
	// TTY step-start omits newline so StepEnd can replace the line.
	assert.False(t, strings.HasSuffix(out, "\n"))
}

func TestDisplay_TTY_StepStart_NoMatrixKey(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepStart("build", "")

	out := buf.String()
	assert.Contains(t, out, "Step: build")
	assert.NotContains(t, out, "[")
	assert.False(t, strings.HasSuffix(out, "\n"))
}

func TestDisplay_TTY_StepEnd_ReplacesActiveLine(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepStart("build", "")
	d.StepEnd("build", "", "passed", 1230*time.Millisecond)

	out := buf.String()
	// StepEnd should emit \r\x1b[K to clear the in-progress line.
	assert.Contains(t, out, "\r\x1b[K")
	assert.Contains(t, out, "\u2713") // ✓ passed icon
	assert.Contains(t, out, "1.23s")
}

func TestDisplay_TTY_StepEnd_Passed(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepEnd("build", "os=linux", "passed", 1230*time.Millisecond)

	out := buf.String()
	assert.Contains(t, out, "\u2713") // ✓ passed icon
	assert.Contains(t, out, "Step: build [os=linux]")
	assert.Contains(t, out, "1.23s")
	assert.NotContains(t, out, "FAILED")
}

func TestDisplay_TTY_StepEnd_Failed(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepEnd("test", "os=linux", "failed", 450*time.Millisecond)

	out := buf.String()
	assert.Contains(t, out, "\u2717") // ✗ failed icon
	assert.Contains(t, out, "FAILED")
	assert.Contains(t, out, "0.45s")
}

func TestDisplay_TTY_StepEnd_Skipped(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepEnd("deploy", "os=linux", "skipped", 0)

	out := buf.String()
	assert.Contains(t, out, "\u2298") // ⊘ skipped icon
	assert.Contains(t, out, "(skipped)")
	assert.NotContains(t, out, "0.00s")
}

func TestDisplay_TTY_AnalysisStart(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.AnalysisStart("build")

	out := buf.String()
	assert.Contains(t, out, "\u2026") // … analyzing icon
	assert.Contains(t, out, "Analyzing: build")
}

func TestDisplay_TTY_AnalysisEnd_Proceed(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.AnalysisEnd("build", "proceed")

	out := buf.String()
	assert.Contains(t, out, "\u2713") // ✓ proceed
	assert.Contains(t, out, "Analysis: build -> proceed")
}

func TestDisplay_TTY_AnalysisEnd_Halt(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.AnalysisEnd("build", "halt")

	out := buf.String()
	assert.Contains(t, out, "\u2717") // ✗ halt
	assert.Contains(t, out, "Analysis: build -> halt")
}

func TestDisplay_TTY_ReviewPrompt(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.ReviewPrompt("deploy")

	out := buf.String()
	assert.Contains(t, out, "\u25CB") // ○ pending icon
	assert.Contains(t, out, "Review needed: deploy")
}

func TestDisplay_TTY_RunEnd_AllPassed(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	steps := []StepSummary{
		{Name: "build", Status: "passed", Duration: 1230 * time.Millisecond},
		{Name: "test", Status: "passed", Duration: 450 * time.Millisecond},
	}

	d.RunEnd("PASSED", 1680*time.Millisecond, steps)

	out := buf.String()
	assert.Contains(t, out, "Run PASSED")
	assert.Contains(t, out, "2 passed")
	assert.Contains(t, out, "1.68s")
	assert.NotContains(t, out, "failed")
	assert.NotContains(t, out, "skipped")
}

func TestDisplay_TTY_RunEnd_WithFailures(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	steps := []StepSummary{
		{Name: "build", Status: "passed", Duration: 1230 * time.Millisecond},
		{Name: "test", Status: "failed", Duration: 450 * time.Millisecond},
		{Name: "deploy", Status: "skipped", Duration: 0},
	}

	d.RunEnd("FAILED", 1680*time.Millisecond, steps)

	out := buf.String()
	assert.Contains(t, out, "Run FAILED")
	assert.Contains(t, out, "1 passed")
	assert.Contains(t, out, "1 failed")
	assert.Contains(t, out, "1 skipped")
	assert.Contains(t, out, "1.68s")
}

func TestDisplay_TTY_RunEnd_NoSteps(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.RunEnd("PASSED", 0, nil)

	out := buf.String()
	assert.Contains(t, out, "0 passed")
}

func TestDisplay_Plain_RunStart(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.RunStart("deploy", "run-abc123")

	out := buf.String()
	assert.Equal(t, "[run_start] deploy (run-abc123)\n", out)
}

func TestDisplay_Plain_StepStart(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepStart("build", "os=linux")

	out := buf.String()
	assert.Equal(t, "[step_start] build [os=linux]\n", out)
}

func TestDisplay_Plain_StepEnd_Passed(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepEnd("build", "os=linux", "passed", 1230*time.Millisecond)

	out := buf.String()
	assert.Equal(t, "[step_end] build [os=linux] passed 1.23s\n", out)
}

func TestDisplay_Plain_StepEnd_Failed(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepEnd("test", "os=linux", "failed", 450*time.Millisecond)

	out := buf.String()
	assert.Equal(t, "[step_end] test [os=linux] failed 0.45s\n", out)
}

func TestDisplay_Plain_StepEnd_Skipped(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepEnd("deploy", "os=linux", "skipped", 0)

	out := buf.String()
	assert.Equal(t, "[step_skip] deploy [os=linux]\n", out)
}

func TestDisplay_TTY_StepError(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepError("error: command not found\nexit status 1", "")

	out := buf.String()
	assert.Contains(t, out, "  error: command not found")
	assert.Contains(t, out, "  exit status 1")
}

func TestDisplay_TTY_StepError_FallbackToStdout(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepError("", "FAIL: TestSomething")

	out := buf.String()
	assert.Contains(t, out, "  FAIL: TestSomething")
}

func TestDisplay_TTY_StepError_Empty(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepError("", "")

	assert.Empty(t, buf.String())
}

func TestDisplay_Plain_StepError(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepError("error: missing file\nexit status 2", "")

	out := buf.String()
	assert.Contains(t, out, "[step_error] error: missing file")
	assert.Contains(t, out, "[step_error] exit status 2")
}

func TestDisplay_Plain_AnalysisStart(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.AnalysisStart("build")

	out := buf.String()
	assert.Equal(t, "[analysis_start] build\n", out)
}

func TestDisplay_Plain_AnalysisEnd(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.AnalysisEnd("build", "approve")

	out := buf.String()
	assert.Equal(t, "[analysis_end] build approve\n", out)
}

func TestDisplay_Plain_ReviewPrompt(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.ReviewPrompt("deploy")

	out := buf.String()
	assert.Equal(t, "[review] deploy\n", out)
}

func TestDisplay_Plain_RunEnd(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	steps := []StepSummary{
		{Name: "build", Status: "passed", Duration: 1230 * time.Millisecond},
		{Name: "test", Status: "failed", Duration: 450 * time.Millisecond},
		{Name: "deploy", Status: "skipped", Duration: 0},
	}

	d.RunEnd("failed", 1680*time.Millisecond, steps)

	out := buf.String()
	assert.Equal(t, "[run_end] failed 1.68s (1 passed, 1 failed, 1 skipped)\n", out)
}

func TestDisplay_MatrixKey_Present(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepStart("build", "os=linux")
	d.StepEnd("build", "os=linux", "passed", time.Second)

	out := buf.String()
	assert.True(t, strings.Contains(out, "[os=linux]"))
}

func TestDisplay_MatrixKey_Empty(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.StepStart("build", "")
	d.StepEnd("build", "", "passed", time.Second)

	out := buf.String()
	// The ANSI clear sequence \x1b[K contains a bracket, so strip it first.
	cleaned := strings.ReplaceAll(out, "\x1b[K", "")
	assert.False(t, strings.Contains(cleaned, "["))
}

func TestDisplay_Plain_MatrixKey_Present(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepStart("build", "os=linux")
	d.StepEnd("build", "os=linux", "passed", time.Second)

	out := buf.String()
	assert.True(t, strings.Contains(out, "[os=linux]"))
}

func TestDisplay_Plain_MatrixKey_Empty(t *testing.T) {
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.StepStart("build", "")
	d.StepEnd("build", "", "passed", time.Second)

	out := buf.String()
	// Plain mode has [step_start] and [step_end] tags, so check that
	// no matrix key brackets appear after the step name.
	assert.NotContains(t, out, "build [")
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{0, "0.00s"},
		{time.Millisecond, "0.00s"},
		{10 * time.Millisecond, "0.01s"},
		{100 * time.Millisecond, "0.10s"},
		{1230 * time.Millisecond, "1.23s"},
		{time.Minute + 30*time.Second, "90.00s"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatDuration(tc.input))
		})
	}
}

func TestFormatSummary(t *testing.T) {
	tests := []struct {
		passed, failed, skipped, cancelled int
		expected                           string
	}{
		{0, 0, 0, 0, "0 passed"},
		{3, 0, 0, 0, "3 passed"},
		{1, 1, 0, 0, "1 passed, 1 failed"},
		{1, 1, 1, 0, "1 passed, 1 failed, 1 skipped"},
		{0, 1, 0, 0, "1 failed"},
		{0, 0, 2, 0, "2 skipped"},
		{0, 0, 0, 1, "1 cancelled"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatSummary(tc.passed, tc.failed, tc.skipped, tc.cancelled))
		})
	}
}

func TestCountStatuses(t *testing.T) {
	steps := []StepSummary{
		{Status: "passed"},
		{Status: "passed"},
		{Status: "failed"},
		{Status: "skipped"},
		{Status: "cancelled"},
		{Status: "passed"},
	}
	passed, failed, skipped, cancelled := countStatuses(steps)
	assert.Equal(t, 3, passed)
	assert.Equal(t, 1, failed)
	assert.Equal(t, 1, skipped)
	assert.Equal(t, 1, cancelled)
}

func TestIconForDecision(t *testing.T) {
	assert.Equal(t, iconPassed, iconForDecision("proceed"))
	assert.Equal(t, iconFailed, iconForDecision("halt"))
	assert.Equal(t, iconPending, iconForDecision("needs_human"))
	assert.Equal(t, iconPending, iconForDecision("unknown"))
}

func TestDisplay_FullTTYSequence(t *testing.T) {
	// End-to-end test: simulate a full workflow run in TTY mode.
	// Call ordering reflects the real flow: StepStart → StepEnd → analysis.
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayTTY)

	d.RunStart("deploy", "run-abc123")
	d.StepStart("build", "os=linux")
	d.StepEnd("build", "os=linux", "passed", 1230*time.Millisecond)
	d.StepStart("test", "os=linux")
	d.StepEnd("test", "os=linux", "failed", 450*time.Millisecond)
	d.AnalysisStart("test")
	d.AnalysisEnd("test", "approve")
	d.StepEnd("deploy", "os=linux", "skipped", 0)

	steps := []StepSummary{
		{Name: "build", Status: "passed", Duration: 1230 * time.Millisecond},
		{Name: "test", Status: "failed", Duration: 450 * time.Millisecond},
		{Name: "deploy", Status: "skipped", Duration: 0},
	}
	d.RunEnd("FAILED", 1680*time.Millisecond, steps)

	out := buf.String()

	// Verify key structural elements are present.
	assert.Contains(t, out, "Running workflow: deploy")
	assert.Contains(t, out, "Run FAILED")
	// In-progress lines are replaced, so the final output should not
	// contain the running icon for steps that completed.
	assert.Contains(t, out, "\u2713") // ✓ passed
	assert.Contains(t, out, "\u2717") // ✗ failed
	assert.Contains(t, out, "\u2298") // ⊘ skipped
}

func TestDisplay_FullPlainSequence(t *testing.T) {
	// End-to-end test: simulate a full workflow run in plain mode.
	// Plain mode prints both step_start and step_end as separate lines.
	var buf bytes.Buffer
	d := NewDisplay(&buf, DisplayPlain)

	d.RunStart("deploy", "run-abc123")
	d.StepStart("build", "os=linux")
	d.StepEnd("build", "os=linux", "passed", 1230*time.Millisecond)
	d.StepStart("test", "os=linux")
	d.StepEnd("test", "os=linux", "failed", 450*time.Millisecond)
	d.StepEnd("deploy", "os=linux", "skipped", 0)

	steps := []StepSummary{
		{Name: "build", Status: "passed", Duration: 1230 * time.Millisecond},
		{Name: "test", Status: "failed", Duration: 450 * time.Millisecond},
		{Name: "deploy", Status: "skipped", Duration: 0},
	}
	d.RunEnd("failed", 1680*time.Millisecond, steps)

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	expected := []string{
		"[run_start] deploy (run-abc123)",
		"[step_start] build [os=linux]",
		"[step_end] build [os=linux] passed 1.23s",
		"[step_start] test [os=linux]",
		"[step_end] test [os=linux] failed 0.45s",
		"[step_skip] deploy [os=linux]",
		"[run_end] failed 1.68s (1 passed, 1 failed, 1 skipped)",
	}

	require.Equal(t, len(expected), len(lines), "line count mismatch")
	for i, exp := range expected {
		assert.Equal(t, exp, lines[i], "line %d mismatch", i)
	}
}
