# Getting Started

## Requirements

- macOS or Linux
- Zsh, Bash, or Fish shell

## Installation

### Homebrew (Recommended)

```bash
brew install runger/tap/clai
```

### From Source

```bash
git clone https://github.com/runger/clai.git
cd clai
make install
```

### Go Install

```bash
go install github.com/runger/clai/cmd/clai@latest
```

## Shell Setup

Add clai to your shell configuration:

### Zsh

```bash
echo 'eval "$(clai init zsh)"' >> ~/.zshrc
source ~/.zshrc
```

### Bash

```bash
echo 'eval "$(clai init bash)"' >> ~/.bashrc
source ~/.bashrc
```

### Fish

```bash
echo 'clai init fish | source' >> ~/.config/fish/config.fish
source ~/.config/fish/config.fish
```

## Verify Installation

```bash
clai version
```

You should see version information. Now start typing commands and watch the suggestions appear!
