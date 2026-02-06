// Package score provides scoring and aggregate tracking for the clai suggestions engine.
// TransitionStore implements Markov bigram transition tracking per spec Section 9.2.
package score

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TransitionStore manages Markov bigram transitions between commands.
// It tracks the probability of command sequences: P(next_cmd | prev_cmd).
type TransitionStore struct {
	db *sql.DB

	// Prepared statements
	getStmt        *sql.Stmt
	upsertStmt     *sql.Stmt
	getPrevStmt    *sql.Stmt
	getTopNextStmt *sql.Stmt
}

// TransitionOptions configures the transition store.
type TransitionOptions struct {
	// FallbackWindowMs is the maximum time window to look back for a previous command
	// when no session match is found. Defaults to 5 minutes.
	FallbackWindowMs int64
}

// DefaultTransitionOptions returns the default options.
func DefaultTransitionOptions() TransitionOptions {
	return TransitionOptions{
		FallbackWindowMs: 5 * 60 * 1000, // 5 minutes
	}
}

// NewTransitionStore creates a new transition store.
func NewTransitionStore(db *sql.DB) (*TransitionStore, error) {
	ts := &TransitionStore{
		db: db,
	}

	if err := ts.prepareStatements(); err != nil {
		return nil, err
	}

	return ts, nil
}

// prepareStatements prepares the SQL statements for the transition store.
func (ts *TransitionStore) prepareStatements() error {
	var err error

	// Get transition count for a specific pair
	ts.getStmt, err = ts.db.Prepare(`
		SELECT count, last_ts FROM transition
		WHERE scope = ? AND prev_norm = ? AND next_norm = ?
	`)
	if err != nil {
		return err
	}

	// Upsert transition (insert or update count)
	ts.upsertStmt, err = ts.db.Prepare(`
		INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(scope, prev_norm, next_norm) DO UPDATE SET
			count = count + 1,
			last_ts = ?
	`)
	if err != nil {
		ts.getStmt.Close()
		return err
	}

	// Get previous command from command_event by session_id
	ts.getPrevStmt, err = ts.db.Prepare(`
		SELECT cmd_norm FROM command_event
		WHERE session_id = ? AND ts < ?
		ORDER BY ts DESC
		LIMIT 1
	`)
	if err != nil {
		ts.getStmt.Close()
		ts.upsertStmt.Close()
		return err
	}

	// Get top next commands for a given prev_norm
	ts.getTopNextStmt, err = ts.db.Prepare(`
		SELECT next_norm, count FROM transition
		WHERE scope = ? AND prev_norm = ?
		ORDER BY count DESC
		LIMIT ?
	`)
	if err != nil {
		ts.getStmt.Close()
		ts.upsertStmt.Close()
		ts.getPrevStmt.Close()
		return err
	}

	return nil
}

// Close releases resources held by the transition store.
func (ts *TransitionStore) Close() error {
	if ts.getStmt != nil {
		ts.getStmt.Close()
	}
	if ts.upsertStmt != nil {
		ts.upsertStmt.Close()
	}
	if ts.getPrevStmt != nil {
		ts.getPrevStmt.Close()
	}
	if ts.getTopNextStmt != nil {
		ts.getTopNextStmt.Close()
	}
	return nil
}

// RecordTransition records a transition from prevNorm to nextNorm in the given scope.
// This increments the transition count and updates the last timestamp.
func (ts *TransitionStore) RecordTransition(ctx context.Context, scope, prevNorm, nextNorm string, nowMs int64) error {
	_, err := ts.upsertStmt.ExecContext(ctx, scope, prevNorm, nextNorm, nowMs, nowMs)
	return err
}

