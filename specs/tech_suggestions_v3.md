# clai Non-PTY Suggestions Engine (Go + SQLite) — Technical Specification (v3)

This document specifies a **non-PTY**, **non-LLM** next-command suggestion engine for clai.
It preserves all implementation details from v2 and incorporates additional UX/behavior upgrades:

1. **Proactive pre-computation** (zero-latency suggestions)
2. **Semantic slot filling** (argument prediction via histograms)
3. **Incognito mode** (ephemeral, no-disk recording)
4. **User-defined task discovery** (config-driven "plugins")
5. **Did You Mean?** (typo correction on failures)
6. **SQLite FTS5** for fuzzy history search

---

## 0) Goals, Non-goals, Guarantees

### 0.1 Goals
- "Wow" next-command suggestions without an LLM, based on:
  - command history + repo context
  - Markov bigrams + decayed frequency
  - project task discovery (built-in + user-defined)
  - argument prediction via slot histograms
  - error-aware typo correction
- Cross-shell on macOS/Linux:
  - **bash** (4.0+, with 4.4+ preferred for array PROMPT_COMMAND)
  - **zsh** (5.0+)
  - **fish** (3.0+)
- Windows later (daemon + transport supports Named Pipes now).

### 0.2 Non-goals
- PTY interception, stdout/stderr parsing
- Full shell semantic parsing across all shells (best-effort normalization)
- Remote sync / multi-machine federation

### 0.3 Hard Guarantees (Ingestion safety)
- **No user prompt blocking**: ingestion is fire-and-forget with short timeouts.
- **No quoting/escaping bugs**: cmd/cwd are never passed as CLI args.
- **No UTF-8 assumptions**: JSON produced only by `clai-hook` after lossy conversion.
- **Secure, user-scoped IPC paths**.
- **Automatic schema migrations**: daemon runs migrations with locking.
- **Incognito is strict**: ephemeral events must never be persisted when enabled.
- **Graceful degradation**: shell hooks never break user workflow even if daemon crashes.

---

## 1) Inputs and Capture (No PTY)

### 1.1 Captured fields (minimum)
For every executed command:
- `cmd_raw` (string; may be skipped/truncated if too large)
- `cwd`
- `ts` (unix ms)
- `exit_code`
- `shell` (bash|zsh|fish)
- `session_id`
- `ephemeral` (bool; true in incognito/ephemeral mode)

Optional but high value:
- `duration_ms` (best effort; see 6.6)
- git context:
  - `repo_root`, `remote_url`, `branch`, `dirty` (best effort)

### 1.2 Shell timing precision

| Shell | Mechanism | Precision | Notes |
|-------|-----------|-----------|-------|
| zsh | `$EPOCHREALTIME` | microseconds | Requires `zsh/datetime` module (auto-loaded on access) |
| fish | `$CMD_DURATION` | milliseconds | Built-in, set after each command |
| bash | `$SECONDS` | seconds | Integer only; see 1.2.1 for high-precision alternative |

#### 1.2.1 Bash high-precision timing (optional)
For sub-second precision in bash, use external `date`:
```bash
# preexec equivalent (DEBUG trap):
_clai_start_time=$(date +%s%3N)

# postcmd (PROMPT_COMMAND):
_clai_end_time=$(date +%s%3N)
_clai_duration_ms=$(( _clai_end_time - _clai_start_time ))
```
**Trade-off**: Adds ~2ms overhead per command. Opt-in via config.

---

## 2) Architecture Overview

### 2.1 Components
**A) Shell hooks**
- Call `clai-hook` on command completion.
- Provide data via env vars or stdin (never CLI args).
- Respect incognito flags and env-size thresholds.

**B) `clai-hook`**
- Reads fields from env/stdin.
- Performs:
  - lossy UTF-8 conversion
  - NDJSON serialization
  - non-blocking send to daemon (default 15ms connect, no ACK)
- Drops events if daemon unavailable/busy.

**C) `clai-daemon` (Go)**
- Transport abstraction (Unix socket now, Windows named pipe later).
- Validates/normalizes events; updates in-memory hot caches.
- Persists non-ephemeral events to SQLite (batched transactions).
- Maintains aggregates (transition + decayed command_score + slot histograms).
- Discovers tasks (built-in + user-defined config).
- Computes git context (repo_key, branch, dirty) — **never in hooks**.
- Serves suggestion + search APIs.

**D) SQLite**
- command history + aggregates + task caches + optional FTS index
- migrations managed by daemon startup.

### 2.2 Data flow (updated)
1. Shell hook → `clai-hook` (env/stdin) → daemon (NDJSON)
2. Daemon ingestion:
   - validate + normalize
   - compute git context from `cwd` (cached, TTL-based)
   - update in-memory caches (precompute top suggestions per session)
   - if `ephemeral=false`: batch-write to SQLite + update persisted aggregates
   - if `ephemeral=true`: update only in-memory session model; do not persist
3. UI requests suggestions:
   - returns from hot cache (usually) else DB-backed compute fallback

---

## 3) Transport, IPC, Paths, Security

### 3.1 Transport abstraction (Required)
Implement a transport interface in Go:
- Unix: `net.Listener` on Unix domain socket
- Windows: Named pipe listener (e.g., `winio.ListenPipe`) behind the same interface

All layers above transport consume an `io.Reader` stream of NDJSON lines.

### 3.2 Path standards (Unix + Windows)
**Unix (macOS/Linux):**
1. `$XDG_RUNTIME_DIR/clai/daemon.sock` (preferred)
2. fallback: `$TMPDIR/clai-$UID/daemon.sock` or `/tmp/clai-$UID/daemon.sock`
- directory mode: `0700`

**Windows (defined now):**
- `\\.\pipe\clai-<SID>-daemon`
- pipe ACL restricts to current user

### 3.3 Timeouts and fire-and-forget (Required)
- default connect timeout: **15ms** (range 10–20ms)
- write timeout: 10–20ms
- no ACK; no read
- on any error: drop event silently

### 3.4 Nice-to-have: stale socket cleanup on daemon start
After acquiring daemon lock (see 5.2), unlink stale Unix socket file before listening.

---

## 4) Storage Model (SQLite)

### 4.1 SQLite configuration
- WAL mode
- one writer goroutine
- prepared statements
- ingestion batching (25–50ms or 100 events)
- optional retention policy

### 4.2 Schema (core tables)

> Notes:
> - `ephemeral` events are not persisted (by default). The column is included to support optional "persist but mark" modes later.
> - If you decide to never persist ephemeral events, you can omit the column from `command_event` initially.

