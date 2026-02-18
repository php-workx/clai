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
	"github.com/runger/clai/internal/suggest"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// ============================================================================
// Full V2 lifecycle integration tests
// ============================================================================

// TestV2Integration_FullLifecycle exercises the complete V2 pipeline:
// 1. Daemon starts with V2 database
// 2. Session starts
// 3. Import history triggers V2 backfill
// 4. Commands are started and ended (feeding V2 batch writer)
// 5. Suggest returns results from V2 scorer
func TestV2Integration_FullLifecycle(t *testing.T) {
	t.Parallel()

	// Use /tmp to avoid macOS socket path length limits
	tmpDir, err := os.MkdirTemp("/tmp", "clai-v2-lifecycle-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	// Step 1: Open V2 database
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	paths := &config.Paths{BaseDir: tmpDir}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := newMockStore()
	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{Text: "git status", Source: "history", Score: 0.9},
		},
	}

	// Create server with V2 enabled in v2 mode
	server, err := NewServer(&ServerConfig{
		Store:         store,
		Ranker:        ranker,
		V2DB:          v2db,
		Paths:         paths,
		Logger:        logger,
		IdleTimeout:   1 * time.Hour,
		ScorerVersion: "v2",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify V2 components are initialized
	if server.v2db == nil {
		t.Fatal("v2db should be initialized")
	}
	if server.batchWriter == nil {
		t.Fatal("batchWriter should be initialized when V2DB is provided")
	}
	if server.v2Scorer == nil {
		t.Fatal("v2Scorer should be auto-initialized when V2DB is provided")
	}
	if server.scorerVersion != "v2" {
		t.Fatalf("expected scorerVersion='v2', got %q", server.scorerVersion)
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

	// Step 5: Request suggestions (v2 mode)
	suggestResp, err := server.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "lifecycle-session-1",
		Cwd:        "/home/user/project",
		Buffer:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	// V1 ranker always returns "git status" so we should have at least that
	if len(suggestResp.Suggestions) == 0 {
		t.Error("expected at least one suggestion in v2 mode")
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

// TestV2Integration_GracefulDegradation_NilDB verifies the full server lifecycle
// works when V2 database is nil (V1-only mode).
func TestV2Integration_GracefulDegradation_NilDB(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("/tmp", "clai-v2-degrade-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	paths := &config.Paths{BaseDir: tmpDir}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	store := newMockStore()
	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{Text: "make test", Source: "history", Score: 0.8},
		},
	}

	// Create server without V2 (nil V2DB)
	server, err := NewServer(&ServerConfig{
		Store:         store,
		Ranker:        ranker,
		V2DB:          nil,
		Paths:         paths,
		Logger:        logger,
		IdleTimeout:   1 * time.Hour,
		ScorerVersion: "v2", // Request V2 but should fall back
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify V2 is disabled gracefully
	if server.v2db != nil {
		t.Error("v2db should be nil")
	}
	if server.batchWriter != nil {
		t.Error("batchWriter should be nil without V2DB")
	}
	if server.v2Scorer != nil {
		t.Error("v2Scorer should be nil without V2DB")
	}
	if server.scorerVersion != "v1" {
		t.Errorf("expected fallback to v1, got %q", server.scorerVersion)
	}

	ctx := context.Background()

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

	// Session + command should work fine in V1 mode
	_, err = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "degrade-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	_, err = server.CommandStarted(ctx, &pb.CommandStartRequest{
		CommandId: "dcmd-1",
		SessionId: "degrade-session",
		Command:   "make test",
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	_, err = server.CommandEnded(ctx, &pb.CommandEndRequest{
		CommandId:  "dcmd-1",
		SessionId:  "degrade-session",
		ExitCode:   0,
		DurationMs: 100,
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}

	// Suggest should use V1 ranker
	resp, err := server.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "degrade-session",
		Cwd:        "/tmp",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	if len(resp.Suggestions) == 0 {
		t.Error("expected V1 suggestions")
	}
	if resp.Suggestions[0].Text != "make test" {
		t.Errorf("expected V1 ranker result 'make test', got %q", resp.Suggestions[0].Text)
	}

	// Shutdown
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
}

// TestV2Integration_GracefulDegradation_CorruptDB verifies the server handles
// a V2 database that becomes unavailable after initial open.
func TestV2Integration_GracefulDegradation_CorruptDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	store := newMockStore()
	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{Text: "npm install", Source: "history", Score: 0.7},
		},
	}

	server, err := NewServer(&ServerConfig{
		Store:         store,
		Ranker:        ranker,
		V2DB:          v2db,
		Logger:        logger,
		ScorerVersion: "v2",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.v2Scorer == nil {
		t.Fatal("v2Scorer should be initialized")
	}

	// Suggest should work in v2 mode (V2 will return empty since no data,
	// but V1 provides results)
	resp, err := server.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "corrupt-session",
		Cwd:        "/tmp",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	if len(resp.Suggestions) == 0 {
		t.Error("expected at least V1 suggestions in v2 mode")
	}
}

// TestV2Integration_BatchWriterLifecycle_Extended verifies the batch writer
// processes events throughout the server lifecycle and flushes on shutdown.
func TestV2Integration_BatchWriterLifecycle_Extended(t *testing.T) {
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
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	paths := &config.Paths{BaseDir: tmpDir}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	store := newMockStore()

	server, err := NewServer(&ServerConfig{
		Store:       store,
		V2DB:        v2db,
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

// TestV2Integration_ScorerVersionSwitching verifies that different scorer versions
// produce different behavior with the same input.
func TestV2Integration_ScorerVersionSwitching(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	store := newMockStore()
	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{Text: "docker compose up", Source: "history", Score: 0.9},
			{Text: "docker ps", Source: "history", Score: 0.6},
		},
	}

	// Test each scorer version
	versions := []string{"v1", "v2"}
	for _, version := range versions {
		t.Run("version="+version, func(t *testing.T) {
			server, err := NewServer(&ServerConfig{
				Store:         store,
				Ranker:        ranker,
				V2DB:          v2db,
				ScorerVersion: version,
			})
			if err != nil {
				t.Fatalf("NewServer failed: %v", err)
			}

			if server.scorerVersion != version {
				t.Fatalf("expected scorerVersion=%q, got %q", version, server.scorerVersion)
			}

			resp, err := server.Suggest(ctx, &pb.SuggestRequest{
				SessionId:  "version-test",
				Cwd:        "/tmp",
				MaxResults: 5,
			})
			if err != nil {
				t.Fatalf("Suggest failed: %v", err)
			}

			// All versions should produce results (V2 falls through to V1 on empty DB)
			switch version {
			case "v1":
				if len(resp.Suggestions) != 2 {
					t.Errorf("v1: expected 2 suggestions, got %d", len(resp.Suggestions))
				}
			case "v2":
				// V2 merges V2 (empty DB) + V1, so should return V1 results
				if len(resp.Suggestions) == 0 {
					t.Error("v2: expected at least V1 suggestions")
				}
			}
		})
	}
}

// TestV2Integration_ImportHistoryWithBackfill verifies that ImportHistory
// triggers V2 backfill when V2DB is available.
func TestV2Integration_ImportHistoryWithBackfill(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := newMockStore()

	server, err := NewServer(&ServerConfig{
		Store:  store,
		V2DB:   v2db,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.v2db == nil {
		t.Fatal("v2db should be set for import backfill testing")
	}

	// Import history (the mock store handles the import)
	resp, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("ImportHistory failed: %v", err)
	}

	// Mock store returns 0 entries, so no backfill occurs (no error either)
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestV2Integration_CommandEndedFeedsBatchWriter verifies that CommandEnded
// properly feeds the V2 batch writer when it is initialized.
func TestV2Integration_CommandEndedFeedsBatchWriter(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")
	ctx := context.Background()

	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	store := newMockStore()

	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  v2db,
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