// RecordTransitionBoth records a transition in both global and repo scopes.
func (ts *TransitionStore) RecordTransitionBoth(ctx context.Context, prevNorm, nextNorm, repoKey string, nowMs int64) error {
	tx, err := ts.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Update global scope
	_, err = tx.ExecContext(ctx, `
		INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(scope, prev_norm, next_norm) DO UPDATE SET
			count = count + 1,
			last_ts = ?
	`, ScopeGlobal, prevNorm, nextNorm, nowMs, nowMs)
	if err != nil {
		return err
	}

	// Update repo scope if provided
	if repoKey != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
			VALUES (?, ?, ?, 1, ?)
			ON CONFLICT(scope, prev_norm, next_norm) DO UPDATE SET
				count = count + 1,
				last_ts = ?
		`, repoKey, prevNorm, nextNorm, nowMs, nowMs)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// RecordTransitionAll records a transition in global, repo, and directory scopes.
// This extends RecordTransitionBoth by also recording the directory scope aggregate
// when dirScopeKey is non-empty.
func (ts *TransitionStore) RecordTransitionAll(ctx context.Context, prevNorm, nextNorm, repoKey, dirScopeKey string, nowMs int64) error {
	tx, err := ts.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Update global scope
	_, err = tx.ExecContext(ctx, `
		INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(scope, prev_norm, next_norm) DO UPDATE SET
			count = count + 1,
			last_ts = ?
	`, ScopeGlobal, prevNorm, nextNorm, nowMs, nowMs)
	if err != nil {
		return err
	}

	// Update repo scope if provided
	if repoKey != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
			VALUES (?, ?, ?, 1, ?)
			ON CONFLICT(scope, prev_norm, next_norm) DO UPDATE SET
				count = count + 1,
				last_ts = ?
		`, repoKey, prevNorm, nextNorm, nowMs, nowMs)
		if err != nil {
			return err
		}
	}

	// Update directory scope if provided
	if dirScopeKey != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
			VALUES (?, ?, ?, 1, ?)
			ON CONFLICT(scope, prev_norm, next_norm) DO UPDATE SET
				count = count + 1,
				last_ts = ?
		`, dirScopeKey, prevNorm, nextNorm, nowMs, nowMs)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetPreviousCommand retrieves the previous command for a session.
// Per spec Section 9.2, this looks up the previous cmd_norm in the same session_id.
func (ts *TransitionStore) GetPreviousCommand(ctx context.Context, sessionID string, beforeTs int64) (string, error) {
	var prevNorm string
	err := ts.getPrevStmt.QueryRowContext(ctx, sessionID, beforeTs).Scan(&prevNorm)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return prevNorm, nil
}

// GetPreviousCommandWithFallback retrieves the previous command, falling back to
// repo-based lookup if no session match is found.
// Per spec Section 9.2: "previous cmd_norm in same session_id (fallback: same repo within last N minutes)"
func (ts *TransitionStore) GetPreviousCommandWithFallback(ctx context.Context, sessionID, repoKey string, beforeTs, fallbackWindowMs int64) (string, error) {
	// First try session-based lookup
	prev, err := ts.GetPreviousCommand(ctx, sessionID, beforeTs)
	if err != nil {
		return "", err
	}
	if prev != "" {
		return prev, nil
	}

	// Fallback to repo-based lookup within time window
	if repoKey == "" {
		return "", nil
	}

	minTs := beforeTs - fallbackWindowMs
	var prevNorm string
	err = ts.db.QueryRowContext(ctx, `
		SELECT cmd_norm FROM command_event
		WHERE repo_key = ? AND ts >= ? AND ts < ?
		ORDER BY ts DESC
		LIMIT 1
	`, repoKey, minTs, beforeTs).Scan(&prevNorm)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return prevNorm, nil
}

// Transition represents a command transition with its count.
type Transition struct {
	PrevNorm string
	NextNorm string
	Count    int
}

// GetTopNextCommands retrieves the most frequent commands that follow a given command.
func (ts *TransitionStore) GetTopNextCommands(ctx context.Context, scope, prevNorm string, limit int) ([]Transition, error) {
	rows, err := ts.getTopNextStmt.QueryContext(ctx, scope, prevNorm, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Transition
	for rows.Next() {
		var t Transition
		t.PrevNorm = prevNorm
		if err := rows.Scan(&t.NextNorm, &t.Count); err != nil {
			return nil, err
		}
		results = append(results, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// GetTransitionCount retrieves the count for a specific transition.
func (ts *TransitionStore) GetTransitionCount(ctx context.Context, scope, prevNorm, nextNorm string) (int, error) {
	var count int
	var lastTs int64
	err := ts.getStmt.QueryRowContext(ctx, scope, prevNorm, nextNorm).Scan(&count, &lastTs)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Now returns the current time in milliseconds.
// This is a convenience function for callers.
func Now() int64 {
	return time.Now().UnixMilli()
}
