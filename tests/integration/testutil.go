// Package integration provides end-to-end integration tests for clai.
// These tests verify the complete system including daemon, IPC, storage, and suggestions.
package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/storage"
)

// TestEnv holds all resources for an integration test.
type TestEnv struct {
	Client     pb.ClaiServiceClient
	Cancel     context.CancelFunc
	T          *testing.T
	Store      *storage.SQLiteStore
	Server     *daemon.Server
	Conn       *grpc.ClientConn
	Paths      *config.Paths
	TempDir    string
	DBPath     string
	SocketPath string
}

// SetupTestEnv creates a complete test environment with daemon, store, and client.
// The environment uses a temporary directory and in-memory IPC.
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "clai-integration-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create paths
	paths := &config.Paths{
		BaseDir: tempDir,
	}
	if dirErr := paths.EnsureDirectories(); dirErr != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create directories: %v", dirErr)
	}

	// Create SQLite store
	dbPath := filepath.Join(tempDir, "state.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create store: %v", err)
	}

	// Create mock provider for AI operations
	mockProv := newMockProvider("test-provider", true)
	registry := provider.NewRegistry()
	registry.Register(mockProv)
	registry.SetPreferred("test-provider")

	// Create server
	serverCfg := &daemon.ServerConfig{
		Store:       store,
		Registry:    registry,
		Paths:       paths,
		IdleTimeout: 30 * time.Minute, // Long timeout for tests
	}
	server, err := daemon.NewServer(serverCfg)
	if err != nil {
		store.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create server: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start server in background
	socketPath := paths.SocketFile()
	errChan := make(chan error, 1)
	go func() {
		if startErr := server.Start(ctx); startErr != nil {
			errChan <- startErr
		}
	}()

	// Wait for socket to be available or error
	if err = waitForSocket(socketPath, 5*time.Second); err != nil {
		select {
		case startErr := <-errChan:
			cancel()
			store.Close()
			os.RemoveAll(tempDir)
			t.Fatalf("server start error: %v", startErr)
		default:
			cancel()
			store.Close()
			os.RemoveAll(tempDir)
			t.Fatalf("failed to wait for socket: %v", err)
		}
	}

	// Connect gRPC client
	conn, client, err := dialSocket(socketPath)
	if err != nil {
		cancel()
		server.Shutdown()
		store.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to connect to server: %v", err)
	}

	return &TestEnv{
		T:          t,
		TempDir:    tempDir,
		DBPath:     dbPath,
		SocketPath: socketPath,
		Store:      store,
		Server:     server,
		Client:     client,
		Conn:       conn,
		Paths:      paths,
		Cancel:     cancel,
	}
}

// SetupTestEnvWithSuggestions creates a test environment with pre-populated command history.
func SetupTestEnvWithSuggestions(t *testing.T) *TestEnv {
	t.Helper()

	env := SetupTestEnv(t)

	ctx := context.Background()

	// Create a session first
	sessionID := "hist-session"
	_, err := env.Client.SessionStart(ctx, &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             "/home/test",
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client: &pb.ClientInfo{
			Shell:    "zsh",
			Os:       "linux",
			Hostname: "testhost",
			Username: "testuser",
		},
	})
	if err != nil {
		env.Teardown()
		t.Fatalf("failed to create session: %v", err)
	}

	// Add some command history
	commands := []struct {
		id      string
		cmd     string
		cwd     string
		success bool
	}{
		{"cmd-1", "git status", "/home/test/repo", true},
		{"cmd-2", "git diff", "/home/test/repo", true},
		{"cmd-3", "git commit -m 'test'", "/home/test/repo", true},
		{"cmd-4", "npm install", "/home/test/project", true},
		{"cmd-5", "npm test", "/home/test/project", false},
		{"cmd-6", "npm run build", "/home/test/project", true},
		{"cmd-7", "docker ps", "/home/test", true},
		{"cmd-8", "docker build .", "/home/test/app", true},
		{"cmd-9", "ls -la", "/home/test", true},
		{"cmd-10", "cd project", "/home/test", true},
	}

	for _, c := range commands {
		now := time.Now()

		// Log command start
		_, err := env.Client.CommandStarted(ctx, &pb.CommandStartRequest{
			SessionId: sessionID,
			CommandId: c.id,
			Cwd:       c.cwd,
			Command:   c.cmd,
			TsUnixMs:  now.UnixMilli(),
		})
		if err != nil {
			env.Teardown()
			t.Fatalf("failed to log command start: %v", err)
		}

		// Log command end
		exitCode := int32(0)
		if !c.success {
			exitCode = 1
		}
		_, err = env.Client.CommandEnded(ctx, &pb.CommandEndRequest{
			SessionId:  sessionID,
			CommandId:  c.id,
			ExitCode:   exitCode,
			DurationMs: 100,
			TsUnixMs:   now.Add(100 * time.Millisecond).UnixMilli(),
		})
		if err != nil {
			env.Teardown()
			t.Fatalf("failed to log command end: %v", err)
		}
	}

	return env
}

