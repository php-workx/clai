# Plan: clai-picker TUI (Cross-Shell History/Suggest Picker)

**Date:** 2026-02-04
**Source:** None

## Overview
Build an internal TUI picker (fzf-like) to provide a consistent history/suggestion selection experience across zsh/bash/fish. Shells invoke a single `clai` entry point; the TUI handles tabs (Session/Global), paging, selection, and optional filtering. This makes the UX consistent without shell-native UI complexity and keeps backends swappable (builtin/fzf/clai).

## Issues

### Issue 1: Define picker contract + config
**Dependencies:** None
**Acceptance:**
- Config supports selecting backend (builtin|fzf|clai), default remains current behavior.
- New CLI entry point and flags are documented in help output.
**Description:**
- Add config key `suggestions.history_picker_backend` (values: builtin|fzf|clai).
- Add config key `suggestions.history_picker_open_on_empty` (default false).
- Add CLI entry point: `clai picker history` and `clai picker suggest` with flags:
  - `--tabs=session,global` (history)
  - `--limit`, `--query`, `--session`, `--cwd`
  - `--output=plain` (stdout selected item)

### Issue 2: Implement picker TUI core
**Dependencies:** Issue 1
**Acceptance:**
- TUI renders header, optional tabs, list (N lines), and footer/hints.
- Handles Up/Down, Tab (switch tabs), Enter (select), Esc/Ctrl+C (cancel).
- Restores terminal state cleanly on exit.
**Description:**
- Build a minimal terminal UI loop (Bubble Tea or tcell).
- Rendering includes tab bar and list area.
- Implement paging logic and selection state.

### Issue 3: Data providers (history + suggestions)
**Dependencies:** Issue 1
**Acceptance:**
- History provider supports session/global scopes and prefix filtering.
- Suggest provider returns candidates based on current buffer.
**Description:**
- Create provider interface in `internal/picker`.
- Implement history provider using existing history/daemon paths (reuse `clai history` logic or IPC).
- Implement suggestion provider using `clai suggest` or internal suggest logic.

### Issue 4: Shell integration (zsh/bash/fish)
**Dependencies:** Issue 2, Issue 3
**Acceptance:**
- Single shell function invokes `clai picker ...` and inserts result into buffer.
- Tab switching is handled inside TUI (no shell-level tab logic).
- `suggestions.history_picker_open_on_empty` controls Up behavior.
**Description:**
- Zsh: update ZLE widgets to call picker for history; respect empty-buffer rule.
- Bash: `bind -x` to call picker and write `READLINE_LINE/POINT`.
- Fish: bind to function and set buffer via `commandline -r`.
- Keep `Alt+H` (or similar) as an always-open fallback.

### Issue 5: Backend swap + fallback behavior
**Dependencies:** Issue 4
**Acceptance:**
- If backend is `fzf` and not installed, fall back to builtin or show a clear message.
- If `clai` backend fails, return to normal history behavior.
**Description:**
- Add backend dispatcher that chooses implementation based on config + availability.
- Ensure shell glue calls a single `clai history-picker` entry point.

### Issue 6: Manual test checklist + docs
**Dependencies:** Issue 4
**Acceptance:**
- Manual checklist covers: open history picker, tab switch, paging, selection, cancel, fallback.
**Description:**
- Update `specs/shell-integration-testing.md` or add new section for picker TUI.

## Execution Order

**Wave 1** (parallel): Issue 1

**Wave 2** (after Wave 1): Issue 2, Issue 3

**Wave 3** (after Wave 2): Issue 4

**Wave 4** (after Wave 3): Issue 5

**Wave 5** (after Wave 4): Issue 6

## Next Steps
- Approve plan to create beads tasks.
- Decide on TUI library (Bubble Tea vs tcell) for Issue 2.
