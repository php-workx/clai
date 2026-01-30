# AI Providers

clai supports multiple AI providers for intelligent features like voice-to-command, error diagnosis, and smart suggestions.

## Supported Providers

| Provider | Environment Variable | Models |
|----------|---------------------|--------|
| Anthropic | `ANTHROPIC_API_KEY` | claude-3-haiku, claude-3-sonnet, claude-3-opus |
| OpenAI | `OPENAI_API_KEY` | gpt-4o-mini, gpt-4o, gpt-4-turbo |
| Google | `GOOGLE_API_KEY` | gemini-1.5-flash, gemini-1.5-pro |
| Claude CLI | (uses Claude CLI auth) | Configured in Claude CLI |

## Quick Setup

### 1. Get an API Key

**Anthropic (Recommended):**
1. Go to [console.anthropic.com](https://console.anthropic.com)
2. Create an account and add credits
3. Generate an API key

**OpenAI:**
1. Go to [platform.openai.com](https://platform.openai.com)
2. Create an account and add credits
3. Generate an API key

**Google:**
1. Go to [makersuite.google.com](https://makersuite.google.com)
2. Create a project
3. Generate an API key

### 2. Set Environment Variable

Add to your shell RC file (`~/.zshrc` or `~/.bashrc`):

```bash
# Anthropic
export ANTHROPIC_API_KEY="sk-ant-api03-..."

# Or OpenAI
export OPENAI_API_KEY="sk-..."

# Or Google
export GOOGLE_API_KEY="AIza..."
```

### 3. Enable AI Features

```bash
# Enable AI
clai config set ai.enabled true

# Optionally set provider explicitly
clai config set ai.provider anthropic

# Verify
clai doctor
```

## Provider Configuration

### Auto-Detection (Default)

When `ai.provider` is set to `"auto"` (default), clai checks for API keys in order:

1. `ANTHROPIC_API_KEY` → Uses Anthropic
2. `OPENAI_API_KEY` → Uses OpenAI
3. `GOOGLE_API_KEY` → Uses Google
4. Claude CLI installed → Uses Claude CLI

### Explicit Provider

```bash
# Set specific provider
clai config set ai.provider anthropic
clai config set ai.provider openai
clai config set ai.provider google
```

### Custom Model

```bash
# Use a specific model
clai config set ai.model claude-3-sonnet-20240229
clai config set ai.model gpt-4o
clai config set ai.model gemini-1.5-pro
```

## Using Claude CLI

If you have the [Claude CLI](https://claude.ai/cli) installed, clai can use it without an API key:

```bash
# Install Claude CLI (if not installed)
# See: https://claude.ai/cli

# Authenticate
claude login

# clai will auto-detect and use Claude CLI
clai config set ai.enabled true
clai doctor  # Should show [OK] Claude CLI
```

## AI Features

### Voice-to-Command

Convert natural language to shell commands:

```
$ `show disk usage by folder
# Suggests: du -sh */ | sort -h

$ `find python files modified today
# Suggests: find . -name "*.py" -mtime 0

$ `compress all logs older than 7 days
# Suggests: find /var/log -name "*.log" -mtime +7 -exec gzip {} \;
```

### Error Diagnosis

Get explanations for command failures:

```bash
# Enable auto-diagnosis
clai config set ai.auto_diagnose true

# Or use the run wrapper
run npm install
# On failure, explains the error and suggests fixes
```

### Smart Suggestions

AI-powered command suggestions based on context:

```bash
# Enable AI suggestions
clai config set ai.enabled true
clai config set suggestions.max_ai 3

# Now suggestions include AI-generated options
```

## Response Caching

AI responses are cached to reduce API calls and latency:

```bash
# Set cache duration (hours)
clai config set ai.cache_ttl_hours 24

# Cache location
ls ~/.cache/clai/
```

Caching behavior:
- Same natural language query → returns cached response
- Same error diagnosis → returns cached response
- Cache is pruned automatically (hourly and on daemon start)

## Privacy & Sanitization

By default, clai sanitizes sensitive data before sending to AI:

```bash
# Check sanitization status
clai config get privacy.sanitize_ai_calls

# Disable (not recommended)
clai config set privacy.sanitize_ai_calls false
```

Sanitization removes:
- API keys and tokens (patterns like `sk-...`, `token=...`)
- Passwords in URLs
- AWS credentials
- Private keys
- Email addresses (optional)

## Cost Considerations

AI providers charge per token. clai is designed to minimize costs:

| Feature | Typical Tokens | Frequency |
|---------|---------------|-----------|
| Voice-to-command | ~500 | Per query |
| Error diagnosis | ~1000 | Per failure |
| Smart suggestions | ~300 | Per request |

Tips to reduce costs:
- Use `claude-3-haiku` or `gpt-4o-mini` (cheapest models)
- Increase cache TTL
- Disable auto-diagnosis if not needed
- Use history-based suggestions (free) over AI suggestions

```bash
# Use cheaper model
clai config set ai.model claude-3-haiku-20240307

# Increase cache duration
clai config set ai.cache_ttl_hours 48

# Reduce AI suggestions
clai config set suggestions.max_ai 1
```

## Troubleshooting

### API Key Not Detected

```bash
# Check environment variable is set
echo $ANTHROPIC_API_KEY

# Verify in clai
clai doctor
# Should show: [OK] Anthropic API key - Set in environment
```

### Rate Limiting

If you hit rate limits:
- Increase cache TTL
- Use a cheaper model (often higher limits)
- Check your API usage dashboard

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
