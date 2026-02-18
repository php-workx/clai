// Package batch provides batched event writing to SQLite for the clai suggestions engine.
// Batching reduces lock churn during event bursts (e.g., loops, scripts).
//
// Per spec Section 10.3:
//   - Flush every 25-50ms or 100 events
//   - Single writer goroutine
//   - Transaction per batch
//   - Prepared statements
//   - Graceful flush on shutdown
package batch

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/ingest"
)

// Default configuration values per spec Section 10.3.
const (
	// DefaultFlushInterval is the default batch flush interval (25-50ms).
	DefaultFlushInterval = 35 * time.Millisecond

	// DefaultMaxBatchSize is the default maximum events per batch.
	DefaultMaxBatchSize = 100

	// DefaultQueueSize is the default channel buffer size.
	DefaultQueueSize = 500
)

// Options configures the batch writer.
type Options struct {
	// WritePathConfig configures the ingest.WritePath call per event.
	// When set, each event in the batch also populates V2 aggregate tables
	// (command_template, command_stat, transition_stat, etc.) via WritePath.
	WritePathConfig *ingest.WritePathConfig

	// Logger is the structured logger (optional, uses slog.Default if nil).
	Logger *slog.Logger

	// FlushInterval is how often to flush batched events.
	// Defaults to DefaultFlushInterval (35ms).
	FlushInterval time.Duration

	// MaxBatchSize is the maximum number of events per batch.
	// When this limit is reached, the batch is flushed immediately.
	// Defaults to DefaultMaxBatchSize (100).
	MaxBatchSize int

	// QueueSize is the size of the event channel buffer.
	// Defaults to DefaultQueueSize (500).
	QueueSize int
}

// DefaultOptions returns the default batch writer options.
func DefaultOptions() Options {
	return Options{
		FlushInterval: DefaultFlushInterval,
		MaxBatchSize:  DefaultMaxBatchSize,
		QueueSize:     DefaultQueueSize,
	}
}

// maxSessionEntries is the maximum number of session entries to keep in memory.
// When exceeded, the oldest entries are evicted.
const maxSessionEntries = 256

// sessionState tracks per-session state for transition tracking across batches.
type sessionState struct {
	lastSeen       time.Time
	lastTemplateID string
	lastExitCode   int
	lastFailed     bool
}

// Writer batches events and writes them to SQLite in transactions.
// It is safe for concurrent use.
type Writer struct {
	lastErrorTime  time.Time
	lastFlushTime  time.Time
	lastWriteError error
	doneCh         chan struct{}
	db             *sql.DB
	stoppedCh      chan struct{}
	sessions       map[string]*sessionState
	flushCh        chan struct{}
	eventCh        chan *event.CommandEvent
	logger         *slog.Logger
	opts           Options
	eventsDropped  int64
	batchesWritten int64
	eventsWritten  int64
	writeErrors    int64
	lastBatchSize  int
	mu             sync.RWMutex
	stopOnce       sync.Once
}

// NewWriter creates a new batch writer with the given database and options.
// Call Start() to begin the write loop.
func NewWriter(db *sql.DB, opts Options) *Writer {
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = DefaultFlushInterval
	}
	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = DefaultMaxBatchSize
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultQueueSize
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Writer{
		db:        db,
		opts:      opts,
		logger:    logger,
		eventCh:   make(chan *event.CommandEvent, opts.QueueSize),
		flushCh:   make(chan struct{}, 1),
		doneCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		sessions:  make(map[string]*sessionState),
	}
}

// Start begins the write loop.
// Returns immediately; the loop runs in a goroutine.
func (w *Writer) Start() {
	go w.writeLoop()
}

// Stop gracefully stops the writer, flushing any pending events.
// It is safe to call Stop multiple times.
func (w *Writer) Stop() {
	w.stopOnce.Do(func() {
		close(w.doneCh)
		<-w.stoppedCh // Wait for write loop to finish
	})
}

// Enqueue adds an event to the write queue.
// Returns true if the event was queued, false if dropped (queue full).
// This method is non-blocking and safe for concurrent use.
func (w *Writer) Enqueue(ev *event.CommandEvent) bool {
	if ev == nil || ev.Ephemeral {
		// Don't persist ephemeral events (incognito mode)
		return true
	}

	select {
	case w.eventCh <- ev:
		return true
	default:
		// Queue full, drop the event
		w.mu.Lock()
		w.eventsDropped++
		w.mu.Unlock()
		return false
	}
}

// Flush triggers an immediate flush of the current batch.
// This is non-blocking; the actual flush happens asynchronously.
func (w *Writer) Flush() {
	select {
	case w.flushCh <- struct{}{}:
	default:
		// Flush already pending
	}
}

// writeLoop is the main write loop that batches and persists events.
func (w *Writer) writeLoop() {
	defer close(w.stoppedCh)

	batch := make([]*event.CommandEvent, 0, w.opts.MaxBatchSize)
	ticker := time.NewTicker(w.opts.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := w.writeBatch(batch); err != nil {
			w.mu.Lock()
			w.writeErrors++
			w.lastWriteError = err
			w.lastErrorTime = time.Now()
			w.mu.Unlock()
		} else {
			w.mu.Lock()
			w.eventsWritten += int64(len(batch))
			w.batchesWritten++
			w.lastFlushTime = time.Now()
			w.lastBatchSize = len(batch)
			w.mu.Unlock()
		}

		batch = batch[:0] // Reset slice, keep capacity
	}

	for {
		select {
		case ev := <-w.eventCh:
			batch = append(batch, ev)
			if len(batch) >= w.opts.MaxBatchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-w.flushCh:
			flush()

		case <-w.doneCh:
			// Drain remaining events from channel
			for {
				select {
				case ev := <-w.eventCh:
					batch = append(batch, ev)
					if len(batch) >= w.opts.MaxBatchSize {
						flush()
					}
				default:
					// No more events
					flush()
					return
				}
			}
		}
	}
}

