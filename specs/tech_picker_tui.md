# Spec: clai-picker TUI (History Picker)

**Date:** 2026-02-04
**Owner:** clai
**Status:** Draft

## Summary
Add a first-party TUI picker (Bubble Tea) that provides a consistent **history** selection experience across zsh, bash, and fish. Shells invoke a standalone `clai-picker` binary directly; the TUI handles tabs (Session/Global), paging, selection, and filtering. Suggestions remain ghost text or shell-native (no suggestion picker).

## Goals
- Consistent history picker UX across zsh/bash/fish.
- Low-latency interaction with large histories.
- Safe behavior in non-interactive contexts (no TTY -> fail fast).
- Backend swapping (builtin/fzf/clai) without changing shell glue.
- Extensible tabs (each tab maps to a clai-defined provider + args; no arbitrary commands in v1).

## Non-Goals
- Full fuzzy-search parity with fzf.
- Suggestion picker UI.
- Replacing native shell completion systems.
- Windows/PowerShell support in this phase.
- Mouse interaction (click/scroll) in v1.
- Full screen reader accessibility in v1.

## User Experience
### Entry
- Primary trigger: Up Arrow (configurable behavior when buffer empty).
- Always-open trigger: Alt+H (or similar).

### Tabs
- History tabs: Session | Global.
- Tab key cycles tabs inside the picker. Tab change resets paging: set page=0, selection=0, at_end=false, cancel any in-flight or debounced fetch, then fetch page 0 for the new tab.
- Active tab is rendered with bold and `[brackets]` (e.g., `[Session] Global`). In monochrome/NO_COLOR mode, the active tab is prefixed with `>`.
- Future tabs may execute different provider providers/args; the tab system must support that.

### List Behavior
- Bottom-up ordering (newest item at bottom).
- Up moves toward older items; when at top, fetch next page (show `...` placeholder at top while loading) and move cursor to bottom of the new page.
- Down moves toward newer items; does nothing when at the first (newest) item.
- Enter inserts the selected command into the shell buffer. If the item list is empty, Enter is a no-op.
- Esc / Ctrl+C cancels and restores the original buffer/cursor.
- When the item list is empty, display "No history found" (or "No matches for \"<query>\"" when a query is active). Navigation keys (Up/Down) are no-ops. The query input field remains active so the user can edit the query or press Esc to exit.

**Paging cursor detail:** When the cursor is on the topmost (oldest visible) item and the user presses Up, fetch the next page of older items, prepend them to the list, and place the cursor on the last item of the newly fetched page (the item just above the previously topmost item). The viewport scrolls to keep the cursor visible.

### Search Input
- Picker has an input field; initial value is seeded from the shell buffer.
- The search input is always focused. Printable characters update the query and re-fetch results (debounced at ~100ms). Each keystroke resets the debounce timer; only the final query after the debounce interval fires a fetch. Any in-flight fetch from a prior debounce is cancelled via context.
- Left/Right move the cursor within the query for mid-string edits.
- Up/Down/Tab/Enter/Esc are reserved for list/navigation and never inserted into the query.
- Matching is **substring** by default.
- Matched substrings are highlighted in the results list (bold or inverse). In NO_COLOR/monochrome mode, matched text uses bold if supported; if bold is unavailable, use reverse video or underline.
- Case sensitivity is configurable (default: case-insensitive).
- Commands longer than the terminal width are **middle-truncated** with `...` in the list view. Show at least 20 characters from the start and the remainder from the end; if terminal width < 45, use start-only truncation with trailing `...`.

### Keybindings by Shell
**Policy:** Up is intercepted only when `history.picker_open_on_empty=true` **or** the buffer is non-empty. Otherwise Up falls back to native history. Down is intercepted only while the picker is open.

| Shell | Modes | Up | Down | Tab | Esc/Ctrl+C | Always-open |
| --- | --- | --- | --- | --- | --- | --- |
| zsh | emacs, viins, vicmd | open picker (policy above) | navigate (picker open) | cycle tabs (picker open) | cancel picker | Alt+H |
| bash | emacs, vi-insert, vi-command | open picker (policy above) | navigate (picker open) | cycle tabs (picker open) | cancel picker | Alt+H |
| fish | default, insert, visual | open picker (policy above) | navigate (picker open) | cycle tabs (picker open) | cancel picker | Alt+H |

