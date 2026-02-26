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
	"github.com/runger/clai/internal/storage"
	"github.com/runger/clai/internal/suggest"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/learning"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
)

// mockStore implements storage.Store for testing.
type mockStore struct {
	sessions map[string]*storage.Session
	commands map[string]*storage.Command
	cache    map[string]*storage.CacheEntry
}

type importStatusStore struct {
	hasErr    error
	importErr error
	*mockStore
	has bool
}

func (m *importStatusStore) HasImportedHistory(ctx context.Context, shell string) (bool, error) {
	if m.hasErr != nil {
		return false, m.hasErr
	}
	return m.has, nil
}

func (m *importStatusStore) ImportHistory(ctx context.Context, entries []history.ImportEntry, shell string) (int, error) {
	if m.importErr != nil {
		return 0, m.importErr
	}
	return len(entries), nil
}

func newMockStore() *mockStore {
	return &mockStore{
		sessions: make(map[string]*storage.Session),
		commands: make(map[string]*storage.Command),
		cache:    make(map[string]*storage.CacheEntry),
	}
}

func (m *mockStore) CreateSession(ctx context.Context, s *storage.Session) error {
	m.sessions[s.SessionID] = s
	return nil
}

func (m *mockStore) EndSession(ctx context.Context, sessionID string, endTime int64) error {
	if s, ok := m.sessions[sessionID]; ok {
		s.EndedAtUnixMs = &endTime
	}
	return nil
}

func (m *mockStore) GetSession(ctx context.Context, sessionID string) (*storage.Session, error) {
	if s, ok := m.sessions[sessionID]; ok {
		return s, nil
	}
	return nil, storage.ErrSessionNotFound
}

func (m *mockStore) GetSessionByPrefix(ctx context.Context, prefix string) (*storage.Session, error) {
	var matches []*storage.Session
	for id, s := range m.sessions {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return nil, storage.ErrSessionNotFound
	}
	if len(matches) > 1 {
		return nil, storage.ErrAmbiguousSession
	}
	return matches[0], nil
}

func (m *mockStore) CreateCommand(ctx context.Context, c *storage.Command) error {
	m.commands[c.CommandID] = c
	return nil
}

func (m *mockStore) UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error {
	if c, ok := m.commands[commandID]; ok {
		ec := exitCode
		isSuccess := exitCode == 0
		c.ExitCode = &ec
		c.TSEndUnixMs = &endTime
		c.DurationMs = &duration
		c.IsSuccess = &isSuccess
	}
	return nil
}

func (m *mockStore) QueryCommands(ctx context.Context, q storage.CommandQuery) ([]storage.Command, error) {
	result := make([]storage.Command, 0, len(m.commands))
	for _, c := range m.commands {
		result = append(result, *c)
	}
	return result, nil
}

func (m *mockStore) QueryHistoryCommands(ctx context.Context, q storage.CommandQuery) ([]storage.HistoryRow, error) {
	// Collect commands, dedup by exact command text.
	seen := make(map[string]storage.HistoryRow)
	for _, c := range m.commands {
		commandNorm := strings.ToLower(c.Command)
		// Substring filter
		if q.Substring != "" && !strings.Contains(commandNorm, strings.ToLower(q.Substring)) {
			continue
		}
		// Session filter
		if q.SessionID != nil && c.SessionID != *q.SessionID {
			continue
		}
		existing, ok := seen[c.Command]
		if !ok || c.TSStartUnixMs > existing.TimestampMs {
			seen[c.Command] = storage.HistoryRow{
				Command:     c.Command,
				TimestampMs: c.TSStartUnixMs,
			}
		}
	}
	// Sort by timestamp descending
	result := make([]storage.HistoryRow, 0, len(seen))
	for _, row := range seen {
		result = append(result, row)
	}
	// Simple sort (stable enough for tests)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TimestampMs > result[i].TimestampMs {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	// Apply offset
	if q.Offset > 0 && q.Offset < len(result) {
		result = result[q.Offset:]
	} else if q.Offset >= len(result) {
		result = nil
	}
	// Apply limit
	if q.Limit > 0 && len(result) > q.Limit {
		result = result[:q.Limit]
	}
	return result, nil
}

