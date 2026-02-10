# Design: Session History Falls Through To Global

**Date:** 2026-02-10
**Context:** New/fresh sessions often have little or no session history, but users still expect to access global history seamlessly (especially via the history picker).

## Goals

- In the **history picker Session tab**, seamlessly include global history once session history is exhausted, so a fresh session still shows useful history.
- When Session history is small (spec says "does not have 100 entries yet"):
  - Show **global entries** in a ghosttext-like style with a `[G]` prefix in the Session tab.
  - Selecting a `[G]` entry inserts the raw command (no `[G]`).
- If the session history is **completely empty**, show global history directly (no `[G]` prefix).
- When paging/scrolling through Session history in the picker, continue into global history (without repeating session commands).

## Non-Goals

- Change daemon RPC schema (no proto changes).
- Change shell-native history behavior when the picker is not used.
- Change the underlying command stored/executed; display-only changes only.

## Proposed Approach (Recommended)

Implement a **client-side composite view** for the Session tab in the Go TUI picker:

- `internal/picker/HistoryProvider.Fetch` detects a Session-scoped request.
- It performs a bounded probe to determine whether the session has fewer than 100 commands overall.
- If so, it returns a combined list:
  1. Session history items (normal display)
  2. Global history items not present in the session (display prefixed with `[G] ` and rendered dim)

Pagination is implemented over the **combined** list by translating `(limit, offset)` into:
- A slice from session items (if `offset < len(sessionItemsForQuery)`), then
- A slice from global items after filtering out duplicates.

The model view layer renders `[G]` items using a dimmer base style (ghosttext-like), while keeping selection styling prominent.

## Open Questions / Assumptions

- For "session history does not have 100 entries yet", interpret as **overall session size < 100** (not query-specific).
- For completely empty session history, "directly show Global history" is implemented as: return global items without `[G]` prefix (still within the Session tab).

## Testing Plan

- Unit tests for composite behavior in `internal/picker/history_provider_test.go`:
  - Session under 100 merges global items with `[G]` display prefix.
  - Session empty returns global items without `[G]`.
  - Session >= 100 does not merge (Session-only).
  - Dedupe removes session commands from global portion.
  - Offset/limit pagination across session+global boundary.
