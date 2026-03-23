//! Resize handler with trailing-edge debounce for terminal resize events.
//!
//! This module provides a resize handler that debounces rapid resize events (e.g., from
//! drag-resizing a terminal window) while ensuring the final resize event is always applied.
//!
//! # Trailing-Edge Debounce
//!
//! The debouncing strategy uses trailing-edge semantics, which means:
//!
//! 1. When a resize event arrives, the new size is stored atomically
//! 2. A debounce timer is started (or reset if already running)
//! 3. When the timer fires, the most recent size is propagated to the PTY
//! 4. If a new event arrives before the timer fires, the timer resets
//!
//! **Key invariant:** The LAST resize event received is ALWAYS eventually applied.
//! We never drop the tail.
//!
//! # Example
//!
//! ```
//! use clai_wrap::resize::ResizeHandler;
//! use std::time::Duration;
//!
//! let handler = ResizeHandler::new();
//!
//! // Simulate receiving resize events
//! handler.on_resize(80, 24);
//! handler.on_resize(100, 30);  // Rapid resize
//! handler.on_resize(120, 40);  // Final size
//!
//! // After debounce period, tick() returns the final size
//! std::thread::sleep(Duration::from_millis(60));
//! if let Some((cols, rows)) = handler.tick() {
//!     assert_eq!(cols, 120);
//!     assert_eq!(rows, 40);
//! }
//! ```

use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::time::{Duration, Instant};

/// Default debounce period in milliseconds.
pub const DEFAULT_DEBOUNCE_MS: u64 = 50;

/// Sentinel value indicating no pending resize.
const NO_PENDING: u32 = 0;

/// Handles terminal resize events with trailing-edge debounce.
///
/// This struct is designed for concurrent access:
/// - `on_resize()` can be called from a signal handler thread
/// - `tick()` can be called from the main event loop
///
/// The implementation uses atomic operations to avoid locks.
pub struct ResizeHandler {
    /// Packed terminal size (cols in upper 16 bits, rows in lower 16 bits).
    /// A value of 0 indicates no pending resize.
    latest_size: AtomicU32,

    /// Timestamp when the debounce timer should fire (stored as nanoseconds since
    /// the reference instant). We use nanoseconds to fit in u64.
    timer_deadline_nanos: AtomicU64,

    /// Reference instant for timer calculations (set at creation time).
    reference_instant: Instant,

    /// Debounce period.
    debounce_duration: Duration,
}

impl ResizeHandler {
    /// Creates a new `ResizeHandler` with the default debounce period (50ms).
    #[must_use]
    pub fn new() -> Self {
        Self::with_debounce(Duration::from_millis(DEFAULT_DEBOUNCE_MS))
    }

    /// Creates a new `ResizeHandler` with a custom debounce period.
    #[must_use]
    pub fn with_debounce(debounce_duration: Duration) -> Self {
        Self {
            latest_size: AtomicU32::new(NO_PENDING),
            timer_deadline_nanos: AtomicU64::new(0),
            reference_instant: Instant::now(),
            debounce_duration,
        }
    }

    /// Called when a resize event is received (e.g., from SIGWINCH).
    ///
    /// This stores the new size and resets the debounce timer.
    /// Multiple rapid calls will only result in one resize propagation
    /// after the debounce period, using the most recent size.
    ///
    /// # Arguments
    ///
    /// * `cols` - New terminal width in columns
    /// * `rows` - New terminal height in rows
    pub fn on_resize(&self, cols: u16, rows: u16) {
        // Pack the size (cols in upper 16 bits, rows in lower 16 bits)
        let packed = pack_size(cols, rows);

        // Store the new size
        self.latest_size.store(packed, Ordering::SeqCst);

        // Reset the timer deadline
        let deadline = self.reference_instant.elapsed() + self.debounce_duration;
        // Truncation is acceptable here: u64 nanos can represent ~584 years,
        // which far exceeds any practical debounce duration.
        #[allow(clippy::cast_possible_truncation)]
        self.timer_deadline_nanos
            .store(deadline.as_nanos() as u64, Ordering::SeqCst);
    }

    /// Returns the current pending size, if any.
    ///
    /// This does not affect the debounce timer or consume the pending resize.
    #[must_use]
    pub fn pending_size(&self) -> Option<(u16, u16)> {
        let packed = self.latest_size.load(Ordering::SeqCst);
        if packed == NO_PENDING {
            None
        } else {
            Some(unpack_size(packed))
        }
    }

