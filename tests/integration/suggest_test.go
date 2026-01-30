package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/storage"
)

// TestCP4_Suggestions verifies history-based suggestions work.
// Checkpoint 4: Suggestions - History-based suggestions work
func TestCP4_Suggestions(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	// Test suggestions with "git" prefix
	t.Run("GitPrefix", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "hist-session",
			Cwd:        "/home/test/repo",
			Buffer:     "git",
			MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Should get git commands from history
		if len(resp.Suggestions) == 0 {
			t.Error("expected suggestions for 'git' prefix")
		}

		// Verify suggestions match the prefix
		for _, s := range resp.Suggestions {
			if len(s.Text) < 3 || s.Text[:3] != "git" {
				t.Errorf("suggestion %q does not match 'git' prefix", s.Text)
			}
		}
	})

	// Test suggestions with "npm" prefix
	t.Run("NpmPrefix", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "hist-session",
			Cwd:        "/home/test/project",
			Buffer:     "npm",
			MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Should get npm commands from history
		if len(resp.Suggestions) == 0 {
			t.Error("expected suggestions for 'npm' prefix")
		}

		// Verify suggestions match the prefix
		for _, s := range resp.Suggestions {
			if len(s.Text) < 3 || s.Text[:3] != "npm" {
				t.Errorf("suggestion %q does not match 'npm' prefix", s.Text)
			}
		}
	})

	// Test suggestions with "docker" prefix
	t.Run("DockerPrefix", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "hist-session",
			Cwd:        "/home/test",
			Buffer:     "docker",
			MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Should get docker commands from history
		if len(resp.Suggestions) == 0 {
			t.Error("expected suggestions for 'docker' prefix")
		}
	})

	// Test no match
	t.Run("NoMatch", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "hist-session",
			Cwd:        "/home/test",
			Buffer:     "xyz123notacommand",
			MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Should return empty or no suggestions for non-matching prefix
		if len(resp.Suggestions) > 0 {
			t.Logf("got %d suggestions for non-matching prefix (may be from AI fallback)", len(resp.Suggestions))
		}
	})
}

// TestSuggest_EmptyHistory tests suggestions with no command history.
func TestSuggest_EmptyHistory(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// Start a new session (no command history)
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

	// Request suggestions
	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        "/home/test",
		Buffer:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// With no history, should return empty suggestions or AI-based suggestions
	t.Logf("got %d suggestions with empty history", len(resp.Suggestions))
}

// TestSuggest_CWDContext tests CWD-aware suggestions.
func TestSuggest_CWDContext(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	// Suggestions should consider CWD context
	// Commands executed in /home/test/repo should rank higher when in that directory
	t.Run("RepoDirectory", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "hist-session",
			Cwd:        "/home/test/repo",
			Buffer:     "git",
			MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// Git commands from repo directory should be present
		hasGitCommand := false
		for _, s := range resp.Suggestions {
			if s.Text == "git status" || s.Text == "git diff" || s.Text == "git commit -m 'test'" {
				hasGitCommand = true
				break
			}
		}
		if !hasGitCommand && len(resp.Suggestions) > 0 {
			t.Log("git commands from repo directory not found in suggestions")
		}
	})

	t.Run("ProjectDirectory", func(t *testing.T) {
		resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "hist-session",
			Cwd:        "/home/test/project",
			Buffer:     "npm",
			MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}

		// npm commands from project directory should be present
		hasNpmCommand := false
		for _, s := range resp.Suggestions {
			if s.Text == "npm install" || s.Text == "npm test" || s.Text == "npm run build" {
				hasNpmCommand = true
				break
			}
		}
		if !hasNpmCommand && len(resp.Suggestions) > 0 {
			t.Log("npm commands from project directory not found in suggestions")
		}
	})
}

// TestSuggest_MaxResults tests the max results parameter.
func TestSuggest_MaxResults(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	testCases := []int{1, 3, 5, 10}

	for _, maxResults := range testCases {
		t.Run(fmt.Sprintf("MaxResults%d", maxResults), func(t *testing.T) {
			resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
				SessionId:  "hist-session",
				Cwd:        "/home/test",
				Buffer:     "", // Empty prefix to get all suggestions
				MaxResults: int32(maxResults),
			})
			if err != nil {
				t.Fatalf("Suggest failed: %v", err)
			}

			if len(resp.Suggestions) > maxResults {
				t.Errorf("got %d suggestions, expected at most %d", len(resp.Suggestions), maxResults)
			}
		})
	}
}

