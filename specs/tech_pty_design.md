# clai-wrap — Technical Specification (Phase 2)

**Version:** 2.3 (Pre-mortem Addressed)
**Scope:** PTY wrapper with hotkey-triggered UI and crash-resistant output capture
**Target:** Cross-platform (macOS/Linux via POSIX PTY, Windows via ConPTY)
**Goal:** A high-fidelity, crash-resistant PTY wrapper that safely captures output and provides instant history/autocomplete UI.

---

## 1. Goals and Non-Goals

### 1.1 Goals

1. **High-fidelity terminal wrapping**:
   - Crash-resistant PTY wrapper that safely captures output
   - Raw mode correctly managed
   - Terminal resize propagated to child PTY reliably
   - Clean teardown/restoration on exit/crash signals

2. **Instant UI** (perceived <100ms) display of history/autocomplete options via hotkey while the user is in:
   - Local shell prompt
   - Full-screen apps (vim, less, top)
   - Remote SSH session running inside the wrapper

3. **Cross-platform support**:
   - Linux/macOS: POSIX PTY
   - Windows 10/11: ConPTY via cross-platform PTY abstraction

4. **Cross-shell support**:
   - Bash, Zsh, Fish (primary targets)
   - PowerShell, cmd.exe (Windows)
   - Graceful degradation for unknown shells

5. **Privacy-first output capture**:
   - Two-gate safety system for sensitive content
   - Ring buffer for efficient memory usage
   - No capture of passwords, SSH sessions, or interactive editors

6. **Simple insertion mechanism**:
   - Selected entry inserted into active session by sending bytes to PTY
   - Prefer bracketed paste sequences when available; fallback to raw bytes

7. **AI-powered suggestions**:
   - Daemon analyzes failed commands
   - Suggestions displayed as comments after prompt

### 1.2 Non-Goals

- True "transparent overlay" with visible shell background behind UI
- Full terminal emulation / composited screen model
- Shell prompt buffer integration (readline/ZLE widget insertion) as primary mechanism
- Running remote helper on SSH targets to access remote history/fs completions
- tmux integration (explicitly postponed)

---

## 2. Product Overview

`clai-wrap` is a Rust binary that:
- Launches the user's login shell inside a PTY
- Owns the real terminal (stdin/stdout)
- Forwards input/output between real terminal and PTY
- Intercepts a configurable hotkey chord
- Shows an interactive picker UI (history/autocomplete) instantly using alt-screen
- Inserts selection into the PTY session
- Captures command output for AI analysis
- Displays AI suggestions after failed commands

It is designed to be safe under strict review:
- Pinned toolchain
- Minimal and justified dependencies
- Deterministic formatting/linting
- Extensive test plan including integration and OS-specific cases

---

## 3. System Architecture

### 3.1 The Process Split

To mitigate the "Mission Critical" risk of wrapping the user's shell, we separate **Systems Reliability** from **Intelligence Logic**.

#### `clai-wrap` (The Dumb Host)

- **Language:** Rust
- **Role:** The "Man-in-the-Middle"
- **Constraint:** Zero AI logic. Zero network calls. Minimal memory allocation.
- **Responsibility:**
  - Manages the Master/Slave PTY pair
  - Pumps I/O (Stdin <-> Master <-> Stdout)
  - Parses ANSI/OSC state
  - Buffers data in Ring Buffers
  - Forwards structured events to the Daemon
  - Renders picker UI on hotkey trigger

#### `clai-daemon` (The Smart Brain)

- **Language:** Go (Extended from Phase 1)
- **Role:** The Intelligence Engine
- **Responsibility:**
  - Receives output logs from `clai-wrap`
  - Runs sanitization (Regex)
  - Persists to SQLite
  - Calls AI Providers
  - Sends suggestions back to `clai-wrap`

### 3.2 The "Rescue Net" Topology

We cannot recover a session if the PTY Host crashes. Therefore, **the Host must never crash**.

- **Design:** `clai-wrap` is "Systems-Only." It has no dependencies on the DB or the AI.
- **Failure Mode:** If `clai-daemon` (the risky process) panics or hangs, `clai-wrap` detects the socket break. It logs a warning ("Daemon lost") but keeps the terminal window open and continues functioning as a dumb passthrough pipe.
- **Daemon Connection:**
  - **Connection Timeout:** 500ms. If daemon does not respond to `ping` within 500ms, operate in standalone mode.
  - **Reconnection:** On socket error mid-session, attempt reconnect once with 500ms timeout. If fails, continue in standalone mode.

- **Stale Socket Handling:** On connect failure with `ECONNREFUSED`:
  1. Check socket file ownership via `stat()`
  2. If owned by current user: unlink socket and retry once (with 500ms timeout)
  3. If owned by different user: log error ("socket owned by uid X, cannot unlink"), operate in standalone mode
  4. If socket doesn't exist after failed connect: proceed to standalone mode

- **Standalone Mode Definition:**

  When daemon is unavailable, wrapper operates in standalone mode with reduced functionality:

  | Feature | Standalone Behavior |
  |---------|---------------------|
  | PTY passthrough | ✅ Full functionality |
  | Hotkey detection | ✅ Full functionality |
  | Picker UI | ✅ History-only (local file) |
  | Output capture | ❌ Disabled (no daemon to receive) |
  | AI suggestions | ❌ Disabled |
  | Privacy gates | ⚠️ Denylist active, but no logging occurs |

  Standalone mode is **transparent to the user** except:
  - One-time warning logged to stderr: "Daemon unavailable, running in standalone mode"
  - AI suggestions will not appear after failed commands

### 3.3 Components

1. **PTY Host**
   - Creates PTY and spawns child shell
   - Maintains handles for reading/writing PTY
   - Propagates terminal resize to PTY

2. **Terminal Controller**
   - Sets raw mode on stdin
   - Restores terminal settings on exit (normal and abnormal)

3. **Input Router**
   - Reads from real stdin
   - Detects hotkey chord
   - Forwards other bytes to PTY

4. **Output Router**
   - Reads from PTY
   - If UI inactive: writes directly to stdout
   - If UI active: buffers output (lossless up to cap) but does not write

5. **Picker UI**
   - Runs in-process for minimal startup latency
   - Uses alt-screen buffer while active
   - Receives candidate list from local stores (MVP: local history file + in-memory) and/or external provider interface

6. **Selection Injector**
   - Writes selected text into PTY session
   - Uses bracketed paste when enabled; falls back to raw byte write

7. **Daemon Connector**
   - Unix socket connection to `clai-daemon`
   - Forwards command events and output
   - Receives AI suggestions
   - Connection timeout: 500ms (see Section 5.2)

### 3.4 IPC Protocol Schema

Communication between `clai-wrap` and `clai-daemon` uses JSON-RPC 2.0 over Unix domain sockets (or named pipes on Windows).

#### Protocol Version

| Field | Value |
|-------|-------|
| Protocol Version | `1.0` |
| Transport | Unix socket (Unix) / Named pipe (Windows) |
| Encoding | UTF-8 JSON, newline-delimited |
| Max Message Size | 1 MiB |

#### Message Types

**Request (wrapper → daemon):**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "command.start|command.end|output.chunk|ping",
  "params": { ... }
}
```

**Response (daemon → wrapper):**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": { ... }
}
```

**Notification (daemon → wrapper, no response expected):**
```json
{
  "jsonrpc": "2.0",
  "method": "suggestion.available",
  "params": { "command_id": "...", "suggestion": "..." }
}
```

