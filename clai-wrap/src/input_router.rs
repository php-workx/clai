//! Input router module for clai-wrap.
//!
//! This module provides an input routing layer that sits between stdin input
//! and the PTY. It detects hotkey chords and forwards non-hotkey bytes to
//! the PTY.

use std::sync::mpsc::Sender;
use std::time::Duration;

use anyhow::Result;

use crate::hotkey::{HotkeyConfig, HotkeyEvent, HotkeyParser, HotkeyType};

/// Events produced by the input router.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InputEvent {
    /// Bytes that should be forwarded to the PTY.
    ForwardToPty(Vec<u8>),
    /// Hotkey triggered - open history picker.
    OpenHistoryPicker,
    /// Hotkey triggered - open completions picker.
    OpenCompletionsPicker,
}

/// Input router that detects hotkeys and forwards other input to the PTY.
///
/// The `InputRouter` processes input bytes from stdin, detecting two-key chord
/// sequences for hotkeys while forwarding all other bytes to the PTY. It uses
/// the underlying `HotkeyParser` for chord detection and emits events through
/// a channel.
///
/// # Example
///
/// ```
/// use std::sync::mpsc;
/// use clai_wrap::input_router::{InputRouter, InputEvent};
/// use clai_wrap::hotkey::HotkeyConfig;
///
/// let (tx, rx) = mpsc::channel();
/// let mut router = InputRouter::new(HotkeyConfig::default(), tx);
///
/// // Process regular input - will emit ForwardToPty
/// router.process_byte(b'a').unwrap();
///
/// // Check for the event
/// if let Ok(event) = rx.try_recv() {
///     assert_eq!(event, InputEvent::ForwardToPty(vec![b'a']));
/// }
/// ```
#[derive(Debug)]
pub struct InputRouter {
    hotkey_parser: HotkeyParser,
    event_tx: Sender<InputEvent>,
}

impl InputRouter {
    /// Creates a new `InputRouter` with the given configuration and event channel.
    ///
    /// # Arguments
    ///
    /// * `config` - Configuration for hotkey detection
    /// * `event_tx` - Channel sender for emitting input events
    #[must_use]
    pub const fn new(config: HotkeyConfig, event_tx: Sender<InputEvent>) -> Self {
        Self {
            hotkey_parser: HotkeyParser::with_config(config),
            event_tx,
        }
    }

    /// Creates a new `InputRouter` with default hotkey configuration.
    ///
    /// # Arguments
    ///
    /// * `event_tx` - Channel sender for emitting input events
    #[must_use]
    pub fn with_default_config(event_tx: Sender<InputEvent>) -> Self {
        Self {
            hotkey_parser: HotkeyParser::new(),
            event_tx,
        }
    }

    /// Returns the current timeout configuration.
    #[must_use]
    pub const fn timeout(&self) -> Duration {
        self.hotkey_parser.timeout()
    }

    /// Returns whether the parser is currently waiting for a second byte.
    #[must_use]
    pub const fn is_waiting(&self) -> bool {
        self.hotkey_parser.is_waiting()
    }

    /// Processes a single input byte.
    ///
    /// The byte is passed to the hotkey parser. Depending on the parser's
    /// response, this method may emit:
    /// - `InputEvent::ForwardToPty` for bytes that should go to the PTY
    /// - `InputEvent::OpenHistoryPicker` when the history hotkey is triggered
    /// - `InputEvent::OpenCompletionsPicker` when the completions hotkey is triggered
    ///
    /// # Errors
    ///
    /// Returns an error if sending an event to the channel fails.
    pub fn process_byte(&mut self, byte: u8) -> Result<()> {
        if let Some(hotkey_event) = self.hotkey_parser.process_byte(byte) {
            self.emit_event(hotkey_event)?;
        }
        Ok(())
    }

