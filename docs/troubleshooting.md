# Troubleshooting

Common issues and solutions for clai.

## Diagnostic Commands

```bash
# Run full diagnostic check
clai doctor

# View daemon status
clai status

# Check daemon logs
clai logs

# View recent logs
clai logs --follow
```

## Common Issues

### Shell Integration Not Working

**Symptoms:** Suggestions don't appear, commands not logged.

**Solutions:**

1. Verify shell integration is installed:
   ```bash
   clai status
   # Look for: Shell Integration: installed
   ```

2. Check hook file exists:
   ```bash
   ls ~/.local/share/clai/hooks/
   # Should show: clai.zsh or clai.bash
   ```

3. Verify source line in RC file:
   ```bash
   grep clai ~/.zshrc  # or ~/.bashrc
   # Should show: source "...clai.zsh"
   ```

4. Reinstall:
   ```bash
   clai uninstall && clai install
   source ~/.zshrc  # restart shell
   ```

### Daemon Not Starting

**Symptoms:** `clai status` shows "not running", commands hang.

**Solutions:**

1. Check for stale socket/PID files:
   ```bash
   rm -f ~/.local/state/clai/clai.sock ~/.local/state/clai/clai.pid
   ```

2. Check logs for errors:
   ```bash
   cat ~/.local/state/clai/clai.log
   ```

3. Start daemon manually to see errors:
   ```bash
   clai daemon start --foreground
   ```

4. Check permissions:
   ```bash
   ls -la ~/.local/state/clai/
   # Should be owned by you with rwx permissions
   ```

### Suggestions Are Slow

**Symptoms:** Noticeable delay when typing.

**Solutions:**

1. Check daemon is running locally:
   ```bash
   clai status
   # Daemon should show "running"
   ```

2. Reduce suggestion timeout:
   ```bash
   clai config set client.suggest_timeout_ms 30
   ```

3. Disable AI suggestions (use history only):
   ```bash
   clai config set suggestions.max_ai 0
   ```

4. Check daemon logs for slow queries:
   ```bash
   clai logs | grep -i slow
   ```

### AI Features Not Working

**Symptoms:** Voice mode doesn't work, no AI suggestions.

**Solutions:**

1. Verify AI is enabled:
   ```bash
   clai config get ai.enabled
   # Should show: true
   ```

2. Check API key is set:
   ```bash
   clai doctor
   # Look for: [OK] Anthropic API key
   ```

3. Test API key directly:
   ```bash
   curl -H "x-api-key: $ANTHROPIC_API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        https://api.anthropic.com/v1/messages \
        -d '{"model":"claude-3-haiku-20240307","max_tokens":10,"messages":[{"role":"user","content":"Hi"}]}'
   ```

4. Check provider setting:
   ```bash
   clai config get ai.provider
   # Should match your API key (anthropic, openai, google, or auto)
   ```

### Database Issues

**Symptoms:** History not showing, errors about database.

**Solutions:**

1. Check database file:
   ```bash
   ls -la ~/.local/share/clai/clai.db
   # Should exist and have content
   ```

2. Check for corruption:
   ```bash
   sqlite3 ~/.local/share/clai/clai.db "PRAGMA integrity_check;"
   # Should show: ok
   ```

3. Reset database (loses history):
   ```bash
   rm ~/.local/share/clai/clai.db
   clai daemon stop
   # Database recreated on next command
   ```

### High Memory/CPU Usage

**Symptoms:** Daemon using excessive resources.

**Solutions:**

1. Check daemon idle timeout:
   ```bash
   clai config get daemon.idle_timeout_mins
   # Default 20 - daemon stops after idle
   ```

2. Restart daemon:
   ```bash
   clai daemon stop
   # Restarts automatically on next command
   ```

3. Check for runaway processes:
   ```bash
   ps aux | grep clai
   # Should show one daemon process
   ```

### Permission Errors

**Symptoms:** "Permission denied" errors.

**Solutions:**

1. Check directory permissions:
   ```bash
   ls -la ~/.config/clai ~/.local/share/clai ~/.local/state/clai
   # All should be owned by you
   ```

2. Fix permissions:
   ```bash
   chmod -R u+rwX ~/.config/clai ~/.local/share/clai ~/.local/state/clai
   ```

3. Check socket permissions:
   ```bash
   ls -la ~/.local/state/clai/clai.sock
   # Should be srwxr-xr-x owned by you
   ```

## Log Levels

Increase logging for debugging:

```bash
# Set debug logging
clai config set daemon.log_level debug

# Restart daemon
clai daemon stop

# View detailed logs
clai logs
```

Log levels: `debug`, `info`, `warn`, `error`

## Getting Help

### Collect Diagnostic Info

```bash
# Run diagnostics
clai doctor

# Get version
clai --version

# Get environment info
echo "Shell: $SHELL"
echo "OS: $(uname -a)"
```

### Reporting Issues

When reporting issues, include:

1. Output of `clai doctor`
2. Output of `clai --version`
3. Your shell and OS version
4. Relevant log output: `clai logs | tail -50`
5. Steps to reproduce the issue

Report issues at: https://github.com/runger/clai/issues

## FAQ

### Q: Does clai send my commands to the cloud?

Only if AI features are enabled (`ai.enabled: true`). With AI disabled, all data stays local in SQLite.

### Q: Can I use clai without AI?

Yes! AI is opt-in. clai works fully offline with history-based suggestions.

### Q: How much disk space does clai use?

Typically < 50MB:
- Database: ~10-30MB (depends on history size)
- Cache: ~5-10MB
- Binaries: ~15MB

### Q: Does clai slow down my shell?

No. Shell hooks are designed to be non-blocking:
- Command logging is fire-and-forget
- Suggestions have a 50ms timeout
- Daemon runs in background

### Q: Can I use clai on remote servers?

Yes, but:
- Install clai on the server
- AI features require API keys on the server
- Each server has its own history database

### Q: How do I completely remove clai?

```bash
# Remove shell integration
clai uninstall

# Stop daemon
clai daemon stop

# Remove binaries
sudo rm /usr/local/bin/clai /usr/local/bin/clai-shim

# Remove all data
rm -rf ~/.config/clai ~/.local/share/clai ~/.cache/clai ~/.local/state/clai
```
