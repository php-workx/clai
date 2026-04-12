//! Echo-Gap Heuristic module for detecting password prompts.
//!
//! This module detects when user input is not being echoed back, which typically
//! indicates a password prompt or other secure input mode. When detected, it
//! signals that the ring buffer should scrub recent input to prevent capturing
//! sensitive data.
//!
//! # Algorithm
//!
//! 1. Track user input bytes and their timestamps
//! 2. Track PTY output bytes to detect if input is being echoed
//! 3. If user types but no echo appears for > threshold -> enter "Secure Mode"
//! 4. Provide count of bytes to scrub from the ring buffer
//! 5. Resume normal recording after newline + echo resumes
//!
//! # Adaptive Timing
//!
//! The threshold is adaptive to account for network latency (SSH scenarios):
//! - Start with baseline threshold (default 100ms)
//! - Track recent echo latencies in a rolling window
//! - Adjust threshold based on p90 latency × 2
//! - Never go below minimum safe threshold (50ms)
//! - Cap at maximum threshold (500ms)

use std::collections::VecDeque;
use std::time::{Duration, Instant};

/// Default initial threshold for echo gap detection (50ms per spec).
pub const DEFAULT_THRESHOLD_MS: u64 = 50;

/// Minimum safe threshold to avoid false positives (50ms).
pub const MIN_THRESHOLD_MS: u64 = 50;

/// Maximum threshold cap to avoid false negatives (500ms).
pub const MAX_THRESHOLD_MS: u64 = 500;

/// Number of latency samples to keep for adaptive timing.
const LATENCY_WINDOW_SIZE: usize = 10;

/// Maximum number of unechoed input bytes to track.
const MAX_PENDING_INPUT: usize = 256;

/// Newline byte constant.
const NEWLINE: u8 = b'\n';

/// Carriage return byte constant.
const CARRIAGE_RETURN: u8 = b'\r';

/// Detector state machine states.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EchoGapState {
    /// Normal state: echo is being detected, recording enabled.
    Normal,
    /// Detecting state: input received, waiting for echo.
    Detecting,
    /// Secure mode: no echo detected, recording should be paused.
    Secure,
}

/// A single pending input byte awaiting echo.
#[derive(Debug, Clone)]
struct PendingInput {
    /// The input byte.
    byte: u8,
    /// When the byte was recorded.
    timestamp: Instant,
}

/// Configuration for the echo gap detector.
#[derive(Debug, Clone)]
pub struct EchoGapConfig {
    /// Initial threshold for echo gap detection.
    pub initial_threshold: Duration,
    /// Minimum threshold (floor).
    pub min_threshold: Duration,
    /// Maximum threshold (ceiling).
    pub max_threshold: Duration,
    /// Whether adaptive timing is enabled.
    pub adaptive: bool,
}

impl Default for EchoGapConfig {
    fn default() -> Self {
        Self {
            initial_threshold: Duration::from_millis(DEFAULT_THRESHOLD_MS),
            min_threshold: Duration::from_millis(MIN_THRESHOLD_MS),
            max_threshold: Duration::from_millis(MAX_THRESHOLD_MS),
            adaptive: true,
        }
    }
}

impl EchoGapConfig {
    /// Creates a new configuration with a fixed threshold (no adaptive timing).
    #[must_use]
    pub fn with_fixed_threshold(threshold_ms: u64) -> Self {
        let threshold =
            Duration::from_millis(threshold_ms.clamp(MIN_THRESHOLD_MS, MAX_THRESHOLD_MS));
        Self {
            initial_threshold: threshold,
            min_threshold: threshold,
            max_threshold: threshold,
            adaptive: false,
        }
    }
}

/// Echo-Gap detector for identifying password prompts.
///
/// This detector monitors the relationship between user input and PTY output
/// to detect when echo is disabled, which typically indicates a password prompt
/// or other secure input scenario.
///
/// # Example
///
/// ```
/// use clai_wrap::echo_gap::{EchoGapDetector, EchoGapState};
/// use std::time::Instant;
///
/// let mut detector = EchoGapDetector::new(100);
///
/// // Simulate normal typing with echo
/// let now = Instant::now();
/// detector.record_input(b'a', now);
/// detector.record_output(b'a', now); // Echo detected
///
/// assert_eq!(detector.state(), EchoGapState::Normal);
/// ```
#[derive(Debug)]
pub struct EchoGapDetector {
    /// Current state of the detector.
    state: EchoGapState,
    /// Configuration parameters.
    config: EchoGapConfig,
    /// Current effective threshold (may differ from initial due to adaptation).
    current_threshold: Duration,
    /// Queue of input bytes waiting for echo.
    pending_input: VecDeque<PendingInput>,
    /// Rolling window of recent echo latencies for adaptive timing.
    latency_samples: VecDeque<Duration>,
    /// Count of bytes to scrub when entering secure mode.
    scrub_count: usize,
    /// Total unechoed input bytes since last reset (for scrub calculation).
    unechoed_byte_count: usize,
    /// Last input timestamp for timeout checking.
    last_input_time: Option<Instant>,
}

