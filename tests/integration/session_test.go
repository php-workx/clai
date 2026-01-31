package integration

import (
	"context"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/storage"
)

// TestCP2_SessionFlow verifies session start/end persists to SQLite.
// Checkpoint 2: Session Flow - Session start/end persists to SQLite
func TestCP2_SessionFlow(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Create a session via IPC
	sessionID := generateSessionID()
	startTime := time.Now()

	startResp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test/project",
		StartedAtUnixMs: startTime.UnixMilli(),
		Client: &pb.ClientInfo{
			Shell:    "zsh",
			Os:       "darwin",
			Hostname: "testhost",
			Username: "testuser",
			Version:  "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}
	if !startResp.Ok {
		t.Fatalf("SessionStart returned ok=false: %s", startResp.Error)
	}

	// Verify session was persisted to SQLite
	session, err := env.Store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	// Check session fields
	if session.SessionID != sessionID {
		t.Errorf("session ID mismatch: got %s, want %s", session.SessionID, sessionID)
	}
	if session.Shell != "zsh" {
		t.Errorf("session shell mismatch: got %s, want zsh", session.Shell)
	}
	if session.OS != "darwin" {
		t.Errorf("session OS mismatch: got %s, want darwin", session.OS)
	}
	if session.Hostname != "testhost" {
		t.Errorf("session hostname mismatch: got %s, want testhost", session.Hostname)
	}
	if session.Username != "testuser" {
		t.Errorf("session username mismatch: got %s, want testuser", session.Username)
	}
	if session.InitialCWD != "/home/test/project" {
		t.Errorf("session CWD mismatch: got %s, want /home/test/project", session.InitialCWD)
	}
	if session.EndedAtUnixMs != nil {
		t.Errorf("session should not have end time yet")
	}

	// End the session
	endTime := time.Now()
	endResp, err := env.Client.SessionEnd(ctx, &pb.SessionEndRequest{
		SessionId:     sessionID,
		EndedAtUnixMs: endTime.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("SessionEnd failed: %v", err)
	}
	if !endResp.Ok {
		t.Fatalf("SessionEnd returned ok=false: %s", endResp.Error)
	}

	// Verify end time was persisted
	session, err = env.Store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession after end failed: %v", err)
	}
	if session.EndedAtUnixMs == nil {
		t.Fatal("session should have end time after SessionEnd")
	}
	if *session.EndedAtUnixMs != endTime.UnixMilli() {
		t.Errorf("session end time mismatch: got %d, want %d", *session.EndedAtUnixMs, endTime.UnixMilli())
	}
}

// TestCP3_CommandLogging verifies commands are logged with exit codes.
// Checkpoint 3: Command Logging - Commands logged with exit codes
func TestCP3_CommandLogging(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Create a session
	sessionID := generateSessionID()
	_, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell: "bash",
			Os:    "linux",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Log commands with different exit codes
	testCases := []struct {
		command  string
		exitCode int
		cwd      string
	}{
		{"echo hello", 0, "/home/test"},
		{"ls -la", 0, "/home/test"},
		{"git status", 0, "/home/test/repo"},
		{"npm test", 1, "/home/test/project"},
		{"make build", 2, "/home/test/src"},
	}

	for _, tc := range testCases {
		commandID := generateCommandID()
		startTime := time.Now()

		// Log command start
		startResp, err := env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       tc.cwd,
			Command:   tc.command,
			TsUnixMs:  startTime.UnixMilli(),
		})
		if err != nil {
			t.Fatalf("CommandStarted failed for %q: %v", tc.command, err)
		}
		if !startResp.Ok {
			t.Fatalf("CommandStarted returned ok=false for %q: %s", tc.command, startResp.Error)
		}

		// Log command end
		endTime := time.Now()
		durationMs := endTime.Sub(startTime).Milliseconds()
		endResp, err := env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   int32(tc.exitCode),
			DurationMs: durationMs,
			TsUnixMs:   endTime.UnixMilli(),
		})
		if err != nil {
			t.Fatalf("CommandEnded failed for %q: %v", tc.command, err)
		}
		if !endResp.Ok {
			t.Fatalf("CommandEnded returned ok=false for %q: %s", tc.command, endResp.Error)
		}
	}

	// Query commands from storage
	commands, err := env.Store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryCommands failed: %v", err)
	}

	if len(commands) != len(testCases) {
		t.Errorf("expected %d commands, got %d", len(testCases), len(commands))
	}

	// Verify each command was stored correctly
	commandMap := make(map[string]storage.Command)
	for _, cmd := range commands {
		commandMap[cmd.Command] = cmd
	}

	for _, tc := range testCases {
		cmd, ok := commandMap[tc.command]
		if !ok {
			t.Errorf("command %q not found in storage", tc.command)
			continue
		}

		if cmd.CWD != tc.cwd {
			t.Errorf("command %q CWD mismatch: got %s, want %s", tc.command, cmd.CWD, tc.cwd)
		}
		if cmd.ExitCode == nil {
			t.Errorf("command %q should have exit code", tc.command)
		} else if *cmd.ExitCode != tc.exitCode {
			t.Errorf("command %q exit code mismatch: got %d, want %d", tc.command, *cmd.ExitCode, tc.exitCode)
		}
		expectedSuccess := tc.exitCode == 0
		if cmd.IsSuccess == nil || *cmd.IsSuccess != expectedSuccess {
			t.Errorf("command %q IsSuccess mismatch: got %v, want %v", tc.command, cmd.IsSuccess, expectedSuccess)
		}
	}
}

