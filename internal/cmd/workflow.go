package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/ipc"
	"github.com/runger/clai/internal/workflow"
)

// Workflow exit codes per spec SS15.
const (
	ExitSuccess           = 0
	ExitStepFailed        = 1
	ExitValidationError   = 2
	ExitHumanReject       = 3
	ExitCancelled         = 4
	ExitNeedsHuman        = 5
	ExitDaemonUnavailable = 6 //nolint:unused // reserved for future use
	ExitPolicyHalt        = 7 //nolint:unused // reserved for future use
	ExitDependencyMissing = 8 //nolint:unused // reserved for future use
	ExitTimeout           = 124
)

// WorkflowExitError is an error that carries a specific exit code.
// cobra.RunE returns this so the caller can set the process exit code.
type WorkflowExitError struct {
	Code    int
	Message string
}

func (e *WorkflowExitError) Error() string {
	return e.Message
}

var workflowCmd = &cobra.Command{
	Use:     "workflow",
	Aliases: []string{"w"},
	Short:   "Run and validate workflow files",
	GroupID: groupCore,
}

var workflowRunCmd = &cobra.Command{
	Use:          "run <path>",
	Short:        "Execute a workflow file",
	Args:         cobra.ExactArgs(1),
	RunE:         runWorkflow,
	SilenceUsage: true,
}

var workflowValidateCmd = &cobra.Command{
	Use:          "validate <path>",
	Short:        "Validate a workflow file without executing",
	Args:         cobra.ExactArgs(1),
	RunE:         validateWorkflow,
	SilenceUsage: true,
}

func init() {
	workflowCmd.AddCommand(workflowRunCmd)
	workflowCmd.AddCommand(workflowValidateCmd)

	workflowRunCmd.Flags().String("mode", "auto", "Execution mode: auto, attended, unattended")
	workflowRunCmd.Flags().StringSlice("var", nil, "Set workflow variable (key=value)")
	workflowRunCmd.Flags().Bool("no-daemon", false, "Skip daemon connection")
}

// workflowRunContext holds all state for a workflow run, reducing the parameter
// count across helper functions and lowering complexity of the main orchestrator.
type workflowRunContext struct {
	runID          string
	workflowHash   string
	normalizedPath string
	workDir        string
	ctx            context.Context
	def            *workflow.WorkflowDef
	display        *workflow.Display
	artifact       *workflow.RunArtifact
	transport      *workflow.AnalysisTransport
	handler        workflow.InteractionHandler
	noDaemon       bool
}

func runWorkflow(cmd *cobra.Command, args []string) error {
	// Phase 1: Parse and validate.
	def, data, err := loadWorkflow(args[0])
	if err != nil {
		return err
	}

	// Phase 2: Setup run context.
	rc, cancel, err := setupRunContext(cmd, def, data, args[0])
	if err != nil {
		return err
	}
	defer cancel()
	if rc.artifact != nil {
		defer rc.artifact.Close()
	}

	// Phase 3: Execute job.
	result := executeJob(cmd, rc, def)

	// Phase 4: Report results.
	return reportResults(rc, result)
}

// loadWorkflow reads, parses, and validates a workflow file.
func loadWorkflow(path string) (*workflow.WorkflowDef, []byte, error) {
	data, err := readWorkflowBytes(path)
	if err != nil {
		return nil, nil, err
	}

	def, err := workflow.ParseWorkflow(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing workflow: %w", err)
	}

	if errs := workflow.ValidateWorkflow(def); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "validation error: %s\n", e)
		}
		return nil, nil, &WorkflowExitError{Code: ExitValidationError, Message: fmt.Sprintf("workflow validation failed with %d errors", len(errs))}
	}

	return def, data, nil
}

