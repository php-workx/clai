# Plan: Workflow Execution — Tier 0 Implementation

**Date:** 2026-02-11 (revised post pre-mortem)
**Source:** `specs/tech_workflows_v4.md` v4.2 (approved), pre-mortem council report
**Scope:** Tier 0 only (~5-7 weeks, D28)
**Restructured:** 18 issues / 4 waves (was 20/5, per D30)

## Overview

Implement the complete Tier 0 workflow execution engine for clai: YAML parsing + validation, shell execution, expression interpolation, LLM-powered step analysis (core + transport split) with human review, SQLite persistence via claid gRPC, JSONL run artifacts, and CLI commands (`workflow run` + `workflow validate`). Follows §3.8 build sequence.

## Post Pre-Mortem Changes (D29-D32)

| Decision | Choice | Impact |
|----------|--------|--------|
| D29: RunArtifact JSONL tier | Keep in Tier 0 + document | Resolves FR-39/FR-40 spec contradiction |
| D30: Apply scope merges | Yes: 19→18, 8→3, 9→13 | 20→18 issues, 5→4 waves |
| D31: Split Issue 12 | Yes: 14a (core) + 21 (transport) | Surfaces D24 dual-path complexity |
| D32: Move Issue 15 | To Wave 2 | Better parallelism, only needs types |

Critical findings addressed: C1 (TTY detection → Issue 18), C2 (daemon client → Issue 18), C3 (Store mock stubs → Issue 2), C4 (dual-LLM split → Issues 14+21), C5 (RunArtifact tier → D29).

## Issues

### Issue 1: Proto schema — Workflow RPC messages and service extension
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- 8 new messages defined in `proto/clai/v1/clai.proto` (4 request + 4 response)
- 4 new RPCs added to `ClaiService` (WorkflowRunStart, WorkflowRunEnd, WorkflowStepUpdate, AnalyzeStepOutput)
- `make proto` generates cleanly into `gen/clai/v1/`
- Proto comments reference spec sections
**Description:**
Add Tier 0 workflow RPC definitions per §13.1. Messages: `WorkflowRunStartRequest` (with `workflow_hash` field per M12, `started_at_unix_ms` per M18), `WorkflowStepUpdateRequest` (composite key: run_id + step_id + matrix_key per D16), `AnalyzeStepOutputRequest` (with `scrubbed_output` ≤100KB per C5), `AnalyzeStepOutputResponse` (decision + reasoning + flags_json). Tier 1 RPCs (WorkflowWatch, WorkflowStatus, WorkflowStop, WorkflowHistory) are NOT included per D25/D26.

### Issue 2: SQLite schema — Migration V3 + Store interface extension
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `migrationV3` constant in `internal/storage/db.go` creates 3 tables: `workflow_runs`, `workflow_steps`, `workflow_analyses`
- `Store` interface has 9 new methods (additive, C4 — no existing methods changed)
- `SQLiteStore` implements all 9 methods in new file `internal/storage/workflow.go`
- `schema_meta` migration tracking (D27)
- CRITICAL (C3): Update existing `mockStore` in `handlers_test.go` with stubs for new methods — verify `make test` passes
- PruneWorkflowRuns omitted entirely (additive interface, Tier 1)
- Unit tests for all CRUD operations
**Description:**
Extend the existing SQLite store per §12.2-12.5. Tables use composite key `(run_id, step_id, matrix_key)` per D16. Define Go structs: `WorkflowRun`, `WorkflowStep`, `WorkflowStepUpdate`, `WorkflowAnalysis`, `WorkflowAnalysisRecord`, `WorkflowRunQuery` (all per M13/M14). Method name: `EnvVars()` not `Env()` per M15.