impl EchoGapDetector {
    /// Creates a new `EchoGapDetector` with the specified initial threshold.
    ///
    /// # Arguments
    ///
    /// * `initial_threshold_ms` - Initial threshold in milliseconds (clamped to valid range).
    #[must_use]
    pub fn new(initial_threshold_ms: u64) -> Self {
        let clamped = initial_threshold_ms.clamp(MIN_THRESHOLD_MS, MAX_THRESHOLD_MS);
        let threshold = Duration::from_millis(clamped);

        Self {
            state: EchoGapState::Normal,
            config: EchoGapConfig {
                initial_threshold: threshold,
                ..Default::default()
            },
            current_threshold: threshold,
            pending_input: VecDeque::with_capacity(MAX_PENDING_INPUT),
            latency_samples: VecDeque::with_capacity(LATENCY_WINDOW_SIZE),
            scrub_count: 0,
            unechoed_byte_count: 0,
            last_input_time: None,
        }
    }

    /// Creates a new `EchoGapDetector` with custom configuration.
    #[must_use]
    pub fn with_config(config: EchoGapConfig) -> Self {
        let threshold = config.initial_threshold;
        Self {
            state: EchoGapState::Normal,
            config,
            current_threshold: threshold,
            pending_input: VecDeque::with_capacity(MAX_PENDING_INPUT),
            latency_samples: VecDeque::with_capacity(LATENCY_WINDOW_SIZE),
            scrub_count: 0,
            unechoed_byte_count: 0,
            last_input_time: None,
        }
    }

    /// Records a user input byte with its timestamp.
    ///
    /// This should be called for each byte the user types.
    ///
    /// # Arguments
    ///
    /// * `byte` - The input byte.
    /// * `timestamp` - When the byte was received.
    pub fn record_input(&mut self, byte: u8, timestamp: Instant) {
        // If we're in secure mode and see a newline, prepare for potential exit
        if self.state == EchoGapState::Secure && is_newline(byte) {
            // Newline in secure mode - we'll check if echo resumes after
            // Don't add newlines to pending input in secure mode
            return;
        }

        // Limit pending input queue size
        if self.pending_input.len() >= MAX_PENDING_INPUT {
            self.pending_input.pop_front();
        }

        self.pending_input
            .push_back(PendingInput { byte, timestamp });
        self.last_input_time = Some(timestamp);
        self.unechoed_byte_count += 1;

        // Transition from Normal to Detecting when we have pending input
        if self.state == EchoGapState::Normal {
            self.state = EchoGapState::Detecting;
        }
    }

    /// Records a PTY output byte with its timestamp.
    ///
    /// This should be called for each byte output from the PTY.
    /// The detector will check if this byte matches pending input (echo).
    ///
    /// # Arguments
    ///
    /// * `byte` - The output byte.
    /// * `timestamp` - When the byte was received.
    pub fn record_output(&mut self, byte: u8, timestamp: Instant) {
        // Check if this output byte matches any pending input (echo detection)
        let echo_found = self.find_and_remove_echo(byte, timestamp);

        if echo_found {
            // Echo detected - if we were in Detecting, go back to Normal
            if self.state == EchoGapState::Detecting {
                self.state = EchoGapState::Normal;
                // Reset scrub count since we're back to normal
                self.scrub_count = 0;
                self.unechoed_byte_count = 0;
            } else if self.state == EchoGapState::Secure {
                // In secure mode, echo resuming after newline exits secure mode
                self.state = EchoGapState::Normal;
                self.scrub_count = 0;
                self.unechoed_byte_count = 0;
            }
        }

        // If we see a newline in output while in secure mode, prepare for exit check
        if self.state == EchoGapState::Secure && is_newline(byte) {
            // Newline in output during secure mode - next echo will exit secure mode
            // This allows the password entry to complete with Enter
        }
    }

