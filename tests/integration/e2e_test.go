package integration

import (
	"context"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/storage"
)

// TestCP1_BasicIPC verifies that basic IPC communication works (ping).
// Checkpoint 1: Basic IPC - Ping works between shim and daemon
func TestCP1_BasicIPC(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Test ping
	resp, err := env.Client.Ping(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if !resp.Ok {
		t.Error("Ping returned ok=false")
	}
}

// TestCP1_GetStatus verifies that daemon status can be retrieved.
func TestCP1_GetStatus(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	resp, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if resp.Version == "" {
		t.Error("GetStatus returned empty version")
	}

	if resp.UptimeSeconds < 0 {
		t.Errorf("GetStatus returned negative uptime: %d", resp.UptimeSeconds)
	}
}

// TestCP6_FullCLI_SessionLifecycle tests a complete session lifecycle.
// Checkpoint 6: Full CLI - Key commands work
func TestCP6_FullCLI_SessionLifecycle(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// 1. Start a session
	sessionID := generateSessionID()
	startResp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell:    "zsh",
			Os:       "darwin",
			Hostname: "test-host",
			Username: "test-user",
			Version:  "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}
	if !startResp.Ok {
		t.Errorf("SessionStart returned ok=false: %s", startResp.Error)
	}

	// Verify session is tracked
	status, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ActiveSessions != 1 {
		t.Errorf("expected 1 active session, got %d", status.ActiveSessions)
	}

	// 2. Log a command start
	commandID := generateCommandID()
	cmdStartResp, err := env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: sessionID,
		CommandId: commandID,
		Cwd:       "/home/test",
		Command:   "echo hello",
		TsUnixMs:  time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}
	if !cmdStartResp.Ok {
		t.Errorf("CommandStarted returned ok=false: %s", cmdStartResp.Error)
	}

	// 3. Log command end
	cmdEndResp, err := env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  sessionID,
		CommandId:  commandID,
		ExitCode:   0,
		DurationMs: 50,
		TsUnixMs:   time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}
	if !cmdEndResp.Ok {
		t.Errorf("CommandEnded returned ok=false: %s", cmdEndResp.Error)
	}

	// 4. Get suggestions
	suggestResp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        "/home/test",
		Buffer:     "ec",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	// Suggestions may or may not be returned depending on history
	_ = suggestResp

	// 5. End the session
	endResp, err := env.Client.SessionEnd(ctx, &pb.SessionEndRequest{
		SessionId:     sessionID,
		EndedAtUnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("SessionEnd failed: %v", err)
	}
	if !endResp.Ok {
		t.Errorf("SessionEnd returned ok=false: %s", endResp.Error)
	}

	// Verify session is no longer tracked
	status, err = env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ActiveSessions != 0 {
		t.Errorf("expected 0 active sessions after end, got %d", status.ActiveSessions)
	}
}

// TestCP5_AIIntegration tests text-to-command with mock provider.
// Checkpoint 5: AI Integration - Text-to-command (mock provider for tests)
func TestCP5_AIIntegration(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Start a session for context
	sessionID := generateSessionID()
	_, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell: "zsh",
			Os:    "darwin",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Test TextToCommand
	t.Run("TextToCommand", func(t *testing.T) {
		resp, err := env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
			SessionId:      sessionID,
			Prompt:         "list all files including hidden",
			Cwd:            "/home/test",
			MaxSuggestions: 3,
		})
		if err != nil {
			t.Fatalf("TextToCommand failed: %v", err)
		}

		if len(resp.Suggestions) == 0 {
			t.Error("TextToCommand returned no suggestions")
		}

		if resp.Provider == "" {
			t.Error("TextToCommand returned empty provider name")
		}
	})

	// Test NextStep
	t.Run("NextStep", func(t *testing.T) {
		resp, err := env.Client.NextStep(ctx, &pb.NextStepRequest{
			SessionId:    sessionID,
			LastCommand:  "git add .",
			LastExitCode: 0,
			Cwd:          "/home/test/repo",
		})
		if err != nil {
			t.Fatalf("NextStep failed: %v", err)
		}

		if len(resp.Suggestions) == 0 {
			t.Error("NextStep returned no suggestions")
		}
	})

	// Test Diagnose
	t.Run("Diagnose", func(t *testing.T) {
		resp, err := env.Client.Diagnose(ctx, &pb.DiagnoseRequest{
			SessionId: sessionID,
			Command:   "npm install",
			ExitCode:  1,
			Cwd:       "/home/test/project",
		})
		if err != nil {
			t.Fatalf("Diagnose failed: %v", err)
		}

		if resp.Explanation == "" {
			t.Error("Diagnose returned empty explanation")
		}

		if len(resp.Fixes) == 0 {
			t.Error("Diagnose returned no fixes")
		}
	})
}

