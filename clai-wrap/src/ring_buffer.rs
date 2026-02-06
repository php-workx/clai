//! Lock-free SPSC ring buffer for PTY output capture.
//!
//! This module provides a single-producer single-consumer ring buffer that never
//! blocks on writes. When the buffer is full, it overwrites the oldest data and
//! sets an overflow flag.
//!
//! # Usage
//!
//! ```
//! use clai_wrap::ring_buffer::SpscRingBuffer;
//!
//! let mut buffer = SpscRingBuffer::new(1024);
//! buffer.push(b"hello world");
//! let data = buffer.drain();
//! assert_eq!(&data, b"hello world");
//! ```

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use ringbuf::{
    traits::{Consumer, Observer, Producer, Split},
    HeapRb,
};

/// Shared overflow flag between producer and consumer.
#[derive(Debug, Clone)]
pub struct OverflowFlag(Arc<AtomicBool>);

impl OverflowFlag {
    /// Returns whether overflow has occurred.
    #[must_use]
    pub fn has_overflowed(&self) -> bool {
        self.0.load(Ordering::SeqCst)
    }

    /// Resets the overflow flag.
    pub fn reset(&self) {
        self.0.store(false, Ordering::SeqCst);
    }
}

/// A ring buffer that overwrites oldest data on overflow.
///
/// This buffer is designed for capturing PTY output where:
/// - A single producer thread writes data from the PTY
/// - A single consumer thread reads data for processing
/// - Writes must never block (overflow drops oldest data)
/// - Overflow tracking is available for diagnostics
///
/// # Usage
///
/// ```
/// use clai_wrap::ring_buffer::SpscRingBuffer;
///
/// let mut buffer = SpscRingBuffer::new(100);
/// buffer.push(b"hello");
/// assert_eq!(buffer.drain(), b"hello");
/// ```
pub struct SpscRingBuffer {
    producer: ringbuf::HeapProd<u8>,
    consumer: ringbuf::HeapCons<u8>,
    overflow: Arc<AtomicBool>,
    capacity: usize,
}

impl SpscRingBuffer {
    /// Creates a new ring buffer with the specified capacity.
    ///
    /// # Arguments
    ///
    /// * `capacity` - Maximum number of bytes the buffer can hold
    ///
    /// # Returns
    ///
    /// A new `SpscRingBuffer` instance
    #[must_use]
    pub fn new(capacity: usize) -> Self {
        let rb = HeapRb::<u8>::new(capacity);
        let (producer, consumer) = rb.split();

        Self {
            producer,
            consumer,
            overflow: Arc::new(AtomicBool::new(false)),
            capacity,
        }
    }

    /// Pushes bytes into the buffer, overwriting oldest data if necessary.
    ///
    /// This method never blocks. If there isn't enough space for the new data,
    /// it will discard the oldest bytes to make room and set the overflow flag.
    ///
    /// # Arguments
    ///
    /// * `bytes` - The bytes to push into the buffer
    pub fn push(&mut self, bytes: &[u8]) {
        // If the data is larger than the entire buffer, only keep the last `capacity` bytes
        let bytes_to_write = if bytes.len() > self.capacity {
            self.overflow.store(true, Ordering::SeqCst);
            &bytes[bytes.len() - self.capacity..]
        } else {
            bytes
        };

        let available = self.producer.vacant_len();
        let needed = bytes_to_write.len();

        if needed > available {
            // Need to discard oldest data to make room
            let discard = needed - available;
            self.consumer.skip(discard);
            self.overflow.store(true, Ordering::SeqCst);
        }

        // Now we have enough space, push the data
        self.producer.push_slice(bytes_to_write);
    }

    /// Drains all available data from the buffer.
    ///
    /// # Returns
    ///
    /// A `Vec<u8>` containing all bytes currently in the buffer.
    /// The buffer will be empty after this call.
    pub fn drain(&mut self) -> Vec<u8> {
        let len = self.consumer.occupied_len();
        let mut result = vec![0u8; len];
        self.consumer.pop_slice(&mut result);
        result
    }

    /// Returns whether the buffer has overflowed since the last reset.
    ///
    /// # Returns
    ///
    /// `true` if data has been lost due to overflow, `false` otherwise
    #[must_use]
    pub fn has_overflowed(&self) -> bool {
        self.overflow.load(Ordering::SeqCst)
    }

    /// Resets the overflow flag to `false`.
    pub fn reset_overflow(&self) {
        self.overflow.store(false, Ordering::SeqCst);
    }

    /// Returns the capacity of the buffer.
    #[must_use]
    pub const fn capacity(&self) -> usize {
        self.capacity
    }

    /// Returns the number of bytes currently in the buffer.
    #[must_use]
    pub fn len(&self) -> usize {
        self.consumer.occupied_len()
    }

