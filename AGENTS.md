# Agent Instructions

This file provides guidance when working with code in this repository.

## Project Overview

clai is an intelligent terminal assistant that integrates Claude into your shell. It provides command suggestion extraction and voice-to-command correction for Zsh, Bash, and Fish shells.

## Critical Rules

| Rule                          | Reason                                     |
|-------------------------------|--------------------------------------------|
| NEVER push to remote          | User pushes when ready                     |
| NEVER commit to main          | Always use feature branches                |
| conventional commits          | Alignment on commit message format         |
| TARGET = `make dev`           | Automatic via pre-commit hooks             |
| Fix failures immediately      | Don't leave broken gates for user          |
| Always commit before stopping | Don't leave work stranded locally          |
| NEVER change `make` rules     | gates protect quality and set expectations |


## Build Commands

```bash
make build          # Build to bin/clai
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

Read more in [architecture.md](docs/architecture.md)

## Key Patterns

- All Claude queries use `context.Context` for Ctrl+C interruptibility
- Daemon uses Unix domain sockets for IPC with JSON request/response protocol
- Version info injected via ldflags at build time (`Version`, `GitCommit`, `BuildDate`)

## Codebase navigation

This project uses `roam` for codebase comprehension. Always prefer roam over Glob/Grep/Read exploration.

Before modifying any code:
1. First time in the repo: `roam understand` then `roam tour`
2. Find a symbol: `roam search <pattern>`
3. Before changing a symbol: `roam preflight <name>` (blast radius + tests + fitness)
4. Need files to read: `roam context <name>` (files + line ranges, prioritized)
5. Debugging a failure: `roam diagnose <name>` (root cause ranking)
6. After making changes: `roam diff` (blast radius of uncommitted changes)

Additional: `roam health` (0-100 score), `roam impact <name>` (what breaks), `roam pr-risk` (PR risk), `roam file <path>` (file skeleton).

Run `roam --help` for all commands. Use `roam --json <cmd>` for structured output.

For more details, see `specs/roam.md`.

## Task Tracking

This project uses **bd (beads)** for ALL issue tracking.

```bash
bd ready              # Find unblocked work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync --from-main   # Sync with main branch
```

For more details, see `specs/beads.md`.
