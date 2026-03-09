// Package metrics provides atomic counters for suggestions engine observability.
// Counters are lock-free (sync/atomic) and safe for concurrent use across
// daemon goroutines.
package metrics

import (
	"sync/atomic"
)

// Counters holds atomic observability counters for the suggestions engine.
// All fields use sync/atomic for lock-free concurrent access.
type Counters struct {
	SuggestRequests   atomic.Int64 // total suggest API calls
	SuggestHits       atomic.Int64 // requests that produced >= 1 suggestion
	SuggestMisses     atomic.Int64 // requests that produced zero suggestions
	CacheHits         atomic.Int64 // suggestion cache hits (any layer)
	CacheMisses       atomic.Int64 // suggestion cache misses
	FeedbackAccepted  atomic.Int64 // accepted feedback events
	FeedbackDismissed atomic.Int64 // dismissed feedback events
	FeedbackEdited    atomic.Int64 // edited-then-run feedback events
	IngestCommands    atomic.Int64 // command events ingested
	IngestErrors      atomic.Int64 // ingestion errors
	LatencySumMs      atomic.Int64 // cumulative suggest latency for average calculation
}

// Global is the process-wide metrics singleton.
var Global = &Counters{}

// Snapshot returns a point-in-time copy of all counters as a string-keyed map.
// The snapshot is consistent per-field but not transactionally consistent
// across fields (acceptable for observability).
func (c *Counters) Snapshot() map[string]int64 {
	return map[string]int64{
		"suggest_requests":   c.SuggestRequests.Load(),
		"suggest_hits":       c.SuggestHits.Load(),
		"suggest_misses":     c.SuggestMisses.Load(),
		"cache_hits":         c.CacheHits.Load(),
		"cache_misses":       c.CacheMisses.Load(),
		"feedback_accepted":  c.FeedbackAccepted.Load(),
		"feedback_dismissed": c.FeedbackDismissed.Load(),
		"feedback_edited":    c.FeedbackEdited.Load(),
		"ingest_commands":    c.IngestCommands.Load(),
		"ingest_errors":      c.IngestErrors.Load(),
		"latency_sum_ms":     c.LatencySumMs.Load(),
	}
}

// Reset zeroes all counters. Useful for test isolation and periodic reporting.
func (c *Counters) Reset() {
	c.SuggestRequests.Store(0)
	c.SuggestHits.Store(0)
	c.SuggestMisses.Store(0)
	c.CacheHits.Store(0)
	c.CacheMisses.Store(0)
	c.FeedbackAccepted.Store(0)
	c.FeedbackDismissed.Store(0)
	c.FeedbackEdited.Store(0)
	c.IngestCommands.Store(0)
	c.IngestErrors.Store(0)
	c.LatencySumMs.Store(0)
}

// AverageSuggestLatencyMs returns the mean suggest latency in milliseconds.
// Returns 0 if no requests have been recorded.
func (c *Counters) AverageSuggestLatencyMs() float64 {
	reqs := c.SuggestRequests.Load()
	if reqs == 0 {
		return 0
	}
	return float64(c.LatencySumMs.Load()) / float64(reqs)
}

// CacheHitRate returns the cache hit rate as a fraction in [0, 1].
// Returns 0 if no cache lookups have been recorded.
func (c *Counters) CacheHitRate() float64 {
	hits := c.CacheHits.Load()
	misses := c.CacheMisses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}
