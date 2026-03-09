package suggest

import (
	"sync"
	"time"
)

// Default cache configuration.
const (
	// DefaultCacheTTL is the default TTL for cached suggestions.
	// Per spec Section 11.2.3: 30 seconds.
	DefaultCacheTTL = 30 * time.Second

	// MaxCacheTTL is the maximum allowed TTL.
	MaxCacheTTL = 5 * time.Minute

	// MinCacheTTL is the minimum allowed TTL.
	MinCacheTTL = 1 * time.Second
)

// CacheEntry represents a cached set of suggestions for a session.
// Per spec Section 11.2.1.
type CacheEntry struct {
	ComputedAt  time.Time
	Suggestions []Suggestion
	TTL         time.Duration
	LastEventID int64
}

// IsExpired returns true if the cache entry has expired.
func (e *CacheEntry) IsExpired(now time.Time) bool {
	return now.After(e.ComputedAt.Add(e.TTL))
}

// Cache provides pre-computed suggestion caching per session.
// Per spec Section 11.2: "zero-latency suggestions via pre-computation cache".
type Cache struct {
	entries map[string]*CacheEntry
	ttl     time.Duration
	mu      sync.RWMutex
}

// CacheConfig configures the suggestion cache.
type CacheConfig struct {
	// TTL is the time-to-live for cached entries.
	// Default: 30 seconds.
	TTL time.Duration
}

// DefaultCacheConfig returns the default cache configuration.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		TTL: DefaultCacheTTL,
	}
}

// NewCache creates a new suggestion cache.
func NewCache(cfg CacheConfig) *Cache {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	if ttl > MaxCacheTTL {
		ttl = MaxCacheTTL
	}
	if ttl < MinCacheTTL {
		ttl = MinCacheTTL
	}

	return &Cache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}
}

// Get returns cached suggestions for a session if they exist and are valid.
// Per spec Section 11.2.3: return cached suggestions if cache exists,
// matches most recent session event, and is not expired.
func (c *Cache) Get(sessionID string, lastEventID int64) ([]Suggestion, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[sessionID]
	if !ok {
		return nil, false
	}

	// Check if cache is expired
	if entry.IsExpired(time.Now()) {
		return nil, false
	}

	// Check if cache matches the most recent event
	if entry.LastEventID != lastEventID {
		return nil, false
	}

	return entry.Suggestions, true
}

// GetAny returns cached suggestions without checking event ID.
// Useful for "cold session" fallback.
func (c *Cache) GetAny(sessionID string) ([]Suggestion, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[sessionID]
	if !ok {
		return nil, false
	}

	// Check if cache is expired
	if entry.IsExpired(time.Now()) {
		return nil, false
	}

	return entry.Suggestions, true
}

// Set stores suggestions for a session.
// Per spec Section 11.2.2: called on command_end ingest.
func (c *Cache) Set(sessionID string, lastEventID int64, suggestions []Suggestion) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[sessionID] = &CacheEntry{
		Suggestions: suggestions,
		LastEventID: lastEventID,
		ComputedAt:  time.Now(),
		TTL:         c.ttl,
	}
}

// Invalidate removes the cache entry for a session.
func (c *Cache) Invalidate(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, sessionID)
}

// InvalidateAll removes all cache entries.
func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
}

// Cleanup removes expired entries from the cache.
// Should be called periodically (e.g., every minute).
func (c *Cache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	count := 0

	for sessionID, entry := range c.entries {
		if entry.IsExpired(now) {
			delete(c.entries, sessionID)
			count++
		}
	}

	return count
}

// Size returns the number of cached entries.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// TTL returns the configured TTL.
func (c *Cache) TTL() time.Duration {
	return c.ttl
}
