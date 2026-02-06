// Package suggest provides the multi-factor suggestion scoring algorithm.
// It implements the 10-feature weighted scoring specified in
// tech_suggestions_ext_v1.md Section 7.1 with amplifiers, deterministic
// tie-breaking, near-duplicate suppression, and prefix filtering.
package suggest

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/recovery"
	"github.com/runger/clai/internal/suggestions/score"
	"github.com/runger/clai/internal/suggestions/workflow"
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

// Default amplifier factors per spec Section 7.1.
const (
	DefaultWorkflowBoostFactor = 1.5
	DefaultDismissalPenalty    = 0.3
	DefaultPermanentPenalty    = 0.0
	DefaultRecoveryBoostFactor = 2.0
	DefaultPipelineConfWeight  = 50.0
	DefaultRecencyDecayTauMs   = 7 * 24 * 60 * 60 * 1000 // 7 days in ms
	DefaultPlaybookBoostFactor = 1.3
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
	ReasonWorkflowBoost    = "workflow_boost"
	ReasonPipelineConf     = "pipeline_conf"
	ReasonDismissalPenalty = "dismissal_penalty"
	ReasonRecoveryBoost    = "recovery_boost"
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

// AmplifierConfig configures the post-score amplifier factors.
type AmplifierConfig struct {
	// WorkflowBoostFactor multiplies score when a workflow is active and command matches.
	// Default: 1.5
	WorkflowBoostFactor float64

	// DismissalPenaltyFactor multiplies score for dismissed suggestions.
	// Default: 0.3 (reduces to 30% of original score)
	DismissalPenaltyFactor float64

	// PermanentPenaltyFactor multiplies score for "never" feedback.
	// Default: 0.0 (completely suppresses)
	PermanentPenaltyFactor float64

	// RecoveryBoostFactor multiplies score after a command failure.
	// Default: 2.0
	RecoveryBoostFactor float64

	// PipelineConfidenceWeight is the weight added for pipeline confidence.
	// Default: 50.0
	PipelineConfidenceWeight float64

	// RecencyDecayTauMs is the time constant for recency decay on frequency scores.
	// Default: 7 days (same as frequency store tau)
	RecencyDecayTauMs int64

	// PlaybookBoostFactor multiplies score for playbook-sourced suggestions.
	// Default: 1.3
	PlaybookBoostFactor float64
}

// DefaultAmplifierConfig returns the default amplifier configuration.
func DefaultAmplifierConfig() AmplifierConfig {
	return AmplifierConfig{
		WorkflowBoostFactor:      DefaultWorkflowBoostFactor,
		DismissalPenaltyFactor:   DefaultDismissalPenalty,
		PermanentPenaltyFactor:   DefaultPermanentPenalty,
		RecoveryBoostFactor:      DefaultRecoveryBoostFactor,
		PipelineConfidenceWeight: DefaultPipelineConfWeight,
		RecencyDecayTauMs:        DefaultRecencyDecayTauMs,
		PlaybookBoostFactor:      DefaultPlaybookBoostFactor,
	}
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
	Weights    Weights
	Amplifiers AmplifierConfig
	TopK       int
	Logger     *slog.Logger
}

// DefaultScorerConfig returns the default scorer configuration.
func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		Weights:    DefaultWeights(),
		Amplifiers: DefaultAmplifierConfig(),
		TopK:       DefaultTopK,
		Logger:     slog.Default(),
	}
}

// Suggestion represents a scored command suggestion.
type Suggestion struct {
	Command    string    // The suggested command (normalized)
	TemplateID string    // Template ID for near-duplicate suppression
	Score      float64   // Combined score
	Confidence float64   // Confidence score (0-1)
	Reasons    []string  // Tags explaining why this was suggested
	scores     scoreInfo // Internal scoring breakdown
	frequency  float64   // Raw frequency for tie-breaking
	lastSeenMs int64     // Last seen timestamp for tie-breaking
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
	workflowBoost    float64
	pipelineConf     float64
	dismissalPenalty float64
	recoveryBoost    float64
}

// ScoreBreakdown provides the per-feature score contributions for a suggestion.
// This is used by the explain package to generate human-readable reasons.
type ScoreBreakdown struct {
	RepoTransition   float64
	GlobalTransition float64
	RepoFrequency    float64
	GlobalFrequency  float64
	ProjectTask      float64
	Dangerous        float64
	DirTransition    float64
	DirFrequency     float64
	WorkflowBoost    float64
	PipelineConf     float64
	DismissalPenalty float64
	RecoveryBoost    float64
}