```sql
CREATE TABLE IF NOT EXISTS session (
  id            TEXT PRIMARY KEY,
  created_at    INTEGER NOT NULL,
  shell         TEXT NOT NULL,
  host          TEXT,
  user          TEXT
);

CREATE TABLE IF NOT EXISTS command_event (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id    TEXT NOT NULL REFERENCES session(id),
  ts            INTEGER NOT NULL,
  duration_ms   INTEGER,
  exit_code     INTEGER,
  cwd           TEXT NOT NULL,
  repo_key      TEXT,
  branch        TEXT,
  cmd_raw       TEXT NOT NULL,
  cmd_norm      TEXT NOT NULL,
  ephemeral     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_event_ts ON command_event(ts);
CREATE INDEX IF NOT EXISTS idx_event_repo_ts ON command_event(repo_key, ts);
CREATE INDEX IF NOT EXISTS idx_event_cwd_ts ON command_event(cwd, ts);
CREATE INDEX IF NOT EXISTS idx_event_session_ts ON command_event(session_id, ts);
CREATE INDEX IF NOT EXISTS idx_event_norm_repo ON command_event(cmd_norm, repo_key);

CREATE TABLE IF NOT EXISTS transition (
  scope         TEXT NOT NULL,            -- 'global' or repo_key
  prev_norm     TEXT NOT NULL,
  next_norm     TEXT NOT NULL,
  count         INTEGER NOT NULL,
  last_ts       INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_norm, next_norm)
);

CREATE INDEX IF NOT EXISTS idx_transition_prev ON transition(scope, prev_norm);

CREATE TABLE IF NOT EXISTS command_score (
  scope         TEXT NOT NULL,            -- 'global' or repo_key
  cmd_norm      TEXT NOT NULL,
  score         REAL NOT NULL,
  last_ts       INTEGER NOT NULL,
  PRIMARY KEY(scope, cmd_norm)
);

CREATE INDEX IF NOT EXISTS idx_command_score_scope ON command_score(scope, score DESC);

CREATE TABLE IF NOT EXISTS project_task (
  repo_key      TEXT NOT NULL,
  kind          TEXT NOT NULL,            -- built-in or user-defined tool id
  name          TEXT NOT NULL,
  command       TEXT NOT NULL,
  description   TEXT,
  discovered_ts INTEGER NOT NULL,
  PRIMARY KEY(repo_key, kind, name)
);

CREATE INDEX IF NOT EXISTS idx_project_task_repo ON project_task(repo_key);

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_ts INTEGER NOT NULL
);
```

---

## 5) Database Migration Strategy (Required)

### 5.1 Who runs migrations?
`clai-daemon` runs migrations on startup before serving requests.

### 5.2 Concurrency safety
- Acquire `${DB_DIR}/.daemon.lock` (advisory lock).
- While lock is held:
  - open DB
  - apply migrations in order
  - perform stale socket cleanup (Unix; 3.4)
  - start listening
- If lock cannot be acquired: exit ("daemon already running").

### 5.3 Compatibility policy
- Refuse to run if DB schema version > supported version.
- Forward-only migrations.

---

## 6) Shell Integration (Ingestion Pipeline) — Detailed & Safe

### 6.1 Command transport (Required)
**Never pass cmd/cwd as CLI arguments.** No `--cmd "$BASH_COMMAND"`.

Allowed transports:
1. **Environment variables** (preferred for small payloads)
2. **stdin** to `clai-hook` (preferred if size may be large)

Required patterns:

**Env var method (zsh/bash)**
```sh
CLAI_CMD="$cmd" CLAI_CWD="$PWD" CLAI_EXIT="$status" CLAI_TS="$ts" CLAI_SHELL="zsh" CLAI_SESSION_ID="$sid" clai-hook ingest
```

**Env var method (fish)** — Note: fish requires `env` prefix for inline variable assignment:
```fish
env CLAI_CMD="$cmd" CLAI_CWD="$PWD" CLAI_EXIT="$status" CLAI_TS="$ts" CLAI_SHELL="fish" CLAI_SESSION_ID="$sid" clai-hook ingest
```

**stdin method**
```sh
printf '%s' "$cmd" | CLAI_CWD="$PWD" ... clai-hook ingest --cmd-stdin
```

`clai-hook` is responsible for escaping/encoding, not shell.

### 6.2 Environment size limits (Required)
- If `${#cmd} > 32768` (32KB):
  - prefer stdin mode (`--cmd-stdin`) if available
  - else skip ingestion for that command (default)
  - optional alternative: truncate + suffix marker, configurable

### 6.3 Non-UTF8 safety (Required)
- Shell never builds JSON.
- `clai-hook` performs lossy UTF-8 conversion before JSON encoding.

### 6.4 Prompt blocking prevention (Required)
`clai-hook` must not freeze the terminal.

Rules:
- Connect timeout: **~15ms** (configurable, range 10–20ms).
- Write timeout: small (10–20ms).
- No ACK / no read.
- If daemon is busy or socket missing: drop event.

Shell hooks should:
- redirect `clai-hook` stderr to `/dev/null` to avoid prompt noise.
- avoid running hook if `clai-hook` missing.

### 6.5 Session ID generation (Required)
Generating UUIDs in bash without forking is non-trivial.

Accepted strategies:

**Strategy A (daemon-assigned) — preferred**
- Shell calls `clai-hook session-start` once per shell instance.
- `clai-hook` asks daemon for a session id with micro-timeout.
- `clai-hook` writes session id to a temp file:
  - `${XDG_RUNTIME_DIR}/clai/session.$PPID` or `/tmp/clai-$UID/session.$PPID`
- Shell hook reads it and exports `CLAI_SESSION_ID` for future calls.

If daemon not reachable:
- fall back to Strategy B.

**Strategy B (shell-local fallback)**
- Bash: `session_id = sha256(hostname + $$ + start_epoch_seconds + random)` where random may be `$RANDOM` repeated.
- Zsh: can use built-in `$RANDOM` and `$EPOCHREALTIME`.
- Fish: use `random` + `date +%s%N` (best effort).

Session id must be stable per shell instance.

**Session file cleanup:**
- Shell hooks should register cleanup on exit: `trap '_clai_cleanup' EXIT` (bash/zsh)
- Fish: use `function _clai_cleanup --on-event fish_exit`
- Cleanup removes `${XDG_RUNTIME_DIR}/clai/session.$$`
- Daemon periodically prunes stale session files (PID no longer exists) during idle periods

### 6.6 Duration capture (Required)
- zsh: `$EPOCHREALTIME` preexec/precmd (requires `zsh/datetime` module)
- fish: `$CMD_DURATION` (milliseconds, built-in)
- bash:
  - default: `$SECONDS` integer seconds
  - optional high precision mode (uses `date`, documented overhead; see 1.2.1)

### 6.7 Bash PROMPT_COMMAND handling (Required)

#### 6.7.1 Version detection
```bash
_clai_bash_version_ge() {
  local major=${BASH_VERSINFO[0]:-0}
  local minor=${BASH_VERSINFO[1]:-0}
  [[ $major -gt $1 ]] || { [[ $major -eq $1 ]] && [[ $minor -ge $2 ]]; }
}
```