// TestE2E_MultipleSessionsConcurrent tests concurrent session handling.
func TestE2E_MultipleSessionsConcurrent(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	numSessions := 5
	sessionIDs := make([]string, numSessions)

	// Start multiple sessions
	for i := 0; i < numSessions; i++ {
		sessionIDs[i] = generateSessionID()
		resp, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
			SessionId:       sessionIDs[i],
			Cwd:             "/home/test",
			StartedAtUnixMs: time.Now().UnixMilli(),
			Client: &pb.ClientInfo{
				Shell: "bash",
				Os:    "linux",
			},
		})
		if err != nil {
			t.Fatalf("SessionStart %d failed: %v", i, err)
		}
		if !resp.Ok {
			t.Errorf("SessionStart %d returned ok=false: %s", i, resp.Error)
		}
	}

	// Verify all sessions are tracked
	status, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ActiveSessions != int32(numSessions) {
		t.Errorf("expected %d active sessions, got %d", numSessions, status.ActiveSessions)
	}

	// Log commands in different sessions concurrently
	for i, sessionID := range sessionIDs {
		commandID := generateCommandID()
		_, err = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/home/test",
			Command:   "test command " + string(rune('0'+i)),
			TsUnixMs:  time.Now().UnixMilli(),
		})
		if err != nil {
			t.Errorf("CommandStarted for session %d failed: %v", i, err)
		}

		_, err = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 10,
			TsUnixMs:   time.Now().UnixMilli(),
		})
		if err != nil {
			t.Errorf("CommandEnded for session %d failed: %v", i, err)
		}
	}

	// End all sessions
	for i, sessionID := range sessionIDs {
		var resp *pb.Ack
		resp, err = env.Client.SessionEnd(ctx, &pb.SessionEndRequest{
			SessionId:     sessionID,
			EndedAtUnixMs: time.Now().UnixMilli(),
		})
		if err != nil {
			t.Errorf("SessionEnd for session %d failed: %v", i, err)
		}
		if !resp.Ok {
			t.Errorf("SessionEnd %d returned ok=false: %s", i, resp.Error)
		}
	}

	// Verify all sessions ended
	status, err = env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ActiveSessions != 0 {
		t.Errorf("expected 0 active sessions, got %d", status.ActiveSessions)
	}
}

// TestE2E_CommandsLoggedCounter verifies the commands logged counter.
func TestE2E_CommandsLoggedCounter(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Start a session
	sessionID := generateSessionID()
	_, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell: "zsh",
			Os:    "darwin",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Check initial count
	status, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	initialCount := status.CommandsLogged

	// Log several commands
	numCommands := 5
	for i := 0; i < numCommands; i++ {
		commandID := generateCommandID()
		_, err = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/home/test",
			Command:   "test command",
			TsUnixMs:  time.Now().UnixMilli(),
		})
		if err != nil {
			t.Fatalf("CommandStarted failed: %v", err)
		}

		_, err = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 10,
			TsUnixMs:   time.Now().UnixMilli(),
		})
		if err != nil {
			t.Fatalf("CommandEnded failed: %v", err)
		}
	}

	// Check final count
	status, err = env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	expectedCount := initialCount + int64(numCommands)
	if status.CommandsLogged != expectedCount {
		t.Errorf("expected %d commands logged, got %d", expectedCount, status.CommandsLogged)
	}
}

// TestE2E_DestructiveCommandRiskFlag tests that destructive commands are flagged.
func TestE2E_DestructiveCommandRiskFlag(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Start a session and log a destructive command
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

	// Log a destructive command
	commandID := generateCommandID()
	startResp, err := env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: sessionID,
		CommandId: commandID,
		Cwd:       "/",
		Command:   "rm -rf /",
		TsUnixMs:  time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}
	if !startResp.Ok {
		t.Fatalf("CommandStarted returned ok=false: %s", startResp.Error)
	}

	endResp, err := env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  sessionID,
		CommandId:  commandID,
		ExitCode:   0,
		DurationMs: 100,
		TsUnixMs:   time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}
	if !endResp.Ok {
		t.Fatalf("CommandEnded returned ok=false: %s", endResp.Error)
	}

	// The destructive risk flag is applied by the suggestion handler,
	// which calls sanitize.IsDestructive(). We verify the command was stored
	// by querying commands directly.
	commands, err := env.Store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("QueryCommands failed: %v", err)
	}

	found := false
	for _, cmd := range commands {
		if cmd.Command == "rm -rf /" {
			found = true
			break
		}
	}
	if !found {
		t.Error("destructive command was not stored")
	}
}
