# Quick Start

Get up and running with clai in a few minutes.

## 1. Install

```bash
# Build from source
make build

go build -o bin/clai-shim ./cmd/clai-shim
sudo cp bin/clai bin/clai-shim /usr/local/bin/
```

Or use Homebrew:

```bash
brew install runger/tap/clai
```

## 2. Enable Shell Integration

```bash
clai install
exec $SHELL
```

If you prefer `eval` instead of a hook file:

```bash
eval "$(clai init zsh)"
```

## 3. Verify

```bash
clai status
clai suggest "git st"
```

## 4. Basic Usage

### Suggestions

```text
git c[TAB]     # suggestion picker
```

### History

```bash
clai history
clai history git
clai history --global
```

### Toggle

```bash
clai off --session
clai on --session
```

## 5. AI Commands (Optional)

```bash
clai cmd "list files larger than 100MB"
clai ask "How do I find large files?"
```

For faster `clai cmd` calls:

```bash
clai daemon start
```

## 6. Configuration

```bash
clai config
clai config suggestions.enabled false
```

## Next Steps

- [Configuration](configuration.md)
- [Shell Integration](shell-integration.md)
- [Troubleshooting](troubleshooting.md)
