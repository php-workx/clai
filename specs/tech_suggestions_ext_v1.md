# clai Suggestions Engine V2 (Greenfield) - Technical Concept

This document defines a clean-slate technical concept for a production-grade suggestions engine.
It assumes no backward-compatibility constraints and no requirement to preserve V1 storage, IPC, or ranking contracts.

## 0) Product Goals

### 0.1 Primary Outcomes
- Predict the next command with high precision and low latency in interactive shells.
- Reduce typing effort, command lookup effort, and recovery time from command errors.
- Keep shell UX non-blocking even when daemon/storage are unavailable.

### 0.2 Non-goals
- PTY capture, terminal stream parsing, or full shell AST replay.
- Cloud sync, multi-device profile sharing, or remote inference.
- LLM dependency in the hot path.

### 0.3 UX Success Criteria
- P50 suggestion latency under 15ms from shell request to first result.
- P95 under 50ms on warm cache, under 120ms on cold compute.
- Top-1 accept rate improves over baseline by at least 25%.
- Shell adapter overhead with persistent helper channel under 3ms median and under 8ms p95 per command completion.
- Fallback adapter overhead (fork/exec helper) under 8ms median and under 15ms p95 per command completion.

### 0.4 Runtime Prerequisites
- OS scope for V2:
- first-class: macOS, Linux, and WSL (bash/zsh/fish)
- native Windows (PowerShell/cmd + named pipe lifecycle) is explicitly out of scope for V2 and reserved for V3.
- SQLite build must support WAL; FTS5 is strongly recommended (fallback search path exists when unavailable).
- Runtime directory must be local filesystem with advisory locking support (`flock` semantics); network filesystems are unsupported for daemon runtime files.
- `/dev/urandom` (or platform CSPRNG equivalent) must be available for session entropy fallback.
- `$HOME` (and config/runtime dirs) must be writable for local state.
- Shell minimum versions:
- `bash >= 4.0` (`>= 4.4` preferred)
- `zsh >= 5.0`
- `fish >= 3.0`

## 1) High-Level Architecture

### 1.1 Core Components
- Shell Adapters (`bash`, `zsh`, `fish`): collect execution context and send events through a persistent helper channel by default.
- `clai-shim` (persistent helper): long-running per-shell-process helper that validates fields, lossily normalizes UTF-8, and writes NDJSON events fire-and-forget to daemon IPC.
- `clai-shim` (fallback exec mode): short-lived per-event mode for environments where persistent helper cannot be started; functionally equivalent with higher latency budget.

Persistent helper lifecycle:
- Started by shell adapter on `session_start` via `clai-shim --persistent` (background process, stdin from shell pipe).
- Shell adapter stores PID in `$CLAI_SHIM_PID` for cleanup.
- Terminated on shell exit via trap (`EXIT` handler sends `SIGTERM` to `$CLAI_SHIM_PID`).
- If helper process dies mid-session (crash, OOM), shell adapter detects broken pipe on next write and falls back to oneshot mode for the remainder of the session. No restart is attempted to avoid loop.
- Helper maintains a single gRPC connection to daemon with lazy reconnect on write failure (backoff: 100ms, 500ms, then give up and drop event).
- In-process state: connection handle, session ID, and a bounded event buffer (max 16 events, ~64KB) for absorbing short daemon unavailability. Buffer is best-effort; overflow drops oldest event.
- Resource overhead per shell: ~3MB RSS (Go runtime baseline), one goroutine for write loop.
- Note: existing `clai-shim` is oneshot-only (`cmd/clai-shim/main.go`). Persistent mode is new work requiring a `--persistent` flag, stdin read loop, and background connection management.
- `claid` daemon:
- Ingestion pipeline with validation and normalization.
- Feature extraction and online aggregate maintenance.
- Candidate generation and deterministic ranking.
- Suggestion cache and feedback processing.
- SQLite datastore with WAL and migration lock

### 1.2 Design Principles
- Hot path is memory-first, DB-second.
- Hook layer is best-effort and never blocks the prompt.
- Ranking is deterministic and explainable.
- Learning is online and fast, with bounded memory and bounded drift.

### 1.3 Runtime Data Flow
1. User runs command in shell.
2. Post-command hook emits `command_end` to persistent `clai-shim`.
3. `clai-shim` sends NDJSON to daemon over local IPC.
4. Daemon updates in-memory aggregates and asynchronously flushes batched state to SQLite.
5. On prompt update or typed prefix, shell asks `clai suggest`.
6. Daemon returns top-k suggestions from session cache or computes on demand.
7. On suggestion accept/dismiss, shell sends feedback event to improve future ranking.

Fallback mode:
- If persistent helper is unavailable, hook invokes `clai-shim ingest --oneshot`.
- This mode is fail-open and retains full correctness with degraded latency targets.

### 1.4 Ingestion-Suggestion Ordering Guarantee
- Race condition to handle: `command_end` ingestion may arrive slightly after the immediate subsequent `Suggest` call.
- `SuggestRequest` includes fallback context fields:
- `last_cmd_raw`, `last_cmd_norm` (if available), `last_cmd_ts_ms`, `last_event_seq` (monotonic per session when available).
- Daemon behavior:
- If there is a pending ingestion item for the same session newer than cached state, daemon may wait up to `suggestions.ingest_sync_wait_ms` (default `5ms`) before ranking.
- If wait budget is exceeded, daemon ranks using persisted/cache state plus `SuggestRequest` fallback context.
- Daemon must never block longer than the suggest hard timeout.

## 2) Cross-Shell Contract

### 2.1 Supported Shells and Versions
- `bash >= 4.4` preferred, `bash >= 4.0` supported with compatibility branch.
- `zsh >= 5.0`.
- `fish >= 3.0`.
- V2 adapter scope is Unix-like shells only (including WSL distributions). Native PowerShell/cmd adapters are out of scope for V2.
- Install-time adapter probe must detect shell version and emit explicit diagnostics on unsupported versions.
- macOS bash `3.2` must be detected and handled with clear warning + degraded compatibility mode (no preexec timing, postexec-only ingestion), unless user upgrades bash.

Implementation milestone order:
- Phase 1 (MVP): bash adapter — existing `internal/cmd/shell/bash/clai.bash` is the baseline; extend with V2 shim integration, feedback hooks, and session ID contract.
- Phase 2: zsh adapter — highest-value addition (ghost text via ZLE POSTDISPLAY, full adaptive timing, native accept binding).
- Phase 3: fish adapter — lowest friction (fish-native autosuggestion cadence, built-in `string` builtins, clean event model).
- All three adapters share the same cross-shell contract (Sections 2.2-2.6). Shell-specific behavior is isolated to Section 2.4.
- Adapters not yet implemented emit a clear diagnostic on `clai init <shell>` and degrade to no-suggestion mode without breaking the shell.

### 2.2 Hook Lifecycle Events
- `session_start`: emitted once per interactive shell instance.
- `command_start`: emitted before command execution.
- `command_end`: emitted after completion with exit and duration.
- `suggest_request`: emitted when shell asks suggestions (optional telemetry).
- `suggest_feedback`: accepted, dismissed, edited-before-run.

### 2.3 Required Event Fields
- `event_type`
- `session_id`
- `shell`
- `ts_ms`
- `cwd`
- `cmd_raw` (for command events)
- `exit_code` (for `command_end`)
- `duration_ms` (if available)
- `ephemeral` (incognito)

### 2.4 Shell-Specific Behavior
- `zsh`: use `preexec`/`precmd`, duration from `EPOCHREALTIME`.
- `fish`: use `fish_preexec`/`fish_postexec`, duration from `CMD_DURATION`.
- `bash`: use `DEBUG` trap plus `PROMPT_COMMAND`; for 4.4+ use array `PROMPT_COMMAND`, for older use safe string append.

Bash critical rule:
- The first operation in post-command hook must capture previous exit status: `local _clai_exit=$?`.
- No command (including helper invocations) may run before this capture.

