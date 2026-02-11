# clai Workflow Execution — Technical Specification

**Version:** 3.0
**Date:** 2026-02-11
**Status:** RFC
**Supersedes:** `specs/tech_workflows_v2.md` (v2)
**Companion:** `specs/func_workflows.md` (functional requirements)

> This document specifies **how** to implement the workflow execution feature defined in `func_workflows.md`. It covers architecture, data structures, protocols, and implementation details. Reference the functional spec for *what* the system should do.
>
> Implementation draws inspiration from [dagu-org/dagu](https://github.com/dagu-org/dagu) — a self-contained Go workflow engine with YAML definitions, DAG-based step execution, and structured run artifacts.

### Changelog from v2

- **Shell schema unification (P0-1):** Replaced `Shell *bool` + `ShellName string` with unified `Shell string` field; custom `UnmarshalYAML` accepts both `shell: true` and `shell: pwsh`
- **FR-24 consistency (P0-2):** Unified unparseable LLM response handling — unparseable maps to `needs_human`, then follows the standard risk matrix; removed special-case override row from §4.4
- **Missing schema fields (P0-3):** Added `Vars`, `LLM`, `Params`, `Preconditions`, `Jobs` scope to type definitions
- **Store identity fix (P0-4):** Changed from `int64` row ID to composite key `(runID, stepID, matrixKey)` for step state updates
- **Windows transport (P0-5):** Added Transport Abstraction §3.7; daemon IPC deferred on Windows to Tier 1; added `--daemon=false` flag for Tier 0 Windows support
- **Strict YAML parsing (P1-2):** Added `KnownFields(true)` to reject unknown fields in workflow YAML
- **Output parser fix (P1-4):** Replaced broken `strings.Contains(parts[0], "")` with regex validation via `isValidOutputKey()`
- **Cancellation fix (P1-3):** Fixed `cancel()` scope in concurrent goroutine to capture from outer `context.WithCancel`
- **Windows security (P1-5):** Added warning logging for Windows permission checks and documented caveats
- **Func spec compatibility note (P1-1):** Documented `shell: true` requirement for pipe/redirect examples in functional spec
- **Non-TTY mode handling (MR-1):** Added fallback when `--mode interactive` used without TTY; logs warning and falls back to `non-interactive-fail`
- **LLM resilience (MR-2):** Added timeout, retry, and failure handling for LLM analysis calls
- **Env precedence chain (MR-9):** Explicit step > job > workflow > `--env` > secrets > process env
- **NO_COLOR/TERM=dumb support (MR-8):** Respect terminal capability detection for color output
- **Steps stdin handling (MR-7):** Non-interactive steps receive `/dev/null` (NUL on Windows) for immediate EOF
- **Run ID collision retry (MR-10):** Retry once on UNIQUE violation, fail on second collision
- **CI gates and E2E test harness (AR-8, DG-6):** Added `go test -race`, cross-OS matrix, binary size budgets, `testscript` harness
- **Exit code operator runbook (DG-3):** Added CI/CD handling guidance per exit code
- **Proto/DB migration lockstep (DG-1):** Added migration strategy for schema evolution
- **Shell completion install paths (DG-2):** Expanded with bash/zsh/fish install paths
- **Windows named pipe notes (DG-4):** Pipe naming, permissions, cleanup documented in §3.7
- **Man page / help text (DG-5):** Cobra auto-help and man page generation

### Changelog from v1

- **Cross-platform execution:** Shell-specific `$SHELL -c` replaced with `ShellAdapter` abstraction; argv execution by default, shell mode opt-in (CB-2, CB-4)
- **Security hardening:** Secret lifecycle management, masked command logging, `/proc`-safe execution (CB-5, AR-7)
- **Headless-first architecture:** Explicit execution modes for interactive/CI use, structured exit codes, push-based stop signals (CB-6, AR-1, AR-8)
- **Scope clarity:** Single implementation tiers table replaces scattered v0/v1 annotations (CB-1)
- **Complete specification:** All 45 FRs addressed; missing requirements (dependency detection, concurrency, exit codes) fully specified (CB-7, CB-10)
- **Type consistency:** Schema mismatches (`StepDef.Shell` type, `HumanReview` vs `HumanReviewer`, `*MatrixDef`) corrected (CB-8)
- **Windows filesystem safety:** `sanitizePathComponent()` is platform-aware, handles `: * ? " < > |` (CB-9)
- **Signal handling:** Cross-platform `ProcessController` with Windows job objects, not just Unix signals (CB-3)
- **Architectural improvements:** Config precedence chain (AR-5), no TUI dependency in engine (AR-4, AR-10), OS-specific adapters via build tags (AR-3), structured run artifact (AR-6), concurrency design (AR-9), safer command model (AR-2)

### V2 Review Feedback Traceability

Every finding from the v2 review maps to a v3 section. Nothing is dropped.

| ID | Finding | V3 Section |
|----|---------|------------|
| P0-1 | Shell schema contradiction (`Shell *bool` + `ShellName`) | §7.1 StepDef, HandlerDef — unified `Shell string` |
| P0-2 | FR-24 contradiction across §4.4 and §10.7 | §4.4 Decision Behavior Matrix (note below table) |
| P0-3 | Missing schema fields (Vars, LLM, Params, Preconditions, Jobs) | §7.1 WorkflowDef, JobDef, StepDef; §9.2 scopes; §9.3 ExprContext |
| P0-4 | Store identity mismatch (int64 vs string) | §12.4 store interface — composite key (runID, stepID, matrixKey) |
| P0-5 | Windows transport gap (Unix socket only) | §3.7 Transport Abstraction; §2.2 Tier 1 table; §4.5 CI guide |
| P0-6 | WorkflowConfigBlock.Shell alignment | §7.1 — confirmed `Shell string` follows unified model |
| P1-1 | Argv default vs func spec pipe/redirect examples | §5.1 compatibility note |
| P1-2 | Non-strict YAML parsing (silently ignores unknowns) | §7.2 parsing pipeline — `KnownFields(true)` |
| P1-3 | Cancellation `cancel()` scope in goroutine | §17.1 parallel matrix — full closure signature |
| P1-4 | Output parser bug (`strings.Contains(parts[0], "")`) | §9.5 — regex validation via `isValidOutputKey()` |
| P1-5 | Windows security permission no-op | §19.4 Windows impl; §19.5 Windows Security Caveats |
| AR-1 | Normalize execution schema | Addressed by P0-1 (§7.1) |
| AR-2 | Strict YAML parser | Addressed by P1-2 (§7.2) |
| AR-3 | Wire missing model fields | Addressed by P0-3 (§7.1, §9.2, §9.3) |
| AR-4 | Transport abstraction | Addressed by P0-5 (§3.7) |
| AR-5 | Keep Cobra, no TUI | Already correct — no change needed |
| AR-6 | Engine/presentation separation | Already correct — no change needed |
| AR-7 | Fix store/RPC identity | Addressed by P0-4 (§12.4, §13.3) |
| AR-8 | Hard CI gates | §23.6 CI Gates |
| MR-1 | `--mode interactive` in non-TTY | §4.2 Mode Selection |
| MR-2 | LLM timeout/retry | §10.10 LLM Resilience |
| MR-3 | $CLAI_OUTPUT malformed handling | §9.5 output parser error handling |
| MR-4 | `-- PARAM=value` typing | §22.1 CLI param handling |
| MR-5 | Resume with changed file | §18.5 resume hash check |
| MR-6 | Exec-start failure exit codes | §15 Exit Codes |
| MR-7 | Steps blocking stdin | §11.3 stdin handling for non-interactive steps |
| MR-8 | NO_COLOR / TERM=dumb | §11.5 Color and Formatting |
| MR-9 | Env key precedence chain | §4.6 Environment Variable Precedence |
| MR-10 | Run ID collision retry | §12.1 Run ID generation |
| DG-1 | Proto/DB migration lockstep | §12.8 Schema Migration |
| DG-2 | Shell completion install | §26.1 Shell Completion |
| DG-3 | Exit code operator runbook | §15.2 CI/CD Handling |
| DG-4 | Windows named pipe notes | §3.7 Transport Abstraction |
| DG-5 | Man page / help text | §22.4 Help and Man Pages |
| DG-6 | CLI E2E test harness | §23.6 CI Gates |

### V1 Review Feedback Traceability

Every finding from the v1 review maps to a v2/v3 section. Nothing is dropped.

| ID | Finding | V2/V3 Section |
|----|---------|---------------|
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
| **TransportDialer** | IPC transport abstraction for Unix domain sockets (Tier 0) and Windows named pipes (Tier 1) |
| **PreconditionsDef** | Pre-execution checks for environment variables, commands, and files required by a workflow or job |

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
| — | Windows named pipe transport | `TransportDialer` impl for `\\.\pipe\clai-daemon` (P0-5) |

**Tier 1 also includes:**
- `non-interactive-auto` execution mode
- Pre-run dependency detection (`requires:` block)
- Full concurrency design (parallel matrix, multi-job DAG waves)
- Resume from failure (`--resume <run-id>`)
- Full CLI command set (list, status, history, stop, logs)
- Windows named pipe IPC transport

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

```
User invokes:  clai workflow run pulumi-compliance-run
                        |
                        v
              +----------------------------+
              |  CLI Process               |
              |  (clai workflow run)       |
              |                            |
              |  1. Load & parse YAML      |
              |  2. Load secrets           |
              |  3. Check dependencies     |
              |  4. Expand matrix          |
              |  5. Resolve expressions    |
              |  6. Validate DAG           |
              |  7. WorkflowRunStart ------+-------> +------------------+
              |     RPC to daemon          |         |  Daemon (claid)  |
              |  8. Execute steps via      |         |                  |
              |     ShellAdapter           |         |  Persists:       |
              |  9. WorkflowStepUpdate ----+-------> |  - run state     |
              |     RPC per state change   |         |  - step state    |
              | 10. LLM analysis via       |         |  - analyses      |
              |     claude daemon          |         |  in SQLite       |
              | 11. Human prompts via      |         |                  |
              |     InteractionHandler     |         |  Serves:         |
              | 12. WorkflowRunEnd --------+-------> |  - status RPCs   |
              +----------------------------+         |  - history RPCs  |
                        |                            |  - stop stream   |
                        | spawns per step            +------------------+
                        v
              +----------------------------+
              |  Subprocesses              |
              |  (one per step)            |
              |                            |
              |  Managed by                |
              |  ProcessController         |
              |  (platform-specific)       |
              |                            |
              |  stdout -> limitedBuffer   |
              |  stderr -> limitedBuffer   |
              |  + tee to RunArtifact      |
              +----------------------------+
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
```

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

#### 3.5.1 Environment Variable Precedence for Step Execution

When resolving environment variables for step execution, the following precedence applies (highest priority first):

1. **Step-level `env`** — from the step's `env:` block
2. **Job-level `env`** — from the job's `env:` block
3. **Workflow-level `env`** — from the workflow's `env:` block
4. **CLI `--env` flags** — runtime overrides passed at invocation
5. **Secrets** — injected by SecretStore
6. **Process environment** — inherited from the parent process

Later entries are overridden by earlier ones. This chain is applied once per step at expression resolution time. See also §4.6 for the full specification.

### 3.6 Graceful Degradation

If the daemon is unavailable:

1. CLI attempts `ipc.EnsureDaemon()` (existing pattern from `internal/ipc/spawn.go`)
2. If daemon still unreachable: run workflow **without** state persistence
3. Print warning: `"daemon unavailable — run history will not be persisted"`
4. RunArtifact (JSONL file) is always written regardless of daemon availability
5. All other functionality (execution, LLM analysis, human prompts) works normally

### 3.7 Transport Abstraction

The IPC layer between the CLI and daemon uses a `TransportDialer` interface to abstract platform-specific transport mechanisms.

```go
// TransportDialer abstracts the IPC transport between CLI and daemon.
// Tier 0: Unix domain sockets (Linux, macOS)
// Tier 1: Windows named pipes
type TransportDialer interface {
    // Dial connects to the daemon.
    Dial(ctx context.Context) (net.Conn, error)
    // Listen creates a listener for the daemon server.
    Listen() (net.Listener, error)
    // Cleanup removes stale socket/pipe files.
    Cleanup() error
    // Address returns the transport address for logging.
    Address() string
}
```

#### Platform Transport Matrix

| Platform | Tier | Transport | Address | Notes |
|----------|------|-----------|---------|-------|
| Linux/macOS | 0 | Unix domain socket | `~/.clai/daemon.sock` | Existing implementation |
| Windows | 1 | Named pipe | `\\.\pipe\clai-daemon-{user}` | User-scoped, ACL-protected |

#### Windows Named Pipe Notes (Tier 1)

- **Pipe naming convention:** The pipe name includes the current username (`clai-daemon-{user}`) to support multi-user systems where multiple users may run `claid` concurrently.
- **Access control:** The named pipe DACL restricts access to the current user SID. Other users on the same machine MUST NOT be able to connect.
- **Cleanup on exit:** The daemon removes the pipe handle on graceful shutdown. On crash recovery, the CLI detects a stale pipe via a failed connection attempt and re-spawns the daemon.

#### Tier 0 on Windows

In Tier 0, Windows does not have daemon IPC support. The `--daemon=false` flag disables daemon IPC. The workflow engine runs locally — argv/shell execution works, state persistence is deferred. RunArtifact (JSONL file) is still written.

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

**Non-TTY fallback (MR-1):** If `--mode interactive` is specified but no TTY is detected, the CLI MUST log a warning (`interactive mode requested but no TTY detected; falling back to non-interactive-fail`) and fall back to `non-interactive-fail`. This prevents hangs in CI environments where interactive mode is mistakenly configured.

### 4.3 TTY Detection

```go
// TTYDetector determines whether interactive I/O is available.
type TTYDetector interface {
    IsInteractive() bool
}
```

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
| Secret entry (interactive source) | Prompt for input | Exit code 5 | Exit code 5 |

**Note (FR-24):** Unparseable LLM responses are mapped to `needs_human` by `parseAnalysisResponse()` (§10.5). The `needs_human` decision then follows the standard risk-level rows above. There is no special override for unparseable responses — the risk matrix is the single source of truth.

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
```

For Windows CI where daemon IPC is not yet available (Tier 1):

```bash
# Windows CI: disable daemon IPC (Tier 1 adds named pipe support)
clai workflow run --daemon=false --mode non-interactive-fail pulumi-compliance
```

### 4.6 Environment Variable Precedence

When resolving environment variables for step execution, the following precedence applies (highest priority first):

1. **Step-level `env`** — from the step's `env:` block
2. **Job-level `env`** — from the job's `env:` block
3. **Workflow-level `env`** — from the workflow's `env:` block
4. **CLI `--env` flags** — runtime overrides passed at invocation
5. **Secrets** — injected by SecretStore
6. **Process environment** — inherited from the parent process

Later entries are overridden by earlier ones. This chain is applied once per step at expression resolution time.

The resolution algorithm builds the environment in reverse precedence order (process env first, then secrets, then CLI flags, etc.), with each layer overwriting keys from the previous layer. The final merged map is converted to `KEY=VALUE` pairs and passed to `ExecOpts.Env`.

```go
func (r *Runner) resolveStepEnv(wf *WorkflowDef, job *JobDef, step *StepDef) []string {
    merged := envFromProcess()              // 6. process env
    mergeInto(merged, r.secretStore.Env())  // 5. secrets
    mergeInto(merged, r.cliEnvFlags)        // 4. --env flags
    mergeInto(merged, wf.Env)               // 3. workflow env
    mergeInto(merged, job.Env)              // 2. job env
    mergeInto(merged, step.Env)             // 1. step env (highest priority)
    return toEnvList(merged)
}
```

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

**Compatibility note (P1-1):** The Pulumi compliance example in `func_workflows.md` §6 uses shell features (`$()`, pipes, `>>`) in some step `run` fields. Those steps require `shell: true` (or an explicit shell name). The functional spec should be updated to include `shell: true` on steps that use shell features. Argv mode is the secure default for simple commands like `pulumi stack ls --json`.

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
```

### 5.3 Platform Implementations

**Unix** (file: `internal/workflow/exec_unix.go`, build tag `//go:build !windows`):

```go
// ExecArgv: exec.CommandContext(ctx, argv[0], argv[1:]...)
// ExecShell: exec.CommandContext(ctx, shell, "-c", command)
// DefaultShell: uses internal/cmd/shelldetect.go -> bash, zsh, or fish
// SplitCommand: POSIX shlex tokenization
```

**Windows** (file: `internal/workflow/exec_windows.go`, build tag `//go:build windows`):

```go
// ExecArgv: exec.CommandContext(ctx, argv[0], argv[1:]...)
// ExecShell: for pwsh -> exec.CommandContext(ctx, "pwsh", "-Command", command)
//            for cmd -> exec.CommandContext(ctx, "cmd", "/C", command)
// DefaultShell: "pwsh" (PowerShell Core), fallback to "cmd"
// SplitCommand: CommandLineToArgvW semantics
```

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
```

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
```

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
    Vars     map[string]string     `yaml:"vars"`       // Tier 1 (FR-37): workflow-level variables
    LLM      *LLMConfig            `yaml:"llm"`        // Tier 1 (FR-29): per-workflow LLM config
    Params   []ParamDef            `yaml:"params"`     // Tier 1 (FR-44): CLI parameter definitions

    // Tier 1
    OnSuccess *HandlerDef `yaml:"onSuccess"`
    OnFailure *HandlerDef `yaml:"onFailure"`
    OnExit    *HandlerDef `yaml:"onExit"`
}

