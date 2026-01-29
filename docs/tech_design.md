# clai — Technical Specification (v1.3)

**Scope:** Phase 1 (Internal Launch)
**Architecture:** Shell Hooks + Thin Client Binary + User Daemon
**Status:** Ready for Parallel Implementation (Test-Driven)

---

## 1. System Overview

### 1.1 Components

#### Shell Integration (Scripts)

- **Artifacts:** `.zshrc` / `.bashrc` sources, PowerShell module
- **Role:** Detects interactive events (command start/end, session lifecycle)
- **Constraint:** Must execute in < 5ms. Does not perform network I/O directly.
- **Action:** Calls the Thin Client Binary (`clai-shim`)

#### Thin Client Binary (`clai-shim`)

- **Implementation:** Compiled binary (Go or Rust recommended for startup speed)
- **Role:** Bridges the shell environment to the Daemon via gRPC
- **Latency Contract:**
  - **Logging:** Fire-and-forget (spawn daemon if needed, send message, exit immediately)
  - **Suggestions:** Blocks with a hard timeout (default: 50ms) to await Daemon response

#### Daemon (`clai-daemon`)

- **Role:** The "Brain." Owns the SQLite DB (Single Writer)
- **Lifecycle:** Lazy-started by `clai-shim`. Auto-exits after idle timeout.
- **Concurrency:** Handles multiple simultaneous clients (tabs) via gRPC

#### Provider Adapters

- **Role:** Interface to AI providers for text-to-command and diagnosis
- **Strategy:** CLI-first (wrap `claude`, `openai`, `gemini` CLIs), fallback to direct API
- **Features:** Streaming responses, cancellation support

#### Local Store

- **Technology:** SQLite (`~/.clai/state.db`)
- **Mode:** WAL (Write-Ahead Log) enabled for concurrency

### 1.2 Data Flow

1. User types command → Shell Hook triggers `preexec`
2. Shell Hook executes `clai-shim log-start --session-id=... --command="..."`
3. `clai-shim` checks for socket:
   - **If missing:** Spawns `clai-daemon` in detached background process, returns 0 immediately
   - **If present:** Sends gRPC message (fire-and-forget)
4. Shell Hook returns control to user (non-blocking)
5. Command executes...
6. Shell Hook triggers `precmd`, calls `clai-shim log-end --exit-code=...`

---

## 2. Daemon Lifecycle & IPC

### 2.1 The "Fire-and-Forget" Contract

To prevent the "Latency Trap," the interaction model is strict:

**SessionStart / CommandStart / CommandEnd:**

- Client sends message
- Client does not wait for server Ack
- If socket connection fails (after 10ms timeout), client drops the event and exits silent/success

**Reasoning:** Missing a history entry is better than blocking the prompt.

**Suggest / TextToCommand:**

- Client waits for response up to `client.suggest_timeout_ms` (config, default 50ms)
- If timeout occurs, client prints nothing and exits

### 2.2 IPC Transport

- **Protocol:** gRPC (Protobuf)
- **Transport:**
  - Unix: `unix://~/.clai/run/clai.sock`
  - Windows: `npipe:////./pipe/clai_user_<uid>`

**Rationale:** gRPC is heavy for scripts, but acceptable for the `clai-shim` binary. It provides strict schemas and forward compatibility.

### 2.3 Daemon Spawning

When `clai-shim` finds no socket:

1. Fork/exec `clai-daemon` in background (detached, no stdout/stderr)
2. Write PID to `~/.clai/run/clai.pid`
3. Return immediately (do not wait for daemon ready)
4. Subsequent calls will retry socket connection

**Daemon shutdown:**

- Auto-exits after `daemon.idle_timeout_mins` (default: 20) with no active sessions
- Graceful shutdown on SIGTERM
- Cleans up socket and PID file on exit

---

## 3. Security & Sanitization

### 3.1 Policy: Best-Effort Sanitization

We use "Sanitization" (not "Redaction") to set appropriate expectations—this is best-effort, not a security guarantee.

- **Mechanism:** Regex Denylist
- **Execution:** Runs in Daemon before any provider call

### 3.2 Sanitization Rules

The Daemon must strip patterns matching:

| Pattern Type | Regex | Example |
|--------------|-------|---------|
| AWS Access Key | `AKIA[0-9A-Z]{16}` | `AKIAIOSFODNN7EXAMPLE` |
| AWS Secret Key | `(?i)(aws_secret_access_key&#124;secret_access_key)\s*[=:]\s*\S+` | `aws_secret_access_key=wJalr...` |
| JWT Tokens | `eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+` | `eyJhbGc...` |
| Slack Tokens | `xox[baprs]-[0-9a-zA-Z-]+` | `xoxb-123-456-abc` |
| PEM Blocks | `-----BEGIN [A-Z ]+-----[\s\S]+?-----END [A-Z ]+-----` | Private keys |
| Generic Secrets | `(?i)(password&#124;token&#124;secret&#124;api_key)\s*[=:]\s*\S+` | `password=hunter2` |

**Note:** History is stored locally without sanitization. Sanitization applies only to AI provider calls.

### 3.3 Risk Tagging

Suggestions containing these patterns are flagged as "destructive":

- `rm -rf`, `rm -r`, `rmdir`
- `DROP TABLE`, `DROP DATABASE`, `TRUNCATE`
- `--force`, `--hard`, `-f` (in destructive contexts)
- `chmod 777`, `chmod -R`
- `> /dev/sda`, `dd if=`

Flagged suggestions include `risk: "destructive"` in the response for UI highlighting.

---

## 4. Event Model & Protobuf Definition

**File:** `proto/clai/v1/clai.proto`

