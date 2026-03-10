// Package db provides SQLite-based storage for the suggestions engine.
// It implements the V2 schema defined in specs/tech_suggestions_ext_v1.md Section 4.
package db

// Schema version constants.
//
// Version history:
//   - V1: Original schema (suggestions.db) - 7 tables
//   - V2: Extended schema (suggestions_v2.db) - 23 tables, separate DB file
//   - V3: Unified schema - adds tables from state.db (ai_cache, pty, ci_workflow, etc.)
const (
	// V1SchemaVersion is the schema version for V1 database files (suggestions.db).
	V1SchemaVersion = 1

	// SchemaVersion is the current supported schema version (V3).
	// The daemon will refuse to run if the DB schema version exceeds this.
	SchemaVersion = 3
)

// schemaV1 creates the initial V1 schema for the suggestions engine.
// This schema is retained for reference and for V1 database files.
// V2 uses a separate database file and does not migrate from V1.
const schemaV1 = `
-- Sessions table
CREATE TABLE IF NOT EXISTS session (
  id            TEXT PRIMARY KEY,
  created_at    INTEGER NOT NULL,
  shell         TEXT NOT NULL,
  host          TEXT,
  user          TEXT
);

-- Command events table (core history)
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

-- Indexes for command_event per spec Section 4.2
CREATE INDEX IF NOT EXISTS idx_event_ts ON command_event(ts);
CREATE INDEX IF NOT EXISTS idx_event_repo_ts ON command_event(repo_key, ts);
CREATE INDEX IF NOT EXISTS idx_event_cwd_ts ON command_event(cwd, ts);
CREATE INDEX IF NOT EXISTS idx_event_session_ts ON command_event(session_id, ts);
CREATE INDEX IF NOT EXISTS idx_event_norm_repo ON command_event(cmd_norm, repo_key);

-- Transition table (Markov bigrams)
CREATE TABLE IF NOT EXISTS transition (
  scope         TEXT NOT NULL,            -- 'global' or repo_key
  prev_norm     TEXT NOT NULL,
  next_norm     TEXT NOT NULL,
  count         INTEGER NOT NULL,
  last_ts       INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_norm, next_norm)
);

CREATE INDEX IF NOT EXISTS idx_transition_prev ON transition(scope, prev_norm);

-- Command score table (decayed frequency)
CREATE TABLE IF NOT EXISTS command_score (
  scope         TEXT NOT NULL,            -- 'global' or repo_key
  cmd_norm      TEXT NOT NULL,
  score         REAL NOT NULL,
  last_ts       INTEGER NOT NULL,
  PRIMARY KEY(scope, cmd_norm)
);

CREATE INDEX IF NOT EXISTS idx_command_score_scope ON command_score(scope, score DESC);

-- Slot value table (semantic slot filling)
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

-- Project task table (discovered tasks from package.json, Makefile, etc.)
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

-- Schema migrations tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_ts INTEGER NOT NULL
);
`

