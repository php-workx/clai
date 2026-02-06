package suggest

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// L3Cache is the SQLite persistent cache (Level 3).
// It stores suggestions in the suggestion_cache table defined in the V2 schema.
// L3 is the slowest layer but survives process restarts.
type L3Cache struct {
	db      *sql.DB
	ttl     time.Duration
	metrics *CacheMetrics
}

// NewL3Cache creates a new L3 cache backed by SQLite.
func NewL3Cache(db *sql.DB, ttl time.Duration, metrics *CacheMetrics) *L3Cache {
	if metrics == nil {
		metrics = &CacheMetrics{}
	}
	return &L3Cache{
		db:      db,
		ttl:     ttl,
		metrics: metrics,
	}
}

// MakeL3CacheKey creates a composite key for L3 lookup.
func MakeL3CacheKey(sessionID, contextHash string) string {
	return sessionID + ":" + contextHash
}

// Get retrieves suggestions from the L3 SQLite cache.
// It checks TTL and increments hit_count asynchronously.
func (c *L3Cache) Get(ctx context.Context, cacheKey string) ([]Suggestion, bool) {
	if c.db == nil {
		c.metrics.RecordMiss(CacheLayerL3)
		return nil, false
	}

	var suggestionsJSON string
	var createdMs, ttlMs int64

	err := c.db.QueryRowContext(ctx,
		`SELECT suggestions_json, created_ms, ttl_ms FROM suggestion_cache WHERE cache_key = ?`,
		cacheKey,
	).Scan(&suggestionsJSON, &createdMs, &ttlMs)

	if err != nil {
		c.metrics.RecordMiss(CacheLayerL3)
		return nil, false
	}

	// Check TTL
	nowMs := time.Now().UnixMilli()
	if nowMs-createdMs > ttlMs {
		c.metrics.RecordMiss(CacheLayerL3)
		// Clean up expired entry synchronously (low cost for single row)
		_, _ = c.db.ExecContext(ctx, `DELETE FROM suggestion_cache WHERE cache_key = ?`, cacheKey)
		return nil, false
	}

	var suggestions []Suggestion
	if err := json.Unmarshal([]byte(suggestionsJSON), &suggestions); err != nil {
		c.metrics.RecordMiss(CacheLayerL3)
		return nil, false
	}

	// Increment hit_count synchronously (low cost for single row update)
	_, _ = c.db.ExecContext(ctx, `UPDATE suggestion_cache SET hit_count = hit_count + 1 WHERE cache_key = ?`, cacheKey)

	c.metrics.RecordHit(CacheLayerL3)
	return suggestions, true
}

// Set stores suggestions in the L3 SQLite cache.
func (c *L3Cache) Set(ctx context.Context, cacheKey, sessionID, contextHash string, suggestions []Suggestion) error {
	if c.db == nil {
		return nil
	}

	data, err := json.Marshal(suggestions)
	if err != nil {
		return err
	}

	nowMs := time.Now().UnixMilli()
	ttlMs := c.ttl.Milliseconds()

	_, err = c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO suggestion_cache (cache_key, session_id, context_hash, suggestions_json, created_ms, ttl_ms, hit_count)
		 VALUES (?, ?, ?, ?, ?, ?, 0)`,
		cacheKey, sessionID, contextHash, string(data), nowMs, ttlMs,
	)
	return err
}

// Invalidate removes a specific cache entry.
func (c *L3Cache) Invalidate(ctx context.Context, cacheKey string) error {
	if c.db == nil {
		return nil
	}
	_, err := c.db.ExecContext(ctx, `DELETE FROM suggestion_cache WHERE cache_key = ?`, cacheKey)
	return err
}

// InvalidateSession removes all cache entries for a session.
func (c *L3Cache) InvalidateSession(ctx context.Context, sessionID string) error {
	if c.db == nil {
		return nil
	}
	_, err := c.db.ExecContext(ctx, `DELETE FROM suggestion_cache WHERE session_id = ?`, sessionID)
	return err
}

// Cleanup removes expired entries from the suggestion_cache table.
// Returns the number of entries removed.
func (c *L3Cache) Cleanup(ctx context.Context) (int64, error) {
	if c.db == nil {
		return 0, nil
	}

	nowMs := time.Now().UnixMilli()
	result, err := c.db.ExecContext(ctx,
		`DELETE FROM suggestion_cache WHERE (created_ms + ttl_ms) < ?`,
		nowMs,
	)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Size returns the number of entries in the L3 cache.
func (c *L3Cache) Size(ctx context.Context) (int64, error) {
	if c.db == nil {
		return 0, nil
	}

	var count int64
	err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM suggestion_cache`).Scan(&count)
	return count, err
}