// WorkflowConfigBlock holds per-workflow config overrides.
// Shell follows the same unified enum as StepDef.Shell:
//   "" = no override, "true" = default shell, "bash"/"zsh"/"fish"/"pwsh"/"cmd" = explicit.
type WorkflowConfigBlock struct {
    Mode      string `yaml:"mode"`       // execution mode override
    Shell     string `yaml:"shell"`      // default shell override (same enum as StepDef.Shell)
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
    If            string            `yaml:"if"`
    Preconditions *PreconditionsDef `yaml:"preconditions"` // Tier 1 (FR-20)
    OnSuccess     *HandlerDef       `yaml:"onSuccess"`
    OnFailure     *HandlerDef       `yaml:"onFailure"`
    OnExit        *HandlerDef       `yaml:"onExit"`
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
    // e.g., os: [linux, darwin] -> auto-expanded
}

// StepDef represents a single step within a job.
type StepDef struct {
    ID             string            `yaml:"id"`
    Name           string            `yaml:"name"`
    Run            string            `yaml:"run"`
    Env            map[string]string `yaml:"env"`
    Shell          string            `yaml:"shell"`              // "" = argv (default), "true" = default shell, "bash"/"zsh"/"fish"/"pwsh"/"cmd" = explicit
    WorkingDir     string            `yaml:"working_directory"`  // Tier 1 (FR-12)
    TimeoutMinutes *int              `yaml:"timeout_minutes"`    // Tier 1 (FR-10)
    Analyze        bool              `yaml:"analyze"`
    AnalysisPrompt string            `yaml:"analysis_prompt"`
    RiskLevel      string            `yaml:"risk_level"`         // "low", "medium", "high"
    ContextFrom    string            `yaml:"context_from"`       // Tier 1 (FR-26)
    ContextFor     string            `yaml:"context_for"`        // Tier 1 (FR-26)
    If             string            `yaml:"if"`                 // Tier 1 (FR-19)

    // Tier 1
    Preconditions *PreconditionsDef `yaml:"preconditions"` // Tier 1 (FR-20)

    // Runtime fields (not from YAML)
    ResolvedArgv    []string `yaml:"-"` // for argv mode
    ResolvedCommand string   `yaml:"-"` // for shell mode (ALWAYS masked before storage)
    ResolvedEnv     []string `yaml:"-"`
}

