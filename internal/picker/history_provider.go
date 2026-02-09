package picker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/ipc"
)

// fetchTimeout is the maximum time allowed for a single Fetch call,
// covering both connection establishment and the RPC itself.
const fetchTimeout = 200 * time.Millisecond

// recoveryTimeout bounds daemon recovery attempts when the default socket
// temporarily disappears.
const recoveryTimeout = 600 * time.Millisecond

// recoveryRetryDelay is the wait between retry attempts during recovery.
const recoveryRetryDelay = 30 * time.Millisecond

// HistoryProvider implements Provider using the daemon's FetchHistory gRPC RPC.
type HistoryProvider struct {
	socketPath string
	// ensureDaemon is injected for testing; defaults to ipc.EnsureDaemon.
	ensureDaemon func() error
}

// Compile-time check that HistoryProvider implements Provider.
var _ Provider = (*HistoryProvider)(nil)

// NewHistoryProvider creates a provider that connects to the daemon socket.
func NewHistoryProvider(socketPath string) *HistoryProvider {
	return &HistoryProvider{
		socketPath:   socketPath,
		ensureDaemon: ipc.EnsureDaemon,
	}
}

// Fetch calls the daemon's FetchHistory RPC and returns sanitized results.
func (p *HistoryProvider) Fetch(ctx context.Context, req Request) (Response, error) {
	resp, err := p.fetchWithTimeout(ctx, req, fetchTimeout)
	if err == nil {
		return resp, nil
	}

	if !p.shouldRecover(err) || p.ensureDaemon == nil {
		return Response{}, err
	}

	if err := p.ensureDaemon(); err != nil {
		return Response{}, fmt.Errorf("history provider: daemon recovery: %w", err)
	}

	// Daemon spawn is asynchronous; retry for a bounded window until it accepts RPCs.
	retryCtx, retryCancel := context.WithTimeout(ctx, recoveryTimeout)
	defer retryCancel()

	var lastErr error
	for {
		resp, fetchErr := p.fetchWithContext(retryCtx, req)
		if fetchErr == nil {
			return resp, nil
		}
		lastErr = fetchErr
		if !isUnavailable(fetchErr) {
			return Response{}, fetchErr
		}

		timer := time.NewTimer(recoveryRetryDelay)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return Response{}, lastErr
		case <-timer.C:
		}
	}
}

func (p *HistoryProvider) fetchWithTimeout(ctx context.Context, req Request, timeout time.Duration) (Response, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return p.fetchWithContext(timeoutCtx, req)
}

func (p *HistoryProvider) fetchWithContext(ctx context.Context, req Request) (Response, error) {
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

	items := make([]Item, 0, len(grpcResp.Items))
	for _, item := range grpcResp.Items {
		cmd := ValidateUTF8(StripANSI(item.Command))
		items = append(items, Item{Value: cmd, Display: cmd})
	}

	return Response{
		RequestID: req.RequestID,
		Items:     items,
		AtEnd:     grpcResp.AtEnd,
	}, nil
}

func (p *HistoryProvider) shouldRecover(err error) bool {
	// Only auto-recover when using the canonical IPC socket path to avoid
	// interfering with explicit custom socket targets.
	if p.socketPath != ipc.SocketPath() {
		return false
	}
	return isUnavailable(err)
}

func isUnavailable(err error) bool {
	if status.Code(err) == codes.Unavailable {
		return true
	}
	// Defensive fallback in case wrapping obscures gRPC status extraction.
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "transport is closing")
}