### Issue 3: YAML parser + types + validator (merged from Issues 3+4)
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `internal/workflow/types.go` defines `WorkflowDef`, `JobDef`, `StepDef`, `MatrixStrategy`, `SecretStore` structs
- `internal/workflow/parser.go` parses YAML into `WorkflowDef`
- `internal/workflow/validate.go` validates `WorkflowDef` post-parse
- Strict YAML parsing rejects unknown fields (D17, `KnownFields(true)`)
- Custom `UnmarshalYAML` for `ShellField` handles bool + string (D15)
- `validShellValues` includes `sh` (m11)
- Matrix `include` entries expand correctly
- Validator checks: step IDs unique, required fields present, risk_level valid (low/medium/high)
- Expression reference validation (`${{ steps.X }}` verifies step X exists) deferred to Issue 9 (expression engine)
- `sanitizePathComponent()` (SS7.5)
- Spike yaml.v3 `KnownFields(true)` + `inline` + custom `UnmarshalYAML` interaction first (M5)
- Unit tests for parse + validation rules
**Description:**
Per §4, §5, §7. Parse GHA-style YAML workflow files and validate. Key types: `WorkflowDef` (name, env, secrets, jobs), `JobDef` (name, strategy, steps, env), `StepDef` (id, name, run, shell, env, analyze, analysis_prompt, risk_level, timeout), `MatrixStrategy` (include entries), `SecretStore` (loads from env vars in Tier 0). Validator operates directly on parser output types. Note yaml.v3 `KnownFields(true)` + custom UnmarshalYAML + `inline` tag interaction (m16 risk — spike and test explicitly). Use `github.com/google/shlex` for shell tokenization (m17).

### Issue 4: ShellAdapter (cross-platform)
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `internal/workflow/shell.go` defines `ShellAdapter` interface
- `internal/workflow/shell_unix.go` (build tag: `!windows`) implements argv mode (default) and shell mode
- `internal/workflow/shell_windows.go` (build tag: `windows`) implements via `cmd.exe /C`
- Argv mode: splits command via `shlex.Split()`, executes directly
- Shell mode: wraps in `/bin/sh -c` (or configured shell)
- `$CLAI_OUTPUT` temp file path set in environment
- Unit tests for both modes (argv + shell) on current platform
**Description:**
Per §5. Two execution modes: argv default (D8 — eliminates command injection) and shell opt-in for pipes/redirects. The adapter builds `exec.Cmd` with environment merged (step > job > workflow precedence per FR-4). Creates `$CLAI_OUTPUT` temp file for step output capture. Platform-specific via build tags matching existing `internal/ipc/spawn_{unix,windows}.go` pattern (D14).

### Issue 5: ProcessController (cross-platform)
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `internal/workflow/process.go` defines `ProcessController` interface
- `internal/workflow/process_unix.go` uses `pgid` for process group management
- `internal/workflow/process_windows.go` uses `CREATE_NEW_PROCESS_GROUP` + `GenerateConsoleCtrlEvent`
- Simplified to match existing `spawn_windows.go` pattern (CREATE_NEW_PROCESS_GROUP only, M6)
- Ctrl+C sends `SIGINT` to process group, waits grace period, then `SIGKILL`
- `signal.NotifyContext` integration for parent-level cancellation
- Unit tests with a subprocess that traps signals
**Description:**
Per §6. Manages subprocess lifecycle: start, signal, kill. Uses process groups (pgid on Unix, CREATE_NEW_PROCESS_GROUP on Windows) to ensure child processes are cleaned up. Grace period before escalation from SIGINT → SIGKILL. Integrates with `context.Context` cancellation. Windows simplified per M6.

### Issue 7: Config extension — WorkflowsConfig
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `WorkflowsConfig` struct added to `internal/config/config.go`
- Tier 0 acceptance criteria fields: `Enabled`, `DefaultMode`, `DefaultShell`, `LogDir`
- Struct may define `SearchPaths`, `RetainRuns`, `StrictPermissions`, `SecretFile` (Go zero values harmless) but these are Tier 1 — no Tier 0 code consumes them (M10)
- `Paths.WorkflowLogDir()` method added to `internal/config/paths.go`
- Defaults: `Enabled: false`, `DefaultMode: "interactive"`
- Add manual switch-case dispatch in Get/Set methods (6-8 method changes needed)
- Existing config loading unchanged (additive)
- Unit test for config parsing with workflow section
**Description:**
Per §14. Extend the existing config system with workflow-specific settings. The `workflows:` YAML key is added to `Config`. Default mode is interactive. Tier 1 config fields (SearchPaths, RetainRuns, StrictPermissions, SecretFile) defined in struct but not consumed in Tier 0.

