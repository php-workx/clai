# clai Non-PTY Suggestions Engine (Go + SQLite) — Technical Specification (Revised v2)

This document specifies a **non-PTY**, **non-LLM** next-command suggestion engine for clai. It incorporates all review feedback from **Blocker → Nice-to-Have**, including the latest edge-case guidance on Windows readiness, Bash hook safety, and transport limits.

---

## 0) Goals, Non-goals, Guarantees

### 0.1 Goals
- “Wow” next-command suggestions without an LLM, based on:
  - command history + repo context
  - lightweight statistics (Markov bigrams + decayed frequency)
  - project task discovery (Makefile targets, package scripts, etc.)
- Cross-shell on macOS/Linux:
  - **bash**, **zsh**, **fish**
- Windows later (daemon + transport must support Named Pipes without refactors).

### 0.2 Non-goals
- PTY interception, stdout/stderr parsing
- Full shell parsing / semantic correctness for all shells (best-effort normalization)
- Remote sync / multi-machine federation

### 0.3 Hard Guarantees (Ingestion safety)
- **No user prompt blocking**: ingestion is **fire-and-forget** with short timeouts.
- **No quoting/escaping bugs**: command transport **never uses CLI arguments** for cmd strings or cwd.
- **No UTF-8 assumptions**: JSON is produced only by `clai-hook` after lossy conversion.
- **Secure, user-scoped IPC paths** (no global /tmp socket collisions).
- **Schema migrations are automatic and safe**: daemon runs migrations with locking.

---

## 1) Inputs and Capture (No PTY)

### 1.1 Captured fields (minimum)
For every executed command:
- `cmd_raw` (string as provided by shell; may be truncated/skipped if too large)
- `cwd`
- `ts` (unix ms)
- `exit_code`
- `shell` (bash|zsh|fish)
- `session_id`

Optional but high value:
- `duration_ms` (best effort; see 6.6)
- git context:
  - `repo_root`, `remote_url`, `branch`, `dirty` (best effort)

### 1.2 Timing precision by shell
- **zsh**: high precision via `$EPOCHREALTIME` (microseconds as string)
- **fish**: duration available via `$CMD_DURATION` (ms)
- **bash**: no native high-precision; duration is **best effort** (see 6.6)

---

## 2) Architecture Overview

### 2.1 Components
**A) Shell hooks**
- Minimal hook code in each shell calls `clai-hook`.
- Hook passes data via **environment variables or stdin** (never CLI args).
- Hook must not block prompt.

**B) `clai-hook` (small helper executable)**
- Reads event fields from **env vars** and/or **stdin**.
- Performs:
  - lossy UTF-8 conversion
  - NDJSON serialization
  - non-blocking send to daemon with short timeouts
- Drops events if daemon is unavailable/busy.

**C) `clai-daemon` (Go)**
- Receives events over transport abstraction (Unix socket now, Windows named pipe later).
- Normalizes commands into templates (`cmd_norm`).
- Stores history and aggregates in SQLite (WAL).
- Performs project task discovery.
- Serves suggestions via local API.

**D) SQLite**
- command history + derived stats + task cache
- migrations managed by daemon

### 2.2 Data flow
1. Shell hook calls `clai-hook ingest` with data in env/stdin.
2. `clai-hook` serializes NDJSON safely and attempts to send to daemon (15ms connect timeout, no ACK).
3. `clai-daemon` validates, normalizes, writes to SQLite in batched transactions, updates aggregates.
4. UI requests suggestions from `clai-daemon`.

---

## 3) Transport, IPC, Socket/Pipe Paths, Security

> **Should Address (Windows):** define transport abstraction now to avoid refactoring later.

### 3.1 Transport abstraction (Required)
Implement a transport interface in Go immediately:
- Unix: `net.Listener` on Unix domain socket
- Windows: Named pipe listener (e.g., `winio.ListenPipe`) behind the same interface

**Requirement:** All code above the transport layer consumes an `io.Reader` stream of NDJSON lines; it must not depend on filesystem socket specifics.

### 3.2 Path standards (Unix + Windows)
**Unix (macOS/Linux):**
1. `$XDG_RUNTIME_DIR/clai/daemon.sock` (preferred)
2. fallback: `$TMPDIR/clai-$UID/daemon.sock` (macOS) or `/tmp/clai-$UID/daemon.sock`

Directory permissions:
- directory mode: `0700`
- verify socket owner is current UID (best effort)

**Windows (defined now):**
- Named pipe path standard (initial):
  - `\\.\pipe\clai-<SID>-daemon`
