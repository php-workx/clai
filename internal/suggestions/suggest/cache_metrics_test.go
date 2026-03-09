package suggest

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCacheMetrics_RecordHitMiss(t *testing.T) {
	m := &CacheMetrics{}

	m.RecordHit(CacheLayerL1)
	m.RecordHit(CacheLayerL1)
	m.RecordMiss(CacheLayerL1)

	m.RecordHit(CacheLayerL2)
	m.RecordMiss(CacheLayerL2)
	m.RecordMiss(CacheLayerL2)

	m.RecordHit(CacheLayerL3)

	snap := m.Snapshot()

	assert.Equal(t, int64(2), snap.L1.Hits)
	assert.Equal(t, int64(1), snap.L1.Misses)
	assert.Equal(t, int64(3), snap.L1.Requests)
	assert.InDelta(t, 0.6667, snap.L1.HitRate, 0.001)

	assert.Equal(t, int64(1), snap.L2.Hits)
	assert.Equal(t, int64(2), snap.L2.Misses)
	assert.InDelta(t, 0.3333, snap.L2.HitRate, 0.001)

	assert.Equal(t, int64(1), snap.L3.Hits)
	assert.Equal(t, int64(0), snap.L3.Misses)
	assert.InDelta(t, 1.0, snap.L3.HitRate, 0.001)
}

func TestCacheMetrics_ZeroState(t *testing.T) {
	m := &CacheMetrics{}
	snap := m.Snapshot()

	assert.Equal(t, int64(0), snap.L1.Hits)
	assert.Equal(t, int64(0), snap.L1.Misses)
	assert.Equal(t, float64(0), snap.L1.HitRate)
	assert.Equal(t, int64(0), snap.L1.Requests)
}

func TestCacheMetrics_ConcurrentAccess(t *testing.T) {
	m := &CacheMetrics{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			m.RecordHit(CacheLayerL1)
		}()
		go func() {
			defer wg.Done()
			m.RecordMiss(CacheLayerL2)
		}()
		go func() {
			defer wg.Done()
			m.RecordHit(CacheLayerL3)
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	assert.Equal(t, int64(100), snap.L1.Hits)
	assert.Equal(t, int64(100), snap.L2.Misses)
	assert.Equal(t, int64(100), snap.L3.Hits)
}

func TestPrecomputeTracker_TryAcquire(t *testing.T) {
	tracker := NewPrecomputeTracker()

	assert.True(t, tracker.TryAcquire("key1"))
	assert.False(t, tracker.TryAcquire("key1")) // duplicate
	assert.True(t, tracker.TryAcquire("key2"))  // different key

	assert.Equal(t, 2, tracker.PendingCount())
}

func TestPrecomputeTracker_Release(t *testing.T) {
	tracker := NewPrecomputeTracker()

	tracker.TryAcquire("key1")
	tracker.Release("key1", nil)
	assert.Equal(t, 0, tracker.PendingCount())
	assert.Equal(t, int64(1), tracker.CompletedCount())
	assert.Equal(t, int64(0), tracker.ErrorCount())

	// Can re-acquire after release
	assert.True(t, tracker.TryAcquire("key1"))
}

func TestPrecomputeTracker_ReleaseWithError(t *testing.T) {
	tracker := NewPrecomputeTracker()

	tracker.TryAcquire("key1")
	tracker.Release("key1", errors.New("test error"))
	assert.Equal(t, int64(0), tracker.CompletedCount())
	assert.Equal(t, int64(1), tracker.ErrorCount())
}

func TestPrecomputeTracker_ConcurrentAcquire(t *testing.T) {
	tracker := NewPrecomputeTracker()
	var wg sync.WaitGroup
	acquired := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acquired <- tracker.TryAcquire("same-key")
		}()
	}
	wg.Wait()
	close(acquired)

	successCount := 0
	for ok := range acquired {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one goroutine should acquire")
}
