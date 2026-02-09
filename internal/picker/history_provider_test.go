package picker

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/runger/clai/gen/clai/v1"
)

// testSocketCounter generates unique short socket names to stay within
// the macOS 104-character Unix socket path limit.
var testSocketCounter atomic.Uint64

// mockClaiService implements the FetchHistory RPC for testing.
type mockClaiService struct {
	pb.UnimplementedClaiServiceServer
	items    []*pb.HistoryItem
	atEnd    bool
	delay    time.Duration
	lastReq  *pb.HistoryFetchRequest
	failWith error
}

func (m *mockClaiService) FetchHistory(_ context.Context, req *pb.HistoryFetchRequest) (*pb.HistoryFetchResponse, error) {
	m.lastReq = req

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	if m.failWith != nil {
		return nil, m.failWith
	}

	return &pb.HistoryFetchResponse{
		Items: m.items,
		AtEnd: m.atEnd,
	}, nil
}

// startMockServer starts a gRPC server on a temporary Unix socket and returns
// the socket path. Uses /tmp directly with short names to stay within the
// macOS 104-character Unix socket path limit.
func startMockServer(t *testing.T, svc pb.ClaiServiceServer) string {
	t.Helper()

	id := testSocketCounter.Add(1)
	socketPath := fmt.Sprintf("/tmp/clai-hp-test-%d-%d.sock", os.Getpid(), id)
	startMockServerOnPath(t, svc, socketPath)
	return socketPath
}

func startMockServerOnPath(t *testing.T, svc pb.ClaiServiceServer, socketPath string) {
	t.Helper()

	_ = os.Remove(socketPath)
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()
	pb.RegisterClaiServiceServer(srv, svc)

	go func() {
		_ = srv.Serve(lis) // Error expected during cleanup when server is stopped.
	}()

	t.Cleanup(func() {
		srv.GracefulStop()
		os.Remove(socketPath)
	})
}

func TestHistoryProvider_BasicFetch(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "git status", TimestampMs: 3000},
			{Command: "git log", TimestampMs: 2000},
			{Command: "ls -la", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 42,
		Query:     "git",
		Limit:     50,
		Offset:    0,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if resp.RequestID != 42 {
		t.Errorf("expected RequestID 42, got %d", resp.RequestID)
	}

	if len(resp.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resp.Items))
	}

	if resp.Items[0].Value != "git status" {
		t.Errorf("expected first item 'git status', got %q", resp.Items[0].Value)
	}

	if !resp.AtEnd {
		t.Error("expected AtEnd=true")
	}
}

func TestHistoryProvider_Timeout(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "slow cmd", TimestampMs: 1000},
		},
		delay: 500 * time.Millisecond, // Well beyond the 200ms timeout
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Limit:     10,
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Verify it's a deadline exceeded error
	if st, ok := status.FromError(err); ok {
		if st.Code() != codes.DeadlineExceeded {
			t.Errorf("expected DeadlineExceeded, got %v", st.Code())
		}
	}
}

func TestHistoryProvider_EmptyResults(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: nil,
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 5,
		Query:     "nonexistent",
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Items))
	}

	if !resp.AtEnd {
		t.Error("expected AtEnd=true for empty results")
	}

	if resp.RequestID != 5 {
		t.Errorf("expected RequestID 5, got %d", resp.RequestID)
	}
}

func TestHistoryProvider_ANSIStripping(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "\x1b[32mgit\x1b[0m status", TimestampMs: 1000},
			{Command: "\x1b[1;31merror\x1b[0m command", TimestampMs: 2000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 10,
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}

	if resp.Items[0].Value != "git status" {
		t.Errorf("expected stripped 'git status', got %q", resp.Items[0].Value)
	}

	if resp.Items[1].Value != "error command" {
		t.Errorf("expected stripped 'error command', got %q", resp.Items[1].Value)
	}
}

func TestHistoryProvider_SessionScoping(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "echo hello", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 20,
		Limit:     50,
		Options: map[string]string{
			"session_id": "test-session-abc",
		},
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if svc.lastReq.SessionId != "test-session-abc" {
		t.Errorf("expected session_id 'test-session-abc', got %q", svc.lastReq.SessionId)
	}

	if svc.lastReq.Global {
		t.Error("expected Global=false when session_id is set")
	}
}

func TestHistoryProvider_SessionKeyFallback(t *testing.T) {
	t.Parallel()

	// Test that "session" key is accepted as a fallback for "session_id".
	// This allows config to use the shorter "session" key in tab Args.
	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "session cmd", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 21,
		Limit:     50,
		Options: map[string]string{
			"session": "short-key-session",
		},
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if svc.lastReq.SessionId != "short-key-session" {
		t.Errorf("expected session_id 'short-key-session' from 'session' key, got %q", svc.lastReq.SessionId)
	}
}

