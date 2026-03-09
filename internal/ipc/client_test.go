package ipc

import (
	"context"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/runger/clai/gen/clai/v1"
)

// mockServer implements the ClaiService for testing
type mockServer struct {
	pb.UnimplementedClaiServiceServer
	sessionStartCalled  bool
	sessionEndCalled    bool
	commandStartCalled  bool
	commandEndCalled    bool
	suggestCalled       bool
	textToCommandCalled bool
	pingCalled          bool
	statusCalled        bool
}

func (m *mockServer) SessionStart(ctx context.Context, req *pb.SessionStartRequest) (*pb.Ack, error) {
	m.sessionStartCalled = true
	return &pb.Ack{Ok: true}, nil
}

func (m *mockServer) SessionEnd(ctx context.Context, req *pb.SessionEndRequest) (*pb.Ack, error) {
	m.sessionEndCalled = true
	return &pb.Ack{Ok: true}, nil
}

func (m *mockServer) CommandStarted(ctx context.Context, req *pb.CommandStartRequest) (*pb.Ack, error) {
	m.commandStartCalled = true
	return &pb.Ack{Ok: true}, nil
}

func (m *mockServer) CommandEnded(ctx context.Context, req *pb.CommandEndRequest) (*pb.Ack, error) {
	m.commandEndCalled = true
	return &pb.Ack{Ok: true}, nil
}

func (m *mockServer) Suggest(ctx context.Context, req *pb.SuggestRequest) (*pb.SuggestResponse, error) {
	m.suggestCalled = true
	return &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "ls -la", Description: "List files", Source: "history", Score: 0.9},
			{Text: "git status", Description: "Show git status", Source: "global", Score: 0.8},
		},
		FromCache: false,
	}, nil
}

func (m *mockServer) TextToCommand(ctx context.Context, req *pb.TextToCommandRequest) (*pb.TextToCommandResponse, error) {
	m.textToCommandCalled = true
	return &pb.TextToCommandResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "find . -name '*.go'", Description: "Find Go files", Score: 0.95},
		},
		Provider:  "mock",
		LatencyMs: 100,
	}, nil
}

func (m *mockServer) Ping(ctx context.Context, req *pb.Ack) (*pb.Ack, error) {
	m.pingCalled = true
	return &pb.Ack{Ok: true}, nil
}

func (m *mockServer) GetStatus(ctx context.Context, req *pb.Ack) (*pb.StatusResponse, error) {
	m.statusCalled = true
	return &pb.StatusResponse{
		Version:        "test-1.0",
		ActiveSessions: 2,
		UptimeSeconds:  3600,
		CommandsLogged: 100,
	}, nil
}

// startMockServer starts a mock gRPC server on a Unix socket
func startMockServer(t *testing.T) (string, *mockServer, func()) {
	t.Helper()

	// Create temp socket path
	tmpDir, err := os.MkdirTemp("", "clai-ipc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	sockPath := tmpDir + "/test.sock"

	// Create listener
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create listener: %v", err)
	}

	// Create server
	server := grpc.NewServer()
	mock := &mockServer{}
	pb.RegisterClaiServiceServer(server, mock)

	// Start serving in background
	go func() {
		_ = server.Serve(listener)
	}()

	cleanup := func() {
		server.Stop()
		listener.Close()
		os.RemoveAll(tmpDir)
	}

	return sockPath, mock, cleanup
}

