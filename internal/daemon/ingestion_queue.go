package daemon

import (
	"log/slog"
	"sync"
	"time"
)

// Event represents a generic event in the ingestion queue.
type Event struct {
	Type      string
	Payload   interface{}
	Timestamp time.Time
}

// IngestionQueue is a bounded FIFO queue for ingestion events.
// When the queue is full, it drops the oldest events (not newest).
// It logs a warning when the queue exceeds 75% capacity.
type IngestionQueue struct {
	mu            sync.Mutex
	events        []Event
	maxSize       int
	logger        *slog.Logger
	warnThreshold int // 75% of maxSize
	warned        bool
	totalDropped  int64
	totalEnqueued int64
}

// NewIngestionQueue creates a new IngestionQueue with the specified maximum size.
// If maxSize <= 0, it defaults to 8192.
func NewIngestionQueue(maxSize int, logger *slog.Logger) *IngestionQueue {
	if maxSize <= 0 {
		maxSize = 8192
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &IngestionQueue{
		events:        make([]Event, 0, maxSize),
		maxSize:       maxSize,
		logger:        logger,
		warnThreshold: (maxSize * 3) / 4, // 75%
	}
}

// Enqueue adds an event to the queue. If the queue is full, the oldest event
// is dropped to make room for the new one. Returns true if an event was dropped.
func (q *IngestionQueue) Enqueue(event Event) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	dropped := false

	// Check if queue is full
	if len(q.events) >= q.maxSize {
		// Drop oldest event (shift left)
		q.events = q.events[1:]
		q.totalDropped++
		dropped = true

		q.logger.Warn("ingestion queue full, dropping oldest event",
			"queue_size", q.maxSize,
			"total_dropped", q.totalDropped,
		)
	}

	q.events = append(q.events, event)
	q.totalEnqueued++

	// Check 75% threshold
	if len(q.events) >= q.warnThreshold && !q.warned {
		q.warned = true
		q.logger.Warn("ingestion queue exceeds 75% capacity",
			"current_size", len(q.events),
			"max_size", q.maxSize,
			"threshold", q.warnThreshold,
		)
	} else if len(q.events) < q.warnThreshold {
		q.warned = false // Reset warning when below threshold
	}

	return dropped
}

// Dequeue removes and returns the oldest event from the queue.
// Returns the event and true if successful, or zero Event and false if empty.
func (q *IngestionQueue) Dequeue() (Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.events) == 0 {
		return Event{}, false
	}

	event := q.events[0]
	q.events = q.events[1:]

	return event, true
}

// DequeueN removes and returns up to n events from the queue.
func (q *IngestionQueue) DequeueN(n int) []Event {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.events) == 0 {
		return nil
	}

	if n > len(q.events) {
		n = len(q.events)
	}

	batch := make([]Event, n)
	copy(batch, q.events[:n])
	q.events = q.events[n:]

	return batch
}

// Len returns the current number of events in the queue.
func (q *IngestionQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events)
}

// Cap returns the maximum capacity of the queue.
func (q *IngestionQueue) Cap() int {
	return q.maxSize
}

// Stats returns queue statistics.
func (q *IngestionQueue) Stats() IngestionQueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return IngestionQueueStats{
		CurrentSize:   len(q.events),
		MaxSize:       q.maxSize,
		TotalEnqueued: q.totalEnqueued,
		TotalDropped:  q.totalDropped,
	}
}

// IngestionQueueStats holds queue statistics.
type IngestionQueueStats struct {
	CurrentSize   int
	MaxSize       int
	TotalEnqueued int64
	TotalDropped  int64
}

// Clear empties the queue.
func (q *IngestionQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = q.events[:0]
	q.warned = false
}
