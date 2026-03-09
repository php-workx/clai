# Bug Hunt: History Picker Global Segment Looks Half Empty

Date: 2026-02-10

Symptom (reported)
- In the TUI history picker, the session segment transitions into global history (`[G]` prefix), but the global segment looks "half empty" / contains far fewer commands than expected.
- Example: many expected global entries (especially commands differing only by paths/args) are missing.

## Phase 1: Root Cause

### Confirm
- The history picker uses the daemon `FetchHistory` gRPC RPC via `internal/picker/history_provider.go`.
- The daemon `FetchHistory` handler uses `store.QueryHistoryCommands(...)` with `Deduplicate: true`.

### Root Cause
- `internal/storage/commands.go:buildHistoryQuerySQL` previously grouped history rows by `command_norm`:
  - `GROUP BY command_norm ORDER BY latest_ts DESC`
- `command_norm` is produced by `cmdutil.NormalizeCommand`, which intentionally replaces variable arguments (paths, URLs, numbers) with placeholders (e.g. `cd /a` and `cd /b` both normalize to `cd <path>`).
- Result: history rows were aggressively collapsed, so many distinct commands were lost from the picker’s global history view.

Locations
- Normalization: `internal/cmdutil/normalize.go`
- History query grouping: `internal/storage/commands.go` (`buildHistoryQuerySQL`)
- Daemon history RPC: `internal/daemon/handlers.go` (`FetchHistory`)

## Phase 2: Pattern

- For suggestions/scoring, grouping by normalized command is desirable.
- For *history browsing/picking*, grouping by normalized command is too aggressive; users expect different concrete commands (especially paths) to remain distinct.

## Phase 3: Hypothesis

Hypothesis
- If `QueryHistoryCommands` deduplicates by raw `command` text instead of `command_norm`, then:
  - distinct commands that normalize to the same template remain visible,
  - while still preventing exact-duplicate spam in the picker.

Test
- Added a storage test where two commands normalize to the same `command_norm` (`cd <path>`) but differ in raw command text.
- Expected result: both appear in history results when deduplicating.

## Phase 4: Implementation

Changes
- `internal/storage/commands.go`
  - `QueryHistoryCommands` continues to deduplicate for picker use, but now groups by raw `command`.
  - `buildHistoryQuerySQL`: `GROUP BY command` (not `command_norm`).
- `internal/daemon/handlers.go`
  - No behavior change beyond the updated store behavior; still uses `QueryHistoryCommands` for RPC.
- `internal/storage/commands_test.go`
  - Added `TestSQLiteStore_QueryHistoryCommands_DoesNotCollapseNormalizedArgs`.

Verification
- `make test` passed.

