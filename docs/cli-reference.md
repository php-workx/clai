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
clai history --global     # Across all sessions
clai history --cwd /tmp   # Filter by working directory
clai history --format json
```

### `clai suggest`

Get command suggestions for the current prefix.

```bash
clai suggest "git st"           # Top suggestion
clai suggest --limit 5 "git"    # Up to 5 suggestions
clai suggest --json "git"       # JSON output for picker use
```

### `clai on` / `clai off`

Enable or disable suggestion UX.

```bash
clai off           # Disable suggestions globally
clai on            # Re-enable globally
clai off --session # Disable only in current shell
clai on --session  # Re-enable for current shell
```

## Daemon

clai runs a lightweight background daemon that:

- Stores command history
- Provides fast suggestions
- Auto-starts on first use
- Auto-exits after idle timeout (default: 2 hours)

The daemon uses minimal resources and only runs when you have active terminal sessions.