// UnmarshalYAML handles both bool and string values for the Shell field.
// shell: true  -> "true" (use default shell)
// shell: bash  -> "bash" (explicit shell)
// shell: (omitted) -> "" (argv mode, no shell)
func (s *StepDef) UnmarshalYAML(unmarshal func(interface{}) error) error {
    // Use an alias to avoid infinite recursion
    type stepAlias StepDef
    var raw struct {
        stepAlias `yaml:",inline"`
        ShellRaw  interface{} `yaml:"shell"`
    }
    if err := unmarshal(&raw); err != nil {
        return err
    }
    *s = StepDef(raw.stepAlias)
    switch v := raw.ShellRaw.(type) {
    case bool:
        if v {
            s.Shell = "true"
        }
        // false -> "" (same as omitted)
    case string:
        s.Shell = v
    case nil:
        // omitted -- Shell stays ""
    default:
        return fmt.Errorf("shell: expected bool or string, got %T", v)
    }
    return nil
}

// ShellMode returns the effective shell execution mode.
func (s *StepDef) ShellMode() string {
    if s.Shell == "" {
        return "" // argv mode
    }
    if s.Shell == "true" {
        return "default" // use platform default shell
    }
    return s.Shell // explicit shell name
}

// validShellValues defines the allowed values for Shell fields.
var validShellValues = map[string]bool{
    "": true, "true": true,
    "bash": true, "zsh": true, "fish": true,
    "pwsh": true, "cmd": true,
}

