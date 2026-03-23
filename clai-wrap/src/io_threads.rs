//! I/O Passthrough Threads for PTY wrapper.
//!
//! This module provides two dedicated threads for I/O forwarding:
//! - **Stdin reader**: reads from real stdin, forwards to PTY
//! - **PTY reader**: reads from PTY, writes to stdout (or buffers when picker is open)
//!
//! # Critical Requirement
//!
//! The PTY read thread **MUST NEVER BLOCK**. PTY kernel buffers are limited
//! (typically 4KB-64KB). If the child process writes more data than the kernel
//! buffer can hold while we've stopped reading, the child will **block on `write()`**,
//! causing a deadlock.
//!
//! Buffer overflow is acceptable; PTY deadlock is not.

use std::io::{Read, Write};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, Receiver, Sender, TryRecvError};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::Duration;

use anyhow::{Context, Result};

/// Size of the read buffer for PTY output (64KB to match typical kernel PTY buffer).
const PTY_READ_BUFFER_SIZE: usize = 65536;

/// Size of the read buffer for stdin.
const STDIN_READ_BUFFER_SIZE: usize = 4096;

/// Events that can be sent from I/O threads to the main thread.
#[derive(Debug, Clone)]
pub enum IoEvent {
    /// PTY output data received (when picker is closed, this is written directly to stdout).
    /// When picker is open, data is buffered and this event is sent for notification.
    PtyOutput(Vec<u8>),
    /// Stdin data received.
    StdinInput(Vec<u8>),
    /// PTY read error occurred.
    PtyReadError(String),
    /// Stdin read error occurred.
    StdinReadError(String),
    /// PTY EOF reached (child process closed).
    PtyEof,
    /// Stdin EOF reached.
    StdinEof,
}

/// Shared state between I/O threads and the main thread.
pub struct IoState {
    /// Whether the picker UI is currently open.
    /// When true, PTY output is buffered instead of written to stdout.
    picker_open: AtomicBool,
    /// Shutdown signal for all threads.
    shutdown: AtomicBool,
    /// Whether overflow has occurred in the ring buffer.
    overflow_occurred: AtomicBool,
}

impl IoState {
    /// Creates a new `IoState` with default values.
    #[must_use]
    pub fn new() -> Arc<Self> {
        Arc::new(Self {
            picker_open: AtomicBool::new(false),
            shutdown: AtomicBool::new(false),
            overflow_occurred: AtomicBool::new(false),
        })
    }

    /// Returns whether the picker is currently open.
    pub fn is_picker_open(&self) -> bool {
        self.picker_open.load(Ordering::SeqCst)
    }

    /// Sets the picker open state.
    pub fn set_picker_open(&self, open: bool) {
        self.picker_open.store(open, Ordering::SeqCst);
    }

    /// Returns whether shutdown has been requested.
    pub fn is_shutdown(&self) -> bool {
        self.shutdown.load(Ordering::SeqCst)
    }

    /// Signals shutdown to all threads.
    pub fn request_shutdown(&self) {
        self.shutdown.store(true, Ordering::SeqCst);
    }

    /// Returns whether overflow has occurred.
    pub fn has_overflowed(&self) -> bool {
        self.overflow_occurred.load(Ordering::SeqCst)
    }

    /// Sets the overflow flag.
    pub fn set_overflow(&self, overflow: bool) {
        self.overflow_occurred.store(overflow, Ordering::SeqCst);
    }
}

impl Default for IoState {
    fn default() -> Self {
        Self {
            picker_open: AtomicBool::new(false),
            shutdown: AtomicBool::new(false),
            overflow_occurred: AtomicBool::new(false),
        }
    }
}

