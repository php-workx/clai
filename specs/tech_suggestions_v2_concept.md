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
- Shell hook overhead under 2ms median per command completion.

## 1) High-Level Architecture

### 1.1 Core Components
- Shell Adapters (`bash`, `zsh`, `fish`): collect execution context and invoke `clai-hook`.
- `clai-hook`: validates fields, lossily normalizes UTF-8, writes NDJSON events fire-and-forget.
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
2. Post-command hook emits `command_end` to `clai-hook`.
3. `clai-hook` sends NDJSON to daemon over local IPC.
4. Daemon updates in-memory aggregates and asynchronously flushes batched state to SQLite.
5. On prompt update or typed prefix, shell asks `clai suggest`.
6. Daemon returns top-k suggestions from session cache or computes on demand.
7. On suggestion accept/dismiss, shell sends feedback event to improve future ranking.

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

### 2.5 Safety Rules
- Interactive shell check required before installing hook behavior.
- Never pass command text via CLI args.
- Never emit shell-generated JSON.
- Hook must suppress non-critical stderr noise.
- Hook must fail open (shell continues normally on hook failure).

## 3) IPC and Daemon Resilience

### 3.1 Transport
- Unix domain socket on macOS/Linux.
- Transport abstraction with future named-pipe backend for Windows.

### 3.2 Timeout Policy
- Connect timeout default 15ms (configurable 10-25ms).
- Write timeout default 20ms.
- No response wait in hook path.

### 3.3 Socket Location
- Preferred: `$XDG_RUNTIME_DIR/clai/suggestd.sock`.
- Fallback: `$TMPDIR/clai-$UID/suggestd.sock`.
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
- When queue is full, daemon applies drop-oldest policy for non-critical telemetry events and increments `ingest_drop_count`.
- `command_end` and `session_start/session_end` are high-priority and must be retained preferentially over optional telemetry.
- Hook path remains fail-open: if connect or write exceeds timeout budget, event is dropped without blocking shell.
- Daemon must never block suggestion serving on ingestion flush backlog.

## 4) Storage Model (V2 Schema)

No migration bridge to V1 is required. V2 initializes and owns its schema.

### 4.1 Tables
- `session`
- `command_event`
- `command_template`
- `transition_stat`
- `command_stat`
- `slot_stat`
- `task_candidate`
- `suggestion_cache`
- `suggestion_feedback`
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

CREATE TABLE task_candidate (
  repo_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  command_text TEXT NOT NULL,
  description TEXT,
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

CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_ms INTEGER NOT NULL
);
```

### 4.3 Storage Policies
- SQLite WAL mode, one writer goroutine, batched commits every 25-50ms or 100 events.
- Ephemeral events are never persisted to long-lived aggregates.
- Optional retention policy by age and max rows.

### 4.4 Write-Path Transaction Semantics
- All writes for a single ingested event are applied in one transaction (`BEGIN IMMEDIATE ... COMMIT`) to preserve aggregate consistency.
- Transaction order for non-ephemeral `command_end`:
- Insert row in `command_event`.
- Upsert `command_template`.
- Update `command_stat` (frequency + success/failure counters).
- Update `transition_stat` if previous template is known for session.
- Update `slot_stat` values extracted from template/args alignment.
- Update in-memory cache index and invalidation markers.
- For `ephemeral=1`, only in-memory session-scoped structures are updated; no SQLite write occurs.
- If transaction fails, daemon records error metric, abandons partial event effects, and keeps process alive.

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

### 9.2 Update Rules
- Accepted suggestions increase source-specific and template-specific priors.
- Dismissed suggestions apply short-term suppression.
- Edited-then-run updates slot statistics and normalizer correction maps.

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

### 11.2 Discovery Runtime Rules
- Run with timeout and output cap.
- Sandboxed environment (no inherited secrets by default).
- Errors are non-fatal and observable through diagnostics.

### 11.3 Data Contract
- Each task candidate includes `kind`, `name`, `command_text`, optional description.

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
- `session_id`, `cwd`, `repo_key`, `prefix`, `cursor_pos`, `limit`, `include_low_confidence`
- `SuggestResponse`:
- `suggestions[] { text, cmd_norm, source, score, confidence, reasons[], risk }`, `cache_status`, `latency_ms`
- `RecordFeedbackRequest`:
- `session_id`, `action`, `suggested_text`, `executed_text`, `prefix`, `latency_ms`

### 13.2 CLI Commands
- `clai suggest [prefix] --limit N --format text|json|fzf`
- `clai search [query] --limit N --scope session|repo|global`
- `clai suggest-feedback --action accepted|dismissed|edited`
- `clai suggestions doctor`

### 13.3 Error Model and Response Contract
- All daemon API responses must include one of:
- `ok=true` with payload.
- `ok=false` with structured error `{ code, message, retryable }`.
- Standard error codes:
- `E_INVALID_ARGUMENT`: malformed request fields.
- `E_DAEMON_UNAVAILABLE`: transport/listener unavailable.
- `E_STORAGE_BUSY`: SQLite contention beyond retry budget.
- `E_STORAGE_CORRUPT`: DB corruption detected; requires operator action.
- `E_TIMEOUT`: operation exceeded hard timeout.
- `E_INTERNAL`: unexpected internal error.
- CLI behavior:
- `clai suggest` falls back to empty output on daemon failure (non-zero only with `--strict`).
- `clai search` returns user-facing error and non-zero exit on daemon/storage failures.

## 14) Testing Strategy (Extensive)

### 14.1 Unit Tests
- Normalization/tokenization edge cases.
- Slot extraction and filling correctness.
- Ranking determinism and monotonicity.
- Feedback update math and decay behavior.
- Timeout and non-blocking guarantees in hook sender.

### 14.2 Property and Fuzz Tests
- Fuzz malformed UTF-8 and shell-escaped sequences.
- Fuzz parser/tokenizer with long and adversarial commands.
- Property tests for idempotent normalization.

### 14.3 Integration Tests
- Daemon ingest -> aggregate -> suggest end-to-end.
- Session isolation and repo isolation correctness.
- Cache hit/miss behavior and invalidation correctness.
- Migration tests from empty DB to latest schema.

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
- Any change in top-k set, ordering, or confidence beyond configured thresholds requires explicit review approval.

## 15) Observability and Diagnostics

### 15.1 Metrics
- Suggest latency, cache hit ratio, accept rate, dismiss rate.
- Ingest drop rate and timeout rate.
- DB flush queue depth and flush latency.

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

## 18) Acceptance Checklist

- Suggestion API meets latency budgets.
- Cross-shell tests pass on local and Docker matrix.
- Non-interactive shells remain unaffected.
- Incognito modes validated with persistence assertions.
- Hook path remains non-blocking under daemon failure.
- Deterministic ranking and stable top-k behavior across repeated runs.

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