func TestClientWithMockServer(t *testing.T) {
	sockPath, mock, cleanup := startMockServer(t)
	defer cleanup()

	// Override socket path
	os.Setenv("CLAI_SOCKET", sockPath)
	defer os.Unsetenv("CLAI_SOCKET")

	// Create client connection directly using blocking dial
	// For Unix sockets, we need to use "passthrough" scheme and connect directly
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sockPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//nolint:staticcheck // Using deprecated Dial for blocking connection behavior in tests
	conn, err := grpc.DialContext(
		ctx,
		"passthrough:///"+sockPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := NewClientWithConn(conn)
	defer client.Close()

	// Test SessionStart
	client.SessionStart("test-session", "/home/user", DefaultClientInfo("test"))
	// Note: fire-and-forget might not complete before check
	// In real test we'd need synchronization

	// Test Ping
	if !client.Ping() {
		t.Error("Ping() returned false")
	}
	if !mock.pingCalled {
		t.Error("Ping was not called on server")
	}

	// Test Suggest
	suggestions := client.Suggest(context.Background(), "test-session", "/home/user", "ls", 2, false, 5)
	if len(suggestions) != 2 {
		t.Errorf("Suggest() returned %d suggestions, want 2", len(suggestions))
	}
	if !mock.suggestCalled {
		t.Error("Suggest was not called on server")
	}

	// Test TextToCommand
	resp, err := client.TextToCommand(context.Background(), "test-session", "find go files", "/home/user", 3)
	if err != nil {
		t.Errorf("TextToCommand() error = %v", err)
	}
	if resp == nil || len(resp.Suggestions) != 1 {
		t.Error("TextToCommand() returned unexpected response")
	}
	if !mock.textToCommandCalled {
		t.Error("TextToCommand was not called on server")
	}

	// Test GetStatus
	status, err := client.GetStatus()
	if err != nil {
		t.Errorf("GetStatus() error = %v", err)
	}
	if status.Version != "test-1.0" {
		t.Errorf("GetStatus().Version = %q, want %q", status.Version, "test-1.0")
	}
	if !mock.statusCalled {
		t.Error("GetStatus was not called on server")
	}
}

func TestDefaultClientInfo(t *testing.T) {
	info := DefaultClientInfo("1.0.0")

	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.0.0")
	}

	if info.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
	}

	// Shell might be empty in some test environments - just verify it doesn't panic
	_ = info.Shell

	// Hostname should be populated (or empty string on error)
	// Just verify it doesn't panic
	_ = info.Hostname
	_ = info.Username
}

func TestClientInfoToProto(t *testing.T) {
	info := &ClientInfo{
		Version:  "1.0.0",
		OS:       "linux",
		Shell:    "/bin/bash",
		Hostname: "testhost",
		Username: "testuser",
	}

	proto := info.toProto()

	if proto.Version != info.Version {
		t.Errorf("proto.Version = %q, want %q", proto.Version, info.Version)
	}
	if proto.Os != info.OS {
		t.Errorf("proto.Os = %q, want %q", proto.Os, info.OS)
	}
	if proto.Shell != info.Shell {
		t.Errorf("proto.Shell = %q, want %q", proto.Shell, info.Shell)
	}
	if proto.Hostname != info.Hostname {
		t.Errorf("proto.Hostname = %q, want %q", proto.Hostname, info.Hostname)
	}
	if proto.Username != info.Username {
		t.Errorf("proto.Username = %q, want %q", proto.Username, info.Username)
	}
}

func TestClientInfoToProtoNil(t *testing.T) {
	var info *ClientInfo = nil
	proto := info.toProto()
	if proto != nil {
		t.Error("toProto() on nil should return nil")
	}
}

func TestNewClientNoSocket(t *testing.T) {
	// Use non-existent socket
	os.Setenv("CLAI_SOCKET", "/tmp/nonexistent-clai-client-test.sock")
	defer os.Unsetenv("CLAI_SOCKET")

	// Also ensure daemon can't be spawned
	os.Unsetenv("CLAI_DAEMON_PATH")
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", oldPath)

	client, err := NewClient()
	if err == nil && client != nil {
		client.Close()
		t.Error("NewClient() should fail when socket doesn't exist and daemon can't spawn")
	}
}

func TestSuggestDefaultMaxResults(t *testing.T) {
	sockPath, _, cleanup := startMockServer(t)
	defer cleanup()

	os.Setenv("CLAI_SOCKET", sockPath)
	defer os.Unsetenv("CLAI_SOCKET")

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sockPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//nolint:staticcheck // Using deprecated Dial for blocking connection behavior in tests
	conn, err := grpc.DialContext(
		ctx,
		"passthrough:///"+sockPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := NewClientWithConn(conn)
	defer client.Close()

	// Test with maxResults = 0 (should default to 5)
	suggestions := client.Suggest(context.Background(), "test", "/", "ls", 2, false, 0)
	if suggestions == nil {
		t.Error("Suggest with maxResults=0 returned nil")
	}
}

func TestTextToCommandDefaultMaxSuggestions(t *testing.T) {
	sockPath, _, cleanup := startMockServer(t)
	defer cleanup()

	os.Setenv("CLAI_SOCKET", sockPath)
	defer os.Unsetenv("CLAI_SOCKET")

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sockPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//nolint:staticcheck // Using deprecated Dial for blocking connection behavior in tests
	conn, err := grpc.DialContext(
		ctx,
		"passthrough:///"+sockPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := NewClientWithConn(conn)
	defer client.Close()

	// Test with maxSuggestions = 0 (should default to 3)
	resp, err := client.TextToCommand(context.Background(), "test", "find files", "/", 0)
	if err != nil {
		t.Errorf("TextToCommand with maxSuggestions=0 failed: %v", err)
	}
	if resp == nil {
		t.Error("TextToCommand with maxSuggestions=0 returned nil response")
	}
}