#### 6.7.2 Safe PROMPT_COMMAND modification
```bash
if _clai_bash_version_ge 4 4; then
  # Bash 4.4+: use array form (preferred)
  PROMPT_COMMAND+=('_clai_postexec')
else
  # Bash 4.0-4.3: safe string append with semicolon
  case "$PROMPT_COMMAND" in
    *_clai_postexec*) ;;  # already present
    '') PROMPT_COMMAND='_clai_postexec' ;;
    *';') PROMPT_COMMAND+='_clai_postexec' ;;
    *) PROMPT_COMMAND+=';_clai_postexec' ;;
  esac
fi
```

### 6.8 Bash/zsh/fish hook requirements (behavioral)
- Hooks must be idempotent and not modify user prompt formatting.
- Hooks must guard recursion:
  - do not ingest when command is `clai-hook` itself.
- Hooks must not run in non-interactive shells by default, unless user opts in.

### 6.9 Interactive shell detection (Required)

Hooks must detect interactive mode to avoid running in scripts/subshells:

```bash
# Bash
[[ $- == *i* ]] || return

# Zsh
[[ -o interactive ]] || return

# Fish
status is-interactive; or return
```

### 6.9.1 Tool prerequisites
Hooks should gracefully degrade if tools are missing:

```bash
# Bash/Zsh: sha256 fallback (Linux uses sha256sum, macOS uses shasum)
_clai_sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum
  else
    # Fallback: use random + timestamp (less unique but functional)
    cat
  fi
}

# Git is optional for hooks (daemon computes git context)
# Hooks do NOT require git
```

### 6.10 Incognito mode integration (NEW)
Two supported modes:

**A) No-send (simplest):**
- `clai incognito on` sets `CLAI_NO_RECORD=1` in shell session
- hooks skip ingestion entirely when set

**B) Ephemeral (recommended):**
- `clai incognito on` sets `CLAI_EPHEMERAL=1`
- hooks keep sending events but include `ephemeral=true`
- daemon uses them for in-memory session context only; **never writes to SQLite** and must not update persisted aggregates

Default: **Ephemeral mode** (best UX; keeps current-session suggestions useful).

### 6.11 Pipe/subshell detection (Should-have)
To avoid recording intermediate pipeline commands:
```bash
# Bash/Zsh: skip if in pipeline
[[ -t 1 ]] || return  # stdout not a tty = likely in pipe
```

Fish handles this via `status is-interactive` which already excludes non-interactive contexts.

---

## 7) Repo Identification (repo_key) and Canonicalization

### 7.1 Canonical repo_root (Nice-to-have, recommended)
To avoid split history via symlinks:
- canonicalize repo_root (prefer `git rev-parse --show-toplevel`)
- if needed, canonicalize physical path (like `pwd -P`)

### 7.2 repo_key computation
`repo_key = SHA256(lower(remote_url) + "|" + canonical(repo_root))`
If no remote: `SHA256("local|" + canonical(repo_root))`

### 7.3 Git context performance
- **Computed by daemon**, not hooks (to avoid git fork in every postcmd)
- Refresh only when cwd changes, git-related command, or TTL expired (1–3s)
- Cache keyed by `cwd`

---

## 8) Normalization and Tokenization (Required)

### 8.1 Tokenizer strategy
No regex tokenizers.

Use a library:
- `github.com/google/shlex` (default)
- `mvdan.cc/sh/v3/syntax` (optional future for bash)

### 8.2 Normalization rules
- preserve command/subcommand + flags
- slot replacements:
  - `<path>`: token looks like a path (`/`, `./`, `../`, `~`, contains `/`)
  - `<num>`: digits
  - `<sha>`: 7–40 hex
  - `<url>`: `http(s)://` or `git@...:`
  - `<msg>`: commit messages in common patterns (`git commit -m ...`)
  - allow command-specific "typed slots" where useful (e.g., `<ns>` for kubectl `-n`)
- deterministic output

### 8.3 Command-specific normalization (starter set)
- git:
  - `git commit -m <msg>`
  - `git checkout -b <branch>`
  - `git push <remote> <branch>`
- npm/pnpm/yarn:
  - `install <pkg>`, `run <script>`
- go:
  - `go test <path|./...>`
- pytest:
  - `pytest <path>`

---

## 9) Aggregates and Learning (Expanded)

### 9.1 Decayed command frequency (`command_score`)
- `d = exp(-(now - last_ts)/tau_ms)`
- `score = score * d + 1.0`
- update global + repo scopes

Default `tau_ms`: ~7 days (configurable).
- Minimum `tau_ms`: 1 day (86400000 ms) — prevents scores from decaying too fast
- If configured below minimum, clamp and log warning

### 9.2 Transitions (`transition`)
- previous cmd_norm in same session_id (fallback: same repo within last N minutes)
- update global + repo scopes

### 9.3 Semantic slot filling (NEW)
Track per-template slot value frequencies so suggestions can be rendered with highly probable arguments.

#### 9.3.1 Slot model
For a normalized command template:
- `cmd_norm = "kubectl get pods -n <arg>"`
- Identify slot positions (e.g., `<arg>` at slot_idx=0 for this template)
- Extract concrete values from the raw command aligned to the template

#### 9.3.2 Persistence (recommended)
Store top-K values per slot in SQLite (or keep in memory + periodic flush). Persisted storage enables long-term learning.

Add table:
```sql
CREATE TABLE IF NOT EXISTS slot_value (
  scope     TEXT NOT NULL,          -- 'global' or repo_key
  cmd_norm  TEXT NOT NULL,
  slot_idx  INTEGER NOT NULL,       -- index among slot tokens
  value     TEXT NOT NULL,          -- concrete argument value
  count     REAL NOT NULL,          -- decayed count (float)
  last_ts   INTEGER NOT NULL,
  PRIMARY KEY(scope, cmd_norm, slot_idx, value)
);

CREATE INDEX IF NOT EXISTS idx_slot_value_lookup
  ON slot_value(scope, cmd_norm, slot_idx, count DESC);
```

#### 9.3.3 Update rule (decayed histogram)
On ingest of a non-ephemeral event:
- for each observed slot value:
  - decay existing `count` based on elapsed time
  - increment by 1
- keep only top-K per slot (default K=20, minimum K=1, maximum K=100) using periodic cleanup:
  - delete values ranked > K
  - if K configured outside range, clamp and log warning

Decay function can reuse the same exponential approach as `command_score`.

#### 9.3.4 Rendering using slot histograms
When proposing a template containing slots:
- fill each slot from:
  1) repo-scoped top value
  2) global-scoped top value
  3) last-used value fallback
- only fill when confidence is high (e.g., top value has ≥2x count of second)

---

## 10) Task Discovery (Built-in + User-defined) + Ingestion Bursts

### 10.1 Built-in discovery
- package.json scripts
- Makefile targets (Mode A heuristic / Mode B `make -qp`)

