package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/workflow"
)

// validWorkflowYAML is a minimal valid workflow for testing.
const validWorkflowYAML = `name: test-workflow
description: A test workflow for unit tests
jobs:
  build:
    steps:
      - id: run-tests
        name: Run tests
        run: echo hello
`

// invalidWorkflowYAML is missing the required name field.
const invalidWorkflowYAML = `jobs:
  build:
    steps:
      - id: run-tests
        name: Run tests
        run: echo hello
`

// multiJobWorkflowYAML has multiple jobs for step counting.
const multiJobWorkflowYAML = `name: multi-job
jobs:
  build:
    steps:
      - id: step-1
        name: Step 1
        run: echo one
      - id: step-2
        name: Step 2
        run: echo two
  test:
    steps:
      - id: step-3
        name: Step 3
        run: echo three
`

func TestValidateWorkflow_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validWorkflowYAML), 0644))

	cmd := &cobra.Command{Use: "validate", RunE: validateWorkflow, Args: cobra.ExactArgs(1)}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{path})

	// Redirect stdout to capture output.
	out := captureStdout(t, func() {
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "test-workflow is valid")
	assert.Contains(t, out, "1 jobs")
	assert.Contains(t, out, "1 total steps")
	assert.Contains(t, out, "A test workflow for unit tests")
}

func TestValidateWorkflow_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	require.NoError(t, os.WriteFile(path, []byte(invalidWorkflowYAML), 0644))

	cmd := &cobra.Command{Use: "validate", RunE: validateWorkflow, Args: cobra.ExactArgs(1)}
	cmd.SetArgs([]string{path})

	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidateWorkflow_FileNotFound(t *testing.T) {
	cmd := &cobra.Command{Use: "validate", RunE: validateWorkflow, Args: cobra.ExactArgs(1)}
	cmd.SetArgs([]string{"/nonexistent/path/workflow.yaml"})

	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading workflow file")
}

func TestValidateWorkflow_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not: [valid: yaml: content"), 0644))

	cmd := &cobra.Command{Use: "validate", RunE: validateWorkflow, Args: cobra.ExactArgs(1)}
	cmd.SetArgs([]string{path})

	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing workflow")
}

func TestValidateWorkflow_FromStdin(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)

	_, _ = w.WriteString(validWorkflowYAML)
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	cmd := &cobra.Command{Use: "validate", RunE: validateWorkflow, Args: cobra.ExactArgs(1)}
	cmd.SetArgs([]string{"-"})

	out := captureStdout(t, func() {
		execErr := cmd.Execute()
		assert.NoError(t, execErr)
	})

	assert.Contains(t, out, "test-workflow is valid")
}

func TestWorkflowExitError_Error(t *testing.T) {
	err := &WorkflowExitError{Code: ExitValidationError, Message: "boom"}
	assert.Equal(t, "boom", err.Error())
}

func TestLoadWorkflow_SuccessFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validWorkflowYAML), 0644))

	def, data, err := loadWorkflow(path)
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, "test-workflow", def.Name)
	assert.NotEmpty(t, data)
}

func TestLoadWorkflow_SuccessFromStdin(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)

	_, _ = w.WriteString(validWorkflowYAML)
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	def, data, loadErr := loadWorkflow("-")
	require.NoError(t, loadErr)
	require.NotNil(t, def)
	assert.Equal(t, "test-workflow", def.Name)
	assert.NotEmpty(t, data)
}

func TestLoadWorkflow_ValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	require.NoError(t, os.WriteFile(path, []byte(invalidWorkflowYAML), 0644))

	_, _, err := loadWorkflow(path)
	require.Error(t, err)

	var exitErr *WorkflowExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitValidationError, exitErr.Code)
}

func TestSetupRunContext_NoDaemon(t *testing.T) {
	def, err := workflow.ParseWorkflow([]byte(validWorkflowYAML))
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().Bool("no-daemon", true, "")
	cmd.Flags().String("mode", "unattended", "")
	require.NoError(t, cmd.Flags().Set("no-daemon", "true"))
	require.NoError(t, cmd.Flags().Set("mode", "unattended"))

	rc, cancel, err := setupRunContext(cmd, def, []byte(validWorkflowYAML), "workflow.yaml")
	require.NoError(t, err)
	require.NotNil(t, cancel)
	defer cancel()
	require.NotNil(t, rc)
	assert.True(t, rc.noDaemon)
	assert.NotEmpty(t, rc.runID)
	assert.Equal(t, ".", rc.workDir)

	_, isNonInteractive := rc.handler.(*workflow.NonInteractiveHandler)
	assert.True(t, isNonInteractive)
}