- `<SID>` is the Windows user SID string (preferred), or a stable username hash if SID retrieval is not available.
- Pipe ACL must restrict access to the current user.

### 3.3 Timeouts and fire-and-forget (Updated)
**Should Address:** 5ms is too aggressive.

`clai-hook` must:
- Use a short, configurable timeout budget:
  - **default connect timeout: 15ms**
  - allowed range: **10–20ms**
- Use a short write timeout (e.g., 10–20ms).
- Never wait for daemon ACK (no read).
- On timeout/error: drop event silently.

This remains well below human perception but tolerates scheduler jitter.

### 3.4 Nice-to-have: stale socket cleanup on daemon start
If daemon crashes hard, Unix socket file may remain.

Requirement (Nice-to-have, recommended to implement now):
- After acquiring the “daemon lock” (see 5.2), daemon should:
  - unlink/remove existing socket file at the expected path *before* calling `Listen`
  - then create/listen on the socket
This avoids `EADDRINUSE` from stale files.

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

## 5) Database Migration Strategy (Required)

### 5.1 Who runs migrations?
**`clai-daemon` runs migrations on startup** before serving requests.

### 5.2 Concurrency safety
- Acquire a **file lock** on DB directory:
  - `${DB_DIR}/.daemon.lock` (also functions as “single daemon” lock)
- While lock is held:
  - open DB
  - apply migrations
  - perform stale socket cleanup (3.4)
  - start listening
- If lock cannot be acquired: daemon must exit with a clear message (“daemon already running”).

### 5.3 Compatibility policy
- Daemon refuses to run if schema version is newer than supported binary.
- Migrations are forward-only.

---

## 6) Shell Integration (Ingestion Pipeline) — Detailed & Safe

> This is the highest-risk area. The rules below are mandatory.

### 6.1 Command transport (Required)
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

### 6.2 Environment variable size limits (Should Address)
OS environments have size limits; huge one-liners can cause spawn failures (E2BIG).

Requirement:
- Hooks must enforce a safe threshold for env-based transport:
  - if `${#cmd} > 32768` (32KB), do **one** of:
    - Prefer: switch to stdin transport (`--cmd-stdin`)
    - Or: skip ingestion for that command
    - Or: truncate with a suffix marker (configurable; default: skip or stdin)
- Do not attempt env transport for massive commands.

**Default policy:**
- If cmd length > 32KB: use stdin mode if available; else skip.

### 6.3 NDJSON and non-UTF8 safety (Required)
Shell commands and filenames may contain bytes that are not valid UTF-8. JSON requires valid UTF-8.

Rules:
- Shell hook must **not** attempt to create JSON.
- `clai-hook` must:
  - read `cmd_raw` as bytes (from env or stdin)
  - convert to UTF-8 **lossy** before JSON encoding

Implementation requirement in `clai-hook`:
- If input bytes are invalid UTF-8, replace invalid sequences with the Unicode replacement character (�) (or a configured placeholder).
- Then serialize JSON.

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

### 6.6 Duration capture (Required)
- zsh: use `$EPOCHREALTIME`
- fish: `$CMD_DURATION` (ms)
- bash: best effort:
  - default: `$SECONDS` precision (integer seconds)
  - optional “high precision” mode using `date` in preexec/precmd, with documented overhead

### 6.7 Bash PROMPT_COMMAND recursion safety (Should Address)
Appending strings to `PROMPT_COMMAND` is brittle.

Requirement:
- For **Bash 4.4+**, use array form:
  - `PROMPT_COMMAND+=('clai_postexec')`
- For older bash versions:
  - use string concatenation only after ensuring separator correctness:
    - if PROMPT_COMMAND is non-empty and does not end with `;` or newline, append `;`
    - then append `clai_postexec`
- Hooks must also avoid recursion (do not ingest when running `clai-hook` itself).

### 6.8 Bash/zsh/fish hook requirements (behavioral)
- Hooks must be idempotent and not modify user prompt formatting.
- Hooks must guard recursion:
  - do not ingest when command is `clai-hook` itself.
- Hooks must not run in non-interactive shells by default, unless user opts in.

---

## 7) Repo Identification (repo_key) and Git Context

### 7.1 repo_root canonicalization (Nice-to-have)
Symlinks can create split histories for the same repo.

Requirement (Nice-to-have, recommended):
- Canonicalize repo_root before hashing into repo_key:
  - prefer git’s canonical root from `git rev-parse --show-toplevel`
  - and/or canonicalize using physical path (`pwd -P` equivalent)
