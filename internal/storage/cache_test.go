package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSQLiteStore_GetCached_Hit(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Set a cache entry
	entry := &CacheEntry{
		CacheKey:        "test-key-1",
		ResponseJSON:    `{"suggestions": ["ls", "pwd"]}`,
		Provider:        "anthropic",
		CreatedAtUnixMs: time.Now().UnixMilli(),
		ExpiresAtUnixMs: time.Now().Add(1 * time.Hour).UnixMilli(),
		HitCount:        0,
	}
	if err := store.SetCached(ctx, entry); err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Get the entry
	got, err := store.GetCached(ctx, "test-key-1")
	if err != nil {
		t.Fatalf("GetCached() error = %v", err)
	}

	if got.CacheKey != entry.CacheKey {
		t.Errorf("CacheKey = %s, want %s", got.CacheKey, entry.CacheKey)
	}
	if got.ResponseJSON != entry.ResponseJSON {
		t.Errorf("ResponseJSON = %s, want %s", got.ResponseJSON, entry.ResponseJSON)
	}
	if got.Provider != entry.Provider {
		t.Errorf("Provider = %s, want %s", got.Provider, entry.Provider)
	}
}

func TestSQLiteStore_GetCached_Miss(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetCached(context.Background(), "nonexistent-key")
	if !errors.Is(err, ErrCacheNotFound) {
		t.Errorf("GetCached() error = %v, want ErrCacheNotFound", err)
	}
}

func TestSQLiteStore_GetCached_Expired(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Set an expired cache entry
	entry := &CacheEntry{
		CacheKey:        "expired-key",
		ResponseJSON:    `{"data": "old"}`,
		Provider:        "openai",
		CreatedAtUnixMs: time.Now().Add(-2 * time.Hour).UnixMilli(),
		ExpiresAtUnixMs: time.Now().Add(-1 * time.Hour).UnixMilli(), // Expired 1 hour ago
		HitCount:        0,
	}
	if err := store.SetCached(ctx, entry); err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Should not find expired entry
	_, err := store.GetCached(ctx, "expired-key")
	if !errors.Is(err, ErrCacheNotFound) {
		t.Errorf("GetCached() error = %v, want ErrCacheNotFound for expired entry", err)
	}
}

func TestSQLiteStore_GetCached_EmptyKey(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetCached(context.Background(), "")
	if err == nil {
		t.Error("Expected error for empty cache key")
	}
}

func TestSQLiteStore_GetCached_IncrementsHitCount(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Set a cache entry
	entry := &CacheEntry{
		CacheKey:        "hit-count-key",
		ResponseJSON:    `{"data": "test"}`,
		Provider:        "anthropic",
		CreatedAtUnixMs: time.Now().UnixMilli(),
		ExpiresAtUnixMs: time.Now().Add(1 * time.Hour).UnixMilli(),
		HitCount:        0,
	}
	if err := store.SetCached(ctx, entry); err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Get multiple times
	for i := 0; i < 3; i++ {
		_, err := store.GetCached(ctx, "hit-count-key")
		if err != nil {
			t.Fatalf("GetCached() error = %v", err)
		}
	}

	// Give async update time to complete
	time.Sleep(100 * time.Millisecond)

	// Check hit count directly
	var hitCount int64
	err := store.DB().QueryRowContext(ctx,
		"SELECT hit_count FROM ai_cache WHERE cache_key = ?",
		"hit-count-key").Scan(&hitCount)
	if err != nil {
		t.Fatalf("Failed to query hit count: %v", err)
	}

	if hitCount < 3 {
		t.Errorf("hit_count = %d, want >= 3", hitCount)
	}
}

