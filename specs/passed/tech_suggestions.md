# clai Non-PTY Suggestions Engine (Go + SQLite) — Technical Specification (Revised)

This document specifies a **non-PTY**, **non-LLM** next-command suggestion engine for clai. It incorporates all review feedback from **Blocker → Nice-to-Have**, with particular emphasis on the “dirty path”: safe, fast, cross-shell ingestion.

---

## 0) Goals, Non-goals, Guarantees

### 0.1 Goals
- “Wow” next-command suggestions without an LLM, based on:
  - command history + repo context
  - lightweight statistics (Markov bigrams + decayed frequency)
  - project task discovery (Makefile targets, package scripts, etc.)
- Cross-shell on macOS/Linux:
  - **bash**, **zsh**, **fish**
- Windows later (daemon + storage architecture must not prevent a Windows client).

### 0.2 Non-goals
- PTY interception, stdout/stderr parsing
- Full shell parsing / semantic correctness for all shells (we are best-effort on normalization)
- Remote sync / multi-machine federation

### 0.3 Hard Guarantees (Ingestion safety)
- **No user prompt blocking**: ingestion must be **fire-and-forget** with micro-timeouts.
- **No quoting/escaping bugs**: command transport **must not use CLI arguments** for cmd strings or cwd.
- **No UTF-8 assumptions**: NDJSON must be produced only after lossy/escaped conversion in a binary, not shell.
- **No insecure socket path**: user-scoped runtime directory with proper permissions.
- **Schema migrations are automatic and safe**: daemon runs migrations with locking.

---

## 1) Inputs and Capture (No PTY)

### 1.1 Captured fields (minimum)
For every executed command (postexec/postcmd/postprompt):
- `cmd_raw` (string as provided by shell)
- `cwd`
- `ts` (unix ms)
- `exit_code`
- `shell` (bash|zsh|fish)
- `session_id`

Optional but high value:
- `duration_ms` (best effort; see 6.5)
- git context:
  - `repo_root`, `remote_url`, `branch`, `dirty` (best effort)

### 1.2 Timing precision by shell
- **zsh**: high precision via `$EPOCHREALTIME` (microseconds as string)
- **fish**: duration available via `$CMD_DURATION` (ms)
- **bash**: no native high-precision; duration is **best effort** (see 6.5)

---

## 2) Architecture Overview

### 2.1 Components
**A) Shell hooks**
- Install minimal hook code in each shell to call a tiny helper binary `clai-hook`.
- Hook passes data via **environment variables or stdin** (never CLI args).
- Hook must never block user prompt.

**B) `clai-hook` (small helper executable)**
- Reads event fields from **env vars** and/or **stdin**.
- Performs:
  - **lossy UTF-8 conversion** before JSON encoding
  - NDJSON serialization
  - **non-blocking send** to daemon with micro-timeouts
- Drops events if daemon is unavailable/busy.

**C) `clai-daemon` (Go)**
- Receives NDJSON events over local IPC.
- Normalizes commands into templates (`cmd_norm`).
- Stores history and aggregates in SQLite (WAL).
- Performs project task discovery.
- Serves suggestions via local API.

**D) SQLite**
- command history + derived stats + task cache
- migrations managed by daemon

### 2.2 Data flow
1. Shell hook calls `clai-hook ingest` with data in env/stdin.
2. `clai-hook` serializes NDJSON safely and attempts to send to daemon (5ms connect timeout, no ACK).
3. `clai-daemon` validates, normalizes, writes to SQLite in batched transactions, updates aggregates.
4. UI requests suggestions from `clai-daemon`.

---

## 3) IPC, Socket Paths, Security

### 3.1 Socket path standard (Required)
**Never** use a global `/tmp/clai.sock`.

Use:
1. `$XDG_RUNTIME_DIR/clai/daemon.sock` (preferred on Linux)
2. macOS fallback: `$TMPDIR/clai-$UID/daemon.sock` (or `/tmp/clai-$UID/daemon.sock`)
3. Ensure directory permissions are strict:
   - directory mode: `0700`
   - socket file inherits secure ownership; verify owner is current UID

**Rationale:** avoid multi-user collisions and permission hijacking.

### 3.2 IPC protocol
- Prefer **Unix domain socket** (macOS/Linux).
- Payload is NDJSON, one event per line.
- Daemon reads until newline; does not require an ACK response.

