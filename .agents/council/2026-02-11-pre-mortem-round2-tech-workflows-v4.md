# Pre-Mortem Council Report — Round 2 (Mixed Cross-Vendor)

**Date:** 2026-02-11
**Session:** a52d34f1-7a78-4bbf-ae89-a8ab8f96c0e3
**Target:** `specs/tech_workflows_v4.md` v4.1 (3315 lines, 27 sections)
**Round:** 2 (post-errata validation with cross-vendor judges)
**Verdict:** WARN (unanimous, all 6 judges, HIGH confidence)

## Council Composition

| # | Model | Lens | Verdict | Confidence |
|---|-------|------|---------|------------|
| 1 | Claude Opus | Missing Requirements | WARN | HIGH |
| 2 | Claude Opus | Feasibility | WARN | HIGH |
| 3 | Claude Opus | Scope | WARN | HIGH |
| 4 | Haiku (Codex) | Missing Requirements | WARN | HIGH |
| 5 | Haiku (Codex) | Feasibility | WARN | HIGH |
| 6 | Haiku (Codex) | Scope | WARN | HIGH |

## Cross-Vendor Consensus

All 6 judges independently returned **WARN** with **HIGH confidence**. No FAIL verdicts. The v4.1 errata successfully addressed the round 1 findings (daemon disambiguation, Converse deferral, etc.), but the cross-vendor review surfaced **new integration-level findings** that round 1 missed.

**Key theme across all judges:** The AnalyzeStepOutput RPC (D22) is architecturally sound but has **unspecified integration mechanics** — particularly around Go package imports, daemon lifecycle coupling, and error propagation.

---

## Shared Critical Finding: AnalyzeStepOutput Import Path

**Identified by:** Claude Opus (Judge 1 C1, Judge 2 M1), Haiku (Judge 4 Q1, Judge 5 M4)

The `handleAnalyzeStepOutput()` handler inside `internal/daemon/server.go` must call `claude.QueryFast()`, but today `internal/daemon` has **zero imports from `internal/claude`**. The spec does not:

1. Verify this import is safe (no circular dependency at Go package level)
2. Specify who manages the Claude daemon subprocess lifecycle when called from claid
3. Address `StartDaemonProcess()` ownership — if claid calls `QueryFast()` which internally spawns a Claude daemon subprocess, that subprocess becomes a child of claid, not the CLI
4. Provide a testability strategy — `QueryFast()` is a package-level function, not an interface method, making it hard to mock in daemon handler tests

**Resolution needed:** Either:
- (a) Introduce an `LLMQuerier` interface in `internal/daemon` that wraps `QueryFast()`, enabling dependency injection and testing
- (b) Or document that `internal/daemon` will import `internal/claude` directly and explain the lifecycle implications

---

## New Findings (Not in Round 1)

### Critical

| # | Finding | Source | Resolution |
|---|---------|--------|------------|
| C5 | `scrubbed_output` field says "<=4KB tail" but `buildAnalysisContext` operates on up to 100KB — contradictory | Claude J1 C2 | Clarify: CLI runs `buildAnalysisContext()` (up to 100KB head+tail) and sends result as `scrubbed_output`. Update proto comment to "<=100KB scrubbed context". |
| C6 | LLM fallback chain broken when daemon is unavailable — D22 routes ALL LLM through claid, but if claid is down, LLM analysis is impossible | Claude J3, Haiku J4 Q2 | Add CLI-side fallback: if claid RPC fails, CLI calls `QueryFast()` directly. Log warning about missing centralized logging. |

### Major