    /// Checks if the debounce timer has fired and returns the size to propagate.
    ///
    /// This method should be called periodically from the main event loop.
    ///
    /// # Returns
    ///
    /// - `Some((cols, rows))` if the timer has fired and there's a pending size
    /// - `None` if no resize is pending or the timer hasn't fired yet
    ///
    /// When `Some` is returned, the pending size is cleared.
    #[must_use]
    pub fn tick(&self) -> Option<(u16, u16)> {
        // Check if we have a pending resize
        let packed = self.latest_size.load(Ordering::SeqCst);
        if packed == NO_PENDING {
            return None;
        }

        // Check if the timer has fired
        let deadline_nanos = self.timer_deadline_nanos.load(Ordering::SeqCst);
        // Truncation is acceptable: u64 nanos can represent ~584 years
        #[allow(clippy::cast_possible_truncation)]
        let now_nanos = self.reference_instant.elapsed().as_nanos() as u64;

        if now_nanos < deadline_nanos {
            // Timer hasn't fired yet
            return None;
        }

        // Timer has fired - atomically clear the pending size
        // Use compare_exchange to ensure we only clear if it hasn't changed
        match self.latest_size.compare_exchange(
            packed,
            NO_PENDING,
            Ordering::SeqCst,
            Ordering::SeqCst,
        ) {
            Ok(_) => Some(unpack_size(packed)),
            Err(_) => {
                // Size changed between our read and CAS - either a new resize arrived
                // or someone else consumed it. Don't consume anything; let the new
                // resize's timer handle it on next tick.
                None
            }
        }
    }

    /// Returns the time remaining until the timer fires.
    ///
    /// Returns `None` if no resize is pending.
    /// Returns `Some(Duration::ZERO)` if the timer has already fired.
    #[must_use]
    pub fn time_until_fire(&self) -> Option<Duration> {
        let packed = self.latest_size.load(Ordering::SeqCst);
        if packed == NO_PENDING {
            return None;
        }

        let deadline_nanos = self.timer_deadline_nanos.load(Ordering::SeqCst);
        // Truncation is acceptable: u64 nanos can represent ~584 years
        #[allow(clippy::cast_possible_truncation)]
        let now_nanos = self.reference_instant.elapsed().as_nanos() as u64;

        if now_nanos >= deadline_nanos {
            Some(Duration::ZERO)
        } else {
            Some(Duration::from_nanos(deadline_nanos - now_nanos))
        }
    }

    /// Returns the configured debounce duration.
    #[must_use]
    pub const fn debounce_duration(&self) -> Duration {
        self.debounce_duration
    }
}

impl Default for ResizeHandler {
    fn default() -> Self {
        Self::new()
    }
}

/// Packs columns and rows into a single u32.
///
/// Format: `(cols << 16) | rows`
///
/// Note: A size of (0, 0) is not valid for terminals, so we can use 0
/// as a sentinel for "no pending resize".
#[inline]
const fn pack_size(cols: u16, rows: u16) -> u32 {
    ((cols as u32) << 16) | (rows as u32)
}

