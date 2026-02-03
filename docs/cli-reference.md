# CLI Reference

## Core Commands

### `clai cmd <natural language>`

Convert natural language into a shell command via Claude CLI.
The generated command is also cached for Tab completion.

```bash
clai cmd "list all files in the current directory"
clai cmd "find all Python files modified today"
```

### `clai ask <question>`

Ask Claude a question with terminal context (cwd + shell).
If a command is present in the answer, it is cached for Tab completion.

```bash
clai ask "How do I find large files?"
clai ask --context "git status\nmake test" "What should I run next?"
```

### `clai suggest [prefix]`

Get suggestions for the current prefix. Falls back to shell history when the
history daemon is unavailable. With an empty prefix, returns the cached AI suggestion.

```bash
clai suggest "git st"           # Top suggestion
clai suggest --limit 5 "git"    # Up to 5 suggestions
clai suggest --json "git"       # JSON output (includes risk field)
```

### `clai history [query]`

Query command history stored in the clai database.
By default, it uses the current session ID.

```bash
clai history                 # Recent commands (current session)
clai history git             # Commands starting with "git"
clai history --global        # Across all sessions
clai history --cwd /tmp      # Filter by working directory
clai history --status success
clai history --format json
```

### `clai on` / `clai off`

Enable or disable suggestion UX globally (config) or for the current session.

```bash
clai off
clai on
clai off --session
clai on --session
```

## Setup & Configuration

### `clai install`

Install shell integration by writing a hook file and sourcing it from your rc file.

```bash
clai install
clai install --shell=zsh
clai install --shell=bash
clai install --shell=fish
```

### `clai uninstall`

Remove shell integration lines from your rc file.

```bash
clai uninstall
```

### `clai init <shell>`

Print the shell integration script to stdout and generate a session ID.
Use this if you prefer `eval` in your rc file instead of `clai install`.

```bash
eval "$(clai init zsh)"
clai init fish | source
```

### `clai config [key] [value]`

Get or set configuration values in `~/.clai/config.yaml`.

```bash
clai config
clai config suggestions.enabled
clai config suggestions.enabled false
```

### `clai status`

Show status for Claude CLI, shell integration, session ID, and the history daemon.

```bash
clai status
```

### `clai version`

Print version, git commit, and build date.

```bash
clai version
```

## Advanced / Troubleshooting

### `clai logs`

Tail the history daemon log file (`~/.clai/logs/daemon.log`).

```bash
clai logs
clai logs --follow
```

### `clai doctor`

Run a diagnostic check (Claude CLI, shell integration, daemon status, config).

```bash
clai doctor
```

### `clai daemon`

Manage the **Claude CLI** background process used to speed up `clai cmd`.
This is **not** the history/suggestions daemon.

```bash
clai daemon start
clai daemon stop
clai daemon status
```

## Background Processes

- **History daemon (`claid`)**: gRPC daemon that stores history and serves
  session-aware suggestions. It is started by `clai-shim` when available.
  If `claid` is not installed, `clai suggest` falls back to your shell history.
- **Claude CLI daemon (`clai daemon`)**: keeps a Claude CLI process warm for
  faster `clai cmd` calls.
