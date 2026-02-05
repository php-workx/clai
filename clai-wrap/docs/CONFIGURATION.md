# clai-wrap Configuration

This document describes all configuration options for clai-wrap, including the configuration file format, environment variables, and command-line arguments.

## Configuration File

### Location

clai-wrap searches for configuration files in the following order (first found wins):

| Platform | Primary Location | Fallback Location |
|----------|------------------|-------------------|
| Unix/Linux | `$XDG_CONFIG_HOME/clai/wrap.toml` | `~/.clai-wrap.toml` |
| macOS | `~/.config/clai/wrap.toml` | `~/.clai-wrap.toml` |
| Windows | `%APPDATA%\clai\wrap.toml` | `~/.clai-wrap.toml` |

If `$XDG_CONFIG_HOME` is not set on Unix, it defaults to `~/.config`.

### File Format

The configuration file uses TOML format. All options are optional; sensible defaults are used when not specified.

### Complete Example

```toml
# clai-wrap configuration file
# Location: ~/.config/clai/wrap.toml

# Hotkey chord to trigger picker
# Format: "modifier-key second-key"
# Default: "ctrl-\ h" for history, "ctrl-\ c" for completions
hotkey = "ctrl-\\ h"

# Output buffer capacity in bytes
# Used for buffering PTY output while picker is open
# Default: 2097152 (2 MiB)
buffer_capacity = 2097152

# Unix socket path for daemon connection
# Default: $XDG_RUNTIME_DIR/clai/daemon.sock or /tmp/clai-{uid}/daemon.sock
daemon_socket = "/run/user/1000/clai/daemon.sock"

# Execute command immediately after selection
# If true, appends newline after injection
# Default: false
execute_on_select = false

# Denylist patterns for privacy (processes to exclude from output capture)
# Format: "type:pattern" where type is exact, prefix, or contains
# Lines without a type prefix default to exact matching
denylist = [
    "exact:ssh",
    "exact:scp",
    "exact:sftp",
    "exact:mysql",
    "exact:psql",
    "exact:passwd",
    "exact:vim",
    "exact:nvim",
    "exact:nano",
    "exact:less",
    "exact:more",
    "exact:htop",
    "exact:top",
    "exact:docker",
    "exact:sudo",
    "exact:su",
    "exact:doas",
    "prefix:gpg",
    "contains:password",
]
```

## Configuration Options

### `hotkey`

The hotkey chord to trigger the picker UI.

- **Type**: String
- **Default**: `"ctrl-\\ h"` (Ctrl-\ followed by h)
- **Format**: `"modifier-key second-key"`

The default chord uses `Ctrl-\` as the first key. This key normally generates SIGQUIT on Unix, but clai-wrap intercepts it in raw mode.

```toml
# Use Ctrl-\ h for history (default)
hotkey = "ctrl-\\ h"

# Note: The backslash must be escaped in TOML
```

### `buffer_capacity`

Output buffer capacity in bytes for buffering PTY output while the picker is open.

- **Type**: Integer
- **Default**: `2097152` (2 MiB)
- **Minimum**: 1
- **Maximum**: No hard limit (memory permitting)

When the buffer is full, oldest data is overwritten (ring buffer behavior).

```toml
# Default (2 MiB)
buffer_capacity = 2097152

# Larger buffer for high-output scenarios (4 MiB)
buffer_capacity = 4194304

# Smaller buffer for memory-constrained systems (512 KiB)
buffer_capacity = 524288
```

### `daemon_socket`

Path to the Unix socket for daemon connection.

- **Type**: String (path)
- **Default**: Platform-specific (see below)

Default socket paths:
- **Unix**: `$XDG_RUNTIME_DIR/clai/daemon.sock` or `/tmp/clai-{uid}/daemon.sock`
- **Windows**: Named pipe `\\.\pipe\clai-daemon-{username}`

```toml
# Custom socket path
daemon_socket = "/var/run/clai/daemon.sock"

# User-specific path
daemon_socket = "/run/user/1000/clai/daemon.sock"
```

### `execute_on_select`

Whether to execute the selected command immediately after insertion.

- **Type**: Boolean
- **Default**: `false`

When `false`, the selected command is inserted at the cursor position without a trailing newline.
When `true`, a newline is appended, causing the command to execute immediately.

```toml
# Insert only (default)
execute_on_select = false

