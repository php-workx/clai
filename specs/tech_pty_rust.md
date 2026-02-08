# clai-wrap (Rust) — Technical Specification
Version: 0.1 (Draft for rigorous review)
Scope: PTY wrapper enabling instant, hotkey-triggered history/autocomplete UI without composited overlays.
Non-scope (explicit): tmux integration, composited/background overlays, prompt-anchored popups, remote-agent on SSH hosts.

---

## 1. Goals and Non-Goals

### 1.1 Goals
1. **Instant** (perceived <100ms) display of history/autocomplete options via hotkey while the user is in:
	- local shell prompt
	- full-screen apps (vim, less, top)
	- remote SSH session running inside the wrapper
2. **Cross-platform** support:
	- Linux/macOS: POSIX PTY
	- Windows 10/11: ConPTY via cross-platform PTY abstraction
3. **Robust terminal behavior**:
	- Raw mode correctly managed
	- Terminal resize propagated to child PTY reliably
	- Clean teardown/restoration on exit/crash signals
4. **Simple insertion mechanism**:
	- Selected entry is inserted into the active session by sending bytes to the PTY
	- Prefer bracketed paste sequences when available; fallback to “type” bytes
5. **No screen compositing required**:
	- Popup uses alt-screen takeover for stability and simplicity
	- While popup is open, child output is buffered (lossless up to cap) and flushed afterward

### 1.2 Non-Goals
- True “transparent overlay” with visible shell background behind UI
- Full terminal emulation / composited screen model
- Shell prompt buffer integration (readline/ZLE widget insertion) as primary mechanism
- Running remote helper on SSH targets to access remote history/fs completions
- tmux integration (explicitly postponed)

---

## 2. Product Overview

`clai-wrap` is a Rust binary that:
- launches the user’s login shell inside a PTY
- owns the real terminal (stdin/stdout)
- forwards input/output between real terminal and PTY
- intercepts a configurable hotkey chord
- shows an interactive picker UI (history/autocomplete) instantly using alt-screen
- inserts selection into the PTY session
- resumes passthrough and flushes buffered output

It is designed to be safe under strict review:
- pinned toolchain
- minimal and justified dependencies
- deterministic formatting/linting
- extensive test plan including integration and OS-specific cases

---

## 3. High-Level Architecture

### 3.1 Components
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

### 3.2 State Machine
- **Passthrough**
	- stdin → PTY
	- PTY → stdout
- **PickerOpen**
	- stdin captured by UI only
	- PTY output buffered (not written)
	- on selection:
		- inject bytes into PTY
		- close UI
		- flush buffered PTY output
	- return to Passthrough

### 3.3 Concurrency Model
- Dedicated threads:
	- Thread A: read stdin, hotkey detection, forward bytes
	- Thread B: read PTY output, write/ buffer depending on state
	- Main thread: signal handling + UI loop orchestration (or UI on main, depending on UI crate requirements)
- Shared state via `Arc<AtomicBool>` + bounded channels + mutex-protected buffer:
	- `picker_open: AtomicBool`
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

### 4.3 UI Library
Option A (preferred): `ratatui`
- Mature Rust TUI
- Fast redraw
- Works cleanly in alt-screen

Option B: Keep picker as separate binary initially
- `clai-wrap` runs `clai-picker` as a subprocess in alt-screen
- Still “instant” only if warm-start is handled (not recommended for strict “instant” goal unless preloaded)

This spec assumes **Option A** (in-process UI) to meet “instant” requirement.

### 4.4 CLI / Config / Logging
- CLI: `clap` (derive)
- Logging: `tracing` + `tracing-subscriber`
- Errors: `anyhow` (binary) + `thiserror` (library modules)

### 4.5 History / Autocomplete Data
MVP: local-only
- History source:
	- `clai`-managed history file (recommended) OR
	- import from shell history file (best-effort, not relied upon for correctness)
- Autocomplete candidates:
	- from history + simple prefix matching on currently typed buffer (best-effort; see limitation)
	- optional plugin/provider interface for external sources later

Note: Without shell integration, “current typed buffer” is hard to know precisely during interactive editing. MVP may support:
- “search history” picker (fzf-like)
- “recent commands” picker
- optional “AI suggestions” seeded by last N commands and environment metadata

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
	- explicit rationale
	- security posture (maintenance, popularity, license)
	- no redundant dependencies overlapping functionality
- Prefer minimal features flags.

### 5.4 Code Requirements
- No `unsafe` in MVP unless justified; if used:
	- isolated in module
	- extensive comments and tests
- Explicit handling of terminal restoration on every exit path:
	- normal exit
	- Ctrl-C / SIGINT
	- SIGTERM
	- child exit
	- UI panic (must be caught; terminal restored)
- No silent fallbacks:
	- log when bracketed paste not available
	- log when output buffer cap reached and truncation begins
- Deterministic behavior:
	- no non-deterministic UI ordering for candidates unless explicitly desired

---

