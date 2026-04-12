# clai PTY Wrapper — Functional Specification

**Version:** 1.0 (User-Facing Functional Spec)
**Technical Source:** `./specs/pty_tech_v1.md`
**Test Source:** `./specs/pty_tests_v1.md`

> This document describes PTY wrapper behavior from the user's perspective: what works, when it degrades, what gets captured, and what quality guarantees exist.

---

## 1. Overview and Goals

`clai-wrap` runs your shell inside a PTY and adds fast command assistance without taking control away from you.

Primary user outcomes:

- Open a history/completion picker instantly from your current shell session.
- Insert commands safely into the current prompt (never auto-execute).
- Keep shell behavior stable across normal usage, full-screen tools, and remote shells.
- Get post-failure assistant comments when daemon intelligence is available.
- Continue working even if the daemon is unavailable (standalone fallback).

Non-goals in v1:

- No transparent composited overlay on top of existing terminal content.
- No remote agent on SSH target hosts.
- No tmux-specific feature guarantees.

---

## 2. User Experience Principles

- Fast: picker appears with low perceived latency.
- Safe: suggestions are inserted, not auto-run by default.
- Stable: terminal state is restored on exit and signal interruptions.
- Resilient: daemon failures do not kill your shell session.
- Privacy-first: sensitive interactive sessions are excluded from capture.
- Predictable: degraded modes are explicit and deterministic.

---

## 3. What the User Runs

Wrapper mode:

- Install once with shell integration (`clai install`) and start a new shell session.
- By default (`pty.enabled=true`), new interactive sessions auto-start in `clai-wrap` when available.
- `clai-wrap` starts your shell inside a PTY and owns terminal IO routing.
- You can toggle this behavior with:
  - `clai pty on` (enable auto-wrap for new sessions)
  - `clai pty off` (disable auto-wrap for new sessions)
  - `clai pty status` (show state)

Daemon behavior:

- Daemon connection is default-on.
- If daemon connect/health checks fail (500ms timeout), wrapper continues in standalone mode.
- If daemon disconnects mid-session, wrapper retries once (500ms), then stays standalone.

Standalone mode from user perspective:

- Shell remains fully usable.
- Picker remains available with local history behavior.
- Some AI suggestion/comment features are unavailable.
- A one-time warning is shown.

---

## 4. Supported Platforms and Shells

Platforms:

- Linux/macOS via POSIX PTY.
- Windows 10/11 via ConPTY.

Shells:

- Primary: bash, zsh, fish.
- Windows shells: PowerShell and cmd (with cmd limitations).

Shell integration notes:

- fish with native OSC133 support uses native behavior when available.
- cmd does not support OSC133 semantics used by capture timing features.
- Bash login-shell injection is opt-in in v1.

---

## 5. Core Interaction Model

### 5.1 Runtime states

- `Passthrough`: normal typing and shell output flow.
- `PickerOpen`: keyboard input is routed to picker; PTY output is buffered.
- `Standalone`: daemon-intelligence disabled, shell and picker remain usable.

### 5.2 Hotkey behavior

- Hotkey chords are configurable.
- Default behavior supports chord timeout and byte-safe forwarding on timeout.
- Hotkey handling avoids unexpected byte loss.

### 5.3 Picker behavior

- Opens in alt-screen.
- Supports incremental search and selection/cancel actions.
- On close, buffered output is flushed in order and normal passthrough resumes.

### 5.4 Insert behavior

- Default: insert selected command without execution.
- Optional: insert and execute mode appends newline.
- Bracketed paste is used only when shell/app has enabled it.

---

## 6. Degraded and Non-TTY Behavior

v1 default is stream-aware degradation:

- stdin non-TTY: hotkey detection disabled; passthrough remains where possible.
- stdout non-TTY: picker disabled; passthrough/capture paths continue where valid.
- all streams non-TTY: wrapper errors unless `--force-non-tty` is set.
- `--force-non-tty`: pure passthrough (no interactive wrapper features).

Additional degrade trigger:

