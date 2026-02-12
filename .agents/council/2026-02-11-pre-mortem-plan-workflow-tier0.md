# Pre-Mortem Council Report — Workflow Execution Tier 0 Plan

**Date:** 2026-02-11
**Plan:** `.agents/plans/2026-02-11-workflow-execution-tier0.md`
**Spec:** `specs/tech_workflows_v4.md` v4.2
**Mode:** deep (3 judges, plan-review preset)
**Verdict:** WARN (unanimous, all 3 judges, HIGH confidence)

## Council Composition

| # | Model | Perspective | Verdict | Confidence |
|---|-------|-------------|---------|------------|
| 1 | Claude Opus | Missing Requirements | WARN | HIGH |
| 2 | Claude Opus | Feasibility | WARN | HIGH |
| 3 | Claude Opus | Scope | WARN | HIGH |

## Consensus: WARN

The plan is architecturally sound, follows the spec's §3.8 build sequence faithfully, and has a mostly-correct dependency graph. However, all three judges independently found gaps that would cause implementation pain. No FAIL verdicts — the issues are fixable without redesign.

---

## Shared Findings (agreed by 2+ judges)

### S1: Issue 11 (Executor) is the critical-path bottleneck
**Judges:** Feasibility (F5), Missing-Requirements (M6)
The executor has 6 input dependencies and 3 downstream consumers. Any slip in Wave 1-2 cascades here. The feasibility judge estimates Issue 11 alone at 1-1.5 weeks, leaving minimal time for the other 4 Wave 3 issues.

### S2: Issue 14 (Human Review UI) has incomplete dependencies
**Judges:** Feasibility (F8), Missing-Requirements (M7)
The `[c]ommand` option needs ShellAdapter (Issue 5), `[q]uestion` needs an LLM client, and the `InteractionHandler` only needs analysis result types — not the full LLM integration. Dependency chain is wrong.

### S3: Issue 12 (LLM Analysis) is more complex than scoped
**Judges:** Feasibility (F2), Scope (S-minor)
The D24 dual-path (claid RPC + direct QueryFast fallback) creates two code paths with identical behavior but different error handling. Testing matrix is 6x a single-path.

### S4: Config integration (Issue 17) is more work than stated
**Judges:** Feasibility (F9), Scope (S5)
The existing Config uses manual switch-case dispatch in Get/Set methods. Adding "workflows" requires 6-8 method changes, not just a struct field. Also includes Tier 1 config fields (SearchPaths, RetainRuns, StrictPermissions, SecretFile) that have no Tier 0 consumer.

### S5: Exit code values incorrect in Issue 19
**Judges:** Missing-Requirements (m1), Scope (S2)
Issue 19 says code 3 = "user cancelled/Ctrl+C" but spec says 3 = human reject, 4 = Ctrl+C. Scope judge additionally argues Issue 19 should merge into Issue 18 entirely.

---

## Critical Findings

| # | Finding | Source | Resolution |
|---|---------|--------|------------|
| C1 | TTY detection has no issue owner (spec SS4.3) — load-bearing for mode auto-detection, display formatting | Judge 1 | Add to Issue 18 (CLI) or create Wave 1 issue |
| C2 | Daemon client layer unowned — gRPC connection, EnsureDaemon(), --daemon=false path between Issues 13 and 18 | Judge 1 | Add as sub-task in Issue 18 with explicit acceptance criteria |
| C3 | Store interface expansion breaks existing mockStore in handlers_test.go — make test breaks from Wave 1 | Judge 2 | Add mock stubs in same PR as Issue 2. Verify `make test` passes |
| C4 | Issue 12 dual-LLM path (D24) is actually two features hiding as one issue | Judge 2 | Split into 12a (core: prompt, parsing, risk matrix) + 12b (transport: RPC + fallback) |
| C5 | RunArtifact JSONL (Issue 15) implements FR-39/FR-40 which are Tier 1 in spec §2.2, contradicting Tier 0 acceptance criteria | Judge 3 | Add D29 decision entry to clarify, or remove from Tier 0 |

## Major Findings

