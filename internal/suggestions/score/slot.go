// Package score provides scoring and aggregate tracking for the clai suggestions engine.
// SlotStore implements slot value histogram tracking per spec Section 9.3.
package score

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// Slot histogram configuration defaults per spec Section 9.3.3.
const (
	// DefaultTopK is the default number of top values to keep per slot.
	DefaultTopK = 20

	// MinTopK is the minimum allowed top-K value.
	MinTopK = 1

	// MaxTopK is the maximum allowed top-K value.
	MaxTopK = 100

	// DefaultSlotTauMs uses the same decay time constant as frequency scoring.
	DefaultSlotTauMs = DefaultTauMs
)

// SlotStore manages slot value histograms for argument prediction.
// It tracks the most frequent values used for each slot position in normalized commands.
type SlotStore struct {
	db         *sql.DB
	getStmt    *sql.Stmt
	upsertStmt *sql.Stmt
	topKStmt   *sql.Stmt
	tauMs      int64
	topK       int
}

// SlotOptions configures the slot store.
type SlotOptions struct {
	// TauMs is the decay time constant in milliseconds.
	// Defaults to DefaultSlotTauMs (7 days).
	TauMs int64

	// TopK is the number of top values to keep per slot.
	// Defaults to DefaultTopK (20).
	TopK int
}

// DefaultSlotOptions returns the default options.
func DefaultSlotOptions() SlotOptions {
	return SlotOptions{
		TauMs: DefaultSlotTauMs,
		TopK:  DefaultTopK,
	}
}

// NewSlotStore creates a new slot store.
func NewSlotStore(db *sql.DB, opts SlotOptions) (*SlotStore, error) {
	tauMs := opts.TauMs
	if tauMs <= 0 {
		tauMs = DefaultSlotTauMs
	}
	if tauMs < MinTauMs {
		tauMs = MinTauMs
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = DefaultTopK
	}
	if topK < MinTopK {
		topK = MinTopK
	}
	if topK > MaxTopK {
		topK = MaxTopK
	}

	ss := &SlotStore{
		db:    db,
		tauMs: tauMs,
		topK:  topK,
	}

	if err := ss.prepareStatements(); err != nil {
		return nil, err
	}

	return ss, nil
}

// prepareStatements prepares the SQL statements for the slot store.
func (ss *SlotStore) prepareStatements() error {
	var err error

	// Get slot value count
	ss.getStmt, err = ss.db.Prepare(`
		SELECT count, last_ts FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ? AND value = ?
	`)
	if err != nil {
		return err
	}

	// Upsert slot value
	ss.upsertStmt, err = ss.db.Prepare(`
		INSERT INTO slot_value (scope, cmd_norm, slot_idx, value, count, last_ts)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, cmd_norm, slot_idx, value) DO UPDATE SET
			count = ?,
			last_ts = ?
	`)
	if err != nil {
		ss.getStmt.Close()
		return err
	}

	// Get top-K values for a slot
	ss.topKStmt, err = ss.db.Prepare(`
		SELECT value, count, last_ts FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ?
		ORDER BY count DESC
		LIMIT ?
	`)
	if err != nil {
		ss.getStmt.Close()
		ss.upsertStmt.Close()
		return err
	}

	return nil
}

// Close releases resources held by the slot store.
func (ss *SlotStore) Close() error {
	if ss.getStmt != nil {
		ss.getStmt.Close()
	}
	if ss.upsertStmt != nil {
		ss.upsertStmt.Close()
	}
	if ss.topKStmt != nil {
		ss.topKStmt.Close()
	}
	return nil
}

// SlotValue represents a value with its decayed count.
type SlotValue struct {
	Value  string
	Count  float64
	LastTS int64
}

