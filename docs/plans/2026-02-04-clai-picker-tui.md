# clai-picker TUI (History Picker) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a standalone `clai-picker` Bubble Tea history picker that works across zsh/bash/fish and integrates with shell buffers safely.

**Architecture:** Separate `clai-picker` binary with Bubble Tea UI, provider-side filtering, config-driven tabs (clai-defined providers only), and shell glue that captures stdout while the TUI renders to `/dev/tty`.

**Tech Stack:** Go, Bubble Tea (charmbracelet), existing clai config/history modules.

---

### Task 1: Config schema + defaults for history picker

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
func TestHistoryPickerConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.History.PickerBackend != "builtin" {
		t.Fatalf("expected default backend builtin, got %s", cfg.History.PickerBackend)
	}
	if cfg.History.PickerOpenOnEmpty {
		t.Fatalf("expected picker_open_on_empty=false by default")
	}
	if cfg.History.PickerPageSize != 100 {
		t.Fatalf("expected picker_page_size=100, got %d", cfg.History.PickerPageSize)
	}
	if cfg.History.PickerCaseSensitive {
		t.Fatalf("expected picker_case_sensitive=false by default")
	}
	if len(cfg.History.PickerTabs) == 0 {
		t.Fatalf("expected picker_tabs defaults to be populated")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run TestHistoryPickerConfigDefaults -v`
Expected: FAIL (missing fields)

**Step 3: Write minimal implementation**

- Add `HistoryConfig` with:
  - `picker_backend`, `picker_open_on_empty`, `picker_page_size`, `picker_case_sensitive`, `picker_tabs`.
- Add default tabs for `session` and `global` with clai-defined provider and args.
- Add validation for page size range and backend values.
- Add `history.*` keys to `Get/Set` and `ListKeys`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config -run TestHistoryPickerConfigDefaults -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add history picker config defaults"
```

---

### Task 2: clai-picker binary skeleton + TTY open

**Files:**
- Create: `cmd/clai-picker/main.go`
- Create: `internal/picker/tty.go`
- Create: `internal/picker/tty_test.go`

**Step 1: Write the failing test**

```go
func TestOpenTTYFailureExits(t *testing.T) {
	code := runPickerWithNoTTY()
	if code != 2 {
		t.Fatalf("expected exit code 2 for no tty, got %d", code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/picker -run TestOpenTTYFailureExits -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Open `/dev/tty` for input+output; if open fails, exit 2.
- Wire `main.go` to parse args and call the picker entry point.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/picker -run TestOpenTTYFailureExits -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/clai-picker/main.go internal/picker/tty.go internal/picker/tty_test.go
git commit -m "feat: add clai-picker tty open"
```

---

### Task 3: Bubble Tea model + UI with search input

**Files:**
- Create: `internal/picker/model.go`
- Create: `internal/picker/update.go`
- Create: `internal/picker/view.go`
- Create: `internal/picker/model_test.go`

**Step 1: Write the failing test**

```go
func TestPickerQueryResetsPaging(t *testing.T) {
	m := NewModel(...)
	m = m.SetQuery("git")
	if m.Page != 0 || m.Selection != 0 || m.AtEnd {
		t.Fatalf("expected paging reset on query change")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/picker -run TestPickerQueryResetsPaging -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Model state: items, selection, page, at_end, tabs, active tab, query, cursor position.
- Key handling:
  - Left/Right move cursor in query.
  - Up/Down navigate list.
  - Tab switches tabs.
  - Enter selects; Esc/Ctrl+C cancels.
- Query changes reset paging and trigger fetch.
- Use `tea.Cmd` for async fetch; stale results discarded by request ID.
- Render middle-truncated rows; highlight match substrings; show loading/empty states.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/picker -run TestPickerQueryResetsPaging -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/picker/model.go internal/picker/update.go internal/picker/view.go internal/picker/model_test.go
git commit -m "feat: add picker model and ui"
```

---

### Task 4: Provider interface + history provider (server-side filtering)

**Files:**
- Create: `internal/picker/provider.go`
- Create: `internal/picker/provider_history.go`
- Create: `internal/picker/provider_history_test.go`

**Step 1: Write the failing test**

```go
func TestHistoryProviderFiltersAndPages(t *testing.T) {
	items, atEnd := FetchHistory("git", 0, 50)
	if len(items) == 0 {
		t.Fatalf("expected filtered results")
	}
	if atEnd && len(items) == 50 {
		t.Fatalf("atEnd should be false when full page returned")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/picker -run TestHistoryProviderFiltersAndPages -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Provider API: `Fetch(ctx, req)` where req includes query/limit/offset.
- Implement substring filtering **inside the provider** (no client-side filtering).
- Offset/limit apply to filtered results.
- Deduplicate by command text, keeping most recent occurrence.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/picker -run TestHistoryProviderFiltersAndPages -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/picker/provider.go internal/picker/provider_history.go internal/picker/provider_history_test.go
git commit -m "feat: add history provider with server-side filtering"
```

---

### Task 5: IO routing + signals

**Files:**
- Modify: `cmd/clai-picker/main.go`
- Modify: `internal/picker/model.go`
- Create: `internal/picker/io_test.go`

**Step 1: Write the failing test**

```go
func TestStdoutContract(t *testing.T) {
	out := runPickerAndCaptureStdout()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("stdout must not include escape sequences")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/picker -run TestStdoutContract -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Use `tea.WithInput(tty)` and `tea.WithOutput(tty)`.
- stdout only prints the final selection.
- Ignore SIGTTOU and SIGPIPE; handle SIGWINCH/SIGTSTP/SIGTERM/SIGHUP/SIGINT.
- Use alternate screen buffer.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/picker -run TestStdoutContract -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/clai-picker/main.go internal/picker/model.go internal/picker/io_test.go
git commit -m "feat: harden picker io and signals"
```

---

### Task 6: Shell glue integration (zsh/bash/fish)

**Files:**
- Modify: `internal/cmd/shell/zsh/clai.zsh`
- Modify: `internal/cmd/shell/bash/clai.bash`
- Modify: `internal/cmd/shell/fish/clai.fish`
- Modify: `specs/shell-integration-testing.md`

**Step 1: Write the failing test**

Add to `specs/shell-integration-testing.md`:
- Up opens picker when buffer non-empty (open_on_empty=false).
- Alt+H always opens picker.
- Cancel restores buffer.
- Bash cancel leaves buffer unchanged (no native Up fallback).
- Bash 3.2 note: picker integration disabled.

**Step 2: Run test to verify it fails**

Run: manual checklist (expected failures)

**Step 3: Write minimal implementation**

- Zsh: bind in emacs/viins/vicmd; pass `--query="$BUFFER" --session="$CLAI_SESSION_ID"`.
- Bash: bind in emacs/vi-insert/vi-command; pass `--query="$READLINE_LINE" --session="$CLAI_SESSION_ID"`.
- Fish: bind in default/insert/visual; pass `--query="$(commandline)" --session="$CLAI_SESSION_ID"`.
- If `clai-picker` missing, fall back to native history silently.

**Step 4: Run test to verify it passes**

Run: manual checklist
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cmd/shell/zsh/clai.zsh internal/cmd/shell/bash/clai.bash internal/cmd/shell/fish/clai.fish specs/shell-integration-testing.md
git commit -m "feat: integrate clai-picker shell glue"
```

---

### Task 7: Backend dispatcher + docs

**Files:**
- Modify: `cmd/clai-picker/main.go`
- Modify: `specs/2026-02-04-clai-picker-tui.md`

**Step 1: Write the failing test**

```go
func TestBackendDispatchFallback(t *testing.T) {
	backend := chooseBackend("fzf", false)
	if backend != "builtin" {
		t.Fatalf("expected fallback to builtin")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/clai-picker -run TestBackendDispatchFallback -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Implement backend selection + fallback rules.
- Ensure `fzf` not installed -> fallback to builtin.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/clai-picker -run TestBackendDispatchFallback -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/clai-picker/main.go specs/2026-02-04-clai-picker-tui.md
git commit -m "feat: add backend dispatcher for clai-picker"
```
