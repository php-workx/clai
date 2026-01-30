# Shell Integration

clai integrates with your shell to provide command suggestions, history tracking, and AI features.

## Supported Shells

| Shell | Version | Status |
| ----- | ------- | ------ |
| zsh | 5.8+ | Full support |
| bash | 4.4+ (3.2 on macOS) | Full support |
| fish | 3.0+ | Full support |
| PowerShell | 7.x | Planned |

## How It Works

clai uses shell hooks to:

1. **Track sessions** - Start/end events when shell opens/closes
2. **Log commands** - Record commands with exit codes and duration
3. **Provide suggestions** - Show completions as you type
4. **Enable voice mode** - Natural language command generation

```
┌─────────────────────────────────────────────────────┐
│  Shell (zsh/bash)                                   │
│  ┌─────────────┐    ┌─────────────┐                │
│  │  preexec    │───▶│  clai-shim  │───▶ daemon    │
│  │  precmd     │    │  (IPC)      │                │
│  └─────────────┘    └─────────────┘                │
└─────────────────────────────────────────────────────┘
```

## Installation

### Automatic

```bash
# Detects your shell and installs
clai install

# Specify shell explicitly
clai install --shell=zsh
clai install --shell=bash
```

### Manual

Add to your shell RC file:

**Zsh** (`~/.zshrc`):
```bash
source "$HOME/.local/share/clai/hooks/clai.zsh"
```

**Bash** (`~/.bashrc` or `~/.bash_profile`):
```bash
source "$HOME/.local/share/clai/hooks/clai.bash"
```

## Features

### Command Suggestions

As you type, clai shows suggestions from your history:

```
$ git ch[suggestions appear above]
  git checkout main
  git cherry-pick abc123
  git checkout -b feature
```

**Keybindings:**
- **Tab** - Cycle through suggestions
- **Right Arrow** - Accept suggestion
- **Escape** - Dismiss suggestions

### Voice Mode (AI Required)

Prefix commands with backtick for natural language:

```
$ `list files larger than 100MB
# Suggests: find . -type f -size +100M

$ `kill the process on port 3000
# Suggests: lsof -ti:3000 | xargs kill -9
```

### Error Diagnosis (AI Required)

Use the `run` wrapper for automatic error diagnosis:

```bash
run npm install

# On failure:
# ╭─ Error Diagnosis ─────────────────────────────╮
# │ The npm install failed because...            │
# │                                              │
# │ Suggested fix:                               │
# │   npm cache clean --force && npm install    │
# ╰──────────────────────────────────────────────╯
```

## Environment Variables

Configure shell integration before sourcing:

```bash
# In ~/.zshrc, BEFORE the source line:

# Auto-start daemon (default: true)
export CLAI_AUTO_DAEMON=true

# Auto-diagnose on command failure (default: false)
export CLAI_AUTO_DIAGNOSE=false
```

## Session Management

Each shell instance gets a unique session ID:

```bash
# View current session
echo $CLAI_SESSION_ID

# Sessions track:
# - Start/end time
# - Working directory changes
# - Commands executed
# - Shell type and version
```

## Uninstalling

```bash
# Remove shell integration
clai uninstall

# Or manually remove the source line from your RC file
```

## Troubleshooting

### Suggestions Not Appearing

1. Check shell integration is loaded:
   ```bash
   type __clai_suggest 2>/dev/null && echo "Loaded" || echo "Not loaded"
   ```

2. Check daemon is running:
   ```bash
   clai status
   ```

3. Verify hook file exists:
   ```bash
   ls ~/.local/share/clai/hooks/
   ```

### Slow Shell Startup

clai is designed for minimal startup impact:
- Shell hooks are lightweight (~100 lines)
- Daemon communication is fire-and-forget
- No blocking operations in shell hooks

If startup is slow, check:
```bash
# Time shell startup
time zsh -i -c exit

# Check daemon logs for issues
clai logs
```

### Conflicts with Other Tools

clai uses standard shell hooks (preexec/precmd) that may conflict with:
- Other suggestion tools (zsh-autosuggestions)
- Command timing tools
- History managers

To resolve, ensure clai is sourced last in your RC file.

## Advanced Configuration

### Custom Suggestion Bindings

The default keybindings can be customized in your RC file after sourcing clai:

```bash
# Example: Use Ctrl+Space instead of Tab
bindkey '^@' __clai_accept_suggestion
```

### Disabling Specific Features

```bash
# Disable suggestions (keep logging)
export CLAI_DISABLE_SUGGEST=1

# Disable session tracking
export CLAI_DISABLE_SESSION=1
```

## Next Steps

- [Configuration](configuration.md) - All configuration options
- [AI Providers](ai-providers.md) - Enable AI features
- [Troubleshooting](troubleshooting.md) - Common issues
