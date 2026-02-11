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
	merged := interleaveUniqueSuggestions(v2.Suggestions, v1.Suggestions, maxResults)

	return &pb.SuggestResponse{
		Suggestions: merged,
		FromCache:   v1.FromCache,
	}
}

func interleaveUniqueSuggestions(primary, secondary []*pb.Suggestion, maxResults int) []*pb.Suggestion {
	seen := make(map[string]struct{}, maxResults)
	merged := make([]*pb.Suggestion, 0, maxResults)
	pIdx, sIdx := 0, 0
	for len(merged) < maxResults && (pIdx < len(primary) || sIdx < len(secondary)) {
		merged, pIdx = appendUniqueSuggestion(merged, primary, pIdx, seen)
		if len(merged) >= maxResults {
			break
		}
		merged, sIdx = appendUniqueSuggestion(merged, secondary, sIdx, seen)
	}
	return merged
}

func appendUniqueSuggestion(
	merged []*pb.Suggestion,
	source []*pb.Suggestion,
	idx int,
	seen map[string]struct{},
) ([]*pb.Suggestion, int) {
	if idx >= len(source) {
		return merged, idx
	}
	sug := source[idx]
	idx++
	if sug == nil {
		return merged, idx
	}
	if _, exists := seen[sug.Text]; exists {
		return merged, idx
	}
	seen[sug.Text] = struct{}{}
	return append(merged, sug), idx
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
	for i := range suggestions {
		pbSuggestions[i] = v2SuggestionToProto(suggestions[i], prevCmd, nowMs, explainCfg)
	}
	return &pb.SuggestResponse{
		Suggestions: pbSuggestions,
		FromCache:   false,
	}
}

func v2SuggestionToProto(
	sug suggest2.Suggestion,
	prevCmd string,
	nowMs int64,
	explainCfg explain.Config,
) *pb.Suggestion {
	why := explain.Explain(sug, explainCfg, prevCmd)
	return &pb.Suggestion{
		Text:        sug.Command,
		Description: v2SuggestionDescription(sug, why, prevCmd),
		Source:      "global",
		Score:       sug.Score,
		Risk:        v2SuggestionRisk(sug.Command),
		CmdNorm:     sug.Command,
		Confidence:  sug.Confidence,
		Reasons:     v2SuggestionReasons(sug, why, nowMs),
	}
}

func v2SuggestionRisk(command string) string {
	if sanitize.IsDestructive(command) {
		return riskDestructive
	}
	return ""
}

func v2SuggestionReasons(
	sug suggest2.Suggestion,
	why []explain.Reason,
	nowMs int64,
) []*pb.SuggestionReason {
	reasons := make([]*pb.SuggestionReason, 0, len(why)+3)
	for _, r := range why {
		reasons = append(reasons, &pb.SuggestionReason{
			Type:         r.Tag,
			Description:  r.Description,
			Contribution: float32(r.Contribution),
		})
	}
	if sugLast := sug.LastSeenMs(); sugLast > 0 {
		reasons = append(reasons, &pb.SuggestionReason{
			Type:        "recency",
			Description: fmt.Sprintf("last %s ago", formatAgo(nowMs-sugLast)),
		})
	}
	if fs := sug.MaxFreqScore(); fs > 0 {
		reasons = append(reasons, &pb.SuggestionReason{
			Type:        "frequency",
			Description: fmt.Sprintf("freq %.2f", fs),
		})
	}
	if tc := sug.MaxTransitionCount(); tc > 0 {
		reasons = append(reasons, &pb.SuggestionReason{
			Type:        "transition_count",
			Description: fmt.Sprintf("trans %d", tc),
		})
	}
	return reasons
}

func v2SuggestionDescription(sug suggest2.Suggestion, why []explain.Reason, prevCmd string) string {
	if len(why) > 0 {
		return why[0].Description
	}
	displayCmd := prevCmd
	if len(displayCmd) > 40 {
		displayCmd = displayCmd[:37] + "..."
	}
	switch {
	case prevCmd != "" && sug.MaxTransitionCount() > 0:
		return fmt.Sprintf("Often run after '%s'.", displayCmd)
	case sug.MaxFreqScore() > 0:
		return "Frequently used command."
	case sug.LastSeenMs() > 0:
		return "Used recently."
	default:
		return ""
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