Notes:
- zsh: bind in emacs/viins/vicmd keymaps.
- bash: bind in emacs/vi-insert/vi-command keymaps.
- fish: bind with `bind -M default`, `bind -M insert`, `bind -M visual`.

### Bash 3.2 Compatibility
- `bind -x` requires Bash 4.0+. On macOS (Bash 3.2), picker integration is unavailable; fall back to native history.

## CLI Contract
New entry point:
- `clai-picker history`

Flags:
- `--tabs=session,global` (comma-separated tab IDs; must match configured tab IDs)
- `--limit` (positive integer; overrides `picker_page_size` config)
- `--query` (string, max 4096 bytes; seeded from shell buffer)
- `--session` (string; `CLAI_SESSION_ID` UUID v4, 36 chars; if empty, session tab falls back to global scope)
- `--output=plain` (stdout selected item; only mode in v1, exists for forward compatibility; unknown values cause exit 1 with error to stderr)

Unknown flags cause exit 1 with usage printed to stderr. `--help` and `--version` are supported (exit 0).

Exit behavior:
- Success: exit 0 with selected item on stdout (followed by a single newline; `$()` capture strips it).
- Cancel: exit 1 with no output.
- No terminal / TERM=dumb: exit 2 with no output (cannot open `/dev/tty` or terminal incapable).
- Invalid flags/args: exit 1 with error to stderr.

### Query Semantics
`--query` is **substring match** for v1 (no fuzzy). Filtered results are ordered by timestamp descending (most recent first) within the selected scope. Case sensitivity is controlled by config (default: case-insensitive).

## Config
- `history.picker_backend`: `builtin|fzf|clai` (default: current behavior). Must be one of the three values; reject unknown values at config load.
- `history.picker_open_on_empty`: `false` by default.
- `history.picker_page_size`: default 100, min 20, max 500. Values outside range are clamped with a warning to stderr.
- `history.picker_case_sensitive`: `false` by default.

These keys must be added to `internal/config/config.go` as a new `HistoryConfig` struct with YAML tags. Expose all keys in `ListKeys()`.

## Environment Variables
- No new environment variables are introduced in v1.
- Existing `CLAI_*` variables (e.g., `CLAI_SESSION_ID`) are used for session context.
- `CLAI_SESSION_ID`: UUID v4 (36 chars), generated per interactive shell session by `clai init`, preserved across re-sourcing. Scopes daemon history queries to the current session. Lifecycle: created on shell init, destroyed when shell exits. If empty or missing, the session tab falls back to global scope.
- `CLAI_DEBUG=1`: when set, picker logs diagnostic messages to stderr (connection failures, config warnings, fallback events).

## Performance Constraints
- Provider context timeout: **200ms hard deadline** per individual fetch call. On timeout: Model transitions to error state; display "Loading timed out" inline; user can press Up to retry or Esc to cancel. No automatic retry.
- UI must remain responsive while data loads (no blocking render loop).
- Lazy paging only; never fetch entire history up front.
- **Provider-side filtering only:** queries must be pushed down to the provider; the TUI must never client-filter paged results.
- **Paging applies to the filtered result set:** offset/limit are applied after filtering. Filtered results are ordered by timestamp descending.
- **Query change resets paging:** set page=0, selection=0, at_end=false, cancel in-flight fetch, then fetch page 0.
- **Tab change resets paging:** same reset as query change (page=0, selection=0, at_end=false, cancel in-flight).
- Startup latency budget: first frame (loading state or empty state) rendered within 100ms of `tea.Run()` call (target). This excludes config load and TTY open.
- **Minimum terminal width:** 20 columns. If terminal width < 20, exit 2 with "Terminal too narrow" to stderr.

## TTY / Non-Interactive Behavior
- If `/dev/tty` cannot be opened, picker exits 2 with no output. Stdin/stdout TTY status is not checked since the picker opens `/dev/tty` directly for both input and rendering.
- Shell glue must only invoke picker in interactive shells.

## Terminal Capability & Accessibility
- If `TERM=dumb` or terminal capability detection fails, skip TUI and exit 2 (same as no-TTY; shell glue treats exit 2 as "picker unavailable").
- Only TERM=dumb triggers fallback; screen*, tmux*, and xterm* are treated as capable terminals.
- Provide a monochrome ASCII fallback (no Unicode/emoji; selection indicated with `>`), and avoid relying on color alone.
- Honor `NO_COLOR` to disable color styling.
- Default rendering delegates to Bubble Tea/lipgloss adaptive color detection (no explicit truecolor sequences).
- Accessibility: do not use color-only distinction; always show a leading marker on the selected row.
- In monochrome mode, invert foreground/background for the selected row when possible (fallback to `>` marker).

