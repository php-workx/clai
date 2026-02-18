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
	// ensureDaemon is injected for testing; defaults to ipc.EnsureDaemon.
	ensureDaemon func() error

	// state tracks per-(session,query) paging progress so the Session tab can
	// seamlessly continue into global history once the session segment ends.
	state      map[string]*sessionQueryState
	socketPath string
	stateMu    sync.Mutex
}

type sessionQueryState struct {
	// seen is the set of session commands observed so far for this (session,query),
	// used to dedupe global fallback items.
	seen map[string]struct{}
	// total is the total number of session items for this query once we've reached
	// the end (atEnd=true). Until then, it is -1.
	total int
}

// Compile-time check that HistoryProvider implements Provider.
var _ Provider = (*HistoryProvider)(nil)

// NewHistoryProvider creates a provider that connects to the daemon socket.
func NewHistoryProvider(socketPath string) *HistoryProvider {
	return &HistoryProvider{
		socketPath:   socketPath,
		ensureDaemon: ipc.EnsureDaemon,
		state:        make(map[string]*sessionQueryState),
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
		return p.fetchScopedHistory(ctx, client, req, sessionID, global)
	}

	state := p.getSessionQueryState(sessionID, req.Query)
	if p.shouldServeGlobalFromKnownSession(state, req.Offset) {
		return p.fetchKnownGlobalPage(ctx, client, req, state)
	}

	sessionPage, sessionAtEnd, total, dedupe, err := p.fetchSessionPageAndUpdateState(ctx, client, req, sessionID, state)
	if err != nil {
		return Response{}, err
	}
	return p.composeSessionAndGlobalResponse(ctx, client, req, sessionPage, sessionAtEnd, total, dedupe)
}

func (p *HistoryProvider) fetchScopedHistory(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	sessionID string,
	global bool,
) (Response, error) {
	items, atEnd, err := p.fetchHistoryItems(ctx, client, sessionID, global, req.Query, req.Limit, req.Offset)
	if err != nil {
		return Response{}, err
	}
	return Response{RequestID: req.RequestID, Items: items, AtEnd: atEnd}, nil
}

func (p *HistoryProvider) shouldServeGlobalFromKnownSession(state *sessionQueryState, offset int) bool {
	return state.total >= 0 && offset >= state.total
}

func (p *HistoryProvider) fetchKnownGlobalPage(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	state *sessionQueryState,
) (Response, error) {
	globalOffset := req.Offset - state.total
	dedupe := p.sessionSeenSnapshot(state)
	return p.fetchCompositeGlobalPage(ctx, client, req, dedupe, globalOffset, state.total == 0)
}

func (p *HistoryProvider) fetchSessionPageAndUpdateState(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	sessionID string,
	state *sessionQueryState,
) (sessionPage []Item, sessionAtEnd bool, total int, dedupe map[string]struct{}, err error) {
	sessionPage, sessionAtEnd, err = p.fetchHistoryItems(ctx, client, sessionID, false, req.Query, req.Limit, req.Offset)
	if err != nil {
		return nil, false, 0, nil, err
	}
	sessionPage = dedupeItems(sessionPage, nil)

	p.stateMu.Lock()
	for _, it := range sessionPage {
		state.seen[it.Value] = struct{}{}
	}
	if sessionAtEnd {
		state.total = req.Offset + len(sessionPage)
	}
	total = state.total
	p.stateMu.Unlock()
	return sessionPage, sessionAtEnd, total, p.sessionSeenSnapshot(state), nil
}

func (p *HistoryProvider) composeSessionAndGlobalResponse(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	sessionPage []Item,
	sessionAtEnd bool,
	total int,
	dedupe map[string]struct{},
) (Response, error) {
	if sessionAtEnd && total == 0 && req.Offset == 0 {
		return p.fetchScopedHistory(ctx, client, req, "", true)
	}
	if !sessionAtEnd {
		return Response{RequestID: req.RequestID, Items: sessionPage, AtEnd: false}, nil
	}
	remaining := req.Limit - len(sessionPage)
	if remaining <= 0 {
		return p.sessionBoundaryResponse(ctx, client, req, sessionPage, dedupe)
	}
	globalOffset := computeGlobalOffset(req.Offset, total)
	out, atEnd, err := p.fetchCompositeGlobalRemainder(ctx, client, req, dedupe, globalOffset, remaining)
	if err != nil {
		return Response{}, err
	}
	return Response{RequestID: req.RequestID, Items: append(sessionPage, out...), AtEnd: atEnd}, nil
}

func (p *HistoryProvider) sessionBoundaryResponse(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	sessionPage []Item,
	dedupe map[string]struct{},
) (Response, error) {
	globalFiltered, globalAtEnd, err := p.fetchGlobalFiltered(ctx, client, req.Query, 1, dedupe)
	if err != nil {
		return Response{}, err
	}
	atEnd := globalAtEnd && len(globalFiltered) == 0
	return Response{RequestID: req.RequestID, Items: sessionPage, AtEnd: atEnd}, nil
}

