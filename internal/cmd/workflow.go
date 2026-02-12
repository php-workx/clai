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
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	pb "github.com/runger/clai/gen/clai/v1"
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
	Short:   "Run and validate workflow files",
	GroupID: groupCore,
}

var workflowRunCmd = &cobra.Command{
	Use:   "run <path>",
	Short: "Execute a workflow file",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflow,
}

var workflowValidateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Validate a workflow file without executing",
	Args:  cobra.ExactArgs(1),
	RunE:  validateWorkflow,
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
		return nil, nil, fmt.Errorf("reading workflow file: %w", err)
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

	absPath, _ := filepath.Abs(workflowPath)
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
	transport := workflow.NewAnalysisTransport(analyzer, nil)

	mode, _ := cmd.Flags().GetString("mode")
	handler := selectInteractionHandler(mode, displayMode)

	rc := &workflowRunContext{
		runID:          runID,
		workflowHash:   workflowHash,
		normalizedPath: normalizedPath,
		def:            def,
		display:        display,
		artifact:       artifact,
		transport:      transport,
		handler:        handler,
		noDaemon:       noDaemon,
	}

	// Store ctx on cancel so callers can use it; we return cancel for defer.
	_ = ctx
	return rc, cancel, nil
}

// jobExecutionResult holds the outcome of executing a job.
type jobExecutionResult struct {
	allStepResults []*workflow.StepResult
	overallStatus  string
	humanRejected  bool
	totalDuration  time.Duration
}

// executeJob runs all matrix combinations for the first job in the workflow.
func executeJob(cmd *cobra.Command, rc *workflowRunContext, def *workflow.WorkflowDef) *jobExecutionResult {
	// Get the first job (Tier 0: single job support).
	var job *workflow.JobDef
	for _, v := range def.Jobs {
		job = v
		break
	}
	if job == nil {
		return &jobExecutionResult{overallStatus: string(workflow.RunFailed)}
	}

	// Parse --var flags into env overrides.
	vars, _ := cmd.Flags().GetStringSlice("var")
	varEnv := parseVarFlags(vars)
	workflowEnv := mergeMaps(def.Env, varEnv)

	matrixCombinations := expandMatrix(job)

	// Setup signal handling context for execution.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runStart := time.Now()
	result := &jobExecutionResult{
		overallStatus: string(workflow.RunPassed),
	}

	for _, matrixVars := range matrixCombinations {
		cfg := workflow.RunnerConfig{
			WorkDir:    ".",
			Env:        workflowEnv,
			JobEnv:     job.Env,
			MatrixVars: matrixVars,
			Secrets:    def.Secrets,
		}

		runner := workflow.NewRunner(cfg)
		runResult := runner.Run(ctx, job.Steps)
		matrixKey := matrixKeyString(matrixVars)

		rejected := rc.processStepResults(ctx, runResult.Steps, job.Steps, matrixKey)
		result.allStepResults = append(result.allStepResults, runResult.Steps...)

		if rejected {
			result.overallStatus = string(workflow.RunFailed)
			result.humanRejected = true
			break
		}

		if runResult.Status == string(workflow.RunFailed) {
			result.overallStatus = string(workflow.RunFailed)
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

		if rc.artifact != nil {
			rc.artifact.WriteEvent(workflow.EventStepEnd, &workflow.StepEndData{
				RunID: rc.runID, StepID: sr.StepID, MatrixKey: matrixKey,
				Status: sr.Status, ExitCode: sr.ExitCode, DurationMs: sr.DurationMs,
			})
		}

		if !rc.noDaemon {
			notifyDaemonStepUpdate(ctx, rc.runID, sr, matrixKey)
		}

		if sr.Status != "skipped" {
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
		decision, reviewErr := rc.handler.PromptReview(ctx, sr.Name, analysisResult, sr.StdoutTail)
		if reviewErr != nil {
			slog.Warn("review error", "error", reviewErr)
		}
		if decision != nil && decision.Action == string(workflow.ActionReject) {
			return true
		}
	}
	return false
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
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading workflow file: %w", err)
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
	return nil
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

func analyzeStep(ctx context.Context, transport *workflow.AnalysisTransport, runID string, sr *workflow.StepResult, step *workflow.StepDef, matrixKey string) *workflow.AnalysisResult {
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

	client := pb.NewClaiServiceClient(conn)
	_, _ = client.WorkflowRunStart(ctx, &pb.WorkflowRunStartRequest{
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

	// Convert outputs to JSON string for the proto field.
	var outputsJSON string
	if len(sr.Outputs) > 0 {
		if data, jsonErr := json.Marshal(sr.Outputs); jsonErr == nil {
			outputsJSON = string(data)
		}
	}

	client := pb.NewClaiServiceClient(conn)
	_, _ = client.WorkflowStepUpdate(ctx, &pb.WorkflowStepUpdateRequest{
		RunId:       runID,
		StepId:      sr.StepID,
		MatrixKey:   matrixKey,
		Status:      sr.Status,
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

	client := pb.NewClaiServiceClient(conn)
	_, _ = client.WorkflowRunEnd(ctx, &pb.WorkflowRunEndRequest{
		RunId:         runID,
		Status:        status,
		DurationMs:    duration.Milliseconds(),
		EndedAtUnixMs: time.Now().UnixMilli(),
	})
}
