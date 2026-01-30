# clai

Your shell, with smart completions — supercharged with AI.

<!-- TODO: Add demo GIF here
     Record with: asciinema rec demo.cast && agg demo.cast demo.gif
     Show: typing "git c" and getting history-based suggestions
-->

Start typing and get intelligent suggestions from your command history.
No more ↑↑↑ scrolling or trying to remember that complex kubectl command.

## ✨ Features

📜 **History-Aware Completions** — Suggestions from your actual command history as you type

🎯 **Inline Suggestions** — See what you'll run before you hit Enter (like fish, but everywhere)

🐚 **Works Everywhere** — Zsh, Bash, and Fish support

### Coming Soon

🎤 Voice-to-Command • 🔧 AI Error Diagnosis • 💡 Smart Output Extraction

## 🚀 Quick Start

```bash
# Install
brew tap runger/clai && brew install clai

# Add to your shell config (pick one)
echo 'eval "$(clai init zsh)"' >> ~/.zshrc
echo 'eval "$(clai init bash)"' >> ~/.bashrc

# Restart your shell — done!
```

## 💡 Example

```
$ git c█
  git commit -m "fix: address review feedback"  ← from your history
  git checkout main
  git cherry-pick abc123
```

Type a few characters, get smart suggestions ranked by recency and context.
Press **Tab** to complete.

## 📚 Documentation

- [Getting Started](docs/getting-started.md)
- [Features & Usage](docs/features.md)
- [Configuration](docs/configuration.md)
- [CLI Reference](docs/cli-reference.md)
- [Troubleshooting](docs/troubleshooting.md)

## Requirements

- macOS or Linux
- Zsh, Bash, or Fish

## License

MIT
