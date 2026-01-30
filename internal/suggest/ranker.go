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

// Rank retrieves and ranks command suggestions based on the scoring formula:
// score = (source_weight * 0.4) + (recency_score * 0.3) + (success_score * 0.2) + (affinity_score * 0.1)
func (r *DefaultRanker) Rank(ctx context.Context, req *RankRequest) ([]Suggestion, error) {
	if req == nil {
		return nil, nil
	}

	// Default max results
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	// Query limit per scope - get more than needed to allow for deduplication
	limitPerScope := maxResults * 3

	// Query all scopes
	results, err := r.source.QueryAllScopes(ctx, req.SessionID, req.CWD, req.Prefix, limitPerScope)
	if err != nil {
		return nil, err
	}

	// Build scored candidates
	now := time.Now()
	lastToolPrefix := GetToolPrefix(req.LastCommand)

	// Map to aggregate commands by normalized form
	type candidate struct {
		cmd          storage.Command
		source       Source
		successCount int
		failureCount int
		latestTime   int64
	}
	candidates := make(map[string]*candidate)

	for _, result := range results {
		for _, cmd := range result.Commands {
			key := DeduplicateKey(cmd.CommandNorm)

			existing, ok := candidates[key]
			if !ok {
				// New candidate
				successCount := 0
				failureCount := 0
				if cmd.IsSuccess {
					successCount = 1
				} else {
					failureCount = 1
				}
				candidates[key] = &candidate{
					cmd:          cmd,
					source:       result.Source,
					successCount: successCount,
					failureCount: failureCount,
					latestTime:   cmd.TsStartUnixMs,
				}
			} else {
				// Update existing candidate
				if cmd.IsSuccess {
					existing.successCount++
				} else {
					existing.failureCount++
				}
				// Keep the most recent timestamp
				if cmd.TsStartUnixMs > existing.latestTime {
					existing.latestTime = cmd.TsStartUnixMs
					existing.cmd = cmd
				}
				// Keep the highest priority source
				if SourceWeight(result.Source) > SourceWeight(existing.source) {
					existing.source = result.Source
				}
			}
		}
	}

	// Score and rank candidates
	suggestions := make([]Suggestion, 0, len(candidates))
	for _, c := range candidates {
		score := calculateScore(c.source, c.latestTime, now, c.successCount, c.failureCount, c.cmd.Command, lastToolPrefix)

		suggestions = append(suggestions, Suggestion{
			Text:   c.cmd.Command,
			Source: string(c.source),
			Score:  score,
			Risk:   "", // Risk assessment to be added by caller if needed
		})
	}

	// Sort by score (descending)
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	// Limit results
	if len(suggestions) > maxResults {
		suggestions = suggestions[:maxResults]
	}

	return suggestions, nil
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
