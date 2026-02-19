# clai Suggestions Engine Consolidated Specification (`suggestions_tech_v1.md`)

This document is the consolidated baseline suggestions specification for clai. It merges and normalizes requirements from:

- `/Users/runger/.claude-worktrees/clai/spec-reviews/specs/passed/tech_suggestions.md`
- `/Users/runger/.claude-worktrees/clai/spec-reviews/specs/passed/tech_suggestions_v2.md`
- `/Users/runger/.claude-worktrees/clai/spec-reviews/specs/tech_suggestions_ext_v1.md`
- `/Users/runger/.claude-worktrees/clai/spec-reviews/specs/tech_suggestions_ext_v1_appendix.md`

## 1. Purpose, Scope, and Exclusions

### 1.1 Purpose
This document defines a decision-complete, production-ready technical contract for the non-PTY, non-LLM suggestions engine.

### 1.2 In Scope
All non-excluded requirements, algorithms, schemas, APIs, CLI contracts, safety guarantees, operational behavior, testing, observability, and configuration from the source specifications are in scope.

### 1.3 Explicit Exclusions
The following feature family is excluded from normative runtime behavior in this consolidated spec:

- multi-step sequence-mining feature set
- all tables, retrieval sources, boosts, activation/mining logic, config keys, tests, and metrics tied to that feature set
- playbook sequence seeding and `workflows:` playbook block semantics
- any ranking/reason/traceability rule that depends on that feature set as an active signal

The excluded items are listed explicitly in Section 24.

