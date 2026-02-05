// Package db provides SQLite-based storage for the suggestions engine.
// It implements the schema defined in specs/tech_suggestions_v3.md Section 4.
package db

// SchemaVersion is the current supported schema version.
// The daemon will refuse to run if the DB schema version exceeds this.
const SchemaVersion = 1

// schemaV1 creates the initial schema for the suggestions engine.
// This schema supports command history, transitions, scores, slot values,
// project tasks, and FTS search.
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

// AllTables lists all tables in the schema for validation purposes.
var AllTables = []string{
	"session",
	"command_event",
	"transition",
	"command_score",
	"slot_value",
	"project_task",
	"schema_migrations",
}

// AllIndexes lists all indexes in the schema for validation purposes.
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
