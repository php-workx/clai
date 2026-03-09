# Plan: Session History Falls Through To Global

**Date:** 2026-02-10
**Source:** docs/plans/2026-02-10-history-session-fallback-design.md

## Overview

Improve the history picker so the Session tab remains useful in brand new or fresh sessions by continuing into global history (with clear visual indication) and without executing any display prefixes.

## Issues

### Issue 1: Composite Session+Global History In TUI Provider
**Dependencies:** None
**Acceptance:**
- In `clai-picker history`, Session tab shows session items first and then global items prefixed with `[G]` when session has < 100 entries.
- Selecting a `[G]` item inserts the command without `[G]`.
- If session history is empty, Session tab shows global history directly (no `[G]` prefix).
- Paging past the end of session continues into global without repeating session commands.
- Tests cover merge + pagination behavior.
**Description:**
- Update `internal/picker/history_provider.go` to merge global items into session results under the threshold.
- Update `internal/picker/model.go` to render `[G]` items in a dim/ghosttext-like style when not selected.
- Add/adjust tests in `internal/picker/history_provider_test.go`.

## Execution Order

**Wave 1:** Issue 1

## Next Steps

- Create beads epic + child issue and implement Issue 1.

