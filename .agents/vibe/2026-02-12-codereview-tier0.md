# Code Review Report — Workflow Tier 0

**Branch:** `claude/gallant-ramanujan`
**Spec:** `specs/tech_workflows_v4.md` v4.3
**Scope:** 62 files, ~11,476 insertions, 187 deletions
**Date:** 2026-02-12
**Verdict:** ❌ FAIL

## Executive Summary

Two HIGH-severity confirmed findings block merge. 11 MEDIUM findings are should-fix. 9 LOW findings are advisory. 13 spec gaps identified. Deterministic scans (semgrep, gocyclo) are clean.

## Pipeline

| Phase | Tool | Result |
|-------|------|--------|
| Deterministic: semgrep v1.151.0 | `internal/workflow/` | ✅ 0 findings |
| Deterministic: gocyclo (threshold 10) | `internal/workflow/` | ✅ 0 functions over threshold |
| Explorer: Correctness | 10 raw findings | → 6 validated |
| Explorer: Security | 6 raw findings | → 2 validated |
| Explorer: Reliability/Performance | 8 raw findings | → 3 validated |
| Explorer: Test Adequacy | 7 raw findings | → 3 validated |
| Explorer: Error Handling | 6 raw findings | → 1 validated (rest merged) |
| Explorer: Concurrency | 2 raw findings | → 0 validated (merged) |
| Explorer: Spec Verification | 17 raw findings | → 13 validated |
| **Judge** | 56 → 22 validated | 60% reduction |

## Blocking Findings (HIGH)

### F1: ShouldPromptHuman risk matrix deviates from spec SS10.7
- **File:** `internal/workflow/analyze.go:263`
- **Confidence:** 0.97
- **Pass:** spec_verification
- **Evidence:** Spec SS10.7: `risk=low` + `needs_human` → auto-proceed. Implementation groups `DecisionNeedsHuman` with `DecisionHalt` and returns `true` for ALL risk levels. Every low-risk step getting `needs_human` will block on human review.
- **Fix:** For `RiskLow`, return `false` when decision is `needs_human`.

### F2: protoToResult unmarshals daemon flags JSON array as map[string]string
- **File:** `internal/workflow/transport.go:105`
- **Confidence:** 0.88
- **Pass:** correctness
- **Evidence:** Daemon `analysisResult.Flags` is `[]string` (workflow_handlers.go:262). Client `AnalysisResult.Flags` is `map[string]string` (analyze.go:17). JSON array → map unmarshal silently fails. All analysis flags dropped via daemon RPC path.
- **Fix:** Align types: change daemon to `map[string]string` or client to `[]string`.

## Should-Fix Findings (MEDIUM)

### F3: Custom analysis_prompt replaces entire LLM prompt, losing step output
- **File:** `internal/daemon/workflow_handlers.go:159`
- **Confidence:** 0.85
- **Pass:** correctness
- **Evidence:** When `req.AnalysisPrompt != ""`, `buildAnalysisPrompt(req)` (which includes `ScrubbedOutput`) is bypassed. LLM gets custom prompt without step output context.
- **Fix:** Prepend `ScrubbedOutput` context before custom prompt.

### F4: Non-deterministic job selection from Go map iteration
- **File:** `internal/cmd/workflow.go:217`
- **Confidence:** 0.82
- **Pass:** correctness
- **Evidence:** `for _, v := range def.Jobs { job = v; break }` — Go map iteration is non-deterministic. Different jobs run on different executions.
- **Fix:** Sort job names, select first alphabetically.

### F5: Output file parse error overrides actual process error
- **File:** `internal/workflow/runner.go:247`
- **Confidence:** 0.80
- **Pass:** correctness
- **Evidence:** `ParseOutputFile` error returns before `waitErr` check. Process crash error is lost when output file is also malformed.
- **Fix:** Check `waitErr` first; log `parseErr` separately.

### F6: Pattern — 3 spec deviations in CLI layer
- **File:** `internal/cmd/workflow.go:378,74,342`
- **Confidence:** 0.93
- **Pass:** spec_verification
- **Evidence:** (1) Run ID `run-<nanos>` vs spec `wfr-<millis>-<hex>`. (2) Mode `auto/attended/unattended` vs spec `interactive/non-interactive-fail`. (3) Exit code 3 for non-interactive instead of 5.
- **Fix:** Align all three with spec.

### F7: Expression engine missing steps.ID.outcome and analysis.decision scopes
- **File:** `internal/workflow/expression.go:131`
- **Confidence:** 0.92
- **Pass:** spec_verification
- **Evidence:** `resolveSteps` only handles `outputs` segment. Spec SS9.6 requires `steps.ID.outcome` and `steps.ID.analysis.decision` for Tier 0.
- **Fix:** Add `outcome` and `analysis` segment handling.

### F8: Truncation head/tail ratio 70/30 vs spec 40/40/20
- **File:** `internal/workflow/analyze.go:72`
- **Confidence:** 0.90
- **Pass:** spec_verification
- **Evidence:** Implementation: `headBudget = budget * 7 / 10`. Spec SS10.6: 40% head, 40% tail, 20% marker.
- **Fix:** Align ratio.

### F9: SecretMasker missing pattern-based detection per spec SS8.5
- **File:** `internal/workflow/secrets.go:50`
- **Confidence:** 0.87
- **Pass:** spec_verification
- **Evidence:** Spec requires combining value-based AND pattern-based detection via `internal/sanitize`. `sanitize.NewSanitizer().Sanitize()` exists but isn't called.
- **Fix:** Call `sanitize.NewSanitizer().Sanitize()` in `Mask()`.

