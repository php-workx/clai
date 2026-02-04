# Plan: Tabbed History Panel (clai-picker TUI)

**Date:** 2026-02-04
**Source:** None

## Overview
Implement the tabbed history panel inside the `clai-picker` TUI so the same UX works across zsh, bash, and fish. Tabs switch between Session and Global history; list ordering and paging follow the bottom-up semantics already requested.

## Issues

### Issue 1: Add tab bar + list rendering to clai-picker
**Dependencies:** None
**Acceptance:**
- clai-picker renders a tab bar (Session/Global) and list beneath.
- List renderer supports TopDown/BottomUp ordering via an explicit option.
  **Description:**
- Implement a list renderer in clai-picker parameterized by order, selection index, and header.
- Add tab state (tabs, active tab index) and render active tab visually distinct.

### Issue 2: Implement bottom-up ordering + paging semantics
**Dependencies:** Issue 1
**Acceptance:**
- BottomUp ordering: newest item appears at bottom.
- Up paginates older items when at top; cursor lands at bottom after paging.
- Down does nothing when at first suggestion.
  **Description:**
- Implement paging in the TUI: fixed list length, load next page on Up at top.
- Ensure selection index resets correctly on page load and tab switch.

### Issue 3: Wire Session/Global data providers
**Dependencies:** Issue 1
**Acceptance:**
- Tab switches between Session and Global history sources.
- Switching tabs resets page/index and fetches correct scope.
- List shows up to 5 items by default (configurable).
  **Description:**
- Map tabs to history args (session/global) and reload items on switch.
- Keep scope switching in-shell optional (Ctrl+X bindings) if desired, but primary path is tabs inside picker.

### Issue 4: Shell integration (open picker + insert selection)
**Dependencies:** Issue 3
**Acceptance:**
- Up opens picker depending on config (empty buffer behavior configurable).
- Enter inserts selection into the shell buffer; Esc/Ctrl+C cancels.
- Tab switches tabs inside picker; does not affect shell completion while picker is open.
  **Description:**
- Zsh: ZLE widget calls `clai picker history`, inserts stdout into BUFFER.
- Bash: `bind -x` to call picker and set `READLINE_LINE/POINT`.
- Fish: bind to function and set `commandline -r`.
- Respect `suggestions.history_picker_open_on_empty` to decide Up behavior on empty buffer.

### Issue 5: Update manual test checklist
**Dependencies:** Issue 4
**Acceptance:**
- Manual checklist covers: open picker, switch tabs, paging, bounds, selection insert, cancel behavior.
  **Description:**
- Update `specs/shell-integration-testing.md` (or add a short checklist in the plan) to verify tab switching and list ordering/paging.

## Execution Order

**Wave 1** (parallel): Issue 1

**Wave 2** (after Wave 1): Issue 2, Issue 3

**Wave 3** (after Wave 2): Issue 4

**Wave 4** (after Wave 3): Issue 5

## Next Steps
- Get approval to create beads tasks.
- Implement in clai-picker TUI + shell glue.