// schemaV2 creates the complete V2 schema for the suggestions engine.
// V2 uses a separate database file (suggestions_v2.db) and starts fresh.
// All 23 tables from spec Section 4.1 are created here.
//
// Tables:
//  1. session                  - Shell sessions
//  2. command_event            - Command history events
//  3. command_template         - Normalized command templates
//  4. transition_stat          - Markov bigram transitions
//  5. command_stat             - Command frequency/success stats
//  6. slot_stat                - Slot value statistics
//  7. slot_correlation         - Multi-slot correlations
//  8. project_type_stat        - Project-type command stats
//  9. project_type_transition  - Project-type transitions
//  10. pipeline_event          - Pipeline segment events
//  11. pipeline_transition     - Pipeline segment transitions
//  12. pipeline_pattern        - Full pipeline patterns
//  13. failure_recovery        - Error recovery patterns
//  14. workflow_pattern         - Multi-step workflow patterns
//  15. workflow_step            - Individual workflow steps
//  16. task_candidate           - Discovered project tasks
//  17. suggestion_cache         - Suggestion result cache
//  18. suggestion_feedback      - User feedback on suggestions
//  19. session_alias            - Shell alias snapshots
//  20. dismissal_pattern        - Persistent dismissal learning
//  21. rank_weight_profile      - Adaptive ranking weights
//  22. command_event_fts        - FTS5 virtual table for search
//  23. schema_migrations        - Migration version tracking
const schemaV2 = `
-- 1. Sessions table
CREATE TABLE IF NOT EXISTS session (
  id              TEXT PRIMARY KEY,
  shell           TEXT NOT NULL,
  started_at_ms   INTEGER NOT NULL,
  project_types   TEXT,
  host            TEXT,
  user_name       TEXT
);

-- 2. Command events table (core history)
CREATE TABLE IF NOT EXISTS command_event (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id      TEXT NOT NULL,
  ts_ms           INTEGER NOT NULL,
  cwd             TEXT NOT NULL,
  repo_key        TEXT,
  branch          TEXT,
  cmd_raw         TEXT NOT NULL,
  cmd_norm        TEXT NOT NULL,
  cmd_truncated   INTEGER NOT NULL DEFAULT 0,
  template_id     TEXT,
  exit_code       INTEGER,
  duration_ms     INTEGER,
  ephemeral       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_event_ts ON command_event(ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_repo_ts ON command_event(repo_key, ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_cwd_ts ON command_event(cwd, ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_session_ts ON command_event(session_id, ts_ms);
CREATE INDEX IF NOT EXISTS idx_event_norm_repo ON command_event(cmd_norm, repo_key);
CREATE INDEX IF NOT EXISTS idx_event_template ON command_event(template_id);

-- 3. Command template table
CREATE TABLE IF NOT EXISTS command_template (
  template_id     TEXT PRIMARY KEY,
  cmd_norm        TEXT NOT NULL,
  tags            TEXT,
  slot_count      INTEGER NOT NULL,
  first_seen_ms   INTEGER NOT NULL,
  last_seen_ms    INTEGER NOT NULL
);

-- 4. Transition stat table (Markov bigrams by template)
CREATE TABLE IF NOT EXISTS transition_stat (
  scope             TEXT NOT NULL,
  prev_template_id  TEXT NOT NULL,
  next_template_id  TEXT NOT NULL,
  weight            REAL NOT NULL,
  count             INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_template_id, next_template_id)
);

CREATE INDEX IF NOT EXISTS idx_transition_stat_prev ON transition_stat(scope, prev_template_id);

-- 5. Command stat table (frequency/success by template)
CREATE TABLE IF NOT EXISTS command_stat (
  scope           TEXT NOT NULL,
  template_id     TEXT NOT NULL,
  score           REAL NOT NULL,
  success_count   INTEGER NOT NULL,
  failure_count   INTEGER NOT NULL,
  last_seen_ms    INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id)
);

CREATE INDEX IF NOT EXISTS idx_command_stat_scope ON command_stat(scope, score DESC);

-- 6. Slot stat table (slot value statistics)
CREATE TABLE IF NOT EXISTS slot_stat (
  scope           TEXT NOT NULL,
  template_id     TEXT NOT NULL,
  slot_index      INTEGER NOT NULL,
  value           TEXT NOT NULL,
  weight          REAL NOT NULL,
  count           INTEGER NOT NULL,
  last_seen_ms    INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id, slot_index, value)
);

CREATE INDEX IF NOT EXISTS idx_slot_stat_lookup
  ON slot_stat(scope, template_id, slot_index, weight DESC);

-- 7. Slot correlation table (multi-slot correlations)
CREATE TABLE IF NOT EXISTS slot_correlation (
  scope             TEXT NOT NULL,
  template_id       TEXT NOT NULL,
  slot_key          TEXT NOT NULL,
  tuple_hash        TEXT NOT NULL,
  tuple_value_json  TEXT NOT NULL,
  weight            REAL NOT NULL,
  count             INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  PRIMARY KEY(scope, template_id, slot_key, tuple_hash)
);

-- 8. Project type stat table (project-type command stats)
CREATE TABLE IF NOT EXISTS project_type_stat (
  project_type    TEXT NOT NULL,
  template_id     TEXT NOT NULL,
  score           REAL NOT NULL,
  count           INTEGER NOT NULL,
  last_seen_ms    INTEGER NOT NULL,
  PRIMARY KEY(project_type, template_id)
);

-- 9. Project type transition table
CREATE TABLE IF NOT EXISTS project_type_transition (
  project_type      TEXT NOT NULL,
  prev_template_id  TEXT NOT NULL,
  next_template_id  TEXT NOT NULL,
  weight            REAL NOT NULL,
  count             INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  PRIMARY KEY(project_type, prev_template_id, next_template_id)
);

-- 10. Pipeline event table (pipeline segment events)
CREATE TABLE IF NOT EXISTS pipeline_event (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  command_event_id  INTEGER NOT NULL REFERENCES command_event(id),
  position          INTEGER NOT NULL,
  operator          TEXT,
  cmd_raw           TEXT NOT NULL,
  cmd_norm          TEXT NOT NULL,
  template_id       TEXT NOT NULL,
  UNIQUE(command_event_id, position)
);

-- 11. Pipeline transition table
CREATE TABLE IF NOT EXISTS pipeline_transition (
  scope             TEXT NOT NULL,
  prev_template_id  TEXT NOT NULL,
  next_template_id  TEXT NOT NULL,
  operator          TEXT NOT NULL,
  weight            REAL NOT NULL,
  count             INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_template_id, next_template_id, operator)
);

-- 12. Pipeline pattern table
CREATE TABLE IF NOT EXISTS pipeline_pattern (
  pattern_hash      TEXT PRIMARY KEY,
  template_chain    TEXT NOT NULL,
  operator_chain    TEXT NOT NULL,
  scope             TEXT NOT NULL,
  count             INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  cmd_norm_display  TEXT NOT NULL
);

-- 13. Failure recovery table
CREATE TABLE IF NOT EXISTS failure_recovery (
  scope                 TEXT NOT NULL,
  failed_template_id    TEXT NOT NULL,
  exit_code_class       TEXT NOT NULL,
  recovery_template_id  TEXT NOT NULL,
  weight                REAL NOT NULL,
  count                 INTEGER NOT NULL,
  success_rate          REAL NOT NULL,
  last_seen_ms          INTEGER NOT NULL,
  source                TEXT NOT NULL DEFAULT 'learned',
  PRIMARY KEY(scope, failed_template_id, exit_code_class, recovery_template_id)
);

-- 14. Workflow pattern table
CREATE TABLE IF NOT EXISTS workflow_pattern (
  pattern_id        TEXT PRIMARY KEY,
  template_chain    TEXT NOT NULL,
  display_chain     TEXT NOT NULL,
  scope             TEXT NOT NULL,
  step_count        INTEGER NOT NULL,
  occurrence_count  INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  avg_duration_ms   INTEGER
);

-- 15. Workflow step table
CREATE TABLE IF NOT EXISTS workflow_step (
  pattern_id    TEXT NOT NULL REFERENCES workflow_pattern(pattern_id),
  step_index    INTEGER NOT NULL,
  template_id   TEXT NOT NULL,
  PRIMARY KEY(pattern_id, step_index)
);

CREATE INDEX IF NOT EXISTS idx_workflow_step_template ON workflow_step(template_id);

-- 16. Task candidate table (discovered project tasks)
CREATE TABLE IF NOT EXISTS task_candidate (
  repo_key          TEXT NOT NULL,
  kind              TEXT NOT NULL,
  name              TEXT NOT NULL,
  command_text      TEXT NOT NULL,
  description       TEXT,
  source            TEXT NOT NULL DEFAULT 'auto',
  priority_boost    REAL NOT NULL DEFAULT 0,
  source_checksum   TEXT,
  discovered_ms     INTEGER NOT NULL,
  PRIMARY KEY(repo_key, kind, name)
);

CREATE INDEX IF NOT EXISTS idx_task_candidate_repo ON task_candidate(repo_key);

-- 17. Suggestion cache table
CREATE TABLE IF NOT EXISTS suggestion_cache (
  cache_key         TEXT PRIMARY KEY,
  session_id        TEXT NOT NULL,
  context_hash      TEXT NOT NULL,
  suggestions_json  TEXT NOT NULL,
  created_ms        INTEGER NOT NULL,
  ttl_ms            INTEGER NOT NULL,
  hit_count         INTEGER NOT NULL DEFAULT 0
);

-- 18. Suggestion feedback table
CREATE TABLE IF NOT EXISTS suggestion_feedback (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id      TEXT NOT NULL,
  ts_ms           INTEGER NOT NULL,
  prompt_prefix   TEXT,
  suggested_text  TEXT NOT NULL,
  action          TEXT NOT NULL,
  executed_text   TEXT,
  latency_ms      INTEGER
);

CREATE INDEX IF NOT EXISTS idx_feedback_session ON suggestion_feedback(session_id, ts_ms);

-- 19. Session alias table (shell alias snapshots)
CREATE TABLE IF NOT EXISTS session_alias (
  session_id    TEXT NOT NULL,
  alias_key     TEXT NOT NULL,
  expansion     TEXT NOT NULL,
  PRIMARY KEY(session_id, alias_key)
);

-- 20. Dismissal pattern table (persistent dismissal learning)
CREATE TABLE IF NOT EXISTS dismissal_pattern (
  scope                   TEXT NOT NULL,
  context_template_id     TEXT NOT NULL,
  dismissed_template_id   TEXT NOT NULL,
  dismissal_count         INTEGER NOT NULL,
  last_dismissed_ms       INTEGER NOT NULL,
  suppression_level       TEXT NOT NULL,
  PRIMARY KEY(scope, context_template_id, dismissed_template_id)
);

-- 21. Rank weight profile table (adaptive ranking weights)
CREATE TABLE IF NOT EXISTS rank_weight_profile (
  profile_key               TEXT PRIMARY KEY,
  scope                     TEXT NOT NULL,
  updated_ms                INTEGER NOT NULL,
  w_transition              REAL NOT NULL,
  w_frequency               REAL NOT NULL,
  w_success                 REAL NOT NULL,
  w_prefix                  REAL NOT NULL,
  w_affinity                REAL NOT NULL,
  w_task                    REAL NOT NULL,
  w_feedback                REAL NOT NULL,
  w_project_type_affinity   REAL NOT NULL,
  w_failure_recovery        REAL NOT NULL,
  w_risk_penalty            REAL NOT NULL,
  sample_count              INTEGER NOT NULL,
  learning_rate             REAL NOT NULL
);

-- 22. FTS5 virtual table for full-text search on commands
CREATE VIRTUAL TABLE IF NOT EXISTS command_event_fts USING fts5(
  cmd_raw,
  cmd_norm,
  repo_key UNINDEXED,
  session_id UNINDEXED,
  content='command_event',
  content_rowid='id',
  tokenize='trigram'
);

-- FTS sync triggers: keep FTS index in sync with command_event
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

-- 23. Schema migrations tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INTEGER PRIMARY KEY,
  applied_ms  INTEGER NOT NULL
);
`

