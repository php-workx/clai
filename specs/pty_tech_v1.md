# clai-wrap PTY Technical Specification v1 (Combined)

**Version:** 1.0 (Combined Canonical Spec)
**Canonical File:** `/Users/runger/.claude-worktrees/clai/hungry-swartz/specs/pty_tech_v1.md`
**Superseded Source Inputs:**
- `/Users/runger/.claude-worktrees/clai/hungry-swartz/specs/tech_pty_rust.md`
- `/Users/runger/.claude-worktrees/clai/hungry-swartz/specs/tech_pty_design.md`

**Conflict Policy:** Rust is the default baseline for CORE. Explicit v1 decisions in this document override that baseline where noted.

**Tag Schema Used In This Document:**
- `Source`: `RUST`, `DESIGN`, `BOTH`
- `Profile`: `CORE`, `EXTENDED`
- `Priority`: `Normative`, `Variant`

## 1. Title, Version, Scope, Non-scope

### 1.1 Scope
`Source=BOTH | Profile=CORE | Priority=Normative`
- PTY wrapper enabling instant, hotkey-triggered history/autocomplete UI.
- High-fidelity terminal wrapping with crash-resistant behavior and terminal restoration.
- Cross-platform target: macOS/Linux via POSIX PTY; Windows 10/11 via ConPTY.
- Default-on daemon-integrated intelligence path with deterministic standalone fallback.

### 1.2 Explicit Non-scope
`Source=BOTH | Profile=CORE | Priority=Normative`
- True transparent composited overlays over existing terminal contents.
- Full terminal emulation/composited screen model.
- Shell-native prompt buffer insertion as primary path (readline/ZLE/widget integration).
- Remote helper/agent running on SSH hosts for remote history/completion.
- tmux integration (postponed).

## 2. Goals and Non-Goals

### 2.1 Goals
`Source=BOTH | Profile=CORE | Priority=Normative`
1. Instant UI open (perceived <100ms) via hotkey from prompt, full-screen TUI apps, and SSH sessions.
2. Robust terminal ownership and restoration on normal and abnormal exits.
3. Correct resize propagation to child PTY across platforms.
4. Selection injection into active session, preferring bracketed paste when enabled.
5. Bounded buffering while picker is open; ordered flush on close.
6. Cross-shell support: bash/zsh/fish primary; PowerShell/cmd on Windows with graceful degradation.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
7. Privacy-first capture model with two-gate safety system.
8. Daemon connectivity that degrades safely (standalone mode) without breaking shell session.
9. AI suggestions rendered as comment lines at prompt boundaries.

### 2.2 Non-Goals
`Source=BOTH | Profile=CORE | Priority=Normative`
- No hidden background compositing architecture in v1.
- No remote data-plane integration in SSH targets.
- No tmux-specific behavior guarantees in v1.

## 3. Product Overview

`Source=BOTH | Profile=CORE | Priority=Normative`
`clai-wrap` is a Rust binary that:
- launches the user shell inside a PTY,
- owns real terminal stdin/stdout,
- forwards input/output between terminal and PTY,
- intercepts configurable hotkey chords,
- opens in-process picker UI in alt-screen,
- inserts selected entry into PTY session,
- resumes passthrough and flushes buffered output.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
When daemon path is active, wrapper also:
- forwards command lifecycle and output chunks to daemon,
- receives asynchronous suggestion notifications,
- renders prompt-safe assistant comments.

`Source=BOTH | Profile=CORE | Priority=Normative`
Quality expectations:
- pinned toolchains,
- deterministic formatting and lint gates,
- minimal justified dependencies,
- platform test coverage and explicit review checklists.

## 4. System Architecture

### 4.1 Process Split
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- `clai-wrap` (Rust): systems-only PTY host, IO router, parser state, picker rendering, safe fallback behavior.
- `clai-daemon` (Go): output sanitization, persistence, AI provider calls, suggestion generation.
- Wrapper MUST continue shell operation when daemon unavailable.

### 4.2 Core Components
`Source=BOTH | Profile=CORE | Priority=Normative`
1. PTY Host: create PTY, spawn child, propagate resize.
2. Terminal Controller: set/restore raw mode and visual state.
3. Input Router: read stdin, detect hotkey, forward non-hotkey bytes.
4. Output Router: PTY->stdout live path, PTY->buffer path while picker open.
5. Picker UI: in-process alt-screen interactive selector.
6. Selection Injector: write selected payload to PTY.
7. Daemon Connector (default-on runtime mode with opt-out): IPC request/response and notification intake.

### 4.3 Runtime States
`Source=BOTH | Profile=CORE | Priority=Normative`
- `Passthrough`: stdin->PTY and PTY->stdout.
- `PickerOpen`: stdin captured by UI; PTY output buffered.
- On picker close: flush buffered PTY output in order; return to `Passthrough`.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- `Standalone`: daemon unavailable; wrapper remains functional with reduced intelligence features.

### 4.4 OSC 133 Tracking State
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Parser MUST tolerate escape sequences split across read boundaries.

Accepted prompt markers:
- `\x1b]133;A\x07` => PROMPT
- `\x1b]133;B\x07` => INPUT
- `\x1b]133;C\x07` => OUTPUT
- `\x1b]133;D;{code}\x07` => FINISHED

Terminator support:
- BEL (`\x07`) and ST (`\x1b\\`) MUST both be accepted.

### 4.5 Concurrency Model
`Source=BOTH | Profile=CORE | Priority=Normative`
- Thread A: stdin read, chord parsing, PTY write.
- Thread B: PTY read, stdout write or buffer append depending on state.
- Main thread: signal handling and UI orchestration.

Shared primitives:
- `picker_open: AtomicBool`
- bounded channels for events
- buffer object for picker output
- packed terminal size atomics may be used.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- PTY read hot path MUST not block on UI path.
- Prefer lock-free SPSC ring for picker buffer producer/consumer to avoid deadlock.

### 4.6 Daemon IPC Protocol
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Transport contract:
- JSON-RPC 2.0 over Unix domain socket (Unix) / named pipe (Windows)
- UTF-8 JSON, newline-delimited framing
- max message size 1 MiB