Bash coexistence requirements:
- If `bash-preexec` is present, register hooks through its API instead of replacing traps directly.
- If `bash-preexec` is not present, install a chaining wrapper that preserves existing `DEBUG` trap behavior.
- For bash `< 4.4`, define post-command hook in function `__clai_postcmd` and append `; __clai_postcmd` to `PROMPT_COMMAND` with idempotent guard.
- Document expected behavior under `set -T`, `set -E`, and `shopt -s extdebug`: clai hooks remain best-effort and must not break existing trap semantics; diagnostics are emitted through doctor when unsafe combinations are detected.

Bash `DEBUG` chaining pseudocode (implementation sketch):
```bash
__clai_original_debug_trap="$(trap -p DEBUG | sed "s/^trap -- '//;s/' DEBUG$//")"
trap '__clai_preexec_hook "$BASH_COMMAND"; __clai_status=$?; if [[ -n "$__clai_original_debug_trap" ]]; then eval "$__clai_original_debug_trap"; fi; return $__clai_status' DEBUG
```
- Wrapper requirement: preserve original trap execution and do not swallow non-zero status from previous trap chain elements.

Fish-specific implementation notes:
- Use `set -l` for local variables; never use `local`.
- Capture exit status with `set -l _clai_exit $status` as the first line of `fish_postexec`.
- Do not rely on process substitution (`<()`, `>()`); use pipes or `psub` only when unavoidable.
- Prefer autoloaded hook functions in `~/.config/fish/functions/` for deterministic load order and debuggability.
- Prefer fish `string` builtins over external `sed`/`awk` for adapter text operations.
- Fish alias capture scope for V2:
- capture abbreviations via `abbr --show`.
- do not capture function-backed aliases in V2 (including `alias`-generated wrapper functions).

### 2.5 Safety Rules
- Interactive shell check required before installing hook behavior.
- Never pass command text via CLI args.
- Never emit shell-generated JSON.
- Hook must suppress non-critical stderr noise.
- Hook must fail open (shell continues normally on hook failure).

Interactivity and TTY detection contract:
- `bash`/`zsh`: require shell interactive mode (`$-` contains `i` or equivalent), and `test -t 0`.
- `fish`: require `status is-interactive`, and `test -t 0`.
- Non-interactive shells must skip hook installation and produce no suggestion side effects.

Hook-path stderr discipline:
- Fatal hook-path errors may emit one line to stderr: `clai: <message>`.
- Non-fatal hook-path errors must be suppressed from stderr and routed to daemon logs/doctor surfaces.

### 2.6 Session ID Assignment
- `session_id` is required for all command and suggestion events.
- Preferred strategy: daemon-assigned at shell startup:
- Shell invokes `clai-shim session-start`; shim gets/creates session ID from daemon and exports it for the shell process.
- Fallback strategy (daemon unavailable at startup):
- Shell computes local ID from `hostname + pid + shell_start_time + random_seed + container_fingerprint`, hashed to stable string.
- `random_seed` must be at least 64 bits from CSPRNG (`/dev/urandom` or platform equivalent).
- `container_fingerprint` should include `/proc/self/cgroup` content or container ID env value when available.
- Session ID must be stable for shell lifetime and unique across concurrent shells on same host.

### 2.7 Suggestion Presentation Contract
- `clai suggest` is line-oriented in V2 (no built-in fullscreen TUI).
- Default rendering mode per shell:
- `zsh`: render ghost text using ZLE `POSTDISPLAY` strategy; clear on buffer mutation.
- `fish`: use fish-native autosuggestion cadence/path where integration allows; clear on next edit/commandline change.
- `bash`: render suggestion on a separate prompt-adjacent line (non-inline) to avoid invasive readline patching in V2.
- CLI output remains plain line output unless explicit `--format` is requested.
- Completion coexistence:
- native shell completion remains bound to `Tab` by default
- clai acceptance bindings must be explicit and opt-in per shell integration.
- Feedback hooks should fire on explicit accept/dismiss actions where shell supports it.
- Default accept bindings:
- `zsh`/`fish` integration defaults to `Right Arrow` accept when cursor is at line end.
- `bash` defaults to non-invasive hint mode (no default accept keybinding) unless user explicitly enables binding.

### 2.8 CLI Output and Terminal Capability Contract
- `--color=auto|always|never` must be supported by suggestion-facing CLI commands.
- Default is `--color=auto`:
- ANSI formatting only when stdout is a TTY, `$TERM` is not `dumb`, and `$NO_COLOR` is not set.
- JSON output must never contain ANSI escape sequences.
- `--format` behavior:
- `text`: safe for TTY and piped output.
- `json`: machine-safe, no ANSI, deterministic field order.
- `fzf`: only valid in interactive TTY; if stdin/stdout are not TTY, command must fail with structured `E_UNSUPPORTED_TTY`.
- Capability fallback:
- if `$TERM` is empty or `dumb`, use ASCII-only rendering and no ANSI styling.
- Width detection for wrapped line output should use terminal ioctls (`x/term`) rather than external tools (`tput`).
- Accessibility default:
- textual line output is the baseline behavior; no overlay-only rendering path is allowed in V2.
- honor `$NO_COLOR` for high-contrast and screen-reader-friendly terminal workflows.

## 3) IPC and Daemon Resilience

### 3.1 Transport
- Unix domain socket on macOS/Linux.
- Transport abstraction keeps named-pipe backend reserved for future native Windows support.
- Wire format: gRPC with protobuf over Unix domain socket.
- The canonical service definition is `proto/clai/v1/clai.proto`.
- JSON payload descriptions in Section 13 are logical schemas; implementations use protobuf wire encoding.
- HTTP/JSON debug endpoints (Section 13.1 DebugStats) are served on a separate diagnostic listener when enabled.

Transport migration note:
- V2 retains gRPC-over-UDS as the primary transport to preserve compatibility with the existing `clai-shim` gRPC client and `ClaiService` proto definition.
- Section 13 API names map to proto RPCs as follows:
  - `IngestEvent` -> `CommandStarted` + `CommandEnded` (split by event type)
  - `Suggest` -> `Suggest`
  - `Search` -> `FetchHistory` (extended with search modes)
  - `RecordFeedback` -> new RPC to add to proto
  - `DebugStats` -> HTTP/JSON diagnostic endpoint (not gRPC)
- The proto must be extended to add: `RecordFeedback`, `SuggestFeedback`, search mode fields on `FetchHistory`, and the additional request/response fields defined in Section 13.1.

Windows named-pipe contract:
- This is a forward-compatibility placeholder for V3 native Windows support.
- Pipe path format: `\\\\.\\pipe\\claid-<user-scope>`.
- `<user-scope>` should be SID-based when available; fallback is a stable username hash.
- Pipe ACL must restrict access to current user context.

### 3.2 Timeout Policy
- Connect timeout default 15ms (configurable 10-25ms).
- Write timeout default 20ms.
- No response wait in hook path.

### 3.3 Socket Location
- Resolution order:
- `$XDG_RUNTIME_DIR/clai/suggestd.sock`
- macOS fallback: `$HOME/Library/Caches/clai/suggestd.sock`
- `$TMPDIR/clai-$UID/suggestd.sock`
- `/tmp/clai-$UID/suggestd.sock`
- Parent dir mode `0700`.

### 3.4 Crash Recovery
- Single-instance lock file (`.suggestd.lock`).
- On daemon startup:
- Acquire lock using `flock`-style non-blocking exclusive lock.
- If lock acquisition fails, inspect PID owner:
- if owner is alive and process is `claid`, exit with `E_DAEMON_UNAVAILABLE`.
- if owner is stale, clean stale lock and retry lock acquisition once.
- Run migrations.
- Clean stale socket.
- Start listeners.
- Runtime dir must be local filesystem; NFS/network FS for lock files is unsupported.

### 3.5 Backpressure and Failure Policy
- Ingestion queue is bounded (default `8192` events).
- Burst-mode circuit breaker protects against script storms:
- Enter burst mode when more than `suggestions.burst_events_threshold` events from one `session_id` arrive inside `suggestions.burst_window_ms` (defaults: `10` events in `100ms`).
- While in burst mode, persist only boundary events (`first` and `last` `command_end` in a burst bucket) and update in-memory recency for intermediate events.
- Exit burst mode after `suggestions.burst_quiet_ms` of silence (default `500ms`).
- Emit `ingest_burst_mode_entries` and `ingest_burst_mode_dropped_events` metrics.
- When queue is full, daemon applies drop-oldest policy for non-critical telemetry events and increments `ingest_drop_count`.
- `command_end` and `session_start/session_end` are high-priority and must be retained preferentially over optional telemetry.
- Hook path remains fail-open: if connect or write exceeds timeout budget, event is dropped without blocking shell.
- Daemon must never block suggestion serving on ingestion flush backlog.