### 1.4 Normative Keywords
The keywords `MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, and `MAY` are used as normative requirement terms.

## 2. Goals, Non-goals, and Guarantees

### 2.1 Goals
The engine MUST:

- predict useful next commands with low latency and deterministic behavior
- reduce typing effort, command lookup effort, and recovery time after command failures
- remain fail-open so shell interactivity is never blocked by daemon or storage failures
- support bash, zsh, and fish on Unix-like platforms
- provide explainable ranking without hot-path LLM dependency

### 2.2 UX and Latency Targets
The implementation SHOULD meet:

- `Suggest` P50 latency < 15ms
- `Suggest` P95 latency < 50ms on warm cache
- `Suggest` P95 latency < 120ms on cold compute
- persistent helper shell overhead < 3ms median, < 8ms P95 per command completion
- fallback helper overhead < 8ms median, < 15ms P95 per command completion

### 2.3 Non-goals
The engine is not responsible for:

- PTY capture or stdout/stderr stream parsing
- complete shell AST replay for all shell grammar edge cases
- cloud sync or multi-device profile sharing
- LLM-dependent hot-path ranking

### 2.4 Hard Safety Guarantees
The implementation MUST guarantee:

- no prompt blocking in hook path
- no transport of raw command/cwd through CLI args
- JSON generation only inside helper/binary path (never in shell script)
- UTF-8 safety via lossy conversion before serialization
- secure user-scoped runtime directories and IPC endpoints
- automatic, safe startup migration behavior with locking

## 3. Runtime Prerequisites and Platform Support

### 3.1 Platform Scope
Current runtime support is Unix-first:

- first-class: macOS, Linux, WSL
- shells: bash, zsh, fish
- native Windows runtime behavior is out of current active scope; pipe-related details are forward-compatibility guidance only

### 3.2 Shell Versions
- bash >= 4.0 supported; >= 4.4 preferred
- zsh >= 5.0
- fish >= 3.0

Install-time probe SHOULD emit explicit diagnostics on unsupported versions.

### 3.3 Runtime Prerequisites
Runtime environment MUST provide:

- writable home/config/runtime directories
- local filesystem runtime dir with advisory lock semantics
- SQLite with WAL support
- CSPRNG source for session entropy fallback

FTS5 SHOULD be enabled. A fallback search path MUST exist when FTS5 is unavailable.

## 4. Cross-shell Contract

### 4.1 Event Lifecycle
Shell adapters MUST emit:

- `session_start`
- `command_start`
- `command_end`
- optional telemetry `suggest_request`
- `suggest_feedback` events (`accepted`, `dismissed`, `edited_then_run`, `never`, `unblock`)

### 4.2 Required Event Fields
Required fields:

- `event_type`
- `session_id`
- `shell`
- `ts_ms`
- `cwd`
- `cmd_raw` for command events
- `exit_code` for `command_end`
- `ephemeral` (incognito state)

Optional high-value fields:

- `duration_ms`
- `repo_root`, `remote_url`, `branch`, `dirty`

Field behavior:

- `cmd_raw` MAY be truncated to configured max bytes and marked as truncated.
- empty `cmd_raw` is valid (for example empty prompt submit).

### 4.3 Interactive-only Hook Behavior
Hooks MUST only run in interactive mode.

Interactive checks:

- bash/zsh: shell interactive flag and `test -t 0`
- fish: `status is-interactive` and `test -t 0`

Non-interactive shells MUST skip hook behavior by default.

### 4.4 Shell-specific Rules
#### bash
- MUST capture previous exit code as first operation in post-command hook.
- SHOULD use `PROMPT_COMMAND` array form on bash >= 4.4.
- MUST use safe idempotent string append fallback on older bash.
- MUST preserve existing trap semantics and avoid recursion.
- SHOULD integrate with `bash-preexec` if present.

#### zsh
- SHOULD use `preexec`/`precmd` and `EPOCHREALTIME` timing.

#### fish
- SHOULD use `fish_preexec`/`fish_postexec` and `CMD_DURATION`.
- MUST use fish-compatible local variable and status idioms.

### 4.5 Command Duration Precision by Shell
- zsh SHOULD use `EPOCHREALTIME` precision.
- fish SHOULD use `CMD_DURATION` milliseconds.
- bash MUST support low-overhead default (`SECONDS`) and MAY support optional higher precision mode with documented overhead.

### 4.6 Session ID Assignment
`session_id` is required for all command and suggestion events.

Preferred strategy:

- helper requests daemon-assigned session id during session start and exports it to shell environment.
- helper SHOULD persist session id in runtime file (`$XDG_RUNTIME_DIR/clai/session.$PPID` or `/tmp/clai-$UID/session.$PPID`) for shell reuse.

Fallback strategy when daemon unavailable:

- shell computes stable local id from host + pid + shell start time + random seed + optional container fingerprint, hashed to stable string.
- random seed MUST be at least 64 bits from CSPRNG.

### 4.7 Presentation Contract
- line-oriented suggestion surface is baseline
- zsh may render ghost text via `POSTDISPLAY`
- fish may integrate with native autosuggestion cadence
- bash defaults to non-invasive adjacent-line hinting
- native completion key bindings MUST remain primary unless user opts in

### 4.8 Output and Terminal Capability Contract
Suggestion-facing CLI commands MUST support:

- `--color=auto|always|never`
- `--format=text|json|fzf`

Rules:

- `json` output MUST never include ANSI sequences
- `fzf` mode MUST require interactive TTY; otherwise return `E_UNSUPPORTED_TTY`
- `--color=auto` MUST honor tty + term + `NO_COLOR`
- terminal width detection SHOULD use ioctl APIs, not external tools

## 5. Ingestion Architecture

### 5.1 Components
The architecture consists of:

- shell adapters (bash/zsh/fish)
- `clai-shim` helper (persistent mode + oneshot fallback)
- `claid` daemon
- SQLite storage with in-memory caches and batched writes

Canonical runtime ingest path:

- shell adapter -> `clai-shim` (persistent or oneshot mode) -> gRPC over UDS -> `claid`
- no alternative production ingest transport is allowed in this spec

Legacy name mapping:

- references to `clai-hook` in older specs map to `clai-shim` oneshot semantics in this consolidated contract

### 5.2 Helper Lifecycle
Persistent helper mode:

- shell starts `clai-shim --persistent` once per interactive shell session
- shell stores helper PID and terminates helper on shell exit
- helper keeps one daemon connection with bounded reconnect policy
- helper has bounded in-memory buffer (default max 16 events, approx 64KB) and drops oldest on overflow

Fallback mode:

- if persistent helper cannot run, shell uses oneshot helper path
- fallback MUST remain fail-open and correctness-equivalent

### 5.3 Ordering Contract Between Ingest and Suggest
`Suggest` may race with immediate prior `command_end` ingest.

`SuggestRequest` MUST include fallback context fields:

- `last_cmd_raw`
- `last_cmd_norm`
- `last_cmd_ts_ms`
- `last_event_seq` when available

Daemon behavior:

- MUST wait up to `suggestions.ingest_sync_wait_ms` (default 5ms) when newer pending ingest exists for same session
- MUST never exceed suggest hard timeout
- MUST fallback to persisted/cache plus request fallback context if wait budget expires

### 5.4 Fire-and-Forget Requirement
Hook/helper path MUST:

- never wait for response ACK
- use short connect and write timeout budgets
- drop event on timeout/error
- suppress non-fatal stderr noise in shell path

## 6. IPC, Transport, Paths, and Security

### 6.1 Transport Contract
Higher layers MUST use an abstraction independent of endpoint implementation details.

Active current transport:

- local daemon transport using Unix domain sockets
- canonical service contract via `proto/clai/v1/clai.proto`

Forward compatibility note:

- named-pipe interface definitions may remain in code/docs as non-active guidance

### 6.2 Wire and Payload Contract
- protobuf/gRPC is the canonical wire contract for runtime RPCs
- logical payload descriptions in this spec define field semantics independent of wire encoding
- optional JSON/NDJSON surfaces MAY exist only on diagnostics endpoints and MUST NOT be used by shell adapter/helper production ingest path

### 6.3 IPC Path Resolution
Unix socket path resolution order:

1. `$XDG_RUNTIME_DIR/clai/suggestd.sock`
2. `$HOME/Library/Caches/clai/suggestd.sock` (macOS fallback)
3. `$TMPDIR/clai-$UID/suggestd.sock`
4. `/tmp/clai-$UID/suggestd.sock`

Runtime directory MUST be mode `0700` and user-owned.

Forward-compatibility non-normative guidance:

- named pipe path format may use `\\\\.\\pipe\\claid-<user-scope>` with user-restricted ACL.

### 6.4 Timeout Policy
Defaults:

- connect timeout: 15ms
- write timeout: 20ms

Allowed operational range:

- connect 10-25ms
- write 10-25ms

### 6.5 Command Transport Safety
Command and cwd transport MUST NOT use CLI args.

Allowed transport forms:

- env vars for small payloads
- stdin transport for large payloads

Env-size safeguards:

- shell adapters MUST switch to stdin or skip when cmd length exceeds 32KB to avoid `E2BIG`

## 7. Daemon Lifecycle and Resilience

### 7.1 Single-instance Locking
Daemon MUST enforce single-instance semantics with a lock file in runtime/db directory.

Recommended lock file name: `.daemon.lock`.

On lock acquisition failure:

- if owning process is live daemon, exit with `E_DAEMON_UNAVAILABLE`
- if lock is stale, clean stale lock and retry once

### 7.2 Startup Sequence
Startup sequence MUST be:

1. acquire lock
2. run migrations
3. cleanup stale socket path
4. start listeners

### 7.3 Crash Recovery and Startup Hygiene
- stale socket file MUST be removed before `Listen`
- runtime dir on network FS is unsupported for lock-path correctness

### 7.4 Backpressure and Burst Policy
Ingest queue is bounded (default `8192` events and `8MB` byte cap).

Burst mode defaults:

- threshold: 10 events in 100ms per session
- quiet exit: 500ms

During burst mode:

- persist boundary `command_end` events preferentially
- update in-memory recency for intermediate events
- increment burst/drop metrics

Priority retention:

- `command_end`, `session_start`, and `session_end` are high priority
- lower-priority telemetry may be dropped first under pressure

Queue policy:

- full queue MUST drop oldest low-priority telemetry first.
- suggestion serving MUST remain independent from ingestion backlog flush.

### 7.5 Signal and Lifecycle Behavior
- `SIGHUP` MUST reload runtime config
- daemon MUST ignore `SIGPIPE`
- hook/CLI path MUST treat pipe `EPIPE` as clean termination when downstream closes

Daemon management surface:

- `clai daemon start|stop|status|reload`

Auto-start:

- first session path MUST opportunistically start daemon if unavailable

Upgrade behavior:

- self-reexec upgrade path MAY be implemented with graceful handoff; failure MUST end cleanly and recover via reconnect/autostart path.

## 8. Storage Model and Schema

### 8.1 Storage Policies
- SQLite WAL mode
- one writer goroutine
- batched transactions every 25-50ms or 100 events
- prepared statements
- foreign keys enabled

Recommended pragmas:

- `PRAGMA journal_mode=WAL;`
- `PRAGMA synchronous=NORMAL;`
- `PRAGMA foreign_keys=ON;`
- `PRAGMA wal_autocheckpoint=1000;` (tunable)

### 8.2 V2 Ownership and Coexistence
- V2 schema is authoritative for this spec
- backward compatibility with older schema/storage is out of scope
- runtime MUST operate on V2 schema only
- V2 schema/functionality fully covers and supersedes V1 command-event, transition, frequency, and task-discovery capability scope
- older data import, if implemented, is an offline one-time tool and not part of runtime behavior or acceptance criteria

### 8.3 Core and Advanced Non-Excluded Tables
```sql
CREATE TABLE IF NOT EXISTS session (
  id TEXT PRIMARY KEY,
  shell TEXT NOT NULL,
  started_at_ms INTEGER NOT NULL,
  project_types TEXT,
  host TEXT,
  user_name TEXT
);