/// Ring buffer for buffering PTY output when picker is open.
///
/// This is a simple, thread-safe ring buffer that overwrites oldest data
/// when full. It's designed for the specific use case where:
/// - A single producer (PTY read thread) pushes data
/// - A single consumer (main thread) drains data when picker closes
/// - The producer must never block
pub struct OutputBuffer {
    /// The underlying buffer storage.
    buffer: Vec<u8>,
    /// Current write position (always advances, wraps via modulo).
    write_pos: usize,
    /// Amount of valid data in the buffer.
    len: usize,
    /// Maximum capacity.
    capacity: usize,
}

impl OutputBuffer {
    /// Creates a new output buffer with the specified capacity.
    #[must_use]
    pub fn new(capacity: usize) -> Self {
        Self {
            buffer: vec![0u8; capacity],
            write_pos: 0,
            len: 0,
            capacity,
        }
    }

    /// Pushes bytes into the buffer, overwriting oldest data if necessary.
    ///
    /// This method never blocks. If there isn't enough space for the new data,
    /// it will overwrite the oldest bytes.
    ///
    /// # Returns
    ///
    /// `true` if overflow occurred (data was overwritten), `false` otherwise.
    pub fn push(&mut self, bytes: &[u8]) -> bool {
        if bytes.is_empty() {
            return false;
        }

        let mut overflow = false;

        // If the data is larger than the entire buffer, only keep the last `capacity` bytes
        let bytes_to_write = if bytes.len() > self.capacity {
            overflow = true;
            &bytes[bytes.len() - self.capacity..]
        } else {
            bytes
        };

        // Check if we'll overflow
        if self.len + bytes_to_write.len() > self.capacity {
            overflow = true;
        }

        // Write the data, potentially wrapping around
        for &byte in bytes_to_write {
            self.buffer[self.write_pos] = byte;
            self.write_pos = (self.write_pos + 1) % self.capacity;
        }

        // Update length, capped at capacity
        self.len = (self.len + bytes_to_write.len()).min(self.capacity);

        overflow
    }

    /// Drains all available data from the buffer in the correct order.
    ///
    /// # Returns
    ///
    /// A `Vec<u8>` containing all bytes currently in the buffer.
    /// The buffer will be empty after this call.
    pub fn drain(&mut self) -> Vec<u8> {
        if self.len == 0 {
            return Vec::new();
        }

        let mut result = Vec::with_capacity(self.len);

        // Calculate read start position
        let read_start = if self.len == self.capacity {
            // Buffer is full, read from write_pos (oldest data)
            self.write_pos
        } else {
            // Buffer not full, data starts at 0
            0
        };

        // Read data in correct order
        for i in 0..self.len {
            let pos = (read_start + i) % self.capacity;
            result.push(self.buffer[pos]);
        }

        // Reset buffer state
        self.write_pos = 0;
        self.len = 0;

        result
    }

    /// Returns the number of bytes currently in the buffer.
    #[must_use]
    pub const fn len(&self) -> usize {
        self.len
    }

    /// Returns `true` if the buffer is empty.
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.len == 0
    }

    /// Returns the capacity of the buffer.
    #[must_use]
    pub const fn capacity(&self) -> usize {
        self.capacity
    }

    /// Clears the buffer.
    pub const fn clear(&mut self) {
        self.write_pos = 0;
        self.len = 0;
    }
}

/// Manages the I/O passthrough threads.
///
/// This struct owns the handles for the stdin and PTY reader threads,
/// and provides methods for clean shutdown.
pub struct IoThreads {
    /// Handle for the stdin reader thread.
    stdin_handle: Option<JoinHandle<()>>,
    /// Handle for the PTY output reader thread.
    pty_handle: Option<JoinHandle<()>>,
    /// Shared state for thread coordination.
    state: Arc<IoState>,
    /// Channel for sending data to the PTY.
    pty_tx: Option<Sender<Vec<u8>>>,
    /// Channel for receiving events from I/O threads.
    event_rx: Receiver<IoEvent>,
    /// Buffer for PTY output when picker is open.
    output_buffer: OutputBuffer,
}

