# clai Workflow Execution — Technical Specification

**Version:** 2.0
**Date:** 2026-02-11
**Status:** RFC
**Supersedes:** `specs/tech_workflows.md` (v1)
**Companion:** `specs/func_workflows.md` (functional requirements)

> This document specifies **how** to implement the workflow execution feature defined in `func_workflows.md`. It covers architecture, data structures, protocols, and implementation details. Reference the functional spec for *what* the system should do.
>
> Implementation draws inspiration from [dagu-org/dagu](https://github.com/dagu-org/dagu) — a self-contained Go workflow engine with YAML definitions, DAG-based step execution, and structured run artifacts.

## Changelog from v1

- **Cross-platform execution:** Shell-specific `$SHELL -c` replaced with `ShellAdapter` abstraction; argv execution by default, shell mode opt-in (CB-2, CB-4)
- **Security hardening:** Secret lifecycle management, masked command logging, `/proc`-safe execution (CB-5, AR-7)
- **Headless-first architecture:** Explicit execution modes for interactive/CI use, structured exit codes, push-based stop signals (CB-6, AR-1, AR-8)
- **Scope clarity:** Single implementation tiers table replaces scattered v0/v1 annotations (CB-1)
- **Complete specification:** All 45 FRs addressed; missing requirements (dependency detection, concurrency, exit codes) fully specified (CB-7, CB-10)
- **Type consistency:** Schema mismatches (`StepDef.Shell` type, `HumanReview` vs `HumanReviewer`, `*MatrixDef`) corrected (CB-8)
- **Windows filesystem safety:** `sanitizePathComponent()` is platform-aware, handles `: * ? " < > |` (CB-9)
- **Signal handling:** Cross-platform `ProcessController` with Windows job objects, not just Unix signals (CB-3)
- **Architectural improvements:** Config precedence chain (AR-5), no TUI dependency in engine (AR-4, AR-10), OS-specific adapters via build tags (AR-3), structured run artifact (AR-6), concurrency design (AR-9), safer command model (AR-2)

### V1 Review Feedback Traceability

Every finding from the v1 review maps to a v2 section. Nothing is dropped.

| ID | Finding | V2 Section |
|----|---------|------------|
| CB-1 | Scope/version mismatch | §2 Implementation Tiers |
| CB-2 | Cross-platform shell execution | §5 Shell Execution |
| CB-3 | Signal/cancellation Unix-only | §6 Process Control |
| CB-4 | Command injection via `sh -c` | §5 argv-default model |
| CB-5 | Secrets leak via args/DB | §8 Secrets Management |
| CB-6 | Non-interactive/CI undefined | §4 Execution Modes |
| CB-7 | v1 FRs underspecified | §§7-11, 16-17, 22 |
| CB-8 | Type inconsistencies | §§7, 10 |
| CB-9 | Windows filesystem safety | §7.5 sanitizePathComponent |
| CB-10 | No machine-readable log | §12.3 RunArtifact |
| AR-1 | Explicit execution modes | §4 |
| AR-2 | Safer command model | §5 |
| AR-3 | OS-specific adapters | §§5, 6 |
| AR-4 | Headless-first engine | §3 Architecture |
| AR-5 | Config precedence | §14 |
| AR-6 | Structured run artifact | §12.3 |
| AR-7 | Masked secret handling | §8 |
| AR-8 | Push-based stop signals | §12.6 |
| AR-9 | v1 concurrency design | §17 |
| AR-10 | No TUI dependency | §3, §11 |
| MR-1 | Non-interactive/CI mode | §4 Execution Modes |
| MR-2 | Cross-platform signal handling | §6 Process Control |
| MR-3 | Secrets management lifecycle | §8 Secrets Management |
| MR-4 | Exit code specification | §15 Exit Codes |
| MR-5 | Pre-run dependency detection | §16 Dependencies |
| MR-6 | Concurrency/parallel design | §17 Concurrency |
| MR-7 | Structured logging format | §12.3 RunArtifact |
| MR-8 | Windows filesystem handling | §7.5 sanitizePathComponent |
| MR-9 | Config precedence chain | §14 Configuration |
| MR-10 | Shell completion plan | §26.1 Shell Completion |
| DG-1 | Decision behavior matrix | §4.4 Decision Behavior Matrix |
| DG-2 | Cross-platform execution matrix | §5.4 Execution Matrix |
| DG-3 | Secret scrubbing pipeline | §8.5 Scrubbing Pipeline |
| DG-4 | Concurrent workflow isolation | §17.5 Multiple Concurrent Runs |
| DG-5 | CLI flag reference | §22 CLI Commands and Flags |
| DG-6 | Acceptance criteria per tier | §24 Acceptance Criteria |
| DG-7 | Dagu comparison update | §25 Dagu Pattern Comparison |

---

## 1. Terminology and Conventions

### 1.1 RFC 2119 Keywords

The keywords MUST, MUST NOT, SHALL, SHALL NOT, SHOULD, SHOULD NOT, MAY, and OPTIONAL in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 1.2 Glossary

| Term | Definition |
|------|-----------|
| **ShellAdapter** | Platform-specific interface for executing commands (argv or shell mode) |
| **ProcessController** | Platform-specific interface for subprocess lifecycle and signal management |
| **ExecutionMode** | One of: `interactive`, `non-interactive-fail`, `non-interactive-auto` |
| **RunArtifact** | Canonical JSONL file recording every event in a workflow run (FR-40) |
| **SecretStore** | In-memory-only store holding secret values and providing masking |
| **SecretMask** | The process of replacing secret values with `***` before logging or persistence |
| **Tier** | Implementation scope level: Tier 0 (MVP), Tier 1 (Full), Tier 2 (Future) |
| **Matrix entry** | One combination of parameters from a matrix strategy |
| **Wave** | A set of jobs that can execute in parallel (Tier 1) |
| **Claude daemon** | Existing managed Claude CLI process in `internal/claude/daemon.go` |

### 1.3 Cross-References

- `func_workflows.md` — Functional requirements (FR-1 through FR-45)
- `internal/claude/daemon.go` — Claude daemon implementation (LLM fallback chain)
- `internal/sanitize/patterns.go` — Secret detection patterns
- `internal/ipc/spawn_{unix,windows}.go` — Platform-specific process spawning (build tag pattern)
- `internal/config/config.go` — Configuration struct and loading

---

## 2. Implementation Tiers

Every functional requirement (FR-1 through FR-45) is assigned to exactly one tier. Features MUST NOT be implemented partially — either the entire tier's requirements are met, or the feature is deferred.

### 2.1 Tier 0 — MVP (Pulumi Compliance Use Case)

The minimum viable implementation targeting the Pulumi compliance workflow from func spec §6.

| FR | Feature | Notes |
|----|---------|-------|
| FR-1 | YAML workflow files | Single-file workflows |
| FR-2 | GHA-style syntax | jobs, steps, matrix, env |
| FR-3 | clai-specific attributes | analyze, analysis_prompt, risk_level |
| FR-4 | Environment variables | step > job > workflow precedence |
| FR-5 | Host shell execution | Via ShellAdapter |
| FR-6 | Sequential step execution | Single job, sequential steps |
| FR-7 | Stdout/stderr capture | Via limitedBuffer (4KB tails) |
| FR-8 | Step output export | `$CLAI_OUTPUT` file, key=value format |
| FR-9 | Output auto-inheritance | Within same job |
| FR-13 | Matrix strategy | `include` entries only |
| FR-14 | Matrix include/exclude | Sequential execution |
| FR-22 | Step LLM analysis | analyze: true → QueryFast() |
| FR-23 | Structured LLM response | proceed/halt/needs_human + flags |
| FR-24 | Fallback on parse failure | Unparseable → needs_human |
| FR-25 | Risk level decision matrix | low/medium/high modifiers |
| FR-27 | Output truncation for LLM | 100KB with head+tail preservation |
| FR-28 | LLM backends | Claude daemon + CLI fallback |
| FR-30 | Human interaction interface | Terminal prompts |
| FR-31 | Terminal-based interaction | stdin/stdout |
| FR-32 | Human interaction options | approve/reject/inspect/command/question |
| FR-33 | LLM follow-up conversation | Context-preserving follow-up |
| FR-36 | LLM secret scrubbing | Pattern-based before LLM context |
| FR-37 | Expression interpolation | Subset: env, matrix, steps.outputs |
| FR-41 | Human-readable real-time output | Step names, status, durations |
| FR-42 | Persistent run history | SQLite via daemon |
| FR-43 | CLI commands | `run` and `validate` only |

**Tier 0 also includes** (not in FR list but required for correctness):
- Cross-platform ShellAdapter (argv default + shell opt-in)
- Cross-platform ProcessController (signal handling)
- Execution modes (interactive + non-interactive-fail)
- Structured exit codes
- Secret masking in stored resolved commands

### 2.2 Tier 1 — Full Specification

All Tier 0 features plus:

| FR | Feature | Notes |
|----|---------|-------|
| FR-10 | Step timeout | Configurable per-step, exit code 124 |
| FR-11 | Working directory | Per-step `working-directory` |
| FR-12 | Shell configuration | Per-step `shell` override |
| FR-15 | Parallel matrix execution | `max_parallel` semaphore |
| FR-16 | Fail-fast behavior | Configurable `fail_fast` flag |
| FR-17 | CLI matrix subset | `--matrix key:value` filter |
| FR-18 | Job dependencies | `needs` field, DAG resolution |
| FR-19 | Conditional execution | `if` expressions |
| FR-20 | Preconditions | env, command, file checks |
| FR-21 | Lifecycle handlers | onSuccess, onFailure, onExit |
| FR-26 | Cross-step context | context_from / context_for |
| FR-29 | Configurable LLM backend | Per-workflow LLM config |
| FR-34 | Secret loading sources | .secrets file, interactive entry |
| FR-35 | Log secret auto-redaction | SecretStore.Mask in all outputs |
| FR-38 | Expression operators | Comparisons, logical operators for `if` |
| FR-39 | Structured execution log | RunArtifact with full event data |
| FR-40 | Machine-readable log format | JSONL run artifact |
| FR-44 | Workflow parameters via CLI | `-- PARAM=value` arguments |
| FR-45 | Dry-run mode | `--dry-run` shows plan without executing |

**Tier 1 also includes:**
- `non-interactive-auto` execution mode
- Pre-run dependency detection (`requires:` block)
- Full concurrency design (parallel matrix, multi-job DAG waves)
- Resume from failure (`--resume <run-id>`)
- Full CLI command set (list, status, history, stop, logs)

### 2.3 Tier 2 — Future

Deferred beyond this specification:

- Sub-workflows / child DAGs
- Retry with configurable backoff
- `continueOn` failure granularity
- Container execution (Docker)
- Remote execution (SSH)
- Web UI for run monitoring
- Event-triggered scheduling
- Distributed execution

---

## 3. Technical Architecture

### 3.1 Hybrid Execution Model

The workflow system uses a **hybrid** model: the CLI process orchestrates execution, the daemon tracks state, and steps run as subprocesses.

```text
User invokes:  clai workflow run pulumi-compliance-run
                        │
                        ▼
              ┌──────────────────────────┐
              │  CLI Process             │
              │  (clai workflow run)     │
              │                          │
              │  1. Load & parse YAML    │
              │  2. Load secrets         │
              │  3. Check dependencies   │
              │  4. Expand matrix        │
              │  5. Resolve expressions  │
              │  6. Validate DAG         │
              │  7. WorkflowRunStart ────┼──────► ┌────────────────┐
              │     RPC to daemon        │        │  Daemon (claid) │
              │  8. Execute steps via    │        │                 │
              │     ShellAdapter         │        │  Persists:      │
              │  9. WorkflowStepUpdate ──┼──────► │  • run state    │
              │     RPC per state change │        │  • step state   │
              │ 10. LLM analysis via     │        │  • analyses     │
              │     claude daemon        │        │  in SQLite      │
              │ 11. Human prompts via    │        │                 │
              │     InteractionHandler   │        │  Serves:        │
              │ 12. WorkflowRunEnd ──────┼──────► │  • status RPCs  │
              └──────────────────────────┘        │  • history RPCs │
                        │                         │  • stop stream  │
                        │ spawns per step          └────────────────┘
                        ▼
              ┌──────────────────────────┐
              │  Subprocesses            │
              │  (one per step)          │
              │                          │
              │  Managed by              │
              │  ProcessController       │
              │  (platform-specific)     │
              │                          │
              │  stdout → limitedBuffer  │
              │  stderr → limitedBuffer  │
              │  + tee to RunArtifact    │
              └──────────────────────────┘
```

### 3.2 Headless-First Design

The workflow engine MUST NOT depend on terminal I/O directly. All output goes through `io.Writer` interfaces:

```go
// Runner accepts writers, not terminals.
type Runner struct {
    out         io.Writer       // human-readable progress output
    errOut      io.Writer       // error/warning output
    interaction InteractionHandler  // abstracted human interaction
    artifact    *RunArtifact    // structured event log
    // ...
}
```text

This ensures:
- CI/headless execution works without modification
- Testing does not require a terminal
- Future TUI can wrap the engine without changes

### 3.3 Why CLI Orchestrates (Not Daemon)

The daemon (`claid`) is designed as a lightweight state tracker with idle timeout. Running long-lived workflow execution inside the daemon would:

- Conflict with idle timeout behavior (daemon auto-exits after 2 hours; see `defaultIdleTimeout` in `internal/claude/daemon.go`)
- Make the daemon harder to restart/upgrade during a workflow run
- Route potentially large output through gRPC unnecessarily

The CLI process:

- Has direct terminal access for human interaction (when in interactive mode)
- Supports `Ctrl+C` via `context.Context` (existing pattern)
- Lifecycle matches the workflow run lifecycle
- Interacts with Claude daemon directly via `QueryFast()` (see `internal/claude/daemon.go`)

### 3.4 Component Responsibilities

| Component | Responsibilities |
|-----------|-----------------|
| **CLI** (`clai workflow run`) | YAML parsing, expression evaluation, matrix expansion, DAG validation, step execution via ShellAdapter, output capture, LLM interaction, human prompts via InteractionHandler, RunArtifact writing |
| **Daemon** (`claid`) | Persist run/step state in SQLite, serve status/history queries, stream stop signals |
| **ShellAdapter** | Execute commands (argv or shell mode), platform-specific shell selection |
| **ProcessController** | Subprocess lifecycle, signal delivery, process tree management |
| **Claude daemon** (`internal/claude/daemon.go`) | Analyze step output via `QueryFast()`, return structured decisions, support follow-up conversation |
| **InteractionHandler** | Abstract human interaction (terminal or non-interactive implementations) |

### 3.5 Config Precedence

Configuration values are resolved in this order (highest priority first):

1. **CLI flags** — `--mode`, `--matrix`, `--env`, etc.
2. **Environment variables** — `CLAI_WORKFLOW_MODE`, `CLAI_WORKFLOW_*`
3. **Workflow YAML** — `config:` block within the workflow file
4. **User config** — `~/.clai/config.yaml` `workflows:` section
5. **Compiled defaults** — hardcoded in `internal/config/config.go`

### 3.6 Graceful Degradation

If the daemon is unavailable:

1. CLI attempts `ipc.EnsureDaemon()` (existing pattern from `internal/ipc/spawn.go`)
2. If daemon still unreachable: run workflow **without** state persistence
3. Print warning: `"daemon unavailable — run history will not be persisted"`
4. RunArtifact (JSONL file) is always written regardless of daemon availability
5. All other functionality (execution, LLM analysis, human prompts) works normally

---

## 4. Execution Modes

### 4.1 Mode Definitions

```go
// ExecutionMode controls behavior at decision points.
type ExecutionMode int

const (
    // ModeInteractive prompts the user at a terminal for decisions.
    // Requires TTY. Default when stdin is a terminal.
    ModeInteractive ExecutionMode = iota

    // ModeNonInteractiveFail exits with code 5 when a human decision is required.
    // For CI pipelines that should fail loudly on uncertainty.
    ModeNonInteractiveFail

    // ModeNonInteractiveAuto auto-approves low/medium risk decisions.
    // For CI pipelines with pre-vetted workflows. Tier 1 only.
    ModeNonInteractiveAuto
)
```

### 4.2 Mode Selection

Priority order:

1. `--mode <mode>` CLI flag (explicit)
2. `CLAI_WORKFLOW_MODE` environment variable
3. `workflows.default_mode` in config
4. Auto-detection: interactive if TTY detected, non-interactive-fail otherwise

### 4.3 TTY Detection

```go
// TTYDetector determines whether interactive I/O is available.
type TTYDetector interface {
    IsInteractive() bool
}
```text

**Unix implementation** (file: `internal/workflow/tty_unix.go`, build tag `//go:build !windows`):
- Check `os.Stdin.Stat()` for `ModeCharDevice`
- Fallback: attempt to open `/dev/tty` (existing pattern from `cmd/clai-picker/tty_unix.go`)

**Windows implementation** (file: `internal/workflow/tty_windows.go`, build tag `//go:build windows`):
- Use `windows.GetConsoleMode()` on stdin handle

### 4.4 Decision Behavior Matrix

| Decision Point | Interactive | Non-Interactive-Fail | Non-Interactive-Auto (Tier 1) |
|----------------|------------|---------------------|------------------------------|
| risk=low, LLM=proceed | Auto-proceed | Auto-proceed | Auto-proceed |
| risk=low, LLM=needs_human | Auto-proceed | Auto-proceed | Auto-proceed |
| risk=low, LLM=halt | Prompt human | Exit code 5 | Auto-halt, exit 7 |
| risk=medium, LLM=proceed | Auto-proceed | Auto-proceed | Auto-proceed |
| risk=medium, LLM=needs_human | Prompt human | Exit code 5 | Prompt → exit 5 |
| risk=medium, LLM=halt | Prompt human | Exit code 5 | Auto-halt, exit 7 |
| risk=high, LLM=proceed | Prompt human | Exit code 5 | Exit code 5 |
| risk=high, LLM=needs_human | Prompt human | Exit code 5 | Exit code 5 |
| risk=high, LLM=halt | Prompt human | Exit code 5 | Exit code 5 |
| Any, LLM=unparseable | Prompt human (FR-24) | Exit code 5 | Exit code 5 |
| Secret entry (interactive source) | Prompt for input | Exit code 5 | Exit code 5 |

### 4.5 CI Usage Guide

For CI/CD pipelines:

```yaml
# GitHub Actions example
- name: Run compliance check
  env:
    CLAI_WORKFLOW_MODE: non-interactive-fail
    AWS_ACCESS_KEY_ID: ${{ secrets.AWS_KEY }}
  run: clai workflow run pulumi-compliance --require-deps
```

For pre-vetted workflows where auto-approval is acceptable:

```bash
clai workflow run --mode non-interactive-auto --env RISK_OVERRIDE=accepted ...
```text

---

## 5. Cross-Platform Shell Execution

### 5.1 Command Execution Model

Steps execute in one of two modes:

1. **Argv mode (default):** The `run` field is split into an argument vector using platform-appropriate rules. No shell is involved. This prevents command injection.
2. **Shell mode (opt-in):** The `run` field is passed as a single string to a shell interpreter. Required for pipes, redirects, globs, and shell built-ins.

```yaml
steps:
  # Argv mode (default): safe, no injection risk
  - name: list-stacks
    run: pulumi stack ls --json

  # Shell mode (opt-in): needed for pipes
  - name: filter-stacks
    shell: true
    run: pulumi stack ls --json | jq '.[] | .name'

  # Explicit shell selection
  - name: powershell-step
    shell: pwsh
    run: Get-ChildItem -Recurse
```

### 5.2 ShellAdapter Interface

```go
// ShellAdapter executes commands with platform-appropriate mechanics.
type ShellAdapter interface {
    // ExecArgv runs a command as an argv array (no shell). Default mode.
    ExecArgv(ctx context.Context, argv []string, opts ExecOpts) (*StepResult, error)

    // ExecShell runs a command string through a shell interpreter. Opt-in mode.
    ExecShell(ctx context.Context, shell string, command string, opts ExecOpts) (*StepResult, error)

    // DefaultShell returns the default shell for the current platform.
    // Uses shell detection from internal/cmd/shelldetect.go.
    DefaultShell() string

    // SplitCommand splits a command string into argv using platform-appropriate rules.
    SplitCommand(command string) ([]string, error)
}

// ExecOpts holds execution parameters for both modes.
type ExecOpts struct {
    Env         []string      // environment variables (merged)
    Dir         string        // working directory
    Timeout     time.Duration // zero = no timeout
    Stdin       io.Reader     // nil for non-interactive steps
    Stdout      io.Writer     // tee to limitedBuffer + output writer
    Stderr      io.Writer     // tee to limitedBuffer + output writer
    OutputFile  string        // path for $CLAI_OUTPUT
    SecretStore *SecretStore  // for masking in logs
}

// StepResult holds the outcome of a single step execution.
type StepResult struct {
    ExitCode  int
    Stdout    string        // last 4KB via limitedBuffer
    Stderr    string        // last 4KB via limitedBuffer
    Duration  time.Duration
    StartedAt time.Time
}
```text

### 5.3 Platform Implementations

**Unix** (file: `internal/workflow/exec_unix.go`, build tag `//go:build !windows`):

```go
// ExecArgv: exec.CommandContext(ctx, argv[0], argv[1:]...)
// ExecShell: exec.CommandContext(ctx, shell, "-c", command)
// DefaultShell: uses internal/cmd/shelldetect.go → bash, zsh, or fish
// SplitCommand: POSIX shlex tokenization
```

**Windows** (file: `internal/workflow/exec_windows.go`, build tag `//go:build windows`):

```go
// ExecArgv: exec.CommandContext(ctx, argv[0], argv[1:]...)
// ExecShell: for pwsh → exec.CommandContext(ctx, "pwsh", "-Command", command)
//            for cmd → exec.CommandContext(ctx, "cmd", "/C", command)
// DefaultShell: "pwsh" (PowerShell Core), fallback to "cmd"
// SplitCommand: CommandLineToArgvW semantics
```text

### 5.4 Cross-Platform Execution Matrix

| Platform | Default Shell | Argv Execution | Shell Invocation | Notes |
|----------|--------------|----------------|------------------|-------|
| Linux | bash | `exec.Command(argv[0], argv[1:]...)` | `bash -c "command"` | Detected via `internal/cmd/shelldetect.go` |
| macOS | zsh | `exec.Command(argv[0], argv[1:]...)` | `zsh -c "command"` | Detected via parent process |
| Windows | pwsh | `exec.Command(argv[0], argv[1:]...)` | `pwsh -Command "command"` | Fallback: `cmd /C` |
| Any (fish) | fish | `exec.Command(argv[0], argv[1:]...)` | `fish -c "command"` | When detected or explicit |

### 5.5 Argv Splitting Rules

For argv mode, the `run` field is split into tokens:

**POSIX (Linux/macOS):**
- Whitespace-delimited tokens
- Single quotes preserve literal content: `'hello world'` → one token
- Double quotes allow variable expansion: `"$HOME/bin"` → one token (but `$HOME` is NOT expanded by Go — expressions use `${{ }}`)
- Backslash escapes: `hello\ world` → one token

**Windows:**
- Follows `CommandLineToArgvW` convention
- Double quotes group tokens: `"C:\Program Files\tool"` → one token
- Backslash escaping follows Windows rules

### 5.6 limitedBuffer

```go
// limitedBuffer is a ring buffer that retains the last maxSize bytes.
// Used to capture step stdout/stderr tails for storage without unbounded memory.
const defaultBufferSize = 4096 // 4KB

type limitedBuffer struct {
    buf     []byte
    maxSize int
    pos     int
    full    bool
}

func newLimitedBuffer(size int) *limitedBuffer {
    return &limitedBuffer{buf: make([]byte, size), maxSize: size}
}

func (lb *limitedBuffer) Write(p []byte) (int, error) { /* ring buffer write */ }
func (lb *limitedBuffer) String() string              { /* linearize ring buffer */ }
```

---

## 6. Signal Handling and Process Control

### 6.1 ProcessController Interface

```go
// ProcessController manages subprocess lifecycle with platform-appropriate signals.
type ProcessController interface {
    // Start begins a process and returns a handle for later control.
    Start(ctx context.Context, cmd *exec.Cmd) (ProcessHandle, error)

    // GracefulStop sends a graceful termination signal, waits up to grace period.
    // Returns nil if process exits within grace period.
    GracefulStop(handle ProcessHandle, grace time.Duration) error

    // ForceKill immediately terminates the process and its entire process tree.
    ForceKill(handle ProcessHandle) error
}

// ProcessHandle is an opaque reference to a running process.
type ProcessHandle interface {
    Wait() error
    Pid() int
}
```text

### 6.2 Unix Implementation

File: `internal/workflow/process_unix.go` (build tag `//go:build !windows`)

```go
// Start sets Setpgid: true to create a process group
// (matching existing pattern in internal/ipc/spawn_unix.go).
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

// GracefulStop sends SIGTERM to the process group (-pgid).
syscall.Kill(-pgid, syscall.SIGTERM)
// Wait up to grace period, then ForceKill.

// ForceKill sends SIGKILL to the process group.
syscall.Kill(-pgid, syscall.SIGKILL)
```

### 6.3 Windows Implementation

File: `internal/workflow/process_windows.go` (build tag `//go:build windows`)

```go
// Start creates a Windows Job Object to track the process tree.
// Sets CREATE_NEW_PROCESS_GROUP (matching internal/ipc/spawn_windows.go).
cmd.SysProcAttr = &syscall.SysProcAttr{
    CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
}
// Assign process to job object for tree management.

// GracefulStop sends CTRL_BREAK_EVENT via GenerateConsoleCtrlEvent.
// Wait up to grace period, then ForceKill.

// ForceKill terminates the job object (kills entire process tree).
windows.TerminateJobObject(jobHandle, 1)
```text

### 6.4 Ctrl+C Behavior Matrix

| Phase | First Ctrl+C | Second Ctrl+C | Context |
|-------|-------------|---------------|---------|
| **Step executing** | GracefulStop subprocess (SIGTERM/CTRL_BREAK, 5s grace) | ForceKill immediately | `ctx` passed to `exec.CommandContext` |
| **Human review prompt** | Cancel review, mark step `cancelled` | Force exit process | Review loop checks `ctx.Done()` |
| **LLM analysis** | Cancel LLM query, treat as `needs_human` | Force exit process | `QueryFast` respects context cancellation |
| **Between steps** | Skip remaining steps, run `onExit` handler if defined | Force exit process | Runner checks `ctx.Done()` before each step |

### 6.5 Runner Signal Setup

```go
func (r *Runner) Run(ctx context.Context, wf *WorkflowDef) error {
    // Signal-aware context (matching pattern from cmd/clai-shim/main.go)
    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
    defer cancel()

    // Subscribe to daemon stop signals via streaming RPC
    go r.watchStopSignal(ctx, cancel)

    // Execute steps, checking ctx.Done() between each
    for _, step := range steps {
        select {
        case <-ctx.Done():
            r.markRemainingCancelled(steps)
            return &WorkflowCancelledError{Reason: ctx.Err().Error()}
        default:
            if err := r.executeStep(ctx, step); err != nil {
                return err
            }
        }
    }
    return nil
}
```

---

## 7. YAML Parsing and Validation

### 7.1 Go Type Definitions

```go
// WorkflowDef is the top-level workflow file.
type WorkflowDef struct {
    Name     string                `yaml:"name"`
    Env      map[string]string     `yaml:"env"`
    Secrets  []SecretDef           `yaml:"secrets"`
    Requires []string              `yaml:"requires"`
    Config   *WorkflowConfigBlock  `yaml:"config"`
    Jobs     map[string]*JobDef    `yaml:"jobs"`

    // Tier 1
    OnSuccess *HandlerDef `yaml:"onSuccess"`
    OnFailure *HandlerDef `yaml:"onFailure"`
    OnExit    *HandlerDef `yaml:"onExit"`
}

// WorkflowConfigBlock holds per-workflow config overrides.
type WorkflowConfigBlock struct {
    Mode      string `yaml:"mode"`       // execution mode override
    Shell     string `yaml:"shell"`      // default shell override
    LogDir    string `yaml:"log_dir"`    // log directory override
}

// SecretDef defines a secret to be loaded before execution.
type SecretDef struct {
    Name   string `yaml:"name"`             // env var name
    From   string `yaml:"from"`             // "env" (Tier 0), "file", "interactive" (Tier 1)
    Path   string `yaml:"path,omitempty"`   // for "file" source
    Prompt string `yaml:"prompt,omitempty"` // for "interactive" source
}

// JobDef represents a single job within a workflow.
type JobDef struct {
    Name     string            `yaml:"name"`
    Needs    []string          `yaml:"needs"`
    Env      map[string]string `yaml:"env"`
    Strategy *StrategyDef      `yaml:"strategy"`
    Steps    []*StepDef        `yaml:"steps"`

    // Tier 1
    If        string      `yaml:"if"`
    OnSuccess *HandlerDef `yaml:"onSuccess"`
    OnFailure *HandlerDef `yaml:"onFailure"`
    OnExit    *HandlerDef `yaml:"onExit"`
}

// StrategyDef controls matrix expansion and parallelism.
type StrategyDef struct {
    Matrix      *MatrixDef `yaml:"matrix"`
    MaxParallel int        `yaml:"max_parallel"` // Tier 1; 0 = unlimited
    FailFast    *bool      `yaml:"fail_fast"`    // default: true
}

// MatrixDef defines parameter combinations.
type MatrixDef struct {
    // Tier 0: explicit include entries only
    Include []map[string]string `yaml:"include"`
    Exclude []map[string]string `yaml:"exclude"`
    // Tier 1: cartesian product from top-level keys
    // e.g., os: [linux, darwin] → auto-expanded
}

// StepDef represents a single step within a job.
type StepDef struct {
    ID             string            `yaml:"id"`
    Name           string            `yaml:"name"`
    Run            string            `yaml:"run"`
    Env            map[string]string `yaml:"env"`
    Shell          *bool             `yaml:"shell"`              // nil = argv (default), true = use default shell
    ShellName      string            `yaml:"shell_name"`         // explicit: "bash", "zsh", "pwsh", "fish"
    WorkingDir     string            `yaml:"working_directory"`  // Tier 1 (FR-12)
    TimeoutMinutes *int              `yaml:"timeout_minutes"`    // Tier 1 (FR-10)
    Analyze        bool              `yaml:"analyze"`
    AnalysisPrompt string            `yaml:"analysis_prompt"`
    RiskLevel      string            `yaml:"risk_level"`         // "low", "medium", "high"
    ContextFrom    string            `yaml:"context_from"`       // Tier 1 (FR-26)
    ContextFor     string            `yaml:"context_for"`        // Tier 1 (FR-26)
    If             string            `yaml:"if"`                 // Tier 1 (FR-19)

    // Runtime fields (not from YAML)
    ResolvedArgv    []string `yaml:"-"` // for argv mode
    ResolvedCommand string   `yaml:"-"` // for shell mode (ALWAYS masked before storage)
    ResolvedEnv     []string `yaml:"-"`
}

// HandlerDef is a step to run on lifecycle events (Tier 1).
type HandlerDef struct {
    Run   string `yaml:"run"`
    Shell *bool  `yaml:"shell"`
}

// LLMConfig represents workflow-level LLM configuration.
type LLMConfig struct {
    Backend   string `yaml:"backend"`     // "daemon" (default), "claude-cli", "api"
    Provider  string `yaml:"provider"`    // for API backend: "anthropic", "openai"
    Model     string `yaml:"model"`
    APIKeyEnv string `yaml:"api_key_env"`
}
```text

### 7.2 Parsing Pipeline

```go
func ParseWorkflow(path string) (*WorkflowDef, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read workflow: %w", err)
    }

    var wf WorkflowDef
    if err := yaml.Unmarshal(data, &wf); err != nil {
        return nil, fmt.Errorf("parse YAML: %w", err)
    }

    return &wf, nil
}
```

### 7.3 Validation Rules

Validation runs as a separate pass, collecting all errors before returning:

**Tier 0 rules:**
- Workflow MUST have at least one job
- Each job MUST have at least one step
- Each step MUST have a `run` field (non-empty)
- Step `id` MUST be unique within a job (defaults to step index if omitted)
- `risk_level` MUST be one of: `low`, `medium`, `high` (default: `medium`)
- `analyze: true` MUST have a non-empty `analysis_prompt`
- `${{ }}` expressions MUST reference valid scopes (env, matrix, steps)
- Secrets `from` MUST be one of: `env`, `file`, `interactive`
- `requires` entries MUST NOT be empty strings

**Tier 1 additional rules:**
- Job `needs` MUST NOT form cycles (validated via Kahn's algorithm)
- `if` expressions MUST parse as valid expressions
- `timeout_minutes` MUST be positive if set
- `max_parallel` MUST be non-negative (0 = unlimited)
- Matrix `exclude` entries MUST match at least one `include` entry's keys

### 7.4 File Discovery

Workflows are discovered from these locations (in order):

1. Direct path: `clai workflow run ./path/to/workflow.yaml`
2. Name lookup: `clai workflow run my-workflow` searches:
   - `./.clai/workflows/my-workflow.yaml`
   - `~/.clai/workflows/my-workflow.yaml`
   - Additional paths from `workflows.search_paths` config

### 7.5 Filename Sanitization

Log file paths and artifact directories use step IDs and matrix keys. These MUST be sanitized:

```go
// sanitizePathComponent makes a string safe for use in filenames on all platforms.
func sanitizePathComponent(s string) string {
    // Replace characters unsafe on any platform:
    //   / \ .. : * ? " < > | and non-printable chars (< 0x20)
    // Also replace Windows-reserved names: CON, PRN, AUX, NUL, COM1-9, LPT1-9
    // Truncate to 200 characters
    // Platform-aware via runtime.GOOS for additional restrictions
}
```text

---

## 8. Secrets Management

### 8.1 Secret Lifecycle

```
Workflow YAML          SecretStore           Process Env        DB / Logs
┌──────────────┐      ┌─────────────┐      ┌──────────┐      ┌──────────┐
│ secrets:     │      │ In-memory   │      │ Env vars │      │ Masked   │
│   - name: X  │──1──►│ name→value  │──2──►│ X=secret │      │ X=***    │
│     from: env│      │ (never      │      │ (per     │      │ resolved │
│              │      │  persisted) │      │  process)│      │ =masked  │
└──────────────┘      └──────┬──────┘      └──────────┘      └──────────┘
                             │                                     ▲
                             │ Mask()                               │
                             └────────3────────────────────────────┘
```text

1. **Load:** Secrets loaded from sources (env, file, interactive) into `SecretStore`
2. **Inject:** Secret values set as env vars for subprocess execution
3. **Mask:** All output, logs, and DB fields run through `SecretStore.Mask()` before persistence

### 8.2 SecretStore

```go
// SecretStore holds secret values in memory and provides masking.
// Values are NEVER written to disk, database, or log files.
type SecretStore struct {
    mu      sync.RWMutex
    secrets map[string]string // name → value
}

// Load populates secrets from the workflow's secrets definitions.
func (ss *SecretStore) Load(defs []SecretDef, mode ExecutionMode) error {
    for _, def := range defs {
        switch def.From {
        case "env":
            val := os.Getenv(def.Name)
            if val == "" {
                return fmt.Errorf("secret %q: environment variable not set", def.Name)
            }
            ss.secrets[def.Name] = val

        case "file": // Tier 1
            val, err := loadSecretFromFile(def.Path, def.Name)
            if err != nil {
                return fmt.Errorf("secret %q: %w", def.Name, err)
            }
            ss.secrets[def.Name] = val

        case "interactive": // Tier 1
            if mode != ModeInteractive {
                return fmt.Errorf("secret %q requires interactive mode (from: interactive)", def.Name)
            }
            val, err := readSecureInput(def.Prompt)
            if err != nil {
                return fmt.Errorf("secret %q: %w", def.Name, err)
            }
            ss.secrets[def.Name] = val
        }
    }
    return nil
}

// Mask replaces all known secret values in a string with "***".
// Also applies pattern-based detection from internal/sanitize/patterns.go.
func (ss *SecretStore) Mask(s string) string {
    ss.mu.RLock()
    defer ss.mu.RUnlock()
    for _, val := range ss.secrets {
        if val != "" && len(val) > 3 {
            s = strings.ReplaceAll(s, val, "***")
        }
    }
    // Also apply pattern-based sanitization (AWS keys, JWTs, etc.)
    return sanitize.SanitizeText(s)
}

// EnvVars returns secret values as env var assignments for subprocess injection.
func (ss *SecretStore) EnvVars() []string {
    ss.mu.RLock()
    defer ss.mu.RUnlock()
    vars := make([]string, 0, len(ss.secrets))
    for name, val := range ss.secrets {
        vars = append(vars, name+"="+val)
    }
    return vars
}
```

### 8.3 .secrets File Format (Tier 1)

Dotenv format, loaded from the workflow file's directory:

```text
# Comments start with #
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
DEPLOY_TOKEN=your-deploy-token-here
```

### 8.4 /proc Protection

**Argv mode (default):** The command visible in `/proc/PID/cmdline` is the program and its arguments (e.g., `pulumi stack ls --json`). Secret values are only in environment variables, which are not visible to other users on modern Linux (requires `CAP_SYS_PTRACE`).

**Shell mode:** The command visible in `/proc/PID/cmdline` is `bash -c "the command string"`. If the `run` field contains `${{ env.SECRET }}` expressions, the resolved value would be visible. To prevent this:
- Expressions referencing secrets SHOULD use env var references (`$SECRET`) instead of `${{ env.SECRET }}`
- The expression evaluator emits a warning if a secret-valued expression is resolved into a shell-mode command

### 8.5 Secret Scrubbing Pipeline

`SecretStore.Mask()` is applied in **three places**:

1. **Before storing** `stdout_tail` / `stderr_tail` in SQLite (FR-35)
2. **Before sending** output to the LLM for analysis (FR-36)
3. **Before writing** `resolved_command` to SQLite or RunArtifact

The scrubbing combines:
- Known secret values from `SecretStore`
- Pattern-based detection from `internal/sanitize/patterns.go` (9 pattern types: AWS, JWT, Slack, PEM, GitHub, bearer, basic auth, generic, private keys)

---

## 9. Expression Engine

### 9.1 Syntax

`${{ <expression> }}` — GHA-compatible expression syntax (FR-37).

### 9.2 Supported Scopes

| Scope | Example | Tier | Source |
|-------|---------|------|--------|
| Environment | `${{ env.AWS_REGION }}` | 0 | Merged env vars |
| Matrix | `${{ matrix.stack }}` | 0 | Current matrix entry |
| Step outputs | `${{ steps.assume-role.outputs.ACCESS_KEY }}` | 0 | Parsed from `$CLAI_OUTPUT` |
| Step status | `${{ steps.deploy.outcome }}` | 0 | `"success"`, `"failure"`, `"skipped"` |
| Analysis | `${{ steps.check.analysis.decision }}` | 0 | `"proceed"`, `"halt"`, `"needs_human"` |
| Variables | `${{ vars.REGION }}` | 1 | Workflow-level vars |
| Job results | `${{ jobs.build.result }}` | 1 | `"success"`, `"failure"` |

### 9.3 Evaluation

```go
var exprPattern = regexp.MustCompile(`\$\{\{\s*(.+?)\s*\}\}`)

func (e *ExprEvaluator) Resolve(template string, ctx *ExprContext) (string, error) {
    var resolveErr error
    result := exprPattern.ReplaceAllStringFunc(template, func(match string) string {
        inner := exprPattern.FindStringSubmatch(match)[1]
        val, err := e.evaluate(inner, ctx)
        if err != nil {
            resolveErr = fmt.Errorf("unresolved expression %s: %w", match, err)
            return match
        }
        return val
    })
    if resolveErr != nil {
        // Hard error: MUST NOT pass unresolved expressions to shell/argv.
        return "", resolveErr
    }
    return result, nil
}

type ExprContext struct {
    Env    map[string]string
    Matrix map[string]string
    Steps  map[string]*StepOutputs
    Vars   map[string]string         // Tier 1
}

type StepOutputs struct {
    Outputs  map[string]string
    Outcome  string              // "success", "failure", "skipped"
    Analysis *AnalysisResponse   // if analyze=true (defined in §10.3)
}
```text

### 9.4 Expression Safety

- All `${{ }}` expressions are resolved in Go **before** the command string is passed to ShellAdapter
- Unresolved expressions are a **hard error** — execution stops, never passes broken `${{ }}` to a subprocess
- Step outputs used in expressions are inserted at the Go level (not shell-interpreted)
- In argv mode, resolved values become individual argv elements — no shell interpretation occurs

### 9.5 Output File Format ($CLAI_OUTPUT)

Steps export outputs by writing to a temporary file referenced by `$CLAI_OUTPUT`:

**Simple format:** `KEY=value` (one per line)

**Multiline format (Tier 1):** Heredoc-style delimiters (matching GitHub Actions):

```
SIMPLE_KEY=simple_value
MULTILINE_KEY<<EOF
line 1
line 2
line 3
EOF
```text

```go
// parseOutputFile reads a dotenv-style file with heredoc support for multiline values.
func parseOutputFile(path string) (map[string]string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // step didn't write outputs — not an error
        }
        return nil, err
    }
    outputs := make(map[string]string)
    lines := strings.Split(string(data), "\n")
    for i := 0; i < len(lines); i++ {
        line := strings.TrimSpace(lines[i])
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        // Check for heredoc: KEY<<DELIMITER
        if parts := strings.SplitN(line, "<<", 2); len(parts) == 2 && strings.Contains(parts[0], "") {
            key := strings.TrimSpace(parts[0])
            delimiter := strings.TrimSpace(parts[1])
            var value strings.Builder
            i++
            for i < len(lines) && strings.TrimSpace(lines[i]) != delimiter {
                if value.Len() > 0 {
                    value.WriteByte('\n')
                }
                value.WriteString(lines[i])
                i++
            }
            outputs[key] = value.String()
            continue
        }
        // Simple KEY=value
        if k, v, ok := strings.Cut(line, "="); ok {
            outputs[strings.TrimSpace(k)] = strings.TrimSpace(v)
        }
    }
    return outputs, nil
}
```

### 9.6 v0 Expression Subset

For Tier 0: `${{ env.NAME }}`, `${{ matrix.KEY }}`, `${{ steps.ID.outputs.KEY }}`, `${{ steps.ID.outcome }}`, `${{ steps.ID.analysis.decision }}`. Comparisons and logical operators (`if` expressions) are deferred to Tier 1 (FR-38).

---

## 10. LLM Integration

### 10.1 Existing Claude Daemon (FR-28)

The "claudemon" concept from the functional spec is **already implemented** as the Claude daemon in `internal/claude/daemon.go`. It manages a persistent Claude CLI process (`claude --print --verbose --model haiku --input-format stream-json --output-format stream-json`) and communicates via Unix socket IPC.

**Existing types (from `internal/claude/daemon.go`):**

- `claudeProcess` — manages the persistent Claude CLI subprocess
- `DaemonRequest{Prompt}` / `DaemonResponse{Result, Error}` — JSON protocol over Unix socket
- `QueryViaDaemon(ctx, prompt)` — sends prompt to daemon, returns result
- `QueryFast(ctx, prompt)` — tries daemon first, falls back to `QueryWithContext` (one-shot CLI)

### 10.2 Analysis Prompt Format

The prompt sent to the LLM combines the user's `analysis_prompt` with the scrubbed step output:

```go
func buildAnalysisPrompt(step *StepDef, scrubbedOutput string) string {
    return fmt.Sprintf(`%s

--- STEP OUTPUT (stdout) ---
%s
--- END STEP OUTPUT ---

Respond with ONLY a JSON object in this exact format:
{
  "decision": "proceed" | "halt" | "needs_human",
  "reasoning": "<brief explanation>",
  "flags": [{"item": "<what>", "severity": "info|warning|critical", "reason": "<why>"}]
}`, step.AnalysisPrompt, scrubbedOutput)
}
```text

### 10.3 Workflow-Specific Types

```go
// AnalysisResponse is the structured decision from LLM analysis of step output.
type AnalysisResponse struct {
    Decision  string        `json:"decision"`  // "proceed", "halt", "needs_human"
    Reasoning string        `json:"reasoning"`
    Flags     []FlaggedItem `json:"flags,omitempty"`
}

type FlaggedItem struct {
    Item     string `json:"item"`
    Severity string `json:"severity"` // "info", "warning", "critical"
    Reason   string `json:"reason"`
}

func isValidDecision(d string) bool {
    switch d {
    case "proceed", "halt", "needs_human":
        return true
    }
    return false
}
```

### 10.4 Fallback Chain

`QueryFast()` in `internal/claude/daemon.go` already implements the first two levels:

1. **Claude daemon** (preferred): warm session via `QueryViaDaemon()`, fast response, Unix socket IPC
2. **claude CLI**: one-shot `claude --print` via `QueryWithContext()` in `internal/claude/claude.go`
3. **API provider**: direct Anthropic/OpenAI API call via `internal/provider/` (Tier 1)

If all backends fail, treat the analysis as `needs_human` (FR-24).

### 10.5 Structured Response Parsing

```go
// parseAnalysisResponse extracts a structured decision from the LLM response.
// Per FR-24: if the response cannot be parsed, treat as needs_human.
func parseAnalysisResponse(raw string) (*AnalysisResponse, error) {
    var resp AnalysisResponse
    if err := json.Unmarshal([]byte(raw), &resp); err == nil {
        if isValidDecision(resp.Decision) {
            return &resp, nil
        }
    }
    // FR-24: unparseable response → needs_human (never guess the decision)
    return &AnalysisResponse{
        Decision:  "needs_human",
        Reasoning: raw,
    }, nil
}
```text

### 10.6 Context Building and Truncation (FR-27)

```go
const maxOutputForLLM = 100 * 1024 // 100KB (~25k tokens)

func buildAnalysisContext(step *StepDef, result *StepResult, secretStore *SecretStore) string {
    output := result.Stdout
    if result.Stderr != "" {
        output += "\n--- STDERR ---\n" + result.Stderr
    }

    // 1. Scrub secrets (both known values and pattern-based)
    output = secretStore.Mask(output)

    // 2. Truncate with head+tail preservation
    marker := "\n\n... [truncated: middle section omitted, showing first and last portions] ...\n\n"
    minSizeForTruncation := maxOutputForLLM + len(marker)
    if len(output) > minSizeForTruncation {
        headSize := maxOutputForLLM * 40 / 100
        tailSize := maxOutputForLLM * 40 / 100
        output = output[:headSize] + marker + output[len(output)-tailSize:]
    } else if len(output) > maxOutputForLLM {
        output = output[:maxOutputForLLM]
    }

    return output
}
```

### 10.7 Risk Level Decision Matrix (FR-25)

Fully specified for all combinations:

| risk_level | LLM: proceed | LLM: needs_human | LLM: halt | LLM: unparseable (FR-24) |
|------------|-------------|-------------------|-----------|--------------------------|
| **low** | auto-proceed | auto-proceed | prompt human | auto-proceed |
| **medium** | auto-proceed | prompt human | prompt human | prompt human |
| **high** | prompt human | prompt human | prompt human | prompt human |

```go
func shouldPromptHuman(riskLevel string, decision string) bool {
    switch riskLevel {
    case "low":
        return decision == "halt"
    case "high":
        return true
    default: // "medium" or unset (default to medium)
        return decision != "proceed"
    }
}
```text

### 10.8 LLM Client Interface

```go
// Message represents a single turn in an LLM conversation.
type Message struct {
    Role    string // "user" or "assistant"
    Content string
}

// LLMClient abstracts LLM interaction for testability.
// Production implementation wraps claude.QueryFast().
type LLMClient interface {
    Query(ctx context.Context, prompt string) (string, error)
    Converse(ctx context.Context, transcript []Message) (*AnalysisResponse, error)
}
```

### 10.9 Follow-Up Conversation (FR-33)

```go
type ReviewSession struct {
    client     LLMClient
    transcript []Message
    stepOutput string
}

func (rs *ReviewSession) AskFollowUp(ctx context.Context, question string) (*AnalysisResponse, error) {
    rs.transcript = append(rs.transcript, Message{Role: "user", Content: question})
    resp, err := rs.client.Converse(ctx, rs.transcript)
    if err != nil {
        return nil, err
    }
    rs.transcript = append(rs.transcript, Message{Role: "assistant", Content: resp.Reasoning})
    return resp, nil
}
```text

---

## 11. Human Interaction

### 11.1 InteractionHandler Interface

```go
// InteractionHandler abstracts human interaction for different execution modes.
type InteractionHandler interface {
    // Review presents analysis results and returns a decision.
    Review(ctx context.Context, step *StepDef, result *StepResult, analysis *AnalysisResponse) (Decision, error)

    // Confirm presents a simple yes/no prompt.
    Confirm(ctx context.Context, message string) (bool, error)
}

type Decision int

const (
    DecisionApprove Decision = iota
    DecisionReject
)
```

### 11.2 Terminal Implementation (Interactive Mode)

```go
// TerminalReviewer implements InteractionHandler for interactive terminals.
type TerminalReviewer struct {
    in      io.Reader      // os.Stdin
    out     io.Writer      // os.Stdout
    llm     *ReviewSession // for follow-up questions
}

func (hr *TerminalReviewer) Review(ctx context.Context, step *StepDef, result *StepResult, analysis *AnalysisResponse) (Decision, error) {
    hr.printSummary(step, result, analysis)

    for {
        select {
        case <-ctx.Done():
            return DecisionReject, ctx.Err()
        default:
        }

        hr.prompt("[a]pprove / [r]eject / [i]nspect full output / [c]ommand / [q]uestion: ")
        choice := hr.readLine()
        switch strings.ToLower(strings.TrimSpace(choice)) {
        case "a":
            return DecisionApprove, nil
        case "r":
            return DecisionReject, nil
        case "i":
            hr.printFullOutput(result)
        case "c":
            hr.runAdHocCommand(ctx)
        case "q":
            question := hr.readLine("Question: ")
            followUp, err := hr.llm.AskFollowUp(ctx, question)
            if err != nil {
                fmt.Fprintf(hr.out, "LLM error: %v\n", err)
                continue
            }
            hr.printFollowUp(followUp)
        }
    }
}

// runAdHocCommand lets the user run an arbitrary shell command during review.
// Inherits the step's environment and working directory.
// Output shown on terminal but NOT persisted to workflow run state.
func (hr *TerminalReviewer) runAdHocCommand(ctx context.Context) {
    cmdStr := hr.readLine("$ ")
    // Uses ShellAdapter.ExecShell (always shell mode for ad-hoc commands)
    // Stdout/Stderr go directly to hr.out
}
```text

### 11.3 Non-Interactive Implementation

```go
// NonInteractiveHandler implements InteractionHandler for CI/headless execution.
type NonInteractiveHandler struct {
    mode   ExecutionMode
    out    io.Writer
}

func (ni *NonInteractiveHandler) Review(ctx context.Context, step *StepDef, result *StepResult, analysis *AnalysisResponse) (Decision, error) {
    // Log the analysis for audit trail
    fmt.Fprintf(ni.out, "[non-interactive] step=%s decision=%s risk=%s\n",
        step.Name, analysis.Decision, step.RiskLevel)

    if ni.mode == ModeNonInteractiveFail {
        return DecisionReject, &NeedsHumanError{Step: step.Name, Decision: analysis.Decision}
    }

    // ModeNonInteractiveAuto: auto-approve if risk allows (see §4.4 behavior matrix)
    if step.RiskLevel == "low" || (step.RiskLevel == "medium" && analysis.Decision == "proceed") {
        fmt.Fprintf(ni.out, "[auto-approve] risk=%s decision=%s\n", step.RiskLevel, analysis.Decision)
        return DecisionApprove, nil
    }

    return DecisionReject, &NeedsHumanError{Step: step.Name, Decision: analysis.Decision}
}
```

### 11.4 Interactive Stdin for Steps

Some steps may require interactive user input (e.g., `read -p "Continue?"` in bash). The `ExecOpts.Stdin` field controls this:

- **Interactive mode:** `Stdin = os.Stdin` (user can interact with subprocess)
- **Non-interactive mode:** `Stdin = nil` (subprocess receives EOF on stdin read)

This is set per-step by the runner based on the current execution mode.

---

## 12. State Management

### 12.1 Run ID Generation

```go
func generateRunID() string {
    return fmt.Sprintf("wfr-%d-%04x", time.Now().UnixMilli(), rand.Intn(0xFFFF))
}
```text

Example: `wfr-1707654321000-a3f8`

### 12.2 SQLite Schema (Migration V3)

Follows the existing migration pattern from `internal/storage/db.go`:

```go
const migrationV3 = `
-- Workflow runs
CREATE TABLE IF NOT EXISTS workflow_runs (
  run_id TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  workflow_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  execution_mode TEXT NOT NULL DEFAULT 'interactive',
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
  command_masked TEXT,         -- resolved command with secrets replaced by ***
  status TEXT NOT NULL DEFAULT 'pending',
  started_at_unix_ms INTEGER,
  ended_at_unix_ms INTEGER,
  duration_ms INTEGER,
  exit_code INTEGER,
  stdout_tail TEXT,            -- last 4KB, secrets masked
  stderr_tail TEXT,            -- last 4KB, secrets masked
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
  output_sent_masked TEXT,     -- output sent to LLM, secrets masked
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

**Key change from v1:** All `resolved_command`, `stdout_tail`, `stderr_tail`, and `output_sent` fields store **masked** values (secrets replaced by `***`). The column `command_masked` replaces v1's `resolved_command` to make the masking explicit.

### 12.3 Structured Run Artifact (FR-40)

Each workflow run produces a canonical JSONL file at `~/.clai/workflow-logs/<run-id>/run.jsonl`. This satisfies FR-40 (machine-readable log format).

```go
// RunArtifact writes structured JSONL events for a single workflow run.
type RunArtifact struct {
    file *os.File
    enc  *json.Encoder
    mu   sync.Mutex
    ss   *SecretStore // for masking
}

type RunEvent struct {
    Timestamp int64                  `json:"ts"`
    Type      string                 `json:"type"`
    Data      map[string]interface{} `json:"data"`
}

// Event types:
//   "run_start"      — run_id, workflow_name, workflow_path, execution_mode, params
//   "step_start"     — step_id, step_name, job_name, matrix_key, command_masked
//   "step_output"    — step_id, stdout_sample (first 1KB, masked), stderr_sample
//   "step_end"       — step_id, status, exit_code, duration_ms
//   "analysis_start" — step_id, analysis_prompt
//   "analysis_end"   — step_id, decision, reasoning, flags
//   "human_decision" — step_id, decision (approve/reject), mode
//   "run_end"        — status, duration_ms, error
//   "error"          — step_id (optional), message

func (ra *RunArtifact) WriteEvent(evt RunEvent) error {
    ra.mu.Lock()
    defer ra.mu.Unlock()
    // Mask all string values in evt.Data through SecretStore
    return ra.enc.Encode(evt)
}
```text

SQLite rows are **indexed projections** of this artifact — optimized for queries but not the canonical record.

### 12.4 Store Interface Extensions

New methods on `storage.Store`:

```go
// Workflow Runs
CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error
UpdateWorkflowRun(ctx context.Context, runID string, status string, endTime int64, err string) error
GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error)
ListWorkflowRuns(ctx context.Context, q WorkflowRunQuery) ([]WorkflowRun, error)

// Workflow Steps
CreateWorkflowStep(ctx context.Context, step *WorkflowStep) error
UpdateWorkflowStep(ctx context.Context, id int64, update WorkflowStepUpdate) error
GetWorkflowSteps(ctx context.Context, runID string) ([]WorkflowStep, error)

// Workflow Analyses
CreateWorkflowAnalysis(ctx context.Context, a *WorkflowAnalysis) error
GetWorkflowAnalyses(ctx context.Context, runID string) ([]WorkflowAnalysis, error)

// Retention
PruneWorkflowRuns(ctx context.Context, maxPerWorkflow int) (int64, error)
```

### 12.5 Log File Layout

Full step output is written to log files (SQLite stores only 4KB tails):

```text
~/.clai/workflow-logs/
  <run-id>/
    run.jsonl                                    -- structured event log (FR-40)
    <job>--<matrix-key>--<step-id>.stdout        -- full stdout
    <job>--<matrix-key>--<step-id>.stderr        -- full stderr
```

**Path sanitization:** `<job>`, `<matrix-key>`, and `<step-id>` values are sanitized via `sanitizePathComponent()` (§7.5) to prevent path traversal or malformed filenames on all platforms.

### 12.6 Push-Based Stop Signals

The v1 spec used 500ms polling. v2 replaces this with a push-based approach:

```go
// Runner watches for stop signals via streaming RPC.
func (r *Runner) watchStopSignal(ctx context.Context, cancel context.CancelFunc) {
    stream, err := r.daemonClient.WorkflowWatch(ctx, &WorkflowWatchRequest{
        RunId: r.runID,
    })
    if err != nil {
        // Daemon unavailable — graceful degradation, no stop signal support
        return
    }
    for {
        evt, err := stream.Recv()
        if err != nil {
            return // stream closed
        }
        if evt.Type == "stop" {
            cancel()
            return
        }
    }
}
```text

### 12.7 Retention

- Default: keep last 100 runs per workflow name
- Pruning runs in the daemon's periodic maintenance loop
- When a run is pruned: `DELETE FROM workflow_runs WHERE run_id = ?` cascades to steps and analyses
- Log directory cleanup: scan for orphan directories not matching any DB run

---

## 13. IPC / gRPC Extensions

### 13.1 New Protobuf Messages

```protobuf
// --- Workflow Run Messages ---

message WorkflowRunStartRequest {
  string run_id = 1;
  string workflow_name = 2;
  string workflow_path = 3;
  string execution_mode = 4;
  string params_json = 5;
}

message WorkflowRunStartResponse {
  bool ok = 1;
}

message WorkflowRunEndRequest {
  string run_id = 1;
  string status = 2;     // "success", "failed", "cancelled"
  int64 ended_at_unix_ms = 3;
  int64 duration_ms = 4;
  string error = 5;
}

message WorkflowRunEndResponse {
  bool ok = 1;
}

// --- Workflow Step Messages ---

message WorkflowStepUpdateRequest {
  string run_id = 1;
  string job_name = 2;
  string step_id = 3;
  string step_name = 4;
  string matrix_key = 5;
  string command_masked = 6;     // resolved command with secrets masked
  string status = 7;
  int64 started_at_unix_ms = 8;
  int64 ended_at_unix_ms = 9;
  int64 duration_ms = 10;
  int32 exit_code = 11;
  string stdout_tail = 12;       // last 4KB, secrets masked
  string stderr_tail = 13;       // last 4KB, secrets masked
  string error = 14;
}

message WorkflowStepUpdateResponse {
  bool ok = 1;
}

// --- Workflow Analysis Messages ---

message WorkflowAnalysisRequest {
  string run_id = 1;
  string step_id = 2;
  string matrix_key = 3;
  string analysis_prompt = 4;
  string output_sent_masked = 5;
  string decision = 6;
  string reasoning = 7;
  string flags_json = 8;
  string transcript_json = 9;
  string human_decision = 10;
}

message WorkflowAnalysisResponse {
  bool ok = 1;
}

// --- Workflow Status / Control ---

message WorkflowStatusRequest {
  string run_id = 1;
}

message WorkflowStatusResponse {
  string status = 1;
  string current_step = 2;
  int32 steps_completed = 3;
  int32 steps_total = 4;
}

message WorkflowStopRequest {
  string run_id = 1;
  string reason = 2;
}

message WorkflowStopResponse {
  bool ok = 1;
}

// --- Push-based stop signal (replaces v1 polling) ---

message WorkflowWatchRequest {
  string run_id = 1;
}

message WorkflowWatchEvent {
  string type = 1;     // "stop"
  string reason = 2;
}

// --- History ---

message WorkflowHistoryRequest {
  string workflow_name = 1;  // optional filter
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
  int32 steps_total = 6;
  int32 steps_failed = 7;
}
```

### 13.2 New RPC Methods

```protobuf
service ClaiService {
  // ... existing RPCs ...

  // Workflow lifecycle
  rpc WorkflowRunStart(WorkflowRunStartRequest) returns (WorkflowRunStartResponse);
  rpc WorkflowRunEnd(WorkflowRunEndRequest) returns (WorkflowRunEndResponse);
  rpc WorkflowStepUpdate(WorkflowStepUpdateRequest) returns (WorkflowStepUpdateResponse);
  rpc WorkflowAnalysis(WorkflowAnalysisRequest) returns (WorkflowAnalysisResponse);

  // Workflow control
  rpc WorkflowStatus(WorkflowStatusRequest) returns (WorkflowStatusResponse);
  rpc WorkflowStop(WorkflowStopRequest) returns (WorkflowStopResponse);
  rpc WorkflowWatch(WorkflowWatchRequest) returns (stream WorkflowWatchEvent);  // push-based

  // Workflow history
  rpc WorkflowHistory(WorkflowHistoryRequest) returns (WorkflowHistoryResponse);
}
```text

### 13.3 Daemon Handler Pattern

Follows the existing pattern from `internal/daemon/handlers.go`:

```go
func (s *Server) handleWorkflowStepUpdate(ctx context.Context, req *pb.WorkflowStepUpdateRequest) (*pb.WorkflowStepUpdateResponse, error) {
    // All string fields are already masked by the CLI before sending
    err := s.store.UpdateWorkflowStep(ctx, req.StepId, WorkflowStepUpdate{
        Status:     req.Status,
        ExitCode:   int(req.ExitCode),
        EndTime:    req.EndedAtUnixMs,
        StdoutTail: req.StdoutTail,
        StderrTail: req.StderrTail,
        Error:      req.Error,
    })
    if err != nil {
        return nil, err
    }
    return &pb.WorkflowStepUpdateResponse{Ok: true}, nil
}
```

---

## 14. Configuration

### 14.1 New Config Section

```go
type WorkflowsConfig struct {
    Enabled           bool     `yaml:"enabled"`
    DefaultMode       string   `yaml:"default_mode"`         // "interactive", "non-interactive-fail", "non-interactive-auto"
    DefaultShell      string   `yaml:"default_shell"`        // override platform default
    SearchPaths       []string `yaml:"search_paths"`         // additional discovery dirs
    LogDir            string   `yaml:"log_dir"`              // override log directory
    RetainRuns        int      `yaml:"retain_runs"`          // max runs per workflow (default: 100)
    StrictPermissions bool     `yaml:"strict_permissions"`   // error on insecure file perms
    SecretFile        string   `yaml:"secret_file"`          // default: ".secrets"
}
```text

### 14.2 Defaults

```go
func defaultWorkflowsConfig() WorkflowsConfig {
    return WorkflowsConfig{
        Enabled:     false,
        DefaultMode: "interactive",
        RetainRuns:  100,
        SecretFile:  ".secrets",
    }
}
```

### 14.3 Environment Variable Overrides

| Env Var | Config Key | CLI Flag |
|---------|-----------|----------|
| `CLAI_WORKFLOW_MODE` | `workflows.default_mode` | `--mode` |
| `CLAI_WORKFLOW_LOG_DIR` | `workflows.log_dir` | — |
| `CLAI_WORKFLOW_SECRET_FILE` | `workflows.secret_file` | `--secret-file` |

---

## 15. Exit Codes

| Code | Constant | Meaning | When |
|------|----------|---------|------|
| 0 | `ExitSuccess` | Workflow completed successfully | All steps passed |
| 1 | `ExitStepFailed` | A step failed | Non-zero exit code from subprocess |
| 2 | `ExitValidationError` | YAML validation failed | Invalid workflow file |
| 3 | `ExitHumanReject` | Human rejected a step | User chose "reject" in review |
| 4 | `ExitCancelled` | Workflow cancelled | Ctrl+C or daemon stop signal |
| 5 | `ExitNeedsHuman` | Non-interactive mode requires human | Decision point in non-interactive-fail mode |
| 6 | `ExitDaemonUnavailable` | Daemon required but unavailable | `--require-daemon` flag set |
| 7 | `ExitPolicyHalt` | LLM policy halt enforced | Auto mode halted on risk policy |
| 8 | `ExitDependencyMissing` | Required tool not found | Pre-run dependency check failed |
| 124 | `ExitTimeout` | Step timed out | Step exceeded `timeout_minutes` (Tier 1) |

```go
const (
    ExitSuccess            = 0
    ExitStepFailed         = 1
    ExitValidationError    = 2
    ExitHumanReject        = 3
    ExitCancelled          = 4
    ExitNeedsHuman         = 5
    ExitDaemonUnavailable  = 6
    ExitPolicyHalt         = 7
    ExitDependencyMissing  = 8
    ExitTimeout            = 124  // Standard timeout exit code (from dagu/coreutils)
)
```text

---

## 16. Pre-Run Dependency Detection

### 16.1 Requires Block

```yaml
requires:
  - aws
  - jq
  - pulumi
```

### 16.2 Detection Logic

```go
type MissingDep struct {
    Name   string
    Reason string
}

// CheckDependencies verifies all required tools are available on PATH.
func CheckDependencies(requires []string) []MissingDep {
    var missing []MissingDep
    for _, tool := range requires {
        if _, err := exec.LookPath(tool); err != nil {
            missing = append(missing, MissingDep{Name: tool, Reason: err.Error()})
        }
    }
    return missing
}
```text

### 16.3 Auto-Detection

If `analyze: true` is used in any step, `claude` is automatically added to the implicit requires list (unless `--skip-dep-check` is set).

### 16.4 Behavior

- Missing dependencies are reported **all at once** (not fail-on-first)
- Exit code 8 (`ExitDependencyMissing`)
- Output lists all missing tools with install guidance hints
- `--require-deps` makes this a hard failure; otherwise it's a warning

---

## 17. Concurrency Design (Tier 1)

### 17.1 Parallel Matrix Execution

```go
func (r *Runner) executeMatrixParallel(ctx context.Context, job *JobDef, entries []MatrixEntry) error {
    maxP := job.Strategy.MaxParallel
    if maxP <= 0 {
        maxP = len(entries) // unlimited
    }

    sem := make(chan struct{}, maxP)
    var wg sync.WaitGroup
    var mu sync.Mutex
    var firstErr error

    for _, entry := range entries {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case sem <- struct{}{}:
        }

        wg.Add(1)
        go func(e MatrixEntry) {
            defer wg.Done()
            defer func() { <-sem }()

            // Each entry gets its own ExprContext (no shared mutable state)
            entryCtx := r.buildEntryContext(e)
            if err := r.executeStepsSequential(ctx, job.Steps, entryCtx); err != nil {
                mu.Lock()
                if firstErr == nil {
                    firstErr = err
                }
                mu.Unlock()
                if job.Strategy.FailFast == nil || *job.Strategy.FailFast {
                    cancel() // cancel remaining entries
                }
            }
        }(entry)
    }

    wg.Wait()
    return firstErr
}
```

### 17.2 Shared State Ownership

- Each matrix entry gets its own `ExprContext` — **no shared mutable state**
- Step outputs from one matrix entry are NOT visible to another entry
- `RunArtifact` is thread-safe (mutex-protected `json.Encoder`)
- Daemon RPC calls are independent (gRPC handles concurrency)

### 17.3 Multi-Job DAG (Tier 1)

Job dependency resolution uses Kahn's algorithm:

```go
func resolveJobOrder(jobs map[string]*JobDef) ([][]string, error) {
    // 1. Build adjacency list from needs fields
    // 2. Compute in-degree for each job
    // 3. Initialize queue with zero-in-degree jobs
    // 4. Process waves:
    //    - Dequeue all zero-in-degree jobs → one wave
    //    - Execute wave (jobs in parallel, bounded by resources)
    //    - Decrement in-degree of dependents
    //    - Enqueue newly zero-in-degree jobs
    // 5. If unprocessed jobs remain → cycle detected → error
    // Returns: [][]string — each inner slice is a wave of parallelizable jobs
}
```text

### 17.4 Deterministic Output Merge

When parallel jobs finish and a downstream job depends on them, outputs are merged in **sorted key order** to ensure deterministic behavior regardless of completion order.

### 17.5 Multiple Concurrent Workflow Runs

Each run is independent:
- Unique `run_id` (timestamp + random)
- Separate log directory
- Separate `context.Context`
- No global lock between runs
- Daemon tracks runs independently in SQLite

---

## 18. Error Handling and Recovery

### 18.1 Step Failure (Tier 0)

Any step failure halts the entire matrix entry (implicit fail-fast). The workflow reports `failed` with the failing step's error message.

### 18.2 Daemon Unreachable

- **Default:** CLI continues executing without state persistence, warning printed
- **`--require-daemon`:** Exit code 6 (`ExitDaemonUnavailable`)
- RunArtifact is always written regardless of daemon availability

### 18.3 CLI Crash / SIGKILL

If the CLI process is killed:
- Subprocesses in the same process group are also killed (Unix: SIGKILL to pgid; Windows: job object termination)
- Daemon retains partial state (run stuck in `running`)
- `clai workflow stop <run-id>` can mark it `cancelled`
- Orphaned run detection: on daemon startup, scan for runs in `running` status with no live CLI process

### 18.4 Claude Daemon Crash

If the Claude daemon dies during analysis (detected by `QueryViaDaemon()` returning error):

1. `QueryFast()` automatically falls back to one-shot `QueryWithContext()` (claude CLI)
2. If CLI also fails: treat as `needs_human` (FR-24)
3. Background: `StartDaemonProcess()` can restart for subsequent steps

### 18.5 Resume from Failure (Tier 1)

```bash
clai workflow run --resume <run-id>
```

1. Load previous run's step statuses from SQLite
2. Steps that were `success`: skip, use cached outputs
3. Steps that were `failed`, `skipped`, or `cancelled`: reset to `pending`
4. Create a new `run_id` (for clean state, linked to original)
5. Re-execute the DAG from the first non-succeeded step

---

## 19. Security

### 19.1 Command Execution Safety

**Argv mode (default)** eliminates the primary command injection vector:
- Commands are split into argv arrays by Go (not by a shell)
- No shell metacharacter interpretation occurs
- `exec.Command(argv[0], argv[1:]...)` does not invoke any shell

**Shell mode (opt-in)** is available when needed but carries documented risks:
- User explicitly opts in via `shell: true` or `shell: bash`
- All `${{ }}` expressions are resolved in Go **before** passing to the shell
- The resolved command is a plain string passed as a single argument to `shell -c`

### 19.2 Expression Injection Prevention

```go
// SAFE: Expression resolved in Go, result passed as single argv element
resolvedArgv := exprEval.ResolveArgv(step.Run, exprCtx)
cmd := exec.Command(resolvedArgv[0], resolvedArgv[1:]...)

// ALSO SAFE (shell mode): Expression resolved in Go before shell invocation
resolvedCmd := exprEval.Resolve(step.Run, exprCtx)
cmd := exec.Command("bash", "-c", resolvedCmd)

// NEVER DONE: Passing unresolved expressions to any execution context
```text

### 19.3 Secret Protection Summary

| Threat | Mitigation |
|--------|-----------|
| Secrets in DB | `command_masked`, `stdout_tail`, `stderr_tail` — all masked via `SecretStore.Mask()` |
| Secrets in logs | RunArtifact masks all string values before writing |
| Secrets in LLM context | `buildAnalysisContext()` applies `SecretStore.Mask()` (FR-36) |
| Secrets in `/proc` | Argv mode: secrets only in env vars (not cmdline). Shell mode: warning if secret resolved into command |
| Secrets in memory | `SecretStore` is in-memory only; Go GC handles cleanup |

### 19.4 File Permission Checks

**Unix** (file: `internal/workflow/security_unix.go`):
```go
func checkFilePermissions(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }
    if info.Mode()&0022 != 0 {
        return fmt.Errorf("workflow %s writable by group/other (mode %o); run: chmod go-w %s",
            path, info.Mode().Perm(), path)
    }
    return nil
}
```

**Windows** (file: `internal/workflow/security_windows.go`):
```go
func checkFilePermissions(path string) error {
    // Windows ACLs are complex; skip permission check.
    // Future: use windows.GetSecurityInfo for ACL verification.
    return nil
}
```text

In Tier 0 this is a **warning**. In Tier 1 with `workflows.strict_permissions: true`, it's an error.

---

## 20. Execution Engine

### 20.1 Execution Flow (Tier 0)

```
Parse YAML → Load secrets → Check dependencies → Validate →
For each matrix entry (sequential):
   a. Expand expressions for this entry
   b. For each step (sequential):
      1. Resolve ${{ }} expressions
      2. Split into argv (or prepare shell command)
      3. Report step_start to daemon + RunArtifact
      4. Execute via ShellAdapter
      5. Parse $CLAI_OUTPUT (if exists)
      6. Report step_end to daemon + RunArtifact
      7. If analyze: true → LLM analysis
      8. If human prompt needed → InteractionHandler
      9. If step fails → stop (implicit fail-fast)
   c. Report matrix entry completion
Report run completion
```text

### 20.2 Step Lifecycle State Machine

```
              ┌───────────┐
              │  pending   │
              └─────┬──────┘
                    │
          ┌─────────┴─────────┐
          │                   │
          ▼                   ▼
     ┌──────────┐       ┌──────────┐
     │ running  │       │ skipped  │  (Tier 1: precondition failed, or
     └────┬─────┘       └──────────┘   dependency failed w/o continue_on)
          │
     ┌────┼────────┐
     │    │        │
     ▼    ▼        ▼
┌─────┐ ┌──────┐ ┌───────────┐
│succ.│ │failed│ │ cancelled │  (SIGINT/stop signal)
└─────┘ └──────┘ └───────────┘
```text

| From | To | Trigger |
|------|----|---------|
| pending | running | Step execution starts |
| pending | skipped | Prior step failed (fail-fast) or Tier 1 precondition false |
| running | success | Subprocess exits with code 0 |
| running | failed | Subprocess exits with non-zero code or exec error |
| running | cancelled | `ctx.Done()` fires (SIGINT or daemon stop signal) |

### 20.3 Matrix Expansion

Given a matrix definition:

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

Expansion produces `[]MatrixEntry` where each entry is `map[string]string`. For Tier 0, entries execute **sequentially**. For Tier 1, `strategy.max_parallel` controls concurrency.

---

## 21. Package Structure

### 21.1 New Package: `internal/workflow/`

```text
internal/workflow/
  schema.go              # WorkflowDef, JobDef, StepDef types + YAML parsing
  validate.go            # Validation rules (multi-error collection)
  validate_test.go
  matrix.go              # Matrix expansion
  matrix_test.go
  expr.go                # Expression engine (${{ }} evaluation)
  expr_test.go
  executor.go            # Step execution (wraps ShellAdapter + ProcessController)
  exec_unix.go           # Unix ShellAdapter + ProcessController (//go:build !windows)
  exec_windows.go        # Windows ShellAdapter + ProcessController (//go:build windows)
  process_unix.go        # Unix ProcessController (//go:build !windows)
  process_windows.go     # Windows ProcessController (//go:build windows)
  runner.go              # Main orchestration loop
  runner_test.go
  llm.go                 # LLM integration (uses claude daemon via QueryFast, fallback)
  llm_test.go
  review.go              # InteractionHandler implementations
  review_test.go
  secrets.go             # SecretStore, SecretDef, loading, masking
  secrets_test.go
  scrub.go               # Output scrubbing (SecretStore + pattern-based)
  scrub_test.go
  artifact.go            # RunArtifact (JSONL structured log)
  artifact_test.go
  modes.go               # ExecutionMode, decision logic
  modes_test.go
  tty_unix.go            # TTY detection for Unix (//go:build !windows)
  tty_windows.go         # TTY detection for Windows (//go:build windows)
  deps.go                # Pre-run dependency checking
  deps_test.go
  discovery.go           # Workflow file discovery
  discovery_test.go
  output.go              # Terminal output formatting
  output_test.go
  exitcodes.go           # Exit code constants
```

### 21.2 CLI Commands

File: `internal/cmd/workflow.go`

```go
func newWorkflowCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "workflow",
        Short: "Run and manage workflows",
    }
    cmd.AddCommand(
        newWorkflowRunCmd(),      // clai workflow run
        newWorkflowValidateCmd(), // clai workflow validate
        newWorkflowListCmd(),     // clai workflow list        (Tier 1)
        newWorkflowStatusCmd(),   // clai workflow status      (Tier 1)
        newWorkflowHistoryCmd(),  // clai workflow history     (Tier 1)
        newWorkflowStopCmd(),     // clai workflow stop        (Tier 1)
        newWorkflowLogsCmd(),     // clai workflow logs        (Tier 1)
    )
    return cmd
}
```text

---

## 22. CLI Commands and Flags

### 22.1 `clai workflow run`

```
clai workflow run <file-or-name> [flags] [-- PARAM=value ...]

Flags:
  --mode <mode>                Execution mode: interactive | non-interactive-fail | non-interactive-auto
  --matrix key:value           Run subset of matrix entries (can repeat, AND filter)  [Tier 1]
  --job <job_id>               Run specific job only                                  [Tier 1]
  --dry-run                    Parse, validate, show execution plan without running   [Tier 1]
  --resume <run-id>            Resume from a failed run                               [Tier 1]
  --secret-file <path>         Override .secrets file path
  --env KEY=VALUE              Override/add environment variable (can repeat)
  --require-daemon             Fail (exit 6) if daemon unavailable
  --require-deps               Fail (exit 8) if required tools missing
  --skip-dep-check             Skip dependency detection
  --verbose                    Verbose output (step commands, timing details)
```text

### 22.2 `--matrix` Parsing

`--matrix stack:ojin-staging` is parsed as key=`stack`, value=`ojin-staging`.

- Multiple `--matrix` flags form an **AND filter** (entry must match all)
- Unknown keys produce validation error (exit 2)
- Value matching is exact string comparison

### 22.3 Other Subcommands

```
clai workflow validate <file-or-name>         # Parse and validate without executing
clai workflow list [dir]                       # List discovered workflows                [Tier 1]
clai workflow status <run-id>                  # Show current run status                  [Tier 1]
clai workflow history [--workflow <name>] [-n] # Show run history                         [Tier 1]
clai workflow stop <run-id>                    # Send stop signal to a running workflow   [Tier 1]
clai workflow logs <run-id> [--step <id>]      # View run logs (from RunArtifact)         [Tier 1]
    --format json|text                         # Output format (default: text)
```text

---

## 23. Testing Strategy

### 23.1 Unit Tests

Each file in `internal/workflow/` has a corresponding `_test.go` file. Key coverage:

- **schema_test.go:** YAML parsing edge cases, missing fields, invalid types
- **validate_test.go:** All validation rules, multi-error collection
- **matrix_test.go:** Expansion with include/exclude, empty matrix, single entry
- **expr_test.go:** All scopes, nested references, unresolved expression errors, multiline output parsing
- **secrets_test.go:** Loading from env, masking, dual scrubbing
- **modes_test.go:** TTY detection mocking, decision matrix for all risk × decision combinations
- **deps_test.go:** Missing tool detection, auto-detection of `claude`
- **artifact_test.go:** JSONL event writing, concurrent writes, secret masking

### 23.2 Integration Tests

- End-to-end workflow run with `echo` commands (no LLM)
- Matrix expansion with 3 entries
- Step output passing (`$CLAI_OUTPUT` → expression resolution)
- LLM analysis with mock claude daemon (pre-recorded responses via test socket)
- Human review with simulated stdin
- Non-interactive mode (verify exit codes)
- Daemon state persistence (verify SQLite rows after run)
- RunArtifact verification (parse JSONL, check all events present)

### 23.3 Platform-Specific Tests

- CI runs on Linux, macOS, and Windows
- ShellAdapter tests: argv splitting, shell invocation, default shell detection
- ProcessController tests: graceful stop, force kill, timeout
- TTY detection: mock implementations for testing

### 23.4 Test Fixtures

```
testdata/workflows/
  simple-chain.yaml          # 3 sequential steps, echo commands
  matrix-multi.yaml          # 3 matrix entries, echo with ${{ matrix.key }}
  output-passing.yaml        # Step A writes $CLAI_OUTPUT, Step B reads ${{ steps.A.outputs.key }}
  analyze-step.yaml          # Step with analyze: true + analysis_prompt
  secrets-env.yaml           # Workflow with secrets from env
  invalid-cycle.yaml         # Circular job dependencies (should fail validation)
  requires-tools.yaml        # requires: block with known and unknown tools
```text

### 23.5 Mock LLM

```go
type MockLLMClient struct {
    responses []string // pre-recorded responses, returned in order
    idx       int
}

func (m *MockLLMClient) Query(ctx context.Context, prompt string) (string, error) {
    if m.idx >= len(m.responses) {
        return "", fmt.Errorf("no more mock responses")
    }
    resp := m.responses[m.idx]
    m.idx++
    return resp, nil
}
```

---

## 24. Acceptance Criteria

### 24.1 Tier 0

- [ ] YAML parsing produces correct `WorkflowDef` for the Pulumi example (func spec §6)
- [ ] Matrix expansion generates correct entries from `include`
- [ ] Steps execute sequentially via ShellAdapter (argv default mode)
- [ ] Shell mode works when `shell: true` is set
- [ ] Step outputs exported via `$CLAI_OUTPUT` resolve in downstream `${{ }}` expressions
- [ ] Unresolved expressions produce hard error (not passed to subprocess)
- [ ] `analyze: true` steps send output to Claude daemon via `QueryFast()` and receive structured response
- [ ] LLM unparseable response → `needs_human` (FR-24)
- [ ] Risk level decision matrix correctly determines when to prompt human
- [ ] Human review interface presents approve/reject/inspect/command/question options
- [ ] Follow-up LLM conversation preserves context
- [ ] Step failure halts remaining steps in the matrix entry
- [ ] Workflow run state persisted in SQLite via daemon gRPC
- [ ] RunArtifact (JSONL) written for every run
- [ ] Secrets loaded from env, masked in all stored/logged outputs
- [ ] Exit codes match §15 specification
- [ ] Non-interactive-fail mode exits with code 5 on human decision needed
- [ ] Ctrl+C gracefully stops subprocess and cancels remaining steps
- [ ] Works on Linux, macOS, and Windows
- [ ] `clai workflow validate` checks YAML without executing

### 24.2 Tier 1

- [ ] Parallel matrix execution with `max_parallel` semaphore
- [ ] Multi-job DAG resolution and execution
- [ ] `if` conditionals in steps and jobs
- [ ] `--matrix key:value` subset filtering
- [ ] `--dry-run` shows execution plan
- [ ] `--resume <run-id>` resumes from failure
- [ ] `.secrets` file loading
- [ ] Interactive secret entry
- [ ] Step timeout with exit code 124
- [ ] Lifecycle handlers (onSuccess, onFailure, onExit)
- [ ] Non-interactive-auto mode
- [ ] All CLI subcommands functional (list, status, history, stop, logs)

---

## 25. Dagu Pattern Comparison

| Dagu Pattern | Status | clai Implementation | Notes |
|-------------|--------|-------------------|-------|
| YAML workflow files | ✅ Adopted | GHA-style syntax (not dagu-style) | FR-2 |
| Step command execution | ✅ Adapted | argv default + shell opt-in (dagu uses shell) | Safer than dagu |
| `secrets:` block | ✅ Adopted | env, file, interactive sources | Inspired by dagu's secret providers |
| Headless mode | ✅ Adopted | `ModeNonInteractiveFail` / `ModeNonInteractiveAuto` | Inspired by `DAGU_HEADLESS` |
| Process tree termination | ✅ Adopted | ProcessController with job objects (Windows) | From dagu Windows support |
| Exit code 124 (timeout) | ✅ Adopted | `ExitTimeout = 124` | Standard convention |
| Output variables | ✅ Adapted | `$CLAI_OUTPUT` file (not `${VAR}` dagu-style) | GHA-compatible |
| Variable expansion | ✅ Adapted | `${{ }}` GHA-style (not `${VAR}` dagu-style) | Go-side evaluation for safety |
| Environment variables | ✅ Adopted | Workflow → job → step env merge | Same precedence model |
| Lifecycle handlers | ✅ Adopted (Tier 1) | `onSuccess`, `onFailure`, `onExit` | Identical semantics |
| `maxActiveSteps` | ✅ Adopted (Tier 1) | `max_parallel` semaphore-bounded goroutines | Same concept |
| Preconditions | ✅ Adopted (Tier 1) | Command, env, file checks | Same concept |
| JSON execution history | ✅ Adopted | RunArtifact JSONL | FR-40 |
| Retry with backoff | ⏳ Deferred | Tier 2 | Not needed for Pulumi use case |
| `continueOn` | ⏳ Deferred | Tier 2 | Fine-grained failure control |
| Sub-workflows | ⏳ Deferred | Tier 2 | Dagu's child DAG pattern |
| File-based state | ❌ Replaced | SQLite (existing clai pattern) | Single source of truth |
| Web UI | ❌ Omitted | CLI-only (Tier 2 consideration) | |
| Docker/SSH/HTTP executors | ❌ Omitted | Shell commands only | Use `docker run`, `curl`, `ssh` |

---

## 26. Non-Functional Requirements

### 26.1 Shell Completion

Use cobra's built-in completion generation:

```go
// Auto-generated by cobra for zsh, bash, fish
cmd.GenBashCompletionFile("completions/clai.bash")
cmd.GenZshCompletionFile("completions/_clai")
cmd.GenFishCompletionFile("completions/clai.fish")
```text

Custom completions for:
- Workflow name completion: reads from discovery directories
- `--matrix` key completion: parses YAML and lists matrix keys
- `--resume` run ID completion: queries daemon for recent failed runs

### 26.2 Performance Budget

| Metric | Target | Rationale |
|--------|--------|-----------|
| Binary size increase | < 2 MB | Workflow package is pure Go, no CGo |
| `clai workflow validate` | < 100ms for 200-line YAML | YAML parsing + validation only |
| Memory per step | ~12 KB overhead | 2 × 4KB limitedBuffer + ExprContext |
| RunArtifact write | < 1ms per event | Buffered JSONL encoder |
| Secret masking | < 10ms for 100KB output | String replacement is fast |

### 26.3 Dependency Checklist

For workflow users:

| Tool | Required? | Checked By |
|------|----------|------------|
| Shell (bash/zsh/pwsh) | Yes (auto-detected) | ShellAdapter.DefaultShell() |
| `claude` CLI | Only if `analyze: true` steps exist | Auto-detected in dep check |
| External tools (aws, jq, pulumi...) | Per workflow `requires:` block | `exec.LookPath()` |

---

## 27. Decision Log

| ID | Decision | Options | Choice | Rationale |
|----|----------|---------|--------|-----------|
| D1 | Execution location | (a) Daemon (b) CLI + daemon state tracking | **(b)** | Daemon is lightweight with idle timeout. CLI has terminal access for human interaction. |
| D2 | State storage | (a) SQLite (b) SQLite + JSONL (c) JSONL only | **(b)** | SQLite for indexed queries; JSONL as canonical machine-readable artifact (FR-40). |
| D3 | Output routing | (a) All through daemon (b) CLI captures, sends tails | **(b)** | Avoids routing large output through gRPC. |
| D4 | YAML syntax | (a) Dagu-style (b) GHA-style (c) Custom | **(b)** | GHA is widely known. Matrix strategy from GHA/act is essential for multi-target use cases. |
| D5 | Expression syntax | (a) `${VAR}` dagu-style (b) `${{ }}` GHA-style | **(b)** | GHA-style is unambiguous (no conflict with shell `$VAR`). Evaluated Go-side. |
| D6 | LLM backend | (a) API only (b) daemon only (c) daemon + fallback | **(c)** | Existing `QueryFast()` provides daemon for speed, CLI fallback for availability. |
| D7 | Tier 0 scope | (a) Full DAG (b) Sequential only | **(b)** | Sequential sufficient for Pulumi use case. DAG in Tier 1. |
| D8 | Command execution | (a) Shell by default (b) Argv by default, shell opt-in | **(b)** | Argv eliminates command injection. Shell opt-in for pipes/redirects. |
| D9 | Stop signals | (a) Polling (500ms) (b) Push (streaming RPC) | **(b)** | Reduces overhead and race windows. Streaming RPC is natural fit. |
| D10 | Run artifact format | (a) JSON (b) JSONL (c) SQLite only | **(b)** | Append-only, streamable, machine-readable. SQLite rows are projections. |
| D11 | Secret persistence | (a) Store resolved commands (b) Store masked commands | **(b)** | Secrets MUST NOT appear in DB. Masking is mandatory. |
| D12 | Non-interactive design | (a) Single mode (b) Two modes (c) Three modes | **(c)** | `fail` for strict CI, `auto` for pre-vetted workflows, `interactive` for human use. |
| D13 | Multiline output | (a) Escaped newlines (b) Heredoc delimiters (c) JSON values | **(b)** | GitHub Actions precedent. Simple to parse, familiar to users. |
| D14 | Platform adapters | (a) Runtime checks (b) Build-tag files (c) Interface + factory | **(b)** | Matches existing clai pattern (`internal/ipc/spawn_{unix,windows}.go`). |
