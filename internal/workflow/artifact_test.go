package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifact_WriteRunStart(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("run-001", dir)
	require.NoError(t, err)
	defer a.Close()

	a.WriteEvent(EventRunStart, RunStartData{
		RunID:        "run-001",
		WorkflowName: "test-wf",
		WorkflowPath: "/tmp/test.yaml",
	})

	content, err := os.ReadFile(a.Path())
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 1)

	var evt ArtifactEvent
	err = json.Unmarshal([]byte(lines[0]), &evt)
	require.NoError(t, err)

	assert.Equal(t, EventRunStart, evt.Type)
	assert.Greater(t, evt.Timestamp, int64(0))

	// Unmarshal Data into RunStartData.
	dataBytes, err := json.Marshal(evt.Data)
	require.NoError(t, err)
	var data RunStartData
	err = json.Unmarshal(dataBytes, &data)
	require.NoError(t, err)

	assert.Equal(t, "run-001", data.RunID)
	assert.Equal(t, "test-wf", data.WorkflowName)
	assert.Equal(t, "/tmp/test.yaml", data.WorkflowPath)
}

func TestArtifact_WriteMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("run-multi", dir)
	require.NoError(t, err)
	defer a.Close()

	a.WriteEvent(EventRunStart, RunStartData{
		RunID:        "run-multi",
		WorkflowName: "multi-wf",
		WorkflowPath: "/tmp/multi.yaml",
	})
	a.WriteEvent(EventStepStart, StepStartData{
		RunID:    "run-multi",
		StepID:   "build",
		StepName: "Build",
		Command:  "go build ./...",
	})
	a.WriteEvent(EventStepEnd, StepEndData{
		RunID:      "run-multi",
		StepID:     "build",
		Status:     "passed",
		ExitCode:   0,
		DurationMs: 1234,
	})

	content, err := os.ReadFile(a.Path())
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 3)

	// Verify each line is valid JSON.
	for i, line := range lines {
		var evt ArtifactEvent
		err := json.Unmarshal([]byte(line), &evt)
		require.NoError(t, err, "line %d should be valid JSON", i)
	}
}

func TestArtifact_EventTypes(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("run-types", dir)
	require.NoError(t, err)
	defer a.Close()

	events := []struct {
		eventType string
		data      interface{}
	}{
		{EventRunStart, RunStartData{RunID: "run-types", WorkflowName: "wf", WorkflowPath: "/tmp/wf.yaml"}},
		{EventStepStart, StepStartData{RunID: "run-types", StepID: "s1", StepName: "Step 1", Command: "echo hi"}},
		{EventStepEnd, StepEndData{RunID: "run-types", StepID: "s1", Status: "passed", ExitCode: 0, DurationMs: 100}},
		{EventAnalysis, AnalysisData{RunID: "run-types", StepID: "s1", Decision: "approve", Reasoning: "looks good"}},
		{EventHumanDecision, HumanDecisionData{RunID: "run-types", StepID: "s1", Action: "approve", Input: "yes"}},
		{EventRunEnd, RunEndData{RunID: "run-types", Status: "passed", DurationMs: 5000}},
	}

	for _, e := range events {
		a.WriteEvent(e.eventType, e.data)
	}

	content, err := os.ReadFile(a.Path())
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 6)

	for i, line := range lines {
		var evt ArtifactEvent
		err := json.Unmarshal([]byte(line), &evt)
		require.NoError(t, err, "line %d should be valid JSON", i)
		assert.Equal(t, events[i].eventType, evt.Type, "line %d event type", i)
		assert.Greater(t, evt.Timestamp, int64(0), "line %d timestamp", i)
	}
}

func TestArtifact_PathSanitization(t *testing.T) {
	tests := []struct {
		name    string
		runID   string
		wantNot string // character that should NOT appear in the filename
	}{
		{name: "slashes", runID: "run/with/slashes", wantNot: "/"},
		{name: "backslashes", runID: "run\\back\\slash", wantNot: "\\"},
		{name: "colons", runID: "run:colons", wantNot: ":"},
		{name: "special chars", runID: "run*?<>|", wantNot: "*"},
		{name: "path traversal", runID: "../../../etc/passwd", wantNot: ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			a, err := NewRunArtifactWithDir(tt.runID, dir)
			require.NoError(t, err)
			defer a.Close()

			// The filename (base) should not contain the unsafe character.
			base := filepath.Base(a.Path())
			assert.NotContains(t, base, tt.wantNot,
				"filename %q should not contain %q", base, tt.wantNot)

			// Should end with .jsonl.
			assert.True(t, strings.HasSuffix(base, ".jsonl"),
				"filename %q should end with .jsonl", base)

			// File should be inside the temp dir (no traversal).
			assert.True(t, strings.HasPrefix(a.Path(), dir),
				"path %q should be inside dir %q", a.Path(), dir)
		})
	}
}

func TestArtifact_Close(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("run-close", dir)
	require.NoError(t, err)

	// Write an event before closing.
	a.WriteEvent(EventRunStart, RunStartData{
		RunID:        "run-close",
		WorkflowName: "close-wf",
		WorkflowPath: "/tmp/close.yaml",
	})

	err = a.Close()
	require.NoError(t, err)

	// Verify the file exists and has content.
	content, err := os.ReadFile(a.Path())
	require.NoError(t, err)
	assert.NotEmpty(t, content)
}

func TestArtifact_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("run-after-close", dir)
	require.NoError(t, err)

	err = a.Close()
	require.NoError(t, err)

	// Writing after close should not panic; error is logged via slog.
	assert.NotPanics(t, func() {
		a.WriteEvent(EventRunEnd, RunEndData{
			RunID:      "run-after-close",
			Status:     "failed",
			DurationMs: 0,
		})
	})
}

func TestArtifact_Path(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("my-run", dir)
	require.NoError(t, err)
	defer a.Close()

	expected := filepath.Join(dir, "my-run.jsonl")
	assert.Equal(t, expected, a.Path())
}

func TestArtifact_StepStartWithMatrixKey(t *testing.T) {
	dir := t.TempDir()
	a, err := NewRunArtifactWithDir("run-matrix", dir)
	require.NoError(t, err)
	defer a.Close()

	a.WriteEvent(EventStepStart, StepStartData{
		RunID:     "run-matrix",
		StepID:    "deploy",
		StepName:  "Deploy",
		MatrixKey: "stack=dev,region=us-east-1",
		Command:   "pulumi up",
	})

	content, err := os.ReadFile(a.Path())
	require.NoError(t, err)

	var evt ArtifactEvent
	err = json.Unmarshal([]byte(strings.TrimSpace(string(content))), &evt)
	require.NoError(t, err)

	dataBytes, err := json.Marshal(evt.Data)
	require.NoError(t, err)
	var data StepStartData
	err = json.Unmarshal(dataBytes, &data)
	require.NoError(t, err)

	assert.Equal(t, "stack=dev,region=us-east-1", data.MatrixKey)
}