// ScoreBreakdown returns the per-feature score contributions for this suggestion.
func (s *Suggestion) ScoreBreakdown() ScoreBreakdown {
	return ScoreBreakdown{
		RepoTransition:   s.scores.repoTransition,
		GlobalTransition: s.scores.globalTransition,
		RepoFrequency:    s.scores.repoFrequency,
		GlobalFrequency:  s.scores.globalFrequency,
		ProjectTask:      s.scores.projectTask,
		Dangerous:        s.scores.dangerous,
		DirTransition:    s.scores.dirTransition,
		DirFrequency:     s.scores.dirFrequency,
		WorkflowBoost:    s.scores.workflowBoost,
		PipelineConf:     s.scores.pipelineConf,
		DismissalPenalty: s.scores.dismissalPenalty,
		RecoveryBoost:    s.scores.recoveryBoost,
	}
}

// Scorer implements the multi-factor suggestion scoring algorithm.
type Scorer struct {
	db                *sql.DB
	freqStore         *score.FrequencyStore
	transitionStore   *score.TransitionStore
	pipelineStore     *score.PipelineStore
	discoveryService  *discovery.Service
	workflowTracker   *workflow.Tracker
	dismissalStore    *dismissal.Store
	recoveryEngine    *recovery.Engine
	dangerousCommands map[string]bool
	cfg               ScorerConfig
}

// ScorerDependencies contains the required dependencies for the scorer.
type ScorerDependencies struct {
	DB               *sql.DB
	FreqStore        *score.FrequencyStore
	TransitionStore  *score.TransitionStore
	PipelineStore    *score.PipelineStore
	DiscoveryService *discovery.Service
	WorkflowTracker  *workflow.Tracker
	DismissalStore   *dismissal.Store
	RecoveryEngine   *recovery.Engine
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
	// Ensure amplifier defaults
	if cfg.Amplifiers.RecencyDecayTauMs == 0 {
		cfg.Amplifiers.RecencyDecayTauMs = DefaultRecencyDecayTauMs
	}

	return &Scorer{
		db:                deps.DB,
		freqStore:         deps.FreqStore,
		transitionStore:   deps.TransitionStore,
		pipelineStore:     deps.PipelineStore,
		discoveryService:  deps.DiscoveryService,
		workflowTracker:   deps.WorkflowTracker,
		dismissalStore:    deps.DismissalStore,
		recoveryEngine:    deps.RecoveryEngine,
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
	SessionID      string
	RepoKey        string
	LastCmd        string // Last command (normalized)
	LastTemplateID string // Template ID of the last command
	LastExitCode   int    // Exit code of the last command (0 = success)
	LastFailed     bool   // Whether the last command failed
	Prefix         string // Typed prefix for filtering
	Cwd            string
	NowMs          int64
	DirScopeKey    string // Directory scope key (from dirscope.ComputeScopeKey)
	Scope          string // Scope for dismissal/recovery lookups
}

// Suggest generates scored suggestions based on the current context.
// Implements the 10-feature weighted scoring from spec Section 7.1:
//  1. Repo transitions
//  2. Global transitions
//  3. Dir-scoped transitions
//  4. Repo frequency
//  5. Global frequency
//  6. Dir-scoped frequency
//  7. Project tasks
//  8. Workflow boost
//  9. Pipeline confidence
//  10. Recovery boost (after failure)
//
// Plus amplifiers: dismissal penalty, recency decay, prefix filtering,
// near-duplicate suppression, and deterministic tie-breaking.
func (s *Scorer) Suggest(ctx context.Context, suggestCtx SuggestContext) ([]Suggestion, error) {
	if suggestCtx.NowMs == 0 {
		suggestCtx.NowMs = time.Now().UnixMilli()
	}
	if suggestCtx.Scope == "" {
		suggestCtx.Scope = score.ScopeGlobal
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

	// 3. Directory-scoped transitions
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

	// 4. Repo frequency
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

	// 5. Global frequency
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

	// 6. Directory-scoped frequency
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

	// 7. Project tasks
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

	// 8. Workflow boost: amplify candidates matching active workflow next-step
	s.applyWorkflowBoost(candidates, suggestCtx)

	// 9. Pipeline confidence: add weight for pipeline continuation candidates
	s.applyPipelineConfidence(ctx, candidates, suggestCtx)

	// 10. Recovery boost: amplify recovery candidates after failures
	s.applyRecoveryBoost(ctx, candidates, suggestCtx)

	// Apply dangerous command penalties
	for cmd, sug := range candidates {
		if s.isDangerous(cmd) {
			sug.scores.dangerous = s.cfg.Weights.DangerousPenalty
			sug.Score += s.cfg.Weights.DangerousPenalty
			sug.Reasons = append(sug.Reasons, ReasonDangerous)
		}
	}

	// Apply dismissal penalties
	s.applyDismissalPenalties(ctx, candidates, suggestCtx)

	// Apply prefix filtering
	if suggestCtx.Prefix != "" {
		candidates = s.filterByPrefix(candidates, suggestCtx.Prefix)
	}

	// Convert to sorted slice
	suggestions := make([]Suggestion, 0, len(candidates))
	for _, sug := range candidates {
		sug.Confidence = s.calculateConfidence(sug)
		suggestions = append(suggestions, *sug)
	}

	// Apply near-duplicate suppression (by template ID)
	suggestions = suppressNearDuplicates(suggestions)

	// Deterministic sort: score desc > frequency desc > recency desc > lexicographic cmd asc
	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		if suggestions[i].frequency != suggestions[j].frequency {
			return suggestions[i].frequency > suggestions[j].frequency
		}
		if suggestions[i].lastSeenMs != suggestions[j].lastSeenMs {
			return suggestions[i].lastSeenMs > suggestions[j].lastSeenMs
		}
		return suggestions[i].Command < suggestions[j].Command
	})

	// Limit to top-K
	if len(suggestions) > s.cfg.TopK {
		suggestions = suggestions[:s.cfg.TopK]
	}

	return suggestions, nil
}

