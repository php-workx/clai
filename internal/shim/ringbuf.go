// Package shim provides persistent connection mode for the clai-shim binary.
// It implements a long-lived NDJSON stdin loop with a single gRPC connection
// to the daemon, lazy reconnection with exponential backoff, and a ring buffer
// for events during connection loss.
package shim

import (
	"sync"
)

// DefaultRingCapacity is the default number of events buffered during connection loss.
const DefaultRingCapacity = 16

// RingBuffer is a fixed-capacity, thread-safe circular buffer.
// When the buffer is full, the oldest item is silently dropped (overwritten).
type RingBuffer[T any] struct {
	mu    sync.Mutex
	items []T
	head  int // index of oldest item
	count int // number of items currently stored
	cap   int // maximum capacity
}

// NewRingBuffer creates a new RingBuffer with the given capacity.
// If cap is <= 0, DefaultRingCapacity is used.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		capacity = DefaultRingCapacity
	}
	return &RingBuffer[T]{
		items: make([]T, capacity),
		cap:   capacity,
	}
}

// Push adds an item to the buffer. If the buffer is full, the oldest item
// is silently dropped to make room.
func (r *RingBuffer[T]) Push(item T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	writeIdx := (r.head + r.count) % r.cap
	r.items[writeIdx] = item

	if r.count == r.cap {
		// Buffer full: advance head to drop oldest
		r.head = (r.head + 1) % r.cap
	} else {
		r.count++
	}
}

// DrainAll removes and returns all items in FIFO order (oldest first).
// The buffer is empty after this call.
func (r *RingBuffer[T]) DrainAll() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.count == 0 {
		return nil
	}

	result := make([]T, r.count)
	for i := 0; i < r.count; i++ {
		idx := (r.head + i) % r.cap
		result[i] = r.items[idx]
	}

	// Reset
	r.count = 0
	r.head = 0

	return result
}

// Len returns the number of items currently in the buffer.
func (r *RingBuffer[T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// Cap returns the capacity of the buffer.
func (r *RingBuffer[T]) Cap() int {
	return r.cap
}