## Input Sanitization
- **--query:** max 4096 bytes. Strip control characters (0x00-0x1F except 0x09 tab). Reject embedded newlines. If exceeded, truncate to 4096 bytes and proceed.
- **History items from providers:** strip ANSI escape sequences (regex `\x1b\[[0-9;]*[a-zA-Z]`) before display AND before stdout output. Multi-line commands: display with a visual `\n` indicator in the list; output the full multi-line command as-is on stdout (shell handles multi-line insertion).
- **Selected output:** raw UTF-8 text only, no terminal escape codes. Followed by a single newline.

## Encoding
- Assume UTF-8 for rendering.
- Providers must return UTF-8 strings; replace invalid sequences before returning items.
- Replace invalid UTF-8 with the Unicode replacement character or `?` in ASCII fallback mode.

## Architecture
- TUI implemented with Bubble Tea (requires `github.com/charmbracelet/bubbletea` v2.0+ and `github.com/charmbracelet/lipgloss` v1.0+). **Prerequisite:** `go get github.com/charmbracelet/bubbletea@v2 github.com/charmbracelet/lipgloss@v1` before any picker code.
- Providers:
	- History provider (session/global, optional substring filtering).
	- Tabs map to **clai-defined providers + args** (no arbitrary shell commands in v1).
- Providers use contexts with 200ms timeouts and support cancellation on tab/page/query changes.
- Deduplicate history results by command text (case-insensitive per config), keeping the most recent occurrence by `ts_unix_ms`. Deduplication is scoped to the active tab.
- **Concurrency:** Use a file lock (`$CLAI_CACHE/picker.lock`) to prevent concurrent picker instances. If lock acquisition fails, exit 1.

## Tab-to-Provider Mapping
Tab definitions must be configurable to allow future tabs to call different providers.

Proposed structure (config):
- `history.picker_tabs`: list of objects `{id, label, provider, args}`
- `provider` is a clai-defined provider name (v1: `history`).
- `args` are provider-specific key/value args (no shell commands). Values containing `$ENV_VAR` or `${ENV_VAR}` are interpolated at runtime.
- v1 defaults:
  ```yaml
  history:
    picker_tabs:
      - id: session
        label: Session
        provider: history
        args:
          session: "$CLAI_SESSION_ID"  # Interpolated at runtime
      - id: global
        label: Global
        provider: history
        args:
          global: "true"
          # No session arg = query all sessions
  ```

CLI remains `--tabs=session,global` for v1; tab ids map to config entries.

## Implementation Instructions
### 0) Prerequisites
- Add bubbletea and lipgloss to go.mod: `go get github.com/charmbracelet/bubbletea@v2 github.com/charmbracelet/lipgloss@v1`
- Add `HistoryConfig` struct to `internal/config/config.go` with all picker keys and YAML tags.
- Verify `go build ./cmd/clai-picker` succeeds before proceeding.

### 1) Binary and Distribution
- Implement `clai-picker` as a separate Go binary (similar to `clai-shim`).
- Build via `go build ./cmd/clai-picker`. Add to Makefile `build-all` in a follow-up task.
- `clai-picker` imports `internal/config` for config loading. Keep `internal/config` free of transitive dependencies on provider/API packages to minimize binary size and startup time.
- Shell glue must locate `clai-picker` via PATH; if missing, fall back to native history silently.

### 2) Command + Dispatch
- Add `clai-picker history` command.
- Startup order (strict):
	1. Check `/dev/tty` is openable; exit 2 if not (before any I/O).
	2. Check `TERM != dumb`; exit 2 if dumb.
	3. Check terminal width >= 20; exit 2 if too narrow.
	4. Acquire file lock (`$CLAI_CACHE/picker.lock`); exit 1 if another instance is running.
	5. Parse flags; exit 1 with usage on error.
	6. Read config and choose backend (`builtin|fzf|clai`).
	7. Dispatch to backend implementation.