// applyWorkflowBoost amplifies candidates that match active workflow next-steps.
// Per spec Section 7.1: workflow_boost_factor (default 1.5x when workflow active).
func (s *Scorer) applyWorkflowBoost(candidates map[string]*Suggestion, suggestCtx SuggestContext) {
	if s.workflowTracker == nil {
		return
	}

	// Get workflow next-step candidates (the tracker has already been fed the current command)
	workflowCandidates := s.workflowTracker.OnCommand(suggestCtx.LastTemplateID)
	if len(workflowCandidates) == 0 {
		return
	}

	// Build a set of workflow next-step display names for matching
	workflowNextSteps := make(map[string]bool)
	for _, wc := range workflowCandidates {
		if wc.DisplayName != "" {
			workflowNextSteps[wc.DisplayName] = true
		}
	}

	boostFactor := s.cfg.Amplifiers.WorkflowBoostFactor
	if boostFactor == 0 {
		boostFactor = DefaultWorkflowBoostFactor
	}

	for cmd, sug := range candidates {
		if workflowNextSteps[cmd] {
			boostAmount := sug.Score * (boostFactor - 1.0)
			sug.Score += boostAmount
			sug.scores.workflowBoost = boostAmount
			sug.Reasons = append(sug.Reasons, ReasonWorkflowBoost)
		}
	}

	// Also add workflow candidates that are not yet in the candidate set
	for _, wc := range workflowCandidates {
		if wc.DisplayName == "" {
			continue
		}
		if _, exists := candidates[wc.DisplayName]; !exists {
			// Add as a new candidate with a base workflow score
			baseScore := s.cfg.Weights.GlobalTransition * 0.5 // Give a moderate base
			boostedScore := baseScore * boostFactor
			candidates[wc.DisplayName] = &Suggestion{
				Command: wc.DisplayName,
				Score:   boostedScore,
				Reasons: []string{ReasonWorkflowBoost},
				scores:  scoreInfo{workflowBoost: boostedScore},
			}
		}
	}
}

