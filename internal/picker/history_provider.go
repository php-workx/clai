package picker

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/runger/clai/gen/clai/v1"
)

// fetchTimeout is the maximum time allowed for a single Fetch call,
// covering both connection establishment and the RPC itself.
const fetchTimeout = 200 * time.Millisecond

// HistoryProvider implements Provider using the daemon's FetchHistory gRPC RPC.
type HistoryProvider struct {
	socketPath string
}

// Compile-time check that HistoryProvider implements Provider.
var _ Provider = (*HistoryProvider)(nil)

// NewHistoryProvider creates a provider that connects to the daemon socket.
func NewHistoryProvider(socketPath string) *HistoryProvider {
	return &HistoryProvider{socketPath: socketPath}
}

// Fetch calls the daemon's FetchHistory RPC and returns sanitized results.
func (p *HistoryProvider) Fetch(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	conn, err := grpc.NewClient(
		"unix://"+p.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return Response{}, fmt.Errorf("history provider: dial: %w", err)
	}
	defer conn.Close()

	client := pb.NewClaiServiceClient(conn)

	grpcReq := &pb.HistoryFetchRequest{
		Query:  req.Query,
		Limit:  int32(req.Limit),
		Offset: int32(req.Offset),
	}

	// Map optional fields from Options map.
	if req.Options != nil {
		// Accept both "session_id" and "session" for the session filter.
		if sid, ok := req.Options["session_id"]; ok {
			grpcReq.SessionId = sid
		} else if sid, ok := req.Options["session"]; ok {
			grpcReq.SessionId = sid
		}
		if g, ok := req.Options["global"]; ok && g == "true" {
			grpcReq.Global = true
		}
	}

	grpcResp, err := client.FetchHistory(ctx, grpcReq)
	if err != nil {
		return Response{}, fmt.Errorf("history provider: rpc: %w", err)
	}

	items := make([]string, 0, len(grpcResp.Items))
	for _, item := range grpcResp.Items {
		cmd := ValidateUTF8(StripANSI(item.Command))
		items = append(items, cmd)
	}

	return Response{
		RequestID: req.RequestID,
		Items:     items,
		AtEnd:     grpcResp.AtEnd,
	}, nil
}
