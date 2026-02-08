# Bug Report: Zsh Ghost Text Persists on Tab/History Navigation

**Date:** 2026-02-07
**Severity:** medium
**Status:** root-cause-found

## Symptom
- Ghost text is not cleared when pressing Tab.
- Ghost text remains visible when pressing Up arrow to traverse zsh history.

## Expected Behavior
Ghost text should be cleared whenever navigation/completion actions invalidate the inline suggestion context.

## Reproduction Steps
1. Start zsh with clai integration loaded.
2. Type a command prefix that shows ghost text.
3. Press Tab or Up arrow.
4. Observe ghost text remains stale.

## Root Cause Analysis

### Location
- **File:** `internal/cmd/shell/zsh/clai.zsh`
- **Functions:** `_clai_picker_suggest`, `_clai_up_arrow`, default zle widgets

### Cause
Ghost text state (`_AI_CURRENT_SUGGESTION`, `POSTDISPLAY`, `region_highlight`) is cleared on typed edits and some custom widgets, but not on default `expand-or-complete`/`up-line-or-history` paths. After keybinding changes that restored default Tab/arrow behavior, those paths bypassed clai’s ghost-text reset logic.

### When Introduced
- **Commit:** `546eb35`
- **Summary:** disabled inline picker menu and restored shell defaults for Tab/arrows.

## Pattern Analysis
Working pattern:
- Widgets like `_ai_backward_delete_char`, `_ai_backward_char`, and `_ai_voice_accept_line` explicitly clear/recompute ghost text.

Broken pattern:
- Default completion/history widgets had no clai wrapper to clear stale ghost text.

## Hypothesis
If default Tab/history widgets route through clai wrappers that clear ghost text first, stale ghost text will no longer persist.

## Implementation
- Added `_ai_clear_ghost_text` helper.
- Added wrappers and zle registrations:
  - `expand-or-complete -> _ai_expand_or_complete`
  - `up-line-or-history -> _ai_up_line_or_history`
  - `down-line-or-history -> _ai_down_line_or_history`
- Updated `_clai_up_arrow` to clear ghost text before delegating/fallback.
- Updated `_clai_disable` to restore overridden widgets.
- Added test `TestZshScript_DefaultCompletionAndHistoryClearGhostText` in `internal/cmd/init_test.go`.

## Verification
- `go test ./internal/cmd -run 'TestZshScript_DefaultCompletionAndHistoryClearGhostText|TestShellScripts_UpArrowConditionalBinding' -count=1`
- `go test ./tests/expect -run 'TestZsh_SourceWithoutError|TestZsh_EscapeNotBound' -count=1`
- `go test ./tests/integration -run 'TestShellHooks_ZshSyntaxValid|TestShellHooks_ZshRequiredFunctions|TestShellHooks_ZshIntegration' -count=1`

All passed.
