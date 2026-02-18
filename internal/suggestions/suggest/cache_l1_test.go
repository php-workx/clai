package suggest

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestL1Cache_BasicGetSet(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL1Cache(100, 30*time.Second, metrics)

	key := MakeL1Key("sess1", 1, MakePrefixHash("/home", "repo1", "main"))
	suggestions := []Suggestion{
		{Command: "git status", Score: 0.9},
		{Command: "git diff", Score: 0.7},
	}

	cache.Set(key, suggestions)
	got, ok := cache.Get(key)
	require.True(t, ok)
	assert.Equal(t, 2, len(got))
	assert.Equal(t, "git status", got[0].Command)
}

func TestL1Cache_Miss(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL1Cache(100, 30*time.Second, metrics)

	got, ok := cache.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)

	snap := metrics.Snapshot()
	assert.Equal(t, int64(1), snap.L1.Misses)
}

func TestL1Cache_TTLExpiry(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL1Cache(100, 1*time.Millisecond, metrics)

	key := MakeL1Key("sess1", 1, "hash1")
	cache.Set(key, []Suggestion{{Command: "ls"}})

	time.Sleep(5 * time.Millisecond)

	got, ok := cache.Get(key)
	assert.False(t, ok)
	assert.Nil(t, got)

	snap := metrics.Snapshot()
	assert.Equal(t, int64(1), snap.L1.Misses)
}

func TestL1Cache_InvalidateSession(t *testing.T) {
	cache := NewL1Cache(100, 30*time.Second, nil)

	// Add entries for two sessions
	cache.Set(MakeL1Key("sess1", 1, "h1"), []Suggestion{{Command: "a"}})
	cache.Set(MakeL1Key("sess1", 2, "h2"), []Suggestion{{Command: "b"}})
	cache.Set(MakeL1Key("sess2", 1, "h3"), []Suggestion{{Command: "c"}})

	assert.Equal(t, 3, cache.Len())

	removed := cache.InvalidateSession("sess1")
	assert.Equal(t, 2, removed)
	assert.Equal(t, 1, cache.Len())

	// sess2 entry should still be there
	_, ok := cache.Get(MakeL1Key("sess2", 1, "h3"))
	assert.True(t, ok)
}

func TestL1Cache_InvalidateAll(t *testing.T) {
	cache := NewL1Cache(100, 30*time.Second, nil)
	cache.Set(MakeL1Key("s1", 1, "h1"), []Suggestion{{Command: "a"}})
	cache.Set(MakeL1Key("s2", 1, "h2"), []Suggestion{{Command: "b"}})

	cache.InvalidateAll()
	assert.Equal(t, 0, cache.Len())
}

func TestL1Cache_LRUEviction(t *testing.T) {
	cache := NewL1Cache(2, 30*time.Second, nil)

	cache.Set(MakeL1Key("s1", 1, "h1"), []Suggestion{{Command: "a"}})
	cache.Set(MakeL1Key("s2", 1, "h2"), []Suggestion{{Command: "b"}})
	cache.Set(MakeL1Key("s3", 1, "h3"), []Suggestion{{Command: "c"}})

	// First entry should have been evicted
	_, ok := cache.Get(MakeL1Key("s1", 1, "h1"))
	assert.False(t, ok)

	assert.Equal(t, 2, cache.Len())
}

func TestL1Cache_MemorySize(t *testing.T) {
	cache := NewL1Cache(100, 30*time.Second, nil)

	cache.Set("key1", []Suggestion{
		{Command: "git status", Score: 0.9, Reasons: []string{"freq", "transition"}},
	})

	assert.Greater(t, cache.MemorySize(), int64(0))
}

func TestL1Cache_EvictToSize(t *testing.T) {
	cache := NewL1Cache(100, 30*time.Second, nil)

	for i := 0; i < 10; i++ {
		cache.Set(fmt.Sprintf("key%d", i), []Suggestion{
			{Command: fmt.Sprintf("cmd%d", i), Score: 0.5},
		})
	}

	initialSize := cache.MemorySize()
	assert.Greater(t, initialSize, int64(0))

	evicted := cache.EvictToSize(initialSize / 2)
	assert.Greater(t, evicted, 0)
	assert.LessOrEqual(t, cache.MemorySize(), initialSize/2)
}

func TestMakeL1Key(t *testing.T) {
	key := MakeL1Key("session-abc", 42, "deadbeef")
	assert.Equal(t, "session-abc:42:deadbeef", key)
}

func TestMakePrefixHash(t *testing.T) {
	h1 := MakePrefixHash("/home/user/project", "repo:github.com/user/repo", "main")
	h2 := MakePrefixHash("/home/user/project", "repo:github.com/user/repo", "main")
	h3 := MakePrefixHash("/home/user/other", "repo:github.com/user/repo", "main")

	assert.Equal(t, h1, h2, "same inputs should produce same hash")
	assert.NotEqual(t, h1, h3, "different inputs should produce different hash")
	assert.Equal(t, 16, len(h1), "hash should be 16 hex chars")
}

func TestL1Cache_DefaultCapacity(t *testing.T) {
	cache := NewL1Cache(0, 30*time.Second, nil)
	// Should not panic, default capacity should be used
	cache.Set("key1", []Suggestion{{Command: "ls"}})
	got, ok := cache.Get("key1")
	require.True(t, ok)
	assert.Equal(t, 1, len(got))
}

func TestL1Cache_NilMetrics(t *testing.T) {
	// Nil metrics should not panic
	cache := NewL1Cache(10, 30*time.Second, nil)
	cache.Set("key1", []Suggestion{{Command: "ls"}})
	got, ok := cache.Get("key1")
	require.True(t, ok)
	assert.Equal(t, 1, len(got))

	_, ok = cache.Get("nonexistent")
	assert.False(t, ok)
}

func TestEstimateSuggestionsSize(t *testing.T) {
	suggestions := []Suggestion{
		{Command: "git status", Score: 0.9, Reasons: []string{"freq"}},
		{Command: "git diff", Score: 0.7, Reasons: []string{"transition", "freq"}},
	}

	size := estimateSuggestionsSize(suggestions)
	assert.Greater(t, size, int64(0))

	// Larger suggestions should have larger size
	bigger := append(suggestions, Suggestion{
		Command: "git commit -m 'a very long commit message here'",
		Score:   0.5,
		Reasons: []string{"freq", "transition", "task"},
	})
	biggerSize := estimateSuggestionsSize(bigger)
	assert.Greater(t, biggerSize, size)
}
