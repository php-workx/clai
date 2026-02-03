# AI Integration

clai’s AI features use the **Claude CLI**. There is no hosted provider built in.

## Requirements

Install and authenticate Claude CLI:

```bash
claude login
which claude
```

## AI Commands

### Natural language → command

```bash
clai cmd "find all Python files modified today"
```

- Uses Claude CLI directly.
- The result is cached for Tab completion in `~/.cache/clai/suggestion`.

### Ask a question

```bash
clai ask "How do I find large files?"
```

- Sends the question plus working directory + shell info.
- If the response contains a command, it is cached for Tab completion.

## Speeding Up Requests

`clai cmd` can use a local **Claude CLI daemon** for faster responses:

```bash
clai daemon start
clai daemon status
```

This daemon is **only** for Claude CLI. It is separate from the history daemon.

## Notes on Privacy and Caching

- `clai cmd` and `clai ask` send input directly to Claude CLI (no automatic redaction).
- There is no response caching for these commands today.
- Configuration keys under `ai.*` exist but are **not enforced** by the current CLI.

## Troubleshooting

If AI commands fail:

```bash
which claude
claude --version
claude login
```

Also check:

```bash
clai status
```

## Next Steps

- [CLI Reference](cli-reference.md)
- [Configuration](configuration.md)
