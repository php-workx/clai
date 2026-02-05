package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/history"
)

func TestImportSessionID(t *testing.T) {
	assert.Equal(t, "imported-bash", ImportSessionID("bash"))
	assert.Equal(t, "imported-zsh", ImportSessionID("zsh"))
	assert.Equal(t, "imported-fish", ImportSessionID("fish"))
}

func TestHasImportedHistory_Empty(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	has, err := store.HasImportedHistory(ctx, "bash")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestHasImportedHistory_AfterImport(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	entries := []history.ImportEntry{
		{Command: "ls -la", Timestamp: time.Now()},
	}

	_, err := store.ImportHistory(ctx, entries, "bash")
	require.NoError(t, err)

	has, err := store.HasImportedHistory(ctx, "bash")
	require.NoError(t, err)
	assert.True(t, has)

	// Different shell should still be empty
	has, err = store.HasImportedHistory(ctx, "zsh")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestImportHistory_Basic(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now()
	entries := []history.ImportEntry{
		{Command: "ls -la", Timestamp: now.Add(-3 * time.Hour)},
		{Command: "git status", Timestamp: now.Add(-2 * time.Hour)},
		{Command: "echo hello", Timestamp: now.Add(-1 * time.Hour)},
	}

	imported, err := store.ImportHistory(ctx, entries, "bash")
	require.NoError(t, err)
	assert.Equal(t, 3, imported)

	// Verify entries are in database
	sessionID := ImportSessionID("bash")
	cmds, err := store.QueryCommands(ctx, CommandQuery{SessionID: &sessionID})
	require.NoError(t, err)
	assert.Len(t, cmds, 3)
}

func TestImportHistory_Empty(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	imported, err := store.ImportHistory(ctx, nil, "bash")
	require.NoError(t, err)
	assert.Equal(t, 0, imported)
}

func TestImportHistory_SkipsEmptyCommands(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	entries := []history.ImportEntry{
		{Command: "ls -la", Timestamp: time.Now()},
		{Command: "", Timestamp: time.Now()}, // Empty, should be skipped
		{Command: "git status", Timestamp: time.Now()},
	}

	imported, err := store.ImportHistory(ctx, entries, "bash")
	require.NoError(t, err)
	assert.Equal(t, 2, imported)
}

func TestImportHistory_ReplacesExisting(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// First import
	entries1 := []history.ImportEntry{
		{Command: "ls -la", Timestamp: time.Now()},
		{Command: "git status", Timestamp: time.Now()},
	}
	imported, err := store.ImportHistory(ctx, entries1, "bash")
	require.NoError(t, err)
	assert.Equal(t, 2, imported)

	// Second import (should replace)
	entries2 := []history.ImportEntry{
		{Command: "echo hello", Timestamp: time.Now()},
	}
	imported, err = store.ImportHistory(ctx, entries2, "bash")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)

	// Verify only new entries exist
	sessionID := ImportSessionID("bash")
	cmds, err := store.QueryCommands(ctx, CommandQuery{SessionID: &sessionID})
	require.NoError(t, err)
	assert.Len(t, cmds, 1)
	assert.Equal(t, "echo hello", cmds[0].Command)
}

func TestImportHistory_DifferentShellsIndependent(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Import bash history
	bashEntries := []history.ImportEntry{
		{Command: "bash command 1", Timestamp: time.Now()},
		{Command: "bash command 2", Timestamp: time.Now()},
	}
	imported, err := store.ImportHistory(ctx, bashEntries, "bash")
	require.NoError(t, err)
	assert.Equal(t, 2, imported)

	// Import zsh history
	zshEntries := []history.ImportEntry{
		{Command: "zsh command 1", Timestamp: time.Now()},
	}
	imported, err = store.ImportHistory(ctx, zshEntries, "zsh")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)

	// Verify both exist independently
	bashSessionID := ImportSessionID("bash")
	bashCmds, err := store.QueryCommands(ctx, CommandQuery{SessionID: &bashSessionID})
	require.NoError(t, err)
	assert.Len(t, bashCmds, 2)

	zshSessionID := ImportSessionID("zsh")
	zshCmds, err := store.QueryCommands(ctx, CommandQuery{SessionID: &zshSessionID})
	require.NoError(t, err)
	assert.Len(t, zshCmds, 1)
}

func TestImportHistory_NoTimestamps(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Entries without timestamps
	entries := []history.ImportEntry{
		{Command: "ls -la"},
		{Command: "git status"},
		{Command: "echo hello"},
	}

	imported, err := store.ImportHistory(ctx, entries, "bash")
	require.NoError(t, err)
	assert.Equal(t, 3, imported)

	// Verify entries are in database with timestamps
	sessionID := ImportSessionID("bash")
	cmds, err := store.QueryCommands(ctx, CommandQuery{SessionID: &sessionID, Limit: 100})
	require.NoError(t, err)
	assert.Len(t, cmds, 3)

	// All should have non-zero timestamps
	for _, cmd := range cmds {
		assert.NotZero(t, cmd.TsStartUnixMs)
	}
}

func TestImportHistory_CommandMetadata(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	entries := []history.ImportEntry{
		{Command: "sudo apt install pkg | tee log.txt", Timestamp: time.Now()},
	}

	_, err := store.ImportHistory(ctx, entries, "bash")
	require.NoError(t, err)

	sessionID := ImportSessionID("bash")
	cmds, err := store.QueryCommands(ctx, CommandQuery{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, cmds, 1)

	cmd := cmds[0]
	assert.Equal(t, "sudo apt install pkg | tee log.txt", cmd.Command)
	assert.NotEmpty(t, cmd.CommandNorm)
	assert.NotEmpty(t, cmd.CommandHash)
	assert.True(t, cmd.IsSudo)
	assert.Equal(t, 1, cmd.PipeCount)
	assert.Greater(t, cmd.WordCount, 0)
}
