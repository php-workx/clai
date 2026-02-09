# Plan: Fish-like UX Phase 1

**Date:** 2026-02-03
**Source:** specs/tech_fish_like_ux.md

## Overview
Implement fish-like autosuggestions, suggestion/history pickers, and keybindings across zsh, bash, and fish using clai’s suggestion engine while keeping typing non-blocking and behavior consistent across shells.

## Spec Addendum (Clarifications)

### CLI Output Contracts

**`clai suggest [prefix]`**
- Default output: plain text, one suggestion per line, ranked best-first.
- Empty results: output nothing (no headers).
- `--limit N` preserves existing behavior; default remains 1.
- `--json` outputs a JSON array of objects in rank order:

```json
[
  {"text":"git status","source":"session","score":0.91,"description":"","risk":""},
  {"text":"git stash","source":"global","score":0.63,"description":"","risk":"destructive"}
]
```

Fields are always present. `risk` is `destructive` or empty. Empty results yield `[]`.

**`clai history [prefix]`**
- Use existing scope flags: `--session`, `--cwd`, `--global` (default behavior unchanged: session when `CLAI_SESSION_ID` set; otherwise global).
- Add `--format raw|json` (default: `raw`).
- `raw` output: one command per line, most relevant first; no headers.
- `json` output: array of objects with `text`, `cwd`, `ts_unix_ms`, `exit_code`, `source`.
- Invalid scope flags are ignored (fallback to default scope) with zero output noise.
- Flag combination semantics (not ambiguous):
  - `--global` expands scope across all sessions.
  - `--cwd` filters results to the provided cwd (regardless of session scope).
  - `--session` restricts to the specified session and **overrides** `--global` if both are set.

### Toggle Semantics (`clai on` / `clai off`)

- `CLAI_OFF=1` **always** disables clai UX (highest precedence).
- `clai off` is a new CLI command that **persists** disabled state across shells (writes config, e.g., `suggestions.enabled=false`).
- `clai on` re-enables globally (writes config, `suggestions.enabled=true`).
- Add session-only overrides: `clai off --session` writes `$CLAI_CACHE/off` and does **not** touch config; `clai on --session` removes it.
- Precedence order: `CLAI_OFF=1` > session off file > config default.
- Shell integrations check env + session flag + config before rendering ghost text or pickers.

### Timeouts, Debounce, and Staleness

- Debounce suggestion requests: **50–100ms** (default 75ms).
- Zsh/Fish: async fetch, never block ZLE/commandline.
- Bash: maximum blocking for Tab completion **20ms**; if not ready, return native completions immediately.
- Daemon request timeout for interactive suggestions: **50ms** (use `client.suggest_timeout_ms`).
- Staleness token: `(buffer, cursor, cwd, session_id, scope)`; drop stale responses.

### Error Recovery Matrix

| Failure | User Sees | Shell Action |
|---------|-----------|--------------|
| Daemon unavailable | No suggestions | Fall back to history/native completions |
| Timeout | No suggestions | Drop result, keep typing responsive |
| Empty suggestions | Nothing | Hide ghost/picker, keep buffer intact |
| Invalid scope | Nothing | Default to session scope silently |
| Cache read error | Nothing | Ignore, continue normal shell behavior |

### Safety Display

- If `suggestions&#46;show_risk_warning=true`, mark destructive suggestions in pickers (e.g., `! rm -rf …`).
- Risk does **not** auto-block selection; commands are never auto-executed.

### Shell/Plugin Prerequisites

- Bash **3.2+** (menu-complete support).
- Zsh **5.1+** and compatible with `POSTDISPLAY`.
- Fish **3.1+** (autosuggestion hook support).
- When `zsh-autosuggestions` is installed, clai temporarily disables it while active, then restores it on `clai off`.

### Debug/Observability

- When log level is `debug`, record suggestion latency and fallback reason (timeout, daemon down, empty).
- Do not emit user-facing output for failures.

### State Transitions (Picker/Ghost)

States: **Idle**, **GhostVisible**, **SuggestPickerOpen**, **HistoryPickerOpen**, **Paused**

Transitions (all shells):
- **Idle → GhostVisible**: buffer non-empty, cursor at EOL, suggestions available.
- **GhostVisible → Idle**: buffer empty, cursor left, or suggestion cleared.
- **Idle/GhostVisible → SuggestPickerOpen**: `Tab` pressed.
- **Idle/GhostVisible → HistoryPickerOpen**: `↑` pressed.
- **SuggestPickerOpen/HistoryPickerOpen → Idle**: `Esc` cancels and restores original buffer.
- **SuggestPickerOpen/HistoryPickerOpen → Idle**: `Enter` accepts selection, replaces buffer.
- **Any → Paused**: alternate screen/raw TTY detected → hide ghost/pickers.
- **Paused → Idle**: resume normal TTY → redraw prompt and re-evaluate suggestions.

