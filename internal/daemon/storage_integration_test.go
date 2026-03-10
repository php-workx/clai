package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/config"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// ============================================================================
// Full V2 lifecycle integration tests
// ============================================================================

// TestIntegration_FullLifecycle exercises the complete storage pipeline:
// 1. Daemon starts with database
// 2. Session starts
// 3. Commands are started and ended (feeding batch writer)
// 4. Suggest returns results from scorer
func TestIntegration_FullLifecycle(t *testing.T) {
	t.Parallel()

	// Use /tmp to avoid macOS socket path length limits
	tmpDir, err := os.MkdirTemp("/tmp", "clai-v2-lifecycle-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	// Step 1: Open database
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer v2db.Close()

	paths := &config.Paths{BaseDir: tmpDir}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Create server with DB.
	server, err := NewServer(&ServerConfig{
		DB:          v2db,
		Paths:       paths,
		Logger:      logger,
		IdleTimeout: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify components are initialized
	if server.db == nil {
		t.Fatal("db should be initialized")
	}
	if server.batchWriter == nil {
		t.Fatal("batchWriter should be initialized when DB is provided")
	}
	if server.scorer == nil {
		t.Fatal("scorer should be auto-initialized when DB is provided")
	}

	// Step 2: Start and verify the server (with gRPC)
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(serverCtx)
	}()

	// Wait for server to start
	socketPath := paths.SocketFile()
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if _, statErr := os.Stat(socketPath); statErr == nil {
			break
		}
		select {
		case srvErr := <-serverErr:
			if srvErr != nil {
				t.Fatalf("server.Start failed: %v", srvErr)
			}
		default:
		}
	}

	// Step 3: Start a session
	startResp, err := server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "lifecycle-session-1",
		Cwd:       "/home/user/project",
		Client: &pb.ClientInfo{
			Shell:    "zsh",
			Os:       "darwin",
			Hostname: "test-host",
			Username: "testuser",
		},
	})
	if err != nil || !startResp.Ok {
		t.Fatalf("SessionStart failed: %v (ok=%v)", err, startResp.Ok)
	}

	// Step 4: Execute some commands
	cmdStartResp, err := server.CommandStarted(ctx, &pb.CommandStartRequest{
		CommandId:   "cmd-1",
		SessionId:   "lifecycle-session-1",
		Command:     "git status",
		Cwd:         "/home/user/project",
		GitRepoName: "project",
		GitBranch:   "main",
	})
	if err != nil || !cmdStartResp.Ok {
		t.Fatalf("CommandStarted failed: %v (ok=%v)", err, cmdStartResp.Ok)
	}

	cmdEndResp, err := server.CommandEnded(ctx, &pb.CommandEndRequest{
		CommandId:  "cmd-1",
		SessionId:  "lifecycle-session-1",
		ExitCode:   0,
		DurationMs: 150,
	})
	if err != nil || !cmdEndResp.Ok {
		t.Fatalf("CommandEnded failed: %v (ok=%v)", err, cmdEndResp.Ok)
	}

	// Execute a second command for transition data
	cmdStartResp2, err := server.CommandStarted(ctx, &pb.CommandStartRequest{
		CommandId:     "cmd-2",
		SessionId:     "lifecycle-session-1",
		Command:       "git commit -m 'test'",
		Cwd:           "/home/user/project",
		GitRepoName:   "project",
		GitBranch:     "main",
		PrevCommandId: "cmd-1",
	})
	if err != nil || !cmdStartResp2.Ok {
		t.Fatalf("CommandStarted (cmd-2) failed: %v (ok=%v)", err, cmdStartResp2.Ok)
	}

	cmdEndResp2, err := server.CommandEnded(ctx, &pb.CommandEndRequest{
		CommandId:  "cmd-2",
		SessionId:  "lifecycle-session-1",
		ExitCode:   0,
		DurationMs: 500,
	})
	if err != nil || !cmdEndResp2.Ok {
		t.Fatalf("CommandEnded (cmd-2) failed: %v (ok=%v)", err, cmdEndResp2.Ok)
	}

	// Step 5: Request suggestions (V2 mode). Allow a short window for async ingest flush.
	var suggestResp *pb.SuggestResponse
	for i := 0; i < 20; i++ {
		suggestResp, err = server.Suggest(ctx, &pb.SuggestRequest{
			SessionId:  "lifecycle-session-1",
			Cwd:        "/home/user/project",
			Buffer:     "git",
			MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("Suggest failed: %v", err)
		}
		if len(suggestResp.Suggestions) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(suggestResp.Suggestions) == 0 {
		t.Error("expected at least one suggestion from storage pipeline")
	}

	// Step 6: End session
	endResp, err := server.SessionEnd(ctx, &pb.SessionEndRequest{
		SessionId: "lifecycle-session-1",
	})
	if err != nil || !endResp.Ok {
		t.Fatalf("SessionEnd failed: %v (ok=%v)", err, endResp.Ok)
	}

	// Step 7: Shutdown (flushes batch writer)
	cancel()
	server.Shutdown()

	select {
	case srvErr := <-serverErr:
		if srvErr != nil {
			t.Errorf("unexpected server error: %v", srvErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not stop in time")
	}

	// Verify batch writer processed events (checked after shutdown to ensure flush)
	stats := server.batchWriter.Stats()
	if stats.EventsWritten == 0 {
		t.Error("expected batch writer to have written events after shutdown flush")
	}

	// Verify server logged commands
	if server.getCommandsLogged() != 2 {
		t.Errorf("expected 2 commands logged, got %d", server.getCommandsLogged())
	}
}

// TestIntegration_BatchWriterLifecycle_Extended verifies the batch writer
// processes events throughout the server lifecycle and flushes on shutdown.
func TestIntegration_BatchWriterLifecycle_Extended(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("/tmp", "clai-bw-ext-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer v2db.Close()

	paths := &config.Paths{BaseDir: tmpDir}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		DB:          v2db,
		Paths:       paths,
		Logger:      logger,
		IdleTimeout: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.batchWriter == nil {
		t.Fatal("batchWriter should be initialized")
	}

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(serverCtx)
	}()

	socketPath := paths.SocketFile()
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if _, statErr := os.Stat(socketPath); statErr == nil {
			break
		}
	}

	// Start a session
	_, err = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "bw-session",
		Cwd:       "/tmp/project",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Execute multiple commands to generate batch writer events
	commandCount := 5
	for i := 0; i < commandCount; i++ {
		cmdID := "bwcmd-" + string(rune('a'+i))
		_, err = server.CommandStarted(ctx, &pb.CommandStartRequest{
			CommandId: cmdID,
			SessionId: "bw-session",
			Command:   "echo test" + string(rune('0'+i)),
			Cwd:       "/tmp/project",
		})
		if err != nil {
			t.Fatalf("CommandStarted(%s) failed: %v", cmdID, err)
		}

		_, err = server.CommandEnded(ctx, &pb.CommandEndRequest{
			CommandId:  cmdID,
			SessionId:  "bw-session",
			ExitCode:   0,
			DurationMs: int64(50 + i*10),
		})
		if err != nil {
			t.Fatalf("CommandEnded(%s) failed: %v", cmdID, err)
		}
	}

	// Allow batch writer time to process events
	time.Sleep(100 * time.Millisecond)

	// Shutdown (triggers batch writer flush and ensures all events are written)
	cancel()
	server.Shutdown()

	select {
	case srvErr := <-serverErr:
		if srvErr != nil {
			t.Errorf("unexpected server error: %v", srvErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not stop in time")
	}

	// After shutdown, batch writer Stop() should have flushed all pending events.
	// EventsWritten tracks successfully flushed events.
	finalStats := server.batchWriter.Stats()
	if finalStats.EventsWritten < int64(commandCount) {
		t.Errorf("expected at least %d events written after shutdown, got %d",
			commandCount, finalStats.EventsWritten)
	}
}

// TestIntegration_ImportHistoryWithBackfill verifies that ImportHistory
// triggers V2 backfill when DB is available.
func TestIntegration_ImportHistoryWithBackfill(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer v2db.Close()

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	server, err := NewServer(&ServerConfig{
		DB:     v2db,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.db == nil {
		t.Fatal("v2db should be set for import backfill testing")
	}

	// Import history
	resp, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("ImportHistory failed: %v", err)
	}

	// No entries imported (no shell history file), so no backfill occurs (no error either)
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestIntegration_CommandEndedFeedsBatchWriter verifies that CommandEnded
// properly feeds the batch writer when it is initialized.
func TestIntegration_CommandEndedFeedsBatchWriter(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.batchWriter == nil {
		t.Fatal("batchWriter should be initialized")
	}

	// Start batch writer (normally done by server.Start)
	server.batchWriter.Start()

	// Start a session (needed for batch writer to get command context)
	_, err = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "feed-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// Start a command
	_, err = server.CommandStarted(ctx, &pb.CommandStartRequest{
		CommandId: "feed-cmd-1",
		SessionId: "feed-session",
		Command:   "ls -la",
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	// End the command (triggers batch writer enqueue)
	_, err = server.CommandEnded(ctx, &pb.CommandEndRequest{
		CommandId:  "feed-cmd-1",
		SessionId:  "feed-session",
		ExitCode:   0,
		DurationMs: 50,
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}

	// Stop the batch writer to flush all pending events before checking stats
	server.batchWriter.Stop()

	// After stop/flush, the batch writer should have written at least one event
	stats := server.batchWriter.Stats()
	if stats.EventsWritten == 0 {
		t.Error("expected at least one event written after batch writer flush")
	}
}
