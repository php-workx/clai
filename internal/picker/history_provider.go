package picker

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

	// probeCache memoizes session size probes for the lifetime of a picker run,
	// avoiding extra RPCs on every keystroke/page.
	probeMu    sync.Mutex
	probeCache map[string]sessionProbe
}

type sessionProbe struct {
	// count is the number of session items returned by the probe (capped at probeLimit).
	count int
	// atEnd indicates whether the daemon reports there are no more session items beyond count.
	atEnd bool
	// cmds holds the probed session commands (sanitized) for deduping global fallback items.
	cmds []string
}

const (
	// probeLimit matches the UX rule: for sessions with fewer than 100 commands,
	// we treat the Session tab as "session + global fallback".
	probeLimit = 100
)

// Compile-time check that HistoryProvider implements Provider.
var _ Provider = (*HistoryProvider)(nil)

// NewHistoryProvider creates a provider that connects to the daemon socket.
func NewHistoryProvider(socketPath string) *HistoryProvider {
	return &HistoryProvider{
		socketPath:   socketPath,
		ensureDaemon: ipc.EnsureDaemon,
		probeCache:   make(map[string]sessionProbe),
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
	return p.fetchWithClient(ctx, client, req)
}

func (p *HistoryProvider) fetchWithClient(ctx context.Context, client pb.ClaiServiceClient, req Request) (Response, error) {
	sessionID, global := extractHistoryScope(req.Options)
	if global || sessionID == "" {
		items, atEnd, err := p.fetchHistoryItems(ctx, client, sessionID, global, req.Query, req.Limit, req.Offset)
		if err != nil {
			return Response{}, err
		}
		return Response{
			RequestID: req.RequestID,
			Items:     items,
			AtEnd:     atEnd,
		}, nil
	}

	probe, err := p.getSessionProbe(ctx, client, sessionID)
	if err != nil {
		return Response{}, err
	}

	// Only fall through to global when the overall session has fewer than 100 commands.
	// This keeps the Session tab focused for longer-running sessions.
	sessionUnderThreshold := probe.atEnd && probe.count < probeLimit
	if !sessionUnderThreshold {
		items, atEnd, fetchErr := p.fetchHistoryItems(ctx, client, sessionID, false, req.Query, req.Limit, req.Offset)
		if fetchErr != nil {
			return Response{}, fetchErr
		}
		return Response{
			RequestID: req.RequestID,
			Items:     items,
			AtEnd:     atEnd,
		}, nil
	}

	// If the session is completely empty, show global history directly.
	if probe.count == 0 {
		items, atEnd, fetchErr := p.fetchHistoryItems(ctx, client, "", true, req.Query, req.Limit, req.Offset)
		if fetchErr != nil {
			return Response{}, fetchErr
		}
		return Response{
			RequestID: req.RequestID,
			Items:     items,
			AtEnd:     atEnd,
		}, nil
	}

	// Composite mode: Session results first, then global fallback (deduped).
	sessionAll, _, fetchErr := p.fetchHistoryItems(ctx, client, sessionID, false, req.Query, probeLimit, 0)
	if fetchErr != nil {
		return Response{}, fetchErr
	}
	sessionAll = dedupeItems(sessionAll, nil)

	sessionCount := len(sessionAll)
	var out []Item

	// 1) Slice session segment.
	if req.Offset < sessionCount {
		end := req.Offset + req.Limit
		if end > sessionCount {
			end = sessionCount
		}
		out = append(out, sessionAll[req.Offset:end]...)
	}

	// 2) Slice global segment after session segment.
	remaining := req.Limit - len(out)
	if remaining < 0 {
		remaining = 0
	}

	// Compute offset into the global filtered list.
	globalOffset := 0
	if req.Offset >= sessionCount {
		globalOffset = req.Offset - sessionCount
	}

	if remaining == 0 {
		// If we're still within session results, we definitely aren't at the end.
		// If we ended exactly at the session boundary, only mark atEnd=true if
		// there are no global items after deduping the session commands.
		if req.Offset+req.Limit < sessionCount {
			return Response{RequestID: req.RequestID, Items: out, AtEnd: false}, nil
		}

		sessionCmdSet := make(map[string]struct{}, len(probe.cmds))
		for _, c := range probe.cmds {
			sessionCmdSet[c] = struct{}{}
		}
		globalFiltered, globalAtEnd, moreErr := p.fetchGlobalFiltered(ctx, client, req.Query, 1, sessionCmdSet)
		if moreErr != nil {
			return Response{}, moreErr
		}
		atEnd := globalAtEnd && len(globalFiltered) == 0
		return Response{RequestID: req.RequestID, Items: out, AtEnd: atEnd}, nil
	}

	sessionCmdSet := make(map[string]struct{}, len(probe.cmds))
	for _, c := range probe.cmds {
		sessionCmdSet[c] = struct{}{}
	}

	// We fetch enough to determine if there's one more item beyond this page
	// so AtEnd is meaningful for paging UX.
	want := globalOffset + remaining + 1
	globalFiltered, globalAtEnd, gerr := p.fetchGlobalFiltered(ctx, client, req.Query, want, sessionCmdSet)
	if gerr != nil {
		return Response{}, gerr
	}

	// Apply the slice for this page.
	if globalOffset < len(globalFiltered) {
		gend := globalOffset + remaining
		if gend > len(globalFiltered) {
			gend = len(globalFiltered)
		}
		page := globalFiltered[globalOffset:gend]
		for i := range page {
			// Display-only prefix; Value remains the raw command.
			page[i].Display = "[G] " + page[i].Value
		}
		out = append(out, page...)
	}

	// Determine whether there are more items after this page.
	atEnd := false
	if req.Offset+req.Limit < sessionCount {
		atEnd = false
	} else if globalAtEnd && len(globalFiltered) <= globalOffset+remaining {
		atEnd = true
	}

	return Response{RequestID: req.RequestID, Items: out, AtEnd: atEnd}, nil
}

func extractHistoryScope(opts map[string]string) (sessionID string, global bool) {
	if opts == nil {
		return "", false
	}
	// Accept both "session_id" and "session" for the session filter.
	if sid, ok := opts["session_id"]; ok {
		sessionID = sid
	} else if sid, ok := opts["session"]; ok {
		sessionID = sid
	}
	if g, ok := opts["global"]; ok && g == "true" {
		global = true
	}
	return sessionID, global
}

func (p *HistoryProvider) getSessionProbe(ctx context.Context, client pb.ClaiServiceClient, sessionID string) (sessionProbe, error) {
	p.probeMu.Lock()
	if v, ok := p.probeCache[sessionID]; ok {
		p.probeMu.Unlock()
		return v, nil
	}
	p.probeMu.Unlock()

	// Probe without query to estimate overall session size (bounded to probeLimit).
	grpcResp, err := client.FetchHistory(ctx, &pb.HistoryFetchRequest{
		SessionId: sessionID,
		Query:     "",
		Limit:     probeLimit,
		Offset:    0,
		Global:    false,
	})
	if err != nil {
		return sessionProbe{}, fmt.Errorf("history provider: rpc: %w", err)
	}

	cmds := make([]string, 0, len(grpcResp.Items))
	for _, item := range grpcResp.Items {
		cmd := ValidateUTF8(StripANSI(item.Command))
		if cmd == "" {
			continue
		}
		cmds = append(cmds, cmd)
	}

	out := sessionProbe{
		count: len(cmds),
		atEnd: grpcResp.AtEnd,
		cmds:  cmds,
	}

	p.probeMu.Lock()
	p.probeCache[sessionID] = out
	p.probeMu.Unlock()

	return out, nil
}

func (p *HistoryProvider) fetchHistoryItems(
	ctx context.Context,
	client pb.ClaiServiceClient,
	sessionID string,
	global bool,
	query string,
	limit int,
	offset int,
) ([]Item, bool, error) {
	grpcReq := &pb.HistoryFetchRequest{
		SessionId: sessionID,
		Global:    global,
		Query:     query,
		Limit:     int32(limit),
		Offset:    int32(offset),
	}

	grpcResp, err := client.FetchHistory(ctx, grpcReq)
	if err != nil {
		return nil, false, fmt.Errorf("history provider: rpc: %w", err)
	}

	items := make([]Item, 0, len(grpcResp.Items))
	for _, item := range grpcResp.Items {
		cmd := ValidateUTF8(StripANSI(item.Command))
		if cmd == "" {
			continue
		}
		items = append(items, Item{Value: cmd, Display: cmd})
	}
	return items, grpcResp.AtEnd, nil
}

func (p *HistoryProvider) fetchGlobalFiltered(
	ctx context.Context,
	client pb.ClaiServiceClient,
	query string,
	want int,
	dedupe map[string]struct{},
) ([]Item, bool, error) {
	if want <= 0 {
		return nil, true, nil
	}

	// Fetch in chunks from offset=0 and filter client-side. This keeps the
	// paging semantics stable after removing session duplicates.
	const maxChunk = 2000

	seen := make(map[string]struct{}, 128)
	out := make([]Item, 0, want)

	offset := 0
	for {
		// Over-fetch a bit to compensate for filtered-out duplicates.
		chunkLimit := want + len(dedupe) + 50
		if chunkLimit > maxChunk {
			chunkLimit = maxChunk
		}
		items, atEnd, err := p.fetchHistoryItems(ctx, client, "", true, query, chunkLimit, offset)
		if err != nil {
			return nil, false, err
		}
		if len(items) == 0 {
			return out, true, nil
		}
		offset += len(items)

		for _, it := range items {
			if _, ok := dedupe[it.Value]; ok {
				continue
			}
			if _, ok := seen[it.Value]; ok {
				continue
			}
			seen[it.Value] = struct{}{}
			out = append(out, it)
			if len(out) >= want {
				return out, false, nil
			}
		}

		if atEnd {
			return out, true, nil
		}
		// Continue with next chunk.
	}
}

func dedupeItems(items []Item, exclude map[string]struct{}) []Item {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Value == "" {
			continue
		}
		if exclude != nil {
			if _, ok := exclude[it.Value]; ok {
				continue
			}
		}
		if _, ok := seen[it.Value]; ok {
			continue
		}
		seen[it.Value] = struct{}{}
		out = append(out, it)
	}
	return out
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
