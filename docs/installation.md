# Installation

This guide covers installing clai on macOS, Linux, and Windows.

## Requirements

- **Go 1.21+** (for building from source)
- **Shell**: zsh 5.8+, bash 4.4+ (or 3.2 on macOS), or fish 3.0+
- **Optional**: Claude CLI for `clai cmd` / `clai ask`

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
Invoke-WebRequest -Uri "https://github.com/runger/clai/releases/latest/download/clai-windows-amd64.zip" -OutFile clai.zip
Expand-Archive clai.zip -DestinationPath "$env:LOCALAPPDATA\clai"
$env:PATH += ";$env:LOCALAPPDATA\clai"
```

## Building from Source

```bash
git clone https://github.com/runger/clai.git
cd clai
make build

go build -o bin/clai-shim ./cmd/clai-shim
sudo cp bin/clai bin/clai-shim /usr/local/bin/
```

### History Daemon (`claid`)

Session‑aware history and suggestions use a separate daemon binary named `claid`.
`clai-shim` will try to spawn it automatically if it is on your `PATH` or pointed
at via `CLAI_DAEMON_PATH`.

`claid` suggestion transport is currently Unix-only (`darwin`/`linux`). On Windows,
`clai` commands still work, but daemon-backed suggestions are not available yet.

## Shell Integration

After installing the binaries, set up shell integration:

```bash
clai install
clai install --shell=zsh
clai install --shell=bash
clai install --shell=fish
```

This writes hooks to `~/.clai/hooks/` and adds a source line to your rc file.

**Restart your shell** or source the rc file:

```bash
source ~/.zshrc  # or ~/.bashrc
```

## Verify Installation

```bash
clai status
clai suggest "git st"
```

## Directory Structure

**Base directory** (default `~/.clai`):

| Location | Purpose |
| -------- | ------- |
| `~/.clai/config.yaml` | Configuration file |
| `~/.clai/state.db` | SQLite database |
| `~/.clai/hooks/` | Shell hook scripts |
| `~/.clai/logs/` | History daemon log files |
| `~/.clai/clai.sock` | History daemon socket |
| `~/.clai/clai.pid` | History daemon PID |

**Cache directory** (default `~/.cache/clai`):

| Location | Purpose |
| -------- | ------- |
| `~/.cache/clai/suggestion` | Cached suggestion |
| `~/.cache/clai/last_output` | Last output (reserved) |
| `~/.cache/clai/daemon.*` | Claude CLI daemon files |

Set `CLAI_HOME` or `CLAI_CACHE` to override these locations.

## Uninstalling

```bash
clai uninstall
sudo rm /usr/local/bin/clai /usr/local/bin/clai-shim
rm -rf ~/.clai ~/.cache/clai
```

## Next Steps

- [Quick Start](quickstart.md)
- [Shell Integration](shell-integration.md)
