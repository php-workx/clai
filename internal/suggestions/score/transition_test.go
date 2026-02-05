package score

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

// createTestDBWithTransitions creates a test DB with transition and command_event tables.
func createTestDBWithTransitions(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-transition-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create transition table
	_, err = db.Exec(`
		CREATE TABLE transition (
			scope         TEXT NOT NULL,
			prev_norm     TEXT NOT NULL,
			next_norm     TEXT NOT NULL,
			count         INTEGER NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_norm, next_norm)
		);
		CREATE INDEX idx_transition_prev ON transition(scope, prev_norm);
	`)
	require.NoError(t, err)

	// Create command_event table for prev command lookup
	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_norm      TEXT NOT NULL,
			repo_key      TEXT
		);
		CREATE INDEX idx_event_session_ts ON command_event(session_id, ts);
	`)
	require.NoError(t, err)

	return db
}

func TestNewTransitionStore(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)

	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	assert.NotNil(t, ts.getStmt)
	assert.NotNil(t, ts.upsertStmt)
	assert.NotNil(t, ts.getPrevStmt)
	assert.NotNil(t, ts.getTopNextStmt)
}

func TestTransitionStore_RecordTransition(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)
	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	ctx := context.Background()

	t.Run("first transition sets count to 1", func(t *testing.T) {
		err := ts.RecordTransition(ctx, ScopeGlobal, "git status", "git commit", 1000)
		require.NoError(t, err)

		count, err := ts.GetTransitionCount(ctx, ScopeGlobal, "git status", "git commit")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("repeated transition increments count", func(t *testing.T) {
		err := ts.RecordTransition(ctx, ScopeGlobal, "git status", "git commit", 2000)
		require.NoError(t, err)

		count, err := ts.GetTransitionCount(ctx, ScopeGlobal, "git status", "git commit")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("different transition is tracked separately", func(t *testing.T) {
		err := ts.RecordTransition(ctx, ScopeGlobal, "git status", "git add", 3000)
		require.NoError(t, err)

		count, err := ts.GetTransitionCount(ctx, ScopeGlobal, "git status", "git add")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Original transition unchanged
		count, err = ts.GetTransitionCount(ctx, ScopeGlobal, "git status", "git commit")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})
}

func TestTransitionStore_RecordTransitionBoth(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)
	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	ctx := context.Background()

	t.Run("updates both global and repo scopes", func(t *testing.T) {
		repoKey := "sha256:testrepo"
		err := ts.RecordTransitionBoth(ctx, "make build", "make test", repoKey, 5000)
		require.NoError(t, err)

		// Check global scope
		globalCount, err := ts.GetTransitionCount(ctx, ScopeGlobal, "make build", "make test")
		require.NoError(t, err)
		assert.Equal(t, 1, globalCount)

		// Check repo scope
		repoCount, err := ts.GetTransitionCount(ctx, repoKey, "make build", "make test")
		require.NoError(t, err)
		assert.Equal(t, 1, repoCount)
	})

	t.Run("empty repo key only updates global", func(t *testing.T) {
		err := ts.RecordTransitionBoth(ctx, "ls -la", "cd", "", 6000)
		require.NoError(t, err)

		// Check global scope
		globalCount, err := ts.GetTransitionCount(ctx, ScopeGlobal, "ls -la", "cd")
		require.NoError(t, err)
		assert.Equal(t, 1, globalCount)
	})
}

func TestTransitionStore_GetPreviousCommand(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)
	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	ctx := context.Background()

	// Insert some command events
	_, err = db.Exec(`
		INSERT INTO command_event (session_id, ts, cmd_norm, repo_key) VALUES
		('session1', 1000, 'git status', 'repo1'),
		('session1', 2000, 'git add', 'repo1'),
		('session1', 3000, 'git commit', 'repo1'),
		('session2', 1500, 'npm test', 'repo2')
	`)
	require.NoError(t, err)

	t.Run("returns previous command in same session", func(t *testing.T) {
		prev, err := ts.GetPreviousCommand(ctx, "session1", 3000)
		require.NoError(t, err)
		assert.Equal(t, "git add", prev)
	})

	t.Run("returns most recent command before timestamp", func(t *testing.T) {
		prev, err := ts.GetPreviousCommand(ctx, "session1", 2500)
		require.NoError(t, err)
		assert.Equal(t, "git add", prev)
	})

	t.Run("returns empty for first command", func(t *testing.T) {
		prev, err := ts.GetPreviousCommand(ctx, "session1", 1000)
		require.NoError(t, err)
		assert.Equal(t, "", prev)
	})

	t.Run("returns empty for nonexistent session", func(t *testing.T) {
		prev, err := ts.GetPreviousCommand(ctx, "nonexistent", 5000)
		require.NoError(t, err)
		assert.Equal(t, "", prev)
	})
}

func TestTransitionStore_GetPreviousCommandWithFallback(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)
	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	ctx := context.Background()

	// Insert command events
	_, err = db.Exec(`
		INSERT INTO command_event (session_id, ts, cmd_norm, repo_key) VALUES
		('session1', 1000, 'git status', 'repo1'),
		('session2', 2000, 'make build', 'repo1')
	`)
	require.NoError(t, err)

	t.Run("prefers session match", func(t *testing.T) {
		// session1 has a command, should return it
		prev, err := ts.GetPreviousCommandWithFallback(ctx, "session1", "repo1", 5000, 60000)
		require.NoError(t, err)
		assert.Equal(t, "git status", prev)
	})

	t.Run("falls back to repo when no session match", func(t *testing.T) {
		// session3 has no commands, should fallback to repo1's most recent
		prev, err := ts.GetPreviousCommandWithFallback(ctx, "session3", "repo1", 5000, 60000)
		require.NoError(t, err)
		assert.Equal(t, "make build", prev)
	})

	t.Run("respects fallback window", func(t *testing.T) {
		// With a small window, older commands won't be found
		prev, err := ts.GetPreviousCommandWithFallback(ctx, "session3", "repo1", 5000, 1000)
		require.NoError(t, err)
		assert.Equal(t, "", prev) // 5000 - 1000 = 4000, command at 2000 is outside window
	})

	t.Run("returns empty when no repo key", func(t *testing.T) {
		prev, err := ts.GetPreviousCommandWithFallback(ctx, "session3", "", 5000, 60000)
		require.NoError(t, err)
		assert.Equal(t, "", prev)
	})
}

func TestTransitionStore_GetTopNextCommands(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)
	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	ctx := context.Background()
	baseTs := int64(1000000)

	// Record transitions with different frequencies
	// git status -> git add (3 times)
	// git status -> git commit (2 times)
	// git status -> git diff (1 time)
	for i := 0; i < 3; i++ {
		require.NoError(t, ts.RecordTransition(ctx, ScopeGlobal, "git status", "git add", baseTs+int64(i*100)))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, ts.RecordTransition(ctx, ScopeGlobal, "git status", "git commit", baseTs+int64(i*100)))
	}
	require.NoError(t, ts.RecordTransition(ctx, ScopeGlobal, "git status", "git diff", baseTs))

	t.Run("returns top commands by count", func(t *testing.T) {
		top, err := ts.GetTopNextCommands(ctx, ScopeGlobal, "git status", 3)
		require.NoError(t, err)

		assert.Len(t, top, 3)
		// git add has highest count (3)
		assert.Equal(t, "git add", top[0].NextNorm)
		assert.Equal(t, 3, top[0].Count)
		// git commit has second highest (2)
		assert.Equal(t, "git commit", top[1].NextNorm)
		assert.Equal(t, 2, top[1].Count)
		// git diff has lowest (1)
		assert.Equal(t, "git diff", top[2].NextNorm)
		assert.Equal(t, 1, top[2].Count)
	})

	t.Run("respects limit", func(t *testing.T) {
		top, err := ts.GetTopNextCommands(ctx, ScopeGlobal, "git status", 2)
		require.NoError(t, err)

		assert.Len(t, top, 2)
	})

	t.Run("returns empty for nonexistent prev command", func(t *testing.T) {
		top, err := ts.GetTopNextCommands(ctx, ScopeGlobal, "nonexistent", 10)
		require.NoError(t, err)

		assert.Empty(t, top)
	})
}

func TestTransitionStore_GetTransitionCount_Nonexistent(t *testing.T) {
	t.Parallel()

	db := createTestDBWithTransitions(t)
	ts, err := NewTransitionStore(db)
	require.NoError(t, err)
	defer ts.Close()

	ctx := context.Background()

	count, err := ts.GetTransitionCount(ctx, ScopeGlobal, "nonexistent", "also-nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
