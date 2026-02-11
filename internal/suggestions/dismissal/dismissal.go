// Package dismissal implements persistent dismissal learning for the suggestions engine.
//
// It manages a state machine with four states:
//
//	NONE      -> No dismissal history for this pattern
//	TEMPORARY -> Dismissed 1-2 times, suppressed for current session only
//	LEARNED   -> Dismissed >= threshold times, suppressed across sessions
//	PERMANENT -> User explicitly marked as "never show", filtered before ranking
//
// Transitions:
//   - Dismiss feedback: NONE -> TEMPORARY -> LEARNED (when count >= threshold)
//   - Accept feedback: any state -> NONE (acceptance resets suppression)
//   - Never action: any state -> PERMANENT
//   - Unblock action: PERMANENT -> NONE
package dismissal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// State represents a dismissal suppression level.
type State string

const (
	StateNone      State = "none"
	StateTemporary State = "temporary"
	StateLearned   State = "learned"
	StatePermanent State = "permanent"
)

var errRequiredDismissalFields = errors.New("scope, context_template_id, and dismissed_template_id are required")

// IsValid returns true if s is a recognized dismissal state.
func (s State) IsValid() bool {
	switch s {
	case StateNone, StateTemporary, StateLearned, StatePermanent:
		return true
	}
	return false
}

// Candidate represents a suggestion candidate that can be filtered.
type Candidate struct {
	TemplateID string
}

// Config holds dismissal learning configuration.
type Config struct {
	// LearnedThreshold is the number of dismissals required to transition
	// from TEMPORARY to LEARNED. Default: 3.
	LearnedThreshold int
}

// DefaultConfig returns the default dismissal configuration.
func DefaultConfig() Config {
	return Config{
		LearnedThreshold: 3,
	}
}

// Store manages persistent dismissal pattern storage and state transitions.
type Store struct {
	db     *sql.DB
	cfg    Config
	logger *slog.Logger
}

// NewStore creates a new dismissal store.
func NewStore(db *sql.DB, cfg Config, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.LearnedThreshold < 1 {
		cfg.LearnedThreshold = DefaultConfig().LearnedThreshold
	}
	return &Store{db: db, cfg: cfg, logger: logger}
}

// PatternRecord represents a row in the dismissal_pattern table.
type PatternRecord struct {
	Scope               string
	ContextTemplateID   string
	DismissedTemplateID string
	DismissalCount      int
	LastDismissedMs     int64
	SuppressionLevel    State
}

// RecordDismissal increments the dismissal count for the given pattern and
// updates the state according to the state machine.
//
// State transitions on dismiss:
//   - NONE -> TEMPORARY (count becomes 1)
//   - TEMPORARY -> TEMPORARY (count < threshold)
//   - TEMPORARY -> LEARNED (count >= threshold)
//   - LEARNED stays LEARNED
//   - PERMANENT stays PERMANENT (dismiss does not downgrade permanent)
func (s *Store) RecordDismissal(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string, nowMs int64) error {
	if scope == "" || contextTemplateID == "" || dismissedTemplateID == "" {
		return errRequiredDismissalFields
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}

	// Use upsert: insert new record or update existing one.
	// On conflict, increment count and potentially update state.
	//
	// The state logic:
	// - If current state is 'permanent', keep it permanent.
	// - Otherwise, if new count >= threshold, set to 'learned'.
	// - Otherwise, set to 'temporary'.
	//
	// The initial INSERT also checks the threshold: if threshold <= 1,
	// the first dismissal goes directly to 'learned'.
	initialState := StateTemporary
	if s.cfg.LearnedThreshold <= 1 {
		initialState = StateLearned
	}

	query := `
		INSERT INTO dismissal_pattern (scope, context_template_id, dismissed_template_id, dismissal_count, last_dismissed_ms, suppression_level)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(scope, context_template_id, dismissed_template_id) DO UPDATE SET
			dismissal_count = CASE
				WHEN suppression_level = 'permanent' THEN dismissal_count
				ELSE dismissal_count + 1
			END,
			last_dismissed_ms = excluded.last_dismissed_ms,
			suppression_level = CASE
				WHEN suppression_level = 'permanent' THEN 'permanent'
				WHEN dismissal_count + 1 >= ? THEN 'learned'
				ELSE 'temporary'
			END
	`

	_, err := s.db.ExecContext(ctx, query, scope, contextTemplateID, dismissedTemplateID, nowMs, string(initialState), s.cfg.LearnedThreshold)
	if err != nil {
		return fmt.Errorf("failed to record dismissal: %w", err)
	}

	s.logger.Debug("recorded dismissal",
		"scope", scope,
		"context_template_id", contextTemplateID,
		"dismissed_template_id", dismissedTemplateID,
	)
	return nil
}

