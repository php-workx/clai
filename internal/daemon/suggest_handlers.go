package daemon

import (
	"context"
	"fmt"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/sanitize"
	"github.com/runger/clai/internal/suggestions/dirscope"
	"github.com/runger/clai/internal/suggestions/explain"
	"github.com/runger/clai/internal/suggestions/normalize"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
)

// suggestV2 generates suggestions using only the V2 scorer.
// Returns nil response if the V2 scorer is not available (caller should fall back).
func (s *Server) suggestV2(ctx context.Context, req *pb.SuggestRequest, maxResults int) *pb.SuggestResponse {
	if s.v2Scorer == nil {
		return nil
	}

	suggestCtx := s.buildV2SuggestContext(req)
	if suggestCtx.NowMs == 0 {
		suggestCtx.NowMs = time.Now().UnixMilli()
	}

	suggestions, err := s.v2Scorer.Suggest(ctx, suggestCtx)
	if err != nil {
		s.logger.Warn("V2 scorer failed", "error", err)
		return nil
	}

	if maxResults > 0 && len(suggestions) > maxResults {
		suggestions = suggestions[:maxResults]
	}

	return s.v2SuggestionsToProto(suggestions, suggestCtx.LastCmd, suggestCtx.NowMs)
}

// suggestBlend generates suggestions by merging V1 and V2 results.
// V2 results are interleaved with V1, deduplicated by command text,
// with V2 suggestions taking priority on conflicts.
func (s *Server) suggestBlend(ctx context.Context, req *pb.SuggestRequest, maxResults int, v1Resp *pb.SuggestResponse) *pb.SuggestResponse {
	v2Resp := s.suggestV2(ctx, req, maxResults)
	if v2Resp == nil {
		// V2 unavailable -- return V1 results only
		return v1Resp
	}

	return mergeResponses(v1Resp, v2Resp, maxResults)
}

// mergeResponses merges V1 and V2 responses, deduplicating by command text.
// V2 suggestions take priority on conflicts. Results are interleaved
// (v2, v1, v2, v1, ...) and capped at maxResults.
func mergeResponses(v1, v2 *pb.SuggestResponse, maxResults int) *pb.SuggestResponse {
	if v2 == nil || len(v2.Suggestions) == 0 {
		return v1
	}
	if v1 == nil || len(v1.Suggestions) == 0 {
		return v2
	}

	seen := make(map[string]bool)
	merged := make([]*pb.Suggestion, 0, maxResults)

	// Interleave: alternate V2 and V1 suggestions
	v2Items := v2.Suggestions
	v1Items := v1.Suggestions
	v2Idx, v1Idx := 0, 0

	for len(merged) < maxResults && (v2Idx < len(v2Items) || v1Idx < len(v1Items)) {
		// Take from V2 first
		if v2Idx < len(v2Items) {
			sug := v2Items[v2Idx]
			v2Idx++
			if !seen[sug.Text] {
				seen[sug.Text] = true
				merged = append(merged, sug)
			}
		}

		if len(merged) >= maxResults {
			break
		}

		// Then from V1
		if v1Idx < len(v1Items) {
			sug := v1Items[v1Idx]
			v1Idx++
			if !seen[sug.Text] {
				seen[sug.Text] = true
				merged = append(merged, sug)
			}
		}
	}

	return &pb.SuggestResponse{
		Suggestions: merged,
		FromCache:   v1.FromCache,
	}
}

// buildV2SuggestContext creates a V2 SuggestContext from a Suggest RPC request.
func (s *Server) buildV2SuggestContext(req *pb.SuggestRequest) suggest2.SuggestContext {
	suggestCtx := suggest2.SuggestContext{
		SessionID: req.SessionId,
		Prefix:    req.Buffer,
		Cwd:       req.Cwd,
	}

	// Try to get the last command from session for transition scoring
	if info, ok := s.sessionManager.Get(req.SessionId); ok {
		// V2 scorer expects normalized command strings.
		suggestCtx.LastCmd = normalize.NormalizeSimple(info.LastCmdRaw)
		suggestCtx.RepoKey = info.LastGitRepo
		// Directory scope key for cwd-scoped transitions/frequency (best-effort).
		suggestCtx.DirScopeKey = dirscope.ComputeScopeKey(req.Cwd, info.LastGitRoot, dirscope.DefaultMaxDepth)
	}

	return suggestCtx
}

// v2SuggestionsToProto converts V2 scorer suggestions to protobuf format.
func (s *Server) v2SuggestionsToProto(suggestions []suggest2.Suggestion, prevCmd string, nowMs int64) *pb.SuggestResponse {
	// Use default explain config; CLI can request more, but the picker UI
	// should always have a basic "why" available.
	explainCfg := explain.DefaultConfig()

	pbSuggestions := make([]*pb.Suggestion, len(suggestions))
	for i, sug := range suggestions {
		risk := ""
		if sanitize.IsDestructive(sug.Command) {
			risk = riskDestructive
		}

		why := explain.Explain(sug, explainCfg, prevCmd)
		pbReasons := make([]*pb.SuggestionReason, 0, len(why)+4)
		for _, r := range why {
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:         r.Tag,
				Description:  r.Description,
				Contribution: float32(r.Contribution),
			})
		}

		// Add recency/frequency hints for UI display. These do not necessarily map
		// 1:1 to score contributions, but help users understand provenance.
		if sugLast := sug.LastSeenMs(); sugLast > 0 {
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:        "recency",
				Description: fmt.Sprintf("last %s", formatAgo(nowMs-sugLast)),
			})
		}
		if fs := sug.MaxFreqScore(); fs > 0 {
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:        "frequency",
				Description: fmt.Sprintf("freq %.2f", fs),
			})
		}
		if tc := sug.MaxTransitionCount(); tc > 0 {
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:        "transition_count",
				Description: fmt.Sprintf("trans %d", tc),
			})
		}

		desc := ""
		if len(why) > 0 {
			desc = why[0].Description
		}

		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Command,
			Description: desc,
			Source:      "global",
			Score:       sug.Score,
			Risk:        risk,
			CmdNorm:     sug.Command,
			Confidence:  sug.Confidence,
			Reasons:     pbReasons,
		}
	}
	return &pb.SuggestResponse{
		Suggestions: pbSuggestions,
		FromCache:   false,
	}
}

func formatAgo(deltaMs int64) string {
	if deltaMs <= 0 {
		return "0s"
	}
	d := time.Duration(deltaMs) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
