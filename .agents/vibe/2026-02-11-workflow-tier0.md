# Vibe Check: Workflow Tier 0 Implementation

**Date:** 2026-02-11
**Target:** 55 source files across HEAD~5 (Waves 1-4)
**Verdict:** WARN
**Council:** 3 judges (error-paths, api-surface, spec-compliance), all WARN/HIGH confidence

## Complexity Hotspots

| Function | Score | Rating | File |
|----------|-------|--------|------|
| `runWorkflow` | 28 | D | `internal/cmd/workflow.go` |
| `Validate` | 16 | C | `internal/workflow/validate.go` |
| `setWorkflowsField` | 15 | C | `internal/config/config.go` |
| `PromptReview` | 14 | C | `internal/workflow/review.go` |

## Council Summary

### Judge 1: Error Paths — WARN (HIGH confidence)

**Critical:**
- `workflow_handlers.go:55-63`: `GetWorkflowStep` returns `(nil, ErrWorkflowStepNotFound)` but `WorkflowStepUpdate` handler treats ALL non-nil errors as failures. New steps can NEVER be created via this RPC. The `existing == nil` check at line 65 is unreachable for not-found cases.

**High:**
- `cmd/workflow.go:242-243`: `os.Exit(1)` bypasses all deferred cleanup (artifact.Close, signal cancel, cobra RunE defers)
- `cmd/workflow.go:199-201`: Nil-pointer risk when handler returns `(nil, someErr)` — currently safe with `NonInteractiveHandler` but fragile
- `cmd/workflow.go:200`: `break` only exits inner step loop, not outer matrix loop — continues executing matrix combinations after reject

**Medium (7):** Silent artifact creation failures, discarded `filepath.Abs` errors, discarded flag parse errors, error priority inversion in `executeStep` (parse error hides process result), silent JSON parse failure in transport, silent `Interrupt` errors, silent `Kill` errors.

**Low (5):** Non-deterministic job selection, no circuit-breaker for artifact write failures, SecretNames/values misalignment after sort, 3 separate gRPC connections per run, limitedBuffer always returns nil error.

### Judge 2: API Surface — WARN (HIGH confidence)

**High:**
- `buffer.go`: Exported `NewLimitedBuffer` returns unexported `limitedBuffer` type — Go API antipattern
- `cmd/workflow.go`: `runWorkflow` complexity 28, god function mixing 10+ concerns
- `parser.go`: `sanitizePathComponent` unexported but could be needed externally

**Medium (7):** Bare string types for all enums (no typed constants), hardcoded deps in `NewRunner` (no DI), raw `func` instead of `LLMQuerier` interface in transport, untyped `interface{}` for artifact event data, unexported `validDecisions` map, transitive gRPC dependency in workflow package, `NewRunArtifact` hardcodes `config.DefaultPaths()`.

**Low (6):** Mixed YAML/runtime fields in `StepDef`, inconsistent return conventions, `StepSummary` duplicates `StepResult`, `ValidateWorkflow` returns `[]ValidationError` not `error`, test-only types exported in production, `WorkflowStepUpdate` struct duplicates `WorkflowStep`.

### Judge 3: Spec Compliance — WARN (HIGH confidence)

**Critical:**
- Decision enum diverges: spec defines `proceed/halt/needs_human`, implementation uses `approve/reject/needs_human/error`
- Exit codes incomplete: only 0 and 1 implemented, spec requires 0-8 and 124

**High (6):**
- Env precedence chain missing secrets injection and CLI `--env` flags layers
- D19 path normalization (`filepath.ToSlash`) not implemented anywhere
- SS19 file permission checks completely absent
- Types will reject valid YAML with Tier 1 fields (KnownFields rejects unknown fields)
- Expression scopes missing `steps.ID.outcome` and `steps.ID.analysis.decision`
- Stdin workflow input (`-` argument) not supported

**Medium (7):** Name-based workflow discovery missing, CLI flags diverge from spec vocab, no `ExecutionMode` type, no LLM retry/resilience, run ID format differs (`run-` vs `wfr-`), DB schema divergences, `NO_COLOR` not checked.

**Low (5):** Simplified JSONL event types, `ShellAdapter` interface differs, `Runner` struct shape differs, no per-step log files, `SecretMasker` has no mutex.

## Cross-Judge Consensus

Findings confirmed by 2+ judges:
1. **runWorkflow god function** (Judge 1 + Judge 2): Complexity 28, source of most error-handling gaps
2. **Decision enum mismatch** (Judge 1 + Judge 3): approve/reject vs proceed/halt — semantic rename from spec
3. **Incomplete exit codes** (Judge 1 + Judge 3): Only `os.Exit(1)`, spec needs 0-8 and 124
4. **Bare string enums** (Judge 2 + Judge 3): No typed constants, fragile contracts

## Recommended Actions

### Must Fix (blocks quality gate):
1. **Daemon handler bug** (Judge 1 Critical): `WorkflowStepUpdate` must handle `ErrWorkflowStepNotFound` as "not found" (create path), not as error
2. **Exit codes** (Judge 3 Critical): Define constants, return typed errors from `runWorkflow` instead of `os.Exit(1)`
3. **Decision enum alignment** (Judge 3 Critical): Align with spec's `proceed/halt/needs_human` or update spec to match implementation

### Should Fix (before API stabilizes):
4. Export `LimitedBuffer` or unexport `NewLimitedBuffer`
5. Add typed string constants for status/decision/risk enums
6. Implement D19 `filepath.ToSlash` in artifact paths
7. Add `NO_COLOR` support to display
8. Support stdin (`-`) for workflow input
9. Add unknown-field tolerance for Tier 1 YAML fields

### Can Defer:
10. Refactor `runWorkflow` into smaller functions (complex, low risk if tests cover)
11. DI for `ShellAdapter`/`ProcessController` in `RunnerConfig`
12. LLM retry/resilience logic
13. Per-step log files
14. Name-based workflow discovery
