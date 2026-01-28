# AI Terminal

Intelligent terminal integration using Claude Code CLI. Adds AI-powered features to your shell:

1. **Command Suggestion**: Extracts suggested commands from output (install commands, etc.) and lets you accept them
2. **Auto Error Diagnosis**: Automatically diagnoses failed commands using Claude
3. **Voice Input Correction**: Converts natural language (from speech-to-text) into proper terminal commands
4. **Interruptible AI**: Press Ctrl+C to cancel any AI analysis in progress

## Supported Shells

- **Zsh** (default on macOS)
- **Bash**
- **Fish**

## Requirements

- **Go 1.21+** - For building the binary
- **Claude Code CLI** - Install from: https://docs.anthropic.com/en/docs/claude-code
- A Claude Pro/Max subscription for Claude Code

## Installation

### Quick Install

```bash
# Clone the repository
git clone https://github.com/runger/ai-terminal.git
cd ai-terminal

# Run the installer
./install.sh
```

### Manual Install

```bash
# 1. Install the binary
go install github.com/runger/ai-terminal/cmd/ai-terminal@latest

# 2. Add to your shell config:

# For Zsh (~/.zshrc):
eval "$(ai-terminal init zsh)"

# For Bash (~/.bashrc):
eval "$(ai-terminal init bash)"

# For Fish (~/.config/fish/config.fish):
ai-terminal init fish | source
```

### From Source

```bash
git clone https://github.com/runger/ai-terminal.git
cd ai-terminal
make install
```

## Usage

### Command Suggestion

When a command outputs text with suggested commands (like `pip install X`), they're extracted automatically.

```bash
# Use 'run' to capture output and extract suggestions
run pro add-tab

# If a command is suggested, you'll see it in the prompt
#
# Zsh: appears in right prompt, press Tab to accept
# Fish: appears in right prompt, press Alt+Enter to accept
# Bash: shown after command, type 'accept' to run
```

**Supported patterns:**
- Commands in backticks: `` `npm install express` ``
- Install commands: `pip install requests`, `brew install wget`
- "Run:" prefixes: `Run: npm start`
- Documentation examples: `$ python app.py`

### Auto Error Diagnosis

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

### Voice Input Correction

If you use voice input (speech-to-text like [Wispr Flow](https://wisprflow.ai)), the `` ` `` prefix converts natural language to proper terminal commands:

```bash
# Just start with ` and speak naturally
`list all files in this directory
# 🎤 Converting: list all files in this directory
# → ls -la

`show me the git status
# → git status

`find all Python files
# → find . -name "*.py"

`install the requests package with pip
# → pip install requests
```

**Workflow with speech-to-text tools:**
1. Press your speech-to-text hotkey (e.g., Cmd+Ctrl for Wispr Flow)
2. Say "backtick" then your command naturally
3. Press Enter - it converts and puts the command in your buffer to review

The converted command is cached for Tab completion (Zsh) or `accept` (Bash).

You can also use the `voice` command directly:
```bash
voice "list all files"
```

### Commands

| Command | Description |
|---------|-------------|
| `ai-fix` | Manually diagnose the last failed command |
| `ai-fix "cmd"` | Diagnose a specific command |
| `ai "question"` | Ask Claude anything with terminal context |
| `voice "text"` | Convert natural language to terminal command |
| `ai-toggle` | Turn auto-diagnosis on/off |
| `run <cmd>` | Run command with output capture |
| `accept` | (Bash only) Accept and run the suggested command |

### CLI Commands

The `ai-terminal` binary also provides direct CLI access:

```bash
ai-terminal diagnose "npm run build" 1    # Diagnose a command
ai-terminal extract < output.txt          # Extract suggestions from file
ai-terminal ask "How do I find large files?"
ai-terminal voice "list all python files" # Convert voice/natural language to command
ai-terminal init zsh                      # Output shell integration script
ai-terminal version                       # Show version info
```

### Interruptible AI Analysis

All AI operations support Ctrl+C to cancel:

```bash
# If AI analysis is taking too long, just press Ctrl+C
$ ai-fix
⚡ Analyzing error...
^C
Analysis cancelled
```

This is especially useful when auto-diagnosis kicks in but you've already spotted the issue yourself.

## Configuration

### Terminal Emulator Setup (Optional)

If you want a dedicated hotkey for voice mode (instead of the `?` prefix), you can configure your terminal to send a special sequence:

**Ghostty** (`~/.config/ghostty/config`):
```
keybind = super+ctrl+v=text:\x18\x16
```
This maps Cmd+Ctrl+V to send Ctrl+X Ctrl+V, which enters voice mode.

**iTerm2** (Preferences → Keys → Key Bindings):
- Add new binding: `⌘⌃V` → Send Escape Sequence → `[24~`
- Then add to your shell config: `bindkey '\e[24~' _ai_enter_voice_mode`

### Environment Variables

Set these environment variables **before** the init line in your shell config:

```bash
# Disable auto-diagnosis (default: true)
export AI_TERMINAL_AUTO_DIAGNOSE=false

# Disable command extraction (default: true)
export AI_TERMINAL_AUTO_EXTRACT=false

# Custom cache directory (default: ~/.cache/ai-terminal)
export AI_TERMINAL_CACHE=~/.cache/ai-terminal

# Then init
eval "$(ai-terminal init zsh)"
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Shell Integration Layer                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐          │
│  │ Zsh hooks   │  │ Bash hooks  │  │ Fish hooks  │          │
│  │ + ZLE       │  │ + DEBUG     │  │ + events    │          │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘          │
│         └────────────────┼────────────────┘                 │
│                          ▼                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  ai-terminal (Go binary)                              │  │
│  │  - diagnose: Error analysis                           │  │
│  │  - extract: Command extraction                        │  │
│  │  - ask: AI questions                                  │  │
│  │  - init: Shell script generation                      │  │
│  └─────────────────────────┬─────────────────────────────┘  │
│                            ▼                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  claude --print (Claude Code CLI)                     │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## File Structure

```
ai-terminal/
├── cmd/ai-terminal/        # Main entry point
├── internal/
│   ├── cmd/                # Cobra commands
│   │   ├── shell/          # Embedded shell scripts
│   │   │   ├── zsh/
│   │   │   ├── bash/
│   │   │   └── fish/
│   │   ├── root.go
│   │   ├── diagnose.go
│   │   ├── extract.go
│   │   ├── ask.go
│   │   └── init.go
│   ├── cache/              # Cache management
│   └── claude/             # Claude CLI wrapper
├── shell/                  # Source shell scripts (for reference)
├── go.mod
├── Makefile
├── install.sh
└── README.md
```

## Building

```bash
# Build binary
make build

# Install to $GOPATH/bin
make install

# Run tests
make test

# Build for all platforms
make build-all

# Show all targets
make help
```

## Troubleshooting

### "claude: command not found"

Install Claude Code CLI:
```bash
npm install -g @anthropic-ai/claude-code
```

### "ai-terminal: command not found"

Make sure `$GOPATH/bin` (or `$HOME/go/bin`) is in your PATH:
```bash
export PATH="$PATH:$HOME/go/bin"
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

Or alias your common commands in your shell config:
```bash
alias pip='run pip'
alias npm='run npm'
```

## License

MIT - Do whatever you want with it!
