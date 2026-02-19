package suggest

import (
	"container/list"
	"sync"
)

// LRU is a generic least-recently-used cache with size tracking.
// It is safe for concurrent use.
type LRU[K comparable, V any] struct {
	items    map[K]*list.Element
	order    *list.List
	sizeFunc func(K, V) int64
	capacity int
	size     int64
	mu       sync.Mutex
}

type lruEntry[K comparable, V any] struct {
	key  K
	val  V
	size int64
}

// NewLRU creates a new LRU cache with the given capacity (max number of items).
// sizeFunc estimates the size of an entry in bytes for memory accounting.
// If sizeFunc is nil, each entry counts as 1 byte.
func NewLRU[K comparable, V any](capacity int, sizeFunc func(K, V) int64) *LRU[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &LRU[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element, capacity),
		order:    list.New(),
		sizeFunc: sizeFunc,
	}
}

// Get retrieves a value from the cache and marks it as recently used.
// Returns the value and true if found, zero value and false otherwise.
func (l *LRU[K, V]) Get(key K) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[key]; ok {
		l.order.MoveToFront(elem)
		return elem.Value.(*lruEntry[K, V]).val, true
	}
	var zero V
	return zero, false
}

// Put adds or updates a value in the cache.
// If the cache is at capacity, the least recently used item is evicted.
func (l *LRU[K, V]) Put(key K, val V) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entrySize := int64(1)
	if l.sizeFunc != nil {
		entrySize = l.sizeFunc(key, val)
	}

	if elem, ok := l.items[key]; ok {
		// Update existing
		old := elem.Value.(*lruEntry[K, V])
		l.size -= old.size
		old.val = val
		old.size = entrySize
		l.size += entrySize
		l.order.MoveToFront(elem)
		return
	}

	// Evict if at capacity
	for l.order.Len() >= l.capacity {
		l.evictOldest()
	}

	entry := &lruEntry[K, V]{key: key, val: val, size: entrySize}
	elem := l.order.PushFront(entry)
	l.items[key] = elem
	l.size += entrySize
}

// Delete removes an entry from the cache.
func (l *LRU[K, V]) Delete(key K) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[key]; ok {
		l.removeElement(elem)
		return true
	}
	return false
}

// DeleteFunc removes all entries matching the predicate.
// Returns the number of entries removed.
func (l *LRU[K, V]) DeleteFunc(pred func(K, V) bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	var toRemove []*list.Element
	for e := l.order.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*lruEntry[K, V])
		if pred(entry.key, entry.val) {
			toRemove = append(toRemove, e)
		}
	}

	for _, e := range toRemove {
		l.removeElement(e)
	}
	return len(toRemove)
}

// EvictToSize evicts least-recently-used entries until the total size
// is at or below the target size. Returns the number of entries evicted.
func (l *LRU[K, V]) EvictToSize(targetSize int64) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	evicted := 0
	for l.size > targetSize && l.order.Len() > 0 {
		l.evictOldest()
		evicted++
	}
	return evicted
}

// Len returns the number of entries in the cache.
func (l *LRU[K, V]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}

// Size returns the total estimated size in bytes.
func (l *LRU[K, V]) Size() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.size
}

// Clear removes all entries from the cache.
func (l *LRU[K, V]) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = make(map[K]*list.Element, l.capacity)
	l.order.Init()
	l.size = 0
}

func (l *LRU[K, V]) evictOldest() {
	back := l.order.Back()
	if back == nil {
		return
	}
	l.removeElement(back)
}

func (l *LRU[K, V]) removeElement(elem *list.Element) {
	entry := elem.Value.(*lruEntry[K, V])
	l.order.Remove(elem)
	delete(l.items, entry.key)
	l.size -= entry.size
}
