package suggest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/storage"
)

// Test helpers

func newTestStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}

	return store
}

func createTestSession(t *testing.T, store *storage.SQLiteStore, sessionID string) {
	t.Helper()
	ctx := context.Background()

	session := &storage.Session{
		SessionID:       sessionID,
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
}

func createTestCommand(t *testing.T, store *storage.SQLiteStore, sessionID, commandID, cwd, command string, ts int64, isSuccess bool) {
	t.Helper()
	ctx := context.Background()

	cmd := &storage.Command{
		CommandID:     commandID,
		SessionID:     sessionID,
		TsStartUnixMs: ts,
		CWD:           cwd,
		Command:       command,
		IsSuccess:     &isSuccess,
	}
	if err := store.CreateCommand(ctx, cmd); err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}
}

func TestSourceWeight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source Source
		want   float64
	}{
		{SourceSession, 1.0},
		{SourceCWD, 0.7},
		{SourceGlobal, 0.4},
		{SourceAI, 0.5},
		{Source("unknown"), 0.0},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			t.Parallel()
			got := SourceWeight(tt.source)
			if got != tt.want {
				t.Errorf("SourceWeight(%s) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

func TestCommandSource_QuerySession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session and commands
	createTestSession(t, store, "session-1")
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "git status", 1700000001000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/tmp", "git push", 1700000002000, true)

	source := NewCommandSource(store)

	// Query session
	result, err := source.QuerySession(ctx, "session-1", "git", 10)
	if err != nil {
		t.Fatalf("QuerySession() error = %v", err)
	}

	if result.Source != SourceSession {
		t.Errorf("Source = %v, want %v", result.Source, SourceSession)
	}
	if len(result.Commands) != 2 {
		t.Errorf("Got %d commands, want 2", len(result.Commands))
	}
}

func TestCommandSource_QuerySession_EmptySessionID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	source := NewCommandSource(store)

	result, err := source.QuerySession(context.Background(), "", "git", 10)
	if err != nil {
		t.Fatalf("QuerySession() error = %v", err)
	}

	if len(result.Commands) != 0 {
		t.Errorf("Expected 0 commands for empty sessionID, got %d", len(result.Commands))
	}
}

func TestCommandSource_QueryCWD(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session and commands in different directories
	createTestSession(t, store, "session-1")
	createTestCommand(t, store, "session-1", "cmd-1", "/home/user", "make build", 1700000001000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/home/user", "make test", 1700000002000, true)
	createTestCommand(t, store, "session-1", "cmd-3", "/tmp", "ls -la", 1700000003000, true)

	source := NewCommandSource(store)

	// Query by CWD
	result, err := source.QueryCWD(ctx, "/home/user", "make", 10)
	if err != nil {
		t.Fatalf("QueryCWD() error = %v", err)
	}

	if result.Source != SourceCWD {
		t.Errorf("Source = %v, want %v", result.Source, SourceCWD)
	}
	if len(result.Commands) != 2 {
		t.Errorf("Got %d commands, want 2", len(result.Commands))
	}
}

func TestCommandSource_QueryCWD_EmptyCWD(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	source := NewCommandSource(store)

	result, err := source.QueryCWD(context.Background(), "", "make", 10)
	if err != nil {
		t.Fatalf("QueryCWD() error = %v", err)
	}

	if len(result.Commands) != 0 {
		t.Errorf("Expected 0 commands for empty CWD, got %d", len(result.Commands))
	}
}

func TestCommandSource_QueryGlobal(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create commands in multiple sessions
	createTestSession(t, store, "session-1")
	createTestSession(t, store, "session-2")
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "docker run nginx", 1700000001000, true)
	createTestCommand(t, store, "session-2", "cmd-2", "/var", "docker build .", 1700000002000, true)

	source := NewCommandSource(store)

	// Query global
	result, err := source.QueryGlobal(ctx, "docker", 10)
	if err != nil {
		t.Fatalf("QueryGlobal() error = %v", err)
	}

	if result.Source != SourceGlobal {
		t.Errorf("Source = %v, want %v", result.Source, SourceGlobal)
	}
	if len(result.Commands) != 2 {
		t.Errorf("Got %d commands, want 2", len(result.Commands))
	}
}

func TestCommandSource_QueryAllScopes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create commands
	createTestSession(t, store, "session-1")
	createTestSession(t, store, "session-2")

	// Session-1 commands
	createTestCommand(t, store, "session-1", "cmd-1", "/home/user", "git status", 1700000001000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/home/user", "git push", 1700000002000, true)

	// Session-2 commands (different directory)
	createTestCommand(t, store, "session-2", "cmd-3", "/var/log", "git log", 1700000003000, true)

	source := NewCommandSource(store)

	// Query all scopes for session-1 in /home/user
	results, err := source.QueryAllScopes(ctx, "session-1", "/home/user", "git", 10)
	if err != nil {
		t.Fatalf("QueryAllScopes() error = %v", err)
	}

	// Should have results from all three scopes
	if len(results) != 3 {
		t.Errorf("Got %d result sets, want 3", len(results))
	}

	// Verify sources are in expected order
	expectedSources := []Source{SourceSession, SourceCWD, SourceGlobal}
	for i, result := range results {
		if result.Source != expectedSources[i] {
			t.Errorf("results[%d].Source = %v, want %v", i, result.Source, expectedSources[i])
		}
	}
}

func TestCommandSource_QueryAllScopes_EmptyResults(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	source := NewCommandSource(store)

	// Query with no matching commands
	results, err := source.QueryAllScopes(context.Background(), "nonexistent", "/nonexistent", "xyz", 10)
	if err != nil {
		t.Fatalf("QueryAllScopes() error = %v", err)
	}

	// Should have no results
	if len(results) != 0 {
		t.Errorf("Got %d result sets, want 0", len(results))
	}
}
