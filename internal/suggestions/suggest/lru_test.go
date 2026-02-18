package suggest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_BasicPutGet(t *testing.T) {
	lru := NewLRU[string, int](3, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3)

	v, ok := lru.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)

	v, ok = lru.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)

	_, ok = lru.Get("missing")
	assert.False(t, ok)
}

func TestLRU_EvictsOldest(t *testing.T) {
	lru := NewLRU[string, int](2, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3) // should evict "a"

	_, ok := lru.Get("a")
	assert.False(t, ok, "a should have been evicted")

	v, ok := lru.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)

	v, ok = lru.Get("c")
	require.True(t, ok)
	assert.Equal(t, 3, v)
}

func TestLRU_GetPromotesItem(t *testing.T) {
	lru := NewLRU[string, int](2, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)

	// Access "a" to promote it
	lru.Get("a")

	// Now "b" is the oldest, adding "c" should evict "b"
	lru.Put("c", 3)

	_, ok := lru.Get("b")
	assert.False(t, ok, "b should have been evicted")

	v, ok := lru.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestLRU_UpdateExistingKey(t *testing.T) {
	lru := NewLRU[string, int](2, nil)
	lru.Put("a", 1)
	lru.Put("a", 10)

	v, ok := lru.Get("a")
	require.True(t, ok)
	assert.Equal(t, 10, v)
	assert.Equal(t, 1, lru.Len())
}

func TestLRU_Delete(t *testing.T) {
	lru := NewLRU[string, int](3, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)

	ok := lru.Delete("a")
	assert.True(t, ok)
	assert.Equal(t, 1, lru.Len())

	_, found := lru.Get("a")
	assert.False(t, found)

	ok = lru.Delete("nonexistent")
	assert.False(t, ok)
}

func TestLRU_DeleteFunc(t *testing.T) {
	lru := NewLRU[string, int](10, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3)
	lru.Put("d", 4)

	// Delete all entries with even values
	removed := lru.DeleteFunc(func(_ string, v int) bool {
		return v%2 == 0
	})
	assert.Equal(t, 2, removed)
	assert.Equal(t, 2, lru.Len())

	_, ok := lru.Get("b")
	assert.False(t, ok)
	_, ok = lru.Get("d")
	assert.False(t, ok)

	v, ok := lru.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestLRU_SizeFunc(t *testing.T) {
	sizeFunc := func(_ string, v int) int64 {
		return int64(v)
	}
	lru := NewLRU[string, int](100, sizeFunc)
	lru.Put("a", 100)
	lru.Put("b", 200)

	assert.Equal(t, int64(300), lru.Size())

	lru.Delete("a")
	assert.Equal(t, int64(200), lru.Size())
}

func TestLRU_EvictToSize(t *testing.T) {
	sizeFunc := func(_ string, v int) int64 {
		return int64(v)
	}
	lru := NewLRU[string, int](100, sizeFunc)
	lru.Put("a", 100) // oldest
	lru.Put("b", 200)
	lru.Put("c", 300) // newest

	// Total size = 600, target = 300
	// Should evict "a" (100) -> 500, then "b" (200) -> 300
	evicted := lru.EvictToSize(300)
	assert.Equal(t, 2, evicted)
	assert.Equal(t, int64(300), lru.Size())
	assert.Equal(t, 1, lru.Len())

	_, ok := lru.Get("a")
	assert.False(t, ok)
	_, ok = lru.Get("b")
	assert.False(t, ok)
	v, ok := lru.Get("c")
	require.True(t, ok)
	assert.Equal(t, 300, v)
}

func TestLRU_Clear(t *testing.T) {
	lru := NewLRU[string, int](10, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)

	lru.Clear()
	assert.Equal(t, 0, lru.Len())
	assert.Equal(t, int64(0), lru.Size())

	_, ok := lru.Get("a")
	assert.False(t, ok)
}

func TestLRU_ZeroCapacity(t *testing.T) {
	// Capacity < 1 should be clamped to 1
	lru := NewLRU[string, int](0, nil)
	lru.Put("a", 1)
	assert.Equal(t, 1, lru.Len())

	lru.Put("b", 2) // should evict "a"
	assert.Equal(t, 1, lru.Len())

	_, ok := lru.Get("a")
	assert.False(t, ok)

	v, ok := lru.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)
}

func TestLRU_UpdatePreservesCapacity(t *testing.T) {
	lru := NewLRU[string, int](2, nil)
	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("a", 10) // Update, should not increase count

	assert.Equal(t, 2, lru.Len())

	// "b" should still be accessible
	v, ok := lru.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)
}

func TestLRU_SizeFuncUpdate(t *testing.T) {
	sizeFunc := func(_ string, v int) int64 {
		return int64(v)
	}
	lru := NewLRU[string, int](100, sizeFunc)
	lru.Put("a", 100)
	assert.Equal(t, int64(100), lru.Size())

	lru.Put("a", 50) // Update should adjust size
	assert.Equal(t, int64(50), lru.Size())
	assert.Equal(t, 1, lru.Len())
}