// HandlerDef is a step to run on lifecycle events (Tier 1).
// Shell field follows the same unified model as StepDef.Shell.
type HandlerDef struct {
    Run   string `yaml:"run"`
    Shell string `yaml:"shell"` // "" = argv, "true" = default shell, "bash"/"zsh"/etc.
}

// UnmarshalYAML handles both bool and string values for HandlerDef.Shell.
// Same pattern as StepDef: shell: true -> "true", shell: bash -> "bash".
func (h *HandlerDef) UnmarshalYAML(unmarshal func(interface{}) error) error {
    type handlerAlias HandlerDef
    var raw struct {
        handlerAlias `yaml:",inline"`
        ShellRaw     interface{} `yaml:"shell"`
    }
    if err := unmarshal(&raw); err != nil {
        return err
    }
    *h = HandlerDef(raw.handlerAlias)
    switch v := raw.ShellRaw.(type) {
    case bool:
        if v {
            h.Shell = "true"
        }
    case string:
        h.Shell = v
    case nil:
        // omitted
    default:
        return fmt.Errorf("shell: expected bool or string, got %T", v)
    }
    return nil
}

// PreconditionsDef defines pre-execution checks (Tier 1, FR-20).
type PreconditionsDef struct {
    Env      map[string]string `yaml:"env"`      // env var name -> expected value ("" = must exist)
    Commands []string          `yaml:"commands"` // commands that must succeed (exit 0)
    Files    []string          `yaml:"files"`    // files that must exist
}

// ParamDef defines a CLI parameter for the workflow (Tier 1, FR-44).
type ParamDef struct {
    Name        string `yaml:"name"`
    Default     string `yaml:"default,omitempty"`
    Description string `yaml:"description,omitempty"`
}

// JobResult holds the outcome of a completed job (Tier 1).
type JobResult struct {
    Result  string            // "success", "failure"
    Outputs map[string]string // merged from job's steps
}

