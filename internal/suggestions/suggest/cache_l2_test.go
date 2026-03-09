package suggest

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestL2Cache_BasicGetSet(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL2Cache(100, 30*time.Second, metrics)

	suggestions := []Suggestion{
		{Command: "make build", Score: 0.8},
		{Command: "make test", Score: 0.7},
	}

	cache.Set("repo:github.com/user/project", suggestions)
	got, ok := cache.Get("repo:github.com/user/project")
	require.True(t, ok)
	assert.Equal(t, 2, len(got))
	assert.Equal(t, "make build", got[0].Command)
}

func TestL2Cache_Miss(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL2Cache(100, 30*time.Second, metrics)

	got, ok := cache.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)

	snap := metrics.Snapshot()
	assert.Equal(t, int64(1), snap.L2.Misses)
}

func TestL2Cache_TTLExpiry(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL2Cache(100, 1*time.Millisecond, metrics)

	cache.Set("repo1", []Suggestion{{Command: "ls"}})
	time.Sleep(5 * time.Millisecond)

	got, ok := cache.Get("repo1")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestL2Cache_InvalidateRepo(t *testing.T) {
	cache := NewL2Cache(100, 30*time.Second, nil)

	cache.Set("repo1", []Suggestion{{Command: "a"}})
	cache.Set("repo2", []Suggestion{{Command: "b"}})

	ok := cache.InvalidateRepo("repo1")
	assert.True(t, ok)
	assert.Equal(t, 1, cache.Len())

	_, found := cache.Get("repo1")
	assert.False(t, found)

	got, found := cache.Get("repo2")
	require.True(t, found)
	assert.Equal(t, "b", got[0].Command)
}

func TestL2Cache_InvalidateAll(t *testing.T) {
	cache := NewL2Cache(100, 30*time.Second, nil)
	cache.Set("repo1", []Suggestion{{Command: "a"}})
	cache.Set("repo2", []Suggestion{{Command: "b"}})

	cache.InvalidateAll()
	assert.Equal(t, 0, cache.Len())
}

func TestL2Cache_LRUEviction(t *testing.T) {
	cache := NewL2Cache(2, 30*time.Second, nil)

	cache.Set("repo1", []Suggestion{{Command: "a"}})
	cache.Set("repo2", []Suggestion{{Command: "b"}})
	cache.Set("repo3", []Suggestion{{Command: "c"}})

	_, ok := cache.Get("repo1")
	assert.False(t, ok, "repo1 should have been evicted")
	assert.Equal(t, 2, cache.Len())
}

func TestL2Cache_MemorySize(t *testing.T) {
	cache := NewL2Cache(100, 30*time.Second, nil)
	cache.Set("repo1", []Suggestion{
		{Command: "git status", Score: 0.9, Reasons: []string{"freq"}},
	})

	assert.Greater(t, cache.MemorySize(), int64(0))
}

func TestL2Cache_EvictToSize(t *testing.T) {
	cache := NewL2Cache(100, 30*time.Second, nil)

	for i := 0; i < 10; i++ {
		cache.Set(fmt.Sprintf("repo%d", i), []Suggestion{
			{Command: fmt.Sprintf("cmd%d", i), Score: 0.5},
		})
	}

	initialSize := cache.MemorySize()
	evicted := cache.EvictToSize(initialSize / 2)
	assert.Greater(t, evicted, 0)
	assert.LessOrEqual(t, cache.MemorySize(), initialSize/2)
}

func TestL2Cache_DefaultCapacity(t *testing.T) {
	cache := NewL2Cache(0, 30*time.Second, nil)
	cache.Set("repo1", []Suggestion{{Command: "ls"}})
	got, ok := cache.Get("repo1")
	require.True(t, ok)
	assert.Equal(t, 1, len(got))
}

func TestL2Cache_MetricsTracking(t *testing.T) {
	metrics := &CacheMetrics{}
	cache := NewL2Cache(100, 30*time.Second, metrics)

	cache.Set("repo1", []Suggestion{{Command: "a"}})

	// Hit
	cache.Get("repo1")
	// Miss
	cache.Get("repo2")
	// Miss
	cache.Get("repo3")

	snap := metrics.Snapshot()
	assert.Equal(t, int64(1), snap.L2.Hits)
	assert.Equal(t, int64(2), snap.L2.Misses)
}

func TestL2Cache_UpdateExistingRepo(t *testing.T) {
	cache := NewL2Cache(100, 30*time.Second, nil)

	cache.Set("repo1", []Suggestion{{Command: "old"}})
	cache.Set("repo1", []Suggestion{{Command: "new"}})

	assert.Equal(t, 1, cache.Len())
	got, ok := cache.Get("repo1")
	require.True(t, ok)
	assert.Equal(t, "new", got[0].Command)
}
