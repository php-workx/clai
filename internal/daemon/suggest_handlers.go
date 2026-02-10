package daemon

import (
	"context"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/sanitize"
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

	suggestions, err := s.v2Scorer.Suggest(ctx, suggestCtx)
	if err != nil {
		s.logger.Warn("V2 scorer failed", "error", err)
		return nil
	}

	if maxResults > 0 && len(suggestions) > maxResults {
		suggestions = suggestions[:maxResults]
	}

	return s.v2SuggestionsToProto(suggestions)
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
	}

	return suggestCtx
}

// v2SuggestionsToProto converts V2 scorer suggestions to protobuf format.
func (s *Server) v2SuggestionsToProto(suggestions []suggest2.Suggestion) *pb.SuggestResponse {
	pbSuggestions := make([]*pb.Suggestion, len(suggestions))
	for i, sug := range suggestions {
		risk := ""
		if sanitize.IsDestructive(sug.Command) {
			risk = riskDestructive
		}
		source := "v2"
		if len(sug.Reasons) > 0 {
			source = "v2:" + sug.Reasons[0]
		}
		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Command,
			Description: formatV2Reasons(sug.Reasons),
			Source:      source,
			Score:       sug.Score,
			Risk:        risk,
		}
	}
	return &pb.SuggestResponse{
		Suggestions: pbSuggestions,
		FromCache:   false,
	}
}

// formatV2Reasons formats V2 reason tags into a human-readable description.
func formatV2Reasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	desc := reasons[0]
	for _, r := range reasons[1:] {
		desc += ", " + r
	}
	return desc
}