func TestSQLiteStore_SetCached_Success(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	entry := &CacheEntry{
		CacheKey:        "set-test-key",
		ResponseJSON:    `{"result": "success"}`,
		Provider:        "google",
		CreatedAtUnixMs: time.Now().UnixMilli(),
		ExpiresAtUnixMs: time.Now().Add(24 * time.Hour).UnixMilli(),
		HitCount:        5,
	}

	err := store.SetCached(ctx, entry)
	if err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Verify by reading back
	got, err := store.GetCached(ctx, "set-test-key")
	if err != nil {
		t.Fatalf("GetCached() error = %v", err)
	}

	if got.HitCount != 5 {
		t.Errorf("HitCount = %d, want 5", got.HitCount)
	}
}

func TestSQLiteStore_SetCached_DefaultTTL(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	entry := &CacheEntry{
		CacheKey:     "default-ttl-key",
		ResponseJSON: `{"data": "test"}`,
		Provider:     "anthropic",
		// CreatedAtUnixMs and ExpiresAtUnixMs not set - should use defaults
	}

	err := store.SetCached(ctx, entry)
	if err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Verify defaults were set
	if entry.CreatedAtUnixMs == 0 {
		t.Error("CreatedAtUnixMs was not set")
	}
	if entry.ExpiresAtUnixMs == 0 {
		t.Error("ExpiresAtUnixMs was not set")
	}

	// Verify TTL is approximately 24 hours
	ttl := entry.ExpiresAtUnixMs - entry.CreatedAtUnixMs
	expected := int64(24 * 60 * 60 * 1000) // 24 hours in ms
	if ttl != expected {
		t.Errorf("TTL = %d ms, want %d ms", ttl, expected)
	}
}

func TestSQLiteStore_SetCached_Upsert(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Set initial entry
	entry1 := &CacheEntry{
		CacheKey:        "upsert-key",
		ResponseJSON:    `{"version": 1}`,
		Provider:        "anthropic",
		CreatedAtUnixMs: time.Now().UnixMilli(),
		ExpiresAtUnixMs: time.Now().Add(1 * time.Hour).UnixMilli(),
	}
	if err := store.SetCached(ctx, entry1); err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Update with same key
	entry2 := &CacheEntry{
		CacheKey:        "upsert-key",
		ResponseJSON:    `{"version": 2}`,
		Provider:        "openai", // Different provider
		CreatedAtUnixMs: time.Now().UnixMilli(),
		ExpiresAtUnixMs: time.Now().Add(2 * time.Hour).UnixMilli(),
	}
	if err := store.SetCached(ctx, entry2); err != nil {
		t.Fatalf("SetCached() error = %v", err)
	}

	// Verify update
	got, err := store.GetCached(ctx, "upsert-key")
	if err != nil {
		t.Fatalf("GetCached() error = %v", err)
	}

	if got.ResponseJSON != `{"version": 2}` {
		t.Errorf("ResponseJSON = %s, want version 2", got.ResponseJSON)
	}
	if got.Provider != "openai" {
		t.Errorf("Provider = %s, want openai", got.Provider)
	}
}

func TestSQLiteStore_SetCached_Validation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	tests := []struct {
		name    string
		entry   *CacheEntry
		wantErr string
	}{
		{
			name:    "nil entry",
			entry:   nil,
			wantErr: "cache entry cannot be nil",
		},
		{
			name: "missing cache_key",
			entry: &CacheEntry{
				ResponseJSON: `{}`,
				Provider:     "test",
			},
			wantErr: "cache_key is required",
		},
		{
			name: "missing response_json",
			entry: &CacheEntry{
				CacheKey: "test",
				Provider: "test",
			},
			wantErr: "response_json is required",
		},
		{
			name: "missing provider",
			entry: &CacheEntry{
				CacheKey:     "test",
				ResponseJSON: `{}`,
			},
			wantErr: "provider is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.SetCached(context.Background(), tt.entry)
			if err == nil {
				t.Error("Expected error, got nil")
				return
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want containing %s", err, tt.wantErr)
			}
		})
	}
}