// RecordAcceptance resets the dismissal state for the given pattern to NONE.
// Acceptance means the user found the suggestion useful, so prior dismissals
// are forgiven. The record is deleted from the table.
func (s *Store) RecordAcceptance(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string) error {
	if scope == "" || contextTemplateID == "" || dismissedTemplateID == "" {
		return errRequiredDismissalFields
	}

	_, err := s.db.ExecContext(ctx,
		"DELETE FROM dismissal_pattern WHERE scope = ? AND context_template_id = ? AND dismissed_template_id = ?",
		scope, contextTemplateID, dismissedTemplateID)
	if err != nil {
		return fmt.Errorf("failed to record acceptance: %w", err)
	}

	s.logger.Debug("recorded acceptance (reset to NONE)",
		"scope", scope,
		"context_template_id", contextTemplateID,
		"dismissed_template_id", dismissedTemplateID,
	)
	return nil
}

// RecordNever sets the pattern to PERMANENT suppression.
// The user explicitly chose "never show this suggestion".
func (s *Store) RecordNever(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string, nowMs int64) error {
	if scope == "" || contextTemplateID == "" || dismissedTemplateID == "" {
		return errRequiredDismissalFields
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}

	query := `
		INSERT INTO dismissal_pattern (scope, context_template_id, dismissed_template_id, dismissal_count, last_dismissed_ms, suppression_level)
		VALUES (?, ?, ?, 1, ?, 'permanent')
		ON CONFLICT(scope, context_template_id, dismissed_template_id) DO UPDATE SET
			last_dismissed_ms = excluded.last_dismissed_ms,
			suppression_level = 'permanent'
	`

	_, err := s.db.ExecContext(ctx, query, scope, contextTemplateID, dismissedTemplateID, nowMs)
	if err != nil {
		return fmt.Errorf("failed to record never: %w", err)
	}

	s.logger.Debug("recorded never (set to PERMANENT)",
		"scope", scope,
		"context_template_id", contextTemplateID,
		"dismissed_template_id", dismissedTemplateID,
	)
	return nil
}

// RecordUnblock resets a PERMANENT pattern back to NONE.
// Only affects patterns that are currently PERMANENT.
func (s *Store) RecordUnblock(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string) error {
	if scope == "" || contextTemplateID == "" || dismissedTemplateID == "" {
		return errRequiredDismissalFields
	}

	_, err := s.db.ExecContext(ctx,
		"DELETE FROM dismissal_pattern WHERE scope = ? AND context_template_id = ? AND dismissed_template_id = ? AND suppression_level = 'permanent'",
		scope, contextTemplateID, dismissedTemplateID)
	if err != nil {
		return fmt.Errorf("failed to record unblock: %w", err)
	}

	s.logger.Debug("recorded unblock (reset PERMANENT to NONE)",
		"scope", scope,
		"context_template_id", contextTemplateID,
		"dismissed_template_id", dismissedTemplateID,
	)
	return nil
}

