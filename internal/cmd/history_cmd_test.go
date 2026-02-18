package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/storage"
)

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		expected string
		ms       int64
	}{
		{"0ms", 0},
		{"100ms", 100},
		{"999ms", 999},
		{"1.0s", 1000},
		{"1.5s", 1500},
		{"5.0s", 5000},
		{"59.0s", 59000},
		{"1m0s", 60000},
		{"1m30s", 90000},
		{"60m0s", 3600000},
	}

	for _, tt := range tests {
		result := formatDurationMs(tt.ms)
		if result != tt.expected {
			t.Errorf("formatDurationMs(%d) = %q, want %q", tt.ms, result, tt.expected)
		}
	}
}

func TestHistoryCmd_Flags(t *testing.T) {
	// Verify all expected flags are registered
	expectedFlags := []struct {
		name      string
		shorthand string
	}{
		{"limit", "n"},
		{"cwd", "c"},
		{"session", ""},
		{"global", "g"},
		{"status", "s"},
		{"format", ""},
	}

	for _, f := range expectedFlags {
		flag := historyCmd.Flags().Lookup(f.name)
		if flag == nil {
			t.Errorf("Expected flag --%s to be registered", f.name)
			continue
		}
		if flag.Shorthand != f.shorthand {
			t.Errorf("Flag --%s: expected shorthand %q, got %q", f.name, f.shorthand, flag.Shorthand)
		}
	}
}

func TestHistoryCmd_DefaultLimit(t *testing.T) {
	flag := historyCmd.Flags().Lookup("limit")
	if flag == nil {
		t.Fatal("limit flag not found")
	}
	if flag.DefValue != "20" {
		t.Errorf("Expected default limit=20, got %s", flag.DefValue)
	}
}

func TestHistoryCmd_GlobalDefault(t *testing.T) {
	flag := historyCmd.Flags().Lookup("global")
	if flag == nil {
		t.Fatal("global flag not found")
	}
	if flag.DefValue != "false" {
		t.Errorf("Expected default global=false, got %s", flag.DefValue)
	}
}

func TestHistoryCmd_StatusDefault(t *testing.T) {
	flag := historyCmd.Flags().Lookup("status")
	if flag == nil {
		t.Fatal("status flag not found")
	}
	if flag.DefValue != "" {
		t.Errorf("Expected default status=\"\", got %q", flag.DefValue)
	}
	if flag.Shorthand != "s" {
		t.Errorf("Expected status shorthand -s, got -%s", flag.Shorthand)
	}
}

func TestHistoryCmd_FormatDefault(t *testing.T) {
	flag := historyCmd.Flags().Lookup("format")
	if flag == nil {
		t.Fatal("format flag not found")
	}
	if flag.DefValue != "raw" {
		t.Errorf("Expected default format=raw, got %s", flag.DefValue)
	}
}

// Integration tests for session ID resolution

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

func TestHistoryCmd_ShortSessionID_Resolution(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a session with a known ID
	session := &storage.Session{
		SessionID:       "abc12345-6789-0def-ghij-klmnopqrstuv",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/home/user",
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Test: 8-character prefix should resolve to full session
	shortID := "abc12345"
	resolved, err := store.GetSessionByPrefix(ctx, shortID)
	if err != nil {
		t.Fatalf("GetSessionByPrefix(%q) error = %v", shortID, err)
	}

	if resolved.SessionID != session.SessionID {
		t.Errorf("Resolved session ID = %q, want %q", resolved.SessionID, session.SessionID)
	}
}

func TestHistoryCmd_ShortSessionID_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Test: Non-existent prefix should return error
	_, err := store.GetSessionByPrefix(ctx, "nonexist")
	if err == nil {
		t.Error("GetSessionByPrefix() should return error for non-existent session")
	}
	if !errors.Is(err, storage.ErrSessionNotFound) {
		t.Errorf("GetSessionByPrefix() error = %v, want ErrSessionNotFound", err)
	}
}

