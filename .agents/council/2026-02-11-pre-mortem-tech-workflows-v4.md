# Pre-Mortem Council Report: specs/tech_workflows_v4.md

**Date:** 2026-02-11
**Session:** a52d34f1-7a78-4bbf-ae89-a8ab8f96c0e3
**Target:** `specs/tech_workflows_v4.md` (3145 lines, 27 sections)
**Verdict:** WARN (all 3 judges, HIGH confidence)

## Council Composition

| Judge | Lens | Verdict | Confidence |
|-------|------|---------|------------|
| 1 | Missing Requirements | WARN | HIGH |
| 2 | Feasibility | WARN | HIGH |
| 3 | Scope | WARN | HIGH |

## Shared Critical Finding

**Dual-daemon confusion:** The spec references two daemon architectures (Claude daemon at `internal/claude/daemon.go` and claid at `internal/daemon/server.go`) without a clear disambiguation section. Both daemons already exist in the codebase. The spec's design is correct but the naming creates confusion.

**Resolution:** Added §3.0 "Daemon Architecture Overview" in v4.1 errata.

## Key Decisions Made

- **D22:** All LLM calls during workflow execution route through claid as a pass-through (new `AnalyzeStepOutput` RPC)
- FR-33 follow-up conversation (multi-turn) deferred to Tier 1
- Effort estimate updated from 3-4 to 6-8 weeks
- `schema_meta` table removed in favor of `PRAGMA user_version`

## Findings Summary

- **Critical:** 4 (dual-daemon confusion, Tier 0 scope, config gap, store interface)
- **Major:** 11 (idle timeout, Converse, gRPC streaming, shlex, composite key, schema version, isTransientError, limitedBuffer, matrix key, implementation ordering, effort estimate)
- **Minor:** 14 (temp file cleanup, YAML size limit, recursion depth, rate limiting, etc.)

## Outcome

All findings addressed in v4.1 errata plan. No FAIL verdicts — spec is implementable with the corrections applied.
