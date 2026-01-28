# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AI Terminal is an intelligent terminal assistant that integrates Claude into your shell. It provides command suggestion extraction, auto error diagnosis, and voice input correction for Zsh, Bash, and Fish shells.

## Build Commands

```bash
make build          # Build to bin/ai-terminal
make install        # Install to $GOPATH/bin
make test           # Run all tests
make fmt            # Format code
make lint           # Run golangci-lint
make build-all      # Cross-compile for all platforms
```

Run a single test:
```bash
go test ./internal/extract -run TestExtractCommand
go test ./internal/cmd -run TestVoiceCleanCommand
```

## Architecture

### Entry Point
`cmd/ai-terminal/main.go` → `internal/cmd.Execute()` (Cobra CLI framework)

### Core Packages

**`internal/cmd/`** - Cobra command implementations
- `diagnose.go` - Error diagnosis via Claude
- `extract.go` - Command extraction from output (stdin pass-through)
- `voice.go` - Natural language to shell command conversion
- `ask.go` - Ask Claude questions with terminal context
- `init.go` - Shell integration script generator (embeds shell/ scripts)
- `daemon.go` - Background daemon management

**`internal/claude/`** - Claude CLI wrapper
- `Query()` / `QueryWithContext()` - Basic Claude queries
- `QueryFast()` - Tries daemon first, falls back to CLI
- `daemon.go` - Background daemon with Unix socket IPC, JSON protocol, 2-minute idle timeout

**`internal/extract/`** - Command extraction engine
- 5 regex patterns in priority order: backticks, install commands, prefixed (`Run:`, `Try:`), dollar-prefix (`$ cmd`), to-prefix (`To install, run:`)
- Returns last match, cleans trailing punctuation

**`internal/cache/`** - File-based caching in `~/.cache/ai-terminal`
- `suggestion` - Current command suggestion
- `last_output` - Last command output for diagnosis

### Shell Integration

Shell scripts in `internal/cmd/shell/{zsh,bash,fish}/` are embedded via `//go:embed` and output by `ai-terminal init <shell>`. They:
- Hook into post-command execution
- Display suggestions in prompt
- Handle Tab/Alt+Enter to accept suggestions
- Auto-diagnose on non-zero exit codes
- Support voice mode with `` ` `` prefix

### Environment Variables

```bash
AI_TERMINAL_AUTO_DIAGNOSE=true/false  # Auto error diagnosis
AI_TERMINAL_AUTO_EXTRACT=true/false   # Auto command extraction
AI_TERMINAL_AUTO_DAEMON=true/false    # Auto-start daemon (Zsh only)
AI_TERMINAL_CACHE=~/.cache/ai-terminal
```

## Key Patterns

- All Claude queries use `context.Context` for Ctrl+C interruptibility
- Daemon uses Unix domain sockets for IPC with JSON request/response protocol
- Version info injected via ldflags at build time (`Version`, `GitCommit`, `BuildDate`)