| # | Finding | Source | Resolution |
|---|---------|--------|------------|
| M12 | `workflow_hash` column in SQLite but missing from `WorkflowRunStartRequest` proto | Claude J1 M1 | Add `string workflow_hash = N` to proto message |
| M13 | `WorkflowRunQuery` type used but never defined | Claude J1 M2 | Define struct with `WorkflowName`, `Limit`, `Offset` fields |
| M14 | `WorkflowRun`, `WorkflowStep`, `WorkflowStepUpdate` Go structs never defined | Claude J1 M3 | Define structs derived from SQLite schema, document nullable fields |
| M15 | `Env()` vs `EnvVars()` method name inconsistency on `SecretStore` | Claude J1 M4 | Standardize to `EnvVars()` |
| M16 | claid idle timeout (20min) may expire during long human review pauses | Claude J1 M5, Haiku J4 Q2 | Count active workflow runs as "activity" for idle timeout purposes |
| M17 | `buildAnalysisPrompt` has two incompatible signatures (StepDef vs strings) | Claude J1 M6 | Consolidate: claid version takes two strings (from proto), CLI version is removed |
| M18 | `started_at_unix_ms` missing from `WorkflowRunStartRequest` proto | Claude J1 M8 | Add field; use CLI-side timestamp for accuracy |
| M19 | Store interface uses `schema_meta` table but spec says `PRAGMA user_version` | Claude J1 m1, J2 M2 | **Existing code uses `schema_meta`**. Spec should match existing pattern, not the reverse. Update spec §12.8 to reference `schema_meta`. |
| M20 | First `AnalyzeStepOutput` call may hit 90-second Claude daemon cold-start | Claude J2 M3 | `llmTimeout` must account for cold start (increase to 120s on first call, or pre-warm) |
| M21 | Claude daemon single-threaded mutex — serializes concurrent AnalyzeStepOutput RPCs | Claude J2 M3 | Document as Tier 0 limitation (sequential execution anyway). Tier 1 parallel needs connection pooling. |
| M22 | `WorkflowWatch` streaming RPC is first stream in codebase — no existing pattern | Claude J2 M5 | Defer `WorkflowWatch` to Tier 1 (per scope judge recommendation). Ctrl+C signal handling is sufficient for Tier 0. |

### Minor

| # | Finding | Source |
|---|---------|--------|
| m11 | `sh` missing from `validShellValues` — Alpine containers use plain `sh` | Claude J1 m2 |
| m12 | `limitedBuffer` ring buffer algorithm only shown as stubs | Claude J1 m3 |
| m13 | `WriteEvent` error return ignored in example code | Claude J1 m4 |
| m14 | Cobra command wiring to root.go not mentioned | Claude J2 m2, Haiku J6 |
| m15 | `flags_json` as string in proto (not repeated message) — awkward but intentional | Claude J1 m8 |
| m16 | `KnownFields(true)` + custom `UnmarshalYAML` + `inline` — known yaml.v3 bug risk | Claude J2 m6, J1 m6 |
| m17 | No shlex library named — need `github.com/google/shlex` | Claude J1 m10, J2 M4 |
| m18 | FR-33 remnants: `ReviewSession`, `Converse()`, and `[q]uestion` option still in Tier 0 text | Claude J3 cut-1 |
| m19 | Non-interactive-auto mode code in Tier 0 sections despite being Tier 1 | Claude J3 cut-7, Haiku J6 risk-7 |
| m20 | Heredoc multiline output parser fully specified but labeled Tier 1 | Claude J3 cut-8 |

---

## Scope Judge Recommendations (Claude J3)

The scope judge provided the most aggressive cut list. Key recommendations:

### Defer from Tier 0 to Tier 1

1. **WorkflowWatch streaming RPC** — Ctrl+C via signal.NotifyContext is sufficient
2. **WorkflowStatus/WorkflowHistory/WorkflowStop RPCs** — Their CLI consumers are already Tier 1
3. **Run retention/pruning** — Premature for MVP with few dozen runs
4. **WorkflowConfigBlock (full config file support)** — CLI flags sufficient for Tier 0
5. **Workflow file discovery from search paths** — Accept direct file path only
6. **Non-interactive-auto mode** — Ship interactive + non-interactive-fail only
7. **Heredoc output parser** — KEY=value only for Tier 0
8. **FR-33 remnants** (ReviewSession, Converse, [q]uestion) — Cut entirely from Tier 0