Note: session/global toggle (`CLAI_OFF=1` or `clai off`) forces **Idle** until re-enabled.

## Issues

### Issue 1: Add shell-friendly suggestion/history APIs and toggles
**Dependencies:** None
**Acceptance:**
- `clai suggest` supports multi-result output with a fast, shell-friendly format (plain lines; optional `--json` for metadata).
- `clai history` supports picker-friendly output via `--format raw|json` and uses existing scope flags (`--session`, `--cwd`, `--global`).
- `clai on` / `clai off` persistently enable/disable suggestions (`suggestions.enabled`); `--session` restricts to current session; `CLAI_OFF=1` disables shell UX without breaking normal shell use.
**Description:**
- Extend the CLI to provide predictable, low-latency outputs for shell pickers (no headers, colors, or extra text).
- Add a simple toggle mechanism the shell scripts can check (env + lightweight state file or config).
- Ensure defaults remain safe and backward-compatible for existing `clai suggest` usage.

### Issue 2: Zsh fish-like UX integration (ghost text + pickers)
**Dependencies:** Issue 1
**Acceptance:**
- Inline ghost text uses POSTDISPLAY and is hidden when buffer is empty, cursor not at EOL, picker open, or UI paused.
- Right Arrow accepts full suggestion; Alt+Right accepts next token.
- Tab opens suggestion picker (≤5 items) merging clai suggestions + native completions, ranked with clai first.
- Up Arrow opens history picker with scope switching via `Ctrl+x s/d/g`.
- `Esc` cancels pickers and restores original buffer.
- `zsh-autosuggestions` is suppressed while clai suggestions are active.
- Debounce/cancel/staleness logic prevents blocking typing.
**Description:**
- Replace current right-prompt hint with real ghost text and picker UI via ZLE widgets and `zle -M`.
- Implement picker state management, keybinding overrides, and buffer restore on cancel.
- Add pause detection (alternate screen/raw TTY) and fallback to normal ZLE behavior.

### Issue 3: Bash fish-like UX integration (picker-only)
**Dependencies:** Issue 1
**Acceptance:**
- Tab opens suggestion picker using Readline `COMPREPLY` with `menu-complete` cycling; clai suggestions appear first.
- Up Arrow opens history picker with scope switching via `Ctrl+x s/d/g`.
- If clai is slow/unavailable, native completions return immediately (bounded budget).
- `CLAI_OFF=1` disables clai UX and restores default readline behavior.
**Description:**
- Extend the completion function to merge clai suggestions with native completions and dedupe.
- Add readline bindings for picker navigation and history selection.
- Implement non-blocking suggestion fetch with cache + short timeout.

### Issue 4: Fish fish-like UX integration (ghost text + pickers)
**Dependencies:** Issue 1
**Acceptance:**
- Ghost text uses fish autosuggestion hook with clai suggestions and disables native autosuggestions while active.
- Right Arrow accepts full suggestion; Alt+Right accepts next token.
- Tab opens suggestion picker; Up Arrow opens history picker; Esc cancels and restores buffer.
- Scope switching via `Ctrl+x s/d/g` updates history picker results.
- Suggestion fetch is async/debounced and never blocks typing.
**Description:**
- Integrate clai suggestion source into fish autosuggestion and picker rendering.
- Implement picker UI using fish `commandline` and `complete` mechanisms while preserving native completions.
- Ensure clean fallback when clai is disabled or unavailable.

### Issue 5: Tests and documentation updates
**Dependencies:** Issues 2–4
**Acceptance:**
- Shell init script tests updated for new keybindings and UX behavior.
- Integration checklist in `specs/shell-integration-testing.md` updated to cover pickers, scope switching, and disable toggles.
- Any new CLI flags/commands documented in `docs/` as needed.
**Description:**
- Update unit tests validating generated shell integration content.
- Add/refresh manual test steps for each shell’s picker and ghost text behavior.

## Execution Order

**Wave 1** (parallel): Issue 1
**Wave 2** (after Wave 1): Issue 2, Issue 3, Issue 4
**Wave 3** (after Wave 2): Issue 5

## Next Steps
- Run `/pre-mortem` for failure simulation
- Or `/implement <issue>` for a single issue