func TestHistoryCmd_ShortSessionID_Ambiguous(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create two sessions with same prefix
	session1 := &storage.Session{
		SessionID:       "same1234-aaaa-bbbb-cccc-ddddeeeeffffgggg",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/home/user",
	}
	session2 := &storage.Session{
		SessionID:       "same1234-xxxx-yyyy-zzzz-111122223333",
		StartedAtUnixMs: 1700000001000,
		Shell:           "bash",
		OS:              "linux",
		InitialCWD:      "/tmp",
	}

	if err := store.CreateSession(ctx, session1); err != nil {
		t.Fatalf("CreateSession(1) error = %v", err)
	}
	if err := store.CreateSession(ctx, session2); err != nil {
		t.Fatalf("CreateSession(2) error = %v", err)
	}

	// Test: Ambiguous prefix should return error
	_, err := store.GetSessionByPrefix(ctx, "same1234")
	if err == nil {
		t.Error("GetSessionByPrefix() should return error for ambiguous prefix")
	}
	if !errors.Is(err, storage.ErrAmbiguousSession) {
		t.Errorf("GetSessionByPrefix() error = %v, want ErrAmbiguousSession", err)
	}
}

func TestHistoryCmd_FullSessionID_StillWorks(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a session
	fullID := "full1234-5678-90ab-cdef-ghijklmnopqr"
	session := &storage.Session{
		SessionID:       fullID,
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/home/user",
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Test: Full session ID should work via exact match
	resolved, err := store.GetSession(ctx, fullID)
	if err != nil {
		t.Fatalf("GetSession(%q) error = %v", fullID, err)
	}

	if resolved.SessionID != fullID {
		t.Errorf("Resolved session ID = %q, want %q", resolved.SessionID, fullID)
	}
}

func TestHistoryCmd_FullSessionID_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Test: Full UUID that doesn't exist should return error
	fullID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, err := store.GetSession(ctx, fullID)
	if err == nil {
		t.Error("GetSession() should return error for non-existent full UUID")
	}
	if !errors.Is(err, storage.ErrSessionNotFound) {
		t.Errorf("GetSession() error = %v, want ErrSessionNotFound", err)
	}
}

// Tests below use the production resolveSessionID from history_cmd.go.

func TestHistoryCmd_SessionResolution_ShortToFull(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a session with a full UUID
	fullID := "12345678-1234-5678-9abc-def012345678"
	session := &storage.Session{
		SessionID:       fullID,
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/home/user",
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Test: Short prefix should resolve to full UUID
	resolved, err := resolveSessionID(ctx, store, "12345678")
	if err != nil {
		t.Fatalf("resolveSessionID(%q) error = %v", "12345678", err)
	}
	if resolved != fullID {
		t.Errorf("resolveSessionID(%q) = %q, want %q", "12345678", resolved, fullID)
	}

	// Test: Full UUID should resolve to itself
	resolved, err = resolveSessionID(ctx, store, fullID)
	if err != nil {
		t.Fatalf("resolveSessionID(%q) error = %v", fullID, err)
	}
	if resolved != fullID {
		t.Errorf("resolveSessionID(%q) = %q, want %q", fullID, resolved, fullID)
	}
}

func TestHistoryCmd_SessionResolution_FullUUID_NoFallback(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a session
	existingID := "abcd1234-5678-90ab-cdef-ghijklmnopqr"
	session := &storage.Session{
		SessionID:       existingID,
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/home/user",
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Test: Non-existent full UUID should NOT fall back to prefix matching
	// Even though "abcd1234" prefix exists, the full UUID lookup should fail
	nonExistentFullID := "abcd1234-0000-0000-0000-000000000000"
	_, err := resolveSessionID(ctx, store, nonExistentFullID)
	if err == nil {
		t.Error("resolveSessionID() should return error for non-existent full UUID")
	}
	if !errors.Is(err, storage.ErrSessionNotFound) {
		t.Errorf("resolveSessionID() error = %v, want ErrSessionNotFound", err)
	}
}

func setupHistoryStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAI_HOME", root)
	paths := config.DefaultPaths()
	store, err := storage.NewSQLiteStore(paths.DatabaseFile())
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	return store
}

func createSession(t *testing.T, store *storage.SQLiteStore, id string) {
	t.Helper()
	ctx := context.Background()
	session := &storage.Session{
		SessionID:       id,
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/home/user",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
}

func createCommand(t *testing.T, store *storage.SQLiteStore, cmd storage.Command) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateCommand(ctx, &cmd); err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}
}

