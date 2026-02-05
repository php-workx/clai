# clai-wrap

A high-fidelity PTY wrapper for intelligent terminal assistance.

`clai-wrap` wraps your shell in a pseudo-terminal to provide intelligent command suggestions, history search, and autocomplete features. It intercepts a configurable hotkey chord to display an instant picker UI while maintaining full terminal compatibility.

## Features

- **High-fidelity terminal wrapping**: Crash-resistant PTY wrapper with proper raw mode management
- **Instant UI**: Sub-100ms picker display for history search and completions
- **Cross-platform support**: Linux, macOS (POSIX PTY), and Windows 10/11 (ConPTY)
- **Cross-shell support**: Bash, Zsh, Fish, PowerShell
- **Privacy-first**: Two-gate safety system prevents capture of sensitive content
- **AI-powered suggestions**: Integration with clai-daemon for intelligent command suggestions

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/your-org/clai.git
cd clai/clai-wrap

# Build the release binary
cargo build --release

# The binary is at target/release/clai-wrap
```

### Requirements

- Rust 1.78.0 or later (see `rust-toolchain.toml`)
- On Windows: Windows 10 1809+ (ConPTY support required)

## Quick Start

### Basic Usage

Launch your shell inside clai-wrap:

```bash
clai-wrap
```

Launch a specific shell:

```bash
clai-wrap --shell /bin/zsh
```

### Hotkey Usage

clai-wrap intercepts a two-key chord to display the picker UI:

| Chord | Action |
|-------|--------|
| `Ctrl-\` then `h` | Open history picker |
| `Ctrl-\` then `c` | Open completions picker |

The chord timeout is 500ms by default. If you press `Ctrl-\` and don't follow with `h` or `c` within 500ms, the key is forwarded to the shell.

### Picker UI

When the picker opens:

- **Type** to filter items incrementally (case-insensitive)
- **Arrow keys** (Up/Down) to navigate
- **Enter** to select and insert into the shell
- **Escape** to cancel and return to the shell

## Configuration

### Command Line Options

```
clai-wrap [OPTIONS] [COMMAND]

Options:
  -s, --shell <PATH>          Shell to launch (defaults to $SHELL or /bin/bash)
      --login-shell           Launch as a login shell (default: true)
      --hotkey <CHORD>        Hotkey chord to trigger picker (e.g., "ctrl-\ h")
      --buffer-cap <BYTES>    Output buffer capacity in bytes (default: 2097152)
      --execute-on-select     Execute command immediately after selection
      --history-file <PATH>   Path to history file
      --daemon-socket <PATH>  Unix socket path for daemon connection
      --no-daemon             Disable daemon connection (standalone mode)
      --standalone            Alias for --no-daemon
      --no-ui                 Disable picker UI entirely
      --force-non-tty         Run without TTY requirement (passthrough mode)
      --passthrough           Alias for --force-non-tty
      --debug                 Enable debug logging
      --daemon-timeout <MS>   Daemon connection timeout (default: 500)
      --hotkey-timeout <MS>   Hotkey chord timeout (default: 500)
  -h, --help                  Print help
  -V, --version               Print version

Commands:
  version         Show version information
  reset-terminal  Reset terminal state (useful after abnormal exit)
```

### Configuration File

clai-wrap reads configuration from:

- **Unix/macOS**: `~/.config/clai/wrap.toml`
- **Windows**: `%APPDATA%\clai\wrap.toml`
- **Legacy**: `~/.clai-wrap.toml` (fallback)

See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) for detailed configuration options.

### Environment Variables

| Variable | Description |
|----------|-------------|
| `CLAI_WRAP=1` | Set by wrapper (read-only) |
| `CLAI_DEBUG=1` | Enable debug logging |
| `CLAI_HOTKEY` | Override default hotkey chord |
| `CLAI_SOCKET` | Override daemon socket path |

## Shell Support

| Shell | Status | Notes |
|-------|--------|-------|
| Bash | Full | OSC 133 injection via `--rcfile` |
| Zsh | Full | OSC 133 injection via `ZDOTDIR` wrapper |
| Fish | Full | Fish >= 3.6 has native OSC 133; older versions use `--init-command` |
| PowerShell | Full | OSC 133 injection via `-NoExit -Command` |
| cmd.exe | Passthrough | No OSC 133 support |

## Operation Modes

### Full Mode (Default)

Connects to clai-daemon for AI-powered suggestions and output analysis.

```bash
clai-wrap
```

### Standalone Mode

Operates without daemon connection. History picker works from local shell history files.

```bash
clai-wrap --standalone
# or
clai-wrap --no-daemon
```

### Passthrough Mode

Pure PTY passthrough without UI or hotkey interception. Useful for testing or when TTY is unavailable.

```bash
clai-wrap --passthrough
# or
clai-wrap --force-non-tty
```

## Troubleshooting

### Terminal State Recovery

If the terminal state is corrupted after an abnormal exit:

```bash
# Reset terminal to sane state
stty sane

# Or use the built-in command
clai-wrap reset-terminal

# Full terminal reset
reset
```

### Common Issues

| Symptom | Cause | Solution |
|---------|-------|----------|
| No echo after exit | Terminal not restored | Run `stty sane` |
| Stuck in alt-screen | UI crashed | Run `tput rmcup` or `reset` |
| Hotkey not working | TTY not detected | Check if stdin is a TTY |
| High latency | Running inside tmux | Expected; tmux buffers output |
| ConPTY error (Windows) | Old Windows version | Requires Windows 10 1809+ |

### Debug Mode

Enable verbose logging for troubleshooting:

```bash
clai-wrap --debug

# Or via environment variable
CLAI_DEBUG=1 clai-wrap
```

Logs are written to stderr and optionally to `~/.local/state/clai/clai-wrap.log`.

### Denylist Verification

clai-wrap automatically pauses output capture for sensitive processes. The default denylist includes:

- Remote access: `ssh`, `scp`, `sftp`
- Databases: `mysql`, `psql`
- Editors: `vim`, `nvim`, `nano`
- Pagers: `less`, `more`
- System monitors: `htop`, `top`
- Privileged: `sudo`, `su`, `doas`

Custom denylist patterns can be configured in `wrap.toml`. See [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Architecture

For technical details about the internal architecture, threading model, and component design, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Development

### Building

```bash
cargo build
```

### Testing

```bash
cargo test
```

### Linting

```bash
cargo fmt --check
cargo clippy --all-targets --all-features -- -D warnings
```

### Generating Documentation

```bash
cargo doc --no-deps --open
```

## License

MIT License - see LICENSE file for details.