### 3.6 Reload and Upgrade Behavior
- `SIGHUP`: reload runtime config (weights, timeouts, discovery settings) without dropping listener.
- `SIGUSR1`: graceful self-reexec path for binary upgrades:
- stop accepting new ingest frames
- flush in-flight writes
- release and reacquire lock/socket around exec handoff
- preserve zero-downtime best effort; if handoff fails, daemon exits cleanly and client auto-reconnect path recovers.

Signal handling safety:
- Daemon must ignore `SIGPIPE` to prevent crash on broken socket peers.
- CLI commands and shim must treat stdout/stderr `EPIPE` as clean termination when downstream pipe closes.
- V2 line-oriented CLI mode does not require `SIGWINCH`/`SIGTSTP` terminal-state handling.

Windows parity note:
- `SIGHUP`/`SIGUSR1` semantics are Unix-only in V2 scope.
- Native Windows control-plane equivalents are specified as future work (control command or service manager integration).

### 3.7 Daemon Lifecycle Management
- Auto-start is required:
- first `session_start` path attempts to start daemon opportunistically if not running.
- Required mechanism:
- `clai-shim session-start` attempts socket connect first.
- on connect failure, fork+exec `claid` in background with stdout/stderr redirected to daemon log path, then retry connect with bounded backoff.
- explicit management surface is required:
- `clai daemon start|stop|status|reload`.
- Optional platform integration:
- launchd (macOS) and systemd user service (Linux) recommended, not required.
- Degraded behavior when daemon unavailable:
- shell hooks remain fail-open, events may be dropped
- `clai suggest` returns empty (or explicit error in `--strict`)
- shell interactivity is never blocked.

## 4) Storage Model (V2 Schema)

No migration bridge to V1 is required. V2 initializes and owns its schema.

V1 coexistence and data policy:
- V2 uses a separate database file (`suggestions_v2.db`) alongside the existing V1 database (`suggestions.db`).
- V1 data is not migrated automatically; V1 remains read-only for existing `clai search` until deprecated.
- V2 starts with empty history and learns from new commands. Users who want V1 history in V2 can run an optional one-time import tool: `clai suggestions import-v1` (best-effort, skips events that fail V2 validation).
- The import tool is not required for V2 to function and is not on the critical path.
- V1 code (`internal/suggest/`) and V2 code (`internal/suggestions/`) coexist until V1 is explicitly removed in a future release.

### 4.1 Tables
- `session`
- `command_event`
- `command_template`
- `transition_stat`
- `command_stat`
- `slot_stat`
- `slot_correlation`
- `project_type_stat`
- `project_type_transition`
- `pipeline_event`
- `pipeline_transition`
- `pipeline_pattern`
- `failure_recovery`
- `workflow_pattern`
- `workflow_step`
- `task_candidate`
- `suggestion_cache`
- `suggestion_feedback`
- `session_alias`
- `dismissal_pattern`
- `rank_weight_profile`
- `command_event_fts` (virtual table)
- `schema_migrations`

Schema lifecycle rule:
- All tables listed in this section are created during initial DB setup regardless of feature-flag enablement state.
- Feature flags gate runtime writes/reads only; they do not gate schema existence.

### 4.2 Schema Sketch
```sql
CREATE TABLE session (
  id TEXT PRIMARY KEY,
  shell TEXT NOT NULL,
  started_at_ms INTEGER NOT NULL,
  project_types TEXT,
  host TEXT,
  user_name TEXT
);

CREATE TABLE command_event (
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

CREATE TABLE command_template (
  template_id TEXT PRIMARY KEY,
  cmd_norm TEXT NOT NULL,
  tags TEXT,
  slot_count INTEGER NOT NULL,
  first_seen_ms INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL
);

CREATE TABLE transition_stat (
  scope TEXT NOT NULL,
  prev_template_id TEXT NOT NULL,
  next_template_id TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_template_id, next_template_id)
);

CREATE TABLE command_stat (
  scope TEXT NOT NULL,
  template_id TEXT NOT NULL,
  score REAL NOT NULL,
  success_count INTEGER NOT NULL,
  failure_count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id)
);

CREATE TABLE slot_stat (
  scope TEXT NOT NULL,
  template_id TEXT NOT NULL,
  slot_index INTEGER NOT NULL,
  value TEXT NOT NULL,
  weight REAL NOT NULL,
  count INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id, slot_index, value)
);

CREATE TABLE slot_correlation (
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

CREATE TABLE task_candidate (
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

CREATE TABLE suggestion_feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  ts_ms INTEGER NOT NULL,
  prompt_prefix TEXT,
  suggested_text TEXT NOT NULL,
  action TEXT NOT NULL,
  executed_text TEXT,
  latency_ms INTEGER
);

CREATE TABLE rank_weight_profile (
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

CREATE TABLE suggestion_cache (
  cache_key TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  context_hash TEXT NOT NULL,
  suggestions_json TEXT NOT NULL,
  created_ms INTEGER NOT NULL,
  ttl_ms INTEGER NOT NULL,
  hit_count INTEGER NOT NULL DEFAULT 0
);

CREATE VIRTUAL TABLE command_event_fts USING fts5(
  cmd_raw,
  cmd_norm,
  repo_key UNINDEXED,
  session_id UNINDEXED,
  content='command_event',
  content_rowid='id',
  tokenize='trigram'
);

-- Tokenizer shown above is the default.
-- On fresh DB initialization, tokenizer may be selected from config
-- (`suggestions.search_fts_tokenizer`: `trigram` or `unicode61`).

CREATE TRIGGER command_event_ai AFTER INSERT ON command_event
WHEN NEW.ephemeral = 0
BEGIN
  INSERT INTO command_event_fts(rowid, cmd_raw, cmd_norm, repo_key, session_id)
  VALUES (NEW.id, NEW.cmd_raw, NEW.cmd_norm, NEW.repo_key, NEW.session_id);
END;

CREATE TRIGGER command_event_ad AFTER DELETE ON command_event
BEGIN
  INSERT INTO command_event_fts(command_event_fts, rowid, cmd_raw, cmd_norm, repo_key, session_id)
  VALUES ('delete', OLD.id, OLD.cmd_raw, OLD.cmd_norm, OLD.repo_key, OLD.session_id);
END;

CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_ms INTEGER NOT NULL
);
```

Additional schema objects:
- Advanced V2 objects are also required for project-type priors, pipelines, failure recovery, workflows, aliases, and persistent dismissals:
- `project_type_stat`, `project_type_transition`
- `pipeline_event`, `pipeline_transition`, `pipeline_pattern`
- `failure_recovery`
- `workflow_pattern`, `workflow_step`
- `session_alias`
- `dismissal_pattern`
- The full SQL sketch for these additive objects is maintained in `/Users/runger/.claude-worktrees/clai/happy-hypatia/specs/tech_suggestions_ext_v1_appendix.md`.
- Normative supplement rule:
- Appendix sections `20.1` through `20.15` are normative for additive table SQL and algorithms referenced by this section.

### 4.3 Storage Policies
- SQLite WAL mode, one writer goroutine, batched commits every 25-50ms or 100 events.
- Ephemeral events are never persisted to long-lived aggregates.
- Retention policy is mandatory:
- retain last `90` days of `command_event` rows.
- retain maximum `500000` `command_event` rows.
- when both limits apply, prune oldest rows first.
- Retention pruning semantics:
- pruning `command_event` rows must trigger FTS delete synchronization via trigger path.
- `pipeline_event` rows for pruned `command_event` rows are cascade-deleted (or equivalent explicit delete in same maintenance task).
- aggregate tables (`command_stat`, `transition_stat`, `slot_stat`, `project_type_stat`, `project_type_transition`, `failure_recovery`) are cumulative and are not rewritten during pruning.

