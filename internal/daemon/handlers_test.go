package daemon

import (
	"context"
	"testing"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/storage"
	"github.com/runger/clai/internal/suggest"
)

// mockStore implements storage.Store for testing.
type mockStore struct {
	sessions map[string]*storage.Session
	commands map[string]*storage.Command
	cache    map[string]*storage.CacheEntry
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

func (m *mockStore) CreateCommand(ctx context.Context, c *storage.Command) error {
	m.commands[c.CommandID] = c
	return nil
}

func (m *mockStore) UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error {
	if c, ok := m.commands[commandID]; ok {
		ec := exitCode
		c.ExitCode = &ec
		c.TsEndUnixMs = &endTime
		c.DurationMs = &duration
		c.IsSuccess = exitCode == 0
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

func (m *mockStore) Close() error {
	return nil
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
	available  bool
	suggestion string
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
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"hello", 0, ""},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}
