package workflow

import "sync"

// LimitedBuffer is a fixed-capacity tail buffer that implements io.Writer.
// It retains the last N bytes written, discarding oldest bytes when full.
// Thread-safe with sync.Mutex.
// Default capacity: 4096 bytes (4KB per FR-7).
type LimitedBuffer struct {
	mu       sync.Mutex
	buf      []byte
	capacity int
	// Ring buffer state: data lives in buf[0:size].
	// When the buffer is full, we shift data to make room.
	// For simplicity we keep a contiguous buffer and shift on overflow.
	size int
}

// NewLimitedBuffer creates a buffer with the given capacity.
func NewLimitedBuffer(capacity int) *LimitedBuffer {
	if capacity <= 0 {
		capacity = DefaultBufferSize
	}
	return &LimitedBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

// Write implements io.Writer. Always returns len(p), nil.
func (b *LimitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n := len(p)
	if n == 0 {
		return 0, nil
	}

	if n >= b.capacity {
		// Data is larger than or equal to capacity: keep only the last capacity bytes.
		copy(b.buf, p[n-b.capacity:])
		b.size = b.capacity
		return n, nil
	}

	// How much space is available?
	avail := b.capacity - b.size
	if n <= avail {
		// Enough room: just append.
		copy(b.buf[b.size:], p)
		b.size += n
	} else {
		// Need to discard oldest bytes to make room.
		// Shift existing data left by (n - avail) bytes.
		discard := n - avail
		copy(b.buf, b.buf[discard:b.size])
		b.size -= discard
		copy(b.buf[b.size:], p)
		b.size += n
	}

	return n, nil
}

// Bytes returns a copy of the current buffer contents.
func (b *LimitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]byte, b.size)
	copy(out, b.buf[:b.size])
	return out
}

// String returns the current buffer contents as a string.
func (b *LimitedBuffer) String() string {
	return string(b.Bytes())
}

// Len returns the number of bytes currently in the buffer.
func (b *LimitedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// Reset clears the buffer.
func (b *LimitedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.size = 0
}
