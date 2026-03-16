//! Output capture ring buffer for AI analysis.
//!
//! This module provides a ring buffer for capturing PTY output destined for AI analysis.
//! It integrates with the privacy gate system to respect denylist rules.
//!
//! # Overview
//!
//! The `OutputCapture` struct wraps an `SpscRingBuffer` with additional functionality:
//! - Start/stop capture for specific commands
//! - Track capture duration
//! - Detect buffer overflow (truncation)
//! - Privacy gate integration (disable/enable capture)
//!
//! # Default Buffer Size
//!
//! The default buffer size is 4MB, which should be sufficient for most command outputs
//! while avoiding excessive memory usage.
//!
//! # Privacy Integration
//!
//! When a denylist process is detected, `disable()` should be called to:
//! 1. Stop any active capture
//! 2. Clear the buffer (don't retain sensitive data)
//! 3. Prevent new captures until `enable()` is called
//!
//! # Example
//!
//! ```
//! use clai_wrap::output_capture::{OutputCapture, CapturedOutput};
//!
//! let mut capture = OutputCapture::new(4096);
//!
//! // Start capturing output for a command
//! capture.start_capture("cmd-123");
//!
//! // Push output bytes
//! capture.push(b"Hello, world!\n");
//! capture.push(b"Command output...\n");
//!
//! // Stop capturing and get the result
//! if let Some(output) = capture.stop_capture() {
//!     println!("Command: {}", output.command_id);
//!     println!("Data: {} bytes", output.data.len());
//!     println!("Truncated: {}", output.truncated);
//!     println!("Duration: {:?}", output.duration);
//! }
//! ```

use std::time::{Duration, Instant};

use crate::ring_buffer::SpscRingBuffer;

/// Default buffer capacity: 4MB.
pub const DEFAULT_CAPACITY: usize = 4 * 1024 * 1024;

/// Captured output from a command.
///
/// This struct contains the result of an output capture session, including
/// the raw output data and metadata about the capture.
#[derive(Debug, Clone)]
pub struct CapturedOutput {
    /// The command identifier this output was captured for.
    pub command_id: String,
    /// The captured output data.
    pub data: Vec<u8>,
    /// Whether the buffer overflowed and data was lost (truncated).
    pub truncated: bool,
    /// How long the capture was active.
    pub duration: Duration,
}

impl CapturedOutput {
    /// Returns the captured data as a UTF-8 string, with invalid sequences replaced.
    ///
    /// This is useful for logging or display purposes when the exact bytes don't matter.
    #[must_use]
    pub fn as_string_lossy(&self) -> String {
        String::from_utf8_lossy(&self.data).to_string()
    }

    /// Returns `true` if the captured data is empty.
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.data.is_empty()
    }

    /// Returns the number of captured bytes.
    #[must_use]
    pub const fn len(&self) -> usize {
        self.data.len()
    }
}

/// Output capture ring buffer for AI analysis.
///
/// This struct provides a ring buffer for capturing PTY output with privacy
/// gate integration. It tracks capture state, timing, and buffer overflow.
pub struct OutputCapture {
    /// The underlying ring buffer for storing output.
    buffer: SpscRingBuffer,
    /// Whether capture is enabled (respects privacy gates).
    enabled: bool,
    /// The command ID currently being captured.
    current_command: Option<String>,
    /// When the current capture started.
    capture_start: Option<Instant>,
}

impl OutputCapture {
    /// Creates a new `OutputCapture` with the specified capacity.
    ///
    /// # Arguments
    ///
    /// * `capacity` - Maximum number of bytes the buffer can hold.
    ///
    /// # Returns
    ///
    /// A new `OutputCapture` instance with capture enabled.
    ///
    /// # Example
    ///
    /// ```
    /// use clai_wrap::output_capture::OutputCapture;
    ///
    /// // Create with 4MB capacity (default)
    /// let capture = OutputCapture::new(4 * 1024 * 1024);
    /// assert!(capture.is_enabled());
    /// assert!(!capture.is_capturing());
    /// ```
    #[must_use]
    pub fn new(capacity: usize) -> Self {
        Self {
            buffer: SpscRingBuffer::new(capacity),
            enabled: true,
            current_command: None,
            capture_start: None,
        }
    }

    /// Creates a new `OutputCapture` with the default capacity (4MB).
    ///
    /// # Returns
    ///
    /// A new `OutputCapture` instance with default capacity and capture enabled.
    #[must_use]
    pub fn with_default_capacity() -> Self {
        Self::new(DEFAULT_CAPACITY)
    }