- If OSC133 prompt markers are not detected within 500ms at startup, prompt-bound capture/comment features are disabled while shell and picker remain usable.

---

## 7. Suggestions and Assistant Comments

Failed-command flow:

1. Command fails.
2. Wrapper/daemon pipeline analyzes eligible context.
3. Suggestion is rendered as a shell comment at a prompt-safe boundary.

Comment behavior:

- Bash/zsh/fish/PowerShell: `#`
- cmd: `REM `

Safety property:

- Suggestions are shown as comments and not injected into the active editable command buffer.

---

## 8. Privacy and Capture Policy

### 8.1 Interactive safety gates

Gate 1: denylist-based pause (for sensitive interactive commands).  
Gate 2: echo-gap heuristic fallback for likely secure input scenarios.

Effect:

- Sensitive periods are excluded from persisted capture.
- Shell output still appears normally to user.

### 8.2 Hop 1 and Hop 2 transfer policy

Hop 1 (`clai-wrap` -> `clai-daemon`, local IPC):

- Failed command: captured output sent.
- Successful command: only last 20 lines sent.
- Explicit user trigger: send captured output regardless of exit code.

Hop 2 (`clai-daemon` -> external provider):

- Failed command: allowed by policy after privacy gates.
- Successful command: not sent in v1.

---

## 9. Reliability and Recovery Guarantees

- Terminal attributes are restored on normal exit and signal-driven shutdown paths.
- Alt-screen and cursor state are restored after picker use and interruption.
- Resize behavior uses trailing-edge debounce and applies final size.
- PTY read path prioritizes non-blocking drain behavior to avoid deadlock under heavy output.
- Buffer overflow may truncate old output, but wrapper should remain responsive.

---

## 10. Configuration Surface (User-Visible)

Primary flags:

- `--shell <path>`
- `--login-shell`
- `--hotkey <chord>`
- `--buffer-cap <bytes>`
- `--execute-on-select`
- `--history-file <path>`
- `--daemon-socket <path>`
- `--no-daemon`
- `--no-ui`
- `--force-non-tty`
- `--bash-login-injection` (opt-in)
- `--debug`

Key environment variables:

- `CLAI_WRAP=1`
- `CLAI_DEBUG=1`
- `CLAI_NO_COLOR=1`
- `CLAI_HOTKEY`
- `CLAI_SOCKET`
- `CLAI_ECHO_GAP_MS`
- `CLAI_PTY_DISABLE=1` (session escape hatch to skip auto-wrap)

---

## 11. Migration and Coexistence Expectations

- Hook mode and wrapper mode are mutually exclusive runtime modes.
- Wrapper mode sets `CLAI_WRAP=1` so legacy hook behavior can disable itself.
- Multiple wrapper sessions can share one daemon endpoint.

---

## 12. Quality and Validation Model

Validation categories used by project quality gates:

1. Unit: parser/state/policy correctness.
2. Integration: PTY/daemon/storage boundary behavior.
3. Expect: scripted real-shell interaction behavior.
4. Docker: distro portability for interactive shell tests.
5. End-to-End: complete user workflow validation.

Gating intent:

- PR lanes prioritize Unit + Integration + Expect smoke.
- Nightly/release lanes include Docker and full End-to-End coverage.
- Security checks include `cargo audit` in CI, pre-commit, and `make dev`.

---

## 13. User-Visible Known Limitations

- Current editable shell buffer introspection is best-effort without deeper shell-native integration.
- SSH uses local UI behavior; remote history/completion requires future remote integration.
- No composited overlay mode.
- tmux/screen environments may add latency.
- cmd has reduced OSC133-dependent behavior.

---

## 14. Troubleshooting Expectations

If terminal state becomes inconsistent after interruption:

```bash
stty sane
reset
```

Common symptoms and expectations:

- No echo: terminal mode was not restored by process shutdown path.
- Stuck alt-screen: session ended during UI mode; reset clears state.
- Missing assistant comments: daemon unavailable, fallback active, or prompt markers unavailable.
- Hotkey ineffective: non-TTY input, terminal mapping differences, or chord configuration mismatch.
