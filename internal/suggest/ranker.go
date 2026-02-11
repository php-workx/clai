package suggest

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/runger/clai/internal/storage"
)

// Ranker provides suggestion ranking capabilities.
type Ranker interface {
	Rank(ctx context.Context, req *RankRequest) ([]Suggestion, error)
}

// RankRequest contains the parameters for ranking suggestions.
type RankRequest struct {
	SessionID   string
	CWD         string
	Prefix      string
	LastCommand string
	MaxResults  int
}

// Suggestion represents a ranked command suggestion.
type Suggestion struct {
	Text        string  // The suggested command
	Description string  // Optional description
	Source      string  // "session", "cwd", "global", "ai"
	Score       float64 // Ranking score (0.0 to 1.0)
	Risk        string  // "safe", "destructive", or empty

	// Enrichment fields for explainability/UX.
	CmdNorm        string
	LastSeenUnixMs int64
	SuccessCount   int
	FailureCount   int
	Reasons        []Reason // weighted scoring reasons (type + contribution)
}

// Reason describes a single "why" component for a suggestion score.
// Contribution is the weighted contribution (0.0 to 1.0) to the final score.
type Reason struct {
	Type         string
	Description  string
	Contribution float64
}

// DefaultRanker implements the Ranker interface using the scoring formula
// from the tech design.
type DefaultRanker struct {
	source *CommandSource
}

// NewRanker creates a new DefaultRanker.
func NewRanker(store storage.Store) *DefaultRanker {
	return &DefaultRanker{
		source: NewCommandSource(store),
	}
}

// Scoring weights from the tech design
const (
	weightSource   = 0.4
	weightRecency  = 0.3
	weightSuccess  = 0.2
	weightAffinity = 0.1
)

// candidate represents an aggregated command candidate for ranking.
type candidate struct {
	cmd          storage.Command
	source       Source
	successCount int
	failureCount int
	latestTime   int64
}

// Rank retrieves and ranks command suggestions based on the scoring formula:
// score = (source_weight * 0.4) + (recency_score * 0.3) + (success_score * 0.2) + (affinity_score * 0.1)
func (r *DefaultRanker) Rank(ctx context.Context, req *RankRequest) ([]Suggestion, error) {
	if req == nil {
		return nil, nil
	}

	maxResults := normalizeMaxResults(req.MaxResults)
	limitPerScope := maxResults * 3

	results, err := r.source.QueryAllScopes(ctx, req.SessionID, req.CWD, req.Prefix, limitPerScope)
	if err != nil {
		return nil, err
	}

	candidates := aggregateCandidates(results)
	// Never suggest the exact last command again; users can re-run via shell history.
	// This prevents the common "next suggestion == last command" annoyance.
	if req.LastCommand != "" {
		lastNorm := NormalizeCommand(req.LastCommand)
		if lastNorm != "" {
			delete(candidates, DeduplicateKey(lastNorm))
		}
	}
	suggestions := scoreCandidates(candidates, time.Now(), GetToolPrefix(req.LastCommand))
	return limitResults(suggestions, maxResults), nil
}

// normalizeMaxResults returns a valid max results value, defaulting to 10.
func normalizeMaxResults(maxResults int) int {
	if maxResults <= 0 {
		return 10
	}
	return maxResults
}

// aggregateCandidates builds a map of deduplicated candidates from query results.
func aggregateCandidates(results []*QueryResult) map[string]*candidate {
	candidates := make(map[string]*candidate)

	for _, result := range results {
		for i := range result.Commands {
			cmd := &result.Commands[i]
			key := DeduplicateKey(cmd.CommandNorm)
			existing, ok := candidates[key]
			if !ok {
				candidates[key] = newCandidate(*cmd, result.Source)
			} else {
				updateCandidate(existing, *cmd, result.Source)
			}
		}
	}

	return candidates
}

// newCandidate creates a new candidate from a command.
func newCandidate(cmd storage.Command, source Source) *candidate {
	successCount, failureCount := countSuccessFailure(cmd)
	return &candidate{
		cmd:          cmd,
		source:       source,
		successCount: successCount,
		failureCount: failureCount,
		latestTime:   cmd.TsStartUnixMs,
	}
}

// countSuccessFailure returns (successCount, failureCount) for a command.
func countSuccessFailure(cmd storage.Command) (int, int) {
	if cmd.IsSuccess == nil || *cmd.IsSuccess {
		return 1, 0
	}
	return 0, 1
}