WAL and checkpoint policy:
- Set `PRAGMA wal_autocheckpoint=1000` (tunable).
- Background maintenance task performs:
- periodic `PRAGMA wal_checkpoint(PASSIVE)` during steady state
- `PRAGMA wal_checkpoint(TRUNCATE)` on low-activity windows
- optional `VACUUM` on size/fragmentation threshold and only outside hot path.
- FTS maintenance includes periodic `INSERT INTO command_event_fts(command_event_fts) VALUES('optimize')` off hot path.

Background maintenance scheduling:
- Maintenance runs on a dedicated goroutine with a ticker-based schedule.
- Base interval: every `suggestions.maintenance_interval_ms` (default `300000`, i.e., 5 minutes).
- Low-activity detection: fewer than `5` ingested events in the last maintenance interval qualifies as low-activity.
- During low-activity windows: run `wal_checkpoint(TRUNCATE)` and FTS optimize.
- During active windows: run `wal_checkpoint(PASSIVE)` only.
- VACUUM trigger: when DB file size exceeds `suggestions.maintenance_vacuum_threshold_mb` (default `100`) AND freelist pages exceed 20% of total. VACUUM runs at most once per `24` hours.
- Retention pruning runs in the same maintenance goroutine, using batched deletes of `1000` rows per iteration to avoid long-held write locks. Pruning yields between batches (100ms sleep) to allow ingestion writes.
- If any maintenance operation fails, log error metric and continue; do not crash daemon.
- Maintenance is skipped entirely while daemon is in startup or shutdown phase.

### 4.4 Write-Path Transaction Semantics
- All writes for a single ingested event are applied in one transaction (`BEGIN IMMEDIATE ... COMMIT`) to preserve aggregate consistency.
- Writer connection sets `PRAGMA busy_timeout=50` (configurable) to absorb short lock contention.
- Transaction order for non-ephemeral `command_end`:
- Insert row in `command_event`.
- Upsert `command_template`.
- Update `command_stat` (frequency + success/failure counters).
- Update `transition_stat` if previous template is known for session.
- Update `slot_stat` values extracted from template/args alignment.
- Update `slot_correlation` for configured slot tuples (example: `<namespace>|<pod>`).
- Update project-type aggregates (`project_type_stat`, `project_type_transition`) when project types are active.
- Update directory-scoped aggregates using `scope=dir:<hash>` for repo-relative cwd buckets.
- For compound commands, update `pipeline_event`, `pipeline_transition`, and `pipeline_pattern`.
- Update failure recovery aggregates when previous command in session failed.
- Update in-memory cache index and invalidation markers.
- If online ranking updates are enabled, enqueue async `rank_weight_profile` update work item after transaction commit.
- For `ephemeral=1`, only in-memory session-scoped structures are updated; no SQLite write occurs.
- If transaction fails, daemon records error metric, abandons partial event effects, and keeps process alive.
- Busy retry policy:
- on `SQLITE_BUSY` after busy timeout, requeue event once.
- if second attempt fails, drop event, increment `ingest_drop_count`, and continue.
- Keep write-path bounded with feature caps:
- max project types per event
- `pipeline_max_segments`
- bounded workflow activation set per session (in-memory).
- Advanced feature writes (`project_type_*`, directory-scope aggregates, `pipeline_*`, `failure_recovery`) are order-independent within the transaction after core writes complete.
- Performance invariant:
- combined write-path transaction must complete within `25ms` P95 on reference hardware.
- if this invariant regresses, advanced feature writes may be shifted to a bounded deferred batch path while core writes remain synchronous.

### 4.5 Corruption Recovery
- On startup, if SQLite returns corruption/malformed errors:
- rename DB files to `suggestions.db.corrupt.<timestamp>` (including `-wal` and `-shm` when present)
- initialize a fresh DB
- emit critical log + doctor diagnostic entry
- continue serving with empty history rather than crash-looping.

## 5) Normalization and Template System

### 5.1 Goals
- Deduplicate equivalent commands.
- Preserve intent while abstracting volatile arguments.
- Enable transition and slot prediction.

### 5.2 Tokenization
- Use shell-like tokenizer (`shlex` style) with robust fallback for malformed input.
- Keep original raw command for audit/debug and rendering decisions.
- UTF-8 normalization contract:
- invalid UTF-8 is normalized with `strings.ToValidUTF8(..., "\uFFFD")`.
- raw byte sequence is not preserved in V2.
- locale transcoding is not attempted in V2; input is treated as UTF-8 byte stream.

Pre-tokenization stages:
- Ordered pre-normalization pipeline:
1. alias expansion on first token (session-scoped; bounded depth; cycle detection).
2. pipeline/compound split on unquoted operators (`|`, `|&`, `&&`, `||`, `;`).
3. per-segment tokenization and normalization.
- Implementation details for alias capture and splitter edge cases are specified in appendix sections `20.2` and `20.6`.

### 5.3 Event Size Limits
- `cmd_raw` ingestion maximum is `16384` bytes (configurable).
- Events exceeding limit are truncated to max length and marked `cmd_truncated=1`.
- Truncation is applied before persistence, ranking features, and FTS indexing.
- Oversized events are never allowed to bypass queue byte budgets.

### 5.4 Normalization Rules
- Lowercase command/tool token.
- Collapse whitespace.
- Replace dynamic values with slots:
- `<path>`
- `<num>`
- `<sha>`
- `<url>`
- `<msg>`
- Optionally domain slots (`<branch>`, `<namespace>`, `<service>`).
- Extract deterministic semantic tags into `command_template.tags` for describe-mode search (`tool`, `subcommand`, selected flag semantics, argument-pattern tags).
- Tag vocabulary contract:
- initial vocabulary is deterministic and embedded in code (plus optional override path).
- extraction rules and examples are specified in appendix section `20.12`.

### 5.5 Template Identity
- `template_id = sha256(cmd_norm)`.
- Store `cmd_norm` and slot count in `command_template`.

### 5.6 Slot Dependency Registry
- Normalizer maintains optional per-template dependency sets for semantically coupled slots (for example `<namespace>` + `<pod>`, `<cluster>` + `<context>`).
- Dependency sets are keyed by `slot_key` (pipe-delimited slot indexes such as `1|3`) and used by `slot_correlation`.

## 6) Candidate Generation Pipeline

### 6.1 Inputs
- Session context: last command/template, cwd, repo key, branch.
- Typed prefix (possibly empty).
- Incognito state.
- Recent feedback for immediate personalization.

### 6.2 Retrieval Sources
Priority order:
1. Session transitions.
2. Failure recovery candidates (only when last command failed).
3. Active workflow next-step candidates.
4. Pipeline pattern recall.
5. Repo transitions.
6. Directory-scoped transitions.
7. Project-type transitions.
8. Playbook conditional task triggers (`after`, `after_failure`).
9. Global transitions.
10. Session/repo/dir/global frequency priors.
11. Typo correction candidates after failures (when applicable).
12. Discovery candidates (low-priority gated source).

### 6.3 Retrieval Budget
- Retrieve up to 200 candidates total in ranked source order.
- Hard cap per source to avoid source domination.
- Recommended source caps:
- failure recovery: 5
- workflow next-step: 3
- pipeline patterns: 10
- project-type transitions: 30
- discovery: 2

### 6.4 Prefix Filtering Modes
- Empty prefix: pure next-step mode.
- Non-empty prefix: constrained mode (prefix and fuzzy tolerance).

## 7) Ranking and Post-Processing

### 7.1 Ranking Model (Deterministic Weighted Score)
`score = w1*transition_effective + w2*frequency + w3*success + w4*prefix + w5*affinity_enhanced + w6*task_extended + w7*feedback_extended + w9*project_type_affinity + w10*failure_recovery - w8*risk_penalty`

Default initial weights:
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

