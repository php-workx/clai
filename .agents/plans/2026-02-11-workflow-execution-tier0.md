# Plan: Workflow Execution — Tier 0 Implementation

**Date:** 2026-02-11
**Source:** `specs/tech_workflows_v4.md` v4.2 (approved), round 2 pre-mortem council report
**Scope:** Tier 0 only (~5-6 weeks, D28)

## Overview

Implement the complete Tier 0 workflow execution engine for clai: YAML parsing, shell execution, expression interpolation, LLM-powered step analysis with human review, SQLite persistence via claid gRPC, JSONL run artifacts, and CLI commands (`workflow run` + `workflow validate`). Follows §3.8 build sequence.

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
- Unit tests for all CRUD operations
**Description:**
Extend the existing SQLite store per §12.2-12.5. Tables use composite key `(run_id, step_id, matrix_key)` per D16. Define Go structs: `WorkflowRun`, `WorkflowStep`, `WorkflowStepUpdate`, `WorkflowAnalysis`, `WorkflowAnalysisRecord`, `WorkflowRunQuery` (all per M13/M14). `PruneWorkflowRuns` stubbed (returns 0, Tier 1). Method name: `EnvVars()` not `Env()` per M15.

### Issue 3: YAML parser + types
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `internal/workflow/types.go` defines `WorkflowDef`, `JobDef`, `StepDef`, `MatrixStrategy`, `SecretStore` structs
- `internal/workflow/parser.go` parses YAML into `WorkflowDef`
- Strict YAML parsing rejects unknown fields (D17, `KnownFields(true)`)
- Custom `UnmarshalYAML` for `ShellField` handles bool + string (D15)
- `validShellValues` includes `sh` (m11)
- Matrix `include` entries expand correctly
- Pulumi example YAML parses to correct `WorkflowDef`
- Unit tests cover: valid YAML, unknown fields rejected, shell field bool/string, matrix expansion
**Description:**
Per §4, §7. Parse GHA-style YAML workflow files. Key types: `WorkflowDef` (name, env, secrets, jobs), `JobDef` (name, strategy, steps, env), `StepDef` (id, name, run, shell, env, analyze, analysis_prompt, risk_level, timeout), `MatrixStrategy` (include entries), `SecretStore` (loads from env vars in Tier 0). Note yaml.v3 `KnownFields(true)` + custom UnmarshalYAML + `inline` tag interaction (m16 risk — test explicitly). Use `github.com/google/shlex` for shell tokenization (m17).

### Issue 4: Workflow validator (`workflow validate`)
**Wave:** 2
**Dependencies:** Issue 3
**Acceptance:**
- `internal/workflow/validate.go` validates `WorkflowDef` post-parse
- Checks: step IDs unique, step ID references in expressions exist, required fields present, risk_level values valid
- Expression reference validation: `${{ steps.X.outputs.Y }}` verifies step X exists
- Returns structured error list (not just first error)
- Unit tests for each validation rule
**Description:**
Per §7. Validates parsed `WorkflowDef` before execution. Must catch: duplicate step IDs, invalid risk levels, unknown step references in expressions, missing `run` fields, invalid shell values. The scope judge's "must-add" item: expression reference validation checks that step IDs actually exist.

### Issue 5: ShellAdapter (cross-platform)
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

### Issue 6: ProcessController (cross-platform)
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `internal/workflow/process.go` defines `ProcessController` interface
- `internal/workflow/process_unix.go` uses `pgid` for process group management
- `internal/workflow/process_windows.go` uses `CREATE_NEW_PROCESS_GROUP` + `GenerateConsoleCtrlEvent`
- Ctrl+C sends `SIGINT` to process group, waits grace period, then `SIGKILL`
- `signal.NotifyContext` integration for parent-level cancellation
- Unit tests with a subprocess that traps signals
**Description:**
Per §6. Manages subprocess lifecycle: start, signal, kill. Uses process groups (pgid on Unix, job objects on Windows) to ensure child processes are cleaned up. Grace period before escalation from SIGINT → SIGKILL. Integrates with `context.Context` cancellation.

