// Package retention implements data retention and purge for the suggestions engine.
// It implements the retention policy specified in tech_suggestions_v3.md Section 20.1-20.2.
package retention

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// Default configuration values per spec Section 20.1.
const (
	// DefaultRetentionDays is the default retention period for command_event.
	DefaultRetentionDays = 90

	// MinRetentionDays is the minimum allowed retention period.
	MinRetentionDays = 1

	// MaxRetentionDays is the maximum allowed retention period (10 years).
	MaxRetentionDays = 3650

	// VacuumThreshold is the number of deleted rows that triggers a vacuum.
	VacuumThreshold = 10000
)

// Policy defines the retention policy for stored data.
type Policy struct {
	Logger        *slog.Logger
	RetentionDays int
	AutoVacuum    bool
}

// DefaultPolicy returns the default retention policy.
func DefaultPolicy() *Policy {
	return &Policy{
		RetentionDays: DefaultRetentionDays,
		AutoVacuum:    true,
		Logger:        slog.Default(),
	}
}

// Purger handles data retention and purge operations.
type Purger struct {
	db     *sql.DB
	policy *Policy
}

// NewPurger creates a new purger with the given database and policy.
func NewPurger(db *sql.DB, policy *Policy) *Purger {
	if policy == nil {
		policy = DefaultPolicy()
	}

	// Clamp retention days to valid range
	if policy.RetentionDays < MinRetentionDays {
		policy.RetentionDays = MinRetentionDays
	}
	if policy.RetentionDays > MaxRetentionDays {
		policy.RetentionDays = MaxRetentionDays
	}

	if policy.Logger == nil {
		policy.Logger = slog.Default()
	}

	return &Purger{
		db:     db,
		policy: policy,
	}
}

// PurgeResult contains the results of a purge operation.
type PurgeResult struct {
	// DeletedEvents is the number of command_event rows deleted.
	DeletedEvents int64

	// Vacuumed indicates whether a vacuum was performed.
	Vacuumed bool

	// Duration is how long the purge took.
	Duration time.Duration
}

// Purge deletes old command_event data based on the retention policy.
// Per spec Section 20.2:
//
//	DELETE FROM command_event WHERE ts < (now_ms - retention_days * 86400000)
func (p *Purger) Purge(ctx context.Context) (*PurgeResult, error) {
	return p.PurgeAt(ctx, time.Now().UnixMilli())
}

// PurgeAt deletes old command_event data using a specific current time.
// This is useful for testing with a controlled timestamp.
func (p *Purger) PurgeAt(ctx context.Context, nowMs int64) (*PurgeResult, error) {
	start := time.Now()
	result := &PurgeResult{}

	// Calculate cutoff timestamp
	retentionMs := int64(p.policy.RetentionDays) * 24 * 60 * 60 * 1000
	cutoffMs := nowMs - retentionMs

	p.policy.Logger.Debug("starting purge",
		"retention_days", p.policy.RetentionDays,
		"cutoff_ms", cutoffMs,
	)

	// Delete old command_event rows
	res, err := p.db.ExecContext(ctx, `
		DELETE FROM command_event WHERE ts < ?
	`, cutoffMs)
	if err != nil {
		return nil, err
	}

	deleted, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	result.DeletedEvents = deleted

	// Optionally vacuum after large deletes
	if p.policy.AutoVacuum && deleted >= VacuumThreshold {
		p.policy.Logger.Debug("running vacuum after large purge", "deleted", deleted)
		if _, err := p.db.ExecContext(ctx, "VACUUM"); err != nil {
			p.policy.Logger.Warn("vacuum failed", "error", err)
			// Don't fail the purge on vacuum error
		} else {
			result.Vacuumed = true
		}
	}

	result.Duration = time.Since(start)

	p.policy.Logger.Info("purge completed",
		"deleted_events", result.DeletedEvents,
		"vacuumed", result.Vacuumed,
		"duration", result.Duration,
	)

	return result, nil
}

// RebuildAggregates rebuilds aggregate tables from retained command_event history.
// This can be used after restoring from backup or when aggregates are corrupted.
// Per spec Section 20.1: "clai-daemon --rebuild-aggregates"
func (p *Purger) RebuildAggregates(ctx context.Context) error {
	p.policy.Logger.Info("rebuilding aggregates from retained history")

	// This is a placeholder for aggregate rebuilding.
	// The actual implementation depends on the aggregate storage implementation
	// (FrequencyStore, TransitionStore, SlotStore).
	//
	// The rebuild process would:
	// 1. Clear existing aggregate tables
	// 2. Iterate through command_event in chronological order
	// 3. Replay each event through the scoring/transition logic
	//
	// For now, we just log that this is not yet implemented.
	p.policy.Logger.Warn("rebuild aggregates not yet implemented")

	return nil
}

// GetRetentionDays returns the configured retention period in days.
func (p *Purger) GetRetentionDays() int {
	return p.policy.RetentionDays
}

// EstimatePurgeCount returns the estimated number of rows that would be purged.
func (p *Purger) EstimatePurgeCount(ctx context.Context) (int64, error) {
	return p.EstimatePurgeCountAt(ctx, time.Now().UnixMilli())
}

// EstimatePurgeCountAt returns the estimated purge count for a specific time.
func (p *Purger) EstimatePurgeCountAt(ctx context.Context, nowMs int64) (int64, error) {
	retentionMs := int64(p.policy.RetentionDays) * 24 * 60 * 60 * 1000
	cutoffMs := nowMs - retentionMs

	var count int64
	err := p.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM command_event WHERE ts < ?
	`, cutoffMs).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