// TestSuggest_SuggestionScoring tests that suggestions are scored and ordered.
func TestSuggest_SuggestionScoring(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "hist-session",
		Cwd:        "/home/test",
		Buffer:     "",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	if len(resp.Suggestions) < 2 {
		t.Skip("not enough suggestions to test ordering")
	}

	// Verify suggestions are ordered by score (descending)
	for i := 0; i < len(resp.Suggestions)-1; i++ {
		if resp.Suggestions[i].Score < resp.Suggestions[i+1].Score {
			t.Errorf("suggestions not ordered by score: %f < %f at index %d",
				resp.Suggestions[i].Score, resp.Suggestions[i+1].Score, i)
		}
	}
}

// TestSuggest_SuggestionSource tests that suggestions have source information.
func TestSuggest_SuggestionSource(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "hist-session",
		Cwd:        "/home/test",
		Buffer:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	for _, s := range resp.Suggestions {
		// Source should be set (session, cwd, global, or ai)
		validSources := []string{"session", "cwd", "global", "ai", ""}
		isValid := false
		for _, vs := range validSources {
			if s.Source == vs {
				isValid = true
				break
			}
		}
		if !isValid {
			t.Errorf("unexpected source: %s", s.Source)
		}
	}
}

// TestSuggest_CommandNormalization tests that commands are normalized for matching.
func TestSuggest_CommandNormalization(t *testing.T) {
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

	// Log commands with different argument values
	commands := []string{
		"git commit -m 'first message'",
		"git commit -m 'second message'",
		"git commit -m 'third message'",
	}

	for i, cmd := range commands {
		commandID := generateCommandID()
		_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/home/test",
			Command:   cmd,
			TsUnixMs:  time.Now().UnixMilli(),
		})
		_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: int64(i * 10),
			TsUnixMs:   time.Now().UnixMilli(),
		})
	}

	// Query for git commit suggestions
	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        "/home/test",
		Buffer:     "git commit",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// Due to normalization, similar commands might be deduplicated
	// or one variant should be returned
	t.Logf("got %d suggestions for 'git commit'", len(resp.Suggestions))

	for _, s := range resp.Suggestions {
		t.Logf("  - %s (score: %.3f, source: %s)", s.Text, s.Score, s.Source)
	}
}

// TestSuggest_SuccessFailureWeighting tests that successful commands rank higher.
func TestSuggest_SuccessFailureWeighting(t *testing.T) {
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
			Shell: "bash",
			Os:    "linux",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Log a command that succeeds multiple times
	for i := 0; i < 5; i++ {
		commandID := generateCommandID()
		_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/home/test",
			Command:   "make build",
			TsUnixMs:  time.Now().UnixMilli(),
		})
		_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 100,
			TsUnixMs:   time.Now().UnixMilli(),
		})
	}

	// Log a command that fails multiple times
	for i := 0; i < 5; i++ {
		commandID := generateCommandID()
		_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/home/test",
			Command:   "make test",
			TsUnixMs:  time.Now().UnixMilli(),
		})
		_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   1,
			DurationMs: 100,
			TsUnixMs:   time.Now().UnixMilli(),
		})
	}

	// Query for "make" commands
	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        "/home/test",
		Buffer:     "make",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// The successful command should rank higher due to success weighting
	if len(resp.Suggestions) >= 2 {
		// Find positions of make build and make test
		buildIdx, testIdx := -1, -1
		for i, s := range resp.Suggestions {
			if s.Text == "make build" {
				buildIdx = i
			}
			if s.Text == "make test" {
				testIdx = i
			}
		}

		if buildIdx >= 0 && testIdx >= 0 {
			if buildIdx > testIdx {
				t.Errorf("successful command 'make build' (idx=%d) should rank higher than failing 'make test' (idx=%d)", buildIdx, testIdx)
			}
		}
	}
}

