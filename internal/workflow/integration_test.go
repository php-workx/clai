package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIntegrationOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("integration tests use Unix shell commands")
	}
}

// TestIntegration_HappyPath exercises the full pipeline: YAML parse -> validate ->
// Runner execute with real ShellAdapter, expression resolution, and output capture.
func TestIntegration_HappyPath(t *testing.T) {
	skipIntegrationOnWindows(t)

	yamlContent := `
name: integration-test
env:
  GREETING: hello
jobs:
  build:
    steps:
      - id: greet
        name: Greet
        run: echo "$GREETING world"
        shell: true
      - id: output
        name: Set output
        run: echo "RESULT=success" > "$CLAI_OUTPUT"
        shell: true
      - id: use-output
        name: Use output
        run: echo "Got ${{ steps.output.outputs.RESULT }}"
        shell: true
`

	// Parse YAML.
	wf, err := ParseWorkflow([]byte(yamlContent))
	require.NoError(t, err, "parse should succeed")

	// Validate.
	verrs := ValidateWorkflow(wf)
	assert.Empty(t, verrs, "validation should pass with no errors")

	// Execute using real Runner with real ShellAdapter.
	job := wf.Jobs["build"]
	require.NotNil(t, job, "build job must exist")

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		Env:     wf.Env,
	}
	runner := NewRunner(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := runner.Run(ctx, job.Steps)

	// Verify all 3 steps passed.
	require.Len(t, result.Steps, 3)
	assert.Equal(t, "passed", result.Steps[0].Status, "greet step should pass")
	assert.Equal(t, "passed", result.Steps[1].Status, "output step should pass")
	assert.Equal(t, "passed", result.Steps[2].Status, "use-output step should pass")

	// Verify step "output" captured the RESULT output.
	assert.Equal(t, "success", result.Steps[1].Outputs["RESULT"])

	// Verify overall run status.
	assert.Equal(t, "passed", result.Status)

	// Verify stdout captured expected content.
	assert.Contains(t, result.Steps[0].StdoutTail, "hello world")
	assert.Contains(t, result.Steps[2].StdoutTail, "Got success")
}

// TestIntegration_StepFailure verifies that step failure halts execution and
// remaining steps are marked as skipped.
func TestIntegration_StepFailure(t *testing.T) {
	skipIntegrationOnWindows(t)

	yamlContent := `
name: failure-test
jobs:
  build:
    steps:
      - id: pass
        name: Pass
        run: echo ok
        shell: true
      - id: fail
        name: Fail
        run: exit 1
        shell: true
      - id: skipped
        name: Should Skip
        run: echo should-not-run
        shell: true
`

	wf, err := ParseWorkflow([]byte(yamlContent))
	require.NoError(t, err)

	verrs := ValidateWorkflow(wf)
	assert.Empty(t, verrs)

	job := wf.Jobs["build"]
	require.NotNil(t, job)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := runner.Run(ctx, job.Steps)

	require.Len(t, result.Steps, 3)
	assert.Equal(t, "passed", result.Steps[0].Status, "first step should pass")
	assert.Equal(t, "failed", result.Steps[1].Status, "second step should fail")
	assert.Equal(t, 1, result.Steps[1].ExitCode, "exit code should be 1")
	assert.Equal(t, "skipped", result.Steps[2].Status, "third step should be skipped")
	assert.Equal(t, "failed", result.Status, "overall run should be failed")
}

// TestIntegration_MatrixExpansion verifies that matrix combinations are resolved
// correctly for each matrix entry when run separately.
func TestIntegration_MatrixExpansion(t *testing.T) {
	skipIntegrationOnWindows(t)

	yamlContent := `
name: matrix-test
jobs:
  build:
    strategy:
      matrix:
        include:
          - os: linux
          - os: darwin
    steps:
      - id: show-os
        name: Show OS
        run: echo "Building for ${{ matrix.os }}"
        shell: true
`

	wf, err := ParseWorkflow([]byte(yamlContent))
	require.NoError(t, err)

	verrs := ValidateWorkflow(wf)
	assert.Empty(t, verrs)

	job := wf.Jobs["build"]
	require.NotNil(t, job)
	require.NotNil(t, job.Strategy)
	require.NotNil(t, job.Strategy.Matrix)
	require.Len(t, job.Strategy.Matrix.Include, 2)

	// Run the workflow for each matrix combination separately.
	for _, combo := range job.Strategy.Matrix.Include {
		t.Run("matrix-"+combo["os"], func(t *testing.T) {
			cfg := RunnerConfig{
				WorkDir:    t.TempDir(),
				MatrixVars: combo,
			}
			runner := NewRunner(cfg)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result := runner.Run(ctx, job.Steps)

			assert.Equal(t, "passed", result.Status)
			require.Len(t, result.Steps, 1)
			assert.Equal(t, "passed", result.Steps[0].Status)
			assert.Contains(t, result.Steps[0].StdoutTail, "Building for "+combo["os"])
		})
	}
}

