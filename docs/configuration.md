# Configuration

clai stores configuration in `~/.clai/config.yaml`.

## Managing Configuration

```bash
# List all settings with current values
clai config

# Get a specific value
clai config suggestions.enabled

# Set a value
clai config suggestions.enabled false
```

## What Is Used Today

These settings are currently honored by the CLI and shell hooks:

- `suggestions.enabled` (checked by `clai suggest` and shell hooks)
- `suggestions.*` limits are **not** wired yet (hooks use `CLAI_MENU_LIMIT`)
- `ai.*`, `client.*`, `daemon.*`, and `privacy.*` are parsed and validated,
  but most are **not** applied by the current CLI.

If you set a value and don’t see a behavior change, it is likely reserved for
future daemon work.

## Configuration Reference

> Note: suggestion daemon transport is Unix-only in the current release (`darwin`/`linux`).
> Windows can still run CLI commands, but `claid` suggestion IPC is not supported yet.

### Daemon Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `daemon.idle_timeout_mins` | int | `0` | Idle timeout in minutes (0 = never) |
| `daemon.socket_path` | string | `""` | Override history daemon socket path |
| `daemon.log_level` | string | `"info"` | Log level: debug, info, warn, error |
| `daemon.log_file` | string | `""` | Override log file path |

```yaml
daemon:
  idle_timeout_mins: 0
  log_level: info
```

### Client Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `client.suggest_timeout_ms` | int | `50` | Max wait for suggestions (ms) |
| `client.connect_timeout_ms` | int | `10` | Socket connection timeout (ms) |
| `client.fire_and_forget` | bool | `true` | Don’t wait for logging acks |
| `client.auto_start_daemon` | bool | `true` | Auto-start history daemon |

```yaml
client:
  suggest_timeout_ms: 50
  connect_timeout_ms: 10
  fire_and_forget: true
  auto_start_daemon: true
```

### AI Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ai.enabled` | bool | `false` | Reserved (not enforced by CLI) |
| `ai.provider` | string | `"auto"` | Reserved (Claude CLI only) |
| `ai.model` | string | `""` | Reserved provider model name |
| `ai.auto_diagnose` | bool | `false` | Reserved (no auto-diagnose in CLI) |
| `ai.cache_ttl_hours` | int | `24` | Reserved for daemon cache TTL |

```yaml
ai:
  enabled: false
  provider: auto
  auto_diagnose: false
  cache_ttl_hours: 24
```

### Suggestion Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `suggestions.enabled` | bool | `true` | Enable suggestion UX |
| `suggestions.max_history` | int | `5` | Reserved (hooks use `CLAI_MENU_LIMIT`) |
| `suggestions.max_ai` | int | `3` | Reserved |
| `suggestions.show_risk_warning` | bool | `true` | Reserved |

```yaml
suggestions:
  enabled: true
  max_history: 5
  max_ai: 3
  show_risk_warning: true
```

### Privacy Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `privacy.sanitize_ai_calls` | bool | `true` | Reserved (CLI does not sanitize) |

```yaml
privacy:
  sanitize_ai_calls: true
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `CLAI_HOME` | Base directory for config, DB, hooks, logs |
| `CLAI_CACHE` | Cache directory for suggestion/last_output and Claude daemon |
| `CLAI_SOCKET` | Override history daemon socket path |
| `CLAI_DAEMON_PATH` | Override `claid` binary path |
| `CLAI_OFF` | Disable suggestions (checked by `clai suggest`) |

## Paths

**Base directory** (default `~/.clai` or `CLAI_HOME`):

| Path | Purpose |
|------|---------|
| `~/.clai/config.yaml` | Configuration file |
| `~/.clai/state.db` | SQLite history database |
| `~/.clai/hooks/` | Shell hook scripts |
| `~/.clai/logs/daemon.log` | History daemon log |
| `~/.clai/clai.sock` | History daemon socket |
| `~/.clai/clai.pid` | History daemon PID |

**Cache directory** (default `~/.cache/clai` or `CLAI_CACHE`):

| Path | Purpose |
|------|---------|
| `~/.cache/clai/suggestion` | Cached suggestion for Tab completion |
| `~/.cache/clai/last_output` | Last command output (reserved) |
| `~/.cache/clai/daemon.sock` | Claude CLI daemon socket |
| `~/.cache/clai/daemon.pid` | Claude CLI daemon PID |
| `~/.cache/clai/daemon.log` | Claude CLI daemon log |

## Next Steps

- [Shell Integration](shell-integration.md)
- [Troubleshooting](troubleshooting.md)
