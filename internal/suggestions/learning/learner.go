package learning

import (
	"context"
	"log/slog"
	"math"
	"sync"
)

// Config holds tuning parameters for the online learner.
// Defaults match spec Section 7.7 and Section 16.
type Config struct {
	// EtaInitial is the initial learning rate (default 0.02).
	EtaInitial float64

	// EtaDecayConstant controls how fast eta decays with sample count.
	// eta_effective = EtaInitial / (1 + sampleCount / EtaDecayConstant).
	// Default 500.
	EtaDecayConstant float64

	// EtaFloor is the minimum effective learning rate (default 0.001).
	EtaFloor float64

	// WeightMin is the minimum allowed weight for non-penalty features (default 0.0).
	WeightMin float64

	// WeightMax is the maximum allowed weight for non-penalty features (default 0.60).
	WeightMax float64

	// WeightRiskMin is the minimum allowed risk penalty weight (default 0.10).
	WeightRiskMin float64

	// WeightRiskMax is the maximum allowed risk penalty weight (default 0.60).
	WeightRiskMax float64

	// MinSamples is the number of feedback events required before
	// weight updates begin. Below this threshold, static defaults are used.
	// Default 30.
	MinSamples int64

	// Logger for diagnostic output (optional).
	Logger *slog.Logger
}

// DefaultConfig returns the spec-default learner configuration.
func DefaultConfig() Config {
	return Config{
		EtaInitial:       0.02,
		EtaDecayConstant: 500,
		EtaFloor:         0.001,
		WeightMin:        0.0,
		WeightMax:        0.60,
		WeightRiskMin:    0.10,
		WeightRiskMax:    0.60,
		MinSamples:       30,
		Logger:           slog.Default(),
	}
}

// FeatureVector holds the per-feature contribution values for a single
// candidate, normalised into [0, 1] before the learner sees them.
type FeatureVector struct {
	Transition          float64
	Frequency           float64
	Success             float64
	Prefix              float64
	Affinity            float64
	Task                float64
	Feedback            float64
	ProjectTypeAffinity float64
	FailureRecovery     float64
	RiskPenalty         float64
}

// Learner implements adaptive weight tuning with pairwise updates and
// eta decay per spec Section 7.7. It is safe for concurrent use.
type Learner struct {
	weights     Weights
	sampleCount int64
	config      Config
	store       *Store
	mu          sync.RWMutex
}

// NewLearner creates a learner with the given initial weights, config,
// and optional persistence store. If store is nil, weights are kept
// in memory only.
func NewLearner(initial Weights, cfg Config, store *Store) *Learner {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Learner{
		weights: initial,
		config:  cfg,
		store:   store,
	}
}

// Weights returns a snapshot of the current weight vector.
func (l *Learner) Weights() Weights {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.weights
}

// SampleCount returns the current sample count.
func (l *Learner) SampleCount() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.sampleCount
}

// Eta computes the effective learning rate for the current sample count.
// eta_effective = max(EtaFloor, EtaInitial / (1 + sampleCount / EtaDecayConstant))
func (l *Learner) Eta() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.etaLocked()
}

// etaLocked computes eta while the caller already holds at least a read lock.
func (l *Learner) etaLocked() float64 {
	return effectiveEta(l.config.EtaInitial, l.config.EtaDecayConstant, l.config.EtaFloor, l.sampleCount)
}

// effectiveEta is a pure function for the eta decay formula.
func effectiveEta(etaInitial, etaDecayConstant, etaFloor float64, sampleCount int64) float64 {
	eta := etaInitial / (1.0 + float64(sampleCount)/etaDecayConstant)
	if eta < etaFloor {
		eta = etaFloor
	}
	return eta
}