```protobuf
syntax = "proto3";
package clai.v1;

// ---------------------------------------------------------
// Common
// ---------------------------------------------------------

message ClientInfo {
  string version = 1;
  string os = 2;        // darwin, linux, windows
  string shell = 3;     // zsh, bash, pwsh
  string hostname = 4;  // machine hostname
  string username = 5;  // current user
}

message Ack {
  bool ok = 1;
  string error = 2;     // populated if ok=false
}

// ---------------------------------------------------------
// Session Lifecycle
// ---------------------------------------------------------

message SessionStartRequest {
  ClientInfo client = 1;
  string session_id = 2;          // UUID v4 generated by clai-shim
  string cwd = 3;
  int64 started_at_unix_ms = 4;
}

message SessionEndRequest {
  string session_id = 1;
  int64 ended_at_unix_ms = 2;
}

// ---------------------------------------------------------
// Command Lifecycle (Phase 1: No Output Capture)
// ---------------------------------------------------------

message CommandStartRequest {
  string session_id = 1;
  string command_id = 2;    // UUID v4
  int64 ts_unix_ms = 3;
  string cwd = 4;
  string command = 5;       // Raw command string
}

message CommandEndRequest {
  string session_id = 1;
  string command_id = 2;
  int64 ts_unix_ms = 3;
  int32 exit_code = 4;
  int64 duration_ms = 5;
  // NOTE: stdout/stderr fields removed for Phase 1 stability
}

// ---------------------------------------------------------
// Suggestions
// ---------------------------------------------------------

message SuggestRequest {
  string session_id = 1;
  string cwd = 2;
  string buffer = 3;        // Current typing buffer (prefix)
  int32 cursor_pos = 4;     // Cursor position in buffer
  bool include_ai = 5;      // Request AI suggestions (explicit trigger)
  int32 max_results = 6;    // Max suggestions to return (default: 5)
}

message Suggestion {
  string text = 1;              // The suggested command
  string description = 2;       // Optional description
  string source = 3;            // "session", "cwd", "global", "ai"
  double score = 4;             // Ranking score (0.0 to 1.0)
  string risk = 5;              // "safe", "destructive", or empty
}

message SuggestResponse {
  repeated Suggestion suggestions = 1;
  bool from_cache = 2;          // True if served from cache
}

// ---------------------------------------------------------
// Text-to-Command (AI)
// ---------------------------------------------------------

message TextToCommandRequest {
  string session_id = 1;
  string prompt = 2;            // Natural language prompt
  string cwd = 3;
  int32 max_suggestions = 4;    // Default: 3
}

message TextToCommandResponse {
  repeated Suggestion suggestions = 1;
  string provider = 2;          // Which AI provider was used
  int64 latency_ms = 3;         // AI response time
}

// ---------------------------------------------------------
// Next Step Prediction
// ---------------------------------------------------------

message NextStepRequest {
  string session_id = 1;
  string last_command = 2;
  int32 last_exit_code = 3;
  string cwd = 4;
}

message NextStepResponse {
  repeated Suggestion suggestions = 1;
}

// ---------------------------------------------------------
// Diagnosis
// ---------------------------------------------------------

message DiagnoseRequest {
  string session_id = 1;
  string command = 2;
  int32 exit_code = 3;
  string cwd = 4;
}

message DiagnoseResponse {
  string explanation = 1;       // Human-readable explanation
  repeated Suggestion fixes = 2; // Suggested fix commands
}

// ---------------------------------------------------------
// Service
// ---------------------------------------------------------

service ClaiService {
  // Fire-and-Forget (Client ignores return)
  rpc SessionStart(SessionStartRequest) returns (Ack);
  rpc SessionEnd(SessionEndRequest) returns (Ack);
  rpc CommandStarted(CommandStartRequest) returns (Ack);
  rpc CommandEnded(CommandEndRequest) returns (Ack);

  // Interactive (Client waits with timeout)
  rpc Suggest(SuggestRequest) returns (SuggestResponse);
  rpc TextToCommand(TextToCommandRequest) returns (TextToCommandResponse);
  rpc NextStep(NextStepRequest) returns (NextStepResponse);
  rpc Diagnose(DiagnoseRequest) returns (DiagnoseResponse);

  // Ops
  rpc Ping(Ack) returns (Ack);
  rpc GetStatus(Ack) returns (StatusResponse);
}

message StatusResponse {
  string version = 1;
  int32 active_sessions = 2;
  int64 uptime_seconds = 3;
  int64 commands_logged = 4;
}
```

---

## 5. Database Schema (SQLite)

**Constraint:** The Daemon is the single writer. `clai-shim` never opens the DB.

### 5.1 Tables

```sql
-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_meta (
  version INTEGER PRIMARY KEY,
  applied_at_unix_ms INTEGER NOT NULL
);

-- Current schema version: 1
INSERT OR IGNORE INTO schema_meta (version, applied_at_unix_ms)
VALUES (1, strftime('%s', 'now') * 1000);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
  session_id TEXT PRIMARY KEY,
  started_at_unix_ms INTEGER NOT NULL,
  ended_at_unix_ms INTEGER,
  shell TEXT NOT NULL,
  os TEXT NOT NULL,
  hostname TEXT,
  username TEXT,
  initial_cwd TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at_unix_ms DESC);

-- Commands (History)
CREATE TABLE IF NOT EXISTS commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_id TEXT NOT NULL UNIQUE,
  session_id TEXT NOT NULL REFERENCES sessions(session_id),

  -- Timing
  ts_start_unix_ms INTEGER NOT NULL,
  ts_end_unix_ms INTEGER,
  duration_ms INTEGER,

  -- Context
  cwd TEXT NOT NULL,

  -- The Command
  command TEXT NOT NULL,          -- Raw display version
  command_norm TEXT NOT NULL,     -- Normalized (lowercase, trimmed, args removed)
  command_hash TEXT NOT NULL,     -- SHA256(command_norm) for fast dedup

  -- Result
  exit_code INTEGER,
  is_success INTEGER DEFAULT 1    -- 1=success (exit 0), 0=failure
);

CREATE INDEX IF NOT EXISTS idx_commands_session ON commands(session_id, ts_start_unix_ms DESC);
CREATE INDEX IF NOT EXISTS idx_commands_cwd ON commands(cwd, ts_start_unix_ms DESC);
CREATE INDEX IF NOT EXISTS idx_commands_norm ON commands(command_norm);
CREATE INDEX IF NOT EXISTS idx_commands_hash ON commands(command_hash);

-- AI Response Cache
CREATE TABLE IF NOT EXISTS ai_cache (
  cache_key TEXT PRIMARY KEY,     -- SHA256(provider + prompt + context)
  response_json TEXT NOT NULL,    -- Cached response
  provider TEXT NOT NULL,         -- anthropic, openai, google
  created_at_unix_ms INTEGER NOT NULL,
  expires_at_unix_ms INTEGER NOT NULL,  -- created_at + 24 hours
  hit_count INTEGER DEFAULT 0     -- For cache analytics
);

CREATE INDEX IF NOT EXISTS idx_ai_cache_expires ON ai_cache(expires_at_unix_ms);
```

### 5.2 Cache Eviction

The daemon runs cache cleanup on startup and every hour:

```sql
DELETE FROM ai_cache WHERE expires_at_unix_ms < strftime('%s', 'now') * 1000;
```

---

## 6. Shell Hook Implementation

### 6.1 zsh Integration

**File:** `~/.clai/hooks/clai.zsh` (sourced from `.zshrc`)

```zsh
# clai zsh integration
# Source this file from your .zshrc

# Generate session ID once per shell instance
export CLAI_SESSION_ID="${CLAI_SESSION_ID:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

# Session start (runs once when shell starts)
__clai_session_start() {
  clai-shim session-start \
    --session-id="$CLAI_SESSION_ID" \
    --cwd="$PWD" \
    --shell=zsh &!
}

# Before command execution
__clai_preexec() {
  export __CLAI_CMD_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
  export __CLAI_CMD_START=$(($(date +%s) * 1000))

  clai-shim log-start \
    --session-id="$CLAI_SESSION_ID" \
    --command-id="$__CLAI_CMD_ID" \
    --cwd="$PWD" \
    --command="$1" &!
}

# After command execution
__clai_precmd() {
  local exit_code=$?

  # Skip if no command was run
  [[ -z "$__CLAI_CMD_ID" ]] && return

  local now=$(($(date +%s) * 1000))
  local duration=$((now - __CLAI_CMD_START))

  clai-shim log-end \
    --session-id="$CLAI_SESSION_ID" \
    --command-id="$__CLAI_CMD_ID" \
    --exit-code="$exit_code" \
    --duration="$duration" &!

  unset __CLAI_CMD_ID __CLAI_CMD_START
}

# Suggestion widget (bound to right arrow for ghost text acceptance)
__clai_suggest() {
  local suggestion
  suggestion=$(clai-shim suggest \
    --session-id="$CLAI_SESSION_ID" \
    --cwd="$PWD" \
    --buffer="$BUFFER" \
    --cursor="$CURSOR" 2>/dev/null)

  if [[ -n "$suggestion" ]]; then
    BUFFER="$suggestion"
    CURSOR=${#BUFFER}
  fi
}

# AI text-to-command widget (Ctrl+G)
__clai_ask() {
  local suggestion
  suggestion=$(clai-shim text-to-command \
    --session-id="$CLAI_SESSION_ID" \
    --cwd="$PWD" \
    --prompt="$BUFFER" 2>/dev/null)

  if [[ -n "$suggestion" ]]; then
    BUFFER="$suggestion"
    CURSOR=${#BUFFER}
  fi
  zle redisplay
}

# Register hooks
autoload -Uz add-zsh-hook
add-zsh-hook preexec __clai_preexec
add-zsh-hook precmd __clai_precmd

# Register widgets
zle -N __clai_suggest
zle -N __clai_ask
bindkey '^G' __clai_ask

# Start session
__clai_session_start
```

