package workflow

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// createTestDB creates a test database with the V2 schema tables needed for workflow.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-workflow-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create minimal V2 tables needed for workflow tests.
	_, err = db.Exec(`
		CREATE TABLE command_event (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT NOT NULL,
			ts_ms           INTEGER NOT NULL,
			cwd             TEXT NOT NULL DEFAULT '/',
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
		CREATE INDEX idx_event_session_ts ON command_event(session_id, ts_ms);
		CREATE INDEX idx_event_template ON command_event(template_id);

		CREATE TABLE workflow_pattern (
			pattern_id        TEXT PRIMARY KEY,
			template_chain    TEXT NOT NULL,
			display_chain     TEXT NOT NULL,
			scope             TEXT NOT NULL,
			step_count        INTEGER NOT NULL,
			occurrence_count  INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			avg_duration_ms   INTEGER
		);

		CREATE TABLE workflow_step (
			pattern_id    TEXT NOT NULL,
			step_index    INTEGER NOT NULL,
			template_id   TEXT NOT NULL,
			PRIMARY KEY(pattern_id, step_index)
		);
		CREATE INDEX idx_workflow_step_template ON workflow_step(template_id);
	`)
	require.NoError(t, err)

	return db
}

// insertEvent inserts a command event into the test database.
func insertEvent(t *testing.T, db *sql.DB, sessionID string, tsMs int64, cmdNorm, templateID string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id)
		VALUES (?, ?, '/', ?, ?, ?)
	`, sessionID, tsMs, cmdNorm, cmdNorm, templateID)
	require.NoError(t, err)
}

func TestMiner_DetectsContiguousSequences(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinOccurrences = 2 // Lower threshold for testing.

	// Insert the same 3-step sequence twice in the same session.
	// Sequence: git add -> git commit -> git push
	for round := 0; round < 2; round++ {
		base := int64(round * 10000)
		insertEvent(t, db, "session1", base+1000, "git add <path>", "tmpl_add")
		insertEvent(t, db, "session1", base+2000, "git commit -m <msg>", "tmpl_commit")
		insertEvent(t, db, "session1", base+3000, "git push <arg> <arg>", "tmpl_push")
	}

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	// Verify the pattern was stored.
	patterns, err := LoadPromotedPatterns(ctx, db, cfg.MinOccurrences)
	require.NoError(t, err)
	require.NotEmpty(t, patterns, "expected at least one pattern to be detected")

	// Find the 3-step pattern.
	found := false
	for _, p := range patterns {
		if p.StepCount == 3 &&
			len(p.TemplateIDs) == 3 &&
			p.TemplateIDs[0] == "tmpl_add" &&
			p.TemplateIDs[1] == "tmpl_commit" &&
			p.TemplateIDs[2] == "tmpl_push" {
			found = true
			assert.GreaterOrEqual(t, p.OccurrenceCount, 2)
		}
	}
	assert.True(t, found, "expected to find the git add->commit->push pattern")
}

func TestMiner_DetectsAcrossSessions(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinOccurrences = 2

	// Same sequence in two different sessions.
	for i, sess := range []string{"sess1", "sess2"} {
		base := int64(i * 10000)
		insertEvent(t, db, sess, base+1000, "make build", "tmpl_build")
		insertEvent(t, db, sess, base+2000, "make test", "tmpl_test")
		insertEvent(t, db, sess, base+3000, "make lint", "tmpl_lint")
	}

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	patterns, err := LoadPromotedPatterns(ctx, db, cfg.MinOccurrences)
	require.NoError(t, err)

	found := false
	for _, p := range patterns {
		if len(p.TemplateIDs) == 3 &&
			p.TemplateIDs[0] == "tmpl_build" &&
			p.TemplateIDs[1] == "tmpl_test" &&
			p.TemplateIDs[2] == "tmpl_lint" {
			found = true
			assert.GreaterOrEqual(t, p.OccurrenceCount, 2)
		}
	}
	assert.True(t, found, "expected to find the build->test->lint pattern across sessions")
}

func TestMiner_RespectsMinOccurrences(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinOccurrences = 5

	// Insert the sequence only twice - below the threshold.
	for round := 0; round < 2; round++ {
		base := int64(round * 10000)
		insertEvent(t, db, "session1", base+1000, "cmd_a", "tmpl_a")
		insertEvent(t, db, "session1", base+2000, "cmd_b", "tmpl_b")
		insertEvent(t, db, "session1", base+3000, "cmd_c", "tmpl_c")
	}

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	patterns, err := LoadPromotedPatterns(ctx, db, cfg.MinOccurrences)
	require.NoError(t, err)
	assert.Empty(t, patterns, "expected no patterns below min occurrences threshold")
}

func TestMiner_RespectsMinMaxSteps(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinSteps = 4
	cfg.MaxSteps = 4
	cfg.MinOccurrences = 2

	// Insert 3-step sequences (below minSteps=4).
	for round := 0; round < 3; round++ {
		base := int64(round * 10000)
		insertEvent(t, db, "session1", base+1000, "step_a", "tmpl_a")
		insertEvent(t, db, "session1", base+2000, "step_b", "tmpl_b")
		insertEvent(t, db, "session1", base+3000, "step_c", "tmpl_c")
	}

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	patterns, err := LoadPromotedPatterns(ctx, db, cfg.MinOccurrences)
	require.NoError(t, err)
	// Only 4-step patterns should be stored; with only 3 commands, none qualify.
	for _, p := range patterns {
		assert.GreaterOrEqual(t, p.StepCount, 4, "expected patterns with at least 4 steps")
	}
}

func TestMiner_PatternHashDeterministic(t *testing.T) {
	t.Parallel()

	ids1 := []string{"tmpl_a", "tmpl_b", "tmpl_c"}
	ids2 := []string{"tmpl_a", "tmpl_b", "tmpl_c"}
	ids3 := []string{"tmpl_a", "tmpl_c", "tmpl_b"} // Different order.

	assert.Equal(t, patternHash(ids1), patternHash(ids2))
	assert.NotEqual(t, patternHash(ids1), patternHash(ids3))
}

func TestMiner_StorePatternUpdatesOccurrenceCount(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinOccurrences = 1

	// Insert the sequence three times.
	for round := 0; round < 3; round++ {
		base := int64(round * 10000)
		insertEvent(t, db, "session1", base+1000, "npm install", "tmpl_install")
		insertEvent(t, db, "session1", base+2000, "npm build", "tmpl_build")
		insertEvent(t, db, "session1", base+3000, "npm test", "tmpl_test")
	}

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	patterns, err := LoadPromotedPatterns(ctx, db, 1)
	require.NoError(t, err)

	found := false
	for _, p := range patterns {
		if len(p.TemplateIDs) == 3 &&
			p.TemplateIDs[0] == "tmpl_install" &&
			p.TemplateIDs[1] == "tmpl_build" &&
			p.TemplateIDs[2] == "tmpl_test" {
			found = true
			assert.GreaterOrEqual(t, p.OccurrenceCount, 3)
		}
	}
	assert.True(t, found, "expected to find the install->build->test pattern")
}

func TestMiner_SkipsSessionsWithTooFewCommands(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinSteps = 3
	cfg.MinOccurrences = 1

	// Insert only 2 events (below minSteps=3).
	insertEvent(t, db, "session1", 1000, "cmd_a", "tmpl_a")
	insertEvent(t, db, "session1", 2000, "cmd_b", "tmpl_b")

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	patterns, err := LoadPromotedPatterns(ctx, db, 1)
	require.NoError(t, err)
	assert.Empty(t, patterns, "expected no patterns from too-short session")
}

func TestMiner_IgnoresEventsWithoutTemplateID(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	cfg := DefaultMinerConfig()
	cfg.MinOccurrences = 1

	// Insert events where some lack template_id.
	insertEvent(t, db, "session1", 1000, "cmd_a", "tmpl_a")
	// Insert event without template_id.
	_, err := db.Exec(`
		INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id)
		VALUES ('session1', 2000, '/', 'some cmd', 'some cmd', '')
	`)
	require.NoError(t, err)
	insertEvent(t, db, "session1", 3000, "cmd_b", "tmpl_b")
	insertEvent(t, db, "session1", 4000, "cmd_c", "tmpl_c")

	m := NewMiner(db, cfg)
	m.MineOnce(ctx)

	// The empty-template event is excluded from the session's template list,
	// so the remaining 3 events should form a pattern.
	patterns, err := LoadPromotedPatterns(ctx, db, 1)
	require.NoError(t, err)

	found := false
	for _, p := range patterns {
		if len(p.TemplateIDs) == 3 &&
			p.TemplateIDs[0] == "tmpl_a" &&
			p.TemplateIDs[1] == "tmpl_b" &&
			p.TemplateIDs[2] == "tmpl_c" {
			found = true
		}
	}
	assert.True(t, found, "expected to find pattern excluding events without template_id")
}

func TestMiner_StartStop(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)

	cfg := DefaultMinerConfig()
	cfg.MineIntervalMs = 50 // Very short interval for test.

	m := NewMiner(db, cfg)
	m.Start()
	// Give it a moment to run at least once.
	m.Stop()
	// Should not block/panic.
}

func TestLoadPromotedPatterns_Empty(t *testing.T) {
	t.Parallel()
	db := createTestDB(t)
	ctx := context.Background()

	patterns, err := LoadPromotedPatterns(ctx, db, 3)
	require.NoError(t, err)
	assert.Empty(t, patterns)
}
