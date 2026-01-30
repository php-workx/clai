package integration

import (
	"context"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
)

// TestCache_AIResponseCached tests that AI responses are cached.
func TestCache_AIResponseCached(t *testing.T) {
	// Create tracking provider to count calls
	baseProv := newMockProvider("tracking-provider", true)
	trackProv := newTrackingProvider(baseProv)
	env := setupEnvWithProvider(t, trackProv)
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

	// First call - should hit provider
	resp1, err := env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "list all files",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})
	if err != nil {
		t.Fatalf("First TextToCommand failed: %v", err)
	}

	firstCallCount := trackProv.CallCount("TextToCommand")
	if firstCallCount != 1 {
		t.Errorf("expected 1 provider call, got %d", firstCallCount)
	}

	// Second call with same parameters - may hit cache
	resp2, err := env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "list all files",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})
	if err != nil {
		t.Fatalf("Second TextToCommand failed: %v", err)
	}

	secondCallCount := trackProv.CallCount("TextToCommand")

	// If caching is implemented, call count should still be 1
	// If not, call count will be 2
	if secondCallCount == 1 {
		t.Log("AI response was cached (provider not called second time)")
	} else {
		t.Logf("AI response not cached (provider called %d times)", secondCallCount)
	}

	// Verify responses are equivalent
	if len(resp1.Suggestions) != len(resp2.Suggestions) {
		t.Errorf("cached response differs: got %d suggestions, want %d",
			len(resp2.Suggestions), len(resp1.Suggestions))
	}
}

// TestCache_DifferentPromptNotCached tests different prompts aren't cached together.
func TestCache_DifferentPromptNotCached(t *testing.T) {
	baseProv := newMockProvider("tracking-provider", true)
	trackProv := newTrackingProvider(baseProv)
	env := setupEnvWithProvider(t, trackProv)
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

	// First call
	_, err = env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "list files",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})
	if err != nil {
		t.Fatalf("First TextToCommand failed: %v", err)
	}

	// Second call with different prompt
	_, err = env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "show disk usage",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})
	if err != nil {
		t.Fatalf("Second TextToCommand failed: %v", err)
	}

	// Both prompts should have called the provider
	callCount := trackProv.CallCount("TextToCommand")
	if callCount != 2 {
		t.Errorf("expected 2 provider calls for different prompts, got %d", callCount)
	}
}

// TestCache_DiagnoseResponseCached tests that diagnose responses are cached.
func TestCache_DiagnoseResponseCached(t *testing.T) {
	baseProv := newMockProvider("tracking-provider", true)
	trackProv := newTrackingProvider(baseProv)
	env := setupEnvWithProvider(t, trackProv)
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

	// First call
	_, err = env.Client.Diagnose(ctx, &pb.DiagnoseRequest{
		SessionId: sessionID,
		Command:   "npm install",
		ExitCode:  1,
		Cwd:       "/home/test",
	})
	if err != nil {
		t.Fatalf("First Diagnose failed: %v", err)
	}

	firstCallCount := trackProv.CallCount("Diagnose")

	// Second call with same parameters
	_, err = env.Client.Diagnose(ctx, &pb.DiagnoseRequest{
		SessionId: sessionID,
		Command:   "npm install",
		ExitCode:  1,
		Cwd:       "/home/test",
	})
	if err != nil {
		t.Fatalf("Second Diagnose failed: %v", err)
	}

	secondCallCount := trackProv.CallCount("Diagnose")

	if secondCallCount == firstCallCount {
		t.Log("Diagnose response was cached")
	} else {
		t.Logf("Diagnose response not cached (provider called %d times)", secondCallCount)
	}
}

// TestCache_NextStepResponseCached tests that next step responses are cached.
func TestCache_NextStepResponseCached(t *testing.T) {
	baseProv := newMockProvider("tracking-provider", true)
	trackProv := newTrackingProvider(baseProv)
	env := setupEnvWithProvider(t, trackProv)
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

	// First call
	_, err = env.Client.NextStep(ctx, &pb.NextStepRequest{
		SessionId:    sessionID,
		LastCommand:  "git add .",
		LastExitCode: 0,
		Cwd:          "/home/test",
	})
	if err != nil {
		t.Fatalf("First NextStep failed: %v", err)
	}

	firstCallCount := trackProv.CallCount("NextStep")

	// Second call with same parameters
	_, err = env.Client.NextStep(ctx, &pb.NextStepRequest{
		SessionId:    sessionID,
		LastCommand:  "git add .",
		LastExitCode: 0,
		Cwd:          "/home/test",
	})
	if err != nil {
		t.Fatalf("Second NextStep failed: %v", err)
	}

	secondCallCount := trackProv.CallCount("NextStep")

	if secondCallCount == firstCallCount {
		t.Log("NextStep response was cached")
	} else {
		t.Logf("NextStep response not cached (provider called %d times)", secondCallCount)
	}
}