### 6.2 bash Integration

**File:** `~/.clai/hooks/clai.bash` (sourced from `.bashrc`)

```bash
# clai bash integration
# Source this file from your .bashrc

# Generate session ID once per shell instance
export CLAI_SESSION_ID="${CLAI_SESSION_ID:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

# Session start
__clai_session_start() {
  clai-shim session-start \
    --session-id="$CLAI_SESSION_ID" \
    --cwd="$PWD" \
    --shell=bash &
  disown
}

# preexec equivalent using DEBUG trap
__clai_preexec() {
  # Skip if inside prompt command
  [[ "$BASH_COMMAND" == "$PROMPT_COMMAND" ]] && return
  # Skip clai commands
  [[ "$BASH_COMMAND" == clai-shim* ]] && return

  export __CLAI_CMD_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
  export __CLAI_CMD_START=$(($(date +%s) * 1000))
  export __CLAI_CMD="$BASH_COMMAND"

  clai-shim log-start \
    --session-id="$CLAI_SESSION_ID" \
    --command-id="$__CLAI_CMD_ID" \
    --cwd="$PWD" \
    --command="$BASH_COMMAND" &
  disown
}

# precmd equivalent using PROMPT_COMMAND
__clai_precmd() {
  local exit_code=$?

  # Skip if no command was tracked
  [[ -z "$__CLAI_CMD_ID" ]] && return

  local now=$(($(date +%s) * 1000))
  local duration=$((now - __CLAI_CMD_START))

  clai-shim log-end \
    --session-id="$CLAI_SESSION_ID" \
    --command-id="$__CLAI_CMD_ID" \
    --exit-code="$exit_code" \
    --duration="$duration" &
  disown

  unset __CLAI_CMD_ID __CLAI_CMD_START __CLAI_CMD
}

# Set up hooks
trap '__clai_preexec' DEBUG
PROMPT_COMMAND="__clai_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"

# Start session
__clai_session_start
```

### 6.3 PowerShell Integration

**File:** `~/.clai/hooks/clai.psm1`

```powershell
# clai PowerShell integration module

$script:ClaiSessionId = [guid]::NewGuid().ToString()

# Session start
function Start-ClaiSession {
    Start-Process -NoNewWindow -FilePath "clai-shim" -ArgumentList @(
        "session-start",
        "--session-id=$script:ClaiSessionId",
        "--cwd=$PWD",
        "--shell=pwsh"
    )
}

# PSReadLine handlers
$script:ClaiCommandId = $null
$script:ClaiCommandStart = $null

Set-PSReadLineOption -AddToHistoryHandler {
    param($command)

    $script:ClaiCommandId = [guid]::NewGuid().ToString()
    $script:ClaiCommandStart = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()

    Start-Process -NoNewWindow -FilePath "clai-shim" -ArgumentList @(
        "log-start",
        "--session-id=$script:ClaiSessionId",
        "--command-id=$script:ClaiCommandId",
        "--cwd=$PWD",
        "--command=$command"
    )

    return $true  # Add to history
}

# Prompt function wrapper for post-command logging
$script:OriginalPrompt = $function:prompt

function prompt {
    $exitCode = 0
    $isFailure = $false

    if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
        $exitCode = $LASTEXITCODE
        $isFailure = $true
    } elseif (-not $?) {
        $exitCode = 1
        $isFailure = $true
    }

    if ($null -ne $script:ClaiCommandId) {
        $now = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
        $duration = $now - $script:ClaiCommandStart

        Start-Process -NoNewWindow -FilePath "clai-shim" -ArgumentList @(
            "log-end",
            "--session-id=$script:ClaiSessionId",
            "--command-id=$script:ClaiCommandId",
            "--exit-code=$exitCode",
            "--duration=$duration"
        )

        $script:ClaiCommandId = $null
        $script:ClaiCommandStart = $null
    }

    # Call original prompt
    & $script:OriginalPrompt
}

# AI trigger (Ctrl+G)
Set-PSReadLineKeyHandler -Chord "Ctrl+g" -ScriptBlock {
    $line = $null
    $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)

    $suggestion = & clai-shim text-to-command `
        --session-id=$script:ClaiSessionId `
        --cwd=$PWD `
        --prompt="$line" 2>$null

    if ($suggestion) {
        [Microsoft.PowerShell.PSConsoleReadLine]::RevertLine()
        [Microsoft.PowerShell.PSConsoleReadLine]::Insert($suggestion)
    }
}

# Initialize
Start-ClaiSession

Export-ModuleMember -Function @()
```

---

## 7. Suggestion Ranking Algorithm

### 7.1 Scoring Formula

```
score = (source_weight * 0.4) + (recency_score * 0.3) + (success_score * 0.2) + (affinity_score * 0.1)
```

### 7.2 Weights

| Factor | Calculation |
|--------|-------------|
| Source Weight | session=1.0, cwd=0.7, global=0.4 |
| Recency Score | `1.0 / (1 + log(hours_since_use + 1))` |
| Success Score | `success_count / (success_count + failure_count)` |
| Affinity Score | 1.0 if same tool prefix as last command, else 0.0 |

### 7.3 Query Strategy

```sql
-- Get candidates matching prefix
SELECT
  command,
  source,
  MAX(ts_start_unix_ms) as last_used,
  SUM(CASE WHEN is_success = 1 THEN 1 ELSE 0 END) as success_count,
  COUNT(*) as total_count
FROM commands
WHERE command_norm LIKE ? || '%'
  AND session_id = ?  -- session scope
GROUP BY command_hash
ORDER BY last_used DESC
LIMIT 20;

-- Repeat for CWD and global scopes, then merge and rank
```

---

## 8. Provider Integration

### 8.1 Provider Adapter Interface

```go
type Provider interface {
    Name() string
    Available() bool  // Check if CLI/API is accessible

    TextToCommand(ctx context.Context, req TextToCommandRequest) ([]Suggestion, error)
    NextStep(ctx context.Context, req NextStepRequest) ([]Suggestion, error)
    Diagnose(ctx context.Context, req DiagnoseRequest) (DiagnoseResponse, error)
}

type TextToCommandRequest struct {
    Prompt      string
    CWD         string
    OS          string
    Shell       string
    RecentCmds  []CommandContext  // Last 10 commands
}

type CommandContext struct {
    Command  string
    ExitCode int
}
```

### 8.2 AI Context Construction

When calling AI providers, construct this context:

```
System: You are a command-line assistant. Generate shell commands for the user's request.

Context:
- OS: {os}
- Shell: {shell}
- Working Directory: {cwd}