// updateCandidate updates an existing candidate with a new command occurrence.
func updateCandidate(existing *candidate, cmd storage.Command, source Source) {
	if cmd.IsSuccess == nil || *cmd.IsSuccess {
		existing.successCount++
	} else {
		existing.failureCount++
	}

	if cmd.TsStartUnixMs > existing.latestTime {
		existing.latestTime = cmd.TsStartUnixMs
		existing.cmd = cmd
	}

	if SourceWeight(source) > SourceWeight(existing.source) {
		existing.source = source
	}
}

// scoreCandidates scores all candidates and returns sorted suggestions.
func scoreCandidates(candidates map[string]*candidate, now time.Time, lastToolPrefix string) []Suggestion {
	suggestions := make([]Suggestion, 0, len(candidates))

	for _, c := range candidates {
		sourceScore := SourceWeight(c.source)
		recencyScore := calculateRecencyScore(c.latestTime, now)
		successScore := calculateSuccessScore(c.successCount, c.failureCount)
		affinityScore := calculateAffinityScore(c.cmd.Command, lastToolPrefix)

		score := (sourceScore * weightSource) +
			(recencyScore * weightRecency) +
			(successScore * weightSuccess) +
			(affinityScore * weightAffinity)

		reasons := make([]Reason, 0, 4)
		reasons = append(reasons,
			Reason{
				Type:         "source",
				Description:  "", // UI already shows source; keep tags compact
				Contribution: sourceScore * weightSource,
			},
			Reason{
				Type:         "recency",
				Description:  "", // human-friendly "last ..." hint is added at RPC boundary
				Contribution: recencyScore * weightRecency,
			},
			Reason{
				Type:         "success",
				Description:  "", // human-friendly hint is added at RPC boundary
				Contribution: successScore * weightSuccess,
			},
		)
		if affinityScore != 0 {
			desc := ""
			if lastToolPrefix != "" {
				desc = "same tool: " + lastToolPrefix
			}
			reasons = append(reasons, Reason{
				Type:         "affinity",
				Description:  desc,
				Contribution: affinityScore * weightAffinity,
			})
		}
		suggestions = append(suggestions, Suggestion{
			Text:           c.cmd.Command,
			Source:         string(c.source),
			Score:          score,
			Risk:           "", // Risk assessment to be added by caller if needed
			CmdNorm:        c.cmd.CommandNorm,
			LastSeenUnixMs: c.latestTime,
			SuccessCount:   c.successCount,
			FailureCount:   c.failureCount,
			Reasons:        reasons,
		})
	}

	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	return suggestions
}

// limitResults truncates the suggestions slice to maxResults.
func limitResults(suggestions []Suggestion, maxResults int) []Suggestion {
	if len(suggestions) > maxResults {
		return suggestions[:maxResults]
	}
	return suggestions
}

// calculateScore computes the final score for a command using the formula:
// score = (source_weight * 0.4) + (recency_score * 0.3) + (success_score * 0.2) + (affinity_score * 0.1)
func calculateScore(source Source, commandTime int64, now time.Time, successCount, failureCount int, command, lastToolPrefix string) float64 {
	sourceScore := SourceWeight(source)
	recencyScore := calculateRecencyScore(commandTime, now)
	successScore := calculateSuccessScore(successCount, failureCount)
	affinityScore := calculateAffinityScore(command, lastToolPrefix)

	return (sourceScore * weightSource) +
		(recencyScore * weightRecency) +
		(successScore * weightSuccess) +
		(affinityScore * weightAffinity)
}

// calculateRecencyScore computes the recency score using:
// 1.0 / (1 + log(hours_since_use + 1))
func calculateRecencyScore(commandTimeMs int64, now time.Time) float64 {
	commandTime := time.UnixMilli(commandTimeMs)
	hoursSinceUse := now.Sub(commandTime).Hours()
	if hoursSinceUse < 0 {
		hoursSinceUse = 0
	}

	// Formula: 1.0 / (1 + log(hours_since_use + 1))
	return 1.0 / (1.0 + math.Log(hoursSinceUse+1))
}

// calculateSuccessScore computes the success score using:
// success_count / (success_count + failure_count)
func calculateSuccessScore(successCount, failureCount int) float64 {
	total := successCount + failureCount
	if total == 0 {
		return 0.5 // Default to 50% if no data
	}
	return float64(successCount) / float64(total)
}

// calculateAffinityScore returns 1.0 if the command has the same tool prefix
// as the last command, else 0.0.
func calculateAffinityScore(command, lastToolPrefix string) float64 {
	if lastToolPrefix == "" {
		return 0.0
	}
	cmdPrefix := GetToolPrefix(command)
	if cmdPrefix == lastToolPrefix {
		return 1.0
	}
	return 0.0
}