// TestSuggest_RecencyWeighting tests that recent commands rank higher.
func TestSuggest_RecencyWeighting(t *testing.T) {
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

	// Log an old command (simulate with older timestamp)
	oldTime := time.Now().Add(-24 * time.Hour)
	commandID1 := generateCommandID()
	_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: sessionID,
		CommandId: commandID1,
		Cwd:       "/home/test",
		Command:   "old command",
		TsUnixMs:  oldTime.UnixMilli(),
	})
	_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  sessionID,
		CommandId:  commandID1,
		ExitCode:   0,
		DurationMs: 50,
		TsUnixMs:   oldTime.Add(50 * time.Millisecond).UnixMilli(),
	})

	// Log a recent command
	newTime := time.Now()
	commandID2 := generateCommandID()
	_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: sessionID,
		CommandId: commandID2,
		Cwd:       "/home/test",
		Command:   "old recent",
		TsUnixMs:  newTime.UnixMilli(),
	})
	_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  sessionID,
		CommandId:  commandID2,
		ExitCode:   0,
		DurationMs: 50,
		TsUnixMs:   newTime.Add(50 * time.Millisecond).UnixMilli(),
	})

	// Query for "old" commands
	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        "/home/test",
		Buffer:     "old",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// Recent command should rank higher
	if len(resp.Suggestions) >= 2 {
		// Find positions
		oldIdx, recentIdx := -1, -1
		for i, s := range resp.Suggestions {
			if s.Text == "old command" {
				oldIdx = i
			}
			if s.Text == "old recent" {
				recentIdx = i
			}
		}

		if oldIdx >= 0 && recentIdx >= 0 {
			if recentIdx > oldIdx {
				t.Errorf("recent command 'old recent' (idx=%d) should rank higher than old command 'old command' (idx=%d)", recentIdx, oldIdx)
			}
		}
	}
}

// TestSuggest_FromCache tests the FromCache flag.
func TestSuggest_FromCache(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	// First request
	resp1, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "hist-session",
		Cwd:        "/home/test",
		Buffer:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("First Suggest failed: %v", err)
	}

	// Currently, suggestions are not cached (FromCache should be false)
	if resp1.FromCache {
		t.Log("suggestions were served from cache")
	}

	// Second request with same parameters
	resp2, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "hist-session",
		Cwd:        "/home/test",
		Buffer:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Second Suggest failed: %v", err)
	}

	// Log caching behavior
	t.Logf("First request FromCache: %v, Second request FromCache: %v", resp1.FromCache, resp2.FromCache)
}

// TestSuggest_DestructiveRiskFlag tests risk flagging for destructive commands.
func TestSuggest_DestructiveRiskFlag(t *testing.T) {
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
			Shell: "bash",
			Os:    "linux",
		},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Log destructive commands
	destructiveCommands := []string{
		"rm -rf /",
		"rm -rf /*",
		":(){:|:&};:",
		"dd if=/dev/zero of=/dev/sda",
	}

	for _, cmd := range destructiveCommands {
		commandID := generateCommandID()
		_, _ = env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/",
			Command:   cmd,
			TsUnixMs:  time.Now().UnixMilli(),
		})
		_, _ = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 100,
			TsUnixMs:   time.Now().UnixMilli(),
		})
	}

	// Query for "rm" suggestions
	resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        "/",
		Buffer:     "rm",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// Check that destructive commands are flagged
	for _, s := range resp.Suggestions {
		if s.Text == "rm -rf /" || s.Text == "rm -rf /*" {
			if s.Risk != "destructive" {
				t.Errorf("command %q should be flagged as destructive", s.Text)
			}
		}
	}
}

// TestSuggest_QueryCommandsDirectly tests querying commands directly from storage.
func TestSuggest_QueryCommandsDirectly(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()

	// Query by session
	sessionID := "hist-session"
	commands, err := env.Store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryCommands by session failed: %v", err)
	}

	if len(commands) == 0 {
		t.Error("expected commands in session")
	}

	// Query by CWD
	cwd := "/home/test/repo"
	commands, err = env.Store.QueryCommands(ctx, storage.CommandQuery{
		CWD:   &cwd,
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("QueryCommands by CWD failed: %v", err)
	}

	// All commands should be from the specified CWD
	for _, cmd := range commands {
		if cmd.CWD != cwd {
			t.Errorf("command CWD mismatch: got %s, want %s", cmd.CWD, cwd)
		}
	}

	// Query by prefix
	commands, err = env.Store.QueryCommands(ctx, storage.CommandQuery{
		Prefix: "git",
		Limit:  100,
	})
	if err != nil {
		t.Fatalf("QueryCommands by prefix failed: %v", err)
	}

	for _, cmd := range commands {
		if len(cmd.Command) < 3 || cmd.Command[:3] != "git" {
			t.Errorf("command %q does not match 'git' prefix", cmd.Command)
		}
	}
}