// LLMConfig represents workflow-level LLM configuration.
type LLMConfig struct {
    Backend   string `yaml:"backend"`     // "daemon" (default), "claude-cli", "api"
    Provider  string `yaml:"provider"`    // for API backend: "anthropic", "openai"
    Model     string `yaml:"model"`
    APIKeyEnv string `yaml:"api_key_env"`
}
```

### 7.2 Parsing Pipeline

```go
func ParseWorkflow(path string) (*WorkflowDef, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read workflow: %w", err)
    }

    var wf WorkflowDef
    decoder := yaml.NewDecoder(bytes.NewReader(data))
    decoder.KnownFields(true) // P1-2: Unknown YAML fields produce a parse error, preventing silent typos
    if err := decoder.Decode(&wf); err != nil {
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
- `shell` MUST be one of: `""`, `"true"`, `"bash"`, `"zsh"`, `"fish"`, `"pwsh"`, `"cmd"` (validated via `validShellValues`)

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
```

---

## 8. Secrets Management

### 8.1 Secret Lifecycle

```
Workflow YAML          SecretStore           Process Env        DB / Logs
+--------------+      +-------------+      +----------+      +----------+
| secrets:     |      | In-memory   |      | Env vars |      | Masked   |
|   - name: X  |--1-->| name->value |--2-->| X=secret |      | X=***    |
|     from: env|      | (never      |      | (per     |      | resolved |
|              |      |  persisted) |      |  process)|      | =masked  |
+--------------+      +------+------+      +----------+      +----------+
                             |                                     ^
                             | Mask()                               |
                             +----------3--------------------------+
```

1. **Load:** Secrets loaded from sources (env, file, interactive) into `SecretStore`
2. **Inject:** Secret values set as env vars for subprocess execution
3. **Mask:** All output, logs, and DB fields run through `SecretStore.Mask()` before persistence

### 8.2 SecretStore

```go
// SecretStore holds secret values in memory and provides masking.
// Values are NEVER written to disk, database, or log files.
type SecretStore struct {
    mu      sync.RWMutex
    secrets map[string]string // name -> value
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

```
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

`${{ <expression> }}` -- GHA-compatible expression syntax (FR-37).

### 9.2 Supported Scopes

| Scope | Example | Tier | Source |
|-------|---------|------|--------|
| Environment | `${{ env.AWS_REGION }}` | 0 | Merged env vars |
| Matrix | `${{ matrix.stack }}` | 0 | Current matrix entry |
| Step outputs | `${{ steps.assume-role.outputs.ACCESS_KEY }}` | 0 | Parsed from `$CLAI_OUTPUT` |
| Step status | `${{ steps.deploy.outcome }}` | 0 | `"success"`, `"failure"`, `"skipped"` |
| Analysis | `${{ steps.check.analysis.decision }}` | 0 | `"proceed"`, `"halt"`, `"needs_human"` |
| Variables | `${{ vars.REGION }}` | 1 | Workflow-level vars |
| Job results | `${{ jobs.build.result }}` | 1 | `JobResult.Result` |
| Job outputs | `${{ jobs.build.outputs.KEY }}` | 1 | `JobResult.Outputs` |

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
    Jobs   map[string]*JobResult     // Tier 1 (P0-3)
}

type StepOutputs struct {
    Outputs  map[string]string
    Outcome  string              // "success", "failure", "skipped"
    Analysis *AnalysisResponse   // if analyze=true (defined in SS10.3)
}
```

### 9.4 Expression Safety

- All `${{ }}` expressions are resolved in Go **before** the command string is passed to ShellAdapter
- Unresolved expressions are a **hard error** -- execution stops, never passes broken `${{ }}` to a subprocess
- Step outputs used in expressions are inserted at the Go level (not shell-interpreted)
- In argv mode, resolved values become individual argv elements -- no shell interpretation occurs

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
```

```go
// validOutputKey checks that a key follows identifier rules: [A-Za-z_][A-Za-z0-9_]*
var validOutputKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isValidOutputKey(key string) bool {
    return validOutputKeyRe.MatchString(key)
}

const maxOutputFileSize = 10 * 1024 * 1024 // 10MB

// parseOutputFile reads a dotenv-style file with heredoc support for multiline values.
// Error handling (P1-4):
//   - Invalid key names -> error (must match [A-Za-z_][A-Za-z0-9_]*)
//   - Unterminated heredoc -> error
//   - Duplicate keys -> last-write-wins with warning logged
//   - Invalid UTF-8 -> error
//   - File > 10MB -> error
func parseOutputFile(path string) (map[string]string, error) {
    info, err := os.Stat(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // step didn't write outputs -- not an error
        }
        return nil, err
    }
    if info.Size() > maxOutputFileSize {
        return nil, fmt.Errorf("output file %s exceeds 10MB limit (%d bytes)", path, info.Size())
    }

    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    if !utf8.Valid(data) {
        return nil, fmt.Errorf("output file %s contains invalid UTF-8", path)
    }

    outputs := make(map[string]string)
    lines := strings.Split(string(data), "\n")
    for i := 0; i < len(lines); i++ {
        line := strings.TrimSpace(lines[i])
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        // Check for heredoc: KEY<<DELIMITER
        if parts := strings.SplitN(line, "<<", 2); len(parts) == 2 {
            key := strings.TrimSpace(parts[0])
            if !isValidOutputKey(key) {
                return nil, fmt.Errorf("invalid output key %q at line %d: must match [A-Za-z_][A-Za-z0-9_]*", key, i+1)
            }
            delimiter := strings.TrimSpace(parts[1])
            if delimiter == "" {
                return nil, fmt.Errorf("empty heredoc delimiter at line %d", i+1)
            }
            var value strings.Builder
            i++
            found := false
            for i < len(lines) {
                if strings.TrimSpace(lines[i]) == delimiter {
                    found = true
                    break
                }
                if value.Len() > 0 {
                    value.WriteByte('\n')
                }
                value.WriteString(lines[i])
                i++
            }
            if !found {
                return nil, fmt.Errorf("unterminated heredoc for key %q (expected delimiter %q)", key, delimiter)
            }
            if _, exists := outputs[key]; exists {
                log.Printf("warning: duplicate output key %q, using last value", key)
            }
            outputs[key] = value.String()
            continue
        }
        // Simple KEY=value
        if k, v, ok := strings.Cut(line, "="); ok {
            key := strings.TrimSpace(k)
            if !isValidOutputKey(key) {
                return nil, fmt.Errorf("invalid output key %q at line %d: must match [A-Za-z_][A-Za-z0-9_]*", key, i+1)
            }
            if _, exists := outputs[key]; exists {
                log.Printf("warning: duplicate output key %q, using last value", key)
            }
            outputs[key] = strings.TrimSpace(v)
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

- `claudeProcess` -- manages the persistent Claude CLI subprocess
- `DaemonRequest{Prompt}` / `DaemonResponse{Result, Error}` -- JSON protocol over Unix socket
- `QueryViaDaemon(ctx, prompt)` -- sends prompt to daemon, returns result
- `QueryFast(ctx, prompt)` -- tries daemon first, falls back to `QueryWithContext` (one-shot CLI)

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
```

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
    // FR-24: unparseable response -> needs_human (never guess the decision)
    return &AnalysisResponse{
        Decision:  "needs_human",
        Reasoning: raw,
    }, nil
}
```

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
```

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
```

### 10.10 LLM Resilience (MR-2)

All LLM calls (`QueryFast`, `Converse`) are wrapped with timeout and retry logic to prevent LLM outages from blocking workflow execution indefinitely.

**Policy:**
- **Timeout:** 30 seconds per attempt (`context.WithTimeout`)
- **Retry:** 1 retry on transient errors (timeout, connection refused, 5xx)
- **Final failure:** treat as `needs_human` (FR-24)

```go
const (
    llmTimeout    = 30 * time.Second
    llmMaxRetries = 1
)

func (r *Runner) queryLLMWithResilience(ctx context.Context, prompt string) (string, error) {
    var lastErr error
    for attempt := 0; attempt <= llmMaxRetries; attempt++ {
        reqCtx, cancel := context.WithTimeout(ctx, llmTimeout)
        result, err := r.llmClient.Query(reqCtx, prompt)
        cancel()
        if err == nil {
            return result, nil
        }
        lastErr = err
        if !isTransientError(err) {
            break // non-retryable (e.g., invalid prompt)
        }
        if attempt < llmMaxRetries {
            log.Printf("LLM attempt %d failed (%v), retrying...", attempt+1, err)
        }
    }
    return "", fmt.Errorf("LLM query failed after %d attempts: %w", llmMaxRetries+1, lastErr)
}

// isTransientError returns true for errors that may succeed on retry.
func isTransientError(err error) bool {
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }
    // Connection refused, reset, or timeout errors
    var netErr net.Error
    if errors.As(err, &netErr) {
        return netErr.Timeout() || !netErr.Temporary()
    }
    return false
}
```

The caller (step analysis in the runner loop) catches the final error from `queryLLMWithResilience` and falls back to `needs_human`, ensuring the workflow is never stuck waiting on an unavailable LLM.

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
```

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

    // ModeNonInteractiveAuto: auto-approve if risk allows (see SS4.4 behavior matrix)
    if step.RiskLevel == "low" || (step.RiskLevel == "medium" && analysis.Decision == "proceed") {
        fmt.Fprintf(ni.out, "[auto-approve] risk=%s decision=%s\n", step.RiskLevel, analysis.Decision)
        return DecisionApprove, nil
    }

    return DecisionReject, &NeedsHumanError{Step: step.Name, Decision: analysis.Decision}
}
```

**Stdin Handling (MR-7):** Non-interactive steps receive `stdin = /dev/null` (Unix) or `NUL` (Windows), providing immediate EOF. This prevents steps from accidentally blocking on stdin input. In interactive mode, `Stdin = os.Stdin` is passed through to allow subprocess interaction.

```go
// stdinForMode returns the appropriate stdin for the current execution mode.
func stdinForMode(mode ExecutionMode) io.Reader {
    if mode == ModeInteractive {
        return os.Stdin
    }
    // Non-interactive: /dev/null provides immediate EOF
    // Prevents steps from blocking on stdin
    return nil // exec.Cmd with nil Stdin reads from os.DevNull
}
```

### 11.4 Interactive Stdin for Steps

Some steps may require interactive user input (e.g., `read -p "Continue?"` in bash). The `ExecOpts.Stdin` field controls this:

- **Interactive mode:** `Stdin = os.Stdin` (user can interact with subprocess)
- **Non-interactive mode:** `Stdin = nil` (subprocess receives EOF on stdin read)

This is set per-step by the runner based on the current execution mode.

### 11.5 Color and Formatting (MR-8)

The CLI output respects standard color conventions:

```go
// shouldUseColor determines whether terminal colors should be used.
func shouldUseColor(out *os.File) bool {
    // NO_COLOR takes precedence (https://no-color.org/)
    if _, ok := os.LookupEnv("NO_COLOR"); ok {
        return false
    }
    // TERM=dumb disables color
    if os.Getenv("TERM") == "dumb" {
        return false
    }
    // Only use color when writing to a terminal
    return isatty(out)
}
```

Color is used for: step status indicators (green checkmark / red cross), section headers, timing information. All color output uses ANSI escape codes that are stripped when color is disabled.

---

## 12. State Management

### 12.1 Run ID Generation

```go
func generateRunID() string {
    return fmt.Sprintf("wfr-%d-%04x", time.Now().UnixMilli(), rand.Intn(0xFFFF))
}
```

Example: `wfr-1707654321000-a3f8`

**Collision Handling (MR-10):** On the unlikely event of a run ID collision, the runner retries once with a freshly generated ID before failing:

```go
// createRunWithRetry handles the unlikely case of run ID collision.
// On UNIQUE constraint violation, generates a new ID and retries once.
func (r *Runner) createRunWithRetry(ctx context.Context, wf *WorkflowDef) (string, error) {
    runID := generateRunID()
    err := r.store.CreateWorkflowRun(ctx, &WorkflowRun{RunID: runID, /* ... */})
    if err != nil && isUniqueViolation(err) {
        // Retry once with new ID (collision is extremely unlikely, double collision negligible)
        runID = generateRunID()
        err = r.store.CreateWorkflowRun(ctx, &WorkflowRun{RunID: runID, /* ... */})
    }
    if err != nil {
        return "", fmt.Errorf("create workflow run: %w", err)
    }
    return runID, nil
}
```

### 12.2 SQLite Schema (Migration V3)

Follows the existing migration pattern from `internal/storage/db.go`:

```go
const migrationV3 = `
-- Workflow runs
CREATE TABLE IF NOT EXISTS workflow_runs (
  run_id TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  workflow_path TEXT NOT NULL,
  workflow_hash TEXT,                   -- SHA-256 of workflow file for resume validation (MR-5)
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

-- Composite unique index for step identity (P0-4)
CREATE UNIQUE INDEX IF NOT EXISTS idx_wf_steps_composite
  ON workflow_steps(run_id, step_id, matrix_key);

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
//   "run_start"      -- run_id, workflow_name, workflow_path, execution_mode, params
//   "step_start"     -- step_id, step_name, job_name, matrix_key, command_masked
//   "step_output"    -- step_id, stdout_sample (first 1KB, masked), stderr_sample
//   "step_end"       -- step_id, status, exit_code, duration_ms
//   "analysis_start" -- step_id, analysis_prompt
//   "analysis_end"   -- step_id, decision, reasoning, flags
//   "human_decision" -- step_id, decision (approve/reject), mode
//   "run_end"        -- status, duration_ms, error
//   "error"          -- step_id (optional), message

func (ra *RunArtifact) WriteEvent(evt RunEvent) error {
    ra.mu.Lock()
    defer ra.mu.Unlock()
    // Mask all string values in evt.Data through SecretStore
    return ra.enc.Encode(evt)
}
```

SQLite rows are **indexed projections** of this artifact -- optimized for queries but not the canonical record.

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
UpdateWorkflowStep(ctx context.Context, runID string, stepID string, matrixKey string, update WorkflowStepUpdate) error
GetWorkflowSteps(ctx context.Context, runID string) ([]WorkflowStep, error)

// Workflow Analyses
CreateWorkflowAnalysis(ctx context.Context, a *WorkflowAnalysis) error
GetWorkflowAnalyses(ctx context.Context, runID string) ([]WorkflowAnalysis, error)

// Retention
PruneWorkflowRuns(ctx context.Context, maxPerWorkflow int) (int64, error)
```

**Note (P0-4):** `UpdateWorkflowStep` uses the composite key `(runID, stepID, matrixKey)` to identify a step row instead of an `int64` row ID. This matches the gRPC handler which receives `run_id`, `step_id`, and `matrix_key` as string fields from the RPC request. The unique index `idx_wf_steps_composite` guarantees this triple is unique.

### 12.5 Log File Layout

Full step output is written to log files (SQLite stores only 4KB tails):

```
~/.clai/workflow-logs/
  <run-id>/
    run.jsonl                                    -- structured event log (FR-40)
    <job>--<matrix-key>--<step-id>.stdout        -- full stdout
    <job>--<matrix-key>--<step-id>.stderr        -- full stderr
```

**Path sanitization:** `<job>`, `<matrix-key>`, and `<step-id>` values are sanitized via `sanitizePathComponent()` (SS7.5) to prevent path traversal or malformed filenames on all platforms.

### 12.6 Push-Based Stop Signals

The v1 spec used 500ms polling. v2 replaces this with a push-based approach:

```go
// Runner watches for stop signals via streaming RPC.
func (r *Runner) watchStopSignal(ctx context.Context, cancel context.CancelFunc) {
    stream, err := r.daemonClient.WorkflowWatch(ctx, &WorkflowWatchRequest{
        RunId: r.runID,
    })
    if err != nil {
        // Daemon unavailable -- graceful degradation, no stop signal support
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
```

### 12.7 Retention

- Default: keep last 100 runs per workflow name
- Pruning runs in the daemon's periodic maintenance loop
- When a run is pruned: `DELETE FROM workflow_runs WHERE run_id = ?` cascades to steps and analyses
- Log directory cleanup: scan for orphan directories not matching any DB run

### 12.8 Proto/DB Migration Lockstep (DG-1)

Protobuf schema changes and SQLite migrations are coupled to the binary release:

1. **Atomic delivery:** Both proto changes and migration SQL ship in the same binary. No separate migration step.
2. **Auto-apply on startup:** The daemon checks `PRAGMA user_version` on startup and applies pending migrations before accepting RPCs.
3. **Forward-compatible wire format:** New proto fields are additive (never remove or renumber). Old clients sending fewer fields is safe; new fields have zero-value defaults.
4. **Rollback:** Downgrading the binary may leave new columns in SQLite -- these are ignored by older code (SQLite is schema-flexible). No destructive rollback migration is provided.
5. **Version check:** The CLI sends its build version in the gRPC metadata. If the daemon is older than the CLI, the CLI logs a warning suggesting `clai daemon restart`.

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
```

### 13.3 Daemon Handler Pattern

Follows the existing pattern from `internal/daemon/handlers.go`. The handler now passes `req.RunId`, `req.StepId`, and `req.MatrixKey` to the store, matching the composite key signature from §12.4 (P0-4):

```go
func (s *Server) handleWorkflowStepUpdate(ctx context.Context, req *pb.WorkflowStepUpdateRequest) (*pb.WorkflowStepUpdateResponse, error) {
    // All string fields are already masked by the CLI before sending
    err := s.store.UpdateWorkflowStep(ctx, req.RunId, req.StepId, req.MatrixKey, WorkflowStepUpdate{
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

Note: The handler now passes `req.RunId`, `req.StepId`, and `req.MatrixKey` to the store, matching the composite key signature from §12.4. The v2 handler only passed `req.StepId` as a single identifier, which created a type mismatch between the gRPC string IDs and the store's int64 auto-increment key. The composite key model eliminates this mismatch entirely.

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
```

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

### 15.1 Exit Code Definitions

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
```

### 15.2 Exit Code Operator Runbook

CI/CD pipelines should handle exit codes as follows (DG-3):

| Code | CI Action | Example |
|------|-----------|---------|
| 0 | Continue pipeline | All checks passed |
| 1 | Fail build, inspect step logs | `clai workflow logs <run-id> --step <failed-step>` |
| 2 | Fix workflow YAML | `clai workflow validate <file>` to see errors |
| 3 | Review required — consider `--mode non-interactive-fail` for CI | Human rejected a step |
| 4 | Normal cancellation — no action needed | User pressed Ctrl+C |
| 5 | Workflow needs human review — cannot run unattended | Escalate to on-call |
| 6 | Start daemon or use `--daemon=false` | `clai daemon start` or run without daemon |
| 7 | Expected policy enforcement — check risk_level settings | LLM flagged high-risk |
| 8 | Install missing tools from `requires:` block | Check `which <tool>` |
| 124 | Increase step's `timeout_minutes` or investigate slow step | Step exceeded timeout |

**Exec-start failure mapping (MR-6):** When the subprocess itself fails to start (before any user code runs):
- `ENOENT` / `EACCES` (command not found / permission denied) → exit code 8 (`ExitDependencyMissing`)
- `ENOEXEC` (exec format error, e.g., wrong architecture) → exit code 2 (`ExitValidationError`)
- Other exec errors → exit code 1 (`ExitStepFailed`)

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
```

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

The context cancellation is created in the outer scope so that goroutines can trigger cancellation when `fail_fast` is enabled (P1-3):

```go
func (r *Runner) executeMatrixParallel(ctx context.Context, job *JobDef, entries []MatrixEntry) error {
    maxP := job.Strategy.MaxParallel
    if maxP <= 0 {
        maxP = len(entries) // unlimited
    }

    // Create cancellable context in outer scope so goroutines can trigger cancellation (P1-3)
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

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
                    cancel() // cancel is in scope — captured from outer context.WithCancel
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
```

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

**Workflow hash check (MR-5):** Before resuming, the runner validates that the workflow file has not changed since the original run:

```go
// Resume validation (MR-5): check that the workflow file hasn't changed since the original run.
func (r *Runner) validateResume(ctx context.Context, originalRunID string, currentPath string) error {
    run, err := r.store.GetWorkflowRun(ctx, originalRunID)
    if err != nil {
        return fmt.Errorf("load original run: %w", err)
    }
    if run.WorkflowHash != "" {
        currentHash, err := hashFile(currentPath)
        if err != nil {
            return fmt.Errorf("hash workflow file: %w", err)
        }
        if currentHash != run.WorkflowHash {
            return &WorkflowChangedError{
                RunID:        originalRunID,
                OriginalHash: run.WorkflowHash,
                CurrentHash:  currentHash,
            }
        }
    }
    return nil
}

func hashFile(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil {
        return "", err
    }
    defer f.Close()
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil {
        return "", err
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}
```

The `--force-resume` flag bypasses the hash check, allowing resume even when the workflow file has been modified. Use with caution — step IDs or matrix definitions may have changed.

---

## 19. Security

### 19.1 Command Execution Safety

**Argv mode (default)** eliminates the primary command injection vector:
- Commands are split into argv arrays by Go (not by a shell)
- No shell metacharacter interpretation occurs
- `exec.Command(argv[0], argv[1:]...)` does not invoke any shell

**Shell mode (opt-in)** is available when needed but carries documented risks:
- User explicitly opts in via `shell: true` or `shell: <name>` where name is one of: `bash`, `zsh`, `fish`, `pwsh`, `cmd`
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
```

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
    // Windows ACLs are complex; skip detailed permission check in Tier 0.
    // Log a warning so operators are aware of the gap.
    log.Printf("warning: file permission check skipped on Windows for %s (Tier 1 will add ACL verification)", path)
    return nil
}
```

In Tier 0 this is a **warning**. In Tier 1 with `workflows.strict_permissions: true`, it's an error.

### 19.5 Windows Security Caveats

**Tier 0 limitations on Windows (P1-5):**
- File permission checks (`checkFilePermissions`) log a warning and return nil. Workflow files are not verified for restrictive ACLs.
- Daemon IPC uses `--daemon=false` (no Unix socket available). State is not persisted remotely.

**Tier 1 plan:**
- Implement ACL verification using `windows.GetSecurityInfo()` and `windows.GetEffectiveRightsFromAcl()`.
- Verify that workflow files are only writable by the current user and Administrators group.
- Named pipe transport with user-scoped ACLs for daemon IPC.

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
```

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
```

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

```
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
  security_unix.go       # Unix file permission checks (//go:build !windows)
  security_windows.go    # Windows file permission checks (//go:build windows)
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
```

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
  --daemon=true|false          Enable/disable daemon IPC (default: true; Windows: false until Tier 1)
  --force-resume               Allow resume even if workflow file has changed (MR-5)
```

**Parameter handling (MR-4):** `-- PARAM=value` arguments after the double-dash are parsed as literal string key-value pairs. All parameter values are strings (no type coercion). Parameters are stored in `ExprContext.Vars` and accessible via `${{ vars.PARAM }}` expressions. Unknown parameters (not declared in `params:` block, Tier 1) produce a validation warning.

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
```

### 22.4 Help Text and Man Pages

Cobra auto-generates `--help` output for all commands. Additionally (DG-5):

```go
// Each command includes an Example field for help text:
cmd := &cobra.Command{
    Use:     "workflow run <file-or-name> [flags] [-- PARAM=value ...]",
    Short:   "Execute a workflow",
    Long:    "Run a workflow file or named workflow with optional parameters.",
    Example: `  # Run a workflow file
  clai workflow run ./compliance-check.yaml

  # Run with environment override
  clai workflow run pulumi-compliance --env AWS_REGION=us-east-1

  # Non-interactive CI mode
  clai workflow run --mode non-interactive-fail pulumi-compliance

  # Run specific matrix entry
  clai workflow run pulumi-compliance --matrix stack:production`,
}
```

Man pages can be generated from Cobra command definitions using `cobra/doc`:

```go
import "github.com/spf13/cobra/doc"
doc.GenManTree(rootCmd, &doc.GenManHeader{Title: "CLAI", Section: "1"}, "./man/")
```

This is a build-time step, not runtime.

---

## 23. Testing Strategy

### 23.1 Unit Tests

Each file in `internal/workflow/` has a corresponding `_test.go` file. Key coverage:

- **schema_test.go:** YAML parsing edge cases, missing fields, invalid types
- **validate_test.go:** All validation rules, multi-error collection
- **matrix_test.go:** Expansion with include/exclude, empty matrix, single entry
- **expr_test.go:** All scopes, nested references, unresolved expression errors, multiline output parsing
- **secrets_test.go:** Loading from env, masking, dual scrubbing
- **modes_test.go:** TTY detection mocking, decision matrix for all risk x decision combinations
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
```

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

### 23.6 CI Gates and E2E Test Harness

**Hard CI gates (AR-8):**

| Gate | Command | Threshold |
|------|---------|-----------|
| Race detector | `go test -race ./...` | Zero races |
| Cross-OS matrix | `GOOS=linux,darwin,windows go build` | All must compile |
| Binary size | `ls -la bin/clai` | < 2MB increase from baseline |
| Unit test timing | `go test -timeout 60s ./internal/workflow/...` | All under 60s |
| Lint | `golangci-lint run` | Zero findings |

**E2E test harness (DG-6):**

Non-interactive workflows are tested using `testscript` (from `golang.org/x/tools`):

```
# testdata/script/workflow_simple.txt
exec clai workflow run testdata/workflows/simple-chain.yaml --mode non-interactive-fail
stdout 'step.*success'
! stderr 'error'
```

Interactive prompts are tested using mock `InteractionHandler`:

```go
type ScriptedInteractionHandler struct {
    decisions []Decision // pre-loaded approve/reject sequence
    idx       int
}

func (s *ScriptedInteractionHandler) Review(ctx context.Context, step *StepDef, result *StepResult, analysis *AnalysisResponse) (Decision, error) {
    if s.idx >= len(s.decisions) {
        return DecisionReject, fmt.Errorf("no more scripted decisions")
    }
    d := s.decisions[s.idx]
    s.idx++
    return d, nil
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
- [ ] LLM unparseable response → `needs_human` → follows risk matrix (FR-24, P0-2)
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
- [ ] Works on Linux and macOS (full daemon IPC); Windows: engine runs (argv/shell execution, LLM analysis), daemon IPC deferred to Tier 1
- [ ] `clai workflow validate` checks YAML without executing
- [ ] Strict YAML parsing rejects unknown fields
- [ ] Output parser validates key names and handles malformed files

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
| YAML workflow files | Adopted | GHA-style syntax (not dagu-style) | FR-2 |
| Step command execution | Adapted | argv default + shell opt-in (dagu uses shell) | Safer than dagu |
| `secrets:` block | Adopted | env, file, interactive sources | Inspired by dagu's secret providers |
| Headless mode | Adopted | `ModeNonInteractiveFail` / `ModeNonInteractiveAuto` | Inspired by `DAGU_HEADLESS` |
| Process tree termination | Adopted | ProcessController with job objects (Windows) | From dagu Windows support |
| Exit code 124 (timeout) | Adopted | `ExitTimeout = 124` | Standard convention |
| Output variables | Adapted | `$CLAI_OUTPUT` file (not `${VAR}` dagu-style) | GHA-compatible |
| Variable expansion | Adapted | `${{ }}` GHA-style (not `${VAR}` dagu-style) | Go-side evaluation for safety |
| Environment variables | Adopted | Workflow → job → step env merge | Same precedence model |
| Lifecycle handlers | Adopted (Tier 1) | `onSuccess`, `onFailure`, `onExit` | Identical semantics |
| `maxActiveSteps` | Adopted (Tier 1) | `max_parallel` semaphore-bounded goroutines | Same concept |
| Preconditions | Adopted (Tier 1) | Command, env, file checks | Same concept |
| JSON execution history | Adopted | RunArtifact JSONL | FR-40 |
| Retry with backoff | Deferred | Tier 2 | Not needed for Pulumi use case |
| `continueOn` | Deferred | Tier 2 | Fine-grained failure control |
| Sub-workflows | Deferred | Tier 2 | Dagu's child DAG pattern |
| File-based state | Replaced | SQLite (existing clai pattern) | Single source of truth |
| Web UI | Omitted | CLI-only (Tier 2 consideration) | |
| Docker/SSH/HTTP executors | Omitted | Shell commands only | Use `docker run`, `curl`, `ssh` |

---

## 26. Non-Functional Requirements

### 26.1 Shell Completion

Use cobra's built-in completion generation:

```go
// Auto-generated by cobra for zsh, bash, fish
cmd.GenBashCompletionFile("completions/clai.bash")
cmd.GenZshCompletionFile("completions/_clai")
cmd.GenFishCompletionFile("completions/clai.fish")
```

Custom completions for:
- Workflow name completion: reads from discovery directories
- `--matrix` key completion: parses YAML and lists matrix keys
- `--resume` run ID completion: queries daemon for recent failed runs

**Installation paths (DG-2):**
- **bash:** `clai completion bash > /etc/bash_completion.d/clai` (system) or `~/.bash_completion` (user)
- **zsh:** `clai completion zsh > ${fpath[1]}/_clai` (requires `compinit`)
- **fish:** `clai completion fish > ~/.config/fish/completions/clai.fish`
- **PowerShell (Tier 1):** `clai completion powershell > $PROFILE.CurrentUserAllHosts` (requires PSReadLine)

### 26.2 Performance Budget

| Metric | Target | Rationale |
|--------|--------|-----------|
| Binary size increase | < 2 MB | Workflow package is pure Go, no CGo |
| `clai workflow validate` | < 100ms for 200-line YAML | YAML parsing + validation only |
| Memory per step | ~12 KB overhead | 2 x 4KB limitedBuffer + ExprContext |
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
| D15 | Shell field type | (a) `*bool` + `string` (b) unified `string` | **(b)** | Single field handles bool and string YAML values via custom UnmarshalYAML. Eliminates schema contradiction. (P0-1) |
| D16 | Store key model | (a) int64 auto-increment (b) composite (runID, stepID, matrixKey) | **(b)** | Matches RPC identity model. Eliminates type mismatch between gRPC string IDs and store int64. (P0-4) |
| D17 | YAML strictness | (a) Lenient (ignore unknown) (b) Strict (error on unknown) | **(b)** | Prevents silent typos in workflow files. Users get immediate feedback on misspelled keys. (P1-2) |
| D18 | Windows daemon IPC | (a) Claim parity now (b) Defer, provide --daemon=false | **(b)** | Unix sockets don't exist on Windows. Named pipes need distinct implementation. Defer to Tier 1. (P0-5) |