Recent commands:
1. {cmd1} (exit {code1})
2. {cmd2} (exit {code2})
...

User request: {prompt}

Respond with 1-3 shell commands, one per line. No explanations.
```

### 8.3 Provider Priority

1. Check config `ai.provider` setting
2. If set, use that provider exclusively
3. If "auto", try in order: anthropic → openai → google
4. Skip unavailable providers (CLI not found, API key missing)

---

## 9. Configuration

### 9.1 File Location

- Unix: `~/.clai/config.toml`
- Windows: `%APPDATA%\clai\config.toml`

### 9.2 Full Schema

```toml
[daemon]
idle_timeout_mins = 20          # Auto-shutdown after idle (0 = never)
socket_path = "~/.clai/run/clai.sock"  # Unix socket path
log_level = "info"              # debug, info, warn, error
log_file = "~/.clai/logs/daemon.log"

[client]
suggest_timeout_ms = 50         # Max wait for suggestions
connect_timeout_ms = 10         # Socket connection timeout
fire_and_forget = true          # Don't wait for logging acks

[ai]
enabled = false                 # Must opt-in to AI features
provider = "auto"               # anthropic, openai, google, or auto
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

---

## 10. CLI Commands

| Command | Description | Implementation |
|---------|-------------|----------------|
| `clai install` | Add hooks to rc files | Appends source line to `.zshrc`/`.bashrc`, imports PS module |
| `clai uninstall` | Remove hooks | Removes source lines from rc files |
| `clai status` | Show daemon status | Calls `GetStatus` RPC |
| `clai doctor` | Diagnose issues | Checks: binary paths, socket, hooks, provider CLIs |
| `clai logs [-f]` | View daemon logs | Tails `~/.clai/logs/daemon.log` |
| `clai config [key] [value]` | Get/set config | Reads/writes `config.toml` |
| `clai ask "prompt"` | Text-to-command | Calls `TextToCommand` RPC, prints result |
| `clai history [query]` | Search history | Queries SQLite directly |
| `clai update` | Update binaries | Downloads latest release, replaces binaries |
| `clai daemon start` | Manual daemon start | For debugging |
| `clai daemon stop` | Stop daemon | Sends SIGTERM |

---

## 11. Error Handling & Recovery

### 11.1 Daemon Crash Recovery

- `clai-shim` detects stale socket (connection refused)
- Removes stale socket file and PID file
- Spawns new daemon instance
- Retries operation once

### 11.2 Logging Failures

- All logging operations are fire-and-forget
- Failed log events are silently dropped (no user impact)
- Daemon logs failures internally for debugging

### 11.3 AI Provider Failures

- Timeout after 10 seconds for AI calls
- On failure, return empty suggestions (graceful degradation)
- Cache successful responses to reduce future failures

---

## 12. Testing Strategy

### 12.1 Unit Tests

- Sanitization regex patterns
- Suggestion ranking algorithm
- Protobuf serialization

### 12.2 Integration Tests

- Shell hook → shim → daemon → response flow
- Multiple concurrent sessions
- Daemon auto-start and auto-shutdown

### 12.3 Shell Compatibility Matrix

Test on:
- zsh 5.8+ (macOS default, Linux)
- bash 4.4+ (Linux default), bash 3.2 (macOS default)
- PowerShell 7.x (cross-platform)

---

## 13. Directory Structure

```
~/.clai/
├── config.toml           # User configuration
├── state.db              # SQLite database
├── run/
│   ├── clai.sock         # Unix domain socket
│   └── clai.pid          # Daemon PID file
├── logs/
│   └── daemon.log        # Daemon log file
├── hooks/
│   ├── clai.zsh          # zsh integration
│   ├── clai.bash         # bash integration
│   └── clai.psm1         # PowerShell module
└── cache/
    └── providers/        # Provider-specific cache
```

---

## 14. Future Considerations (Phase 2)

- PTY wrapper for output capture
- SSH session support
- Plugin system for custom providers
- Web UI for history browsing
- Team sync (opt-in cloud backup)

---

## 15. Implementation Guide

This section provides detailed implementation specifications to enable parallel development by multiple agents.

### 15.1 Go Project Structure

```
clai/
├── go.mod
├── go.sum
├── Makefile
├── README.md
│
├── cmd/
│   ├── clai/                       # User-facing CLI
│   │   ├── main.go                 # Entry point
│   │   └── commands/
│   │       ├── root.go             # Cobra root command
│   │       ├── install.go          # clai install
│   │       ├── uninstall.go        # clai uninstall
│   │       ├── status.go           # clai status
│   │       ├── doctor.go           # clai doctor
│   │       ├── logs.go             # clai logs
│   │       ├── config.go           # clai config
│   │       ├── ask.go              # clai ask
│   │       ├── history.go          # clai history
│   │       ├── update.go           # clai update
│   │       └── daemon.go           # clai daemon start/stop
│   │
│   └── clai-shim/                  # Thin client binary
│       ├── main.go                 # Entry point
│       └── commands/
│           ├── root.go             # Argument parsing
│           ├── session.go          # session-start, session-end
│           ├── log.go              # log-start, log-end
│           ├── suggest.go          # suggest
│           └── textcmd.go          # text-to-command
│
├── internal/
│   ├── daemon/                     # Daemon core
│   │   ├── server.go               # gRPC server setup
│   │   ├── lifecycle.go            # Start, stop, idle timeout
│   │   ├── session_manager.go      # Session tracking
│   │   └── handlers/
│   │       ├── session.go          # SessionStart, SessionEnd handlers
│   │       ├── command.go          # CommandStarted, CommandEnded handlers
│   │       ├── suggest.go          # Suggest handler
│   │       ├── textcmd.go          # TextToCommand handler
│   │       ├── nextstep.go         # NextStep handler
│   │       ├── diagnose.go         # Diagnose handler
│   │       └── ops.go              # Ping, GetStatus handlers
│   │
│   ├── storage/                    # SQLite layer
│   │   ├── db.go                   # Connection, migrations
│   │   ├── sessions.go             # Session CRUD
│   │   ├── commands.go             # Command CRUD
│   │   ├── cache.go                # AI cache operations
│   │   └── queries.go              # Raw SQL queries (embedded)
│   │
│   ├── suggest/                    # Suggestion engine
│   │   ├── ranker.go               # Scoring algorithm
│   │   ├── sources.go              # Session/CWD/Global sources
│   │   └── normalize.go            # Command normalization
│   │
│   ├── provider/                   # AI provider adapters
│   │   ├── provider.go             # Interface definition
│   │   ├── registry.go             # Provider selection logic
│   │   ├── anthropic.go            # Claude CLI/API wrapper
│   │   ├── openai.go               # OpenAI CLI/API wrapper
│   │   ├── google.go               # Gemini CLI/API wrapper
│   │   └── context.go              # AI context builder
│   │
│   ├── sanitize/                   # Security
│   │   ├── sanitizer.go            # Regex sanitization
│   │   ├── patterns.go             # Compiled regex patterns
│   │   └── risk.go                 # Destructive command detection
│   │
│   ├── config/                     # Configuration
│   │   ├── config.go               # TOML parsing, defaults
│   │   ├── paths.go                # Platform-specific paths
│   │   └── validate.go             # Config validation
│   │
│   ├── ipc/                        # Client-side gRPC
│   │   ├── client.go               # gRPC client wrapper
│   │   ├── dial.go                 # Socket connection logic
│   │   └── spawn.go                # Daemon spawning
│   │
│   └── logging/                    # Logging utilities
│       ├── logger.go               # Structured logger setup
│       └── rotate.go               # Log rotation
│
├── proto/
│   └── clai/
│       └── v1/
│           └── clai.proto          # Protobuf definitions
│
├── gen/                            # Generated code (gitignored)
│   └── proto/
│       └── clai/
│           └── v1/
│               ├── clai.pb.go
│               └── clai_grpc.pb.go
│
├── hooks/                          # Shell integration scripts
│   ├── clai.zsh
│   ├── clai.bash
│   └── clai.psm1
│
└── scripts/
    ├── install.sh                  # Unix installer
    └── install.ps1                 # Windows installer
```