### 10.2 User-defined discovery config (NEW)
Support a config file:
- `~/.config/clai/discovery.yaml` (and/or TOML)
- Each entry maps a file pattern to a discovery runner and parser.

Example YAML:
```yaml
- file_pattern: "Justfile"
  kind: "just"
  runner: "just --list --json"
  parser:
    type: "json_keys"
    path: "recipes"
  timeout_ms: 300
  max_output_bytes: 1048576
```

#### 10.2.1 Runner safety constraints (required)
All discovery commands must run with:
- working directory = repo root
- timeout (default 200–500ms; per-rule override allowed)
- output cap (default 1MB; per-rule override allowed)
- no stdin
- environment sanitized (no secrets added)
- never run as root (or require explicit opt-in)

**Error handling:**
- If runner exits non-zero: log warning, skip this discovery source, continue with others
- If runner times out: log warning with command, skip this source
- If parser fails (invalid JSON, regex mismatch): log warning with parse error, skip this source
- Discovery errors must never fail daemon startup or crash the daemon
- Debug endpoint `/debug/discovery-errors` shows recent failures (last 100)

#### 10.2.2 Parser types (initial set)
- `json_keys`: extract keys from JSON object at `path`
- `json_array`: extract string array at `path`
- `regex_lines`: apply regex to each line; capture group defines task name
- `make_qp`: specialized parser for `make -qp` output (if enabled)

Each discovered task yields:
- `kind` (tool id)
- `name`
- `command` (template command to run)
- optional `description`

### 10.3 Event burst handling (Nice-to-have, recommended)
Batch ingested events into SQLite:
- flush every 25–50ms or 100 events
- prevents lock churn during loops/scripts

---

## 11) Suggestions, Pre-computation, Error-aware UX (Expanded)

### 11.1 Candidate sources (unchanged)
- repo transitions
- global transitions
- repo frequency
- global frequency
- project tasks (built-in + config)
- context defaults

### 11.2 Proactive pre-computation ("zero-latency") (NEW)
Compute suggestions on ingest, cache in memory, and serve instantly.

#### 11.2.1 Cache model
Maintain in daemon memory:
- `session_suggest_cache[session_id] = {last_event_id, computed_at, suggestions[3]}`
- optional: `repo_suggest_cache[repo_key]` for "cold session" fallback

#### 11.2.2 Compute trigger
On each `command_end` ingest:
1) Update in-memory aggregates (always; even for ephemeral events, session-local only)
2) Compute top 3 suggestions for that session context
3) Store in cache

#### 11.2.3 Serving logic
`/suggest` should:
- return cached suggestions if:
  - cache exists AND matches most recent session event AND not expired
  - cache TTL: **30 seconds** (configurable via `CLAI_CACHE_TTL_MS`)
- else compute synchronously (DB-backed) and populate cache

**Goal:** UI path avoids SQLite queries in the critical interaction loop.

### 11.3 Scoring (unchanged; now also used in precompute)
Combine:
- transition strength `log(count+1)`
- frequency strength `log(score+1)`
- project/task boosts
- slot-fill confidence boost
- safety penalties (optional)

Return top 1–3.

### 11.4 Scoring weights (reference starting point)

| Source                    | Weight |
|---------------------------|--------|
| Repo transition match     | +80    |
| Global transition match   | +60    |
| Repo frequency            | +30    |
| Project task              | +20    |
| Dangerous command penalty | -50    |

These are tunable; adjust based on observed suggestion quality.

### 11.5 De-duplication
Deduplicate by `cmd_norm`, merge reasons and score contributions.

### 11.6 Did You Mean? (NEW)
Handle typo correction explicitly when previous command failed.

#### 11.6.1 Trigger
If `exit_code != 0`, particularly:
- `127` (command not found)
- other common "unknown subcommand" cases can be added later (needs stderr to be perfect; without it we stay conservative)

#### 11.6.2 Candidate set
Use repo-scoped high-frequency commands first, then global:
- retrieve top-N cmd_norm (and/or cmd_raw) from `command_score` (N=200–1000)
- optionally also consult FTS (12) for better fuzzy retrieval

#### 11.6.3 Fuzzy match
Compute similarity between:
- the misspelled token (e.g., first word) and candidate first tokens
- use Damerau-Levenshtein distance or trigram similarity
- only suggest when:
  - similarity above threshold (default: **0.7** Damerau-Levenshtein, configurable) AND
  - candidate is high frequency (default: **top 10%** by score, configurable)

#### 11.6.4 UX
If match found, inject a top suggestion:
- `git status` for `gti status`
- rank above standard next-step predictions for that immediate response

### 11.7 Incognito behavior in suggestions
- If `ephemeral=true`:
  - update only session-local in-memory model
  - cached suggestions may incorporate ephemeral context
  - no persistence and no long-term learning updates

---

## 12) History Search via SQLite FTS5 (NEW)

### 12.1 Feature
Provide instant history search:
- `clai search "docker run"`
- fuzzy-ish matching via FTS5

### 12.2 Schema (FTS5)
If SQLite build supports FTS5:
- create an FTS virtual table and keep it in sync.

Example:
```sql
CREATE VIRTUAL TABLE IF NOT EXISTS command_fts
USING fts5(cmd_raw, repo_key, cwd, content='command_event', content_rowid='id');
```

Synchronization approach (choose one):
- **Triggers** on `command_event` insert/update/delete
- **Daemon-managed** writes during ingestion batching (preferred for control)

### 12.3 Query behavior
- allow filtering by repo_key
- return top K results ordered by bm25 score and recency
- use this also as an optional candidate retriever for "Did You Mean?"

### 12.4 Safety note
- Do not index ephemeral events.
- If raw command storage is disabled, FTS indexing is disabled or indexes normalized-only strings.

### 12.5 FTS5 unavailability fallback
If SQLite is built without FTS5:
- Daemon logs warning on startup: "FTS5 not available; history search disabled"
- `/search` endpoint returns structured error (see 15.3)
- `clai search` CLI shows user-friendly message with instructions
- Optional fallback: LIKE-based search (slower, configurable via `CLAI_SEARCH_FALLBACK=like`)

---

## 13) Signal Handling (Required)

### 13.1 Daemon signal handling

| Signal | Action | Notes |
|--------|--------|-------|
| SIGTERM | Graceful shutdown | Flush pending writes, close DB, remove socket, exit 0 |
| SIGINT | Graceful shutdown | Same as SIGTERM (allows Ctrl+C during dev) |
| SIGHUP | Reload config | Re-read discovery.yaml, refresh task caches |
| SIGPIPE | Ignore | Prevents crash when client disconnects mid-write |

### 13.2 Implementation
```go
func setupSignals(ctx context.Context, cancel context.CancelFunc) {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
    signal.Ignore(syscall.SIGPIPE)  // Critical: prevents crash on client disconnect

    go func() {
        for sig := range sigCh {
            switch sig {
            case syscall.SIGTERM, syscall.SIGINT:
                log.Info("shutdown signal received", "signal", sig)
                cancel()
                return
            case syscall.SIGHUP:
                log.Info("reloading configuration")
                reloadConfig()
            }
        }
    }()
}
```

