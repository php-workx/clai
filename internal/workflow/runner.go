package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultBufferSize is the default limitedBuffer capacity (4KB).
const DefaultBufferSize = 4096

// StepResult holds the outcome of a single step execution.
type StepResult struct {
	StepID     string
	Name       string
	Status     string // "passed", "failed", "skipped"
	ExitCode   int
	DurationMs int64
	StdoutTail string
	StderrTail string
	Outputs    map[string]string // from $CLAI_OUTPUT file
	Error      error
}

// RunResult holds the outcome of a complete job run.
type RunResult struct {
	Status     string // "passed", "failed"
	Steps      []*StepResult
	DurationMs int64
	Error      error
}

// RunnerConfig configures the runner.
type RunnerConfig struct {
	WorkDir    string
	Env        map[string]string // workflow-level env
	JobEnv     map[string]string // job-level env
	MatrixVars map[string]string // matrix combination
	Secrets    []SecretDef
	BufferSize int // 0 = DefaultBufferSize
}

// Runner executes a job's steps sequentially.
type Runner struct {
	shell   ShellAdapter
	process ProcessController
	masker  *SecretMasker
	config  RunnerConfig
}

// NewRunner creates a runner with the given config.
func NewRunner(cfg RunnerConfig) *Runner {
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = DefaultBufferSize
	}
	cfg.BufferSize = bufSize

	return &Runner{
		shell:   NewShellAdapter(),
		process: NewProcessController(),
		masker:  NewSecretMasker(cfg.Secrets),
		config:  cfg,
	}
}

// Run executes all steps in sequence. Returns RunResult.
// If any step fails (non-zero exit), remaining steps are skipped.
// Uses context for Ctrl+C cancellation.
func (r *Runner) Run(ctx context.Context, steps []*StepDef) *RunResult {
	runStart := time.Now()
	result := &RunResult{
		Status: "passed",
		Steps:  make([]*StepResult, 0, len(steps)),
	}

	// Track step outputs for expression resolution.
	stepOutputs := make(map[string]map[string]string)
	failed := false

	for i, step := range steps {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			// Mark remaining steps as skipped.
			for j := i; j < len(steps); j++ {
				result.Steps = append(result.Steps, &StepResult{
					StepID: steps[j].ID,
					Name:   steps[j].Name,
					Status: "skipped",
				})
			}
			result.Status = "failed"
			result.DurationMs = time.Since(runStart).Milliseconds()
			result.Error = ctx.Err()
			return result
		default:
		}

		// Skip remaining steps if a previous step failed.
		if failed {
			result.Steps = append(result.Steps, &StepResult{
				StepID: step.ID,
				Name:   step.Name,
				Status: "skipped",
			})
			continue
		}

		stepResult := r.executeStep(ctx, step, stepOutputs)
		result.Steps = append(result.Steps, stepResult)

		// Store outputs for expression resolution in subsequent steps.
		if step.ID != "" && stepResult.Outputs != nil {
			stepOutputs[step.ID] = stepResult.Outputs
		}

		if stepResult.Status == "failed" {
			failed = true
			result.Status = "failed"
		}
	}

	result.DurationMs = time.Since(runStart).Milliseconds()
	return result
}

