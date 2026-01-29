## Architecture

### Entry Point
`cmd/clai/main.go` → `internal/cmd.Execute()` (Cobra CLI framework)

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
- `daemon.go` - Background daemon with Unix socket IPC, JSON protocol, configurable idle timeout (default 2 hours, via `CLAI_IDLE_TIMEOUT`)

**`internal/extract/`** - Command extraction engine
- 5 regex patterns in priority order: backticks, install commands, prefixed (`Run:`, `Try:`), dollar-prefix (`$ cmd`), to-prefix (`To install, run:`)
- Returns last match, cleans trailing punctuation

**`internal/cache/`** - File-based caching in `~/.cache/clai`
- `suggestion` - Current command suggestion
- `last_output` - Last command output for diagnosis

### Shell Integration

Shell scripts in `internal/cmd/shell/{zsh,bash,fish}/` are embedded via `//go:embed` and output by `clai init <shell>`. They:
- Hook into post-command execution
- Display suggestions in prompt
- Handle Tab/Alt+Enter to accept suggestions
- Auto-diagnose on non-zero exit codes
- Support voice mode with `` ` `` prefix

### Environment Variables

```bash
CLAI_AUTO_DIAGNOSE=true/false  # Auto error diagnosis
CLAI_AUTO_EXTRACT=true/false   # Auto command extraction
CLAI_AUTO_DAEMON=true/false    # Auto-start daemon (Zsh only)
CLAI_CACHE=~/.cache/clai
CLAI_IDLE_TIMEOUT=2h           # Daemon idle timeout (default: 2h)
```
