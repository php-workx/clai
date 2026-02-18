package suggest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS suggestion_cache (
		cache_key         TEXT PRIMARY KEY,
		session_id        TEXT NOT NULL,
		context_hash      TEXT NOT NULL,
		suggestions_json  TEXT NOT NULL,
		created_ms        INTEGER NOT NULL,
		ttl_ms            INTEGER NOT NULL,
		hit_count         INTEGER NOT NULL DEFAULT 0
	)`)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })
	return db
}

func TestL3Cache_BasicGetSet(t *testing.T) {
	db := setupTestDB(t)
	metrics := &CacheMetrics{}
	cache := NewL3Cache(db, 30*time.Second, metrics)
	ctx := context.Background()

	suggestions := []Suggestion{
		{Command: "git status", Score: 0.9, Confidence: 0.8, Reasons: []string{"freq"}},
		{Command: "git diff", Score: 0.7, Confidence: 0.6, Reasons: []string{"transition"}},
	}

	cacheKey := MakeL3CacheKey("sess1", "ctxhash1")
	err := cache.Set(ctx, cacheKey, "sess1", "ctxhash1", suggestions)
	require.NoError(t, err)

	got, ok := cache.Get(ctx, cacheKey)
	require.True(t, ok)
	assert.Equal(t, 2, len(got))
	assert.Equal(t, "git status", got[0].Command)
	assert.InDelta(t, 0.9, got[0].Score, 0.001)
}

func TestL3Cache_Miss(t *testing.T) {
	db := setupTestDB(t)
	metrics := &CacheMetrics{}
	cache := NewL3Cache(db, 30*time.Second, metrics)

	got, ok := cache.Get(context.Background(), "nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)

	snap := metrics.Snapshot()
	assert.Equal(t, int64(1), snap.L3.Misses)
}

func TestL3Cache_TTLExpiry(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 1*time.Millisecond, nil)
	ctx := context.Background()

	cacheKey := MakeL3CacheKey("sess1", "ctx1")
	err := cache.Set(ctx, cacheKey, "sess1", "ctx1", []Suggestion{{Command: "ls"}})
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)

	got, ok := cache.Get(ctx, cacheKey)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestL3Cache_Invalidate(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 30*time.Second, nil)
	ctx := context.Background()

	cacheKey := MakeL3CacheKey("sess1", "ctx1")
	_ = cache.Set(ctx, cacheKey, "sess1", "ctx1", []Suggestion{{Command: "ls"}})

	err := cache.Invalidate(ctx, cacheKey)
	require.NoError(t, err)

	_, ok := cache.Get(ctx, cacheKey)
	assert.False(t, ok)
}

func TestL3Cache_InvalidateSession(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 30*time.Second, nil)
	ctx := context.Background()

	_ = cache.Set(ctx, MakeL3CacheKey("sess1", "c1"), "sess1", "c1", []Suggestion{{Command: "a"}})
	_ = cache.Set(ctx, MakeL3CacheKey("sess1", "c2"), "sess1", "c2", []Suggestion{{Command: "b"}})
	_ = cache.Set(ctx, MakeL3CacheKey("sess2", "c3"), "sess2", "c3", []Suggestion{{Command: "c"}})

	err := cache.InvalidateSession(ctx, "sess1")
	require.NoError(t, err)

	size, err := cache.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), size)

	// sess2 should still exist
	_, ok := cache.Get(ctx, MakeL3CacheKey("sess2", "c3"))
	assert.True(t, ok)
}

func TestL3Cache_Cleanup(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 1*time.Millisecond, nil)
	ctx := context.Background()

	_ = cache.Set(ctx, "k1", "s1", "c1", []Suggestion{{Command: "a"}})
	_ = cache.Set(ctx, "k2", "s1", "c2", []Suggestion{{Command: "b"}})

	time.Sleep(5 * time.Millisecond)

	// Add a non-expired entry
	cache2 := NewL3Cache(db, 30*time.Second, nil)
	_ = cache2.Set(ctx, "k3", "s1", "c3", []Suggestion{{Command: "c"}})

	removed, err := cache.Cleanup(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)

	size, err := cache.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), size)
}

func TestL3Cache_Size(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 30*time.Second, nil)
	ctx := context.Background()

	size, err := cache.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)

	_ = cache.Set(ctx, "k1", "s1", "c1", []Suggestion{{Command: "a"}})
	_ = cache.Set(ctx, "k2", "s1", "c2", []Suggestion{{Command: "b"}})

	size, err = cache.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), size)
}

func TestL3Cache_NilDB(t *testing.T) {
	cache := NewL3Cache(nil, 30*time.Second, nil)
	ctx := context.Background()

	got, ok := cache.Get(ctx, "key")
	assert.False(t, ok)
	assert.Nil(t, got)

	err := cache.Set(ctx, "key", "sess", "ctx", []Suggestion{{Command: "ls"}})
	assert.NoError(t, err)

	err = cache.Invalidate(ctx, "key")
	assert.NoError(t, err)

	err = cache.InvalidateSession(ctx, "sess")
	assert.NoError(t, err)

	removed, err := cache.Cleanup(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), removed)

	size, err := cache.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestL3Cache_ReplaceExisting(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 30*time.Second, nil)
	ctx := context.Background()

	cacheKey := MakeL3CacheKey("sess1", "ctx1")
	_ = cache.Set(ctx, cacheKey, "sess1", "ctx1", []Suggestion{{Command: "old"}})
	_ = cache.Set(ctx, cacheKey, "sess1", "ctx1", []Suggestion{{Command: "new"}})

	got, ok := cache.Get(ctx, cacheKey)
	require.True(t, ok)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "new", got[0].Command)

	size, err := cache.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), size, "replace should not create duplicate")
}

func TestL3Cache_MetricsTracking(t *testing.T) {
	db := setupTestDB(t)
	metrics := &CacheMetrics{}
	cache := NewL3Cache(db, 30*time.Second, metrics)
	ctx := context.Background()

	cacheKey := MakeL3CacheKey("sess1", "ctx1")
	_ = cache.Set(ctx, cacheKey, "sess1", "ctx1", []Suggestion{{Command: "ls"}})

	// Hit
	cache.Get(ctx, cacheKey)
	// Miss
	cache.Get(ctx, "nonexistent")

	snap := metrics.Snapshot()
	assert.Equal(t, int64(1), snap.L3.Hits)
	assert.Equal(t, int64(1), snap.L3.Misses)
}

func TestL3Cache_HitCountIncrement(t *testing.T) {
	db := setupTestDB(t)
	cache := NewL3Cache(db, 30*time.Second, nil)
	ctx := context.Background()

	cacheKey := MakeL3CacheKey("sess1", "ctx1")
	_ = cache.Set(ctx, cacheKey, "sess1", "ctx1", []Suggestion{{Command: "ls"}})

	// Multiple gets - hit_count is incremented synchronously
	cache.Get(ctx, cacheKey)
	cache.Get(ctx, cacheKey)

	var hitCount int64
	err := db.QueryRow(`SELECT hit_count FROM suggestion_cache WHERE cache_key = ?`, cacheKey).Scan(&hitCount)
	require.NoError(t, err)
	assert.Equal(t, int64(2), hitCount)
}

func TestMakeL3CacheKey(t *testing.T) {
	key := MakeL3CacheKey("session-123", "abcdef")
	assert.Equal(t, "session-123:abcdef", key)
}