func TestParseVarFlags(t *testing.T) {
	tests := []struct {
		expect map[string]string
		name   string
		input  []string
	}{
		{
			name:   "empty",
			input:  nil,
			expect: map[string]string{},
		},
		{
			name:   "single var",
			input:  []string{"FOO=bar"},
			expect: map[string]string{"FOO": "bar"},
		},
		{
			name:   "multiple vars",
			input:  []string{"FOO=bar", "BAZ=qux"},
			expect: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:   "value with equals sign",
			input:  []string{"URL=http://example.com?a=1"},
			expect: map[string]string{"URL": "http://example.com?a=1"},
		},
		{
			name:   "no equals sign is ignored",
			input:  []string{"NOEQUALS"},
			expect: map[string]string{},
		},
		{
			name:   "empty value",
			input:  []string{"EMPTY="},
			expect: map[string]string{"EMPTY": ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseVarFlags(tc.input)
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestRunWorkflow_Success(t *testing.T) {
	t.Setenv("CLAI_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))

	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validWorkflowYAML), 0644))

	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().String("mode", "auto", "")
	cmd.Flags().StringSlice("var", nil, "")
	cmd.Flags().Bool("no-daemon", false, "")
	require.NoError(t, cmd.Flags().Set("mode", "unattended"))

	err := runWorkflow(cmd, []string{path})
	require.NoError(t, err)
}

func TestExecuteJob_InvalidLayout(t *testing.T) {
	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().StringSlice("var", nil, "")

	rc := &workflowRunContext{
		runID:    "run-test",
		workDir:  ".",
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}
	def := &workflow.WorkflowDef{Name: "broken", Jobs: map[string]*workflow.JobDef{}}

	result := executeJob(cmd, rc, def)
	require.NotNil(t, result)
	assert.True(t, result.validationErr)
	assert.Equal(t, string(workflow.RunFailed), result.overallStatus)
}

func TestHandleAnalysis_Reject(t *testing.T) {
	t.Setenv("CLAI_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))

	rc := &workflowRunContext{
		runID:    "run-test",
		workDir:  ".",
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
		handler: workflow.NewScriptedHandler(&workflow.ReviewDecision{
			Action: string(workflow.ActionReject),
		}),
		transport: workflow.NewAnalysisTransport(
			workflow.NewAnalyzer(nil),
			func(_ context.Context, _ string) (string, error) {
				return `{"decision":"halt","reasoning":"needs review"}`, nil
			},
		),
	}

	sr := &workflow.StepResult{
		StepID:     "s1",
		Name:       "Run tests",
		StdoutTail: "output",
		StderrTail: "",
		RiskLevel:  string(workflow.RiskLow),
	}

	rejected := rc.handleAnalysis(context.Background(), sr, "os=linux")
	assert.True(t, rejected)
}

func TestHandleAnalysis_QuestionThenApprove(t *testing.T) {
	t.Setenv("CLAI_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))

	calls := 0
	var prompts []string
	transport := workflow.NewAnalysisTransport(
		workflow.NewAnalyzer(nil),
		func(_ context.Context, prompt string) (string, error) {
			calls++
			prompts = append(prompts, prompt)
			return `{"decision":"halt","reasoning":"more context needed"}`, nil
		},
	)

	rc := &workflowRunContext{
		runID:     "run-test",
		workDir:   ".",
		display:   workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon:  true,
		transport: transport,
		handler: workflow.NewScriptedHandler(
			&workflow.ReviewDecision{Action: string(workflow.ActionQuestion), Input: "what failed?"},
			&workflow.ReviewDecision{Action: string(workflow.ActionApprove)},
		),
	}

	sr := &workflow.StepResult{
		StepID:         "s1",
		Name:           "Analyze",
		StdoutTail:     "stdout tail",
		StderrTail:     "stderr tail",
		RiskLevel:      string(workflow.RiskHigh),
		AnalysisPrompt: "Look for regressions.",
	}

	rejected := rc.handleAnalysis(context.Background(), sr, "")
	assert.False(t, rejected)
	assert.Equal(t, 2, calls)
	require.Len(t, prompts, 2)
	assert.Contains(t, prompts[1], "Follow-up question from reviewer: what failed?")
}

func TestRunAdHocCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	t.Run("success", func(t *testing.T) {
		err := runAdHocCommand(ctx, "echo ok", t.TempDir(), nil)
		assert.NoError(t, err)
	})

	t.Run("failure", func(t *testing.T) {
		var command string
		if runtime.GOOS == "windows" {
			command = "nonexistent_command_12345"
		} else {
			command = "command_that_does_not_exist_12345"
		}

		err := runAdHocCommand(ctx, command, t.TempDir(), nil)
		assert.Error(t, err)
	})
}

func TestMergeCommandEnv(t *testing.T) {
	base := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/tmp/home",
		"DUP=base",
	}
	overrides := []string{
		"DUP=override",
		"CUSTOM=value",
	}

	merged := mergeCommandEnv(base, overrides)
	envMap := make(map[string]string, len(merged))
	for _, entry := range merged {
		key, value, hasValue := strings.Cut(entry, "=")
		if !hasValue {
			t.Fatalf("merged env entry is malformed: %q", entry)
		}
		envMap[key] = value
	}

	assert.Equal(t, "/usr/bin:/bin", envMap["PATH"])
	assert.Equal(t, "/tmp/home", envMap["HOME"])
	assert.Equal(t, "override", envMap["DUP"])
	assert.Equal(t, "value", envMap["CUSTOM"])
}

func TestExpandMatrix_NoMatrix(t *testing.T) {
	job := &workflow.JobDef{
		Steps: []*workflow.StepDef{{Name: "test", Run: "echo hi"}},
	}

	result := expandMatrix(job)
	assert.Len(t, result, 1)
	assert.Empty(t, result[0])
}

func TestExpandMatrix_NilStrategy(t *testing.T) {
	job := &workflow.JobDef{
		Strategy: nil,
		Steps:    []*workflow.StepDef{{Name: "test", Run: "echo hi"}},
	}

	result := expandMatrix(job)
	assert.Len(t, result, 1)
	assert.Empty(t, result[0])
}

func TestExpandMatrix_EmptyMatrix(t *testing.T) {
	job := &workflow.JobDef{
		Strategy: &workflow.StrategyDef{
			Matrix: &workflow.MatrixDef{
				Include: []map[string]string{},
			},
		},
		Steps: []*workflow.StepDef{{Name: "test", Run: "echo hi"}},
	}

	result := expandMatrix(job)
	assert.Len(t, result, 1)
	assert.Empty(t, result[0])
}

func TestExpandMatrix_WithMatrix(t *testing.T) {
	job := &workflow.JobDef{
		Strategy: &workflow.StrategyDef{
			Matrix: &workflow.MatrixDef{
				Include: []map[string]string{
					{"os": "linux", "go": "1.21"},
					{"os": "darwin", "go": "1.22"},
				},
			},
		},
		Steps: []*workflow.StepDef{{Name: "test", Run: "echo hi"}},
	}

	result := expandMatrix(job)
	assert.Len(t, result, 2)
	assert.Equal(t, "linux", result[0]["os"])
	assert.Equal(t, "1.21", result[0]["go"])
	assert.Equal(t, "darwin", result[1]["os"])
	assert.Equal(t, "1.22", result[1]["go"])
}

func TestMatrixKeyString(t *testing.T) {
	tests := []struct {
		name   string
		vars   map[string]string
		expect string
	}{
		{
			name:   "empty",
			vars:   map[string]string{},
			expect: "",
		},
		{
			name:   "single var",
			vars:   map[string]string{"os": "linux"},
			expect: "os=linux",
		},
		{
			name:   "multiple vars sorted",
			vars:   map[string]string{"os": "linux", "go": "1.21"},
			expect: "go=1.21,os=linux",
		},
		{
			name:   "nil map",
			vars:   nil,
			expect: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := matrixKeyString(tc.vars)
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestSelectInteractionHandler(t *testing.T) {
	t.Run("unattended returns NonInteractiveHandler", func(t *testing.T) {
		h := selectInteractionHandler("unattended", workflow.DisplayTTY)
		_, ok := h.(*workflow.NonInteractiveHandler)
		assert.True(t, ok)
	})

	t.Run("attended returns TerminalReviewer", func(t *testing.T) {
		h := selectInteractionHandler("attended", workflow.DisplayPlain)
		_, ok := h.(*workflow.TerminalReviewer)
		assert.True(t, ok)
	})

	t.Run("auto with TTY returns TerminalReviewer", func(t *testing.T) {
		h := selectInteractionHandler("auto", workflow.DisplayTTY)
		_, ok := h.(*workflow.TerminalReviewer)
		assert.True(t, ok)
	})

	t.Run("auto with plain returns NonInteractiveHandler", func(t *testing.T) {
		h := selectInteractionHandler("auto", workflow.DisplayPlain)
		_, ok := h.(*workflow.NonInteractiveHandler)
		assert.True(t, ok)
	})

	t.Run("empty string defaults to auto", func(t *testing.T) {
		h := selectInteractionHandler("", workflow.DisplayPlain)
		_, ok := h.(*workflow.NonInteractiveHandler)
		assert.True(t, ok)
	})
}

func TestGenerateRunID(t *testing.T) {
	id := generateRunID()
	assert.True(t, strings.HasPrefix(id, "run-"), "run ID should start with 'run-', got %q", id)
	assert.Greater(t, len(id), 4, "run ID should have more than just the prefix")

	// Generate two IDs to verify they are unique.
	id2 := generateRunID()
	assert.NotEqual(t, id, id2, "consecutive run IDs should differ")
}

func TestMergeMaps(t *testing.T) {
	t.Run("nil maps", func(t *testing.T) {
		result := mergeMaps(nil, nil)
		assert.Empty(t, result)
	})

	t.Run("override takes precedence", func(t *testing.T) {
		base := map[string]string{"A": "1", "B": "2"}
		override := map[string]string{"B": "3", "C": "4"}
		result := mergeMaps(base, override)
		assert.Equal(t, map[string]string{"A": "1", "B": "3", "C": "4"}, result)
	})

	t.Run("empty override", func(t *testing.T) {
		base := map[string]string{"A": "1"}
		result := mergeMaps(base, nil)
		assert.Equal(t, map[string]string{"A": "1"}, result)
	})
}

func TestFindStepDef(t *testing.T) {
	steps := []*workflow.StepDef{
		{ID: "s1", Name: "Step 1", Run: "echo one"},
		{ID: "s2", Name: "Step 2", Run: "echo two"},
		{ID: "s3", Name: "Step 3", Run: "echo three"},
	}

	t.Run("found", func(t *testing.T) {
		s := findStepDef(steps, "s2")
		require.NotNil(t, s)
		assert.Equal(t, "Step 2", s.Name)
	})

	t.Run("not found", func(t *testing.T) {
		s := findStepDef(steps, "s99")
		assert.Nil(t, s)
	})

}

func TestCountSteps(t *testing.T) {
	t.Run("single job", func(t *testing.T) {
		def, err := workflow.ParseWorkflow([]byte(validWorkflowYAML))
		require.NoError(t, err)
		assert.Equal(t, 1, countSteps(def))
	})

	t.Run("multi job", func(t *testing.T) {
		def, err := workflow.ParseWorkflow([]byte(multiJobWorkflowYAML))
		require.NoError(t, err)
		assert.Equal(t, 3, countSteps(def))
	})
}

func TestGetSingleJob(t *testing.T) {
	def, err := workflow.ParseWorkflow([]byte(validWorkflowYAML))
	require.NoError(t, err)
	job, err := getSingleJob(def)
	require.NoError(t, err)
	require.NotNil(t, job)
}

func TestGetSingleJob_MultipleJobsError(t *testing.T) {
	def, err := workflow.ParseWorkflow([]byte(multiJobWorkflowYAML))
	require.NoError(t, err)
	_, err = getSingleJob(def)
	require.Error(t, err)
}

func TestReportResults_CancelledExitCode(t *testing.T) {
	rc := &workflowRunContext{
		runID:    "run-test",
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}
	result := &jobExecutionResult{
		overallStatus: string(workflow.RunCancelled),
		cancelled:     true,
	}

	err := reportResults(rc, result)
	require.Error(t, err)
	var exitErr *WorkflowExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitCancelled, exitErr.Code)
}

func TestReportResults_HumanRejectedExitCode(t *testing.T) {
	rc := &workflowRunContext{
		runID:    "run-test",
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}
	result := &jobExecutionResult{
		overallStatus: string(workflow.RunFailed),
		humanRejected: true,
	}

	err := reportResults(rc, result)
	require.Error(t, err)
	var exitErr *WorkflowExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitHumanReject, exitErr.Code)
}

func TestReportResults_ValidationExitCode(t *testing.T) {
	rc := &workflowRunContext{
		runID:    "run-test",
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}
	result := &jobExecutionResult{
		overallStatus: string(workflow.RunFailed),
		validationErr: true,
	}

	err := reportResults(rc, result)
	require.Error(t, err)
	var exitErr *WorkflowExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitValidationError, exitErr.Code)
}

func TestReportResults_Success(t *testing.T) {
	rc := &workflowRunContext{
		runID:    "run-test",
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}
	result := &jobExecutionResult{
		overallStatus: string(workflow.RunPassed),
		allStepResults: []*workflow.StepResult{
			{Name: "step-1", Status: string(workflow.StepPassed), DurationMs: 1},
		},
	}

	err := reportResults(rc, result)
	assert.NoError(t, err)
}

func TestWorkflowCmd_HasSubcommands(t *testing.T) {
	subCmds := make(map[string]bool)
	for _, cmd := range workflowCmd.Commands() {
		subCmds[cmd.Name()] = true
	}

	assert.True(t, subCmds["run"], "workflow should have 'run' subcommand")
	assert.True(t, subCmds["validate"], "workflow should have 'validate' subcommand")
}

func TestWorkflowCmd_IsRegistered(t *testing.T) {
	var found *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "workflow" {
			found = cmd
			break
		}
	}
	require.NotNil(t, found, "workflow command should be registered on root")
	assert.Equal(t, groupCore, found.GroupID)
}

func TestWorkflowRunCmd_Flags(t *testing.T) {
	f := workflowRunCmd.Flags()

	modeFlag := f.Lookup("mode")
	require.NotNil(t, modeFlag)
	assert.Equal(t, "auto", modeFlag.DefValue)

	varFlag := f.Lookup("var")
	require.NotNil(t, varFlag)

	noDaemonFlag := f.Lookup("no-daemon")
	require.NotNil(t, noDaemonFlag)
	assert.Equal(t, "false", noDaemonFlag.DefValue)
}

func TestExecuteJob_FailFastTrue_StopsOnFirstFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell commands")
	}

	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().StringSlice("var", nil, "")

	rc := &workflowRunContext{
		runID:    "run-test",
		workDir:  t.TempDir(),
		ctx:      context.Background(),
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}

	failFast := true
	def := &workflow.WorkflowDef{
		Name: "fail-fast-test",
		Jobs: map[string]*workflow.JobDef{
			"test": {
				Strategy: &workflow.StrategyDef{
					FailFast: &failFast,
					Matrix: &workflow.MatrixDef{
						Include: []map[string]string{
							{"val": "first"},
							{"val": "second"},
							{"val": "third"},
						},
					},
				},
				Steps: []*workflow.StepDef{
					{ID: "s1", Name: "Fail step", Run: "exit 1", Shell: "true"},
				},
			},
		},
	}

	result := executeJob(cmd, rc, def)
	assert.Equal(t, string(workflow.RunFailed), result.overallStatus)
	// With fail_fast: true, should stop after first combination fails.
	assert.Len(t, result.allStepResults, 1)
}