### Issue 7: Expression engine
**Wave:** 2
**Dependencies:** Issue 3 (types)
**Acceptance:**
- `internal/workflow/expression.go` evaluates `${{ }}` expressions
- Supports: `env.VAR`, `matrix.KEY`, `steps.ID.outputs.KEY`
- Unresolved expressions produce hard error (not passed to subprocess)
- Partial evaluation: env/matrix resolved before execution, step outputs resolved at runtime
- No shell variable collision (`${{ }}` is distinct from `$VAR`)
- Unit tests for all expression types, error cases, and nested expressions
**Description:**
Per §4.5. GHA-style expression evaluation (D5). Three namespaces in Tier 0: `env`, `matrix`, `steps`. Expressions like `${{ steps.pulumi_preview.outputs.DEPLOY_TOKEN }}` resolve from the step output map. Runtime resolution: `env` and `matrix` are known at parse time; `steps.X.outputs.Y` requires deferred resolution after step X executes. Unresolved expressions MUST hard-error — this is a correctness requirement to prevent passing literal `${{ }}` strings to subprocesses.

### Issue 8: Output parser (`$CLAI_OUTPUT`)
**Wave:** 2
**Dependencies:** Issue 5 (ShellAdapter creates the output file)
**Acceptance:**
- `internal/workflow/output.go` parses `KEY=value` lines from `$CLAI_OUTPUT` file
- Validates key names (alphanumeric + underscore)
- Handles malformed files gracefully (skip bad lines, log warning)
- Tier 0: KEY=value only (heredoc multiline deferred to Tier 1 per m20)
- Unit tests for: valid output, malformed lines, empty file, missing file
**Description:**
Per §8. After each step executes, read the `$CLAI_OUTPUT` temp file and parse KEY=value pairs. These outputs become available via `${{ steps.ID.outputs.KEY }}`. Only KEY=value format in Tier 0 (heredoc delimiter format deferred to Tier 1, m20).

### Issue 9: limitedBuffer (thread-safe ring buffer)
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `internal/workflow/buffer.go` implements `limitedBuffer`
- Captures last N bytes of stdout/stderr (4KB tails per FR-7)
- Thread-safe with `sync.Mutex` (M8 — concurrent stdout/stderr writes)
- `io.Writer` interface for direct use with `exec.Cmd.Stdout`/`Stderr`
- Unit tests for: overflow, concurrent writes, exact capacity
**Description:**
Per §5.6. A fixed-capacity ring buffer used to capture stdout/stderr tails. The full output streams to the console but only the tail is retained for LLM analysis and storage. Must be goroutine-safe since stdout and stderr are written concurrently by the subprocess.

### Issue 10: Secret masking
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

### Issue 11: Sequential executor (DAG runner)
**Wave:** 3
**Dependencies:** Issue 5 (ShellAdapter), Issue 6 (ProcessController), Issue 7 (expressions), Issue 8 (output parser), Issue 9 (limitedBuffer), Issue 10 (secrets)
**Acceptance:**
- `internal/workflow/runner.go` orchestrates a complete workflow run
- `internal/workflow/executor.go` executes a single step
- Sequential execution within matrix entries (Tier 0, D7)
- Matrix entries executed sequentially (Tier 0 — parallel deferred to Tier 1)
- Step failure halts remaining steps in the matrix entry
- `context.Context` propagated for Ctrl+C cancellation
- Step outputs accumulated and available to downstream expressions
- Environment precedence: step > job > workflow
- Unit tests with mock ShellAdapter (no real subprocesses)
**Description:**
Per §9. The core execution engine. For each matrix entry, runs steps sequentially. Each step: resolve expressions → build environment → execute via ShellAdapter → capture output → parse $CLAI_OUTPUT → update output map. On failure (non-zero exit): halt remaining steps for that matrix entry. The runner holds the accumulated state (outputs, env) and drives the execute→analyze→review loop.