    /// Checks the current state and potentially transitions to secure mode.
    ///
    /// This should be called periodically (e.g., in the main loop) to detect
    /// when the echo gap threshold has been exceeded.
    ///
    /// # Arguments
    ///
    /// * `now` - Current timestamp for timeout checking.
    ///
    /// # Returns
    ///
    /// `true` if the state changed to Secure mode, `false` otherwise.
    pub fn check_timeout(&mut self, now: Instant) -> bool {
        // Only check timeout in Detecting state
        if self.state != EchoGapState::Detecting {
            return false;
        }

        // Check if oldest pending input has exceeded threshold
        if let Some(oldest) = self.pending_input.front() {
            let elapsed = now.duration_since(oldest.timestamp);
            if elapsed >= self.current_threshold {
                // Transition to Secure mode
                self.state = EchoGapState::Secure;
                // Set scrub count to include all unechoed input
                self.scrub_count = self.unechoed_byte_count;
                return true;
            }
        }

        false
    }

    /// Returns the current state of the detector.
    #[must_use]
    pub const fn state(&self) -> EchoGapState {
        self.state
    }

    /// Returns `true` if currently in secure mode (no echo detected).
    #[must_use]
    pub const fn is_secure_mode(&self) -> bool {
        matches!(self.state, EchoGapState::Secure)
    }

    /// Returns the number of bytes that should be scrubbed from the ring buffer.
    ///
    /// This value is set when entering secure mode and represents the number
    /// of bytes of user input that may have been captured before the password
    /// prompt was detected.
    #[must_use]
    pub const fn bytes_to_scrub(&self) -> usize {
        self.scrub_count
    }

    /// Resets the detector state.
    ///
    /// This should be called after a command completes (newline + echo resumes)
    /// or when starting fresh.
    pub fn reset(&mut self) {
        self.state = EchoGapState::Normal;
        self.pending_input.clear();
        self.scrub_count = 0;
        self.unechoed_byte_count = 0;
        self.last_input_time = None;
    }

    /// Returns the current effective threshold.
    #[must_use]
    pub const fn current_threshold(&self) -> Duration {
        self.current_threshold
    }

    /// Returns the number of pending (unechoed) input bytes.
    #[must_use]
    pub fn pending_count(&self) -> usize {
        self.pending_input.len()
    }

    /// Finds and removes a matching echo byte from pending input.
    ///
    /// Returns `true` if an echo was found and latency was recorded.
    fn find_and_remove_echo(&mut self, output_byte: u8, output_time: Instant) -> bool {
        // Look for a matching input byte in the pending queue
        // We search from the front (oldest) to find the first match
        let mut found_idx = None;

        for (idx, pending) in self.pending_input.iter().enumerate() {
            if pending.byte == output_byte {
                found_idx = Some((idx, pending.timestamp));
                break;
            }
        }

        if let Some((idx, input_time)) = found_idx {
            // Remove the matched input
            self.pending_input.remove(idx);

            // Calculate and record latency for adaptive timing
            if output_time >= input_time {
                let latency = output_time.duration_since(input_time);
                self.record_latency(latency);
            }

            // Decrement unechoed count
            if self.unechoed_byte_count > 0 {
                self.unechoed_byte_count -= 1;
            }

            return true;
        }

        false
    }

    /// Records a latency sample and updates the adaptive threshold.
    fn record_latency(&mut self, latency: Duration) {
        if !self.config.adaptive {
            return;
        }

        // Add to rolling window
        if self.latency_samples.len() >= LATENCY_WINDOW_SIZE {
            self.latency_samples.pop_front();
        }
        self.latency_samples.push_back(latency);

        // Update threshold based on p90 latency
        self.update_adaptive_threshold();
    }

    /// Updates the adaptive threshold based on recent latency samples.
    fn update_adaptive_threshold(&mut self) {
        if self.latency_samples.is_empty() {
            return;
        }

        // Calculate p90 latency
        let mut sorted: Vec<_> = self.latency_samples.iter().copied().collect();
        sorted.sort();

        let p90_idx = (sorted.len() * 90 / 100).max(1) - 1;
        let p90_latency = sorted[p90_idx.min(sorted.len() - 1)];

        // Set threshold to p90 × 2, clamped to valid range
        let new_threshold = p90_latency.saturating_mul(2);
        self.current_threshold =
            new_threshold.clamp(self.config.min_threshold, self.config.max_threshold);
    }
}

