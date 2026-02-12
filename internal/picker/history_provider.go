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

// fetchOpts holds parsed options for a Fetch call.
type fetchOpts struct {
	sessionID     string
	global        bool
	caseSensitive bool
}

// parseFetchOpts extracts recognised option values from the request.
func parseFetchOpts(opts map[string]string) fetchOpts {
	if opts == nil {
		return fetchOpts{}
	}
	var fo fetchOpts
	if sid, ok := opts["session_id"]; ok {
		fo.sessionID = sid
	} else if sid, ok := opts["session"]; ok {
		fo.sessionID = sid
	}
	fo.global = opts["global"] == "true"
	fo.caseSensitive = opts["case_sensitive"] == "true"
	return fo
}

// acceptItem sanitises a single history entry and deduplicates it.
// It returns the cleaned command and true if the item should be included.
func acceptItem(item *pb.HistoryItem, query string, caseSensitive bool, seen map[string]struct{}) (string, bool) {
	cmd := strings.TrimSpace(ValidateUTF8(StripANSI(item.Command)))
	if cmd == "" {
		return "", false
	}
	if caseSensitive && query != "" && !strings.Contains(cmd, query) {
		return "", false
	}
	key := cmdutil.NormalizeCommand(cmd)
	if key == "" {
		return "", false
	}
	if _, exists := seen[key]; exists {
		return "", false
	}
	seen[key] = struct{}{}
	return cmd, true
}

// Fetch calls the daemon's FetchHistory RPC and returns sanitized results.
func (p *HistoryProvider) Fetch(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	client, err := p.getClient()
	if err != nil {
		return Response{}, err
	}

	opts := parseFetchOpts(req.Options)

	baseReq := &pb.HistoryFetchRequest{
		Query:     req.Query,
		Limit:     int32(req.Limit),
		Offset:    int32(req.Offset),
		SessionId: opts.sessionID,
		Global:    opts.global,
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
			if cmd, ok := acceptItem(item, req.Query, opts.caseSensitive, seen); ok {
				items = append(items, cmd)
			}
		}

		if shouldStopFetching(targetUnique, len(items), atEnd, fetched, attempt) {
			break
		}

		nextOffset += fetched
		limit = targetUnique - len(items)
	}

	if targetUnique > 0 && len(items) > targetUnique {
		items = items[:targetUnique]
	}

	return Response{
		RequestID: req.RequestID,
		Items:     items,
		AtEnd:     atEnd,
	}, nil
}

// shouldStopFetching returns true when the top-up loop should exit.
func shouldStopFetching(target, collected int, atEnd bool, fetched, attempt int) bool {
	if target <= 0 {
		return true // no limit requested: single-request semantics
	}
	if collected >= target {
		return true
	}
	return atEnd || fetched == 0 || attempt >= maxTopUpFetches
}
