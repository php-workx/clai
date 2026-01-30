# Configuration

clai uses a YAML configuration file at `~/.config/clai/config.yaml`.

## Managing Configuration

```bash
# List all settings with current values
clai config list

# Get a specific value
clai config get ai.enabled

# Set a value
clai config set ai.enabled true

# View config file location
clai status
```

## Configuration Reference

### Daemon Settings

Control the background daemon behavior.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `daemon.idle_timeout_mins` | int | `20` | Auto-shutdown after idle (0 = never) |
| `daemon.socket_path` | string | `""` | Unix socket path (empty = default) |
| `daemon.log_level` | string | `"info"` | Log level: debug, info, warn, error |
| `daemon.log_file` | string | `""` | Log file path (empty = default) |

```yaml
daemon:
  idle_timeout_mins: 20
  log_level: info
```

### Client Settings

Control how the shell client communicates with the daemon.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `client.suggest_timeout_ms` | int | `50` | Max wait for suggestions (ms) |
| `client.connect_timeout_ms` | int | `10` | Socket connection timeout (ms) |
| `client.fire_and_forget` | bool | `true` | Don't wait for logging acks |
| `client.auto_start_daemon` | bool | `true` | Auto-start daemon if not running |

```yaml
client:
  suggest_timeout_ms: 50
  connect_timeout_ms: 10
  fire_and_forget: true
  auto_start_daemon: true
```

### AI Settings

Configure AI-powered features.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ai.enabled` | bool | `false` | Enable AI features (opt-in required) |
| `ai.provider` | string | `"auto"` | Provider: anthropic, openai, google, auto |
| `ai.model` | string | `""` | Model name (empty = provider default) |
| `ai.auto_diagnose` | bool | `false` | Auto-diagnose on non-zero exit |
| `ai.cache_ttl_hours` | int | `24` | AI response cache lifetime |

```yaml
ai:
  enabled: true
  provider: anthropic
  model: claude-3-haiku-20240307
  auto_diagnose: false
  cache_ttl_hours: 24
```

### Suggestion Settings

Control command suggestions.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `suggestions.max_history` | int | `5` | Max history-based suggestions |
| `suggestions.max_ai` | int | `3` | Max AI-generated suggestions |
| `suggestions.show_risk_warning` | bool | `true` | Highlight destructive commands |

```yaml
suggestions:
  max_history: 5
  max_ai: 3
  show_risk_warning: true
```

### Privacy Settings

Control data handling and privacy.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `privacy.sanitize_ai_calls` | bool | `true` | Sanitize sensitive data before AI calls |

```yaml
privacy:
  sanitize_ai_calls: true
```

## Example Configuration File

Full example at `~/.config/clai/config.yaml`:

```yaml
daemon:
  idle_timeout_mins: 30
  log_level: info

client:
  suggest_timeout_ms: 50
  auto_start_daemon: true

ai:
  enabled: true
  provider: anthropic
  auto_diagnose: false
  cache_ttl_hours: 48

suggestions:
  max_history: 10
  max_ai: 5
  show_risk_warning: true

privacy:
  sanitize_ai_calls: true
```

## Environment Variables

Some settings can be overridden via environment variables:

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `OPENAI_API_KEY` | OpenAI API key |
| `GOOGLE_API_KEY` | Google AI API key |
| `CLAI_AUTO_DAEMON` | Override auto-start daemon |
| `CLAI_AUTO_DIAGNOSE` | Override auto-diagnose |
| `CLAI_LOG_LEVEL` | Override log level |

## Default Paths

clai follows XDG Base Directory conventions:

| Path | Purpose |
|------|---------|
| `~/.config/clai/config.yaml` | Configuration file |
| `~/.local/share/clai/clai.db` | SQLite database |
| `~/.local/share/clai/hooks/` | Shell hook scripts |
| `~/.cache/clai/` | AI response cache |
| `~/.local/state/clai/clai.sock` | Unix socket |
| `~/.local/state/clai/clai.pid` | Daemon PID file |
| `~/.local/state/clai/clai.log` | Daemon log file |

Override with XDG environment variables:
- `XDG_CONFIG_HOME` (default: `~/.config`)
- `XDG_DATA_HOME` (default: `~/.local/share`)
- `XDG_CACHE_HOME` (default: `~/.cache`)
- `XDG_STATE_HOME` (default: `~/.local/state`)

## Validation

clai validates configuration on load:

```bash
# Check configuration is valid
clai doctor

# Shows:
#   [OK] Configuration - ~/.config/clai/config.yaml
# Or:
#   [ERROR] Configuration - Invalid: ai.provider must be...
```

## Next Steps

- [AI Providers](ai-providers.md) - Configure AI features
- [Shell Integration](shell-integration.md) - Shell-specific settings
- [Troubleshooting](troubleshooting.md) - Common issues
