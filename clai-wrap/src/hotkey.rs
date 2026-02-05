//! Hotkey chord parser module for clai-wrap.
//!
//! This module provides a state machine for detecting two-key chord sequences
//! in a byte stream. It supports configurable timeouts and forwards unmatched
//! bytes appropriately.

use std::time::{Duration, Instant};

/// Default timeout for chord completion (500ms).
pub const DEFAULT_CHORD_TIMEOUT: Duration = Duration::from_millis(500);

/// The first byte of the chord sequence (Ctrl-\, ASCII 0x1C).
pub const CHORD_FIRST_BYTE: u8 = 0x1C;

/// Second byte for history hotkey.
pub const CHORD_HISTORY_BYTE: u8 = b'h';

/// Second byte for completions hotkey.
pub const CHORD_COMPLETIONS_BYTE: u8 = b'c';

/// Escape byte for canceling a chord.
pub const ESC_BYTE: u8 = 0x1B;

/// Types of hotkeys that can be triggered.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HotkeyType {
    /// Trigger history view.
    History,
    /// Trigger completions view.
    Completions,
}

/// Events produced by the hotkey parser.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HotkeyEvent {
    /// A hotkey chord was successfully triggered.
    Triggered(HotkeyType),
    /// Bytes that should be forwarded to the underlying process.
    Forward(Vec<u8>),
}

/// Internal state of the hotkey parser.
#[derive(Debug, Clone)]
enum State {
    /// Idle state, waiting for the first byte of a chord.
    Idle,
    /// Received the first byte, waiting for the second byte.
    WaitingForSecond {
        /// When the first byte was received.
        start_time: Instant,
        /// The first byte that was received.
        first_byte: u8,
    },
}

/// Configuration for the hotkey parser.
#[derive(Debug, Clone)]
pub struct HotkeyConfig {
    /// Timeout for completing a chord sequence.
    pub timeout: Duration,
    /// First byte of the chord (default: Ctrl-\, 0x1C).
    pub first_byte: u8,
    /// Second byte for history hotkey (default: 'h').
    pub history_byte: u8,
    /// Second byte for completions hotkey (default: 'c').
    pub completions_byte: u8,
}

impl Default for HotkeyConfig {
    fn default() -> Self {
        Self {
            timeout: DEFAULT_CHORD_TIMEOUT,
            first_byte: CHORD_FIRST_BYTE,
            history_byte: CHORD_HISTORY_BYTE,
            completions_byte: CHORD_COMPLETIONS_BYTE,
        }
    }
}

/// A state machine parser for detecting two-key hotkey chords.
///
/// # Example
///
/// ```
/// use clai_wrap::hotkey::{HotkeyParser, HotkeyEvent, HotkeyType};
///
/// let mut parser = HotkeyParser::new();
///
/// // Process Ctrl-\ (0x1C)
/// let event = parser.process_byte(0x1C);
/// assert!(event.is_none()); // Waiting for second byte
///
/// // Process 'h' for history
/// let event = parser.process_byte(b'h');
/// assert_eq!(event, Some(HotkeyEvent::Triggered(HotkeyType::History)));
/// ```
#[derive(Debug)]
pub struct HotkeyParser {
    state: State,
    config: HotkeyConfig,
}

impl Default for HotkeyParser {
    fn default() -> Self {
        Self::new()
    }
}

impl HotkeyParser {
    /// Creates a new `HotkeyParser` with default configuration.
    #[must_use]
    pub fn new() -> Self {
        Self {
            state: State::Idle,
            config: HotkeyConfig::default(),
        }
    }

    /// Creates a new `HotkeyParser` with custom configuration.
    #[must_use]
    pub const fn with_config(config: HotkeyConfig) -> Self {
        Self {
            state: State::Idle,
            config,
        }
    }

    /// Returns the current timeout configuration.
    #[must_use]
    pub const fn timeout(&self) -> Duration {
        self.config.timeout
    }

    /// Checks if the parser is currently waiting for a second byte.
    #[must_use]
    pub const fn is_waiting(&self) -> bool {
        matches!(self.state, State::WaitingForSecond { .. })
    }

    /// Checks if the chord has timed out.
    ///
    /// Returns `Some(HotkeyEvent::Forward(...))` if timed out with the first byte,
    /// or `None` if not waiting or not timed out.
    pub fn check_timeout(&mut self) -> Option<HotkeyEvent> {
        if let State::WaitingForSecond {
            start_time,
            first_byte,
        } = self.state
        {
            if start_time.elapsed() >= self.config.timeout {
                self.state = State::Idle;
                return Some(HotkeyEvent::Forward(vec![first_byte]));
            }
        }
        None
    }

