# CLAUDE.md

clai is an intelligent terminal assistant that integrates Claude into your shell. It provides command suggestion extraction and voice-to-command correction for Zsh, Bash, and Fish shells.

Always read @AGENTS.md for agent instructions.

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