// schemaV3 migrates V2 to the unified schema by adding tables previously
// stored in state.db (ai_cache, PTY capture, CI workflow, history import meta)
// and extending existing tables with columns from V1's sessions/commands.
const schemaV3 = `
-- Extend session table with V1 fields
ALTER TABLE session ADD COLUMN os TEXT;
ALTER TABLE session ADD COLUMN initial_cwd TEXT;

-- Extend command_event with V1's command_id UUID
ALTER TABLE command_event ADD COLUMN command_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_event_command_id ON command_event(command_id);

-- AI response cache (from state.db ai_cache)
CREATE TABLE IF NOT EXISTS ai_cache (
  cache_key         TEXT PRIMARY KEY,
  response_json     TEXT NOT NULL,
  provider          TEXT NOT NULL,
  created_at_ms     INTEGER NOT NULL,
  expires_at_ms     INTEGER NOT NULL,
  hit_count         INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ai_cache_expires ON ai_cache(expires_at_ms);

-- PTY command events (from state.db command_events, renamed to avoid collision)
CREATE TABLE IF NOT EXISTS pty_command_event (
  command_id        TEXT PRIMARY KEY,
  session_id        TEXT NOT NULL,
  start_ts          INTEGER,
  end_ts            INTEGER,
  exit_code         INTEGER,
  is_sensitive      INTEGER NOT NULL DEFAULT 0,
  captured_bytes    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_pty_command_event_session ON pty_command_event(session_id);

-- PTY command output blobs (from state.db command_output)
CREATE TABLE IF NOT EXISTS pty_command_output (
  command_id        TEXT PRIMARY KEY,
  stdout_blob       BLOB,
  stderr_blob       BLOB,
  created_at        INTEGER NOT NULL,
  expires_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pty_command_output_expires ON pty_command_output(expires_at);

-- CI workflow runs (from state.db workflow_runs, renamed)
CREATE TABLE IF NOT EXISTS ci_workflow_run (
  run_id            TEXT PRIMARY KEY,
  workflow_name     TEXT NOT NULL,
  workflow_hash     TEXT NOT NULL,
  workflow_path     TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'running',
  started_at        INTEGER NOT NULL,
  ended_at          INTEGER NOT NULL DEFAULT 0,
  duration_ms       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_run_name ON ci_workflow_run(workflow_name);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_run_status ON ci_workflow_run(status);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_run_started ON ci_workflow_run(started_at DESC);

-- CI workflow steps (from state.db workflow_steps, renamed)
CREATE TABLE IF NOT EXISTS ci_workflow_step (
  run_id            TEXT NOT NULL REFERENCES ci_workflow_run(run_id),
  step_id           TEXT NOT NULL,
  matrix_key        TEXT NOT NULL DEFAULT '',
  status            TEXT NOT NULL DEFAULT 'running',
  command           TEXT NOT NULL DEFAULT '',
  exit_code         INTEGER NOT NULL DEFAULT 0,
  duration_ms       INTEGER NOT NULL DEFAULT 0,
  stdout_tail       TEXT NOT NULL DEFAULT '',
  stderr_tail       TEXT NOT NULL DEFAULT '',
  outputs_json      TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY (run_id, step_id, matrix_key)
);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_step_run ON ci_workflow_step(run_id);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_step_status ON ci_workflow_step(status);

-- CI workflow analyses (from state.db workflow_analyses, renamed)
CREATE TABLE IF NOT EXISTS ci_workflow_analysis (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id            TEXT NOT NULL,
  step_id           TEXT NOT NULL,
  matrix_key        TEXT NOT NULL DEFAULT '',
  decision          TEXT NOT NULL,
  reasoning         TEXT NOT NULL DEFAULT '',
  flags_json        TEXT NOT NULL DEFAULT '{}',
  prompt            TEXT NOT NULL DEFAULT '',
  raw_response      TEXT NOT NULL DEFAULT '',
  duration_ms       INTEGER NOT NULL DEFAULT 0,
  analyzed_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_analysis_step ON ci_workflow_analysis(run_id, step_id, matrix_key);
CREATE INDEX IF NOT EXISTS idx_ci_workflow_analysis_decision ON ci_workflow_analysis(decision);

-- History import metadata (replaces V1 convention of session_id = 'imported-<shell>')
CREATE TABLE IF NOT EXISTS history_import_meta (
  shell             TEXT PRIMARY KEY,
  imported_at_ms    INTEGER NOT NULL,
  entry_count       INTEGER NOT NULL DEFAULT 0
);
`

