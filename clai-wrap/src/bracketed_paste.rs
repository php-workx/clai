//! Bracketed Paste Encoder Module
//!
//! This module handles bracketed paste mode for terminal emulators.
//! Bracketed paste wraps pasted content with escape sequences so the
//! terminal application can distinguish pasted text from typed text.
//!
//! The escape sequences are:
//! - `\x1b[?2004h` - Enable bracketed paste mode
//! - `\x1b[?2004l` - Disable bracketed paste mode
//! - `\x1b[200~` - Start of pasted content
//! - `\x1b[201~` - End of pasted content

use thiserror::Error;

/// Escape sequence to enable bracketed paste mode
const ENABLE_BRACKETED_PASTE: &[u8] = b"\x1b[?2004h";

/// Escape sequence to disable bracketed paste mode
const DISABLE_BRACKETED_PASTE: &[u8] = b"\x1b[?2004l";

/// Escape sequence marking the start of pasted content
const PASTE_START: &[u8] = b"\x1b[200~";

/// Escape sequence marking the end of pasted content
const PASTE_END: &[u8] = b"\x1b[201~";

/// Errors that can occur during paste operations
#[derive(Debug, Error, PartialEq, Eq)]
pub enum PasteError {
    /// The paste content contains NUL bytes which are not allowed
    #[error("paste content contains NUL byte at position {position}")]
    ContainsNul {
        /// The position of the first NUL byte in the content
        position: usize,
    },
}

/// Tracks the bracketed paste mode state by monitoring terminal output.
///
/// Terminal applications enable bracketed paste mode by sending `\x1b[?2004h`
/// and disable it with `\x1b[?2004l`. This tracker monitors the output stream
/// to maintain the current state.
#[derive(Debug, Default)]
pub struct BracketedPasteTracker {
    /// Whether bracketed paste mode is currently enabled
    enabled: bool,
    /// Buffer for partial sequence matching
    partial_match: Vec<u8>,
}

impl BracketedPasteTracker {
    /// Creates a new tracker with bracketed paste mode disabled by default.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Returns whether bracketed paste mode is currently enabled.
    #[must_use]
    pub const fn is_enabled(&self) -> bool {
        self.enabled
    }

    /// Updates the tracker state by scanning output bytes for enable/disable sequences.
    ///
    /// This method should be called with all bytes sent from the PTY to the terminal
    /// so the tracker can detect when the shell enables or disables bracketed paste mode.
    pub fn update_from_output(&mut self, bytes: &[u8]) {
        for &byte in bytes {
            self.partial_match.push(byte);

            // Check if we have a complete enable sequence
            if self.partial_match.ends_with(ENABLE_BRACKETED_PASTE) {
                self.enabled = true;
                self.partial_match.clear();
                continue;
            }

            // Check if we have a complete disable sequence
            if self.partial_match.ends_with(DISABLE_BRACKETED_PASTE) {
                self.enabled = false;
                self.partial_match.clear();
                continue;
            }

            // Prune the buffer if it's getting too long and can't possibly match
            // The longest sequence we're looking for is 8 bytes (\x1b[?2004h or \x1b[?2004l)
            if self.partial_match.len() > 16 {
                // Keep only the last few bytes that might be the start of a sequence
                let keep_from = self.partial_match.len() - 8;
                self.partial_match.drain(..keep_from);
            }
        }
    }