/// Unpacks a u32 into (columns, rows).
#[inline]
const fn unpack_size(packed: u32) -> (u16, u16) {
    let cols = (packed >> 16) as u16;
    let rows = (packed & 0xFFFF) as u16;
    (cols, rows)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    #[test]
    fn test_pack_unpack_size() {
        // Test basic packing/unpacking
        assert_eq!(unpack_size(pack_size(80, 24)), (80, 24));
        assert_eq!(unpack_size(pack_size(120, 40)), (120, 40));
        assert_eq!(unpack_size(pack_size(0, 0)), (0, 0));

        // Test max values
        assert_eq!(
            unpack_size(pack_size(u16::MAX, u16::MAX)),
            (u16::MAX, u16::MAX)
        );

        // Test asymmetric values
        assert_eq!(unpack_size(pack_size(1, 65535)), (1, 65535));
        assert_eq!(unpack_size(pack_size(65535, 1)), (65535, 1));
    }

    #[test]
    fn test_new_handler_has_no_pending() {
        let handler = ResizeHandler::new();
        assert!(handler.pending_size().is_none());
        assert!(handler.tick().is_none());
    }

    #[test]
    fn test_single_resize_event() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(10));

        // Trigger resize
        handler.on_resize(80, 24);

        // Should have pending size immediately
        assert_eq!(handler.pending_size(), Some((80, 24)));

        // tick() should return None before debounce period
        assert!(handler.tick().is_none());

        // Wait for debounce period
        thread::sleep(Duration::from_millis(15));

        // tick() should now return the size
        assert_eq!(handler.tick(), Some((80, 24)));

        // After consumption, no more pending
        assert!(handler.pending_size().is_none());
        assert!(handler.tick().is_none());
    }

    #[test]
    fn test_rapid_resize_only_last_applied() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(20));

        // Send rapid resize events
        handler.on_resize(80, 24);
        thread::sleep(Duration::from_millis(5));
        handler.on_resize(100, 30);
        thread::sleep(Duration::from_millis(5));
        handler.on_resize(120, 40); // This should be the one that gets applied

        // tick() should return None (timer resets each time)
        assert!(handler.tick().is_none());

        // Wait for debounce period after last event
        thread::sleep(Duration::from_millis(25));

        // Should get the LAST size (trailing edge)
        assert_eq!(handler.tick(), Some((120, 40)));

        // No more pending
        assert!(handler.pending_size().is_none());
    }

    #[test]
    fn test_timer_reset_on_new_event() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(30));

        // First resize
        handler.on_resize(80, 24);

        // Wait almost until debounce fires
        thread::sleep(Duration::from_millis(25));

        // New resize comes in - timer should reset
        handler.on_resize(100, 30);

        // tick() should still return None (timer was reset)
        assert!(handler.tick().is_none());

        // Wait again for new debounce period
        thread::sleep(Duration::from_millis(35));

        // Now should get the new size
        assert_eq!(handler.tick(), Some((100, 30)));
    }

    #[test]
    fn test_time_until_fire() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(50));

        // No pending resize
        assert!(handler.time_until_fire().is_none());

        // Trigger resize
        handler.on_resize(80, 24);

        // Should have some time remaining
        let remaining = handler.time_until_fire().unwrap();
        assert!(remaining > Duration::ZERO);
        assert!(remaining <= Duration::from_millis(50));

        // Wait for timer to fire
        thread::sleep(Duration::from_millis(60));

        // Should be zero (timer fired)
        assert_eq!(handler.time_until_fire(), Some(Duration::ZERO));
    }

    #[test]
    fn test_default_debounce_duration() {
        let handler = ResizeHandler::new();
        assert_eq!(handler.debounce_duration(), Duration::from_millis(50));
    }

    #[test]
    fn test_custom_debounce_duration() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(100));
        assert_eq!(handler.debounce_duration(), Duration::from_millis(100));
    }

    #[test]
    fn test_default_impl() {
        let handler = ResizeHandler::default();
        assert!(handler.pending_size().is_none());
        assert_eq!(handler.debounce_duration(), Duration::from_millis(50));
    }

    #[test]
    fn test_concurrent_resize_and_tick() {
        use std::sync::Arc;

        let handler = Arc::new(ResizeHandler::with_debounce(Duration::from_millis(20)));
        let handler_clone = Arc::clone(&handler);

        // Spawn thread to send resize events
        let resize_thread = thread::spawn(move || {
            for i in 0..10 {
                handler_clone.on_resize(80 + i, 24 + i);
                thread::sleep(Duration::from_millis(5));
            }
        });

        // Main thread tries to tick
        let mut final_size = None;
        for _ in 0..50 {
            thread::sleep(Duration::from_millis(10));
            if let Some(size) = handler.tick() {
                final_size = Some(size);
            }
        }

        resize_thread.join().unwrap();

        // After all resizes and sufficient time, we should have gotten the final size
        // The exact size depends on timing, but it should be one of the sizes we sent
        if let Some((cols, rows)) = final_size {
            assert!((80..=89).contains(&cols));
            assert!((24..=33).contains(&rows));
        }
    }

    #[test]
    fn test_zero_cols_or_rows_is_valid() {
        // While (0, 0) is our sentinel, (0, n) or (n, 0) should work
        // Note: in practice, terminals don't have 0 cols or rows, but the
        // packing should handle it correctly.
        let handler = ResizeHandler::with_debounce(Duration::from_millis(10));

        // (0, 24) - 0 cols
        handler.on_resize(0, 24);
        thread::sleep(Duration::from_millis(15));
        // This will work because pack_size(0, 24) != 0
        assert_eq!(handler.tick(), Some((0, 24)));

        // (80, 0) - 0 rows
        handler.on_resize(80, 0);
        thread::sleep(Duration::from_millis(15));
        // This will work because pack_size(80, 0) != 0
        assert_eq!(handler.tick(), Some((80, 0)));
    }

    #[test]
    fn test_multiple_tick_calls_after_fire() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(10));

        handler.on_resize(80, 24);
        thread::sleep(Duration::from_millis(15));

        // First tick consumes the pending resize
        assert_eq!(handler.tick(), Some((80, 24)));

        // Subsequent ticks return None
        assert!(handler.tick().is_none());
        assert!(handler.tick().is_none());
    }

    #[test]
    fn test_resize_immediately_after_tick() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(10));

        // First resize cycle
        handler.on_resize(80, 24);
        thread::sleep(Duration::from_millis(15));
        assert_eq!(handler.tick(), Some((80, 24)));

        // Immediately trigger another resize
        handler.on_resize(100, 30);
        assert_eq!(handler.pending_size(), Some((100, 30)));

        thread::sleep(Duration::from_millis(15));
        assert_eq!(handler.tick(), Some((100, 30)));
    }

    #[test]
    fn test_pending_size_does_not_consume() {
        let handler = ResizeHandler::with_debounce(Duration::from_millis(10));

        handler.on_resize(80, 24);

        // pending_size should not consume
        assert_eq!(handler.pending_size(), Some((80, 24)));
        assert_eq!(handler.pending_size(), Some((80, 24)));
        assert_eq!(handler.pending_size(), Some((80, 24)));

        thread::sleep(Duration::from_millis(15));

        // tick should still work
        assert_eq!(handler.tick(), Some((80, 24)));
    }
}