// TestIntegration_ContextCancellation verifies that cancelling the context
// interrupts the running step and skips remaining steps.
func TestIntegration_ContextCancellation(t *testing.T) {
	skipIntegrationOnWindows(t)

	yamlContent := `
name: cancel-test
jobs:
  build:
    steps:
      - name: Long step
        run: sleep 30
        shell: true
      - name: Should Skip
        run: echo after-cancel
        shell: true
`

	wf, err := ParseWorkflow([]byte(yamlContent))
	require.NoError(t, err)

	job := wf.Jobs["build"]
	require.NotNil(t, job)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context shortly after starting.
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result := runner.Run(ctx, job.Steps)
	elapsed := time.Since(start)

	// Should complete well before the sleep would finish.
	assert.Less(t, elapsed, 15*time.Second, "run should not wait for full sleep")

	// Overall run should be cancelled due to cancellation.
	assert.Equal(t, "cancelled", result.Status)

	// All steps should have results.
	require.Len(t, result.Steps, 2)
	assert.Equal(t, "cancelled", result.Steps[0].Status)

	// The second step should be skipped.
	assert.Equal(t, "skipped", result.Steps[1].Status)
}

// TestIntegration_ValidateCommand tests the validate workflow function by
// parsing and validating a complete YAML with env, secrets, and analyze fields.
func TestIntegration_ValidateCommand(t *testing.T) {
	yamlContent := `
name: deploy
env:
  REGION: us-east-1
secrets:
  - name: DEPLOY_TOKEN
    from: env
jobs:
  deploy:
    steps:
      - id: validate
        name: validate
        run: echo "checking..."
        shell: true
      - id: deploy
        name: deploy
        run: echo "deploying to ${{ env.REGION }}"
        shell: true
        analyze: true
        analysis_prompt: Check deployment for errors
        risk_level: high
`

	wf, err := ParseWorkflow([]byte(yamlContent))
	require.NoError(t, err, "parse should succeed")

	verrs := ValidateWorkflow(wf)
	assert.Empty(t, verrs, "ValidateWorkflow should return no errors for valid workflow")

	// Also verify the parsed structure is correct.
	assert.Equal(t, "deploy", wf.Name)
	assert.Equal(t, "us-east-1", wf.Env["REGION"])
	require.Len(t, wf.Secrets, 1)
	assert.Equal(t, "DEPLOY_TOKEN", wf.Secrets[0].Name)
	assert.Equal(t, "env", wf.Secrets[0].From)

	job := wf.Jobs["deploy"]
	require.NotNil(t, job)
	require.Len(t, job.Steps, 2)
	assert.True(t, job.Steps[1].Analyze)
	assert.Equal(t, "high", job.Steps[1].RiskLevel)
	assert.Equal(t, "Check deployment for errors", job.Steps[1].AnalysisPrompt)
}

// TestIntegration_RunArtifact tests JSONL artifact creation during a run.
func TestIntegration_RunArtifact(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")

	runID := "integration-test-run-001"
	artifact, err := NewRunArtifactWithDir(runID, logDir)
	require.NoError(t, err, "creating artifact should succeed")
	defer artifact.Close()

	// Write events that would occur during a real run.
	artifact.WriteEvent(EventRunStart, RunStartData{
		RunID:        runID,
		WorkflowName: "test-workflow",
		WorkflowPath: "/tmp/test.yaml",
	})
	artifact.WriteEvent(EventStepStart, StepStartData{
		RunID:    runID,
		StepID:   "step1",
		StepName: "Build",
		Command:  "echo hello",
	})
	artifact.WriteEvent(EventStepEnd, StepEndData{
		RunID:      runID,
		StepID:     "step1",
		Status:     "passed",
		ExitCode:   0,
		DurationMs: 150,
	})
	artifact.WriteEvent(EventRunEnd, RunEndData{
		RunID:      runID,
		Status:     "passed",
		DurationMs: 200,
	})

	// Close to flush writes.
	err = artifact.Close()
	require.NoError(t, err)

	// Read the JSONL file.
	data, err := os.ReadFile(artifact.Path())
	require.NoError(t, err, "should be able to read artifact file")

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 4, "should have 4 event lines")

	// Verify each line is valid JSON with correct event type.
	expectedTypes := []string{EventRunStart, EventStepStart, EventStepEnd, EventRunEnd}
	for i, line := range lines {
		var evt ArtifactEvent
		err := json.Unmarshal([]byte(line), &evt)
		require.NoError(t, err, "line %d should be valid JSON", i)
		assert.Equal(t, expectedTypes[i], evt.Type, "line %d event type mismatch", i)
		assert.Greater(t, evt.Timestamp, int64(0), "timestamp should be positive")
	}
}

// TestIntegration_SecretMasking tests that secrets are masked in output.
func TestIntegration_SecretMasking(t *testing.T) {
	t.Setenv("TEST_SECRET_VALUE", "supersecret123")

	secrets := []SecretDef{{Name: "TEST_SECRET_VALUE", From: "env"}}
	masker := NewSecretMasker(secrets)

	input := "The password is supersecret123 and also supersecret123"
	masked := masker.Mask(input)

	assert.NotContains(t, masked, "supersecret123")
	assert.Contains(t, masked, "***")
	// Verify the non-secret parts are preserved.
	assert.Contains(t, masked, "The password is")
	assert.Contains(t, masked, "and also")
}

