package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrCacheNotFound is returned when a cache entry is not found.
var ErrCacheNotFound = errors.New("cache entry not found")

// GetCached retrieves a cached entry by key.
// Returns ErrCacheNotFound if the entry doesn't exist.
// Expired entries are treated as not found and are not returned.
// If found, increments the hit count.
func (s *SQLiteStore) GetCached(ctx context.Context, key string) (*CacheEntry, error) {
	if key == "" {
		return nil, errors.New("cache key is required")
	}

	now := time.Now().UnixMilli()

	row := s.db.QueryRowContext(ctx, `
		SELECT cache_key, response_json, provider, created_at_unix_ms,
		       expires_at_unix_ms, hit_count
		FROM ai_cache
		WHERE cache_key = ? AND expires_at_unix_ms > ?
	`, key, now)

	var entry CacheEntry
	err := row.Scan(
		&entry.CacheKey,
		&entry.ResponseJSON,
		&entry.Provider,
		&entry.CreatedAtUnixMs,
		&entry.ExpiresAtUnixMs,
		&entry.HitCount,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCacheNotFound
		}
		return nil, fmt.Errorf("failed to get cache entry: %w", err)
	}

	// Increment hit count synchronously (best-effort, ignore errors)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE ai_cache SET hit_count = hit_count + 1 WHERE cache_key = ?
	`, key)

	return &entry, nil
}

// SetCached stores or updates a cache entry.
func (s *SQLiteStore) SetCached(ctx context.Context, entry *CacheEntry) error {
	if entry == nil {
		return errors.New("cache entry cannot be nil")
	}
	if entry.CacheKey == "" {
		return errors.New("cache_key is required")
	}
	if entry.ResponseJSON == "" {
		return errors.New("response_json is required")
	}
	if entry.Provider == "" {
		return errors.New("provider is required")
	}

	// Set defaults if not provided
	if entry.CreatedAtUnixMs == 0 {
		entry.CreatedAtUnixMs = time.Now().UnixMilli()
	}
	if entry.ExpiresAtUnixMs == 0 {
		// Default TTL: 24 hours
		entry.ExpiresAtUnixMs = entry.CreatedAtUnixMs + (24 * time.Hour).Milliseconds()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO ai_cache (
			cache_key, response_json, provider,
			created_at_unix_ms, expires_at_unix_ms, hit_count
		) VALUES (?, ?, ?, ?, ?, ?)
	`,
		entry.CacheKey,
		entry.ResponseJSON,
		entry.Provider,
		entry.CreatedAtUnixMs,
		entry.ExpiresAtUnixMs,
		entry.HitCount,
	)
	if err != nil {
		return fmt.Errorf("failed to set cache entry: %w", err)
	}

	return nil
}

// PruneExpiredCache removes all expired cache entries.
// Returns the number of entries removed.
func (s *SQLiteStore) PruneExpiredCache(ctx context.Context) (int64, error) {
	now := time.Now().UnixMilli()

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM ai_cache WHERE expires_at_unix_ms < ?
	`, now)
	if err != nil {
		return 0, fmt.Errorf("failed to prune cache: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rows, nil
}

// GetCacheStats returns statistics about the cache.
type CacheStats struct {
	TotalEntries   int64
	ExpiredEntries int64
	TotalHits      int64
}

// GetCacheStats retrieves cache statistics.
func (s *SQLiteStore) GetCacheStats(ctx context.Context) (*CacheStats, error) {
	now := time.Now().UnixMilli()

	var stats CacheStats

	// Get total entries and total hits
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(hit_count), 0) FROM ai_cache
	`)
	if err := row.Scan(&stats.TotalEntries, &stats.TotalHits); err != nil {
		return nil, fmt.Errorf("failed to get cache stats: %w", err)
	}

	// Get expired entries
	row = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ai_cache WHERE expires_at_unix_ms < ?
	`, now)
	if err := row.Scan(&stats.ExpiredEntries); err != nil {
		return nil, fmt.Errorf("failed to get expired count: %w", err)
	}

	return &stats, nil
}
