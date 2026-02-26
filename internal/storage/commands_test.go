package storage

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func TestSQLiteStore_CreateCommand_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session first (foreign key constraint)
	session := &Session{
		SessionID:       "cmd-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	cmd := &Command{
		CommandID:     "test-cmd-1",
		SessionID:     "cmd-session",
		TSStartUnixMs: 1700000001000,
		CWD:           "/home/user",
		Command:       "git status",
		IsSuccess:     boolPtr(true),
	}

	err := store.CreateCommand(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}

	// Verify ID was set
	if cmd.ID == 0 {
		t.Error("Command ID was not set")
	}

	// Verify normalization happened
	if cmd.CommandNorm == "" {
		t.Error("CommandNorm was not set")
	}
	if cmd.CommandHash == "" {
		t.Error("CommandHash was not set")
	}
}

func TestSQLiteStore_CreateCommand_AutoNormalization(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session first
	session := &Session{
		SessionID:       "norm-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	cmd := &Command{
		CommandID:     "norm-cmd-1",
		SessionID:     "norm-session",
		TSStartUnixMs: 1700000001000,
		CWD:           "/home/user",
		Command:       "  Git Status  ",
		IsSuccess:     boolPtr(true),
	}

	err := store.CreateCommand(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}

	if cmd.CommandNorm != "git status" {
		t.Errorf("CommandNorm = %s, want 'git status'", cmd.CommandNorm)
	}
}

func TestSQLiteStore_CreateCommand_InvalidSessionID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	cmd := &Command{
		CommandID:     "orphan-cmd",
		SessionID:     "nonexistent-session",
		TSStartUnixMs: 1700000001000,
		CWD:           "/tmp",
		Command:       "ls",
		IsSuccess:     boolPtr(true),
	}

	err := store.CreateCommand(context.Background(), cmd)
	if err == nil {
		t.Error("Expected error for invalid session_id")
	}
	// The error should mention the session doesn't exist
	if err != nil && !contains(err.Error(), "does not exist") && !contains(err.Error(), "FOREIGN KEY") {
		t.Logf("Got error: %v (expected foreign key error)", err)
	}
}

func TestSQLiteStore_CreateCommand_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	tests := []struct {
		name    string
		cmd     *Command
		wantErr string
	}{
		{
			name:    "nil command",
			cmd:     nil,
			wantErr: "command cannot be nil",
		},
		{
			name: "missing command_id",
			cmd: &Command{
				SessionID: "test",
				CWD:       "/tmp",
				Command:   "ls",
			},
			wantErr: "command_id is required",
		},
		{
			name: "missing session_id",
			cmd: &Command{
				CommandID: "test",
				CWD:       "/tmp",
				Command:   "ls",
			},
			wantErr: "session_id is required",
		},
		{
			name: "missing cwd",
			cmd: &Command{
				CommandID: "test",
				SessionID: "test",
				Command:   "ls",
			},
			wantErr: "cwd is required",
		},
		{
			name: "missing command",
			cmd: &Command{
				CommandID: "test",
				SessionID: "test",
				CWD:       "/tmp",
			},
			wantErr: "command is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.CreateCommand(context.Background(), tt.cmd)
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

func TestSQLiteStore_UpdateCommandEnd_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session first
	session := &Session{
		SessionID:       "update-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create command
	cmd := &Command{
		CommandID:     "update-cmd",
		SessionID:     "update-session",
		TSStartUnixMs: 1700000001000,
		CWD:           "/tmp",
		Command:       "make build",
		IsSuccess:     boolPtr(true),
	}
	if err := store.CreateCommand(ctx, cmd); err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}

	// Update end
	err := store.UpdateCommandEnd(ctx, "update-cmd", 0, 1700000002000, 1000)
	if err != nil {
		t.Fatalf("UpdateCommandEnd() error = %v", err)
	}

	// Verify
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: strPtr("update-session"),
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(cmds))
	}

	if cmds[0].TSEndUnixMs == nil || *cmds[0].TSEndUnixMs != 1700000002000 {
		t.Errorf("TSEndUnixMs = %v, want 1700000002000", cmds[0].TSEndUnixMs)
	}
	if cmds[0].DurationMs == nil || *cmds[0].DurationMs != 1000 {
		t.Errorf("DurationMs = %v, want 1000", cmds[0].DurationMs)
	}
	if cmds[0].ExitCode == nil || *cmds[0].ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", cmds[0].ExitCode)
	}
	if cmds[0].IsSuccess == nil || !*cmds[0].IsSuccess {
		t.Error("IsSuccess = false or nil, want true")
	}
}

