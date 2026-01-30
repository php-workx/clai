# AI Integration

clai uses the Claude CLI for intelligent features like voice-to-command, error diagnosis, and smart suggestions.

## Requirements

To use AI features, you need the [Claude CLI](https://claude.ai/cli) installed and authenticated:

```bash
# Install Claude CLI (see https://claude.ai/cli)
# Then authenticate:
claude login

# Verify installation
which claude

# Enable AI features in clai
clai config ai.enabled true

# Verify
clai doctor  # Should show [OK] Claude CLI
```

## AI Features

### Voice-to-Command

Convert natural language to shell commands:

```text
`show disk usage by folder
# Suggests: du -sh */ | sort -h

`find python files modified today
# Suggests: find . -name "*.py" -mtime 0

`compress all logs older than 7 days
# Suggests: find /var/log -name "*.log" -mtime +7 -exec gzip {} \;
```

### Error Diagnosis

Get explanations for command failures:

```bash
# Enable auto-diagnosis
clai config ai.auto_diagnose true

# Or use the run wrapper
run npm install
# On failure, explains the error and suggests fixes
```

### Smart Suggestions

AI-powered command suggestions based on context:

```bash
# Enable AI suggestions
clai config ai.enabled true
clai config suggestions.max_ai 3

# Now suggestions include AI-generated options
```

## Response Caching

AI responses are cached to reduce latency:

```bash
# Set cache duration (hours)
clai config ai.cache_ttl_hours 24

# Cache location
ls ~/.cache/clai/
```

Caching behavior:
- Same natural language query returns cached response
- Same error diagnosis returns cached response
- Cache is pruned automatically (hourly and on daemon start)

## Privacy & Sanitization

By default, clai sanitizes sensitive data before sending to Claude:

```bash
# Check sanitization status
clai config privacy.sanitize_ai_calls

# Disable (not recommended)
clai config privacy.sanitize_ai_calls false
```

Sanitization removes:
- API keys and tokens (patterns like `sk-...`, `token=...`)
- Passwords in URLs
- AWS credentials
- Private keys
- Email addresses (optional)

## Troubleshooting

### Claude CLI Not Found

```bash
# Check Claude CLI is installed
which claude

# If not installed, see https://claude.ai/cli
```

### Not Authenticated

```bash
# Run Claude login
claude login

# Verify
claude --version
```

### Network Issues

clai has timeouts to prevent hanging:
- AI calls timeout after 30 seconds
- Failed calls return gracefully (shell continues working)

```bash
# Check daemon logs for errors
clai logs | grep -i error
```

## Next Steps

- [Configuration](configuration.md) - All configuration options
- [Shell Integration](shell-integration.md) - Shell features
- [Troubleshooting](troubleshooting.md) - Common issues
