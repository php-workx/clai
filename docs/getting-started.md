# Getting Started

## Requirements

- macOS or Linux
- Zsh, Bash, or Fish
- Optional: Claude CLI for `clai cmd` / `clai ask`

## Installation

### Homebrew (Recommended)

```bash
brew install runger/tap/clai
```

### From Source

```bash
git clone https://github.com/runger/clai.git
cd clai
make build

go build -o bin/clai-shim ./cmd/clai-shim
sudo cp bin/clai bin/clai-shim /usr/local/bin/
```

## Shell Setup

### Automatic (writes hook file)

```bash
clai install
```

### Manual (eval in rc file)

```bash
eval "$(clai init zsh)"
# or
clai init fish | source
```

`clai init` generates a session ID each time it’s evaluated.

## Verify Installation

```bash
clai status
clai suggest "git st"
```

## Optional: AI Commands

```bash
clai cmd "list large files"
clai ask "How do I find large files?"
```

If you want faster `clai cmd` calls:

```bash
clai daemon start
```