// V2AllTables lists all tables in the V2 schema for validation purposes.
// This includes all 23 tables from spec Section 4.1 plus V3 additions.
var V2AllTables = []string{
	"session",
	"command_event",
	"command_template",
	"transition_stat",
	"command_stat",
	"slot_stat",
	"slot_correlation",
	"project_type_stat",
	"project_type_transition",
	"pipeline_event",
	"pipeline_transition",
	"pipeline_pattern",
	"failure_recovery",
	"workflow_pattern",
	"workflow_step",
	"task_candidate",
	"suggestion_cache",
	"suggestion_feedback",
	"session_alias",
	"dismissal_pattern",
	"rank_weight_profile",
	"command_event_fts",
	"schema_migrations",
	// V3 additions
	"ai_cache",
	"pty_command_event",
	"pty_command_output",
	"ci_workflow_run",
	"ci_workflow_step",
	"ci_workflow_analysis",
	"history_import_meta",
}

// V2AllIndexes lists all indexes in the V2 schema for validation purposes.
var V2AllIndexes = []string{
	"idx_event_ts",
	"idx_event_repo_ts",
	"idx_event_cwd_ts",
	"idx_event_session_ts",
	"idx_event_norm_repo",
	"idx_event_template",
	"idx_transition_stat_prev",
	"idx_command_stat_scope",
	"idx_slot_stat_lookup",
	"idx_workflow_step_template",
	"idx_task_candidate_repo",
	"idx_feedback_session",
	// V3 additions
	"idx_event_command_id",
	"idx_ai_cache_expires",
	"idx_pty_command_event_session",
	"idx_pty_command_output_expires",
	"idx_ci_workflow_run_name",
	"idx_ci_workflow_run_status",
	"idx_ci_workflow_run_started",
	"idx_ci_workflow_step_run",
	"idx_ci_workflow_step_status",
	"idx_ci_workflow_analysis_step",
	"idx_ci_workflow_analysis_decision",
}

// V2AllTriggers lists all triggers in the V2 schema for validation purposes.
var V2AllTriggers = []string{
	"command_event_ai",
	"command_event_ad",
}

// AllTables lists all tables in the V1 schema for validation purposes.
// Retained for backward compatibility with V1 database files.
var AllTables = []string{
	"session",
	"command_event",
	"transition",
	"command_score",
	"slot_value",
	"project_task",
	"schema_migrations",
}

// AllIndexes lists all indexes in the V1 schema for validation purposes.
// Retained for backward compatibility with V1 database files.
var AllIndexes = []string{
	"idx_event_ts",
	"idx_event_repo_ts",
	"idx_event_cwd_ts",
	"idx_event_session_ts",
	"idx_event_norm_repo",
	"idx_transition_prev",
	"idx_command_score_scope",
	"idx_slot_value_lookup",
	"idx_project_task_repo",
}