Scoring notes:
- Each feature is normalized into `[0, 1]` before weighting.
- Transition and frequency values are log-scaled before normalization.
- Risk penalty is applied post-aggregation and can force candidate suppression.
- Per request, weight vector is resolved from `rank_weight_profile` (`session` -> `repo` -> `global_default`) and snapshotted for deterministic ordering within that request.
- `transition_effective` may be amplified by workflow activation and pipeline confidence.
- `affinity_enhanced` includes directory-scope locality.
- `task_extended` includes playbook conditional trigger boosts.
- `feedback_extended` includes persistent dismissal penalties.
- Amplifier formulas:
- `transition_effective = clamp(base_transition + workflow_boost*activation_score + pipeline_confidence*pipeline_transition_weight, 0, 1)`
- `affinity_enhanced = 0.4*repo_match + 0.3*dir_scope_match + 0.2*branch_match + 0.1*cwd_exact_match`
- `task_extended = clamp(base_task_score + playbook_after_boost, 0, 1)` when playbook trigger matches, otherwise `base_task_score`
- `feedback_extended = base_feedback + dismissal_penalty(context, candidate)` where dismissal penalty is in `[-1, 0]`
- Score scale rule:
- final weighted score is not renormalized to `[0,1]`; thresholds (`min_score`) and margins operate on raw weighted scale.

### 7.2 Feature Definitions
- `transition_effective`: decayed edge weight from previous template, optionally amplified by workflow and pipeline signals.
- `pipeline_transition_weight`: normalized pipeline-transition support signal in `[0,1]` when continuation/pattern context applies.
- `frequency`: decayed command/template score.
- `success`: success ratio for candidate template.
- `prefix`: exact + fuzzy prefix match quality.
- `affinity_enhanced`: cwd/repo/branch + directory-scope proximity.
- `task_extended`: boost for discovered tasks and playbook trigger matches.
- `feedback_extended`: recent accept boost plus persistent dismissal learning.
- `project_type_affinity`: normalized strength of command in active project type(s).
- `failure_recovery`: recovery relevance for recent non-zero exit context.
- `risk_penalty`: destructive pattern penalty.

### 7.3 Confidence
- Confidence is calibrated from feature support diversity and score margin over runner-up.
- Low-confidence suggestions can be hidden unless user asks for full list.

### 7.4 Diversity and Dedup
- Deduplicate by normalized command and rendered text.
- Apply near-duplicate suppression so top-k is meaningfully different.

### 7.5 Slot Filling
- Fill slots from scoped histograms with confidence threshold.
- Fallback order: session -> repo -> global.
- For templates with dependency sets, generate multi-slot assignments from `slot_correlation` first, then fill remaining independent slots from `slot_stat`.
- Reject mixed assignments that violate correlation confidence threshold (`suggestions.slot_correlation_min_confidence`).
- If uncertain, return template with partially unfilled slots only if UX supports it; otherwise skip.

Slot value eviction:
- Each `(scope, template_id, slot_index)` group is bounded by `suggestions.slot_max_values_per_slot` (default `20`).
- When a new value would exceed the limit, evict the entry with the lowest `weight` in that group.
- Tie-break for eviction: oldest `last_seen_ms` first.
- Eviction is performed inline during the ingestion write transaction (DELETE + INSERT within the same transaction).
- Eviction applies per-scope: session, repo, and global scopes maintain independent value sets.

### 7.6 Deterministic Ordering and Tie-Break Rules
- Primary sort key: `score DESC`.
- Secondary sort key: `confidence DESC`.
- Tertiary sort key: `last_seen_ms DESC`.
- Final tie-break: lexical `cmd_norm ASC` to guarantee stable output.
- Candidate suppression rules:
- Drop suggestions with `confidence < min_confidence` unless `include_low_confidence=true`.
- Drop risky candidates when `risk_penalty` forces final score below `min_score`.
- At most one suggestion per normalized template unless slot-filled variants differ by at least one non-trivial argument.

### 7.7 Adaptive Weight Tuning (Online Learning)
- Weight adaptation is online and per profile (`session`, `repo`, `global`) with strict guardrails.
- Update trigger:
- explicit `accepted` feedback with candidate snapshot
- implicit acceptance from exact next-command match
- Pairwise update rule (lightweight bandit-style):
- positive sample = accepted suggestion feature vector `f_pos`
- negative sample = highest-ranked unaccepted candidate feature vector `f_neg`
- `w_next = clamp(w_prev + eta * (f_pos - f_neg), min_w, max_w)`
- Renormalize non-penalty weights to keep bounded total contribution; keep `w_risk_penalty` in independent safe range.
- Default `eta` is small (`0.02`) and decays with `sample_count` to avoid overfitting.
- Decay formula: `eta_effective = eta_initial / (1 + sample_count / eta_decay_constant)`.
- `eta_decay_constant` defaults to `500` (configurable as `suggestions.online_learning_eta_decay_constant`).
- Floor: `eta_effective` is clamped to a minimum of `0.001` to prevent learning from halting entirely.
- At 500 samples, effective rate is `0.01`; at 2000 samples, effective rate is `0.004`.
- If feedback volume is below `suggestions.online_learning_min_samples`, engine uses static defaults only.
- All updates are async and versioned; suggest path reads last committed profile snapshot only.

## 8) Typo Recovery

### 8.1 Trigger
- Primary trigger: previous command exit code `127`.
- Optional trigger: explicit parser hints for common "unknown command" patterns.
- Broader failure-context recovery is enabled when previous command exit code is non-zero.

### 8.2 Matching
- Damerau-Levenshtein on first token and command stem.
- Candidate pool constrained to frequent commands and current scope.

### 8.3 Ranking Behavior
- Typo-fixed candidate gets temporary high boost for immediate next prompt.
- Boost decays quickly if not accepted.
- Failure-recovery candidates are prioritized after failed commands and weighted by observed recovery success.

## 9) Learning Loop and Feedback

### 9.1 Feedback Events
- `accepted`
- `dismissed`
- `edited_then_run`
- `ignored_timeout`
- `never` (explicit permanent suppression)
- `unblock` (explicit reversal of permanent suppression)

Feedback signal sources:
- Explicit signals from shell UX integrations that support accept/dismiss bindings (zsh/fish widgets, picker accept actions).
- Implicit heuristic for shells without explicit callbacks:
- if next executed command exactly matches prior top suggestion within `feedback_match_window_ms` (default `5000ms`), record `accepted_implicit`.
- if executed command shares template but differs arguments, record `edited_then_run`.
- `dismissed` is recorded only when explicit UI signal exists to avoid false negatives.

### 9.2 Update Rules
- Accepted suggestions increase source-specific and template-specific priors.
- Dismissed suggestions apply short-term suppression.
- Edited-then-run updates slot statistics and normalizer correction maps.
- Accepted and accepted_implicit events update `slot_correlation` counts for dependency sets when all required slot values are present.
- Accepted events trigger adaptive weight update pipeline for the active profile.
- Persistent dismissal learning:
- repeated context-specific dismissals graduate from temporary to learned suppression.
- explicit `never` action sets permanent block until explicit `unblock`.
- Detailed suppression state-machine semantics are specified in appendix section `20.7`.

### 9.3 Drift Control
- Decay all feedback effects over time.
- Clamp max per-template boost to avoid lock-in.

## 10) Caching and Latency Strategy

### 10.1 Multi-layer Cache
- L1: per-session hot cache in memory keyed by last event id + prefix hash.
- L2: per-repo cache for cold session fallback.
- L3: SQLite aggregate fallback.
- Global in-memory budget applies to suggestion caches and session hot state (default `50MB`).
- Eviction policy under pressure:
- evict L2 entries first (LRU), then L1 entries (LRU).

### 10.2 Invalidation
- Invalidate on new `command_end` for session.
- Partial invalidate on cwd/repo/branch change.
- TTL invalidate (default 30s) for stale contexts.

### 10.3 Budgets
- P50 cache-hit response under 10ms.
- P95 cold compute under 120ms.
- Async precompute on command completion.

Hot-path limits:
- Candidate retrieval deadline: 20ms.
- Ranking deadline: 10ms.
- End-to-end hard timeout for `Suggest`: 150ms.
- If timeout exceeded, return best cache fallback, never empty unless no cache exists.

## 11) Project Task Discovery

### 11.1 Built-in Sources
- `package.json` scripts.
- Makefile targets.
- `justfile` recipes.
- Optional extensions (`taskfile`, `cargo`, `pnpm`).
- Static team playbook file: `.clai/tasks.yaml`.