func computeGlobalOffset(requestOffset, sessionTotal int) int {
	if sessionTotal >= 0 && requestOffset > sessionTotal {
		return requestOffset - sessionTotal
	}
	return 0
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

func (p *HistoryProvider) getSessionQueryState(sessionID, query string) *sessionQueryState {
	key := sessionID + "\n" + query
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if st, ok := p.state[key]; ok {
		return st
	}
	st := &sessionQueryState{
		total: -1,
		seen:  make(map[string]struct{}, 128),
	}
	p.state[key] = st
	return st
}

func (p *HistoryProvider) sessionSeenSnapshot(st *sessionQueryState) map[string]struct{} {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	out := make(map[string]struct{}, len(st.seen))
	for k := range st.seen {
		out[k] = struct{}{}
	}
	return out
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
		Limit:     int32(limit),  //nolint:gosec // G115: limit is bounded by picker page size
		Offset:    int32(offset), //nolint:gosec // G115: offset is bounded by result count
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

func (p *HistoryProvider) fetchCompositeGlobalPage(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	dedupe map[string]struct{},
	globalOffset int,
	direct bool,
) (Response, error) {
	if direct {
		items, atEnd, err := p.fetchHistoryItems(ctx, client, "", true, req.Query, req.Limit, globalOffset)
		if err != nil {
			return Response{}, err
		}
		return Response{RequestID: req.RequestID, Items: items, AtEnd: atEnd}, nil
	}

	want := globalOffset + req.Limit + 1
	globalFiltered, globalAtEnd, err := p.fetchGlobalFiltered(ctx, client, req.Query, want, dedupe)
	if err != nil {
		return Response{}, err
	}

	out := make([]Item, 0, req.Limit)
	if globalOffset < len(globalFiltered) {
		end := globalOffset + req.Limit
		if end > len(globalFiltered) {
			end = len(globalFiltered)
		}
		page := globalFiltered[globalOffset:end]
		for i := range page {
			page[i].Display = "[G] " + page[i].Value
		}
		out = append(out, page...)
	}

	atEnd := globalAtEnd && len(globalFiltered) <= globalOffset+req.Limit
	return Response{RequestID: req.RequestID, Items: out, AtEnd: atEnd}, nil
}

func (p *HistoryProvider) fetchCompositeGlobalRemainder(
	ctx context.Context,
	client pb.ClaiServiceClient,
	req Request,
	dedupe map[string]struct{},
	globalOffset int,
	remaining int,
) ([]Item, bool, error) {
	if remaining <= 0 {
		return nil, false, nil
	}

	want := globalOffset + remaining + 1
	globalFiltered, globalAtEnd, err := p.fetchGlobalFiltered(ctx, client, req.Query, want, dedupe)
	if err != nil {
		return nil, false, err
	}

	var out []Item
	if globalOffset < len(globalFiltered) {
		end := globalOffset + remaining
		if end > len(globalFiltered) {
			end = len(globalFiltered)
		}
		page := globalFiltered[globalOffset:end]
		for i := range page {
			page[i].Display = "[G] " + page[i].Value
		}
		out = append(out, page...)
	}

	atEnd := globalAtEnd && len(globalFiltered) <= globalOffset+remaining
	return out, atEnd, nil
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
		items, atEnd, nextOffset, err := p.fetchGlobalChunk(ctx, client, query, want, dedupe, offset, maxChunk)
		if err != nil {
			return nil, false, err
		}
		offset = nextOffset
		if len(items) == 0 {
			return out, true, nil
		}
		if p.appendFilteredGlobalItems(&out, items, dedupe, seen, want) {
			return out, false, nil
		}

		if atEnd {
			return out, true, nil
		}
		// Continue with next chunk.
	}
}

func (p *HistoryProvider) fetchGlobalChunk(
	ctx context.Context,
	client pb.ClaiServiceClient,
	query string,
	want int,
	dedupe map[string]struct{},
	offset int,
	maxChunk int,
) (items []Item, atEnd bool, nextOffset int, err error) {
	chunkLimit := want + len(dedupe) + 50
	if chunkLimit > maxChunk {
		chunkLimit = maxChunk
	}
	items, atEnd, err = p.fetchHistoryItems(ctx, client, "", true, query, chunkLimit, offset)
	if err != nil {
		return nil, false, 0, err
	}
	return items, atEnd, offset + len(items), nil
}

func (p *HistoryProvider) appendFilteredGlobalItems(
	out *[]Item,
	items []Item,
	dedupe map[string]struct{},
	seen map[string]struct{},
	want int,
) bool {
	for _, it := range items {
		if _, ok := dedupe[it.Value]; ok {
			continue
		}
		if _, ok := seen[it.Value]; ok {
			continue
		}
		seen[it.Value] = struct{}{}
		*out = append(*out, it)
		if len(*out) >= want {
			return true
		}
	}
	return false
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
