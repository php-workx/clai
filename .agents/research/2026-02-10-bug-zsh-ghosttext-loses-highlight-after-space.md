# Bug Hunt: zsh Ghosttext Loses Highlight After Space

Symptom (reported):
- Typing `make` shows `make| install` with `install` as ghosttext.
- Pressing Space results in `make | install`, but `install` is no longer ghosttext (highlight/color is lost).

## Phase 1: Root Cause

Where it happens:
- zsh ghosttext rendering lives in `internal/cmd/shell/zsh/clai.zsh` via `POSTDISPLAY` + `region_highlight`.

What was wrong:
- Space is commonly bound to the ZLE widget `magic-space`, not `self-insert`.
- The integration wrapped `self-insert` to recompute suggestions and to set `region_highlight`, but did not wrap `magic-space`.
- The “safety net” `_ai_sync_ghost_text` only cleared stale state; it did not recompute `POSTDISPLAY`/`region_highlight` when `BUFFER` changed via an unwrapped widget.

Result:
- After inserting a space via `magic-space`, the suggestion could remain logically valid, but the ghosttext highlight could be stale/cleared, causing the suffix to render as normal text.

## Phase 2: Pattern

Working pattern:
- Widgets that mutate `BUFFER` and should preserve ghosttext call `_ai_update_suggestion`, which sets:
  - `POSTDISPLAY="${suggestion:${#BUFFER}}"`
  - `region_highlight=(...) fg=242`

Broken pattern:
- `magic-space` mutated `BUFFER` without calling `_ai_update_suggestion`.
- `_ai_sync_ghost_text` did not reapply highlight for “still-valid” ghosttext.

## Phase 3: Hypothesis

Hypothesis:
- If we (1) wrap `magic-space` and (2) make `_ai_sync_ghost_text` recompute `POSTDISPLAY`/`region_highlight` whenever the existing suggestion is still a prefix-extension of `BUFFER`, then the ghosttext remains ghost-colored after inserting a space.

Test:
- Add an expect test that:
  - Uses a deterministic `HISTFILE` (`echo world` as the suggestion).
  - Uses a small ZLE debug widget to print `POSTDISPLAY` and `region_highlight`.
  - Verifies `fg=242` persists before and after typing a space.

Result:
- Test passes with the fix; reproduces the expected ghosttext behavior.

## Phase 4: Implementation

Changes:
- `internal/cmd/shell/zsh/clai.zsh`
  - Wrap `magic-space` to call `_ai_update_suggestion` after inserting a space.
  - Upgrade `_ai_sync_ghost_text` to recompute `POSTDISPLAY` and `region_highlight` when the suggestion remains valid.
- `tests/expect/zsh_test.go`
  - Add `TestZsh_GhostTextPersistsAfterSpace`.

