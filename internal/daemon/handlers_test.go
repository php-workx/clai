package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/provider"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/learning"
	"github.com/runger/clai/internal/suggestions/ops"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
)

// newTestDB creates a temporary V2 database for testing.
// The database is automatically cleaned up when the test finishes.
func newTestDB(t *testing.T) *suggestdb.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	v2db, err := suggestdb.Open(context.Background(), suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open test V2 DB: %v", err)
	}
	t.Cleanup(func() { _ = v2db.Close() })
	return v2db
}

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name       string
	suggestion string
	available  bool
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Available() bool {
	return m.available
}

func (m *mockProvider) TextToCommand(ctx context.Context, req *provider.TextToCommandRequest) (*provider.TextToCommandResponse, error) {
	return &provider.TextToCommandResponse{
		Suggestions: []provider.Suggestion{
			{Text: m.suggestion, Source: "ai"},
		},
		ProviderName: m.name,
	}, nil
}

func (m *mockProvider) NextStep(ctx context.Context, req *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	return &provider.NextStepResponse{
		Suggestions: []provider.Suggestion{
			{Text: m.suggestion, Source: "ai"},
		},
		ProviderName: m.name,
	}, nil
}

func (m *mockProvider) Diagnose(ctx context.Context, req *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	return &provider.DiagnoseResponse{
		Explanation: "Test explanation",
		Fixes: []provider.Suggestion{
			{Text: m.suggestion, Source: "ai"},
		},
		ProviderName: m.name,
	}, nil
}

func createTestServer(t *testing.T) *Server {
	t.Helper()

	v2db := newTestDB(t)

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "echo hello",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		DB:          v2db,
		Registry:    registry,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server
}

func TestHandler_SessionStart_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.SessionStartRequest{
		SessionId: "test-session-1",
		Cwd:       "/tmp",
		Client: &pb.ClientInfo{
			Shell:    "zsh",
			Os:       "darwin",
			Hostname: "test-host",
			Username: "test-user",
		},
		StartedAtUnixMs: time.Now().UnixMilli(),
	}

	resp, err := server.SessionStart(ctx, req)
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("SessionStart returned ok=false: %s", resp.Error)
	}

	// Verify session was registered
	if !server.sessionManager.Exists("test-session-1") {
		t.Error("session was not registered in session manager")
	}

	if server.sessionManager.ActiveCount() != 1 {
		t.Errorf("expected 1 active session, got %d", server.sessionManager.ActiveCount())
	}
}

func TestHandler_SessionEnd_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// First start a session
	startReq := &pb.SessionStartRequest{
		SessionId: "test-session-2",
		Cwd:       "/tmp",
		Client: &pb.ClientInfo{
			Shell: "zsh",
		},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// End the session
	endReq := &pb.SessionEndRequest{
		SessionId:     "test-session-2",
		EndedAtUnixMs: time.Now().UnixMilli(),
	}

	resp, err := server.SessionEnd(ctx, endReq)
	if err != nil {
		t.Fatalf("SessionEnd failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("SessionEnd returned ok=false: %s", resp.Error)
	}

	// Verify session was removed
	if server.sessionManager.Exists("test-session-2") {
		t.Error("session was not removed from session manager")
	}
}

func TestHandler_SessionEnd_ClearsSuggestSnapshot(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	_, _ = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "snapshot-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	})

	server.snapshotMu.Lock()
	server.lastSuggestSnapshots["snapshot-session"] = suggestSnapshot{
		ShownAtMs: time.Now().UnixMilli(),
	}
	server.snapshotMu.Unlock()

	_, _ = server.SessionEnd(ctx, &pb.SessionEndRequest{
		SessionId: "snapshot-session",
	})

	_, ok := server.getSuggestSnapshot("snapshot-session")
	if ok {
		t.Fatal("expected session-end to clear suggest snapshot")
	}
}

func TestHandler_CommandStarted_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// First start a session
	startReq := &pb.SessionStartRequest{
		SessionId: "test-session-3",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// Start a command
	cmdReq := &pb.CommandStartRequest{
		SessionId: "test-session-3",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "echo hello",
		TsUnixMs:  time.Now().UnixMilli(),
	}

	resp, err := server.CommandStarted(ctx, cmdReq)
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("CommandStarted returned ok=false: %s", resp.Error)
	}
}

func TestHandler_CommandEnded_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// First start a session
	startReq := &pb.SessionStartRequest{
		SessionId: "test-session-4",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// Start a command
	cmdStartReq := &pb.CommandStartRequest{
		SessionId: "test-session-4",
		CommandId: "cmd-2",
		Cwd:       "/tmp",
		Command:   "echo hello",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	_, _ = server.CommandStarted(ctx, cmdStartReq)

	// End the command
	cmdEndReq := &pb.CommandEndRequest{
		SessionId:  "test-session-4",
		CommandId:  "cmd-2",
		ExitCode:   0,
		DurationMs: 100,
		TsUnixMs:   time.Now().UnixMilli(),
	}

	resp, err := server.CommandEnded(ctx, cmdEndReq)
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("CommandEnded returned ok=false: %s", resp.Error)
	}

	// Verify commands logged counter was incremented
	if server.getCommandsLogged() != 1 {
		t.Errorf("expected commands logged to be 1, got %d", server.getCommandsLogged())
	}
}

func newFeedbackStoreWithDB(t *testing.T) (*feedback.Store, func()) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "feedback_test.db")
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open feedback db: %v", err)
	}
	store := feedback.NewStore(v2db.DB(), feedback.DefaultConfig(), nil)
	cleanup := func() {
		_ = v2db.Close()
	}
	return store, cleanup
}

func TestHandler_RecordFeedback_WithDB(t *testing.T) {
	t.Parallel()
	// With DB configured, NewServer auto-creates a feedback store,
	// so RecordFeedback should succeed for valid requests.
	server := createTestServer(t)
	ctx := context.Background()

	resp, err := server.RecordFeedback(ctx, &pb.RecordFeedbackRequest{
		SessionId:     "sess-1",
		SuggestedText: "make test",
		Action:        "accepted",
	})
	if err != nil {
		t.Fatalf("RecordFeedback failed: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected response.Ok=true with DB, got error: %+v", resp.Error)
	}
}