### Must-Add to Tier 0

1. **CLI-side LLM fallback** when daemon is unavailable (C6)
2. **Happy-path smoke test** running the actual Pulumi YAML (with mock commands)
3. **Error message quality** for common failures (unresolved expressions, missing $CLAI_OUTPUT, empty LLM response)
4. **Expression reference validation** in `validate` command (check step IDs exist)

### Minimal Viable Tier 0 (per scope judge)

4-week build if scoped to: YAML parser, sequential execution, ShellAdapter (Unix), expression engine (string interpolation only), $CLAI_OUTPUT KEY=value parser, LLM via direct QueryFast() with claid fallback, risk matrix + interactive review (no follow-up), secret masking (env only), basic console output, exit codes, `workflow run` + `workflow validate` commands, Ctrl+C handling, minimal RunArtifact JSONL, daemon persistence (3 RPCs: RunStart/StepUpdate/RunEnd + graceful degradation).

---

## Effort Estimates (Cross-Vendor)

| Judge | Model | Estimate |
|-------|-------|----------|
| Claude Opus (Feasibility) | opus | 8-10 weeks |
| Haiku (Feasibility) | haiku | 7-9 weeks |
| Haiku (Scope) | haiku | 4-6 weeks (aggressive MVP) |
| Claude Opus (Scope) | opus | 4 weeks (minimal) / 6-8 weeks (current spec) |
| Spec estimate | — | 6-8 weeks |

**Consensus:** 6-8 weeks for the current Tier 0 scope. Could be 4-5 weeks with the scope judge's cut list applied. The feasibility judges consistently estimated higher (8-10 weeks) due to AnalyzeStepOutput integration complexity.

---

## Decisions Required

| # | Decision | Options | Recommendation |
|---|----------|---------|----------------|
| D23 | LLMQuerier interface vs direct import | (a) Interface (b) Direct import | **(a)** — enables testing without Claude daemon |
| D24 | LLM fallback when daemon unavailable | (a) Return needs_human (b) CLI calls QueryFast directly | **(b)** — preserves user experience |
| D25 | Defer WorkflowWatch streaming to Tier 1 | (a) Keep in Tier 0 (b) Defer | **(b)** — Ctrl+C sufficient |
| D26 | Defer status/history/stop RPCs to Tier 1 | (a) Keep proto + handlers (b) Defer | **(b)** — CLI consumers already Tier 1 |
| D27 | schema_meta vs PRAGMA user_version | (a) Match existing code (schema_meta) (b) Migrate to PRAGMA | **(a)** — consistency with existing pattern |
| D28 | Scope reduction (4-week vs 6-8 week) | (a) Full Tier 0 (b) Minimal MVP | User decides |

---

## Comparison with Round 1

| Aspect | Round 1 | Round 2 |
|--------|---------|---------|
| Judges | 3 Claude sonnet | 3 Claude Opus + 3 Haiku |
| Verdict | WARN (all) | WARN (all) |
| Critical findings | 4 (resolved) | 2 new (C5, C6) |
| Major findings | 11 (resolved) | 11 new |
| Integration depth | Surface-level | Deep codebase verification |
| Key insight | Daemon naming confusion | AnalyzeStepOutput import path + LLM fallback gap |

Round 2 judges had deeper codebase access and found **integration-level** issues that round 1's more abstract review missed. The D22 LLM pass-through decision was validated as architecturally correct but revealed implementation mechanics that need resolution before coding begins.

---

## Next Steps

1. Resolve D23-D28 decisions
2. Apply v4.2 errata for C5, C6, and priority major findings
3. Finalize proto schema for workflow RPCs
4. Begin implementation per §3.8 build sequence