### Issue 12: LLM analysis integration
**Wave:** 3
**Dependencies:** Issue 9 (limitedBuffer for output), Issue 10 (secret masking), Issue 3 (types for risk_level)
**Acceptance:**
- `internal/workflow/analyze.go` builds analysis context and prompts
- `buildAnalysisContext()` produces ≤100KB head+tail scrubbed output
- Rune-aware truncation (D20 — no broken UTF-8)
- For claid path: sends `AnalyzeStepOutput` RPC, receives structured response
- CLI-side fallback (D24): if claid unavailable → direct `QueryFast()` → if that fails → `needs_human`
- `parseAnalysisResponse()` extracts decision/reasoning/flags from LLM text
- Unparseable LLM response → `needs_human` (FR-24)
- Risk matrix: `shouldPromptHuman(decision, riskLevel)` per §10.7
- Unit tests with mock LLM responses (various formats, edge cases)
**Description:**
Per §10. When a step has `analyze: true`, the executor sends the scrubbed output to the LLM for analysis. Two paths: (1) via claid `AnalyzeStepOutput` RPC (preferred — centralized logging), (2) direct `QueryFast()` fallback when claid unavailable (D24). The `buildAnalysisContext()` function takes up to 100KB of output with head+tail preservation. Response parsing extracts the structured decision. The risk matrix (§10.7) determines whether to prompt the human based on the LLM's decision + the step's declared risk_level.

### Issue 13: Daemon handlers — LLMQuerier + 4 workflow RPCs
**Wave:** 2
**Dependencies:** Issue 1 (proto), Issue 2 (Store)
**Acceptance:**
- `LLMQuerier` interface + `claudeQuerier` production impl in `internal/daemon/`
- `Server` struct gains `llm LLMQuerier` field, injected at construction
- 4 new handlers in `internal/daemon/workflow_handlers.go`
- `handleAnalyzeStepOutput` uses `s.llm.Query()` (not direct `claude.QueryFast()` — D23)
- All handlers call `s.touchActivity()` (idle timeout counting — M16)
- `llmTimeout` accounts for cold-start (120s first call — M20)
- Unit tests with mock `LLMQuerier` and mock `Store`
**Description:**
Per §13.3-13.4, D22, D23. Add workflow RPC handlers to claid following the existing pattern in `handlers.go`. Key handler: `AnalyzeStepOutput` — builds prompt, queries LLM via `LLMQuerier` interface, persists analysis record, returns decision. The `LLMQuerier` is injected so tests don't need the Claude daemon. Other handlers: `WorkflowRunStart` (creates run in SQLite), `WorkflowStepUpdate` (updates step with composite key), `WorkflowRunEnd` (finalizes run).

### Issue 14: Human review UI (interactive mode)
**Wave:** 3
**Dependencies:** Issue 12 (LLM analysis for analysis results)
**Acceptance:**
- `internal/workflow/review.go` implements `InteractionHandler` interface and `TerminalReviewer`
- Options: `[a]pprove / [r]eject / [i]nspect / [c]ommand / [q]uestion`
- `[q]uestion` uses single-turn `QueryFast()` (Tier 0 — multi-turn deferred per FR-33)
- `[i]nspect` shows full output
- `[c]ommand` runs ad-hoc shell command
- Non-interactive-fail mode: returns reject with exit code 5 when human review needed
- `ModeNonInteractiveAuto` rejected with error in Tier 0 (§4.1)
- Unit tests with scripted stdin
**Description:**
Per §11, FR-30-32. The human review interface presents the LLM analysis and available options. Tier 0 has two modes: interactive (terminal prompts) and non-interactive-fail (exits immediately when human decision needed). The `[q]uestion` option sends a single-turn query with the step context (not a multi-turn conversation — FR-33 deferred to Tier 1). Uses `InteractionHandler` interface for testability with `ScriptedInteractionHandler`.

### Issue 15: RunArtifact (JSONL event log)
**Wave:** 3
**Dependencies:** Issue 11 (executor produces events)
**Acceptance:**
- `internal/workflow/artifact.go` implements `ArtifactWriter`
- JSONL format with event types: `run_start`, `step_start`, `step_end`, `analysis`, `human_decision`, `run_end`
- Paths normalized to forward slashes on all platforms (D19)
- File written to `~/.clai/workflow-logs/<run-id>.jsonl`
- `WriteEvent` errors are logged but don't halt execution
- Unit tests verify JSONL structure and path normalization
**Description:**
Per §12.3, FR-40. Append-only JSONL event log for every workflow run. Machine-readable companion to the SQLite data. Written concurrently with execution — each event appended as it occurs. `WriteEvent` errors logged but don't halt execution (m13 — don't kill a workflow because the log file had a write error).