func TestHandler_RecordFeedback_ValidationAndSuccess(t *testing.T) {
	v2db := newTestDB(t)
	feedbackStore, cleanup := newFeedbackStoreWithDB(t)
	defer cleanup()

	server, err := NewServer(&ServerConfig{
		DB:            v2db,
		FeedbackStore: feedbackStore,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	cases := []struct {
		name    string
		req     *pb.RecordFeedbackRequest
		wantMsg string
	}{
		{
			name: "missing session",
			req: &pb.RecordFeedbackRequest{
				SuggestedText: "git status",
				Action:        "accepted",
			},
			wantMsg: "session_id is required",
		},
		{
			name: "missing suggested text",
			req: &pb.RecordFeedbackRequest{
				SessionId: "sess-1",
				Action:    "accepted",
			},
			wantMsg: "suggested_text is required",
		},
		{
			name: "missing action",
			req: &pb.RecordFeedbackRequest{
				SessionId:     "sess-1",
				SuggestedText: "git status",
			},
			wantMsg: "action is required",
		},
	}
	for _, tc := range cases {
		resp, fbErr := server.RecordFeedback(ctx, tc.req)
		if fbErr != nil {
			t.Fatalf("%s: RecordFeedback returned error: %v", tc.name, fbErr)
		}
		if resp.Ok {
			t.Fatalf("%s: expected response.Ok=false", tc.name)
		}
		if resp.Error == nil || resp.Error.Code != "E_INVALID_REQUEST" || !strings.Contains(resp.Error.Message, tc.wantMsg) {
			t.Fatalf("%s: expected validation error %q, got %+v", tc.name, tc.wantMsg, resp.Error)
		}
	}

	successReq := &pb.RecordFeedbackRequest{
		SessionId:     "sess-success",
		SuggestedText: "git status",
		Action:        "accepted",
		ExecutedText:  "git status",
		Prefix:        "git st",
		LatencyMs:     42,
	}
	resp, err := server.RecordFeedback(ctx, successReq)
	if err != nil {
		t.Fatalf("success RecordFeedback error: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected success response, got %+v", resp.Error)
	}

	aliasResp, err := server.SuggestFeedback(ctx, &pb.RecordFeedbackRequest{
		SessionId:     "sess-success",
		SuggestedText: "git diff",
		Action:        "dismissed",
	})
	if err != nil {
		t.Fatalf("SuggestFeedback error: %v", err)
	}
	if !aliasResp.Ok {
		t.Fatalf("expected alias success response, got %+v", aliasResp.Error)
	}

	recs, err := feedbackStore.QueryFeedback(ctx, "sess-success", 10)
	if err != nil {
		t.Fatalf("QueryFeedback failed: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 feedback records, got %d", len(recs))
	}
}

func TestHandler_RecordFeedback_StoreError(t *testing.T) {
	v2db := newTestDB(t)
	feedbackStore, cleanup := newFeedbackStoreWithDB(t)
	// Close the DB immediately to force store errors during RecordFeedback.
	cleanup()

	server, err := NewServer(&ServerConfig{
		DB:            v2db,
		FeedbackStore: feedbackStore,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	resp, err := server.RecordFeedback(context.Background(), &pb.RecordFeedbackRequest{
		SessionId:     "sess-err",
		SuggestedText: "npm test",
		Action:        "accepted",
	})
	if err != nil {
		t.Fatalf("RecordFeedback returned error: %v", err)
	}
	if resp.Ok {
		t.Fatal("expected Ok=false when feedback store write fails")
	}
	if resp.Error == nil || resp.Error.Code != "E_STORE_ERROR" {
		t.Fatalf("expected E_STORE_ERROR, got %+v", resp.Error)
	}
}

func TestHandler_RecordFeedback_StaleSnapshotIgnored(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "stale_snapshot.db")
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 DB: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	server.snapshotMu.Lock()
	server.lastSuggestSnapshots["sess-stale"] = suggestSnapshot{
		Context: suggest2.SuggestContext{
			Scope:          "global",
			LastTemplateID: "tmpl:last",
		},
		Suggestions: []suggest2.Suggestion{
			{Command: "git status", TemplateID: "tmpl:git-status", Score: 10},
			{Command: "ls -la", TemplateID: "tmpl:ls", Score: 9},
		},
		ShownAtMs: time.Now().Add(-maxSuggestSnapshotAge - time.Minute).UnixMilli(),
	}
	server.snapshotMu.Unlock()

	before := int64(0)
	if server.learner != nil {
		before = server.learner.SampleCount()
	}

	resp, err := server.RecordFeedback(ctx, &pb.RecordFeedbackRequest{
		SessionId:     "sess-stale",
		SuggestedText: "git status",
		Action:        "accepted",
		Prefix:        "git",
	})
	if err != nil {
		t.Fatalf("RecordFeedback failed: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("RecordFeedback returned ok=false: %+v", resp.Error)
	}

	if server.learner != nil && server.learner.SampleCount() != before {
		t.Fatalf("expected stale snapshot to skip learner update")
	}
	if _, ok := server.getSuggestSnapshot("sess-stale"); ok {
		t.Fatalf("expected stale snapshot to be evicted")
	}
}

func TestServer_ApplyLearningProfile_ReordersSuggestions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "learning_profile.db")
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 DB: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	if server.learningStore == nil {
		t.Fatal("expected learning store to be configured")
	}

	w := learning.DefaultWeights()
	w.Prefix = 0.60
	w.RiskPenalty = 0.20
	if err := server.learningStore.SaveWeights(ctx, "repo:demo", &w, 80, 0.01); err != nil {
		t.Fatalf("failed to save weights: %v", err)
	}

	suggestions := []suggest2.Suggestion{
		{Command: "ls -la", Score: 10.5},
		{Command: "git status", Score: 10.0},
	}

	server.applyLearningProfile(ctx, &suggest2.SuggestContext{
		Scope:  "repo:demo",
		Prefix: "git",
	}, suggestions)

	if suggestions[0].Command != "git status" {
		t.Fatalf("expected learning profile to reorder prefix match first, got %q", suggestions[0].Command)
	}
}

func TestImportHistory_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("if_not_exists_skip", func(t *testing.T) {
		v2db := newTestDB(t)

		// Pre-import history so HasImportedHistory returns true.
		_, err := ops.ImportHistory(ctx, v2db, []history.ImportEntry{{Command: "echo pre"}}, "bash")
		if err != nil {
			t.Fatalf("pre-import failed: %v", err)
		}

		server, err := NewServer(&ServerConfig{DB: v2db})
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		resp, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{
			Shell:       "bash",
			IfNotExists: true,
		})
		if err != nil {
			t.Fatalf("ImportHistory failed: %v", err)
		}
		if !resp.Skipped {
			t.Fatalf("expected import to be skipped: %+v", resp)
		}
	})

	t.Run("unsupported_shell", func(t *testing.T) {
		server := createTestServer(t)
		resp, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{Shell: "pwsh"})
		if err != nil {
			t.Fatalf("ImportHistory failed: %v", err)
		}
		if !strings.Contains(resp.Error, "unsupported shell") {
			t.Fatalf("expected unsupported shell response, got %+v", resp)
		}
	})

	t.Run("auto_detect_failure", func(t *testing.T) {
		server := createTestServer(t)
		t.Setenv("SHELL", "")
		resp, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{Shell: "auto"})
		if err != nil {
			t.Fatalf("ImportHistory failed: %v", err)
		}
		if !strings.Contains(resp.Error, "could not detect shell type") {
			t.Fatalf("expected detect shell failure, got %+v", resp)
		}
	})

	t.Run("read_error", func(t *testing.T) {
		server := createTestServer(t)
		if _, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{
			Shell:       "bash",
			HistoryPath: t.TempDir(),
		}); err == nil {
			t.Fatal("expected read error for directory history path")
		}
	})
}

func TestHandler_Suggest_ReturnsHistorySuggestions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	v2db := newTestDB(t)

	// Seed the V2 database with a session and command for suggest to find
	if err := ops.CreateSession(ctx, v2db, &ops.Session{
		SessionID:   "test-session",
		Shell:       "zsh",
		StartedAtMs: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	cmd := ops.Command{
		CommandID: "cmd-suggest-1",
		SessionID: "test-session",
		CmdRaw:    "git status",
		CmdNorm:   "git status",
		TSStartMs: time.Now().UnixMilli(),
		CWD:       "/tmp",
	}
	if err := ops.CreateCommand(ctx, v2db, &cmd); err != nil {
		t.Fatalf("failed to create command: %v", err)
	}

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "echo hello",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		DB:          v2db,
		Registry:    registry,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "git",
		MaxResults: 5,
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// With V2 scorer and seeded data, we should get suggestions
	if resp.Suggestions == nil {
		t.Error("expected non-nil suggestions slice")
	}
}

// V1 Suggest "why details" test removed — it relied on V1 mockRanker which no longer exists.

func TestHandler_TextToCommand_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session for context
	startReq := &pb.SessionStartRequest{
		SessionId: "test-session-5",
		Cwd:       "/tmp",
		Client: &pb.ClientInfo{
			Shell: "zsh",
			Os:    "darwin",
		},
	}
	_, _ = server.SessionStart(ctx, startReq)

	req := &pb.TextToCommandRequest{
		SessionId: "test-session-5",
		Prompt:    "print hello world",
		Cwd:       "/tmp",
	}

	resp, err := server.TextToCommand(ctx, req)
	if err != nil {
		t.Fatalf("TextToCommand failed: %v", err)
	}

	if len(resp.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}

	if resp.Provider != "test" {
		t.Errorf("expected provider 'test', got %s", resp.Provider)
	}
}

