package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func runWorkflow(cmd *cobra.Command, args []string) error {
	workflowPath := args[0]

	// 1. Read and parse YAML.
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("reading workflow file: %w", err)
	}

	def, err := workflow.ParseWorkflow(data)
	if err != nil {
		return fmt.Errorf("parsing workflow: %w", err)
	}

	// 2. Validate.
	if errs := workflow.ValidateWorkflow(def); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "validation error: %s\n", e)
		}
		return fmt.Errorf("workflow validation failed with %d errors", len(errs))
	}

	// 3. Generate run ID.
	runID := generateRunID()

	// 4. Compute workflow hash (first 8 bytes of SHA-256).
	hash := sha256.Sum256(data)
	workflowHash := hex.EncodeToString(hash[:8])

	// 5. Setup signal handling.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// 6. Detect display mode.
	displayMode := workflow.DetectMode()
	display := workflow.NewDisplay(os.Stdout, displayMode)

	// 7. Setup secret masker.
	masker := workflow.NewSecretMasker(def.Secrets)

	// 8. Create RunArtifact.
	artifact, err := workflow.NewRunArtifact(runID)
	if err != nil {
		slog.Warn("failed to create run artifact", "error", err)
		// Continue without artifact -- don't fail the run.
	}
	if artifact != nil {
		defer artifact.Close()
	}

	// 9. Write run_start event.
	absPath, _ := filepath.Abs(workflowPath)
	if artifact != nil {
		artifact.WriteEvent(workflow.EventRunStart, &workflow.RunStartData{
			RunID: runID, WorkflowName: def.Name, WorkflowPath: absPath,
		})
	}

	// 10. Display run start.
	display.RunStart(def.Name, runID)

	// 11. Notify daemon (if available).
	noDaemon, _ := cmd.Flags().GetBool("no-daemon")
	if !noDaemon {
		notifyDaemonRunStart(ctx, runID, def.Name, workflowHash, absPath)
	}

	// 12. Create analyzer and transport.
	analyzer := workflow.NewAnalyzer(masker)
	transport := workflow.NewAnalysisTransport(analyzer, nil) // no direct LLM in Tier 0 CLI

	// 13. Determine interaction handler based on mode.
	mode, _ := cmd.Flags().GetString("mode")
	handler := selectInteractionHandler(mode, displayMode)

	// 14. Get the first job (Tier 0: single job support).
	var jobName string
	var job *workflow.JobDef
	for k, v := range def.Jobs {
		jobName = k
		job = v
		break
	}
	if job == nil {
		return fmt.Errorf("workflow has no jobs")
	}
	_ = jobName // used for logging in future tiers

	// 15. Parse --var flags into env overrides.
	vars, _ := cmd.Flags().GetStringSlice("var")
	varEnv := parseVarFlags(vars)

	// Merge workflow env + var overrides.
	workflowEnv := mergeMaps(def.Env, varEnv)

	// 16. Handle matrix (Tier 0: if no matrix, single execution).
	matrixCombinations := expandMatrix(job)

	runStart := time.Now()
	var allStepResults []*workflow.StepResult
	overallStatus := "passed"

	for _, matrixVars := range matrixCombinations {
		cfg := workflow.RunnerConfig{
			WorkDir:    ".",
			Env:        workflowEnv,
			JobEnv:     job.Env,
			MatrixVars: matrixVars,
			Secrets:    def.Secrets,
		}

		runner := workflow.NewRunner(cfg)
		result := runner.Run(ctx, job.Steps)

		for _, sr := range result.Steps {
			allStepResults = append(allStepResults, sr)

			matrixKey := matrixKeyString(matrixVars)

			// Display step end.
			display.StepEnd(sr.Name, matrixKey, sr.Status, time.Duration(sr.DurationMs)*time.Millisecond)

			// Write artifact events.
			if artifact != nil {
				artifact.WriteEvent(workflow.EventStepEnd, &workflow.StepEndData{
					RunID: runID, StepID: sr.StepID, MatrixKey: matrixKey,
					Status: sr.Status, ExitCode: sr.ExitCode, DurationMs: sr.DurationMs,
				})
			}

			// Notify daemon.
			if !noDaemon {
				notifyDaemonStepUpdate(ctx, runID, sr, matrixKey)
			}

			// Analysis for steps with analyze: true.
			if sr.Status != "skipped" {
				step := findStepDef(job.Steps, sr.StepID)
				if step != nil && step.Analyze {
					analysisResult := analyzeStep(ctx, transport, runID, sr, step, matrixKey)

					if analysisResult != nil && workflow.ShouldPromptHuman(analysisResult.Decision, step.RiskLevel) {
						decision, reviewErr := handler.PromptReview(ctx, sr.Name, analysisResult, sr.StdoutTail)
						if reviewErr != nil {
							slog.Warn("review error", "error", reviewErr)
						}
						if decision != nil && decision.Action == "reject" {
							overallStatus = "failed"
							break
						}
					}
				}
			}
		}

		if result.Status == "failed" {
			overallStatus = "failed"
		}
	}

	totalDuration := time.Since(runStart)

	// Convert StepResults to StepSummaries for display.
	summaries := make([]workflow.StepSummary, len(allStepResults))
	for i, sr := range allStepResults {
		summaries[i] = workflow.StepSummary{
			Name:     sr.Name,
			Status:   sr.Status,
			Duration: time.Duration(sr.DurationMs) * time.Millisecond,
		}
	}

	// Display run end.
	display.RunEnd(overallStatus, totalDuration, summaries)

	// Write run_end artifact.
	if artifact != nil {
		artifact.WriteEvent(workflow.EventRunEnd, &workflow.RunEndData{
			RunID: runID, Status: overallStatus, DurationMs: totalDuration.Milliseconds(),
		})
	}

	// Notify daemon run end.
	if !noDaemon {
		notifyDaemonRunEnd(ctx, runID, overallStatus, totalDuration)
	}

	// Exit codes.
	if overallStatus == "failed" {
		os.Exit(1)
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