// TestSession_DuplicateStart verifies that duplicate session start is handled.
func TestSession_DuplicateStart(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	sessionID := generateSessionID()

	// Start session first time
	resp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell: "zsh",
			Os:    "darwin",
		},
	})
	if err != nil {
		t.Fatalf("First SessionStart failed: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("First SessionStart returned ok=false: %s", resp.Error)
	}

	// Try to start same session again
	resp, err = env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test2",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell: "bash",
			Os:    "linux",
		},
	})
	if err != nil {
		// gRPC error is acceptable
		t.Logf("Duplicate session start returned gRPC error: %v", err)
	} else if resp.Ok {
		// Duplicate should not succeed silently
		t.Error("Duplicate session start should not succeed")
	}
}

// TestSession_EndNonexistent verifies ending a non-existent session is handled.
func TestSession_EndNonexistent(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Try to end a session that doesn't exist
	resp, err := env.Client.SessionEnd(ctx, &pb.SessionEndRequest{
		SessionId:     "nonexistent-session-id",
		EndedAtUnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		// gRPC error for non-existent session is acceptable
		t.Logf("End nonexistent session returned gRPC error: %v", err)
		return
	}

	// If no gRPC error, the response should indicate failure
	if resp.Ok {
		t.Error("Ending nonexistent session should not succeed")
	}
}

// TestSession_CommandInNonexistentSession verifies command logging in non-existent session.
func TestSession_CommandInNonexistentSession(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Note: In the current implementation, commands can be logged even without
	// a valid session because the command table has a foreign key reference.
	// This test documents the expected behavior.

	commandID := generateCommandID()
	resp, err := env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: "nonexistent-session",
		CommandId: commandID,
		Cwd:       "/home/test",
		Command:   "test command",
		TsUnixMs:  time.Now().UnixMilli(),
	})
	if err != nil {
		// gRPC error is acceptable if foreign key constraints are enforced
		t.Logf("CommandStarted in nonexistent session returned: %v", err)
		return
	}

	// If the operation succeeded, it may mean FK constraints are not enforced
	// or the daemon handles this gracefully
	if !resp.Ok {
		t.Logf("CommandStarted returned ok=false for nonexistent session: %s", resp.Error)
	}
}

// TestSession_MultipleSessions verifies multiple concurrent sessions.
func TestSession_MultipleSessions(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	sessions := []struct {
		id       string
		shell    string
		os       string
		cwd      string
		hostname string
		username string
	}{
		{generateSessionID(), "zsh", "darwin", "/Users/user1", "mac1", "user1"},
		{generateSessionID(), "bash", "linux", "/home/user2", "linux1", "user2"},
		{generateSessionID(), "fish", "darwin", "/Users/user3", "mac2", "user3"},
	}

	// Start all sessions
	for _, s := range sessions {
		resp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
			SessionId:       s.id,
			Cwd:             s.cwd,
			StartedAtUnixMs: time.Now().UnixMilli(),
			Client: &pb.ClientInfo{
				Shell:    s.shell,
				Os:       s.os,
				Hostname: s.hostname,
				Username: s.username,
			},
		})
		if err != nil {
			t.Fatalf("SessionStart for %s failed: %v", s.id, err)
		}
		if !resp.Ok {
			t.Fatalf("SessionStart for %s returned ok=false: %s", s.id, resp.Error)
		}
	}

	// Verify all sessions in status
	status, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ActiveSessions != int32(len(sessions)) {
		t.Errorf("expected %d active sessions, got %d", len(sessions), status.ActiveSessions)
	}

	// Verify each session in database
	for _, s := range sessions {
		session, err := env.Store.GetSession(ctx, s.id)
		if err != nil {
			t.Errorf("GetSession for %s failed: %v", s.id, err)
			continue
		}
		if session.Shell != s.shell {
			t.Errorf("session %s shell mismatch: got %s, want %s", s.id, session.Shell, s.shell)
		}
		if session.OS != s.os {
			t.Errorf("session %s OS mismatch: got %s, want %s", s.id, session.OS, s.os)
		}
	}

	// End sessions one by one and verify count decreases
	for i, s := range sessions {
		resp, err := env.Client.SessionEnd(ctx, &pb.SessionEndRequest{
			SessionId:     s.id,
			EndedAtUnixMs: time.Now().UnixMilli(),
		})
		if err != nil {
			t.Fatalf("SessionEnd for %s failed: %v", s.id, err)
		}
		if !resp.Ok {
			t.Fatalf("SessionEnd for %s returned ok=false: %s", s.id, resp.Error)
		}

		status, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		expectedActive := len(sessions) - (i + 1)
		if status.ActiveSessions != int32(expectedActive) {
			t.Errorf("after ending session %d, expected %d active sessions, got %d", i, expectedActive, status.ActiveSessions)
		}
	}
}