    /// Wraps content with bracketed paste escape sequences.
    ///
    /// Returns the content wrapped with `\x1b[200~` prefix and `\x1b[201~` suffix.
    ///
    /// # Errors
    ///
    /// Returns [`PasteError::ContainsNul`] if the content contains any NUL bytes,
    /// as these are not valid in paste content.
    pub fn wrap_content(content: &[u8]) -> Result<Vec<u8>, PasteError> {
        // Check for NUL bytes
        if let Some(position) = content.iter().position(|&b| b == 0) {
            return Err(PasteError::ContainsNul { position });
        }

        let mut result = Vec::with_capacity(PASTE_START.len() + content.len() + PASTE_END.len());
        result.extend_from_slice(PASTE_START);
        result.extend_from_slice(content);
        result.extend_from_slice(PASTE_END);

        Ok(result)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_wrap_content() {
        let content = b"hello world";
        let wrapped = BracketedPasteTracker::wrap_content(content).unwrap();

        // Check the result contains the proper prefix and suffix
        assert!(wrapped.starts_with(PASTE_START));
        assert!(wrapped.ends_with(PASTE_END));

        // Check the content is in the middle
        let expected = b"\x1b[200~hello world\x1b[201~";
        assert_eq!(wrapped, expected.to_vec());
    }

    #[test]
    fn test_utf8_preserved() {
        // UTF-8 content with various characters
        let content = "Hello, \u{4e16}\u{754c}! \u{1f600}".as_bytes(); // "Hello, 世界! 😀"
        let wrapped = BracketedPasteTracker::wrap_content(content).unwrap();

        // Verify the content is preserved correctly
        let prefix_len = PASTE_START.len();
        let suffix_len = PASTE_END.len();
        let inner = &wrapped[prefix_len..wrapped.len() - suffix_len];
        assert_eq!(inner, content);

        // Verify the UTF-8 can be decoded correctly
        let inner_str = std::str::from_utf8(inner).unwrap();
        assert_eq!(inner_str, "Hello, \u{4e16}\u{754c}! \u{1f600}");
    }

    #[test]
    fn test_nul_rejected() {
        // Content with NUL byte at position 5
        let content = b"hello\x00world";
        let result = BracketedPasteTracker::wrap_content(content);

        assert_eq!(result, Err(PasteError::ContainsNul { position: 5 }));
    }

    #[test]
    fn test_nul_at_start() {
        let content = b"\x00hello";
        let result = BracketedPasteTracker::wrap_content(content);

        assert_eq!(result, Err(PasteError::ContainsNul { position: 0 }));
    }

    #[test]
    fn test_enable_disable() {
        let mut tracker = BracketedPasteTracker::new();

        // Initially disabled
        assert!(!tracker.is_enabled());

        // Enable with the escape sequence
        tracker.update_from_output(b"\x1b[?2004h");
        assert!(tracker.is_enabled());

        // Disable with the escape sequence
        tracker.update_from_output(b"\x1b[?2004l");
        assert!(!tracker.is_enabled());

        // Enable again
        tracker.update_from_output(b"\x1b[?2004h");
        assert!(tracker.is_enabled());
    }

    #[test]
    fn test_enable_disable_in_stream() {
        let mut tracker = BracketedPasteTracker::new();

        // Simulate a typical shell output stream with enable sequence embedded
        let output = b"some prompt text\x1b[?2004hmore text";
        tracker.update_from_output(output);
        assert!(tracker.is_enabled());

        // Now disable in another stream
        let output2 = b"running command\x1b[?2004lcommand output";
        tracker.update_from_output(output2);
        assert!(!tracker.is_enabled());
    }

    #[test]
    fn test_sequence_split_across_updates() {
        let mut tracker = BracketedPasteTracker::new();

        // Split the enable sequence across multiple updates
        tracker.update_from_output(b"\x1b[?20");
        assert!(!tracker.is_enabled());

        tracker.update_from_output(b"04h");
        assert!(tracker.is_enabled());
    }

    #[test]
    fn test_default_disabled() {
        let tracker = BracketedPasteTracker::new();
        assert!(!tracker.is_enabled());

        let tracker_default = BracketedPasteTracker::default();
        assert!(!tracker_default.is_enabled());
    }

    #[test]
    fn test_wrap_empty_content() {
        let content = b"";
        let wrapped = BracketedPasteTracker::wrap_content(content).unwrap();

        let expected = b"\x1b[200~\x1b[201~";
        assert_eq!(wrapped, expected.to_vec());
    }

    #[test]
    fn test_multiple_enable_sequences() {
        let mut tracker = BracketedPasteTracker::new();

        // Multiple enables should still be enabled
        tracker.update_from_output(b"\x1b[?2004h\x1b[?2004h");
        assert!(tracker.is_enabled());
    }

    #[test]
    fn test_noise_between_sequences() {
        let mut tracker = BracketedPasteTracker::new();

        // Random noise shouldn't affect state
        tracker.update_from_output(b"random text and \x1b[0m escape codes");
        assert!(!tracker.is_enabled());

        // But proper sequence should work
        tracker.update_from_output(b"\x1b[?2004h");
        assert!(tracker.is_enabled());
    }
}