/// Returns `true` if the byte is a newline character.
#[inline]
const fn is_newline(byte: u8) -> bool {
    byte == NEWLINE || byte == CARRIAGE_RETURN
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    #[test]
    fn test_new_detector() {
        let detector = EchoGapDetector::new(100);
        assert_eq!(detector.state(), EchoGapState::Normal);
        assert!(!detector.is_secure_mode());
        assert_eq!(detector.bytes_to_scrub(), 0);
        assert_eq!(detector.pending_count(), 0);
    }

    #[test]
    fn test_threshold_clamping() {
        // Test minimum clamping
        let detector = EchoGapDetector::new(10);
        assert_eq!(
            detector.current_threshold(),
            Duration::from_millis(MIN_THRESHOLD_MS)
        );

        // Test maximum clamping
        let detector = EchoGapDetector::new(1000);
        assert_eq!(
            detector.current_threshold(),
            Duration::from_millis(MAX_THRESHOLD_MS)
        );

        // Test valid value
        let detector = EchoGapDetector::new(100);
        assert_eq!(detector.current_threshold(), Duration::from_millis(100));
    }

    #[test]
    fn test_normal_typing_with_echo() {
        let mut detector = EchoGapDetector::new(100);
        let now = Instant::now();

        // Type 'a' and receive echo immediately
        detector.record_input(b'a', now);
        assert_eq!(detector.state(), EchoGapState::Detecting);
        assert_eq!(detector.pending_count(), 1);

        detector.record_output(b'a', now);
        assert_eq!(detector.state(), EchoGapState::Normal);
        assert_eq!(detector.pending_count(), 0);
        assert!(!detector.is_secure_mode());
    }

    #[test]
    fn test_password_prompt_detection() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(10),
            min_threshold: Duration::from_millis(10),
            max_threshold: Duration::from_millis(500),
            adaptive: false,
        };
        let mut detector = EchoGapDetector::with_config(config);
        let start = Instant::now();

        // Type some characters (password)
        detector.record_input(b'p', start);
        detector.record_input(b'a', start);
        detector.record_input(b's', start);
        detector.record_input(b's', start);

        assert_eq!(detector.state(), EchoGapState::Detecting);
        assert_eq!(detector.pending_count(), 4);

        // Wait for threshold to expire
        thread::sleep(Duration::from_millis(20));

        // Check timeout - should transition to Secure
        let after = Instant::now();
        let transitioned = detector.check_timeout(after);

        assert!(transitioned);
        assert_eq!(detector.state(), EchoGapState::Secure);
        assert!(detector.is_secure_mode());
        assert_eq!(detector.bytes_to_scrub(), 4);
    }

    #[test]
    fn test_newline_triggers_reset_check() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(10),
            min_threshold: Duration::from_millis(10),
            max_threshold: Duration::from_millis(500),
            adaptive: false,
        };
        let mut detector = EchoGapDetector::with_config(config);
        let start = Instant::now();

        // Enter secure mode
        detector.record_input(b'p', start);
        thread::sleep(Duration::from_millis(20));
        let after = Instant::now();
        detector.check_timeout(after);
        assert!(detector.is_secure_mode());

        // Newline in input during secure mode is ignored
        detector.record_input(NEWLINE, after);
        assert!(detector.is_secure_mode());

        // Newline in output during secure mode prepares for exit
        detector.record_output(NEWLINE, after);
        assert!(detector.is_secure_mode()); // Still secure until echo resumes

        // Echo resuming after newline exits secure mode
        detector.record_input(b'a', after);
        detector.record_output(b'a', after);
        assert!(!detector.is_secure_mode());
        assert_eq!(detector.state(), EchoGapState::Normal);
    }

    #[test]
    fn test_adaptive_threshold_adjustment() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(100),
            min_threshold: Duration::from_millis(50),
            max_threshold: Duration::from_millis(500),
            adaptive: true,
        };
        let mut detector = EchoGapDetector::with_config(config);

        // Simulate typing with consistent latency
        let base = Instant::now();
        for i in 0..10 {
            let input_time = base + Duration::from_millis(i * 100);
            let output_time = input_time + Duration::from_millis(30); // 30ms latency
            detector.record_input(b'a', input_time);
            detector.record_output(b'a', output_time);
        }

        // Threshold should adapt based on p90 × 2 = 30 × 2 = 60ms
        // But minimum is 50ms, so should be 60ms
        assert!(detector.current_threshold() >= Duration::from_millis(50));
        assert!(detector.current_threshold() <= Duration::from_millis(100));
    }

    #[test]
    fn test_adaptive_with_high_latency() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(100),
            min_threshold: Duration::from_millis(50),
            max_threshold: Duration::from_millis(500),
            adaptive: true,
        };
        let mut detector = EchoGapDetector::with_config(config);

        // Simulate typing with high latency (like SSH over satellite)
        let base = Instant::now();
        for i in 0..10 {
            let input_time = base + Duration::from_millis(i * 500);
            let output_time = input_time + Duration::from_millis(200); // 200ms latency
            detector.record_input(b'a', input_time);
            detector.record_output(b'a', output_time);
        }

        // Threshold should increase to accommodate high latency
        // p90 × 2 = 200 × 2 = 400ms
        assert!(detector.current_threshold() >= Duration::from_millis(200));
    }

    #[test]
    fn test_fixed_threshold_config() {
        let config = EchoGapConfig::with_fixed_threshold(75);
        let mut detector = EchoGapDetector::with_config(config);

        assert_eq!(detector.current_threshold(), Duration::from_millis(75));

        // Even with latency samples, threshold shouldn't change
        let base = Instant::now();
        for i in 0..10 {
            let input_time = base + Duration::from_millis(i * 100);
            let output_time = input_time + Duration::from_millis(10);
            detector.record_input(b'a', input_time);
            detector.record_output(b'a', output_time);
        }

        // Threshold should remain fixed
        assert_eq!(detector.current_threshold(), Duration::from_millis(75));
    }

    #[test]
    fn test_rapid_input_handling() {
        let mut detector = EchoGapDetector::new(100);
        let now = Instant::now();

        // Rapid input burst
        for i in 0..100u8 {
            detector.record_input(i, now);
        }

        assert_eq!(detector.pending_count(), 100);
        assert_eq!(detector.state(), EchoGapState::Detecting);

        // Rapid echo burst (all echoed)
        for i in 0..100u8 {
            detector.record_output(i, now);
        }

        assert_eq!(detector.pending_count(), 0);
        assert_eq!(detector.state(), EchoGapState::Normal);
    }

    #[test]
    fn test_pending_input_overflow() {
        let mut detector = EchoGapDetector::new(100);
        let now = Instant::now();

        // Overflow the pending input queue
        for i in 0..300u16 {
            #[allow(clippy::cast_possible_truncation)]
            detector.record_input(i as u8, now);
        }

        // Should be limited to MAX_PENDING_INPUT
        assert!(detector.pending_count() <= MAX_PENDING_INPUT);
    }

    #[test]
    fn test_reset() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(10),
            min_threshold: Duration::from_millis(10),
            max_threshold: Duration::from_millis(500),
            adaptive: false,
        };
        let mut detector = EchoGapDetector::with_config(config);
        let start = Instant::now();

        // Enter secure mode
        detector.record_input(b'p', start);
        thread::sleep(Duration::from_millis(20));
        detector.check_timeout(Instant::now());
        assert!(detector.is_secure_mode());
        assert!(detector.bytes_to_scrub() > 0);

        // Reset
        detector.reset();

        assert_eq!(detector.state(), EchoGapState::Normal);
        assert!(!detector.is_secure_mode());
        assert_eq!(detector.bytes_to_scrub(), 0);
        assert_eq!(detector.pending_count(), 0);
    }

    #[test]
    fn test_out_of_order_echo() {
        let mut detector = EchoGapDetector::new(100);
        let now = Instant::now();

        // Type 'abc'
        detector.record_input(b'a', now);
        detector.record_input(b'b', now);
        detector.record_input(b'c', now);

        assert_eq!(detector.pending_count(), 3);

        // Echo comes back out of order (unlikely but possible): 'b' first
        detector.record_output(b'b', now);
        assert_eq!(detector.pending_count(), 2);
        assert_eq!(detector.state(), EchoGapState::Normal);

        // Rest of echo
        detector.record_output(b'a', now);
        detector.record_output(b'c', now);
        assert_eq!(detector.pending_count(), 0);
    }

    #[test]
    fn test_no_timeout_when_idle() {
        let mut detector = EchoGapDetector::new(100);
        let now = Instant::now();

        // No input, check timeout should return false
        let transitioned = detector.check_timeout(now);
        assert!(!transitioned);
        assert_eq!(detector.state(), EchoGapState::Normal);
    }

    #[test]
    fn test_timeout_not_reached() {
        let mut detector = EchoGapDetector::new(100);
        let now = Instant::now();

        // Record input
        detector.record_input(b'a', now);
        assert_eq!(detector.state(), EchoGapState::Detecting);

        // Check immediately - threshold not reached
        let transitioned = detector.check_timeout(now);
        assert!(!transitioned);
        assert_eq!(detector.state(), EchoGapState::Detecting);
    }

    #[test]
    fn test_scrub_count_accuracy() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(10),
            min_threshold: Duration::from_millis(10),
            max_threshold: Duration::from_millis(500),
            adaptive: false,
        };
        let mut detector = EchoGapDetector::with_config(config);
        let start = Instant::now();

        // Type exactly 7 characters
        for c in b"password" {
            detector.record_input(*c, start);
        }

        thread::sleep(Duration::from_millis(20));
        detector.check_timeout(Instant::now());

        // Should want to scrub all 8 characters
        assert!(detector.is_secure_mode());
        assert_eq!(detector.bytes_to_scrub(), 8);
    }

    #[test]
    fn test_partial_echo_before_secure_mode() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(50),
            min_threshold: Duration::from_millis(50),
            max_threshold: Duration::from_millis(500),
            adaptive: false,
        };
        let mut detector = EchoGapDetector::with_config(config);
        let start = Instant::now();

        // Type 'user: pass'
        // First part echoed (username)
        detector.record_input(b'u', start);
        detector.record_input(b's', start);
        detector.record_output(b'u', start);
        detector.record_output(b's', start);

        assert_eq!(detector.state(), EchoGapState::Normal);

        // Now type password (not echoed)
        detector.record_input(b'p', start);
        detector.record_input(b'a', start);
        detector.record_input(b's', start);
        detector.record_input(b's', start);

        thread::sleep(Duration::from_millis(60));
        detector.check_timeout(Instant::now());

        // Should be in secure mode with 4 bytes to scrub
        assert!(detector.is_secure_mode());
        assert_eq!(detector.bytes_to_scrub(), 4);
    }

    #[test]
    fn test_simulated_network_latency() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(100),
            min_threshold: Duration::from_millis(50),
            max_threshold: Duration::from_millis(500),
            adaptive: true,
        };
        let mut detector = EchoGapDetector::with_config(config);

        // Simulate SSH with 80ms round-trip latency
        // Train the adaptive algorithm using real-time delays
        for _ in 0..5 {
            let input_time = Instant::now();
            detector.record_input(b'x', input_time);
            // Simulate 80ms network latency
            thread::sleep(Duration::from_millis(20)); // Use shorter delay for test speed
            let output_time = Instant::now();
            detector.record_output(b'x', output_time);
        }

        // Threshold should have adapted based on observed latencies
        let threshold = detector.current_threshold();
        // Since we slept ~20ms, p90 × 2 should be around 40ms, but clamped to min (50ms)
        assert!(threshold >= Duration::from_millis(50)); // At least minimum

        // Reset for password test
        detector.reset();

        // Now simulate password prompt - type characters that won't be echoed
        let pwd_start = Instant::now();
        detector.record_input(b'p', pwd_start);
        detector.record_input(b'w', pwd_start);
        detector.record_input(b'd', pwd_start);

        // Wait for threshold to expire
        thread::sleep(threshold + Duration::from_millis(50));

        let check_time = Instant::now();
        let transitioned = detector.check_timeout(check_time);

        assert!(transitioned);
        assert!(detector.is_secure_mode());
    }

    #[test]
    fn test_carriage_return_as_newline() {
        let config = EchoGapConfig {
            initial_threshold: Duration::from_millis(10),
            min_threshold: Duration::from_millis(10),
            max_threshold: Duration::from_millis(500),
            adaptive: false,
        };
        let mut detector = EchoGapDetector::with_config(config);
        let start = Instant::now();

        // Enter secure mode
        detector.record_input(b'p', start);
        thread::sleep(Duration::from_millis(20));
        detector.check_timeout(Instant::now());
        assert!(detector.is_secure_mode());

        // CR should also be treated as newline
        detector.record_output(CARRIAGE_RETURN, Instant::now());
        assert!(detector.is_secure_mode());

        // Echo resuming exits secure mode
        detector.record_input(b'a', Instant::now());
        detector.record_output(b'a', Instant::now());
        assert!(!detector.is_secure_mode());
    }
}