// applyPipelineConfidence adds pipeline confidence scores for candidates
// that match pipeline continuation patterns.
// Per spec Section 7.1: pipeline_confidence as direct weight addition.
func (s *Scorer) applyPipelineConfidence(ctx context.Context, candidates map[string]*Suggestion, suggestCtx SuggestContext) {
	if s.pipelineStore == nil || suggestCtx.LastTemplateID == "" {
		return
	}

	pipelineScope := suggestCtx.Scope
	if suggestCtx.RepoKey != "" {
		pipelineScope = suggestCtx.RepoKey
	}

	// Get next pipeline segments
	nextSegments, err := s.pipelineStore.GetNextSegments(ctx, pipelineScope, suggestCtx.LastTemplateID, "", 10)
	if err != nil {
		s.cfg.Logger.Debug("pipeline confidence query failed", "error", err)
		return
	}

	pipelineWeight := s.cfg.Amplifiers.PipelineConfidenceWeight
	if pipelineWeight == 0 {
		pipelineWeight = DefaultPipelineConfWeight
	}

	for _, seg := range nextSegments {
		if seg.NextCmdNorm == "" {
			continue
		}
		// Normalize the pipeline weight by the segment's confidence
		confScore := seg.Weight * pipelineWeight

		if existing, ok := candidates[seg.NextCmdNorm]; ok {
			existing.Score += confScore
			existing.scores.pipelineConf += confScore
			existing.Reasons = append(existing.Reasons, ReasonPipelineConf)
		} else {
			candidates[seg.NextCmdNorm] = &Suggestion{
				Command: seg.NextCmdNorm,
				Score:   confScore,
				Reasons: []string{ReasonPipelineConf},
				scores:  scoreInfo{pipelineConf: confScore},
			}
		}
	}
}

// applyRecoveryBoost amplifies recovery candidates when the last command failed.
// Per spec Section 7.1: recovery_boost_factor (default 2.0x after failure).
func (s *Scorer) applyRecoveryBoost(ctx context.Context, candidates map[string]*Suggestion, suggestCtx SuggestContext) {
	if s.recoveryEngine == nil || !suggestCtx.LastFailed {
		return
	}

	recoveryCandidates, err := s.recoveryEngine.QueryRecoveries(
		ctx,
		suggestCtx.LastTemplateID,
		suggestCtx.LastExitCode,
		suggestCtx.Scope,
	)
	if err != nil {
		s.cfg.Logger.Debug("recovery query failed", "error", err)
		return
	}

	boostFactor := s.cfg.Amplifiers.RecoveryBoostFactor
	if boostFactor == 0 {
		boostFactor = DefaultRecoveryBoostFactor
	}

	for _, rc := range recoveryCandidates {
		if rc.RecoveryCmdNorm == "" {
			continue
		}
		if existing, ok := candidates[rc.RecoveryCmdNorm]; ok {
			// Boost existing candidate
			boostAmount := existing.Score * (boostFactor - 1.0)
			existing.Score += boostAmount
			existing.scores.recoveryBoost = boostAmount
			existing.Reasons = append(existing.Reasons, ReasonRecoveryBoost)
		} else {
			// Add as new candidate with recovery-based score
			recoveryScore := rc.SuccessRate * rc.Weight * boostFactor * 10.0
			candidates[rc.RecoveryCmdNorm] = &Suggestion{
				Command: rc.RecoveryCmdNorm,
				Score:   recoveryScore,
				Reasons: []string{ReasonRecoveryBoost},
				scores:  scoreInfo{recoveryBoost: recoveryScore},
			}
		}
	}
}

// applyDismissalPenalties reduces scores for previously dismissed suggestions.
// Per spec Section 7.1:
//   - dismissed: score *= DismissalPenaltyFactor (default 0.3)
//   - permanent/never: score *= PermanentPenaltyFactor (default 0.0)
func (s *Scorer) applyDismissalPenalties(ctx context.Context, candidates map[string]*Suggestion, suggestCtx SuggestContext) {
	if s.dismissalStore == nil || suggestCtx.LastTemplateID == "" {
		return
	}

	for cmd, sug := range candidates {
		templateID := sug.TemplateID
		if templateID == "" {
			// Use command itself as a proxy template ID
			templateID = cmd
		}

		state, err := s.dismissalStore.GetState(ctx, suggestCtx.Scope, suggestCtx.LastTemplateID, templateID)
		if err != nil {
			s.cfg.Logger.Debug("dismissal state query failed", "error", err, "cmd", cmd)
			continue
		}

		switch state {
		case dismissal.StatePermanent:
			// "Never show" feedback: multiply by PermanentPenaltyFactor (default 0.0)
			penaltyFactor := s.cfg.Amplifiers.PermanentPenaltyFactor
			penaltyAmount := sug.Score * (1.0 - penaltyFactor)
			sug.Score *= penaltyFactor
			sug.scores.dismissalPenalty = -penaltyAmount
			sug.Reasons = append(sug.Reasons, ReasonDismissalPenalty)

		case dismissal.StateLearned:
			// Learned dismissal: multiply by DismissalPenaltyFactor (default 0.3)
			penaltyFactor := s.cfg.Amplifiers.DismissalPenaltyFactor
			penaltyAmount := sug.Score * (1.0 - penaltyFactor)
			sug.Score *= penaltyFactor
			sug.scores.dismissalPenalty = -penaltyAmount
			sug.Reasons = append(sug.Reasons, ReasonDismissalPenalty)

		case dismissal.StateTemporary:
			// Temporary dismissal: lighter penalty (halfway between full and learned)
			penaltyFactor := (1.0 + s.cfg.Amplifiers.DismissalPenaltyFactor) / 2.0
			penaltyAmount := sug.Score * (1.0 - penaltyFactor)
			sug.Score *= penaltyFactor
			sug.scores.dismissalPenalty = -penaltyAmount
			sug.Reasons = append(sug.Reasons, ReasonDismissalPenalty)
		}
	}
}

