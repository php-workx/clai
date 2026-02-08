package picker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/cmdutil"
)

// fetchTimeout is the maximum time allowed for a single Fetch call RPC.
const fetchTimeout = 200 * time.Millisecond

// maxTopUpFetches bounds additional RPCs when display-level dedupe shrinks
// a page below the requested limit.
const maxTopUpFetches = 3

// HistoryProvider implements Provider using the daemon's FetchHistory gRPC RPC.
// It maintains a persistent gRPC connection for reduced latency.
type HistoryProvider struct {
	socketPath string

	mu     sync.Mutex
	conn   *grpc.ClientConn
	client pb.ClaiServiceClient
}

// Compile-time check that HistoryProvider implements Provider.
var _ Provider = (*HistoryProvider)(nil)

// NewHistoryProvider creates a provider that connects to the daemon socket.
// Call Close when done to release resources.
func NewHistoryProvider(socketPath string) *HistoryProvider {
	return &HistoryProvider{socketPath: socketPath}
}

// Close releases the gRPC connection. Safe to call multiple times.
func (p *HistoryProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		err := p.conn.Close()
		p.conn = nil
		p.client = nil
		return err
	}
	return nil
}

// getClient returns a ready gRPC client, creating or reconnecting as needed.
func (p *HistoryProvider) getClient() (pb.ClaiServiceClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if existing connection is usable
	if p.conn != nil {
		state := p.conn.GetState()
		if state == connectivity.Ready || state == connectivity.Idle {
			return p.client, nil
		}
		// Connection is in a bad state; close and recreate
		_ = p.conn.Close()
		p.conn = nil
		p.client = nil
	}

	// Create new connection
	conn, err := grpc.NewClient(
		"unix://"+p.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("history provider: dial: %w", err)
	}

	p.conn = conn
	p.client = pb.NewClaiServiceClient(conn)
	return p.client, nil
}

// Fetch calls the daemon's FetchHistory RPC and returns sanitized results.
func (p *HistoryProvider) Fetch(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	client, err := p.getClient()
	if err != nil {
		return Response{}, err
	}

	baseReq := &pb.HistoryFetchRequest{
		Query:  req.Query,
		Limit:  int32(req.Limit),
		Offset: int32(req.Offset),
	}

	// Map optional fields from Options map.
	if req.Options != nil {
		// Accept both "session_id" and "session" for the session filter.
		if sid, ok := req.Options["session_id"]; ok {
			baseReq.SessionId = sid
		} else if sid, ok := req.Options["session"]; ok {
			baseReq.SessionId = sid
		}
		if g, ok := req.Options["global"]; ok && g == "true" {
			baseReq.Global = true
		}
	}

	targetUnique := int(baseReq.Limit)
	nextOffset := int(baseReq.Offset)
	limit := int(baseReq.Limit)

	items := make([]string, 0, max(targetUnique, 16))
	seen := make(map[string]struct{}, max(targetUnique, 16))
	atEnd := false

	for attempt := 0; ; attempt++ {
		grpcReq := &pb.HistoryFetchRequest{
			Query:     baseReq.Query,
			Limit:     int32(limit),
			Offset:    int32(nextOffset),
			SessionId: baseReq.SessionId,
			Global:    baseReq.Global,
		}

		grpcResp, err := client.FetchHistory(ctx, grpcReq)
		if err != nil {
			return Response{}, fmt.Errorf("history provider: rpc: %w", err)
		}

		atEnd = grpcResp.AtEnd
		fetched := len(grpcResp.Items)

		for _, item := range grpcResp.Items {
			cmd := strings.TrimSpace(ValidateUTF8(StripANSI(item.Command)))
			if cmd == "" {
				continue
			}
			key := cmdutil.NormalizeCommand(cmd)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, cmd)
		}

		// When limit isn't set, preserve single-request semantics and rely on daemon defaults.
		if targetUnique <= 0 {
			break
		}
		if len(items) >= targetUnique {
			items = items[:targetUnique]
			break
		}
		if atEnd || fetched == 0 || attempt >= maxTopUpFetches {
			break
		}

		nextOffset += fetched
		limit = targetUnique - len(items)
	}

	return Response{
		RequestID: req.RequestID,
		Items:     items,
		AtEnd:     atEnd,
	}, nil
}