func TestSQLiteStore_PruneExpiredCache_RemovesExpired(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create mix of expired and valid entries
	entries := []struct {
		key     string
		expired bool
	}{
		{"expired-1", true},
		{"expired-2", true},
		{"valid-1", false},
		{"valid-2", false},
		{"expired-3", true},
	}

	now := time.Now().UnixMilli()
	for _, e := range entries {
		var expiresAt int64
		if e.expired {
			expiresAt = now - 1000 // Expired 1 second ago
		} else {
			expiresAt = now + 3600000 // Expires in 1 hour
		}

		entry := &CacheEntry{
			CacheKey:        e.key,
			ResponseJSON:    `{}`,
			Provider:        "test",
			CreatedAtUnixMs: now - 7200000, // Created 2 hours ago
			ExpiresAtUnixMs: expiresAt,
		}
		if err := store.SetCached(ctx, entry); err != nil {
			t.Fatalf("SetCached() error = %v", err)
		}
	}

	// Prune expired
	removed, err := store.PruneExpiredCache(ctx)
	if err != nil {
		t.Fatalf("PruneExpiredCache() error = %v", err)
	}

	if removed != 3 {
		t.Errorf("Removed %d entries, want 3", removed)
	}

	// Verify only valid entries remain
	for _, e := range entries {
		_, err := store.GetCached(ctx, e.key)
		if e.expired {
			if !errors.Is(err, ErrCacheNotFound) {
				t.Errorf("Expected expired entry %s to be removed", e.key)
			}
		} else {
			if err != nil {
				t.Errorf("Expected valid entry %s to remain: %v", e.key, err)
			}
		}
	}
}

func TestSQLiteStore_PruneExpiredCache_NoExpired(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create only valid entries
	for i := 0; i < 3; i++ {
		entry := &CacheEntry{
			CacheKey:        generateTestCacheKey(i),
			ResponseJSON:    `{}`,
			Provider:        "test",
			CreatedAtUnixMs: time.Now().UnixMilli(),
			ExpiresAtUnixMs: time.Now().Add(1 * time.Hour).UnixMilli(),
		}
		if err := store.SetCached(ctx, entry); err != nil {
			t.Fatalf("SetCached() error = %v", err)
		}
	}

	removed, err := store.PruneExpiredCache(ctx)
	if err != nil {
		t.Fatalf("PruneExpiredCache() error = %v", err)
	}

	if removed != 0 {
		t.Errorf("Removed %d entries, want 0", removed)
	}
}

func TestSQLiteStore_GetCacheStats(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create entries with different states
	now := time.Now().UnixMilli()
	entries := []struct {
		key       string
		hitCount  int64
		expiresAt int64
	}{
		{"stats-1", 10, now + 3600000}, // Valid, 10 hits
		{"stats-2", 5, now + 3600000},  // Valid, 5 hits
		{"stats-3", 3, now - 1000},     // Expired, 3 hits
		{"stats-4", 0, now + 3600000},  // Valid, 0 hits
	}

	for _, e := range entries {
		entry := &CacheEntry{
			CacheKey:        e.key,
			ResponseJSON:    `{}`,
			Provider:        "test",
			CreatedAtUnixMs: now - 7200000,
			ExpiresAtUnixMs: e.expiresAt,
			HitCount:        e.hitCount,
		}
		if err := store.SetCached(ctx, entry); err != nil {
			t.Fatalf("SetCached() error = %v", err)
		}
	}

	stats, err := store.GetCacheStats(ctx)
	if err != nil {
		t.Fatalf("GetCacheStats() error = %v", err)
	}

	if stats.TotalEntries != 4 {
		t.Errorf("TotalEntries = %d, want 4", stats.TotalEntries)
	}
	if stats.ExpiredEntries != 1 {
		t.Errorf("ExpiredEntries = %d, want 1", stats.ExpiredEntries)
	}
	if stats.TotalHits != 18 { // 10 + 5 + 3 + 0
		t.Errorf("TotalHits = %d, want 18", stats.TotalHits)
	}
}

func generateTestCacheKey(n int) string {
	return "cache-key-" + string(rune('A'+n))
}
