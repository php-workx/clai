package suggest

import (
	"time"
)

// L2Cache is the per-repo cold cache (Level 2).
// It provides fallback suggestions when L1 misses, keyed by repo key.
// Items are evicted via LRU when the count exceeds capacity.
type L2Cache struct {
	lru     *LRU[string, *l2Entry]
	ttl     time.Duration
	metrics *CacheMetrics
}

type l2Entry struct {
	suggestions []Suggestion
	createdAt   time.Time
}

// DefaultL2Capacity is the default max number of L2 cache entries.
const DefaultL2Capacity = 128

// NewL2Cache creates a new L2 cache.
func NewL2Cache(capacity int, ttl time.Duration, metrics *CacheMetrics) *L2Cache {
	if capacity <= 0 {
		capacity = DefaultL2Capacity
	}
	if metrics == nil {
		metrics = &CacheMetrics{}
	}
	return &L2Cache{
		lru:     NewLRU[string, *l2Entry](capacity, l2SizeFunc),
		ttl:     ttl,
		metrics: metrics,
	}
}

// l2SizeFunc estimates the memory size of an L2 cache entry.
func l2SizeFunc(_ string, entry *l2Entry) int64 {
	if entry == nil {
		return 0
	}
	return estimateSuggestionsSize(entry.suggestions)
}

// Get retrieves suggestions from L2 cache by repo key.
func (c *L2Cache) Get(repoKey string) ([]Suggestion, bool) {
	entry, ok := c.lru.Get(repoKey)
	if !ok {
		c.metrics.RecordMiss(CacheLayerL2)
		return nil, false
	}

	if time.Since(entry.createdAt) > c.ttl {
		c.lru.Delete(repoKey)
		c.metrics.RecordMiss(CacheLayerL2)
		return nil, false
	}

	c.metrics.RecordHit(CacheLayerL2)
	return entry.suggestions, true
}

// Set stores suggestions in L2 cache by repo key.
func (c *L2Cache) Set(repoKey string, suggestions []Suggestion) {
	c.lru.Put(repoKey, &l2Entry{
		suggestions: suggestions,
		createdAt:   time.Now(),
	})
}

// InvalidateRepo removes the cached entry for a specific repo.
func (c *L2Cache) InvalidateRepo(repoKey string) bool {
	return c.lru.Delete(repoKey)
}

// InvalidateAll clears the entire L2 cache.
func (c *L2Cache) InvalidateAll() {
	c.lru.Clear()
}

// Len returns the number of entries in L2.
func (c *L2Cache) Len() int {
	return c.lru.Len()
}

// MemorySize returns the estimated memory size in bytes.
func (c *L2Cache) MemorySize() int64 {
	return c.lru.Size()
}

// EvictToSize evicts entries until memory is at or below target bytes.
func (c *L2Cache) EvictToSize(targetBytes int64) int {
	return c.lru.EvictToSize(targetBytes)
}