#### Method Definitions

| Method | Direction | Params | Description |
|--------|-----------|--------|-------------|
| `ping` | wrap→daemon | `{}` | Health check, returns `{"pong": true}` |
| `command.start` | wrap→daemon | `{session_id, command_id, timestamp}` | Command execution started |
| `command.end` | wrap→daemon | `{command_id, exit_code, timestamp}` | Command completed |
| `output.chunk` | wrap→daemon | `{command_id, data_base64, is_stderr}` | Output data (base64 encoded) |
| `suggestion.available` | daemon→wrap | `{command_id, suggestion}` | AI suggestion ready |

#### Error Codes

| Code | Meaning |
|------|---------|
| `-32700` | Parse error (invalid JSON) |
| `-32600` | Invalid request |
| `-32601` | Method not found |
| `-32602` | Invalid params |
| `-32603` | Internal error |
| `-32000` | Daemon busy (retry with backoff) |
| `-32001` | Command not found |

#### Backward Compatibility

- Daemon MUST accept messages with unknown fields (ignore them)
- Wrapper MUST accept responses with unknown fields (ignore them)
- Protocol version mismatch: daemon responds with error `-32602`, wrapper falls back to standalone mode
- Future versions may add new methods; unknown methods return `-32601`

### 3.5 State Machine

#### Wrapper States

- **Passthrough**
  - stdin -> PTY
  - PTY -> stdout
- **PickerOpen**
  - stdin captured by UI only
  - PTY output buffered (not written)
  - On selection:
    - Inject bytes into PTY
    - Close UI
    - Flush buffered PTY output
  - Return to Passthrough

#### OSC 133 Tracking

The parser must handle escape sequences split across `read()` buffers.

> **Example:** Packet 1 ends with `\x1b]`; Packet 2 begins with `133;A\x07`.

| Sequence | State |
|----------|-------|
| `\x1b]133;A\x07` | `PROMPT` |
| `\x1b]133;B\x07` | `INPUT` |
| `\x1b]133;C\x07` | `OUTPUT` |
| `\x1b]133;D;{code}\x07` | `FINISHED` |

**OSC Terminator Handling:**
- Accept both `\x07` (BEL) and `\x1b\\` (ST - String Terminator) as valid terminators
- When injecting OSC sequences in init scripts, use `\x07` for maximum compatibility

### 3.6 Concurrency Model

- Dedicated threads:
  - Thread A: read stdin, hotkey detection, forward bytes
  - Thread B: read PTY output, write/buffer depending on state
  - Main thread: signal handling + UI loop orchestration
- Shared state via `Arc<AtomicBool>` + bounded channels + mutex-protected buffer:
  - `picker_open: AtomicBool`
  - `terminal_size: AtomicUsize` (packed cols/rows to avoid locking on read)
  - `buffer: Mutex<ByteRingBuffer>`
  - `events: crossbeam_channel` (recommended) or `std::sync::mpsc` (acceptable)

---

## 4. Technology Choices

### 4.1 Language

- Rust stable (pinned toolchain)

### 4.2 PTY Abstraction (Cross-platform)

- `portable-pty` (WezTerm ecosystem) for:
  - Unix PTYs
  - Windows ConPTY
- Rationale: provides a consistent PTY interface across platforms, reducing bespoke Windows code.

**Windows ConPTY Fallback:**

ConPTY requires Windows 10 1809+ (build 17763). Handle failures gracefully:

| Failure | Detection | Fallback |
|---------|-----------|----------|
| ConPTY unavailable | `portable-pty` returns error on PTY creation | Exit with clear error: "ConPTY not available. Requires Windows 10 1809 or later." |
| ConPTY init fails | Runtime error from `portable-pty` | Log error, exit with code 1. Do not fall back to raw console (behavior would be too different). |
| ConPTY resize fails | Error on `set_size()` | Log warning, continue with current size |

**Note:** Unlike Unix where we could theoretically fall back to non-PTY mode, Windows without ConPTY has fundamentally different console behavior. Failing cleanly is better than degraded operation.

### 4.3 UI Library

**Option A (preferred):** `ratatui`
- Mature Rust TUI
- Fast redraw
- Works cleanly in alt-screen

**Option B:** Keep picker as separate binary initially
- `clai-wrap` runs `clai-picker` as a subprocess in alt-screen
- Still "instant" only if warm-start is handled (not recommended for strict "instant" goal unless preloaded)

This spec assumes **Option A** (in-process UI) to meet "instant" requirement.

### 4.4 CLI / Config / Logging

- CLI: `clap` (derive)
- Logging: `tracing` + `tracing-subscriber`
- Errors: `anyhow` (binary) + `thiserror` (library modules)

**Logging Destinations:**
- Default: stderr (to avoid polluting stdout passthrough)
- With `--debug`: also write to `$XDG_STATE_HOME/clai/clai-wrap.log` (or `~/.local/state/clai/`)
- Errors that prevent operation: stderr + exit with non-zero code
- Warnings (daemon lost, buffer overflow): stderr once, then suppress repeats for 60 seconds

### 4.5 History / Autocomplete Data

MVP: local-only
- History source:
  - `clai`-managed history file (recommended) OR
  - Import from shell history file (best-effort, not relied upon for correctness)
- Autocomplete candidates:
  - From history + simple prefix matching on currently typed buffer (best-effort; see limitation)
  - Optional plugin/provider interface for external sources later

**Shell History Format Support:**
| Shell | Format | Support Level |
|-------|--------|---------------|
| Bash | Plain text, one per line | Full |
| Bash (timestamped) | `#timestamp\ncommand` | Full |
| Zsh | Plain or `: timestamp:0;command` | Full |
| Fish | YAML-like in `~/.local/share/fish/fish_history` | Best-effort |

Note: Without shell integration, "current typed buffer" is hard to know precisely during interactive editing. MVP may support:
- "Search history" picker (fzf-like)
- "Recent commands" picker
- Optional "AI suggestions" seeded by last N commands and environment metadata

### 4.6 ANSI/OSC Parsing

- `vte` crate for state machine parsing of escape sequences
- Rationale: Battle-tested parser used by Alacritty; handles split-packet edge cases correctly
- `unicode-width` crate for accurate column width calculations (CJK, emoji)

---

## 5. Toolchain and Repo Standards

### 5.1 Rust Toolchain Pinning

- `rust-toolchain.toml`:
  - channel: stable
  - exact version pinned (e.g., `1.78.0` or newer decided at project start)
- CI must use the pinned toolchain.

### 5.2 Formatting / Lint / Build

Required commands (must pass):
- `cargo fmt --check`
- `cargo clippy --all-targets --all-features -- -D warnings`
- `cargo test --all-targets --all-features`
- `cargo build --release`

### 5.3 Dependency Policy

- New dependencies require:
  - Explicit rationale
  - Security posture (maintenance, popularity, license)
  - No redundant dependencies overlapping functionality
- Prefer minimal features flags.

### 5.4 Code Requirements

- No `unsafe` in MVP unless justified; if used:
  - Isolated in module
  - Extensive comments and tests
- Explicit handling of terminal restoration on every exit path:
  - Normal exit
  - Ctrl-C / SIGINT
  - SIGTERM
  - SIGHUP
  - SIGTSTP / SIGCONT
  - Child exit
  - UI panic (must be caught; terminal restored)
