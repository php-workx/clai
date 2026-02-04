# Features & Usage

## History‑Aware Suggestions

As you type, clai suggests commands based on your history.

- **Session‑aware** when the history daemon (`claid`) is running.
- **Fallback** to your shell’s history file when the daemon is unavailable.

```
git c█
  git commit -m "fix: address review feedback"
  git checkout main
  git cherry-pick abc123
```

## Inline Suggestions (Zsh)

Zsh shows a ghost‑text suggestion while typing.

- **Right Arrow** accepts the full suggestion
- **Alt+Right** accepts the next token

## Suggestion & History Pickers

- **Tab**: open the suggestion picker
- **Up Arrow**: open the history picker

These are available across zsh/bash/fish (with shell‑specific behavior).

## Natural Language → Command

Use `clai cmd` to turn plain English into a shell command:

```bash
clai cmd "list files larger than 100MB"
```

## Ask Claude

Use `clai ask` for short, contextual terminal questions:

```bash
clai ask "What’s the difference between grep and ripgrep?"
```

## Toggle Suggestions

```bash
clai off
clai on
clai off --session
```

## Not Yet Available

The shell hooks include placeholders for:

- Voice mode
- Error diagnosis
- Output extraction

These commands are not shipped in the current CLI.

## Next Steps

- [Quick Start](quickstart.md)
- [Shell Integration](shell-integration.md)
- [CLI Reference](cli-reference.md)
