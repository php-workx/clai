package workflow

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// DisplayMode controls how output is rendered.
type DisplayMode int

const (
	// DisplayTTY uses rich formatting with status icons.
	DisplayTTY DisplayMode = iota
	// DisplayPlain uses simple one-line-per-event output.
	DisplayPlain
)

// Status icons for TTY mode.
const (
	iconPending   = "\u25CB" // ○
	iconRunning   = "\u25D0" // ◐
	iconPassed    = "\u2713" // ✓
	iconFailed    = "\u2717" // ✗
	iconSkipped   = "\u2298" // ⊘
	iconCancelled = "\u25D1" // ◑
	iconAnalyzing = "\u2026" // …
)

// Display renders workflow progress to the terminal.
type Display struct {
	writer     io.Writer
	mode       DisplayMode
	activeStep bool // TTY only: true when a step-start line awaits replacement
}

// StepSummary is a minimal step result for display purposes.
type StepSummary struct {
	Name     string
	Status   string // "passed", "failed", "skipped", "cancelled"
	Duration time.Duration
}

// NewDisplay creates a display writer.
// If mode is DisplayTTY, uses rich formatting.
// If mode is DisplayPlain, uses simple line output.
func NewDisplay(writer io.Writer, mode DisplayMode) *Display {
	return &Display{
		writer: writer,
		mode:   mode,
	}
}

// DetectMode returns DisplayTTY if stdout is a TTY, TERM != "dumb",
// and NO_COLOR is not set (SS11.5); otherwise DisplayPlain.
func DetectMode() DisplayMode {
	if os.Getenv("TERM") == "dumb" {
		return DisplayPlain
	}
	// SS11.5: Respect NO_COLOR (https://no-color.org/).
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return DisplayPlain
	}
	fi, err := os.Stdout.Stat()
	if err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return DisplayTTY
	}
	return DisplayPlain
}

// RunStart prints the workflow run header.
func (d *Display) RunStart(workflowName, runID string) {
	if d.mode == DisplayTTY {
		fmt.Fprintf(d.writer, "\u2550\u2550\u2550 Running workflow: %s (%s) \u2550\u2550\u2550\n", workflowName, runID)
	} else {
		fmt.Fprintf(d.writer, "[run_start] %s (%s)\n", workflowName, runID)
	}
}

// StepStart prints step start indicator.
// In TTY mode the line is printed without a newline so that StepEnd can
// replace it in-place with the final status.
func (d *Display) StepStart(stepName, matrixKey string) {
	d.clearActiveLine()
	label := formatStepLabel(stepName, matrixKey)
	if d.mode == DisplayTTY {
		fmt.Fprintf(d.writer, "%s Step: %s", iconRunning, label)
		d.activeStep = true
	} else {
		fmt.Fprintf(d.writer, "[step_start] %s\n", label)
	}
}

// StepEnd prints step completion with status and duration.
// In TTY mode, if a step-start line is active it is replaced in-place.
func (d *Display) StepEnd(stepName, matrixKey, status string, duration time.Duration) {
	label := formatStepLabel(stepName, matrixKey)
	durStr := formatDuration(duration)

	if d.mode == DisplayTTY {
		if d.activeStep {
			fmt.Fprint(d.writer, "\r\x1b[K")
			d.activeStep = false
		}
		switch status {
		case "passed":
			fmt.Fprintf(d.writer, "%s Step: %s (%s)\n", iconPassed, label, durStr)
		case "failed":
			fmt.Fprintf(d.writer, "%s Step: %s FAILED (%s)\n", iconFailed, label, durStr)
		case "skipped":
			fmt.Fprintf(d.writer, "%s Step: %s (skipped)\n", iconSkipped, label)
		case "cancelled":
			fmt.Fprintf(d.writer, "%s Step: %s (cancelled)\n", iconCancelled, label)
		default:
			fmt.Fprintf(d.writer, "%s Step: %s %s (%s)\n", iconPending, label, status, durStr)
		}
	} else {
		switch status {
		case "skipped":
			fmt.Fprintf(d.writer, "[step_skip] %s\n", label)
		case "cancelled":
			fmt.Fprintf(d.writer, "[step_cancelled] %s\n", label)
		default:
			fmt.Fprintf(d.writer, "[step_end] %s %s %s\n", label, status, durStr)
		}
	}
}

