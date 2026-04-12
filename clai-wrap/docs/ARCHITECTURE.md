# clai-wrap Architecture

This document describes the technical architecture of clai-wrap, including the module structure, data flow, threading model, and key components.

## Overview

clai-wrap is a PTY wrapper written in Rust that interposes between the user's terminal and their shell. It provides intelligent command assistance while maintaining high-fidelity terminal emulation.

```
+------------------+     +------------------+     +------------------+
|                  |     |                  |     |                  |
|  Real Terminal   |<--->|   clai-wrap      |<--->|   Child Shell    |
|  (stdin/stdout)  |     |   (PTY Master)   |     |   (PTY Slave)    |
|                  |     |                  |     |                  |
+------------------+     +------------------+     +------------------+
                               |
                               | Unix Socket
                               v
                         +------------------+
                         |                  |
                         |   clai-daemon    |
                         |   (AI Engine)    |
                         |                  |
                         +------------------+
```

## Module Overview

```
clai-wrap/src/
|
+-- main.rs              # Entry point, CLI handling, mode selection
+-- lib.rs               # Public API exports
+-- cli.rs               # Command-line argument parsing (clap)
+-- config.rs            # TOML configuration file loading
|
+-- PTY & Terminal
|   +-- pty_host.rs      # PTY creation and child process management
|   +-- raw_mode.rs      # Terminal raw mode management
|   +-- resize.rs        # Terminal resize handling and debouncing
|   +-- signals.rs       # Unix signal handling (SIGWINCH, etc.)
|
+-- I/O Layer
|   +-- io_threads.rs    # I/O passthrough threads and buffering
|   +-- input_router.rs  # Hotkey detection and input routing
|   +-- hotkey.rs        # Two-key chord state machine
|   +-- passthrough.rs   # Passthrough mode detection
|
+-- Output Processing
|   +-- output_capture.rs    # Ring buffer for output capture
|   +-- ring_buffer.rs       # Lock-free SPSC ring buffer
|   +-- osc133.rs            # OSC 133 semantic prompt parser
|   +-- alt_screen.rs        # Alternate screen buffer handling
|   +-- color_detect.rs      # Terminal color capability detection
|
+-- Picker UI
|   +-- picker.rs            # Interactive picker component (ratatui)
|   +-- history_picker.rs    # History-specific picker wrapper
|   +-- history_parser.rs    # Shell history file parsing
|
+-- Injection
|   +-- selection_inject.rs      # Selection injection into PTY
|   +-- bracketed_paste.rs       # Bracketed paste mode tracking
|   +-- shell_inject/
|       +-- mod.rs               # Shell injection module
|       +-- bash.rs              # Bash OSC 133 injection
|       +-- zsh.rs               # Zsh OSC 133 injection
|       +-- fish.rs              # Fish OSC 133 injection
|
+-- Privacy
|   +-- denylist.rs          # Interactive denylist for sensitive processes
|   +-- process_detect.rs    # Foreground process detection (Unix)
|   +-- echo_gap.rs          # Echo-gap heuristic for password detection
|
+-- Daemon Communication
|   +-- daemon_client.rs     # Unix socket client for clai-daemon
|   +-- daemon_events.rs     # Event forwarding to daemon
|   +-- jsonrpc.rs           # JSON-RPC 2.0 protocol implementation
|   +-- suggestion_receiver.rs   # AI suggestion handling
|   +-- assistant_comment.rs     # Shell comment rendering
|
+-- Support
|   +-- temp_dir.rs          # Temporary directory management
|   +-- standalone.rs        # Standalone mode state management
|
+-- Platform-specific
    +-- windows/
        +-- mod.rs           # Windows-specific module
        +-- conpty.rs        # ConPTY wrapper
        +-- console_events.rs    # Console event handling
        +-- process_detect.rs    # Windows process detection
```

## Key Components

### PTY Host

The `PtyHost` component manages the pseudo-terminal pair:

- Creates a PTY using `portable-pty` (cross-platform abstraction)
- Spawns the user's shell as the child process
- Provides reader/writer handles for I/O
- Handles PTY resize operations
- Manages child process lifecycle

```rust
// PTY creation flow
let pty_host = PtyHost::new(shell_path)?;
let reader = pty_host.reader()?;
let writer = pty_host.writer()?;
```

### I/O Threads

The I/O layer uses dedicated threads for non-blocking operation:

```
+----------------+       +------------------+       +-----------------+
|  Stdin Reader  |------>|   Input Router   |------>|   PTY Writer    |
|    Thread      |       |   (Hotkey Det.)  |       |    Thread       |
+----------------+       +------------------+       +-----------------+
                                |
                                | Hotkey Event
                                v
                         +------------------+
                         |   Main Thread    |
                         |   (UI/Events)    |
                         +------------------+
                                ^
                                | PTY Output
                                |
+----------------+       +------------------+
|   PTY Reader   |------>|  Output Buffer   |
|    Thread      |       |  (Ring Buffer)   |
+----------------+       +------------------+
```

**Critical Design Constraint**: The PTY reader thread must NEVER block. PTY kernel buffers are limited (4KB-64KB), and blocking could cause the child process to deadlock on `write()`.

### Hotkey Parser

The hotkey parser is a state machine that detects two-key chords:

```
                +----------------+
                |     Idle       |
                +----------------+
                      |
                      | First byte (Ctrl-\)
                      v
           +--------------------+
           | WaitingForSecond   |
           +--------------------+
                 |    |    |
          +------+    |    +------+
          |           |           |
          v           v           v
    [Timeout]    ['h'/'c']   [Other]
          |           |           |
          v           v           v
    Forward      Trigger     Forward
    Bytes        Hotkey      Both
```

### Ring Buffer

Two separate ring buffers serve different purposes:

1. **Output Capture Buffer** (4MB): Stores command output for daemon analysis
2. **Picker Display Buffer** (2MB): Buffers PTY output while picker UI is open

Both use a lock-free SPSC (Single-Producer Single-Consumer) design to ensure the PTY reader never blocks.

### Picker UI

The picker uses `ratatui` for rendering:

```
+----------------------------------------+
| Search (2/15)                          |
+----------------------------------------+
| > git status                           |
|   git commit -m 'fix: bug'             |
|   git push origin main                 |
|   cargo build --release                |
|   ...                                  |
+----------------------------------------+

- Incremental search (filter as you type)
- Arrow key navigation
- Enter to select, Escape to cancel
- Renders in alternate screen buffer
```

### Daemon Client

Communicates with clai-daemon using JSON-RPC 2.0 over Unix sockets:

```
clai-wrap                          clai-daemon
    |                                   |
    |-- ping -------------------------->|
    |<---------------- {"pong": true} --|
    |                                   |
    |-- command.start ----------------->|
    |<-------------------- {"ok": true} |
    |                                   |
    |-- output.chunk (base64) --------->|
    |<-------------------- {"ok": true} |
    |                                   |
    |-- command.end ------------------->|
    |<-------------------- {"ok": true} |
    |                                   |
    |<-- suggestion.available ----------|
    |                                   |
```

## Data Flow

### Normal Operation (Passthrough)

```
1. User types character
2. Stdin Reader reads byte
3. Input Router checks for hotkey chord
4. If not hotkey: forward to PTY Writer
5. PTY Writer sends to child shell
6. Child shell processes input
7. PTY Reader reads output
8. Output written directly to stdout
```

### Picker Open

```
1. User triggers hotkey (Ctrl-\ h)
2. Input Router detects chord completion
3. Main thread enters PickerOpen state
4. Picker UI renders in alt-screen
5. PTY output buffered (not written to stdout)
6. User selects item
7. Selection injected into PTY via bracketed paste
8. Picker closes, alt-screen exits
9. Buffered output flushed to stdout
10. Resume passthrough mode
```

### Privacy Gate Flow

```
1. PTY Reader receives output
2. Check Gate 1: Interactive Denylist
   - Detect foreground process name
   - If denylisted (ssh, vim, etc.): pause capture
3. Check Gate 2: Echo-Gap Heuristic
   - If input received but no echo: likely password entry
   - Retroactively scrub ring buffer
4. If both gates pass: capture output for daemon
```

## Threading Model

clai-wrap uses multiple threads coordinated via atomic flags and channels:

| Thread | Role | Blocking Allowed? |
|--------|------|-------------------|
| Main | Event loop, UI rendering, signal handling | Yes |
| Stdin Reader | Read from real stdin | Yes (blocking read) |
| PTY Writer | Write to PTY master | Yes (blocking write) |
| PTY Reader | Read from PTY master | **No** (critical) |

### Shared State

```rust
pub struct IoState {
    picker_open: AtomicBool,      // UI state
    shutdown: AtomicBool,         // Shutdown signal
    overflow_occurred: AtomicBool, // Buffer overflow flag
}
```

### Synchronization

- **Atomic flags**: For state that changes rarely (picker_open, shutdown)
- **Channels**: For event communication between threads
- **Lock-free ring buffer**: For PTY output buffering

## Platform Considerations

### Unix (Linux/macOS)

- PTY via native POSIX APIs (through `portable-pty`)
- Signals: SIGWINCH, SIGCHLD, SIGINT, SIGTERM, SIGHUP, SIGTSTP, SIGCONT
- Process detection: `/proc/{pid}/comm` (Linux), `libproc` (macOS)

### Windows

- PTY via ConPTY (requires Windows 10 1809+)
- Console events via `SetConsoleCtrlHandler`
- Process detection via Tool Help Library (`CreateToolhelp32Snapshot`)

## Error Handling

### Graceful Degradation

```
Full Mode -> Standalone Mode -> Passthrough Mode
    |              |                  |
    |              |                  +-- Pure PTY forwarding
    |              +-- Local history only
    +-- All features
```

When daemon is unavailable, clai-wrap degrades gracefully:
- AI suggestions disabled
- Output capture disabled
- History picker still works with local files
- PTY passthrough always works

### Terminal Restoration

Terminal state must be restored on ALL exit paths:
- Normal exit
- SIGINT/SIGTERM
- SIGHUP
- SIGTSTP (suspend)
- Child exit
- Panic (caught via panic hook)

This is implemented using Rust's `Drop` trait for the raw mode guard.

## Performance Targets

| Metric | Target |
|--------|--------|
| Hotkey to UI visible | < 100ms (p95) |
| Picker search latency | < 16ms per keystroke |
| Memory usage | < 10MB + buffer caps |
| PTY read latency | < 1ms (no blocking) |

## Security Model

1. **No network access** from clai-wrap (daemon handles all network)
2. **Privacy gates** prevent capture of sensitive content
3. **Stale socket cleanup** with ownership verification
4. **History file permissions** set to 0600 (user-only)
