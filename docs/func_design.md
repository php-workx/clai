# clai — Architecture & Design

**Version:** 0.4 (Phase 1 Ready)

> Living document. Captures decisions, scope, open questions, and design intent.
> Update this as clai evolves.

---

## 1. Overview & Goals

clai is a universal intelligence layer for the command line. It augments existing shells and terminal emulators with:

- Session-based (tab-based) command history
- Smart command suggestions and completions
- Text-to-command assistance
- Generic error diagnosis (exit-code based)
- (Phase 2) Deep error diagnosis and command extraction

### Non-Goals

- clai is not a terminal emulator (in Phase 1)
- clai is not a shell replacement
- clai never auto-executes commands

---

## 2. UX Principles

- Install once, then it just works
- No need to type `clai ...` during normal usage
- Intelligence must never block typing
- AI is assistive, not authoritative
- Suggestions are inserted, never executed
- Safe-by-default: Zero interference with shell streams

---

## 3. Phase Plan

### Phase 1 — Internal / Personal Launch (The "Safe Companion")

**Primary goal:** High-utility history and suggestions without risking terminal stability.

**Scope:**

- zsh, bash, PowerShell support via native hooks
- User-mode lazy-start daemon (no system services)
- Session-based history (one shell = one session)
- History-based suggestions (Session > CWD > Global)
- AI-based text-to-command assistance (on-demand)
- AI-based "Next Step" prediction (post-command suggestions)
- Generic diagnosis (explaining exit codes, no error text reading)
- Offline mode (no AI)
- Basic redaction for AI calls (regex-based sanitization)

**Out of scope (Deferred to Phase 2):**

- Reading stdout/stderr (Output Capture)
- Deep error diagnosis (requires error text)
- Command extraction from output
- SSH / remote intelligence
- PTY Wrapper architecture

---

### Phase 2 — The "Intelligence Layer"

**Scope extensions:**

- PTY Wrapper architecture (`clai-shell`)
- Full output capture (stdout/stderr)
- Deep AI diagnosis (reading specific error messages)
- Extracting runnable commands from logs/outputs
- Remote session support (SSH wrapper)

---

## 4. High-Level Architecture

clai consists of four main components (Phase 1):

1. **Shell Integration**
   - zsh, bash, PowerShell hooks
   - Fire-and-forget event emission (non-blocking)

2. **Thin Client Binary (`clai-shim`)**
   - Lightweight compiled binary (Go or Rust)
   - Bridges shell hooks to daemon via gRPC
   - Handles daemon spawning if not running

3. **User-Mode Lazy-Start Daemon (`clai-daemon`)**
   - Central brain
   - Manages sessions, history, suggestions, AI calls
   - Single writer to SQLite database

4. **Provider Adapters**
   - Wraps AI provider CLIs (e.g., `claude`, `openai`, `gemini`)
   - Falls back to direct API calls if CLI unavailable
   - Streaming + cancellation support

5. **Local State Store**
   - SQLite with WAL mode
   - Session + global indexes

All intelligence and storage live in the daemon. Shells remain thin clients.

---

## 5. Daemon Design

- Runs in user mode only
- No launchd / systemd / Windows services
- Lazy-started on first shell interaction
- Communicates via local IPC (Unix socket / named pipe)
- Single instance per user
- Auto-exits after configurable idle timeout (default: 20 minutes)

### Daemon Responsibilities

- Session lifecycle tracking
- History storage and indexing
- Suggestion ranking
- AI request orchestration
- Sanitization and safety policies

---

## 6. Session & Terminal Model

### Sessions

- One interactive shell instance = one session
- Session ID (UUID v4) generated at shell startup
- Typically maps 1:1 with terminal tabs/panes
- Session metadata includes:
  - Shell type (zsh, bash, pwsh)
  - OS (darwin, linux, windows)
  - Start time
  - Initial CWD
  - Hostname
  - Username

### Command Boundaries

**Phase 1 approach:**

- Use shell hooks for reliability (Observer only)
- zsh/bash: `preexec` / `precmd`
- PowerShell: PSReadLine hooks

**Captured per command:**

- Raw command buffer
- Working directory
- Exit code
- Execution duration

---

## 7. Output Capture Policy

### Phase 1 Policy: Zero Capture