### 15.2 External Dependencies

```go
// go.mod
module github.com/yourorg/clai

go 1.22

require (
    // CLI framework
    github.com/spf13/cobra v1.8.0

    // gRPC & Protobuf
    google.golang.org/grpc v1.62.0
    google.golang.org/protobuf v1.33.0

    // SQLite
    github.com/mattn/go-sqlite3 v1.14.22  // CGO driver
    // OR for CGO-free:
    modernc.org/sqlite v1.29.0

    // Configuration
    github.com/BurntSushi/toml v1.3.2

    // Logging
    golang.org/x/exp/slog v0.0.0  // stdlib slog (Go 1.21+)

    // Utilities
    github.com/google/uuid v1.6.0
)
```

**Rationale:**
- `cobra` — Industry standard for Go CLIs
- `grpc` + `protobuf` — Type-safe IPC with forward compatibility
- `modernc.org/sqlite` — Pure Go SQLite (no CGO = easier cross-compilation)
- `toml` — Human-readable config format
- `slog` — Structured logging in stdlib (Go 1.21+)

### 15.3 Core Interface Definitions

```go
// internal/storage/storage.go
package storage

import "context"

type Store interface {
    // Sessions
    CreateSession(ctx context.Context, s *Session) error
    EndSession(ctx context.Context, sessionID string, endTime int64) error
    GetSession(ctx context.Context, sessionID string) (*Session, error)

    // Commands
    CreateCommand(ctx context.Context, c *Command) error
    UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error
    QueryCommands(ctx context.Context, q CommandQuery) ([]Command, error)

    // AI Cache
    GetCached(ctx context.Context, key string) (*CacheEntry, error)
    SetCached(ctx context.Context, entry *CacheEntry) error
    PruneExpiredCache(ctx context.Context) (int64, error)

    // Lifecycle
    Close() error
}

type Session struct {
    SessionID       string
    StartedAtUnixMs int64
    EndedAtUnixMs   *int64
    Shell           string
    OS              string
    Hostname        string
    Username        string
    InitialCWD      string
}

type Command struct {
    ID              int64
    CommandID       string
    SessionID       string
    TsStartUnixMs   int64
    TsEndUnixMs     *int64
    DurationMs      *int64
    CWD             string
    Command         string
    CommandNorm     string
    CommandHash     string
    ExitCode        *int
    IsSuccess       bool
}

type CommandQuery struct {
    SessionID   *string
    CWD         *string
    Prefix      string
    Limit       int
}
```

```go
// internal/provider/provider.go
package provider

import "context"

type Provider interface {
    Name() string
    Available() bool

    TextToCommand(ctx context.Context, req *TextToCommandRequest) (*TextToCommandResponse, error)
    NextStep(ctx context.Context, req *NextStepRequest) (*NextStepResponse, error)
    Diagnose(ctx context.Context, req *DiagnoseRequest) (*DiagnoseResponse, error)
}

type TextToCommandRequest struct {
    Prompt     string
    CWD        string
    OS         string
    Shell      string
    RecentCmds []CommandContext
}

type CommandContext struct {
    Command  string
    ExitCode int
}

type TextToCommandResponse struct {
    Suggestions []Suggestion
    ProviderName string
    LatencyMs   int64
}

type Suggestion struct {
    Text        string
    Description string
    Source      string  // "history", "ai"
    Score       float64
    Risk        string  // "safe", "destructive"
}
```

```go
// internal/suggest/ranker.go
package suggest

import "context"

type Ranker interface {
    Rank(ctx context.Context, req *RankRequest) ([]Suggestion, error)
}

type RankRequest struct {
    SessionID   string
    CWD         string
    Prefix      string
    LastCommand string
    MaxResults  int
}
```

```go
// internal/sanitize/sanitizer.go
package sanitize

type Sanitizer interface {
    Sanitize(input string) string
    IsDestructive(command string) bool
}
```

### 15.4 Build System (Makefile)

```makefile
# Makefile

.PHONY: all build clean test proto install lint

# Variables
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
PROTO_DIR := proto
GEN_DIR := gen

# Default target
all: proto build

# Generate protobuf code
proto:
	@mkdir -p $(GEN_DIR)/proto/clai/v1
	protoc \
		--go_out=$(GEN_DIR) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/clai/v1/clai.proto

# Build binaries
build: build-clai build-shim

build-clai:
	go build $(LDFLAGS) -o bin/clai ./cmd/clai

build-shim:
	go build $(LDFLAGS) -o bin/clai-shim ./cmd/clai-shim

# Cross-compilation
build-all:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/clai-darwin-amd64 ./cmd/clai
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/clai-darwin-arm64 ./cmd/clai
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/clai-linux-amd64 ./cmd/clai
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/clai-windows-amd64.exe ./cmd/clai
	# Repeat for clai-shim...

# Run tests
test:
	go test -v -race ./...

test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run ./...

# Clean
clean:
	rm -rf bin/ $(GEN_DIR)/ coverage.out coverage.html

# Install locally (for development)
install: build
	cp bin/clai $(GOPATH)/bin/
	cp bin/clai-shim $(GOPATH)/bin/

# Run daemon (for development)
run-daemon:
	go run ./cmd/clai daemon start
```

### 15.5 Error Handling Conventions

```go
// internal/errors/errors.go
package errors

import "fmt"

// Sentinel errors (use errors.Is())
var (
    ErrSessionNotFound   = fmt.Errorf("session not found")
    ErrCommandNotFound   = fmt.Errorf("command not found")
    ErrDaemonUnavailable = fmt.Errorf("daemon unavailable")
    ErrProviderUnavailable = fmt.Errorf("AI provider unavailable")
    ErrTimeout           = fmt.Errorf("operation timed out")
)

// Wrapped errors (preserve context)
func Wrap(err error, msg string) error {
    if err == nil {
        return nil
    }
    return fmt.Errorf("%s: %w", msg, err)
}

// Example usage:
// return errors.Wrap(err, "failed to create session")
```

**Error Handling Rules:**

1. **clai-shim:** Never print errors to stderr (silent failure). Log to daemon if connected.
2. **clai-daemon:** Log all errors. Return gRPC status codes appropriately.
3. **clai CLI:** Print user-friendly errors to stderr. Use `--verbose` for details.

### 15.6 Logging Conventions