func TestRunHistory_JSON_Global(t *testing.T) {
	store := setupHistoryStore(t)
	defer store.Close()

	createSession(t, store, "sess-1")
	createSession(t, store, "sess-2")

	createCommand(t, store, storage.Command{
		CommandID:     "cmd-1",
		SessionID:     "sess-1",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
		Command:       "git status",
	})
	createCommand(t, store, storage.Command{
		CommandID:     "cmd-2",
		SessionID:     "sess-2",
		TSStartUnixMs: 2000,
		CWD:           "/work",
		Command:       "ls -la",
	})

	withHistoryGlobals(t, historyGlobals{limit: 20, global: true, format: "json"})

	output := captureStdout(t, func() {
		if err := runHistory(historyCmd, nil); err != nil {
			t.Fatalf("runHistory error: %v", err)
		}
	})

	var out []historyOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	if out[0].Text != "ls -la" {
		t.Fatalf("expected most recent command first, got %q", out[0].Text)
	}
	if out[0].Source != "global" {
		t.Fatalf("expected source global, got %q", out[0].Source)
	}
}

func TestRunHistory_JSON_SessionDefault(t *testing.T) {
	store := setupHistoryStore(t)
	defer store.Close()

	createSession(t, store, "sess-a")
	createSession(t, store, "sess-b")

	createCommand(t, store, storage.Command{
		CommandID:     "cmd-a",
		SessionID:     "sess-a",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
		Command:       "git status",
	})
	createCommand(t, store, storage.Command{
		CommandID:     "cmd-b",
		SessionID:     "sess-b",
		TSStartUnixMs: 2000,
		CWD:           "/tmp",
		Command:       "ls",
	})

	t.Setenv("CLAI_SESSION_ID", "sess-a")
	withHistoryGlobals(t, historyGlobals{limit: 20, global: false, format: "json"})

	output := captureStdout(t, func() {
		if err := runHistory(historyCmd, nil); err != nil {
			t.Fatalf("runHistory error: %v", err)
		}
	})

	var out []historyOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	if out[0].Text != "git status" {
		t.Fatalf("expected session command, got %q", out[0].Text)
	}
	if out[0].Source != "session" {
		t.Fatalf("expected source session, got %q", out[0].Source)
	}
}

func TestRunHistory_StatusFilterFailure(t *testing.T) {
	store := setupHistoryStore(t)
	defer store.Close()

	createSession(t, store, "sess-1")

	ok := true
	bad := false
	createCommand(t, store, storage.Command{
		CommandID:     "cmd-ok",
		SessionID:     "sess-1",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
		Command:       "echo ok",
		IsSuccess:     &ok,
		ExitCode:      intPtr(0),
	})
	createCommand(t, store, storage.Command{
		CommandID:     "cmd-bad",
		SessionID:     "sess-1",
		TSStartUnixMs: 2000,
		CWD:           "/tmp",
		Command:       "false",
		IsSuccess:     &bad,
		ExitCode:      intPtr(1),
	})

	t.Setenv("CLAI_SESSION_ID", "sess-1")
	withHistoryGlobals(t, historyGlobals{limit: 20, status: "failure", format: "json"})

	output := captureStdout(t, func() {
		if err := runHistory(historyCmd, nil); err != nil {
			t.Fatalf("runHistory error: %v", err)
		}
	})

	var out []historyOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	if out[0].Text != "false" {
		t.Fatalf("expected failure command, got %q", out[0].Text)
	}
}

func TestRunHistory_JSON_CWDSource(t *testing.T) {
	store := setupHistoryStore(t)
	defer store.Close()

	createSession(t, store, "sess-1")

	createCommand(t, store, storage.Command{
		CommandID:     "cmd-1",
		SessionID:     "sess-1",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
		Command:       "ls",
	})
	createCommand(t, store, storage.Command{
		CommandID:     "cmd-2",
		SessionID:     "sess-1",
		TSStartUnixMs: 2000,
		CWD:           "/work",
		Command:       "pwd",
	})

	t.Setenv("CLAI_SESSION_ID", "")
	withHistoryGlobals(t, historyGlobals{limit: 20, cwd: "/tmp", format: "json"})

	output := captureStdout(t, func() {
		if err := runHistory(historyCmd, nil); err != nil {
			t.Fatalf("runHistory error: %v", err)
		}
	})

	var out []historyOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	if out[0].Source != "cwd" {
		t.Fatalf("expected source cwd, got %q", out[0].Source)
	}
}

func intPtr(v int) *int {
	return &v
}