### 13.3 Graceful shutdown sequence
1. Stop accepting new connections
2. Wait for in-flight requests (with timeout, e.g., 5s)
3. Flush pending SQLite batch
4. Close database connection
5. Remove socket file (Unix)
6. Exit 0

### 13.4 Daemon crash recovery
If daemon crashes unexpectedly:
- Shell hooks continue to work (fire-and-forget drops events)
- Stale socket cleaned up on next daemon start (see 3.4)
- User can manually restart with `clai daemon start`

---

## 14) Suggestion Invocation and Presentation (Required)

This section defines how users invoke and interact with suggestions. **This is critical UX**.

### 14.1 Invocation methods

| Method | Binding | Description |
|--------|---------|-------------|
| Explicit | `clai suggest` | CLI command, returns suggestions to stdout |
| Keybind | User-configured | Shell keybind (e.g., `Ctrl+Space`) calls `clai suggest` |
| Widget (zsh) | ZLE widget | Native Zsh Line Editor integration |
| Abbrev (fish) | Abbreviation | Fish abbreviation expansion |

### 14.2 Output format
```
clai suggest [--format=json|text|fzf]
```

**Text format** (default, for human reading):
```
1. npm test          (project task, freq)
2. git push          (transition from: git commit)
3. make build        (freq)
```

**JSON format** (for programmatic use):
```json
{"suggestions":[{"cmd":"npm test","score":12.3,"reasons":["project_task","freq"]}]}
```

**fzf format** (for piping to fzf):
```
npm test
git push
make build
```

### 14.3 Zsh ZLE widget example
```zsh
_clai_suggest_widget() {
  local suggestions
  suggestions=$(clai suggest --format=fzf 2>/dev/null)
  if [[ -n "$suggestions" ]]; then
    local selected
    selected=$(echo "$suggestions" | fzf --height=10 --reverse)
    if [[ -n "$selected" ]]; then
      LBUFFER="$selected"
      zle redisplay
    fi
  fi
}
zle -N _clai_suggest_widget
bindkey '^@' _clai_suggest_widget  # Ctrl+Space
```

### 14.4 Fish keybind example
```fish
function _clai_suggest
  set -l cmd (clai suggest --format=fzf 2>/dev/null | fzf --height=10 --reverse)
  if test -n "$cmd"
    commandline -r $cmd
  end
  commandline -f repaint
end
bind \e\  _clai_suggest  # Alt+Space (Ctrl+Space often taken)
```

### 14.5 Bash readline integration
```bash
_clai_suggest() {
  local suggestions
  suggestions=$(clai suggest --format=fzf 2>/dev/null | fzf --height=10 --reverse)
  if [[ -n "$suggestions" ]]; then
    READLINE_LINE="$suggestions"
    READLINE_POINT=${#READLINE_LINE}
  fi
}
bind -x '"\C- ": _clai_suggest'  # Ctrl+Space
```

### 14.6 Tab completion integration (Nice-to-have)
For shells that support custom completion:
- Hook into tab completion to offer suggestions as completion candidates
- Only when line is empty or matches prefix patterns

---

## 15) API (Updated)

### 15.1 Ingest event (NDJSON)
Example (shell never emits JSON; `clai-hook` does):
```json
{
  "v": 1,
  "type": "command_end",
  "ts": 1730000000123,
  "session_id": "uuid",
  "shell": "zsh",
  "cwd": "/path",
  "cmd_raw": "git commit -m \"fix\"",
  "exit_code": 0,
  "duration_ms": 420,
  "ephemeral": false
}
```

Note: `git` context is **computed by daemon** from `cwd`, not sent by hooks.

### 15.2 /suggest response (cache-first)
```json
{
  "suggestions": [
    {"cmd":"npm run test","cmd_norm":"npm run test","score":12.34,"reasons":["hot_cache","project_task","freq_repo"],"confidence":0.82}
  ],
  "context": {"cache":"hit","repo_key":"…","last_cmd_norm":"git status"}
}
```

### 15.3 /search endpoint (optional)
Request:
```json
{"query":"docker run","repo_key":"optional","limit":20}
```

Response:
```json
{
  "results": [
    {"cmd_raw":"docker run -it ubuntu bash","ts":1730000000123,"cwd":"/home/user","repo_key":"..."}
  ],
  "total": 42,
  "truncated": true
}
```

If FTS5 unavailable, returns error:
```json
{"error":"fts5_unavailable","message":"History search requires SQLite with FTS5. Rebuild SQLite or use pattern matching."}
```

### 15.4 Debug endpoints (optional, dev/debug builds)
- `GET /debug/scores` — view command_score table (filterable by scope)
- `GET /debug/transitions` — view transition counts
- `GET /debug/tasks` — view discovered project tasks
- `GET /debug/cache` — view suggestion cache state

---

## 16) Environment Variables Reference

### 16.1 Hook → clai-hook communication

| Variable | Required | Description |
|----------|----------|-------------|
| `CLAI_CMD` | Yes* | Raw command string (*or use `--cmd-stdin`) |
| `CLAI_CWD` | Yes | Current working directory |
| `CLAI_EXIT` | Yes | Exit code of command |
| `CLAI_TS` | Yes | Timestamp (Unix ms) |
| `CLAI_SHELL` | Yes | Shell name: `bash`, `zsh`, `fish` |
| `CLAI_SESSION_ID` | Yes | Session identifier |
| `CLAI_DURATION_MS` | No | Command duration in milliseconds |
| `CLAI_EPHEMERAL` | No | If `1`, event is ephemeral (incognito) |
| `CLAI_NO_RECORD` | No | If `1`, skip ingestion entirely |

### 16.2 User configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CLAI_SOCKET_PATH` | Auto | Override daemon socket path |
| `CLAI_CONNECT_TIMEOUT_MS` | `15` | Connect timeout for fire-and-forget |
| `CLAI_DEBUG` | `0` | Enable debug logging |
| `CLAI_CONFIG_DIR` | `~/.config/clai` | Config directory |
| `CLAI_DATA_DIR` | `~/.local/share/clai` | Data directory (DB) |

### 16.3 Namespace convention
All clai environment variables use `CLAI_` prefix to avoid conflicts.

---

## 17) Complete Shell Hook Reference