### 3.3 Fire-and-forget policy (Required)
`clai-hook` must:
- Set **connect timeout ~5ms** (configurable).
- Write with a short write timeout (e.g., 5–10ms).
- **Never wait for daemon response**.
- On any delay/error: drop event silently.

This prevents prompt freezes.

---

## 4) Storage Model (SQLite)

### 4.1 SQLite configuration
- WAL mode
- one writer goroutine
- prepared statements
- batching window (see 10.2)

Recommended pragmas:
- `PRAGMA journal_mode=WAL;`
- `PRAGMA synchronous=NORMAL;` (configurable)
- `PRAGMA foreign_keys=ON;`

### 4.2 Schema
```sql
CREATE TABLE IF NOT EXISTS session (
  id            TEXT PRIMARY KEY,
  created_at    INTEGER NOT NULL,     -- unix ms
  shell         TEXT NOT NULL,        -- bash|zsh|fish
  host          TEXT,
  user          TEXT
);

CREATE TABLE IF NOT EXISTS command_event (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id    TEXT NOT NULL REFERENCES session(id),
  ts            INTEGER NOT NULL,         -- unix ms (completion time)
  duration_ms   INTEGER,
  exit_code     INTEGER,
  cwd           TEXT NOT NULL,
  repo_key      TEXT,
  branch        TEXT,
  cmd_raw       TEXT NOT NULL,
  cmd_norm      TEXT NOT NULL
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
  kind          TEXT NOT NULL,            -- 'make'|'npm'|'just'|'taskfile'|...
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

## 5) Database Migration Strategy (Blocker — Required)

### 5.1 Who runs migrations?
**`clai-daemon` runs migrations on startup** before serving requests.

### 5.2 Concurrency safety
To prevent concurrent migration attempts:
- On startup, daemon acquires a **file lock** on the DB directory, e.g.:
  - lock file: `${DB_DIR}/.migrate.lock`
  - use OS advisory locking (fcntl/flock)
- Only after lock is held:
  - open DB
  - apply pending migrations in order
  - write to `schema_migrations`
  - release lock (or keep a “daemon lock” to prevent multiple daemons)

### 5.3 Compatibility policy
- Daemon should refuse to run if schema version is newer than supported binary (clear error).
- Migrations are forward-only.

---

## 6) Shell Integration (Ingestion Pipeline) — Detailed & Safe

> This is the highest-risk area. The rules below are mandatory.

### 6.1 Command transport (Blocker — Required)
**Never pass cmd/cwd as CLI arguments.** No `--cmd "$BASH_COMMAND"`.

Allowed transports:
1. **Environment variables** (preferred for small payloads)
2. **stdin** to `clai-hook` (preferred if size may be large)

Required patterns:

**Env var method**
```sh
CLAI_CMD="$cmd" CLAI_CWD="$PWD" CLAI_EXIT="$status" CLAI_TS="$ts" CLAI_SHELL="zsh" CLAI_SESSION_ID="$sid" clai-hook ingest
```

**stdin method**
```sh
printf '%s' "$cmd" | CLAI_CWD="$PWD" ... clai-hook ingest --cmd-stdin
```

`clai-hook` is responsible for escaping/encoding, not shell.

### 6.2 NDJSON and non-UTF8 safety (Blocker — Required)
Shell commands and filenames may contain bytes that are not valid UTF-8. JSON requires valid UTF-8.

Rules:
- Shell hook must **not** attempt to create JSON.
- `clai-hook` must:
  - read `cmd_raw` as bytes (from env or stdin)
  - convert to UTF-8 **lossy** before JSON encoding

Implementation requirement in `clai-hook`:
- If input bytes are invalid UTF-8, replace invalid sequences with the Unicode replacement character (�) (or a configured placeholder).
- Then serialize JSON.

### 6.3 Prompt blocking prevention (Should Address — Required)
`clai-hook` must not freeze the terminal.

Rules:
- Connect timeout: **~5ms** (configurable).
- Write timeout: small (5–10ms).
- No ACK / no read.
- If daemon is busy or socket missing: drop event.

Shell hooks should:
- redirect `clai-hook` stderr to `/dev/null` to avoid prompt noise.
- avoid running hook if `clai-hook` missing.

### 6.4 Session ID generation (Should Address — Required)
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

### 6.5 Duration capture (Should Address — Required)
- zsh: compute duration via `EPOCHREALTIME` in preexec/precmd.
- fish: use `$CMD_DURATION` (ms).
- bash: define as **best effort**:
  - Option 1 (low precision, no fork): use `$SECONDS` (integer seconds)
  - Option 2 (higher precision, with fork): use `date +%s%N` (or `%s%3N`) in preexec and precmd; document the overhead

Default for bash:
- use `$SECONDS` (cheap) unless user enables high precision mode.

### 6.6 Bash/zsh/fish hook requirements (behavioral)
- Hooks must be idempotent and not modify user prompt formatting.
- Hooks must guard recursion:
  - do not ingest when command is `clai-hook` itself.
- Hooks must not run in non-interactive shells by default, unless user opts in.

---

## 7) Repo Identification (repo_key) and Git Context

### 7.1 repo_key computation
`repo_key = SHA256(lower(remote_url) + "|" + canonical(repo_root))`
If no remote:
`repo_key = SHA256("local|" + canonical(repo_root))`

### 7.2 Git context performance
- Resolve git context only when:
  - cwd changed, OR
  - command relates to git, OR
  - cached value expired (TTL 1–3s)
- Prefer daemon-side gitctx (Go) if hooks become too heavy, but ensure daemon calls are also fire-and-forget for the shell.

---

## 8) Normalization and Tokenization (Should Address — Required)

### 8.1 Tokenizer strategy (Blocker-level importance)
Do not implement a regex tokenizer.

Use a real parser library:
- For shell-like tokenization: `github.com/google/shlex` (simple)
- For bash parsing: `mvdan.cc/sh/v3/syntax` (more correct; heavier)

Policy:
- Use `shlex` for general tokenization across shells for now.
- If needed later, switch to `mvdan/sh` for bash-specific correctness.

### 8.2 Normalization rules
- Convert raw command string into template `cmd_norm`.
- Preserve command/subcommand and flag structure.
- Replace slots:
  - `<path>`: token looks like a path (`/`, `./`, `../`, `~`, contains `/`)
  - `<num>`: digits
  - `<sha>`: 7–40 hex
  - `<url>`: `http(s)://` or `git@...:`
  - `<msg>`: commit messages in common patterns (`git commit -m ...`)

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