### Issue 9: Expression engine
**Wave:** 2
**Dependencies:** Issue 3 (types)
**Acceptance:**
- `internal/workflow/expression.go` evaluates `${{ }}` expressions
- Supports: `env.VAR`, `matrix.KEY`, `steps.ID.outputs.KEY`
- Unresolved expressions produce hard error (not passed to subprocess)
- Partial evaluation: env/matrix resolved before execution, step outputs resolved at runtime
- Expression reference validation: `${{ steps.X.outputs.Y }}` verifies step X exists (moved from validator)
- No shell variable collision (`${{ }}` is distinct from `$VAR`)
- Unit tests for all expression types, error cases, and nested expressions
**Description:**
Per §4.5. GHA-style expression evaluation (D5). Three namespaces in Tier 0: `env`, `matrix`, `steps`. Expressions like `${{ steps.pulumi_preview.outputs.DEPLOY_TOKEN }}` resolve from the step output map. Runtime resolution: `env` and `matrix` are known at parse time; `steps.X.outputs.Y` requires deferred resolution after step X executes. Unresolved expressions MUST hard-error. Includes expression reference validation moved from the former validator issue.

### Issue 10: Output parser (`$CLAI_OUTPUT`)
**Wave:** 2
**Dependencies:** Issue 4 (ShellAdapter creates the output file)
**Acceptance:**
- `internal/workflow/output.go` parses `KEY=value` lines from `$CLAI_OUTPUT` file
- Validates key names (alphanumeric + underscore)
- Handles malformed files gracefully (skip bad lines, log warning)
- Tier 0: KEY=value only (heredoc multiline deferred to Tier 1 per m20)
- Unit tests for: valid output, malformed lines, empty file, missing file
**Description:**
Per §8. After each step executes, read the `$CLAI_OUTPUT` temp file and parse KEY=value pairs. These outputs become available via `${{ steps.ID.outputs.KEY }}`. Only KEY=value format in Tier 0 (heredoc delimiter format deferred to Tier 1, m20).

### Issue 11: Secret masking
**Wave:** 2
**Dependencies:** Issue 3 (SecretStore type)
**Acceptance:**
- `internal/workflow/secrets.go` implements `SecretMasker`
- Loads secrets from env vars (Tier 0)
- Masks all secret values with `***` in: stored output, LLM context, JSONL artifacts, log messages
- Integrates with existing `internal/sanitize/` patterns
- Unit tests for: masking in various contexts, overlapping secrets, empty secrets
**Description:**
Per §10, FR-36. All outputs sent to LLM, stored in SQLite, or written to JSONL must have secret values replaced. Tier 0 loads secrets from environment variables only (`.secrets` file loading deferred to Tier 1). Pattern: collect all secret values from `SecretStore.EnvVars()`, do literal string replacement in all output paths.

### Issue 12: Daemon handlers — LLMQuerier + 4 workflow RPCs
**Wave:** 2
**Dependencies:** Issue 1 (proto), Issue 2 (Store)
**Acceptance:**
- `LLMQuerier` interface + `claudeQuerier` production impl in `internal/daemon/`
- `Server` struct gains `llm LLMQuerier` field, injected at construction
- 4 new handlers in `internal/daemon/workflow_handlers.go`
- `handleAnalyzeStepOutput` uses `s.llm.Query()` (not direct `claude.QueryFast()` — D23)
- All handlers call `s.touchActivity()` (idle timeout counting — M16)
- `llmTimeout`: single 120s timeout for all calls (Tier 0 simplification per scope judge — no first-call-specific timeout)
- Unit tests with mock `LLMQuerier` and mock `Store`
**Description:**
Per §13.3-13.4, D22, D23. Add workflow RPC handlers to claid following the existing pattern in `handlers.go`. Key handler: `AnalyzeStepOutput` — builds prompt, queries LLM via `LLMQuerier` interface, persists analysis record, returns decision. The `LLMQuerier` is injected so tests don't need the Claude daemon.

