# Bug Hunt: Duplicate Suggestions, Zsh Ghosttext After Space, Daemon Dial Failures

Date: 2026-02-10

Symptoms reported:
- Suggestion sometimes repeats the immediately previous command (e.g. ran `make install`, next suggestion is `make install`).
- Zsh inline suggestion: typing `make` shows `make| install` (ghosttext), but after pressing space it becomes `make | install` where `install` is no longer ghosttext-highlighted.
- Daemon errors like: `rpc error: code = Unavailable ... dial unix ~/.clai/clai.sock`.

## Phase 1: Root Cause

### 1) Duplicate “last command” suggestion
- Root cause: when the daemon is unavailable, `clai suggest` falls back to shell history (`internal/history`). The fallback path had no notion of “last executed command in this session”, so it could return the most recent history match, which is often the just-executed command.
- Location:
  - Fallback behavior: `internal/cmd/suggest.go` calls history fallback when daemon returns nothing.
  - History prefix search: `internal/history/history.go`.

### 2) Zsh ghosttext loses ghost styling after inserting a space
- Root cause: the zsh integration used `region_highlight=()` and `region_highlight=(...)` assignments to manage ghosttext styling.
  - This clobbered any existing `region_highlight` from other plugins (notably `zsh-syntax-highlighting`).
  - Depending on widget ordering (especially `magic-space`), other highlighters could overwrite `region_highlight` after clai updates, leaving `POSTDISPLAY` visible but unstyled.
- Location: `internal/cmd/shell/zsh/clai.zsh`.

### 3) Daemon dial failures / “history provider: rpc Unavailable … dial unix … clai.sock”
- Root cause: a daemon could be alive and holding the lock (`~/.clai/clai.lock`) while the socket path (`~/.clai/clai.sock`) was missing (unlinked socket). In that state:
  - Clients can’t connect because the path doesn’t exist.
  - A new daemon can’t start because the lock is held.
  - Existing code could misreport status because PID files could be stale or overwritten.
- Location:
  - Daemon lifecycle checks: `internal/daemon/lifecycle.go`, `internal/daemon/lockfile.go`.
  - Daemon spawn/socket cleanup: `internal/ipc/spawn.go`.

## Phase 2: Pattern Analysis

- The V2 scorer already had an explicit rule: “Never suggest the exact last command again” (`internal/suggestions/suggest/scorer.go`).
- The bug appeared when that last-command state wasn’t available (daemon down/unreachable), and when shell rendering relied on destructive updates to shared state (`region_highlight`).

## Phase 3: Hypothesis and Testing

Hypotheses:
1. If we suppress `CLAI_LAST_COMMAND` at the `clai suggest` CLI boundary, then we stop repeating the last command even during history fallback.
2. If we preserve other `region_highlight` entries and track/remove only clai’s own highlight span, ghosttext will remain styled after space and won’t break other highlighters.
3. If daemon lifecycle uses lock-held PID as a fallback, and `EnsureDaemon()` handles the “lock held but socket missing” case, clients recover without manual intervention.

Validation:
- `make test` (includes unit, integration, and expect-based interactive shell tests) passed after changes.
  - Notably: `tests/expect` includes `Zsh ghost text persists after space`.

## Phase 4: Implementation

### Fixes

1. Suppress suggesting the last executed command across all sources
- Export `CLAI_LAST_COMMAND` from shell integrations:
  - Zsh: `internal/cmd/shell/zsh/clai.zsh`
  - Bash: `internal/cmd/shell/bash/clai.bash`
  - Fish: `internal/cmd/shell/fish/clai.fish`
- Filter it out in `clai suggest` regardless of daemon/history/AI cache:
  - `internal/cmd/suggest.go`

2. Stabilize zsh ghosttext styling
- Track clai’s highlight span separately (`_AI_GHOST_HIGHLIGHT`) and only add/remove that entry.
- Preserve any existing `region_highlight` from other plugins.
- Register the redraw invariant using `add-zle-hook-widget` when available (fallback to `zle -N` otherwise).
  - `internal/cmd/shell/zsh/clai.zsh`
  - Tests updated accordingly in `internal/cmd/init_test.go`.

3. Daemon robustness: recover from lock-held PID with missing socket
- Add lock-held PID fallback for lifecycle status/stop.
- Make IPC ensure/spawn logic safer around stale socket handling:
  - Refuse to unlink a socket file if daemon lock is held.
  - If socket path is missing but lock is held, terminate the lock-holder after a short grace window, then spawn a fresh daemon.

### Commits
- `1646351` fix(daemon): recover when socket path is missing
- `c71e0cb` fix(suggest): suppress last cmd and stabilize zsh ghosttext

