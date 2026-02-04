# Troubleshooting

Common issues and solutions for clai.

## Diagnostic Commands

```bash
clai status
clai logs
clai doctor
```

## Common Issues

### Suggestions Don’t Appear

**Symptoms:** No inline suggestions or picker entries.

**Checks:**

1. Verify shell integration:
   ```bash
   clai status
   ```

2. Ensure the hook file exists:
   ```bash
   ls ~/.clai/hooks/
   ```

3. Reinstall hooks:
   ```bash
   clai uninstall && clai install
   exec $SHELL
   ```

### History Daemon Not Running

**Symptoms:** `clai status` shows daemon not running, `clai history` returns no data.

**Notes:** Session‑aware history requires the `claid` daemon. If it isn’t installed,
clai falls back to your shell history for suggestions.

**Checks:**

```bash
which claid
```

If it’s installed but not starting, remove stale files and try again:

```bash
rm -f ~/.clai/clai.sock ~/.clai/clai.pid
```

Then run any command to trigger `clai-shim` (or restart the shell).

### AI Commands Fail

**Symptoms:** `clai cmd` / `clai ask` errors.

**Checks:**

```bash
which claude
claude --version
claude login
```

For faster `clai cmd`, start the Claude CLI daemon:

```bash
clai daemon start
```

### History Database Issues

**Symptoms:** `clai history` returns nothing or errors.

**Checks:**

```bash
ls -la ~/.clai/state.db
sqlite3 ~/.clai/state.db "PRAGMA integrity_check;"
```

**Reset (loses history):**

```bash
rm ~/.clai/state.db
rm -f ~/.clai/clai.sock ~/.clai/clai.pid
```

### Logs Are Missing

`clai logs` reads `~/.clai/logs/daemon.log` (history daemon). If the file doesn’t exist,
the daemon has not started yet.

## Getting Help

Collect diagnostics:

```bash
clai status
clai doctor
clai version
```