# Insert and execute
execute_on_select = true
```

### `denylist`

Patterns for processes that should have output capture paused (privacy protection).

- **Type**: Array of strings
- **Default**: Built-in denylist (see below)

Pattern format:
- `"exact:name"` - Match process name exactly (case-insensitive)
- `"prefix:name"` - Match if process name starts with pattern
- `"contains:name"` - Match if process name contains pattern
- `"name"` - No prefix defaults to exact match

```toml
denylist = [
    "exact:ssh",           # Exact match for "ssh"
    "prefix:docker",       # Matches "docker", "dockerd", "docker-compose"
    "contains:password",   # Matches any process with "password" in name
    "mysql",               # Defaults to exact match
]
```

**Default denylist** (always active unless overridden):
- `ssh`, `scp`, `sftp` - Remote access
- `mysql`, `psql` - Database clients
- `passwd` - Password changes
- `vim`, `nvim`, `nano` - Text editors
- `less`, `more` - Pagers
- `htop`, `top` - System monitors
- `docker` - Container commands
- `sudo`, `su`, `doas` - Privileged execution

Custom denylist entries are **merged** with the defaults, not replaced.

## Environment Variables

Environment variables override configuration file settings and can be used for temporary overrides.

| Variable | Description | Example |
|----------|-------------|---------|
| `SHELL` | Default shell to launch | `/bin/zsh` |
| `CLAI_WRAP=1` | Set by wrapper (read-only) | `1` |
| `CLAI_DEBUG=1` | Enable debug logging | `1` |
| `CLAI_HOTKEY` | Override hotkey chord | `ctrl-] h` |
| `CLAI_SOCKET` | Override daemon socket path | `/tmp/clai.sock` |
| `NO_COLOR` | Disable TUI colors | `1` |
| `COLORTERM` | Color depth hint | `truecolor`, `24bit` |
| `TERM` | Terminal type | `xterm-256color` |

### `SHELL`

The default shell to launch if `--shell` is not specified.

```bash
export SHELL=/bin/zsh
clai-wrap  # Launches zsh
```

### `CLAI_DEBUG`

Enable debug logging (equivalent to `--debug`).

```bash
CLAI_DEBUG=1 clai-wrap
```

Debug logs are written to:
- stderr
- `$XDG_STATE_HOME/clai/clai-wrap.log` (default: `~/.local/state/clai/clai-wrap.log`)

### `CLAI_HOTKEY`

Override the default hotkey chord.

```bash
CLAI_HOTKEY="ctrl-] h" clai-wrap
```

### `CLAI_SOCKET`

Override the daemon socket path.

```bash
CLAI_SOCKET=/tmp/my-clai.sock clai-wrap
```

### Color Detection

clai-wrap detects color support using these environment variables (in priority order):

1. `NO_COLOR` - Disables all colors if set
2. `COLORTERM=truecolor` or `COLORTERM=24bit` - Enables 24-bit color
3. `TERM` containing `256color` - Enables 256-color mode
4. Fallback to 16 colors

## Command Line Arguments

Command-line arguments take highest priority, overriding both configuration files and environment variables.

### Shell Options

```bash
# Specify shell to launch
clai-wrap --shell /bin/zsh
clai-wrap -s /bin/fish

# Launch as non-login shell
clai-wrap --login-shell false
```

### Hotkey Options

```bash
# Custom hotkey chord
clai-wrap --hotkey "ctrl-] h"

# Custom chord timeout (milliseconds)
clai-wrap --hotkey-timeout 750
```

### Buffer Options

```bash
# Custom buffer size (4 MiB)
clai-wrap --buffer-cap 4194304
```

### Daemon Options

```bash
# Custom socket path
clai-wrap --daemon-socket /tmp/clai.sock

# Custom connection timeout (milliseconds)
clai-wrap --daemon-timeout 1000

# Disable daemon (standalone mode)
clai-wrap --no-daemon
clai-wrap --standalone
```

### UI Options

```bash
# Disable picker UI
clai-wrap --no-ui

# Execute on select
clai-wrap --execute-on-select
```

### History Options

```bash
# Custom history file
clai-wrap --history-file ~/.my_history
```

### Mode Options

```bash
# Passthrough mode (no TTY requirement)
clai-wrap --passthrough
clai-wrap --force-non-tty

# Debug mode
clai-wrap --debug
```

### Utility Commands

```bash
# Show version
clai-wrap version

# Reset terminal state (after abnormal exit)
clai-wrap reset-terminal
```

## Precedence Order

Configuration sources are applied in the following order (later sources override earlier):

1. **Built-in defaults** (lowest priority)
2. **Configuration file** (`wrap.toml`)
3. **Environment variables**
4. **Command-line arguments** (highest priority)

### Example

```bash
# Configuration file: buffer_capacity = 1000000
# Environment: (not set)
# CLI: --buffer-cap 4000000

clai-wrap --buffer-cap 4000000  # Uses 4000000
```

## Shell-Specific Notes

### Bash

History file: `~/.bash_history`

For timestamped history, clai-wrap parses the `#timestamp` lines.

### Zsh

History file: `~/.zsh_history`

Supports both plain format and extended format (`: timestamp:0;command`).

### Fish

History file: `~/.local/share/fish/fish_history`

Parses Fish's YAML-like history format. Fish >= 3.6 has native OSC 133 support.

### PowerShell

History via PSReadLine history file.

## Validation

clai-wrap validates configuration on startup:

| Setting | Validation |
|---------|------------|
| `buffer_capacity` | Must be > 0 |
| `hotkey` | Must not be empty |
| `denylist` patterns | Must not be empty strings |
| `daemon_socket` | Path must be valid |

Invalid configuration results in a startup error with a descriptive message.

## Migration from Previous Versions

If you have an existing `~/.clai-wrap.toml` file, it will continue to work. The new XDG-compliant location (`~/.config/clai/wrap.toml`) takes precedence if both exist.

To migrate:

```bash
mkdir -p ~/.config/clai
mv ~/.clai-wrap.toml ~/.config/clai/wrap.toml
```
