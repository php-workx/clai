# Bug Report: Bash History Picker Query + Go TLS Toolchain Advisory

**Date:** 2026-02-07
**Severity:** medium
**Status:** fixed

## Symptom
- `bash` history picker opens without using current prompt text as filter (appears unfiltered / empty query).
- Security advisory reported: `GO-2026-4337` in `crypto/tls`, fixed in `go1.24.13`.

## Expected Behavior
- History picker should seed query from current prompt input at open time.
- Repository should target patched Go version.

## Root Cause
- In `internal/cmd/shell/bash/clai.bash`, history loading used mutable `READLINE_LINE` directly (`_clai_picker_load_history`), and scope reloads reused current line state rather than a stable initial query.
- This behavior originates from initial picker implementation (`a6042ad` lineage): no dedicated persisted picker query state.
- `go.mod` pinned `go 1.24.12`, below patched version for the reported advisory.

## Pattern Comparison
- Zsh/fish picker flows preserve original buffer/query context more explicitly in their widget state.
- Bash had `_CLAI_PICKER_ORIG_LINE` but no separate query state, so query behavior could drift with line mutations.

## Hypothesis
> If bash stores an explicit picker query at open time and reuses it for history reload paths (scope/tab), the picker will consistently honor typed prompt input.

Validated by adding a failing script-content regression test first, then making the minimal state changes and re-running tests.

## Fix Implemented
- Added `_CLAI_PICKER_QUERY` state in `internal/cmd/shell/bash/clai.bash`.
- `_clai_history_up` now captures `picker_query` from current prompt and:
  - passes it to `_clai_tui_picker_open`,
  - stores it as `_CLAI_PICKER_QUERY`,
  - uses it for initial inline history load.
- `_clai_picker_load_history` now accepts optional query override and defaults to `_CLAI_PICKER_QUERY`.
- Scope/tab reload paths now call `_clai_picker_load_history "$_CLAI_PICKER_QUERY"`.
- `_clai_picker_close` clears `_CLAI_PICKER_QUERY`.
- Bumped `go.mod` from `go 1.24.12` to `go 1.24.13`.

## Verification
- Added regression test:
  - `TestBashScript_HistoryPickerUsesPromptQuery` in `internal/cmd/init_test.go`
- Targeted tests passed.
- Full `make dev` run passed after fixes.