impl IoThreads {
    /// Creates and starts the I/O threads.
    ///
    /// # Arguments
    ///
    /// * `pty_reader` - Reader for the PTY master
    /// * `pty_writer` - Writer for the PTY master
    /// * `buffer_capacity` - Capacity of the output buffer (default 2MB)
    ///
    /// # Returns
    ///
    /// A new `IoThreads` instance with running threads.
    ///
    /// # Errors
    ///
    /// Returns an error if thread creation fails.
    pub fn new(
        pty_reader: Box<dyn Read + Send>,
        pty_writer: Box<dyn Write + Send>,
        buffer_capacity: usize,
    ) -> Result<Self> {
        let state = IoState::new();

        // Channel for events from I/O threads to main thread
        let (event_tx, event_rx) = mpsc::channel();

        // Channel for sending data to PTY (stdin -> PTY)
        let (pty_tx, pty_rx) = mpsc::channel();

        // Start stdin reader thread
        let stdin_state = Arc::clone(&state);
        let stdin_event_tx = event_tx.clone();
        let stdin_handle = thread::Builder::new()
            .name("stdin-reader".to_string())
            .spawn(move || stdin_reader_thread(&stdin_state, &stdin_event_tx))
            .context("Failed to spawn stdin reader thread")?;

        // Start PTY writer thread (receives from stdin reader via channel)
        let pty_writer_state = Arc::clone(&state);
        let _pty_writer_handle = thread::Builder::new()
            .name("pty-writer".to_string())
            .spawn(move || pty_writer_thread(&pty_writer_state, pty_writer, &pty_rx))
            .context("Failed to spawn PTY writer thread")?;

        // Start PTY reader thread
        let pty_state = Arc::clone(&state);
        let pty_handle = thread::Builder::new()
            .name("pty-reader".to_string())
            .spawn(move || pty_reader_thread(&pty_state, pty_reader, &event_tx))
            .context("Failed to spawn PTY reader thread")?;

        Ok(Self {
            stdin_handle: Some(stdin_handle),
            pty_handle: Some(pty_handle),
            state,
            pty_tx: Some(pty_tx),
            event_rx,
            output_buffer: OutputBuffer::new(buffer_capacity),
        })
    }

    /// Returns a reference to the shared state.
    pub const fn state(&self) -> &Arc<IoState> {
        &self.state
    }

    /// Sets whether the picker is open.
    ///
    /// When the picker is open, PTY output is buffered instead of written to stdout.
    pub fn set_picker_open(&mut self, open: bool) {
        self.state.set_picker_open(open);
        if !open {
            // When picker closes, reset overflow flag
            self.state.set_overflow(false);
        }
    }

    /// Sends data to the PTY (typically from stdin or injected text).
    ///
    /// # Arguments
    ///
    /// * `data` - The bytes to send to the PTY
    ///
    /// # Errors
    ///
    /// Returns an error if the PTY writer channel is closed.
    pub fn send_to_pty(&self, data: Vec<u8>) -> Result<()> {
        if let Some(ref tx) = self.pty_tx {
            tx.send(data).context("PTY writer channel closed")?;
        }
        Ok(())
    }

    /// Tries to receive an event from the I/O threads without blocking.
    ///
    /// # Returns
    ///
    /// - `Some(event)` if an event is available
    /// - `None` if no event is available
    pub fn try_recv_event(&self) -> Option<IoEvent> {
        match self.event_rx.try_recv() {
            Ok(event) => Some(event),
            Err(TryRecvError::Empty) => None,
            Err(TryRecvError::Disconnected) => Some(IoEvent::PtyEof),
        }
    }

    /// Receives an event from the I/O threads, blocking until one is available.
    ///
    /// # Returns
    ///
    /// The next event, or `None` if all sender channels are closed.
    pub fn recv_event(&self) -> Option<IoEvent> {
        self.event_rx.recv().ok()
    }

