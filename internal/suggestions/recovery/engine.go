package recovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// RecoveryCandidate represents a suggested recovery command for a failure.
type RecoveryCandidate struct {
	// RecoveryTemplateID is the template ID of the recovery command.
	RecoveryTemplateID string

	// RecoveryCmdNorm is the normalized recovery command text.
	// For learned patterns this comes from the command_template table.
	// For bootstrap patterns this is extracted from the template ID.
	RecoveryCmdNorm string

	// SuccessRate is the observed success rate (0-1).
	SuccessRate float64

	// Count is the number of times this recovery was observed.
	Count int

	// Source indicates whether this is "learned" or "bootstrap".
	Source string

	// ExitCodeClass is the failure class this recovery applies to.
	ExitCodeClass string

	// Weight is the scoring weight from the database.
	Weight float64
}

// EngineConfig configures the recovery engine.
type EngineConfig struct {
	// MinSuccessRate is the minimum success rate threshold for surfacing candidates.
	// Candidates below this threshold are excluded. Default: 0.2
	MinSuccessRate float64

	// MinCount is the minimum observation count before a learned pattern is surfaced.
	// Bootstrap patterns ignore this threshold. Default: 2
	MinCount int

	// MaxCandidates is the maximum number of recovery candidates to return.
	// Default: 5
	MaxCandidates int

	// IncludeWildcard controls whether wildcard ("any command") bootstrap patterns
	// are included in results. Default: true
	IncludeWildcard bool
}

// DefaultEngineConfig returns the default engine configuration.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MinSuccessRate:  0.2,
		MinCount:        2,
		MaxCandidates:   5,
		IncludeWildcard: true,
	}
}

// Engine is the failure recovery pattern engine. It queries the failure_recovery
// table for recovery candidates when a previous command has failed.
type Engine struct {
	db         *sql.DB
	classifier *Classifier
	safety     *SafetyGate
	cfg        EngineConfig
}

// NewEngine creates a new recovery engine.
func NewEngine(db *sql.DB, classifier *Classifier, safety *SafetyGate, cfg EngineConfig) (*Engine, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if classifier == nil {
		classifier = NewClassifier(nil)
	}
	if safety == nil {
		safety = NewSafetyGate(DefaultSafetyConfig())
	}

	if cfg.MinSuccessRate <= 0 {
		cfg.MinSuccessRate = 0.2
	}
	if cfg.MinCount <= 0 {
		cfg.MinCount = 2
	}
	if cfg.MaxCandidates <= 0 {
		cfg.MaxCandidates = 5
	}

	return &Engine{
		db:         db,
		classifier: classifier,
		safety:     safety,
		cfg:        cfg,
	}, nil
}

// QueryRecoveries returns recovery candidates for a failed command.
//
// Parameters:
//   - failedTemplateID: the template ID of the command that failed
//   - exitCode: the exit code of the failed command
//   - scope: the scope to search (e.g., "global" or a repo key)
//
// The engine queries:
//  1. Exact match on (scope, failed_template_id, exit_code_class)
//  2. Wildcard match on (scope, "__wildcard__", exit_code_class) for bootstrap
//  3. Filters by safety gate
//  4. Sorts by (success_rate * weight) descending
//  5. Returns top N candidates
func (e *Engine) QueryRecoveries(ctx context.Context, failedTemplateID string, exitCode int, scope string) ([]RecoveryCandidate, error) {
	exitCodeClass := e.classifier.ClassifyToKey(exitCode)

	var candidates []RecoveryCandidate

	// Query 1: Exact match on failed_template_id
	exact, err := e.queryByFailedTemplate(ctx, scope, failedTemplateID, exitCodeClass)
	if err != nil {
		return nil, fmt.Errorf("query exact recoveries: %w", err)
	}
	candidates = append(candidates, exact...)

	// Query 2: Wildcard bootstrap patterns
	if e.cfg.IncludeWildcard {
		wildcard, err := e.queryByFailedTemplate(ctx, "global", "__wildcard__", exitCodeClass)
		if err != nil {
			return nil, fmt.Errorf("query wildcard recoveries: %w", err)
		}
		candidates = append(candidates, wildcard...)
	}

	// Resolve recovery command text for each candidate
	for i := range candidates {
		if err := e.resolveRecoveryCmdNorm(ctx, &candidates[i]); err != nil {
			// Non-fatal: skip candidates we can't resolve
			continue
		}
	}

	// Apply safety gate
	candidates = e.safety.FilterSafe(candidates)

	// Apply confidence thresholds
	candidates = e.filterByThresholds(candidates)

	// Deduplicate by recovery command
	candidates = deduplicateCandidates(candidates)

	// Sort by composite score (success_rate * weight) descending
	sort.Slice(candidates, func(i, j int) bool {
		scoreI := candidates[i].SuccessRate * candidates[i].Weight
		scoreJ := candidates[j].SuccessRate * candidates[j].Weight
		return scoreI > scoreJ
	})

	// Cap results
	if len(candidates) > e.cfg.MaxCandidates {
		candidates = candidates[:e.cfg.MaxCandidates]
	}

	return candidates, nil
}