Request envelope:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "command.start|command.end|output.chunk|ping",
  "params": {}
}
```

Response envelope:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

Notification envelope:
```json
{
  "jsonrpc": "2.0",
  "method": "suggestion.available",
  "params": {
    "command_id": "...",
    "suggestion": "..."
  }
}
```

Methods:
- `ping` => `{ pong: true }`
- `command.start` => `{session_id, command_id, timestamp}`
- `command.end` => `{command_id, exit_code, timestamp}`
- `output.chunk` => `{command_id, data_base64, is_stderr}`
- `suggestion.available` (daemon->wrapper notification)

Error codes:
- `-32700` parse error
- `-32600` invalid request
- `-32601` method not found
- `-32602` invalid params
- `-32603` internal error
- `-32000` daemon busy
- `-32001` command not found

Compatibility:
- unknown fields MUST be ignored in both directions,
- unknown methods return `-32601`,
- protocol version mismatch MUST cause wrapper standalone fallback.

### 4.7 Daemon Availability and Standalone Behavior
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Daemon connection is default-on at startup unless explicitly disabled via `--no-daemon`.
- Ping timeout: 500ms.
- On startup daemon timeout/failure: run standalone mode.
- Mid-session daemon failure: one reconnect attempt with 500ms timeout, then standalone mode.
- Stale socket handling on `ECONNREFUSED`:
  1. stat socket owner,
  2. unlink/retry once only if owned by current user,
  3. if different owner, do not unlink; fallback standalone.

Standalone feature matrix:
- PTY passthrough: enabled
- hotkey detection: enabled
- picker UI: enabled (history-only local path)
- output capture to daemon: disabled
- AI suggestions: disabled
- warning: one-time stderr notice

## 5. Technology Choices

### 5.1 Language and Runtime
`Source=BOTH | Profile=CORE | Priority=Normative`
- Rust stable with pinned toolchain for wrapper.
- Go daemon retained for intelligence/storage path.
- Wrapper repository MUST include `rust-toolchain.toml` with an exact stable version pin.
- CI and local quality gates MUST run with the pinned toolchain version.

### 5.2 PTY Abstraction
`Source=BOTH | Profile=CORE | Priority=Normative`
- Use `portable-pty` for Unix PTY and Windows ConPTY.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
ConPTY fallback policy:
- If ConPTY unavailable (< Windows 10 1809 / build 17763), exit clearly.
- Do not fall back to raw console behavior.
- Resize failure should warn and continue where possible.

### 5.3 UI Library
`Source=BOTH | Profile=CORE | Priority=Normative`
- Preferred: in-process `ratatui` picker.
- External picker binary is acceptable only as non-default/transition path due to latency risks.

### 5.4 CLI, Logging, Errors
`Source=BOTH | Profile=CORE | Priority=Normative`
- CLI via `clap`.
- Logging via `tracing` + `tracing-subscriber`.
- Error handling via `anyhow`/`thiserror` split.
- `unsafe` is disallowed in v1 unless explicitly justified; any approved usage MUST be isolated and covered by focused tests.
- Terminal restoration requirements apply to panic/unwind paths as well as normal exits.
- Silent fallback behavior is disallowed for safety-relevant paths; fallback/degrade events MUST emit warnings.
- Candidate ordering MUST be deterministic unless an explicit non-deterministic mode is selected.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Warning log spam suppression window: 60s for repeated identical warnings.

### 5.5 Dependency Policy
`Source=RUST | Profile=CORE | Priority=Normative`
- New dependencies MUST include explicit rationale, maintenance/security/license review, and non-redundancy checks.
- Dependency feature flags SHOULD be minimized to required capability only.

### 5.6 Data Sources and Parsing
`Source=BOTH | Profile=CORE | Priority=Normative`
- Local history source in MVP (managed file or best-effort shell import).
- Prefix matching and recent command modes.

`Source=RUST | Profile=EXTENDED | Priority=Normative`
- Daemon-side suggestion providers MAY be seeded with last-N command history and environment metadata after privacy gates are applied.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- ANSI/OSC parser via `vte` state machine.
- For parser input, use UTF-8 lossy conversion from raw bytes where required.

## 6. Detailed Functional Requirements

### 6.1 Shell Launching
`Source=BOTH | Profile=CORE | Priority=Normative`
- Unix shell path from `$SHELL` else fallback `/bin/bash` (configurable).
- Windows default configurable (PowerShell primary).
- Login-shell behavior configurable; enable where supported.
- `--login-shell` default is true when supported by the selected platform/shell.
- Pass through parent environment and set `CLAI_WRAP=1`.

### 6.2 Raw Mode and Terminal Ownership
`Source=BOTH | Profile=CORE | Priority=Normative`
- Startup: capture current terminal attrs, enter raw mode.
- Exit: restore attrs, disable alt-screen, show cursor, reset styles.
- Terminal restoration MUST run on normal and abnormal exits.

Stream behavior matrix:
`Source=DESIGN | Profile=CORE | Priority=Normative`
- stdin non-TTY: disable hotkey, keep passthrough where possible.
- stdout non-TTY: disable picker UI, keep passthrough/capture paths as allowed.
- all non-TTY: error unless `--force-non-tty`; with force flag use pure passthrough.

Historical strict baseline (rejected for v1 default):
`Source=RUST | Profile=EXTENDED | Priority=Variant`
- Rust draft behavior exited when stdout was non-TTY and no explicit force mode.

### 6.3 Signal Proxying
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Unix handling:
- `SIGWINCH`: debounce trailing-edge 50ms, apply latest size.
- `SIGCHLD`: detect child exit and terminate wrapper with mapped exit code.
- `SIGINT`/`SIGTERM`/`SIGHUP`: forward where meaningful; restore terminal before exit.
- `SIGTSTP`: if picker open, close picker; restore terminal before stop.
- `SIGCONT`: re-enter raw mode and refresh size.
- `SIGPIPE`: ignore signal; handle `EPIPE` from write path.

Windows handling:
- Use `SetConsoleCtrlHandler` for Ctrl-C/Ctrl-Break/close events.
- Resize via platform PTY events.

### 6.4 Hotkey Detection
`Source=BOTH | Profile=CORE | Priority=Normative`
- Configurable chord with timeout.
- Default recommendation: `Ctrl-\\` then `h` (history) and `Ctrl-\\` then `c` (completion).
- Timeout default: 500ms; bytes forwarded untouched on timeout.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Install temporary `SIGQUIT` ignore to prevent accidental core dump windows on Unix startup/shutdown edge cases.
- Provide alternative configurable hotkey for users needing SIGQUIT semantics.

### 6.5 Picker UI Behavior
`Source=BOTH | Profile=CORE | Priority=Normative`
- In-process startup, no external process spawn.
- Incremental search, navigation, Enter-select, Esc-cancel.
- Open: enter alt-screen and hide cursor.
- Close: leave alt-screen, show cursor, return passthrough.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- On close, re-send current terminal size to PTY to handle resize-during-picker race.
- `TERM=dumb` or unset => picker disabled, passthrough mode with warning.
- Color policy:
  - `NO_COLOR` => no colors
  - `COLORTERM=truecolor|24bit` => truecolor
  - `TERM` includes `256color` => 256-color
  - else terminfo/fallback 16-color.
- Unicode width handling SHOULD use `unicode-width` style behavior.
- `CLAI_ACCESSIBILITY=1` reserved for text-only accessibility mode.

### 6.6 Output Buffering While UI Open
`Source=BOTH | Profile=CORE | Priority=Normative`
- While picker open, PTY output is buffered and not written to stdout.
- Default picker buffer cap: 2 MiB, overwrite oldest on overflow.
- On close, flush buffered bytes in order then resume live stream.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Critical non-blocking rule:
- PTY read thread MUST keep draining PTY to avoid child-side deadlock.
- Hot path MUST NOT block waiting for UI thread.
- Overflow is acceptable; read-loop stalls are not.
- Preferred primitive: lock-free SPSC ring buffer with overwrite-oldest producer behavior.

Overflow notification behavior:
- First overflow per picker session logs warning.
- Repeated overflow warnings suppressed.
- On close, UI may show truncated indicator.

### 6.7 Selection Injection
`Source=BOTH | Profile=CORE | Priority=Normative`
- Inject selected text into PTY session.
- Modes:
  - insert-only (default)
  - insert-and-execute (append newline)

Bracketed paste policy:
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Track `\x1b[?2004h` enable and `\x1b[?2004l` disable from PTY output.
- Use bracketed paste only when currently enabled.
- Unknown state falls back to raw byte injection.
- State changes apply to future inserts only.
- NUL-byte policy: selected payloads containing `\x00` MUST be rejected (not injected), with a warning.

### 6.8 Resize Handling
`Source=BOTH | Profile=CORE | Priority=Normative`
- Unix: `SIGWINCH`, Windows: PTY resize events.
- Obtain cols/rows, propagate to PTY.
- If picker open, update layout and rerender.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Trailing-edge debounce algorithm:
1. update `latest_size` on each event,
2. reset 50ms timer,
3. apply `latest_size` when timer fires,
4. never lose final event.

### 6.9 Child Lifecycle and Exit Codes
`Source=BOTH | Profile=CORE | Priority=Normative`
- Child normal exit => wrapper exits same code.
- Child signal death => wrapper exits `128 + signal`.
- Wrapper internal fatal error => exit code 1 and stderr message.

### 6.10 Encoding
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- PTY path stores and forwards raw bytes.
- Parser path may use UTF-8 lossy conversion.
- Picker render path assumes UTF-8 display and replacement char for invalid sequences.
- History files SHOULD be UTF-8; invalid files skipped with warning.
- Startup warning once if locale does not indicate UTF-8.

## 7. Security, Privacy, and Output Capture

### 7.1 Security Baseline
`Source=BOTH | Profile=CORE | Priority=Normative`
- No implicit network exfiltration in wrapper.
- Local history storage only; secure file permissions (0600 on Unix).
- Minimal logging by default; debug mode must avoid secret leakage where feasible.

### 7.2 Two-Gate Capture Safety System
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Gate 1: Interactive denylist by detected foreground process.
- Example denylist: `ssh`, `scp`, `sftp`, `mysql`, `psql`, `passwd`, `vim`, `nano`, `htop`, `docker login`, `sudo`.
- When denylisted, capture pauses while terminal passthrough continues.

Gate 2: Echo-gap heuristic fallback.
- If input activity observed but output echo absent beyond threshold, enter secure mode.
- Secure mode retroactively scrubs recent capture window and resumes only after safe conditions.
- Adaptive threshold:
  - min default 50ms,
  - based on rolling p90 echo latency,
  - capped at 500ms,
  - override env: `CLAI_ECHO_GAP_MS`.

### 7.3 Foreground Process Detection
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Linux: `/proc` + tty process group queries.
- macOS: `libproc` or `sysctl` path.
- Windows: Tool Help process tree walk rooted at shell PID; image name query.

Failure behavior:
- If detection fails, do not hard-disable feature set.
- Use conservative fallback with echo-gap heuristic active.
- Display fallback process label (`shell`/launch shell name/`unknown`).

### 7.4 Ring Buffers
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Capture buffer (daemon-bound): fixed-size circular buffer, default 4 MiB, overwrite oldest.
- Picker display buffer: fixed-size circular buffer, default 2 MiB.

### 7.5 Capture Transfer Policy (Hop 1 vs Hop 2)
`Source=DESIGN | Profile=CORE | Priority=Normative`
- **Hop 1 (`clai-wrap` -> `clai-daemon`, local IPC):**
  - Failed command: send captured output (bounded by capture buffer policy).
  - Successful command: send only the last 20 lines of output.
  - Explicit user trigger: send captured output regardless of exit code.
- **Hop 2 (`clai-daemon` -> external provider/network egress):**
  - Failed command: MAY send after privacy gates and daemon policy checks.
  - Successful command: MUST NOT send in v1.
- Line semantics for successful-command tail:
  - Line split is newline-delimited.
  - If fewer than 20 lines exist, send all available lines.
  - If the tail starts/ends mid-line due to buffer limits, send best-effort partial line content.

## 8. Shell Integration (OSC 133 Injection)

### 8.1 Injection Strategy
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Wrapper controls shell startup so hooks load predictably.
- Source system config first.
- Source user config next.
- Inject OSC hooks after user prompt setup.

### 8.2 Shell-specific Notes
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Zsh: temporary `ZDOTDIR` wrapper files must source user `.zshenv` first, then restore `ZDOTDIR=$HOME`, then source `.zshrc`, then inject hooks.
- Bash interactive: wrapper rcfile must source `/etc/bash.bashrc`, `/etc/bashrc`, then `~/.bashrc`, then inject hooks.
- Bash login shell injection is opt-in in v1 and disabled by default.
- Fish >=3.6 may emit OSC133 natively; detect and skip injection.
- PowerShell: init script via command bootstrap.
- cmd.exe: no OSC133 support; passthrough behavior for OSC-dependent capture features.

### 8.3 Bash Login Injection Opt-In Behavior Matrix
`Source=DESIGN | Profile=CORE | Priority=Normative`
Default policy:
- Login-shell injection for bash is OFF unless explicitly enabled (`--bash-login-injection` or config equivalent).

Behavior matrix:
- Interactive non-login bash:
  - Opt-in OFF: wrapper rcfile path is used; system+user bashrc + OSC hooks are loaded.
  - Opt-in ON: same as OFF (no behavior change for this mode).
- Login bash:
  - Opt-in OFF: wrapper does not rewrite login startup flow; OSC hook reliability is best-effort only.
  - Opt-in ON: wrapper applies login-aware injection path and documents increased startup-script invasiveness.

Test matrix requirements:
- interactive non-login + opt-in OFF
- interactive non-login + opt-in ON
- login + opt-in OFF
- login + opt-in ON
- Each case validates startup files sourced, OSC marker availability, and user environment preservation.

### 8.4 Temp Directory Lifecycle
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Temp wrapper dir locations:
  - Unix: `$XDG_RUNTIME_DIR/clai/` fallback `/tmp/clai-$UID/`
  - Windows: `%TEMP%\\clai\\`
- Permissions: user-only (`0700` on Unix)
- Naming pattern: `clai-shell-{pid}`
- Startup orphan cleanup:
  - scan `clai-shell-*`,
  - if owning pid not alive, remove dir,
- log warnings on cleanup failure; continue startup.
- Normal/signal shutdown removes current temp dir.

### 8.5 OSC133 Startup Watchdog Fallback
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- If no OSC133 sequence observed within 500ms after startup, disable capture features that rely on prompt boundaries.
- Continue passthrough and picker behavior with warning.

## 9. User Experience and Comment Rendering

### 9.1 Picker Modes
`Source=BOTH | Profile=CORE | Priority=Normative`
- History search mode.
- Recent command mode.
- Optional AI-suggestion seeded mode (future-facing).

### 9.2 Assistant Comment Rendering
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Trigger: failed command with available daemon suggestion.
- Wrapper waits for prompt-safe boundary (OSC133 prompt transition) before writing comment.
- Comment prefixes:
  - bash/zsh/fish/PowerShell: `#`
  - cmd.exe: `REM `
  - fallback: `#`
- Suggestions are rendered as shell comments, not inserted into active editable input.

## 10. Storage Schema and Data Retention

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
SQLite additions for phase-2 capture:

`command_events`:
```sql
CREATE TABLE command_events (
  id INTEGER PRIMARY KEY,
  session_id TEXT NOT NULL,
  command_id TEXT NOT NULL,
  exit_code INTEGER,
  start_ts INTEGER,
  end_ts INTEGER,
  is_sensitive BOOLEAN DEFAULT 0,
  captured_bytes INTEGER
);
```

`command_output`:
```sql
CREATE TABLE command_output (
  id INTEGER PRIMARY KEY,
  command_id TEXT NOT NULL,
  stdout_blob BLOB,
  stderr_blob BLOB,
  created_at INTEGER,
  expires_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_command_output_expires ON command_output(expires_at);
```

Retention:
- data expiry pruning policy (example: 7 days) enforced by `expires_at` index and prune jobs.

## 11. Test Strategy

### 11.1 Test Layers
`Source=BOTH | Profile=CORE | Priority=Normative`
1. Unit tests for chord parser, ring buffers, paste encoder.
2. PTY integration tests for passthrough/buffering/resize/injection.
3. E2E tests for real shells, fullscreen interop, SSH path, and termination robustness.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
4. Parser tests for OSC133 split packets and BEL/ST terminators.
5. Daemon connection tests, stale socket behavior, and standalone degrade path.
6. Non-TTY matrix tests and `--force-non-tty` behavior.
7. Privacy gate tests (denylist and echo-gap).
8. Shell injection tests across bash/zsh/fish.
9. Windows-specific process detection and console event handling.

### 11.2 Required Unit Scenarios
`Source=BOTH | Profile=CORE | Priority=Normative`
- Hotkey detection success/timeouts/cancellation/no dropped bytes.
- Ring overwrite ordering and single warning per session.
- Bracketed paste encoding correctness.
- NUL-containing selection payload rejection behavior.
- Deterministic candidate ordering for stable input set.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- OSC parser state machine with split read boundaries.
- Unicode width and rendering behavior where applicable.

### 11.3 Required Integration Scenarios
`Source=BOTH | Profile=CORE | Priority=Normative`
- Passthrough smoke.
- Buffer while picker open.
- Resize propagation.
- Selection injection with optional execute-on-select.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Daemon connect/degrade/reconnect behavior.
- Stale socket ownership and cleanup guardrails.
- Non-TTY stream combinations.
- Bash login injection opt-in matrix (interactive/login x opt-in on/off), including startup-file sourcing and OSC marker verification.

### 11.4 Required E2E Scenarios
`Source=BOTH | Profile=CORE | Priority=Normative`
- Full-screen program interop (`vim`, etc).
- SSH nested session with local picker and remote insertion.
- High-output stress with no deadlock.
- Termination robustness with restored terminal state.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Interactive denylist capture pause/resume.
- Shell-specific comment syntax correctness.

### 11.5 Performance Targets
`Source=BOTH | Profile=CORE | Priority=Normative`
- Hotkey->first frame target p95 < 100ms on typical dev hardware.
- Bounded memory; no unbounded queues in IO paths.
- PTY read loop MUST use fixed-size buffers and avoid steady-state allocations proportional to output rate.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Resize debounce should cap effective resize propagations under heavy drag-resize.

## 12. CI and Automation Requirements

### 12.1 Platforms
`Source=BOTH | Profile=CORE | Priority=Normative`
- Linux (Ubuntu LTS), macOS stable, Windows stable.

### 12.2 Required Jobs
`Source=BOTH | Profile=CORE | Priority=Normative`
- `cargo fmt --check`
- `cargo clippy --all-targets --all-features -- -D warnings`
- `cargo test --all-targets --all-features`
- `cargo build --release`
- `cargo audit`

### 12.3 Pre-commit Gates
`Source=BOTH | Profile=CORE | Priority=Normative`
- rustfmt
- clippy
- fast unit subset or full test command
- `cargo audit`

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Maintain deterministic checks consistent with project make targets and CI matrix.

### 12.4 Local Gate (`make dev`)
`Source=DESIGN | Profile=CORE | Priority=Normative`
- `make dev` MUST execute `cargo audit`.
- `make dev` MUST fail non-zero when `cargo audit` reports unapproved advisories.

## 13. Configuration (CLI, env vars, config files, socket defaults)

### 13.1 CLI
`Source=BOTH | Profile=CORE | Priority=Normative`
- `--shell <path>`
- `--login-shell` (bool, default true when supported)
- `--hotkey <chord>`
- `--buffer-cap <bytes>` (default 2097152)
- `--execute-on-select`
- `--history-file <path>`
- `--debug`

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- `--daemon-socket <path>`
- `--no-daemon`
- `--no-ui`
- `--force-non-tty`
- `--bash-login-injection` (opt-in, default false)

### 13.2 Environment Variables
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- `CLAI_WRAP=1`
- `CLAI_DEBUG=1`
- `CLAI_NO_COLOR=1`
- `CLAI_HOTKEY`
- `CLAI_SOCKET`
- `CLAI_ACCESSIBILITY=1` (future)
- reserved: `CLAI_HISTORY_FILE`, `CLAI_CONFIG`
- optional echo-gap override: `CLAI_ECHO_GAP_MS`
- `CLAI_PTY_DISABLE=1` (session-level escape hatch: skip auto-wrap handoff)

### 13.3 Config File Paths
`Source=BOTH | Profile=CORE | Priority=Normative`
- Unix: `$XDG_CONFIG_HOME/clai/config.toml` default `~/.config/clai/config.toml`
- macOS optional alternative: `~/Library/Application Support/clai/config.toml`
- Windows: `%APPDATA%\\clai\\config.toml`

### 13.4 Socket Defaults
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Unix: `$XDG_RUNTIME_DIR/clai/daemon.sock` fallback `/tmp/clai-$UID/daemon.sock`
- Windows: `\\.\\pipe\\clai-daemon-{username}`
- Multiple wrappers may share one daemon endpoint.

## 14. Migration and Coexistence

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Phase-1 hook mode and phase-2 wrapper mode are mutually exclusive runtime modes.

Activation modes:
- Hook mode: shell hook setup in shell rc files.
- Wrapper mode (default): hook startup auto-execs `clai-wrap` when `pty.enabled=true`.
- Wrapper mode (manual): terminal command launches `clai-wrap` directly.

User controls:
- `clai pty on` sets `pty.enabled=true` for new sessions.
- `clai pty off` sets `pty.enabled=false` for new sessions.
- `clai pty status` reports configured state and current-session wrap status.

Conflict prevention:
- Wrapper sets `CLAI_WRAP=1` and hook path should disable itself when present.

## 15. Roadmap and Milestones

### 15.1 Core Milestones (Rust Baseline)
`Source=RUST | Profile=CORE | Priority=Normative`
- M1: PTY wrapper MVP (no UI)
- M2: hotkey + picker skeleton
- M3: history-backed picker + injection
- M4: cross-platform validation

### 15.2 Required Milestones (M5-M7)
`Source=DESIGN | Profile=CORE | Priority=Normative`
- M5: parser + shell integration (OSC133)
- M6: privacy guardrails + daemon link + stale socket handling
- M7: intelligence path + assistant comments

### 15.3 Acceptance Expansion
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Additional acceptance criteria include:
- non-TTY behavior coverage,
- color and Unicode handling,
- shell injection correctness,
- OSC parser robustness,
- denylist and echo-gap behavior,
- startup temp cleanup and troubleshooting readiness.

## 16. Explicit Assumptions

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- vte parser coverage for expected escape sequences is sufficient with pinned version and tests.
- resize debounce default (50ms) is adequate and may become configurable.
- user shell startup paths are available for injection; watchdog fallback covers failures.
- daemon socket path is writable/accessible in supported environments.
- UTF-8 lossy conversion for parser path is acceptable for rendering contexts.
- `portable-pty` remains stable for target platforms.
- ring buffer defaults may require tuning for long outputs.

Mitigation principle:
- when assumptions fail, degrade to safe passthrough/standalone behavior and surface warnings.

## 17. Known Limitations and Edge Cases

### 17.1 Known Limitations
`Source=BOTH | Profile=CORE | Priority=Normative`
- Current editable shell buffer introspection is best-effort/out of scope without deeper shell integration.
- SSH uses local UI only; remote history/completion requires future remote integration.
- No composited background overlay.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- If OSC133 injection fails, capture/comment features degrade.
- Running inside tmux/screen can increase latency.
- Nested wrappers are undefined and should warn via `CLAI_WRAP` detection.
- cmd.exe lacks OSC133 support, limiting capture semantics.

### 17.2 Edge Cases
`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
- Nested PTY/tmux may confuse OSC tracking.
- RTL rendering in picker may be inaccurate in MVP.
- very long outputs may truncate leading context by design.
- shell exit during picker MUST close picker and restore terminal safely.
- rapid shell restarts require distinct session ids.

## 18. Review Checklist

`Source=BOTH | Profile=CORE | Priority=Normative`
Core checks:
- terminal restore on all exits,
- no IO deadlocks around picker transitions,
- bounded memory,
- no input-byte loss from hotkey parser,
- platform CI coverage and dependency discipline.

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Expanded checks:
- PTY read thread never blocks,
- lock-free/non-blocking buffer path on producer hot path,
- resize trailing-edge invariant,
- JSON-RPC compatibility rules,
- daemon timeout/reconnect/standalone behavior,
- socket ownership guard before unlink,
- shell injection ordering and cleanup,
- OSC parser split-buffer + BEL/ST handling,
- bracketed paste enable/disable correctness,
- NUL-containing insertion payload rejection correctness,
- deterministic ordering for picker candidate rendering,
- privacy gates and adaptive echo-gap,
- Windows-specific signal/process/TTY handling,
- debug and troubleshooting instructions validated.

## 19. Debugging and Troubleshooting

`Source=DESIGN | Profile=EXTENDED | Priority=Normative`
Recovery commands:
```bash
stty sane
reset
```

Common issue patterns:
- no echo after abnormal exit -> restore terminal state,
- stuck alt-screen -> force leave alt-screen/reset,
- hotkey failures -> verify TTY mode and chord config,
- missing OSC markers -> verify shell injection and debug logs,
- unexpected latency in tmux -> known behavior.

Debug mode:
- `--debug` or `CLAI_DEBUG=1` enables verbose diagnostics.
- State log location: `$XDG_STATE_HOME/clai/clai-wrap.log` default `~/.local/state/clai/clai-wrap.log`.

## 20. Appendix A: Source Coverage Matrix

The following matrix maps every heading/subheading from both source specs to this canonical spec.

| Source File | Source Heading | Destination Section |
|---|---|---|
| tech_pty_rust.md | # clai-wrap (Rust) — Technical Specification | 1 |
| tech_pty_rust.md | ## 1. Goals and Non-Goals | 2 |
| tech_pty_rust.md | ### 1.1 Goals | 2 |
| tech_pty_rust.md | ### 1.2 Non-Goals | 2 |
| tech_pty_rust.md | ## 2. Product Overview | 3 |
| tech_pty_rust.md | ## 3. High-Level Architecture | 4 |
| tech_pty_rust.md | ### 3.1 Components | 4 |
| tech_pty_rust.md | ### 3.2 State Machine | 4 |
| tech_pty_rust.md | ### 3.3 Concurrency Model | 4 |
| tech_pty_rust.md | ## 4. Technology Choices | 5 |
| tech_pty_rust.md | ### 4.1 Language | 5 |
| tech_pty_rust.md | ### 4.2 PTY Abstraction (Cross-platform) | 5 |
| tech_pty_rust.md | ### 4.3 UI Library | 5 |
| tech_pty_rust.md | ### 4.4 CLI / Config / Logging | 5 |
| tech_pty_rust.md | ### 4.5 History / Autocomplete Data | 5 |
| tech_pty_rust.md | ## 5. Toolchain and Repo Standards | 5, 12 |
| tech_pty_rust.md | ### 5.1 Rust Toolchain Pinning | 5, 12 |
| tech_pty_rust.md | ### 5.2 Formatting / Lint / Build | 5, 12 |
| tech_pty_rust.md | ### 5.3 Dependency Policy | 5, 12 |
| tech_pty_rust.md | ### 5.4 Code Requirements | 5, 12 |
| tech_pty_rust.md | ## 6. Detailed Functional Requirements | 6 |
| tech_pty_rust.md | ### 6.1 Launching the Shell | 6 |
| tech_pty_rust.md | ### 6.2 Raw Mode and Terminal Ownership | 6 |
| tech_pty_rust.md | ### 6.3 Hotkey Detection | 6 |
| tech_pty_rust.md | ### 6.4 Picker UI Behavior | 6 |
| tech_pty_rust.md | ### 6.5 Output Buffering While UI Open | 6 |
| tech_pty_rust.md | ### 6.6 Inserting Selection into Session | 6 |
| tech_pty_rust.md | ### 6.7 Resize Handling | 6 |
| tech_pty_rust.md | ### 6.8 Child Lifecycle and Signals | 6 |
| tech_pty_rust.md | ## 7. Security and Privacy Requirements | 7 |
| tech_pty_rust.md | ## 8. Test Strategy (Extensive) | 11 |
| tech_pty_rust.md | ### 8.1 Test Layers | 11 |
| tech_pty_rust.md | ### 8.2 Unit Test Cases | 11 |
| tech_pty_rust.md | #### 8.2.1 Hotkey Chord Parser | 11 |
| tech_pty_rust.md | #### 8.2.2 Byte Ring Buffer | 11 |
| tech_pty_rust.md | #### 8.2.3 Bracketed Paste Encoder | 11 |
| tech_pty_rust.md | ### 8.3 Integration Test Cases (Unix + Windows variants) | 11 |
| tech_pty_rust.md | #### 8.3.1 Passthrough Smoke | 11 |
| tech_pty_rust.md | #### 8.3.2 Buffer While Picker Open | 11 |
| tech_pty_rust.md | #### 8.3.3 Resize Propagation | 11 |
| tech_pty_rust.md | #### 8.3.4 Selection Injection | 11 |
| tech_pty_rust.md | ### 8.4 End-to-End Test Cases (Manual + scripted where feasible) | 11 |
| tech_pty_rust.md | #### 8.4.1 Full-screen Program Interop | 11 |
| tech_pty_rust.md | #### 8.4.2 SSH Session | 11 |
| tech_pty_rust.md | #### 8.4.3 High-output Stress | 11 |
| tech_pty_rust.md | #### 8.4.4 Termination Robustness | 11 |
| tech_pty_rust.md | ### 8.5 Windows-Specific Test Cases | 11 |
| tech_pty_rust.md | ### 8.6 Performance / Latency Tests | 11 |
| tech_pty_rust.md | ## 9. CI / Automation Requirements | 12 |
| tech_pty_rust.md | ### 9.1 CI Platforms | 12 |
| tech_pty_rust.md | ### 9.2 CI Jobs | 12 |
| tech_pty_rust.md | ### 9.3 Pre-commit Hooks (Required) | 12 |
| tech_pty_rust.md | ## 10. Configuration | 13 |
| tech_pty_rust.md | ### 10.1 CLI Options (Initial) | 13 |
| tech_pty_rust.md | ### 10.2 Config File (Optional Later) | 13 |
| tech_pty_rust.md | ## 11. Implementation Milestones (Reviewable Deliverables) | 15 |
| tech_pty_rust.md | ### Milestone 1 — PTY Wrapper MVP (no UI) | 15 |
| tech_pty_rust.md | ### Milestone 2 — Hotkey + Alt-screen Picker Skeleton | 15 |
| tech_pty_rust.md | ### Milestone 3 — History-backed Picker + Injection | 15 |
| tech_pty_rust.md | ### Milestone 4 — Cross-platform Validation | 15 |
| tech_pty_rust.md | ## 12. Known Limitations (Documented Up Front) | 17 |
| tech_pty_rust.md | ## 13. Review Checklist (What reviewers should verify) | 18 |
| tech_pty_design.md | # clai-wrap — Technical Specification (Phase 2) | 1 |
| tech_pty_design.md | ## 1. Goals and Non-Goals | 2 |
| tech_pty_design.md | ### 1.1 Goals | 2 |
| tech_pty_design.md | ### 1.2 Non-Goals | 2 |
| tech_pty_design.md | ## 2. Product Overview | 3 |
| tech_pty_design.md | ## 3. System Architecture | 4 |
| tech_pty_design.md | ### 3.1 The Process Split | 4 |
| tech_pty_design.md | #### `clai-wrap` (The Dumb Host) | 4 |
| tech_pty_design.md | #### `clai-daemon` (The Smart Brain) | 4 |
| tech_pty_design.md | ### 3.2 The "Rescue Net" Topology | 4 |
| tech_pty_design.md | ### 3.3 Components | 4 |
| tech_pty_design.md | ### 3.4 IPC Protocol Schema | 4 |
| tech_pty_design.md | #### Protocol Version | 4 |
| tech_pty_design.md | #### Message Types | 4 |
| tech_pty_design.md | #### Method Definitions | 4 |
| tech_pty_design.md | #### Error Codes | 4 |
| tech_pty_design.md | #### Backward Compatibility | 4 |
| tech_pty_design.md | ### 3.5 State Machine | 4 |
| tech_pty_design.md | #### Wrapper States | 4 |
| tech_pty_design.md | #### OSC 133 Tracking | 4 |
| tech_pty_design.md | ### 3.5 Concurrency Model | 4 |
| tech_pty_design.md | ## 4. Technology Choices | 5 |
| tech_pty_design.md | ### 4.1 Language | 5 |
| tech_pty_design.md | ### 4.2 PTY Abstraction (Cross-platform) | 5 |
| tech_pty_design.md | ### 4.3 UI Library | 5 |
| tech_pty_design.md | ### 4.4 CLI / Config / Logging | 5 |
| tech_pty_design.md | ### 4.5 History / Autocomplete Data | 5 |
| tech_pty_design.md | ### 4.6 ANSI/OSC Parsing | 5 |
| tech_pty_design.md | ## 5. Toolchain and Repo Standards | 5, 12 |
| tech_pty_design.md | ### 5.1 Rust Toolchain Pinning | 5, 12 |
| tech_pty_design.md | ### 5.2 Formatting / Lint / Build | 5, 12 |
| tech_pty_design.md | ### 5.3 Dependency Policy | 5, 12 |
| tech_pty_design.md | ### 5.4 Code Requirements | 5, 12 |
| tech_pty_design.md | ## 6. Detailed Functional Requirements | 6 |
| tech_pty_design.md | ### 6.1 Launching the Shell | 6 |
| tech_pty_design.md | ### 6.2 Raw Mode and Terminal Ownership | 6 |
| tech_pty_design.md | ### 6.3 Signal Proxying | 6 |
| tech_pty_design.md | #### Unix Signals | 6 |
| tech_pty_design.md | #### Windows Console Events | 6 |
| tech_pty_design.md | ### 6.4 Hotkey Detection | 6 |
| tech_pty_design.md | ### 6.5 Picker UI Behavior | 6 |
| tech_pty_design.md | ### 6.6 Output Buffering While UI Open | 6 |
| tech_pty_design.md | ### 6.7 Inserting Selection into Session | 6 |
| tech_pty_design.md | ### 6.8 Resize Handling | 6 |
| tech_pty_design.md | ### 6.9 Child Lifecycle and Signals | 6 |
| tech_pty_design.md | ### 6.10 Encoding | 6 |
| tech_pty_design.md | ## 7. Privacy & Output Capture | 7 |
| tech_pty_design.md | ### 7.1 The Two-Gate Safety System | 7 |
| tech_pty_design.md | #### Gate 1: The Interactive Denylist (Deterministic) | 7 |
| tech_pty_design.md | #### Gate 2: The Echo-Gap Heuristic (Fallback) | 7 |
| tech_pty_design.md | ### 7.2 Ring Buffer Implementation | 7 |
| tech_pty_design.md | #### Output Capture Buffer (Daemon-bound) | 7 |
| tech_pty_design.md | #### Picker Display Buffer | 7 |
| tech_pty_design.md | ### 7.3 Security & Privacy Requirements | 7 |
| tech_pty_design.md | ## 8. Shell Integration (OSC 133 Injection) | 8 |
| tech_pty_design.md | ### 8.1 Injection Wrappers | 8 |
| tech_pty_design.md | #### Zsh Injection Details | 8 |
| tech_pty_design.md | # Source user's real .zshenv first | 8 |
| tech_pty_design.md | # Reset ZDOTDIR so subsequent files (.zlogin, .zprofile) load from ~ | 8 |
| tech_pty_design.md | # Inject our early hooks here (before .zshrc) | 8 |
| tech_pty_design.md | # Source user's real .zshrc | 8 |
| tech_pty_design.md | # Inject OSC 133 prompt hooks AFTER user config | 8 |
| tech_pty_design.md | # (so our hooks run after user's prompt setup) | 8 |
| tech_pty_design.md | #### Bash Injection Details | 8 |
| tech_pty_design.md | # Source system bashrc first (Debian/Ubuntu location) | 8 |
| tech_pty_design.md | # Source system bashrc (RHEL/CentOS location) | 8 |
| tech_pty_design.md | # Source user's bashrc | 8 |
| tech_pty_design.md | # Inject OSC 133 hooks AFTER user config | 8 |
| tech_pty_design.md | ### 8.2 Passthrough Fallback | 8 |
| tech_pty_design.md | ## 9. User Experience | 9 |
| tech_pty_design.md | ### 9.1 The "Assistant Comment" | 9 |
| tech_pty_design.md | # clai suggestion: git push | 9 |
| tech_pty_design.md | ### 9.2 Picker Modes | 9 |
| tech_pty_design.md | ## 10. Database Schema | 10 |
| tech_pty_design.md | ### Table: `command_events` | 10 |
| tech_pty_design.md | ### Table: `command_output` | 10 |
| tech_pty_design.md | ## 11. Test Strategy | 11 |
| tech_pty_design.md | ### 11.1 Test Layers | 11 |
| tech_pty_design.md | ### 11.2 Unit Test Cases | 11 |
| tech_pty_design.md | #### Hotkey Chord Parser | 11 |
| tech_pty_design.md | #### Byte Ring Buffer | 11 |
| tech_pty_design.md | #### Bracketed Paste Encoder | 11 |
| tech_pty_design.md | #### OSC 133 Parser | 11 |
| tech_pty_design.md | ### 11.3 Integration Test Cases (Unix + Windows variants) | 11 |
| tech_pty_design.md | #### Passthrough Smoke | 11 |
| tech_pty_design.md | #### Buffer While Picker Open | 11 |
| tech_pty_design.md | #### Resize Propagation | 11 |
| tech_pty_design.md | #### Selection Injection | 11 |
| tech_pty_design.md | #### Daemon Connection | 11 |
| tech_pty_design.md | #### Non-TTY Modes | 11 |
| tech_pty_design.md | ### 11.4 End-to-End Test Cases (Manual + scripted where feasible) | 11 |
| tech_pty_design.md | #### Full-screen Program Interop | 11 |
| tech_pty_design.md | #### SSH Session | 11 |
| tech_pty_design.md | #### High-output Stress | 11 |
| tech_pty_design.md | #### Termination Robustness | 11 |
| tech_pty_design.md | #### Interactive Denylist | 11 |
| tech_pty_design.md | #### Shell-Specific Tests | 11 |
| tech_pty_design.md | ### 11.5 Windows-Specific Test Cases | 11 |
| tech_pty_design.md | ### 11.6 Performance / Latency Tests | 11 |
| tech_pty_design.md | ## 12. CI / Automation Requirements | 12 |
| tech_pty_design.md | ### 12.1 CI Platforms | 12 |
| tech_pty_design.md | ### 12.2 CI Jobs | 12 |
| tech_pty_design.md | ### 12.3 Pre-commit Hooks (Required) | 12 |
| tech_pty_design.md | ## 13. Configuration | 13 |
| tech_pty_design.md | ### 13.1 CLI Options | 13 |
| tech_pty_design.md | ### 13.2 Environment Variables | 13 |
| tech_pty_design.md | ### 13.3 Config File | 13 |
| tech_pty_design.md | ### 13.4 Default Socket Path | 13 |
| tech_pty_design.md | ## 14. Migration & Coexistence | 14 |
| tech_pty_design.md | ### Installation | 14 |
| tech_pty_design.md | ### Activation | 14 |
| tech_pty_design.md | ### Conflict Prevention | 14 |
| tech_pty_design.md | ## 15. Roadmap & Milestones | 15 |
| tech_pty_design.md | ### Milestone 1 — PTY Wrapper MVP (no UI, no daemon) | 15 |
| tech_pty_design.md | ### Milestone 2 — Hotkey + Alt-screen Picker Skeleton | 15 |
| tech_pty_design.md | ### Milestone 3 — History-backed Picker + Injection | 15 |
| tech_pty_design.md | ### Milestone 4 — Cross-platform Validation | 15 |
| tech_pty_design.md | ### Milestone 5 — The Parser & Shell Integration | 15 |
| tech_pty_design.md | ### Milestone 6 — The Guardrails | 15 |
| tech_pty_design.md | ### Milestone 7 — Intelligence | 15 |
| tech_pty_design.md | ## 16. Explicit Assumptions | 16 |
| tech_pty_design.md | ## 17. Known Limitations | 17 |
| tech_pty_design.md | ### 16.1 Edge Cases and Handling | 17 |
| tech_pty_design.md | ## 18. Review Checklist | 18 |
| tech_pty_design.md | ## 19. Debugging & Troubleshooting | 19 |
| tech_pty_design.md | ### Terminal State Recovery | 19 |
| tech_pty_design.md | ### Common Issues | 19 |
| tech_pty_design.md | ### Debug Mode | 19 |

Coverage result: 190/190 extracted headings mapped; 0 unmapped.
## 21. Appendix B: Conflict Resolution Log

| Conflict Area | Canonical v1 Decision (CORE) | Preserved Variant / Historical Context | Resolution Rationale |
|---|---|---|---|
| Non-TTY behavior | Stream-aware degradation is default behavior | Historical strict stdout-non-TTY exit behavior from Rust draft | Stream-aware mode improves real-world mixed-tty usability while preserving safe hard fail for all-non-tty unless forced |
| Daemon mode default | Daemon connection is default-on with deterministic standalone fallback | Runtime opt-out remains via `--no-daemon` | Maximize feature availability without compromising terminal continuity |
| Protocol mismatch fallback level | Version mismatch MUST degrade to standalone | Prior draft wording used SHOULD | Safety-critical behavior requires deterministic fallback |
| Concurrency primitive wording | Non-blocking PTY hot path requirement stands | Mutex-friendly architectural description kept as historical context | Avoid producer-path stalls and PTY deadlock risk |
| Milestone scope | M1-M7 are in-scope milestones | Earlier Rust draft scoped only M1-M4 | Program scope now includes parser/guardrail/intelligence closure |
| Cargo audit enforcement | `cargo audit` required in CI, pre-commit, and `make dev` | Earlier wording treated audit as optional | Security posture is now an explicit release gate |
| Bash login injection policy | Bash login injection is opt-in and default-off, with documented behavior/test matrix | Earlier wording was ambiguous (“may require opt-in”) | Reduces startup-script breakage risk while keeping a deliberate activation path |
| Success-command transfer policy | Hop 1 sends last 20 lines for successful commands; Hop 2 sends no successful-command data in v1 | Earlier wording was ambiguous (“failed and/or explicit trigger”) | Clarifies local context retention without network egress for successful commands |
| Raw mode and SIGQUIT nuance | SIGQUIT hardening guidance retained | N/A | Prevent edge-window accidental signal-induced teardown |
| Output buffer sizes | Dual-buffer model retained (2 MiB picker, 4 MiB capture) | N/A | Separates UI responsiveness from diagnostic capture needs |
| OSC133 dependency | Watchdog fallback retained | N/A | Preserve usability when OSC integration is missing |

Conflict handling policy statement:
- Rust baseline is used by default unless this v1 spec explicitly overrides with a canonical decision.
- EXTENDED entries are normative additions when non-conflicting, and explicit variants when conflicting.
- No source detail from either document is intentionally discarded.