```go
// internal/logging/logger.go
package logging

import (
    "log/slog"
    "os"
)

var Logger *slog.Logger

func Init(level string, logFile string) error {
    var lvl slog.Level
    switch level {
    case "debug":
        lvl = slog.LevelDebug
    case "warn":
        lvl = slog.LevelWarn
    case "error":
        lvl = slog.LevelError
    default:
        lvl = slog.LevelInfo
    }

    f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        return err
    }

    Logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
        Level: lvl,
    }))
    return nil
}

// Usage:
// logging.Logger.Info("session started", "session_id", sid, "shell", shell)
// logging.Logger.Error("failed to save command", "error", err, "command_id", cid)
```

**Logging Rules:**

1. Use structured logging (key-value pairs)
2. Always include `session_id` and `command_id` where applicable
3. Log at appropriate levels:
   - `Debug`: Detailed flow (disabled in production)
   - `Info`: Session/command lifecycle events
   - `Warn`: Recoverable issues (timeout, cache miss)
   - `Error`: Failures requiring attention

---

## 16. Parallel Implementation Streams

This section defines **6 independent work streams** that can be developed in parallel.

### Stream A: Proto & gRPC Foundation

**Owner:** Agent A
**Duration:** 2-3 days
**Dependencies:** None
**Deliverables:**
- `proto/clai/v1/clai.proto` (copy from Section 4)
- `gen/proto/clai/v1/*.pb.go` (generated)
- `Makefile` with `proto` target

**Acceptance Criteria:**
- [ ] `make proto` generates Go code without errors
- [ ] Generated code compiles
- [ ] Basic gRPC server/client can exchange `Ping` message

---

### Stream B: Storage Layer

**Owner:** Agent B
**Duration:** 3-4 days
**Dependencies:** None
**Deliverables:**
- `internal/storage/db.go`
- `internal/storage/sessions.go`
- `internal/storage/commands.go`
- `internal/storage/cache.go`
- `internal/storage/queries.go`

**Acceptance Criteria:**
- [ ] Implements `Store` interface (Section 15.3)
- [ ] Creates schema on first run (Section 5.1)
- [ ] All CRUD operations work
- [ ] Unit tests for each operation
- [ ] WAL mode enabled

**Key Implementation Notes:**
```go
// Open database with WAL mode
db, err := sql.Open("sqlite", "file:~/.clai/state.db?_journal_mode=WAL")
```

---

### Stream C: clai-shim (Thin Client)

**Owner:** Agent C
**Duration:** 3-4 days
**Dependencies:** Stream A (proto)
**Deliverables:**
- `cmd/clai-shim/main.go`
- `cmd/clai-shim/commands/*.go`
- `internal/ipc/client.go`
- `internal/ipc/dial.go`
- `internal/ipc/spawn.go`

**Acceptance Criteria:**
- [ ] Parses all subcommands: `session-start`, `session-end`, `log-start`, `log-end`, `suggest`, `text-to-command`
- [ ] Connects to daemon via Unix socket
- [ ] Spawns daemon if socket missing
- [ ] Fire-and-forget for logging (no wait)
- [ ] 50ms timeout for suggestions
- [ ] Silent failure (exit 0) on all errors
- [ ] < 10ms startup time

**Key Implementation Notes:**
```go
// Fire-and-forget pattern
func fireAndForget(ctx context.Context, client pb.ClaiServiceClient, req *pb.CommandStartRequest) {
    ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
    defer cancel()
    _, _ = client.CommandStarted(ctx, req) // Ignore errors
}
```

---

### Stream D: clai-daemon (Core Server)

**Owner:** Agent D
**Duration:** 5-6 days
**Dependencies:** Stream A (proto), Stream B (storage)
**Deliverables:**
- `cmd/clai-daemon/main.go` (or integrated into `cmd/clai/`)
- `internal/daemon/server.go`
- `internal/daemon/lifecycle.go`
- `internal/daemon/session_manager.go`
- `internal/daemon/handlers/*.go`

**Acceptance Criteria:**
- [ ] Listens on Unix socket `~/.clai/run/clai.sock`
- [ ] Writes PID file `~/.clai/run/clai.pid`
- [ ] Handles all gRPC methods (Section 4)
- [ ] Auto-shutdown after idle timeout
- [ ] Graceful shutdown on SIGTERM
- [ ] Cleans up socket/PID on exit
- [ ] Concurrent session support

**Key Implementation Notes:**
```go
// Idle timeout watcher
func (s *Server) watchIdle() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        if s.sessionManager.ActiveCount() == 0 &&
           time.Since(s.lastActivity) > s.idleTimeout {
            s.Shutdown()
            return
        }
    }
}
```

---

### Stream E: CLI Commands

**Owner:** Agent E
**Duration:** 4-5 days
**Dependencies:** Stream A (proto), Stream B (storage)
**Deliverables:**
- `cmd/clai/main.go`
- `cmd/clai/commands/*.go` (all commands from Section 10)
- `internal/config/config.go`
- `hooks/*.{zsh,bash,psm1}` (copy from Section 6)

**Acceptance Criteria:**
- [ ] All commands from Section 10 implemented
- [ ] `clai install` modifies rc files correctly
- [ ] `clai doctor` checks all dependencies
- [ ] `clai config` reads/writes TOML
- [ ] `clai status` displays daemon info
- [ ] `clai history` queries SQLite directly

**Key Implementation Notes:**
```go
// Cobra command structure
var rootCmd = &cobra.Command{Use: "clai"}

func init() {
    rootCmd.AddCommand(installCmd)
    rootCmd.AddCommand(statusCmd)
    // ...
}
```

---

### Stream F: Provider Adapters

**Owner:** Agent F
**Duration:** 4-5 days
**Dependencies:** None (can mock daemon for testing)
**Deliverables:**
- `internal/provider/provider.go`
- `internal/provider/registry.go`
- `internal/provider/anthropic.go`
- `internal/provider/openai.go`
- `internal/provider/google.go`
- `internal/provider/context.go`
- `internal/sanitize/*.go`

**Acceptance Criteria:**
- [ ] Implements `Provider` interface (Section 15.3)
- [ ] CLI detection (`claude --version`, etc.)
- [ ] Falls back to API if CLI unavailable
- [ ] Constructs AI context (Section 8.2)
- [ ] Sanitizes input before sending (Section 3.2)
- [ ] Caches responses
- [ ] 10-second timeout for AI calls

**Key Implementation Notes:**
```go
// CLI-first pattern
func (p *AnthropicProvider) Available() bool {
    _, err := exec.LookPath("claude")
    if err == nil {
        return true
    }
    // Check for API key as fallback
    return os.Getenv("ANTHROPIC_API_KEY") != ""
}
```

---

### Stream G: Suggestion Ranking (Optional Parallel)

**Owner:** Agent G (or merge with D)
**Duration:** 2-3 days
**Dependencies:** Stream B (storage)
**Deliverables:**
- `internal/suggest/ranker.go`
- `internal/suggest/sources.go`
- `internal/suggest/normalize.go`

**Acceptance Criteria:**
- [ ] Implements scoring formula (Section 7.1)
- [ ] Queries session, CWD, global scopes
- [ ] Merges and deduplicates results
- [ ] Returns top N suggestions
- [ ] < 50ms latency for 10K command history

---

## 17. Integration Checkpoints

After parallel streams complete, integration proceeds in order:

| Checkpoint | Streams Required | Test |
|------------|------------------|------|
| **CP1: Basic IPC** | A | Ping works between shim and daemon |
| **CP2: Session Flow** | A, B, C, D | Session start/end persists to SQLite |
| **CP3: Command Logging** | A, B, C, D | Commands logged with exit codes |
| **CP4: Suggestions** | A, B, C, D, G | History-based suggestions work |
| **CP5: AI Integration** | All | Text-to-command with AI provider |
| **CP6: Full CLI** | All | All `clai` commands work end-to-end |

