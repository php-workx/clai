package claiv1_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	claiv1 "github.com/runger/clai/gen/proto/clai/v1"
)

// testServer is a minimal implementation for testing
type testServer struct {
	claiv1.UnimplementedClaiServiceServer
}

func (s *testServer) Ping(ctx context.Context, req *claiv1.Ack) (*claiv1.Ack, error) {
	return &claiv1.Ack{Ok: true}, nil
}

func (s *testServer) GetStatus(ctx context.Context, req *claiv1.Ack) (*claiv1.StatusResponse, error) {
	return &claiv1.StatusResponse{
		Version:        "test",
		ActiveSessions: 0,
		UptimeSeconds:  42,
		CommandsLogged: 0,
	}, nil
}

// startTestServer creates a gRPC server and returns the client connection.
// The cleanup function should be called with defer.
func startTestServer(t *testing.T) (claiv1.ClaiServiceClient, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	server := grpc.NewServer()
	claiv1.RegisterClaiServiceServer(server, &testServer{})

	go func() {
		_ = server.Serve(listener) // Error on Stop() is expected
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		server.Stop()
		listener.Close()
		t.Fatalf("failed to create client: %v", err)
	}

	// Wait for server to be ready
	for i := 0; i < 10; i++ {
		client := claiv1.NewClaiServiceClient(conn)
		_, err := client.Ping(ctx, &claiv1.Ack{Ok: true})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cleanup := func() {
		conn.Close()
		server.Stop()
		listener.Close()
	}

	return claiv1.NewClaiServiceClient(conn), cleanup
}

func TestPingRPC(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Ping(ctx, &claiv1.Ack{Ok: true})
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if !resp.Ok {
		t.Errorf("Ping response ok = false, want true")
	}
}

func TestGetStatusRPC(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx, &claiv1.Ack{Ok: true})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Version != "test" {
		t.Errorf("GetStatus version = %q, want %q", status.Version, "test")
	}
	if status.UptimeSeconds != 42 {
		t.Errorf("GetStatus uptime = %d, want 42", status.UptimeSeconds)
	}
}

func TestMessageSerialization(t *testing.T) {
	tests := []struct {
		name string
		msg  interface{ String() string }
	}{
		{
			name: "ClientInfo",
			msg: &claiv1.ClientInfo{
				Version:  "1.0.0",
				Os:       "darwin",
				Shell:    "zsh",
				Hostname: "localhost",
				Username: "test",
			},
		},
		{
			name: "SessionStartRequest",
			msg: &claiv1.SessionStartRequest{
				Client: &claiv1.ClientInfo{
					Version: "1.0.0",
					Os:      "linux",
					Shell:   "bash",
				},
				SessionId:       "test-session-id",
				Cwd:             "/home/test",
				StartedAtUnixMs: 1234567890000,
			},
		},
		{
			name: "SuggestRequest",
			msg: &claiv1.SuggestRequest{
				SessionId:  "test-session",
				Cwd:        "/tmp",
				Buffer:     "git sta",
				CursorPos:  7,
				IncludeAi:  false,
				MaxResults: 5,
			},
		},
		{
			name: "Suggestion",
			msg: &claiv1.Suggestion{
				Text:        "git status",
				Description: "Show working tree status",
				Source:      "session",
				Score:       0.95,
				Risk:        "safe",
			},
		},
		{
			name: "DiagnoseResponse",
			msg: &claiv1.DiagnoseResponse{
				Explanation: "Command not found",
				Fixes: []*claiv1.Suggestion{
					{Text: "brew install foo", Description: "Install missing package"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the message can be stringified (basic serialization check)
			str := tt.msg.String()
			if str == "" {
				t.Errorf("%s.String() returned empty string", tt.name)
			}
		})
	}
}
