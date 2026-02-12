# Code Review: Branch claude/gallant-ramanujan — Workflow Execution Engine

**Verdict: WARN** — Two confirmed correctness bugs (high severity) and several should-fix items, but no critical blockers.
**Scope:** branch | **Base:** main | **Head:** claude/gallant-ramanujan
**Reviewed:** 60+ files (~11,400 diff lines) | **Total findings:** 10 (from 20 raw)

## Must Fix (2)

1. **ParseOutputFile error masks command failure** — `runner.go:247`: parseErr causes early return, discarding the real waitErr. Fix: check waitErr first.
2. **Skipped steps have nil Outputs** — `runner.go:105,120`: Missing `Outputs: map[string]string{}` initialization.

## Should Fix (5)

3. **buildExprEnv includes unresolved step.Env** — `runner.go:269`: Same-step env cross-references resolve to raw values.
4. **Step output injection in shell mode** — `expression.go`: Step outputs used in shell-mode commands without escaping.
5. **Matrix values as env vars without validation** — `runner.go:297`: Crafted matrix keys could set dangerous env vars.
6. **No test file for discover.go** — Critical discovery logic untested.
7. **executeStep error paths untested** — 7 error return paths but only 2 tested.

## Consider (3)

8. No size limit on output file parsing.
9. Retry logic not tested.
10. Secret masking edge cases.

## Spec: 33/42 requirements implemented, 5 partial, 4 not implemented

Key gaps: follow-up conversation context, retention/pruning, WorkflowStop RPC, file permission checks, 5 CLI commands.

## Judge Removed 10 Findings

Notable false positives: ShouldPromptHuman claimed not implemented (IS implemented at analyze.go:254), secret leak in artifacts (WriteStepLog never called), matrix expansion not found (expandMatrix exists in cmd/workflow.go).
