package storage

import (
	"context"
	"errors"
	"testing"
)

func TestSQLiteStore_CreateSession_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	session := &Session{
		SessionID:       "test-session-1",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		Hostname:        "macbook",
		Username:        "testuser",
		InitialCWD:      "/home/testuser",
	}

	err := store.CreateSession(context.Background(), session)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Verify by reading back
	got, err := store.GetSession(context.Background(), "test-session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if got.SessionID != session.SessionID {
		t.Errorf("SessionID = %s, want %s", got.SessionID, session.SessionID)
	}
	if got.Shell != session.Shell {
		t.Errorf("Shell = %s, want %s", got.Shell, session.Shell)
	}
	if got.OS != session.OS {
		t.Errorf("OS = %s, want %s", got.OS, session.OS)
	}
	if got.Hostname != session.Hostname {
		t.Errorf("Hostname = %s, want %s", got.Hostname, session.Hostname)
	}
	if got.Username != session.Username {
		t.Errorf("Username = %s, want %s", got.Username, session.Username)
	}
	if got.InitialCWD != session.InitialCWD {
		t.Errorf("InitialCWD = %s, want %s", got.InitialCWD, session.InitialCWD)
	}
}

func TestSQLiteStore_CreateSession_MinimalFields(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	session := &Session{
		SessionID:       "minimal-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "bash",
		OS:              "linux",
		InitialCWD:      "/tmp",
		// Hostname and Username are optional
	}

	err := store.CreateSession(context.Background(), session)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	got, err := store.GetSession(context.Background(), "minimal-session")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if got.Hostname != "" {
		t.Errorf("Hostname = %s, want empty", got.Hostname)
	}
	if got.Username != "" {
		t.Errorf("Username = %s, want empty", got.Username)
	}
}

func TestSQLiteStore_CreateSession_DuplicateID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	session := &Session{
		SessionID:       "duplicate-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}

	// First create should succeed
	err := store.CreateSession(context.Background(), session)
	if err != nil {
		t.Fatalf("First CreateSession() error = %v", err)
	}

	// Second create with same ID should fail
	err = store.CreateSession(context.Background(), session)
	if err == nil {
		t.Error("Expected error for duplicate session ID")
	}
}

func TestSQLiteStore_CreateSession_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	tests := []struct {
		name    string
		session *Session
		wantErr string
	}{
		{
			name:    "nil session",
			session: nil,
			wantErr: "session cannot be nil",
		},
		{
			name: "missing session_id",
			session: &Session{
				Shell:      "zsh",
				OS:         "darwin",
				InitialCWD: "/tmp",
			},
			wantErr: "session_id is required",
		},
		{
			name: "missing shell",
			session: &Session{
				SessionID:  "test",
				OS:         "darwin",
				InitialCWD: "/tmp",
			},
			wantErr: "shell is required",
		},
		{
			name: "missing os",
			session: &Session{
				SessionID:  "test",
				Shell:      "zsh",
				InitialCWD: "/tmp",
			},
			wantErr: "os is required",
		},
		{
			name: "missing initial_cwd",
			session: &Session{
				SessionID: "test",
				Shell:     "zsh",
				OS:        "darwin",
			},
			wantErr: "initial_cwd is required",
		},
	}

	for _, tt := range tests {
		tt := tt // rebind loop variable for parallel safety
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := store.CreateSession(context.Background(), tt.session)
			if err == nil {
				t.Error("Expected error, got nil")
				return
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want containing %s", err, tt.wantErr)
			}
		})
	}
}

func TestSQLiteStore_EndSession_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session first
	session := &Session{
		SessionID:       "end-session-test",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// End session
	endTime := int64(1700001000000)
	err := store.EndSession(ctx, "end-session-test", endTime)
	if err != nil {
		t.Fatalf("EndSession() error = %v", err)
	}

	// Verify
	got, err := store.GetSession(ctx, "end-session-test")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if got.EndedAtUnixMs == nil {
		t.Error("EndedAtUnixMs is nil")
	} else if *got.EndedAtUnixMs != endTime {
		t.Errorf("EndedAtUnixMs = %d, want %d", *got.EndedAtUnixMs, endTime)
	}
}

func TestSQLiteStore_EndSession_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	err := store.EndSession(context.Background(), "nonexistent-session", 1700000000000)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("EndSession() error = %v, want ErrSessionNotFound", err)
	}
}

func TestSQLiteStore_EndSession_EmptyID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	err := store.EndSession(context.Background(), "", 1700000000000)
	if err == nil {
		t.Error("Expected error for empty session ID")
	}
}

func TestSQLiteStore_GetSession_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	session := &Session{
		SessionID:       "get-session-test",
		StartedAtUnixMs: 1700000000000,
		Shell:           "fish",
		OS:              "linux",
		Hostname:        "server1",
		Username:        "admin",
		InitialCWD:      "/root",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	got, err := store.GetSession(ctx, "get-session-test")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if got.SessionID != session.SessionID {
		t.Errorf("SessionID = %s, want %s", got.SessionID, session.SessionID)
	}
	if got.StartedAtUnixMs != session.StartedAtUnixMs {
		t.Errorf("StartedAtUnixMs = %d, want %d", got.StartedAtUnixMs, session.StartedAtUnixMs)
	}
	if got.EndedAtUnixMs != nil {
		t.Errorf("EndedAtUnixMs = %d, want nil", *got.EndedAtUnixMs)
	}
}

func TestSQLiteStore_GetSession_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetSession(context.Background(), "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession() error = %v, want ErrSessionNotFound", err)
	}
}

func TestSQLiteStore_GetSession_EmptyID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetSession(context.Background(), "")
	if err == nil {
		t.Error("Expected error for empty session ID")
	}
}

func TestSQLiteStore_Session_WithEndedAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	endTime := int64(1700001000000)

	session := &Session{
		SessionID:       "session-with-end",
		StartedAtUnixMs: 1700000000000,
		EndedAtUnixMs:   &endTime,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	got, err := store.GetSession(ctx, "session-with-end")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if got.EndedAtUnixMs == nil {
		t.Error("EndedAtUnixMs is nil")
	} else if *got.EndedAtUnixMs != endTime {
		t.Errorf("EndedAtUnixMs = %d, want %d", *got.EndedAtUnixMs, endTime)
	}
}
