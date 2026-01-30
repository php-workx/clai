# Quick Start

Get up and running with clai in under 5 minutes.

## 1. Install and Set Up

```bash
# Install (see installation.md for platform-specific options)
make build && sudo make install

# Set up shell integration
clai install

# Restart your shell
exec $SHELL
```

## 2. Verify It's Working

```bash
# Check everything is configured correctly
clai doctor

# You should see:
#   [OK] clai binary
#   [OK] Config directory
#   [OK] Shell integration
#   ...
```

## 3. Basic Usage

### Command Suggestions

As you type, clai suggests commands from your history:

```
$ git c[TAB]           # Shows: git commit, git checkout, git clone
$ docker r[TAB]        # Shows: docker run, docker rm, docker restart
```

Press **Tab** to cycle through suggestions, **Right Arrow** to accept.

### View Command History

```bash
# Recent commands
clai history

# Search history
clai history --search="docker"

# Limit results
clai history --limit=20
```

### Check Status

```bash
# View clai status
clai status

# Shows:
#   - Daemon status (running/stopped)
#   - Configuration file location
#   - Database size
#   - Shell integration status
```

## 4. Enable AI Features (Optional)

AI features require an API key from Anthropic, OpenAI, or Google.

```bash
# Set your API key
export ANTHROPIC_API_KEY="your-key-here"

# Enable AI in config
clai config set ai.enabled true

# Or use the Claude CLI
# Install from: https://claude.ai/cli
```

### Voice-to-Command

With AI enabled, use the backtick prefix for natural language:

```
$ `find all large log files
# Suggests: find /var/log -type f -size +100M
```

### Error Diagnosis

When a command fails, clai can explain why:

```bash
# Use the 'run' wrapper for auto-diagnosis
run npm install

# If it fails, clai explains the error and suggests fixes
```

## 5. Configuration

View and modify settings:

```bash
# List all settings
clai config list

# Get a specific value
clai config get ai.enabled

# Set a value
clai config set suggestions.max_history 10
```

Common settings:

| Key | Description | Default |
|-----|-------------|---------|
| `ai.enabled` | Enable AI features | `false` |
| `ai.provider` | AI provider (anthropic, openai, google, auto) | `auto` |
| `suggestions.max_history` | Max history-based suggestions | `5` |
| `suggestions.show_risk_warning` | Warn about destructive commands | `true` |

## 6. Daemon Management

The daemon runs automatically but can be managed manually:

```bash
# Check if running
clai status

# View logs
clai logs

# Stop daemon (it auto-restarts when needed)
clai daemon stop
```

## Next Steps

- [Configuration](configuration.md) - All configuration options
- [Shell Integration](shell-integration.md) - Advanced shell setup
- [AI Providers](ai-providers.md) - Set up AI features
- [Troubleshooting](troubleshooting.md) - Common issues and solutions
