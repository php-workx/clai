package alias

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database with the session_alias table.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE session_alias (
			session_id TEXT NOT NULL,
			alias_key  TEXT NOT NULL,
			expansion  TEXT NOT NULL,
			PRIMARY KEY(session_id, alias_key)
		)
	`)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })
	return db
}

func TestStore_SaveAndLoad(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	aliases := AliasMap{
		"gs": "git status",
		"gp": "git push",
		"ll": "ls -la",
	}

	err := store.SaveAliases(ctx, "sess-1", aliases)
	require.NoError(t, err)

	loaded, err := store.LoadAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, aliases, loaded)
}

func TestStore_LoadEmpty(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	loaded, err := store.LoadAliases(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, loaded)
	assert.NotNil(t, loaded) // Should return empty map, not nil
}

func TestStore_SaveReplacesPrevious(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	// Save initial aliases
	err := store.SaveAliases(ctx, "sess-1", AliasMap{
		"gs": "git status",
		"ll": "ls -la",
	})
	require.NoError(t, err)

	// Save new aliases (should replace)
	err = store.SaveAliases(ctx, "sess-1", AliasMap{
		"gs": "git status --short",
		"gp": "git push",
	})
	require.NoError(t, err)

	loaded, err := store.LoadAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "git status --short", loaded["gs"])
	assert.Equal(t, "git push", loaded["gp"])
	assert.Empty(t, loaded["ll"]) // Old alias should be gone
}

func TestStore_SessionIsolation(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	err := store.SaveAliases(ctx, "sess-1", AliasMap{"gs": "git status"})
	require.NoError(t, err)

	err = store.SaveAliases(ctx, "sess-2", AliasMap{"gp": "git push"})
	require.NoError(t, err)

	loaded1, err := store.LoadAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, AliasMap{"gs": "git status"}, loaded1)

	loaded2, err := store.LoadAliases(ctx, "sess-2")
	require.NoError(t, err)
	assert.Equal(t, AliasMap{"gp": "git push"}, loaded2)
}

func TestStore_DeleteAliases(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	err := store.SaveAliases(ctx, "sess-1", AliasMap{"gs": "git status"})
	require.NoError(t, err)

	err = store.DeleteAliases(ctx, "sess-1")
	require.NoError(t, err)

	loaded, err := store.LoadAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestStore_CountAliases(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	count, err := store.CountAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	err = store.SaveAliases(ctx, "sess-1", AliasMap{
		"gs": "git status",
		"gp": "git push",
		"ll": "ls -la",
	})
	require.NoError(t, err)

	count, err = store.CountAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestStore_SaveEmptyMap(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	// Save some aliases first
	err := store.SaveAliases(ctx, "sess-1", AliasMap{"gs": "git status"})
	require.NoError(t, err)

	// Save empty map (should clear all aliases)
	err = store.SaveAliases(ctx, "sess-1", AliasMap{})
	require.NoError(t, err)

	count, err := store.CountAliases(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