## 6. Detailed Functional Requirements

### 6.1 Launching the Shell
- Determine shell path:
	- Unix: `$SHELL` else fallback to `/bin/bash` (configurable)
	- Windows: configurable default (PowerShell, cmd, or Git Bash), but primary target is PowerShell
- Launch mode:
	- login shell (`-l`) when supported/configured
- Environment variables:
	- Pass-through parent environment
	- Set `CLAI_WRAP=1` to allow optional shell scripts to detect wrapper

### 6.2 Raw Mode and Terminal Ownership
- On wrapper start:
	- set stdin to raw mode
	- ensure stdout is a terminal; if not, exit with error (unless explicit `--force-non-tty` mode is added later)
- On wrapper exit:
	- always restore terminal to previous mode
	- always disable alt-screen if active
	- always show cursor and reset styles

### 6.3 Hotkey Detection
- Hotkey must be:
	- configurable
	- robust across terminals
	- avoid collisions with common shell/app keys
- Recommended default: **two-key chord**
	- `Ctrl-\` then `h` (history)
	- `Ctrl-\` then `c` (completions)
- Rules:
	- chord timeout: 500ms (configurable)
	- if timeout expires, forward bytes to PTY unmodified

### 6.4 Picker UI Behavior
- Must open “instantly”:
	- no spawning external processes
	- data preloaded into memory at startup or updated incrementally
- UI:
	- incremental search
	- arrow navigation
	- Enter selects
	- Esc cancels
- On open:
	- switch to alt-screen
	- hide cursor
- On close:
	- restore previous screen buffer
	- show cursor
	- return to passthrough

### 6.5 Output Buffering While UI Open
- While picker open:
	- Do not write PTY output to stdout
	- Append PTY output to an in-memory ring buffer
- Buffer size cap:
	- default 2 MiB (configurable)
	- when cap exceeded: truncate oldest data first (ring buffer)
	- log a warning once per open session
- On close:
	- flush buffered bytes to stdout in correct order
	- resume live PTY->stdout streaming

### 6.6 Inserting Selection into Session
Primary mechanism:
- Send selection to PTY as if typed/pasted.
  Preferred:
- Bracketed paste sequence:
	- `\x1b[200~` + content + `\x1b[201~`
	  Fallback:
- Write raw bytes

Post-insert behavior:
- Optionally append newline if user selected “execute now” mode; default is insert without newline.
- Configurable modes:
	- Insert only
	- Insert + execute

### 6.7 Resize Handling
- Listen for resize events:
	- Unix: SIGWINCH
	- Windows: console buffer resize events via PTY lib
- On resize:
	- obtain current terminal size (cols, rows)
	- propagate to PTY
	- if UI open: also inform UI layout and re-render

### 6.8 Child Lifecycle and Signals
- If child shell exits:
	- wrapper exits with same exit code
	- terminal restored
- If wrapper receives SIGINT/SIGTERM:
	- forward to child where meaningful
	- shutdown cleanly with restoration

---

## 7. Security and Privacy Requirements
- No network access in MVP unless explicitly added.
- No command content exfiltration.
- History file handling:
	- stored locally
	- permissions: user-only (0600 on Unix)
- Logging:
	- default: minimal
	- never log full command contents unless explicitly configured (`--debug`), and even then redact secrets patterns if feasible.

---

## 8. Test Strategy (Extensive)

### 8.1 Test Layers
1. **Unit Tests**
	- ring buffer behavior
	- hotkey chord parser and timeouts
	- selection injection encoding (bracketed paste wrapping)
2. **Integration Tests (PTY-level)**
	- spawn a predictable child (test shell / echo server)
	- verify passthrough and buffering behavior
3. **End-to-End (Manual + Automated where feasible)**
	- run wrapper with real shell
	- open picker, select, insert, execute
	- run ssh inside wrapper and verify hotkey interception

### 8.2 Unit Test Cases

#### 8.2.1 Hotkey Chord Parser
- Detect chord: `Ctrl-\` then `h` within timeout => triggers picker
- Timeout: `Ctrl-\` then wait > timeout then `h` => both forwarded, no trigger
- Cancellation: `Ctrl-\` then `Esc` => forwards appropriately, no trigger
- Overlapping sequences: rapid inputs do not drop bytes

#### 8.2.2 Byte Ring Buffer
- Append under cap => preserves full contents
- Append exceeding cap => oldest bytes dropped; newest preserved
- Multiple wraps => ordering correct
- Warning flag toggles once per “picker open” session

#### 8.2.3 Bracketed Paste Encoder
- Wrap content correctly
- Handles UTF-8 correctly (no re-encoding corruption)
- Optionally strips NUL bytes or rejects them (define behavior; recommended reject)

### 8.3 Integration Test Cases (Unix + Windows variants)

#### 8.3.1 Passthrough Smoke
- Child prints “hello”
- Verify stdout receives “hello”
- Child echoes typed bytes
- Verify typed bytes appear back

#### 8.3.2 Buffer While Picker Open
- Open picker (set state)
- Child emits bytes continuously
- Verify stdout does not receive during open
- Close picker
- Verify buffered bytes flushed in order and then live streaming resumes

#### 8.3.3 Resize Propagation
- Simulate resize event
- Verify PTY receives updated size (via PTY query if available or observed formatting change in child)
- With UI open: verify UI receives new dimensions and renders without panic

#### 8.3.4 Selection Injection
- Select “echo test”
- Verify child receives characters
- With “execute mode”: verify newline appended and command executed (child prints expected output)

### 8.4 End-to-End Test Cases (Manual + scripted where feasible)

#### 8.4.1 Full-screen Program Interop
- Launch vim inside wrapper
- Trigger hotkey chord
- Picker opens and closes cleanly
- Vim continues functioning; no terminal corruption (cursor visible, input ok)

#### 8.4.2 SSH Session
- From inside wrapper: `ssh <host>`
- At remote shell prompt, trigger hotkey chord
- Picker opens locally (expected)
- Select command; verify it is sent into remote session input (as characters/paste)
- Confirm remote session remains stable post-close

#### 8.4.3 High-output Stress
- Run `yes` or equivalent to generate output
- Trigger picker while output streams
- Ensure wrapper remains responsive
- Close picker; output resumes; buffer truncation warning logged if cap exceeded

#### 8.4.4 Termination Robustness
- While picker open, send SIGINT to wrapper
- Verify terminal restored (echo on, cursor visible, no stuck alt-screen)
- Same for SIGTERM

### 8.5 Windows-Specific Test Cases
- Launch PowerShell as child in ConPTY
- Verify passthrough typing and output
- Verify hotkey chord works
- Verify picker open/close restores console state
- Verify resize events don’t deadlock and UI redraws

### 8.6 Performance / Latency Tests
- Measure time from hotkey chord completion to first UI frame:
	- target p95 < 100ms on typical dev laptop
- Ensure no allocations proportional to terminal output rate in steady-state:
	- PTY read loop uses fixed buffers
	- buffer ring uses bounded memory

---

## 9. CI / Automation Requirements

### 9.1 CI Platforms
- Linux (Ubuntu latest LTS)
- macOS latest stable runner
- Windows latest stable runner

### 9.2 CI Jobs
- `fmt`: `cargo fmt --check`
- `clippy`: `cargo clippy --all-targets --all-features -- -D warnings`
- `test`: `cargo test --all-targets --all-features`
- `build-release`: `cargo build --release`
- optional: `audit` (security scanning) if allowed:
	- `cargo audit` (requires policy decision)

### 9.3 Pre-commit Hooks (Required)
- rustfmt
- clippy
- unit tests (fast subset) or `cargo test` if acceptable

---

## 10. Configuration

### 10.1 CLI Options (Initial)
- `clai-wrap` (default runs wrapper)
	- `--shell <path>`
	- `--login-shell` (bool, default true when supported)
	- `--hotkey <chord>` (e.g., `ctrl-\ h`)
	- `--buffer-cap <bytes>` (default 2097152)
	- `--execute-on-select` (bool)
	- `--history-file <path>`
	- `--debug` (verbose logs)

### 10.2 Config File (Optional Later)
- `~/.config/clai/config.toml` (Unix)
- `%APPDATA%\clai\config.toml` (Windows)

---

## 11. Implementation Milestones (Reviewable Deliverables)

### Milestone 1 — PTY Wrapper MVP (no UI)
Acceptance:
- Launch shell in PTY
- Raw mode on/off correct
- Passthrough works
- Resize propagation works
- Clean restore on exit

### Milestone 2 — Hotkey + Alt-screen Picker Skeleton
Acceptance:
- Hotkey chord opens picker UI
- Output buffering while UI open
- Close returns to shell without corruption

### Milestone 3 — History-backed Picker + Injection
Acceptance:
- History loaded
- Selection injected into PTY
- Execute-on-select optional
- Works inside ssh session (local UI, remote insertion via paste)

### Milestone 4 — Cross-platform Validation
Acceptance:
- Windows ConPTY passes smoke tests
- macOS/Linux pass integration tests
- CI green across OSes

---

## 12. Known Limitations (Documented Up Front)
- Without shell integration, wrapper cannot reliably read the “current editable command buffer” (readline/ZLE).
	- Therefore “autocomplete based on current partially-typed command” is best-effort or out of scope for MVP.
- Inside ssh, the UI is local; remote history/completions require remote integration (future work).
- No composited background; popup replaces view (alt-screen).

---

## 13. Review Checklist (What reviewers should verify)
- Terminal restoration guaranteed on all exit paths
- No deadlocks between PTY read thread and UI open/close transitions
- Memory bounded (buffer cap enforced, no unbounded queues)
- Windows behavior validated (ConPTY path tested)
- Hotkey detection does not eat arbitrary input bytes
- CI includes all OSes and required checks
- Dependency list minimal and justified

---
