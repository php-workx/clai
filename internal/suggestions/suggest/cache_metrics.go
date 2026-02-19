package suggest

import (
	"sync"
	"sync/atomic"
)

// CacheLayer identifies which cache layer served a result.
type CacheLayer string

const (
	// CacheLayerL1 indicates a hit from L1 (per-session hot cache).
	CacheLayerL1 CacheLayer = "L1"

	// CacheLayerL2 indicates a hit from L2 (per-repo cold cache).
	CacheLayerL2 CacheLayer = "L2"

	// CacheLayerL3 indicates a hit from L3 (SQLite persistent cache).
	CacheLayerL3 CacheLayer = "L3"

	// CacheLayerMiss indicates a cache miss across all layers.
	CacheLayerMiss CacheLayer = "miss"
)

// CacheMetrics tracks hit/miss counters per cache layer using lock-free atomics.
type CacheMetrics struct {
	l1Hits   atomic.Int64
	l1Misses atomic.Int64
	l2Hits   atomic.Int64
	l2Misses atomic.Int64
	l3Hits   atomic.Int64
	l3Misses atomic.Int64
}

// RecordHit increments the hit counter for the specified layer.
func (m *CacheMetrics) RecordHit(layer CacheLayer) {
	switch layer {
	case CacheLayerL1:
		m.l1Hits.Add(1)
	case CacheLayerL2:
		m.l2Hits.Add(1)
	case CacheLayerL3:
		m.l3Hits.Add(1)
	}
}

// RecordMiss increments the miss counter for the specified layer.
func (m *CacheMetrics) RecordMiss(layer CacheLayer) {
	switch layer {
	case CacheLayerL1:
		m.l1Misses.Add(1)
	case CacheLayerL2:
		m.l2Misses.Add(1)
	case CacheLayerL3:
		m.l3Misses.Add(1)
	}
}

// LayerStats holds hit/miss statistics for a single cache layer.
type LayerStats struct {
	Hits     int64   `json:"hits"`
	Misses   int64   `json:"misses"`
	HitRate  float64 `json:"hit_rate"`
	Requests int64   `json:"requests"`
}

// MetricsSnapshot is a point-in-time snapshot of all cache metrics.
type MetricsSnapshot struct {
	L1 LayerStats `json:"l1"`
	L2 LayerStats `json:"l2"`
	L3 LayerStats `json:"l3"`
}

// Snapshot returns a point-in-time copy of all metrics.
func (m *CacheMetrics) Snapshot() MetricsSnapshot {
	l1h := m.l1Hits.Load()
	l1m := m.l1Misses.Load()
	l2h := m.l2Hits.Load()
	l2m := m.l2Misses.Load()
	l3h := m.l3Hits.Load()
	l3m := m.l3Misses.Load()

	return MetricsSnapshot{
		L1: makeLayerStats(l1h, l1m),
		L2: makeLayerStats(l2h, l2m),
		L3: makeLayerStats(l3h, l3m),
	}
}

func makeLayerStats(hits, misses int64) LayerStats {
	total := hits + misses
	var rate float64
	if total > 0 {
		rate = float64(hits) / float64(total)
	}
	return LayerStats{
		Hits:     hits,
		Misses:   misses,
		HitRate:  rate,
		Requests: total,
	}
}

// PrecomputeTracker provides deduplication for async precompute tasks.
// It tracks which context keys have a pending precompute so we don't
// schedule duplicate work.
type PrecomputeTracker struct {
	pending   map[string]struct{}
	completed atomic.Int64
	errors    atomic.Int64
	mu        sync.Mutex
}

// NewPrecomputeTracker creates a new precompute tracker.
func NewPrecomputeTracker() *PrecomputeTracker {
	return &PrecomputeTracker{
		pending: make(map[string]struct{}),
	}
}

// TryAcquire attempts to acquire a slot for the given key.
// Returns true if the key was not already pending (caller should proceed).
// Returns false if a precompute is already in-flight for this key.
func (t *PrecomputeTracker) TryAcquire(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.pending[key]; exists {
		return false
	}
	t.pending[key] = struct{}{}
	return true
}

// Release marks a precompute as completed for the given key.
func (t *PrecomputeTracker) Release(key string, err error) {
	t.mu.Lock()
	delete(t.pending, key)
	t.mu.Unlock()

	if err != nil {
		t.errors.Add(1)
	} else {
		t.completed.Add(1)
	}
}

// PendingCount returns the number of currently in-flight precomputes.
func (t *PrecomputeTracker) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// CompletedCount returns total completed precomputes.
func (t *PrecomputeTracker) CompletedCount() int64 {
	return t.completed.Load()
}

// ErrorCount returns total errored precomputes.
func (t *PrecomputeTracker) ErrorCount() int64 {
	return t.errors.Load()
}
