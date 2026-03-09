// Package learning implements online adaptive weight tuning for the clai
// suggestions engine. It provides a pairwise update rule with eta decay,
// weight clamping, renormalization, and per-profile persistence backed by
// the rank_weight_profile SQLite table (V2 schema Section 4.2).
package learning

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Weights represents the 10-feature weight vector used by the ranking model.
// Field names correspond to the rank_weight_profile table columns.
type Weights struct {
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

// DefaultWeights returns the spec-default initial weights per Section 7.1.
func DefaultWeights() Weights {
	return Weights{
		Transition:          0.30,
		Frequency:           0.20,
		Success:             0.10,
		Prefix:              0.15,
		Affinity:            0.10,
		Task:                0.05,
		Feedback:            0.15,
		ProjectTypeAffinity: 0.08,
		FailureRecovery:     0.12,
		RiskPenalty:         0.20,
	}
}

// WeightProfile is the full persisted weight profile for a scope.
type WeightProfile struct {
	ProfileKey   string
	Scope        string
	Weights      Weights
	SampleCount  int64
	LearningRate float64
	UpdatedMs    int64
}

// Store persists and retrieves learned weight profiles from SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a new weight persistence store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// LoadWeights loads the weight profile for the given scope.
// Returns nil (no error) when no profile exists for the scope.
func (s *Store) LoadWeights(ctx context.Context, scope string) (*WeightProfile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT profile_key, scope, updated_ms,
		       w_transition, w_frequency, w_success, w_prefix,
		       w_affinity, w_task, w_feedback,
		       w_project_type_affinity, w_failure_recovery, w_risk_penalty,
		       sample_count, learning_rate
		FROM rank_weight_profile
		WHERE scope = ?
		ORDER BY updated_ms DESC
		LIMIT 1
	`, scope)

	var p WeightProfile
	err := row.Scan(
		&p.ProfileKey, &p.Scope, &p.UpdatedMs,
		&p.Weights.Transition, &p.Weights.Frequency, &p.Weights.Success,
		&p.Weights.Prefix, &p.Weights.Affinity, &p.Weights.Task,
		&p.Weights.Feedback, &p.Weights.ProjectTypeAffinity,
		&p.Weights.FailureRecovery, &p.Weights.RiskPenalty,
		&p.SampleCount, &p.LearningRate,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load weights for scope %q: %w", scope, err)
	}
	return &p, nil
}

// SaveWeights persists the weight profile for the given scope.
// It uses UPSERT semantics keyed by profile_key (which is the scope).
func (s *Store) SaveWeights(ctx context.Context, scope string, w *Weights, sampleCount int64, learningRate float64) error {
	nowMs := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rank_weight_profile
		  (profile_key, scope, updated_ms,
		   w_transition, w_frequency, w_success, w_prefix,
		   w_affinity, w_task, w_feedback,
		   w_project_type_affinity, w_failure_recovery, w_risk_penalty,
		   sample_count, learning_rate)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_key) DO UPDATE SET
		  updated_ms = excluded.updated_ms,
		  w_transition = excluded.w_transition,
		  w_frequency = excluded.w_frequency,
		  w_success = excluded.w_success,
		  w_prefix = excluded.w_prefix,
		  w_affinity = excluded.w_affinity,
		  w_task = excluded.w_task,
		  w_feedback = excluded.w_feedback,
		  w_project_type_affinity = excluded.w_project_type_affinity,
		  w_failure_recovery = excluded.w_failure_recovery,
		  w_risk_penalty = excluded.w_risk_penalty,
		  sample_count = excluded.sample_count,
		  learning_rate = excluded.learning_rate
	`, scope, scope, nowMs,
		w.Transition, w.Frequency, w.Success, w.Prefix,
		w.Affinity, w.Task, w.Feedback,
		w.ProjectTypeAffinity, w.FailureRecovery, w.RiskPenalty,
		sampleCount, learningRate,
	)
	if err != nil {
		return fmt.Errorf("save weights for scope %q: %w", scope, err)
	}
	return nil
}
