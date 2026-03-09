package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shellStep creates a StepDef that runs a command through the shell.
func shellStep(id, name, run string) *StepDef {
	return &StepDef{
		ID:    id,
		Name:  name,
		Run:   run,
		Shell: "true", // use default shell
	}
}

// shellStepWithEnv creates a StepDef with env vars.
func shellStepWithEnv(id, name, run string, env map[string]string) *StepDef {
	return &StepDef{
		ID:    id,
		Name:  name,
		Run:   run,
		Shell: "true",
		Env:   env,
	}
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell commands")
	}
}

func TestRunner_SingleStep_Success(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Echo hello", "echo hello"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	assert.Nil(t, result.Error)
	require.Len(t, result.Steps, 1)

	sr := result.Steps[0]
	assert.Equal(t, "step1", sr.StepID)
	assert.Equal(t, "passed", sr.Status)
	assert.Equal(t, 0, sr.ExitCode)
	assert.Contains(t, sr.StdoutTail, "hello")
	assert.True(t, result.DurationMs >= 0)
}

func TestRunner_SingleStep_Failure(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Exit with error", "exit 1"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "failed", result.Status)
	require.Len(t, result.Steps, 1)

	sr := result.Steps[0]
	assert.Equal(t, "failed", sr.Status)
	assert.Equal(t, 1, sr.ExitCode)
}

func TestRunner_MultipleSteps_Success(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Step 1", "echo step1"),
		shellStep("step2", "Step 2", "echo step2"),
		shellStep("step3", "Step 3", "echo step3"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 3)

	for i, sr := range result.Steps {
		assert.Equal(t, "passed", sr.Status, "step %d should pass", i)
		assert.Equal(t, 0, sr.ExitCode)
		assert.Contains(t, sr.StdoutTail, fmt.Sprintf("step%d", i+1))
	}
}

func TestRunner_FailureSkipsRemaining(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Step 1 passes", "echo ok"),
		shellStep("step2", "Step 2 fails", "exit 42"),
		shellStep("step3", "Step 3 skipped", "echo should-not-run"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "failed", result.Status)
	require.Len(t, result.Steps, 3)

	assert.Equal(t, "passed", result.Steps[0].Status)
	assert.Equal(t, "failed", result.Steps[1].Status)
	assert.Equal(t, 42, result.Steps[1].ExitCode)
	assert.Equal(t, "skipped", result.Steps[2].Status)
}

func TestRunner_ExpressionResolution(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		Env: map[string]string{
			"GREETING": "hello-from-expr",
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Use env expression", "echo ${{ env.GREETING }}"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, "passed", result.Steps[0].Status)
	// The expression should be resolved before the command is executed.
	assert.Contains(t, result.Steps[0].StdoutTail, "hello-from-expr")
}

func TestRunner_OutputParsing(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	// Step 1 writes to CLAI_OUTPUT, step 2 reads it via expression.
	steps := []*StepDef{
		shellStep("producer", "Produce output", `echo "RESULT=42" >> "$CLAI_OUTPUT"`),
		shellStep("consumer", "Consume output", `echo "got ${{ steps.producer.outputs.RESULT }}"`),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 2)

	// Step 1 should have parsed output.
	assert.Equal(t, "42", result.Steps[0].Outputs["RESULT"])

	// Step 2 should have resolved the expression.
	assert.Contains(t, result.Steps[1].StdoutTail, "got 42")
}

func TestRunner_ContextCancellation(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	steps := []*StepDef{
		shellStep("step1", "Long sleep", "sleep 60"),
		shellStep("step2", "Should be skipped", "echo never"),
	}

	// Cancel after a short delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result := runner.Run(ctx, steps)
	elapsed := time.Since(start)

	assert.Equal(t, "cancelled", result.Status)
	// Should complete well before the sleep would finish.
	assert.Less(t, elapsed, 30*time.Second)

	// All steps should have results.
	require.Len(t, result.Steps, 2)
	assert.Equal(t, "cancelled", result.Steps[0].Status)
	// Step 2 should be skipped (either because step 1 failed from cancellation
	// or because context was already done).
	assert.Equal(t, "skipped", result.Steps[1].Status)
}

func TestRunner_EnvPrecedence(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		Env: map[string]string{
			"VAR": "workflow-level",
		},
		JobEnv: map[string]string{
			"VAR": "job-level",
		},
	}
	runner := NewRunner(cfg)

	// Step env should override job env, which overrides workflow env.
	steps := []*StepDef{
		shellStepWithEnv("step1", "Step overrides", "echo $VAR", map[string]string{
			"VAR": "step-level",
		}),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Contains(t, result.Steps[0].StdoutTail, "step-level")
}

func TestRunner_EnvPrecedence_JobOverridesWorkflow(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		Env: map[string]string{
			"VAR": "workflow-level",
		},
		JobEnv: map[string]string{
			"VAR": "job-level",
		},
	}
	runner := NewRunner(cfg)

	// No step env — job should override workflow.
	steps := []*StepDef{
		shellStep("step1", "Job overrides workflow", "echo $VAR"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Contains(t, result.Steps[0].StdoutTail, "job-level")
}

func TestRunner_MatrixVars(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		MatrixVars: map[string]string{
			"version": "3.11",
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Use matrix var", "echo ${{ matrix.version }}"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Contains(t, result.Steps[0].StdoutTail, "3.11")
}

func TestRunner_StderrCapture(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Write to stderr", "echo error-output >&2"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Contains(t, result.Steps[0].StderrTail, "error-output")
}

func TestRunner_EmptySteps(t *testing.T) {
	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	result := runner.Run(context.Background(), []*StepDef{})

	assert.Equal(t, "passed", result.Status)
	assert.Empty(t, result.Steps)
}

func TestRunner_NonZeroExitCode(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Exit 2", "exit 2"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "failed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 2, result.Steps[0].ExitCode)
}

func TestRunner_MultipleOutputs(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("producer", "Write multiple outputs",
			`printf "KEY1=value1\nKEY2=value2\n" >> "$CLAI_OUTPUT"`),
		shellStep("consumer", "Read both outputs",
			`echo "${{ steps.producer.outputs.KEY1 }} ${{ steps.producer.outputs.KEY2 }}"`),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 2)
	assert.Equal(t, "value1", result.Steps[0].Outputs["KEY1"])
	assert.Equal(t, "value2", result.Steps[0].Outputs["KEY2"])
	assert.Contains(t, result.Steps[1].StdoutTail, "value1 value2")
}

func TestRunner_OutputEnvInheritedByDownstreamStep(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("producer", "Write output env", `echo "EXPORTED_TOKEN=abc123" >> "$CLAI_OUTPUT"`),
		shellStep("consumer", "Read inherited env", `echo "token=$EXPORTED_TOKEN"`),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 2)
	assert.Contains(t, result.Steps[1].StdoutTail, "token=abc123")
}

