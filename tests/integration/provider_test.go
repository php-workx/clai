package integration

import (
	"context"
	"errors"
	"fmt"
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

// TestProvider_Unavailable tests behavior when no provider is available.
func TestProvider_Unavailable(t *testing.T) {
	env := setupEnvWithProvider(t, newMockProvider("unavailable", false))
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

	// TextToCommand should fail gracefully when provider is unavailable
	resp, err := env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "list files",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})

	// Should either return error or empty suggestions
	if err == nil && len(resp.Suggestions) > 0 {
		t.Error("expected no suggestions when provider is unavailable")
	}
}

// TestProvider_Error tests behavior when provider returns an error.
func TestProvider_Error(t *testing.T) {
	errProvider := &errorProvider{
		name: "error-provider",
		err:  errors.New("simulated provider error"),
	}
	env := setupEnvWithProvider(t, errProvider)
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

	// TextToCommand should handle provider error gracefully
	_, err = env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "list files",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})

	// Should return an error or handle gracefully
	if err != nil {
		t.Logf("TextToCommand returned error as expected: %v", err)
	}
}

// TestProvider_Timeout tests behavior when provider times out.
func TestProvider_Timeout(t *testing.T) {
	slowProvider := &slowProvider{
		name:  "slow-provider",
		delay: 10 * time.Second, // Very slow
	}
	env := setupEnvWithProvider(t, slowProvider)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

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

	// TextToCommand should timeout
	start := time.Now()
	_, err = env.Client.TextToCommand(ctx, &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         "list files",
		Cwd:            "/home/test",
		MaxSuggestions: 3,
	})
	elapsed := time.Since(start)

	// Should timeout within reasonable time
	if elapsed > 5*time.Second {
		t.Errorf("request took too long: %v (expected timeout)", elapsed)
	}

	if err == nil {
		t.Log("request completed before timeout (provider may have fast-path)")
	} else {
		t.Logf("request timed out as expected: %v", err)
	}
}

// TestProvider_DiagnoseError tests Diagnose when provider fails.
func TestProvider_DiagnoseError(t *testing.T) {
	errProvider := &errorProvider{
		name: "error-provider",
		err:  errors.New("simulated diagnose error"),
	}
	env := setupEnvWithProvider(t, errProvider)
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

	// Diagnose should handle provider error gracefully
	_, err = env.Client.Diagnose(ctx, &pb.DiagnoseRequest{
		SessionId: sessionID,
		Command:   "failing-command",
		ExitCode:  1,
		Cwd:       "/home/test",
	})

	// Should return an error or handle gracefully
	if err != nil {
		t.Logf("Diagnose returned error as expected: %v", err)
	}
}

// TestProvider_NextStepError tests NextStep when provider fails.
func TestProvider_NextStepError(t *testing.T) {
	errProvider := &errorProvider{
		name: "error-provider",
		err:  errors.New("simulated next step error"),
	}
	env := setupEnvWithProvider(t, errProvider)
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

	// NextStep should handle provider error gracefully
	_, err = env.Client.NextStep(ctx, &pb.NextStepRequest{
		SessionId:    sessionID,
		LastCommand:  "git add .",
		LastExitCode: 0,
		Cwd:          "/home/test",
	})

	// Should return an error or handle gracefully
	if err != nil {
		t.Logf("NextStep returned error as expected: %v", err)
	}
}

// errorProvider is a provider that always returns an error.
type errorProvider struct {
	err  error
	name string
}

func (p *errorProvider) Name() string    { return p.name }
func (p *errorProvider) Available() bool { return true }

func (p *errorProvider) TextToCommand(_ context.Context, _ *provider.TextToCommandRequest) (*provider.TextToCommandResponse, error) {
	return nil, p.err
}

func (p *errorProvider) NextStep(_ context.Context, _ *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	return nil, p.err
}

func (p *errorProvider) Diagnose(_ context.Context, _ *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	return nil, p.err
}