    /// Processes a single byte and returns an optional hotkey event.
    ///
    /// # Returns
    ///
    /// - `None` if the parser is buffering (waiting for more input).
    /// - `Some(HotkeyEvent::Triggered(...))` if a hotkey chord was completed.
    /// - `Some(HotkeyEvent::Forward(...))` if bytes should be forwarded.
    pub fn process_byte(&mut self, byte: u8) -> Option<HotkeyEvent> {
        match &self.state {
            State::Idle => {
                if byte == self.config.first_byte {
                    // Start of a potential chord
                    self.state = State::WaitingForSecond {
                        start_time: Instant::now(),
                        first_byte: byte,
                    };
                    None
                } else {
                    // Not a chord start, forward the byte
                    Some(HotkeyEvent::Forward(vec![byte]))
                }
            }
            State::WaitingForSecond {
                start_time,
                first_byte,
            } => {
                let first = *first_byte;
                let elapsed = start_time.elapsed();

                // Check for timeout
                if elapsed >= self.config.timeout {
                    self.state = State::Idle;
                    // Timeout: forward the first byte, then process this byte recursively
                    let mut bytes = vec![first];
                    if byte != self.config.first_byte {
                        bytes.push(byte);
                        return Some(HotkeyEvent::Forward(bytes));
                    }
                    // The new byte is another chord start
                    self.state = State::WaitingForSecond {
                        start_time: Instant::now(),
                        first_byte: byte,
                    };
                    return Some(HotkeyEvent::Forward(vec![first]));
                }

                // Check for valid second bytes
                if byte == self.config.history_byte {
                    self.state = State::Idle;
                    Some(HotkeyEvent::Triggered(HotkeyType::History))
                } else if byte == self.config.completions_byte {
                    self.state = State::Idle;
                    Some(HotkeyEvent::Triggered(HotkeyType::Completions))
                } else if byte == ESC_BYTE {
                    // Escape cancels the chord, forward both bytes
                    self.state = State::Idle;
                    Some(HotkeyEvent::Forward(vec![first, byte]))
                } else if byte == self.config.first_byte {
                    // Another chord start byte: forward the first, keep waiting
                    self.state = State::WaitingForSecond {
                        start_time: Instant::now(),
                        first_byte: byte,
                    };
                    Some(HotkeyEvent::Forward(vec![first]))
                } else {
                    // Invalid second byte: forward both
                    self.state = State::Idle;
                    Some(HotkeyEvent::Forward(vec![first, byte]))
                }
            }
        }
    }

    /// Processes multiple bytes and returns all resulting events.
    ///
    /// This is a convenience method for processing a slice of bytes.
    pub fn process_bytes(&mut self, bytes: &[u8]) -> Vec<HotkeyEvent> {
        let mut events = Vec::new();
        for &byte in bytes {
            if let Some(event) = self.process_byte(byte) {
                events.push(event);
            }
        }
        events
    }

    /// Resets the parser to idle state, optionally returning any buffered bytes.
    pub fn reset(&mut self) -> Option<HotkeyEvent> {
        match std::mem::replace(&mut self.state, State::Idle) {
            State::Idle => None,
            State::WaitingForSecond { first_byte, .. } => {
                Some(HotkeyEvent::Forward(vec![first_byte]))
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    #[test]
    fn test_chord_triggers_history() {
        let mut parser = HotkeyParser::new();

        // Process Ctrl-\ (0x1C)
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(event.is_none(), "Should buffer first byte");
        assert!(parser.is_waiting(), "Should be waiting for second byte");

        // Process 'h' for history
        let event = parser.process_byte(b'h');
        assert_eq!(event, Some(HotkeyEvent::Triggered(HotkeyType::History)));
        assert!(!parser.is_waiting(), "Should be back to idle");
    }

    #[test]
    fn test_chord_triggers_completions() {
        let mut parser = HotkeyParser::new();

        // Process Ctrl-\ (0x1C)
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(event.is_none());

        // Process 'c' for completions
        let event = parser.process_byte(b'c');
        assert_eq!(event, Some(HotkeyEvent::Triggered(HotkeyType::Completions)));
    }

    #[test]
    fn test_chord_timeout() {
        // Use a very short timeout for testing
        let config = HotkeyConfig {
            timeout: Duration::from_millis(10),
            ..Default::default()
        };
        let mut parser = HotkeyParser::with_config(config);

        // Process Ctrl-\ (0x1C)
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(event.is_none());

        // Wait for timeout
        thread::sleep(Duration::from_millis(20));

        // Process 'h' after timeout
        let event = parser.process_byte(b'h');
        assert_eq!(
            event,
            Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE, b'h']))
        );
    }