// Update updates the count for a slot value in the given scope.
// The decay formula is the same as frequency scoring:
// count = count * exp(-(now - last_ts) / tau_ms) + 1.0
func (ss *SlotStore) Update(ctx context.Context, scope, cmdNorm string, slotIdx int, value string, nowMs int64) error {
	// Get current count and timestamp
	var currentCount float64
	var lastTS int64

	err := ss.getStmt.QueryRowContext(ctx, scope, cmdNorm, slotIdx, value).Scan(&currentCount, &lastTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Calculate new count using decay formula
	var newCount float64
	if errors.Is(err, sql.ErrNoRows) {
		// First occurrence
		newCount = 1.0
	} else {
		// Apply decay
		elapsed := float64(nowMs - lastTS)
		decay := math.Exp(-elapsed / float64(ss.tauMs))
		newCount = currentCount*decay + 1.0
	}

	// Upsert the new count
	_, err = ss.upsertStmt.ExecContext(ctx, scope, cmdNorm, slotIdx, value, newCount, nowMs, newCount, nowMs)
	return err
}

// UpdateBoth updates both global and repo-scoped slot values.
func (ss *SlotStore) UpdateBoth(ctx context.Context, cmdNorm string, slotIdx int, value, repoKey string, nowMs int64) error {
	tx, err := ss.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on error

	// Update global scope
	if err := ss.updateInTx(ctx, tx, ScopeGlobal, cmdNorm, slotIdx, value, nowMs); err != nil {
		return err
	}

	// Update repo scope if provided
	if repoKey != "" {
		if err := ss.updateInTx(ctx, tx, repoKey, cmdNorm, slotIdx, value, nowMs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// updateInTx performs the slot value update within a transaction.
func (ss *SlotStore) updateInTx(ctx context.Context, tx *sql.Tx, scope, cmdNorm string, slotIdx int, value string, nowMs int64) error {
	// Get current count and timestamp
	var currentCount float64
	var lastTS int64

	err := tx.QueryRowContext(ctx, `
		SELECT count, last_ts FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ? AND value = ?
	`, scope, cmdNorm, slotIdx, value).Scan(&currentCount, &lastTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Calculate new count using decay formula
	var newCount float64
	if errors.Is(err, sql.ErrNoRows) {
		newCount = 1.0
	} else {
		elapsed := float64(nowMs - lastTS)
		decay := math.Exp(-elapsed / float64(ss.tauMs))
		newCount = currentCount*decay + 1.0
	}

	// Upsert
	_, err = tx.ExecContext(ctx, `
		INSERT INTO slot_value (scope, cmd_norm, slot_idx, value, count, last_ts)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, cmd_norm, slot_idx, value) DO UPDATE SET
			count = ?,
			last_ts = ?
	`, scope, cmdNorm, slotIdx, value, newCount, nowMs, newCount, nowMs)

	return err
}

// GetTopValues retrieves the top values for a slot, decayed to the current time.
func (ss *SlotStore) GetTopValues(ctx context.Context, scope, cmdNorm string, slotIdx, limit int) ([]SlotValue, error) {
	return ss.GetTopValuesAt(ctx, scope, cmdNorm, slotIdx, limit, time.Now().UnixMilli())
}

// GetTopValuesAt retrieves the top values for a slot, decayed to a specific time.
func (ss *SlotStore) GetTopValuesAt(ctx context.Context, scope, cmdNorm string, slotIdx, limit int, atMs int64) ([]SlotValue, error) {
	if limit <= 0 {
		limit = ss.topK
	}

	rows, err := ss.topKStmt.QueryContext(ctx, scope, cmdNorm, slotIdx, limit*2) // Fetch more for decay reordering
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SlotValue
	for rows.Next() {
		var sv SlotValue
		if err := rows.Scan(&sv.Value, &sv.Count, &sv.LastTS); err != nil {
			return nil, err
		}

		// Apply decay to get current count
		elapsed := float64(atMs - sv.LastTS)
		decay := math.Exp(-elapsed / float64(ss.tauMs))
		sv.Count *= decay

		results = append(results, sv)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by decayed count
	sortSlotValues(results)

	// Limit to requested count
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// sortSlotValues sorts slot values by count descending.
func sortSlotValues(values []SlotValue) {
	// Simple insertion sort for small arrays
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j].Count > values[j-1].Count; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// GetBestValue returns the best value for a slot, preferring repo scope over global.
// Per spec Section 9.3.4: repo-scoped top value > global-scoped top value > last-used fallback.
// Returns empty string if no suitable value found.
func (ss *SlotStore) GetBestValue(ctx context.Context, cmdNorm string, slotIdx int, repoKey string) (string, error) {
	return ss.GetBestValueAt(ctx, cmdNorm, slotIdx, repoKey, time.Now().UnixMilli())
}

// GetBestValueAt returns the best value for a slot at a specific time.
func (ss *SlotStore) GetBestValueAt(ctx context.Context, cmdNorm string, slotIdx int, repoKey string, atMs int64) (string, error) {
	// Try repo scope first if provided
	if repoKey != "" {
		values, err := ss.GetTopValuesAt(ctx, repoKey, cmdNorm, slotIdx, 2, atMs)
		if err != nil {
			return "", err
		}
		if len(values) > 0 && ss.isConfidentValue(values) {
			return values[0].Value, nil
		}
	}

	// Fall back to global scope
	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, cmdNorm, slotIdx, 2, atMs)
	if err != nil {
		return "", err
	}
	if len(values) > 0 && ss.isConfidentValue(values) {
		return values[0].Value, nil
	}

	return "", nil
}

// isConfidentValue checks if we have high confidence in the top value.
// Per spec Section 9.3.4: "only fill when confidence is high (e.g., top value has ≥2x count of second)"
func (ss *SlotStore) isConfidentValue(values []SlotValue) bool {
	if len(values) == 0 {
		return false
	}
	if len(values) == 1 {
		// Single value with any count is confident enough
		return values[0].Count > 0
	}
	// Top value must be at least 2x the second value
	return values[0].Count >= 2*values[1].Count
}

// PruneSlot removes values beyond the top-K for a specific slot.
// This should be called periodically to maintain top-K behavior.
func (ss *SlotStore) PruneSlot(ctx context.Context, scope, cmdNorm string, slotIdx int) (int64, error) {
	// Count total values for this slot
	var total int
	err := ss.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ?
	`, scope, cmdNorm, slotIdx).Scan(&total)
	if err != nil {
		return 0, err
	}

	if total <= ss.topK {
		return 0, nil // Nothing to prune
	}

	// Delete values not in top-K
	result, err := ss.db.ExecContext(ctx, `
		DELETE FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ?
		AND value NOT IN (
			SELECT value FROM slot_value
			WHERE scope = ? AND cmd_norm = ? AND slot_idx = ?
			ORDER BY count DESC
			LIMIT ?
		)
	`, scope, cmdNorm, slotIdx, scope, cmdNorm, slotIdx, ss.topK)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// TauMs returns the configured tau value.
func (ss *SlotStore) TauMs() int64 {
	return ss.tauMs
}

// TopK returns the configured top-K value.
func (ss *SlotStore) TopK() int {
	return ss.topK
}
