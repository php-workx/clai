# AI Terminal

Intelligent terminal integration using Claude Code CLI. Adds two main features:

1. **Command Suggestion**: Extracts suggested commands from output (install commands, etc.) and lets you accept them with Tab
2. **Auto Error Diagnosis**: Automatically diagnoses failed commands using Claude

## Requirements

- **ZSH** (default on macOS)
- **Claude Code CLI** - Install from: https://docs.anthropic.com/en/docs/claude-code
- A Claude Pro/Max subscription for Claude Code

## Installation

### 1. Clone/Copy to your projects folder

```bash
# Option A: If you downloaded the zip
unzip ai-terminal.zip -d ~/projects/

# Option B: Copy manually
mkdir -p ~/projects/ai-terminal
# ... copy files here
```

### 2. Make scripts executable

```bash
chmod +x ~/projects/ai-terminal/bin/*
```

### 3. Add to your .zshrc

Add this single line to your `~/.zshrc`:

```bash
source ~/projects/ai-terminal/zsh/ai-terminal.zsh
```

### 4. Reload your shell

```bash
source ~/.zshrc
# or just open a new terminal
```

## Usage

### Command Suggestion (Feature 1)

When a command outputs text with suggested commands (like `pip install X`), they're extracted automatically.

```bash
# Use 'run' to capture output and extract suggestions
run pro add-tab

# If a command is suggested, you'll see it in the right prompt:
#                                    → pip install pro-tabs
# 
# Press Tab to accept and fill in the command
# Press Escape to dismiss
```

**Supported patterns:**
- Commands in backticks: `` `npm install express` ``
- Install commands: `pip install requests`, `brew install wget`
- "Run:" prefixes: `Run: npm start`
- Documentation examples: `$ python app.py`

### Auto Error Diagnosis (Feature 2)

When a command fails (non-zero exit code), Claude automatically analyzes it:

```bash
$ npm run biuld
npm ERR! Missing script: "biuld"

⚡ Analyzing error...
━━━ AI Diagnosis ━━━
**Problem**: Typo in script name - "biuld" should be "build"
**Fix**: `npm run build`
━━━━━━━━━━━━━━━━━━━━
```

### Manual Commands

| Command | Description |
|---------|-------------|
| `ai-fix` | Manually diagnose the last failed command |
| `ai-fix "cmd"` | Diagnose a specific command |
| `ai "question"` | Ask Claude anything with terminal context |
| `ai-toggle` | Turn auto-diagnosis on/off |
| `run <cmd>` | Run command with output capture |

## Configuration

Set these in your `.zshrc` **before** sourcing `ai-terminal.zsh`:

```bash
# Disable auto-diagnosis (default: true)
export AI_TERMINAL_AUTO_DIAGNOSE=false

# Disable command extraction (default: true)  
export AI_TERMINAL_AUTO_EXTRACT=false

# Custom paths (usually auto-detected)
export AI_TERMINAL_BIN=~/projects/ai-terminal/bin
export AI_TERMINAL_CACHE=~/.cache/ai-terminal

# Then source the integration
source ~/projects/ai-terminal/zsh/ai-terminal.zsh
```

## File Structure

```
ai-terminal/
├── README.md           # This file
├── bin/
│   ├── ai-diagnose     # Error diagnosis script (calls Claude)
│   └── ai-extract      # Command extraction script
└── zsh/
    └── ai-terminal.zsh # Main ZSH integration (source this)
```

## How It Works

### Bash vs ZSH Explained

- **`bin/` scripts**: Standalone executables. Written in Bash but work fine when called from ZSH. They're separate processes.

- **`zsh/ai-terminal.zsh`**: Sourced directly into your ZSH session. Uses ZSH-specific features like `zle` (line editor), `add-zsh-hook`, and ZSH keybindings. This is why it must be ZSH.

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Your ZSH Shell                                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │  ai-terminal.zsh (sourced)                        │  │
│  │  - Hooks: preexec, precmd                         │  │
│  │  - Keybindings: Tab, Escape                       │  │
│  │  - Functions: ai-fix, ai, run                     │  │
│  └─────────────────┬─────────────────────────────────┘  │
│                    │ calls                              │
│  ┌─────────────────▼─────────────────────────────────┐  │
│  │  bin/ai-diagnose    bin/ai-extract                │  │
│  │  (separate processes)                             │  │
│  └─────────────────┬─────────────────────────────────┘  │
│                    │ calls                              │
│  ┌─────────────────▼─────────────────────────────────┐  │
│  │  claude --print (Claude Code CLI)                 │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Troubleshooting

### "claude: command not found"

Install Claude Code CLI:
```bash
# Check if installed
which claude

# Install (see Anthropic docs for latest)
npm install -g @anthropic-ai/claude-code
```

### Auto-diagnosis is annoying

Toggle it off:
```bash
ai-toggle
# or
export AI_TERMINAL_AUTO_DIAGNOSE=false
```

### Suggestions not appearing

Make sure to use `run` prefix:
```bash
run pip install nonexistent-package
```

Or alias your common commands in `.zshrc`:
```bash
alias pip='run pip'
alias npm='run npm'
```

## Extending

### Add support for more patterns

Edit `bin/ai-extract` and add new grep patterns in the extraction section.

### Add support for OpenAI Codex

Modify `bin/ai-diagnose` to use `codex` instead of `claude`:
```bash
# Replace this line:
echo "$PROMPT" | claude --print
# With:
echo "$PROMPT" | codex --print
```

## License

MIT - Do whatever you want with it!
