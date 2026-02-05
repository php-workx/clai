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
	"sync"
	"time"

	"github.com/runger/clai/internal/suggestions/event"
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

// Writer batches events and writes them to SQLite in transactions.
// It is safe for concurrent use.
type Writer struct {
	db   *sql.DB
	opts Options

	eventCh   chan *event.CommandEvent
	flushCh   chan struct{}
	doneCh    chan struct{}
	stoppedCh chan struct{}
	stopOnce  sync.Once

	// Metrics
	mu             sync.RWMutex
	eventsWritten  int64
	batchesWritten int64
	eventsDropped  int64
	lastFlushTime  time.Time
	lastBatchSize  int
	writeErrors    int64
	lastWriteError error
	lastErrorTime  time.Time
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

	return &Writer{
		db:        db,
		opts:      opts,
		eventCh:   make(chan *event.CommandEvent, opts.QueueSize),
		flushCh:   make(chan struct{}, 1),
		doneCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
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

// writeBatch writes a batch of events to the database in a single transaction.
func (w *Writer) writeBatch(batch []*event.CommandEvent) error {
	if len(batch) == 0 {
		return nil
	}

	ctx := context.Background()

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Rollback is best-effort after commit

	// Prepare statement within transaction for better performance
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO command_event (
			session_id, shell, cwd, cmd_raw, cmd_norm, exit_code,
			duration_ms, repo_key, branch, ts
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			string(ev.Shell),
			ev.Cwd,
			ev.CmdRaw,
			ev.CmdRaw, // cmd_norm - to be replaced with normalized version
			ev.ExitCode,
			durationMs,
			"",    // repo_key - computed by git context
			"",    // branch - computed by git context
			ev.Ts, // timestamp
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Stats returns current writer statistics.
type Stats struct {
	EventsWritten  int64
	BatchesWritten int64
	EventsDropped  int64
	QueueLength    int
	LastFlushTime  time.Time
	LastBatchSize  int
	WriteErrors    int64
	LastWriteError error
	LastErrorTime  time.Time
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