---

## 18. Testing Requirements

This section defines mandatory testing standards for all implementation streams.

### 18.1 Coverage Targets

| Package | Minimum Coverage | Rationale |
|---------|------------------|-----------|
| `internal/storage` | 90% | Data integrity is critical |
| `internal/sanitize` | 95% | Security-critical code |
| `internal/suggest` | 85% | Core ranking logic |
| `internal/provider` | 80% | External dependencies complicate testing |
| `internal/ipc` | 75% | Network code harder to unit test |
| `internal/daemon` | 80% | Core business logic |
| `internal/config` | 85% | Configuration parsing must be reliable |
| `cmd/*` | 70% | CLI wiring, less critical |

**Global Target:** 80% overall coverage across all packages.

**Enforcement:**

```makefile
# Makefile addition
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | grep total | awk '{print $$3}' | \
		sed 's/%//' | xargs -I {} sh -c 'if [ {} -lt 80 ]; then echo "Coverage {}% below 80% threshold"; exit 1; fi'
```

### 18.2 Test File Conventions

| Convention | Requirement |
|------------|-------------|
| **File naming** | `*_test.go` in same package |
| **Test naming** | `TestXxx` for functions, `TestXxx_SubCase` for sub-tests |
| **Table-driven** | Required for functions with >3 test cases |
| **Parallel tests** | Use `t.Parallel()` where safe |
| **Test helpers** | Prefix with `test` or place in `testutil/` package |

### 18.3 Table-Driven Test Pattern

All functions with multiple input/output combinations **must** use table-driven tests:

```go
// internal/sanitize/sanitizer_test.go
func TestSanitizer_Sanitize(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {
            name:     "AWS access key",
            input:    "export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
            expected: "export AWS_ACCESS_KEY_ID=[REDACTED]",
        },
        {
            name:     "JWT token",
            input:    "curl -H 'Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U'",
            expected: "curl -H 'Authorization: Bearer [REDACTED]'",
        },
        {
            name:     "no secrets",
            input:    "git push origin main",
            expected: "git push origin main",
        },
        {
            name:     "password in command",
            input:    "mysql -p password=secret123",
            expected: "mysql -p password=[REDACTED]",
        },
    }

    s := NewSanitizer()

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := s.Sanitize(tt.input)
            if got != tt.expected {
                t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.expected)
            }
        })
    }
}
```

### 18.4 Mock Interfaces

Each interface **must** have a corresponding mock in `internal/mocks/`:

```go
// internal/mocks/store.go
package mocks

import (
    "context"
    "github.com/yourorg/clai/internal/storage"
)

type MockStore struct {
    CreateSessionFunc     func(ctx context.Context, s *storage.Session) error
    EndSessionFunc        func(ctx context.Context, sessionID string, endTime int64) error
    GetSessionFunc        func(ctx context.Context, sessionID string) (*storage.Session, error)
    CreateCommandFunc     func(ctx context.Context, c *storage.Command) error
    UpdateCommandEndFunc  func(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error
    QueryCommandsFunc     func(ctx context.Context, q storage.CommandQuery) ([]storage.Command, error)
    GetCachedFunc         func(ctx context.Context, key string) (*storage.CacheEntry, error)
    SetCachedFunc         func(ctx context.Context, entry *storage.CacheEntry) error
    PruneExpiredCacheFunc func(ctx context.Context) (int64, error)
    CloseFunc             func() error

    // Call tracking
    Calls []MockCall
}

type MockCall struct {
    Method string
    Args   []interface{}
}

func (m *MockStore) CreateSession(ctx context.Context, s *storage.Session) error {
    m.Calls = append(m.Calls, MockCall{Method: "CreateSession", Args: []interface{}{s}})
    if m.CreateSessionFunc != nil {
        return m.CreateSessionFunc(ctx, s)
    }
    return nil
}

// ... implement all interface methods ...
```

**Required Mocks:**

| Interface | Mock File |
|-----------|-----------|
| `storage.Store` | `internal/mocks/store.go` |
| `provider.Provider` | `internal/mocks/provider.go` |
| `suggest.Ranker` | `internal/mocks/ranker.go` |
| `sanitize.Sanitizer` | `internal/mocks/sanitizer.go` |

### 18.5 Required Test Cases by Stream

#### Stream A: Proto & gRPC

```go
// Minimum required tests
TestProto_SessionStartRequest_Serialization
TestProto_CommandEndRequest_Serialization
TestProto_SuggestResponse_Serialization
TestGRPC_PingRoundTrip
TestGRPC_ConnectionRefused_Error
```

#### Stream B: Storage Layer

```go
// Sessions
TestStore_CreateSession_Success
TestStore_CreateSession_DuplicateID
TestStore_EndSession_Success
TestStore_EndSession_NotFound
TestStore_GetSession_Success
TestStore_GetSession_NotFound

// Commands
TestStore_CreateCommand_Success
TestStore_CreateCommand_InvalidSessionID
TestStore_UpdateCommandEnd_Success
TestStore_UpdateCommandEnd_NotFound
TestStore_QueryCommands_BySession
TestStore_QueryCommands_ByCWD
TestStore_QueryCommands_ByPrefix
TestStore_QueryCommands_EmptyResult
TestStore_QueryCommands_Limit

// Cache
TestStore_GetCached_Hit
TestStore_GetCached_Miss
TestStore_GetCached_Expired
TestStore_SetCached_Success
TestStore_PruneExpiredCache_RemovesExpired

// Database
TestStore_Migration_CreatesSchema
TestStore_WALMode_Enabled
TestStore_ConcurrentWrites_Safe
```

#### Stream C: clai-shim

```go
// Command parsing
TestShim_ParseSessionStart_AllFlags
TestShim_ParseLogStart_AllFlags
TestShim_ParseSuggest_AllFlags
TestShim_ParseTextToCommand_AllFlags

// IPC
TestShim_Dial_Success
TestShim_Dial_Timeout
TestShim_Dial_SocketNotFound
TestShim_SpawnDaemon_Success
TestShim_SpawnDaemon_AlreadyRunning

// Fire-and-forget
TestShim_LogStart_FireAndForget_NoBlock
TestShim_LogEnd_FireAndForget_NoBlock
TestShim_LogStart_DaemonDown_SilentExit

// Suggestions
TestShim_Suggest_ReturnsResult
TestShim_Suggest_Timeout
TestShim_Suggest_DaemonDown_EmptyResult
```

#### Stream D: clai-daemon

```go
// Lifecycle
TestDaemon_Start_CreatesSocket
TestDaemon_Start_WritesPIDFile
TestDaemon_Stop_CleansUpSocket
TestDaemon_Stop_CleansUpPIDFile
TestDaemon_IdleTimeout_Shutdown
TestDaemon_SIGTERM_GracefulShutdown

// Session handling
TestHandler_SessionStart_Success
TestHandler_SessionStart_Persists
TestHandler_SessionEnd_Success
TestHandler_SessionEnd_UpdatesDB

// Command handling
TestHandler_CommandStarted_Success
TestHandler_CommandEnded_Success
TestHandler_CommandEnded_CalculatesDuration

// Suggestions
TestHandler_Suggest_ReturnsHistorySuggestions
TestHandler_Suggest_ReturnsAISuggestions
TestHandler_Suggest_CombinesSources

// Concurrency
TestDaemon_MultipleSessions_Concurrent
TestDaemon_MultipleCommands_SameSession
```

