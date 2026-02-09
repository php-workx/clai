# Plan: Tabbed History Panel + Panel Abstraction (Zsh)

**Date:** 2026-02-04
**Source:** None

## Overview
Introduce a reusable panel abstraction for Zsh menu UIs and implement a tabbed history panel (Session/Global) where Tab switches tabs and list ordering/paging behaves consistently.

## Issues

### Issue 1: Define panel state + list rendering abstraction
**Dependencies:** None
**Acceptance:**
- A single list renderer supports TopDown/BottomUp ordering.
- Panel state cleanly tracks mode, items, cursor, paging, and optional tabs.
**Description:**
- Introduce panel state vars (e.g., _CLAI_PANEL_KIND, _CLAI_PANEL_ORDER, _CLAI_PANEL_ITEMS, _CLAI_PANEL_INDEX, _CLAI_PANEL_PAGE, _CLAI_PANEL_AT_END).
- Extract list rendering into a function parameterized by order (BottomUp vs TopDown), selection index, and header.

### Issue 2: Implement TabList panel (tabs + list)
**Dependencies:** Issue 1
**Acceptance:**
- TabList renders a tab bar header (e.g., `History [Session | Global]`) and list beneath.
- Tab key cycles active tab only when TabList is open.
**Description:**
- Add tab state (e.g., _CLAI_PANEL_TABS, _CLAI_PANEL_TAB_INDEX, _CLAI_PANEL_TAB_KEY).
- Render tabs with active tab highlighted (consistent with terminal constraints).
- Tab key handler switches active tab and reloads list.

### Issue 3: Integrate history picker with TabList (Session/Global)
**Dependencies:** Issue 2
**Acceptance:**
- History panel opens as TabList with Session/Global tabs.
- Switching tabs resets page/index and fetches correct scope.
- List shows up to 5 items, consistent with existing behavior.
**Description:**
- Replace scope switching via Ctrl+Xs/d/g with tab switching in-panel (keep Ctrl+X bindings as optional fallback if desired).
- Map tabs to history args (session/global) and reload items on switch.

### Issue 4: Context-sensitive keybindings and open behavior
**Dependencies:** Issue 3
**Acceptance:**
- Tab opens suggest picker when no panel is open.
- Tab switches tabs when history TabList is open.
- Up/Down/Enter/Esc behave consistently across list types.
**Description:**
- Gate Tab behavior by panel state (history panel vs no panel vs suggest panel).
- Ensure send-break/ESC cancels panel and restores buffer.

### Issue 5: Paging + ordering behavior consistency
**Dependencies:** Issue 3
**Acceptance:**
- BottomUp ordering: newest at bottom, Up paginates older items; Down stops at first suggestion.
- After paging up, cursor lands at bottom with correct item selected.
- No stale items when switching tabs.
**Description:**
- Ensure paging logic works with order abstraction and tab reloads.
- Reset index/page/at_end on tab switch and on panel open.

### Issue 6: Update manual test checklist
**Dependencies:** Issue 4
**Acceptance:**
- Manual checklist covers: open history panel, switch tabs, paging, bounds, and cancel behavior.
**Description:**
- Update `specs/shell-integration-testing.md` (or add a short checklist in the plan) to verify tab switching and list ordering/paging.

## Execution Order

**Wave 1** (parallel): Issue 1

**Wave 2** (after Wave 1): Issue 2

**Wave 3** (after Wave 2): Issue 3, Issue 4

**Wave 4** (after Wave 3): Issue 5

**Wave 5** (after Wave 4): Issue 6

## Next Steps
- Get approval to create beads tasks.
- Implement in `internal/cmd/shell/zsh/clai.zsh`.