func TestRunner_OutputParseErrorIsNonFatal(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
	}
	runner := NewRunner(cfg)

	// Scanner default token limit is 64K, so this causes ParseOutputFile to error.
	steps := []*StepDef{
		shellStep("step1", "Write oversized output line", `awk 'BEGIN { printf("K=%070000d\n", 0) }' > "$CLAI_OUTPUT"`),
	}

	result := runner.Run(context.Background(), steps)
	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, "passed", result.Steps[0].Status)
	assert.Empty(t, result.Steps[0].Outputs)
}

func TestMergeEnv(t *testing.T) {
	wf := map[string]string{"A": "w", "B": "w"}
	job := map[string]string{"B": "j", "C": "j"}
	step := map[string]string{"C": "s", "D": "s"}
	matrix := map[string]string{"M": "m1"}

	env := mergeEnv(nil, wf, job, step, matrix, nil)

	// Convert to map for easier assertions.
	envMap := make(map[string]string)
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx >= 0 {
			envMap[kv[:idx]] = kv[idx+1:]
		}
	}

	// Check precedence: step > job > workflow.
	assert.Equal(t, "w", envMap["A"])  // only in workflow
	assert.Equal(t, "j", envMap["B"])  // job overrides workflow
	assert.Equal(t, "s", envMap["C"])  // step overrides job
	assert.Equal(t, "s", envMap["D"])  // only in step
	assert.Equal(t, "m1", envMap["M"]) // matrix

	// OS env should also be present.
	assert.NotEmpty(t, envMap["PATH"]) // PATH should come from os.Environ
}

func TestMergeEnv_VarOverridesWin(t *testing.T) {
	wf := map[string]string{"A": "workflow"}
	job := map[string]string{"A": "job"}
	step := map[string]string{"A": "step"}
	matrix := map[string]string{"A": "matrix"}
	varOverrides := map[string]string{"A": "cli-var"}

	env := mergeEnv(nil, wf, job, step, matrix, varOverrides)

	envMap := make(map[string]string)
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx >= 0 {
			envMap[kv[:idx]] = kv[idx+1:]
		}
	}

	assert.Equal(t, "cli-var", envMap["A"], "--var should override all other layers")
}

func TestExitCodeFromError(t *testing.T) {
	assert.Equal(t, 0, exitCodeFromError(nil))
	assert.Equal(t, 1, exitCodeFromError(fmt.Errorf("some error")))

	// Test with a real exec.ExitError if possible.
	if runtime.GOOS != "windows" {
		cmd := exec.Command("sh", "-c", "exit 42")
		err := cmd.Run()
		assert.Equal(t, 42, exitCodeFromError(err))
	}
}