### 3) Picker Core (Bubble Tea)
- Create `internal/picker` package with:
	- `Model` with explicit state enum:
	  ```
	  States: idle | loading | loaded | empty | error
	  Transitions:
		idle    → loading    (on open / fetch triggered)
		loading → loaded     (fetch returns items)
		loading → empty      (fetch returns 0 items)
		loading → error      (fetch timeout / failure)
		loaded  → loading    (page change / tab change / query change)
		empty   → loading    (query change / tab change)
		error   → loading    (user retries with Up)
		any     → cancelled  (Esc / Ctrl+C)
	  ```
	- Model fields: state, items, selection index, page, at_end, tab list, active tab, query string, currentRequestID (uint64), viewport width/height.
	- **Selection bounds:** after any items list mutation, clamp `selection = min(selection, len(items)-1)`. If items is empty, selection = -1 (Enter is no-op).
	- `Provider` interface: `Fetch(ctx, req) (items, atEnd, err)`.
	- `Request` includes scope, query, limit, offset/page, `RequestID uint64`, and a generic `Options map[string]string` for future providers. Provider echoes `RequestID` in response.
- Implement Bubble Tea `Init`, `Update`, `View`:
	- `Update` handles keys (Up/Down/Tab/Enter/Esc and text input).
	- Use `tea.Cmd` to fetch pages asynchronously.
	- Discard stale results: only apply fetch results where `response.RequestID == model.currentRequestID`. Increment `currentRequestID` on every new fetch (page/tab/query change).
	- Provide loading and empty states. Loading: show spinner or `...` placeholder. Empty state: display "No history found" (or "No matches for \"<query>\"" when a query is active). Error state: display "Loading timed out" with hint to press Up to retry or Esc to cancel.
- Handle `tea.WindowSizeMsg` (SIGWINCH) to recompute layout while preserving selection. View must re-derive viewport dimensions from the latest WindowSizeMsg on each render (not cached).
- Handle suspend/terminate signals: restore terminal state on SIGTSTP/SIGTERM/SIGHUP/SIGINT.
	- Use `tea.WithSuspendHandler` if available; otherwise implement custom suspend/resume sequence.
- Use the alternate screen buffer (tea.WithAltScreen) for clean entry/exit.
- On Enter: print selected item (sanitized, no ANSI codes) to stdout and exit 0.
- On Esc/Ctrl+C: exit 1 with no output.

### 4) IO Contract
- TUI must render to /dev/tty (not stdout). Use `tea.WithInput(tty)` and `tea.WithOutput(tty)`.
- stdout is reserved for the selected command only (followed by a single newline).
- stderr is reserved for errors/diagnostics.
- If /dev/tty cannot be opened, exit 2 with no output.

### 5) Providers

#### Provider Protocol (Daemon IPC)
The history provider queries the daemon via gRPC. A new RPC method is required:

```
rpc FetchHistory(HistoryFetchRequest) returns (HistoryFetchResponse)

HistoryFetchRequest {
  string session_id = 1;   // Optional: filter by session (UUID v4)
  string query = 2;        // Substring filter (provider-side)
  int32  limit = 3;        // Page size
  int32  offset = 4;       // Pagination offset
  bool   global = 5;       // True = all sessions
}

HistoryFetchResponse {
  repeated HistoryItem items = 1;
  bool at_end = 2;
}

HistoryItem {
  string command = 1;
  int64  timestamp_ms = 2;
}
```

The daemon handler queries SQLite (`internal/storage`) with the existing pagination and query capabilities. Socket location: `config.DefaultPaths().SocketFile()` (same as daemon server).

#### Provider Fallback
- If daemon IPC fails (socket not found, connection refused, timeout), exit 1. Shell glue treats non-zero as cancel and falls back to native history.
- No direct file reading fallback in v1 (daemon must be running).

#### Provider Implementation
- Implement substring filtering **in the provider** (push query into provider fetch; no client-side filtering).
- All provider calls are wrapped with context timeout of 200ms.
- Provider must strip ANSI escape sequences from history entries before returning.

### 6) Shell Integration
Shell glue must capture stdout for selection while allowing the picker to read and render via /dev/tty.

**Shell capture pattern (all shells):**
- Picker opens `/dev/tty` directly for input and rendering; stdout is captured by the shell's `$()`.
- No stdin redirect is needed since the binary opens `/dev/tty` itself.

**Zsh:**
```zsh
_clai_picker_open() {
  local result
  result=$(clai-picker history --query="$BUFFER" --session="$CLAI_SESSION_ID") || return
  BUFFER="$result"
  CURSOR=${#BUFFER}
  zle redisplay
}
zle -N _clai_picker_open
bindkey '^[[A' _clai_picker_open  # Up arrow (emacs/viins)
```
- Respect `history.picker_open_on_empty`.
- On cancel (exit 1/2): restore buffer and cursor; fall back to native history if picker exited 2.