### 11.2 Discovery Runtime Rules
- Run with timeout and output cap.
- Sandboxed environment (no inherited secrets by default).
- Errors are non-fatal and observable through diagnostics.
- Watch `.clai/tasks.yaml` with debounce; apply incremental refresh on checksum change.
- Playbook entries use `source='playbook'` and receive configured boost (`suggestions.task_playbook_boost`) over auto-discovered tasks.
- Invalid YAML never blocks suggestions; daemon keeps last valid snapshot and reports parse error in diagnostics.
- Extended playbook mode:
- support task dependencies (`after`) and failure dependencies (`after_failure`) for conditional suggestions.
- support `workflows` seeding into workflow pattern store.

### 11.3 Data Contract
- Each task candidate includes `kind`, `name`, `command_text`, optional description, `source`, and `priority_boost`.

### 11.4 `.clai/tasks.yaml` Contract
- File path is repository root relative: `.clai/tasks.yaml`.
- Schema fields per entry:
- `name` (required, unique within file)
- `command` (required)
- `description` (optional)
- `tags` (optional)
- `enabled` (optional, default true)
- Parse failure is soft; previous valid set stays active until fixed.
- Extended optional fields:
- `after`: task/template triggers
- `after_failure`: triggers only when previous command failed
- `priority`: `low|normal|high`
- optional `workflows` block with ordered `steps` arrays
- Validation rules:
- no circular `after` graphs
- all referenced task names must resolve
- workflow steps must normalize successfully.

## 12) Security and Privacy

### 12.1 Data Safety
- Local-only storage by default.
- No shell blocking for security checks.
- No command transport over arguments.

### 12.2 Incognito Modes
- `no_send`: skip ingestion.
- `ephemeral`: ingest for session quality, no persistence.

### 12.3 Sensitive Data Handling
- Optional sanitization stage for tokens resembling secrets.
- Never log raw command text at info level.

### 12.4 Privilege and Multi-User Safety
- Daemon must run as the invoking user only; running daemon as root is forbidden.
- Commands prefixed with `sudo` are ingested as normal command patterns.
- Entering root shells (`sudo -i`, `su -`) starts a new logical shell session; if user daemon socket is inaccessible, engine degrades gracefully with no shell interruption.
- Runtime socket and lock directories remain user-private (`0700`) and must not be shared across users.

## 13) API and CLI Surface

### 13.1 Daemon APIs
- `IngestEvent`
- `Suggest(prefix, limit, context)`
- `Search(query, scope, limit)`
- `RecordFeedback(action, suggestion, executed)`
- `DebugStats`

API payload contract (logical schema; wire encoding is protobuf per Section 3.1):
- `IngestEventRequest`:
- `event_type`, `session_id`, `shell`, `ts_ms`, `cwd`, `cmd_raw`, `cmd_truncated`, `exit_code`, `duration_ms`, `ephemeral`
- `SuggestRequest`:
- `session_id`, `cwd`, `repo_key`, `prefix`, `cursor_pos`, `limit`, `include_low_confidence`, `last_cmd_raw`, `last_cmd_norm`, `last_cmd_ts_ms`, `last_event_seq`
- Project-type and alias context resolution:
- daemon resolves `project_types` and alias map from session state keyed by `session_id`; these are not required request payload fields.
- `SuggestResponse`:
- `suggestions[] { text, cmd_norm, source, score, confidence, reasons[], risk }`, `cache_status`, `latency_ms`
- `SuggestResponse` may also include `timing_hint { user_speed_class, suggested_pause_threshold_ms }`
- `SearchRequest`:
- `query`, `scope`, `limit`, `repo_key`, `session_id`, `mode` (`fts|prefix|describe|auto`)
- `SearchResponse`:
- `results[] { cmd_raw, cmd_norm, ts_ms, repo_key, rank_score, tags?, matched_tags? }`, `latency_ms`, `backend`
- `RecordFeedbackRequest`:
- `session_id`, `action`, `suggested_text`, `executed_text`, `prefix`, `latency_ms`

### 13.2 CLI Commands
- `clai suggest [prefix] --limit N --format text|json|fzf --color auto|always|never`
- `clai search [query] --limit N --scope session|repo|global --mode fts|prefix|describe|auto --color auto|always|never`
- `clai suggest-feedback --action accepted|dismissed|edited|never|unblock`
- `clai suggestions doctor`
- `clai daemon start|stop|status|reload`

Note:
- `clai suggest-feedback` is diagnostic/manual tooling.
- Primary feedback path is automatic from shell integrations and implicit matching heuristics.
- `clai search` prefers SQLite FTS5 (`backend=fts5`) and falls back to prefix/LIKE scan (`backend=fallback`) when FTS is unavailable.
- `describe` and `auto` search modes use deterministic tag matching (no LLM in hot path).

### 13.3 Error Model and Response Contract
- All daemon API responses must include one of:
- `ok=true` with payload.
- `ok=false` with structured error `{ code, message, retryable }`.
- Standard error codes:
- `E_INVALID_ARGUMENT`: malformed request fields.
- `E_DAEMON_UNAVAILABLE`: transport/listener unavailable.
- `E_STORAGE_BUSY`: SQLite contention beyond retry budget.
- `E_STORAGE_CORRUPT`: DB corruption detected; daemon auto-recovers by rotating corrupt DB and rebuilding.
- `E_TIMEOUT`: operation exceeded hard timeout.
- `E_UNSUPPORTED_TTY`: output mode requires TTY (for example `--format fzf` when piped).
- `E_INTERNAL`: unexpected internal error.
- CLI behavior:
- `clai suggest` falls back to empty output on daemon failure (non-zero only with `--strict`).
- `clai search` returns user-facing error and non-zero exit on daemon/storage failures.

### 13.4 Input Validation Rules
- All daemon API endpoints must validate inputs before processing.
- Invalid inputs return `E_INVALID_ARGUMENT` with a descriptive message.

Field validation table:

| Field | Valid Range | On Invalid |
|-------|------------|------------|
| `session_id` | non-empty string, max 256 bytes | reject with `E_INVALID_ARGUMENT` |
| `event_type` | known enum value | reject with `E_INVALID_ARGUMENT` |
| `shell` | `bash`, `zsh`, `fish`, or empty | accept empty as unknown; reject other values |
| `ts_ms` | positive integer, not in future by more than 60s | clamp future timestamps to now; reject zero/negative |
| `cwd` | valid UTF-8, max 4096 bytes | truncate to max; replace invalid UTF-8 |
| `cmd_raw` | 0-16384 bytes (per `cmd_raw_max_bytes`) | truncate + set `cmd_truncated=1`; empty is valid (e.g., Enter on empty prompt) |
| `exit_code` | 0-255 | store raw; values >255 stored as-is for platform compat |
| `duration_ms` | 0-86400000 (24 hours) | clamp negative to 0; clamp overflow to max |
| `prefix` | 0-16384 bytes | truncate to `cmd_raw_max_bytes` silently |
| `cursor_pos` | 0-len(prefix) | clamp to len(prefix) |
| `limit` | 1-100 | clamp to range; 0 becomes default (5) |
| `scope` | `session`, `repo`, `global` | reject unknown with `E_INVALID_ARGUMENT` |
| `mode` | `fts`, `prefix`, `describe`, `auto` | reject unknown with `E_INVALID_ARGUMENT` |
| `action` (feedback) | known enum value | reject unknown with `E_INVALID_ARGUMENT` |

Config value validation (on daemon startup):
- All timeout values must be positive integers; reject and refuse to start on negative or zero.
- All weight values must be in `[0.0, 1.0]`; clamp and warn on out-of-range.
- `workflow_min_steps` must be <= `workflow_max_steps`; reject and fall back to defaults on violation.
- `cache_memory_budget_mb` must be >= 1; reject zero or negative.
- `retention_days` must be >= 1; zero means "no time-based pruning" (count-based still applies).
- Invalid config emits structured warning to daemon log and falls back to built-in defaults for the invalid key only.

## 14) Testing Strategy (Extensive)