// executeStep runs a single step and returns the result.
func (r *Runner) executeStep(ctx context.Context, step *StepDef, stepOutputs map[string]map[string]string) *StepResult {
	stepStart := time.Now()
	sr := &StepResult{
		StepID:  step.ID,
		Name:    step.Name,
		Outputs: map[string]string{},
	}

	// Create temp file for CLAI_OUTPUT.
	outputFile, err := os.CreateTemp("", "clai-output-*")
	if err != nil {
		sr.Status = "failed"
		sr.ExitCode = 1
		sr.Error = fmt.Errorf("creating output temp file: %w", err)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		return sr
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	// Build expression context for resolving ${{ }} expressions.
	exprCtx := &ExpressionContext{
		Env:    r.buildExprEnv(step),
		Matrix: r.config.MatrixVars,
		Steps:  stepOutputs,
	}

	// Resolve expressions in the run command.
	resolvedRun, err := ResolveExpressions(step.Run, exprCtx)
	if err != nil {
		sr.Status = "failed"
		sr.ExitCode = 1
		sr.Error = fmt.Errorf("resolving expressions in run: %w", err)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		return sr
	}

	// Resolve expressions in step env values.
	resolvedStepEnv := make(map[string]string, len(step.Env))
	for k, v := range step.Env {
		resolved, resolveErr := ResolveExpressions(v, exprCtx)
		if resolveErr != nil {
			sr.Status = "failed"
			sr.ExitCode = 1
			sr.Error = fmt.Errorf("resolving expressions in env %s: %w", k, resolveErr)
			sr.DurationMs = time.Since(stepStart).Milliseconds()
			return sr
		}
		resolvedStepEnv[k] = resolved
	}

	// Resolve expressions in the step name (for display).
	resolvedName, err := ResolveExpressions(step.Name, exprCtx)
	if err == nil {
		sr.Name = resolvedName
	}

	// Create a modified step with the resolved command.
	resolvedStep := *step
	resolvedStep.Run = resolvedRun

	// Merge environment: workflow -> job -> step precedence.
	env := mergeEnv(r.config.Env, r.config.JobEnv, resolvedStepEnv, r.config.MatrixVars)

	// Build command via ShellAdapter.
	cmd, err := r.shell.BuildCommand(ctx, &resolvedStep, r.config.WorkDir, env, outputPath)
	if err != nil {
		sr.Status = "failed"
		sr.ExitCode = 1
		sr.Error = fmt.Errorf("building command: %w", err)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		return sr
	}

	// Create limited buffers for stdout/stderr capture.
	stdoutBuf := NewLimitedBuffer(r.config.BufferSize)
	stderrBuf := NewLimitedBuffer(r.config.BufferSize)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	// Start the process.
	if err := r.process.Start(cmd); err != nil {
		sr.Status = "failed"
		sr.ExitCode = 1
		sr.Error = fmt.Errorf("starting process: %w", err)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		return sr
	}

	// Wait for completion with context cancellation support.
	waitErr := r.process.Wait(ctx, cmd, DefaultGracePeriod)

	sr.DurationMs = time.Since(stepStart).Milliseconds()

	// Capture stdout/stderr tails, masking secrets.
	sr.StdoutTail = r.masker.Mask(stdoutBuf.String())
	sr.StderrTail = r.masker.Mask(stderrBuf.String())

	// Parse the CLAI_OUTPUT file for step outputs.
	outputs, parseErr := ParseOutputFile(outputPath)
	if parseErr != nil {
		sr.Status = "failed"
		sr.ExitCode = 1
		sr.Error = fmt.Errorf("parsing output file: %w", parseErr)
		return sr
	}
	sr.Outputs = outputs

	// Determine exit code and status.
	if waitErr != nil {
		sr.ExitCode = exitCodeFromError(waitErr)
		sr.Status = "failed"
		sr.Error = waitErr
	} else {
		sr.ExitCode = 0
		sr.Status = "passed"
	}

	return sr
}

// buildExprEnv builds the env map for expression resolution.
// This includes all environment layers (workflow, job, step) merged with proper precedence.
func (r *Runner) buildExprEnv(step *StepDef) map[string]string {
	env := make(map[string]string)

	// Workflow env.
	for k, v := range r.config.Env {
		env[k] = v
	}
	// Job env overrides workflow.
	for k, v := range r.config.JobEnv {
		env[k] = v
	}
	// Step env overrides job.
	for k, v := range step.Env {
		env[k] = v
	}
	// Matrix vars.
	for k, v := range r.config.MatrixVars {
		env[k] = v
	}

	return env
}

// mergeEnv merges environment variables with proper precedence:
// OS env < workflow < job < step. Matrix vars are also included.
// Returns a []string in "KEY=value" format suitable for exec.Cmd.Env.
func mergeEnv(workflow, job, step map[string]string, matrix map[string]string) []string {
	merged := make(map[string]string)

	// Start with OS environment.
	for _, kv := range os.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx >= 0 {
			merged[kv[:idx]] = kv[idx+1:]
		}
	}

	// Layer workflow env.
	for k, v := range workflow {
		merged[k] = v
	}
	// Layer job env.
	for k, v := range job {
		merged[k] = v
	}
	// Layer step env.
	for k, v := range step {
		merged[k] = v
	}
	// Add matrix vars as environment variables.
	for k, v := range matrix {
		merged[k] = v
	}

	// Convert to []string.
	result := make([]string, 0, len(merged))
	for k, v := range merged {
		result = append(result, k+"="+v)
	}
	return result
}

// exitCodeFromError extracts the exit code from an exec error.
func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