    /// Returns `true` if the buffer is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Returns the overflow flag for external monitoring.
    #[must_use]
    pub fn overflow_flag(&self) -> OverflowFlag {
        OverflowFlag(Arc::clone(&self.overflow))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::mpsc;
    use std::thread;

    #[test]
    fn test_append_under_cap() {
        let mut buffer = SpscRingBuffer::new(100);

        buffer.push(b"hello");
        buffer.push(b" ");
        buffer.push(b"world");

        let data = buffer.drain();
        assert_eq!(data, b"hello world");
        assert!(!buffer.has_overflowed());
    }

    #[test]
    fn test_append_exceeds_cap() {
        let mut buffer = SpscRingBuffer::new(10);

        // Push more data than capacity
        buffer.push(b"hello"); // 5 bytes
        buffer.push(b"world!"); // 6 bytes, total 11, exceeds 10

        let data = buffer.drain();
        // Should have dropped oldest byte ('h') to make room
        assert_eq!(data.len(), 10);
        // Data should be "elloworld!" - the oldest byte was dropped
        assert_eq!(&data, b"elloworld!");
        assert!(buffer.has_overflowed());
    }

    #[test]
    fn test_multiple_wraps() {
        let mut buffer = SpscRingBuffer::new(10);

        // Fill and wrap multiple times
        for i in 0u8..30 {
            buffer.push(&[i]);
        }

        let data = buffer.drain();
        // Should contain the last 10 bytes: 20, 21, 22, 23, 24, 25, 26, 27, 28, 29
        assert_eq!(data.len(), 10);
        assert_eq!(data, vec![20, 21, 22, 23, 24, 25, 26, 27, 28, 29]);
        assert!(buffer.has_overflowed());
    }

    #[test]
    fn test_overflow_flag() {
        let mut buffer = SpscRingBuffer::new(5);

        // No overflow yet
        assert!(!buffer.has_overflowed());

        // Push within capacity
        buffer.push(b"abc");
        assert!(!buffer.has_overflowed());

        // Push to cause overflow
        buffer.push(b"defgh"); // 3 + 5 = 8 > 5
        assert!(buffer.has_overflowed());

        // Reset the flag
        buffer.reset_overflow();
        assert!(!buffer.has_overflowed());

        // Push within remaining capacity (after drain)
        buffer.drain();
        buffer.push(b"xy");
        assert!(!buffer.has_overflowed());

        // Overflow again
        buffer.push(b"12345"); // 2 + 5 = 7 > 5
        assert!(buffer.has_overflowed());
    }

    #[test]
    fn test_thread_safety_with_channel() {
        // Test thread-safe communication using channels
        // This demonstrates the pattern that should be used for multi-threaded access
        let (tx, rx) = mpsc::channel::<Vec<u8>>();

        // Producer thread creates its own buffer, processes, sends result
        let producer_handle = thread::spawn(move || {
            let mut local_buffer = SpscRingBuffer::new(1000);
            for i in 0u8..100 {
                local_buffer.push(&[i]);
            }
            let data = local_buffer.drain();
            tx.send(data).unwrap();
        });

        // Consumer receives the data
        let received = rx.recv().unwrap();

        producer_handle.join().unwrap();

        // Verify data integrity
        assert_eq!(received.len(), 100);
        for (i, &byte) in received.iter().enumerate() {
            assert_eq!(byte, i as u8);
        }
    }

    #[test]
    fn test_overflow_flag_external() {
        let mut buffer = SpscRingBuffer::new(5);
        let flag = buffer.overflow_flag();

        assert!(!flag.has_overflowed());

        // Cause overflow
        buffer.push(b"hello world"); // > 5 bytes
        assert!(flag.has_overflowed());

        // Reset via flag
        flag.reset();
        assert!(!flag.has_overflowed());
        assert!(!buffer.has_overflowed());
    }

    #[test]
    fn test_large_single_push_exceeds_capacity() {
        let mut buffer = SpscRingBuffer::new(5);

        // Push data larger than entire capacity
        buffer.push(b"hello world!"); // 12 bytes > 5 capacity

        let data = buffer.drain();
        // Should only contain the last 5 bytes: "orld!"
        assert_eq!(data, b"orld!");
        assert!(buffer.has_overflowed());
    }

    #[test]
    fn test_empty_buffer() {
        let mut buffer = SpscRingBuffer::new(10);

        assert!(buffer.is_empty());
        assert_eq!(buffer.len(), 0);

        let data = buffer.drain();
        assert!(data.is_empty());

        buffer.push(b"test");
        assert!(!buffer.is_empty());
        assert_eq!(buffer.len(), 4);
    }
}