### 17.1 Zsh hook
```zsh
# ~/.config/clai/hooks/clai.zsh

# Guard: only in interactive shells
[[ -o interactive ]] || return 0

# Guard: don't re-source
(( ${+_CLAI_LOADED} )) && return 0
_CLAI_LOADED=1

# Check clai-hook exists
(( ${+commands[clai-hook]} )) || return 0

# Load datetime module for EPOCHREALTIME
zmodload zsh/datetime 2>/dev/null

# Session ID (lazy init)
typeset -g _CLAI_SESSION_ID=""
typeset -g _CLAI_PREEXEC_TS=""
typeset -g _CLAI_LAST_CMD=""

_clai_sha256() {
  if (( ${+commands[shasum]} )); then
    shasum -a 256
  elif (( ${+commands[sha256sum]} )); then
    sha256sum
  else
    cat  # fallback
  fi
}

_clai_get_session_id() {
  if [[ -z "$_CLAI_SESSION_ID" ]]; then
    local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
    if [[ -f "$session_file" ]]; then
      _CLAI_SESSION_ID=$(<"$session_file")
    else
      # Fallback: generate locally
      _CLAI_SESSION_ID=$(printf '%s-%s-%s' "${HOST:-localhost}" "$$" "$EPOCHREALTIME" | _clai_sha256 | cut -c1-16)
    fi
  fi
  echo "$_CLAI_SESSION_ID"
}

_clai_preexec() {
  # Skip if recording disabled
  [[ -n "$CLAI_NO_RECORD" ]] && return

  # Store command and start time
  _CLAI_LAST_CMD="$1"
  _CLAI_PREEXEC_TS="$EPOCHREALTIME"
}

_clai_precmd() {
  local exit_code=$?

  # Skip if no command recorded or recording disabled
  [[ -z "$_CLAI_LAST_CMD" || -n "$CLAI_NO_RECORD" ]] && return

  # Skip if command is clai-hook itself
  [[ "$_CLAI_LAST_CMD" == clai-hook* ]] && return

  # Calculate duration
  local duration_ms=0
  if [[ -n "$_CLAI_PREEXEC_TS" ]]; then
    local now="$EPOCHREALTIME"
    duration_ms=$(( (now - _CLAI_PREEXEC_TS) * 1000 ))
    duration_ms=${duration_ms%.*}  # truncate to int
  fi

  # Get timestamp
  local ts_ms=$(( EPOCHREALTIME * 1000 ))
  ts_ms=${ts_ms%.*}

  # Determine ephemeral flag
  local ephemeral=0
  [[ -n "$CLAI_EPHEMERAL" ]] && ephemeral=1

  # Fire and forget
  CLAI_CMD="$_CLAI_LAST_CMD" \
  CLAI_CWD="$PWD" \
  CLAI_EXIT="$exit_code" \
  CLAI_TS="$ts_ms" \
  CLAI_DURATION_MS="$duration_ms" \
  CLAI_SHELL="zsh" \
  CLAI_SESSION_ID="$(_clai_get_session_id)" \
  CLAI_EPHEMERAL="$ephemeral" \
  clai-hook ingest 2>/dev/null &!

  # Clear for next command
  _CLAI_LAST_CMD=""
  _CLAI_PREEXEC_TS=""
}

# Cleanup on shell exit
_clai_cleanup() {
  local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
  [[ -f "$session_file" ]] && rm -f "$session_file"
}

# Register hooks
autoload -Uz add-zsh-hook
add-zsh-hook preexec _clai_preexec
add-zsh-hook precmd _clai_precmd
add-zsh-hook zshexit _clai_cleanup
```

### 17.2 Bash hook
```bash
# ~/.config/clai/hooks/clai.bash

# Guard: only in interactive shells
[[ $- == *i* ]] || return 0

# Guard: don't re-source
[[ -n "$_CLAI_LOADED" ]] && return 0
_CLAI_LOADED=1

# Check clai-hook exists
command -v clai-hook >/dev/null 2>&1 || return 0

# Session ID (lazy init)
_CLAI_SESSION_ID=""
_CLAI_PREEXEC_TS=""
_CLAI_LAST_CMD=""
_CLAI_LAST_HIST=""

# Version check helper
_clai_bash_version_ge() {
  local major=${BASH_VERSINFO[0]:-0}
  local minor=${BASH_VERSINFO[1]:-0}
  [[ $major -gt $1 ]] || { [[ $major -eq $1 ]] && [[ $minor -ge $2 ]]; }
}

_clai_sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum
  else
    # Fallback: just use input (less unique but functional)
    cat
  fi
}

_clai_get_session_id() {
  if [[ -z "$_CLAI_SESSION_ID" ]]; then
    local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
    if [[ -f "$session_file" ]]; then
      _CLAI_SESSION_ID=$(<"$session_file")
    else
      # Fallback: generate locally
      _CLAI_SESSION_ID=$(printf '%s-%s-%s-%s' "${HOSTNAME:-localhost}" "$$" "$SECONDS" "$RANDOM$RANDOM" | _clai_sha256 | cut -c1-16)
    fi
  fi
  echo "$_CLAI_SESSION_ID"
}

# Preexec via DEBUG trap
_clai_preexec() {
  # Only capture on real commands, not PROMPT_COMMAND
  [[ -n "$COMP_LINE" ]] && return  # Skip during completion

  local this_hist
  this_hist=$(HISTTIMEFORMAT='' history 1)

  # Skip if same as last (prevents double-capture)
  [[ "$this_hist" == "$_CLAI_LAST_HIST" ]] && return
  _CLAI_LAST_HIST="$this_hist"

  # Extract command (remove history number)
  _CLAI_LAST_CMD="${this_hist#*[0-9]  }"
  _CLAI_PREEXEC_TS="$SECONDS"
}

_clai_postexec() {
  local exit_code=$?

  # Skip if no command or recording disabled
  [[ -z "$_CLAI_LAST_CMD" || -n "$CLAI_NO_RECORD" ]] && return

  # Skip clai-hook commands
  [[ "$_CLAI_LAST_CMD" == clai-hook* ]] && return

  # Calculate duration (seconds only in bash default mode)
  local duration_ms=0
  if [[ -n "$_CLAI_PREEXEC_TS" ]]; then
    duration_ms=$(( (SECONDS - _CLAI_PREEXEC_TS) * 1000 ))
  fi

  # Timestamp (seconds precision)
  local ts_ms
  ts_ms=$(date +%s)000

  # Ephemeral flag
  local ephemeral=0
  [[ -n "$CLAI_EPHEMERAL" ]] && ephemeral=1

  # Fire and forget (use subshell to background)
  (
    CLAI_CMD="$_CLAI_LAST_CMD" \
    CLAI_CWD="$PWD" \
    CLAI_EXIT="$exit_code" \
    CLAI_TS="$ts_ms" \
    CLAI_DURATION_MS="$duration_ms" \
    CLAI_SHELL="bash" \
    CLAI_SESSION_ID="$(_clai_get_session_id)" \
    CLAI_EPHEMERAL="$ephemeral" \
    clai-hook ingest 2>/dev/null
  ) &
  disown 2>/dev/null

  # Clear for next command
  _CLAI_LAST_CMD=""
  _CLAI_PREEXEC_TS=""
}

# Cleanup on shell exit
_clai_cleanup() {
  local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
  [[ -f "$session_file" ]] && rm -f "$session_file"
}
trap '_clai_cleanup' EXIT

# Set up DEBUG trap for preexec
trap '_clai_preexec' DEBUG

# Set up PROMPT_COMMAND for postexec
if _clai_bash_version_ge 4 4; then
  PROMPT_COMMAND+=('_clai_postexec')
else
  case "$PROMPT_COMMAND" in
    *_clai_postexec*) ;;
    '') PROMPT_COMMAND='_clai_postexec' ;;
    *';') PROMPT_COMMAND+='_clai_postexec' ;;
    *) PROMPT_COMMAND+=';_clai_postexec' ;;
  esac
fi
```