func TestRunner_BufferSizeConfig(t *testing.T) {
	skipOnWindows(t)

	// Use a tiny buffer to verify truncation behavior.
	cfg := RunnerConfig{
		WorkDir:    t.TempDir(),
		BufferSize: 10,
	}
	runner := NewRunner(cfg)

	// Generate output longer than buffer size.
	steps := []*StepDef{
		shellStep("step1", "Long output", "echo abcdefghijklmnopqrstuvwxyz"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	// The buffer should only retain the last 10 bytes (including the newline from echo).
	assert.LessOrEqual(t, len(result.Steps[0].StdoutTail), 10)
}

func TestRunner_StepCallback(t *testing.T) {
	skipOnWindows(t)

	type callbackEvent struct {
		result *StepResult
		stepID string
		event  StepEvent
	}

	var events []callbackEvent

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		OnStep: func(event StepEvent, stepDef *StepDef, result *StepResult) error {
			events = append(events, callbackEvent{
				stepID: stepDef.ID,
				result: result,
				event:  event,
			})
			return nil
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Step 1", "echo one"),
		shellStep("step2", "Step 2", "echo two"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, events, 4) // 2 steps × (start + end)

	// Step 1 start.
	assert.Equal(t, StepEventStart, events[0].event)
	assert.Equal(t, "step1", events[0].stepID)
	assert.Nil(t, events[0].result)

	// Step 1 end.
	assert.Equal(t, StepEventEnd, events[1].event)
	assert.Equal(t, "step1", events[1].stepID)
	require.NotNil(t, events[1].result)
	assert.Equal(t, "passed", events[1].result.Status)

	// Step 2 start.
	assert.Equal(t, StepEventStart, events[2].event)
	assert.Equal(t, "step2", events[2].stepID)

	// Step 2 end.
	assert.Equal(t, StepEventEnd, events[3].event)
	assert.Equal(t, "step2", events[3].stepID)
	require.NotNil(t, events[3].result)
	assert.Equal(t, "passed", events[3].result.Status)
}

func TestRunner_StepCallback_SkippedStepsNoCallback(t *testing.T) {
	skipOnWindows(t)

	var callbackStepIDs []string

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		OnStep: func(event StepEvent, stepDef *StepDef, _ *StepResult) error {
			callbackStepIDs = append(callbackStepIDs, fmt.Sprintf("%s:%d", stepDef.ID, event))
			return nil
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Fails", "exit 1"),
		shellStep("step2", "Skipped", "echo never"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "failed", result.Status)
	// Only step1 should get callbacks (start + end). Step2 is skipped, no callbacks.
	assert.Equal(t, []string{"step1:0", "step1:1"}, callbackStepIDs)
}

func TestRunner_RiskLevelExpressionResolution(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		MatrixVars: map[string]string{
			"risk": "high",
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		{
			ID:             "step1",
			Name:           "Check ${{ matrix.risk }}",
			Run:            "echo ok",
			Shell:          "true",
			RiskLevel:      "${{ matrix.risk }}",
			AnalysisPrompt: "Check ${{ matrix.risk }} environment",
		},
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, "high", result.Steps[0].RiskLevel)
	assert.Equal(t, "Check high environment", result.Steps[0].AnalysisPrompt)
}

func TestRunner_StepCallback_ErrorHaltsExecution(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		OnStep: func(event StepEvent, stepDef *StepDef, _ *StepResult) error {
			if event == StepEventEnd && stepDef.ID == "step1" {
				return fmt.Errorf("analysis rejected step1")
			}
			return nil
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Step 1", "echo one"),
		shellStep("step2", "Step 2", "echo two"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Error.Error(), "analysis rejected step1")
	require.Len(t, result.Steps, 2)
	assert.Equal(t, "passed", result.Steps[0].Status)
	assert.Equal(t, "skipped", result.Steps[1].Status)
}

func TestRunner_VarOverridesMatrix(t *testing.T) {
	skipOnWindows(t)

	cfg := RunnerConfig{
		WorkDir: t.TempDir(),
		Env:     map[string]string{"VAR": "workflow"},
		JobEnv:  map[string]string{"VAR": "job"},
		MatrixVars: map[string]string{
			"VAR": "matrix",
		},
		VarOverrides: map[string]string{
			"VAR": "cli-override",
		},
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Check var precedence", "echo $VAR"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Contains(t, result.Steps[0].StdoutTail, "cli-override",
		"--var should override matrix, job, and workflow env")
}

func TestRunner_WorkDir(t *testing.T) {
	skipOnWindows(t)

	workDir := t.TempDir()

	// Create a file in the work directory.
	err := os.WriteFile(workDir+"/testfile.txt", []byte("found-it"), 0644)
	require.NoError(t, err)

	cfg := RunnerConfig{
		WorkDir: workDir,
	}
	runner := NewRunner(cfg)

	steps := []*StepDef{
		shellStep("step1", "Read file in workdir", "cat testfile.txt"),
	}

	result := runner.Run(context.Background(), steps)

	assert.Equal(t, "passed", result.Status)
	require.Len(t, result.Steps, 1)
	assert.Contains(t, result.Steps[0].StdoutTail, "found-it")
}
