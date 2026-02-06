package suggest

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func setupMultiCache(t *testing.T) (*MultiCache, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)

	mc := NewMultiCache(MultiCacheConfig{
		L1Capacity:   100,
		L2Capacity:   50,
		TTL:          30 * time.Second,
		MemoryBudget: DefaultMemoryBudgetBytes,
		DB:           db,
	})

	return mc, db
}

func TestMultiCache_L1Hit(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	suggestions := []Suggestion{{Command: "git status", Score: 0.9}}
	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", suggestions)

	// Wait for async L3 write
	time.Sleep(50 * time.Millisecond)

	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL1, layer)
	assert.Equal(t, "git status", got[0].Command)
}

func TestMultiCache_L2Fallback(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	// Set directly in L2
	mc.L2().Set("repo1", []Suggestion{{Command: "make build", Score: 0.8}})

	// Query with different session/event so L1 misses
	got, layer := mc.Get(ctx, "new-session", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL2, layer)
	assert.Equal(t, "make build", got[0].Command)

	// Should have been promoted to L1
	snap := mc.MetricsSnapshot()
	assert.Equal(t, int64(1), snap.L1.Misses)
	assert.Equal(t, int64(1), snap.L2.Hits)
}

func TestMultiCache_L3Fallback(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	// Set directly in L3
	l3Key := MakeL3CacheKey("sess1", "ctx1")
	err := mc.L3().Set(ctx, l3Key, "sess1", "ctx1", []Suggestion{{Command: "npm test", Score: 0.7}})
	require.NoError(t, err)

	got, layer := mc.Get(ctx, "sess1", 1, "", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL3, layer)
	assert.Equal(t, "npm test", got[0].Command)
}

func TestMultiCache_Miss(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	assert.Nil(t, got)
	assert.Equal(t, CacheLayerMiss, layer)
}

func TestMultiCache_OnCommandEnd(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", []Suggestion{{Command: "ls"}})

	// Verify L1 has the entry
	_, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	assert.Equal(t, CacheLayerL1, layer)

	// Command end should clear L1 for this session
	mc.OnCommandEnd("sess1")

	// L1 should miss now, but L2 should hit (repo-keyed)
	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL2, layer)
}

func TestMultiCache_OnContextChange(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", []Suggestion{{Command: "ls"}})

	// Wait for async L3 write
	time.Sleep(50 * time.Millisecond)

	// Context change should clear both L1 and L2
	mc.OnContextChange("sess1", "repo1")

	// Should fall through to L3
	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL3, layer)
}

func TestMultiCache_MemoryBudgetEnforcement(t *testing.T) {
	// Create cache with very small budget
	db := setupTestDB(t)
	mc := NewMultiCache(MultiCacheConfig{
		L1Capacity:   1000,
		L2Capacity:   1000,
		TTL:          30 * time.Second,
		MemoryBudget: 100, // 100 bytes budget
		DB:           db,
	})

	ctx := context.Background()

	// Fill with many entries to exceed budget
	for i := 0; i < 50; i++ {
		mc.Set(ctx, "sess1", int64(i), "repo1", "pfx1", "ctx1", []Suggestion{
			{Command: "a very long command that takes up memory space in the cache", Score: 0.5, Reasons: []string{"freq", "transition"}},
		})
	}

	// Memory should be at or below budget after enforcement
	totalSize := mc.L1().MemorySize() + mc.L2().MemorySize()
	assert.LessOrEqual(t, totalSize, int64(100)+500, // allow some overhead for the last entry
		"memory should be near budget after enforcement")
}

func TestMultiCache_SetWritesToL3Async(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", []Suggestion{{Command: "git push"}})

	// L3 write is async, wait for it
	time.Sleep(100 * time.Millisecond)

	size, err := mc.L3().Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), size)
}

func TestMultiCache_L2PromotesToL1(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	mc.L2().Set("repo1", []Suggestion{{Command: "make test"}})

	// First access from L2
	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL2, layer)

	// Second access should hit L1 (promoted)
	got, layer = mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL1, layer)
}

func TestMultiCache_L3PromotesToL1AndL2(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	l3Key := MakeL3CacheKey("sess1", "ctx1")
	_ = mc.L3().Set(ctx, l3Key, "sess1", "ctx1", []Suggestion{{Command: "docker build"}})

	// First access from L3
	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL3, layer)

	// Second access should hit L1
	got, layer = mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL1, layer)

	// L2 should also have the entry
	l2Got, l2Ok := mc.L2().Get("repo1")
	require.True(t, l2Ok)
	assert.Equal(t, "docker build", l2Got[0].Command)
}