### Issue 16: Console output + progress display
**Wave:** 3
**Dependencies:** Issue 11 (executor drives progress)
**Acceptance:**
- `internal/workflow/display.go` implements progress output
- TTY mode: step names, status icons, durations, spinner
- Non-TTY / `TERM=dumb`: one line per event, no `\r` overwrites, no spinners
- Matrix key shown when running matrix entries
- Step status: pending → running → passed/failed/skipped
- Unit tests with captured output
**Description:**
Per §11, FR-41. Real-time progress display during workflow execution. Adapts to terminal capabilities: rich output with spinners in TTY mode, log-friendly single-line events in non-TTY mode. Shows step name, status, duration, and matrix context.

### Issue 17: Config extension — WorkflowsConfig
**Wave:** 1
**Dependencies:** None
**Acceptance:**
- `WorkflowsConfig` struct added to `internal/config/config.go`
- Fields: `Enabled`, `DefaultMode`, `DefaultShell`, `SearchPaths`, `LogDir`, `RetainRuns`, `StrictPermissions`
- `Paths.WorkflowLogDir()` method added to `internal/config/paths.go`
- Defaults: `Enabled: false`, `DefaultMode: "interactive"`, `RetainRuns: 100`
- Existing config loading unchanged (additive)
- Unit test for config parsing with workflow section
**Description:**
Per §14. Extend the existing config system with workflow-specific settings. The `workflows:` YAML key is added to `Config`. Default mode is interactive. Search paths allow workflow file discovery. All fields have sensible defaults so zero-config works.

### Issue 18: CLI commands — `workflow run` + `workflow validate`
**Wave:** 4
**Dependencies:** Issue 4 (validator), Issue 11 (executor), Issue 12 (LLM), Issue 13 (daemon handlers), Issue 14 (review UI), Issue 15 (artifact), Issue 16 (display), Issue 17 (config)
**Acceptance:**
- `internal/cmd/workflow.go` defines `workflowCmd` with `run` and `validate` subcommands
- `workflow run <file>` executes a workflow with flags: `--mode`, `--shell`, `--daemon`, `--verbose`
- `workflow run -` reads YAML from stdin (D21)
- `workflow validate <file>` validates without executing
- Exit codes per §15 (0=success, 1=step failure, 2=system error, 3=user cancelled, 5=human decision needed in non-interactive)
- Registered in `root.go` under new `groupWorkflow` command group
- Integration test: parse + validate + run a minimal workflow (mock shell)
**Description:**
Per §26. The CLI entry point. `workflow run` ties everything together: parse YAML → validate → connect to claid (or fallback) → execute → display progress → write artifact → exit. `workflow validate` is the dry-run: parse + validate only. Flags control execution mode, shell preference, daemon usage. Cobra command wiring per m14.

### Issue 19: Exit codes + error messages
**Wave:** 4
**Dependencies:** Issue 18 (CLI commands)
**Acceptance:**
- Exit codes: 0 (success), 1 (step failure), 2 (system error), 3 (user cancelled/Ctrl+C), 5 (human decision in non-interactive)
- Error messages for common failures: unresolved expressions, missing $CLAI_OUTPUT, empty LLM response, invalid YAML
- Error messages include step name and matrix key context
- Ctrl+C via `signal.NotifyContext` propagates cleanly
**Description:**
Per §15. Structured exit codes and quality error messages. The scope judge's "must-add" item: error message quality for common failures. Users should immediately understand what went wrong without reading logs.

### Issue 20: Integration tests + smoke test
**Wave:** 5
**Dependencies:** Issue 18 (CLI commands), Issue 19 (exit codes)
**Acceptance:**
- Unit tests for all packages (each issue above includes unit tests)
- Integration test: full workflow run with mock shell commands (no real infrastructure)
- Smoke test: run the actual Pulumi YAML from func spec §6 with mock commands
- `ScriptedInteractionHandler` for deterministic human review in tests
- `make test` passes
- `make lint` passes
**Description:**
Per §23. The testing harness ties together all components. Key: happy-path smoke test running the actual Pulumi compliance YAML (with mock commands that produce expected output). This validates the full pipeline: parse → validate → execute → analyze → review → persist → artifact. The scope judge listed this as a "must-add" for Tier 0.

