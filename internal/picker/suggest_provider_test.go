package picker

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/runger/clai/gen/clai/v1"
)

type mockSuggestService struct {
	pb.UnimplementedClaiServiceServer
	suggestions []*pb.Suggestion
	delay       time.Duration
	lastReq     *pb.SuggestRequest
	failWith    error
}

func (m *mockSuggestService) Suggest(_ context.Context, req *pb.SuggestRequest) (*pb.SuggestResponse, error) {
	m.lastReq = req
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.failWith != nil {
		return nil, m.failWith
	}
	return &pb.SuggestResponse{Suggestions: m.suggestions}, nil
}

func TestSuggestProvider_BasicFetch_Detailed(t *testing.T) {
	t.Parallel()

	svc := &mockSuggestService{
		suggestions: []*pb.Suggestion{
			{Text: "git status", Source: "session", Score: 0.9, Risk: "safe", Description: "Working tree status"},
			{Text: "rm -rf /tmp/foo", Source: "global", Score: 0.2, Risk: "destructive"},
		},
	}
	socketPath := startMockServer(t, svc)
	provider := NewSuggestProvider(socketPath, "detailed")

	resp, err := provider.Fetch(context.Background(), Request{
		RequestID: 7,
		Query:     "g",
		Limit:     10,
		Options: map[string]string{
			"session_id": "sess-1",
			"cwd":        "/repo",
		},
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if resp.RequestID != 7 {
		t.Fatalf("expected RequestID=7, got %d", resp.RequestID)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].Value != "git status" {
		t.Fatalf("unexpected first item value: %q", resp.Items[0].Value)
	}
	if resp.Items[0].Display == "" {
		t.Fatalf("expected display to be set")
	}

	if svc.lastReq == nil {
		t.Fatalf("expected Suggest to be called")
	}
	if svc.lastReq.SessionId != "sess-1" {
		t.Fatalf("expected session_id to be passed, got %q", svc.lastReq.SessionId)
	}
	if svc.lastReq.Cwd != "/repo" {
		t.Fatalf("expected cwd to be passed, got %q", svc.lastReq.Cwd)
	}
	if svc.lastReq.Buffer != "g" {
		t.Fatalf("expected buffer to be passed, got %q", svc.lastReq.Buffer)
	}
}

func TestSuggestProvider_Timeout(t *testing.T) {
	t.Parallel()

	svc := &mockSuggestService{
		suggestions: []*pb.Suggestion{{Text: "slow"}},
		delay:       2 * time.Second,
	}
	socketPath := startMockServer(t, svc)
	provider := NewSuggestProvider(socketPath, "compact")

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Query:     "s",
		Limit:     10,
	})
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestSuggestProvider_RPCError_Propagates(t *testing.T) {
	t.Parallel()

	svc := &mockSuggestService{
		failWith: status.Error(codes.Internal, "boom"),
	}
	socketPath := startMockServer(t, svc)
	provider := NewSuggestProvider(socketPath, "compact")

	_, err := provider.Fetch(context.Background(), Request{
		RequestID: 1,
		Query:     "x",
		Limit:     10,
	})
	if err == nil {
		t.Fatalf("expected rpc error, got nil")
	}
}
