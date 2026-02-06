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
- Shell adapter overhead under 8ms median and under 15ms p95 per command completion when invoking a helper process.
- Optional optimization target (persistent helper channel): under 3ms median.

## 1) High-Level Architecture

### 1.1 Core Components
- Shell Adapters (`bash`, `zsh`, `fish`): collect execution context and invoke `clai-shim` ingest mode.
- `clai-shim` (ingest mode): validates fields, lossily normalizes UTF-8, writes NDJSON events fire-and-forget.
- `clai-suggestd` daemon:
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
2. Post-command hook emits `command_end` to `clai-shim`.
3. `clai-shim` sends NDJSON to daemon over local IPC.
4. Daemon updates in-memory aggregates and asynchronously flushes batched state to SQLite.
5. On prompt update or typed prefix, shell asks `clai suggest`.
6. Daemon returns top-k suggestions from session cache or computes on demand.
7. On suggestion accept/dismiss, shell sends feedback event to improve future ranking.

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
- `ts_unix_ms`
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

### 2.5 Safety Rules
- Interactive shell check required before installing hook behavior.
- Never pass command text via CLI args.
- Never emit shell-generated JSON.
- Hook must suppress non-critical stderr noise.
- Hook must fail open (shell continues normally on hook failure).

### 2.6 Session ID Assignment
- `session_id` is required for all command and suggestion events.
- Preferred strategy: daemon-assigned at shell startup:
- Shell invokes `clai-shim session-start`; shim gets/creates session ID from daemon and exports it for the shell process.
- Fallback strategy (daemon unavailable at startup):
- Shell computes local ID from `hostname + pid + shell_start_time + random_seed`, hashed to stable string.
- Session ID must be stable for shell lifetime and unique across concurrent shells on same host.

## 3) IPC and Daemon Resilience

### 3.1 Transport
- Unix domain socket on macOS/Linux.
- Transport abstraction with named-pipe backend for Windows.

Windows named-pipe contract:
- Pipe path format: `\\\\.\\pipe\\clai-suggestd-<user-scope>`.
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
- Acquire lock.
- Run migrations.
- Clean stale socket.
- Start listeners.

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

## 4) Storage Model (V2 Schema)

No migration bridge to V1 is required. V2 initializes and owns its schema.

### 4.1 Tables
- `session`
- `command_event`
- `command_template`
- `transition_stat`
- `command_stat`
- `slot_stat`
- `slot_correlation`
- `task_candidate`
- `suggestion_cache`
- `suggestion_feedback`
- `rank_weight_profile`
- `command_event_fts` (virtual table)
- `schema_migrations`

