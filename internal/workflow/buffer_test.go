package workflow

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitedBuffer_Basic(t *testing.T) {
	buf := NewLimitedBuffer(64)
	n, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", buf.String())
	assert.Equal(t, 5, buf.Len())
}

func TestLimitedBuffer_Overflow(t *testing.T) {
	buf := NewLimitedBuffer(8)

	// Write 12 bytes into an 8-byte buffer.
	n, err := buf.Write([]byte("abcdefghijkl"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	// Should retain only the last 8 bytes.
	assert.Equal(t, "efghijkl", buf.String())
	assert.Equal(t, 8, buf.Len())
}

func TestLimitedBuffer_ExactCapacity(t *testing.T) {
	buf := NewLimitedBuffer(5)
	n, err := buf.Write([]byte("abcde"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "abcde", buf.String())
	assert.Equal(t, 5, buf.Len())
}

func TestLimitedBuffer_MultipleWrites(t *testing.T) {
	buf := NewLimitedBuffer(8)

	// Write "abcdef" (6 bytes) — fits.
	_, _ = buf.Write([]byte("abcdef"))
	assert.Equal(t, "abcdef", buf.String())

	// Write "ghij" (4 bytes) — total 10, capacity 8 → discard oldest 2.
	_, _ = buf.Write([]byte("ghij"))
	assert.Equal(t, "cdefghij", buf.String())
	assert.Equal(t, 8, buf.Len())

	// Write "klmn" (4 bytes) — discard oldest 4.
	_, _ = buf.Write([]byte("klmn"))
	assert.Equal(t, "ghijklmn", buf.String())
	assert.Equal(t, 8, buf.Len())
}

func TestLimitedBuffer_LargeWrite(t *testing.T) {
	buf := NewLimitedBuffer(4)

	// Write 12 bytes (3x capacity).
	n, err := buf.Write([]byte("abcdefghijkl"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	// Should retain only the last 4 bytes.
	assert.Equal(t, "ijkl", buf.String())
	assert.Equal(t, 4, buf.Len())
}

func TestLimitedBuffer_ConcurrentWrites(t *testing.T) {
	buf := NewLimitedBuffer(1024)

	var wg sync.WaitGroup
	writers := 10
	bytesPerWriter := 200

	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			data := make([]byte, bytesPerWriter)
			for j := range data {
				data[j] = byte('A')
			}
			_, _ = buf.Write(data)
		}()
	}
	wg.Wait()

	// Total written: 2000 bytes into 1024-byte buffer.
	// Buffer should contain exactly 1024 bytes, all 'A'.
	assert.Equal(t, 1024, buf.Len())
	bytes := buf.Bytes()
	for i, b := range bytes {
		assert.Equal(t, byte('A'), b, "byte %d should be 'A'", i)
	}
}

func TestLimitedBuffer_Reset(t *testing.T) {
	buf := NewLimitedBuffer(64)
	_, _ = buf.Write([]byte("some data"))
	assert.Equal(t, 9, buf.Len())

	buf.Reset()
	assert.Equal(t, 0, buf.Len())
	assert.Equal(t, "", buf.String())
	assert.Empty(t, buf.Bytes())
}

func TestLimitedBuffer_EmptyWrite(t *testing.T) {
	buf := NewLimitedBuffer(64)
	n, err := buf.Write([]byte{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, buf.Len())
}

func TestLimitedBuffer_DefaultCapacity(t *testing.T) {
	// Zero capacity should use DefaultBufferSize.
	buf := NewLimitedBuffer(0)
	assert.NotNil(t, buf)
	// Write DefaultBufferSize bytes — should fit exactly.
	data := make([]byte, DefaultBufferSize)
	for i := range data {
		data[i] = byte('x')
	}
	n, err := buf.Write(data)
	require.NoError(t, err)
	assert.Equal(t, DefaultBufferSize, n)
	assert.Equal(t, DefaultBufferSize, buf.Len())
}

func TestLimitedBuffer_NegativeCapacity(t *testing.T) {
	// Negative capacity should use DefaultBufferSize.
	buf := NewLimitedBuffer(-1)
	assert.NotNil(t, buf)
	assert.Equal(t, 0, buf.Len())
}

func TestLimitedBuffer_BytesReturnsCopy(t *testing.T) {
	buf := NewLimitedBuffer(64)
	_, _ = buf.Write([]byte("hello"))

	// Modify the returned slice — should not affect the buffer.
	bytes := buf.Bytes()
	bytes[0] = 'X'
	assert.Equal(t, "hello", buf.String())
}