| # | Finding | Source | Resolution |
|---|---------|--------|------------|
| M1 | Pre-run dependency detection (spec SS16, exit code 8) has no owner | Judge 1 | Fold into Issue 3+4 (parser/validator) |
| M2 | Workflow file discovery (spec SS7.4 name-based lookup) not in Issue 18 acceptance criteria | Judge 1 | Add to Issue 18 |
| M3 | File permission checks (spec SS19.2-19.5) not covered by any issue | Judge 1 | Add to Issue 18 (warning-only in Tier 0) |
| M4 | Issues 11 and 12 both Wave 3 with no cross-dependency, but executor must call analyzer | Judge 1 | Define explicit interface boundary; Issue 11 tests with mock analyzer |
| M5 | yaml.v3 KnownFields + inline + custom UnmarshalYAML (m16) needs a spike FIRST | Judge 2 | Spike in first day of Wave 1 before committing to parsing strategy |
| M6 | Windows ProcessController with Job Objects over-scoped for Tier 0 | Judge 2 | Simplify to match existing spawn_windows.go pattern (CREATE_NEW_PROCESS_GROUP only) |
| M7 | Issue 19 should merge into Issue 18 — collapses 5 waves to 4 | Judge 3 | Merge; exit codes are 30 lines of constants |
| M8 | Issue 4 should merge into Issue 3 — trivially coupled | Judge 3 | Merge; move expression-ref validation to Issue 7 |
| M9 | Issue 14 can move to Wave 2 — only needs analysis result types, not LLM | Judge 3 | Move; allows Wave 3 to focus on integration |
| M10 | Issue 17 smuggles 4 Tier 1 config fields into Tier 0 acceptance criteria | Judge 3 | Strip from acceptance criteria; keep Go fields (zero values harmless) |
| M11 | isTransientError + retry logic (spec line 3242) missing from Issue 12 | Judge 3 | Add to Issue 12 acceptance criteria |

## Minor Findings

| # | Finding | Source |
|---|---------|--------|
| m1 | Cobra groupWorkflow not specified for root.go AddGroup | Judge 1 |
| m2 | sanitizePathComponent() (SS7.5) not in any issue | Judge 1 |
| m3 | Full stdout/stderr log files (SS12.5) not owned — separate from JSONL + SQLite tails | Judge 1 |
| m4 | Stdin /dev/null handling for non-interactive steps (SS11.3) not mentioned | Judge 1 |
| m5 | go test -race not in Issue 20 acceptance criteria (SS23.6) | Judge 1 |
| m6 | Issue 9 (limitedBuffer, ~60 lines) too granular for standalone issue | Judge 3 |
| m7 | PruneWorkflowRuns stub in Issue 2 unnecessary — omit from interface | Judge 3 |
| m8 | First-call-specific LLM timeout (Issue 13) is premature optimization | Judge 3 |
| m9 | google/shlex dependency not in go.mod — needs `go get` first step | Judge 2 |
| m10 | Proto changes force UnimplementedClaiServiceServer stubs — coordinate with handlers | Judge 2 |

---

## Scope Judge Recommendations (Restructuring)

| Action | From | To | Impact |
|--------|------|----|--------|
| Merge | Issue 19 → Issue 18 | Collapses Waves 4-5 | -1 wave, -1 issue |
| Merge | Issue 4 → Issue 3 | Eliminates false Wave 1→2 dep | -1 issue |
| Merge | Issue 9 → Issue 11 | Reduces process overhead | -1 issue |
| Split | Issue 12 → 12a + 12b | Surfaces transport complexity | +1 issue |
| Move | Issue 14 → Wave 2 | Better parallelism | No count change |
| Total | 20 issues / 5 waves | **18 issues / 4 waves** | |

---

## Timeline Assessment

| Source | Estimate | Assumption |
|--------|----------|------------|
| Plan | 5-6 weeks | Parallel execution of waves |
| Feasibility Judge | 7-9.5 weeks | Single developer, no parallelism |
| Scope Judge | 4.5-5 weeks | After restructuring, tight scope |
| Consensus | **5-7 weeks** | Single developer with restructuring |

The 5-6 week estimate is achievable with the scope judge's restructuring applied and either: (a) two developers parallelizing Wave 1, or (b) a single developer who spikes yaml.v3 early and starts the Issue 11 skeleton in Wave 2 with mocks.

---

## Decisions Required

| # | Decision | Options | Recommendation |
|---|----------|---------|----------------|
| D29 | RunArtifact JSONL (FR-39/FR-40) tier | (a) Keep in Tier 0 + document (b) Defer to Tier 1 | **(a)** — referenced in build sequence and acceptance criteria |
| D30 | Apply scope judge merges? | (a) Keep 20 issues (b) Merge to 18 issues | **(b)** — reduces false dependencies |
| D31 | Split Issue 12? | (a) Single issue (b) Split 12a/12b | User decides — splitting adds clarity but adds coordination |
| D32 | Move Issue 14 to Wave 2? | (a) Keep Wave 3 (b) Move to Wave 2 | **(b)** — better parallelism |

---

## Recommendation

**PROCEED with modifications.** The plan is architecturally sound. Apply these before implementation:

1. **Must-fix (before Wave 1):** C3 (Store mock stubs), C1 (TTY detection ownership), C2 (daemon client ownership)
2. **Should-fix (before Wave 2):** M5 (yaml.v3 spike), M8 (merge Issue 4→3), M7 (merge Issue 19→18)
3. **Nice-to-have:** M9 (move Issue 14 to Wave 2), D31 (split Issue 12), m6 (merge Issue 9→11)

## Decision Gate

- [x] PROCEED - Address critical findings, then implement
- [ ] ADDRESS - Fix concerns before implementing
- [ ] RETHINK - Fundamental issues, needs redesign