// setupRunContext initializes all the infrastructure for a workflow run.
// Returns the run context and a cancel function for signal handling.
func setupRunContext(cmd *cobra.Command, def *workflow.WorkflowDef, data []byte, workflowPath string) (*workflowRunContext, context.CancelFunc, error) {
	runID := generateRunID()

	hash := sha256.Sum256(data)
	workflowHash := hex.EncodeToString(hash[:8])

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	displayMode := workflow.DetectMode()
	display := workflow.NewDisplay(os.Stdout, displayMode)

	masker := workflow.NewSecretMasker(def.Secrets)

	artifact, err := workflow.NewRunArtifact(runID)
	if err != nil {
		slog.Warn("failed to create run artifact", "error", err)
	}

	absPath, err := filepath.Abs(workflowPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve workflow path: %w", err)
	}
	normalizedPath := workflow.NormalizePath(absPath)

	if artifact != nil {
		artifact.WriteEvent(workflow.EventRunStart, &workflow.RunStartData{
			RunID: runID, WorkflowName: def.Name, WorkflowPath: normalizedPath,
		})
	}

	display.RunStart(def.Name, runID)

	noDaemon, _ := cmd.Flags().GetBool("no-daemon")
	if !noDaemon {
		notifyDaemonRunStart(ctx, runID, def.Name, workflowHash, normalizedPath)
	}

	analyzer := workflow.NewAnalyzer(masker)
	transport := workflow.NewAnalysisTransport(analyzer, claude.QueryWithContext)

	mode, _ := cmd.Flags().GetString("mode")
	handler := selectInteractionHandler(mode, displayMode)

	rc := &workflowRunContext{
		runID:          runID,
		workflowHash:   workflowHash,
		normalizedPath: normalizedPath,
		workDir:        ".",
		ctx:            ctx,
		def:            def,
		display:        display,
		artifact:       artifact,
		transport:      transport,
		handler:        handler,
		noDaemon:       noDaemon,
	}

	return rc, cancel, nil
}

// jobExecutionResult holds the outcome of executing a job.
type jobExecutionResult struct {
	allStepResults []*workflow.StepResult
	overallStatus  string
	humanRejected  bool
	validationErr  bool
	cancelled      bool
	totalDuration  time.Duration
}

// executeJob runs all matrix combinations for the workflow's single v0 job.
func executeJob(cmd *cobra.Command, rc *workflowRunContext, def *workflow.WorkflowDef) *jobExecutionResult {
	job, err := getSingleJob(def)
	if err != nil {
		slog.Error("invalid workflow job layout", "error", err)
		return &jobExecutionResult{overallStatus: string(workflow.RunFailed), validationErr: true}
	}

	// Parse --var flags into env overrides.
	vars, _ := cmd.Flags().GetStringSlice("var")
	varEnv := parseVarFlags(vars)
	workflowEnv := mergeMaps(def.Env, varEnv)

	matrixCombinations := expandMatrix(job)

	runStart := time.Now()
	result := &jobExecutionResult{
		overallStatus: string(workflow.RunPassed),
	}

	for _, matrixVars := range matrixCombinations {
		cfg := workflow.RunnerConfig{
			WorkDir:    rc.workDir,
			Env:        workflowEnv,
			JobEnv:     job.Env,
			MatrixVars: matrixVars,
			Secrets:    def.Secrets,
		}

		runner := workflow.NewRunner(cfg)
		runResult := runner.Run(rc.ctx, job.Steps)
		matrixKey := matrixKeyString(matrixVars)

		rejected := rc.processStepResults(rc.ctx, runResult.Steps, job.Steps, matrixKey)
		result.allStepResults = append(result.allStepResults, runResult.Steps...)

		if rejected {
			result.overallStatus = string(workflow.RunFailed)
			result.humanRejected = true
			break
		}

		if runResult.Status == string(workflow.RunFailed) {
			result.overallStatus = string(workflow.RunFailed)
		}
		if runResult.Status == string(workflow.RunCancelled) {
			result.overallStatus = string(workflow.RunCancelled)
			result.cancelled = true
			break
		}
	}

	result.totalDuration = time.Since(runStart)
	return result
}