- No silent fallbacks:
  - Log when bracketed paste not available
  - Log when output buffer cap reached and truncation begins
- Deterministic behavior:
  - No non-deterministic UI ordering for candidates unless explicitly desired

---

## 6. Detailed Functional Requirements

### 6.1 Launching the Shell

- Determine shell path:
  - Unix: `$SHELL` else fallback to `/bin/sh` (configurable; `/bin/sh` is guaranteed on all POSIX systems unlike `/bin/bash`)
  - Windows: configurable default (PowerShell, cmd, or Git Bash), but primary target is PowerShell
- Launch mode:
  - Login shell (`-l`) when supported/configured
- Environment variables:
  - Pass-through parent environment
  - Set `CLAI_WRAP=1` to allow optional shell scripts to detect wrapper

### 6.2 Raw Mode and Terminal Ownership

- On wrapper start:
  - Capture TTY attributes (`tcgetattr`)
  - Set raw mode (disable canonical mode, echo, signals)
  - Validate stream states (see below)
- On wrapper exit:
  - **Guarantee:** Restore original attributes (`Drop` trait in Rust)
  - Always disable alt-screen if active
  - Always show cursor and reset styles

**Stream Requirements:**

| Stream | Requirement | Behavior if not met |
|--------|-------------|---------------------|
| stdin | Must be TTY for full functionality | If pipe: disable hotkey detection, passthrough only |
| stdout | Must be TTY for picker UI | If pipe: disable picker UI, continue output capture |
| stderr | Used for wrapper diagnostics | Always usable; redirect won't break wrapper |

- If all three are non-TTY: exit with error unless `--force-non-tty` specified
- With `--force-non-tty`: operate as pure passthrough, no UI, no hotkey

**TTY Detection:**
- Unix: Use standard `isatty()` from libc
- Windows: Use `crossterm::tty::IsTty` or equivalent crate (raw libc `isatty` is unreliable across MSYS2/Cygwin/Native Windows environments)

### 6.3 Signal Proxying

#### Unix Signals

| Signal | Handling |
|--------|----------|
| **SIGWINCH** | Catch signal -> debounce (50ms trailing edge) -> `ioctl(TIOCGWINSZ)` (Host) -> `ioctl(TIOCSWINSZ)` (Master). If UI open: mark layout dirty, re-render on next frame. |
| **SIGCHLD** | Detect when the shell exits to close the wrapper |
| **SIGINT** | Forward to child where meaningful; shutdown cleanly with restoration |
| **SIGTERM** | Forward to child where meaningful; shutdown cleanly with restoration |
| **SIGHUP** | Clean shutdown: restore terminal, forward to child, exit |
| **SIGTSTP** | If picker open: close picker first. Restore terminal before stop. |
| **SIGCONT** | Re-enter raw mode, re-query terminal size, resume operation |
| **SIGPIPE** | Ignore (`SIG_IGN`); handle `EPIPE` from `write()` gracefully; continue operating |

#### Windows Console Events

Windows does not have POSIX signals. Use platform-specific handling:

| Event | Handling |
|-------|----------|
| **Ctrl-C** | Use `SetConsoleCtrlHandler` to intercept; forward to child process or handle gracefully |
| **Ctrl-Break** | Use `SetConsoleCtrlHandler`; typically signals immediate termination |
| **Console Resize** | Handled by `portable-pty` via console buffer events; propagate to PTY |
| **Close Button** | `CTRL_CLOSE_EVENT` via handler; restore terminal state, clean shutdown |

**Note:** SIGTSTP/SIGCONT (job control) have no Windows equivalent. Suspending console applications works differently on Windows.

### 6.4 Hotkey Detection

- Hotkey must be:
  - Configurable
  - Robust across terminals
  - Avoid collisions with common shell/app keys