### 14.1 Unit Tests
- Input validation rules from Section 13.4 (boundary clamping, rejection, empty/null handling).
- Config validation rules (invalid ranges, conflicting min/max, zero budgets).
- Normalization/tokenization edge cases.
- UTF-8 invalid sequence normalization and replacement behavior.
- Event truncation marker behavior and max-size enforcement.
- Slot extraction and filling correctness.
- Slot correlation join/fallback correctness and invalid-tuple rejection.
- Ranking determinism and monotonicity.
- Online learning update clamp/renormalize correctness and low-sample freeze behavior.
- Feedback update math and decay behavior.
- Timeout and non-blocking guarantees in hook sender.
- Burst mode detector thresholds and quiet-window recovery.
- `.clai/tasks.yaml` parser validation and merge precedence.
- Shell version detection and degraded compatibility branch selection.
- Bash trap chaining behavior with and without `bash-preexec`.
- Fish adapter lint checks (`set -l`, `$status`, no process substitution).
- Project-type marker detection precedence, overrides, and cache invalidation.
- Pipeline splitter correctness for quoted operators, subshells, and heredoc boundaries.
- Failure recovery classification and update math.
- Workflow activation state transitions and timeout behavior.
- Alias expansion depth/cycle protection and alias-preferred rendering.
- Persistent dismissal state-machine transitions (`temporary|learned|permanent`).
- Describe-mode tag extraction and synonym matching.

### 14.2 Property and Fuzz Tests
- Fuzz malformed UTF-8 and shell-escaped sequences.
- Fuzz parser/tokenizer with long and adversarial commands.
- Property tests for idempotent normalization.

### 14.3 Integration Tests
- Daemon ingest -> aggregate -> suggest end-to-end.
- Session isolation and repo isolation correctness.
- Cache hit/miss behavior and invalidation correctness.
- Migration tests from empty DB to latest schema.
- Burst-mode ingestion under command storms with bounded durable writes.
- FTS5 index synchronization (`command_event` <-> `command_event_fts`) and fallback path correctness.
- Retention pruning correctness (`90d` and `500k` thresholds).
- Busy lock retry path under synthetic `SQLITE_BUSY` contention.
- Retrieval priority order enforcement across all advanced sources.
- Playbook `after`/`after_failure` triggers and workflow seeding behavior.

### 14.4 Cross-Shell Interactive Tests
- `go-expect` driven tests for `bash`, `zsh`, `fish`:
- Hook install idempotency
- Prompt integrity
- Suggestion acceptance keys
- Session start/end behavior
- Non-interactive shell no-op behavior
- Interactivity detection matrix (TTY and non-TTY combinations)

### 14.5 Docker Matrix
- Distros: alpine, ubuntu, debian.
- Run tests sequentially per container to avoid flake from CPU contention.
- Set deterministic test parallelism for interactive tests.

### 14.6 Performance Tests
- Hook overhead micro-benchmarks.
- Suggestion latency benchmark with warm/cold paths.
- Load test ingest burst (`10k` events) with durability checks.
- Burst mode benchmark verifies queue stability under loop-like traffic.
- Search benchmark verifies FTS query p95 budget under large history corpus.
- Ingestion overhead benchmarks with project-type, pipeline, and failure-recovery features enabled.
- Suggest latency benchmarks with workflow and directory-scoped retrieval active.

### 14.7 Reliability and Chaos Tests
- Kill daemon mid-session; shell remains functional.
- Simulate stale socket, lock contention, DB busy.
- Validate automatic recovery and bounded error logs.
- Validate `SIGPIPE` resilience in daemon and CLI pipe scenarios (`clai suggest | head -1`).

### 14.8 Security Tests
- Socket permission validation.
- Event transport injection and malformed frame handling.
- Incognito persistence guarantees.
- Root/sudo session isolation and non-root daemon enforcement.

### 14.9 Deterministic Replay Validation
- Maintain a replay corpus of sanitized command sessions with expected top-k suggestions per step.
- Replay runner executes with fixed clock and fixed random seed for deterministic comparisons.
- Any change in top-k set, ordering, confidence, or learned weight profile beyond configured thresholds requires explicit review approval.

## 15) Observability and Diagnostics

### 15.1 Metrics
- Suggest latency, cache hit ratio, accept rate, dismiss rate.
- Ingest drop rate and timeout rate.
- DB flush queue depth and flush latency.
- Burst mode entries/dropped event counts and active duration.
- Online learning update count, clamp count, and per-profile sample counts.
- Search backend split (`fts5` vs fallback) and search latency percentiles.
- FTS index size bytes and ratio versus primary DB size.
- Project-type detection cache hit rate and scan latency.
- Pipeline splitter invocation count and segment distribution.
- Failure-recovery hit rate and accepted recovery rate.
- Workflow activation count and next-step acceptance rate.
- Discovery suggestion show/accept/dismiss/ignore rates.

### 15.2 Structured Logging
- Debug-level includes template ids and feature contributions.
- Info-level excludes raw command text.

### 15.3 Doctor Surface
- `clai suggestions doctor` reports:
- Daemon health
- IPC path and permissions
- Migration version
- Cache stats
- Last discovery errors
- FTS availability and last index sync status
- Playbook parse status (`.clai/tasks.yaml`)
- Active advanced features and shell-capability matrix (`zsh|bash|fish` support states)

## 16) Configuration Surface (V2)

Config format and resolution:
- File format is YAML (consistent with existing `internal/config/config.go` which uses `gopkg.in/yaml.v3`).
- Resolution order:
- `$CLAI_CONFIG`
- `$XDG_CONFIG_HOME/clai/config.yaml`
- `$HOME/.config/clai/config.yaml`
- built-in defaults
- Environment overrides:
- environment variables override config file values.
- key mapping rule: `suggestions.enabled` -> `CLAI_SUGGESTIONS_ENABLED`.
- reserved top-level overrides: `CLAI_DEBUG`, `CLAI_LOG_LEVEL`, `CLAI_SOCKET_PATH`.

Core:
- `suggestions.enabled=true`
- `suggestions.max_results=5`
- `suggestions.cache_ttl_ms=30000`
- `suggestions.hard_timeout_ms=150`

Hook/transport:
- `suggestions.hook_connect_timeout_ms=15`
- `suggestions.hook_write_timeout_ms=20`
- `suggestions.socket_path=""` (auto default)
- `suggestions.ingest_sync_wait_ms=5`
- `suggestions.interactive_require_tty=true`
- `suggestions.cmd_raw_max_bytes=16384`
- `suggestions.shim_mode=auto` (`auto|persistent|oneshot`)
- shim mode behavior:
- `auto` (default): attempt persistent helper on session start; on helper startup/connect failure, fall back to oneshot for remainder of session.
- `persistent`: require persistent helper; emit diagnostic and disable suggestions if unavailable.
- `oneshot`: always fork/exec helper per event.

Ranking:
- `suggestions.weights.transition=0.30`
- `suggestions.weights.frequency=0.20`
- `suggestions.weights.success=0.10`
- `suggestions.weights.prefix=0.15`
- `suggestions.weights.affinity=0.10`
- `suggestions.weights.task=0.05`
- `suggestions.weights.feedback=0.15`
- `suggestions.weights.risk_penalty=0.20`

Learning:
- `suggestions.decay_half_life_hours=168`
- `suggestions.feedback_boost_accept=0.10`
- `suggestions.feedback_penalty_dismiss=0.08`
- `suggestions.slot_max_values_per_slot=20`
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
- `suggestions.slot_correlation_min_confidence=0.65`

Backpressure:
- `suggestions.burst_events_threshold=10`
- `suggestions.burst_window_ms=100`
- `suggestions.burst_quiet_ms=500`
- `suggestions.ingest_queue_max_events=8192`
- `suggestions.ingest_queue_max_bytes=8388608`

Task discovery:
- `suggestions.task_playbook_enabled=true`
- `suggestions.task_playbook_path=.clai/tasks.yaml`
- `suggestions.task_playbook_boost=0.20`

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
- `suggestions.weights.project_type_affinity=0.08`

Pipeline:
- `suggestions.pipeline_awareness_enabled=true`
- `suggestions.pipeline_max_segments=8`
- `suggestions.pipeline_pattern_min_count=2`

Failure recovery:
- `suggestions.failure_recovery_enabled=true`
- `suggestions.failure_recovery_bootstrap_enabled=true`
- `suggestions.failure_recovery_min_count=2`
- `suggestions.weights.failure_recovery=0.12`