// GetState returns the current dismissal state for a given pattern.
// If no record exists, returns StateNone.
func (s *Store) GetState(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string) (State, error) {
	if scope == "" || contextTemplateID == "" || dismissedTemplateID == "" {
		return StateNone, nil
	}

	var level string
	err := s.db.QueryRowContext(ctx,
		"SELECT suppression_level FROM dismissal_pattern WHERE scope = ? AND context_template_id = ? AND dismissed_template_id = ?",
		scope, contextTemplateID, dismissedTemplateID).Scan(&level)

	if err == sql.ErrNoRows {
		return StateNone, nil
	}
	if err != nil {
		return StateNone, fmt.Errorf("failed to query dismissal state: %w", err)
	}

	state := State(level)
	if !state.IsValid() {
		return StateNone, fmt.Errorf("invalid suppression_level in database: %q", level)
	}
	return state, nil
}

// GetRecord returns the full dismissal pattern record, or nil if not found.
func (s *Store) GetRecord(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string) (*PatternRecord, error) {
	if scope == "" || contextTemplateID == "" || dismissedTemplateID == "" {
		return nil, nil
	}

	var rec PatternRecord
	var level string
	err := s.db.QueryRowContext(ctx,
		"SELECT scope, context_template_id, dismissed_template_id, dismissal_count, last_dismissed_ms, suppression_level FROM dismissal_pattern WHERE scope = ? AND context_template_id = ? AND dismissed_template_id = ?",
		scope, contextTemplateID, dismissedTemplateID).Scan(
		&rec.Scope, &rec.ContextTemplateID, &rec.DismissedTemplateID,
		&rec.DismissalCount, &rec.LastDismissedMs, &level)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query dismissal record: %w", err)
	}

	rec.SuppressionLevel = State(level)
	return &rec, nil
}

// FilterCandidates removes candidates that are in LEARNED or PERMANENT state
// for the given context. This should be called before ranking.
//
// Parameters:
//   - scope: the scope (e.g., "global" or repo_key)
//   - contextTemplateID: the current command context (template_id of previous command)
//   - candidates: the list of suggestion candidates to filter
//
// Returns the filtered list with suppressed candidates removed.
func (s *Store) FilterCandidates(ctx context.Context, scope, contextTemplateID string, candidates []Candidate) ([]Candidate, error) {
	if len(candidates) == 0 || scope == "" || contextTemplateID == "" {
		return candidates, nil
	}

	// Query all suppressed patterns for this context in one go.
	rows, err := s.db.QueryContext(ctx,
		"SELECT dismissed_template_id FROM dismissal_pattern WHERE scope = ? AND context_template_id = ? AND suppression_level IN ('learned', 'permanent')",
		scope, contextTemplateID)
	if err != nil {
		return nil, fmt.Errorf("failed to query suppressed patterns: %w", err)
	}
	defer rows.Close()

	suppressed := make(map[string]bool)
	for rows.Next() {
		var templateID string
		if err := rows.Scan(&templateID); err != nil {
			return nil, fmt.Errorf("failed to scan suppressed template: %w", err)
		}
		suppressed[templateID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate suppressed patterns: %w", err)
	}

	if len(suppressed) == 0 {
		return candidates, nil
	}

	// Filter out suppressed candidates.
	filtered := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if !suppressed[c.TemplateID] {
			filtered = append(filtered, c)
		}
	}

	s.logger.Debug("filtered candidates",
		"scope", scope,
		"context_template_id", contextTemplateID,
		"input_count", len(candidates),
		"output_count", len(filtered),
		"suppressed_count", len(candidates)-len(filtered),
	)
	return filtered, nil
}

// IsSuppressed checks whether a specific pattern is currently suppressed
// (LEARNED or PERMANENT). This is a convenience method for checking
// individual candidates.
func (s *Store) IsSuppressed(ctx context.Context, scope, contextTemplateID, dismissedTemplateID string) (bool, error) {
	state, err := s.GetState(ctx, scope, contextTemplateID, dismissedTemplateID)
	if err != nil {
		return false, err
	}
	return state == StateLearned || state == StatePermanent, nil
}