    /// Start capturing output for a command.
    ///
    /// This begins a new capture session, resetting any previous capture state.
    /// If capture is disabled (privacy gate active), this is a no-op.
    ///
    /// # Arguments
    ///
    /// * `command_id` - An identifier for the command being captured.
    ///
    /// # Example
    ///
    /// ```
    /// use clai_wrap::output_capture::OutputCapture;
    ///
    /// let mut capture = OutputCapture::new(4096);
    /// capture.start_capture("cmd-123");
    /// assert!(capture.is_capturing());
    /// ```
    pub fn start_capture(&mut self, command_id: &str) {
        if !self.enabled {
            return;
        }

        // Stop any existing capture without returning it
        let _ = self.stop_capture();

        // Start new capture
        self.current_command = Some(command_id.to_string());
        self.capture_start = Some(Instant::now());
        self.buffer.reset_overflow();
    }

    /// Stop capturing and return the captured output.
    ///
    /// This ends the current capture session and returns the captured data.
    /// Returns `None` if no capture was in progress.
    ///
    /// # Returns
    ///
    /// `Some(CapturedOutput)` if a capture was in progress, `None` otherwise.
    ///
    /// # Example
    ///
    /// ```
    /// use clai_wrap::output_capture::OutputCapture;
    ///
    /// let mut capture = OutputCapture::new(4096);
    /// capture.start_capture("cmd-123");
    /// capture.push(b"output data");
    ///
    /// let result = capture.stop_capture();
    /// assert!(result.is_some());
    /// assert!(!capture.is_capturing());
    /// ```
    pub fn stop_capture(&mut self) -> Option<CapturedOutput> {
        let command_id = self.current_command.take()?;
        let start_time = self.capture_start.take()?;

        let duration = start_time.elapsed();
        let truncated = self.buffer.has_overflowed();
        let data = self.buffer.drain();

        self.buffer.reset_overflow();

        Some(CapturedOutput {
            command_id,
            data,
            truncated,
            duration,
        })
    }

    /// Push output bytes into the capture buffer.
    ///
    /// This method only stores data if capture is enabled and a capture session
    /// is in progress. Otherwise, it's a no-op.
    ///
    /// # Arguments
    ///
    /// * `data` - The bytes to add to the capture buffer.
    ///
    /// # Example
    ///
    /// ```
    /// use clai_wrap::output_capture::OutputCapture;
    ///
    /// let mut capture = OutputCapture::new(4096);
    /// capture.start_capture("cmd-123");
    /// capture.push(b"Hello, ");
    /// capture.push(b"world!");
    /// ```
    pub fn push(&mut self, data: &[u8]) {
        if !self.enabled || self.current_command.is_none() {
            return;
        }

        self.buffer.push(data);
    }

    /// Check if currently capturing output.
    ///
    /// # Returns
    ///
    /// `true` if a capture session is in progress, `false` otherwise.
    #[must_use]
    pub const fn is_capturing(&self) -> bool {
        self.current_command.is_some()
    }

    /// Check if capture is enabled.
    ///
    /// # Returns
    ///
    /// `true` if capture is enabled (privacy gate not active), `false` otherwise.
    #[must_use]
    pub const fn is_enabled(&self) -> bool {
        self.enabled
    }

    /// Disable capture (privacy mode).
    ///
    /// This should be called when a denylist process is detected.
    /// It will:
    /// 1. Stop any active capture (discarding the data)
    /// 2. Clear the buffer
    /// 3. Prevent new captures until `enable()` is called
    ///
    /// # Example
    ///
    /// ```
    /// use clai_wrap::output_capture::OutputCapture;
    ///
    /// let mut capture = OutputCapture::new(4096);
    /// capture.start_capture("cmd-123");
    /// capture.push(b"sensitive data");
    ///
    /// // Privacy gate triggered - disable capture
    /// capture.disable();
    ///
    /// assert!(!capture.is_capturing());
    /// assert!(!capture.is_enabled());
    /// ```
    pub fn disable(&mut self) {
        // Clear any in-progress capture state
        self.current_command = None;
        self.capture_start = None;

        // Clear the buffer to avoid retaining sensitive data
        let _ = self.buffer.drain();
        self.buffer.reset_overflow();

        self.enabled = false;
    }

    /// Enable capture.
    ///
    /// This should be called when the denylist process exits and
    /// capture can resume.
    ///
    /// # Example
    ///
    /// ```
    /// use clai_wrap::output_capture::OutputCapture;
    ///
    /// let mut capture = OutputCapture::new(4096);
    /// capture.disable();
    /// assert!(!capture.is_enabled());
    ///
    /// capture.enable();
    /// assert!(capture.is_enabled());
    /// ```
    pub const fn enable(&mut self) {
        self.enabled = true;
    }

    /// Returns the buffer capacity.
    #[must_use]
    pub const fn capacity(&self) -> usize {
        self.buffer.capacity()
    }

