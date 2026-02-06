package shim

import (
	"sync"
	"testing"
)

func TestRingBuffer_PushAndDrain(t *testing.T) {
	rb := NewRingBuffer[int](4)

	if rb.Cap() != 4 {
		t.Errorf("Cap() = %d, want 4", rb.Cap())
	}
	if rb.Len() != 0 {
		t.Errorf("Len() = %d, want 0", rb.Len())
	}

	// Drain empty buffer returns nil
	items := rb.DrainAll()
	if items != nil {
		t.Errorf("DrainAll() on empty = %v, want nil", items)
	}

	// Push some items
	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	if rb.Len() != 3 {
		t.Errorf("Len() = %d, want 3", rb.Len())
	}

	items = rb.DrainAll()
	if len(items) != 3 {
		t.Fatalf("DrainAll() len = %d, want 3", len(items))
	}
	if items[0] != 1 || items[1] != 2 || items[2] != 3 {
		t.Errorf("DrainAll() = %v, want [1 2 3]", items)
	}

	// Buffer should be empty after drain
	if rb.Len() != 0 {
		t.Errorf("Len() after drain = %d, want 0", rb.Len())
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer[int](3)

	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	// Buffer full; next push drops oldest (1)
	rb.Push(4)

	if rb.Len() != 3 {
		t.Errorf("Len() = %d, want 3", rb.Len())
	}

	items := rb.DrainAll()
	if len(items) != 3 {
		t.Fatalf("DrainAll() len = %d, want 3", len(items))
	}
	// Should have [2, 3, 4] (oldest dropped)
	if items[0] != 2 || items[1] != 3 || items[2] != 4 {
		t.Errorf("DrainAll() = %v, want [2 3 4]", items)
	}
}

func TestRingBuffer_OverflowMultiple(t *testing.T) {
	rb := NewRingBuffer[int](3)

	// Push 6 items into a capacity-3 buffer
	for i := 1; i <= 6; i++ {
		rb.Push(i)
	}

	items := rb.DrainAll()
	if len(items) != 3 {
		t.Fatalf("DrainAll() len = %d, want 3", len(items))
	}
	// Should have [4, 5, 6] (1, 2, 3 dropped)
	if items[0] != 4 || items[1] != 5 || items[2] != 6 {
		t.Errorf("DrainAll() = %v, want [4 5 6]", items)
	}
}

func TestRingBuffer_DefaultCapacity(t *testing.T) {
	rb := NewRingBuffer[int](0)
	if rb.Cap() != DefaultRingCapacity {
		t.Errorf("Cap() = %d, want %d", rb.Cap(), DefaultRingCapacity)
	}

	rb2 := NewRingBuffer[int](-5)
	if rb2.Cap() != DefaultRingCapacity {
		t.Errorf("Cap() = %d, want %d", rb2.Cap(), DefaultRingCapacity)
	}
}

func TestRingBuffer_PushDrainPush(t *testing.T) {
	rb := NewRingBuffer[string](2)

	rb.Push("a")
	rb.Push("b")

	items := rb.DrainAll()
	if len(items) != 2 || items[0] != "a" || items[1] != "b" {
		t.Errorf("first drain = %v, want [a b]", items)
	}

	// Push again after drain
	rb.Push("c")
	rb.Push("d")
	rb.Push("e") // drops "c"

	items = rb.DrainAll()
	if len(items) != 2 || items[0] != "d" || items[1] != "e" {
		t.Errorf("second drain = %v, want [d e]", items)
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer[int](100)
	var wg sync.WaitGroup

	// Concurrent pushes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				rb.Push(base*10 + j)
			}
		}(i)
	}
	wg.Wait()

	items := rb.DrainAll()
	if len(items) != 100 {
		t.Errorf("DrainAll() len = %d, want 100", len(items))
	}
}
