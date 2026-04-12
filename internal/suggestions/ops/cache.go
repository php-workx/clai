package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// CacheEntry represents a cached AI response.
type CacheEntry struct {
	CacheKey     string
	ResponseJSON string
	Provider     string
	CreatedAtMs  int64
	ExpiresAtMs  int64
	HitCount     int64
}

// ErrCacheNotFound is returned when a cache entry is not found.
var ErrCacheNotFound = errors.New("cache entry not found")

// GetCached retrieves a cached entry by key.
func GetCached(ctx context.Context, db *suggestdb.DB, key string) (*CacheEntry, error) {
	if key == "" {
		return nil, errors.New("cache key is required")
	}

	now := time.Now().UnixMilli()
	row := db.QueryRowContext(ctx, `
		SELECT cache_key, response_json, provider, created_at_ms,
		       expires_at_ms, hit_count
		FROM ai_cache
		WHERE cache_key = ? AND expires_at_ms > ?
	`, key, now)

	var entry CacheEntry
	err := row.Scan(
		&entry.CacheKey, &entry.ResponseJSON, &entry.Provider,
		&entry.CreatedAtMs, &entry.ExpiresAtMs, &entry.HitCount,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCacheNotFound
		}
		return nil, fmt.Errorf("failed to get cache entry: %w", err)
	}

	// Increment hit count (best-effort)
	_, _ = db.ExecContext(ctx, `UPDATE ai_cache SET hit_count = hit_count + 1 WHERE cache_key = ?`, key)

	return &entry, nil
}

// SetCached stores or updates a cache entry.
func SetCached(ctx context.Context, db *suggestdb.DB, entry *CacheEntry) error {
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

	if entry.CreatedAtMs == 0 {
		entry.CreatedAtMs = time.Now().UnixMilli()
	}
	if entry.ExpiresAtMs == 0 {
		entry.ExpiresAtMs = entry.CreatedAtMs + (24 * time.Hour).Milliseconds()
	}

	_, err := db.ExecContext(ctx, `
		INSERT OR REPLACE INTO ai_cache (
			cache_key, response_json, provider,
			created_at_ms, expires_at_ms, hit_count
		) VALUES (?, ?, ?, ?, ?, ?)
	`, entry.CacheKey, entry.ResponseJSON, entry.Provider,
		entry.CreatedAtMs, entry.ExpiresAtMs, entry.HitCount)
	if err != nil {
		return fmt.Errorf("failed to set cache entry: %w", err)
	}
	return nil
}

// PruneExpiredCache removes all expired cache entries.
func PruneExpiredCache(ctx context.Context, db *suggestdb.DB) (int64, error) {
	now := time.Now().UnixMilli()
	result, err := db.ExecContext(ctx, `DELETE FROM ai_cache WHERE expires_at_ms < ?`, now)
	if err != nil {
		return 0, fmt.Errorf("failed to prune cache: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rows, nil
}
