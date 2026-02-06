// Package suggest provides the multi-factor suggestion scoring algorithm.
// It implements the scoring specified in tech_suggestions_v3.md Section 11.
package suggest

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/score"
)

// Default scoring weights per spec Section 11.4.
const (
	DefaultWeightRepoTransition   = 80
	DefaultWeightGlobalTransition = 60
	DefaultWeightRepoFrequency    = 30
	DefaultWeightProjectTask      = 20
	DefaultWeightDangerous        = -50

	// Directory-scoped weights. Dir scope has higher affinity than repo
	// because it captures location-specific patterns within a repository.
	DefaultWeightDirTransition = 90
	DefaultWeightDirFrequency  = 40
)

// Default configuration.
const (
	// DefaultTopK is the number of suggestions to return.
	DefaultTopK = 3

	// MaxTopK is the maximum allowed suggestions.
	MaxTopK = 10
)

// Reason tags for suggestions per spec Section 11.
const (
	ReasonRepoTransition   = "repo_trans"
	ReasonGlobalTransition = "global_trans"
	ReasonRepoFrequency    = "repo_freq"
	ReasonGlobalFrequency  = "global_freq"
	ReasonProjectTask      = "project_task"
	ReasonDangerous        = "dangerous"
	ReasonDirTransition    = "dir_trans"
	ReasonDirFrequency     = "dir_freq"
)

// Weights configures the scoring weights.
type Weights struct {
	RepoTransition   float64
	GlobalTransition float64
	RepoFrequency    float64
	GlobalFrequency  float64
	ProjectTask      float64
	DangerousPenalty float64
	DirTransition    float64
	DirFrequency     float64
}

// DefaultWeights returns the default scoring weights per spec Section 11.4.
func DefaultWeights() Weights {
	return Weights{
		RepoTransition:   DefaultWeightRepoTransition,
		GlobalTransition: DefaultWeightGlobalTransition,
		RepoFrequency:    DefaultWeightRepoFrequency,
		GlobalFrequency:  DefaultWeightRepoFrequency, // Same as repo
		ProjectTask:      DefaultWeightProjectTask,
		DangerousPenalty: DefaultWeightDangerous,
		DirTransition:    DefaultWeightDirTransition,
		DirFrequency:     DefaultWeightDirFrequency,
	}
}

// ScorerConfig configures the scorer.
type ScorerConfig struct {
	Weights Weights
	TopK    int
	Logger  *slog.Logger
}

// DefaultScorerConfig returns the default scorer configuration.
func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		Weights: DefaultWeights(),
		TopK:    DefaultTopK,
		Logger:  slog.Default(),
	}
}

// Suggestion represents a scored command suggestion.
type Suggestion struct {
	Command    string    // The suggested command (normalized)
	Score      float64   // Combined score
	Confidence float64   // Confidence score (0-1)
	Reasons    []string  // Tags explaining why this was suggested
	scores     scoreInfo // Internal scoring breakdown
}

// scoreInfo tracks the breakdown of scoring sources.
type scoreInfo struct {
	repoTransition   float64
	globalTransition float64
	repoFrequency    float64
	globalFrequency  float64
	projectTask      float64
	dangerous        float64
	dirTransition    float64
	dirFrequency     float64
}

// Scorer implements the multi-factor suggestion scoring algorithm.
type Scorer struct {
	db                *sql.DB
	freqStore         *score.FrequencyStore
	transitionStore   *score.TransitionStore
	discoveryService  *discovery.Service
	dangerousCommands map[string]bool
	cfg               ScorerConfig
}

// ScorerDependencies contains the required dependencies for the scorer.
type ScorerDependencies struct {
	DB               *sql.DB
	FreqStore        *score.FrequencyStore
	TransitionStore  *score.TransitionStore
	DiscoveryService *discovery.Service
}