### F10: WorkflowDef missing Config field per spec SS7.1
- **File:** `internal/workflow/types.go:62`
- **Confidence:** 0.87
- **Pass:** spec_verification
- **Evidence:** Spec includes `Config *WorkflowConfigBlock`. Implementation has no Config field.
- **Fix:** Add struct and field.

### F11: mergeEnv missing secrets + CLI --env layers per spec SS4.6
- **File:** `internal/workflow/runner.go:297`
- **Confidence:** 0.85
- **Pass:** spec_verification
- **Evidence:** Spec defines 6 layers: process < secrets < CLI flags < workflow < job < step. Implementation has only 4.
- **Fix:** Add secrets and CLI --env layers.

### F12: Windows default shell maps to cmd.exe, spec says pwsh first
- **File:** `internal/workflow/shell_windows.go:40`
- **Confidence:** 0.85
- **Pass:** spec_verification
- **Evidence:** `shell: true` → `cmd.exe /C`. Spec SS5.3: try pwsh, fallback to cmd.
- **Fix:** `exec.LookPath("pwsh")` first.

### F13: Step output values from CLAI_OUTPUT never secret-masked
- **File:** `internal/workflow/runner.go:254`
- **Confidence:** 0.82
- **Pass:** security
- **Evidence:** `sr.StdoutTail` and `sr.StderrTail` are masked. `sr.Outputs` (from CLAI_OUTPUT) are not.
- **Fix:** Apply `masker.Mask()` to each output value.

## Advisory Findings (LOW)

### F14: ParseOutputFile 64KB scanner buffer truncates long lines
- **File:** `internal/workflow/output.go:31` | Confidence: 0.75
- **Fix:** `scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)`

### F15: Duplicate signal.NotifyContext leaks goroutine
- **File:** `internal/cmd/workflow.go:155` | Confidence: 0.75
- **Fix:** Remove first `signal.NotifyContext`; keep one in `executeJob`.

### F16: Matrix exclude combinations completely ignored
- **File:** `internal/cmd/workflow.go:396` | Confidence: 0.75
- **Fix:** Filter Include against Exclude patterns.

### F17: SecretMasker names/values desync after sort
- **File:** `internal/workflow/secrets.go:39` | Confidence: 0.72
- **Fix:** Use struct slice to sort both together.

### F18: No test file for discover.go
- **File:** `internal/workflow/discover.go` | Confidence: 0.85
- **Fix:** Create `discover_test.go`.

### F19: Nested expression detection untested
- **File:** `internal/workflow/expression.go:44` | Confidence: 0.78
- **Fix:** Add tests for nested `${{ ${{ }} }}` patterns.

### F20: Unix default shell hardcoded /bin/sh vs detected shell
- **File:** `internal/workflow/shell_unix.go:39` | Confidence: 0.80
- **Fix:** Check `$SHELL` env, fallback `/bin/sh`.

### F21: Flat artifact layout vs spec SS12.5 nested directory
- **File:** `internal/workflow/artifact.go:109` | Confidence: 0.82
- **Fix:** Create `<logDir>/<run-id>/run.jsonl`.

### F22: New gRPC connection per daemon notification
- **File:** `internal/cmd/workflow.go:478` | Confidence: 0.78
- **Fix:** Create connection once, reuse for all notifications.

## Spec Gaps (13)

| Spec Section | Description | Status |
|---|---|---|
| SS10.7 | ShouldPromptHuman risk matrix for needs_human+low | partial |
| SS10.6 | Truncation ratio 70/30 vs 40/40/20 | partial |
| SS12.1 | Run ID format `run-<nanos>` vs `wfr-<millis>-<hex>` | partial |
| SS4.1 | Mode names auto/attended/unattended vs spec names | partial |
| SS4.4/SS15 | Exit code 3 instead of 5 for non-interactive needs-human | partial |
| SS9.6 | Missing steps.ID.outcome and analysis.decision scopes | partial |
| SS8.5 | No pattern-based secret detection | partial |
| SS7.1 | Missing WorkflowDef.Config field | not_implemented |
| SS5.3 | Windows default cmd.exe vs pwsh | partial |
| SS4.6 | Missing secrets + CLI --env env layers | partial |
| SS5.3/5.4 | Unix default /bin/sh vs detected shell | partial |
| SS12.5 | Flat artifact layout vs nested directory | partial |
| SS7.4 | No config search_paths support | not_implemented |

## Strengths

1. **Process lifecycle management** — ProcessController abstraction with platform-specific implementations handles Interrupt→grace→Kill cascade correctly, with proper process group management via `Setpgid` and Linux-specific `Pdeathsig` for orphan cleanup.

2. **LLM analysis fallback chain** — Daemon RPC → direct LLM → needs_human fallback is robustly implemented with retry/backoff. Three-stage parsing (JSON → code block extraction → plain text → needs_human) handles diverse LLM output formats gracefully.

3. **LimitedBuffer & YAML parsing** — Thread-safe ring-buffer with `sync.Mutex`, and YAML parser's custom `UnmarshalYAML` handles both bool and string Shell values while enforcing strict unknown-field rejection.

## Statistics

| Metric | Value |
|---|---|
| Raw explorer findings | 56 |
| After judge validation | 22 |
| Removed (false positive / contradicted) | 19 |
| Merged (root cause grouping) | 15 → 6 |
| HIGH severity | 2 |
| MEDIUM severity | 11 |
| LOW severity | 9 |