func TestMultiCache_TriggerPrecompute(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	var computeCalled int
	var mu sync.Mutex

	computeFunc := func(_ context.Context) ([]Suggestion, error) {
		mu.Lock()
		computeCalled++
		mu.Unlock()
		return []Suggestion{{Command: "precomputed-cmd", Score: 0.95}}, nil
	}

	mc.TriggerPrecompute(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", computeFunc)

	// Wait for async precompute
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, computeCalled)
	mu.Unlock()

	// Result should be in cache
	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL1, layer)
	assert.Equal(t, "precomputed-cmd", got[0].Command)
}

func TestMultiCache_PrecomputeDedup(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	var computeCount int
	var mu sync.Mutex
	started := make(chan struct{})

	computeFunc := func(_ context.Context) ([]Suggestion, error) {
		mu.Lock()
		computeCount++
		mu.Unlock()
		<-started // Block until released
		return []Suggestion{{Command: "result"}}, nil
	}

	// Trigger same precompute twice
	mc.TriggerPrecompute(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", computeFunc)
	time.Sleep(10 * time.Millisecond) // Give first goroutine time to acquire
	mc.TriggerPrecompute(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", computeFunc)

	// Release the compute
	close(started)
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, computeCount, "duplicate precompute should be deduplicated")
	mu.Unlock()
}

func TestMultiCache_Metrics(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", []Suggestion{{Command: "ls"}})

	// L1 hit
	mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")

	// Miss
	mc.Get(ctx, "sess2", 2, "repo2", "pfx2", "ctx2")

	snap := mc.MetricsSnapshot()
	assert.Equal(t, int64(1), snap.L1.Hits)
	assert.Greater(t, snap.L1.Misses+snap.L2.Misses+snap.L3.Misses, int64(0))
}

func TestMultiCache_Cleanup(t *testing.T) {
	db := setupTestDB(t)
	mc := NewMultiCache(MultiCacheConfig{
		L1Capacity:   100,
		L2Capacity:   50,
		TTL:          1 * time.Millisecond,
		MemoryBudget: DefaultMemoryBudgetBytes,
		DB:           db,
	})
	ctx := context.Background()

	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", []Suggestion{{Command: "ls"}})

	// Wait for async L3 write and TTL expiry
	time.Sleep(100 * time.Millisecond)

	removed, err := mc.Cleanup(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
}

func TestMultiCache_NilDB(t *testing.T) {
	mc := NewMultiCache(MultiCacheConfig{
		L1Capacity:   10,
		L2Capacity:   10,
		TTL:          30 * time.Second,
		MemoryBudget: DefaultMemoryBudgetBytes,
		DB:           nil,
	})
	ctx := context.Background()

	// Should not panic with nil DB
	mc.Set(ctx, "sess1", 1, "repo1", "pfx1", "ctx1", []Suggestion{{Command: "ls"}})

	got, layer := mc.Get(ctx, "sess1", 1, "repo1", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL1, layer)
}

func TestMultiCache_ConcurrentAccess(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mc.Set(ctx, "sess1", int64(i), "repo1", "pfx1", "ctx1",
				[]Suggestion{{Command: "cmd", Score: float64(i) / 100}})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mc.Get(ctx, "sess1", int64(i), "repo1", "pfx1", "ctx1")
		}(i)
	}

	wg.Wait()
	// No panics = success
}

func TestNewMultiCacheFromConfig(t *testing.T) {
	db := setupTestDB(t)
	mc := NewMultiCacheFromConfig(30000, 50, db, nil)

	assert.NotNil(t, mc)
	assert.NotNil(t, mc.L1())
	assert.NotNil(t, mc.L2())
	assert.NotNil(t, mc.L3())
	assert.NotNil(t, mc.Metrics())
	assert.NotNil(t, mc.PrecomputeTrackerInstance())
}

func TestNewMultiCacheFromConfig_Defaults(t *testing.T) {
	mc := NewMultiCacheFromConfig(0, 0, nil, nil)

	assert.NotNil(t, mc)
	assert.Equal(t, DefaultMemoryBudgetBytes, int(mc.memoryBudget))
}

func TestMultiCache_GetNoRepoKey(t *testing.T) {
	mc, _ := setupMultiCache(t)
	ctx := context.Background()

	// Set with empty repo key
	mc.Set(ctx, "sess1", 1, "", "pfx1", "ctx1", []Suggestion{{Command: "ls"}})

	got, layer := mc.Get(ctx, "sess1", 1, "", "pfx1", "ctx1")
	require.NotNil(t, got)
	assert.Equal(t, CacheLayerL1, layer)
}