// NewScorer creates a new suggestion scorer.
func NewScorer(deps ScorerDependencies, cfg ScorerConfig) (*Scorer, error) {
	if cfg.TopK <= 0 {
		cfg.TopK = DefaultTopK
	}
	if cfg.TopK > MaxTopK {
		cfg.TopK = MaxTopK
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Scorer{
		db:                deps.DB,
		freqStore:         deps.FreqStore,
		transitionStore:   deps.TransitionStore,
		discoveryService:  deps.DiscoveryService,
		dangerousCommands: buildDangerousCommands(),
		cfg:               cfg,
	}, nil
}

// buildDangerousCommands returns a set of dangerous command patterns.
func buildDangerousCommands() map[string]bool {
	return map[string]bool{
		"rm -rf /":        true,
		"rm -rf /*":       true,
		"rm -rf .":        true,
		"rm -rf *":        true,
		"dd if=/dev/zero": true,
		"mkfs":            true,
		":(){:|:&};:":     true, // Fork bomb
		"chmod -R 777":    true,
		"chmod 777":       true,
	}
}

// SuggestContext contains context for generating suggestions.
type SuggestContext struct {
	SessionID   string
	RepoKey     string
	LastCmd     string // Last command (normalized)
	Cwd         string
	NowMs       int64
	DirScopeKey string // Directory scope key (from dirscope.ComputeScopeKey)
}

// Suggest generates scored suggestions based on the current context.
// Per spec Section 11: combines transitions, frequency, project tasks, and directory scope.
func (s *Scorer) Suggest(ctx context.Context, suggestCtx SuggestContext) ([]Suggestion, error) {
	if suggestCtx.NowMs == 0 {
		suggestCtx.NowMs = time.Now().UnixMilli()
	}

	// Gather candidates from all sources
	candidates := make(map[string]*Suggestion)

	// 1. Repo transitions
	if suggestCtx.RepoKey != "" && suggestCtx.LastCmd != "" && s.transitionStore != nil {
		repoTrans, err := s.transitionStore.GetTopNextCommands(ctx, suggestCtx.RepoKey, suggestCtx.LastCmd, 10)
		if err != nil {
			s.cfg.Logger.Debug("repo transitions query failed", "error", err)
		} else {
			for _, t := range repoTrans {
				s.addCandidate(candidates, t.NextNorm, float64(t.Count), ReasonRepoTransition, s.cfg.Weights.RepoTransition)
			}
		}
	}

	// 2. Global transitions
	if suggestCtx.LastCmd != "" && s.transitionStore != nil {
		globalTrans, err := s.transitionStore.GetTopNextCommands(ctx, score.ScopeGlobal, suggestCtx.LastCmd, 10)
		if err != nil {
			s.cfg.Logger.Debug("global transitions query failed", "error", err)
		} else {
			for _, t := range globalTrans {
				s.addCandidate(candidates, t.NextNorm, float64(t.Count), ReasonGlobalTransition, s.cfg.Weights.GlobalTransition)
			}
		}
	}

	// 2b. Directory-scoped transitions
	if suggestCtx.DirScopeKey != "" && suggestCtx.LastCmd != "" && s.transitionStore != nil {
		dirTrans, err := s.transitionStore.GetTopNextCommands(ctx, suggestCtx.DirScopeKey, suggestCtx.LastCmd, 10)
		if err != nil {
			s.cfg.Logger.Debug("dir transitions query failed", "error", err)
		} else {
			for _, t := range dirTrans {
				s.addCandidate(candidates, t.NextNorm, float64(t.Count), ReasonDirTransition, s.cfg.Weights.DirTransition)
			}
		}
	}

	// 3. Repo frequency
	if suggestCtx.RepoKey != "" && s.freqStore != nil {
		repoFreq, err := s.freqStore.GetTopCommandsAt(ctx, suggestCtx.RepoKey, 10, suggestCtx.NowMs)
		if err != nil {
			s.cfg.Logger.Debug("repo frequency query failed", "error", err)
		} else {
			for _, f := range repoFreq {
				s.addCandidate(candidates, f.CmdNorm, f.Score, ReasonRepoFrequency, s.cfg.Weights.RepoFrequency)
			}
		}
	}

	// 4. Global frequency
	if s.freqStore != nil {
		globalFreq, err := s.freqStore.GetTopCommandsAt(ctx, score.ScopeGlobal, 10, suggestCtx.NowMs)
		if err != nil {
			s.cfg.Logger.Debug("global frequency query failed", "error", err)
		} else {
			for _, f := range globalFreq {
				s.addCandidate(candidates, f.CmdNorm, f.Score, ReasonGlobalFrequency, s.cfg.Weights.GlobalFrequency)
			}
		}
	}

	// 4b. Directory-scoped frequency
	if suggestCtx.DirScopeKey != "" && s.freqStore != nil {
		dirFreq, err := s.freqStore.GetTopCommandsAt(ctx, suggestCtx.DirScopeKey, 10, suggestCtx.NowMs)
		if err != nil {
			s.cfg.Logger.Debug("dir frequency query failed", "error", err)
		} else {
			for _, f := range dirFreq {
				s.addCandidate(candidates, f.CmdNorm, f.Score, ReasonDirFrequency, s.cfg.Weights.DirFrequency)
			}
		}
	}

	// 5. Project tasks
	if suggestCtx.RepoKey != "" && s.discoveryService != nil {
		tasks, err := s.discoveryService.GetTasks(ctx, suggestCtx.RepoKey)
		if err != nil {
			s.cfg.Logger.Debug("project tasks query failed", "error", err)
		} else {
			for _, t := range tasks {
				s.addCandidate(candidates, t.Command, 1.0, ReasonProjectTask, s.cfg.Weights.ProjectTask)
			}
		}
	}

	// Apply dangerous command penalties
	for cmd, sug := range candidates {
		if s.isDangerous(cmd) {
			sug.scores.dangerous = s.cfg.Weights.DangerousPenalty
			sug.Score += s.cfg.Weights.DangerousPenalty
			sug.Reasons = append(sug.Reasons, ReasonDangerous)
		}
	}

	// Convert to sorted slice
	suggestions := make([]Suggestion, 0, len(candidates))
	for _, sug := range candidates {
		sug.Confidence = s.calculateConfidence(sug)
		suggestions = append(suggestions, *sug)
	}

	// Sort by score descending
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	// Limit to top-K
	if len(suggestions) > s.cfg.TopK {
		suggestions = suggestions[:s.cfg.TopK]
	}

	return suggestions, nil
}

// addCandidate adds or updates a candidate suggestion.
// Per spec Section 11.3: use log(count+1) for scoring.
// Per spec Section 11.5: deduplicate by cmd_norm, merge reasons.
func (s *Scorer) addCandidate(candidates map[string]*Suggestion, cmd string, rawScore float64, reason string, weight float64) {
	// Apply log(count+1) per spec Section 11.3
	adjustedScore := math.Log(rawScore+1) * weight

	if existing, ok := candidates[cmd]; ok {
		// Merge: add to score and append reason
		existing.Score += adjustedScore
		existing.Reasons = append(existing.Reasons, reason)

		// Track score breakdown
		switch reason {
		case ReasonRepoTransition:
			existing.scores.repoTransition += adjustedScore
		case ReasonGlobalTransition:
			existing.scores.globalTransition += adjustedScore
		case ReasonRepoFrequency:
			existing.scores.repoFrequency += adjustedScore
		case ReasonGlobalFrequency:
			existing.scores.globalFrequency += adjustedScore
		case ReasonProjectTask:
			existing.scores.projectTask += adjustedScore
		case ReasonDirTransition:
			existing.scores.dirTransition += adjustedScore
		case ReasonDirFrequency:
			existing.scores.dirFrequency += adjustedScore
		}
	} else {
		// New candidate
		sug := &Suggestion{
			Command: cmd,
			Score:   adjustedScore,
			Reasons: []string{reason},
		}

		switch reason {
		case ReasonRepoTransition:
			sug.scores.repoTransition = adjustedScore
		case ReasonGlobalTransition:
			sug.scores.globalTransition = adjustedScore
		case ReasonRepoFrequency:
			sug.scores.repoFrequency = adjustedScore
		case ReasonGlobalFrequency:
			sug.scores.globalFrequency = adjustedScore
		case ReasonProjectTask:
			sug.scores.projectTask = adjustedScore
		case ReasonDirTransition:
			sug.scores.dirTransition = adjustedScore
		case ReasonDirFrequency:
			sug.scores.dirFrequency = adjustedScore
		}

		candidates[cmd] = sug
	}
}

// isDangerous checks if a command is potentially dangerous.
func (s *Scorer) isDangerous(cmd string) bool {
	return s.dangerousCommands[cmd]
}

// calculateConfidence calculates a confidence score (0-1) for a suggestion.
func (s *Scorer) calculateConfidence(sug *Suggestion) float64 {
	// Confidence based on number of supporting sources and score magnitude
	sourceCount := 0
	if sug.scores.repoTransition > 0 {
		sourceCount++
	}
	if sug.scores.globalTransition > 0 {
		sourceCount++
	}
	if sug.scores.repoFrequency > 0 {
		sourceCount++
	}
	if sug.scores.globalFrequency > 0 {
		sourceCount++
	}
	if sug.scores.projectTask > 0 {
		sourceCount++
	}
	if sug.scores.dirTransition > 0 {
		sourceCount++
	}
	if sug.scores.dirFrequency > 0 {
		sourceCount++
	}

	// Base confidence from source diversity (up to 0.5)
	// 7 total sources now, so divide by 14 for max 0.5
	sourceConfidence := float64(sourceCount) / 14.0

	// Score-based confidence (up to 0.5)
	// Normalize score to 0-0.5 range using sigmoid
	scoreConfidence := 0.5 / (1.0 + math.Exp(-sug.Score/50.0))

	return sourceConfidence + scoreConfidence
}

// TopK returns the configured top-K value.
func (s *Scorer) TopK() int {
	return s.cfg.TopK
}

// Weights returns the configured weights.
func (s *Scorer) Weights() Weights {
	return s.cfg.Weights
}

// DebugScore represents a score entry for debug output.
type DebugScore struct {
	Scope   string
	CmdNorm string
	Score   float64
	LastTs  int64
}

// DebugScores returns the top scores from the command_score table for debugging.
func (s *Scorer) DebugScores(ctx context.Context, limit int) ([]DebugScore, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT scope, cmd_norm, score, last_ts
		FROM command_score
		ORDER BY score DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scores []DebugScore
	for rows.Next() {
		var ds DebugScore
		if err := rows.Scan(&ds.Scope, &ds.CmdNorm, &ds.Score, &ds.LastTs); err != nil {
			return nil, err
		}
		scores = append(scores, ds)
	}

	return scores, rows.Err()
}