// TestIntegration_SecretMasking_InRunner verifies end-to-end secret masking
// through the runner pipeline.
func TestIntegration_SecretMasking_InRunner(t *testing.T) {
	skipIntegrationOnWindows(t)

	t.Setenv("MY_SECRET", "topsecretvalue42")

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		Secrets: []SecretDef{{Name: "MY_SECRET", From: "env"}},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		{
			Name:  "Leak secret",
			Run:   `echo "secret is $MY_SECRET"`,
			Shell: "true",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := runner.Run(ctx, steps)

	require.Len(t, result.Steps, 1)
	assert.Equal(t, "passed", result.Steps[0].Status)
	// The secret should be masked in captured stdout.
	assert.NotContains(t, result.Steps[0].StdoutTail, "topsecretvalue42")
	assert.Contains(t, result.Steps[0].StdoutTail, "***")
}

// TestIntegration_DisplayOutput tests both TTY and plain display modes
// capture expected output.
func TestIntegration_DisplayOutput(t *testing.T) {
	t.Run("plain mode", func(t *testing.T) {
		var buf bytes.Buffer
		d := NewDisplay(&buf, DisplayPlain)

		d.RunStart("test-workflow", "run-123")
		d.StepStart("build", "")
		d.StepEnd("build", "", "passed", 1230*time.Millisecond)
		d.RunEnd("passed", 1230*time.Millisecond, nil)

		output := buf.String()
		assert.Contains(t, output, "test-workflow")
		assert.Contains(t, output, "build")
		assert.Contains(t, output, "passed")
	})

	t.Run("TTY mode", func(t *testing.T) {
		var buf bytes.Buffer
		d := NewDisplay(&buf, DisplayTTY)

		d.RunStart("my-workflow", "run-456")
		d.StepStart("deploy", "")
		d.StepEnd("deploy", "", "passed", 2500*time.Millisecond)
		d.StepStart("verify", "")
		d.StepEnd("verify", "", "failed", 500*time.Millisecond)
		d.RunEnd("failed", 3000*time.Millisecond, []StepSummary{
			{Name: "deploy", Status: "passed", Duration: 2500 * time.Millisecond},
			{Name: "verify", Status: "failed", Duration: 500 * time.Millisecond},
		})

		output := buf.String()
		assert.Contains(t, output, "my-workflow")
		assert.Contains(t, output, "deploy")
		assert.Contains(t, output, "verify")
		assert.Contains(t, output, "FAILED")
	})

	t.Run("plain mode with step statuses", func(t *testing.T) {
		var buf bytes.Buffer
		d := NewDisplay(&buf, DisplayPlain)

		d.RunStart("status-test", "run-789")
		d.StepStart("step1", "")
		d.StepEnd("step1", "", "passed", 100*time.Millisecond)
		d.StepStart("step2", "")
		d.StepEnd("step2", "", "failed", 200*time.Millisecond)
		d.StepEnd("step3", "", "skipped", 0)
		d.RunEnd("failed", 300*time.Millisecond, []StepSummary{
			{Name: "step1", Status: "passed", Duration: 100 * time.Millisecond},
			{Name: "step2", Status: "failed", Duration: 200 * time.Millisecond},
			{Name: "step3", Status: "skipped"},
		})

		output := buf.String()
		assert.Contains(t, output, "status-test")
		assert.Contains(t, output, "step1")
		assert.Contains(t, output, "passed")
		assert.Contains(t, output, "failed")
	})
}

// TestIntegration_FullPipelineWithOutputFile exercises the complete pipeline
// including writing and reading CLAI_OUTPUT within a parsed workflow.
func TestIntegration_FullPipelineWithOutputFile(t *testing.T) {
	skipIntegrationOnWindows(t)

	yamlContent := `
name: output-pipeline
jobs:
  build:
    steps:
      - id: producer
        name: Produce outputs
        run: |
          echo "VERSION=1.2.3" > "$CLAI_OUTPUT"
          echo "STATUS=green" >> "$CLAI_OUTPUT"
        shell: true
      - id: consumer
        name: Consume outputs
        run: echo "Version ${{ steps.producer.outputs.VERSION }} is ${{ steps.producer.outputs.STATUS }}"
        shell: true
`

	wf, err := ParseWorkflow([]byte(yamlContent))
	require.NoError(t, err)

	verrs := ValidateWorkflow(wf)
	assert.Empty(t, verrs)

	job := wf.Jobs["build"]
	require.NotNil(t, job)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := runner.Run(ctx, job.Steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 2)

	// Verify outputs were parsed from the first step.
	assert.Equal(t, "1.2.3", result.Steps[0].Outputs["VERSION"])
	assert.Equal(t, "green", result.Steps[0].Outputs["STATUS"])

	// Verify expressions were resolved in the second step.
	assert.Contains(t, result.Steps[1].StdoutTail, "Version 1.2.3 is green")
}
