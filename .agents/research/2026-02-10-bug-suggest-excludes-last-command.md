# Bug Hunt: Suggest Should Not Repeat Last Command

Symptom (reported):
- After running a command (example: `make install`), the next-command suggestion is the exact same command (`make install`).

## Phase 1: Root Cause

Reproduction (logic-level):
- The suggest RPC uses history-based ranking (V1) and/or the V2 scorer.
- The most recent command is often the highest-recency candidate, so it ranks at/near the top and gets returned as a suggestion.

Root cause locations:
- V1 ranker: `internal/suggest/ranker.go` aggregated and scored candidates without suppressing the last executed command.
- V2 scorer: `internal/suggestions/suggest/scorer.go` did not suppress `SuggestContext.LastCmd` from the candidate set.
- Daemon V2 context: `internal/daemon/suggest_handlers.go` passed `LastCmdRaw` into V2 context even though V2 expects normalized commands, making transition scoring weaker and leaving frequency/recency to dominate.

## Phase 2: Pattern

Working reference:
- Shell history “completion” logic avoids exact self-suggestion for prefix-complete use cases:
  - `internal/history/history.go` (`Suggestions`: `entry != prefix`).

Difference:
- Suggestion engines (V1 ranker, V2 scorer) did not have an equivalent “suppress last command” rule for “what next?” suggestions.

## Phase 3: Hypothesis

Hypothesis:
- If we suppress the last executed command from the candidate set (normalized appropriately), the suggestion list will no longer repeat the last command.

Test:
- Add regression tests:
  - V1 ranker: ensure `LastCommand` is not returned.
  - V2 scorer: ensure `SuggestContext.LastCmd` is not returned even if it is the strongest frequency candidate.

Result:
- Tests pass after fix; last-command repetition is suppressed.

## Phase 4: Implementation

Fixes:
- V1: suppress last command candidate before scoring/limiting.
  - `internal/suggest/ranker.go`
- V2: suppress last command candidate after prefix filtering (defense-in-depth: suppress normalized form too).
  - `internal/suggestions/suggest/scorer.go`
- V2 daemon context: normalize `LastCmdRaw` before passing to V2 scorer.
  - `internal/daemon/suggest_handlers.go`

Verification:
- Added unit tests:
  - `internal/suggest/ranker_test.go`
  - `internal/suggestions/suggest/scorer_test.go`