### Issue 13: Sequential executor (DAG runner) — includes limitedBuffer
**Wave:** 3
**Dependencies:** Issue 4 (ShellAdapter), Issue 5 (ProcessController), Issue 9 (expressions), Issue 10 (output parser), Issue 11 (secrets)
**Acceptance:**
- `internal/workflow/runner.go` orchestrates a complete workflow run
- `internal/workflow/executor.go` executes a single step
- `internal/workflow/buffer.go` implements `limitedBuffer` (fixed-capacity ring buffer, 4KB tails per FR-7, thread-safe with `sync.Mutex`, `io.Writer` interface)
- Sequential execution within matrix entries (Tier 0, D7)
- Matrix entries executed sequentially (Tier 0 — parallel deferred to Tier 1)
- Step failure halts remaining steps in the matrix entry
- `context.Context` propagated for Ctrl+C cancellation
- Step outputs accumulated and available to downstream expressions
- Environment precedence: step > job > workflow
- Tests use mock analyzer (M4: explicit interface boundary with LLM analysis)
- Unit tests with mock ShellAdapter + buffer overflow/concurrent write tests
**Description:**
Per §9, §5.6. The core execution engine + limitedBuffer (merged per D30). For each matrix entry, runs steps sequentially. Each step: resolve expressions → build environment → execute via ShellAdapter → capture output (via limitedBuffer ring buffer) → parse $CLAI_OUTPUT → update output map. On failure (non-zero exit): halt remaining steps for that matrix entry. The runner holds the accumulated state (outputs, env) and drives the execute→analyze→review loop.

### Issue 14: LLM analysis — core (prompt, parsing, risk matrix)
**Wave:** 2
**Dependencies:** Issue 3 (types), Issue 11 (secret masking)
**Acceptance:**
- `internal/workflow/analyze.go` builds analysis context and prompts
- `buildAnalysisContext()` produces ≤100KB head+tail scrubbed output
- Rune-aware truncation (D20 — no broken UTF-8)
- `parseAnalysisResponse()` extracts decision/reasoning/flags from LLM JSON
- Unparseable LLM response → `needs_human` (FR-24)
- Risk matrix: `shouldPromptHuman(decision, riskLevel)` per §10.7
- `LLMQuerier` interface (D23) for DI/testing
- `isTransientError` with typed error checks, no string matching (M7/M11)
- Retry logic for transient LLM errors
- Unit tests with mock LLMQuerier
**Description:**
Per §10 (core). The analysis engine without transport concerns. Builds context, constructs prompt, parses LLM response, applies risk matrix. Split from transport (Issue 21) per D31. Uses `LLMQuerier` interface for testing without real LLM.

### Issue 15: Human review UI (interactive mode)
**Wave:** 2
**Dependencies:** Issue 3 (types — only needs analysis result types)
**Acceptance:**
- `internal/workflow/review.go` implements `InteractionHandler` interface and `TerminalReviewer`
- Options: `[a]pprove / [r]eject / [i]nspect / [c]ommand / [q]uestion`
- `[q]uestion` uses single-turn `QueryFast()` (Tier 0 — multi-turn deferred per FR-33)
- `[i]nspect` shows full output
- `[c]ommand` runs ad-hoc shell command
- Non-interactive-fail mode: returns reject with exit code 5 when human review needed
- `ModeNonInteractiveAuto` is rejected with error in Tier 0 (§4.1)
- Unit tests with scripted stdin
**Description:**
Per §11, FR-30-32. Moved to Wave 2 per D32 — only needs analysis result types (decision enum, reasoning string, flags map), not the full LLM integration. The review interface presents LLM analysis results and available options. Uses `InteractionHandler` interface for testability with `ScriptedInteractionHandler`.

### Issue 16: RunArtifact (JSONL event log)
**Wave:** 3
**Dependencies:** Issue 13 (executor produces events)
**Acceptance:**
- `internal/workflow/artifact.go` implements `ArtifactWriter`
- JSONL format with event types: `run_start`, `step_start`, `step_end`, `analysis`, `human_decision`, `run_end`
- Paths normalized to forward slashes on all platforms (D19)
- File written to `~/.clai/workflow-logs/<run-id>.jsonl`
- `WriteEvent` errors are logged but don't halt execution
- Unit tests verify JSONL structure and path normalization
**Description:**
Per §12.3, FR-39/FR-40. Kept in Tier 0 per D29 — referenced in build sequence and acceptance criteria despite FR table placement. Append-only JSONL event log for every workflow run.