// processStepResults handles display, artifact, daemon, and analysis for each step.
// Returns true if a human rejected the step (workflow should stop).
func (rc *workflowRunContext) processStepResults(ctx context.Context, results []*workflow.StepResult, stepDefs []*workflow.StepDef, matrixKey string) bool {
	for _, sr := range results {
		rc.display.StepEnd(sr.Name, matrixKey, sr.Status, time.Duration(sr.DurationMs)*time.Millisecond)

		if sr.Status == string(workflow.StepFailed) {
			rc.display.StepError(sr.StderrTail, sr.StdoutTail)
		}

		if rc.artifact != nil {
			rc.artifact.WriteEvent(workflow.EventStepEnd, &workflow.StepEndData{
				RunID: rc.runID, StepID: sr.StepID, MatrixKey: matrixKey,
				Status: sr.Status, ExitCode: sr.ExitCode, DurationMs: sr.DurationMs,
			})
		}

		if !rc.noDaemon {
			notifyDaemonStepUpdate(ctx, rc.runID, sr, matrixKey)
		}

		if sr.Status != string(workflow.StepSkipped) && sr.Status != string(workflow.StepCancelled) {
			step := findStepDef(stepDefs, sr.StepID)
			if step != nil && step.Analyze {
				if rc.handleAnalysis(ctx, sr, step, matrixKey) {
					return true // human rejected
				}
			}
		}
	}
	return false
}

// handleAnalysis runs LLM analysis and prompts for human review if needed.
// Returns true if the human rejected the step.
func (rc *workflowRunContext) handleAnalysis(ctx context.Context, sr *workflow.StepResult, step *workflow.StepDef, matrixKey string) bool {
	analysisResult := analyzeStep(ctx, rc.transport, rc.runID, sr, step, matrixKey)

	if analysisResult != nil && workflow.ShouldPromptHuman(analysisResult.Decision, step.RiskLevel) {
		for {
			decision, reviewErr := rc.handler.PromptReview(ctx, sr.Name, analysisResult, sr.StdoutTail)
			if reviewErr != nil {
				slog.Warn("review error", "error", reviewErr)
			}
			if decision == nil {
				return false
			}

			switch decision.Action {
			case string(workflow.ActionReject):
				return true
			case string(workflow.ActionApprove):
				return false
			case string(workflow.ActionCommand):
				if strings.TrimSpace(decision.Input) == "" {
					continue
				}
				if err := runAdHocCommand(ctx, decision.Input, rc.workDir, sr.ResolvedEnv); err != nil {
					slog.Warn("ad-hoc command failed", "error", err)
					fmt.Fprintf(os.Stderr, "command exited: %v\n", err)
				}
			case string(workflow.ActionQuestion):
				if strings.TrimSpace(decision.Input) == "" {
					continue
				}
				followUp := analyzeStepWithQuestion(ctx, rc.transport, rc.runID, sr, step, matrixKey, decision.Input)
				if followUp != nil {
					analysisResult = followUp
				}
			default:
				// Unknown action: prompt again.
			}
		}
	}
	return false
}

func analyzeStepWithQuestion(
	ctx context.Context,
	transport *workflow.AnalysisTransport,
	runID string,
	sr *workflow.StepResult,
	step *workflow.StepDef,
	matrixKey, question string,
) *workflow.AnalysisResult {
	prompt := strings.TrimSpace(step.AnalysisPrompt)
	if prompt == "" {
		prompt = "Analyze the workflow step output."
	}
	prompt += "\n\nFollow-up question from reviewer: " + question

	result, err := transport.Analyze(ctx, &workflow.AnalysisRequest{
		RunID:          runID,
		StepID:         sr.StepID,
		StepName:       sr.Name,
		MatrixKey:      matrixKey,
		RiskLevel:      step.RiskLevel,
		StdoutTail:     sr.StdoutTail,
		StderrTail:     sr.StderrTail,
		AnalysisPrompt: prompt,
	})
	if err != nil {
		slog.Warn("follow-up analysis failed", "step", sr.Name, "error", err)
		return nil
	}
	return result
}