    /// Processes multiple input bytes.
    ///
    /// This is a convenience method that calls `process_byte` for each byte
    /// in the slice. Events are emitted as they are produced by the parser.
    ///
    /// # Errors
    ///
    /// Returns an error if sending any event to the channel fails.
    pub fn process_bytes(&mut self, bytes: &[u8]) -> Result<()> {
        for &byte in bytes {
            self.process_byte(byte)?;
        }
        Ok(())
    }

    /// Checks for hotkey timeout and emits any resulting events.
    ///
    /// This method should be called periodically to handle the case where
    /// the first byte of a chord was received but the timeout has elapsed
    /// before a second byte arrived.
    ///
    /// # Errors
    ///
    /// Returns an error if sending an event to the channel fails.
    pub fn check_timeout(&mut self) -> Result<()> {
        if let Some(hotkey_event) = self.hotkey_parser.check_timeout() {
            self.emit_event(hotkey_event)?;
        }
        Ok(())
    }

    /// Resets the parser state, emitting any buffered bytes.
    ///
    /// This can be used to flush any pending state, for example when
    /// switching to a different input mode.
    ///
    /// # Errors
    ///
    /// Returns an error if sending an event to the channel fails.
    pub fn reset(&mut self) -> Result<()> {
        if let Some(hotkey_event) = self.hotkey_parser.reset() {
            self.emit_event(hotkey_event)?;
        }
        Ok(())
    }