// filterByPrefix returns a filtered candidate map containing only commands
// that match the given prefix.
// Per spec Section 6.4:
//   - Empty prefix = pure next-step mode (all candidates)
//   - Non-empty prefix = constrained mode (exact prefix match + fuzzy tolerance)
func (s *Scorer) filterByPrefix(candidates map[string]*Suggestion, prefix string) map[string]*Suggestion {
	filtered := make(map[string]*Suggestion)
	prefixLower := strings.ToLower(prefix)

	for cmd, sug := range candidates {
		cmdLower := strings.ToLower(cmd)

		// Exact prefix match
		if strings.HasPrefix(cmdLower, prefixLower) {
			filtered[cmd] = sug
			continue
		}

		// Fuzzy tolerance: allow prefix match on the base command (first word)
		cmdParts := strings.Fields(cmdLower)
		prefixParts := strings.Fields(prefixLower)
		if len(cmdParts) > 0 && len(prefixParts) > 0 {
			if strings.HasPrefix(cmdParts[0], prefixParts[0]) {
				filtered[cmd] = sug
				continue
			}
		}

		// Fuzzy tolerance: allow one edit distance on short prefixes
		if len(prefixLower) <= 5 && len(cmdLower) >= len(prefixLower) {
			cmdPrefix := cmdLower[:len(prefixLower)]
			if editDistance(prefixLower, cmdPrefix) <= 1 {
				filtered[cmd] = sug
			}
		}
	}

	return filtered
}

// editDistance computes Levenshtein distance between two strings.
func editDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}

	return prev[len(b)]
}

// suppressNearDuplicates removes suggestions that differ only in slot values
// when the template_id is the same. Keeps the highest-scored variant.
// Per spec Section 7.4.
func suppressNearDuplicates(suggestions []Suggestion) []Suggestion {
	if len(suggestions) == 0 {
		return suggestions
	}

	// Group by template ID and keep highest scored variant
	seen := make(map[string]int) // templateID -> index in result
	result := make([]Suggestion, 0, len(suggestions))

	for _, sug := range suggestions {
		// If no template ID, treat each command as unique
		if sug.TemplateID == "" {
			result = append(result, sug)
			continue
		}

		if idx, ok := seen[sug.TemplateID]; ok {
			// Keep the higher-scored variant
			if sug.Score > result[idx].Score {
				result[idx] = sug
			}
		} else {
			seen[sug.TemplateID] = len(result)
			result = append(result, sug)
		}
	}

	return result
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

		// Track frequency for tie-breaking (use max of raw scores)
		if rawScore > existing.frequency {
			existing.frequency = rawScore
		}

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
			Command:   cmd,
			Score:     adjustedScore,
			Reasons:   []string{reason},
			frequency: rawScore,
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
// Per spec Section 7.3: confidence calibrated from feature support diversity
// and score magnitude.
// Final confidence = sum(feature_scores * weights) / max_possible_score, capped at [0, 1.0]
func (s *Scorer) calculateConfidence(sug *Suggestion) float64 {
	// Count the number of active scoring sources (features contributing)
	sourceCount := 0
	totalSources := 10 // Total number of possible feature sources

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
	if sug.scores.workflowBoost > 0 {
		sourceCount++
	}
	if sug.scores.pipelineConf > 0 {
		sourceCount++
	}
	if sug.scores.recoveryBoost > 0 {
		sourceCount++
	}

	// Base confidence from source diversity (up to 0.5)
	sourceConfidence := float64(sourceCount) / float64(totalSources*2)

	// Score-based confidence (up to 0.5)
	// Normalize score to 0-0.5 range using sigmoid
	scoreConfidence := 0.5 / (1.0 + math.Exp(-sug.Score/50.0))

	confidence := sourceConfidence + scoreConfidence

	// Cap at [0, 1.0]
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0 {
		confidence = 0
	}

	return confidence
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
