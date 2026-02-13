# Vibe Report: Recent Changes

**Date:** 2026-02-13
**Files Reviewed:** 8

## Changes Reviewed

| File | Summary |
|------|---------|
| `internal/cmd/shell/zsh/clai.zsh` | Ghost text persistence on Arrow UP, double-tap picker detection |
| `internal/cmd/init_test.go` | Updated tests for new history navigation + double-tap detection |
| `internal/workflow/analyze.go` | Multi-line reasoning instruction in LLM prompt |
| `internal/daemon/workflow_handlers.go` | Multi-line reasoning instruction in LLM prompt |
| `internal/workflow/transport.go` | Multi-line reasoning instruction in LLM prompt |
| `.agents/ao/citations.jsonl` | Tracking metadata |
| `.agents/ao/pending.jsonl` | Tracking metadata |
| `.agents/ao/provenance/graph.jsonl` | Tracking metadata |

## Complexity Analysis

**Status:** Completed (gocyclo)
**Hotspots:** None over threshold (10)

No functions in the changed Go files exceed cyclomatic complexity 10.

## Metadata Verification

**Status:** All 8 files verified to exist on disk.

## Council Verdict: WARN

| Judge | Verdict | Key Finding |
|-------|---------|-------------|
| Judge 1 (Correctness) | WARN | EPOCHREALTIME fallback: if `zmodload` fails, `${EPOCHREALTIME:-0}` yields `0-0=0 < 0.5`, which would incorrectly trigger the picker on first press |
| Judge 2 (Security) | WARN | Review display (`printReviewBlock`) renders LLM reasoning without sanitizing ANSI escape sequences — potential terminal injection vector |
| Judge 3 (Quality) | WARN | LLM prompt construction duplicated across 3 locations (`analyze.go`, `workflow_handlers.go`, `transport.go`); magic number `0.5` threshold should be a named constant |

## Shared Findings

- **EPOCHREALTIME edge case** (Judges 1, 3): When `zmodload -F zsh/datetime` fails (older Zsh), the fallback `${EPOCHREALTIME:-0}` means both `now` and `_CLAI_LAST_UP_TIME` start at `0`, so the very first UP press computes `elapsed=0 < 0.5` and opens the picker. Mitigation: initialize `_CLAI_LAST_UP_TIME` to a large negative value or gate on EPOCHREALTIME availability.
- **Prompt duplication** (Judges 2, 3): The analysis prompt template exists in three places. Changes must be synchronized manually.

## Concerns Raised

1. **EPOCHREALTIME fallback** — If `zmodload` fails, double-tap detection breaks on first press (false positive). Low probability (requires Zsh < 5.2) but worth a defensive fix.
2. **Terminal injection** — `printReviewBlock` in `review.go` renders LLM `reasoning` text directly to terminal. A crafted LLM response with ANSI escape codes could manipulate terminal state. Low risk (LLM output, not user input) but defense-in-depth suggests stripping escape sequences.
3. **Prompt duplication** — Three identical prompt templates. Not a ship-blocker but increases maintenance burden.
4. **Magic number** — `0.5` threshold for double-tap should be a named constant for discoverability.

## Recommendation

**WARN** — The code is functional and the changes are well-tested, but two edge cases deserve attention before shipping:

1. (Recommended fix) Initialize `_CLAI_LAST_UP_TIME` to a sentinel that prevents false-positive picker open on first press when EPOCHREALTIME is unavailable.
2. (Nice to have) Extract the 0.5s threshold as a named variable.
3. (Deferred) Prompt deduplication and ANSI sanitization are valid but lower priority.

## Decision

[x] FIX - Address EPOCHREALTIME fallback edge case before shipping
[ ] SHIP - Complexity acceptable, council passed
[ ] REFACTOR - High complexity, needs rework
