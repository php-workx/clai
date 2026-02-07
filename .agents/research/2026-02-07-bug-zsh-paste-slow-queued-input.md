# Bug Report: Zsh Paste Becomes Per-Character Slow

**Date:** 2026-02-07
**Severity:** high
**Status:** root-cause-found

## Symptom
Pasting text in zsh feels very slow and appears as if each character is typed one-by-one.

## Expected Behavior
Pasted text should insert quickly, with suggestion recomputation at most once after bulk input is applied.

## Reproduction Steps
1. Open zsh with clai integration loaded.
2. Paste a long command or multiline text into prompt.
3. Observe visible per-character insertion and lag.

## Root Cause Analysis

### Location
- **File:** `internal/cmd/shell/zsh/clai.zsh`
- **Function:** `_ai_self_insert`

### Cause
`_ai_self_insert` invokes `_ai_update_suggestion` (which calls `clai suggest`) after each inserted character unless `_AI_IN_PASTE` is true.

When bracketed paste is unavailable/inconsistent and input arrives as queued key events, `_AI_IN_PASTE` may not be set, so the expensive suggestion path runs per character.

### When Introduced
Per-char suggestion behavior existed in early zsh integration and only had `_AI_IN_PASTE` protection, but lacked a queued-input guard.

## Pattern Analysis
Working path:
- `_ai_bracketed_paste` sets `_AI_IN_PASTE=true`, pastes content, then updates once.

Broken path:
- Non-bracketed queued input goes through `_ai_self_insert` repeatedly with no queue guard.

## Hypothesis
If `_ai_self_insert` skips suggestion updates while `KEYS_QUEUED_COUNT > 0`, queued paste input will avoid per-character `clai suggest` calls and only recompute once when queue drains.

## Implementation
- Updated `_ai_self_insert` in `internal/cmd/shell/zsh/clai.zsh`:
  - Preserve `_AI_IN_PASTE` guard.
  - Add `KEYS_QUEUED_COUNT` guard to skip per-character suggestion updates during queued bulk input.
- Added regression test:
  - `TestZshScript_SelfInsertSkipsSuggestForQueuedInput` in `internal/cmd/init_test.go`.

## Verification
- `go test ./internal/cmd -run 'TestZshScript_SelfInsertSkipsSuggestForQueuedInput|TestZshScript_EditingWidgetsDismissPicker|TestRunInit_Zsh' -count=1`
- `go test ./tests/expect -run 'TestZsh_SourceWithoutError|TestPerformance_ZshIntegrationOverhead' -count=1`

Both passed.