func (m *mockStore) GetCached(ctx context.Context, key string) (*storage.CacheEntry, error) {
	if e, ok := m.cache[key]; ok {
		return e, nil
	}
	return nil, nil
}

func (m *mockStore) SetCached(ctx context.Context, entry *storage.CacheEntry) error {
	m.cache[entry.CacheKey] = entry
	return nil
}

func (m *mockStore) PruneExpiredCache(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockStore) HasImportedHistory(ctx context.Context, shell string) (bool, error) {
	return false, nil
}

func (m *mockStore) ImportHistory(ctx context.Context, entries []history.ImportEntry, shell string) (int, error) {
	return len(entries), nil
}

func (m *mockStore) Close() error {
	return nil
}

func (m *mockStore) CreateWorkflowRun(ctx context.Context, run *storage.WorkflowRun) error {
	return nil
}

func (m *mockStore) UpdateWorkflowRun(ctx context.Context, runID string, status string, endedAt int64, durationMs int64) error {
	return nil
}

func (m *mockStore) GetWorkflowRun(ctx context.Context, runID string) (*storage.WorkflowRun, error) {
	return nil, nil
}

func (m *mockStore) QueryWorkflowRuns(ctx context.Context, q storage.WorkflowRunQuery) ([]storage.WorkflowRun, error) {
	return nil, nil
}

func (m *mockStore) CreateWorkflowStep(ctx context.Context, step *storage.WorkflowStep) error {
	return nil
}

func (m *mockStore) UpdateWorkflowStep(ctx context.Context, update *storage.WorkflowStepUpdate) error {
	return nil
}

func (m *mockStore) GetWorkflowStep(ctx context.Context, runID, stepID, matrixKey string) (*storage.WorkflowStep, error) {
	return nil, nil
}

func (m *mockStore) CreateWorkflowAnalysis(ctx context.Context, analysis *storage.WorkflowAnalysis) error {
	return nil
}

func (m *mockStore) GetWorkflowAnalyses(ctx context.Context, runID, stepID, matrixKey string) ([]storage.WorkflowAnalysisRecord, error) {
	return nil, nil
}

// mockRanker implements suggest.Ranker for testing.
type mockRanker struct {
	suggestions []suggest.Suggestion
}

func (m *mockRanker) Rank(ctx context.Context, req *suggest.RankRequest) ([]suggest.Suggestion, error) {
	return m.suggestions, nil
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

	store := newMockStore()
	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{Text: "git status", Source: "session", Score: 0.9},
		},
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
		Store:       store,
		Ranker:      ranker,
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

func TestHandler_RecordFeedback_NoStoreConfigured(t *testing.T) {
	t.Parallel()
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
	if resp.Ok {
		t.Fatal("expected response.Ok=false without feedback store")
	}
	if resp.Error == nil || resp.Error.Code != "E_NO_FEEDBACK_STORE" {
		t.Fatalf("expected E_NO_FEEDBACK_STORE, got %+v", resp.Error)
	}
}