func runAdHocCommand(ctx context.Context, command, workDir string, env []string) error {
	fmt.Fprintf(os.Stderr, "warning: executing reviewer-provided shell command via shell: %q\n", command)
	fmt.Fprintln(os.Stderr, "warning: this runs with shell parsing/expansion (pipes, redirects, substitutions); review carefully before execution")

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// #nosec G204 -- command is explicitly provided by the human reviewer at runtime.
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
	} else {
		shellPath := os.Getenv("SHELL")
		if shellPath == "" {
			shellPath = "/bin/sh"
		}
		// #nosec G204 -- command is explicitly provided by the human reviewer at runtime.
		cmd = exec.CommandContext(ctx, shellPath, "-c", command)
	}

	cmd.Dir = workDir
	cmd.Env = mergeCommandEnv(os.Environ(), env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func mergeCommandEnv(base, overrides []string) []string {
	merged := make([]string, 0, len(base)+len(overrides))
	indexByKey := make(map[string]int, len(base)+len(overrides))

	appendOrReplace := func(entry string) {
		key := entry
		if idx := strings.Index(entry, "="); idx >= 0 {
			key = entry[:idx]
		}
		if idx, exists := indexByKey[key]; exists {
			merged[idx] = entry
			return
		}
		indexByKey[key] = len(merged)
		merged = append(merged, entry)
	}

	for _, entry := range base {
		appendOrReplace(entry)
	}
	for _, entry := range overrides {
		appendOrReplace(entry)
	}

	return merged
}

// reportResults displays the final summary and returns the appropriate exit error.
func reportResults(rc *workflowRunContext, result *jobExecutionResult) error {
	summaries := make([]workflow.StepSummary, len(result.allStepResults))
	for i, sr := range result.allStepResults {
		summaries[i] = workflow.StepSummary{
			Name:     sr.Name,
			Status:   sr.Status,
			Duration: time.Duration(sr.DurationMs) * time.Millisecond,
		}
	}

	rc.display.RunEnd(result.overallStatus, result.totalDuration, summaries)

	if rc.artifact != nil {
		rc.artifact.WriteEvent(workflow.EventRunEnd, &workflow.RunEndData{
			RunID: rc.runID, Status: result.overallStatus, DurationMs: result.totalDuration.Milliseconds(),
		})
	}

	if !rc.noDaemon {
		notifyDaemonRunEnd(context.Background(), rc.runID, result.overallStatus, result.totalDuration)
	}

	if result.validationErr {
		return &WorkflowExitError{Code: ExitValidationError, Message: fmt.Sprintf("workflow %s", result.overallStatus)}
	}
	if result.cancelled || result.overallStatus == string(workflow.RunCancelled) {
		return &WorkflowExitError{Code: ExitCancelled, Message: "workflow cancelled"}
	}
	if result.overallStatus == string(workflow.RunFailed) {
		exitCode := ExitStepFailed
		if result.humanRejected {
			exitCode = ExitHumanReject
		}
		return &WorkflowExitError{Code: exitCode, Message: fmt.Sprintf("workflow %s", result.overallStatus)}
	}

	return nil
}

func validateWorkflow(_ *cobra.Command, args []string) error {
	data, err := readWorkflowBytes(args[0])
	if err != nil {
		return err
	}

	def, err := workflow.ParseWorkflow(data)
	if err != nil {
		return fmt.Errorf("parsing workflow: %w", err)
	}

	errs := workflow.ValidateWorkflow(def)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  \u2717 %s\n", e)
		}
		return fmt.Errorf("validation failed with %d errors", len(errs))
	}

	fmt.Printf("  \u2713 %s is valid (%d jobs, %d total steps)\n", def.Name, len(def.Jobs), countSteps(def))
	if def.Description != "" {
		fmt.Printf("  %s\n", def.Description)
	}
	return nil
}

func readWorkflowBytes(path string) ([]byte, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading workflow file: %w", err)
	}
	return data, nil
}

// --- Helper functions ---

func generateRunID() string {
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
}

func selectInteractionHandler(mode string, displayMode workflow.DisplayMode) workflow.InteractionHandler {
	switch mode {
	case "unattended":
		return &workflow.NonInteractiveHandler{}
	case "attended":
		return workflow.NewTerminalReviewer(os.Stdin, os.Stderr)
	default: // "auto"
		if displayMode == workflow.DisplayTTY {
			return workflow.NewTerminalReviewer(os.Stdin, os.Stderr)
		}
		return &workflow.NonInteractiveHandler{}
	}
}