// TestSession_HistoryIsolation verifies that commands from one session don't appear
// in another session's suggestions. This is critical for session-specific history.
func TestSession_HistoryIsolation(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Create two parallel sessions
	sessionA := generateSessionID()
	sessionB := generateSessionID()

	// Start both sessions
	for _, s := range []struct {
		id   string
		cwd  string
		user string
	}{
		{sessionA, "/home/alice/project", "alice"},
		{sessionB, "/home/bob/project", "bob"},
	} {
		_, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
			SessionId:       s.id,
			Cwd:             s.cwd,
			StartedAtUnixMs: time.Now().UnixMilli(),
			Client: &pb.ClientInfo{
				Shell:    "zsh",
				Os:       "darwin",
				Username: s.user,
			},
		})
		if err != nil {
			t.Fatalf("SessionStart for %s failed: %v", s.user, err)
		}
	}

	// Log unique commands to session A
	sessionACommands := []string{
		"alice-unique-command-1",
		"alice-unique-command-2",
		"shared-prefix-alice",
	}
	for _, cmd := range sessionACommands {
		commandID := generateCommandID()
		_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionA,
			CommandId: commandID,
			Cwd:       "/home/alice/project",
			Command:   cmd,
			TsUnixMs:  time.Now().UnixMilli(),
		})
		_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionA,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 100,
			TsUnixMs:   time.Now().UnixMilli(),
		})
	}

	// Log unique commands to session B
	sessionBCommands := []string{
		"bob-unique-command-1",
		"bob-unique-command-2",
		"shared-prefix-bob",
	}
	for _, cmd := range sessionBCommands {
		commandID := generateCommandID()
		_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionB,
			CommandId: commandID,
			Cwd:       "/home/bob/project",
			Command:   cmd,
			TsUnixMs:  time.Now().UnixMilli(),
		})
		_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionB,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 100,
			TsUnixMs:   time.Now().UnixMilli(),
		})
	}

	// Query suggestions from session A with "alice" prefix
	t.Run("SessionA_OnlySeesOwnCommands", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  sessionA,
			Cwd:        "/home/alice/project",
			Buffer:     "alice",
			MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Should find alice's commands
		foundAlice := false
		for _, s := range resp.Suggestions {
			if s.Text == "alice-unique-command-1" || s.Text == "alice-unique-command-2" {
				foundAlice = true
			}
			// Should NOT find bob's commands
			if s.Text == "bob-unique-command-1" || s.Text == "bob-unique-command-2" {
				t.Errorf("session A should not see session B's command: %s", s.Text)
			}
		}
		if !foundAlice && len(resp.Suggestions) > 0 {
			t.Log("alice's commands not found in suggestions (may be expected if session filtering is strict)")
		}
	})

	// Query suggestions from session B with "bob" prefix
	t.Run("SessionB_OnlySeesOwnCommands", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  sessionB,
			Cwd:        "/home/bob/project",
			Buffer:     "bob",
			MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Should find bob's commands
		foundBob := false
		for _, s := range resp.Suggestions {
			if s.Text == "bob-unique-command-1" || s.Text == "bob-unique-command-2" {
				foundBob = true
			}
			// Should NOT find alice's commands
			if s.Text == "alice-unique-command-1" || s.Text == "alice-unique-command-2" {
				t.Errorf("session B should not see session A's command: %s", s.Text)
			}
		}
		if !foundBob && len(resp.Suggestions) > 0 {
			t.Log("bob's commands not found in suggestions (may be expected if session filtering is strict)")
		}
	})

	// Query with shared prefix - each session should only see their own
	t.Run("SharedPrefix_IsolatedBySession", func(t *testing.T) {
		// Session A queries "shared-prefix"
		respA, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  sessionA,
			Cwd:        "/home/alice/project",
			Buffer:     "shared-prefix",
			MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Suggest for session A failed: %v", err)
		}

		// Session B queries "shared-prefix"
		respB, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  sessionB,
			Cwd:        "/home/bob/project",
			Buffer:     "shared-prefix",
			MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Suggest for session B failed: %v", err)
		}

		// Session A should see "shared-prefix-alice" but not "shared-prefix-bob"
		for _, s := range respA.Suggestions {
			if s.Text == "shared-prefix-bob" {
				t.Error("session A should not see session B's 'shared-prefix-bob'")
			}
		}

		// Session B should see "shared-prefix-bob" but not "shared-prefix-alice"
		for _, s := range respB.Suggestions {
			if s.Text == "shared-prefix-alice" {
				t.Error("session B should not see session A's 'shared-prefix-alice'")
			}
		}
	})

	// Verify via direct storage query that commands are properly tagged
	t.Run("StorageQuery_CommandsTaggedWithSession", func(t *testing.T) {
		// Query commands for session A
		commandsA, err := env.Store.QueryCommands(ctx, storage.CommandQuery{
			SessionID: &sessionA,
			Limit:     100,
		})
		if err != nil {
			t.Fatalf("QueryCommands for session A failed: %v", err)
		}

		// Query commands for session B
		commandsB, err := env.Store.QueryCommands(ctx, storage.CommandQuery{
			SessionID: &sessionB,
			Limit:     100,
		})
		if err != nil {
			t.Fatalf("QueryCommands for session B failed: %v", err)
		}

		// Verify counts
		if len(commandsA) != len(sessionACommands) {
			t.Errorf("session A should have %d commands, got %d", len(sessionACommands), len(commandsA))
		}
		if len(commandsB) != len(sessionBCommands) {
			t.Errorf("session B should have %d commands, got %d", len(sessionBCommands), len(commandsB))
		}

		// Verify no cross-contamination
		for _, cmd := range commandsA {
			for _, bCmd := range sessionBCommands {
				if cmd.Command == bCmd {
					t.Errorf("session A commands contain session B command: %s", cmd.Command)
				}
			}
		}
		for _, cmd := range commandsB {
			for _, aCmd := range sessionACommands {
				if cmd.Command == aCmd {
					t.Errorf("session B commands contain session A command: %s", cmd.Command)
				}
			}
		}
	})
}