#### Stream E: CLI Commands

```go
// Install
TestCLI_Install_Zsh_AppendsHook
TestCLI_Install_Bash_AppendsHook
TestCLI_Install_PowerShell_ImportsModule
TestCLI_Install_AlreadyInstalled_NoOp

// Uninstall
TestCLI_Uninstall_RemovesHook

// Config
TestCLI_Config_Get_ExistingKey
TestCLI_Config_Get_MissingKey
TestCLI_Config_Set_UpdatesFile
TestCLI_Config_Validate_InvalidValue

// Status
TestCLI_Status_DaemonRunning
TestCLI_Status_DaemonStopped

// Doctor
TestCLI_Doctor_AllChecksPass
TestCLI_Doctor_MissingBinary
TestCLI_Doctor_BrokenSocket
```

#### Stream F: Provider Adapters

```go
// Availability
TestProvider_Anthropic_Available_CLIFound
TestProvider_Anthropic_Available_APIKeyOnly
TestProvider_Anthropic_Unavailable
TestProvider_Registry_AutoSelect

// TextToCommand
TestProvider_TextToCommand_Success
TestProvider_TextToCommand_Timeout
TestProvider_TextToCommand_ParsesResponse
TestProvider_TextToCommand_Sanitizes Input

// Caching
TestProvider_TextToCommand_CacheHit
TestProvider_TextToCommand_CacheMiss

// Context
TestContext_Builder_IncludesOS
TestContext_Builder_IncludesShell
TestContext_Builder_IncludesRecentCmds
TestContext_Builder_LimitsHistory
```

#### Stream G: Suggestion Ranking

```go
// Scoring
TestRanker_Score_SessionWeightHighest
TestRanker_Score_RecencyDecay
TestRanker_Score_SuccessBias
TestRanker_Score_ToolAffinity

// Queries
TestRanker_Query_SessionScope
TestRanker_Query_CWDScope
TestRanker_Query_GlobalScope
TestRanker_Query_MergesSources
TestRanker_Query_Deduplicates

// Normalization
TestNormalize_Lowercase
TestNormalize_TrimWhitespace
TestNormalize_RemovesArgs

// Performance
TestRanker_Performance_10KCommands_Under50ms
```

### 18.6 Integration Tests

Integration tests live in `tests/integration/` and require a running daemon:

```go
// tests/integration/e2e_test.go
package integration

import (
    "os/exec"
    "testing"
    "time"
)

func TestE2E_SessionLifecycle(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Start daemon
    daemon := exec.Command("clai", "daemon", "start")
    if err := daemon.Start(); err != nil {
        t.Fatalf("failed to start daemon: %v", err)
    }
    defer exec.Command("clai", "daemon", "stop").Run()

    time.Sleep(100 * time.Millisecond) // Wait for socket

    // Session start
    sessionStart := exec.Command("clai-shim", "session-start",
        "--session-id=test-session-123",
        "--cwd=/tmp",
        "--shell=zsh")
    if err := sessionStart.Run(); err != nil {
        t.Fatalf("session-start failed: %v", err)
    }

    // Log command
    logStart := exec.Command("clai-shim", "log-start",
        "--session-id=test-session-123",
        "--command-id=cmd-456",
        "--cwd=/tmp",
        "--command=echo hello")
    if err := logStart.Run(); err != nil {
        t.Fatalf("log-start failed: %v", err)
    }

    logEnd := exec.Command("clai-shim", "log-end",
        "--session-id=test-session-123",
        "--command-id=cmd-456",
        "--exit-code=0",
        "--duration=100")
    if err := logEnd.Run(); err != nil {
        t.Fatalf("log-end failed: %v", err)
    }

    // Verify in DB
    // ...
}

func TestE2E_SuggestionsFlow(t *testing.T) {
    // Similar structure...
}

func TestE2E_ShellHook_Zsh(t *testing.T) {
    // Spawns actual zsh with hook, runs commands, verifies logging
}
```

### 18.7 Test Fixtures

Shared test data lives in `testdata/`:

```
testdata/
├── commands/
│   ├── valid_commands.json     # Sample command records
│   └── edge_cases.json         # Unusual inputs
├── config/
│   ├── valid.toml              # Valid configuration
│   ├── minimal.toml            # Minimal valid config
│   └── invalid/
│       ├── bad_timeout.toml
│       └── missing_required.toml
├── sanitize/
│   ├── secrets.txt             # Inputs containing secrets
│   └── safe.txt                # Safe inputs
└── sqlite/
    └── test.db                 # Pre-populated test database
```

### 18.8 CI/CD Pipeline Requirements

```yaml
# .github/workflows/ci.yml
name: CI

on: [push, pull_request]

jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        go: ['1.22']

    steps:
    - uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: ${{ matrix.go }}

    - name: Install protoc
      run: |
        # Platform-specific protoc installation

    - name: Generate proto
      run: make proto

    - name: Run unit tests
      run: go test -v -race -short ./...

    - name: Run tests with coverage
      run: |
        go test -v -race -coverprofile=coverage.out ./...
        go tool cover -func=coverage.out

    - name: Check coverage threshold
      run: |
        COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
        if (( $(echo "$COVERAGE < 80" | bc -l) )); then
          echo "Coverage $COVERAGE% is below 80% threshold"
          exit 1
        fi

    - name: Upload coverage
      uses: codecov/codecov-action@v4
      with:
        files: ./coverage.out

  lint:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - uses: golangci/golangci-lint-action@v4
      with:
        version: latest

  integration:
    runs-on: ubuntu-latest
    needs: test
    steps:
    - uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: '1.22'

    - name: Build binaries
      run: make build

    - name: Run integration tests
      run: go test -v ./tests/integration/...
```

### 18.9 Pre-Commit Hooks

```bash
#!/bin/bash
# .git/hooks/pre-commit

set -e

echo "Running tests..."
go test -short ./...

echo "Running linter..."
golangci-lint run ./...

echo "Checking coverage..."
go test -coverprofile=/tmp/coverage.out ./... > /dev/null
COVERAGE=$(go tool cover -func=/tmp/coverage.out | grep total | awk '{print $3}' | sed 's/%//')
if (( $(echo "$COVERAGE < 80" | bc -l) )); then
  echo "Coverage $COVERAGE% is below 80% threshold"
  exit 1
fi

echo "All checks passed!"
```

### 18.10 Stream Acceptance Criteria Updates

All stream acceptance criteria now **require** passing tests:

| Stream | Additional Test Requirements |
|--------|------------------------------|
| **A: Proto** | All serialization tests pass |
| **B: Storage** | 90% coverage, all CRUD tests pass |
| **C: clai-shim** | All command/IPC tests pass, <10ms startup verified |
| **D: clai-daemon** | 80% coverage, lifecycle tests pass |
| **E: CLI** | All command tests pass, install/uninstall verified |
| **F: Providers** | 80% coverage, mock provider tests pass |
| **G: Ranking** | 85% coverage, performance test passes |

**Merge Criteria:** No PR merges without:
- [ ] All unit tests passing
- [ ] Coverage at or above package threshold
- [ ] No linter errors
- [ ] Integration tests passing (for affected streams)