### Issue 17: Console output + progress display
**Wave:** 3
**Dependencies:** Issue 13 (executor drives progress)
**Acceptance:**
- `internal/workflow/display.go` implements progress output
- TTY mode: step names, status icons, durations, spinner
- Non-TTY / `TERM=dumb`: one line per event, no `\r` overwrites, no spinners
- Matrix key shown when running matrix entries
- Step status: pending → running → passed/failed/skipped
- Unit tests with captured output
**Description:**
Per §11, FR-41. Real-time progress display during workflow execution. Adapts to terminal capabilities: rich output with spinners in TTY mode, log-friendly single-line events in non-TTY mode.

### Issue 18: CLI commands — `workflow run` + `workflow validate`
**Wave:** 4
**Dependencies:** Issue 3 (parser+validator), Issue 7 (config), Issue 12 (daemon handlers), Issue 13 (executor), Issue 15 (review UI), Issue 16 (RunArtifact), Issue 17 (display), Issue 21 (LLM transport)
**Acceptance:**
- `internal/cmd/workflow.go` defines `workflowCmd` with `run` and `validate` subcommands
- `workflow run <file>` executes a workflow with flags: `--mode`, `--shell`, `--daemon`, `--verbose`
- `workflow run -` reads YAML from stdin (D21)
- `workflow validate <file>` validates without executing
- TTY detection (spec SS4.3) for mode auto-detection and display formatting (C1)
- Daemon client layer: gRPC connection, EnsureDaemon(), --daemon=false path (C2)
- Workflow file discovery: spec SS7.4 name-based lookup (M2)
- File permission checks: SS19.2-19.5, warning-only in Tier 0 (M3)
- Pre-run dependency detection: exit code 8 for missing deps (M1)
- Exit codes per §15: 0=success, 1=step failure, 2=system error, 3=human reject, 4=Ctrl+C, 5=human decision in non-interactive, 8=pre-run dependency error (merged from former Issue 19 per D30)
- Quality error messages with step name and matrix key context
- Registered in `root.go` under new `groupWorkflow` command group
**Description:**
Per §26, §15. The CLI entry point. `workflow run` ties everything together: parse YAML → validate → connect to claid (or fallback) → execute → display progress → write artifact → exit. `workflow validate` is the dry-run: parse + validate only. Includes exit codes and error messages (merged from former Issue 19 per D30). TTY detection and daemon client layer added per C1/C2.

### Issue 20: Integration tests + smoke test
**Wave:** 4
**Dependencies:** Issue 18 (CLI commands)
**Acceptance:**
- Unit tests for all packages (each issue above includes unit tests)
- Integration test: full workflow run with mock shell commands (no real infrastructure)
- Smoke test: run the actual Pulumi YAML from func spec §6 with mock commands
- `ScriptedInteractionHandler` for deterministic human review in tests
- `go test -race` included (m5)
- `make test` passes
- `make lint` passes
**Description:**
Per §23. The testing harness ties together all components. Begins as soon as Issue 18 has minimal functionality. Key: happy-path smoke test running the actual Pulumi compliance YAML (with mock commands that produce expected output). This validates the full pipeline: parse → validate → execute → analyze → review → persist → artifact.

### Issue 21: LLM analysis — transport (RPC + QueryFast fallback)
**Wave:** 3
**Dependencies:** Issue 14 (LLM core), Issue 12 (daemon handlers)
**Acceptance:**
- Two transport paths for LLM queries:
  - (1) claid `AnalyzeStepOutput` RPC (preferred — centralized logging)
  - (2) CLI-side direct `QueryFast()` fallback when claid unavailable (D24)
- If both fail → `needs_human`
- Warning logged when falling back to direct QueryFast (missing centralized persistence)
- Connection logic: EnsureDaemon() check, gRPC dial with timeout, graceful fallback
- Unit tests for both paths + fallback behavior
**Description:**
Per §10, D24 (transport). Split from Issue 14 per D31. The two code paths for LLM queries create different error handling and connection logic. Separated to surface the transport complexity explicitly.

