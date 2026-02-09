package picker

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/ipc"
)

// suggestFetchTimeout is the maximum time allowed for a single Fetch call,
// covering both connection establishment and the RPC itself.
//
// Suggest can be slower than history fetch, but we still want the picker to
// stay responsive.
const suggestFetchTimeout = 400 * time.Millisecond

// SuggestProvider implements Provider using the daemon's Suggest gRPC RPC.
type SuggestProvider struct {
	socketPath string
	view       string
	// ensureDaemon is injected for testing; defaults to ipc.EnsureDaemon.
	ensureDaemon func() error

	// cacheKey is "session_id\ncwd". Suggest picker results do not depend on the
	// filter query; we fetch a broad set once and let the picker do local
	// substring filtering.
	cacheKey string
	cache    []Item
}

// Compile-time check that SuggestProvider implements Provider.
var _ Provider = (*SuggestProvider)(nil)

// NewSuggestProvider creates a provider that connects to the daemon socket.
// view controls how list items are rendered: "compact" or "detailed".
func NewSuggestProvider(socketPath, view string) *SuggestProvider {
	if view == "" {
		view = "detailed"
	}
	return &SuggestProvider{
		socketPath:   socketPath,
		view:         view,
		ensureDaemon: ipc.EnsureDaemon,
	}
}

func suggestContextKey(req Request) (sid, cwd, key string) {
	if req.Options != nil {
		// Accept both "session_id" and "session" for the session filter.
		if v, ok := req.Options["session_id"]; ok {
			sid = v
		} else if v, ok := req.Options["session"]; ok {
			sid = v
		}
		if v, ok := req.Options["cwd"]; ok {
			cwd = v
		}
	}
	return sid, cwd, sid + "\n" + cwd
}

// Fetch calls the daemon's Suggest RPC and returns sanitized results.
func (p *SuggestProvider) Fetch(ctx context.Context, req Request) (Response, error) {
	_, _, key := suggestContextKey(req)
	if key == p.cacheKey && p.cache != nil {
		return Response{
			RequestID: req.RequestID,
			Items:     p.cache,
			AtEnd:     true,
		}, nil
	}

	resp, err := p.fetchWithTimeout(ctx, req, suggestFetchTimeout)
	if err == nil {
		p.cacheKey = key
		p.cache = resp.Items
		return resp, nil
	}

	if !p.shouldRecover(err) || p.ensureDaemon == nil {
		return Response{}, err
	}

	if err := p.ensureDaemon(); err != nil {
		return Response{}, fmt.Errorf("suggest provider: daemon recovery: %w", err)
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

func (p *SuggestProvider) fetchWithTimeout(ctx context.Context, req Request, timeout time.Duration) (Response, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return p.fetchWithContext(timeoutCtx, req)
}

func (p *SuggestProvider) fetchWithContext(ctx context.Context, req Request) (Response, error) {
	conn, err := grpc.NewClient(
		"unix://"+p.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return Response{}, fmt.Errorf("suggest provider: dial: %w", err)
	}
	defer conn.Close()

	client := pb.NewClaiServiceClient(conn)

	sid, cwd, _ := suggestContextKey(req)

	// Fetch a broad set so the picker can do local substring filtering.
	max := 200
	grpcReq := &pb.SuggestRequest{
		SessionId:            sid,
		Cwd:                  cwd,
		Buffer:               "",
		CursorPos:            0,
		IncludeAi:            false,
		MaxResults:           int32(max),
		IncludeLowConfidence: true, // picker is explicit; show more options
	}

	grpcResp, err := client.Suggest(ctx, grpcReq)
	if err != nil {
		return Response{}, fmt.Errorf("suggest provider: rpc: %w", err)
	}

	items := make([]Item, 0, len(grpcResp.Suggestions))
	for _, s := range grpcResp.Suggestions {
		cmd := ValidateUTF8(StripANSI(s.Text))
		cmd = oneLine(cmd)
		if cmd == "" {
			continue
		}
		display := formatSuggestionDisplay(p.view, cmd, s)
		items = append(items, Item{Value: cmd, Display: display})
	}

	return Response{
		RequestID: req.RequestID,
		Items:     items,
		AtEnd:     true, // no pagination supported
	}, nil
}

func (p *SuggestProvider) shouldRecover(err error) bool {
	// Only auto-recover when using the canonical IPC socket path to avoid
	// interfering with explicit custom socket targets.
	if p.socketPath != ipc.SocketPath() {
		return false
	}
	return isUnavailable(err)
}

func oneLine(s string) string {
	// Keep picker list rendering stable; Bubble Tea list assumes single-line items.
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func formatSuggestionDisplay(view, cmd string, s *pb.Suggestion) string {
	src := strings.TrimSpace(s.Source)
	if src == "" {
		src = "unknown"
	}

	risk := strings.TrimSpace(strings.ToLower(s.Risk))
	riskTag := ""
	if risk == "destructive" {
		riskTag = "[!]"
	}

	switch strings.ToLower(view) {
	case "compact":
		// Mode 2: command + source (+ risk)
		if riskTag != "" {
			return fmt.Sprintf("%s  · %s  %s", cmd, src, riskTag)
		}
		return fmt.Sprintf("%s  · %s", cmd, src)

	default:
		// Mode 3: command + source + risk + score + reasons/description
		score := s.Score
		if math.IsNaN(score) || math.IsInf(score, 0) {
			score = 0
		}
		scoreStr := fmt.Sprintf("score %.2f", score)

		confStr := ""
		if s.Confidence > 0 {
			confStr = fmt.Sprintf(" conf %.2f", s.Confidence)
		}

		meta := fmt.Sprintf("%s  · %s  %s%s", cmd, src, scoreStr, confStr)
		if risk != "" {
			meta += "  risk " + risk
		}

		// Prefer description; fall back to top reasons.
		desc := strings.TrimSpace(oneLine(s.Description))
		if desc == "" && len(s.Reasons) > 0 {
			var parts []string
			for i, r := range s.Reasons {
				if i >= 2 {
					break
				}
				d := strings.TrimSpace(oneLine(r.Description))
				if d == "" {
					d = strings.TrimSpace(r.Type)
				}
				if d != "" {
					parts = append(parts, d)
				}
			}
			if len(parts) > 0 {
				desc = strings.Join(parts, "; ")
			}
		}

		if desc != "" {
			meta += "  — " + desc
		}

		if riskTag != "" {
			meta += "  " + riskTag
		}

		return meta
	}
}