    #[test]
    fn test_chord_cancel_with_escape() {
        let mut parser = HotkeyParser::new();

        // Process Ctrl-\ (0x1C)
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(event.is_none());

        // Process Escape to cancel
        let event = parser.process_byte(ESC_BYTE);
        assert_eq!(
            event,
            Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE, ESC_BYTE]))
        );
        assert!(!parser.is_waiting());
    }

    #[test]
    fn test_rapid_inputs_no_byte_loss() {
        let mut parser = HotkeyParser::new();

        // Rapid sequence: normal bytes
        let input = b"hello";
        let events = parser.process_bytes(input);

        // Each byte should be forwarded individually
        assert_eq!(events.len(), 5);
        for (i, event) in events.iter().enumerate() {
            assert_eq!(*event, HotkeyEvent::Forward(vec![input[i]]));
        }
    }

    #[test]
    fn test_rapid_inputs_with_chords() {
        let mut parser = HotkeyParser::new();

        // Mix of regular input and chords
        let input = [b'a', CHORD_FIRST_BYTE, b'h', b'b'];
        let events = parser.process_bytes(&input);

        assert_eq!(events.len(), 3);
        assert_eq!(events[0], HotkeyEvent::Forward(vec![b'a']));
        assert_eq!(events[1], HotkeyEvent::Triggered(HotkeyType::History));
        assert_eq!(events[2], HotkeyEvent::Forward(vec![b'b']));
    }

    #[test]
    fn test_sigquit_byte_in_raw_mode() {
        // In raw mode, Ctrl-\ (0x1C) is passed through as a byte
        // rather than generating SIGQUIT
        let mut parser = HotkeyParser::new();

        // Single 0x1C should be buffered
        let event = parser.process_byte(0x1C);
        assert!(event.is_none());
        assert!(parser.is_waiting());

        // Invalid second byte should forward both
        let event = parser.process_byte(b'x');
        assert_eq!(event, Some(HotkeyEvent::Forward(vec![0x1C, b'x'])));
    }

    #[test]
    fn test_double_chord_start() {
        let mut parser = HotkeyParser::new();

        // First Ctrl-\
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(event.is_none());

        // Second Ctrl-\ should forward the first and wait again
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert_eq!(event, Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE])));
        assert!(parser.is_waiting());

        // Now 'h' should trigger history
        let event = parser.process_byte(b'h');
        assert_eq!(event, Some(HotkeyEvent::Triggered(HotkeyType::History)));
    }

    #[test]
    fn test_reset() {
        let mut parser = HotkeyParser::new();

        // Start a chord
        let _ = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(parser.is_waiting());

        // Reset should return the buffered byte
        let event = parser.reset();
        assert_eq!(event, Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE])));
        assert!(!parser.is_waiting());

        // Reset when idle should return None
        let event = parser.reset();
        assert!(event.is_none());
    }

    #[test]
    fn test_check_timeout() {
        let config = HotkeyConfig {
            timeout: Duration::from_millis(10),
            ..Default::default()
        };
        let mut parser = HotkeyParser::with_config(config);

        // Check timeout when idle - should return None
        let event = parser.check_timeout();
        assert!(event.is_none());

        // Start a chord
        let _ = parser.process_byte(CHORD_FIRST_BYTE);
        assert!(parser.is_waiting());

        // Check timeout before expiry
        let event = parser.check_timeout();
        assert!(event.is_none());
        assert!(parser.is_waiting());

        // Wait for timeout
        thread::sleep(Duration::from_millis(20));

        // Check timeout after expiry
        let event = parser.check_timeout();
        assert_eq!(event, Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE])));
        assert!(!parser.is_waiting());
    }

    #[test]
    fn test_custom_config() {
        let config = HotkeyConfig {
            timeout: Duration::from_millis(100),
            first_byte: b'@',
            history_byte: b'1',
            completions_byte: b'2',
        };
        let mut parser = HotkeyParser::with_config(config);

        // Standard chord should not work
        let event = parser.process_byte(CHORD_FIRST_BYTE);
        assert_eq!(event, Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE])));

        // Custom chord should work
        let event = parser.process_byte(b'@');
        assert!(event.is_none());

        let event = parser.process_byte(b'1');
        assert_eq!(event, Some(HotkeyEvent::Triggered(HotkeyType::History)));

        // Test completions with custom config
        let event = parser.process_byte(b'@');
        assert!(event.is_none());

        let event = parser.process_byte(b'2');
        assert_eq!(event, Some(HotkeyEvent::Triggered(HotkeyType::Completions)));
    }

    #[test]
    fn test_timeout_accessor() {
        let parser = HotkeyParser::new();
        assert_eq!(parser.timeout(), DEFAULT_CHORD_TIMEOUT);

        let config = HotkeyConfig {
            timeout: Duration::from_millis(100),
            ..Default::default()
        };
        let parser = HotkeyParser::with_config(config);
        assert_eq!(parser.timeout(), Duration::from_millis(100));
    }
}