- Repo key must be derived from canonical root to avoid duplicates.

### 7.2 repo_key computation
`repo_key = SHA256(lower(remote_url) + "|" + canonical(repo_root))`
If no remote:
`repo_key = SHA256("local|" + canonical(repo_root))`

### 7.3 Git context performance
- Resolve git context only when:
  - cwd changed, OR
  - git-related command, OR
  - cached value expired (TTL 1–3s)

---

## 8) Normalization and Tokenization (Required)

### 8.1 Tokenizer strategy
Do not implement a regex tokenizer.

Use a library:
- `github.com/google/shlex` (general tokenization, lightweight)
- `mvdan.cc/sh/v3/syntax` (bash parsing, heavier; optional future)

Policy:
- Start with `shlex` across shells.
- If required for bash correctness, add `mvdan/sh` behind feature flag.

### 8.2 Normalization rules
- Preserve command/subcommand + flag structure.
- Replace slots:
  - `<path>`: token looks like a path (`/`, `./`, `../`, `~`, contains `/`)
  - `<num>`: digits
  - `<sha>`: 7–40 hex
  - `<url>`: `http(s)://` or `git@...:`
  - `<msg>`: commit messages in common patterns (`git commit -m ...`)
- Must be deterministic.

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
- `d = exp(-(now - last_ts)/tau_ms)`
- `score = score * d + 1.0`
- update `last_ts = now`
- update both scopes: global + repo

### 9.2 Transitions (`transition`)
- previous command in same session_id (fallback: same repo within last N minutes)
- update global + repo scopes

---

## 10) Project Task Discovery

### 10.1 Makefile targets
Two modes:
- Mode A (default): best-effort heuristic parsing + filtering
- Mode B (optional): authoritative targets from `make -qp`

Document tradeoffs and let user enable Mode B.

### 10.2 Event burst handling (Nice-to-have, recommended to implement now)
Batch ingestion into a single SQLite transaction:
- flush every 25–50ms or 100 events
- reduces lock churn, improves throughput during script loops

---

## 11) Suggestion Engine

### 11.1 Candidate sources
- repo transitions
- global transitions
- repo frequency
- global frequency
- project tasks
- context defaults

### 11.2 Scoring
Combine:
- `log(count+1)` transitions
- `log(score+1)` frequency
- repo/task boosts
- optional safety penalties

Return top 1–3.

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
- `POST /ingest` (NDJSON stream or per-event JSON; NDJSON preferred)
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

## 13) Testing Plan (Expanded)

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

### 13.4 Windows transport tests (unit/contract)
- Ensure transport is abstracted and Unix socket logic is not hardcoded in higher layers.
- Define pipe path builder for Windows and unit test it.

### 13.5 Bash PROMPT_COMMAND tests
- Existing PROMPT_COMMAND empty/non-empty
- Missing trailing semicolon
- Bash 4.4+ array behavior
- Older bash fallback correctness

### 13.6 Env size limit tests
- Ensure hook switches to stdin or skips when cmd > 32KB
- Ensure no E2BIG failures caused by clai integration in fixtures

### 13.7 Daemon stale socket tests (Nice-to-have)
- Simulate leftover socket file; verify daemon unlinks and starts cleanly after lock acquired.

### 13.8 Repo canonicalization tests (Nice-to-have)
- Same repo accessed via symlink and real path yields same repo_key.

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

## 15) Acceptance Criteria (Updated v2)

> Note: Acceptance criteria numbering updated to account for new Operations section.

### Blockers (must)
1. Cmd transport uses env/stdin, never CLI args.
2. Non-UTF8 safe JSON: lossy conversion in `clai-hook`.
3. Fire-and-forget ingestion: short timeouts, no ACK, never blocks prompt.
4. Daemon runs migrations on startup with lock.

### Should Address (required)
5. Transport abstraction supports Windows named pipes without refactor; Windows pipe path standard defined.
6. Bash PROMPT_COMMAND recursion safety uses array method in bash 4.4+; safe fallback for older bash.
7. Env var size limits handled (>=32KB uses stdin or skips).
8. “Micro-timeout” budget set to 10–20ms (default 15ms).
9. Tokenizer uses real library, not regex.
10. Socket path uses XDG_RUNTIME_DIR or secure per-user temp dir.

### Nice-to-Have
11. Daemon unlinks stale socket after lock acquisition.
12. repo_root canonicalization avoids split history from symlink paths.
13. Batch ingestion reduces SQLite churn under event bursts.

---
