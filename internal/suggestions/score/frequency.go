// Package score provides scoring and aggregate tracking for the clai suggestions engine.
// It implements the decayed frequency scoring per spec Section 9.1.
package score

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// Default configuration values per spec Section 9.1.
const (
	// DefaultTauMs is the default decay time constant (7 days in milliseconds).
	DefaultTauMs = 7 * 24 * 60 * 60 * 1000

	// MinTauMs is the minimum allowed tau (1 day in milliseconds).
	// Values below this are clamped with a warning.
	MinTauMs = 1 * 24 * 60 * 60 * 1000

	// ScopeGlobal is the global scope identifier.
	ScopeGlobal = "global"
)

// FrequencyStore manages decayed frequency scores for commands.
type FrequencyStore struct {
	db         *sql.DB
	getStmt    *sql.Stmt
	upsertStmt *sql.Stmt
	tauMs      int64
}

// FrequencyOptions configures the frequency store.
type FrequencyOptions struct {
	// TauMs is the decay time constant in milliseconds.
	// Defaults to DefaultTauMs (7 days).
	TauMs int64
}

// DefaultFrequencyOptions returns the default options.
func DefaultFrequencyOptions() FrequencyOptions {
	return FrequencyOptions{
		TauMs: DefaultTauMs,
	}
}

// NewFrequencyStore creates a new frequency store.
func NewFrequencyStore(db *sql.DB, opts FrequencyOptions) (*FrequencyStore, error) {
	tauMs := opts.TauMs
	if tauMs <= 0 {
		tauMs = DefaultTauMs
	}
	if tauMs < MinTauMs {
		// Clamp to minimum and log warning (caller should handle logging)
		tauMs = MinTauMs
	}

	fs := &FrequencyStore{
		db:    db,
		tauMs: tauMs,
	}

	if err := fs.prepareStatements(); err != nil {
		return nil, err
	}

	return fs, nil
}

// prepareStatements prepares the SQL statements for the frequency store.
func (fs *FrequencyStore) prepareStatements() error {
	var err error

	// Get score for a command
	fs.getStmt, err = fs.db.Prepare(`
		SELECT score, last_ts FROM command_score
		WHERE scope = ? AND cmd_norm = ?
	`)
	if err != nil {
		return err
	}

	// Upsert score (insert or update)
	fs.upsertStmt, err = fs.db.Prepare(`
		INSERT INTO command_score (scope, cmd_norm, score, last_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scope, cmd_norm) DO UPDATE SET
			score = ?,
			last_ts = ?
	`)
	if err != nil {
		fs.getStmt.Close()
		return err
	}

	return nil
}

// Close releases resources held by the frequency store.
func (fs *FrequencyStore) Close() error {
	if fs.getStmt != nil {
		fs.getStmt.Close()
	}
	if fs.upsertStmt != nil {
		fs.upsertStmt.Close()
	}
	return nil
}

