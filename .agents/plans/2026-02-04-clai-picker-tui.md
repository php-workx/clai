# Plan: clai-picker TUI (History Picker)

**Date:** 2026-02-04
**Source:** specs/2026-02-04-clai-picker-tui.md (post pre-mortem)

## Overview
Build a standalone `clai-picker` TUI binary using Bubble Tea that provides consistent history selection across zsh/bash/fish. The binary opens `/dev/tty` for rendering, writes the selected command to stdout, and supports tabs (Session/Global), async paging, substring search, and backend swapping.

## Codebase State (from pre-mortem exploration)
- `cmd/clai-picker/main.go` — exists as empty stub
- `internal/picker/` — does not exist (create from scratch)
- `internal/config/` — exists but missing all picker config keys
- `internal/storage/` — SQLite with pagination support (ready)
- `internal/daemon/` — gRPC server on Unix socket (ready, but no history RPC)
- `internal/ipc/` — gRPC client (ready)
- `proto/clai/v1/clai.proto` — no FetchHistory method
- `go.mod` — missing bubbletea and lipgloss deps
- Shell scripts (zsh/bash/fish) — exist but no picker bindings

## Issues

### Issue 1: Add dependencies and picker config
**Dependencies:** None
**Wave:** 1
**Acceptance:**
- `go get github.com/charmbracelet/bubbletea@v2 github.com/charmbracelet/lipgloss@v1` succeeds
- `HistoryConfig` struct added to `internal/config/config.go` with all picker keys
- `go build ./cmd/clai-picker` compiles (even if main is still a stub)
- Config keys exposed in `ListKeys()`
**Description:**
- Add bubbletea v2 and lipgloss v1 to go.mod
- Add `HistoryConfig` struct: `picker_backend`, `picker_open_on_empty`, `picker_page_size` (validated 20-500), `picker_case_sensitive`, `picker_tabs` (list of TabDef)
- Wire into config load/save/list cycle
- Unit tests for config validation (page_size clamping, backend validation)

### Issue 2: Add FetchHistory gRPC RPC
**Dependencies:** None
**Wave:** 1
**Acceptance:**
- `proto/clai/v1/clai.proto` has FetchHistory RPC with request/response messages
- `protoc` generates Go code successfully
- Daemon handler implemented: queries SQLite with session/global scope, substring filter, pagination, deduplication
- Unit test: handler returns correct paginated, filtered, deduplicated results
**Description:**
- Add to proto: `rpc FetchHistory(HistoryFetchRequest) returns (HistoryFetchResponse)`
- Messages: HistoryFetchRequest (session_id, query, limit, offset, global), HistoryFetchResponse (items, at_end), HistoryItem (command, timestamp_ms)
- Implement daemon handler using `internal/storage` queries
- Deduplication: by command text (case-insensitive per config), keep most recent by ts_unix_ms
- Strip ANSI escape sequences from entries before returning
- Add storage query method if needed for substring + session scoping

### Issue 3: CLI entry point + dispatch skeleton
**Dependencies:** Issue 1
**Wave:** 2
**Acceptance:**
- `clai-picker history` runs with proper startup order (TTY check → TERM check → width check → lock → flags → config → dispatch)
- Exit codes: 0 (success), 1 (cancel/error), 2 (no TTY/TERM=dumb/too narrow)
- `--help`, `--version` work; unknown flags exit 1
- File lock prevents concurrent instances
- `make dev` passes
**Description:**
- Implement `cmd/clai-picker/main.go` with cobra or minimal flag parsing
- Startup order per spec section 2: `/dev/tty` check, TERM check, width check, file lock, flag parse, config load, backend dispatch
- Flag validation: --query max 4096 bytes, --session format, --output=plain only, --limit positive int
- Input sanitization for --query: strip control chars, reject newlines
- Backend dispatch: builtin (Bubble Tea TUI) | fzf (subprocess) | clai (future)
- Fallback: fzf→builtin silently; log to stderr if CLAI_DEBUG=1