// clearActiveLine finalizes any in-progress step-start line by emitting
// a newline. This prevents garbled output if another display method is
// called before StepEnd (which normally replaces the line).
func (d *Display) clearActiveLine() {
	if d.activeStep {
		fmt.Fprint(d.writer, "\n")
		d.activeStep = false
	}
}

// StepError prints the error output of a failed step.
func (d *Display) StepError(stderrTail, stdoutTail string) {
	// Prefer stderr; fall back to stdout if stderr is empty.
	output := strings.TrimSpace(stderrTail)
	if output == "" {
		output = strings.TrimSpace(stdoutTail)
	}
	if output == "" {
		return
	}

	if d.mode == DisplayTTY {
		for _, line := range strings.Split(output, "\n") {
			fmt.Fprintf(d.writer, "  %s\n", line)
		}
	} else {
		for _, line := range strings.Split(output, "\n") {
			fmt.Fprintf(d.writer, "[step_error] %s\n", line)
		}
	}
}

// AnalysisStart prints analysis start indicator.
func (d *Display) AnalysisStart(stepName string) {
	d.clearActiveLine()
	if d.mode == DisplayTTY {
		fmt.Fprintf(d.writer, "%s Analyzing: %s\n", iconAnalyzing, stepName)
	} else {
		fmt.Fprintf(d.writer, "[analysis_start] %s\n", stepName)
	}
}

// AnalysisEnd prints analysis result.
func (d *Display) AnalysisEnd(stepName, decision string) {
	if d.mode == DisplayTTY {
		icon := iconForDecision(decision)
		fmt.Fprintf(d.writer, "%s Analysis: %s -> %s\n", icon, stepName, decision)
	} else {
		fmt.Fprintf(d.writer, "[analysis_end] %s %s\n", stepName, decision)
	}
}

// ReviewPrompt prints that human review is needed.
func (d *Display) ReviewPrompt(stepName string) {
	d.clearActiveLine()
	if d.mode == DisplayTTY {
		fmt.Fprintf(d.writer, "%s Review needed: %s\n", iconPending, stepName)
	} else {
		fmt.Fprintf(d.writer, "[review] %s\n", stepName)
	}
}

// RunEnd prints the final run summary.
func (d *Display) RunEnd(status string, totalDuration time.Duration, steps []StepSummary) {
	d.clearActiveLine()
	passed, failed, skipped, cancelled := countStatuses(steps)
	durStr := formatDuration(totalDuration)
	summary := formatSummary(passed, failed, skipped, cancelled)

	if d.mode == DisplayTTY {
		statusUpper := strings.ToUpper(status)
		fmt.Fprintf(d.writer, "\n\u2550\u2550\u2550 Run %s \u2014 %s (%s) \u2550\u2550\u2550\n", statusUpper, summary, durStr)
	} else {
		fmt.Fprintf(d.writer, "[run_end] %s %s (%s)\n", status, durStr, summary)
	}
}

// formatStepLabel builds the display label for a step, including matrix key if present.
func formatStepLabel(stepName, matrixKey string) string {
	if matrixKey == "" {
		return stepName
	}
	return fmt.Sprintf("%s [%s]", stepName, matrixKey)
}

// formatDuration formats a duration as seconds with two decimal places.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// countStatuses tallies step results by status.
func countStatuses(steps []StepSummary) (passed, failed, skipped, cancelled int) {
	for _, s := range steps {
		switch s.Status {
		case "passed":
			passed++
		case "failed":
			failed++
		case "skipped":
			skipped++
		case "cancelled":
			cancelled++
		}
	}
	return
}

// formatSummary builds a human-readable summary string like "1 passed, 1 failed, 1 skipped".
func formatSummary(passed, failed, skipped, cancelled int) string {
	var parts []string
	if passed > 0 {
		parts = append(parts, fmt.Sprintf("%d passed", passed))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	if cancelled > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", cancelled))
	}
	if len(parts) == 0 {
		return "0 passed"
	}
	return strings.Join(parts, ", ")
}

// iconForDecision returns the appropriate icon for an analysis decision.
func iconForDecision(decision string) string {
	switch Decision(decision) {
	case DecisionProceed:
		return iconPassed
	case DecisionHalt:
		return iconFailed
	case DecisionNeedsHuman:
		return iconPending
	default:
		return iconPending
	}
}
