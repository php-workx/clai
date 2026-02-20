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

	"github.com/runger/clai/internal/suggestions/discover"
	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/normalize"
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
	Logger     *slog.Logger
	Weights    Weights
	Amplifiers AmplifierConfig
	TopK       int
}

// DefaultScorerConfig returns the default scorer configuration.
func DefaultScorerConfig() *ScorerConfig {
	return &ScorerConfig{
		Weights:    DefaultWeights(),
		Amplifiers: DefaultAmplifierConfig(),
		TopK:       DefaultTopK,
		Logger:     slog.Default(),
	}
}

// Suggestion represents a scored command suggestion.
type Suggestion struct {
	Command       string
	TemplateID    string
	Reasons       []string
	scores        scoreInfo
	Score         float64
	Confidence    float64
	frequency     float64
	lastSeenMs    int64
	maxFreqScore  float64
	maxTransCount int
}

// LastSeenMs returns the best-effort last-seen timestamp (unix ms) for this suggestion.
// This is used for UI hints and deterministic tie-breaking.
func (s *Suggestion) LastSeenMs() int64 { return s.lastSeenMs }

// MaxFreqScore returns the best-effort decayed frequency score observed for this suggestion.
func (s *Suggestion) MaxFreqScore() float64 { return s.maxFreqScore }

// MaxTransitionCount returns the best-effort transition count observed for this suggestion.
func (s *Suggestion) MaxTransitionCount() int { return s.maxTransCount }

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
	freqStore         *score.FrequencyStore  // legacy, retained for test compatibility
	transitionStore   *score.TransitionStore // legacy, retained for test compatibility
	pipelineStore     *score.PipelineStore
	discoveryService  *discovery.Service
	discoverEngine    *discover.Engine
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
	DiscoverEngine   *discover.Engine
	WorkflowTracker  *workflow.Tracker
	DismissalStore   *dismissal.Store
	RecoveryEngine   *recovery.Engine
}