CREATE TABLE IF NOT EXISTS command_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  ts_ms INTEGER NOT NULL,
  cwd TEXT NOT NULL,
  repo_key TEXT,
  branch TEXT,
  cmd_raw TEXT NOT NULL,
  cmd_norm TEXT NOT NULL,
  cmd_truncated INTEGER NOT NULL DEFAULT 0,
  template_id TEXT,
  exit_code INTEGER,
  duration_ms INTEGER,
  ephemeral INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_event_ts ON command_event(ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_repo_ts ON command_event(repo_key, ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_cwd_ts ON command_event(cwd, ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_session_ts ON command_event(session_id, ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_norm_repo ON command_event(cmd_norm, repo_key);

CREATE TABLE IF NOT EXISTS command_template (
  template_id TEXT PRIMARY KEY,
  cmd_norm TEXT NOT NULL,
  tags TEXT,
  slot_count INTEGER NOT NULL,
  first_seen_ms INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS transition_stat (
  scope TEXT NOT NULL,
  prev_template_id TEXT NOT NULL,
  next_template_id TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_template_id, next_template_id)
);

CREATE TABLE IF NOT EXISTS command_stat (
  scope TEXT NOT NULL,
  template_id TEXT NOT NULL,
  score REAL NOT NULL,
  success_count INTEGER NOT NULL,
  failure_count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id)
);

CREATE TABLE IF NOT EXISTS slot_stat (
  scope TEXT NOT NULL,
  template_id TEXT NOT NULL,
  slot_index INTEGER NOT NULL,
  value TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id, slot_index, value)
);

CREATE TABLE IF NOT EXISTS slot_correlation (
  scope TEXT NOT NULL,
  template_id TEXT NOT NULL,
  slot_key TEXT NOT NULL,
  tuple_hash TEXT NOT NULL,
  tuple_value_json TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id, slot_key, tuple_hash)
);

CREATE TABLE IF NOT EXISTS project_type_stat (
  project_type TEXT NOT NULL,
  template_id TEXT NOT NULL,
  score REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(project_type, template_id)
);

CREATE TABLE IF NOT EXISTS project_type_transition (
  project_type TEXT NOT NULL,
  prev_template_id TEXT NOT NULL,
  next_template_id TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(project_type, prev_template_id, next_template_id)
);

CREATE TABLE IF NOT EXISTS pipeline_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_event_id INTEGER NOT NULL REFERENCES command_event(id),
  position INTEGER NOT NULL,
  operator TEXT,
  cmd_raw TEXT NOT NULL,
  cmd_norm TEXT NOT NULL,
  template_id TEXT NOT NULL,
  UNIQUE(command_event_id, position)
);

CREATE TABLE IF NOT EXISTS pipeline_transition (
  scope TEXT NOT NULL,
  prev_template_id TEXT NOT NULL,
  next_template_id TEXT NOT NULL,
  operator TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_template_id, next_template_id, operator)
);

CREATE TABLE IF NOT EXISTS pipeline_pattern (
  pattern_hash TEXT PRIMARY KEY,
  template_chain TEXT NOT NULL,
  operator_chain TEXT NOT NULL,
  scope TEXT NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  cmd_norm_display TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS failure_recovery (
  scope TEXT NOT NULL,
  failed_template_id TEXT NOT NULL,
  exit_code_class TEXT NOT NULL,
  recovery_template_id TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  success_rate REAL NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  source TEXT NOT NULL DEFAULT 'learned',
  PRIMARY KEY(scope, failed_template_id, exit_code_class, recovery_template_id)
);

CREATE TABLE IF NOT EXISTS task_candidate (
  repo_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  command_text TEXT NOT NULL,
  description TEXT,
  source TEXT NOT NULL DEFAULT 'auto',
  priority_boost REAL NOT NULL DEFAULT 0,
  source_checksum TEXT,
  discovered_ms INTEGER NOT NULL,
  PRIMARY KEY(repo_key, kind, name)
);

CREATE TABLE IF NOT EXISTS suggestion_feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  ts_ms INTEGER NOT NULL,
  prompt_prefix TEXT,
  suggested_text TEXT NOT NULL,
  action TEXT NOT NULL,
  executed_text TEXT,
  latency_ms INTEGER
);

CREATE TABLE IF NOT EXISTS session_alias (
  session_id TEXT NOT NULL,
  alias_key TEXT NOT NULL,
  expansion TEXT NOT NULL,
  PRIMARY KEY(session_id, alias_key)
);

CREATE TABLE IF NOT EXISTS dismissal_pattern (
  scope TEXT NOT NULL,
  context_template_id TEXT NOT NULL,
  dismissed_template_id TEXT NOT NULL,
  dismissal_count INTEGER NOT NULL,
  last_dismissed_ms INTEGER NOT NULL,
  suppression_level TEXT NOT NULL,
  PRIMARY KEY(scope, context_template_id, dismissed_template_id)
);

