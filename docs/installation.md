# Installation

This guide covers installing clai on macOS, Linux, and Windows.

## Requirements

- **Go 1.21+** (for building from source)
- **Shell**: zsh 5.8+, bash 4.4+ (or 3.2 on macOS), or PowerShell 7.x
- **Optional**: Claude CLI for AI features

## Quick Install

### macOS (Homebrew)

```bash
brew install runger/tap/clai
```

### Linux

Download the latest release for your architecture:

```bash
# AMD64
curl -L https://github.com/runger/clai/releases/latest/download/clai-linux-amd64.tar.gz | tar xz
sudo mv clai clai-shim /usr/local/bin/

# ARM64
curl -L https://github.com/runger/clai/releases/latest/download/clai-linux-arm64.tar.gz | tar xz
sudo mv clai clai-shim /usr/local/bin/
```

### Windows (PowerShell)

```powershell
# Download and extract to a directory in your PATH
Invoke-WebRequest -Uri "https://github.com/runger/clai/releases/latest/download/clai-windows-amd64.zip" -OutFile clai.zip
Expand-Archive clai.zip -DestinationPath "$env:LOCALAPPDATA\clai"

# Add to PATH (run as Administrator or add to user PATH)
$env:PATH += ";$env:LOCALAPPDATA\clai"
```

## Building from Source

```bash
git clone https://github.com/runger/clai.git
cd clai
make build

# Install binaries
sudo make install
# Or manually:
# sudo cp bin/clai bin/clai-shim /usr/local/bin/
```

## Shell Integration

After installing the binaries, set up shell integration:

```bash
# Automatic installation (detects your shell)
clai install

# Or specify shell
clai install --shell=zsh
clai install --shell=bash
```

This adds a source line to your shell's RC file (`.zshrc`, `.bashrc`, or `.bash_profile`).

**Restart your shell** or source the RC file:

```bash
source ~/.zshrc  # or ~/.bashrc
```

## Verify Installation

```bash
# Check installation
clai doctor

# View status
clai status
```

## Directory Structure

clai follows XDG Base Directory conventions:

| Location | Purpose |
| -------- | ------- |
| `~/.config/clai/` | Configuration files |
| `~/.local/share/clai/` | Database, hooks |
| `~/.cache/clai/` | Suggestion cache, AI cache |
| `~/.local/state/clai/` | Runtime files (socket, PID, logs) |

## Uninstalling

```bash
# Remove shell integration
clai uninstall

# Remove binaries (if installed via make)
sudo rm /usr/local/bin/clai /usr/local/bin/clai-shim

# Remove data (optional)
rm -rf ~/.config/clai ~/.local/share/clai ~/.cache/clai ~/.local/state/clai
```

## Next Steps

- [Quick Start](quickstart.md) - Get started with clai
- [Shell Integration](shell-integration.md) - Advanced shell configuration
- [AI Providers](ai-providers.md) - Set up AI features