// queryByFailedTemplate queries the failure_recovery table for a specific
// failed template and exit code class.
func (e *Engine) queryByFailedTemplate(ctx context.Context, scope, failedTemplateID, exitCodeClass string) ([]RecoveryCandidate, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT recovery_template_id, weight, count, success_rate, source, exit_code_class
		FROM failure_recovery
		WHERE scope = ? AND failed_template_id = ? AND exit_code_class = ?
		ORDER BY success_rate * weight DESC
		LIMIT ?
	`, scope, failedTemplateID, exitCodeClass, e.cfg.MaxCandidates*2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []RecoveryCandidate
	for rows.Next() {
		var c RecoveryCandidate
		if err := rows.Scan(&c.RecoveryTemplateID, &c.Weight, &c.Count, &c.SuccessRate, &c.Source, &c.ExitCodeClass); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}

	return candidates, rows.Err()
}

// resolveRecoveryCmdNorm fills in the RecoveryCmdNorm field of a candidate.
// For bootstrap patterns, the command is extracted from the template ID.
// For learned patterns, it is looked up in the command_template table.
func (e *Engine) resolveRecoveryCmdNorm(ctx context.Context, c *RecoveryCandidate) error {
	// Bootstrap patterns embed the command in the template ID
	if IsBootstrapTemplateID(c.RecoveryTemplateID) {
		c.RecoveryCmdNorm = ExtractBootstrapCmd(c.RecoveryTemplateID)
		return nil
	}

	// Look up learned patterns in command_template
	var cmdNorm string
	err := e.db.QueryRowContext(ctx, `
		SELECT cmd_norm FROM command_template WHERE template_id = ?
	`, c.RecoveryTemplateID).Scan(&cmdNorm)
	if errors.Is(err, sql.ErrNoRows) {
		// Template not found; use the template ID as a fallback
		c.RecoveryCmdNorm = c.RecoveryTemplateID
		return nil
	}
	if err != nil {
		return err
	}

	c.RecoveryCmdNorm = cmdNorm
	return nil
}

// filterByThresholds applies minimum success rate and count thresholds.
// Bootstrap patterns (source="bootstrap") are exempt from the count threshold.
func (e *Engine) filterByThresholds(candidates []RecoveryCandidate) []RecoveryCandidate {
	filtered := make([]RecoveryCandidate, 0, len(candidates))
	for _, c := range candidates {
		// Always filter by success rate
		if c.SuccessRate < e.cfg.MinSuccessRate {
			continue
		}

		// Bootstrap patterns skip count threshold
		if c.Source == "bootstrap" {
			filtered = append(filtered, c)
			continue
		}

		// Learned patterns must meet count threshold
		if c.Count >= e.cfg.MinCount {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// deduplicateCandidates removes duplicate recovery commands, keeping the one
// with the highest composite score (success_rate * weight).
func deduplicateCandidates(candidates []RecoveryCandidate) []RecoveryCandidate {
	seen := make(map[string]int) // cmd -> index in result
	result := make([]RecoveryCandidate, 0, len(candidates))

	for _, c := range candidates {
		if c.RecoveryCmdNorm == "" {
			continue
		}

		if idx, ok := seen[c.RecoveryCmdNorm]; ok {
			// Keep the one with higher composite score
			existing := result[idx]
			if c.SuccessRate*c.Weight > existing.SuccessRate*existing.Weight {
				result[idx] = c
			}
		} else {
			seen[c.RecoveryCmdNorm] = len(result)
			result = append(result, c)
		}
	}

	return result
}

// Classifier returns the engine's exit code classifier.
func (e *Engine) Classifier() *Classifier {
	return e.classifier
}

// Safety returns the engine's safety gate.
func (e *Engine) Safety() *SafetyGate {
	return e.safety
}

// RecordRecoveryEdge records a recovery edge in the failure_recovery table.
// This is the public API used by the write path when a command succeeds after
// a previous failure.
//
// This delegates to the same logic as writepath step 10, but provides a
// standalone API for use outside the write path transaction.
func (e *Engine) RecordRecoveryEdge(ctx context.Context, scope, failedTemplateID, recoveryTemplateID string, exitCode int, recoverySucceeded bool) error {
	exitCodeClass := e.classifier.ClassifyToKey(exitCode)

	var currentCount int
	var currentSuccessRate float64

	err := e.db.QueryRowContext(ctx, `
		SELECT count, success_rate
		FROM failure_recovery
		WHERE scope = ? AND failed_template_id = ? AND exit_code_class = ? AND recovery_template_id = ?
	`, scope, failedTemplateID, exitCodeClass, recoveryTemplateID).Scan(&currentCount, &currentSuccessRate)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("query existing recovery: %w", err)
	}

	var newCount int
	var newSuccessRate float64
	var newWeight float64

	if errors.Is(err, sql.ErrNoRows) {
		newCount = 1
		if recoverySucceeded {
			newSuccessRate = 1.0
		}
		newWeight = 1.0
	} else {
		newCount = currentCount + 1
		successSoFar := currentSuccessRate * float64(currentCount)
		if recoverySucceeded {
			successSoFar++
		}
		newSuccessRate = successSoFar / float64(newCount)
		newWeight = float64(newCount)
	}

	nowMs := currentTimeMs()

	_, err = e.db.ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'learned')
		ON CONFLICT(scope, failed_template_id, exit_code_class, recovery_template_id) DO UPDATE SET
			weight = ?,
			count = ?,
			success_rate = ?,
			last_seen_ms = ?
	`,
		scope, failedTemplateID, exitCodeClass, recoveryTemplateID,
		newWeight, newCount, newSuccessRate, nowMs,
		newWeight, newCount, newSuccessRate, nowMs,
	)
	if err != nil {
		return fmt.Errorf("upsert recovery edge: %w", err)
	}

	return nil
}

// currentTimeMs returns the current time in milliseconds.
// This is a package-level function to allow test overrides if needed.
var currentTimeMs = func() int64 {
	return currentTimeMsImpl()
}

func currentTimeMsImpl() int64 {
	return time.Now().UnixMilli()
}
