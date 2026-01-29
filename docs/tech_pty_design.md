# clai — Technical Specification Phase 2 (v2.0 - The PTY Host)

**Scope:** Phase 2 (Invisible PTY Wrapper)
**Target:** POSIX Only (macOS/Linux). Windows ConPTY deferred to v2.1.
**Goal:** A high-fidelity, crash-resistant PTY wrapper that safely captures output.

---

## 1. System Architecture

To mitigate the "Mission Critical" risk of wrapping the user's shell, we separate **Systems Reliability** from **Intelligence Logic**.

### 1.1 The Process Split

#### `clai-pty` (The Dumb Host)

- **Language:** Rust
- **Role:** The "Man-in-the-Middle"
- **Constraint:** Zero logic. Zero network calls. Minimal memory allocation.
- **Responsibility:**
  - Manages the Master/Slave PTY pair
  - Pumps I/O (Stdin ↔ Master ↔ Stdout)
  - Parses ANSI/OSC state
  - Buffers data in a Ring Buffer
  - Forwards structured events to the Daemon

#### `clai-daemon` (The Smart Brain)

- **Language:** Go (Extended from Phase 1)
- **Role:** The Intelligence Engine
- **Responsibility:**
  - Receives output logs from `clai-pty`
  - Runs sanitization (Regex)
  - Persists to SQLite
  - Calls AI Providers

### 1.2 The "Rescue Net" Topology

We cannot recover a session if the PTY Host crashes. Therefore, **the Host must never crash**.

- **Design:** `clai-pty` is "Systems-Only." It has no dependencies on the DB or the AI.
- **Failure Mode:** If `clai-daemon` (the risky process) panics or hangs, `clai-pty` detects the socket break. It logs a warning ("Daemon lost") but keeps the terminal window open and continues functioning as a dumb passthrough pipe.

---

## 2. The `clai-pty` Host (Rust)

### 2.1 Core Responsibilities

This binary replaces the terminal emulator's direct connection to the shell.

#### Raw Mode Management

- Capture TTY attributes (`tcgetattr`)
- Set Raw Mode (disable canonical mode, echo, signals)
- **Guarantee:** Restore original attributes on exit (`Drop` trait in Rust)

#### Fork/Exec

- Use `portable-pty` or `nix` crates to call `forkpty`
- Spawn the user's shell (e.g., `/bin/zsh`) as the child process

#### Signal Proxying

| Signal | Handling |
|--------|----------|
| **SIGWINCH** | Catch signal → `ioctl(TIOCGWINSZ)` (Host) → `ioctl(TIOCSWINSZ)` (Master) |
| **SIGCHLD** | Detect when the shell exits to close the wrapper |

### 2.2 The State Machine (Parsing)

We must parse the byte stream without adding latency to the render path.

**Split-Packet Handling:** The parser must handle escape sequences split across `read()` buffers.

> **Example:** Packet 1 ends with `\x1b]`; Packet 2 begins with `133;A\x07`.

**OSC 133 Tracking:**

| Sequence | State |
|----------|-------|
| `\x1b]133;A\x07` | `PROMPT` |
| `\x1b]133;B\x07` | `INPUT` |
| `\x1b]133;C\x07` | `OUTPUT` |
| `\x1b]133;D;{code}\x07` | `FINISHED` |

---

## 3. Privacy & Output Capture

### 3.1 The Two-Gate Safety System

We assume all output is **toxic until proven safe**.

#### Gate 1: The Interactive Denylist (Deterministic)

| Aspect | Detail |
|--------|--------|
| **Mechanism** | Track the foreground process name (via `tcgetpgrp` or `/proc` scanning) |
| **Denylist** | `ssh`, `scp`, `sftp`, `mysql`, `psql`, `passwd`, `vim`, `nano`, `htop`, `docker login` |
| **Action** | If denylisted, the Ring Buffer is **Paused**. Data flows to the screen, but `clai-pty` records nothing. |

#### Gate 2: The Echo-Gap Heuristic (Fallback)

| Aspect | Detail |
|--------|--------|
| **Logic** | For allowed commands, monitor the Input vs. Output streams |
| **Heuristic** | If User Input > 0 bytes AND Output Echo == 0 bytes for > 50ms → Enter **Secure Mode** |
| **Action** | Retroactively scrub the last N bytes of the Ring Buffer |
| **Exit** | Resume recording only after `\n` is seen in Output AND Echo resumes |

### 3.2 Ring Buffer Implementation