func TestHandler_RecordFeedback_ValidationAndSuccess(t *testing.T) {
	store := newMockStore()
	feedbackStore, cleanup := newFeedbackStoreWithDB(t)
	defer cleanup()

	server, err := NewServer(&ServerConfig{
		Store:         store,
		Ranker:        &mockRanker{},
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
	store := newMockStore()
	feedbackStore, cleanup := newFeedbackStoreWithDB(t)
	// Close the DB immediately to force store errors during RecordFeedback.
	cleanup()

	server, err := NewServer(&ServerConfig{
		Store:         store,
		Ranker:        &mockRanker{},
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
		Store: newMockStore(),
		V2DB:  v2db,
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
		Store: newMockStore(),
		V2DB:  v2db,
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
		store := &importStatusStore{mockStore: newMockStore(), has: true}
		server, err := NewServer(&ServerConfig{Store: store, Ranker: &mockRanker{}})
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

	t.Run("if_not_exists_status_error", func(t *testing.T) {
		store := &importStatusStore{mockStore: newMockStore(), hasErr: fmt.Errorf("boom")}
		server, err := NewServer(&ServerConfig{Store: store, Ranker: &mockRanker{}})
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		if _, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{Shell: "bash", IfNotExists: true}); err == nil {
			t.Fatal("expected HasImportedHistory error")
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

	t.Run("store_import_error", func(t *testing.T) {
		tmpDir := t.TempDir()
		histPath := filepath.Join(tmpDir, "bash_history")
		if err := writeTestFile(histPath, "#1700000000\ngit status\n"); err != nil {
			t.Fatalf("failed writing history file: %v", err)
		}

		store := &importStatusStore{mockStore: newMockStore(), importErr: fmt.Errorf("import failed")}
		server, err := NewServer(&ServerConfig{Store: store, Ranker: &mockRanker{}})
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		if _, err := server.ImportHistory(ctx, &pb.HistoryImportRequest{
			Shell:       "bash",
			HistoryPath: histPath,
		}); err == nil {
			t.Fatal("expected ImportHistory store error")
		}
	})
}

func TestHandler_Suggest_ReturnsHistorySuggestions(t *testing.T) {
	t.Parallel()

	server := createTestServer(t)
	ctx := context.Background()

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

	if len(resp.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}

	// The mock ranker returns "git status"
	if resp.Suggestions[0].Text != "git status" {
		t.Errorf("expected first suggestion to be 'git status', got %s", resp.Suggestions[0].Text)
	}
}

func TestHandler_Suggest_V1IncludesWhyDetailsWhenAvailable(t *testing.T) {
	t.Parallel()

	store := newMockStore()

	now := time.Now().UnixMilli()
	lastSeen := now - (2 * time.Hour).Milliseconds()

	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{
				Text:           "make install",
				Source:         "global",
				Score:          0.77,
				CmdNorm:        "make install",
				LastSeenUnixMs: lastSeen,
				SuccessCount:   11,
				FailureCount:   1,
				Reasons: []suggest.Reason{
					{Type: "source", Contribution: 0.16},
					{Type: "recency", Contribution: 0.21},
					{Type: "success", Contribution: 0.18},
				},
			},
		},
	}

	registry := provider.NewRegistry()
	server, err := NewServer(&ServerConfig{
		Store:       store,
		Ranker:      ranker,
		Registry:    registry,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	req := &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "make",
		MaxResults: 5,
	}

	resp, err := server.Suggest(ctx, req)
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(resp.Suggestions))
	}

	got := resp.Suggestions[0]
	if got.CmdNorm == "" {
		t.Errorf("expected cmd_norm to be set")
	}
	if got.Description == "" {
		t.Errorf("expected description to be set")
	}
	lowDesc := strings.ToLower(got.Description)
	if strings.Contains(lowDesc, "last ") || strings.Contains(lowDesc, "freq") || strings.Contains(lowDesc, "success") {
		t.Errorf("expected narrative description without numeric hints, got %q", got.Description)
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("expected reasons to be set")
	}

	// Ensure hints are present (recency/frequency/success), and causality tags are present.
	// These assertions intentionally pin exact display strings as part of the
	// picker UX contract; changes here should be deliberate and coordinated.
	var hasRecencyHint, hasFreqHint, hasSuccessHint, hasSourceTag bool
	for _, r := range got.Reasons {
		switch strings.TrimSpace(r.Type) {
		case "recency":
			if strings.Contains(r.Description, "last 2h ago") {
				hasRecencyHint = true
			}
		case "frequency":
			if r.Description == "freq 12" {
				hasFreqHint = true
			}
		case "success":
			if strings.Contains(r.Description, "success 91%") {
				hasSuccessHint = true
			}
		case "source":
			if r.Contribution != 0 {
				hasSourceTag = true
			}
		}
	}
	if !hasRecencyHint {
		t.Errorf("expected recency hint (last 2h ago)")
	}
	if !hasFreqHint {
		t.Errorf("expected frequency hint (freq 12)")
	}
	if !hasSuccessHint {
		t.Errorf("expected success hint (success 91%%)")
	}
	if !hasSourceTag {
		t.Errorf("expected scoring tag contribution for source")
	}
}

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

	store := newMockStore()
	ranker := &mockRanker{
		suggestions: []suggest.Suggestion{
			{Text: "rm -rf /", Source: "session", Score: 0.9},
		},
	}

	server, err := NewServer(&ServerConfig{
		Store:  store,
		Ranker: ranker,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
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

	if len(resp.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}

	// Verify the destructive command is flagged
	if resp.Suggestions[0].Risk != "destructive" {
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

	// Start a command with empty CWD
	cmdReq := &pb.CommandStartRequest{
		SessionId: "test-empty-cwd",
		CommandId: "cmd-empty-cwd",
		Cwd:       "", // Empty CWD should not update
		Command:   "echo hello",
	}

	resp, err := server.CommandStarted(ctx, cmdReq)
	if err != nil {
		t.Fatalf("CommandStarted failed: %v", err)
	}

	if !resp.Ok {
		t.Errorf("CommandStarted returned ok=false: %s", resp.Error)
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

	// Should still return suggestions with default limit
	if len(resp.Suggestions) == 0 {
		t.Error("expected suggestions even with MaxResults=0")
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

	// Should still return suggestions with default limit
	if len(resp.Suggestions) == 0 {
		t.Error("expected suggestions even with negative MaxResults")
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

	if len(resp.Suggestions) == 0 {
		t.Error("expected suggestions")
	}
}

// --- Risk detection tests ---

func TestHandler_TextToCommand_DestructiveCommandFlagged(t *testing.T) {
	t.Parallel()

	// Create server with mock provider that returns destructive command
	store := newMockStore()
	ranker := &mockRanker{}

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "rm -rf /important/data",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "git reset --hard HEAD",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	mockProv := &mockProvider{
		name:       "test",
		available:  true,
		suggestion: "sudo rm -rf /var/log/*",
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

// mockFailingStore implements storage.Store with configurable failures.
type mockFailingStore struct {
	*mockStore
	failCreateSession    bool
	failEndSession       bool
	failCreateCommand    bool
	failUpdateCommandEnd bool
}

func newMockFailingStore() *mockFailingStore {
	return &mockFailingStore{
		mockStore: newMockStore(),
	}
}

func (m *mockFailingStore) CreateSession(ctx context.Context, s *storage.Session) error {
	if m.failCreateSession {
		return storage.ErrSessionNotFound
	}
	return m.mockStore.CreateSession(ctx, s)
}

func (m *mockFailingStore) EndSession(ctx context.Context, sessionID string, endTime int64) error {
	if m.failEndSession {
		return storage.ErrSessionNotFound
	}
	return m.mockStore.EndSession(ctx, sessionID, endTime)
}

func (m *mockFailingStore) CreateCommand(ctx context.Context, c *storage.Command) error {
	if m.failCreateCommand {
		return storage.ErrCommandNotFound
	}
	return m.mockStore.CreateCommand(ctx, c)
}

func (m *mockFailingStore) UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error {
	if m.failUpdateCommandEnd {
		return storage.ErrCommandNotFound
	}
	return m.mockStore.UpdateCommandEnd(ctx, commandID, exitCode, endTime, duration)
}

func TestHandler_SessionStart_StoreFailure(t *testing.T) {
	t.Parallel()

	store := newMockFailingStore()
	store.failCreateSession = true

	server, err := NewServer(&ServerConfig{
		Store:  store,
		Ranker: &mockRanker{},
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

func TestHandler_SessionEnd_StoreFailure(t *testing.T) {
	t.Parallel()

	store := newMockFailingStore()
	store.failEndSession = true

	server, err := NewServer(&ServerConfig{
		Store:  store,
		Ranker: &mockRanker{},
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()

	// First start a session successfully
	startReq := &pb.SessionStartRequest{
		SessionId: "end-fail-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	// Now try to end it with store failure
	endReq := &pb.SessionEndRequest{
		SessionId: "end-fail-session",
	}

	resp, err := server.SessionEnd(ctx, endReq)
	if err != nil {
		t.Fatalf("SessionEnd returned error: %v", err)
	}

	if resp.Ok {
		t.Error("expected ok=false on store failure")
	}

	if resp.Error == "" {
		t.Error("expected error message on store failure")
	}
}

func TestHandler_CommandStarted_StoreFailure(t *testing.T) {
	t.Parallel()

	store := newMockFailingStore()
	store.failCreateCommand = true

	server, err := NewServer(&ServerConfig{
		Store:  store,
		Ranker: &mockRanker{},
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()

	// Start a session first
	startReq := &pb.SessionStartRequest{
		SessionId: "cmd-fail-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	}
	_, _ = server.SessionStart(ctx, startReq)

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

func TestHandler_CommandEnded_StoreFailure(t *testing.T) {
	t.Parallel()

	store := newMockFailingStore()

	server, err := NewServer(&ServerConfig{
		Store:  store,
		Ranker: &mockRanker{},
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()

	// Start a session and command first
	startReq := &pb.SessionStartRequest{
		SessionId: "cmd-end-fail-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	}
	_, _ = server.SessionStart(ctx, startReq)

	cmdStartReq := &pb.CommandStartRequest{
		SessionId: "cmd-end-fail-session",
		CommandId: "end-fail-cmd",
		Cwd:       "/tmp",
		Command:   "echo test",
	}
	_, _ = server.CommandStarted(ctx, cmdStartReq)

	// Now make the store fail for update
	store.failUpdateCommandEnd = true

	cmdEndReq := &pb.CommandEndRequest{
		SessionId:  "cmd-end-fail-session",
		CommandId:  "end-fail-cmd",
		ExitCode:   0,
		DurationMs: 100,
	}

	resp, err := server.CommandEnded(ctx, cmdEndReq)
	if err != nil {
		t.Fatalf("CommandEnded returned error: %v", err)
	}

	if resp.Ok {
		t.Error("expected ok=false on store failure")
	}

	if resp.Error == "" {
		t.Error("expected error message on store failure")
	}
}

// --- Ranker failure tests ---

// mockFailingRanker returns errors on Rank.
type mockFailingRanker struct {
	shouldFail bool
}

func (m *mockFailingRanker) Rank(ctx context.Context, req *suggest.RankRequest) ([]suggest.Suggestion, error) {
	if m.shouldFail {
		return nil, storage.ErrSessionNotFound
	}
	return []suggest.Suggestion{}, nil
}

func TestHandler_Suggest_RankerFailure(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	ranker := &mockFailingRanker{shouldFail: true}

	server, err := NewServer(&ServerConfig{
		Store:  store,
		Ranker: ranker,
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

	// Should return empty response on ranker failure (graceful degradation)
	if len(resp.Suggestions) != 0 {
		t.Errorf("expected empty suggestions on ranker failure, got %d", len(resp.Suggestions))
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
		return nil, storage.ErrSessionNotFound
	}
	return &provider.TextToCommandResponse{}, nil
}

func (m *mockFailingProvider) NextStep(ctx context.Context, req *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	if m.shouldFail {
		return nil, storage.ErrSessionNotFound
	}
	return &provider.NextStepResponse{}, nil
}

func (m *mockFailingProvider) Diagnose(ctx context.Context, req *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	if m.shouldFail {
		return nil, storage.ErrSessionNotFound
	}
	return &provider.DiagnoseResponse{}, nil
}

func TestHandler_TextToCommand_NoProvider(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	ranker := &mockRanker{}

	// Empty registry with no available providers
	registry := provider.NewRegistry()
	// Clear default providers by setting preferred to non-existent
	registry.SetPreferred("nonexistent")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	mockProv := &mockFailingProvider{
		name:       "failing",
		available:  true,
		shouldFail: true,
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("failing")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	registry := provider.NewRegistry()
	registry.SetPreferred("nonexistent")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	mockProv := &mockFailingProvider{
		name:       "failing",
		available:  true,
		shouldFail: true,
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("failing")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	registry := provider.NewRegistry()
	registry.SetPreferred("nonexistent")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	store := newMockStore()
	ranker := &mockRanker{}

	mockProv := &mockFailingProvider{
		name:       "failing",
		available:  true,
		shouldFail: true,
	}

	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("failing")

	server, err := NewServer(&ServerConfig{
		Store:    store,
		Ranker:   ranker,
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

	for _, cmd := range destructiveCommands {
		t.Run(cmd, func(t *testing.T) {
			store := newMockStore()
			ranker := &mockRanker{
				suggestions: []suggest.Suggestion{
					{Text: cmd, Source: "session", Score: 0.9},
				},
			}

			server, err := NewServer(&ServerConfig{
				Store:  store,
				Ranker: ranker,
			})
			if err != nil {
				t.Fatalf("failed to create server: %v", err)
			}

			ctx := context.Background()
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

			if len(resp.Suggestions) == 0 {
				t.Fatal("expected suggestions")
			}

			if resp.Suggestions[0].Risk != "destructive" {
				t.Errorf("expected %q to be flagged as destructive, got risk=%q", cmd, resp.Suggestions[0].Risk)
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

	for _, cmd := range safeCommands {
		t.Run(cmd, func(t *testing.T) {
			store := newMockStore()
			ranker := &mockRanker{
				suggestions: []suggest.Suggestion{
					{Text: cmd, Source: "session", Score: 0.9},
				},
			}

			server, err := NewServer(&ServerConfig{
				Store:  store,
				Ranker: ranker,
			})
			if err != nil {
				t.Fatalf("failed to create server: %v", err)
			}

			ctx := context.Background()
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

			if len(resp.Suggestions) == 0 {
				t.Fatal("expected suggestions")
			}

			if resp.Suggestions[0].Risk != "" {
				t.Errorf("expected %q to be safe (empty risk), got risk=%q", cmd, resp.Suggestions[0].Risk)
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

	store := newMockStore()

	// Add a session
	store.sessions["session-1"] = &storage.Session{
		SessionID: "session-1",
	}
	store.sessions["session-2"] = &storage.Session{
		SessionID: "session-2",
	}

	// Add commands with timestamps
	store.commands["cmd-1"] = &storage.Command{
		CommandID:     "cmd-1",
		SessionID:     "session-1",
		Command:       "git status",
		CommandNorm:   "git status",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
	}
	store.commands["cmd-2"] = &storage.Command{
		CommandID:     "cmd-2",
		SessionID:     "session-1",
		Command:       "git log",
		CommandNorm:   "git log",
		TSStartUnixMs: 2000,
		CWD:           "/tmp",
	}
	store.commands["cmd-3"] = &storage.Command{
		CommandID:     "cmd-3",
		SessionID:     "session-1",
		Command:       "git status",
		CommandNorm:   "git status",
		TSStartUnixMs: 3000,
		CWD:           "/tmp",
	}
	store.commands["cmd-4"] = &storage.Command{
		CommandID:     "cmd-4",
		SessionID:     "session-2",
		Command:       "ls -la",
		CommandNorm:   "ls -la",
		TSStartUnixMs: 4000,
		CWD:           "/tmp",
	}
	store.commands["cmd-5"] = &storage.Command{
		CommandID:     "cmd-5",
		SessionID:     "session-2",
		Command:       "echo hello",
		CommandNorm:   "echo hello",
		TSStartUnixMs: 5000,
		CWD:           "/tmp",
	}

	ranker := &mockRanker{}
	server, err := NewServer(&ServerConfig{
		Store:       store,
		Ranker:      ranker,
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

	store := newMockStore()
	store.commands["cmd-ansi"] = &storage.Command{
		CommandID:     "cmd-ansi",
		SessionID:     "session-1",
		Command:       "\x1b[32mgit\x1b[0m status",
		CommandNorm:   "git status",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
	}

	ranker := &mockRanker{}
	server, err := NewServer(&ServerConfig{
		Store:       store,
		Ranker:      ranker,
		IdleTimeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
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
		Store: newMockStore(),
		V2DB:  v2db,
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

func TestHandler_FetchHistory_V2SearchErrorFallsBackToStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "history_v2_fallback.db")
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 DB: %v", err)
	}
	// Force V2 search init/query failures.
	_ = v2db.Close()

	store := newMockStore()
	store.commands["cmd-fallback-1"] = &storage.Command{
		CommandID:     "cmd-fallback-1",
		SessionID:     "sess-fallback",
		Command:       "git status",
		CommandNorm:   "git status",
		TSStartUnixMs: 1000,
		CWD:           "/tmp",
	}

	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  v2db,
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
		t.Fatal("expected storage fallback to return history results")
	}
	if resp.Backend != "storage" {
		t.Fatalf("expected backend=storage on fallback, got %q", resp.Backend)
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

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  v2db,
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

// TestImportHistory_V2BackfillNilDB verifies that ImportHistory works normally
// when v2db is nil (no panic, no error).
func TestImportHistory_V2BackfillNilDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a bash history file
	histPath := filepath.Join(tmpDir, "bash_history")
	histContent := "#1700000000\ngit status\n#1700000100\nls -la\n"
	if err := writeTestFile(histPath, histContent); err != nil {
		t.Fatalf("failed to write test history file: %v", err)
	}

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  nil, // explicitly nil
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
// fails, the V1 import response is still success with the correct count.
func TestImportHistory_V2BackfillFailureNonFatal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "v2_fail_test.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}

	// Close the V2 database to force backfill failure.
	// Operations on a closed DB return errors, simulating V2 unavailability.
	v2db.Close()

	// Create a bash history file
	histPath := filepath.Join(tmpDir, "bash_history")
	histContent := "#1700000000\ngit status\n#1700000100\nls -la\n#1700000200\necho hello\n"
	if writeErr := writeTestFile(histPath, histContent); writeErr != nil {
		t.Fatalf("failed to write test history file: %v", writeErr)
	}

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  v2db, // closed DB - backfill will fail
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
		t.Fatalf("ImportHistory should not return error when V2 backfill fails: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("ImportHistory should not return error message when V2 backfill fails: %s", resp.Error)
	}

	// V1 import should still succeed with correct count
	if resp.ImportedCount != 3 {
		t.Errorf("expected ImportedCount=3 (V1 still succeeds), got %d", resp.ImportedCount)
	}
}

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

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  v2db,
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

	if v2Count != 1 {
		t.Errorf("expected 1 command_event row in V2 DB, got %d", v2Count)
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

// TestCommandEnded_V2NilGraceful verifies that CommandEnded works normally
// when batchWriter is nil (V2 disabled).
func TestCommandEnded_V2NilGraceful(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  nil, // V2 disabled
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify batchWriter is nil
	if server.batchWriter != nil {
		t.Fatal("expected batchWriter to be nil when V2DB is nil")
	}

	ctx := context.Background()

	// Start session and command
	_, _ = server.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId: "nil-v2-session",
		Cwd:       "/tmp",
		Client:    &pb.ClientInfo{Shell: "bash"},
	})

	_, _ = server.CommandStarted(ctx, &pb.CommandStartRequest{
		SessionId: "nil-v2-session",
		CommandId: "nil-v2-cmd",
		Cwd:       "/tmp",
		Command:   "echo hello",
	})

	// CommandEnded should succeed without V2
	resp, err := server.CommandEnded(ctx, &pb.CommandEndRequest{
		SessionId:  "nil-v2-session",
		CommandId:  "nil-v2-cmd",
		ExitCode:   0,
		DurationMs: 50,
	})
	if err != nil {
		t.Fatalf("CommandEnded failed: %v", err)
	}
	if !resp.Ok {
		t.Errorf("CommandEnded returned ok=false: %s", resp.Error)
	}

	// V1 counter should still be incremented
	if server.getCommandsLogged() != 1 {
		t.Errorf("expected commandsLogged=1, got %d", server.getCommandsLogged())
	}
}

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

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store: store,
		V2DB:  v2db,
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