## Execution Order

**Wave 1** (parallel — 7 issues, no dependencies):
- Issue 1: Proto schema
- Issue 2: SQLite schema + Store
- Issue 3: YAML parser + types
- Issue 5: ShellAdapter
- Issue 6: ProcessController
- Issue 9: limitedBuffer
- Issue 17: Config extension

**Wave 2** (after Wave 1 — 5 issues):
- Issue 4: Workflow validator (← Issue 3)
- Issue 7: Expression engine (← Issue 3)
- Issue 8: Output parser (← Issue 5)
- Issue 10: Secret masking (← Issue 3)
- Issue 13: Daemon handlers (← Issue 1, Issue 2)

**Wave 3** (after Wave 2 — 5 issues):
- Issue 11: Sequential executor (← Issues 5, 6, 7, 8, 9, 10)
- Issue 12: LLM analysis integration (← Issues 9, 10, 3)
- Issue 14: Human review UI (← Issue 12)
- Issue 15: RunArtifact JSONL (← Issue 11)
- Issue 16: Console output (← Issue 11)

**Wave 4** (after Wave 3 — 2 issues):
- Issue 18: CLI commands (← Issues 4, 11, 12, 13, 14, 15, 16, 17)
- Issue 19: Exit codes + error messages (← Issue 18)

**Wave 5** (after Wave 4 — 1 issue):
- Issue 20: Integration tests + smoke test (← Issues 18, 19)

## Dependency Validation

All declared dependencies are **file-level or data-flow** (not just logical ordering):

| Dependency | Reason |
|-----------|--------|
| Issue 4 ← 3 | Validator operates on `WorkflowDef` types from parser |
| Issue 7 ← 3 | Expressions reference `StepDef`, `MatrixStrategy` types |
| Issue 8 ← 5 | Output parser reads the `$CLAI_OUTPUT` file created by ShellAdapter |
| Issue 10 ← 3 | SecretMasker uses `SecretStore` type |
| Issue 13 ← 1, 2 | Handlers use generated proto types and Store interface |
| Issue 11 ← 5,6,7,8,9,10 | Executor calls ShellAdapter, ProcessController, expressions, output parser, limitedBuffer, secret masker |
| Issue 12 ← 9, 10, 3 | LLM analysis uses limitedBuffer output, secret masking, and types |
| Issue 14 ← 12 | Review UI displays analysis results |
| Issue 15 ← 11 | Artifact writer receives events from executor |
| Issue 16 ← 11 | Display receives progress events from executor |
| Issue 18 ← (many) | CLI command wires all components together |
| Issue 19 ← 18 | Exit codes are set by the CLI command |
| Issue 20 ← 18, 19 | Integration test exercises the full CLI |

## Risk Notes

- **m16**: yaml.v3 `KnownFields(true)` + custom `UnmarshalYAML` + `inline` tag has a known bug risk. Test explicitly in Issue 3.
- **M20**: First `AnalyzeStepOutput` call may hit 90-second Claude daemon cold-start. Issue 13 must set 120s timeout on first call.
- **M21**: Claude daemon single-threaded mutex serializes concurrent `AnalyzeStepOutput` RPCs. Acceptable in Tier 0 (sequential execution).
- **D24 fallback**: CLI-side LLM fallback means the `internal/workflow/analyze.go` module needs both paths (claid RPC and direct QueryFast).

## Effort Estimate

- **Wave 1**: 1.5-2 weeks (7 parallel issues, all foundational)
- **Wave 2**: 1-1.5 weeks (5 parallel issues, moderate complexity)
- **Wave 3**: 1.5-2 weeks (5 issues, executor is the most complex)
- **Wave 4**: 0.5-1 week (CLI wiring, exit codes)
- **Wave 5**: 0.5-1 week (integration tests, smoke test)

**Total: 5-6 weeks** (per D28 scope reduction)

## Next Steps
- Run `/pre-mortem` on this plan for failure simulation
- Run `/crank` for autonomous execution, or `/implement <issue>` for individual issues