### 4.2 Schema Sketch
```sql
CREATE TABLE session (
  id TEXT PRIMARY KEY,
  shell TEXT NOT NULL,
  started_at_ms INTEGER NOT NULL,
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
  template_id TEXT,
  exit_code INTEGER,
  duration_ms INTEGER,
  ephemeral INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE command_template (
  template_id TEXT PRIMARY KEY,
  cmd_norm TEXT NOT NULL,
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
  w_risk_penalty REAL NOT NULL,
  sample_count INTEGER NOT NULL,
  learning_rate REAL NOT NULL
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

### 4.3 Storage Policies
- SQLite WAL mode, one writer goroutine, batched commits every 25-50ms or 100 events.
- Ephemeral events are never persisted to long-lived aggregates.
- Optional retention policy by age and max rows.

WAL and checkpoint policy:
- Set `PRAGMA wal_autocheckpoint=1000` (tunable).
- Background maintenance task performs:
- periodic `PRAGMA wal_checkpoint(PASSIVE)` during steady state
- `PRAGMA wal_checkpoint(TRUNCATE)` on low-activity windows
- optional `VACUUM` on size/fragmentation threshold and only outside hot path.

### 4.4 Write-Path Transaction Semantics
- All writes for a single ingested event are applied in one transaction (`BEGIN IMMEDIATE ... COMMIT`) to preserve aggregate consistency.
- Transaction order for non-ephemeral `command_end`:
- Insert row in `command_event`.
- Upsert `command_template`.
- Update `command_stat` (frequency + success/failure counters).
- Update `transition_stat` if previous template is known for session.
- Update `slot_stat` values extracted from template/args alignment.
- Update `slot_correlation` for configured slot tuples (example: `<namespace>|<pod>`).
- Update in-memory cache index and invalidation markers.
- If online ranking updates are enabled, enqueue async `rank_weight_profile` update work item after transaction commit.
- For `ephemeral=1`, only in-memory session-scoped structures are updated; no SQLite write occurs.
- If transaction fails, daemon records error metric, abandons partial event effects, and keeps process alive.

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

### 5.3 Normalization Rules
- Lowercase command/tool token.
- Collapse whitespace.
- Replace dynamic values with slots:
- `<path>`
- `<num>`
- `<sha>`
- `<url>`
- `<msg>`
- Optionally domain slots (`<branch>`, `<namespace>`, `<service>`).

### 5.4 Template Identity
- `template_id = sha256(cmd_norm)`.
- Store `cmd_norm` and slot count in `command_template`.

### 5.5 Slot Dependency Registry
- Normalizer maintains optional per-template dependency sets for semantically coupled slots (for example `<namespace>` + `<pod>`, `<cluster>` + `<context>`).
- Dependency sets are keyed by `slot_key` (pipe-delimited slot indexes such as `1|3`) and used by `slot_correlation`.

## 6) Candidate Generation Pipeline

### 6.1 Inputs
- Session context: last command/template, cwd, repo key, branch.
- Typed prefix (possibly empty).
- Incognito state.
- Recent feedback for immediate personalization.

### 6.2 Retrieval Sources
- Session transitions.
- Repo transitions.
- Global transitions.
- Session/repo/global frequency priors.
- Task candidates from repo discovery.
- Typo correction candidates after failures.

### 6.3 Retrieval Budget
- Retrieve up to 200 candidates total in ranked source order.
- Hard cap per source to avoid source domination.

### 6.4 Prefix Filtering Modes
- Empty prefix: pure next-step mode.
- Non-empty prefix: constrained mode (prefix and fuzzy tolerance).

## 7) Ranking and Post-Processing

### 7.1 Ranking Model (Deterministic Weighted Score)
`score = w1*transition + w2*frequency + w3*success + w4*prefix + w5*affinity + w6*task + w7*feedback - w8*risk_penalty`

Default initial weights:
- `w1 transition = 0.30`
- `w2 frequency = 0.20`
- `w3 success = 0.10`
- `w4 prefix = 0.15`
- `w5 affinity = 0.10`
- `w6 task = 0.05`
- `w7 feedback = 0.15`
- `w8 risk_penalty = 0.20`

Scoring notes:
- Each feature is normalized into `[0, 1]` before weighting.
- Transition and frequency values are log-scaled before normalization.
- Risk penalty is applied post-aggregation and can force candidate suppression.
- Per request, weight vector is resolved from `rank_weight_profile` (`session` -> `repo` -> `global_default`) and snapshotted for deterministic ordering within that request.

### 7.2 Feature Definitions
- `transition`: decayed edge weight from previous template.
- `frequency`: decayed command/template score.
- `success`: success ratio for candidate template.
- `prefix`: exact + fuzzy prefix match quality.
- `affinity`: cwd/repo/branch proximity.
- `task`: boost for discovered project tasks.
- `feedback`: recent accept boost and dismiss penalty.
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
- If feedback volume is below `suggestions.online_learning_min_samples`, engine uses static defaults only.
- All updates are async and versioned; suggest path reads last committed profile snapshot only.

## 8) Typo Recovery

### 8.1 Trigger
- Primary trigger: previous command exit code `127`.
- Optional trigger: explicit parser hints for common "unknown command" patterns.

### 8.2 Matching
- Damerau-Levenshtein on first token and command stem.
- Candidate pool constrained to frequent commands and current scope.

### 8.3 Ranking Behavior
- Typo-fixed candidate gets temporary high boost for immediate next prompt.
- Boost decays quickly if not accepted.

## 9) Learning Loop and Feedback

### 9.1 Feedback Events
- `accepted`
- `dismissed`
- `edited_then_run`
- `ignored_timeout`

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

### 9.3 Drift Control
- Decay all feedback effects over time.
- Clamp max per-template boost to avoid lock-in.

## 10) Caching and Latency Strategy

### 10.1 Multi-layer Cache
- L1: per-session hot cache in memory keyed by last event id + prefix hash.
- L2: per-repo cache for cold session fallback.
- L3: SQLite aggregate fallback.

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

## 13) API and CLI Surface

### 13.1 Daemon APIs
- `IngestEvent`
- `Suggest(prefix, limit, context)`
- `Search(query, scope, limit)`
- `RecordFeedback(action, suggestion, executed)`
- `DebugStats`

API payload contract (JSON over local RPC transport wrapper):
- `IngestEventRequest`:
- `event_type`, `session_id`, `shell`, `ts_ms`, `cwd`, `cmd_raw`, `exit_code`, `duration_ms`, `ephemeral`
- `SuggestRequest`:
- `session_id`, `cwd`, `repo_key`, `prefix`, `cursor_pos`, `limit`, `include_low_confidence`, `last_cmd_raw`, `last_cmd_norm`, `last_cmd_ts_ms`, `last_event_seq`
- `SuggestResponse`:
- `suggestions[] { text, cmd_norm, source, score, confidence, reasons[], risk }`, `cache_status`, `latency_ms`
- `SearchRequest`:
- `query`, `scope`, `limit`, `repo_key`, `session_id`, `mode`
- `SearchResponse`:
- `results[] { cmd_raw, cmd_norm, ts_ms, repo_key, rank_score }`, `latency_ms`, `backend`
- `RecordFeedbackRequest`:
- `session_id`, `action`, `suggested_text`, `executed_text`, `prefix`, `latency_ms`

### 13.2 CLI Commands
- `clai suggest [prefix] --limit N --format text|json|fzf`
- `clai search [query] --limit N --scope session|repo|global --mode fts|prefix`
- `clai suggest-feedback --action accepted|dismissed|edited`
- `clai suggestions doctor`

Note:
- `clai suggest-feedback` is diagnostic/manual tooling.
- Primary feedback path is automatic from shell integrations and implicit matching heuristics.
- `clai search` prefers SQLite FTS5 (`backend=fts5`) and falls back to prefix/LIKE scan (`backend=fallback`) when FTS is unavailable.

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
- `E_INTERNAL`: unexpected internal error.
- CLI behavior:
- `clai suggest` falls back to empty output on daemon failure (non-zero only with `--strict`).
- `clai search` returns user-facing error and non-zero exit on daemon/storage failures.

## 14) Testing Strategy (Extensive)

### 14.1 Unit Tests
- Normalization/tokenization edge cases.
- Slot extraction and filling correctness.
- Slot correlation join/fallback correctness and invalid-tuple rejection.
- Ranking determinism and monotonicity.
- Online learning update clamp/renormalize correctness and low-sample freeze behavior.
- Feedback update math and decay behavior.
- Timeout and non-blocking guarantees in hook sender.
- Burst mode detector thresholds and quiet-window recovery.
- `.clai/tasks.yaml` parser validation and merge precedence.

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

### 14.4 Cross-Shell Interactive Tests
- `go-expect` driven tests for `bash`, `zsh`, `fish`:
- Hook install idempotency
- Prompt integrity
- Suggestion acceptance keys
- Session start/end behavior
- Non-interactive shell no-op behavior

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

### 14.7 Reliability and Chaos Tests
- Kill daemon mid-session; shell remains functional.
- Simulate stale socket, lock contention, DB busy.
- Validate automatic recovery and bounded error logs.

### 14.8 Security Tests
- Socket permission validation.
- Event transport injection and malformed frame handling.
- Incognito persistence guarantees.

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

## 16) Configuration Surface (V2)

All values can be set in config and overridden with environment variables for testing.

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

Task discovery:
- `suggestions.task_playbook_enabled=true`
- `suggestions.task_playbook_path=.clai/tasks.yaml`
- `suggestions.task_playbook_boost=0.20`

Search:
- `suggestions.search_fts_enabled=true`
- `suggestions.search_fallback_scan_limit=2000`

Privacy:
- `suggestions.incognito_mode=ephemeral` (`off|ephemeral|no_send`)
- `suggestions.redact_sensitive_tokens=true`

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

## 18) Acceptance Checklist

- Suggestion API meets latency budgets.
- Cross-shell tests pass on local and Docker matrix.
- Non-interactive shells remain unaffected.
- Incognito modes validated with persistence assertions.
- Hook path remains non-blocking under daemon failure.
- Deterministic ranking and stable top-k behavior across repeated runs.
- `.clai/tasks.yaml` discovery, reload, and boost behavior validated.
- Deep-history `clai search` behavior validated with FTS and fallback backend.

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