### 17.3 Fish hook
```fish
# ~/.config/clai/hooks/clai.fish

# Guard: only in interactive shells
status is-interactive; or exit 0

# Guard: don't re-source
set -q _CLAI_LOADED; and exit 0
set -g _CLAI_LOADED 1

# Check clai-hook exists
command -q clai-hook; or exit 0

# Session ID
set -g _CLAI_SESSION_ID ""

function _clai_sha256
  if command -q shasum
    shasum -a 256
  else if command -q sha256sum
    sha256sum
  else
    cat  # fallback
  end
end

function _clai_get_session_id
  if test -z "$_CLAI_SESSION_ID"
    set -l session_file (test -n "$XDG_RUNTIME_DIR"; and echo "$XDG_RUNTIME_DIR"; or echo "/tmp")"/clai/session."(echo %self)
    if test -f "$session_file"
      set _CLAI_SESSION_ID (cat "$session_file")
    else
      # Fallback: generate locally
      set _CLAI_SESSION_ID (printf '%s-%s-%s' (hostname) %self (date +%s) | _clai_sha256 | cut -c1-16)
    end
  end
  echo $_CLAI_SESSION_ID
end

function _clai_postexec --on-event fish_postexec
  set -l exit_code $status
  set -l cmd $argv[1]

  # Skip if recording disabled
  test -n "$CLAI_NO_RECORD"; and return

  # Skip clai-hook commands
  string match -q "clai-hook*" -- "$cmd"; and return

  # CMD_DURATION is in milliseconds
  set -l duration_ms $CMD_DURATION

  # Timestamp
  set -l ts_ms (date +%s)000

  # Ephemeral flag
  set -l ephemeral 0
  test -n "$CLAI_EPHEMERAL"; and set ephemeral 1

  # Get session ID (must evaluate before env call)
  set -l session_id (_clai_get_session_id)

  # Fire and forget (fish uses env for inline var assignment)
  env CLAI_CMD="$cmd" \
      CLAI_CWD="$PWD" \
      CLAI_EXIT="$exit_code" \
      CLAI_TS="$ts_ms" \
      CLAI_DURATION_MS="$duration_ms" \
      CLAI_SHELL="fish" \
      CLAI_SESSION_ID="$session_id" \
      CLAI_EPHEMERAL="$ephemeral" \
      clai-hook ingest 2>/dev/null &
  disown 2>/dev/null
end

# Cleanup on shell exit
function _clai_cleanup --on-event fish_exit
  set -l session_file (test -n "$XDG_RUNTIME_DIR"; and echo "$XDG_RUNTIME_DIR"; or echo "/tmp")"/clai/session."(echo %self)
  test -f "$session_file"; and rm -f "$session_file"
end
```

### 17.4 Hook distribution
Hooks are installed via:
1. `clai init [bash|zsh|fish]` — outputs source line for user's rc file
2. Package managers (brew, apt) — install to standard locations
3. Manual copy from `~/.config/clai/hooks/`

---

## 18) Terminal Capabilities (Nice-to-have)

### 18.1 Detection
For suggestion display formatting:
```go
func supportsColor() bool {
    if os.Getenv("NO_COLOR") != "" {
        return false
    }
    if os.Getenv("TERM") == "dumb" {
        return false
    }
    // Check if stdout is a terminal
    if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
        return false
    }
    return true
}
```

### 18.2 Width detection
```go
func terminalWidth() int {
    if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
        return w
    }
    if cols := os.Getenv("COLUMNS"); cols != "" {
        if w, err := strconv.Atoi(cols); err == nil {
            return w
        }
    }
    return 80 // default
}
```

---

## 19) Testing Plan (Expanded for new features)

### 19.1 Shell integration tests (critical)
- Ensure cmd transport works for complex quoting:
  - `git commit -m "fix: \"quoted\" work"`
  - commands with newlines, pipes, redirects, unicode
- Ensure hooks never block prompt:
  - simulate daemon down; verify no delay
- Ensure environment/stdin transport works across shells.
- **Fish syntax**: verify `env` prefix pattern works correctly

### 19.2 clai-hook tests
- Non-UTF8 inputs:
  - ensure lossy UTF-8 conversion before JSON
- Timeout behavior:
  - connect/write timeout enforced; event dropped fast
- No ACK behavior:
  - verify hook never reads from socket.

### 19.3 Daemon + SQLite tests
- migration lock:
  - simulate concurrent startups; assert only one migrates
- batching:
  - ingest bursts; verify single transaction and no lock errors
- normalization:
  - tokenizer library correctness; golden tests

### 19.4 Windows transport tests (unit/contract)
- Ensure transport is abstracted and Unix socket logic is not hardcoded in higher layers.
- Define pipe path builder for Windows and unit test it.

### 19.5 Bash PROMPT_COMMAND tests
- Existing PROMPT_COMMAND empty/non-empty
- Missing trailing semicolon
- Bash 4.4+ array behavior
- Older bash fallback correctness

### 19.6 Env size limit tests
- Ensure hook switches to stdin or skips when cmd > 32KB
- Ensure no E2BIG failures caused by clai integration in fixtures

### 19.7 Daemon stale socket tests (Nice-to-have)
- Simulate leftover socket file; verify daemon unlinks and starts cleanly after lock acquired.

### 19.8 Repo canonicalization tests (Nice-to-have)
- Same repo accessed via symlink and real path yields same repo_key.

### 19.9 Proactive cache tests
- ingest event → verify session cache populated
- verify /suggest is cache hit after ingest
- invalidation on new event and expiration after TTL

### 19.10 Slot histogram tests
- ingest multiple commands with slot values
- verify top-K behavior, decay math, and confidence-based filling
- verify repo-scope overrides global scope

### 19.11 Incognito tests
- `ephemeral=true` events:
  - must not write to command_event
  - must not update transition/command_score/slot_value persistent tables
  - may update in-memory cache for same session

### 19.12 User-defined discovery tests
- load discovery.yaml fixtures
- enforce timeouts and output caps
- validate parsers (json_keys/json_array/regex_lines)
- ensure tasks written to project_task with correct kind/name/command

### 19.13 Did You Mean tests
- simulate exit_code=127 with typo command
- ensure fuzzy match suggests the high-frequency intended command
- ensure thresholds prevent noisy suggestions