func expandMatrix(job *workflow.JobDef) []map[string]string {
	if job.Strategy == nil || job.Strategy.Matrix == nil || len(job.Strategy.Matrix.Include) == 0 {
		return []map[string]string{{}} // single run with no matrix vars
	}
	return job.Strategy.Matrix.Include
}

func getSingleJob(def *workflow.WorkflowDef) (*workflow.JobDef, error) {
	if len(def.Jobs) != 1 {
		return nil, fmt.Errorf("expected exactly one job in v0, got %d", len(def.Jobs))
	}
	for _, job := range def.Jobs {
		return job, nil
	}
	return nil, fmt.Errorf("no job found")
}

func matrixKeyString(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+vars[k])
	}
	return strings.Join(parts, ",")
}

func parseVarFlags(vars []string) map[string]string {
	result := map[string]string{}
	for _, v := range vars {
		if idx := strings.IndexByte(v, '='); idx >= 0 {
			result[v[:idx]] = v[idx+1:]
		}
	}
	return result
}

func mergeMaps(base, override map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

func findStepDef(steps []*workflow.StepDef, stepID string) *workflow.StepDef {
	for _, s := range steps {
		if s.ID == stepID {
			return s
		}
	}
	return nil
}

func analyzeStep(
	ctx context.Context,
	transport *workflow.AnalysisTransport,
	runID string,
	sr *workflow.StepResult,
	step *workflow.StepDef,
	matrixKey string,
) *workflow.AnalysisResult {
	result, err := transport.Analyze(ctx, &workflow.AnalysisRequest{
		RunID:          runID,
		StepID:         sr.StepID,
		StepName:       sr.Name,
		MatrixKey:      matrixKey,
		RiskLevel:      step.RiskLevel,
		StdoutTail:     sr.StdoutTail,
		StderrTail:     sr.StderrTail,
		AnalysisPrompt: step.AnalysisPrompt,
	})
	if err != nil {
		slog.Warn("analysis failed", "step", sr.Name, "error", err)
		return nil
	}
	return result
}

func countSteps(def *workflow.WorkflowDef) int {
	total := 0
	for _, j := range def.Jobs {
		total += len(j.Steps)
	}
	return total
}

// --- Daemon notification helpers (fire-and-forget, errors logged not returned) ---

func notifyDaemonRunStart(ctx context.Context, runID, name, hash, path string) {
	conn, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	rpcCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	client := pb.NewClaiServiceClient(conn)
	_, _ = client.WorkflowRunStart(rpcCtx, &pb.WorkflowRunStartRequest{
		RunId:           runID,
		WorkflowName:    name,
		WorkflowHash:    hash,
		WorkflowPath:    path,
		StartedAtUnixMs: time.Now().UnixMilli(),
	})
}

func notifyDaemonStepUpdate(ctx context.Context, runID string, sr *workflow.StepResult, matrixKey string) {
	conn, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	rpcCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Convert outputs to JSON string for the proto field.
	var outputsJSON string
	if len(sr.Outputs) > 0 {
		if data, jsonErr := json.Marshal(sr.Outputs); jsonErr == nil {
			outputsJSON = string(data)
		}
	}

	client := pb.NewClaiServiceClient(conn)
	_, _ = client.WorkflowStepUpdate(rpcCtx, &pb.WorkflowStepUpdateRequest{
		RunId:       runID,
		StepId:      sr.StepID,
		MatrixKey:   matrixKey,
		Status:      sr.Status,
		Command:     sr.Command,
		ExitCode:    int32(sr.ExitCode),
		DurationMs:  sr.DurationMs,
		StdoutTail:  sr.StdoutTail,
		StderrTail:  sr.StderrTail,
		OutputsJson: outputsJSON,
	})
}

func notifyDaemonRunEnd(ctx context.Context, runID, status string, duration time.Duration) {
	conn, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	rpcCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	client := pb.NewClaiServiceClient(conn)
	_, _ = client.WorkflowRunEnd(rpcCtx, &pb.WorkflowRunEndRequest{
		RunId:         runID,
		Status:        status,
		DurationMs:    duration.Milliseconds(),
		EndedAtUnixMs: time.Now().UnixMilli(),
	})
}
