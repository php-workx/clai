# clai Workflow Execution — Technical Specification

**Version:** 0.1 (Draft)
**Date:** 2026-02-11
**Status:** RFC
**Companion:** `specs/func_workflows.md` (functional requirements)

> This document specifies **how** to implement the workflow execution feature defined in `func_workflows.md`. It covers architecture, data structures, protocols, and implementation details. Reference the functional spec for *what* the system should do.
>
> Implementation draws inspiration from [dagu-org/dagu](https://github.com/dagu-org/dagu) — a self-contained Go workflow engine with YAML definitions, DAG-based step execution, and file-system state. See §15 for a detailed comparison of adopted vs. omitted patterns.

---

## 1. Technical Architecture

### 1.1 Hybrid Execution Model

The workflow system uses a **hybrid** model: the CLI process orchestrates execution, the daemon tracks state, and steps run as subprocesses.

```
User invokes:  clai workflow run pulumi-compliance-run
                        │
                        ▼
              ┌──────────────────────────┐
              │  CLI Process             │
              │  (clai workflow run)     │
              │                          │
              │  1. Load & parse YAML    │
              │  2. Expand matrix        │
              │  3. Resolve expressions  │
              │  4. Validate DAG         │
              │  5. WorkflowRunStart ────┼──────► ┌────────────────┐
              │     RPC to daemon        │        │  Daemon (claid) │
              │  6. Execute steps via    │        │                 │
              │     os/exec.Command      │        │  Persists:      │
              │  7. WorkflowStepUpdate ──┼──────► │  • run state    │
              │     RPC per state change │        │  • step state   │
              │  8. LLM analysis via     │        │  • analyses     │
              │     claudemon/provider   │        │  in SQLite      │
              │  9. Human prompts via    │        │                 │
              │     stdin/stdout         │        │  Serves:        │
              │ 10. WorkflowRunEnd ──────┼──────► │  • status RPCs  │
              └──────────────────────────┘        │  • history RPCs │
                        │                         │  • stop signal  │
                        │ spawns per step          └────────────────┘
                        ▼
              ┌──────────────────────────┐
              │  Subprocesses            │
              │  (one per step)          │
              │                          │
              │  $SHELL -c "command"     │
              │  stdout → pipe → capture │
              │  stderr → pipe → capture │
              │  exit code → reported    │
              └──────────────────────────┘
```

### 1.2 Why CLI Orchestrates (Not Daemon)

The daemon (`claid`) is designed as a lightweight state tracker with idle timeout. Running long-lived workflow execution inside the daemon would:

- Conflict with idle timeout behavior (daemon auto-exits after 20 min)
- Make the daemon harder to restart/upgrade during a workflow run
- Route potentially large output through gRPC unnecessarily

The CLI process:

- Has direct terminal access for human interaction (func spec §4.6)
- Supports `Ctrl+C` via `context.Context` (existing pattern)
- Lifecycle matches the workflow run lifecycle
- Can interact with claudemon directly (no daemon intermediary)

### 1.3 Component Responsibilities

| Component | Responsibilities |
|-----------|-----------------|
| **CLI** (`clai workflow run`) | YAML parsing, expression evaluation, matrix expansion, DAG validation, step execution, output capture, LLM interaction, human prompts, terminal output |
| **Daemon** (`claid`) | Persist run/step state in SQLite, serve status/history queries, propagate stop signals |
| **Subprocesses** | Execute individual shell commands, produce stdout/stderr |
| **claudemon / LLM** | Analyze step output, return structured decisions, support follow-up conversation |

### 1.4 Graceful Degradation

If the daemon is unavailable:

1. CLI attempts `ipc.EnsureDaemon()` (existing pattern from `internal/ipc/spawn.go`)
2. If daemon still unreachable: run workflow **without** state persistence
3. Print warning: `"daemon unavailable — run history will not be persisted"`
4. All other functionality (execution, LLM analysis, human prompts) works normally

---

## 2. Execution Engine

### 2.1 Execution Flow (v0)

For v0, execution is sequential within a job. Matrix entries also run sequentially. The v0 flow:

```
1. Parse YAML → WorkflowDef
2. Validate (schema + references)
3. Expand matrix → []MatrixEntry
4. For each matrix entry (sequential):
   a. Build environment (workflow env + matrix values)
   b. For each step (sequential):
      i.   Evaluate ${{ }} expressions
      ii.  Execute command via subprocess
      iii. Capture stdout/stderr
      iv.  Export step outputs ($CLAI_OUTPUT)
      v.   If analyze=true: send output to LLM
      vi.  Apply risk_level decision matrix
      vii. If human input needed: pause and prompt
      viii.Report step state to daemon
   c. If step fails → stop (implicit fail-fast)
5. Report workflow completion to daemon
```

### 2.2 Job Dependency Resolution (v1)

Jobs can depend on other jobs via `needs` (func spec FR-18). When multiple jobs exist:

**Algorithm:** Kahn's algorithm (topological sort with in-degree tracking).

```go
func resolveJobOrder(jobs map[string]*JobDef) ([][]string, error) {
    // 1. Build adjacency list from needs fields
    // 2. Compute in-degree for each job
    // 3. Initialize queue with zero-in-degree jobs
    // 4. Process waves:
    //    - Dequeue all zero-in-degree jobs → one wave (can run in parallel)
    //    - Decrement in-degree of dependents
    //    - Enqueue newly zero-in-degree jobs
    // 5. If unprocessed jobs remain → cycle detected → error
    // Returns: [][]string — each inner slice is a wave of parallelizable jobs
}
```

**Cycle detection:** If the algorithm terminates with unprocessed jobs, report the cycle:

```
error: workflow has circular job dependencies: jobA → jobB → jobC → jobA
```

### 2.3 Step Lifecycle State Machine

```
              ┌─────────┐
              │ pending  │
              └────┬─────┘
                   │ preconditions evaluated
                   │
          ┌────────┴────────┐
          │                 │
          ▼                 ▼
     ┌─────────┐      ┌─────────┐
     │ running  │      │ skipped │  (precondition failed, or dependency failed
     └────┬─────┘      └─────────┘   without continue_on)
          │
     ┌────┴────┐
     │         │
     ▼         ▼
┌─────────┐ ┌────────┐
│ success │ │ failed │
└─────────┘ └────────┘
```

State transitions are atomic and reported to the daemon via `WorkflowStepUpdate` RPC.

### 2.4 Matrix Expansion

Given a matrix definition (func spec §4.3):

```yaml
strategy:
  matrix:
    include:
      - stack: ojin-production
        role_arn: arn:...
        risk: high
      - stack: ojin-staging
        role_arn: arn:...
        risk: medium
```

Expansion produces a `[]MatrixEntry` where each entry is a `map[string]string`. For v0, entries execute **sequentially**. For v1, `strategy.max-parallel` controls concurrency (func spec FR-15).

### 2.5 Subprocess Management

Each step executes via Go's `os/exec`:

```go
func executeStep(ctx context.Context, step *StepDef, env []string, cwd string) (*StepResult, error) {
    shell := os.Getenv("SHELL")
    if shell == "" {
        shell = "sh"
    }

    cmd := exec.CommandContext(ctx, shell, "-c", step.ResolvedCommand)
    cmd.Dir = cwd
    cmd.Env = env

    // Capture stdout and stderr via pipes
    var stdoutBuf, stderrBuf limitedBuffer  // ring buffer, keeps last N bytes
    cmd.Stdout = io.MultiWriter(&stdoutBuf, prefixWriter(step.Name, os.Stdout))
    cmd.Stderr = io.MultiWriter(&stderrBuf, prefixWriter(step.Name, os.Stderr))

    startTime := time.Now()
    err := cmd.Run()
    duration := time.Since(startTime)

    exitCode := 0
    if err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            exitCode = exitErr.ExitCode()
        } else {
            return nil, fmt.Errorf("step %s: exec failed: %w", step.Name, err)
        }
    }

    return &StepResult{
        ExitCode:   exitCode,
        Stdout:     stdoutBuf.String(),
        Stderr:     stderrBuf.String(),
        Duration:   duration,
        StartedAt:  startTime,
    }, nil
}
```

### 2.6 Step Output Export (func spec FR-8, FR-9)

Steps export outputs by writing to a temporary file referenced by `$CLAI_OUTPUT`:

```go
// Before step execution:
outputFile, _ := os.CreateTemp("", "clai-output-*")
env = append(env, "CLAI_OUTPUT="+outputFile.Name())

// After step execution:
outputs := parseOutputFile(outputFile.Name())  // key=value per line
// Store in stepContext for downstream ${{ steps.ID.outputs.KEY }} resolution
```

Exported outputs are automatically inherited as environment variables by subsequent steps within the same job (func spec FR-9).

### 2.7 Cancellation & Signal Handling

```go
func (r *Runner) Run(ctx context.Context, wf *WorkflowDef) error {
    // ctx carries Ctrl+C (SIGINT) cancellation
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    // Listen for stop signal from daemon
    go r.pollStopSignal(ctx, cancel)

    // ... execute steps with ctx ...
    // When ctx is cancelled:
    //   - exec.CommandContext sends SIGKILL to subprocess
    //   - We prefer graceful: send SIGTERM first, wait 5s, then SIGKILL
}

func (r *Runner) gracefulKill(cmd *exec.Cmd) {
    cmd.Process.Signal(syscall.SIGTERM)
    done := make(chan struct{})
    go func() {
        cmd.Wait()
        close(done)
    }()
    select {
    case <-done:
        return
    case <-time.After(5 * time.Second):
        cmd.Process.Kill()
    }
}
```

---

## 3. YAML Parsing & Validation

### 3.1 Go Type Definitions

```go
// WorkflowDef represents a parsed workflow YAML file.
type WorkflowDef struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    Env         map[string]string `yaml:"env"`
    Jobs        map[string]*JobDef `yaml:"jobs"`

    // Lifecycle handlers (v1)
    OnSuccess *HandlerDef `yaml:"onSuccess"`
    OnFailure *HandlerDef `yaml:"onFailure"`
    OnExit    *HandlerDef `yaml:"onExit"`

    // LLM configuration override (v1)
    LLM *LLMConfig `yaml:"llm"`
}

// JobDef represents a single job within a workflow.
type JobDef struct {
    Name     string            `yaml:"name"`
    Needs    []string          `yaml:"needs"`     // Job dependencies (v1)
    If       string            `yaml:"if"`        // Conditional expression (v1)
    Env      map[string]string `yaml:"env"`
    Strategy *StrategyDef      `yaml:"strategy"`
    Steps    []*StepDef        `yaml:"steps"`
}

// StrategyDef represents the matrix strategy for a job.
type StrategyDef struct {
    Matrix   MatrixDef `yaml:"matrix"`
    FailFast *bool     `yaml:"fail-fast"` // default: true
    // MaxParallel int  `yaml:"max-parallel"` // v1: parallel matrix execution
}

// MatrixDef represents a matrix configuration.
type MatrixDef struct {
    Include []map[string]string `yaml:"include"`
    Exclude []map[string]string `yaml:"exclude"` // v1
    // Dynamic keys for cartesian product (v1):
    // e.g. os: [linux, darwin], arch: [amd64, arm64]
    Values map[string][]string `yaml:"-"` // parsed from top-level matrix keys
}

// StepDef represents a single step within a job.
type StepDef struct {
    ID             string            `yaml:"id"`
    Name           string            `yaml:"name"`
    Run            string            `yaml:"run"`
    Env            map[string]string `yaml:"env"`
    Shell          string            `yaml:"shell"`          // v1
    WorkingDir     string            `yaml:"working-directory"` // v1
    Timeout        *Duration         `yaml:"timeout-minutes"` // v1

    // LLM analysis (func spec §4.5)
    Analyze        bool   `yaml:"analyze"`
    AnalysisPrompt string `yaml:"analysis_prompt"`
    RiskLevel      string `yaml:"risk_level"`       // "low", "medium", "high"
    ContextFrom    string `yaml:"context_from"`     // v1
    ContextFor     string `yaml:"context_for"`      // v1

    // Resolved at runtime (not from YAML)
    ResolvedCommand string `yaml:"-"`
    ResolvedEnv     []string `yaml:"-"`
}

// HandlerDef represents a lifecycle handler.
type HandlerDef struct {
    Run string `yaml:"run"`
}

// LLMConfig represents workflow-level LLM configuration.
type LLMConfig struct {
    Backend    string `yaml:"backend"`     // "claudemon", "claude-cli", "api"
    Provider   string `yaml:"provider"`    // for API backend: "anthropic", "openai"
    Model      string `yaml:"model"`
    APIKeyEnv  string `yaml:"api_key_env"`
}
```

### 3.2 Parsing Pipeline

```
YAML file  →  yaml.Unmarshal  →  WorkflowDef  →  Validate()  →  Ready
```

Parsing uses `gopkg.in/yaml.v3` (already a dependency).

### 3.3 Validation Rules

| Rule | Error |
|------|-------|
| `name` is required | `workflow name is required` |
| At least one job | `workflow must have at least one job` |
| Each job has at least one step | `job "X" must have at least one step` |
| Each step has `id` and `run` (or `analyze` type) | `step in job "X" missing id or run` |
| Step IDs are unique within a job | `duplicate step id "Y" in job "X"` |
| Job names in `needs` exist | `job "X" depends on unknown job "Y"` |
| No circular job dependencies | `circular dependency: X → Y → X` |
| `risk_level` is valid (`low`, `medium`, `high`) or empty | `invalid risk_level "Z" for step "Y"` |
| `${{ }}` expressions reference valid scopes | `unknown expression scope: "${{ foo.bar }}"` |
| Matrix `include` entries have consistent keys | `matrix include entries have inconsistent keys` |

### 3.4 File Discovery

```go
func DiscoverWorkflows(cwd string) ([]WorkflowFile, error) {
    var results []WorkflowFile

    // 1. Project-local: .clai/workflows/ relative to CWD (or git root)
    gitRoot := findGitRoot(cwd) // walk up, look for .git
    localDir := filepath.Join(gitRoot, ".clai", "workflows")
    results = append(results, scanDir(localDir, "local")...)

    // 2. User global: ~/.clai/workflows/
    home, _ := os.UserHomeDir()
    globalDir := filepath.Join(home, ".clai", "workflows")
    results = append(results, scanDir(globalDir, "global")...)

    return results, nil
}

type WorkflowFile struct {
    Name   string // derived from filename
    Path   string // absolute path
    Source string // "local" or "global"
}
```

---

## 4. Expression Engine

### 4.1 Syntax

Expressions use `${{ <expr> }}` syntax (func spec FR-37, FR-38). This is evaluated **on the Go side** before passing the resolved string to the shell. This prevents shell injection.

### 4.2 Supported Scopes

| Scope | Syntax | Resolves To |
|-------|--------|-------------|
| Environment variables | `${{ env.NAME }}` | Process environment or workflow/job/step `env` |
| Workflow variables | `${{ vars.NAME }}` | Workflow-level parameters (v1) |
| Matrix parameters | `${{ matrix.KEY }}` | Current matrix entry value |
| Step outputs | `${{ steps.ID.outputs.KEY }}` | Value from step's `$CLAI_OUTPUT` file |
| Step status | `${{ steps.ID.outcome }}` | `"success"`, `"failure"`, `"skipped"` |
| Analysis results | `${{ steps.ID.analysis.decision }}` | `"proceed"`, `"halt"`, `"needs_human"` |

### 4.3 Evaluation

```go
// exprPattern matches ${{ ... }} expressions
var exprPattern = regexp.MustCompile(`\$\{\{\s*(.+?)\s*\}\}`)

func (e *ExprEvaluator) Resolve(template string, ctx *ExprContext) (string, error) {
    return exprPattern.ReplaceAllStringFunc(template, func(match string) string {
        inner := exprPattern.FindStringSubmatch(match)[1]
        val, err := e.evaluate(inner, ctx)
        if err != nil {
            // collect errors, don't panic
            e.errors = append(e.errors, err)
            return match // leave unresolved
        }
        return val
    }), nil
}

// ExprContext holds all available data for expression resolution.
type ExprContext struct {
    Env     map[string]string            // env scope
    Matrix  map[string]string            // matrix scope
    Steps   map[string]*StepOutputs      // steps scope
    Vars    map[string]string            // vars scope (v1)
}

type StepOutputs struct {
    Outputs  map[string]string  // key=value pairs from $CLAI_OUTPUT
    Outcome  string             // "success", "failure", "skipped"
    Analysis *AnalysisResult    // if analyze=true
}
```

### 4.4 Expression Safety

- All `${{ }}` expressions are resolved in Go **before** the command string is passed to `sh -c`
- Step outputs used in expressions are **not** shell-escaped (they're inserted at the Go level, not interpreted by the shell evaluator)
- The resolved command string is a plain string passed as a single argument to `sh -c`
- Secret values (from `$CLAI_OUTPUT` or env) can be referenced but are scrubbed before LLM context (see §9)

### 4.5 v0 Expression Subset

For v0, support only: `${{ env.NAME }}`, `${{ matrix.KEY }}`, `${{ steps.ID.outputs.KEY }}`. Comparisons and logical operators (`if` expressions) are deferred to v1 (func spec §5.0).

---

## 5. LLM Integration

### 5.1 claudemon Protocol (func spec FR-28)

claudemon is a managed Claude CLI instance. clai starts it as a subprocess and communicates via stdin/stdout JSON-RPC:

```go
type ClaudemonClient struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout *bufio.Reader
    mu     sync.Mutex
}

// AnalysisRequest is sent to claudemon via stdin.
type AnalysisRequest struct {
    Type    string `json:"type"`    // "analyze"
    Prompt  string `json:"prompt"`  // analysis_prompt with expressions resolved
    Content string `json:"content"` // step stdout (scrubbed, truncated to 100KB)
    Context string `json:"context"` // optional: prior step context
}

// AnalysisResponse is read from claudemon's stdout.
type AnalysisResponse struct {
    Decision string         `json:"decision"` // "proceed", "halt", "needs_human"
    Reasoning string        `json:"reasoning"`
    Flags    []FlaggedItem  `json:"flags,omitempty"`
}

type FlaggedItem struct {
    Item     string `json:"item"`
    Severity string `json:"severity"` // "info", "warning", "critical"
    Reason   string `json:"reason"`
}
```

### 5.2 Fallback Chain

1. **claudemon** (preferred): warm session, fast response, stream-based
2. **claude CLI**: `claude --print` with prompt via stdin (existing pattern from `internal/claude/claude.go`)
3. **API provider**: direct Anthropic/OpenAI API call via `internal/provider/` (v1)

If all backends fail, treat the analysis as `needs_human` (func spec FR-24).

### 5.3 Context Building & Truncation (func spec FR-27)

```go
const maxOutputForLLM = 100 * 1024 // 100KB (~25k tokens)

func buildAnalysisContext(step *StepDef, result *StepResult, secrets []string) string {
    output := result.Stdout

    // 1. Scrub secrets
    output = scrubSecrets(output, secrets)

    // 2. Truncate with head+tail preservation
    if len(output) > maxOutputForLLM {
        headSize := maxOutputForLLM * 40 / 100  // 40% head
        tailSize := maxOutputForLLM * 40 / 100  // 40% tail
        marker := "\n\n... [truncated: middle section omitted, showing first and last portions] ...\n\n"
        output = output[:headSize] + marker + output[len(output)-tailSize:]
    }

    return output
}
```

### 5.4 Structured Response Parsing

```go
func parseAnalysisResponse(raw string) (*AnalysisResponse, error) {
    // Try JSON parsing first
    var resp AnalysisResponse
    if err := json.Unmarshal([]byte(raw), &resp); err == nil {
        if isValidDecision(resp.Decision) {
            return &resp, nil
        }
    }

    // Fallback: scan for decision keywords in natural language
    lower := strings.ToLower(raw)
    switch {
    case strings.Contains(lower, "proceed"):
        return &AnalysisResponse{Decision: "proceed", Reasoning: raw}, nil
    case strings.Contains(lower, "halt"):
        return &AnalysisResponse{Decision: "halt", Reasoning: raw}, nil
    default:
        return &AnalysisResponse{Decision: "needs_human", Reasoning: raw}, nil
    }
}
```

### 5.5 Risk Level Decision Matrix (func spec FR-25)

```go
func shouldPromptHuman(riskLevel string, decision string) bool {
    switch riskLevel {
    case "high":
        return true  // always prompt, regardless of LLM decision
    case "medium":
        return decision != "proceed"  // prompt unless LLM says proceed
    case "low":
        return decision == "halt"  // only prompt if LLM says halt
    default:
        return decision != "proceed"  // default to medium behavior
    }
}
```

### 5.6 Follow-Up Conversation (func spec FR-33)

During human review, the user can ask follow-up questions to the LLM:

```go
type ReviewSession struct {
    client     LLMClient
    transcript []Message
    stepOutput string
}

func (rs *ReviewSession) AskFollowUp(question string) (*AnalysisResponse, error) {
    rs.transcript = append(rs.transcript, Message{Role: "user", Content: question})

    // Send conversation history + original step output
    resp, err := rs.client.Converse(rs.transcript)
    if err != nil {
        return nil, err
    }

    rs.transcript = append(rs.transcript, Message{Role: "assistant", Content: resp.Raw})
    return resp, nil
}
```

The full conversation transcript is persisted in the `workflow_analyses` table (§7).

---

## 6. Human Interaction

### 6.1 Terminal Prompt Interface

When human input is required (func spec §4.6):

```
╭─ Step: Pulumi Preview [ojin-production] ─────────────────────────╮
│                                                                    │
│  LLM Analysis: NEEDS_HUMAN                                        │
│  Reasoning: Found 7 resource changes including IAM policy          │
│  modification. Between 5-10 resources changing warrants review.    │
│                                                                    │
│  Flagged:                                                          │
│    ⚠ WARNING: aws:iam:Policy "deploy-policy" will be updated      │
│    ℹ INFO: 5 Lambda functions will be updated (config only)        │
│                                                                    │
│  Actions:                                                          │
│    [a] Approve and continue                                        │
│    [r] Reject and halt workflow                                    │
│    [i] Inspect full output                                         │
│    [c] Run ad-hoc command                                          │
│    [q] Ask LLM a follow-up question                                │
│                                                                    │
╰────────────────────────────────────────────────────────────────────╯
Choice:
```

### 6.2 Implementation

```go
type HumanReviewer struct {
    reader   *bufio.Reader  // os.Stdin
    writer   io.Writer      // os.Stdout
    llm      *ReviewSession
}

func (hr *HumanReviewer) Review(step *StepDef, result *StepResult, analysis *AnalysisResponse) (Decision, error) {
    hr.printReviewCard(step, result, analysis)

    for {
        choice := hr.readChoice()
        switch choice {
        case "a":
            return DecisionApprove, nil
        case "r":
            return DecisionReject, nil
        case "i":
            hr.printFullOutput(result)
        case "c":
            hr.runAdHocCommand()
        case "q":
            question := hr.readLine("Question: ")
            followUp, _ := hr.llm.AskFollowUp(question)
            hr.printFollowUp(followUp)
        }
    }
}
```

---

## 7. State Management

### 7.1 SQLite Schema (Migration V3)

Follows the existing migration pattern from `internal/storage/db.go`:

```go
const migrationV3 = `
-- Workflow runs
CREATE TABLE IF NOT EXISTS workflow_runs (
  run_id TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  workflow_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  started_at_unix_ms INTEGER NOT NULL,
  ended_at_unix_ms INTEGER,
  duration_ms INTEGER,
  params_json TEXT,
  error TEXT,

  CHECK(status IN ('pending','running','success','failed','cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_wf_runs_name_time
  ON workflow_runs(workflow_name, started_at_unix_ms DESC);
CREATE INDEX IF NOT EXISTS idx_wf_runs_time
  ON workflow_runs(started_at_unix_ms DESC);

-- Workflow step results
CREATE TABLE IF NOT EXISTS workflow_steps (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id) ON DELETE CASCADE,
  job_name TEXT NOT NULL,
  step_id TEXT NOT NULL,
  step_name TEXT NOT NULL,
  matrix_key TEXT,
  command TEXT,
  resolved_command TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  started_at_unix_ms INTEGER,
  ended_at_unix_ms INTEGER,
  duration_ms INTEGER,
  exit_code INTEGER,
  stdout_tail TEXT,
  stderr_tail TEXT,
  error TEXT,

  CHECK(status IN ('pending','running','success','failed','skipped','cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_wf_steps_run
  ON workflow_steps(run_id);

-- LLM analysis results
CREATE TABLE IF NOT EXISTS workflow_analyses (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id) ON DELETE CASCADE,
  step_id TEXT NOT NULL,
  matrix_key TEXT,
  analysis_prompt TEXT NOT NULL,
  output_sent TEXT,
  decision TEXT NOT NULL,
  reasoning TEXT,
  flags_json TEXT,
  transcript_json TEXT,
  human_decision TEXT,
  created_at_unix_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_wf_analyses_run
  ON workflow_analyses(run_id);
`
```

Register in the migration list:

```go
migrations := []struct {
    version int
    sql     string
}{
    {version: 1, sql: migrationV1},
    {version: 2, sql: migrationV2},
    {version: 3, sql: migrationV3},  // ← new
}
```

### 7.2 Store Interface Extensions

New methods on `storage.Store`:

```go
// Workflow Runs
CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error
UpdateWorkflowRun(ctx context.Context, runID string, status string, endTime int64, err string) error
GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error)
ListWorkflowRuns(ctx context.Context, q WorkflowRunQuery) ([]WorkflowRun, error)

// Workflow Steps
CreateWorkflowStep(ctx context.Context, step *WorkflowStep) error
UpdateWorkflowStep(ctx context.Context, id int64, status string, exitCode *int, endTime int64, stdoutTail, stderrTail, errMsg string) error
GetWorkflowSteps(ctx context.Context, runID string) ([]WorkflowStep, error)

// Workflow Analyses
CreateWorkflowAnalysis(ctx context.Context, a *WorkflowAnalysis) error
GetWorkflowAnalyses(ctx context.Context, runID string) ([]WorkflowAnalysis, error)

// Retention
PruneWorkflowRuns(ctx context.Context, maxPerWorkflow int) (int64, error)
```

### 7.3 Storage Types

```go
type WorkflowRun struct {
    RunID           string
    WorkflowName    string
    WorkflowPath    string
    Status          string  // pending, running, success, failed, cancelled
    StartedAtUnixMs int64
    EndedAtUnixMs   *int64
    DurationMs      *int64
    ParamsJSON      string
    Error           string
}

type WorkflowStep struct {
    ID              int64
    RunID           string
    JobName         string
    StepID          string
    StepName        string
    MatrixKey       string  // e.g. "stack=ojin-production"
    Command         string  // original from YAML
    ResolvedCommand string  // after expression evaluation
    Status          string
    StartedAtUnixMs *int64
    EndedAtUnixMs   *int64
    DurationMs      *int64
    ExitCode        *int
    StdoutTail      string  // last 4KB
    StderrTail      string  // last 4KB
    Error           string
}

type WorkflowAnalysis struct {
    ID              int64
    RunID           string
    StepID          string
    MatrixKey       string
    AnalysisPrompt  string
    OutputSent      string  // what was sent to LLM (truncated, scrubbed)
    Decision        string
    Reasoning       string
    FlagsJSON       string  // JSON array of FlaggedItem
    TranscriptJSON  string  // JSON array of follow-up messages
    HumanDecision   string  // "approve", "reject", or empty
    CreatedAtUnixMs int64
}

type WorkflowRunQuery struct {
    WorkflowName string
    Status       string
    Limit        int
    Offset       int
}
```

### 7.4 Log File Layout

Full step output is written to log files (SQLite stores only 4KB tails):

```
~/.clai/workflow-logs/
  <run-id>/
    <job>--<matrix-key>--<step-id>.stdout
    <job>--<matrix-key>--<step-id>.stderr
```

Log files are created by the CLI process. Pruned when corresponding `workflow_runs` rows are deleted.

### 7.5 Retention

- Default: keep last 100 runs per workflow name
- Pruning runs in the daemon's periodic maintenance loop (same pattern as `pruneCacheLoop`)
- When a run is pruned: `DELETE FROM workflow_runs WHERE run_id = ?` cascades to `workflow_steps` and `workflow_analyses`
- Log files are cleaned up separately (scan `~/.clai/workflow-logs/` for orphan directories)

---

## 8. gRPC Extensions

### 8.1 New Protobuf Messages

```protobuf
// ── Workflow State Tracking ──

message WorkflowRunStartRequest {
  string run_id = 1;
  string workflow_name = 2;
  string workflow_path = 3;
  string params_json = 4;
  int64 started_at_unix_ms = 5;
}

message WorkflowRunEndRequest {
  string run_id = 1;
  string status = 2;            // "success", "failed", "cancelled"
  int64 ended_at_unix_ms = 3;
  string error = 4;
}

message WorkflowStepUpdateRequest {
  string run_id = 1;
  string job_name = 2;
  string step_id = 3;
  string step_name = 4;
  string matrix_key = 5;
  string command = 6;
  string resolved_command = 7;
  string status = 8;
  int64 started_at_unix_ms = 9;
  int64 ended_at_unix_ms = 10;
  int32 exit_code = 11;
  string stdout_tail = 12;
  string stderr_tail = 13;
  string error = 14;
}

message WorkflowAnalysisRequest {
  string run_id = 1;
  string step_id = 2;
  string matrix_key = 3;
  string analysis_prompt = 4;
  string output_sent = 5;
  string decision = 6;
  string reasoning = 7;
  string flags_json = 8;
  string transcript_json = 9;
  string human_decision = 10;
  int64 created_at_unix_ms = 11;
}

// ── Workflow Queries ──

message WorkflowStatusRequest {
  string run_id = 1;
}

message WorkflowStatusResponse {
  string run_id = 1;
  string workflow_name = 2;
  string status = 3;
  int64 started_at_unix_ms = 4;
  int64 ended_at_unix_ms = 5;
  int64 duration_ms = 6;
  string error = 7;
  repeated WorkflowStepStatus steps = 8;
}

message WorkflowStepStatus {
  string job_name = 1;
  string step_id = 2;
  string step_name = 3;
  string matrix_key = 4;
  string status = 5;
  int32 exit_code = 6;
  int64 duration_ms = 7;
  string error = 8;
}

message WorkflowHistoryRequest {
  string workflow_name = 1;
  int32 limit = 2;
  int32 offset = 3;
}

message WorkflowHistoryResponse {
  repeated WorkflowRunSummary runs = 1;
}

message WorkflowRunSummary {
  string run_id = 1;
  string workflow_name = 2;
  string status = 3;
  int64 started_at_unix_ms = 4;
  int64 duration_ms = 5;
}

message WorkflowStopRequest {
  string run_id = 1;
}
```

### 8.2 New RPC Methods

```protobuf
service ClaiService {
  // ... existing RPCs ...

  // Workflow state tracking (fire-and-forget from CLI)
  rpc WorkflowRunStart(WorkflowRunStartRequest) returns (Ack);
  rpc WorkflowRunEnd(WorkflowRunEndRequest) returns (Ack);
  rpc WorkflowStepUpdate(WorkflowStepUpdateRequest) returns (Ack);
  rpc WorkflowAnalysisRecord(WorkflowAnalysisRequest) returns (Ack);

  // Workflow queries (interactive)
  rpc WorkflowStatus(WorkflowStatusRequest) returns (WorkflowStatusResponse);
  rpc WorkflowHistory(WorkflowHistoryRequest) returns (WorkflowHistoryResponse);

  // Workflow control
  rpc WorkflowStop(WorkflowStopRequest) returns (Ack);
}
```

### 8.3 Daemon Handler Pattern

New handlers follow the existing pattern from `internal/daemon/handlers.go`:

```go
func (s *Server) WorkflowRunStart(ctx context.Context, req *pb.WorkflowRunStartRequest) (*pb.Ack, error) {
    s.touchActivity()

    run := &storage.WorkflowRun{
        RunID:           req.RunId,
        WorkflowName:    req.WorkflowName,
        WorkflowPath:    req.WorkflowPath,
        Status:          "running",
        StartedAtUnixMs: req.StartedAtUnixMs,
        ParamsJSON:      req.ParamsJson,
    }

    if err := s.store.CreateWorkflowRun(ctx, run); err != nil {
        s.logger.Warn("failed to create workflow run",
            "run_id", req.RunId,
            "error", err,
        )
        return &pb.Ack{Ok: false, Error: err.Error()}, nil
    }

    s.logger.Debug("workflow run started",
        "run_id", req.RunId,
        "workflow", req.WorkflowName,
    )

    return &pb.Ack{Ok: true}, nil
}
```

### 8.4 Stop Signal Mechanism

`WorkflowStop` sets a flag in an in-memory map. The CLI polls via `WorkflowStatus` and checks for a `cancelled` status:

```go
// In daemon:
func (s *Server) WorkflowStop(ctx context.Context, req *pb.WorkflowStopRequest) (*pb.Ack, error) {
    s.touchActivity()
    err := s.store.UpdateWorkflowRun(ctx, req.RunId, "cancelled", time.Now().UnixMilli(), "stopped by user")
    if err != nil {
        return &pb.Ack{Ok: false, Error: err.Error()}, nil
    }
    return &pb.Ack{Ok: true}, nil
}

// In CLI runner:
func (r *Runner) pollStopSignal(ctx context.Context, cancel context.CancelFunc) {
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            status, _ := r.ipcClient.WorkflowStatus(ctx, r.runID)
            if status != nil && status.Status == "cancelled" {
                cancel()
                return
            }
        }
    }
}
```

---

## 9. Security

### 9.1 Expression Injection Prevention

All `${{ }}` expressions are resolved in Go before passing to the shell. The shell receives a fully-resolved command string:

```go
// SAFE: Expression resolved in Go, result passed as single arg to sh -c
resolvedCmd := exprEval.Resolve(step.Run, exprCtx)
cmd := exec.Command("sh", "-c", resolvedCmd)

// UNSAFE (never do this):
// cmd := exec.Command("sh", "-c", step.Run)  // ${{ }} left for shell
```

Step outputs referenced in expressions may contain shell metacharacters. Since Go resolves them before the shell sees the string, and they're embedded within a string already passed to `sh -c`, standard shell quoting within the `run` field handles this:

```yaml
# Safe: the resolved value of steps.build.outputs.path is inserted by Go
run: echo "Build artifact at: ${{ steps.build.outputs.path }}"
```

### 9.2 Secret Scrubbing Pipeline

Before sending output to the LLM:

```go
func scrubSecrets(output string, secrets []string) string {
    for _, secret := range secrets {
        if secret != "" && len(secret) > 3 {
            output = strings.ReplaceAll(output, secret, "***")
        }
    }
    // Also apply existing sanitize.Sanitizer patterns (API keys, tokens, etc.)
    return sanitize.SanitizeText(output)
}
```

Secrets are collected from:

1. Environment variables listed in step `env` that match known secret patterns (e.g., `*_KEY`, `*_SECRET`, `*_TOKEN`, `*_PASSWORD`)
2. Values exported via `$CLAI_OUTPUT` from steps that had `secret: true` on the env var
3. The existing `sanitize.Patterns` from `internal/sanitize/patterns.go`

### 9.3 File Permission Checks

On workflow YAML load:

```go
func checkFilePermissions(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }
    mode := info.Mode()
    if mode&0022 != 0 {
        // Group or other writable
        return fmt.Errorf("workflow file %s is writable by group/other (mode %o); "+
            "this is a security risk — run: chmod go-w %s", path, mode.Perm(), path)
    }
    return nil
}
```

In v0 this is a **warning** printed to stderr. In v1, `workflows.strict_permissions: true` makes it an error.

### 9.4 Resolved Command Logging

Every resolved command is logged to `workflow_steps.resolved_command` **before** execution. This provides a full audit trail of exactly what was executed, including the resolved values of all expressions.

---

## 10. Configuration

### 10.1 New Config Section

```go
// WorkflowsConfig holds workflow-related settings.
type WorkflowsConfig struct {
    Enabled           bool     `yaml:"enabled"`
    SearchPaths       []string `yaml:"search_paths"`        // Additional discovery dirs
    LogDir            string   `yaml:"log_dir"`             // Override log directory
    RetainRuns        int      `yaml:"retain_runs"`         // Max runs per workflow (default: 100)
    StrictPermissions bool     `yaml:"strict_permissions"`  // Error on insecure file perms
}
```

Added to the top-level `Config` struct:

```go
type Config struct {
    Daemon      DaemonConfig      `yaml:"daemon"`
    Client      ClientConfig      `yaml:"client"`
    AI          AIConfig          `yaml:"ai"`
    Suggestions SuggestionsConfig `yaml:"suggestions"`
    Privacy     PrivacyConfig     `yaml:"privacy"`
    History     HistoryConfig     `yaml:"history"`
    Workflows   WorkflowsConfig   `yaml:"workflows"`  // ← new
}
```

### 10.2 Defaults

```go
Workflows: WorkflowsConfig{
    Enabled:           true,
    SearchPaths:       nil,    // use defaults only
    LogDir:            "",     // ~/.clai/workflow-logs
    RetainRuns:        100,
    StrictPermissions: false,
},
```

### 10.3 Config Getter/Setter

Follow the existing `getDaemonField` / `setDaemonField` pattern in `config.go` for the `workflows` section. Add `"workflows"` case to the `Get` and `Set` switch statements.

---

## 11. Error Handling & Recovery

### 11.1 Step Failure (v0)

In v0, any step failure halts the entire matrix entry (implicit fail-fast). The workflow reports `failed` with the failing step's error message.

### 11.2 Daemon Unreachable

CLI continues executing the workflow without state persistence. Step updates are silently dropped. A warning is printed once at the start.

### 11.3 CLI Crash / SIGKILL

- Subprocesses receive SIGHUP (parent process death)
- The daemon retains the `running` status for the orphaned run
- `clai workflow status` will show it as `running` with no recent updates
- `clai workflow stop <run-id>` can mark it `cancelled`
- Future: on daemon startup, detect orphaned runs (no PID alive) and mark `cancelled`

### 11.4 claudemon Crash

If claudemon dies during analysis:

1. CLI detects broken pipe on stdin/stdout
2. Attempts to restart claudemon (once)
3. If restart fails: fall back to claude CLI
4. If claude CLI unavailable: treat as `needs_human` (func spec FR-24)

### 11.5 Resume from Failure (v1)

```bash
clai workflow run --resume <run-id>
```

1. Load previous run's step statuses from SQLite
2. Steps that were `success`: skip, use cached outputs
3. Steps that were `failed`, `skipped`, or `cancelled`: reset to `pending`
4. Create a new `run_id` (for clean state)
5. Re-execute the DAG from the first non-succeeded step

---

## 12. Package Structure & Implementation Plan

### 12.1 New Package: `internal/workflow/`

```
internal/workflow/
  schema.go              # WorkflowDef, JobDef, StepDef types + YAML parsing
  schema_test.go
  validate.go            # Validation rules
  validate_test.go
  matrix.go              # Matrix expansion
  matrix_test.go
  expr.go                # Expression engine (${{ }} evaluation)
  expr_test.go
  executor.go            # Step subprocess execution
  executor_test.go
  runner.go              # Main orchestration loop
  runner_test.go
  llm.go                 # LLM integration (claudemon, fallback)
  llm_test.go
  review.go              # Human review interaction
  review_test.go
  scrub.go               # Secret scrubbing
  scrub_test.go
  discovery.go           # Workflow file discovery
  discovery_test.go
  output.go              # Terminal output formatting
  output_test.go
```

### 12.2 CLI Commands

New file: `internal/cmd/workflow.go`

```go
func newWorkflowCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "workflow",
        Short: "Run and manage workflows",
    }
    cmd.AddCommand(
        newWorkflowRunCmd(),
        newWorkflowValidateCmd(),
        newWorkflowListCmd(),
        newWorkflowStatusCmd(),
        newWorkflowHistoryCmd(),
        newWorkflowStopCmd(),
        newWorkflowLogsCmd(),
    )
    return cmd
}
```

Register in `internal/cmd/root.go` within the existing command group structure.

### 12.3 Implementation Phases

| Phase | Scope | Deliverable |
|-------|-------|-------------|
| **P0-A** | YAML parsing + validation + discovery | `schema.go`, `validate.go`, `discovery.go` + tests |
| **P0-B** | Expression engine (v0 subset) | `expr.go` + tests |
| **P0-C** | Step executor (subprocess, output capture, $CLAI_OUTPUT) | `executor.go` + tests |
| **P0-D** | Matrix expansion | `matrix.go` + tests |
| **P0-E** | Workflow runner (sequential: matrix → steps) | `runner.go` + tests |
| **P0-F** | LLM integration (claudemon + fallback) | `llm.go`, `scrub.go` + tests |
| **P0-G** | Human review (terminal prompts + follow-up) | `review.go` + tests |
| **P0-H** | SQLite migration + gRPC extensions + daemon handlers | `storage/`, `proto/`, `daemon/` |
| **P0-I** | CLI commands (`run`, `validate`) | `cmd/workflow.go` |
| **P0-J** | Terminal output formatting | `output.go` + tests |
| **P0-K** | Integration testing with the Pulumi example workflow | `testdata/workflows/` |

**Parallelizable:** P0-A through P0-D can proceed in parallel. P0-E depends on A-D. P0-F and P0-G can proceed in parallel with P0-E. P0-H can proceed in parallel with P0-E.

---

## 13. Testing Strategy

### 13.1 Unit Tests

| Component | Coverage Target | Key Test Cases |
|-----------|----------------|----------------|
| `schema.go` | 90% | Valid YAML, missing fields, invalid risk_level, unknown keys |
| `validate.go` | 90% | Duplicate step IDs, unknown job refs, cycle detection |
| `matrix.go` | 95% | Simple include, exclude filtering, empty matrix, single entry |
| `expr.go` | 90% | All scopes (env, matrix, steps), nested expressions, unknown scope errors |
| `executor.go` | 85% | Success, failure, timeout, signal handling, output capture |
| `scrub.go` | 90% | Secret patterns, edge cases (empty string, short values) |
| `llm.go` | 80% | Response parsing (valid JSON, malformed, keyword fallback) |
| `discovery.go` | 85% | Local dir, global dir, both, neither, file permissions |

### 13.2 Integration Tests

- End-to-end workflow run with `echo` commands (no LLM)
- Matrix expansion with 3 entries
- Step output passing ($CLAI_OUTPUT → expression resolution)
- LLM analysis with mock claudemon (pre-recorded responses)
- Human review with simulated stdin
- Daemon state persistence (verify SQLite rows after run)

### 13.3 Test Fixtures

```
testdata/workflows/
  simple-chain.yaml          # 3 sequential steps
  matrix-basic.yaml          # Matrix with 2 includes
  output-passing.yaml        # Step output → next step expression
  analysis-step.yaml         # Step with analyze=true
  invalid-schema.yaml        # Missing required fields
  invalid-cycle.yaml         # Circular job dependencies (v1)
  pulumi-example.yaml        # The motivating use case from func spec §6
```

### 13.4 Mock LLM

```go
type MockClaudemon struct {
    responses map[string]*AnalysisResponse  // prompt prefix → response
}

func (m *MockClaudemon) Analyze(req *AnalysisRequest) (*AnalysisResponse, error) {
    for prefix, resp := range m.responses {
        if strings.HasPrefix(req.Prompt, prefix) {
            return resp, nil
        }
    }
    return &AnalysisResponse{Decision: "needs_human", Reasoning: "mock: no matching response"}, nil
}
```

---

## 14. Acceptance Criteria (v0)

- [ ] YAML parsing produces correct `WorkflowDef` for the Pulumi example (func spec §6)
- [ ] Matrix expansion generates correct entries from `include`
- [ ] Steps execute sequentially via `$SHELL -c` with correct environment
- [ ] Step outputs exported via `$CLAI_OUTPUT` resolve in downstream `${{ }}` expressions
- [ ] `analyze: true` steps send output to claudemon and receive structured response
- [ ] Risk level decision matrix correctly determines when to prompt human
- [ ] Human review interface presents approve/reject/inspect/follow-up options
- [ ] Follow-up LLM conversation preserves context
- [ ] Step failure halts remaining steps in the matrix entry
- [ ] Workflow run state persisted in SQLite via daemon gRPC
- [ ] `clai workflow run <file>` executes the full pipeline
- [ ] `clai workflow validate <file>` catches schema errors
- [ ] Secrets are scrubbed from output before LLM analysis
- [ ] Terminal output shows step names, status, and durations
- [ ] All unit tests pass (`go test ./internal/workflow/...`)
- [ ] `make dev` passes

---

## 15. Dagu Pattern Comparison

| Dagu Pattern | Adopted? | clai Implementation | Notes |
|--------------|----------|---------------------|-------|
| YAML workflow files | ✅ Adopted | `WorkflowDef` struct, `yaml.Unmarshal` | Same concept, GHA-inspired syntax instead of dagu syntax |
| Step `command` field | ✅ Adopted | `StepDef.Run`, executed via `os/exec` | Named `run` (GHA style) not `command` (dagu style) |
| Step `depends` / DAG | ✅ Adopted (v1) | Kahn's algorithm, job-level via `needs` | v0 is sequential only |
| Output capture | ✅ Adopted | `$CLAI_OUTPUT` file + `${{ steps.ID.outputs.KEY }}` | GHA-style output mechanism |
| Environment variables | ✅ Adopted | Workflow → job → step env merge | Same precedence model |
| Lifecycle handlers | ✅ Adopted (v1) | `onSuccess`, `onFailure`, `onExit` | Identical semantics |
| `maxActiveSteps` | ✅ Adopted (v1) | Semaphore-bounded goroutines | Same concept |
| Variable expansion | ✅ Adapted | `${{ }}` syntax (GHA-style, not `${VAR}` dagu-style) | Go-side evaluation for safety |
| Preconditions | ✅ Adopted (v1) | Command, env, file checks | Same concept as dagu |
| Chat/LLM executor | ✅ Adapted | `analyze` + `analysis_prompt` + claudemon | Dagu has generic chat; clai has structured analysis |
| Retry with backoff | ⏳ Deferred | v1+ | Dagu has this; not needed for v0 Pulumi use case |
| `continueOn` | ⏳ Deferred | v1+ | Fine-grained failure control |
| Sub-workflows | ⏳ Deferred | v2 | Dagu's child DAG pattern |
| File-based state (status.jsonl) | ❌ Replaced | SQLite (existing clai pattern) | Single source of truth |
| Web UI | ❌ Omitted | CLI-only | TUI possible in v2 |
| Docker/SSH/HTTP executors | ❌ Omitted | Shell commands only | Use `docker run`, `curl`, `ssh` |
| Scheduling (cron) | ❌ Omitted | Use system cron | clai is not a scheduler |
| Queue management | ❌ Omitted | Single run per CLI invocation | No concurrent run coordination |
| **Matrix strategy** | ✅ **clai-specific** | `strategy.matrix` with `include`/`exclude` | From act/GHA, not dagu |
| **Risk levels** | ✅ **clai-specific** | `risk_level` → decision matrix → human prompts | No dagu equivalent |
| **LLM follow-up conversation** | ✅ **clai-specific** | Multi-turn review session during human review | No dagu equivalent |
| **Structured analysis response** | ✅ **clai-specific** | `decision`/`reasoning`/`flags` schema | No dagu equivalent |

---

## 16. Decision Log

| # | Decision | Options | Choice | Rationale |
|---|----------|---------|--------|-----------|
| D1 | Execution location | (a) Daemon (b) CLI + daemon state tracking | **(b)** | Daemon is lightweight with idle timeout. CLI has terminal access for human interaction. |
| D2 | State storage | (a) SQLite (b) SQLite + JSONL (c) JSONL only | **(a)** | SQLite is clai's single source of truth. Log files only for unbounded output. |
| D3 | Output routing | (a) All through daemon (b) CLI captures, sends tails | **(b)** | Avoids routing large output through gRPC. |
| D4 | YAML syntax | (a) Dagu-style (b) GHA-style (c) Custom | **(b)** | GHA is widely known. Matrix strategy from GHA/act is essential for multi-target use cases. |
| D5 | Expression syntax | (a) `${VAR}` dagu-style (b) `${{ }}` GHA-style | **(b)** | GHA-style is unambiguous (no conflict with shell `$VAR`). Evaluated Go-side. |
| D6 | LLM backend | (a) API only (b) claudemon only (c) claudemon + fallback | **(c)** | claudemon for speed (warm session). Fallback for availability. |
| D7 | v0 scope | (a) Full DAG (b) Sequential only | **(b)** | Sequential is sufficient for the Pulumi use case. DAG in v1. |
