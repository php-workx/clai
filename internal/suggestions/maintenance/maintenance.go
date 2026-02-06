// Package maintenance implements background database maintenance tasks for the
// suggestions engine. It runs as a goroutine inside the daemon, performing
// WAL checkpointing, retention pruning, FTS optimization, and VACUUM.
//
// Per spec Section 4.3: ticker-based maintenance goroutine.
package maintenance

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Default configuration values.
const (
	// DefaultInterval is the default maintenance tick interval (5 minutes).
	DefaultInterval = 5 * time.Minute

	// DefaultRetentionDays is the default retention period.
	// A value of 0 disables time-based pruning.
	DefaultRetentionDays = 90

	// DefaultPruneBatchSize is the number of rows deleted per batch.
	DefaultPruneBatchSize = 1000

	// DefaultPruneYieldDuration is the sleep between prune batches.
	DefaultPruneYieldDuration = 100 * time.Millisecond

	// DefaultLowActivityThreshold is the event count below which
	// the system is considered "low activity" for heavier operations.
	DefaultLowActivityThreshold = 5

	// DefaultVacuumGrowthRatio is the ratio of current DB size to
	// last-vacuum size that triggers a new VACUUM.
	DefaultVacuumGrowthRatio = 2.0
)

// Config configures the maintenance runner.
type Config struct {
	// Interval is the ticker interval between maintenance passes.
	// If zero, DefaultInterval is used.
	Interval time.Duration

	// RetentionDays is the number of days to retain command_event rows.
	// 0 disables time-based pruning; negative values use DefaultRetentionDays.
	RetentionDays int

	// PruneBatchSize is the number of rows deleted per batch.
	// If <= 0, DefaultPruneBatchSize is used.
	PruneBatchSize int

	// PruneYieldDuration is the sleep between prune batches.
	// If zero, DefaultPruneYieldDuration is used.
	PruneYieldDuration time.Duration

	// LowActivityThreshold is the event count below which low-activity
	// operations (FTS optimize, VACUUM) are triggered.
	// If <= 0, DefaultLowActivityThreshold is used.
	LowActivityThreshold int

	// VacuumGrowthRatio triggers VACUUM when current DB size exceeds
	// last-vacuum size by this ratio.
	// If <= 0, DefaultVacuumGrowthRatio is used.
	VacuumGrowthRatio float64

	// DBPath is the path to the database file, used for size checks.
	// If empty, VACUUM size checks are skipped.
	DBPath string

	// Logger is the structured logger. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// applyDefaults fills in zero-valued fields with defaults.
func (c *Config) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	if c.RetentionDays < 0 {
		c.RetentionDays = DefaultRetentionDays
	}
	if c.PruneBatchSize <= 0 {
		c.PruneBatchSize = DefaultPruneBatchSize
	}
	if c.PruneYieldDuration <= 0 {
		c.PruneYieldDuration = DefaultPruneYieldDuration
	}
	if c.LowActivityThreshold <= 0 {
		c.LowActivityThreshold = DefaultLowActivityThreshold
	}
	if c.VacuumGrowthRatio <= 0 {
		c.VacuumGrowthRatio = DefaultVacuumGrowthRatio
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Stats holds cumulative maintenance statistics.
type Stats struct {
	Ticks               int64
	EventsPruned        int64
	OrphansCleaned      int64
	WALCheckpoints      int64
	FTSOptimizations    int64
	VacuumsPerformed    int64
	LastTickTime        time.Time
	LastVacuumSizeBytes int64
}

// Runner executes periodic maintenance tasks on the suggestions database.
type Runner struct {
	db     *sql.DB
	cfg    Config
	events atomic.Int64 // events since last tick

	mu    sync.Mutex
	stats Stats
}

// NewRunner creates a new maintenance runner.
// The db parameter must be a V2 suggestions database connection.
func NewRunner(db *sql.DB, cfg Config) *Runner {
	cfg.applyDefaults()
	return &Runner{
		db:  db,
		cfg: cfg,
	}
}

// RecordEvent atomically increments the event counter.
// Call this from the ingestion path to track activity.
func (r *Runner) RecordEvent() {
	r.events.Add(1)
}

// GetStats returns a snapshot of the maintenance statistics.
func (r *Runner) GetStats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// Run starts the maintenance loop. It blocks until ctx is cancelled
// or stopCh is closed. Intended to be called as a goroutine.
func (r *Runner) Run(ctx context.Context, stopCh <-chan struct{}) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	r.cfg.Logger.Info("maintenance runner started",
		"interval", r.cfg.Interval,
		"retention_days", r.cfg.RetentionDays,
		"batch_size", r.cfg.PruneBatchSize,
	)

	for {
		select {
		case <-ctx.Done():
			r.cfg.Logger.Info("maintenance runner stopping (context cancelled)")
			return
		case <-stopCh:
			r.cfg.Logger.Info("maintenance runner stopping (shutdown signal)")
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick performs a single maintenance pass.
func (r *Runner) tick(ctx context.Context) {
	r.mu.Lock()
	r.stats.Ticks++
	r.stats.LastTickTime = time.Now()
	tickNum := r.stats.Ticks
	r.mu.Unlock()

	// Read and reset event counter
	eventCount := r.events.Swap(0)
	lowActivity := eventCount < int64(r.cfg.LowActivityThreshold)

	r.cfg.Logger.Debug("maintenance tick",
		"tick", tickNum,
		"events_since_last", eventCount,
		"low_activity", lowActivity,
	)

	// 1. WAL checkpoint (every tick)
	r.walCheckpoint(ctx, lowActivity)

	// 2. Retention pruning (every tick, if enabled)
	if r.cfg.RetentionDays > 0 {
		pruned := r.retentionPrune(ctx)
		if pruned > 0 {
			// Also clean orphaned templates after pruning
			orphans := r.cleanOrphanedTemplates(ctx)
			if orphans > 0 {
				r.cfg.Logger.Info("cleaned orphaned templates", "count", orphans)
			}
		}
	}

	// 3. FTS optimize (low activity only)
	if lowActivity {
		r.ftsOptimize(ctx)
	}

	// 4. VACUUM (low activity only, when size threshold exceeded)
	if lowActivity {
		r.maybeVacuum(ctx)
	}
}

// walCheckpoint runs a WAL checkpoint with mode depending on activity level.
// During active use: PASSIVE (non-blocking).
// During low activity: TRUNCATE (blocks briefly, reclaims WAL space).
func (r *Runner) walCheckpoint(ctx context.Context, lowActivity bool) {
	mode := "PASSIVE"
	if lowActivity {
		mode = "TRUNCATE"
	}

	// PRAGMA wal_checkpoint does not support parameterized mode,
	// so we build the SQL string directly. The mode is always one
	// of two hardcoded constants, so this is safe.
	query := "PRAGMA wal_checkpoint(" + mode + ")"
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		r.cfg.Logger.Warn("WAL checkpoint failed", "mode", mode, "error", err)
		return
	}

	r.mu.Lock()
	r.stats.WALCheckpoints++
	r.mu.Unlock()
	r.cfg.Logger.Debug("WAL checkpoint completed", "mode", mode)
}

// retentionPrune deletes old command_event rows in batches.
// Uses V2 schema's ts_ms column. Returns total rows deleted.
func (r *Runner) retentionPrune(ctx context.Context) int64 {
	cutoffMs := time.Now().UnixMilli() - int64(r.cfg.RetentionDays)*24*60*60*1000
	var totalDeleted int64

	for {
		// Check for cancellation between batches
		select {
		case <-ctx.Done():
			return totalDeleted
		default:
		}

		// Delete a batch using subquery for SQLite compatibility
		res, err := r.db.ExecContext(ctx, `
			DELETE FROM command_event
			WHERE id IN (
				SELECT id FROM command_event
				WHERE ts_ms < ?
				LIMIT ?
			)
		`, cutoffMs, r.cfg.PruneBatchSize)
		if err != nil {
			r.cfg.Logger.Warn("retention prune batch failed", "error", err)
			break
		}

		deleted, err := res.RowsAffected()
		if err != nil {
			r.cfg.Logger.Warn("failed to get rows affected", "error", err)
			break
		}

		totalDeleted += deleted
		r.mu.Lock()
		r.stats.EventsPruned += deleted
		r.mu.Unlock()

		// If we deleted fewer than a full batch, we're done
		if deleted < int64(r.cfg.PruneBatchSize) {
			break
		}

		// Yield between batches to avoid monopolizing the DB
		select {
		case <-ctx.Done():
			return totalDeleted
		case <-time.After(r.cfg.PruneYieldDuration):
		}
	}

	if totalDeleted > 0 {
		r.cfg.Logger.Info("retention prune completed",
			"deleted", totalDeleted,
			"cutoff_ms", cutoffMs,
		)
	}

	return totalDeleted
}

// cleanOrphanedTemplates removes command_template rows that are no longer
// referenced by any command_event.
func (r *Runner) cleanOrphanedTemplates(ctx context.Context) int64 {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM command_template
		WHERE template_id NOT IN (
			SELECT DISTINCT template_id
			FROM command_event
			WHERE template_id IS NOT NULL
		)
	`)
	if err != nil {
		r.cfg.Logger.Warn("orphan template cleanup failed", "error", err)
		return 0
	}

	deleted, err := res.RowsAffected()
	if err != nil {
		r.cfg.Logger.Warn("failed to get rows affected for orphan cleanup", "error", err)
		return 0
	}

	r.mu.Lock()
	r.stats.OrphansCleaned += deleted
	r.mu.Unlock()
	return deleted
}

// ftsOptimize runs the FTS5 'optimize' command to merge b-tree segments.
func (r *Runner) ftsOptimize(ctx context.Context) {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO command_event_fts(command_event_fts) VALUES('optimize')
	`)
	if err != nil {
		r.cfg.Logger.Warn("FTS optimize failed", "error", err)
		return
	}

	r.mu.Lock()
	r.stats.FTSOptimizations++
	r.mu.Unlock()
	r.cfg.Logger.Debug("FTS optimize completed")
}

// maybeVacuum checks if the database has grown enough to warrant a VACUUM.
// It compares the current file size against the size at last vacuum.
func (r *Runner) maybeVacuum(ctx context.Context) {
	if r.cfg.DBPath == "" {
		return
	}

	info, err := os.Stat(r.cfg.DBPath)
	if err != nil {
		r.cfg.Logger.Warn("failed to stat database for vacuum check", "error", err)
		return
	}

	currentSize := info.Size()

	r.mu.Lock()
	lastSize := r.stats.LastVacuumSizeBytes
	if lastSize == 0 {
		// On first check, just record the size
		r.stats.LastVacuumSizeBytes = currentSize
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	ratio := float64(currentSize) / float64(lastSize)
	if ratio < r.cfg.VacuumGrowthRatio {
		return
	}

	r.cfg.Logger.Info("running VACUUM",
		"current_size", currentSize,
		"last_vacuum_size", lastSize,
		"ratio", ratio,
	)

	if _, err := r.db.ExecContext(ctx, "VACUUM"); err != nil {
		r.cfg.Logger.Warn("VACUUM failed", "error", err)
		return
	}

	// Update size after vacuum
	r.mu.Lock()
	if info, err := os.Stat(r.cfg.DBPath); err == nil {
		r.stats.LastVacuumSizeBytes = info.Size()
	}
	r.stats.VacuumsPerformed++
	r.mu.Unlock()
	r.cfg.Logger.Info("VACUUM completed")
}