// Update updates the score for a command in the given scope.
// The decay formula is: score = score * exp(-(now - last_ts) / tau_ms) + 1.0
// This is called for both global and repo-scoped updates.
func (fs *FrequencyStore) Update(ctx context.Context, scope, cmdNorm string, nowMs int64) error {
	// Get current score and timestamp
	var currentScore float64
	var lastTS int64

	err := fs.getStmt.QueryRowContext(ctx, scope, cmdNorm).Scan(&currentScore, &lastTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Calculate new score using decay formula
	var newScore float64
	if errors.Is(err, sql.ErrNoRows) {
		// First occurrence
		newScore = 1.0
	} else {
		// Apply decay: d = exp(-(now - last_ts) / tau_ms)
		elapsed := float64(nowMs - lastTS)
		decay := math.Exp(-elapsed / float64(fs.tauMs))
		newScore = currentScore*decay + 1.0
	}

	// Upsert the new score
	_, err = fs.upsertStmt.ExecContext(ctx, scope, cmdNorm, newScore, nowMs, newScore, nowMs)
	return err
}

// UpdateBoth updates both global and repo-scoped scores in a single transaction.
func (fs *FrequencyStore) UpdateBoth(ctx context.Context, cmdNorm, repoKey string, nowMs int64) error {
	tx, err := fs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on error

	// Update global scope
	if err := fs.updateInTx(ctx, tx, ScopeGlobal, cmdNorm, nowMs); err != nil {
		return err
	}

	// Update repo scope if provided
	if repoKey != "" {
		if err := fs.updateInTx(ctx, tx, repoKey, cmdNorm, nowMs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpdateAll updates global, repo-scoped, and directory-scoped scores in a single transaction.
// This extends UpdateBoth by also recording the directory scope aggregate when dirScopeKey is non-empty.
func (fs *FrequencyStore) UpdateAll(ctx context.Context, cmdNorm, repoKey, dirScopeKey string, nowMs int64) error {
	tx, err := fs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on error

	// Update global scope
	if err := fs.updateInTx(ctx, tx, ScopeGlobal, cmdNorm, nowMs); err != nil {
		return err
	}

	// Update repo scope if provided
	if repoKey != "" {
		if err := fs.updateInTx(ctx, tx, repoKey, cmdNorm, nowMs); err != nil {
			return err
		}
	}

	// Update directory scope if provided
	if dirScopeKey != "" {
		if err := fs.updateInTx(ctx, tx, dirScopeKey, cmdNorm, nowMs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// updateInTx performs the score update within a transaction.
func (fs *FrequencyStore) updateInTx(ctx context.Context, tx *sql.Tx, scope, cmdNorm string, nowMs int64) error {
	// Get current score and timestamp
	var currentScore float64
	var lastTS int64

	err := tx.QueryRowContext(ctx, `
		SELECT score, last_ts FROM command_score
		WHERE scope = ? AND cmd_norm = ?
	`, scope, cmdNorm).Scan(&currentScore, &lastTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Calculate new score using decay formula
	var newScore float64
	if errors.Is(err, sql.ErrNoRows) {
		// First occurrence
		newScore = 1.0
	} else {
		// Apply decay
		elapsed := float64(nowMs - lastTS)
		decay := math.Exp(-elapsed / float64(fs.tauMs))
		newScore = currentScore*decay + 1.0
	}

	// Upsert the new score
	_, err = tx.ExecContext(ctx, `
		INSERT INTO command_score (scope, cmd_norm, score, last_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scope, cmd_norm) DO UPDATE SET
			score = ?,
			last_ts = ?
	`, scope, cmdNorm, newScore, nowMs, newScore, nowMs)

	return err
}

// GetScore retrieves the current score for a command in the given scope.
// The returned score is the decayed score at the current time.
func (fs *FrequencyStore) GetScore(ctx context.Context, scope, cmdNorm string) (float64, error) {
	return fs.GetScoreAt(ctx, scope, cmdNorm, time.Now().UnixMilli())
}

// GetScoreAt retrieves the decayed score at a specific time.
func (fs *FrequencyStore) GetScoreAt(ctx context.Context, scope, cmdNorm string, atMs int64) (float64, error) {
	var score float64
	var lastTS int64

	err := fs.getStmt.QueryRowContext(ctx, scope, cmdNorm).Scan(&score, &lastTS)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// Apply decay to get current score
	elapsed := float64(atMs - lastTS)
	decay := math.Exp(-elapsed / float64(fs.tauMs))
	return score * decay, nil
}

// GetTopCommands retrieves the top N commands by decayed score in the given scope.
func (fs *FrequencyStore) GetTopCommands(ctx context.Context, scope string, limit int) ([]ScoredCommand, error) {
	return fs.GetTopCommandsAt(ctx, scope, limit, time.Now().UnixMilli())
}

// ScoredCommand represents a command with its score.
type ScoredCommand struct {
	CmdNorm  string
	Score    float64
	LastTSMs int64
}

// GetTopCommandsAt retrieves the top N commands by decayed score at a specific time.
func (fs *FrequencyStore) GetTopCommandsAt(ctx context.Context, scope string, limit int, atMs int64) ([]ScoredCommand, error) {
	// We need to compute decayed scores for all commands and sort
	// For performance, we could use a pre-computed view or just trust the stored order
	// For now, we'll retrieve and compute in memory

	rows, err := fs.db.QueryContext(ctx, `
		SELECT cmd_norm, score, last_ts FROM command_score
		WHERE scope = ?
		ORDER BY score DESC
		LIMIT ?
	`, scope, limit*2) // Fetch more to account for decay reordering
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ScoredCommand
	for rows.Next() {
		var cmdNorm string
		var score float64
		var lastTS int64
		if err := rows.Scan(&cmdNorm, &score, &lastTS); err != nil {
			return nil, err
		}

		// Apply decay
		elapsed := float64(atMs - lastTS)
		decay := math.Exp(-elapsed / float64(fs.tauMs))
		decayedScore := score * decay

		results = append(results, ScoredCommand{
			CmdNorm:  cmdNorm,
			Score:    decayedScore,
			LastTSMs: lastTS,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by decayed score (highest first)
	sortByScore(results)

	// Limit to requested count
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// sortByScore sorts scored commands by score descending.
func sortByScore(cmds []ScoredCommand) {
	// Simple insertion sort for small arrays
	for i := 1; i < len(cmds); i++ {
		for j := i; j > 0 && cmds[j].Score > cmds[j-1].Score; j-- {
			cmds[j], cmds[j-1] = cmds[j-1], cmds[j]
		}
	}
}

// TauMs returns the configured tau value.
func (fs *FrequencyStore) TauMs() int64 {
	return fs.tauMs
}
