package suggest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_NewCache(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())
	assert.NotNil(t, cache)
	assert.Equal(t, DefaultCacheTTL, cache.TTL())
}

func TestCache_NewCache_CustomTTL(t *testing.T) {
	t.Parallel()

	cache := NewCache(CacheConfig{TTL: 10 * time.Second})
	assert.Equal(t, 10*time.Second, cache.TTL())
}

func TestCache_NewCache_ClampsTTL(t *testing.T) {
	t.Parallel()

	// Too small
	cache := NewCache(CacheConfig{TTL: 100 * time.Millisecond})
	assert.Equal(t, MinCacheTTL, cache.TTL())

	// Too large
	cache = NewCache(CacheConfig{TTL: 10 * time.Minute})
	assert.Equal(t, MaxCacheTTL, cache.TTL())

	// Zero defaults
	cache = NewCache(CacheConfig{TTL: 0})
	assert.Equal(t, DefaultCacheTTL, cache.TTL())
}

func TestCache_SetGet(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	suggestions := []Suggestion{
		{Command: "git status", Score: 100},
		{Command: "git commit", Score: 80},
	}

	cache.Set("session1", 42, suggestions)

	// Get with matching event ID
	result, ok := cache.Get("session1", 42)
	require.True(t, ok)
	assert.Equal(t, suggestions, result)
}

func TestCache_Get_NotFound(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	result, ok := cache.Get("nonexistent", 1)
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestCache_Get_EventIDMismatch(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	suggestions := []Suggestion{
		{Command: "git status", Score: 100},
	}

	cache.Set("session1", 42, suggestions)

	// Different event ID should not match
	result, ok := cache.Get("session1", 43)
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestCache_Get_Expired(t *testing.T) {
	t.Parallel()

	// Use very short TTL for testing
	cache := NewCache(CacheConfig{TTL: 1 * time.Second})

	suggestions := []Suggestion{
		{Command: "git status", Score: 100},
	}

	cache.Set("session1", 42, suggestions)

	// Should be valid immediately
	result, ok := cache.Get("session1", 42)
	require.True(t, ok)
	assert.NotEmpty(t, result)

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Should be expired now
	result, ok = cache.Get("session1", 42)
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestCache_GetAny(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	suggestions := []Suggestion{
		{Command: "git status", Score: 100},
	}

	cache.Set("session1", 42, suggestions)

	// GetAny doesn't check event ID
	result, ok := cache.GetAny("session1")
	require.True(t, ok)
	assert.Equal(t, suggestions, result)
}

func TestCache_GetAny_Expired(t *testing.T) {
	t.Parallel()

	cache := NewCache(CacheConfig{TTL: 1 * time.Second})

	suggestions := []Suggestion{
		{Command: "git status", Score: 100},
	}

	cache.Set("session1", 42, suggestions)

	time.Sleep(1100 * time.Millisecond)

	result, ok := cache.GetAny("session1")
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestCache_Invalidate(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	cache.Set("session1", 42, []Suggestion{{Command: "git status"}})
	cache.Set("session2", 43, []Suggestion{{Command: "git commit"}})

	cache.Invalidate("session1")

	_, ok := cache.Get("session1", 42)
	assert.False(t, ok)

	// session2 should still be valid
	result, ok := cache.Get("session2", 43)
	assert.True(t, ok)
	assert.NotEmpty(t, result)
}

func TestCache_InvalidateAll(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	cache.Set("session1", 42, []Suggestion{{Command: "git status"}})
	cache.Set("session2", 43, []Suggestion{{Command: "git commit"}})

	cache.InvalidateAll()

	assert.Equal(t, 0, cache.Size())
}

func TestCache_Cleanup(t *testing.T) {
	t.Parallel()

	cache := NewCache(CacheConfig{TTL: 1 * time.Second})

	cache.Set("session1", 42, []Suggestion{{Command: "git status"}})
	cache.Set("session2", 43, []Suggestion{{Command: "git commit"}})

	assert.Equal(t, 2, cache.Size())

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Add a fresh entry
	cache.Set("session3", 44, []Suggestion{{Command: "git push"}})

	// Cleanup should remove the expired entries
	removed := cache.Cleanup()
	assert.Equal(t, 2, removed)
	assert.Equal(t, 1, cache.Size())

	// The fresh entry should still be valid
	result, ok := cache.Get("session3", 44)
	assert.True(t, ok)
	assert.NotEmpty(t, result)
}

func TestCache_Size(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())
	assert.Equal(t, 0, cache.Size())

	cache.Set("session1", 1, []Suggestion{})
	assert.Equal(t, 1, cache.Size())

	cache.Set("session2", 2, []Suggestion{})
	assert.Equal(t, 2, cache.Size())

	cache.Invalidate("session1")
	assert.Equal(t, 1, cache.Size())
}

func TestCache_OverwritesSameSession(t *testing.T) {
	t.Parallel()

	cache := NewCache(DefaultCacheConfig())

	cache.Set("session1", 42, []Suggestion{{Command: "git status"}})
	cache.Set("session1", 43, []Suggestion{{Command: "git commit"}})

	assert.Equal(t, 1, cache.Size())

	// Should return the new value
	result, ok := cache.Get("session1", 43)
	require.True(t, ok)
	assert.Equal(t, "git commit", result[0].Command)

	// Old event ID should not match
	_, ok = cache.Get("session1", 42)
	assert.False(t, ok)
}

func TestCacheEntry_IsExpired(t *testing.T) {
	t.Parallel()

	entry := &CacheEntry{
		ComputedAt: time.Now(),
		TTL:        1 * time.Second,
	}

	assert.False(t, entry.IsExpired(time.Now()))
	assert.True(t, entry.IsExpired(time.Now().Add(2*time.Second)))
}

func TestCacheConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 30*time.Second, DefaultCacheTTL)
	assert.Equal(t, 5*time.Minute, MaxCacheTTL)
	assert.Equal(t, 1*time.Second, MinCacheTTL)
}

func TestDefaultCacheConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultCacheConfig()
	assert.Equal(t, DefaultCacheTTL, cfg.TTL)
}