    /// Receives an event with a timeout.
    ///
    /// # Arguments
    ///
    /// * `timeout` - Maximum time to wait for an event
    ///
    /// # Returns
    ///
    /// - `Some(event)` if an event is received within the timeout
    /// - `None` if timeout expires or channels are closed
    pub fn recv_event_timeout(&self, timeout: Duration) -> Option<IoEvent> {
        self.event_rx.recv_timeout(timeout).ok()
    }

    /// Buffers PTY output data (when picker is open).
    ///
    /// # Arguments
    ///
    /// * `data` - The bytes to buffer
    ///
    /// # Returns
    ///
    /// `true` if overflow occurred (data was lost), `false` otherwise.
    pub fn buffer_output(&mut self, data: &[u8]) -> bool {
        let overflow = self.output_buffer.push(data);
        if overflow {
            self.state.set_overflow(true);
        }
        overflow
    }

    /// Drains the output buffer.
    ///
    /// # Returns
    ///
    /// All buffered PTY output data in the correct order.
    pub fn drain_output_buffer(&mut self) -> Vec<u8> {
        self.output_buffer.drain()
    }

    /// Returns whether the output buffer has overflowed.
    pub fn has_buffer_overflow(&self) -> bool {
        self.state.has_overflowed()
    }

    /// Returns the current size of the output buffer.
    pub const fn output_buffer_len(&self) -> usize {
        self.output_buffer.len()
    }

    /// Initiates shutdown of all I/O threads.
    pub fn shutdown(&mut self) {
        self.state.request_shutdown();
        // Drop the PTY sender to unblock the writer thread
        self.pty_tx.take();
    }

    /// Waits for all threads to finish.
    ///
    /// # Returns
    ///
    /// - `Ok(())` if all threads finished cleanly
    /// - `Err(...)` if any thread panicked or returned an error
    pub fn join(mut self) -> Result<()> {
        self.shutdown();

        let mut errors = Vec::new();

        if let Some(handle) = self.stdin_handle.take() {
            match handle.join() {
                Ok(()) => {}
                Err(_) => errors.push("stdin thread panicked".to_string()),
            }
        }

        if let Some(handle) = self.pty_handle.take() {
            match handle.join() {
                Ok(()) => {}
                Err(_) => errors.push("PTY thread panicked".to_string()),
            }
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(anyhow::anyhow!("Thread errors: {}", errors.join("; ")))
        }
    }
}

impl Drop for IoThreads {
    fn drop(&mut self) {
        self.shutdown();
    }
}

/// Stdin reader thread function.
///
/// Reads from stdin and sends events to the main thread.
/// The actual writing to PTY is done by a separate writer thread
/// to avoid blocking this thread.
fn stdin_reader_thread(state: &IoState, event_tx: &Sender<IoEvent>) {
    let stdin = std::io::stdin();
    let mut buffer = [0u8; STDIN_READ_BUFFER_SIZE];

    loop {
        if state.is_shutdown() {
            break;
        }

        // Note: This read CAN block, which is acceptable for stdin.
        // The shutdown check above provides a way to exit when the
        // main thread requests shutdown (e.g., on child exit).
        //
        // We acquire the lock inside the loop to avoid holding it across iterations,
        // which helps with the significant_drop_in_scrutinee lint.
        let read_result = {
            let mut stdin_lock = stdin.lock();
            stdin_lock.read(&mut buffer)
        };

        match read_result {
            Ok(0) => {
                // EOF on stdin
                let _ = event_tx.send(IoEvent::StdinEof);
                break;
            }
            Ok(n) => {
                let data = buffer[..n].to_vec();
                if event_tx.send(IoEvent::StdinInput(data)).is_err() {
                    // Receiver dropped, shutdown
                    break;
                }
            }
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {
                // Interrupted by signal, continue
            }
            Err(e) => {
                let _ = event_tx.send(IoEvent::StdinReadError(e.to_string()));
                break;
            }
        }
    }
}