- We do NOT capture stdout or stderr
- We do NOT use pipes or redirections
- We rely solely on command string, exit code, and duration

**Rationale:** Capturing output via hooks requires modifying file descriptors, which risks breaking interactive TUI apps (vim, less, ssh). Phase 2 will introduce a PTY wrapper to do this safely.

---

## 8. Suggestions & Completions

### Suggestion Sources (Phase 1)

- Current session history
- Directory / repo-scoped history
- Global history
- AI-generated suggestions (on explicit trigger)

### Ranking Heuristics

1. **Source Priority:** Session > CWD > Global
2. **Recency Weighting:** More recent commands score higher
3. **Success Bias:** Prefer commands with exit code 0
4. **Tool Affinity:** Commands using the same tool as previous command score higher (e.g., `git` after `git`)

### Suggestion Limit

- History-based: Up to 5 suggestions
- AI-generated: Up to 3 suggestions

### Completions

Augment native shell completion with history-based suggestions only.

---

## 9. Suggestion UX

### Display Mechanism

**zsh/bash:**
- Inline ghost text (dimmed) showing top suggestion
- User presses `→` (Right Arrow) or `Tab` to accept
- User presses `↑`/`↓` to cycle through alternatives
- Implemented via ZLE widgets (zsh) / readline (bash)

**PowerShell:**
- PSReadLine predictive IntelliSense integration
- Uses native `Set-PSReadLineOption -PredictionSource`

### Acceptance Behavior

- Accepting a suggestion inserts the text at cursor
- Command is NOT executed automatically
- User must press Enter to execute

### Explicit AI Trigger

- User types natural language and presses a hotkey (default: `Ctrl+G`)
- Ghost text is replaced with AI-generated command
- Or user invokes `clai ask "natural language prompt"`

---

## 10. AI Capabilities

### Phase 1

AI is used for:

- **Text-to-Command:** Convert natural language to shell commands (up to 3 suggestions)
- **Next Step Prediction:** After a command completes successfully (exit 0), predict what the user might run next based on context (e.g., after `git add .`, suggest `git commit -m "..."`). Triggered automatically as ghost text, not on failures.
- **Generic Diagnosis:** On command failure (non-zero exit), explain exit codes without reading error output (e.g., "Exit 127 means command not found"). Triggered automatically if `ai.auto_diagnose = true`, otherwise on explicit request.

### AI Context Window

When requesting AI help, the daemon provides:

- Current working directory
- User OS and shell type
- Last 10 commands with exit codes
- Current typing buffer (for text-to-command)

**Excluded from AI context:**

- Environment variables
- Command outputs (stdout/stderr)
- File contents

### AI Usage Rules

- Never auto-execute
- Async only (never block typing)
- Triggered explicitly (hotkey or command) or automatically on failure (if enabled)

---

## 11. Trust & Safety

### Guaranteed

- Never auto-execute commands
- Insert-only behavior
- Offline mode available
- Explicit AI triggers

### Phase 1 Safety

- **Regex-based sanitization:** Strip obvious secrets before AI calls (AWS keys, JWT tokens, PEM blocks, password= patterns)
- **Basic risk tagging:** Flag destructive patterns (`rm -rf`, `DROP TABLE`, `--force`) in suggestions
- **Local-only history:** Command history never leaves the machine (AI calls receive sanitized input)

> **Note:** Entropy-based token detection was considered but rejected as it produces too many false positives without meaningful security benefit. See Decision Log.

### Phase 2 Safety

- Advanced risk scoring
- Explicit confirmation flows for destructive commands

---

## 12. Data Model & Storage

### Stored

- Full command lines (raw and normalized)
- Working directory
- Exit code + duration
- Session metadata (shell, OS, hostname, user)

### Not Stored (Phase 1)

- Command outputs (stdout/stderr)
- Extracted commands
- Environment variables

### Data Retention

- Session data: Retained indefinitely (user can purge manually)
- AI cache: Expires after 24 hours
- Future: Configurable retention policies

### Storage

- SQLite database (`~/.clai/state.db`)
- WAL mode enabled for concurrent reads
- Session-scoped and global indexes

---

## 13. Performance Constraints

**Targets:**