func TestSQLiteStore_UpdateCommandEnd_FailedCommand(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session first
	session := &Session{
		SessionID:       "fail-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create command
	cmd := &Command{
		CommandID:     "fail-cmd",
		SessionID:     "fail-session",
		TSStartUnixMs: 1700000001000,
		CWD:           "/tmp",
		Command:       "false",
		IsSuccess:     boolPtr(true), // Initial state
	}
	if err := store.CreateCommand(ctx, cmd); err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}

	// Update with non-zero exit code
	err := store.UpdateCommandEnd(ctx, "fail-cmd", 1, 1700000002000, 100)
	if err != nil {
		t.Fatalf("UpdateCommandEnd() error = %v", err)
	}

	// Verify
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: strPtr("fail-session"),
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(cmds))
	}

	if cmds[0].ExitCode == nil || *cmds[0].ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", cmds[0].ExitCode)
	}
	if cmds[0].IsSuccess == nil || *cmds[0].IsSuccess {
		t.Error("IsSuccess = true or nil, want false")
	}
}

func TestSQLiteStore_UpdateCommandEnd_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	err := store.UpdateCommandEnd(context.Background(), "nonexistent", 0, 1700000002000, 1000)
	if !errors.Is(err, ErrCommandNotFound) {
		t.Errorf("UpdateCommandEnd() error = %v, want ErrCommandNotFound", err)
	}
}