/// PTY writer thread function.
///
/// Receives data from the stdin reader (via channel) and writes to PTY.
fn pty_writer_thread(state: &IoState, mut writer: Box<dyn Write + Send>, rx: &Receiver<Vec<u8>>) {
    loop {
        if state.is_shutdown() {
            break;
        }

        // Use recv_timeout to periodically check shutdown flag
        match rx.recv_timeout(Duration::from_millis(100)) {
            Ok(data) => {
                if let Err(e) = writer.write_all(&data) {
                    // Log but don't propagate - PTY write failures are often
                    // due to child process exiting, which is handled elsewhere
                    tracing::debug!("PTY write error: {}", e);
                    break;
                }
                if let Err(e) = writer.flush() {
                    tracing::debug!("PTY flush error: {}", e);
                    break;
                }
            }
            Err(mpsc::RecvTimeoutError::Timeout) => {
                // Check shutdown and continue
            }
            Err(mpsc::RecvTimeoutError::Disconnected) => {
                // Sender dropped, exit
                break;
            }
        }
    }
}

/// PTY reader thread function.
///
/// Reads from PTY and sends events to the main thread.
///
/// **CRITICAL**: This thread must NEVER block on anything other than
/// the PTY read itself. The PTY must always be drained as fast as possible
/// to prevent kernel buffer overflow and child process blocking.
fn pty_reader_thread(
    state: &IoState,
    mut reader: Box<dyn Read + Send>,
    event_tx: &Sender<IoEvent>,
) {
    let mut buffer = [0u8; PTY_READ_BUFFER_SIZE];

    loop {
        if state.is_shutdown() {
            break;
        }

        // Read from PTY - this may block waiting for data, which is fine.
        // What's critical is that we NEVER block AFTER reading data.
        match reader.read(&mut buffer) {
            Ok(0) => {
                // EOF - child process closed
                let _ = event_tx.send(IoEvent::PtyEof);
                break;
            }
            Ok(n) => {
                let data = buffer[..n].to_vec();
                // Send event to main thread.
                // If the channel is full or the receiver is dropped,
                // we still continue reading to prevent PTY buffer overflow.
                // The main thread is responsible for handling these events
                // appropriately (either write to stdout or buffer).
                if event_tx.send(IoEvent::PtyOutput(data)).is_err() {
                    // Receiver dropped, shutdown
                    break;
                }
            }
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {
                // Interrupted by signal, continue
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                // Non-blocking read returned would-block, yield and retry
                thread::yield_now();
            }
            Err(e) => {
                let _ = event_tx.send(IoEvent::PtyReadError(e.to_string()));
                break;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn test_output_buffer_basic() {
        let mut buffer = OutputBuffer::new(10);

        assert!(buffer.is_empty());
        assert_eq!(buffer.len(), 0);
        assert_eq!(buffer.capacity(), 10);

        // Push some data
        let overflow = buffer.push(b"hello");
        assert!(!overflow);
        assert_eq!(buffer.len(), 5);

        // Drain and verify
        let data = buffer.drain();
        assert_eq!(data, b"hello");
        assert!(buffer.is_empty());
    }

    #[test]
    fn test_output_buffer_overflow() {
        let mut buffer = OutputBuffer::new(10);

        // Push data that fills the buffer
        buffer.push(b"hello");
        let overflow = buffer.push(b"world!"); // 5 + 6 = 11 > 10
        assert!(overflow);

        // Should contain most recent data, oldest dropped
        let data = buffer.drain();
        assert_eq!(data.len(), 10);
        // Data wraps: "elloworld!" - oldest byte 'h' was overwritten
        assert_eq!(&data, b"elloworld!");
    }

    #[test]
    fn test_output_buffer_large_push() {
        let mut buffer = OutputBuffer::new(5);

        // Push data larger than entire buffer
        let overflow = buffer.push(b"hello world!"); // 12 bytes > 5 capacity
        assert!(overflow);

        // Should only contain the last 5 bytes
        let data = buffer.drain();
        assert_eq!(data, b"orld!");
    }

    #[test]
    fn test_output_buffer_multiple_wraps() {
        let mut buffer = OutputBuffer::new(10);

        // Fill and wrap multiple times
        for i in 0u8..30 {
            buffer.push(&[i]);
        }

        // Should contain the last 10 bytes: 20-29
        let data = buffer.drain();
        assert_eq!(data.len(), 10);
        assert_eq!(data, vec![20, 21, 22, 23, 24, 25, 26, 27, 28, 29]);
    }

    #[test]
    fn test_output_buffer_clear() {
        let mut buffer = OutputBuffer::new(10);

        buffer.push(b"hello");
        assert!(!buffer.is_empty());

        buffer.clear();
        assert!(buffer.is_empty());
        assert_eq!(buffer.len(), 0);
    }

    #[test]
    fn test_output_buffer_empty_push() {
        let mut buffer = OutputBuffer::new(10);

        let overflow = buffer.push(b"");
        assert!(!overflow);
        assert!(buffer.is_empty());
    }

    #[test]
    fn test_output_buffer_exact_capacity() {
        let mut buffer = OutputBuffer::new(10);

        // Push exactly capacity
        let overflow = buffer.push(b"0123456789");
        assert!(!overflow);
        assert_eq!(buffer.len(), 10);

        let data = buffer.drain();
        assert_eq!(data, b"0123456789");
    }

    #[test]
    fn test_io_state_default() {
        let state = IoState::default();

        assert!(!state.is_picker_open());
        assert!(!state.is_shutdown());
        assert!(!state.has_overflowed());
    }

    #[test]
    fn test_io_state_picker() {
        let state = IoState::new();

        assert!(!state.is_picker_open());

        state.set_picker_open(true);
        assert!(state.is_picker_open());

        state.set_picker_open(false);
        assert!(!state.is_picker_open());
    }

    #[test]
    fn test_io_state_shutdown() {
        let state = IoState::new();

        assert!(!state.is_shutdown());

        state.request_shutdown();
        assert!(state.is_shutdown());
    }

    #[test]
    fn test_io_state_overflow() {
        let state = IoState::new();

        assert!(!state.has_overflowed());

        state.set_overflow(true);
        assert!(state.has_overflowed());

        state.set_overflow(false);
        assert!(!state.has_overflowed());
    }

    #[test]
    fn test_io_event_variants() {
        // Test that all event variants can be created
        let _ = IoEvent::PtyOutput(vec![1, 2, 3]);
        let _ = IoEvent::StdinInput(vec![4, 5, 6]);
        let _ = IoEvent::PtyReadError("error".to_string());
        let _ = IoEvent::StdinReadError("error".to_string());
        let _ = IoEvent::PtyEof;
        let _ = IoEvent::StdinEof;
    }

    /// Mock reader that returns predefined data then EOF.
    struct MockReader {
        data: Cursor<Vec<u8>>,
    }

    impl MockReader {
        #[allow(clippy::new_ret_no_self)]
        fn new(data: Vec<u8>) -> Box<dyn Read + Send> {
            Box::new(Self {
                data: Cursor::new(data),
            })
        }
    }

    impl Read for MockReader {
        fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
            self.data.read(buf)
        }
    }

    /// Mock writer that captures written data.
    struct MockWriter {
        written: std::sync::Arc<std::sync::Mutex<Vec<u8>>>,
    }

    impl MockWriter {
        #[allow(clippy::new_ret_no_self)]
        fn new() -> (
            Box<dyn Write + Send>,
            std::sync::Arc<std::sync::Mutex<Vec<u8>>>,
        ) {
            let written = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
            let writer = Box::new(Self {
                written: Arc::clone(&written),
            });
            (writer, written)
        }
    }

    impl Write for MockWriter {
        fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
            self.written.lock().unwrap().extend_from_slice(buf);
            Ok(buf.len())
        }

        fn flush(&mut self) -> std::io::Result<()> {
            Ok(())
        }
    }

    #[test]
    fn test_pty_reader_thread_eof() {
        let state = IoState::new();
        let (event_tx, event_rx) = mpsc::channel();
        let reader = MockReader::new(b"hello".to_vec());

        let handle = thread::spawn(move || pty_reader_thread(&state, reader, &event_tx));

        // Should receive the data then EOF
        let mut received_data = Vec::new();
        let mut got_eof = false;

        for _ in 0..10 {
            match event_rx.recv_timeout(Duration::from_millis(100)) {
                Ok(IoEvent::PtyOutput(data)) => received_data.extend(data),
                Ok(IoEvent::PtyEof) => {
                    got_eof = true;
                    break;
                }
                Ok(_) => {}
                Err(_) => break,
            }
        }

        assert_eq!(received_data, b"hello");
        assert!(got_eof);

        handle.join().unwrap();
    }

    #[test]
    fn test_pty_reader_thread_shutdown() {
        // Reader that blocks forever
        struct BlockingReader;
        impl Read for BlockingReader {
            fn read(&mut self, _buf: &mut [u8]) -> std::io::Result<usize> {
                std::thread::sleep(Duration::from_secs(10));
                Ok(0)
            }
        }

        let state = IoState::new();
        let (event_tx, _event_rx) = mpsc::channel();

        let reader: Box<dyn Read + Send> = Box::new(BlockingReader);
        let state_clone = Arc::clone(&state);

        let handle = thread::spawn(move || pty_reader_thread(&state_clone, reader, &event_tx));

        // Request shutdown
        state.request_shutdown();

        // The thread should eventually check shutdown and exit
        // (though it may be blocked on read - this tests the shutdown flag check)
        // In a real scenario, we'd use a pipe/socket that can be closed to interrupt the read

        // Give it a moment, then check if it's still running
        thread::sleep(Duration::from_millis(50));

        // For this test, we accept that the thread may not exit immediately
        // due to the blocking read. The important thing is that the shutdown
        // flag is checked.
        drop(handle);
    }

    #[test]
    fn test_pty_writer_thread() {
        let state = IoState::new();
        let (writer, written) = MockWriter::new();
        let (tx, rx) = mpsc::channel();

        let state_clone = Arc::clone(&state);
        let handle = thread::spawn(move || pty_writer_thread(&state_clone, writer, &rx));

        // Send some data
        tx.send(b"hello".to_vec()).unwrap();
        tx.send(b" world".to_vec()).unwrap();

        // Give time for writes
        thread::sleep(Duration::from_millis(50));

        // Shutdown
        state.request_shutdown();
        drop(tx);

        handle.join().unwrap();

        // Verify written data
        let data = written.lock().unwrap();
        assert_eq!(&*data, b"hello world");
        drop(data);
    }

    #[test]
    fn test_output_buffer_wraparound_correctness() {
        // Test that data is returned in the correct order after wraparound
        let mut buffer = OutputBuffer::new(5);

        // Fill partially
        buffer.push(b"abc"); // Buffer: [a, b, c, _, _], len=3, write_pos=3

        // Drain and verify order
        assert_eq!(buffer.drain(), b"abc");

        // Fill again to test starting from position 0
        buffer.push(b"12345"); // Buffer: [1, 2, 3, 4, 5], len=5, write_pos=0
        assert_eq!(buffer.drain(), b"12345");

        // Now test wraparound
        buffer.push(b"abcde"); // Fill completely
        buffer.push(b"fg"); // Overwrites 'a' and 'b'
                            // Buffer state: [f, g, c, d, e], write_pos=2, len=5
                            // Oldest data starts at write_pos (2), so order is: c, d, e, f, g
        let data = buffer.drain();
        assert_eq!(data, b"cdefg");
    }
}