func TestExecuteJob_FailFastFalse_RunsAllCombinations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell commands")
	}

	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().StringSlice("var", nil, "")

	rc := &workflowRunContext{
		runID:    "run-test",
		workDir:  t.TempDir(),
		ctx:      context.Background(),
		display:  workflow.NewDisplay(new(bytes.Buffer), workflow.DisplayPlain),
		noDaemon: true,
	}

	failFast := false
	def := &workflow.WorkflowDef{
		Name: "no-fail-fast-test",
		Jobs: map[string]*workflow.JobDef{
			"test": {
				Strategy: &workflow.StrategyDef{
					FailFast: &failFast,
					Matrix: &workflow.MatrixDef{
						Include: []map[string]string{
							{"val": "first"},
							{"val": "second"},
							{"val": "third"},
						},
					},
				},
				Steps: []*workflow.StepDef{
					{ID: "s1", Name: "Fail step", Run: "exit 1", Shell: "true"},
				},
			},
		},
	}

	result := executeJob(cmd, rc, def)
	assert.Equal(t, string(workflow.RunFailed), result.overallStatus)
	// With fail_fast: false, all 3 combinations should run.
	assert.Len(t, result.allStepResults, 3)
}

func TestCheckRequires_AllPresent(t *testing.T) {
	tools := []string{"sh"}
	if runtime.GOOS == "windows" {
		tools = []string{"cmd"}
	}
	err := checkRequires(tools)
	assert.NoError(t, err)
}

func TestCheckRequires_MissingTool(t *testing.T) {
	err := checkRequires([]string{"echo", "nonexistent_tool_xyz_12345"})
	require.Error(t, err)

	var exitErr *WorkflowExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitDependencyMissing, exitErr.Code)
	assert.Contains(t, exitErr.Message, "nonexistent_tool_xyz_12345")
}

func TestCheckRequires_Empty(t *testing.T) {
	err := checkRequires(nil)
	assert.NoError(t, err)
}

func TestWorkflowRunCmd_RequiresArg(t *testing.T) {
	cmd := &cobra.Command{Use: "run", RunE: runWorkflow, Args: cobra.ExactArgs(1)}
	cmd.Flags().String("mode", "auto", "")
	cmd.Flags().StringSlice("var", nil, "")
	cmd.Flags().Bool("no-daemon", false, "")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	assert.Error(t, err, "run without args should error")
}