func TestHandler_Diagnose_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.DiagnoseRequest{
		SessionId: "test-session",
		Command:   "npm install",
		ExitCode:  1,
		Cwd:       "/tmp",
	}

	resp, err := server.Diagnose(ctx, req)
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	if resp.Explanation == "" {
		t.Error("expected explanation")
	}

	if len(resp.Fixes) == 0 {
		t.Error("expected at least one fix suggestion")
	}
}

func TestHandler_Ping_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	resp, err := server.Ping(ctx, &pb.Ack{})
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	if !resp.Ok {
		t.Error("Ping returned ok=false")
	}
}

func TestHandler_GetStatus_Success(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start some sessions
	for i := 0; i < 3; i++ {
		startReq := &pb.SessionStartRequest{
			SessionId: "status-session-" + string(rune('0'+i)),
			Cwd:       "/tmp",
			Client:    &pb.ClientInfo{Shell: "zsh"},
		}
		_, _ = server.SessionStart(ctx, startReq)
	}

	resp, err := server.GetStatus(ctx, &pb.Ack{})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if resp.ActiveSessions != 3 {
		t.Errorf("expected 3 active sessions, got %d", resp.ActiveSessions)
	}

	if resp.UptimeSeconds < 0 {
		t.Errorf("uptime should be non-negative, got %d", resp.UptimeSeconds)
	}
}

func TestHandler_SuggestWithDestructiveCommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	v2db := newTestDB(t)

	// Seed V2 DB with a destructive command so suggest can find it
	if err := ops.CreateSession(ctx, v2db, &ops.Session{
		SessionID:   "test-session",
		Shell:       "zsh",
		StartedAtMs: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	ec := 0
	cmd := ops.Command{
		CommandID: "cmd-rm",
		SessionID: "test-session",
		CmdRaw:    "rm -rf /",
		CmdNorm:   "rm -rf /",
		TSStartMs: time.Now().UnixMilli(),
		CWD:       "/tmp",
		ExitCode:  &ec,
	}
	if err := ops.CreateCommand(ctx, v2db, &cmd); err != nil {
		t.Fatalf("failed to create command: %v", err)
	}

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "rm",
		MaxResults: 5,
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// With V2 scorer and seeded data, check suggestions exist and are flagged
	if len(resp.Suggestions) > 0 && resp.Suggestions[0].Risk != "destructive" {
		t.Errorf("expected risk to be 'destructive', got %s", resp.Suggestions[0].Risk)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		maxLen   int
	}{
		{"hello", "hello", 10},
		{"hello world", "he...", 5},
		{"abc", "abc", 3},
		{"abcd", "abc", 3},
		{"hello", "", 0},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

// ============================================================================
// Additional tests for edge cases and error paths
// ============================================================================

// --- truncate function edge cases ---

func TestTruncate_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
		maxLen   int
	}{
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "negative maxLen",
			input:    "hello",
			maxLen:   -5,
			expected: "", // returns empty string for maxLen <= 0
		},
		{
			name:     "zero maxLen",
			input:    "hello",
			maxLen:   0,
			expected: "", // returns empty string for maxLen <= 0
		},
		{
			name:     "maxLen equals 1",
			input:    "hello",
			maxLen:   1,
			expected: "h",
		},
		{
			name:     "maxLen equals 2",
			input:    "hello",
			maxLen:   2,
			expected: "he",
		},
		{
			name:     "maxLen equals 3",
			input:    "hello",
			maxLen:   3,
			expected: "hel",
		},
		{
			name:     "maxLen equals 4 with long string",
			input:    "hello world",
			maxLen:   4,
			expected: "h...",
		},
		{
			name:     "exact length match",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "unicode string",
			input:    "hello 世界",
			maxLen:   8,
			expected: "hello...",
		},
		{
			name:     "very long string",
			input:    "this is a very long command that should be truncated properly",
			maxLen:   20,
			expected: "this is a very lo...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

// --- SessionStart edge cases ---

func TestHandler_SessionStart_NilClient(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.SessionStartRequest{
		SessionId:       "test-nil-client",
		Cwd:             "/tmp",
		Client:          nil, // No client info provided
		StartedAtUnixMs: time.Now().UnixMilli(),
	}

	resp, err := server.SessionStart(ctx, req)
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("SessionStart returned ok=false: %s", resp.Error)
	}

	// Verify session was registered with defaults
	if !server.sessionManager.Exists("test-nil-client") {
		t.Error("session was not registered in session manager")
	}

	// Get session info and verify defaults
	info, ok := server.sessionManager.Get("test-nil-client")
	if !ok {
		t.Fatal("session not found in manager")
	}

	// Shell should be empty when no client info
	if info.Shell != "" {
		t.Errorf("expected empty shell, got %q", info.Shell)
	}

	// OS should default to runtime.GOOS
	if info.OS == "" {
		t.Error("expected OS to be set to default")
	}
}

func TestHandler_SessionStart_PartialClientInfo(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.SessionStartRequest{
		SessionId: "test-partial-client",
		Cwd:       "/home/user",
		Client: &pb.ClientInfo{
			Shell: "fish",
			// OS, Hostname, Username omitted
		},
	}

	resp, err := server.SessionStart(ctx, req)
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("SessionStart returned ok=false: %s", resp.Error)
	}

	info, ok := server.sessionManager.Get("test-partial-client")
	if !ok {
		t.Fatal("session not found")
	}

	if info.Shell != "fish" {
		t.Errorf("expected shell 'fish', got %q", info.Shell)
	}

	// Should have default OS
	if info.OS == "" {
		t.Error("expected default OS")
	}
}

func TestHandler_SessionStart_ZeroTimestamp(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	beforeStart := time.Now()

	req := &pb.SessionStartRequest{
		SessionId:       "test-zero-ts",
		Cwd:             "/tmp",
		Client:          &pb.ClientInfo{Shell: "bash"},
		StartedAtUnixMs: 0, // Zero means use current time
	}

	resp, err := server.SessionStart(ctx, req)
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("SessionStart returned ok=false: %s", resp.Error)
	}

	afterStart := time.Now()

	info, ok := server.sessionManager.Get("test-zero-ts")
	if !ok {
		t.Fatal("session not found")
	}

	// StartedAt should be between beforeStart and afterStart
	if info.StartedAt.Before(beforeStart) || info.StartedAt.After(afterStart) {
		t.Errorf("StartedAt %v not in expected range [%v, %v]", info.StartedAt, beforeStart, afterStart)
	}
}

// --- CommandStarted edge cases ---

func TestHandler_CommandStarted_UpdatesCWD(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// First start a session with initial CWD
	startReq := &pb.SessionStartRequest{
		SessionId: "test-cwd-update",
		Cwd:       "/home/user",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// Verify initial CWD
	info, _ := server.sessionManager.Get("test-cwd-update")
	if info.CWD != "/home/user" {
		t.Errorf("expected initial CWD /home/user, got %s", info.CWD)
	}

	// Start a command with a different CWD
	cmdReq := &pb.CommandStartRequest{
		SessionId: "test-cwd-update",
		CommandId: "cmd-cwd",
		Cwd:       "/home/user/project", // New CWD
		Command:   "ls -la",
	}

	resp, err := server.CommandStarted(ctx, cmdReq)
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("CommandStarted returned ok=false: %s", resp.Error)
	}

	// Verify CWD was updated
	info, _ = server.sessionManager.Get("test-cwd-update")
	if info.CWD != "/home/user/project" {
		t.Errorf("expected CWD to be updated to /home/user/project, got %s", info.CWD)
	}
}