// Update performs a pairwise weight update from a feedback event.
//
// fPos is the feature vector of the accepted (or positively reinforced) candidate.
// fNeg is the feature vector of the highest-ranked non-accepted candidate.
//
// The update rule (spec Section 7.7):
//
//	w_next = clamp(w_prev + eta * (f_pos - f_neg), min_w, max_w)
//
// After clamping, non-penalty weights are renormalized to preserve the
// original non-penalty weight sum.
//
// If sampleCount < MinSamples the call increments the counter but
// does not modify weights (low-sample freeze).
//
// The scope parameter identifies the weight profile (e.g. "session:<id>",
// "repo:<key>", or "global"). When a store is configured the updated
// profile is persisted asynchronously.
func (l *Learner) Update(ctx context.Context, scope string, fPos, fNeg FeatureVector) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sampleCount++

	// Low-sample freeze: do not update weights below threshold.
	if l.sampleCount < l.config.MinSamples {
		l.config.Logger.Debug("learning: low-sample freeze",
			"sample_count", l.sampleCount,
			"min_samples", l.config.MinSamples,
		)
		return
	}

	eta := l.etaLocked()
	w := &l.weights

	// Capture non-penalty sum before update for renormalization.
	nonPenaltySumBefore := nonPenaltySum(w)

	// Pairwise update: w += eta * (fPos - fNeg)
	w.Transition += eta * (fPos.Transition - fNeg.Transition)
	w.Frequency += eta * (fPos.Frequency - fNeg.Frequency)
	w.Success += eta * (fPos.Success - fNeg.Success)
	w.Prefix += eta * (fPos.Prefix - fNeg.Prefix)
	w.Affinity += eta * (fPos.Affinity - fNeg.Affinity)
	w.Task += eta * (fPos.Task - fNeg.Task)
	w.Feedback += eta * (fPos.Feedback - fNeg.Feedback)
	w.ProjectTypeAffinity += eta * (fPos.ProjectTypeAffinity - fNeg.ProjectTypeAffinity)
	w.FailureRecovery += eta * (fPos.FailureRecovery - fNeg.FailureRecovery)

	// Risk penalty lives in an independent safe range.
	w.RiskPenalty += eta * (fPos.RiskPenalty - fNeg.RiskPenalty)

	// Clamp all weights to their allowed ranges.
	l.clampWeights()

	// Renormalize non-penalty weights to preserve original total.
	l.renormalize(nonPenaltySumBefore)

	l.config.Logger.Debug("learning: weight update",
		"scope", scope,
		"eta", eta,
		"sample_count", l.sampleCount,
	)

	// Persist asynchronously if store is available.
	if l.store != nil {
		wCopy := l.weights
		sc := l.sampleCount
		lr := eta
		go func() {
			if err := l.store.SaveWeights(ctx, scope, &wCopy, sc, lr); err != nil {
				l.config.Logger.Error("learning: failed to persist weights",
					"scope", scope, "error", err)
			}
		}()
	}
}

// LoadFromStore loads persisted weights for the given scope, replacing
// the current in-memory state. Returns false if no persisted profile
// was found (in-memory state is unchanged).
func (l *Learner) LoadFromStore(ctx context.Context, scope string) (bool, error) {
	if l.store == nil {
		return false, nil
	}
	profile, err := l.store.LoadWeights(ctx, scope)
	if err != nil {
		return false, err
	}
	if profile == nil {
		return false, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.weights = profile.Weights
	l.sampleCount = profile.SampleCount
	return true, nil
}

// clampWeights clamps each weight to its configured range.
// Must be called with l.mu held.
func (l *Learner) clampWeights() {
	w := &l.weights
	cfg := &l.config

	w.Transition = clamp(w.Transition, cfg.WeightMin, cfg.WeightMax)
	w.Frequency = clamp(w.Frequency, cfg.WeightMin, cfg.WeightMax)
	w.Success = clamp(w.Success, cfg.WeightMin, cfg.WeightMax)
	w.Prefix = clamp(w.Prefix, cfg.WeightMin, cfg.WeightMax)
	w.Affinity = clamp(w.Affinity, cfg.WeightMin, cfg.WeightMax)
	w.Task = clamp(w.Task, cfg.WeightMin, cfg.WeightMax)
	w.Feedback = clamp(w.Feedback, cfg.WeightMin, cfg.WeightMax)
	w.ProjectTypeAffinity = clamp(w.ProjectTypeAffinity, cfg.WeightMin, cfg.WeightMax)
	w.FailureRecovery = clamp(w.FailureRecovery, cfg.WeightMin, cfg.WeightMax)

	// Risk penalty has its own independent range.
	w.RiskPenalty = clamp(w.RiskPenalty, cfg.WeightRiskMin, cfg.WeightRiskMax)
}

// renormalize scales non-penalty weights so their sum equals targetSum.
// If the post-clamp sum is zero, no scaling is applied.
// Must be called with l.mu held.
func (l *Learner) renormalize(targetSum float64) {
	w := &l.weights

	currentSum := nonPenaltySum(w)
	if currentSum == 0 || targetSum == 0 {
		return
	}

	scale := targetSum / currentSum

	w.Transition *= scale
	w.Frequency *= scale
	w.Success *= scale
	w.Prefix *= scale
	w.Affinity *= scale
	w.Task *= scale
	w.Feedback *= scale
	w.ProjectTypeAffinity *= scale
	w.FailureRecovery *= scale

	// Re-clamp after scaling to respect bounds.
	l.clampWeights()
}

// nonPenaltySum returns the sum of all weights except RiskPenalty.
func nonPenaltySum(w *Weights) float64 {
	return w.Transition + w.Frequency + w.Success + w.Prefix +
		w.Affinity + w.Task + w.Feedback +
		w.ProjectTypeAffinity + w.FailureRecovery
}

// clamp restricts v to the range [lo, hi].
func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(v, hi))
}