    /// Returns the current command ID being captured, if any.
    #[must_use]
    pub fn current_command(&self) -> Option<&str> {
        self.current_command.as_deref()
    }

    /// Returns the number of bytes currently in the buffer.
    ///
    /// This is only meaningful during an active capture session.
    #[must_use]
    pub fn buffered_len(&self) -> usize {
        self.buffer.len()
    }
}

impl Default for OutputCapture {
    fn default() -> Self {
        Self::with_default_capacity()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;
    use std::time::Duration as StdDuration;

    #[test]
    fn test_basic_capture_start_stop() {
        let mut capture = OutputCapture::new(4096);

        assert!(!capture.is_capturing());
        assert!(capture.is_enabled());

        capture.start_capture("cmd-1");
        assert!(capture.is_capturing());
        assert_eq!(capture.current_command(), Some("cmd-1"));

        capture.push(b"Hello, world!");

        let result = capture.stop_capture();
        assert!(!capture.is_capturing());
        assert!(result.is_some());

        let output = result.unwrap();
        assert_eq!(output.command_id, "cmd-1");
        assert_eq!(output.data, b"Hello, world!");
        assert!(!output.truncated);
        assert!(!output.is_empty());
        assert_eq!(output.len(), 13);
    }

    #[test]
    fn test_multiple_pushes() {
        let mut capture = OutputCapture::new(4096);

        capture.start_capture("cmd-2");
        capture.push(b"Line 1\n");
        capture.push(b"Line 2\n");
        capture.push(b"Line 3\n");

        let result = capture.stop_capture().unwrap();
        assert_eq!(result.data, b"Line 1\nLine 2\nLine 3\n");
        assert_eq!(result.as_string_lossy(), "Line 1\nLine 2\nLine 3\n");
    }

    #[test]
    fn test_buffer_overflow_handling() {
        let mut capture = OutputCapture::new(10); // Very small buffer

        capture.start_capture("cmd-overflow");

        // Push more data than the buffer can hold
        // "This is way too long for the buffer!" is 36 bytes
        capture.push(b"This is way too long for the buffer!");

        let result = capture.stop_capture().unwrap();

        // Buffer should only contain the last 10 bytes: "he buffer!"
        assert!(result.truncated, "Should indicate truncation");
        assert_eq!(result.data.len(), 10);
        assert_eq!(result.data, b"he buffer!");
    }

    #[test]
    fn test_disable_clears_buffer() {
        let mut capture = OutputCapture::new(4096);

        capture.start_capture("cmd-sensitive");
        capture.push(b"sensitive data that should be cleared");

        assert!(capture.is_capturing());
        assert!(capture.buffered_len() > 0);

        // Disable should clear everything
        capture.disable();

        assert!(!capture.is_capturing());
        assert!(!capture.is_enabled());
        assert_eq!(capture.buffered_len(), 0);
        assert!(capture.current_command().is_none());
    }

    #[test]
    fn test_disable_prevents_new_capture() {
        let mut capture = OutputCapture::new(4096);

        capture.disable();
        assert!(!capture.is_enabled());

        // Start capture should be a no-op when disabled
        capture.start_capture("cmd-blocked");
        assert!(!capture.is_capturing());

        // Push should be a no-op when disabled
        capture.push(b"this should not be captured");
        assert_eq!(capture.buffered_len(), 0);
    }

    #[test]
    fn test_enable_after_disable() {
        let mut capture = OutputCapture::new(4096);

        capture.disable();
        assert!(!capture.is_enabled());

        capture.enable();
        assert!(capture.is_enabled());

        // Should be able to capture again
        capture.start_capture("cmd-after-enable");
        assert!(capture.is_capturing());

        capture.push(b"back to normal");
        let result = capture.stop_capture().unwrap();
        assert_eq!(result.data, b"back to normal");
    }

    #[test]
    fn test_stop_capture_when_not_capturing() {
        let mut capture = OutputCapture::new(4096);

        // Should return None when not capturing
        let result = capture.stop_capture();
        assert!(result.is_none());
    }

    #[test]
    fn test_push_when_not_capturing() {
        let mut capture = OutputCapture::new(4096);

        // Push when not capturing should be a no-op
        capture.push(b"this should not be stored");
        assert_eq!(capture.buffered_len(), 0);
    }

    #[test]
    fn test_duration_tracking() {
        let mut capture = OutputCapture::new(4096);

        capture.start_capture("cmd-duration");
        capture.push(b"test data");

        // Sleep a bit to ensure measurable duration
        thread::sleep(StdDuration::from_millis(10));

        let result = capture.stop_capture().unwrap();

        // Duration should be at least 10ms
        assert!(
            result.duration >= StdDuration::from_millis(10),
            "Duration {:?} should be >= 10ms",
            result.duration
        );
    }

    #[test]
    fn test_truncation_flag() {
        // Test no truncation
        let mut capture = OutputCapture::new(100);
        capture.start_capture("cmd-no-truncation");
        capture.push(b"small data");
        let result = capture.stop_capture().unwrap();
        assert!(!result.truncated, "Should not be truncated");

        // Test truncation
        let mut capture = OutputCapture::new(10);
        capture.start_capture("cmd-truncation");
        capture.push(b"this is longer than 10 bytes");
        let result = capture.stop_capture().unwrap();
        assert!(result.truncated, "Should be truncated");
    }

    #[test]
    fn test_start_capture_resets_previous() {
        let mut capture = OutputCapture::new(4096);

        capture.start_capture("cmd-first");
        capture.push(b"first command data");

        // Starting a new capture should stop the previous one
        capture.start_capture("cmd-second");
        capture.push(b"second command data");

        let result = capture.stop_capture().unwrap();
        assert_eq!(result.command_id, "cmd-second");
        assert_eq!(result.data, b"second command data");
    }

    #[test]
    fn test_default_capacity() {
        let capture = OutputCapture::with_default_capacity();
        assert_eq!(capture.capacity(), DEFAULT_CAPACITY);
        assert_eq!(capture.capacity(), 4 * 1024 * 1024);
    }

    #[test]
    fn test_default_trait() {
        let capture = OutputCapture::default();
        assert_eq!(capture.capacity(), DEFAULT_CAPACITY);
        assert!(capture.is_enabled());
        assert!(!capture.is_capturing());
    }

    #[test]
    fn test_captured_output_as_string_lossy() {
        let output = CapturedOutput {
            command_id: "test".to_string(),
            data: vec![0x48, 0x65, 0x6c, 0x6c, 0x6f], // "Hello"
            truncated: false,
            duration: Duration::from_secs(0),
        };

        assert_eq!(output.as_string_lossy(), "Hello");

        // Test with invalid UTF-8
        let output_invalid = CapturedOutput {
            command_id: "test".to_string(),
            data: vec![0x48, 0x65, 0xFF, 0x6c, 0x6f], // "He\xFFlo"
            truncated: false,
            duration: Duration::from_secs(0),
        };

        // Should replace invalid byte with replacement character
        let s = output_invalid.as_string_lossy();
        assert!(s.contains('H'));
        assert!(s.contains('\u{FFFD}')); // Replacement character
    }

    #[test]
    fn test_captured_output_empty() {
        let output = CapturedOutput {
            command_id: "test".to_string(),
            data: vec![],
            truncated: false,
            duration: Duration::from_secs(0),
        };

        assert!(output.is_empty());
        assert_eq!(output.len(), 0);
    }

    #[test]
    fn test_privacy_gate_workflow() {
        let mut capture = OutputCapture::new(4096);

        // Normal operation
        capture.start_capture("cmd-normal");
        capture.push(b"normal output");
        let result = capture.stop_capture().unwrap();
        assert_eq!(result.data, b"normal output");

        // Start new capture
        capture.start_capture("cmd-sensitive");
        capture.push(b"some output before ssh");

        // Denylist process detected (e.g., ssh started) - privacy gate triggers
        capture.disable();

        // Buffer should be cleared
        assert!(!capture.is_capturing());
        assert_eq!(capture.buffered_len(), 0);

        // Can't start new capture while disabled
        capture.start_capture("cmd-during-ssh");
        assert!(!capture.is_capturing());

        // Denylist process exited - privacy gate releases
        capture.enable();

        // Should be able to capture again
        capture.start_capture("cmd-after-ssh");
        capture.push(b"normal output again");
        let result = capture.stop_capture().unwrap();
        assert_eq!(result.data, b"normal output again");
    }

    #[test]
    fn test_large_data_capture() {
        let mut capture = OutputCapture::new(1024 * 1024); // 1MB buffer

        capture.start_capture("cmd-large");

        // Push 500KB of data
        let chunk = vec![b'x'; 1024];
        for _ in 0..500 {
            capture.push(&chunk);
        }

        let result = capture.stop_capture().unwrap();
        assert_eq!(result.data.len(), 500 * 1024);
        assert!(!result.truncated);
    }

    #[test]
    fn test_overflow_reset_between_captures() {
        let mut capture = OutputCapture::new(10);

        // First capture with overflow
        capture.start_capture("cmd-1");
        capture.push(b"overflow this buffer!");
        let result = capture.stop_capture().unwrap();
        assert!(result.truncated);

        // Second capture should start fresh (no overflow yet)
        capture.start_capture("cmd-2");
        capture.push(b"small");
        let result = capture.stop_capture().unwrap();
        assert!(!result.truncated);
    }
}