// NewScorer creates a new suggestion scorer.
func NewScorer(deps *ScorerDependencies, cfg *ScorerConfig) (*Scorer, error) {
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
		discoverEngine:    deps.DiscoverEngine,
		workflowTracker:   deps.WorkflowTracker,
		dismissalStore:    deps.DismissalStore,
		recoveryEngine:    deps.RecoveryEngine,
		dangerousCommands: buildDangerousCommands(),
		cfg:               *cfg,
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
	LastCmd        string
	LastTemplateID string
	Prefix         string
	Cwd            string
	DirScopeKey    string
	Scope          string
	ProjectTypes   []string
	LastExitCode   int
	NowMs          int64
	LastFailed     bool
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
func (s *Scorer) Suggest(ctx context.Context, suggestCtx *SuggestContext) ([]Suggestion, error) {
	s.normalizeSuggestContext(suggestCtx)
	candidates := make(map[string]*Suggestion)

	s.collectCandidates(ctx, suggestCtx, candidates)
	s.applyContextBoosts(ctx, candidates, suggestCtx)
	s.applyDangerousPenalties(candidates)
	s.applyDismissalPenalties(ctx, candidates, suggestCtx)

	candidates = s.applyPrefixFilter(candidates, suggestCtx.Prefix)
	s.suppressLastCommand(candidates, suggestCtx.LastCmd)

	return s.finalizeSuggestions(candidates), nil
}

func (s *Scorer) normalizeSuggestContext(suggestCtx *SuggestContext) {
	if suggestCtx.NowMs == 0 {
		suggestCtx.NowMs = time.Now().UnixMilli()
	}
	if suggestCtx.Scope == "" {
		suggestCtx.Scope = score.ScopeGlobal
	}
	if suggestCtx.LastTemplateID == "" && suggestCtx.LastCmd != "" {
		suggestCtx.LastTemplateID = normalize.PreNormalize(suggestCtx.LastCmd, normalize.PreNormConfig{}).TemplateID
	}
}

func (s *Scorer) collectCandidates(ctx context.Context, suggestCtx *SuggestContext, candidates map[string]*Suggestion) {
	s.collectTransitionCandidates(
		ctx, candidates, suggestCtx.RepoKey, suggestCtx.LastTemplateID, suggestCtx.LastCmd,
		ReasonRepoTransition, s.cfg.Weights.RepoTransition, "repo transitions query failed",
	)
	s.collectTransitionCandidates(
		ctx, candidates, score.ScopeGlobal, suggestCtx.LastTemplateID, suggestCtx.LastCmd,
		ReasonGlobalTransition, s.cfg.Weights.GlobalTransition, "global transitions query failed",
	)
	s.collectTransitionCandidates(
		ctx, candidates, suggestCtx.DirScopeKey, suggestCtx.LastTemplateID, suggestCtx.LastCmd,
		ReasonDirTransition, s.cfg.Weights.DirTransition, "dir transitions query failed",
	)

	s.collectFrequencyCandidates(
		ctx, candidates, suggestCtx.RepoKey, ReasonRepoFrequency,
		s.cfg.Weights.RepoFrequency, suggestCtx.NowMs, "repo frequency query failed",
	)
	s.collectFrequencyCandidates(
		ctx, candidates, score.ScopeGlobal, ReasonGlobalFrequency,
		s.cfg.Weights.GlobalFrequency, suggestCtx.NowMs, "global frequency query failed",
	)
	s.collectFrequencyCandidates(
		ctx, candidates, suggestCtx.DirScopeKey, ReasonDirFrequency,
		s.cfg.Weights.DirFrequency, suggestCtx.NowMs, "dir frequency query failed",
	)

	s.collectProjectTasks(ctx, candidates, suggestCtx.RepoKey)
	s.collectProjectTypeCandidates(ctx, candidates, suggestCtx.ProjectTypes, suggestCtx.LastTemplateID)
	s.collectDiscoveryPriors(candidates, suggestCtx)
}

func (s *Scorer) collectTransitionCandidates(
	ctx context.Context,
	candidates map[string]*Suggestion,
	scope string,
	lastTemplateID string,
	lastCmd string,
	reason string,
	weight float64,
	logMessage string,
) {
	if scope == "" {
		return
	}

	if s.db == nil || lastTemplateID == "" {
		if s.transitionStore == nil || lastCmd == "" {
			return
		}
		transitions, err := s.transitionStore.GetTopNextCommands(ctx, scope, lastCmd, 10)
		if err != nil {
			s.cfg.Logger.Debug(logMessage, "error", err)
			return
		}
		for _, t := range transitions {
			s.addCandidate(candidates, t.NextNorm, float64(t.Count), reason, weight, t.LastTSMs)
		}
		return
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ct.cmd_norm, ts.count, ts.last_seen_ms
		FROM transition_stat ts
		JOIN command_template ct ON ct.template_id = ts.next_template_id
		WHERE ts.scope = ? AND ts.prev_template_id = ?
		ORDER BY ts.weight DESC, ts.count DESC
		LIMIT 10
	`, scope, lastTemplateID)
	if err != nil {
		if s.transitionStore != nil && lastCmd != "" {
			transitions, legacyErr := s.transitionStore.GetTopNextCommands(ctx, scope, lastCmd, 10)
			if legacyErr == nil {
				for _, t := range transitions {
					s.addCandidate(candidates, t.NextNorm, float64(t.Count), reason, weight, t.LastTSMs)
				}
				return
			}
		}
		s.cfg.Logger.Debug(logMessage, "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cmdNorm string
		var count int
		var lastSeenMs int64
		if err := rows.Scan(&cmdNorm, &count, &lastSeenMs); err != nil {
			s.cfg.Logger.Debug(logMessage, "error", err)
			return
		}
		s.addCandidate(candidates, cmdNorm, float64(count), reason, weight, lastSeenMs)
	}
	if err := rows.Err(); err != nil {
		s.cfg.Logger.Debug(logMessage, "error", err)
	}
}

func (s *Scorer) collectFrequencyCandidates(
	ctx context.Context,
	candidates map[string]*Suggestion,
	scope string,
	reason string,
	weight float64,
	nowMs int64,
	logMessage string,
) {
	if scope == "" {
		return
	}
	if s.db == nil {
		if s.freqStore == nil {
			return
		}
		frequencies, err := s.freqStore.GetTopCommandsAt(ctx, scope, 10, nowMs)
		if err != nil {
			s.cfg.Logger.Debug(logMessage, "error", err)
			return
		}
		for _, f := range frequencies {
			s.addCandidate(candidates, f.CmdNorm, f.Score, reason, weight, f.LastTSMs)
		}
		return
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ct.cmd_norm, cs.score, cs.last_seen_ms
		FROM command_stat cs
		JOIN command_template ct ON ct.template_id = cs.template_id
		WHERE cs.scope = ?
		ORDER BY cs.score DESC
		LIMIT 10
	`, scope)
	if err != nil {
		if s.freqStore != nil {
			frequencies, legacyErr := s.freqStore.GetTopCommandsAt(ctx, scope, 10, nowMs)
			if legacyErr == nil {
				for _, f := range frequencies {
					s.addCandidate(candidates, f.CmdNorm, f.Score, reason, weight, f.LastTSMs)
				}
				return
			}
		}
		s.cfg.Logger.Debug(logMessage, "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cmdNorm string
		var scoreVal float64
		var lastSeenMs int64
		if err := rows.Scan(&cmdNorm, &scoreVal, &lastSeenMs); err != nil {
			s.cfg.Logger.Debug(logMessage, "error", err)
			return
		}
		s.addCandidate(candidates, cmdNorm, applyRecencyDecay(scoreVal, lastSeenMs, nowMs, s.cfg.Amplifiers.RecencyDecayTauMs), reason, weight, lastSeenMs)
	}
	if err := rows.Err(); err != nil {
		s.cfg.Logger.Debug(logMessage, "error", err)
	}
}

func (s *Scorer) collectProjectTasks(ctx context.Context, candidates map[string]*Suggestion, repoKey string) {
	if s.discoveryService == nil || repoKey == "" {
		return
	}
	_ = s.discoveryService.DiscoverIfNeeded(ctx, repoKey)

	tasks, err := s.discoveryService.GetTasks(ctx, repoKey)
	if err != nil {
		s.cfg.Logger.Debug("project tasks query failed", "error", err)
		return
	}

	for _, t := range tasks {
		s.addCandidate(candidates, t.Command, 1.0, ReasonProjectTask, s.cfg.Weights.ProjectTask, 0)
	}
}

func (s *Scorer) collectProjectTypeCandidates(
	ctx context.Context,
	candidates map[string]*Suggestion,
	projectTypes []string,
	lastTemplateID string,
) {
	if s.db == nil || len(projectTypes) == 0 {
		return
	}
	for _, pt := range projectTypes {
		rows, err := s.db.QueryContext(ctx, `
			SELECT ct.cmd_norm, pts.score, pts.last_seen_ms
			FROM project_type_stat pts
			JOIN command_template ct ON ct.template_id = pts.template_id
			WHERE pts.project_type = ?
			ORDER BY pts.score DESC
			LIMIT 5
		`, pt)
		if err == nil {
			for rows.Next() {
				var cmdNorm string
				var scoreVal float64
				var lastSeenMs int64
				if scanErr := rows.Scan(&cmdNorm, &scoreVal, &lastSeenMs); scanErr == nil {
					s.addCandidate(candidates, cmdNorm, scoreVal, ReasonProjectTask, s.cfg.Weights.ProjectTask*0.8, lastSeenMs)
				}
			}
			rows.Close()
		}

		if lastTemplateID == "" {
			continue
		}
		transRows, err := s.db.QueryContext(ctx, `
			SELECT ct.cmd_norm, ptt.count, ptt.last_seen_ms
			FROM project_type_transition ptt
			JOIN command_template ct ON ct.template_id = ptt.next_template_id
			WHERE ptt.project_type = ? AND ptt.prev_template_id = ?
			ORDER BY ptt.weight DESC, ptt.count DESC
			LIMIT 5
		`, pt, lastTemplateID)
		if err != nil {
			continue
		}
		for transRows.Next() {
			var cmdNorm string
			var count int
			var lastSeenMs int64
			if scanErr := transRows.Scan(&cmdNorm, &count, &lastSeenMs); scanErr == nil {
				s.addCandidate(candidates, cmdNorm, float64(count), ReasonRepoTransition, s.cfg.Weights.RepoTransition*0.7, lastSeenMs)
			}
		}
		transRows.Close()
	}
}

func (s *Scorer) collectDiscoveryPriors(candidates map[string]*Suggestion, suggestCtx *SuggestContext) {
	if s.discoverEngine == nil {
		return
	}
	// Prior discovery only supplements sparse/empty contexts.
	if suggestCtx.Prefix != "" || len(candidates) >= 3 {
		return
	}

	prior := s.discoverEngine.Discover(context.Background(), discover.DiscoverConfig{
		ProjectTypes: suggestCtx.ProjectTypes,
		Limit:        5,
		CooldownMs:   0,
	})
	for _, c := range prior {
		s.addCandidate(candidates, c.Command, float64(c.Priority), ReasonProjectTask, s.cfg.Weights.ProjectTask*0.5, 0)
	}
}

func (s *Scorer) applyContextBoosts(ctx context.Context, candidates map[string]*Suggestion, suggestCtx *SuggestContext) {
	s.applyWorkflowBoost(candidates, suggestCtx)
	s.applyPipelineConfidence(ctx, candidates, suggestCtx)
	s.applyRecoveryBoost(ctx, candidates, suggestCtx)
}

func (s *Scorer) applyDangerousPenalties(candidates map[string]*Suggestion) {
	for cmd, sug := range candidates {
		if !s.isDangerous(cmd) {
			continue
		}
		sug.scores.dangerous = s.cfg.Weights.DangerousPenalty
		sug.Score += s.cfg.Weights.DangerousPenalty
		sug.Reasons = append(sug.Reasons, ReasonDangerous)
	}
}

func (s *Scorer) applyPrefixFilter(candidates map[string]*Suggestion, prefix string) map[string]*Suggestion {
	if prefix == "" {
		return candidates
	}
	return s.filterByPrefix(candidates, prefix)
}

func (s *Scorer) suppressLastCommand(candidates map[string]*Suggestion, lastCmd string) {
	if lastCmd == "" {
		return
	}

	delete(candidates, lastCmd)

	normalized := strings.TrimSpace(normalize.NormalizeSimple(lastCmd))
	if normalized != "" && normalized != lastCmd {
		delete(candidates, normalized)
	}
}

func (s *Scorer) finalizeSuggestions(candidates map[string]*Suggestion) []Suggestion {
	suggestions := make([]Suggestion, 0, len(candidates))
	for _, sug := range candidates {
		sug.Confidence = s.calculateConfidence(sug)
		suggestions = append(suggestions, *sug)
	}

	suggestions = suppressNearDuplicates(suggestions)
	sortSuggestions(suggestions)

	if len(suggestions) > s.cfg.TopK {
		return suggestions[:s.cfg.TopK]
	}
	return suggestions
}

func sortSuggestions(suggestions []Suggestion) {
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
}

// applyWorkflowBoost amplifies candidates that match active workflow next-steps.
// Per spec Section 7.1: workflow_boost_factor (default 1.5x when workflow active).
func (s *Scorer) applyWorkflowBoost(candidates map[string]*Suggestion, suggestCtx *SuggestContext) {
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
func (s *Scorer) applyPipelineConfidence(ctx context.Context, candidates map[string]*Suggestion, suggestCtx *SuggestContext) {
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
func (s *Scorer) applyRecoveryBoost(ctx context.Context, candidates map[string]*Suggestion, suggestCtx *SuggestContext) {
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
func (s *Scorer) applyDismissalPenalties(ctx context.Context, candidates map[string]*Suggestion, suggestCtx *SuggestContext) {
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
		if state == dismissal.StateNone && templateID != cmd {
			if fallbackState, fallbackErr := s.dismissalStore.GetState(ctx, suggestCtx.Scope, suggestCtx.LastTemplateID, cmd); fallbackErr == nil {
				state = fallbackState
			}
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
		default:
			// StateNone or unknown: no penalty.
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
	if a == "" {
		return len(b)
	}
	if b == "" {
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

	for i := range suggestions {
		sug := suggestions[i]
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
func (s *Scorer) addCandidate(candidates map[string]*Suggestion, cmd string, rawScore float64, reason string, weight float64, lastSeenMs int64) {
	adjustedScore := math.Log(rawScore+1) * weight

	if existing, ok := candidates[cmd]; ok {
		s.mergeCandidate(existing, rawScore, reason, adjustedScore, lastSeenMs)
		return
	}

	candidates[cmd] = newCandidate(cmd, rawScore, reason, adjustedScore, lastSeenMs)
}

func applyRecencyDecay(scoreVal float64, lastSeenMs, nowMs, tauMs int64) float64 {
	if scoreVal <= 0 || lastSeenMs <= 0 || nowMs <= 0 || tauMs <= 0 {
		return scoreVal
	}
	elapsed := float64(nowMs - lastSeenMs)
	if elapsed <= 0 {
		return scoreVal
	}
	return scoreVal * math.Exp(-elapsed/float64(tauMs))
}

func (s *Scorer) mergeCandidate(existing *Suggestion, rawScore float64, reason string, adjustedScore float64, lastSeenMs int64) {
	existing.Score += adjustedScore
	existing.Reasons = append(existing.Reasons, reason)

	if rawScore > existing.frequency {
		existing.frequency = rawScore
	}
	if lastSeenMs > existing.lastSeenMs {
		existing.lastSeenMs = lastSeenMs
	}

	updateSuggestionRawSignals(existing, reason, rawScore)
	applySuggestionScore(existing, reason, adjustedScore)
}

func newCandidate(cmd string, rawScore float64, reason string, adjustedScore float64, lastSeenMs int64) *Suggestion {
	templateID := normalize.PreNormalize(cmd, normalize.PreNormConfig{}).TemplateID
	suggestion := &Suggestion{
		Command:    cmd,
		TemplateID: templateID,
		Score:      adjustedScore,
		Reasons:    []string{reason},
		frequency:  rawScore,
		lastSeenMs: lastSeenMs,
	}

	updateSuggestionRawSignals(suggestion, reason, rawScore)
	applySuggestionScore(suggestion, reason, adjustedScore)
	return suggestion
}

func updateSuggestionRawSignals(suggestion *Suggestion, reason string, rawScore float64) {
	switch reason {
	case ReasonRepoFrequency, ReasonGlobalFrequency, ReasonDirFrequency:
		if rawScore > suggestion.maxFreqScore {
			suggestion.maxFreqScore = rawScore
		}
	case ReasonRepoTransition, ReasonGlobalTransition, ReasonDirTransition:
		if int(rawScore) > suggestion.maxTransCount {
			suggestion.maxTransCount = int(rawScore)
		}
	default:
		// Other reasons don't track raw signals.
	}
}

func applySuggestionScore(suggestion *Suggestion, reason string, adjustedScore float64) {
	switch reason {
	case ReasonRepoTransition:
		suggestion.scores.repoTransition += adjustedScore
	case ReasonGlobalTransition:
		suggestion.scores.globalTransition += adjustedScore
	case ReasonRepoFrequency:
		suggestion.scores.repoFrequency += adjustedScore
	case ReasonGlobalFrequency:
		suggestion.scores.globalFrequency += adjustedScore
	case ReasonProjectTask:
		suggestion.scores.projectTask += adjustedScore
	case ReasonDirTransition:
		suggestion.scores.dirTransition += adjustedScore
	case ReasonDirFrequency:
		suggestion.scores.dirFrequency += adjustedScore
	default:
		// Amplifier reasons (workflow, pipeline, recovery, dismissal) are applied separately.
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

// SetWorkflowPatterns refreshes workflow tracker patterns at runtime.
func (s *Scorer) SetWorkflowPatterns(patterns []workflow.Pattern) {
	if s.workflowTracker == nil {
		return
	}
	s.workflowTracker.SetPatterns(patterns)
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
	LastTS  int64
}

// DebugScores returns the top scores from the command_score table for debugging.
func (s *Scorer) DebugScores(ctx context.Context, limit int) ([]DebugScore, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT cs.scope, ct.cmd_norm, cs.score, cs.last_seen_ms
		FROM command_stat cs
		JOIN command_template ct ON ct.template_id = cs.template_id
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
		if err := rows.Scan(&ds.Scope, &ds.CmdNorm, &ds.Score, &ds.LastTS); err != nil {
			return nil, err
		}
		scores = append(scores, ds)
	}

	return scores, rows.Err()
}