| Aspect | Detail |
|--------|--------|
| **Storage** | 4MB Fixed-Size Circular Buffer (Stack or Pre-allocated Heap) |
| **Tail Drop** | If buffer fills, overwrite oldest data. Prioritize the end of the error message (diagnosis context). |
| **Commit Strategy** | Data stays in RAM by default. Sent to Daemon (Socket) only on Exit Code != 0 or Explicit User Trigger. |

---

## 4. Shell Integration (OSC 133 Injection)

We do not rely on user config. We **force** the shell to behave.

### 4.1 Injection Wrappers

`clai-pty` spawns the shell with modified arguments to load our init scripts first.

| Shell | Method |
|-------|--------|
| **Zsh** | `ZDOTDIR=/tmp/clai/zsh-wrapper` (Wrapper sources `~/.zshrc` then injects hooks) |
| **Bash** | `bash --rcfile <(cat ~/.bashrc /usr/lib/clai/init.bash)` |

### 4.2 Passthrough Fallback

If `clai-pty` does not detect an OSC 133 Sequence within 500ms of startup (e.g., user heavily customized prompt overrides us), it enters **Passthrough Mode**.

**Behavior:** Disables all AI features. Logs a warning. Acts as a transparent pipe.

---

## 5. User Experience

### 5.1 The "Assistant Comment"

We avoid input buffer injection (too dangerous).

**Trigger:** Command fails (Exit != 0)

**Action:**

1. Daemon analyzes output
2. Daemon sends suggestion back to `clai-pty`
3. `clai-pty` waits for Prompt End (OSC 133 B)
4. `clai-pty` writes to Master: `\n# clai suggestion: git push --set-upstream origin main\n`

**Result:** The suggestion appears as a comment on the user's new line. They can copy/paste it or ignore it.

**Example:**

```
$ git psuh
git: 'psuh' is not a git command. See 'git --help'.

$
# clai suggestion: git push
```

---

## 6. Database Schema (Additions)

Phase 2 adds strict storage for captured logs.

### New Table: `command_events`

```sql
CREATE TABLE command_events (
  id INTEGER PRIMARY KEY,
  session_id TEXT NOT NULL,
  command_id TEXT NOT NULL,
  exit_code INTEGER,
  start_ts INTEGER,
  end_ts INTEGER,
  is_sensitive BOOLEAN DEFAULT 0,  -- Triggered by Denylist or Echo-Gap
  captured_bytes INTEGER
);
```

### New Table: `command_output`

```sql
CREATE TABLE command_output (
  id INTEGER PRIMARY KEY,
  command_id TEXT NOT NULL,
  stdout_blob BLOB,      -- Stored only if not sensitive
  stderr_blob BLOB,
  created_at INTEGER,
  expires_at INTEGER     -- Auto-prune policy (e.g., 7 days)
);

CREATE INDEX IF NOT EXISTS idx_command_output_expires ON command_output(expires_at);
```

---

## 7. Migration & Coexistence

**Strategy:** clai (Phase 1) and `clai-pty` (Phase 2) are **mutually exclusive** modes.

### Installation

The user installs the binary.

### Activation

| Mode | Setup | Result |
|------|-------|--------|
| **Mode A (Hook)** | User adds `eval "$(clai hook zsh)"` to `.zshrc` | Phase 1 behavior |
| **Mode B (Wrapper)** | User changes terminal emulator command to `/usr/local/bin/clai-pty` | Phase 2 behavior |

### Conflict Prevention

`clai-pty` sets an environment variable `CLAI_PTY_ACTIVE=1`. The Phase 1 hooks check this variable. If set, the hooks disable themselves to prevent double-logging.

---

## 8. Roadmap & Milestones

### Milestone 2.1: The Dumb Pipe (Rust)

- [ ] Implement `clai-pty` using `portable-pty`
- [ ] Handle SIGWINCH and Raw Mode
- [ ] Verify `vim`, `ssh`, and `htop` work perfectly
- [ ] No AI, No DB

### Milestone 2.2: The Parser

- [ ] Implement `vte` parser state machine
- [ ] Implement Shell Injection scripts
- [ ] Log "Command Start/End" events to debug stdout

### Milestone 2.3: The Guardrails

- [ ] Implement Interactive Denylist
- [ ] Implement Ring Buffer
- [ ] Connect `clai-pty` → `clai-daemon` (Unix Socket)

### Milestone 2.4: Intelligence

- [ ] Enable Output Capture logic in Daemon
- [ ] Implement "Assistant Comment" rendering
