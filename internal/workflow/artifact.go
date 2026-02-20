package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/runger/clai/internal/config"
)

// ArtifactEvent represents a single event in the JSONL log.
type ArtifactEvent struct {
	Data      interface{} `json:"data"`      // event-specific payload
	Type      string      `json:"type"`      // event type
	Timestamp int64       `json:"timestamp"` // Unix milliseconds
}

// Event types.
const (
	EventRunStart      = "run_start"
	EventStepStart     = "step_start"
	EventStepEnd       = "step_end"
	EventAnalysis      = "analysis"
	EventHumanDecision = "human_decision"
	EventRunEnd        = "run_end"
)

// NormalizePath applies D19 path normalization (forward slashes on all platforms).
func NormalizePath(p string) string {
	return filepath.ToSlash(p)
}

// RunStartData is the payload for run_start events.
type RunStartData struct {
	RunID        string `json:"run_id"`
	WorkflowName string `json:"workflow_name"`
	WorkflowPath string `json:"workflow_path"`
}

// StepStartData is the payload for step_start events.
type StepStartData struct {
	RunID     string `json:"run_id"`
	StepID    string `json:"step_id"`
	StepName  string `json:"step_name"`
	MatrixKey string `json:"matrix_key,omitempty"`
	Command   string `json:"command"`
}

// StepEndData is the payload for step_end events.
type StepEndData struct {
	RunID      string `json:"run_id"`
	StepID     string `json:"step_id"`
	MatrixKey  string `json:"matrix_key,omitempty"`
	Status     string `json:"status"` // "passed", "failed", "skipped"
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// AnalysisData is the payload for analysis events.
type AnalysisData struct {
	RunID     string `json:"run_id"`
	StepID    string `json:"step_id"`
	MatrixKey string `json:"matrix_key,omitempty"`
	Decision  string `json:"decision"`
	Reasoning string `json:"reasoning"`
}

// HumanDecisionData is the payload for human_decision events.
type HumanDecisionData struct {
	RunID     string `json:"run_id"`
	StepID    string `json:"step_id"`
	MatrixKey string `json:"matrix_key,omitempty"`
	Action    string `json:"action"` // "approve", "reject", etc.
	Input     string `json:"input,omitempty"`
}

// RunEndData is the payload for run_end events.
type RunEndData struct {
	RunID      string `json:"run_id"`
	Status     string `json:"status"` // "passed", "failed"
	DurationMs int64  `json:"duration_ms"`
}

// RunArtifact writes events to a JSONL file for a workflow run.
type RunArtifact struct {
	file   *os.File
	runID  string
	logDir string
}

// NewRunArtifact creates a new artifact writer for the given run ID.
// The file is created at <logDir>/<sanitized-run-id>.jsonl.
// Uses config.DefaultPaths().WorkflowLogDir() as the default log directory.
func NewRunArtifact(runID string) (*RunArtifact, error) {
	return NewRunArtifactWithDir(runID, config.DefaultPaths().WorkflowLogDir(context.Background()))
}

// NewRunArtifactWithDir creates a new artifact writer with a custom log directory.
// Useful for testing.
func NewRunArtifactWithDir(runID, logDir string) (*RunArtifact, error) {
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	safeName := sanitizePathComponent(runID)
	path := filepath.Join(logDir, safeName+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: path is constructed from sanitized runID within logDir
	if err != nil {
		return nil, fmt.Errorf("open artifact file: %w", err)
	}

	return &RunArtifact{
		file:   f,
		runID:  runID,
		logDir: logDir,
	}, nil
}

// WriteEvent appends an event to the JSONL file.
// Errors are logged but do NOT halt execution (per m13 risk finding).
func (a *RunArtifact) WriteEvent(eventType string, data interface{}) {
	evt := ArtifactEvent{
		Type:      eventType,
		Timestamp: time.Now().UnixMilli(),
		Data:      data,
	}

	line, err := json.Marshal(evt)
	if err != nil {
		slog.Warn("artifact: marshal event", "error", err, "type", eventType)
		return
	}

	line = append(line, '\n')

	if _, err := a.file.Write(line); err != nil {
		slog.Warn("artifact: write event", "error", err, "type", eventType)
	}
}

// WriteStepLog writes per-step stdout and stderr to individual log files
// in a steps/ subdirectory under the run's log directory.
// Files are named <sanitized-step-id>.stdout and <sanitized-step-id>.stderr.
// Errors are logged but do NOT halt execution.
func (a *RunArtifact) WriteStepLog(stepID, stdout, stderr string) {
	if stdout == "" && stderr == "" {
		return
	}

	stepsDir := filepath.Join(a.logDir, sanitizePathComponent(a.runID)+"-steps")
	if err := os.MkdirAll(stepsDir, 0o750); err != nil {
		slog.Warn("artifact: create steps dir", "error", err)
		return
	}

	safeID := sanitizePathComponent(stepID)
	if safeID == "" {
		safeID = "unnamed"
	}

	if stdout != "" {
		stdoutPath := filepath.Join(stepsDir, safeID+".stdout")
		if err := os.WriteFile(stdoutPath, []byte(stdout), 0o600); err != nil {
			slog.Warn("artifact: write step stdout", "error", err, "step", stepID)
		}
	}

	if stderr != "" {
		stderrPath := filepath.Join(stepsDir, safeID+".stderr")
		if err := os.WriteFile(stderrPath, []byte(stderr), 0o600); err != nil {
			slog.Warn("artifact: write step stderr", "error", err, "step", stepID)
		}
	}
}

// Close closes the underlying file.
func (a *RunArtifact) Close() error {
	return a.file.Close()
}

// Path returns the path to the JSONL file.
func (a *RunArtifact) Path() string {
	return a.file.Name()
}