## 9) Aggregates and Learning

### 9.1 Decayed frequency (`command_score`)
Exponential decay:
- `d = exp(-(now - last_ts)/tau_ms)`
- `score = score * d + 1.0`
- `last_ts = now`

Default `tau_ms` ~ 7 days (configurable).

Update both scopes:
- `global`
- `repo_key` if present

### 9.2 Transitions (`transition`)
- Find previous `cmd_norm` in same `session_id` (fallback: same repo within last N minutes)
- Update transition counts for:
  - `scope=global`
  - `scope=repo_key` if present

---

## 10) Project Task Discovery (Makefile, package scripts)

### 10.1 Makefile parsing (Should Address — Required)
Regex parsing is brittle.

Provide two modes:

**Mode A: Best-effort regex (fast, incomplete)**
- Accept that it may miss complex makefiles and match non-runnable targets.
- Filter known special targets (`.PHONY`, `.SUFFIXES`, etc.)

**Mode B: Authoritative target list (recommended where available)**
- Use: `make -qp` (prints database) and parse targets.
- Pros: more correct
- Cons: runs external process; may be slower; may execute makefile parsing side effects in some environments (generally safe with `-qp`, but still must be cautious)

Default:
- Mode A for safety/perf, with a config option to enable Mode B.

### 10.2 Event burst handling (Nice-to-Have — implement now if easy)
Even though this is a non-PTY engine, daemons may receive bursts of events (scripts/loops).

Requirement:
- Batch ingested events into **a single SQLite transaction** using a short batching window:
  - e.g., flush every 25–50ms or every 100 events, whichever comes first
- Prevent excessive lock churn and improve throughput.

---

## 11) Suggestion Engine

### 11.1 Candidate sources
- repo transitions for `(repo_key, last_cmd_norm)`
- global transitions for `(global, last_cmd_norm)`
- repo top commands by decayed frequency
- global top commands
- project tasks (repo-specific)
- context defaults (safe heuristics)