// Teardown cleans up all test resources.
func (e *TestEnv) Teardown() {
	if e.Conn != nil {
		e.Conn.Close()
	}
	if e.Cancel != nil {
		e.Cancel()
	}
	if e.Server != nil {
		e.Server.Shutdown()
	}
	if e.Store != nil {
		e.Store.Close()
	}
	if e.TempDir != "" {
		os.RemoveAll(e.TempDir)
	}
}

// waitForSocket waits for the Unix socket to become available.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			// Try to actually connect
			conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return os.ErrNotExist
}

// dialSocket creates a gRPC connection to the Unix socket.
func dialSocket(path string) (*grpc.ClientConn, pb.ClaiServiceClient, error) {
	// Use the new grpc.NewClient API with unix socket resolver
	conn, err := grpc.NewClient(
		"unix://"+path,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Verify connection by making a quick ping
	client := pb.NewClaiServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to connect by making a health check call
	_, err = client.Ping(ctx, &pb.Ack{Ok: true})
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}

	return conn, client, nil
}

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name       string
	suggestion string
	available  bool
}

func newMockProvider(name string, available bool) *mockProvider {
	return &mockProvider{
		name:       name,
		available:  available,
		suggestion: "echo 'mock command'",
	}
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
			{Text: m.suggestion, Description: "Mock suggestion", Source: "ai", Score: 0.9},
		},
		ProviderName: m.name,
		LatencyMs:    10,
	}, nil
}

func (m *mockProvider) NextStep(ctx context.Context, req *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	return &provider.NextStepResponse{
		Suggestions: []provider.Suggestion{
			{Text: "git push", Description: "Push changes", Source: "ai", Score: 0.8},
		},
		ProviderName: m.name,
		LatencyMs:    10,
	}, nil
}

func (m *mockProvider) Diagnose(ctx context.Context, req *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	return &provider.DiagnoseResponse{
		Explanation: "The command failed because of a mock error.",
		Fixes: []provider.Suggestion{
			{Text: "retry command", Description: "Try again", Source: "ai", Score: 0.9},
		},
		ProviderName: m.name,
		LatencyMs:    10,
	}, nil
}

// idCounter is used to generate unique IDs for testing.
var (
	idCounter   int64
	idCounterMu sync.Mutex
)

func nextID() int64 {
	idCounterMu.Lock()
	defer idCounterMu.Unlock()
	idCounter++
	return idCounter
}

// generateSessionID generates a unique session ID for testing.
func generateSessionID() string {
	return fmt.Sprintf("test-session-%d-%d", time.Now().UnixNano(), nextID())
}

// generateCommandID generates a unique command ID for testing.
func generateCommandID() string {
	return fmt.Sprintf("test-cmd-%d-%d", time.Now().UnixNano(), nextID())
}
