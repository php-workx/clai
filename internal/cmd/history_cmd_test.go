package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/storage"
)

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{0, "0ms"},
		{100, "100ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{5000, "5.0s"},
		{59000, "59.0s"},
		{60000, "1m0s"},
		{90000, "1m30s"},
		{3600000, "60m0s"},
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

// resolveSessionID mimics the history command's session resolution logic
func resolveSessionID(ctx context.Context, store *storage.SQLiteStore, sessionID string) (string, error) {
	session, err := store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			// Try prefix match for short IDs (< 36 chars, full UUID length)
			if len(sessionID) < 36 {
				session, err = store.GetSessionByPrefix(ctx, sessionID)
				if err != nil {
					return "", err
				}
				return session.SessionID, nil
			}
			return "", storage.ErrSessionNotFound
		}
		return "", err
	}
	return session.SessionID, nil
}

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