| Operation | Target Latency |
|-----------|----------------|
| Shell hook execution | < 5ms |
| History-based suggestions | < 50ms |
| AI suggestions | Async (non-blocking) |
| Daemon startup | < 100ms |

- Shell startup: Zero perceived latency (fire-and-forget init)
- Daemon CPU/memory usage capped

---

## 14. Cross-Shell Support Matrix

### Phase 1

| Shell | History | Suggestions | AI Triggers | Notes |
|-------|---------|-------------|-------------|-------|
| zsh | ✅ Full | ✅ Full | ✅ Full | ZLE widget integration |
| bash | ✅ Full | ✅ Full | ✅ Full | readline integration |
| PowerShell | ✅ Full | ✅ Full | ✅ Full | PSReadLine hooks |

### Later

- fish (Phase 2)
- Other shells (best effort)

---

## 15. Remote Sessions (Future)

**Phase 2 goals:**

- Local wrapper around ssh
- No remote installation required
- Optional remote context probing (read-only)

---

## 16. Configuration & CLI

### CLI Commands (User-Facing)

| Command | Description |
|---------|-------------|
| `clai install` | Add hooks to shell rc files |
| `clai uninstall` | Remove hooks from shell rc files |
| `clai status` | Show daemon status, session info |
| `clai doctor` | Diagnose installation issues |
| `clai logs [-f]` | View daemon logs (optionally follow) |
| `clai config [key] [value]` | View or modify configuration |
| `clai ask "prompt"` | Text-to-command (explicit invocation) |
| `clai history [query]` | Search command history |
| `clai update` | Update clai binaries |
| `clai daemon start` | Manually start daemon (for debugging) |
| `clai daemon stop` | Stop daemon |

> **Note:** Shell hooks internally call `clai-shim` (the thin client binary) for performance-critical operations like `log-start`, `log-end`, `suggest`, and `text-to-command`. Users interact with the `clai` CLI; shell hooks use `clai-shim` transparently.

### Configuration File

Location: `~/.clai/config.toml` (Unix) or `%APPDATA%\clai\config.toml` (Windows)

```toml
[daemon]
idle_timeout_mins = 20          # Auto-shutdown after idle (0 = never)
log_level = "info"              # debug, info, warn, error

[client]
suggest_timeout_ms = 50         # Max wait for suggestions
fire_and_forget = true          # Don't wait for logging acks

[ai]
enabled = false                 # Must opt-in to AI features
provider = "auto"               # anthropic, openai, google, or "auto"
model = ""                      # Provider-specific model (empty = default)
auto_diagnose = false           # Auto-trigger diagnosis on non-zero exit
cache_ttl_hours = 24            # AI response cache lifetime

[suggestions]
max_history = 5                 # Max history-based suggestions
max_ai = 3                      # Max AI-generated suggestions
show_risk_warning = true        # Highlight destructive commands

[privacy]
sanitize_ai_calls = true        # Apply regex sanitization before AI calls
```

**Provider Fallback:** When `ai.provider = "auto"`, clai tries providers in order: Anthropic → OpenAI → Google, skipping any unavailable (CLI not found or API key missing).

---

## 17. Open Questions & Risks

### Open Questions

- UX for destructive-command warnings (visual indicator? confirmation?)
- Keybinding conflicts with existing shell bindings

### Known Risks

- Latency from provider CLIs (mitigated by async UI and caching)
- User trust erosion if suggestions feel unsafe (mitigated by Zero Capture policy)
- Shell compatibility edge cases (mitigated by extensive testing matrix)

---

## 18. Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-01 | User-mode lazy-start daemon | Avoid system service complexity, works without root |
| 2026-01 | Phase 1 uses Shell Hooks only | PTY wrapper deferred due to stability risks |
| 2026-01 | Zero output capture in Phase 1 | Prevents breaking TUI apps (vim, less, ssh) |
| 2026-01 | Regex-based sanitization only | Entropy-based detection rejected as security theater (too many false positives) |
| 2026-01 | CLI-first provider integration | Leverage existing CLI tools (claude, openai) before direct API |
| 2026-01 | NextStep on success only | NextStep predictions only after exit 0; Diagnose on failures |
| 2026-01 | Func/Tech spec alignment | Synced CLI commands, config schema, session model, AI features |