CREATE TABLE IF NOT EXISTS rank_weight_profile (
  profile_key TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  updated_ms INTEGER NOT NULL,
  w_transition REAL NOT NULL,
  w_frequency REAL NOT NULL,
  w_success REAL NOT NULL,
  w_prefix REAL NOT NULL,
  w_affinity REAL NOT NULL,
  w_task REAL NOT NULL,
  w_feedback REAL NOT NULL,
  w_project_type_affinity REAL NOT NULL,
  w_failure_recovery REAL NOT NULL,
  w_risk_penalty REAL NOT NULL,
  sample_count INTEGER NOT NULL,
  learning_rate REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS suggestion_cache (
  cache_key TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  context_hash TEXT NOT NULL,
  suggestions_json TEXT NOT NULL,
  created_ms INTEGER NOT NULL,
  ttl_ms INTEGER NOT NULL,
  hit_count INTEGER NOT NULL DEFAULT 0
);

CREATE VIRTUAL TABLE IF NOT EXISTS command_event_fts USING fts5(
  cmd_raw,
  cmd_norm,
  repo_key UNINDEXED,
  session_id UNINDEXED,
  content='command_event',
  content_rowid='id',
  tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS command_event_ai AFTER INSERT ON command_event
WHEN NEW.ephemeral = 0
BEGIN
  INSERT INTO command_event_fts(rowid, cmd_raw, cmd_norm, repo_key, session_id)
  VALUES (NEW.id, NEW.cmd_raw, NEW.cmd_norm, NEW.repo_key, NEW.session_id);
END;

CREATE TRIGGER IF NOT EXISTS command_event_ad AFTER DELETE ON command_event
BEGIN
  INSERT INTO command_event_fts(command_event_fts, rowid, cmd_raw, cmd_norm, repo_key, session_id)
  VALUES ('delete', OLD.id, OLD.cmd_raw, OLD.cmd_norm, OLD.repo_key, OLD.session_id);
END;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_ms INTEGER NOT NULL
);
```

### 8.4 Retention and Maintenance
Retention defaults:

- keep 90 days of `command_event`
- keep at most 500000 `command_event` rows
- prune oldest rows first when limit exceeded

Maintenance loop defaults:

- interval: 5 minutes
- active window: `wal_checkpoint(PASSIVE)`
- low-activity window: `wal_checkpoint(TRUNCATE)` + optional FTS optimize
- optional VACUUM under threshold policy and off hot path

Maintenance MUST be non-fatal and MUST NOT crash daemon on failure.

Recommended maintenance details:

- low activity threshold: fewer than 5 ingested events in maintenance interval
- prune deletes in batches (for example 1000 rows) with short yield between batches
- run VACUUM at most once per 24h when size and freelist thresholds justify it

Schema lifecycle rule:

- tables in Section 8.3 MUST be created during initialization regardless of feature-flag enablement.
- feature flags gate runtime read/write behavior, not table existence.

### 8.5 Write-path Transaction Semantics
For each non-ephemeral `command_end`, daemon MUST apply writes in one transaction:

1. insert `command_event`
2. upsert `command_template`
3. update `command_stat`
4. update `transition_stat` when previous template is known
5. update `slot_stat` and `slot_correlation`
6. update project-type and directory-scoped aggregates when available
7. update pipeline and failure-recovery aggregates when applicable
8. invalidate in-memory cache markers and enqueue optional async learning updates

Error and retry behavior:

- on transaction failure, event effects MUST be discarded atomically.
- on `SQLITE_BUSY` after busy-timeout, event MUST be retried once; second failure MAY drop event and increment drop metric.

Ephemeral contract:

- `ephemeral=1` events MUST NOT produce persistent SQLite writes and MUST only update bounded in-memory session state.

Performance invariant:

- write-path transaction SHOULD meet 25ms P95 on reference hardware.

## 9. Migration and Corruption Recovery

### 9.1 Migration Contract
- daemon MUST run migrations before serving
- migration path is forward-only
- daemon MUST refuse startup when on-disk schema is newer than supported binary

### 9.2 Migration Locking
Migration and startup locking MUST serialize concurrent daemon startup attempts.

### 9.3 Corruption Recovery
On corruption errors, daemon MUST:

- rotate corrupt DB artifacts to timestamped `.corrupt` files
- initialize fresh DB
- emit critical diagnostic and continue serving with empty history

## 10. Normalization, Tokenization, Slots, and Alias Handling

### 10.1 Tokenization and UTF-8
- tokenizer MUST use a real parser/tokenizer library (`shlex` baseline)
- malformed UTF-8 MUST be repaired with lossy replacement before serialization
- raw bytes need not be preserved

### 10.2 Pre-normalization Stages
Ordered stages:

1. alias expansion on first token (bounded depth, cycle detection)
2. compound/pipeline split on unquoted operators
3. per-segment tokenization and normalization

Splitter edge-case rules:

- split operators: `|`, `|&`, `&&`, `||`, `;` when unquoted/unescaped.
- do not split inside subshell expressions, backticks, or quoted literals.
- treat heredoc body as opaque.
- treat `|&` as distinct stored operator.
- do not model standalone background `&` as pipeline operator in baseline.

### 10.3 Normalization Rules
Normalization MUST be deterministic and include:

- preserve command/subcommand/flag structure
- normalize whitespace
- slot replacements: `<path>`, `<num>`, `<sha>`, `<url>`, `<msg>`
- optional domain slots (`<branch>`, `<namespace>`, `<service>`) where deterministically recognized

### 10.4 Command-specific Templates
Starter patterns MUST include at least:

- `git commit -m <msg>`
- `git checkout -b <branch>`
- `git push <remote> <branch>`
- package manager install/run templates
- `go test` path patterns
- `pytest` path patterns

### 10.5 Template Identity
`template_id = sha256(cmd_norm)`

### 10.6 Event Size Limits
`cmd_raw` max bytes defaults to `16384`.

Oversized events MUST:

- be truncated
- set `cmd_truncated=1`
- remain bounded for storage/indexing/queue budgets

### 10.7 Alias Capture and Rendering
Alias support:

- capture alias map per session
- expand alias for normalization identity
- preserve raw unexpanded command for display/audit
- optional reverse-render to alias-preferred output where mapping exists

Alias expansion MUST:

- enforce max expansion depth
- detect and stop cycles
- re-snapshot alias map after alias-mutating commands

Alias capture SHOULD use shell-native commands:

- bash: `alias -p`
- zsh: `alias -rL`
- fish: `abbr --show`

### 10.8 Repo Key and Git Context
Repo identity MUST use canonicalized repo root.

Formula:

- `repo_key = SHA256(lower(remote_url) + \"|\" + canonical(repo_root))`
- if no remote: `repo_key = SHA256(\"local|\" + canonical(repo_root))`

Canonicalization MUST prefer:

- `git rev-parse --show-toplevel`
- physical path resolution equivalent to `pwd -P`

Git context resolution MUST be cached and refreshed when:

- cwd changes
- command is git-related
- TTL expires (recommended 1-3 seconds)

### 10.9 Project-type Detection
Project-type detection MUST scan upward from cwd until repo root (or bounded depth) using marker files.

Default marker examples:

- `go.mod` -> `go`
- `Cargo.toml` -> `rust`
- `pyproject.toml` / `setup.py` / `requirements.txt` -> `python`
- `package.json` -> `node`
- `Gemfile` -> `ruby`
- `pom.xml` -> `java-maven`
- `build.gradle` -> `java-gradle`
- `CMakeLists.txt` -> `cpp-cmake`
- `Makefile` -> `make`
- `Dockerfile` / `docker-compose.yml` -> `docker`
- `helmfile.yaml` -> `k8s-helm`
- `kustomization.yaml` -> `k8s-kustomize`
- `terraform/*.tf` -> `terraform`
- `serverless.yml` -> `serverless`

Override contract:

- `.clai/project.yaml` MAY provide explicit `project_types` list and override auto-detection.

## 11. Candidate Generation Pipeline (Excluded Feature Family Removed)

### 11.1 Inputs
Inputs include:

- session context (`last command/template`, cwd, repo, branch)
- typed prefix
- incognito state
- recent feedback

### 11.2 Retrieval Source Priority
Deterministic source priority:

1. session transitions
2. failure-recovery candidates (only after failed previous command)
3. pipeline continuation/pattern candidates
4. repo transitions
5. directory-scoped transitions
6. project-type transitions
7. playbook conditional triggers (`after`, `after_failure`)
8. global transitions
9. session/repo/dir/global frequency priors
10. typo-correction candidates after failures
11. discovery candidates (low-priority gated source)

### 11.3 Retrieval Caps
Recommended source caps:

- failure recovery: 5
- pipeline patterns/continuations: 10
- project-type transitions: 30
- discovery: 2
- global total: 200

### 11.4 Prefix Modes
- empty prefix: pure next-step mode
- non-empty prefix: constrained mode with deterministic prefix/fuzzy handling

### 11.5 Failure-recovery and Typo Triggers
Failure-recovery candidates MUST be considered after non-zero previous exit codes.

Exit class buckets SHOULD include:

- `code:1`, `code:2`, `code:126`, `code:127`, `code:128+`, `code:255`, `nonzero`

Typo recovery SHOULD trigger strongly for `code:127` and MAY use additional deterministic unknown-command patterns.

### 11.6 Discovery Display Gate
Discovery candidates MUST be gated:

- only for empty prefix
- only when no high-confidence predictive candidate exists
- never displace high-confidence predictive candidates

Discovery source pools SHOULD include:

- project-type priors with zero personal usage
- playbook tasks never executed by user
- curated tool-common command templates

## 12. Ranking Model and Post-processing

### 12.1 Deterministic Score Formula
```text
score = w1*transition_effective
      + w2*frequency
      + w3*success
      + w4*prefix
      + w5*affinity_enhanced
      + w6*task_extended
      + w7*feedback_extended
      + w9*project_type_affinity
      + w10*failure_recovery
      - w8*risk_penalty
```

Defaults:

- `w1 transition = 0.30`
- `w2 frequency = 0.20`
- `w3 success = 0.10`
- `w4 prefix = 0.15`
- `w5 affinity = 0.10`
- `w6 task = 0.05`
- `w7 feedback = 0.15`
- `w8 risk_penalty = 0.20`
- `w9 project_type_affinity = 0.08`
- `w10 failure_recovery = 0.12`

### 12.2 Feature Normalization and Amplifiers
- feature values MUST be normalized to `[0,1]` before weighting
- transition/frequency signals SHOULD be log-scaled before normalization
- risk penalty MAY suppress low-scoring risky candidates
- `transition_effective` MAY include pipeline confidence amplification

Extended feature formulas:

- `affinity_enhanced = 0.4*repo_match + 0.3*dir_scope_match + 0.2*branch_match + 0.1*cwd_exact_match`
- `task_extended = clamp(base_task_score + playbook_after_boost, 0, 1)` when trigger conditions match
- `feedback_extended = base_feedback + dismissal_penalty(context, candidate)` where dismissal penalty is in `[-1,0]`

Score scale rule:

- final weighted score is raw weighted output; it is not renormalized to `[0,1]` after aggregation.

### 12.3 Confidence and Low-confidence Handling
- confidence MUST derive from support diversity and runner-up margin
- low-confidence candidates MUST be hidden unless `include_low_confidence=true`

### 12.4 Ordering and Tie-break Rules
Sort keys:

1. `score DESC`
2. `confidence DESC`
3. `last_seen_ms DESC`
4. `cmd_norm ASC`

### 12.5 Dedup and Diversity
- deduplicate by normalized template/rendered command identity
- suppress near-duplicates in top-k
- at most one suggestion per normalized template unless slot-filled variants differ meaningfully

### 12.6 Slot Filling and Correlation
- fallback scope order: session -> repo -> global
- use `slot_correlation` first for dependent slot sets
- reject mixed assignments below correlation confidence threshold
- evict low-weight old slot values when per-slot value cap exceeded

### 12.7 Explainability Reasons
Suggestions MUST include top contribution reasons (default max 3).

Allowed reason types:

- `transition`
- `frequency`
- `success`
- `directory`
- `project_type`
- `task`
- `failure_recovery`
- `feedback`
- `pipeline`
- `discovery`

## 13. Feedback and Online Learning Loop

### 13.1 Feedback Events
Supported actions:

- `accepted`
- `accepted_implicit`
- `dismissed`
- `edited_then_run`
- `ignored_timeout`
- `never`
- `unblock`

Implicit acceptance heuristic:

- exact match with recent top suggestion within configured match window

### 13.2 Update Rules
- accepted events increase priors and template/source affinity
- dismissed events add short-term suppression
- edited events update slot statistics and correction maps
- accepted events may update slot-correlation counts when all required slots are present

### 13.3 Persistent Suppression State Machine
Suppression levels:

- `NONE`
- `TEMPORARY`
- `LEARNED`
- `PERMANENT`

Rules:

- repeated dismissals in same context promote suppression
- `never` sets permanent suppression
- `unblock` clears permanent suppression
- accepted suggestion in same context resets suppression
- permanent suppression MAY filter candidate before ranking.

### 13.4 Adaptive Weight Tuning
Per-profile online update:

- profile scopes: session, repo, global
- pairwise update: accepted vector vs highest-ranked unaccepted vector
- bounded clamps and renormalization maintain stable ranges

Update sketch:

```text
w_next = clamp(w_prev + eta * (f_pos - f_neg), min_w, max_w)
```

Learning-rate behavior:

- default `eta=0.02`
- decay with sample count
- floor at `0.001`
- static defaults used when sample count below configured threshold

## 14. Task Discovery and `.clai/tasks.yaml` Contract

### 14.1 Built-in Sources
Discovery sources include:

- package scripts
- Makefile targets
- justfile recipes
- optional taskfile/cargo/pnpm sources
- repository playbook file `.clai/tasks.yaml`

### 14.2 Makefile Modes
Two modes SHOULD exist:

- heuristic mode (default)
- authoritative mode using `make -qp` (optional, configurable)

### 14.3 Runtime Rules
- discovery execution MUST be timeout-bounded and output-capped
- errors MUST be non-fatal
- daemon MUST keep last valid snapshot on parse errors
- file watches SHOULD use debounce refresh
- playbook-derived candidates MUST use source tag `playbook` with configured boost.

### 14.4 `.clai/tasks.yaml` Schema
Entry fields:

- `name` required
- `command` required
- `description` optional
- `tags` optional
- `enabled` optional (default true)
- `after` optional
- `after_failure` optional
- `priority` optional (`low|normal|high`)

Validation:

- referenced tasks in `after`/`after_failure` MUST resolve
- circular `after` graphs MUST be rejected
- parse failure MUST be soft and non-blocking

## 15. Search and Tag Extraction

### 15.1 Search Modes
`clai search` MUST support modes:

- `fts`
- `prefix`
- `describe`
- `auto`

`auto` MUST merge deterministic `fts` and `describe` signals.

### 15.2 FTS Fallback
If FTS5 unavailable:

- MUST use fallback prefix/LIKE scan path with bounded scan limit

### 15.3 Tag Extraction
Normalizer MUST support deterministic tag extraction for describe-mode search.

Tag extraction requirements:

- built-in controlled vocabulary artifact
- optional vocabulary extension path
- deterministic extraction API
- minimum tool coverage tests including git, docker, kubectl, npm, make, find, grep, curl

Search backend behavior:

- `SearchResponse.backend` MUST report `fts5` or fallback backend explicitly.

## 16. Caching and Latency Budgets

### 16.1 Multi-layer Cache
Cache layers:

- L1 per-session hot cache
- L2 per-repo fallback cache
- L3 SQLite aggregate fallback

Global in-memory cache/state budget default is 50MB.

Eviction policy:

- evict L2 first
- then evict L1

### 16.2 Invalidation
Invalidate on:

- new `command_end` for session
- cwd/repo/branch changes
- TTL expiration (default 30s)

### 16.3 Hot-path Deadlines
- retrieval deadline 20ms
- ranking deadline 10ms
- end-to-end suggest hard timeout 150ms

On timeout, daemon MUST return best cache fallback when available.

## 17. Security, Privacy, and Incognito

### 17.1 Local Safety
- local-only storage by default
- no shell-generated JSON
- no command/cwd CLI arg transport

### 17.2 Incognito Modes
Supported modes:

- `off`
- `ephemeral`
- `no_send`

Behavior:

- `no_send`: skip ingestion
- `ephemeral`: allow session-quality behavior but no persistent writes

### 17.3 Sensitive Data Handling
- optional token redaction stage for likely secrets
- info-level logs MUST NOT contain raw command text

### 17.4 Privilege and Multi-user Safety
- daemon MUST run as invoking non-root user context
- runtime socket/lock dirs MUST be user-private (`0700`)
- root-shell transitions MUST degrade gracefully without shell interruption

## 18. API and CLI Surface

### 18.1 Daemon API Set
Canonical runtime API set:

- `SessionStart`
- `SessionEnd`
- `CommandStarted`
- `CommandEnded`
- `Suggest`
- `Search`
- `RecordFeedback`
- `DebugStats`

`IngestEvent` is a logical concept only (not a separate runtime RPC surface in this spec).

Transport/API mapping note:

- debug HTTP diagnostics MAY run on a separate listener from main RPC transport.

### 18.2 Request and Response Contracts
#### SuggestRequest
Required/optional unified fields:

- `session_id`
- `cwd`
- `repo_key`
- `prefix`
- `cursor_pos`
- `limit`
- `include_low_confidence`
- `last_cmd_raw`
- `last_cmd_norm`
- `last_cmd_ts_ms`
- `last_event_seq`

#### SuggestResponse
- `suggestions[] { text, cmd_norm, source, score, confidence, reasons[], risk }`
- `cache_status`
- `latency_ms`
- optional `timing_hint { user_speed_class, suggested_pause_threshold_ms }`

Reason types MUST exclude the excluded sequence-mining feature family.

#### SearchRequest
- `query`
- `scope`
- `limit`
- `repo_key`
- `session_id`
- `mode`

#### SearchResponse
- `results[] { cmd_raw, cmd_norm, ts_ms, repo_key, rank_score, tags?, matched_tags? }`
- `latency_ms`
- `backend`

#### RecordFeedbackRequest
- `session_id`
- `action`
- `suggested_text`
- `executed_text`
- `prefix`
- `latency_ms`

### 18.3 CLI Surface
- `clai suggest [prefix] --limit N --format text|json|fzf --color auto|always|never`
- `clai search [query] --limit N --scope session|repo|global --mode fts|prefix|describe|auto --color auto|always|never`
- `clai suggest-feedback --action accepted|dismissed|edited|never|unblock`
- `clai suggestions doctor`
- `clai daemon start|stop|status|reload`

Compatibility surfaces:

- local health endpoint `GET /healthz` MUST be available.
- debug endpoints MAY expose scores/transitions/tasks for diagnostics builds.

### 18.4 Error Model
All API responses MUST follow:

- success payload (`ok=true`)
- structured error (`ok=false`) with `{ code, message, retryable }`

Standard error codes:

- `E_INVALID_ARGUMENT`
- `E_DAEMON_UNAVAILABLE`
- `E_STORAGE_BUSY`
- `E_STORAGE_CORRUPT`
- `E_TIMEOUT`
- `E_UNSUPPORTED_TTY`
- `E_INTERNAL`

CLI behavior:

- `clai suggest` defaults to fail-open empty output on daemon failure (non-zero with strict mode)
- `clai search` returns user-facing error and non-zero exit on daemon/storage failures

## 19. Validation Rules (Request + Config)

### 19.1 Input Validation Rules

| Field | Valid Range | On Invalid |
| --- | --- | --- |
| `session_id` | non-empty, max 256 bytes | reject with `E_INVALID_ARGUMENT` |
| `event_type` | known enum | reject |
| `shell` | `bash`, `zsh`, `fish`, or empty | reject unknown non-empty |
| `ts_ms` | positive; not > now + 60s | clamp future; reject <=0 |
| `cwd` | valid UTF-8, max 4096 bytes | truncate + UTF-8 repair |
| `cmd_raw` | 0-16384 bytes | truncate + `cmd_truncated=1` |
| `exit_code` | platform integer | preserve raw integer |
| `duration_ms` | 0-86400000 | clamp to range |
| `prefix` | 0-16384 bytes | truncate silently |
| `cursor_pos` | 0..len(prefix) | clamp |
| `limit` | 1-100 | clamp; 0 uses default |
| `repo_key` | UTF-8, max 256 bytes | truncate if oversized; empty allowed |
| `include_low_confidence` | boolean | default false if missing |
| `last_cmd_raw` | 0-16384 bytes | truncate silently |
| `last_cmd_norm` | UTF-8, max 4096 bytes | truncate silently |
| `last_cmd_ts_ms` | positive integer | clamp invalid/future values |
| `last_event_seq` | non-negative integer | default 0 if missing |
| `query` | 0-16384 bytes | truncate silently |
| `scope` | `session`,`repo`,`global` | reject unknown |
| `mode` | `fts`,`prefix`,`describe`,`auto` | reject unknown |
| `action` | known feedback enum | reject unknown |
| `suggested_text` | 1-16384 bytes | reject empty; truncate oversize |
| `executed_text` | 0-16384 bytes | truncate silently |
| `latency_ms` | 0-86400000 | clamp to range |

### 19.2 Config Validation Rules
Config validation applies on startup and reload.

| Category | Keys | Valid Range | On Invalid |
| --- | --- | --- | --- |
| Timeouts | `hard_timeout_ms`, `hook_connect_timeout_ms`, `hook_write_timeout_ms`, `ingest_sync_wait_ms`, `cache_ttl_ms` | >=1 | fallback default + warn |
| Weights | `weights.*` | [0.0,1.0] | clamp + warn |
| Counts | `max_results`, `ingest_queue_max_events`, `burst_events_threshold` | >=1 | fallback default + warn |
| Byte sizes | `cmd_raw_max_bytes`, `ingest_queue_max_bytes`, `cache_memory_budget_mb` | >=1 | fallback default + warn |
| Retention days | `retention_days` | >=1, or 0 to disable time pruning | fallback default + warn |
| Retention max events | `retention_max_events` | >=1000 | clamp to 1000 + warn |
| Learning eta | `online_learning_eta` | (0.0,1.0] | fallback default + warn |
| Learning samples | `online_learning_min_samples` | >=1 | fallback default + warn |
| Pipeline segments | `pipeline_max_segments` | [2,32] | clamp + warn |
| Directory depth | `directory_scope_max_depth` | [1,10] | clamp + warn |
| Enums | `incognito_mode`,`shim_mode`,`search_fts_tokenizer` | known enum set | fallback default + warn |

Validation MUST keep daemon running with key-level fallback unless configuration is unrecoverably malformed.

Reload behavior:

- reload validation MUST preserve previous valid value when new value is invalid.

## 20. Testing Strategy

### 20.1 Unit Tests
MUST include:

- input validation boundaries and clamping
- config validation and fallback behavior
- normalization/tokenization edge cases
- invalid UTF-8 replacement behavior
- truncation markers and size limits
- slot extraction/fill/correlation correctness
- ranking determinism and monotonic feature behavior
- online learning clamp/decay/guardrails
- feedback decay and suppression state transitions
- burst detector thresholds and recovery
- task playbook parser and trigger graph validation
- shell adapter version/compat branches and hook safety
- shell env-size threshold and stdin fallback behavior
- bash PROMPT_COMMAND array and fallback-string append safety
- pipeline splitter edge cases
- alias expansion depth/cycle/render mapping
- project-type detection and cache invalidation

### 20.2 Property and Fuzz Tests
MUST include fuzz/property suites for:

- malformed UTF-8 and adversarial quoting
- parser/tokenizer stress inputs
- normalization idempotency

### 20.3 Integration Tests
MUST include:

- ingest -> aggregate -> suggest flow
- session/repo isolation
- cache invalidation correctness
- migration from empty DB to latest schema
- startup stale-socket cleanup behavior
- burst ingestion under load with bounded durable writes
- FTS sync and fallback search path
- retention pruning correctness
- lock contention and busy retry behavior
- retrieval priority enforcement
- playbook `after`/`after_failure` behavior

### 20.4 Cross-shell Interactive Tests
`go-expect` suites for bash/zsh/fish SHOULD verify:

- hook idempotency
- prompt integrity
- accept/dismiss interactions where supported
- session lifecycle correctness
- non-interactive no-op behavior

### 20.5 Docker Matrix
CI SHOULD validate on alpine/ubuntu/debian with deterministic interactive test parallelism.

### 20.6 Performance Tests
MUST include:

- hook overhead microbenchmarks
- suggest latency warm/cold benchmarks
- burst ingest load test (10k events)
- search benchmark with large history corpus
- feature-on overhead benchmarks for project-type/pipeline/failure-recovery/directory-scoped retrieval

### 20.7 Reliability and Chaos Tests
MUST include:

- daemon kill mid-session
- stale socket and lock contention
- busy DB and degraded behavior
- `SIGPIPE` resilience in pipe scenarios

### 20.8 Security Tests
MUST include:

- socket permission checks
- malformed frame handling
- incognito persistence guarantees
- privilege and multi-user isolation behavior

### 20.9 Deterministic Replay
MUST maintain replay corpus with fixed clock/seed and require explicit review on top-k drift.

## 21. Observability and Diagnostics

### 21.1 Metrics
Required metrics include:

- suggest latency and cache hit ratio
- accept/dismiss rates
- ingest timeout/drop rates
- queue depth and flush latency
- burst mode entries and dropped count
- online learning update/clamp/sample metrics
- search backend split (`fts5` vs fallback) and latency percentiles
- FTS index size and ratio metrics
- project-type detection cache hit and scan latency
- pipeline splitter and segment distribution metrics
- failure-recovery hit and accept rates
- discovery show/accept/dismiss/ignore rates

### 21.2 Structured Logging
- debug logs MAY include template ids and feature contributions
- info logs MUST exclude raw command text

### 21.3 Doctor Surface
`clai suggestions doctor` MUST include:

- daemon health
- IPC path and permissions
- migration version
- cache stats
- discovery errors
- FTS availability/index sync status
- playbook parse status
- active feature/capability matrix for shells

## 22. Configuration Surface (Non-excluded Keys)

### 22.1 Resolution and Override
Config format is YAML.

Resolution order:

1. `$CLAI_CONFIG`
2. `$XDG_CONFIG_HOME/clai/config.yaml`
3. `$HOME/.config/clai/config.yaml`
4. built-in defaults

Environment overrides MUST map dotted keys to uppercase underscore form (for example `suggestions.enabled` -> `CLAI_SUGGESTIONS_ENABLED`).

### 22.2 Key Set
Core:

- `suggestions.enabled=true`
- `suggestions.max_results=5`
- `suggestions.cache_ttl_ms=30000`
- `suggestions.hard_timeout_ms=150`

Hook/transport:

- `suggestions.hook_connect_timeout_ms=15`
- `suggestions.hook_write_timeout_ms=20`
- `suggestions.socket_path=""`
- `suggestions.ingest_sync_wait_ms=5`
- `suggestions.interactive_require_tty=true`
- `suggestions.cmd_raw_max_bytes=16384`
- `suggestions.shim_mode=auto` (`auto|persistent|oneshot`)

Ranking weights:

- `suggestions.weights.transition=0.30`
- `suggestions.weights.frequency=0.20`
- `suggestions.weights.success=0.10`
- `suggestions.weights.prefix=0.15`
- `suggestions.weights.affinity=0.10`
- `suggestions.weights.task=0.05`
- `suggestions.weights.feedback=0.15`
- `suggestions.weights.risk_penalty=0.20`
- `suggestions.weights.project_type_affinity=0.08`
- `suggestions.weights.failure_recovery=0.12`

Learning and slots:

- `suggestions.decay_half_life_hours=168`
- `suggestions.feedback_boost_accept=0.10`
- `suggestions.feedback_penalty_dismiss=0.08`
- `suggestions.feedback_match_window_ms=5000`
- `suggestions.online_learning_enabled=true`
- `suggestions.online_learning_eta=0.02`
- `suggestions.online_learning_eta_decay_constant=500`
- `suggestions.online_learning_eta_floor=0.001`
- `suggestions.online_learning_min_samples=30`
- `suggestions.weight_min=0.00`
- `suggestions.weight_max=0.60`
- `suggestions.weight_risk_min=0.10`
- `suggestions.weight_risk_max=0.60`
- `suggestions.slot_max_values_per_slot=20`
- `suggestions.slot_correlation_min_confidence=0.65`

Backpressure:

- `suggestions.burst_events_threshold=10`
- `suggestions.burst_window_ms=100`
- `suggestions.burst_quiet_ms=500`
- `suggestions.ingest_queue_max_events=8192`
- `suggestions.ingest_queue_max_bytes=8388608`

Task discovery and playbook:

- `suggestions.task_playbook_enabled=true`
- `suggestions.task_playbook_path=.clai/tasks.yaml`
- `suggestions.task_playbook_boost=0.20`
- `suggestions.task_playbook_extended_enabled=true`
- `suggestions.task_playbook_after_boost=0.30`

Search:

- `suggestions.search_fts_enabled=true`
- `suggestions.search_fallback_scan_limit=2000`
- `suggestions.search_fts_tokenizer=trigram` (`trigram|unicode61`)
- `suggestions.search_describe_enabled=true`
- `suggestions.search_auto_mode_merge=true`
- `suggestions.search_tag_vocabulary_path=""`

Project type:

- `suggestions.project_type_detection_enabled=true`
- `suggestions.project_type_cache_ttl_ms=60000`

Pipeline:

- `suggestions.pipeline_awareness_enabled=true`
- `suggestions.pipeline_max_segments=8`
- `suggestions.pipeline_pattern_min_count=2`

Failure recovery:

- `suggestions.failure_recovery_enabled=true`
- `suggestions.failure_recovery_bootstrap_enabled=true`
- `suggestions.failure_recovery_min_count=2`

Adaptive timing:

- `suggestions.adaptive_timing_enabled=true`
- `suggestions.typing_fast_threshold_cps=6.0`
- `suggestions.typing_pause_threshold_ms=300`
- `suggestions.typing_eager_prefix_length=3`

Alias:

- `suggestions.alias_resolution_enabled=true`
- `suggestions.alias_max_expansion_depth=3`
- `suggestions.alias_render_preferred=true`

Dismissal:

- `suggestions.dismissal_learned_threshold=3`
- `suggestions.dismissal_learned_halflife_hours=720`
- `suggestions.dismissal_temporary_halflife_ms=1800000`

Directory scope:

- `suggestions.directory_scoping_enabled=true`
- `suggestions.directory_scope_max_depth=3`

Explainability:

- `suggestions.explain_enabled=true`
- `suggestions.explain_max_reasons=3`
- `suggestions.explain_min_contribution=0.05`

Discovery:

- `suggestions.discovery_enabled=true`
- `suggestions.discovery_cooldown_hours=24`
- `suggestions.discovery_max_confidence_threshold=0.3`
- `suggestions.discovery_source_project_type=true`
- `suggestions.discovery_source_playbook=true`
- `suggestions.discovery_source_tool_common=true`

Storage and cache:

- `suggestions.retention_days=90`
- `suggestions.retention_max_events=500000`
- `suggestions.maintenance_interval_ms=300000`
- `suggestions.maintenance_vacuum_threshold_mb=100`
- `suggestions.sqlite_busy_timeout_ms=50`
- `suggestions.cache_memory_budget_mb=50`

Privacy:

- `suggestions.incognito_mode=ephemeral` (`off|ephemeral|no_send`)
- `suggestions.redact_sensitive_tokens=true`

### 22.3 Feature Flag Dependencies
- feature flags are independent by default; disabling one feature MUST disable its writes, retrieval source, and score contribution without altering unrelated features.
- `task_playbook_extended.after_failure` depends on prior command exit status, not failure-recovery feature enablement.
- disabling alias resolution can temporarily diverge template identities from prior history; this is expected and reversible when re-enabled.

## 23. Quality Gates, Acceptance Checklist, and Invariants

### 23.1 Quality Gates
Build/runtime gates MUST include:

- unit + integration + interactive tests pass
- docker matrix passes
- fuzz suite minimum runtime budget passes
- hook and suggest performance regressions within configured bounds
- cold daemon readiness under 500ms on CI baseline

### 23.2 Acceptance Checklist
Release acceptance MUST verify:

- latency targets and fail-open behavior
- cross-shell behavior and non-interactive no-op
- deterministic ranking repeatability
- incognito persistence guarantees
- task discovery and playbook trigger behavior
- search mode behavior with FTS and fallback
- color/format contracts on TTY/non-TTY
- advanced non-excluded features covered by integration/replay suites

### 23.3 Correctness Invariants
- `I1 Session Isolation`: session-scoped transitions do not bleed across sessions.
- `I2 Ephemeral Persistence`: ephemeral events do not produce persistent aggregate writes.
- `I3 Deterministic Ranking`: identical state returns byte-identical top-k.
- `I4 Bounded Hook Latency`: hook path stays under timeout budget and never blocks prompt.
- `I5 Transactional Consistency`: dependent aggregate updates are atomic per committed non-ephemeral event.
- `I6 Risk Integrity`: destructive patterns always carry risk signal before final ranking.
- `I7 Cache Coherency`: new command invalidates stale session cache before next suggest.
- `I8 Crash Safety`: daemon crash does not expose partial aggregate state after restart.
- `I9 Correlated Slot Validity`: dependent slot suggestions require observed/confident tuples.
- `I10 Learning Guardrails`: adaptive weights always stay inside configured bounds.
- `I11 Event Size Boundedness`: persisted/indexed `cmd_raw` never exceeds configured max and truncation is marked.
- `I12 Fail-open Shell Safety`: loss of daemon/helper/socket never blocks command execution.
- `I13 Retrieval Determinism`: source ordering and caps are deterministic for identical state.
- `I14 Alias Canonicalization`: equivalent alias/expanded forms map to one template identity.
- `I15 Persistent Suppression Safety`: permanent suppression is reversible and correctly scoped.

## 24. Source Traceability Matrix and Explicit Exclusions

### 24.1 Source Traceability Matrix

| Source Section | Destination Section(s) | Status |
| --- | --- | --- |
| `tech_suggestions.md` 0-3 | 2, 3, 6 | Included with consolidation |
| `tech_suggestions.md` 4-6 | 5, 8, 9 | Included with consolidation |
| `tech_suggestions.md` 7-11 | 10, 11, 12, 14 | Included with consolidation |
| `tech_suggestions.md` 12-15 | 18, 20, 23 | Included with consolidation |
| `tech_suggestions_v2.md` transport/path/timeout updates | 6, 7, 19, 22 | Included |
| `tech_suggestions_v2.md` env-size and bash recursion safety | 4, 6, 19, 20 | Included |
| `tech_suggestions_v2.md` windows transport abstraction guidance | 6, 3 | Included with consolidation |
| `tech_suggestions_ext_v1.md` goals, architecture, shell contract | 2, 4, 5, 7 | Included |
| `tech_suggestions_ext_v1.md` V2 schema and write-path semantics | 8, 9, 12, 13 | Included with consolidation |
| `tech_suggestions_ext_v1.md` normalization, slots, alias, timing | 10, 12, 13, 22 | Included |
| `tech_suggestions_ext_v1.md` candidate generation and ranking | 11, 12 | Included with consolidation |
| `tech_suggestions_ext_v1.md` feedback, caching, search, API | 13, 15, 16, 18, 19 | Included |
| `tech_suggestions_ext_v1.md` testing, observability, config, invariants | 20, 21, 22, 23 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.1 | 8, 11, 12, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.2 | 8, 10, 11, 12, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.3 | 8, 11, 12, 13, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.5 | 4, 12, 18, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.6 | 8, 10, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.7 | 8, 13, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.8 | 8, 11, 12, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.9 | 12, 18, 22 | Included with consolidation |
| `tech_suggestions_ext_v1_appendix.md` 20.10 (task triggers) | 14, 22 | Included with consolidation |
| `tech_suggestions_ext_v1_appendix.md` 20.11 | 11, 12, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.12 | 15, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.13 | 11, 12 | Included with consolidation |
| `tech_suggestions_ext_v1_appendix.md` 20.14 (non-excluded deltas) | 8, 22 | Included |
| `tech_suggestions_ext_v1_appendix.md` 20.15 (non-excluded keys) | 22 | Included |

### 24.2 Explicit Exclusions List
The following items are deliberately excluded from normative behavior in this consolidated spec:

- tables: `workflow_pattern`, `workflow_step`
- candidate source and priority entries tied to `workflow`
- ranking terms or amplifiers involving `workflow_boost` or activation state
- activation/mining/background sequence-detection rules and stale activation thresholds
- configuration keys prefixed with `suggestions.workflow_`
- tests, metrics, and acceptance items tied exclusively to workflow activation/mining
- reason types that include `workflow` as active contribution
- `.clai/tasks.yaml` `workflows:` block semantics and any seed logic into workflow tables