- Recommended default: **two-key chord**
  - `Ctrl-\` then `h` (history)
  - `Ctrl-\` then `c` (completions)
- Rules:
  - Chord timeout: 500ms (configurable)
  - If timeout expires, forward bytes to PTY unmodified

**SIGQUIT Collision Warning (Unix):**

`Ctrl-\` generates SIGQUIT by default on Unix, which causes core dumps. The wrapper MUST handle this:

| Requirement | Implementation |
|-------------|----------------|
| Intercept SIGQUIT | Set `SIGQUIT` handler to `SIG_IGN` while wrapper is active |
| Raw mode | In raw mode, `Ctrl-\` is received as byte `0x1C`, not as signal |
| Restore on exit | Restore default SIGQUIT handler on clean exit |
| Alternative hotkey | Provide `--hotkey` option for users who need SIGQUIT (e.g., `Ctrl-]` as alternative)

**Note:** Because we're in raw mode, `Ctrl-\` doesn't generate SIGQUIT—it's received as input byte `0x1C`. However, if raw mode fails to engage or during brief windows (startup/shutdown), SIGQUIT could fire. Always install the handler.

### 6.5 Picker UI Behavior

- Must open "instantly":
  - No spawning external processes
  - Data preloaded into memory at startup or updated incrementally
- UI:
  - Incremental search
  - Arrow navigation
  - Enter selects
  - Esc cancels
- On open:
  - Switch to alt-screen
  - Hide cursor
- On close:
  - Restore previous screen buffer
  - Re-send current terminal size to PTY (handles resize-during-picker race)
  - Show cursor
  - Return to passthrough

**Color Detection:**
- Check `NO_COLOR` env var -> disable colors entirely
- Check `COLORTERM=truecolor` or `COLORTERM=24bit` -> use 24-bit color
- Check `TERM` ends with `-256color` or contains `256color` -> use 256 colors
- Query `TERM` via terminfo -> determine color depth (256, 16, or none)
- Fallback: assume 16 colors

| Priority | Check | Result |
|----------|-------|--------|
| 1 | `NO_COLOR` set | No colors |
| 2 | `COLORTERM=truecolor\|24bit` | 24-bit (16M colors) |
| 3 | `TERM` contains `256color` | 256 colors |
| 4 | terminfo `colors` capability | Use reported value |
| 5 | Fallback | 16 colors |

**Degraded Mode:**
- If `TERM=dumb` or unset: disable picker UI entirely, log warning, operate as passthrough
- If color detection fails: use monochrome/no-styling mode

**Unicode Handling:**
- Use `unicode-width` crate for accurate column calculations
- Wide characters (CJK, emoji) occupy 2 cells
- Combining characters handled via grapheme cluster iteration
- Assume UTF-8 output; do not attempt charset conversion
- If `LANG`/`LC_ALL` doesn't contain "UTF-8" or "utf8": log warning but proceed

**Accessibility (Future):**
- Support `CLAI_ACCESSIBILITY=1` env var for text-only output mode
- Screen reader compatibility out of scope for MVP

### 6.6 Output Buffering While UI Open

- While picker open:
  - Do not write PTY output to stdout
  - Append PTY output to an in-memory ring buffer
- Buffer size cap:
  - Default 2 MiB (configurable)
  - When cap exceeded: truncate oldest data first (ring buffer)
  - Log a warning once per open session
- On close:
  - Flush buffered bytes to stdout in correct order
  - Resume live PTY->stdout streaming

**CRITICAL: PTY Read Thread Must Never Block**

PTY kernel buffers are limited (typically 4KB-64KB). If the child process writes more data than the kernel buffer can hold while we've stopped reading, the child will **block on `write()`**, causing a deadlock.

| Priority | Requirement |
|----------|-------------|
| **1 (Highest)** | Always drain PTY master fd as fast as possible |
| **2** | Buffer drained data in user-space ring buffer |
| **3** | If ring buffer full: drop oldest data, log warning |
| **4 (Lowest)** | Display buffered data (only when UI closes) |

**Buffer overflow is acceptable; PTY deadlock is not.**

The PTY read thread must never:
- Wait for the ring buffer to have space
- Block on any mutex that could be held during UI rendering
- Perform any I/O other than reading from PTY and writing to ring buffer

**Required Synchronization Primitive:**

Use a **lock-free SPSC (Single-Producer Single-Consumer) ring buffer** for the picker display buffer:

| Aspect | Specification |
|--------|---------------|
| Type | Lock-free SPSC ring buffer |
| Producer | PTY read thread (only writer) |
| Consumer | Main thread (reads on picker close) |
| Overflow | Producer overwrites oldest data (never blocks) |
| Crate | `ringbuf` (recommended) or custom implementation |

**Implementation Requirements:**
- Producer (`push`): Always succeeds. If buffer full, advance read pointer and overwrite.
- Consumer (`pop`/`drain`): Only called when picker closes (single-threaded at that point).
- No `Mutex` or blocking locks on the hot path.
- `AtomicUsize` for read/write pointers is acceptable.

**Buffer Overflow Notification:**

When buffer overflow occurs (data dropped), notify the user:

| Severity | Action |
|----------|--------|
| First overflow in session | Log warning to stderr: "Output buffer overflow, some data lost" |
| Subsequent overflows | Suppress warning (avoid spam) |
| On picker close | If data was lost, show brief indicator in UI: "[...truncated...]" at start of output |

### 6.7 Inserting Selection into Session

Primary mechanism:
- Send selection to PTY as if typed/pasted.

Preferred:
- Bracketed paste sequence:
  - `\x1b[200~` + content + `\x1b[201~`

Fallback:
- Write raw bytes

**Bracketed Paste Detection:**

Not all shells/applications support bracketed paste. Sending bracketed paste sequences to an application that doesn't expect them results in garbage characters (`^[[200~`) appearing in the input.

| Requirement | Implementation |
|-------------|----------------|
| Track enablement | Monitor PTY output for `\x1b[?2004h` (Enable Bracketed Paste Mode) via vte parser |
| Track disablement | Monitor for `\x1b[?2004l` (Disable Bracketed Paste Mode) |
| Decision | Only use bracketed paste if enable sequence has been seen AND disable has not been seen since |
| Default | If unsure (never seen either), fall back to raw bytes |

**Edge Case: Mid-Session Enable:**

If shell enables bracketed paste mid-session (after we've already sent raw text), this is fine—our detection state simply updates. The decision applies only to NEW paste operations:

| Scenario | Behavior |
|----------|----------|
| Shell enables after first paste | Next paste will use bracketed mode |
| Shell disables mid-paste | Complete current paste raw, use raw for next |
| State lost (crash/restart) | Reset to "unknown", fall back to raw |

**Note:** We never retroactively modify text already sent. Detection affects future pastes only.

Post-insert behavior:
- Optionally append newline if user selected "execute now" mode; default is insert without newline.
- Configurable modes:
  - Insert only
  - Insert + execute

### 6.8 Resize Handling

- Listen for resize events:
  - Unix: SIGWINCH
  - Windows: console buffer resize events via PTY lib

**Debouncing (Trailing Edge):**

Rapid resize events (e.g., drag-resizing terminal window) can generate dozens of events per second. We must debounce, but critically we must **never drop the final event** or the child will have the wrong terminal size.

| Step | Action |
|------|--------|
| 1 | On resize event: update `latest_size` atomic variable |
| 2 | Reset/start 50ms debounce timer |
| 3 | When timer fires: read `latest_size`, propagate to PTY |
| 4 | If new event arrives before timer fires: go to step 1 (timer resets) |

**Key invariant:** The LAST resize event received is ALWAYS eventually applied. Never drop the tail.

- On resize:
  - Obtain current terminal size (cols, rows)
  - Store in `AtomicUsize` (packed) for lock-free reads
  - Propagate to PTY (after debounce timer fires)
  - If UI open: mark layout dirty, re-render on next frame

### 6.9 Child Lifecycle and Signals

- If child shell exits:
  - Wrapper exits with same exit code
  - Terminal restored
- If wrapper receives SIGINT/SIGTERM:
  - Forward to child where meaningful
  - Shutdown cleanly with restoration

**Exit Code Mapping:**
- Child exits normally: wrapper exits with same code
- Child killed by signal N: wrapper exits with 128 + N (POSIX convention)
- Wrapper's own error: exit code 1 with message to stderr

### 6.10 Encoding

- **PTY I/O:** Treated as raw bytes; no charset conversion performed by wrapper
- **Ring buffer:** Stores raw bytes; encoding is the daemon's concern
- **vte Parser Input:** Use `String::from_utf8_lossy()` before passing bytes to vte parser
- **Picker UI:** Assumes UTF-8 for rendering; invalid sequences replaced with U+FFFD
- **History file:** UTF-8 required; reject files with invalid sequences (log error, skip file)
- **Locale warning:** If `LANG`/`LC_ALL` doesn't contain "UTF-8" or "utf8", log warning once at startup

**Non-UTF-8 PTY Streams:**

Users may SSH into legacy servers that send Latin-1, Shift-JIS, or other encodings. The vte parser and Rust strings require valid UTF-8, so we must handle invalid bytes gracefully.

| Component | Handling |
|-----------|----------|
| PTY read | Store raw bytes in buffer (no conversion) |
| vte parser | Convert with `String::from_utf8_lossy()` before parsing |
| UI display | Invalid sequences become U+FFFD (replacement character) |
| Daemon output | Send raw bytes; daemon handles encoding |

**Never assume** the PTY stream is valid UTF-8. Always use lossy conversion when UTF-8 is required.

---

## 7. Privacy & Output Capture

### 7.1 The Two-Gate Safety System

We assume all output is **toxic until proven safe**.

#### Gate 1: The Interactive Denylist (Deterministic)

| Aspect | Detail |
|--------|--------|
| **Mechanism** | Track the foreground process name (platform-specific; see below) |
| **Denylist** | `ssh`, `scp`, `sftp`, `mysql`, `psql`, `passwd`, `vim`, `nano`, `htop`, `docker login`, `sudo` |
| **Action** | If denylisted, the Ring Buffer is **Paused**. Data flows to the screen, but `clai-wrap` records nothing. |

**Process Detection by Platform:**

| Platform | Method |
|----------|--------|
| **Linux** | Read `/proc/{pid}/comm` or `/proc/{pid}/cmdline` where pid = `tcgetpgrp(master_fd)` |
| **macOS** | Use `proc_name()` from `libproc`, or `sysctl` with `KERN_PROCARGS2` |
| **Windows** | Use Tool Help Library (see below) |

**Windows Process Detection Details:**

ConPTY creates a "headless" console, making foreground process detection non-trivial. `QueryFullProcessImageName()` alone is insufficient because you need a handle to the correct process first.

| Step | Action |
|------|--------|
| 1 | Get the shell process ID (child of clai-wrap) |
| 2 | Use `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` to snapshot all processes |
| 3 | Walk the process tree using `Process32First`/`Process32Next` |
| 4 | Find leaf process(es) descended from shell PID |
| 5 | Use `QueryFullProcessImageName()` on the leaf process handle |

The "foreground" process in a ConPTY context is typically the most recently spawned descendant of the shell.

**Process Detection Failure Handling:**

Process detection may fail due to permissions, race conditions, or platform quirks:

| Failure | Handling |
|---------|----------|
| `/proc/{pid}/comm` unreadable | Fall back to shell name (launch command) |
| `tcgetpgrp()` returns -1 | Assume shell is foreground process |
| Windows process walk fails | Assume PowerShell/cmd (launch command) |
| Process name is empty | Use "unknown" as process name |

**Fallback Display (in picker/logs):**

| Scenario | Display |
|----------|---------|
| Detection succeeds | Actual process name (e.g., "vim", "ssh") |
| Detection fails | Shell name from launch (e.g., "zsh", "bash") |
| Shell name unknown | "shell" |

**Privacy Gate Behavior on Detection Failure:**

If we cannot detect the foreground process:
- **Conservative approach:** Assume process MAY be sensitive
- **Action:** Enable Echo-Gap heuristic (Gate 2) as backup
- **Do NOT:** Pause capture entirely (would break too much functionality)

#### Gate 2: The Echo-Gap Heuristic (Fallback)

| Aspect | Detail |
|--------|--------|
| **Logic** | For allowed commands, monitor the Input vs. Output streams |
| **Heuristic** | If User Input > 0 bytes AND Output Echo == 0 bytes for > threshold -> Enter **Secure Mode** |
| **Action** | Retroactively scrub the last N bytes of the Ring Buffer |
| **Exit** | Resume recording only after `\n` is seen in Output AND Echo resumes |

**Adaptive Echo-Gap Timing:**

Fixed 50ms threshold fails on slow connections (SSH over satellite, high-latency VPNs). Use adaptive timing:

| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| `echo_gap_min_ms` | 50 | 10-500 | Minimum gap to trigger secure mode |
| `echo_gap_adaptive` | true | bool | Enable adaptive timing |

**Adaptive Algorithm:**
1. Track recent echo latencies (rolling window of last 10 commands)
2. Calculate p90 echo latency
3. Set threshold to `max(echo_gap_min_ms, p90_latency * 2)`
4. Cap at 500ms to avoid false negatives

**Configuration Override:**
- `CLAI_ECHO_GAP_MS=100` - Set fixed threshold (disables adaptive)
- Useful for known high-latency environments

### 7.2 Ring Buffer Implementation

Two separate ring buffers serve different purposes:

#### Output Capture Buffer (Daemon-bound)

| Aspect | Detail |
|--------|--------|
| **Storage** | 4MB Fixed-Size Circular Buffer (Stack or Pre-allocated Heap) |
| **Tail Drop** | If buffer fills, overwrite oldest data. Prioritize the end of the error message (diagnosis context). |
| **Commit Strategy** | Data stays in RAM by default. Sent to Daemon (Socket) only on Exit Code != 0 or Explicit User Trigger. |

#### Picker Display Buffer

| Aspect | Detail |
|--------|--------|
| **Storage** | 2MB Fixed-Size Circular Buffer |
| **Purpose** | Buffer PTY output while picker UI is open |
| **Tail Drop** | If buffer fills, overwrite oldest data and log warning |
| **Commit Strategy** | Flush to stdout when picker closes |

### 7.3 Security & Privacy Requirements

- No network access in MVP unless explicitly added.
- No command content exfiltration.
- History file handling:
  - Stored locally
  - Permissions: user-only (0600 on Unix)
- Logging:
  - Default: minimal
  - Never log full command contents unless explicitly configured (`--debug`), and even then redact secrets patterns if feasible.

**Environment Variable Scrubbing (Future Enhancement):**
- Optionally scan captured output for patterns matching known secret formats
- Configurable via `config.toml` with default patterns for AWS keys, GitHub tokens, etc.

---

## 8. Shell Integration (OSC 133 Injection)

We do not rely on user config. We **force** the shell to behave.

### 8.1 Injection Wrappers

`clai-wrap` spawns the shell with modified arguments to load our init scripts first.

**CRITICAL:** Shell injection must not break the user's environment. We must source system configs, then user configs, then our hooks.

| Shell | Method |
|-------|--------|
| **Zsh** | See detailed Zsh injection below |
| **Bash (interactive)** | See detailed Bash injection below |
| **Bash (login)** | Complex; may require user opt-in (see notes) |
| **Fish** | Fish ≥3.6 emits OSC 133 natively; detect version and skip. Older: `fish --init-command "source /path/to/init.fish"` |
| **PowerShell** | `-NoExit -Command ". /path/to/init.ps1"` |
| **cmd.exe** | OSC 133 not supported; operate in passthrough mode |

#### Zsh Injection Details

Setting `ZDOTDIR` changes where Zsh looks for ALL dotfiles. If not handled carefully, `.zlogin`, `.zprofile`, `.zshenv` in `~` will be missed.

**Wrapper files in temp ZDOTDIR:**

`.zshenv` (loaded first, always):
```zsh
# Source user's real .zshenv first
[[ -f ~/.zshenv ]] && source ~/.zshenv

# Reset ZDOTDIR so subsequent files (.zlogin, .zprofile) load from ~
export ZDOTDIR="$HOME"

# Inject our early hooks here (before .zshrc)
```

`.zshrc` (loaded for interactive shells):
```zsh
# Source user's real .zshrc
[[ -f ~/.zshrc ]] && source ~/.zshrc

# Inject OSC 133 prompt hooks AFTER user config
# (so our hooks run after user's prompt setup)
```

#### Bash Injection Details

`--rcfile` replaces `.bashrc` loading but does NOT load system-wide configs.

**Wrapper init file:**
```bash
# Source system bashrc first (Debian/Ubuntu location)
[[ -f /etc/bash.bashrc ]] && source /etc/bash.bashrc

# Source system bashrc (RHEL/CentOS location)
[[ -f /etc/bashrc ]] && source /etc/bashrc

# Source user's bashrc
[[ -f ~/.bashrc ]] && source ~/.bashrc

# Inject OSC 133 hooks AFTER user config
```

Launch with: `bash --rcfile /path/to/temp/init.bash`

**Note on Bash login shells:** Login shells read `~/.bash_profile` (or `~/.bash_login` or `~/.profile`), not `~/.bashrc`. Full login shell injection requires wrapping these files too, which is more invasive. Consider making this opt-in.

**Temp Directory for Shell Wrappers:**
- Unix: `$XDG_RUNTIME_DIR/clai/` (falls back to `/tmp/clai-$UID/`)
- Windows: `%TEMP%\clai\`
- Permissions: 0700 (user-only)
- Directory naming: `clai-shell-{pid}/` where `{pid}` is the wrapper's PID

**Temp Directory Cleanup:**

Orphaned temp directories may remain after crashes. Handle cleanup:

| Event | Action |
|-------|--------|
| Startup | Scan temp directory for orphans (dirs where PID no longer exists) |
| Orphan detection | If `clai-shell-{pid}/` exists but `kill(pid, 0)` returns ESRCH: delete directory |
| Cleanup scope | Only delete directories matching `clai-shell-*` pattern |
| Failure | Log warning if cleanup fails (permission denied, etc.), continue startup |
| Shutdown | Delete own temp directory in normal exit and signal handlers |

**Windows orphan detection:** Use `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, pid)`. If returns `NULL` with `ERROR_INVALID_PARAMETER`, process doesn't exist.

### 8.2 Passthrough Fallback

If `clai-wrap` does not detect an OSC 133 Sequence within 3 seconds of startup (e.g., user heavily customized prompt overrides us), it enters **Passthrough Mode**.

**Rationale:** 500ms was originally specified but is too aggressive. Common shell startup (oh-my-zsh, plugin managers, mise/nvm/rbenv init) routinely takes 700ms–2s. The 3s timeout accommodates slow startups while still detecting injection failures promptly.

**Behavior:** Disables output capture features. Logs a warning. Acts as a transparent pipe with picker UI still available.

---

## 9. User Experience

### 9.1 The "Assistant Comment"

We avoid input buffer injection (too dangerous).

**Trigger:** Command fails (Exit != 0)

**Action:**

1. Daemon analyzes output
2. Daemon sends suggestion back to `clai-wrap`
3. `clai-wrap` waits for Prompt End (OSC 133 B)
4. `clai-wrap` writes to Master using shell-appropriate comment syntax

**Shell-Specific Comment Prefix:**

| Shell | Comment Prefix |
|-------|---------------|
| bash/zsh/fish | `#` |
| PowerShell | `#` |
| cmd.exe | `REM ` |
| Other/unknown | `#` (fallback) |

**Result:** The suggestion appears as a comment on the user's new line. They can copy/paste it or ignore it.

**Example (bash/zsh):**

```
$ git psuh
git: 'psuh' is not a git command. See 'git --help'.

$
# clai suggestion: git push
```

### 9.2 Picker Modes

- **History picker**: fzf-like search through command history
- **Recent commands**: Quick access to last N commands
- **AI suggestions**: Seeded by last N commands and environment metadata (future)

---

## 10. Database Schema

Phase 2 adds strict storage for captured logs.

### Table: `pty_command_event`

> **Note:** Renamed from `command_events` to avoid collision with the V2 suggestions database `command_event` table. See `specs/storage-v1-v2-merge.md` for the unified schema migration plan.

```sql
CREATE TABLE pty_command_event (
  command_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  exit_code INTEGER,
  start_ts INTEGER,
  end_ts INTEGER,
  is_sensitive INTEGER DEFAULT 0,  -- Triggered by Denylist or Echo-Gap
  captured_bytes INTEGER DEFAULT 0
);
```

### Table: `pty_command_output`

> **Note:** Renamed from `command_output` for the same reason as above.

```sql
CREATE TABLE pty_command_output (
  id INTEGER PRIMARY KEY,
  command_id TEXT NOT NULL,
  stdout_blob BLOB,      -- Stored only if not sensitive
  stderr_blob BLOB,
  created_at INTEGER,
  expires_at INTEGER     -- Auto-prune policy (e.g., 7 days)
);

CREATE INDEX IF NOT EXISTS idx_pty_command_output_expires ON pty_command_output(expires_at);
```

---

## 11. Test Strategy

### 11.1 Test Layers

1. **Unit Tests**
   - Ring buffer behavior
   - Hotkey chord parser and timeouts
   - Selection injection encoding (bracketed paste wrapping)
   - OSC 133 parser state machine (including ST terminator)
   - Unicode width calculations

2. **Integration Tests (PTY-level)**
   - Spawn a predictable child (test shell / echo server)
   - Verify passthrough and buffering behavior

3. **End-to-End (Manual + Automated where feasible)**
   - Run wrapper with real shell
   - Open picker, select, insert, execute
   - Run ssh inside wrapper and verify hotkey interception

### 11.2 Unit Test Cases

#### Hotkey Chord Parser

- Detect chord: `Ctrl-\` then `h` within timeout => triggers picker
- Timeout: `Ctrl-\` then wait > timeout then `h` => both forwarded, no trigger
- Cancellation: `Ctrl-\` then `Esc` => forwards appropriately, no trigger
- Overlapping sequences: rapid inputs do not drop bytes

#### Byte Ring Buffer

- Append under cap => preserves full contents
- Append exceeding cap => oldest bytes dropped; newest preserved
- Multiple wraps => ordering correct
- Warning flag toggles once per "picker open" session

#### Bracketed Paste Encoder

- Wrap content correctly
- Handles UTF-8 correctly (no re-encoding corruption)
- Optionally strips NUL bytes or rejects them (define behavior; recommended reject)

#### OSC 133 Parser

- Split-packet handling (escape sequence split across reads)
- State transitions (PROMPT -> INPUT -> OUTPUT -> FINISHED)
- Malformed sequences don't crash parser
- Both BEL (`\x07`) and ST (`\x1b\\`) terminators accepted

### 11.3 Integration Test Cases (Unix + Windows variants)

#### Passthrough Smoke

- Child prints "hello"
- Verify stdout receives "hello"
- Child echoes typed bytes
- Verify typed bytes appear back

#### Buffer While Picker Open

- Open picker (set state)
- Child emits bytes continuously
- Verify stdout does not receive during open
- Close picker
- Verify buffered bytes flushed in order and then live streaming resumes

#### Resize Propagation

- Simulate resize event
- Verify PTY receives updated size (via PTY query if available or observed formatting change in child)
- Rapid resize (10 events in 100ms): verify debouncing, no crashes
- With UI open: verify UI receives new dimensions and renders without panic

#### Selection Injection

- Select "echo test"
- Verify child receives characters
- With "execute mode": verify newline appended and command executed (child prints expected output)

#### Daemon Connection

- Verify socket connection to daemon
- Test graceful degradation when daemon unavailable
- Verify output capture events sent correctly
- Verify stale socket cleanup works

#### Non-TTY Modes

- stdin is pipe, stdout is TTY: verify hotkey disabled, passthrough works
- stdout is pipe, stdin is TTY: verify picker disabled, passthrough works
- All non-TTY without `--force-non-tty`: verify clean exit with error
- All non-TTY with `--force-non-tty`: verify pure passthrough works

### 11.4 End-to-End Test Cases (Manual + scripted where feasible)

#### Full-screen Program Interop

- Launch vim inside wrapper
- Trigger hotkey chord
- Picker opens and closes cleanly
- Vim continues functioning; no terminal corruption (cursor visible, input ok)

#### SSH Session

- From inside wrapper: `ssh <host>`
- At remote shell prompt, trigger hotkey chord
- Picker opens locally (expected)
- Select command; verify it is sent into remote session input (as characters/paste)
- Confirm remote session remains stable post-close

#### High-output Stress

- Run `yes` or equivalent to generate output
- Trigger picker while output streams
- Ensure wrapper remains responsive
- Close picker; output resumes; buffer truncation warning logged if cap exceeded

#### Termination Robustness

- While picker open, send SIGINT to wrapper
- Verify terminal restored (echo on, cursor visible, no stuck alt-screen)
- Same for SIGTERM, SIGHUP
- Send SIGTSTP (Ctrl-Z): verify terminal restored, process suspended
- Send SIGCONT: verify raw mode re-entered, operation resumes

#### Interactive Denylist

- Run `ssh localhost` inside wrapper
- Verify output capture is paused
- Exit ssh; verify capture resumes

#### Shell-Specific Tests

- Test OSC 133 injection with bash, zsh, fish
- Verify Fish ≥3.6 native OSC 133 detected and injection skipped
- Verify comment syntax correct per shell

### 11.5 Windows-Specific Test Cases

- Launch PowerShell as child in ConPTY
- Verify passthrough typing and output
- Verify hotkey chord works
- Verify picker open/close restores console state
- Verify resize events don't deadlock and UI redraws
- Verify process detection works for denylist

### 11.6 Performance / Latency Tests

- Measure time from hotkey chord completion to first UI frame:
  - Target p95 < 100ms on typical dev laptop
- Ensure no allocations proportional to terminal output rate in steady-state:
  - PTY read loop uses fixed buffers
  - Buffer ring uses bounded memory
- Resize debouncing: verify no more than 20 resize propagations per second under rapid resize

---

## 12. CI / Automation Requirements

### 12.1 CI Platforms

- Linux (Ubuntu latest LTS)
- macOS latest stable runner
- Windows latest stable runner

### 12.2 CI Jobs

- `fmt`: `cargo fmt --check`
- `clippy`: `cargo clippy --all-targets --all-features -- -D warnings`
- `test`: `cargo test --all-targets --all-features`
- `build-release`: `cargo build --release`
- Optional: `audit` (security scanning) if allowed:
  - `cargo audit` (requires policy decision)

### 12.3 Pre-commit Hooks (Required)

- rustfmt
- clippy
- unit tests (fast subset) or `cargo test` if acceptable

---

## 13. Configuration

### 13.1 CLI Options

- `clai-wrap` (default runs wrapper)
  - `--shell <path>`
  - `--login-shell` (bool, default true when supported)
  - `--hotkey <chord>` (e.g., `ctrl-\ h`)
  - `--buffer-cap <bytes>` (default 2097152)
  - `--execute-on-select` (bool)
  - `--history-file <path>`
  - `--daemon-socket <path>` (Unix socket path for daemon connection)
  - `--no-daemon` (disable daemon connection, pure passthrough + picker)
  - `--no-ui` (disable picker UI entirely, still capture output)
  - `--force-non-tty` (run even without TTY, pure passthrough)
  - `--debug` (verbose logs)

### 13.2 Environment Variables

All clai environment variables use the `CLAI_` prefix:

| Variable | Purpose |
|----------|---------|
| `CLAI_WRAP=1` | Set by wrapper to signal active (read-only) |
| `CLAI_DEBUG=1` | Enable debug logging (alternative to `--debug`) |
| `CLAI_NO_COLOR=1` | Disable TUI colors |
| `CLAI_HOTKEY` | Override default hotkey chord |
| `CLAI_SOCKET` | Override daemon socket path |
| `CLAI_ACCESSIBILITY=1` | Enable accessibility mode (future) |

**Reserved for future:**
- `CLAI_HISTORY_FILE`
- `CLAI_CONFIG`

### 13.3 Config File

- Unix: `$XDG_CONFIG_HOME/clai/config.toml` (default: `~/.config/clai/config.toml`)
- macOS: Same as Unix (XDG preferred), or `~/Library/Application Support/clai/config.toml`
- Windows: `%APPDATA%\clai\config.toml`

### 13.4 Default Socket Path

- Unix: `$XDG_RUNTIME_DIR/clai/daemon.sock` (falls back to `/tmp/clai-$UID/daemon.sock`)
- Windows: Named pipe `\\.\pipe\clai-daemon-{username}`

Multiple `clai-wrap` instances share the same daemon socket; daemon handles multiple clients.

---

## 14. Migration & Coexistence

**Strategy:** clai (Phase 1) and `clai-wrap` (Phase 2) are **mutually exclusive** modes.

### Installation

The user installs the binary.

### Activation

| Mode | Setup | Result |
|------|-------|--------|
| **Mode A (Hook)** | User adds `eval "$(clai hook zsh)"` to `.zshrc` | Phase 1 behavior |
| **Mode B (Wrapper)** | User changes terminal emulator command to `/usr/local/bin/clai-wrap` | Phase 2 behavior |

### Conflict Prevention

`clai-wrap` sets an environment variable `CLAI_WRAP=1`. The Phase 1 hooks check this variable. If set, the hooks disable themselves to prevent double-logging.

---

## 15. Roadmap & Milestones

### Milestone 1 — PTY Wrapper MVP (no UI, no daemon)

Acceptance:
- [ ] Launch shell in PTY using `portable-pty`
- [ ] Raw mode on/off correct
- [ ] Passthrough works
- [ ] Resize propagation works (with debouncing)
- [ ] Clean restore on exit (including SIGHUP, SIGTSTP)
- [ ] Verify `vim`, `ssh`, and `htop` work perfectly
- [ ] Non-TTY detection and `--force-non-tty` mode

### Milestone 2 — Hotkey + Alt-screen Picker Skeleton

Acceptance:
- [ ] Hotkey chord opens picker UI
- [ ] Output buffering while UI open
- [ ] Close returns to shell without corruption
- [ ] Color detection and degraded mode
- [ ] Unicode width handling

### Milestone 3 — History-backed Picker + Injection

Acceptance:
- [ ] History loaded (bash, zsh, fish formats)
- [ ] Selection injected into PTY
- [ ] Execute-on-select optional
- [ ] Works inside ssh session (local UI, remote insertion via paste)

### Milestone 4 — Cross-platform Validation

Acceptance:
- [ ] Windows ConPTY passes smoke tests
- [ ] macOS/Linux pass integration tests
- [ ] CI green across OSes
- [ ] macOS process detection working

### Milestone 5 — The Parser & Shell Integration

Acceptance:
- [ ] Implement `vte` parser state machine
- [ ] Implement Shell Injection scripts (OSC 133) for bash, zsh, fish
- [ ] Detect Fish ≥3.6 native OSC 133
- [ ] Log "Command Start/End" events to debug stdout
- [ ] Handle both BEL and ST terminators

### Milestone 6 — The Guardrails

Acceptance:
- [ ] Implement Interactive Denylist (all platforms)
- [ ] Implement Output Capture Ring Buffer (4MB)
- [ ] Connect `clai-wrap` -> `clai-daemon` (Unix Socket)
- [ ] Stale socket cleanup

### Milestone 7 — Intelligence

Acceptance:
- [ ] Enable Output Capture logic in Daemon
- [ ] Implement "Assistant Comment" rendering (shell-specific comment syntax)

---

## 16. Explicit Assumptions

These assumptions underpin the design. If any prove false, the indicated mitigation should be applied.

| Assumption | Risk if Wrong | Mitigation |
|------------|--------------|------------|
| `vte` crate handles all common escape sequences | Exotic sequences may corrupt parser state or panic | Pin vte version; add integration tests with edge-case sequences; catch panics in vte callbacks |
| 50ms trailing-edge debounce is sufficient for resize | Aggressive resize patterns may still cause issues | Make debounce interval configurable via `CLAI_RESIZE_DEBOUNCE_MS` |
| User shell sources `~/.zshrc` / `~/.bashrc` | Non-standard setups (minimal containers, custom ZDOTDIR) break injection | Document prerequisites; detect injection failure within 3s and warn |
| Daemon socket path is always accessible | Permissions, SELinux, or sandboxing may block | Check write permission on socket directory at startup; clear error message |
| UTF-8 lossy conversion is acceptable | User may need exact bytes (binary protocols, debugging) | Add `--raw-mode` flag that disables vte parsing and shows hex dump |
| Shell will emit OSC 133 after injection | User's prompt customization may override | Detect missing OSC 133 within 3s; enter passthrough mode with warning |
| `portable-pty` handles all PTY edge cases | Platform-specific bugs may exist | Pin version; maintain integration tests on all platforms |
| Ring buffer 4MB is sufficient for error context | Very long build outputs may exceed buffer | Log warning when truncation occurs; document `--buffer-cap` option |

---

## 17. Known Limitations

- Without shell integration, wrapper cannot reliably read the "current editable command buffer" (readline/ZLE).
  - Therefore "autocomplete based on current partially-typed command" is best-effort or out of scope for MVP.
- Inside ssh, the UI is local; remote history/completions require remote integration (future work).
- No composited background; popup replaces view (alt-screen).
- If OSC 133 injection fails (user's prompt overrides), output capture features degrade gracefully but AI suggestions won't work.
- Running inside tmux/screen: Alt-screen picker works, but latency may exceed 100ms target due to tmux buffering.
- Nested `clai-wrap` instances: Undefined behavior; hotkey chord may be intercepted by outer wrapper. Detect via `CLAI_WRAP=1` and warn.
- cmd.exe on Windows: No OSC 133 support; operates in passthrough mode only.

### 17.1 Workflow Runner Interaction

When `clai w run` (the workflow runner) executes inside `clai-wrap`, both compete for terminal I/O. The workflow runner supports an `interactive: true` step field that connects `os.Stdin`, `os.Stdout`, and `os.Stderr` directly to the process — inside `clai-wrap`, these map to the PTY master, which is correct behavior.

| Scenario | Behavior |
|----------|----------|
| Non-interactive step inside wrapper | Runner captures stdout/stderr into buffers; wrapper sees no output (command runs silently) |
| Interactive step inside wrapper | Runner connects to real stdin/stdout/stderr; wrapper sees output and passes through normally |
| Interactive step needing browser auth (e.g., `assume`) | Browser opens normally; user authenticates; credentials exported to subsequent steps |

**Design constraint:** The workflow runner uses `$SHELL` (with `/bin/sh` fallback) as the default shell for steps, matching the wrapper's shell launch behavior. This ensures shell aliases (e.g., `alias assume="source assume"`) are available in workflow steps without requiring explicit `shell: zsh` configuration.

**No special coordination required:** The wrapper and runner are independent — the runner spawns its own subprocesses that inherit the terminal via the PTY. The wrapper's hotkey detection, output capture, and picker UI continue to function normally during workflow execution.

### 17.2 Edge Cases and Handling

| Edge Case | Impact | Handling |
|-----------|--------|----------|
| **Nested PTY (tmux inside clai-wrap)** | OSC 133 from inner tmux may confuse our parser | Detect `TMUX` env var; log warning; OSC 133 parsing may be unreliable |
| **256-color terminal** | Color detection may miss 256-color mode | Check `TERM` for "256color" suffix; use 256-color palette when detected |
| **Right-to-left text (Hebrew, Arabic)** | Cursor positioning in picker may be incorrect | MVP: Not supported. Log warning if RTL characters detected in input. Future: Use ICU/bidi library. |
| **Very long command (>64KB)** | Ring buffer may truncate mid-command | Log warning; capture buffer stores tail (newest data); command boundaries may be lost |
| **Shell exits during picker display** | Picker shows stale data; child is gone | Detect child exit via `SIGCHLD`; close picker immediately; display "Shell exited" message |
| **Rapid shell restart** | Multiple shells in quick succession | Use unique session_id per shell spawn; old session data isolated |

---

## 18. Review Checklist

What reviewers should verify:

**Core Functionality:**
- [ ] Terminal restoration guaranteed on all exit paths (including SIGHUP, SIGTSTP)
- [ ] No deadlocks between PTY read thread and UI open/close transitions
- [ ] Memory bounded (buffer cap enforced, no unbounded queues)
- [ ] Hotkey detection does not eat arbitrary input bytes

**PTY Read Thread (Critical):**
- [ ] PTY read thread NEVER blocks, even when ring buffer is full
- [ ] Buffer overflow drops oldest data but continues reading
- [ ] No mutex contention that could block PTY reads
- [ ] Lock-free SPSC ring buffer used (or equivalent)
- [ ] Buffer overflow indicator shown to user

**Resize Handling:**
- [ ] Resize debouncing uses trailing edge (final size always applied)
- [ ] Alt-screen exit re-sends terminal size to PTY

**IPC Protocol:**
- [ ] JSON-RPC 2.0 protocol implemented per Section 3.4
- [ ] Daemon connection timeout is 500ms
- [ ] Unknown fields ignored (forward compatibility)
- [ ] Standalone mode works when daemon unavailable

**Cross-Platform:**
- [ ] Windows behavior validated (ConPTY path tested)
- [ ] Windows ConPTY failure gives clear error message
- [ ] Windows uses `SetConsoleCtrlHandler` for signals (not POSIX signals)
- [ ] Windows process detection uses Tool Help Library
- [ ] Windows TTY detection uses crossterm or equivalent (not raw isatty)
- [ ] macOS process detection validated (libproc or sysctl)

**Shell Integration:**
- [ ] Shell injection sources system configs before user configs
- [ ] Zsh injection resets ZDOTDIR after sourcing .zshenv
- [ ] Bash injection sources /etc/bash.bashrc and /etc/bashrc
- [ ] Shell-specific injection tested (bash, zsh, fish)
- [ ] OSC 133 parser handles split packets and both terminators correctly
- [ ] Temp directory cleanup on startup (orphan detection)

**Hotkey & Signals:**
- [ ] SIGQUIT handler installed (SIG_IGN) to prevent core dumps
- [ ] Alternative hotkey configurable via `--hotkey`
- [ ] Hotkey chord timeout works correctly

**Encoding & Paste:**
- [ ] UTF-8 lossy conversion used for vte parser input
- [ ] Bracketed paste only used when child has enabled it (`\e[?2004h`)
- [ ] Unicode width calculations correct for CJK/emoji
- [ ] 256-color terminals detected and handled

**Privacy & Heuristics:**
- [ ] Echo-gap adaptive timing implemented (or fixed threshold configurable)
- [ ] Process detection failure falls back gracefully
- [ ] Privacy gates (Denylist, Echo-Gap) implemented correctly

**Security & Reliability:**
- [ ] CI includes all OSes and required checks
- [ ] Dependency list minimal and justified
- [ ] Daemon connection handles failures gracefully
- [ ] Socket ownership checked before unlink attempt
- [ ] Non-TTY modes work correctly
- [ ] Child exit during picker handled gracefully

---

## 19. Debugging & Troubleshooting

### Terminal State Recovery

If terminal state is corrupted after abnormal exit:
```bash
stty sane
reset
```

Consider adding `clai-wrap --reset-terminal` helper command that runs equivalent restoration.

### Common Issues

| Symptom | Likely Cause | Fix |
|---------|--------------|-----|
| No echo after exit | Terminal not restored | Run `stty sane` |
| Stuck in alt-screen | UI crashed | Run `tput rmcup` or `reset` |
| Hotkey not working | TTY not detected | Check `--force-non-tty` mode |
| OSC 133 not detected | Shell injection failed | Check shell init scripts, run with `--debug` |
| High latency | Running inside tmux | Expected; tmux buffers output |

### Debug Mode

Run with `--debug` or set `CLAI_DEBUG=1` to enable verbose logging to stderr and log file.

Log file location: `$XDG_STATE_HOME/clai/clai-wrap.log` (default: `~/.local/state/clai/clai-wrap.log`)