    /// Converts a `HotkeyEvent` to an `InputEvent` and sends it.
    fn emit_event(&self, hotkey_event: HotkeyEvent) -> Result<()> {
        let input_event = match hotkey_event {
            HotkeyEvent::Triggered(HotkeyType::History) => InputEvent::OpenHistoryPicker,
            HotkeyEvent::Triggered(HotkeyType::Completions) => InputEvent::OpenCompletionsPicker,
            HotkeyEvent::Forward(bytes) => InputEvent::ForwardToPty(bytes),
        };
        self.event_tx
            .send(input_event)
            .map_err(|e| anyhow::anyhow!("Failed to send input event: {e}"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::hotkey::{CHORD_COMPLETIONS_BYTE, CHORD_FIRST_BYTE, CHORD_HISTORY_BYTE};
    use std::sync::mpsc;
    use std::thread;

    /// Helper to create a router and receiver for testing.
    fn create_test_router() -> (InputRouter, mpsc::Receiver<InputEvent>) {
        let (tx, rx) = mpsc::channel();
        let router = InputRouter::with_default_config(tx);
        (router, rx)
    }

    /// Helper to create a router with custom config.
    fn create_test_router_with_config(
        config: HotkeyConfig,
    ) -> (InputRouter, mpsc::Receiver<InputEvent>) {
        let (tx, rx) = mpsc::channel();
        let router = InputRouter::new(config, tx);
        (router, rx)
    }

    /// Helper to collect all available events from the receiver.
    fn collect_events(rx: &mpsc::Receiver<InputEvent>) -> Vec<InputEvent> {
        let mut events = Vec::new();
        while let Ok(event) = rx.try_recv() {
            events.push(event);
        }
        events
    }

    #[test]
    fn test_normal_input_forwarding() {
        let (mut router, rx) = create_test_router();

        // Process regular input
        router.process_bytes(b"hello").unwrap();

        let events = collect_events(&rx);

        // Each byte should be forwarded individually
        assert_eq!(events.len(), 5);
        assert_eq!(events[0], InputEvent::ForwardToPty(vec![b'h']));
        assert_eq!(events[1], InputEvent::ForwardToPty(vec![b'e']));
        assert_eq!(events[2], InputEvent::ForwardToPty(vec![b'l']));
        assert_eq!(events[3], InputEvent::ForwardToPty(vec![b'l']));
        assert_eq!(events[4], InputEvent::ForwardToPty(vec![b'o']));
    }

    #[test]
    fn test_hotkey_detection_history() {
        let (mut router, rx) = create_test_router();

        // Process Ctrl-\ followed by 'h'
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(CHORD_HISTORY_BYTE).unwrap();

        let events = collect_events(&rx);

        // Should emit OpenHistoryPicker
        assert_eq!(events.len(), 1);
        assert_eq!(events[0], InputEvent::OpenHistoryPicker);
    }

    #[test]
    fn test_hotkey_detection_completions() {
        let (mut router, rx) = create_test_router();

        // Process Ctrl-\ followed by 'c'
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(CHORD_COMPLETIONS_BYTE).unwrap();

        let events = collect_events(&rx);

        // Should emit OpenCompletionsPicker
        assert_eq!(events.len(), 1);
        assert_eq!(events[0], InputEvent::OpenCompletionsPicker);
    }

    #[test]
    fn test_hotkey_timeout() {
        let config = HotkeyConfig {
            timeout: Duration::from_millis(10),
            ..Default::default()
        };
        let (mut router, rx) = create_test_router_with_config(config);

        // Process Ctrl-\ (first byte of chord)
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        assert!(router.is_waiting());

        // Wait for timeout
        thread::sleep(Duration::from_millis(20));

        // Check timeout - should forward the buffered byte
        router.check_timeout().unwrap();

        let events = collect_events(&rx);

        // Should emit ForwardToPty with the first byte
        assert_eq!(events.len(), 1);
        assert_eq!(events[0], InputEvent::ForwardToPty(vec![CHORD_FIRST_BYTE]));
        assert!(!router.is_waiting());
    }

    #[test]
    fn test_rapid_input_handling() {
        let (mut router, rx) = create_test_router();

        // Rapid sequence with embedded hotkey
        let input = [b'a', b'b', CHORD_FIRST_BYTE, CHORD_HISTORY_BYTE, b'c', b'd'];
        router.process_bytes(&input).unwrap();

        let events = collect_events(&rx);

        // Should emit: forward 'a', forward 'b', history hotkey, forward 'c', forward 'd'
        assert_eq!(events.len(), 5);
        assert_eq!(events[0], InputEvent::ForwardToPty(vec![b'a']));
        assert_eq!(events[1], InputEvent::ForwardToPty(vec![b'b']));
        assert_eq!(events[2], InputEvent::OpenHistoryPicker);
        assert_eq!(events[3], InputEvent::ForwardToPty(vec![b'c']));
        assert_eq!(events[4], InputEvent::ForwardToPty(vec![b'd']));
    }

    #[test]
    fn test_invalid_second_byte_forwards_both() {
        let (mut router, rx) = create_test_router();

        // Process Ctrl-\ followed by an invalid second byte
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(b'x').unwrap();

        let events = collect_events(&rx);

        // Should emit ForwardToPty with both bytes
        assert_eq!(events.len(), 1);
        assert_eq!(
            events[0],
            InputEvent::ForwardToPty(vec![CHORD_FIRST_BYTE, b'x'])
        );
    }

    #[test]
    fn test_double_chord_start() {
        let (mut router, rx) = create_test_router();

        // Two Ctrl-\ in a row
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(CHORD_FIRST_BYTE).unwrap();

        let events = collect_events(&rx);

        // First Ctrl-\ should be forwarded, still waiting for second byte
        assert_eq!(events.len(), 1);
        assert_eq!(events[0], InputEvent::ForwardToPty(vec![CHORD_FIRST_BYTE]));
        assert!(router.is_waiting());

        // Now complete with 'h'
        router.process_byte(CHORD_HISTORY_BYTE).unwrap();

        let events = collect_events(&rx);
        assert_eq!(events.len(), 1);
        assert_eq!(events[0], InputEvent::OpenHistoryPicker);
    }

    #[test]
    fn test_reset_emits_buffered_bytes() {
        let (mut router, rx) = create_test_router();

        // Start a chord
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        assert!(router.is_waiting());

        // Reset
        router.reset().unwrap();

        let events = collect_events(&rx);

        // Should emit the buffered byte
        assert_eq!(events.len(), 1);
        assert_eq!(events[0], InputEvent::ForwardToPty(vec![CHORD_FIRST_BYTE]));
        assert!(!router.is_waiting());
    }

    #[test]
    fn test_reset_when_idle_does_nothing() {
        let (mut router, rx) = create_test_router();

        // Reset when not waiting
        router.reset().unwrap();

        let events = collect_events(&rx);
        assert!(events.is_empty());
    }

    #[test]
    fn test_timeout_accessor() {
        let (router, _rx) = create_test_router();
        assert_eq!(router.timeout(), crate::hotkey::DEFAULT_CHORD_TIMEOUT);

        let config = HotkeyConfig {
            timeout: Duration::from_millis(100),
            ..Default::default()
        };
        let (router, _rx) = create_test_router_with_config(config);
        assert_eq!(router.timeout(), Duration::from_millis(100));
    }

    #[test]
    fn test_is_waiting() {
        let (mut router, _rx) = create_test_router();

        assert!(!router.is_waiting());

        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        assert!(router.is_waiting());

        router.process_byte(CHORD_HISTORY_BYTE).unwrap();
        assert!(!router.is_waiting());
    }

    #[test]
    fn test_channel_error_handling() {
        let (tx, rx) = mpsc::channel();
        let mut router = InputRouter::with_default_config(tx);

        // Drop the receiver to cause send errors
        drop(rx);

        // Processing should return an error
        let result = router.process_byte(b'a');
        assert!(result.is_err());
    }

    #[test]
    fn test_multiple_hotkeys_in_sequence() {
        let (mut router, rx) = create_test_router();

        // History hotkey
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(CHORD_HISTORY_BYTE).unwrap();

        // Completions hotkey
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(CHORD_COMPLETIONS_BYTE).unwrap();

        let events = collect_events(&rx);

        assert_eq!(events.len(), 2);
        assert_eq!(events[0], InputEvent::OpenHistoryPicker);
        assert_eq!(events[1], InputEvent::OpenCompletionsPicker);
    }

    #[test]
    fn test_escape_cancels_chord() {
        let (mut router, rx) = create_test_router();

        // Start chord then cancel with escape
        router.process_byte(CHORD_FIRST_BYTE).unwrap();
        router.process_byte(0x1B).unwrap(); // ESC

        let events = collect_events(&rx);

        // Should forward both bytes
        assert_eq!(events.len(), 1);
        assert_eq!(
            events[0],
            InputEvent::ForwardToPty(vec![CHORD_FIRST_BYTE, 0x1B])
        );
    }

    #[test]
    fn test_timeout_before_second_byte_then_process_new_input() {
        let config = HotkeyConfig {
            timeout: Duration::from_millis(10),
            ..Default::default()
        };
        let (mut router, rx) = create_test_router_with_config(config);

        // Start a chord
        router.process_byte(CHORD_FIRST_BYTE).unwrap();

        // Wait for timeout
        thread::sleep(Duration::from_millis(20));

        // Process new input (after timeout)
        router.process_byte(b'a').unwrap();

        let events = collect_events(&rx);

        // Should emit both the timed-out first byte and 'a'
        // Because the parser detects timeout when processing the next byte
        assert_eq!(events.len(), 1);
        assert_eq!(
            events[0],
            InputEvent::ForwardToPty(vec![CHORD_FIRST_BYTE, b'a'])
        );
    }

    #[test]
    fn test_empty_bytes_processing() {
        let (mut router, rx) = create_test_router();

        // Process empty slice
        router.process_bytes(&[]).unwrap();

        let events = collect_events(&rx);
        assert!(events.is_empty());
    }

    #[test]
    fn test_binary_data_forwarding() {
        let (mut router, rx) = create_test_router();

        // Process binary data (all bytes except chord first byte)
        let binary_data: Vec<u8> = (0..=255).filter(|&b| b != CHORD_FIRST_BYTE).collect();
        router.process_bytes(&binary_data).unwrap();

        let events = collect_events(&rx);

        // Each byte should be forwarded
        assert_eq!(events.len(), binary_data.len());
        for (i, event) in events.iter().enumerate() {
            assert_eq!(*event, InputEvent::ForwardToPty(vec![binary_data[i]]));
        }
    }
}
