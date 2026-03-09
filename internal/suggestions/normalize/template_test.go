package normalize

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE command_template (
			template_id     TEXT PRIMARY KEY,
			cmd_norm        TEXT NOT NULL,
			tags            TEXT,
			slot_count      INTEGER NOT NULL,
			first_seen_ms   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)
	return db
}

func TestTemplateStore_Upsert(t *testing.T) {
	db := setupTestDB(t)
	store := NewTemplateStore(db)
	ctx := context.Background()

	tmpl := Template{
		TemplateID:  ComputeTemplateID("git commit -m <msg>"),
		CmdNorm:     "git commit -m <msg>",
		Tags:        []string{"git", "vcs"},
		SlotCount:   1,
		FirstSeenMs: 1000,
		LastSeenMs:  1000,
	}

	err := store.Upsert(ctx, &tmpl)
	require.NoError(t, err)

	got, err := store.Get(ctx, tmpl.TemplateID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, tmpl.CmdNorm, got.CmdNorm)
	assert.Equal(t, tmpl.Tags, got.Tags)
	assert.Equal(t, tmpl.SlotCount, got.SlotCount)
	assert.Equal(t, tmpl.FirstSeenMs, got.FirstSeenMs)
	assert.Equal(t, tmpl.LastSeenMs, got.LastSeenMs)
}

func TestTemplateStore_UpsertUpdatesLastSeen(t *testing.T) {
	db := setupTestDB(t)
	store := NewTemplateStore(db)
	ctx := context.Background()

	id := ComputeTemplateID("ls -la")
	tmpl := Template{
		TemplateID:  id,
		CmdNorm:     "ls -la",
		SlotCount:   0,
		FirstSeenMs: 1000,
		LastSeenMs:  1000,
	}

	err := store.Upsert(ctx, &tmpl)
	require.NoError(t, err)

	tmpl.LastSeenMs = 2000
	err = store.Upsert(ctx, &tmpl)
	require.NoError(t, err)

	got, err := store.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(1000), got.FirstSeenMs)
	assert.Equal(t, int64(2000), got.LastSeenMs)
}

func TestTemplateStore_UpsertDoesNotDecrementLastSeen(t *testing.T) {
	db := setupTestDB(t)
	store := NewTemplateStore(db)
	ctx := context.Background()

	id := ComputeTemplateID("ls -la")
	tmpl := Template{
		TemplateID:  id,
		CmdNorm:     "ls -la",
		SlotCount:   0,
		FirstSeenMs: 1000,
		LastSeenMs:  5000,
	}

	err := store.Upsert(ctx, &tmpl)
	require.NoError(t, err)

	tmpl.LastSeenMs = 3000
	err = store.Upsert(ctx, &tmpl)
	require.NoError(t, err)

	got, err := store.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(5000), got.LastSeenMs)
}

func TestTemplateStore_GetNonExistent(t *testing.T) {
	db := setupTestDB(t)
	store := NewTemplateStore(db)
	ctx := context.Background()

	got, err := store.Get(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTemplateStore_UpsertNilTags(t *testing.T) {
	db := setupTestDB(t)
	store := NewTemplateStore(db)
	ctx := context.Background()

	tmpl := Template{
		TemplateID:  ComputeTemplateID("echo hello"),
		CmdNorm:     "echo hello",
		Tags:        nil,
		SlotCount:   0,
		FirstSeenMs: 1000,
		LastSeenMs:  1000,
	}

	err := store.Upsert(ctx, &tmpl)
	require.NoError(t, err)

	got, err := store.Get(ctx, tmpl.TemplateID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got.Tags)
}

func TestCountSlots(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"no slots", "ls -la", 0},
		{"one new placeholder", "cat <PATH>", 1},
		{"one legacy slot", "cat <path>", 1},
		{"mixed", "git commit -m <msg> <PATH>", 2},
		{"multiple same", "<PATH> <PATH>", 2},
		{"all new types", "<PATH> <UUID> <URL> <NUM>", 4},
		{"all legacy types", "<path> <num> <sha> <url> <msg> <arg>", 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountSlots(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