### 11.2 Scoring
Combine signals:
- transition strength: `log(count+1)`
- frequency strength: `log(score+1)`
- project task boost
- repo scope boost
- optional safety penalties (risky commands)

Return top 1–3 suggestions.

### 11.3 Scoring weights (reference starting point)

| Source                    | Weight |
|---------------------------|--------|
| Repo transition match     | +80    |
| Global transition match   | +60    |
| Repo frequency            | +30    |
| Project task              | +20    |
| Dangerous command penalty | -50    |

These are tunable; adjust based on observed suggestion quality.

### 11.4 De-duplication
Deduplicate by `cmd_norm`, merge reasons and score contributions.

### 11.5 Slot filling (no-PTY safe)
Only fill slots when confidence is high:
- reuse last-used args for same template in same repo
- else present template unchanged

---

## 12) Daemon API

### 12.1 Endpoints (local)
- `GET /healthz`
- `POST /ingest` (NDJSON lines or JSON per request; prefer NDJSON for low overhead)
- `POST /suggest`

### 12.2 Suggest request/response
Request:
```json
{"session_id":"uuid","cwd":"/path","repo_key":"optional","limit":3}
```

Response:
```json
{
  "suggestions": [
    {"cmd":"npm run test","cmd_norm":"npm run test","score":12.34,"reasons":["project_task","freq_repo"],"confidence":0.82}
  ],
  "context": {"repo_key":"…","last_cmd_norm":"git status","used_sources":["repo_transition","project_task"]}
}
```

### 12.3 Debug endpoints (optional, dev/debug builds)
- `GET /debug/scores` — view command_score table (filterable by scope)
- `GET /debug/transitions` — view transition counts
- `GET /debug/tasks` — view discovered project tasks

---

## 13) Testing Plan (Expanded for ingestion pitfalls)

### 13.1 Shell integration tests (critical)
- Ensure cmd transport works for complex quoting:
  - `git commit -m "fix: \"quoted\" work"`
  - commands with newlines, pipes, redirects, unicode
- Ensure hooks never block prompt:
  - simulate daemon down; verify no delay
- Ensure environment/stdin transport works across shells.

### 13.2 clai-hook tests
- Non-UTF8 inputs:
  - ensure lossy UTF-8 conversion before JSON
- Timeout behavior:
  - connect/write timeout enforced; event dropped fast
- No ACK behavior:
  - verify hook never reads from socket.

### 13.3 Daemon + SQLite tests
- migration lock:
  - simulate concurrent startups; assert only one migrates
- batching:
  - ingest bursts; verify single transaction and no lock errors
- normalization:
  - tokenizer library correctness; golden tests

---

## 14) Operations and Maintenance

### 14.1 Retention policy
Configurable retention to bound storage growth:
- Keep last N days of `command_event` (default: 90 days)
- Aggregates (`command_score`, `transition`) can be kept longer
- Provide CLI: `clai-daemon --rebuild-aggregates` to recompute from retained history

### 14.2 Purge implementation
Periodic purge (e.g., daily or on daemon startup):
```sql
DELETE FROM command_event WHERE ts < (now_ms - retention_days * 86400000);
```
Optionally vacuum after large deletes.

---

## 15) Acceptance Criteria (Updated)

### Blockers (must)
1. **Cmd transport uses env/stdin, never CLI args** (no quoting hell).
2. **Non-UTF8 safe NDJSON**: `clai-hook` performs lossy conversion; shell never constructs JSON.
3. **Fire-and-forget ingestion**: micro-timeouts, no prompt freeze, no ACK.
4. **Migrations**: daemon runs migrations on startup with DB directory lock.

### Should Address (required)
5. Bash duration strategy explicitly documented (low precision default; optional high precision with overhead).
6. Session ID generation strategy defined (daemon-assigned preferred; bash fallback).
7. Tokenizer uses a real library (shlex/mvdan), not regex.
8. Socket path uses XDG_RUNTIME_DIR or secure per-user temp dir.

### Nice-to-Have
9. Batch ingestion to tolerate event bursts and reduce SQLite lock churn.
10. Makefile target discovery supports “authoritative” mode via `make -qp` or clearly states best-effort limitations.

---