// writeBatch writes a batch of events to the database.
// When WritePathConfig is set, each event is processed through ingest.WritePath
// which populates all V2 aggregate tables atomically. WritePath errors for
// individual events are logged but do not fail the entire batch.
// When WritePathConfig is nil, events are written as raw INSERTs only.
func (w *Writer) writeBatch(batch []*event.CommandEvent) error {
	if len(batch) == 0 {
		return nil
	}

	if w.opts.WritePathConfig != nil {
		return w.writeBatchV2(batch)
	}
	return w.writeBatchRaw(batch)
}

// writeBatchV2 processes each event through ingest.WritePath, populating
// all V2 aggregate tables. Per-session lastTemplateID is tracked for
// transition support across batches.
func (w *Writer) writeBatchV2(batch []*event.CommandEvent) error {
	ctx := context.Background()

	for _, ev := range batch {
		// Look up per-session state for transition tracking
		sess := w.sessions[ev.SessionID]
		if sess == nil {
			sess = &sessionState{}
			w.sessions[ev.SessionID] = sess
		}

		// Build the WritePathContext with transition state
		wctx := ingest.PrepareWriteContext(
			ev,
			ev.RepoKey,
			ev.Branch,
			sess.lastTemplateID,
			sess.lastExitCode,
			sess.lastFailed,
			nil, // aliases
		)

		result, err := ingest.WritePath(ctx, w.db, wctx, w.opts.WritePathConfig)
		if err != nil {
			w.logger.Warn("batch WritePath error",
				"session_id", ev.SessionID,
				"cmd_raw", ev.CmdRaw,
				"error", err,
			)
			continue // Skip this event, don't fail the batch
		}

		// Update per-session state for next event's transitions
		sess.lastTemplateID = result.TemplateID
		sess.lastExitCode = ev.ExitCode
		sess.lastFailed = ev.ExitCode != 0
		sess.lastSeen = time.Now()
	}

	// Evict stale sessions if map exceeds bound
	if len(w.sessions) > maxSessionEntries {
		w.evictStaleSessions()
	}

	return nil
}

// evictStaleSessions removes the oldest half of session entries when the map exceeds bounds.
func (w *Writer) evictStaleSessions() {
	// Find the oldest entries and remove them
	type entry struct {
		lastSeen time.Time
		id       string
	}
	entries := make([]entry, 0, len(w.sessions))
	for id, s := range w.sessions {
		entries = append(entries, entry{id: id, lastSeen: s.lastSeen})
	}
	// Simple approach: remove entries older than 1 hour first
	cutoff := time.Now().Add(-1 * time.Hour)
	removed := 0
	for _, e := range entries {
		if e.lastSeen.Before(cutoff) {
			delete(w.sessions, e.id)
			removed++
		}
	}
	// If still over limit, remove oldest half
	if len(w.sessions) > maxSessionEntries && removed == 0 {
		count := 0
		target := len(w.sessions) / 2
		for id := range w.sessions {
			if count >= target {
				break
			}
			delete(w.sessions, id)
			count++
		}
	}
}

// writeBatchRaw writes a batch of events as raw INSERTs in a single transaction.
// This is the legacy path used when WritePathConfig is nil.
func (w *Writer) writeBatchRaw(batch []*event.CommandEvent) error {
	ctx := context.Background()

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Rollback is best-effort after commit

	// Prepare statement within transaction for better performance.
	// Columns match V2 schema: command_event table (no shell column; ts -> ts_ms).
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO command_event (
			session_id, ts_ms, cwd, repo_key, branch,
			cmd_raw, cmd_norm, exit_code, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ev := range batch {
		var durationMs *int64
		if ev.DurationMs != nil {
			durationMs = ev.DurationMs
		}

		// Note: cmd_norm, repo_key, branch should be computed by caller
		// For now, insert with raw command; normalization is done upstream
		_, err := stmt.ExecContext(ctx,
			ev.SessionID,
			ev.TS, // ts_ms
			ev.Cwd,
			"", // repo_key - computed by git context
			"", // branch - computed by git context
			ev.CmdRaw,
			ev.CmdRaw, // cmd_norm - to be replaced with normalized version
			ev.ExitCode,
			durationMs,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Stats returns current writer statistics.
type Stats struct {
	LastWriteError error
	LastFlushTime  time.Time
	LastErrorTime  time.Time
	EventsWritten  int64
	BatchesWritten int64
	EventsDropped  int64
	WriteErrors    int64
	QueueLength    int
	LastBatchSize  int
}

// Stats returns the current writer statistics.
func (w *Writer) Stats() Stats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return Stats{
		EventsWritten:  w.eventsWritten,
		BatchesWritten: w.batchesWritten,
		EventsDropped:  w.eventsDropped,
		QueueLength:    len(w.eventCh),
		LastFlushTime:  w.lastFlushTime,
		LastBatchSize:  w.lastBatchSize,
		WriteErrors:    w.writeErrors,
		LastWriteError: w.lastWriteError,
		LastErrorTime:  w.lastErrorTime,
	}
}