### Issue 4: Picker TUI core (Bubble Tea Model)
**Dependencies:** Issue 1
**Wave:** 2
**Acceptance:**
- `internal/picker` package with Model, Provider interface, state machine
- All state transitions work: idle→loading→loaded/empty/error, loaded→loading on page/tab/query change
- Selection bounds clamping after any items mutation
- Stale response discarding via RequestID
- Debounced query input (100ms, cancel on new keystroke/tab switch)
- SIGWINCH recomputes layout preserving selection
- Alt screen, signal handling (SIGTSTP/SIGTERM/SIGHUP/SIGINT)
- Unit tests for state machine, bounds clamping, stale response, debounce
**Description:**
- Create `internal/picker/model.go`: state enum, Model struct, Init/Update/View
- Create `internal/picker/provider.go`: Provider interface, Request (with RequestID), Response
- Create `internal/picker/sanitize.go`: ANSI stripping, UTF-8 validation, middle truncation
- Keys: Up/Down navigate, Tab cycles tabs, Enter selects, Esc/Ctrl+C cancels, printable chars → query
- View: tab bar (active tab with brackets), list with selection marker, query input, loading/empty/error states
- Async fetch via tea.Cmd; cancel context on tab/query/page change
- tea.WithAltScreen, tea.WithInput(tty), tea.WithOutput(tty)
- stdout reserved for selected item only

### Issue 5: History provider (daemon IPC client)
**Dependencies:** Issue 2, Issue 4
**Wave:** 3
**Acceptance:**
- Provider implements `Fetch(ctx, req) (items, atEnd, err)` using FetchHistory gRPC
- 200ms context timeout per call
- Connects to daemon socket via `config.DefaultPaths().SocketFile()`
- Returns sanitized items (ANSI stripped, UTF-8 validated)
- Unit test with mock gRPC server
**Description:**
- Create `internal/picker/history_provider.go`
- Dial daemon socket, call FetchHistory with session/global scope from tab args
- Map Request.Options to HistoryFetchRequest fields (session_id from tab args, global flag)
- Env var interpolation for tab args (`$CLAI_SESSION_ID`)
- On IPC failure: return error (caller handles exit 1)

### Issue 6: Shell integration (zsh + bash + fish)
**Dependencies:** Issue 3, Issue 5
**Wave:** 4
**Acceptance:**
- Up arrow opens picker in all three shells (respects picker_open_on_empty config)
- Alt+H always opens picker
- Selection inserts into buffer; cancel restores buffer
- Bash 3.2: silently falls back to native history
- clai-picker not on PATH: silently falls back to native history
- Exit 2 triggers permanent fallback to native history for rest of session
**Description:**
- Zsh: add `_clai_picker_open` ZLE widget, bind in emacs/viins/vicmd keymaps
- Bash: add `_clai_picker_open` function, `bind -x` in emacs/vi-insert/vi-command (4.0+ only)
- Fish: add `_clai_picker_open` function, `bind` in default/insert/visual modes
- All shells: capture stdout with `$()`, check exit code, set buffer accordingly
- Version check for Bash (skip if < 4.0)
- PATH check for clai-picker (skip if not found)

### Issue 7: Backend dispatcher + fzf fallback
**Dependencies:** Issue 3, Issue 5
**Wave:** 4
**Acceptance:**
- Config `picker_backend=fzf` invokes fzf with history piped as input
- fzf not in PATH → silently fall back to builtin
- `picker_backend=builtin` or `picker_backend=clai` → Bubble Tea TUI
- CLAI_DEBUG=1 logs fallback events to stderr
**Description:**
- Add backend dispatch in `cmd/clai-picker` after config load
- fzf backend: pipe history (from provider) to fzf subprocess, capture selection
- builtin backend: run Bubble Tea Model
- Fallback chain: configured backend → builtin → exit 1

### Issue 8: Tests + integration validation
**Dependencies:** Issue 6, Issue 7
**Wave:** 5
**Acceptance:**
- All automated tests from spec Testing section pass
- State machine coverage (all transitions)
- Sanitization tests (ANSI, control chars, query limits)
- File lock test
- Provider timeout test
- Flag validation tests
- `make dev` passes
**Description:**
- Test picker state machine: all transitions, bounds clamping, stale responses
- Test sanitization: --query with ANSI/control chars/newlines/>4096 bytes; history items with ANSI
- Test file lock: concurrent instance exits 1
- Test TTY/TERM/width checks (mock)
- Test backend dispatcher fallback chain
- Test provider timeout → error state → retry
- Integration: verify `clai-picker history --help` works end-to-end

## Execution Order

**Wave 1** (parallel): Issue 1, Issue 2
**Wave 2** (after Wave 1): Issue 3, Issue 4
**Wave 3** (after Wave 2): Issue 5
**Wave 4** (after Wave 3): Issue 6, Issue 7
**Wave 5** (after Wave 4): Issue 8

## Next Steps
- Approve plan → create beads issues with dependencies
- `/crank` for autonomous execution or `/implement <issue>` for single issue