func TestHistoryProvider_SessionIDTakesPrecedence(t *testing.T) {
	t.Parallel()

	// When both "session_id" and "session" are present, "session_id" wins.
	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "cmd", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 22,
		Limit:     50,
		Options: map[string]string{
			"session_id": "primary-id",
			"session":    "fallback-id",
		},
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if svc.lastReq.SessionId != "primary-id" {
		t.Errorf("expected session_id 'primary-id' to take precedence, got %q", svc.lastReq.SessionId)
	}
}

func TestHistoryProvider_GlobalFlag(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "global cmd", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 30,
		Limit:     50,
		Options: map[string]string{
			"global": "true",
		},
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if !svc.lastReq.Global {
		t.Error("expected Global=true in gRPC request")
	}
}

func TestHistoryProvider_RequestIDPassthrough(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "cmd", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	requestIDs := []uint64{0, 1, 42, 999, 18446744073709551615}

	for _, id := range requestIDs {
		resp, err := provider.Fetch(context.Background(), Request{
			RequestID: id,
			Limit:     10,
		})
		if err != nil {
			t.Fatalf("Fetch with RequestID=%d failed: %v", id, err)
		}

		if resp.RequestID != id {
			t.Errorf("expected RequestID %d, got %d", id, resp.RequestID)
		}
	}
}

func TestHistoryProvider_QueryAndPaginationMapping(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "matched cmd", TimestampMs: 1000},
		},
		atEnd: false,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 7,
		Query:     "matched",
		Limit:     25,
		Offset:    10,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	// Verify all fields were mapped to the gRPC request
	if svc.lastReq.Query != "matched" {
		t.Errorf("expected query 'matched', got %q", svc.lastReq.Query)
	}

	if svc.lastReq.Limit != 25 {
		t.Errorf("expected limit 25, got %d", svc.lastReq.Limit)
	}

	if svc.lastReq.Offset != 10 {
		t.Errorf("expected offset 10, got %d", svc.lastReq.Offset)
	}

	if resp.AtEnd {
		t.Error("expected AtEnd=false")
	}
}

func TestHistoryProvider_RPCFailure(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		failWith: status.Error(codes.Internal, "database error"),
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Limit:     10,
	})
	if err == nil {
		t.Fatal("expected error on RPC failure, got nil")
	}
}

func TestHistoryProvider_DialFailure(t *testing.T) {
	t.Parallel()

	// Use a non-existent socket path
	provider := NewHistoryProvider("/tmp/nonexistent-clai-test-socket.sock")

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Limit:     10,
	})
	if err == nil {
		t.Fatal("expected error on dial failure, got nil")
	}
}

func TestHistoryProvider_RecoversWhenDefaultSocketAppears(t *testing.T) {
	id := testSocketCounter.Add(1)
	socketPath := fmt.Sprintf("/tmp/clai-hp-recover-%d-%d.sock", os.Getpid(), id)
	t.Setenv("CLAI_SOCKET", socketPath)

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "git status", TimestampMs: 1000},
		},
		atEnd: true,
	}
	provider := NewHistoryProvider(socketPath)

	recoveryCalled := false
	provider.ensureDaemon = func() error {
		recoveryCalled = true
		startMockServerOnPath(t, svc, socketPath)
		return nil
	}

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 99,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Fetch failed after recovery: %v", err)
	}
	if !recoveryCalled {
		t.Fatal("expected daemon recovery to be attempted")
	}
	if len(resp.Items) != 1 || resp.Items[0].Value != "git status" {
		t.Fatalf("unexpected items after recovery: %+v", resp.Items)
	}
}

func TestHistoryProvider_NilOptions(t *testing.T) {
	t.Parallel()

	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "cmd", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	// Options is nil — should not panic
	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Limit:     10,
		Options:   nil,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if svc.lastReq.SessionId != "" {
		t.Error("expected empty session_id with nil options")
	}

	if svc.lastReq.Global {
		t.Error("expected Global=false with nil options")
	}

	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(resp.Items))
	}
}

func TestHistoryProvider_UTF8WithMultibyteChars(t *testing.T) {
	t.Parallel()

	// gRPC enforces valid UTF-8 in protobuf string fields, so we test with
	// valid multi-byte UTF-8 combined with ANSI codes to verify the full
	// sanitization pipeline (StripANSI + ValidateUTF8) works end-to-end.
	svc := &mockClaiService{
		items: []*pb.HistoryItem{
			{Command: "\x1b[32mecho\x1b[0m '日本語'", TimestampMs: 2000},
			{Command: "echo '\u00e9\u00e8\u00ea'", TimestampMs: 1000},
		},
		atEnd: true,
	}
	socketPath := startMockServer(t, svc)
	provider := NewHistoryProvider(socketPath)

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}

	// ANSI should be stripped but multi-byte UTF-8 preserved
	if resp.Items[0].Value != "echo '日本語'" {
		t.Errorf("expected \"echo '日本語'\", got %q", resp.Items[0].Value)
	}

	if resp.Items[1].Value != "echo '\u00e9\u00e8\u00ea'" {
		t.Errorf("expected \"echo 'éèê'\", got %q", resp.Items[1].Value)
	}
}
