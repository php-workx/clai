package suggest

import (
	"crypto/sha256"
	"fmt"
	"time"
	"unsafe"
)

// L1Cache is the per-session hot cache (Level 1).
// It stores suggestions keyed by session+event+prefix for instant retrieval.
// Items are evicted via LRU when the count exceeds capacity.
type L1Cache struct {
	lru     *LRU[string, *l1Entry]
	metrics *CacheMetrics
	ttl     time.Duration
}

type l1Entry struct {
	createdAt   time.Time
	suggestions []Suggestion
}

// DefaultL1Capacity is the default max number of L1 cache entries.
const DefaultL1Capacity = 256

// NewL1Cache creates a new L1 cache.
func NewL1Cache(capacity int, ttl time.Duration, metrics *CacheMetrics) *L1Cache {
	if capacity <= 0 {
		capacity = DefaultL1Capacity
	}
	if metrics == nil {
		metrics = &CacheMetrics{}
	}
	return &L1Cache{
		lru:     NewLRU[string, *l1Entry](capacity, l1SizeFunc),
		ttl:     ttl,
		metrics: metrics,
	}
}

// l1SizeFunc estimates the memory size of an L1 cache entry.
func l1SizeFunc(_ string, entry *l1Entry) int64 {
	if entry == nil {
		return 0
	}
	return estimateSuggestionsSize(entry.suggestions)
}

// MakeL1Key creates a cache key for L1 lookup.
// Format: sessionID:lastEventID:prefixHash
func MakeL1Key(sessionID string, lastEventID int64, prefixHash string) string {
	return fmt.Sprintf("%s:%d:%s", sessionID, lastEventID, prefixHash)
}

// MakePrefixHash creates a short hash from context fields for cache key deduplication.
// Uses SHA256 truncated to 16 hex chars.
func MakePrefixHash(cwd, repoKey, branch string) string {
	h := sha256.New()
	h.Write([]byte(cwd))
	h.Write([]byte{0})
	h.Write([]byte(repoKey))
	h.Write([]byte{0})
	h.Write([]byte(branch))
	return fmt.Sprintf("%x", h.Sum(nil)[:8])
}

// Get retrieves suggestions from L1 cache.
// Returns suggestions and true on hit, nil and false on miss.
func (c *L1Cache) Get(key string) ([]Suggestion, bool) {
	entry, ok := c.lru.Get(key)
	if !ok {
		c.metrics.RecordMiss(CacheLayerL1)
		return nil, false
	}

	// Check TTL
	if time.Since(entry.createdAt) > c.ttl {
		c.lru.Delete(key)
		c.metrics.RecordMiss(CacheLayerL1)
		return nil, false
	}

	c.metrics.RecordHit(CacheLayerL1)
	return entry.suggestions, true
}

// Set stores suggestions in L1 cache.
func (c *L1Cache) Set(key string, suggestions []Suggestion) {
	c.lru.Put(key, &l1Entry{
		suggestions: suggestions,
		createdAt:   time.Now(),
	})
}

// InvalidateSession removes all entries for the given session ID.
// This is called on command_end to clear stale session suggestions.
func (c *L1Cache) InvalidateSession(sessionID string) int {
	return c.lru.DeleteFunc(func(key string, _ *l1Entry) bool {
		// Key format: sessionID:eventID:prefixHash
		// Check if key starts with sessionID:
		return len(key) > len(sessionID) && key[:len(sessionID)+1] == sessionID+":"
	})
}

// InvalidateAll clears the entire L1 cache.
func (c *L1Cache) InvalidateAll() {
	c.lru.Clear()
}

// Len returns the number of entries in L1.
func (c *L1Cache) Len() int {
	return c.lru.Len()
}

// MemorySize returns the estimated memory size in bytes.
func (c *L1Cache) MemorySize() int64 {
	return c.lru.Size()
}

// EvictToSize evicts entries until memory is at or below target bytes.
func (c *L1Cache) EvictToSize(targetBytes int64) int {
	return c.lru.EvictToSize(targetBytes)
}

// estimateSuggestionsSize estimates the memory footprint of a suggestion slice.
func estimateSuggestionsSize(suggestions []Suggestion) int64 {
	base := int64(unsafe.Sizeof(suggestions))
	for i := range suggestions {
		base += int64(unsafe.Sizeof(suggestions[i]))
		base += int64(len(suggestions[i].Command))
		for _, r := range suggestions[i].Reasons {
			base += int64(len(r))
		}
	}
	return base
}
