# Troubleshooting

Common issues and solutions for clai.

## Diagnostic Command

```bash
clai status
```

## Hidden Diagnostics

`clai doctor` and `clai logs` are still available but hidden from the default
CLI help to keep the main command list focused.

- `clai doctor`: Run integration checks for daemon, shell hooks, and binaries.
  ```bash
  clai doctor
  ```
- `clai logs`: Show daemon log output for debugging runtime issues.
  ```bash
  clai logs --tail 200
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

## Getting Help

Collect diagnostics:

```bash
clai status
clai version
```