### 19.14 FTS5 tests
- verify FTS table sync on insert
- query returns expected matches
- ensure ephemeral events not indexed
- ensure feature disabled gracefully if FTS5 unavailable

### 19.15 Signal handling tests
- SIGTERM triggers graceful shutdown
- SIGPIPE ignored (no crash on client disconnect)
- SIGHUP reloads config without restart

### 19.16 Interactive mode tests
- Verify hooks don't run in non-interactive shells
- Test each shell's detection mechanism

---

## 20) Operations and Maintenance

### 20.1 Retention policy
Configurable retention to bound storage growth:
- Keep last N days of `command_event` (default: 90 days)
- Aggregates (`command_score`, `transition`, `slot_value`) can be kept longer
- Provide CLI: `clai-daemon --rebuild-aggregates` to recompute from retained history

### 20.2 Purge implementation
Periodic purge (e.g., daily or on daemon startup):
```sql
DELETE FROM command_event WHERE ts < (now_ms - retention_days * 86400000);
```
Optionally vacuum after large deletes.

### 20.3 Daemon management
```bash
clai daemon start    # Start daemon (foreground or daemonize)
clai daemon stop     # Graceful shutdown via SIGTERM
clai daemon status   # Check if running, show PID
clai daemon restart  # Stop + start
```

### 20.4 Logging

**Log format (JSON lines):**
```json
{"ts":"2024-01-15T10:30:00Z","level":"info","msg":"daemon started","version":"1.2.0","pid":12345}
{"ts":"2024-01-15T10:30:01Z","level":"warn","msg":"discovery runner timeout","kind":"just","timeout_ms":500}
{"ts":"2024-01-15T10:30:02Z","level":"error","msg":"sqlite error","error":"database is locked"}
```

**Log levels:**
- `debug` — Verbose (enabled via `CLAI_DEBUG=1`)
- `info` — Startup, shutdown, config reload
- `warn` — Non-fatal issues (discovery failures, dropped events)
- `error` — Fatal issues requiring attention

**Startup message includes:**
- Version and git commit
- Config file path loaded
- Database path and schema version
- Socket path
- FTS5 availability

### 20.5 Metrics (Nice-to-have)

Optional Prometheus metrics endpoint at `/metrics`:
```
clai_events_ingested_total{shell="zsh"} 1234
clai_events_dropped_total{reason="timeout"} 5
clai_suggestions_served_total{cache="hit"} 890
clai_suggestions_served_total{cache="miss"} 110
clai_daemon_uptime_seconds 3600
clai_db_size_bytes 1048576
```

Enabled via `CLAI_METRICS=1` or config.

### 20.6 Dropped event visibility (Debug mode)

When `CLAI_DEBUG=1`:
- Daemon logs all dropped events with reason
- `clai daemon status` shows recent drop count
- Debug endpoint `/debug/drops` shows last 100 dropped events with timestamps

---

## 21) Acceptance Criteria (v3)

### Must
1. Cmd transport uses env/stdin, never CLI args.
2. Non-UTF8 safe JSON: lossy conversion in `clai-hook`.
3. Fire-and-forget ingestion: short timeouts, no ACK, never blocks prompt.
4. Daemon runs migrations on startup with lock.
5. Incognito/ephemeral mode:
   - ephemeral events never persisted and never indexed (FTS) by default.
6. **Signal handling**: SIGTERM graceful shutdown, SIGPIPE ignored.
7. **Interactive detection**: hooks only run in interactive shells.
8. **Fish compatibility**: use `env` prefix for inline variable assignment.

### Should
9. Transport abstraction supports Windows named pipes; pipe path standard defined.
10. Env var size limits handled (>32KB uses stdin or skips).
11. Precompute cache implemented; /suggest is cache-first.
12. Slot histograms implemented (persisted or periodically persisted).
13. User-defined discovery config supported with runner safety constraints.
14. **Bash version detection**: PROMPT_COMMAND uses array on 4.4+, safe fallback otherwise.
15. **Git context computed by daemon**, not hooks.
16. **Suggestion invocation defined**: CLI output formats, shell keybind examples.

### Nice-to-have
17. Stale socket cleanup on daemon start (Unix).
18. repo_root canonicalization avoids split history via symlinks.
19. Batched ingestion reduces SQLite churn on bursts.
20. Tab completion integration.
21. Terminal capability detection for formatted output.
22. Accessibility considerations (screen reader support via plain text mode).

---

## Appendix A: CLI Reference

Complete command inventory for clai.

### A.1 Main CLI (`clai`)

| Command | Description | Example |
|---------|-------------|---------|
| `clai suggest` | Get next-command suggestions | `clai suggest --format=json` |
| `clai search <query>` | Search command history (requires FTS5) | `clai search "docker run" --repo` |
| `clai incognito on\|off` | Toggle incognito/ephemeral mode | `clai incognito on` |
| `clai init <shell>` | Output shell integration code | `clai init zsh >> ~/.zshrc` |
| `clai daemon <cmd>` | Daemon management | `clai daemon start` |
| `clai version` | Show version info | `clai version` |

**`clai suggest` flags:**
- `--format=text|json|fzf` — Output format (default: text)
- `--limit=N` — Number of suggestions (default: 3, max: 10)

**`clai search` flags:**
- `--repo` — Limit search to current repository
- `--limit=N` — Max results (default: 20)
- `--json` — JSON output

**`clai daemon` subcommands:**
- `start` — Start daemon (foreground by default, `-d` for daemonize)
- `stop` — Graceful shutdown via SIGTERM
- `status` — Check if running, show PID and uptime
- `restart` — Stop then start
- `--rebuild-aggregates` — Recompute aggregates from retained history

### A.2 Hook Binary (`clai-hook`)

Internal binary called by shell hooks. Not typically invoked directly.

| Command | Description |
|---------|-------------|
| `clai-hook ingest` | Ingest command event from env vars |
| `clai-hook ingest --cmd-stdin` | Ingest with command from stdin |
| `clai-hook session-start` | Request session ID from daemon |

### A.3 Environment Variables

See Section 16 for complete environment variable reference.

---

## Appendix B: Glossary

| Term | Definition |
|------|------------|
| `cmd_raw` | The original command string as entered by user |
| `cmd_norm` | Normalized command with arguments replaced by typed slots |
| `repo_key` | SHA256 hash uniquely identifying a repository |
| `ephemeral` | Event that is not persisted to disk (incognito mode) |
| `slot` | Placeholder in normalized command (e.g., `<path>`, `<msg>`) |
| `transition` | Markov bigram: P(next_cmd | prev_cmd) |
| `fire-and-forget` | Send without waiting for acknowledgment |

---

## Appendix C: Version History

| Version | Date | Changes |
|---------|------|---------|
| v1 | - | Initial spec: core ingestion, aggregates, suggestions |
| v2 | - | Windows transport, env size limits, timing precision |
| v3 | - | Proactive pre-computation, slot filling, incognito, FTS5, signal handling, complete shell hooks |

---