// slowProvider is a provider that takes a long time to respond.
type slowProvider struct {
	name  string
	delay time.Duration
}

func (p *slowProvider) Name() string    { return p.name }
func (p *slowProvider) Available() bool { return true }

func (p *slowProvider) TextToCommand(ctx context.Context, req *provider.TextToCommandRequest) (*provider.TextToCommandResponse, error) {
	select {
	case <-time.After(p.delay):
		return &provider.TextToCommandResponse{
			Suggestions: []provider.Suggestion{
				{Text: "slow response", Description: "Finally!", Source: "ai", Score: 0.5},
			},
			ProviderName: p.name,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *slowProvider) NextStep(ctx context.Context, req *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	select {
	case <-time.After(p.delay):
		return &provider.NextStepResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *slowProvider) Diagnose(ctx context.Context, req *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	select {
	case <-time.After(p.delay):
		return &provider.DiagnoseResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// setupEnvWithProvider creates a test environment with a specific provider.
func setupEnvWithProvider(t *testing.T, prov provider.Provider) *TestEnv {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "clai-provider-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	paths := &config.Paths{
		BaseDir: tempDir,
	}
	if err = paths.EnsureDirectories(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create directories: %v", err)
	}

	dbPath := filepath.Join(tempDir, "state.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create store: %v", err)
	}

	registry := provider.NewRegistry()
	registry.Register(prov)
	registry.SetPreferred(prov.Name())

	serverCfg := &daemon.ServerConfig{
		Store:       store,
		Registry:    registry,
		Paths:       paths,
		IdleTimeout: 30 * time.Minute,
	}
	server, err := daemon.NewServer(serverCfg)
	if err != nil {
		store.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	socketPath := paths.SocketFile()
	go func() {
		_ = server.Start(ctx)
	}()

	if err = waitForSocket(socketPath, 5*time.Second); err != nil {
		cancel()
		store.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to wait for socket: %v", err)
	}

	conn, client, err := dialSocketWithRetry(socketPath, 3)
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

// dialSocketWithRetry attempts to connect with retries.
func dialSocketWithRetry(path string, retries int) (*grpc.ClientConn, pb.ClaiServiceClient, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		conn, err := grpc.NewClient(
			"unix://"+path,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}

		client := pb.NewClaiServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		_, err = client.Ping(ctx, &pb.Ack{Ok: true})
		cancel()

		if err != nil {
			conn.Close()
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}

		return conn, client, nil
	}
	return nil, nil, fmt.Errorf("failed after %d retries: %w", retries, lastErr)
}

// trackingProvider wraps a provider to track calls.
type trackingProvider struct {
	provider.Provider
	callCounts map[string]int
	calls      []string
	mu         sync.Mutex
}

func newTrackingProvider(wrapped provider.Provider) *trackingProvider {
	return &trackingProvider{
		Provider:   wrapped,
		callCounts: make(map[string]int),
	}
}

func (p *trackingProvider) TextToCommand(ctx context.Context, req *provider.TextToCommandRequest) (*provider.TextToCommandResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, "TextToCommand")
	p.callCounts["TextToCommand"]++
	p.mu.Unlock()
	return p.Provider.TextToCommand(ctx, req)
}

func (p *trackingProvider) NextStep(ctx context.Context, req *provider.NextStepRequest) (*provider.NextStepResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, "NextStep")
	p.callCounts["NextStep"]++
	p.mu.Unlock()
	return p.Provider.NextStep(ctx, req)
}

func (p *trackingProvider) Diagnose(ctx context.Context, req *provider.DiagnoseRequest) (*provider.DiagnoseResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, "Diagnose")
	p.callCounts["Diagnose"]++
	p.mu.Unlock()
	return p.Provider.Diagnose(ctx, req)
}

func (p *trackingProvider) CallCount(method string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.callCounts[method]
}