**Bash:**
```bash
_clai_picker_open() {
  local result
  result=$(clai-picker history --query="$READLINE_LINE" --session="$CLAI_SESSION_ID")
  if [ $? -eq 0 ]; then
    READLINE_LINE="$result"
    READLINE_POINT=${#READLINE_LINE}
  fi
  # Non-zero exit: leave buffer unchanged
}
bind -x '"\e[A": _clai_picker_open'  # Requires Bash 4.0+
```
- If picker exits non-zero, do nothing (leave buffer unchanged).

**Fish:**
```fish
function _clai_picker_open
  set -l result (clai-picker history --query=(commandline) --session=$CLAI_SESSION_ID)
  if test $status -eq 0
    commandline -r -- $result
  end
  # Non-zero: buffer unchanged
  commandline -f repaint
end
bind \e\[A _clai_picker_open  # Up arrow
```
- On cancel: restore buffer without side effects.

### 7) Backend Fallback
- If backend is `fzf` and fzf is not in PATH: silently fall back to `builtin`. Log to stderr only if `CLAI_DEBUG=1`.
- If backend is `clai` and daemon is unavailable: exit 1. Shell glue treats non-zero as cancel and falls back to native history.
- Fallback order: configured backend -> `builtin` -> exit 1.
- Shell glue always calls the same entry point. Shell glue distinguishes exit 2 (picker unavailable, fall back to native history permanently) from exit 1 (cancel, no action needed).

## Stdout/Stderr Contract
- stdout: selected command only.
- stderr: all errors/diagnostics.

## Signals and Edge Cases
- SIGWINCH: recompute layout and keep selection stable.
- SIGTSTP: restore terminal state before suspend; re-init on resume.
- SIGTERM/SIGHUP/SIGINT: restore terminal state and exit cleanly.
- SIGTTOU: ignore to prevent background TTY writes from suspending the process (macOS validation required).
- SIGPIPE: ignore signal; exit on write errors.
- Exit codes on signals: SIGTERM=143, SIGHUP=129, SIGINT=130 (shell glue treats any non-zero as cancel).

## Testing
Automated:
- Picker state machine: Up/Down/Tab, paging, selection, cancel, query input. Include all state transitions (idle/loading/loaded/empty/error).
- Selection bounds clamping after query/tab changes (regression: selection > len(items)).
- Backend dispatcher selection + fallback (fzf missing -> builtin).
- TTY detection behavior (non-TTY exit 2, TERM=dumb exit 2).
- Terminal capability fallback (TERM=dumb).
- Signal handling (SIGWINCH, SIGTSTP).
- Request ID stale response handling (rapid tab switch, rapid typing).
- Input sanitization: --query with ANSI codes, control chars, newlines, >4096 bytes.
- History item sanitization: entries with ANSI escapes stripped before display and output.
- Empty history (0 items): verify no-ops for Up/Down/Enter, Esc works.
- Zero search results: verify Enter is no-op, query field editable.
- File lock: concurrent picker instance exits 1.
- Provider timeout: verify error state displayed, retry possible.
- Flag validation: unknown flags, invalid --output, out-of-range --limit.

Manual:
- Open history picker, switch tabs, paging, selection, cancel, fallback.
- Smoke tests for zsh/bash/fish integration (selection insertion + cancel restore).
- Verify active tab indicator is visible in both color and monochrome modes.

## Risks
- Rendering and key handling edge cases under different terminal settings.
- Latency spikes from slow history providers (mitigated by 200ms hard timeout + async fetch).
- Shell glue differences (buffer restoration correctness).
- History entries containing ANSI escapes or newlines (mitigated by sanitization).
- Concurrent picker instances racing on /dev/tty (mitigated by file lock).

## Task Breakdown Recommendation
1) **Core contract + config**
	- CLI entry point, config keys, TTY checks.
2) **Bubble Tea core**
	- Tabs, list rendering, paging, selection, cancel, query input.
3) **Providers**
	- History provider with timeout + cancellation.
4) **Shell glue**
	- zsh + bash + fish integration with buffer restore.
5) **Backend dispatcher**
	- builtin/fzf/clai selection and fallback behavior.
6) **Tests + docs**
	- Unit tests + manual checklist update.
