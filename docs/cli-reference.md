# CLI Reference

## Commands

### `clai init <shell>`

Output shell integration script. Add to your shell config.

```bash
clai init zsh    # For Zsh
clai init bash   # For Bash
clai init fish   # For Fish
```

### `clai version`

Show version, git commit, and build date.

```bash
$ clai version
clai v0.1.0 (abc1234) built 2024-01-15
```

### `clai daemon`

Manage the background daemon.

```bash
clai daemon start   # Start daemon manually
clai daemon stop    # Stop daemon
clai daemon status  # Check if running
```

### `clai history`

Search command history.

```bash
clai history              # Show recent commands
clai history "git"        # Search for git commands
clai history --session    # Current session only
```

## Daemon

clai runs a lightweight background daemon that:

- Stores command history
- Provides fast suggestions
- Auto-starts on first use
- Auto-exits after idle timeout (default: 2 hours)

The daemon uses minimal resources and only runs when you have active terminal sessions.