Workflow:
- `suggestions.workflow_detection_enabled=true`
- `suggestions.workflow_min_steps=3`
- `suggestions.workflow_max_steps=6`
- `suggestions.workflow_min_occurrences=3`
- `suggestions.workflow_max_gap=2`
- `suggestions.workflow_activation_timeout_ms=600000`
- `suggestions.workflow_boost=0.25`
- `suggestions.workflow_mine_interval_ms=600000`

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

Extended playbook:
- `suggestions.task_playbook_extended_enabled=true`
- `suggestions.task_playbook_after_boost=0.30`
- `suggestions.task_playbook_workflow_seed_count=100`

Discovery:
- `suggestions.discovery_enabled=true`
- `suggestions.discovery_cooldown_hours=24`
- `suggestions.discovery_max_confidence_threshold=0.3`
- `suggestions.discovery_source_project_type=true`
- `suggestions.discovery_source_playbook=true`
- `suggestions.discovery_source_tool_common=true`

Storage:
- `suggestions.retention_days=90`
- `suggestions.retention_max_events=500000`
- `suggestions.maintenance_interval_ms=300000`
- `suggestions.maintenance_vacuum_threshold_mb=100`
- `suggestions.sqlite_busy_timeout_ms=50`

Cache:
- `suggestions.cache_memory_budget_mb=50`

Privacy:
- `suggestions.incognito_mode=ephemeral` (`off|ephemeral|no_send`)
- `suggestions.redact_sensitive_tokens=true`

### 16.1 Config Value Validation

On daemon startup, all config values are validated before listeners start. Validation follows these rules:

| Category | Keys | Valid Range | On Invalid |
|----------|------|-------------|------------|
| Timeouts (ms) | `hard_timeout_ms`, `hook_connect_timeout_ms`, `hook_write_timeout_ms`, `ingest_sync_wait_ms`, `cache_ttl_ms` | >= 1 | Fall back to default, warn |
| Weights | `weights.*` (all) | [0.0, 1.0] | Clamp to range, warn |
| Counts | `max_results`, `ingest_queue_max_events`, `burst_events_threshold` | >= 1 | Fall back to default, warn |
| Byte sizes | `cmd_raw_max_bytes`, `ingest_queue_max_bytes`, `cache_memory_budget_mb` | >= 1 | Fall back to default, warn |
| Retention | `retention_days` | >= 1 (0 = disable time pruning) | Fall back to default, warn |
| Retention | `retention_max_events` | >= 1000 | Clamp to 1000 min, warn |
| Learning | `online_learning_eta` | (0.0, 1.0] | Fall back to default, warn |
| Learning | `online_learning_min_samples` | >= 1 | Fall back to default, warn |
| Ranges | `workflow_min_steps` <= `workflow_max_steps` | min <= max | Fall back to defaults for both, warn |
| Ranges | `pipeline_max_segments` | [2, 32] | Clamp to range, warn |
| Ranges | `directory_scope_max_depth` | [1, 10] | Clamp to range, warn |
| Enums | `incognito_mode` | `off`, `ephemeral`, `no_send` | Fall back to default, warn |
| Enums | `shim_mode` | `auto`, `persistent`, `oneshot` | Fall back to `auto`, warn |
| Enums | `search_fts_tokenizer` | `trigram`, `unicode61` | Fall back to `trigram`, warn |

- Validation never prevents daemon startup; all failures fall back to built-in defaults with structured warnings.
- All validation warnings are emitted to daemon log at `WARN` level and surfaced by `clai suggestions doctor`.
- Config reload via `SIGHUP` re-validates; invalid reloaded values keep the previous valid value.

### 16.2 Feature Flag Dependencies
- Feature flags are independent by default:
- disabling a feature disables its ingestion writes, retrieval source, and ranking contribution only.
- `task_playbook_extended.after_failure` is independent of `failure_recovery_enabled`; it depends only on prior command exit status.
- `alias_resolution_enabled` affects template identity. If disabled after prior enabled history, template convergence may diverge until re-enabled; this is an expected limitation.
- workflow mining may run independently of pipeline awareness; when pipeline awareness is disabled, workflows are mined from command-level template sequences only.

## 17) Quality Gates

Build-time gates:
- Unit + integration + interactive shell suites must pass.
- Docker matrix (`alpine`, `ubuntu`, `debian`) must pass with deterministic test parallelism.
- Fuzz suite must run for minimum configured time budget.

Performance gates:
- Hook overhead regression over baseline must be less than 15%.
- Suggest P95 regression must be less than 10% between adjacent commits.
- Cold-start daemon readiness under 500ms on CI runners.

Behavioral gates:
- Session isolation invariants hold.
- Ephemeral mode persistence invariant holds (zero persistent writes from ephemeral events).
- Risk tagging invariant holds for destructive command patterns.
- Correlated slot validity checks pass for templates with dependency sets.
- Online learning guardrails hold (weights remain within configured ranges).
- Hook behavior remains correct with persistent and fallback shim modes.
- Advanced retrieval order remains deterministic and stable across identical state.

## 18) Acceptance Checklist

- Suggestion API meets latency budgets.
- Cross-shell tests pass on local and Docker matrix.
- Non-interactive shells remain unaffected.
- Incognito modes validated with persistence assertions.
- Hook path remains non-blocking under daemon failure.
- Deterministic ranking and stable top-k behavior across repeated runs.
- `.clai/tasks.yaml` discovery, reload, and boost behavior validated.
- Deep-history `clai search` behavior validated with FTS and fallback backend.
- CLI color and format contracts validated for TTY and non-TTY modes.
- Advanced features (project-type, pipeline, recovery, workflow, alias, dismissal, discovery, describe-search) validated with deterministic replay and integration suites.

## 19) Correctness Invariants

The following invariants are mandatory and must be continuously asserted in tests:

- `I1 Session Isolation`:
- Suggestions for `session_id=A` must not include session-scoped transitions derived exclusively from `session_id=B`.
- `I2 Ephemeral Persistence`:
- Events with `ephemeral=1` must not produce persistent writes to `command_event`, `command_stat`, `transition_stat`, or `slot_stat`.
- `I3 Deterministic Ranking`:
- With identical input state (DB snapshot + in-memory cache + clock seed), returned top-k must be byte-for-byte identical.
- `I4 Bounded Hook Latency`:
- Hook processing time must remain below configured timeout budget and never block shell prompt rendering.
- `I5 Transactional Aggregate Consistency`:
- After successful commit of a non-ephemeral `command_end`, dependent aggregate tables reflect the same event version atomically.
- `I6 Risk Label Integrity`:
- Commands matching destructive patterns must always carry risk metadata and applicable penalty before final ranking.
- `I7 Cache Coherency`:
- Any new `command_end` for a session invalidates stale cache entries for that session before the next suggest response.
- `I8 Crash Safety`:
- Daemon crash during ingestion must not leave partially applied aggregate updates visible after restart.
- `I9 Correlated Slot Validity`:
- For templates with configured slot dependency sets, emitted multi-slot suggestions must match an observed tuple or exceed configured correlation confidence.
- `I10 Learning Guardrails`:
- Adaptive updates must never push any weight outside configured min/max bounds, and must preserve deterministic ordering for a fixed profile snapshot.
- `I11 Event Size Boundedness`:
- Persisted and indexed `cmd_raw` values must never exceed configured max byte limit, and truncated events must be marked.
- `I12 Fail-Open Shell Safety`:
- Loss of daemon, helper, pipe, or socket must not block prompt rendering or command execution.
- `I13 Retrieval Priority Determinism`:
- Source ordering and per-source caps must be applied deterministically for identical state.
- `I14 Alias Canonicalization`:
- Equivalent alias and expanded forms must map to one canonical template identity.
- `I15 Persistent Dismissal Safety`:
- Permanent dismissals must be reversible and scoped correctly; they must not suppress unrelated templates.


## 20) Appendix And Traceability

The enhancement proposals are fully preserved in `/Users/runger/.claude-worktrees/clai/happy-hypatia/specs/tech_suggestions_ext_v1_appendix.md`.

Normative rule:
- Sections `0` through `19` in this document are the canonical specification.
- The appendix is detailed supporting material and rationale; in case of conflict, this document takes precedence.
- Exception: where sections in this document explicitly mark appendix content as a normative supplement (for example additive SQL sketches), that referenced appendix content is part of the implementation contract.
