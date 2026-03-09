package shim

import (
	"sync"
	"testing"
)

func TestNewRingBuffer(t *testing.T) {
	t.Run("uses specified capacity", func(t *testing.T) {
		rb := NewRingBuffer[int](8)
		if rb.Cap() != 8 {
			t.Errorf("expected cap 8, got %d", rb.Cap())
		}
	})

	t.Run("defaults to DefaultRingCapacity for zero", func(t *testing.T) {
		rb := NewRingBuffer[int](0)
		if rb.Cap() != DefaultRingCapacity {
			t.Errorf("expected cap %d, got %d", DefaultRingCapacity, rb.Cap())
		}
	})

	t.Run("defaults to DefaultRingCapacity for negative", func(t *testing.T) {
		rb := NewRingBuffer[int](-1)
		if rb.Cap() != DefaultRingCapacity {
			t.Errorf("expected cap %d, got %d", DefaultRingCapacity, rb.Cap())
		}
	})

	t.Run("starts empty", func(t *testing.T) {
		rb := NewRingBuffer[int](4)
		if rb.Len() != 0 {
			t.Errorf("expected len 0, got %d", rb.Len())
		}
	})
}

func TestRingBufferPushAndDrain(t *testing.T) {
	t.Run("push and drain single item", func(t *testing.T) {
		rb := NewRingBuffer[string](4)
		rb.Push("hello")
		if rb.Len() != 1 {
			t.Errorf("expected len 1, got %d", rb.Len())
		}
		items := rb.DrainAll()
		if len(items) != 1 || items[0] != "hello" {
			t.Errorf("expected [hello], got %v", items)
		}
		if rb.Len() != 0 {
			t.Errorf("expected len 0 after drain, got %d", rb.Len())
		}
	})

	t.Run("push and drain multiple items in FIFO order", func(t *testing.T) {
		rb := NewRingBuffer[int](4)
		rb.Push(1)
		rb.Push(2)
		rb.Push(3)
		items := rb.DrainAll()
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		for i, want := range []int{1, 2, 3} {
			if items[i] != want {
				t.Errorf("items[%d] = %d, want %d", i, items[i], want)
			}
		}
	})

	t.Run("drain empty buffer returns nil", func(t *testing.T) {
		rb := NewRingBuffer[int](4)
		items := rb.DrainAll()
		if items != nil {
			t.Errorf("expected nil, got %v", items)
		}
	})

	t.Run("drain twice returns nil second time", func(t *testing.T) {
		rb := NewRingBuffer[int](4)
		rb.Push(1)
		rb.DrainAll()
		items := rb.DrainAll()
		if items != nil {
			t.Errorf("expected nil on second drain, got %v", items)
		}
	})
}

func TestRingBufferOverflow(t *testing.T) {
	t.Run("drops oldest when full", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Push(1)
		rb.Push(2)
		rb.Push(3)
		rb.Push(4) // should drop 1
		if rb.Len() != 3 {
			t.Errorf("expected len 3, got %d", rb.Len())
		}
		items := rb.DrainAll()
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		for i, want := range []int{2, 3, 4} {
			if items[i] != want {
				t.Errorf("items[%d] = %d, want %d", i, items[i], want)
			}
		}
	})

	t.Run("heavy overflow keeps only last N items", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		for i := 0; i < 10; i++ {
			rb.Push(i)
		}
		items := rb.DrainAll()
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		for i, want := range []int{7, 8, 9} {
			if items[i] != want {
				t.Errorf("items[%d] = %d, want %d", i, items[i], want)
			}
		}
	})

	t.Run("push after drain works correctly", func(t *testing.T) {
		rb := NewRingBuffer[int](2)
		rb.Push(1)
		rb.Push(2)
		rb.DrainAll()
		rb.Push(3)
		rb.Push(4)
		items := rb.DrainAll()
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		for i, want := range []int{3, 4} {
			if items[i] != want {
				t.Errorf("items[%d] = %d, want %d", i, items[i], want)
			}
		}
	})
}

func TestRingBufferConcurrency(t *testing.T) {
	rb := NewRingBuffer[int](16)
	var wg sync.WaitGroup
	const goroutines = 10
	const pushesPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < pushesPerGoroutine; i++ {
				rb.Push(base*pushesPerGoroutine + i)
			}
		}(g)
	}
	wg.Wait()

	// Should have at most 16 items (capacity)
	if rb.Len() > 16 {
		t.Errorf("expected len <= 16, got %d", rb.Len())
	}

	items := rb.DrainAll()
	if len(items) > 16 {
		t.Errorf("expected <= 16 items, got %d", len(items))
	}
}