func TestSQLiteStore_QueryCommands_BySession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create two sessions
	for _, sid := range []string{"session-a", "session-b"} {
		session := &Session{
			SessionID:       sid,
			StartedAtUnixMs: 1700000000000,
			Shell:           "zsh",
			OS:              "darwin",
			InitialCWD:      "/tmp",
		}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
	}

	// Create commands in both sessions
	commands := []struct {
		id        string
		sessionID string
		cmd       string
	}{
		{"cmd-a1", "session-a", "ls"},
		{"cmd-a2", "session-a", "pwd"},
		{"cmd-b1", "session-b", "date"},
	}

	for _, c := range commands {
		cmd := &Command{
			CommandID:     c.id,
			SessionID:     c.sessionID,
			TSStartUnixMs: 1700000001000,
			CWD:           "/tmp",
			Command:       c.cmd,
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	// Query session-a only
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: strPtr("session-a"),
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 2 {
		t.Errorf("Got %d commands, want 2", len(cmds))
	}
}

func TestSQLiteStore_QueryCommands_ByCWD(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "cwd-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands in different directories
	commands := []struct {
		id  string
		cwd string
	}{
		{"cwd-cmd-1", "/home/user"},
		{"cwd-cmd-2", "/home/user"},
		{"cwd-cmd-3", "/tmp"},
	}

	for _, c := range commands {
		cmd := &Command{
			CommandID:     c.id,
			SessionID:     "cwd-session",
			TSStartUnixMs: 1700000001000,
			CWD:           c.cwd,
			Command:       "ls",
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	// Query by CWD
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		CWD: strPtr("/home/user"),
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 2 {
		t.Errorf("Got %d commands, want 2", len(cmds))
	}
}

func TestSQLiteStore_QueryCommands_ByPrefix(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "prefix-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands with different prefixes
	commands := []string{
		"git status",
		"git push",
		"go build",
		"grep pattern",
	}

	for i, c := range commands {
		cmd := &Command{
			CommandID:     generateTestCommandID(i),
			SessionID:     "prefix-session",
			TSStartUnixMs: int64(1700000001000 + i),
			CWD:           "/tmp",
			Command:       c,
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	// Query by prefix
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		Prefix: "git",
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 2 {
		t.Errorf("Got %d commands, want 2", len(cmds))
	}
}

func TestSQLiteStore_QueryCommands_WithOffset(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "offset-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create 10 commands (newest first after ORDER BY ts DESC)
	for i := 0; i < 10; i++ {
		cmd := &Command{
			CommandID:     generateTestCommandID(100 + i),
			SessionID:     "offset-session",
			TSStartUnixMs: int64(1700000001000 + i),
			CWD:           "/tmp",
			Command:       "cmd-" + string(rune('a'+i)),
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	sid := "offset-session"

	// Page 1: first 3 items (most recent)
	page1, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: &sid,
		Limit:     3,
		Offset:    0,
	})
	if err != nil {
		t.Fatalf("Page 1 error: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("Page 1: got %d, want 3", len(page1))
	}

	// Page 2: next 3 items
	page2, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: &sid,
		Limit:     3,
		Offset:    3,
	})
	if err != nil {
		t.Fatalf("Page 2 error: %v", err)
	}
	if len(page2) != 3 {
		t.Fatalf("Page 2: got %d, want 3", len(page2))
	}

	// Pages should not overlap
	if page1[0].Command == page2[0].Command {
		t.Errorf("Page 1 and 2 overlap: both start with %q", page1[0].Command)
	}

	// Page 1 should be newer than page 2 (DESC order)
	if page1[0].TSStartUnixMs < page2[0].TSStartUnixMs {
		t.Errorf("Page 1 should be newer: page1[0].ts=%d, page2[0].ts=%d",
			page1[0].TSStartUnixMs, page2[0].TSStartUnixMs)
	}

	// Offset beyond data returns empty
	empty, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: &sid,
		Limit:     3,
		Offset:    100,
	})
	if err != nil {
		t.Fatalf("Empty page error: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected empty page, got %d", len(empty))
	}
}

func TestSQLiteStore_QueryCommands_EmptyResult(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	cmds, err := store.QueryCommands(context.Background(), CommandQuery{
		SessionID: strPtr("nonexistent"),
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 0 {
		t.Errorf("Got %d commands, want 0", len(cmds))
	}
}

func TestSQLiteStore_QueryCommands_Limit(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "limit-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create 10 commands
	for i := 0; i < 10; i++ {
		cmd := &Command{
			CommandID:     generateTestCommandID(i + 100),
			SessionID:     "limit-session",
			TSStartUnixMs: int64(1700000001000 + i),
			CWD:           "/tmp",
			Command:       "ls",
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	// Query with limit
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: strPtr("limit-session"),
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 5 {
		t.Errorf("Got %d commands, want 5", len(cmds))
	}
}

func TestSQLiteStore_QueryCommands_OrderByRecent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "order-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands with different timestamps
	timestamps := []int64{1700000001000, 1700000003000, 1700000002000}
	for i, ts := range timestamps {
		cmd := &Command{
			CommandID:     generateTestCommandID(i + 200),
			SessionID:     "order-session",
			TSStartUnixMs: ts,
			CWD:           "/tmp",
			Command:       "cmd",
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	// Query should return in descending order
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID: strPtr("order-session"),
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 3 {
		t.Fatalf("Got %d commands, want 3", len(cmds))
	}

	// Should be ordered: 1700000003000, 1700000002000, 1700000001000
	expected := []int64{1700000003000, 1700000002000, 1700000001000}
	for i, cmd := range cmds {
		if cmd.TSStartUnixMs != expected[i] {
			t.Errorf("cmds[%d].TSStartUnixMs = %d, want %d", i, cmd.TSStartUnixMs, expected[i])
		}
	}
}

// Test command normalization

func TestNormalizeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple command",
			input:    "ls",
			expected: "ls",
		},
		{
			name:     "command with flags",
			input:    "ls -la",
			expected: "ls -la",
		},
		{
			name:     "uppercase to lowercase",
			input:    "Git Status",
			expected: "git status",
		},
		{
			name:     "trim whitespace",
			input:    "  ls -la  ",
			expected: "ls -la",
		},
		{
			name:     "path normalization",
			input:    "cat /home/user/file.txt",
			expected: "cat <path>",
		},
		{
			name:     "home path normalization",
			input:    "cd ~/projects",
			expected: "cd <path>",
		},
		{
			name:     "url normalization",
			input:    "curl https://example.com/api",
			expected: "curl <url>",
		},
		{
			name:     "number normalization",
			input:    "kill 12345",
			expected: "kill <num>",
		},
		{
			name:     "complex command",
			input:    "docker run -d -p 8080:80 nginx",
			expected: "docker run -d -p 8080:80 nginx", // Port mappings like 8080:80 are kept as-is
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		tt := tt // rebind loop variable for parallel safety
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeCommand(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeCommand(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHashCommand(t *testing.T) {
	t.Parallel()

	// Same input should give same hash
	hash1 := HashCommand("git status")
	hash2 := HashCommand("git status")
	if hash1 != hash2 {
		t.Error("Same input gave different hashes")
	}

	// Different input should give different hash
	hash3 := HashCommand("git push")
	if hash1 == hash3 {
		t.Error("Different input gave same hash")
	}

	// Hash should be hex encoded
	if len(hash1) != 64 { // SHA256 = 32 bytes = 64 hex chars
		t.Errorf("Hash length = %d, want 64", len(hash1))
	}
}

func generateTestCommandID(n int) string {
	return "test-cmd-" + strconv.Itoa(n)
}

func boolPtr(b bool) *bool {
	return &b
}

func TestSQLiteStore_QueryCommands_SuccessOnly(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "success-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands with different success states
	commands := []struct {
		id        string
		cmd       string
		exitCode  int
		isSuccess bool
	}{
		{"success-cmd-1", "ls", 0, true},
		{"success-cmd-2", "pwd", 0, true},
		{"success-cmd-3", "nonexistent-cmd", 127, false},
		{"success-cmd-4", "false", 1, false},
	}

	for _, c := range commands {
		cmd := &Command{
			CommandID:     c.id,
			SessionID:     "success-session",
			TSStartUnixMs: 1700000001000,
			CWD:           "/tmp",
			Command:       c.cmd,
			IsSuccess:     boolPtr(c.isSuccess),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
		// Update with exit code
		if err := store.UpdateCommandEnd(ctx, c.id, c.exitCode, 1700000002000, 100); err != nil {
			t.Fatalf("UpdateCommandEnd() error = %v", err)
		}
	}

	// Query successful commands only
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID:   strPtr("success-session"),
		SuccessOnly: true,
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 2 {
		t.Errorf("Got %d commands, want 2 (success only)", len(cmds))
	}

	// Verify all returned commands are successful
	for _, cmd := range cmds {
		if cmd.IsSuccess == nil || !*cmd.IsSuccess {
			t.Errorf("Expected successful command, got IsSuccess=%v", cmd.IsSuccess)
		}
	}
}

func TestSQLiteStore_QueryCommands_FailureOnly(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "failure-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands with different success states
	commands := []struct {
		id        string
		cmd       string
		exitCode  int
		isSuccess bool
	}{
		{"failure-cmd-1", "ls", 0, true},
		{"failure-cmd-2", "pwd", 0, true},
		{"failure-cmd-3", "nonexistent-cmd", 127, false},
		{"failure-cmd-4", "false", 1, false},
		{"failure-cmd-5", "permission-denied", 126, false},
	}

	for _, c := range commands {
		cmd := &Command{
			CommandID:     c.id,
			SessionID:     "failure-session",
			TSStartUnixMs: 1700000001000,
			CWD:           "/tmp",
			Command:       c.cmd,
			IsSuccess:     boolPtr(c.isSuccess),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
		// Update with exit code
		if err := store.UpdateCommandEnd(ctx, c.id, c.exitCode, 1700000002000, 100); err != nil {
			t.Fatalf("UpdateCommandEnd() error = %v", err)
		}
	}

	// Query failed commands only
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		SessionID:   strPtr("failure-session"),
		FailureOnly: true,
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 3 {
		t.Errorf("Got %d commands, want 3 (failure only)", len(cmds))
	}

	// Verify all returned commands are failures
	for _, cmd := range cmds {
		if cmd.IsSuccess == nil || *cmd.IsSuccess {
			t.Errorf("Expected failed command, got IsSuccess=%v", cmd.IsSuccess)
		}
	}
}

// ============================================================================
// QueryCommands Substring tests
// ============================================================================

func TestSQLiteStore_QueryCommands_Substring(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "substr-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands
	commands := []string{
		"git status",
		"git log --oneline",
		"echo hello",
		"docker build .",
		"go build ./...",
	}

	for i, c := range commands {
		cmd := &Command{
			CommandID:     generateTestCommandID(300 + i),
			SessionID:     "substr-session",
			TSStartUnixMs: int64(1700000001000 + i),
			CWD:           "/tmp",
			Command:       c,
			IsSuccess:     boolPtr(true),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	// Substring "build" should match "docker build ." and "go build ./..."
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		Substring: "build",
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 2 {
		t.Errorf("Got %d commands for substring 'build', want 2", len(cmds))
	}

	// Substring "log" should match "git log --oneline"
	cmds2, err := store.QueryCommands(ctx, CommandQuery{
		Substring: "log",
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds2) != 1 {
		t.Errorf("Got %d commands for substring 'log', want 1", len(cmds2))
	}

	// Substring with no matches
	cmds3, err := store.QueryCommands(ctx, CommandQuery{
		Substring: "nonexistent-xyz",
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds3) != 0 {
		t.Errorf("Got %d commands for non-matching substring, want 0", len(cmds3))
	}
}

// ============================================================================
// QueryHistoryCommands tests
// ============================================================================

func seedHistoryTestData(t *testing.T, store *SQLiteStore) {
	t.Helper()
	ctx := context.Background()

	// Create sessions
	for _, sid := range []string{"hist-sess-1", "hist-sess-2"} {
		session := &Session{
			SessionID:       sid,
			StartedAtUnixMs: 1700000000000,
			Shell:           "zsh",
			OS:              "darwin",
			InitialCWD:      "/tmp",
		}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
	}

	// Commands: "git status" twice in sess-1, "git log" once in sess-1, "ls -la" in sess-2, "echo hello" in sess-2
	commands := []*Command{
		{
			CommandID:     "hist-cmd-1",
			SessionID:     "hist-sess-1",
			TSStartUnixMs: 1000,
			CWD:           "/tmp",
			Command:       "git status",
		},
		{
			CommandID:     "hist-cmd-2",
			SessionID:     "hist-sess-1",
			TSStartUnixMs: 2000,
			CWD:           "/tmp",
			Command:       "git log",
		},
		{
			CommandID:     "hist-cmd-3",
			SessionID:     "hist-sess-1",
			TSStartUnixMs: 3000,
			CWD:           "/tmp",
			Command:       "git status",
		},
		{
			CommandID:     "hist-cmd-4",
			SessionID:     "hist-sess-2",
			TSStartUnixMs: 4000,
			CWD:           "/home",
			Command:       "ls -la",
		},
		{
			CommandID:     "hist-cmd-5",
			SessionID:     "hist-sess-2",
			TSStartUnixMs: 5000,
			CWD:           "/home",
			Command:       "echo hello world",
		},
	}

	for _, cmd := range commands {
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand(%s) error = %v", cmd.CommandID, err)
		}
	}
}

func TestSQLiteStore_QueryHistoryCommands_Deduplication(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	seedHistoryTestData(t, store)
	ctx := context.Background()

	results, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("QueryHistoryCommands() error = %v", err)
	}

	// 4 unique commands: echo hello world, ls -la, git status, git log
	if len(results) != 4 {
		t.Errorf("Got %d results, want 4 deduplicated", len(results))
	}

	// Results should be ordered by most recent timestamp descending
	for i := 1; i < len(results); i++ {
		if results[i].TimestampMs > results[i-1].TimestampMs {
			t.Errorf("Not ordered DESC: index %d ts=%d > index %d ts=%d",
				i, results[i].TimestampMs, i-1, results[i-1].TimestampMs)
		}
	}
}

func TestSQLiteStore_QueryHistoryCommands_KeepsMostRecent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	seedHistoryTestData(t, store)
	ctx := context.Background()

	// "git status" appears at ts=1000 and ts=3000
	results, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Substring: "status",
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryHistoryCommands() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Got %d results, want 1", len(results))
	}

	if results[0].TimestampMs != 3000 {
		t.Errorf("TimestampMs = %d, want 3000 (most recent)", results[0].TimestampMs)
	}
}

func TestSQLiteStore_QueryHistoryCommands_SessionScoped(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	seedHistoryTestData(t, store)
	ctx := context.Background()

	sessID := "hist-sess-1"
	results, err := store.QueryHistoryCommands(ctx, CommandQuery{
		SessionID: &sessID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryHistoryCommands() error = %v", err)
	}

	// hist-sess-1 has: git status (deduped) + git log = 2
	if len(results) != 2 {
		t.Errorf("Got %d results, want 2", len(results))
	}
}

func TestSQLiteStore_QueryHistoryCommands_SubstringFilter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	seedHistoryTestData(t, store)
	ctx := context.Background()

	results, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Substring: "echo",
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryHistoryCommands() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Got %d results for 'echo', want 1", len(results))
	}

	if results[0].Command != "echo hello world" {
		t.Errorf("Command = %q, want 'echo hello world'", results[0].Command)
	}
}

func TestSQLiteStore_QueryHistoryCommands_Pagination(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	seedHistoryTestData(t, store)
	ctx := context.Background()

	// 4 unique commands total. Page 1: limit=2
	page1, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Limit:  2,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("Page 1 error: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("Page 1: got %d, want 2", len(page1))
	}

	// Page 2: offset=2, limit=2
	page2, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Limit:  2,
		Offset: 2,
	})
	if err != nil {
		t.Fatalf("Page 2 error: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("Page 2: got %d, want 2", len(page2))
	}

	// No overlap
	page1Cmds := make(map[string]bool)
	for _, r := range page1 {
		page1Cmds[r.Command] = true
	}
	for _, r := range page2 {
		if page1Cmds[r.Command] {
			t.Errorf("Command %q appears on both pages", r.Command)
		}
	}

	// Past end: offset=4
	page3, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Limit:  2,
		Offset: 4,
	})
	if err != nil {
		t.Fatalf("Page 3 error: %v", err)
	}
	if len(page3) != 0 {
		t.Errorf("Page 3: got %d, want 0", len(page3))
	}
}

func TestSQLiteStore_QueryHistoryCommands_EmptyResult(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	seedHistoryTestData(t, store)
	ctx := context.Background()

	results, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Substring: "zzz-nonexistent",
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryHistoryCommands() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Got %d results, want 0", len(results))
	}
}

func TestSQLiteStore_QueryHistoryCommands_DoesNotCollapseNormalizedArgs(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	session := &Session{
		SessionID:       "hist-path-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// These normalize to the same command_norm ("cd <path>") but should remain
	// distinct history items when deduplicating for pickers.
	cmds := []*Command{
		{
			CommandID:     "hist-path-cmd-1",
			SessionID:     session.SessionID,
			TSStartUnixMs: 1000,
			CWD:           "/tmp",
			Command:       "cd /tmp",
		},
		{
			CommandID:     "hist-path-cmd-2",
			SessionID:     session.SessionID,
			TSStartUnixMs: 2000,
			CWD:           "/tmp",
			Command:       "cd /Users/example/project",
		},
	}
	for _, c := range cmds {
		if err := store.CreateCommand(ctx, c); err != nil {
			t.Fatalf("CreateCommand(%s) error = %v", c.CommandID, err)
		}
	}

	results, err := store.QueryHistoryCommands(ctx, CommandQuery{
		Substring:   "cd",
		Limit:       10,
		Deduplicate: true,
	})
	if err != nil {
		t.Fatalf("QueryHistoryCommands() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Got %d results, want 2", len(results))
	}
	if results[0].Command != "cd /Users/example/project" {
		t.Errorf("results[0].Command = %q, want %q", results[0].Command, "cd /Users/example/project")
	}
	if results[1].Command != "cd /tmp" {
		t.Errorf("results[1].Command = %q, want %q", results[1].Command, "cd /tmp")
	}
}

func TestSQLiteStore_QueryCommands_StatusWithOtherFilters(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session
	session := &Session{
		SessionID:       "combo-session",
		StartedAtUnixMs: 1700000000000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Create commands with different combinations
	commands := []struct {
		id        string
		cmd       string
		cwd       string
		exitCode  int
		isSuccess bool
	}{
		{"combo-cmd-1", "git status", "/project", 0, true},
		{"combo-cmd-2", "git push", "/project", 1, false}, // git push failed
		{"combo-cmd-3", "git pull", "/project", 0, true},
		{"combo-cmd-4", "ls", "/tmp", 0, true},
		{"combo-cmd-5", "git commit", "/project", 1, false}, // git commit failed
	}

	for _, c := range commands {
		cmd := &Command{
			CommandID:     c.id,
			SessionID:     "combo-session",
			TSStartUnixMs: 1700000001000,
			CWD:           c.cwd,
			Command:       c.cmd,
			IsSuccess:     boolPtr(c.isSuccess),
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
		if err := store.UpdateCommandEnd(ctx, c.id, c.exitCode, 1700000002000, 100); err != nil {
			t.Fatalf("UpdateCommandEnd() error = %v", err)
		}
	}

	// Query: git commands that failed in /project
	cwd := "/project"
	cmds, err := store.QueryCommands(ctx, CommandQuery{
		CWD:         &cwd,
		Prefix:      "git",
		FailureOnly: true,
	})
	if err != nil {
		t.Fatalf("QueryCommands() error = %v", err)
	}

	if len(cmds) != 2 {
		t.Errorf("Got %d commands, want 2 (git failures in /project)", len(cmds))
	}
}
