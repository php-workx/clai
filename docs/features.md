# Features & Usage

## History-Aware Completions

clai learns from your command history and suggests commands as you type. Suggestions are ranked by:

1. **Recency** — Recently used commands appear first
2. **Context** — Commands used in the current directory are prioritized
3. **Frequency** — Commonly used commands rank higher

### How It Works

```text
git c█
  git commit -m "fix: address review feedback"  ← most recent
  git checkout main
  git cherry-pick abc123
```

Just start typing and suggestions appear. Press **Tab** to accept.

## Inline Suggestions

Like fish shell's autosuggestions, but for Zsh and Bash too. You see a dimmed preview of the suggested command as you type.

### Accepting Suggestions

| Shell | Accept Full | Accept Word |
|-------|-------------|-------------|
| Zsh   | Tab or →    | Ctrl+→      |
| Bash  | Tab         | —           |
| Fish  | Tab or →    | Alt+→       |

## Session-Based History

Each terminal tab/window maintains its own session. Commands from your current session are prioritized over global history.

This means if you're working on a git repo in one tab and npm in another, each tab gets relevant suggestions.