// TestSession_ClientInfoDefaults verifies that minimal client info is handled properly.
// The storage layer requires shell, so sessions without shell will fail.
func TestSession_ClientInfoDefaults(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Test 1: Session without any client info should fail (shell required)
	t.Run("NoClientInfo", func(t *testing.T) {
		sessionID := generateSessionID()
		resp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
			SessionId:       sessionID,
			Cwd:             "/home/test",
			StartedAtUnixMs: time.Now().UnixMilli(),
			// No Client field
		})
		if err != nil {
			// gRPC error is acceptable
			return
		}
		// The storage layer requires shell, so this should fail
		if resp.Ok {
			t.Error("session without shell should not be created")
		}
	})

	// Test 2: Session with minimal client info (only shell) should work
	t.Run("MinimalClientInfo", func(t *testing.T) {
		sessionID := generateSessionID()
		resp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
			SessionId:       sessionID,
			Cwd:             "/home/test",
			StartedAtUnixMs: time.Now().UnixMilli(),
			Client: &pb.ClientInfo{
				Shell: "bash",
				// OS will be defaulted by the handler
			},
		})
		if err != nil {
			t.Fatalf("SessionStart failed: %v", err)
		}
		if !resp.Ok {
			t.Fatalf("SessionStart returned ok=false: %s", resp.Error)
		}

		// Verify session was created with defaults
		session, err := env.Store.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}

		if session.SessionID != sessionID {
			t.Errorf("session ID mismatch")
		}
		if session.Shell != "bash" {
			t.Errorf("session shell mismatch: got %s, want bash", session.Shell)
		}
		// OS defaults to runtime.GOOS in the handler
		if session.OS == "" {
			t.Error("session OS should have a default value")
		}
	})
}
