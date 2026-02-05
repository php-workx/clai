package search

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

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-search-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create command_event table (required for FTS5 content table)
	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL,
			repo_key      TEXT,
			ephemeral     INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX idx_event_ts ON command_event(ts);
	`)
	require.NoError(t, err)

	return db
}

// insertTestEvent inserts a test event into the database.
func insertTestEvent(t *testing.T, db *sql.DB, cmdRaw, repoKey, cwd string, ephemeral bool) int64 {
	t.Helper()

	ephInt := 0
	if ephemeral {
		ephInt = 1
	}

	result, err := db.Exec(`
		INSERT INTO command_event (session_id, ts, cmd_raw, cmd_norm, cwd, repo_key, ephemeral)
		VALUES ('session1', ?, ?, ?, ?, ?, ?)
	`, 1000000, cmdRaw, cmdRaw, cwd, repoKey, ephInt)
	require.NoError(t, err)

	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestService_NewService(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	// FTS5 should be available with modernc sqlite
	assert.True(t, svc.FTS5Available())
}

func TestService_Search_FTS5(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	require.True(t, svc.FTS5Available())

	ctx := context.Background()

	// Insert test events
	id1 := insertTestEvent(t, db, "docker run nginx", "/home/user/project", "/home/user/project", false)
	id2 := insertTestEvent(t, db, "docker build .", "/home/user/project", "/home/user/project", false)
	id3 := insertTestEvent(t, db, "git status", "/home/user/other", "/home/user/other", false)

	// Index events
	require.NoError(t, svc.IndexEvent(ctx, id1))
	require.NoError(t, svc.IndexEvent(ctx, id2))
	require.NoError(t, svc.IndexEvent(ctx, id3))

	// Search for docker
	results, err := svc.Search(ctx, "docker", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Search for git
	results, err = svc.Search(ctx, "git", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "git status", results[0].CmdRaw)
}

func TestService_Search_RepoFilter(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert events in different repos
	id1 := insertTestEvent(t, db, "make build", "repo1", "/path/repo1", false)
	id2 := insertTestEvent(t, db, "make test", "repo2", "/path/repo2", false)

	require.NoError(t, svc.IndexEvent(ctx, id1))
	require.NoError(t, svc.IndexEvent(ctx, id2))

	// Search all repos
	results, err := svc.Search(ctx, "make", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Filter by repo
	results, err = svc.Search(ctx, "make", SearchOptions{RepoKey: "repo1"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "repo1", results[0].RepoKey)
}

func TestService_Search_CwdFilter(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert events in different directories
	id1 := insertTestEvent(t, db, "npm install", "", "/home/user/frontend", false)
	id2 := insertTestEvent(t, db, "npm test", "", "/home/user/backend", false)

	require.NoError(t, svc.IndexEvent(ctx, id1))
	require.NoError(t, svc.IndexEvent(ctx, id2))

	// Filter by cwd
	results, err := svc.Search(ctx, "npm", SearchOptions{Cwd: "/home/user/frontend"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "npm install", results[0].CmdRaw)
}

func TestService_Search_EphemeralNotIndexed(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert regular and ephemeral events
	id1 := insertTestEvent(t, db, "secret-command", "", "/home/user", false)
	id2 := insertTestEvent(t, db, "secret-password", "", "/home/user", true) // ephemeral

	// Index both (ephemeral should be skipped)
	require.NoError(t, svc.IndexEvent(ctx, id1))
	require.NoError(t, svc.IndexEvent(ctx, id2))

	// Search should only find the regular event
	results, err := svc.Search(ctx, "secret", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "secret-command", results[0].CmdRaw)
}

func TestService_Search_Limit(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert many events
	for i := 0; i < 30; i++ {
		id := insertTestEvent(t, db, "test command", "", "/home/user", false)
		require.NoError(t, svc.IndexEvent(ctx, id))
	}

	// Default limit
	results, err := svc.Search(ctx, "test", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, DefaultLimit)

	// Custom limit
	results, err = svc.Search(ctx, "test", SearchOptions{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, results, 5)

	// Limit capped at max
	results, err = svc.Search(ctx, "test", SearchOptions{Limit: 200})
	require.NoError(t, err)
	assert.Len(t, results, 30) // Only 30 events exist
}

func TestService_Search_EmptyQuery(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	results, err := svc.Search(ctx, "", SearchOptions{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestService_IndexEventBatch(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert multiple events
	ids := make([]int64, 10)
	for i := range ids {
		ids[i] = insertTestEvent(t, db, "batch command", "", "/home/user", false)
	}

	// Batch index
	require.NoError(t, svc.IndexEventBatch(ctx, ids))

	// Search should find all
	results, err := svc.Search(ctx, "batch", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 10)
}

func TestService_RebuildIndex(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultConfig())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert events without indexing
	insertTestEvent(t, db, "pre-rebuild command", "", "/home/user", false)

	// Search should find nothing
	results, err := svc.Search(ctx, "pre-rebuild", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 0)

	// Rebuild index
	require.NoError(t, svc.RebuildIndex(ctx))

	// Now search should work
	results, err = svc.Search(ctx, "pre-rebuild", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestService_FallbackSearch(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create service with fallback enabled
	svc, err := NewService(db, Config{EnableFallback: true})
	require.NoError(t, err)
	defer svc.Close()

	// Insert events (skip indexing to test fallback)
	insertTestEvent(t, db, "fallback test command", "", "/home/user", false)

	// Even without FTS5 index, fallback should work
	// Note: FTS5 is available in modernc sqlite, so we just test the fallback path exists
	assert.True(t, svc.FallbackEnabled())
}

func TestEscapeFTS5Query(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"simple", `"simple"`},
		{`with "quotes"`, `"with ""quotes"""`},
		{"docker run", `"docker run"`},
		{"git commit -m", `"git commit -m"`},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := escapeFTS5Query(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestEscapeLikePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with%percent", `with\%percent`},
		{"under_score", `under\_score`},
		{"mixed%_chars", `mixed\%\_chars`},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := escapeLikePattern(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.NotNil(t, cfg.Logger)
	assert.False(t, cfg.EnableFallback)
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "command_fts", FTS5TableName)
	assert.Equal(t, 20, DefaultLimit)
	assert.Equal(t, 100, MaxLimit)
}