## Execution Order

**Wave 1** (parallel — 6 issues, no dependencies):
- Issue 1: Proto schema
- Issue 2: SQLite schema + Store (+ mock stubs for existing tests, C3)
- Issue 3: YAML parser + types + validator (merged with former Issue 4, D30)
- Issue 4: ShellAdapter
- Issue 5: ProcessController
- Issue 7: Config extension

**Wave 2** (after Wave 1 — 6 issues):
- Issue 9: Expression engine (← Issue 3)
- Issue 10: Output parser (← Issue 4)
- Issue 11: Secret masking (← Issue 3)
- Issue 12: Daemon handlers (← Issues 1, 2)
- Issue 14: LLM analysis core (← Issues 3, 11)
- Issue 15: Human review UI (← Issue 3) — moved from Wave 3 per D32

**Wave 3** (after Wave 2 — 4 issues):
- Issue 13: Sequential executor + limitedBuffer (← Issues 4, 5, 9, 10, 11) — merged with former Issue 6, D30
- Issue 16: RunArtifact JSONL (← Issue 13) — kept in Tier 0 per D29
- Issue 17: Console output (← Issue 13)
- Issue 21: LLM transport (← Issues 12, 14) — new split per D31

**Wave 4** (after Wave 3 — 2 issues):
- Issue 18: CLI commands + exit codes (← Issues 3, 7, 12, 13, 15, 16, 17, 21) — merged with former Issue 19, D30
- Issue 20: Integration tests (← Issue 18) — moved from Wave 5

## Dependency Validation

All declared dependencies are **file-level or data-flow** (not just logical ordering):

| Dependency | Reason |
|-----------|--------|
| Issue 9 ← 3 | Expressions reference `StepDef`, `MatrixStrategy` types |
| Issue 10 ← 4 | Output parser reads the `$CLAI_OUTPUT` file created by ShellAdapter |
| Issue 11 ← 3 | SecretMasker uses `SecretStore` type |
| Issue 12 ← 1, 2 | Handlers use generated proto types and Store interface |
| Issue 14 ← 3, 11 | LLM analysis uses types and secret masking |
| Issue 15 ← 3 | Review UI uses analysis result types (decision, reasoning, flags) |
| Issue 13 ← 4, 5, 9, 10, 11 | Executor calls ShellAdapter, ProcessController, expressions, output parser, secret masker |
| Issue 16 ← 13 | Artifact writer receives events from executor |
| Issue 17 ← 13 | Display receives progress events from executor |
| Issue 21 ← 12, 14 | Transport uses daemon handlers and core analysis |
| Issue 18 ← (many) | CLI command wires all components together |
| Issue 20 ← 18 | Integration test exercises the full CLI |

## Risk Notes

- **M5**: yaml.v3 `KnownFields(true)` + custom `UnmarshalYAML` + `inline` tag has a known bug risk. Spike first in Issue 3.
- **M20**: First `AnalyzeStepOutput` call may hit 90-second Claude daemon cold-start. Single 120s timeout for all calls (simplified per scope judge).
- **M21**: Claude daemon single-threaded mutex serializes concurrent `AnalyzeStepOutput` RPCs. Acceptable in Tier 0 (sequential execution).
- **D24 fallback**: CLI-side LLM fallback split into Issue 21 (transport) for clarity.
- **C3**: Store interface expansion breaks existing mocks — must update in same PR as Issue 2.

## Effort Estimate

- **Wave 1**: 1.5-2 weeks (6 parallel issues, all foundational)
- **Wave 2**: 1-1.5 weeks (6 parallel issues, moderate complexity)
- **Wave 3**: 1-1.5 weeks (4 issues, executor is the most complex)
- **Wave 4**: 1-1.5 weeks (CLI wiring + integration tests)

**Total: 5-7 weeks** (consensus estimate from pre-mortem council)

## Next Steps
- Run `/crank` for autonomous execution, or `/implement <issue>` for individual issues