func TestHandler_CommandStarted_EmptyCWD(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session
	startReq := &pb.SessionStartRequest{
		SessionId: "test-empty-cwd",
		Cwd:       "/home/user",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// Start a command with empty CWD — V2 ops requires CWD, so this should fail
	cmdReq := &pb.CommandStartRequest{
		SessionId: "test-empty-cwd",
		CommandId: "cmd-empty-cwd",
		Cwd:       "", // Empty CWD is rejected by V2 ops
		Command:   "echo hello",
	}

	resp, err := server.CommandStarted(ctx, cmdReq)
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	if resp.Ok {
		t.Error("expected ok=false when CWD is empty (V2 ops requires CWD)")
	}

	// Verify CWD was NOT updated (still original)
	info, _ := server.sessionManager.Get("test-empty-cwd")
	if info.CWD != "/home/user" {
		t.Errorf("expected CWD to remain /home/user, got %s", info.CWD)
	}
}

func TestHandler_CommandStarted_ZeroTimestamp(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	startReq := &pb.SessionStartRequest{
		SessionId: "test-cmd-zero-ts",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	beforeCmd := time.Now()

	cmdReq := &pb.CommandStartRequest{
		SessionId: "test-cmd-zero-ts",
		CommandId: "cmd-zero-ts",
		Cwd:       "/tmp",
		Command:   "pwd",
		TsUnixMs:  0, // Zero means use current time
	}

	resp, err := server.CommandStarted(ctx, cmdReq)
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("CommandStarted returned ok=false: %s", resp.Error)
	}

	afterCmd := time.Now()

	// The command should be recorded with a timestamp between beforeCmd and afterCmd
	// We can't easily verify this without accessing the store, but at least the call succeeded
	_ = beforeCmd
	_ = afterCmd
}

// --- Suggest edge cases ---

func TestHandler_Suggest_ZeroMaxResults(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "git",
		MaxResults: 0, // Should default to 5
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// With empty V2 DB, suggestions may be empty; verify no error on MaxResults=0
	if resp.Suggestions == nil {
		t.Error("expected non-nil suggestions slice even with MaxResults=0")
	}
}

func TestHandler_Suggest_NegativeMaxResults(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "git",
		MaxResults: -10, // Negative should default to 5
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// Negative max results should not cause an error; suggestions may be empty with empty DB
	if resp.Suggestions == nil {
		t.Error("expected non-nil suggestions slice even with negative MaxResults")
	}
}

func TestHandler_Suggest_LargeMaxResults(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "git",
		MaxResults: 1000, // Very large limit
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// Should return whatever suggestions are available
	if resp.Suggestions == nil {
		t.Error("suggestions should not be nil")
	}
}

func TestHandler_Suggest_WithActiveSession(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session first
	startReq := &pb.SessionStartRequest{
		SessionId: "suggest-session",
		Cwd:       "/home/user",
		Client:    &pb.ClientInfo{Shell: "zsh", Os: "darwin"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	req := &pb.SuggestRequest{
		SessionId:  "suggest-session",
		Cwd:        "/home/user",
		Buffer:     "git",
		MaxResults: 5,
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}

	// With V2 scorer and an empty DB, suggestions may be empty; verify no error
	if resp.Suggestions == nil {
		t.Error("expected non-nil suggestions slice")
	}
}

// --- Risk detection tests ---

func TestHandler_TextToCommand_DestructiveCommandFlagged(t *testing.T) {
	t.Parallel()

	// Create server with mock provider that returns destructive command
	v2db := newTestDB(t)

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "rm -rf /important/data",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.TextToCommandRequest{
		SessionId: "test-session",
		Prompt:    "delete all data",
		Cwd:       "/tmp",
	}

	resp, err := server.TextToCommand(ctx, req)
	if err != nil {
		t.Fatalf("TextToCommand failed: %v", err)
	}

	if len(resp.Suggestions) == 0 {
		t.Fatal("expected suggestions")
	}

	if resp.Suggestions[0].Risk != "destructive" {
		t.Errorf("expected risk 'destructive', got %q", resp.Suggestions[0].Risk)
	}
}

func TestHandler_NextStep_DestructiveCommandFlagged(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "git reset --hard HEAD",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.NextStepRequest{
		SessionId:    "test-session",
		LastCommand:  "git add .",
		LastExitCode: 0,
		Cwd:          "/tmp",
	}

	resp, err := server.NextStep(ctx, req)
	if err != nil {
		t.Fatalf("NextStep failed: %v", err)
	}

	if len(resp.Suggestions) == 0 {
		t.Fatal("expected suggestions")
	}

	if resp.Suggestions[0].Risk != "destructive" {
		t.Errorf("expected risk 'destructive', got %q", resp.Suggestions[0].Risk)
	}
}

func TestHandler_Diagnose_DestructiveFixFlagged(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "sudo rm -rf /var/log/*",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.DiagnoseRequest{
		SessionId: "test-session",
		Command:   "disk full error",
		ExitCode:  1,
		Cwd:       "/tmp",
	}

	resp, err := server.Diagnose(ctx, req)
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	if len(resp.Fixes) == 0 {
		t.Fatal("expected fixes")
	}

	if resp.Fixes[0].Risk != "destructive" {
		t.Errorf("expected risk 'destructive', got %q", resp.Fixes[0].Risk)
	}
}

// --- Store failure tests ---

// newClosedTestDB creates a V2 database and immediately closes it,
// so that all subsequent operations against it will fail.
func newClosedTestDB(t *testing.T) *suggestdb.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "closed.db")
	v2db, err := suggestdb.Open(context.Background(), suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 DB: %v", err)
	}
	// Close immediately to force failures on subsequent ops.
	_ = v2db.Close()
	return v2db
}

func TestHandler_SessionStart_StoreFailure(t *testing.T) {
	t.Parallel()

	closedDB := newClosedTestDB(t)

	server, err := NewServer(&ServerConfig{
		DB: closedDB,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.SessionStartRequest{
		SessionId: "fail-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	}

	resp, err := server.SessionStart(ctx, req)
	if err != nil {
		t.Fatalf("SessionStart returned error: %v", err)
	}

	// Should return ok=false with error message
	if resp.Ok {
		t.Error("expected ok=false on store failure")
	}

	if resp.Error == "" {
		t.Error("expected error message on store failure")
	}
}

func TestHandler_CommandStarted_StoreFailure(t *testing.T) {
	t.Parallel()

	// Use a working DB for session start, then swap to a closed one is not possible,
	// so instead we use a closed DB and expect command creation to fail.
	closedDB := newClosedTestDB(t)

	server, err := NewServer(&ServerConfig{
		DB: closedDB,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()

	cmdReq := &pb.CommandStartRequest{
		SessionId: "cmd-fail-session",
		CommandId: "fail-cmd",
		Cwd:       "/tmp",
		Command:   "echo test",
	}

	resp, err := server.CommandStarted(ctx, cmdReq)
	if err != nil {
		t.Fatalf("CommandStarted returned error: %v", err)
	}

	if resp.Ok {
		t.Error("expected ok=false on store failure")
	}

	if resp.Error == "" {
		t.Error("expected error message on store failure")
	}
}

// --- Suggest empty database test ---

func TestHandler_Suggest_EmptyDatabase(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "git",
		MaxResults: 5,
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest returned error: %v", err)
	}

	// Empty database should return empty suggestions (graceful degradation)
	if resp.Suggestions == nil {
		t.Error("expected non-nil suggestions slice")
	}
}

// --- Provider failure tests ---

// mockFailingProvider returns errors on AI calls.
type mockFailingProvider struct {
	name       string
	available  bool
	shouldFail bool
}

func (m *mockFailingProvider) Name() string {
	return m.name
}

func (m *mockFailingProvider) Available() bool {
	return m.available
}

func (m *mockFailingProvider) TextToCommand(ctx context.Context, req *provider.TextToCommandRequest) (*provider.TextToCommandResponse, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("provider failure")
	}
	return &provider.TextToCommandResponse{}, nil
}

func (m *mockFailingProvider) NextStep(ctx context.Context, req *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("provider failure")
	}
	return &provider.NextStepResponse{}, nil
}

func (m *mockFailingProvider) Diagnose(ctx context.Context, req *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("provider failure")
	}
	return &provider.DiagnoseResponse{}, nil
}

func TestHandler_TextToCommand_NoProvider(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	// Empty registry with no available providers
	registry := provider.NewRegistry()
	// Clear default providers by setting preferred to non-existent
	registry.SetPreferred("nonexistent")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.TextToCommandRequest{
		SessionId: "test-session",
		Prompt:    "list files",
		Cwd:       "/tmp",
	}

	resp, err := server.TextToCommand(ctx, req)
	if err != nil {
		t.Fatalf("TextToCommand returned error: %v", err)
	}

	// Should return empty response when no provider available
	if len(resp.Suggestions) != 0 {
		t.Errorf("expected empty suggestions when no provider, got %d", len(resp.Suggestions))
	}
}

func TestHandler_TextToCommand_ProviderFailure(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	mockProv := &mockFailingProvider{
		name:       "failing",
		available:  true,
		shouldFail: true,
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("failing")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.TextToCommandRequest{
		SessionId: "test-session",
		Prompt:    "list files",
		Cwd:       "/tmp",
	}

	resp, err := server.TextToCommand(ctx, req)
	if err != nil {
		t.Fatalf("TextToCommand returned error: %v", err)
	}

	// Should return empty response on provider failure
	if len(resp.Suggestions) != 0 {
		t.Errorf("expected empty suggestions on provider failure, got %d", len(resp.Suggestions))
	}
}

func TestHandler_NextStep_NoProvider(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	registry := provider.NewRegistry()
	registry.SetPreferred("nonexistent")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.NextStepRequest{
		SessionId:    "test-session",
		LastCommand:  "git add .",
		LastExitCode: 0,
		Cwd:          "/tmp",
	}

	resp, err := server.NextStep(ctx, req)
	if err != nil {
		t.Fatalf("NextStep returned error: %v", err)
	}

	if len(resp.Suggestions) != 0 {
		t.Errorf("expected empty suggestions when no provider, got %d", len(resp.Suggestions))
	}
}

func TestHandler_NextStep_ProviderFailure(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	mockProv := &mockFailingProvider{
		name:       "failing",
		available:  true,
		shouldFail: true,
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("failing")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.NextStepRequest{
		SessionId:    "test-session",
		LastCommand:  "git add .",
		LastExitCode: 0,
		Cwd:          "/tmp",
	}

	resp, err := server.NextStep(ctx, req)
	if err != nil {
		t.Fatalf("NextStep returned error: %v", err)
	}

	if len(resp.Suggestions) != 0 {
		t.Errorf("expected empty suggestions on provider failure, got %d", len(resp.Suggestions))
	}
}

func TestHandler_Diagnose_NoProvider(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	registry := provider.NewRegistry()
	registry.SetPreferred("nonexistent")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.DiagnoseRequest{
		SessionId: "test-session",
		Command:   "npm install",
		ExitCode:  1,
		Cwd:       "/tmp",
	}

	resp, err := server.Diagnose(ctx, req)
	if err != nil {
		t.Fatalf("Diagnose returned error: %v", err)
	}

	// Should return explanation about no provider
	if resp.Explanation == "" {
		t.Error("expected explanation when no provider")
	}
}

func TestHandler_Diagnose_ProviderFailure(t *testing.T) {
	t.Parallel()

	v2db := newTestDB(t)

	mockProv := &mockFailingProvider{
		name:       "failing",
		available:  true,
		shouldFail: true,
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("failing")

	server, err := NewServer(&ServerConfig{
		DB:       v2db,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.DiagnoseRequest{
		SessionId: "test-session",
		Command:   "npm install",
		ExitCode:  1,
		Cwd:       "/tmp",
	}

	resp, err := server.Diagnose(ctx, req)
	if err != nil {
		t.Fatalf("Diagnose returned error: %v", err)
	}

	// Should return explanation about failure
	if resp.Explanation == "" {
		t.Error("expected explanation on provider failure")
	}
}

// --- Session context in AI calls ---

func TestHandler_TextToCommand_UsesSessionContext(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session with specific OS and shell
	startReq := &pb.SessionStartRequest{
		SessionId: "context-session",
		Cwd:       "/home/user",
		Client: &pb.ClientInfo{
			Shell: "fish",
			Os:    "linux",
		},
	}
	_, _ = server.SessionStart(ctx, startReq)

	req := &pb.TextToCommandRequest{
		SessionId: "context-session",
		Prompt:    "list files",
		Cwd:       "/home/user",
	}

	resp, err := server.TextToCommand(ctx, req)
	if err != nil {
		t.Fatalf("TextToCommand failed: %v", err)
	}

	// The mock provider should have received the session context
	// We can't easily verify the context was passed, but at least the call succeeded
	if resp.Provider != "test" {
		t.Errorf("expected provider 'test', got %s", resp.Provider)
	}
}

func TestHandler_NextStep_UsesSessionContext(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session with specific OS and shell
	startReq := &pb.SessionStartRequest{
		SessionId: "nextstep-context-session",
		Cwd:       "/home/user",
		Client: &pb.ClientInfo{
			Shell: "fish",
			Os:    "linux",
		},
	}
	_, _ = server.SessionStart(ctx, startReq)

	req := &pb.NextStepRequest{
		SessionId:    "nextstep-context-session",
		LastCommand:  "cd /var/log",
		LastExitCode: 0,
		Cwd:          "/var/log",
	}

	resp, err := server.NextStep(ctx, req)
	if err != nil {
		t.Fatalf("NextStep failed: %v", err)
	}

	// Should return suggestions
	if len(resp.Suggestions) == 0 {
		t.Error("expected suggestions")
	}
}

func TestHandler_Diagnose_UsesSessionContext(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session with specific OS and shell
	startReq := &pb.SessionStartRequest{
		SessionId: "diagnose-context-session",
		Cwd:       "/home/user",
		Client: &pb.ClientInfo{
			Shell: "fish",
			Os:    "linux",
		},
	}
	_, _ = server.SessionStart(ctx, startReq)

	req := &pb.DiagnoseRequest{
		SessionId: "diagnose-context-session",
		Command:   "npm install",
		ExitCode:  1,
		Cwd:       "/home/user/project",
	}

	resp, err := server.Diagnose(ctx, req)
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	// Should return explanation and fixes
	if resp.Explanation == "" {
		t.Error("expected explanation")
	}
}

// --- Multiple destructive commands ---

func TestHandler_Suggest_MultipleDestructivePatterns(t *testing.T) {
	t.Parallel()

	destructiveCommands := []string{
		"rm -rf /",
		"rm -r /home/user",
		"rm -f important.txt",
		"git reset --hard HEAD",
		"git push --force",
		"DROP TABLE users;",
		"kubectl delete pod myapp",
		"docker system prune -a",
		"chmod 777 /etc/passwd",
		"dd if=/dev/zero of=/dev/sda",
	}

	for _, destructiveCmd := range destructiveCommands {
		t.Run(destructiveCmd, func(t *testing.T) {
			ctx := context.Background()
			v2db := newTestDB(t)

			// Seed DB with this destructive command so suggest can find it
			if err := ops.CreateSession(ctx, v2db, &ops.Session{
				SessionID:   "test-session",
				Shell:       "zsh",
				StartedAtMs: time.Now().UnixMilli(),
			}); err != nil {
				t.Fatalf("failed to create session: %v", err)
			}
			ec := 0
			seedCmd := ops.Command{
				CommandID: "cmd-destructive",
				SessionID: "test-session",
				CmdRaw:    destructiveCmd,
				CmdNorm:   strings.ToLower(destructiveCmd),
				TSStartMs: time.Now().UnixMilli(),
				CWD:       "/tmp",
				ExitCode:  &ec,
			}
			if err := ops.CreateCommand(ctx, v2db, &seedCmd); err != nil {
				t.Fatalf("failed to create command: %v", err)
			}

			server, err := NewServer(&ServerConfig{
				DB: v2db,
			})
			if err != nil {
				t.Fatalf("failed to create server: %v", err)
			}

			req := &pb.SuggestRequest{
				SessionId:  "test-session",
				Cwd:        "/tmp",
				Buffer:     "",
				MaxResults: 5,
			}

			resp, err := server.Suggest(ctx, req)
			if err != nil {
				t.Fatalf("Suggest failed: %v", err)
			}

			// With V2 scorer, the seeded destructive command should appear
			// and be flagged as destructive if returned
			for _, s := range resp.Suggestions {
				if s.Text == destructiveCmd && s.Risk != "destructive" {
					t.Errorf("expected %q to be flagged as destructive, got risk=%q", destructiveCmd, s.Risk)
				}
			}
		})
	}
}

func TestHandler_Suggest_SafeCommands(t *testing.T) {
	t.Parallel()

	safeCommands := []string{
		"ls -la",
		"git status",
		"echo hello",
		"cat file.txt",
		"grep pattern file",
		"cd /home/user",
		"pwd",
		"mkdir newdir",
		"npm install",
		"go build",
	}

	for _, safeCmd := range safeCommands {
		t.Run(safeCmd, func(t *testing.T) {
			ctx := context.Background()
			v2db := newTestDB(t)

			// Seed DB with this safe command so suggest can find it
			if err := ops.CreateSession(ctx, v2db, &ops.Session{
				SessionID:   "test-session",
				Shell:       "zsh",
				StartedAtMs: time.Now().UnixMilli(),
			}); err != nil {
				t.Fatalf("failed to create session: %v", err)
			}
			ec := 0
			seedCmd := ops.Command{
				CommandID: "cmd-safe",
				SessionID: "test-session",
				CmdRaw:    safeCmd,
				CmdNorm:   strings.ToLower(safeCmd),
				TSStartMs: time.Now().UnixMilli(),
				CWD:       "/tmp",
				ExitCode:  &ec,
			}
			if err := ops.CreateCommand(ctx, v2db, &seedCmd); err != nil {
				t.Fatalf("failed to create command: %v", err)
			}

			server, err := NewServer(&ServerConfig{
				DB: v2db,
			})
			if err != nil {
				t.Fatalf("failed to create server: %v", err)
			}

			req := &pb.SuggestRequest{
				SessionId:  "test-session",
				Cwd:        "/tmp",
				Buffer:     "",
				MaxResults: 5,
			}

			resp, err := server.Suggest(ctx, req)
			if err != nil {
				t.Fatalf("Suggest failed: %v", err)
			}

			// With V2 scorer, check that any returned suggestions for this command are safe
			for _, s := range resp.Suggestions {
				if s.Text == safeCmd && s.Risk != "" {
					t.Errorf("expected %q to be safe (empty risk), got risk=%q", safeCmd, s.Risk)
				}
			}
		})
	}
}

// --- CommandEnded counter verification ---

func TestHandler_CommandEnded_MultipleCommands(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	// Start a session
	startReq := &pb.SessionStartRequest{
		SessionId: "multi-cmd-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// Execute multiple commands
	numCommands := 5
	for i := 0; i < numCommands; i++ {
		cmdStartReq := &pb.CommandStartRequest{
			SessionId: "multi-cmd-session",
			CommandId: fmt.Sprintf("cmd-%d", i),
			Cwd:       "/tmp",
			Command:   fmt.Sprintf("echo %d", i),
		}
		_, _ = server.CommandStarted(ctx, cmdStartReq)

		cmdEndReq := &pb.CommandEndRequest{
			SessionId:  "multi-cmd-session",
			CommandId:  fmt.Sprintf("cmd-%d", i),
			ExitCode:   0,
			DurationMs: 10,
		}
		_, _ = server.CommandEnded(ctx, cmdEndReq)
	}

	// Verify counter
	if server.getCommandsLogged() != int64(numCommands) {
		t.Errorf("expected %d commands logged, got %d", numCommands, server.getCommandsLogged())
	}
}

// --- GetStatus with version ---

func TestHandler_GetStatus_ReturnsVersion(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

	resp, err := server.GetStatus(ctx, &pb.Ack{})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	// Version should be set (defaults to "dev")
	if resp.Version == "" {
		t.Error("expected version to be set")
	}
}

// ============================================================================
// FetchHistory handler tests
// ============================================================================

func createTestServerWithCommands(t *testing.T) *Server {
	t.Helper()

	ctx := context.Background()
	v2db := newTestDB(t)

	// Add sessions
	for _, sid := range []string{"session-1", "session-2"} {
		if err := ops.CreateSession(ctx, v2db, &ops.Session{
			SessionID:   sid,
			Shell:       "zsh",
			StartedAtMs: 1000,
		}); err != nil {
			t.Fatalf("failed to create session %s: %v", sid, err)
		}
	}

	// Add commands with timestamps
	cmds := []ops.Command{
		{CommandID: "cmd-1", SessionID: "session-1", CmdRaw: "git status", CmdNorm: "git status", TSStartMs: 1000, CWD: "/tmp"},
		{CommandID: "cmd-2", SessionID: "session-1", CmdRaw: "git log", CmdNorm: "git log", TSStartMs: 2000, CWD: "/tmp"},
		{CommandID: "cmd-3", SessionID: "session-1", CmdRaw: "git status", CmdNorm: "git status", TSStartMs: 3000, CWD: "/tmp"},
		{CommandID: "cmd-4", SessionID: "session-2", CmdRaw: "ls -la", CmdNorm: "ls -la", TSStartMs: 4000, CWD: "/tmp"},
		{CommandID: "cmd-5", SessionID: "session-2", CmdRaw: "echo hello", CmdNorm: "echo hello", TSStartMs: 5000, CWD: "/tmp"},
	}
	for i := range cmds {
		if err := ops.CreateCommand(ctx, v2db, &cmds[i]); err != nil {
			t.Fatalf("failed to create command %s: %v", cmds[i].CommandID, err)
		}
	}

	server, err := NewServer(&ServerConfig{
		DB:          v2db,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server
}

func TestHandler_FetchHistory_GlobalQuery(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	req := &pb.HistoryFetchRequest{
		Global: true,
		Limit:  50,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	// Should have 4 deduplicated commands (git status, git log, ls -la, echo hello)
	if len(resp.Items) != 4 {
		t.Errorf("expected 4 items, got %d", len(resp.Items))
	}

	if !resp.AtEnd {
		t.Error("expected at_end=true when all results fit")
	}
}

func TestHandler_FetchHistory_SessionScoped(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	req := &pb.HistoryFetchRequest{
		SessionId: "session-1",
		Limit:     50,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	// session-1 has git status (x2 deduped) and git log = 2 unique commands
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items for session-1, got %d", len(resp.Items))
	}
}

func TestHandler_FetchHistory_SubstringFilter(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	req := &pb.HistoryFetchRequest{
		Global: true,
		Query:  "git",
		Limit:  50,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	// Only "git status" and "git log" match "git"
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items matching 'git', got %d", len(resp.Items))
	}
}

func TestHandler_FetchHistory_Deduplication(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	req := &pb.HistoryFetchRequest{
		SessionId: "session-1",
		Query:     "status",
		Limit:     50,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	// "git status" appears twice (ts=1000, ts=3000), should dedup to 1
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 deduplicated item, got %d", len(resp.Items))
	}

	// Should keep the most recent timestamp
	if resp.Items[0].TimestampMs != 3000 {
		t.Errorf("expected most recent timestamp 3000, got %d", resp.Items[0].TimestampMs)
	}
}

func TestHandler_FetchHistory_Pagination(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	// First page: limit=2
	req := &pb.HistoryFetchRequest{
		Global: true,
		Limit:  2,
		Offset: 0,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items on first page, got %d", len(resp.Items))
	}

	if resp.AtEnd {
		t.Error("expected at_end=false on first page when more items exist")
	}

	// Second page: offset=2, limit=2
	req2 := &pb.HistoryFetchRequest{
		Global: true,
		Limit:  2,
		Offset: 2,
	}

	resp2, err := server.FetchHistory(ctx, req2)
	if err != nil {
		t.Fatalf("FetchHistory page 2 failed: %v", err)
	}

	if len(resp2.Items) != 2 {
		t.Errorf("expected 2 items on second page, got %d", len(resp2.Items))
	}

	if !resp2.AtEnd {
		t.Error("expected at_end=true on last page")
	}
}

func TestHandler_FetchHistory_ANSIStripping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	v2db := newTestDB(t)

	// Create session for the command
	if err := ops.CreateSession(ctx, v2db, &ops.Session{
		SessionID:   "session-1",
		Shell:       "zsh",
		StartedAtMs: 1000,
	}); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	cmd := ops.Command{
		CommandID: "cmd-ansi",
		SessionID: "session-1",
		CmdRaw:    "\x1b[32mgit\x1b[0m status",
		CmdNorm:   "git status",
		TSStartMs: 1000,
		CWD:       "/tmp",
	}
	if err := ops.CreateCommand(ctx, v2db, &cmd); err != nil {
		t.Fatalf("failed to create command: %v", err)
	}

	server, err := NewServer(&ServerConfig{
		DB:          v2db,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := &pb.HistoryFetchRequest{
		Global: true,
		Limit:  50,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}

	expected := "git status"
	if resp.Items[0].Command != expected {
		t.Errorf("expected ANSI-stripped command %q, got %q", expected, resp.Items[0].Command)
	}
}

func TestHandler_FetchHistory_DefaultLimit(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	// Limit=0 should use default of 50
	req := &pb.HistoryFetchRequest{
		Global: true,
		Limit:  0,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	// Should still return results (4 unique commands)
	if len(resp.Items) != 4 {
		t.Errorf("expected 4 items with default limit, got %d", len(resp.Items))
	}
}

func TestHandler_FetchHistory_EmptyResult(t *testing.T) {
	t.Parallel()

	server := createTestServerWithCommands(t)
	ctx := context.Background()

	req := &pb.HistoryFetchRequest{
		Global: true,
		Query:  "nonexistent-command-xyz",
		Limit:  50,
	}

	resp, err := server.FetchHistory(ctx, req)
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items for non-matching query, got %d", len(resp.Items))
	}

	if !resp.AtEnd {
		t.Error("expected at_end=true for empty result")
	}
}

func TestHandler_FetchHistory_V2SearchPaginationOffset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "history_v2_offset.db")
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 DB: %v", err)
	}
	defer v2db.Close()
	if _, ftsErr := v2db.DB().ExecContext(ctx, `
		CREATE VIRTUAL TABLE IF NOT EXISTS command_fts USING fts5(cmd_raw, repo_key, cwd)
	`); ftsErr != nil {
		t.Fatalf("failed to create command_fts: %v", ftsErr)
	}

	for i := 1; i <= 4; i++ {
		cmd := fmt.Sprintf("git cmd %d", i)
		res, insertErr := v2db.DB().ExecContext(ctx, `
			INSERT INTO command_event (session_id, ts_ms, cwd, repo_key, cmd_raw, cmd_norm, ephemeral)
			VALUES (?, ?, ?, ?, ?, ?, 0)
		`, "sess-v2", int64(1000+i), "/tmp", "repo-a", cmd, cmd)
		if insertErr != nil {
			t.Fatalf("insert command_event failed: %v", insertErr)
		}
		id, idErr := res.LastInsertId()
		if idErr != nil {
			t.Fatalf("last insert id failed: %v", idErr)
		}
		if _, ftsInsertErr := v2db.DB().ExecContext(ctx, `
			INSERT INTO command_fts(rowid, cmd_raw, repo_key, cwd)
			VALUES (?, ?, ?, ?)
		`, id, cmd, "repo-a", "/tmp"); ftsInsertErr != nil {
			t.Fatalf("insert command_fts failed: %v", ftsInsertErr)
		}
	}

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	page1, err := server.FetchHistory(ctx, &pb.HistoryFetchRequest{
		Global:  true,
		Query:   "git",
		RepoKey: "repo-a",
		Mode:    pb.SearchMode_SEARCH_MODE_FTS,
		Scope:   "global",
		Limit:   2,
		Offset:  0,
	})
	if err != nil {
		t.Fatalf("FetchHistory page1 failed: %v", err)
	}
	page2, err := server.FetchHistory(ctx, &pb.HistoryFetchRequest{
		Global:  true,
		Query:   "git",
		RepoKey: "repo-a",
		Mode:    pb.SearchMode_SEARCH_MODE_FTS,
		Scope:   "global",
		Limit:   2,
		Offset:  2,
	})
	if err != nil {
		t.Fatalf("FetchHistory page2 failed: %v", err)
	}

	if len(page1.Items) != 2 || len(page2.Items) != 2 {
		t.Fatalf("expected two items per page, got page1=%d page2=%d", len(page1.Items), len(page2.Items))
	}

	seen := make(map[string]struct{}, len(page1.Items))
	for _, it := range page1.Items {
		seen[it.Command] = struct{}{}
	}
	for _, it := range page2.Items {
		if _, ok := seen[it.Command]; ok {
			t.Fatalf("expected non-overlapping pages, found duplicate command %q", it.Command)
		}
	}
}

func TestHandler_FetchHistory_V2SearchFallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	v2db := newTestDB(t)

	// Populate with real data via ops
	if err := ops.CreateSession(ctx, v2db, &ops.Session{
		SessionID:   "sess-fallback",
		Shell:       "zsh",
		StartedAtMs: 1000,
	}); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	cmd := ops.Command{
		CommandID: "cmd-fallback-1",
		SessionID: "sess-fallback",
		CmdRaw:    "git status",
		CmdNorm:   "git status",
		TSStartMs: 1000,
		CWD:       "/tmp",
	}
	if err := ops.CreateCommand(ctx, v2db, &cmd); err != nil {
		t.Fatalf("failed to create command: %v", err)
	}

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	resp, err := server.FetchHistory(ctx, &pb.HistoryFetchRequest{
		Global: true,
		Query:  "git",
		Mode:   pb.SearchMode_SEARCH_MODE_FTS,
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("expected FetchHistory to return results")
	}
}

func TestStripANSI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no ANSI codes",
			input:    "git status",
			expected: "git status",
		},
		{
			name:     "color codes",
			input:    "\x1b[32mgit\x1b[0m status",
			expected: "git status",
		},
		{
			name:     "bold and reset",
			input:    "\x1b[1mhello\x1b[0m world",
			expected: "hello world",
		},
		{
			name:     "multiple codes",
			input:    "\x1b[31;1merror\x1b[0m: \x1b[33mwarning\x1b[0m",
			expected: "error: warning",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "cursor movement",
			input:    "text\x1b[2Amore",
			expected: "textmore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripANSI(tt.input)
			if result != tt.expected {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// ImportHistory V2 backfill tests
// ============================================================================

// TestImportHistory_V2BackfillCalled verifies that V2 backfill writes
// command_event rows into the V2 database after a successful V1 import.
func TestImportHistory_V2BackfillCalled(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "v2_backfill_test.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	// Create a bash history file with timestamped entries
	histPath := filepath.Join(tmpDir, "bash_history")
	histContent := "#1700000000\ngit status\n#1700000100\nls -la\n#1700000200\necho hello\n"
	if writeErr := writeTestFile(histPath, histContent); writeErr != nil {
		t.Fatalf("failed to write test history file: %v", writeErr)
	}

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	req := &pb.HistoryImportRequest{
		Shell:       "bash",
		HistoryPath: histPath,
	}

	resp, err := server.ImportHistory(ctx, req)
	if err != nil {
		t.Fatalf("ImportHistory failed: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("ImportHistory returned error: %s", resp.Error)
	}

	if resp.ImportedCount != 3 {
		t.Errorf("expected ImportedCount=3, got %d", resp.ImportedCount)
	}

	// Verify V2 backfill wrote command_event rows
	var v2Count int
	err = v2db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM command_event WHERE session_id = 'backfill-bash'`,
	).Scan(&v2Count)
	if err != nil {
		t.Fatalf("failed to query V2 command_event: %v", err)
	}

	if v2Count != 3 {
		t.Errorf("expected 3 command_event rows in V2 DB, got %d", v2Count)
	}
}

// TestImportHistory_V2BackfillWithDB verifies that ImportHistory works
// with a V2 database and imports entries correctly.
func TestImportHistory_V2BackfillWithDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a bash history file
	histPath := filepath.Join(tmpDir, "bash_history")
	histContent := "#1700000000\ngit status\n#1700000100\nls -la\n"
	if err := writeTestFile(histPath, histContent); err != nil {
		t.Fatalf("failed to write test history file: %v", err)
	}

	v2db := newTestDB(t)
	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()
	req := &pb.HistoryImportRequest{
		Shell:       "bash",
		HistoryPath: histPath,
	}

	resp, err := server.ImportHistory(ctx, req)
	if err != nil {
		t.Fatalf("ImportHistory failed: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("ImportHistory returned error: %s", resp.Error)
	}

	if resp.ImportedCount != 2 {
		t.Errorf("expected ImportedCount=2, got %d", resp.ImportedCount)
	}
}

// TestImportHistory_V2BackfillFailureNonFatal verifies that if V2 backfill
// fails, the import response is still success with the correct count.
// TestImportHistory_V2BackfillFailureNonFatal was removed during V1->V2 migration.
// In V2, there is no separate backfill -- ImportHistory writes directly to the V2 DB,
// so a closed DB causes the import itself to fail (not just the backfill).

// ============================================================================
// CommandEnded V2 batch writer tests
// ============================================================================

// TestCommandEnded_FeedsV2 verifies that CommandEnded enqueues events to the
// V2 batch writer after V1 storage succeeds.
func TestCommandEnded_FeedsV2(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "v2_cmd_ended_test.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Start the batch writer
	server.batchWriter.Start()

	// Start a session
	_, err = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "v2-session",
		Cwd:       "/home/user",
		Client:    &pb.ClientInfo{Shell: "zsh"},
	})
	if err != nil {
		t.Fatalf("SessionStart failed: %v", err)
	}

	// CommandStarted stashes data for V2
	_, err = server.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId:   "v2-session",
		CommandId:   "v2-cmd-1",
		Cwd:         "/home/user/project",
		Command:     "make build",
		GitRepoName: "clai",
		GitBranch:   "main",
	})
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	// CommandEnded should enqueue to V2 batch writer
	endTS := time.Now().Add(-2 * time.Minute).UnixMilli()
	resp, err := server.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  "v2-session",
		CommandId:  "v2-cmd-1",
		ExitCode:   0,
		TsUnixMs:   endTS,
		DurationMs: 250,
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("CommandEnded returned ok=false: %s", resp.Error)
	}

	// Stop the batch writer to flush all pending events (blocks until done)
	server.batchWriter.Stop()

	// Verify event appears in V2 DB
	var v2Count int
	err = v2db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM command_event WHERE session_id = ? AND cmd_raw = ?`,
		"v2-session", "make build",
	).Scan(&v2Count)
	if err != nil {
		t.Fatalf("failed to query V2 command_event: %v", err)
	}

	// CommandStarted writes the initial row via ops.CreateCommand, and the
	// batch writer may upsert/update it on CommandEnded; expect at least 1 row.
	if v2Count < 1 {
		t.Errorf("expected at least 1 command_event row in V2 DB, got %d", v2Count)
	}

	// Verify exit code, duration, and timestamp in the V2 row
	var exitCode int
	var durationMs int64
	var ts int64
	err = v2db.DB().QueryRowContext(ctx,
		`SELECT exit_code, duration_ms, ts_ms FROM command_event WHERE session_id = ? AND cmd_raw = ?`,
		"v2-session", "make build",
	).Scan(&exitCode, &durationMs, &ts)
	if err != nil {
		t.Fatalf("failed to query V2 event details: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit_code=0, got %d", exitCode)
	}
	if durationMs != 250 {
		t.Errorf("expected duration_ms=250, got %d", durationMs)
	}
	if ts != endTS {
		t.Errorf("expected ts=%d from request, got %d", endTS, ts)
	}
}

// TestCommandEnded_V2NilGraceful was removed during V1->V2 migration.
// DB is now required by NewServer, so the nil-DB scenario no longer applies.

// TestCommandEnded_ExitCodeRecorded verifies that a non-zero exit code is
// correctly recorded in the V2 event.
func TestCommandEnded_ExitCodeRecorded(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "v2_exitcode_test.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	server.batchWriter.Start()

	// Start session + command
	_, _ = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "exitcode-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	})

	_, _ = server.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: "exitcode-session",
		CommandId: "exitcode-cmd",
		Cwd:       "/tmp",
		Command:   "false",
	})

	// CommandEnded with exit_code=1
	resp, err := server.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  "exitcode-session",
		CommandId:  "exitcode-cmd",
		ExitCode:   1,
		DurationMs: 10,
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("CommandEnded returned ok=false: %s", resp.Error)
	}

	// Stop the batch writer to flush all pending events (blocks until done)
	server.batchWriter.Stop()

	// Verify exit_code=1 in V2 DB
	var exitCode int
	err = v2db.DB().QueryRowContext(ctx,
		`SELECT exit_code FROM command_event WHERE session_id = ? AND cmd_raw = ?`,
		"exitcode-session", "false",
	).Scan(&exitCode)
	if err != nil {
		t.Fatalf("failed to query V2 event: %v", err)
	}

	if exitCode != 1 {
		t.Errorf("expected exit_code=1 in V2 event, got %d", exitCode)
	}
}

// writeTestFile is a helper that writes content to a file for testing.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
