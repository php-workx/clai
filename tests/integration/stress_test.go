package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
)

// stressRunSessionCommands runs commands concurrently within a single session.
func stressRunSessionCommands(ctx context.Context, client pb.ClaiServiceClient, sessionID string, numCommands int, errors chan<- error, t *testing.T) {
	var cmdWg sync.WaitGroup
	for j := 0; j < numCommands; j++ {
		cmdWg.Add(1)
		go func(cmdNum int) {
			defer cmdWg.Done()

			commandID := generateCommandID()

			startResp, startErr := client.CommandStarted(ctx, &pb.CommandStartRequest{
				SessionId: sessionID,
				CommandId: commandID,
				Cwd:       "/home/test",
				Command:   "test command",
				TsUnixMs:  time.Now().UnixMilli(),
			})
			if startErr != nil {
				errors <- startErr
				return
			}
			if !startResp.Ok {
				t.Logf("command %d start failed: %s", cmdNum, startResp.Error)
				return
			}

			endResp, endErr := client.CommandEnded(ctx, &pb.CommandEndRequest{
				SessionId:  sessionID,
				CommandId:  commandID,
				ExitCode:   0,
				DurationMs: 10,
				TsUnixMs:   time.Now().UnixMilli(),
			})
			if endErr != nil {
				errors <- endErr
				return
			}
			if !endResp.Ok {
				t.Logf("command %d end failed: %s", cmdNum, endResp.Error)
			}
		}(j)
	}
	cmdWg.Wait()
}

// TestStress_ConcurrentSessions tests concurrent session handling with goroutines.
func TestStress_ConcurrentSessions(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	numSessions := 10
	numCommandsPerSession := 5

	var wg sync.WaitGroup
	errors := make(chan error, numSessions*numCommandsPerSession)

	// Start multiple sessions concurrently
	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(sessionNum int) {
			defer wg.Done()

			sessionID := generateSessionID()

			// Start session
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
				errors <- err
				return
			}
			if !resp.Ok {
				t.Logf("session %d start failed: %s", sessionNum, resp.Error)
				return
			}

			// Log multiple commands concurrently within this session
			stressRunSessionCommands(ctx, env.Client, sessionID, numCommandsPerSession, errors, t)

			// End session
			endResp, err := env.Client.SessionEnd(ctx, &pb.SessionEndRequest{
				SessionId:     sessionID,
				EndedAtUnixMs: time.Now().UnixMilli(),
			})
			if err != nil {
				errors <- err
				return
			}
			if !endResp.Ok {
				t.Logf("session %d end failed: %s", sessionNum, endResp.Error)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	var errCount int
	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Errorf("total concurrent errors: %d", errCount)
	}
}

// TestStress_RapidPing tests rapid ping requests.
func TestStress_RapidPing(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	numPings := 100

	var wg sync.WaitGroup
	errors := make(chan error, numPings)

	for i := 0; i < numPings; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := env.Client.Ping(ctx, &pb.Ack{Ok: true})
			if err != nil {
				errors <- err
				return
			}
			if !resp.Ok {
				t.Log("ping returned ok=false")
			}
		}()
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Errorf("ping error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Errorf("total ping errors: %d", errCount)
	}
}

// TestStress_ConcurrentSuggestions tests concurrent suggestion requests.
func TestStress_ConcurrentSuggestions(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()
	numRequests := 50

	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	prefixes := []string{"git", "npm", "docker", "ls", "cd", "make", "go"}

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			prefix := prefixes[reqNum%len(prefixes)]
			resp, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
				SessionId:  "hist-session",
				Cwd:        "/home/test",
				Buffer:     prefix,
				MaxResults: 5,
			})
			if err != nil {
				errors <- err
				return
			}
			// Just verify response is valid
			_ = resp
		}(i)
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Errorf("suggest error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Errorf("total suggest errors: %d", errCount)
	}
}

// TestStress_MixedOperations tests mixed operations concurrently.
func TestStress_MixedOperations(t *testing.T) {
	env := SetupTestEnvWithSuggestions(t)
	defer env.Teardown()

	ctx := context.Background()
	numOperations := 100

	var wg sync.WaitGroup
	errors := make(chan error, numOperations)

	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(opNum int) {
			defer wg.Done()

			switch opNum % 4 {
			case 0:
				// Ping
				_, err := env.Client.Ping(ctx, &pb.Ack{Ok: true})
				if err != nil {
					errors <- err
				}
			case 1:
				// GetStatus
				_, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
				if err != nil {
					errors <- err
				}
			case 2:
				// Suggest
				_, err := env.Client.Suggest(ctx, &pb.SuggestRequest{
					SessionId:  "hist-session",
					Cwd:        "/home/test",
					Buffer:     "git",
					MaxResults: 3,
				})
				if err != nil {
					errors <- err
				}
			case 3:
				// TextToCommand
				_, err := env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
					SessionId:      "hist-session",
					Prompt:         "list files",
					Cwd:            "/home/test",
					MaxSuggestions: 3,
				})
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Errorf("mixed operation error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Errorf("total mixed operation errors: %d", errCount)
	}
}

// TestStress_HighCommandVolume tests high volume command logging.
func TestStress_HighCommandVolume(t *testing.T) {
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

	// Log many commands rapidly
	numCommands := 500
	start := time.Now()

	for i := 0; i < numCommands; i++ {
		commandID := generateCommandID()

		_, cmdErr := env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: commandID,
			Cwd:       "/home/test",
			Command:   "test command",
			TsUnixMs:  time.Now().UnixMilli(),
		})
		if cmdErr != nil {
			t.Fatalf("CommandStarted %d failed: %v", i, cmdErr)
		}

		_, cmdErr = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  commandID,
			ExitCode:   0,
			DurationMs: 10,
			TsUnixMs:   time.Now().UnixMilli(),
		})
		if cmdErr != nil {
			t.Fatalf("CommandEnded %d failed: %v", i, cmdErr)
		}
	}

	elapsed := time.Since(start)
	t.Logf("logged %d commands in %v (%.2f commands/sec)", numCommands, elapsed, float64(numCommands)/elapsed.Seconds())

	// Verify count
	status, err := env.Client.GetStatus(ctx, &pb.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.CommandsLogged < int64(numCommands) {
		t.Errorf("expected at least %d commands logged, got %d", numCommands, status.CommandsLogged)
	}
}
