//! Selection Injection Module for clai-wrap.
//!
//! This module provides functionality to inject selected text into a PTY session.
//! It supports both raw byte injection and bracketed paste mode, which wraps
//! content with escape sequences so terminal applications can distinguish
//! pasted text from typed text.
//!
//! # Bracketed Paste Mode
//!
//! When bracketed paste is enabled, content is wrapped with:
//! - `\x1b[200~` - Start of pasted content
//! - `\x1b[201~` - End of pasted content
//!
//! The injector tracks whether the shell has enabled bracketed paste mode
//! by monitoring for `\x1b[?2004h` in the PTY output stream.
//!
//! # Execute Mode
//!
//! The injector supports two modes:
//! - **Insert only**: Injects the selection without executing it
//! - **Execute**: Appends a newline after the content to execute the command

use std::io::{Result, Write};

use crate::bracketed_paste::BracketedPasteTracker;

/// Escape sequence marking the start of pasted content.
const PASTE_START: &[u8] = b"\x1b[200~";

/// Escape sequence marking the end of pasted content.
const PASTE_END: &[u8] = b"\x1b[201~";

/// Newline character for execute mode.
const NEWLINE: &[u8] = b"\n";

/// Handles injection of selected text into a PTY session.
///
/// The injector supports both raw byte injection and bracketed paste mode.
/// It uses the `BracketedPasteTracker`'s state to determine whether to
/// wrap content with bracketed paste escape sequences.
///
/// # Example
///
/// ```
/// use std::io::Cursor;
/// use clai_wrap::selection_inject::SelectionInjector;
///
/// let mut injector = SelectionInjector::new();
/// let mut output = Cursor::new(Vec::new());
///
/// // Inject without bracketed paste
/// injector.inject(&mut output, "echo hello").unwrap();
/// assert_eq!(output.get_ref(), b"echo hello");
///
/// // Enable bracketed paste and inject again
/// let mut output = Cursor::new(Vec::new());
/// injector.set_bracketed_paste(true);
/// injector.inject(&mut output, "echo hello").unwrap();
/// assert_eq!(output.get_ref(), b"\x1b[200~echo hello\x1b[201~");
/// ```
#[derive(Debug, Default)]
pub struct SelectionInjector {
    /// Whether to use bracketed paste mode for injection.
    use_bracketed_paste: bool,
}

impl SelectionInjector {
    /// Creates a new `SelectionInjector` with bracketed paste disabled.
    ///
    /// By default, the injector does not use bracketed paste mode.
    /// Use `set_bracketed_paste(true)` after detecting that the shell
    /// has enabled bracketed paste mode.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets whether to use bracketed paste mode for injection.
    ///
    /// When enabled, injected content will be wrapped with escape sequences:
    /// - `\x1b[200~` before the content
    /// - `\x1b[201~` after the content
    ///
    /// # Arguments
    ///
    /// * `enabled` - Whether to enable bracketed paste mode.
    pub const fn set_bracketed_paste(&mut self, enabled: bool) {
        self.use_bracketed_paste = enabled;
    }

    /// Returns whether bracketed paste mode is enabled.
    #[must_use]
    pub const fn is_bracketed_paste_enabled(&self) -> bool {
        self.use_bracketed_paste
    }

    /// Synchronizes the injector's bracketed paste state with a tracker.
    ///
    /// This is a convenience method that sets the injector's bracketed paste
    /// mode based on the tracker's current state.
    ///
    /// # Arguments
    ///
    /// * `tracker` - The `BracketedPasteTracker` to sync state from.
    pub const fn sync_with_tracker(&mut self, tracker: &BracketedPasteTracker) {
        self.use_bracketed_paste = tracker.is_enabled();
    }

    /// Injects the selection into the PTY without executing it.
    ///
    /// If bracketed paste mode is enabled, the content is wrapped with
    /// escape sequences. Otherwise, raw bytes are written directly.
    ///
    /// # Arguments
    ///
    /// * `pty_writer` - A writer connected to the PTY master.
    /// * `selection` - The text to inject into the PTY.
    ///
    /// # Returns
    ///
    /// `Ok(())` if the injection was successful, or an I/O error if writing failed.
    ///
    /// # Example
    ///
    /// ```
    /// use std::io::Cursor;
    /// use clai_wrap::selection_inject::SelectionInjector;
    ///
    /// let injector = SelectionInjector::new();
    /// let mut output = Cursor::new(Vec::new());
    ///
    /// injector.inject(&mut output, "ls -la").unwrap();
    /// assert_eq!(output.get_ref(), b"ls -la");
    /// ```
    pub fn inject(&self, pty_writer: &mut impl Write, selection: &str) -> Result<()> {
        self.inject_bytes(pty_writer, selection.as_bytes(), false)
    }

    /// Injects the selection into the PTY and appends a newline to execute it.
    ///
    /// This method is similar to `inject()` but appends a newline character
    /// after the content, which typically executes the command in shell contexts.
    ///
    /// # Arguments
    ///
    /// * `pty_writer` - A writer connected to the PTY master.
    /// * `selection` - The text to inject and execute.
    ///
    /// # Returns
    ///
    /// `Ok(())` if the injection was successful, or an I/O error if writing failed.
    ///
    /// # Example
    ///
    /// ```
    /// use std::io::Cursor;
    /// use clai_wrap::selection_inject::SelectionInjector;
    ///
    /// let injector = SelectionInjector::new();
    /// let mut output = Cursor::new(Vec::new());
    ///
    /// injector.inject_with_execute(&mut output, "ls -la").unwrap();
    /// assert_eq!(output.get_ref(), b"ls -la\n");
    /// ```
    pub fn inject_with_execute(&self, pty_writer: &mut impl Write, selection: &str) -> Result<()> {
        self.inject_bytes(pty_writer, selection.as_bytes(), true)
    }

    /// Internal method to inject bytes with optional execute mode.
    fn inject_bytes(
        &self,
        pty_writer: &mut impl Write,
        content: &[u8],
        execute: bool,
    ) -> Result<()> {
        if self.use_bracketed_paste {
            // Write bracketed paste start sequence
            pty_writer.write_all(PASTE_START)?;

            // Write the content
            pty_writer.write_all(content)?;

            // Write bracketed paste end sequence
            pty_writer.write_all(PASTE_END)?;
        } else {
            // Write raw bytes
            pty_writer.write_all(content)?;
        }

        // Append newline if execute mode is enabled
        if execute {
            pty_writer.write_all(NEWLINE)?;
        }

        // Ensure all data is flushed to the PTY
        pty_writer.flush()?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn test_new_injector() {
        let injector = SelectionInjector::new();
        assert!(!injector.is_bracketed_paste_enabled());
    }

    #[test]
    fn test_default_injector() {
        let injector = SelectionInjector::default();
        assert!(!injector.is_bracketed_paste_enabled());
    }

    #[test]
    fn test_set_bracketed_paste() {
        let mut injector = SelectionInjector::new();

        // Initially disabled
        assert!(!injector.is_bracketed_paste_enabled());

        // Enable
        injector.set_bracketed_paste(true);
        assert!(injector.is_bracketed_paste_enabled());

        // Disable again
        injector.set_bracketed_paste(false);
        assert!(!injector.is_bracketed_paste_enabled());
    }

    #[test]
    fn test_inject_without_bracketed_paste() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        injector.inject(&mut output, "echo hello").unwrap();

        assert_eq!(output.get_ref(), b"echo hello");
    }

    #[test]
    fn test_inject_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());
        injector.inject(&mut output, "echo hello").unwrap();

        let expected = b"\x1b[200~echo hello\x1b[201~";
        assert_eq!(output.get_ref(), expected);
    }

    #[test]
    fn test_inject_with_execute_without_bracketed_paste() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        injector.inject_with_execute(&mut output, "ls -la").unwrap();

        assert_eq!(output.get_ref(), b"ls -la\n");
    }

    #[test]
    fn test_inject_with_execute_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());
        injector.inject_with_execute(&mut output, "ls -la").unwrap();

        let expected = b"\x1b[200~ls -la\x1b[201~\n";
        assert_eq!(output.get_ref(), expected);
    }

    #[test]
    fn test_inject_empty_selection() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        injector.inject(&mut output, "").unwrap();

        assert!(output.get_ref().is_empty());
    }

    #[test]
    fn test_inject_empty_selection_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());
        injector.inject(&mut output, "").unwrap();

        // Even empty content should be wrapped
        let expected = b"\x1b[200~\x1b[201~";
        assert_eq!(output.get_ref(), expected);
    }

    #[test]
    fn test_inject_empty_selection_with_execute() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        injector.inject_with_execute(&mut output, "").unwrap();

        // Empty content with execute should just be a newline
        assert_eq!(output.get_ref(), b"\n");
    }

    #[test]
    fn test_inject_special_characters() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        // Command with special shell characters
        let selection = r#"echo "hello $USER" | grep -v 'test'"#;
        injector.inject(&mut output, selection).unwrap();

        assert_eq!(output.get_ref(), selection.as_bytes());
    }

    #[test]
    fn test_inject_special_characters_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());

        // Command with special shell characters
        let selection = r#"echo "hello $USER" | grep -v 'test'"#;
        injector.inject(&mut output, selection).unwrap();

        let mut expected = Vec::new();
        expected.extend_from_slice(PASTE_START);
        expected.extend_from_slice(selection.as_bytes());
        expected.extend_from_slice(PASTE_END);

        assert_eq!(output.get_ref(), &expected);
    }

    #[test]
    fn test_inject_utf8_content() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        // UTF-8 content with various characters
        let selection = "echo '世界 🌍 émojis'";
        injector.inject(&mut output, selection).unwrap();

        assert_eq!(output.get_ref(), selection.as_bytes());
    }

    #[test]
    fn test_inject_utf8_content_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());

        // UTF-8 content
        let selection = "echo '世界 🌍 émojis'";
        injector.inject(&mut output, selection).unwrap();

        let mut expected = Vec::new();
        expected.extend_from_slice(PASTE_START);
        expected.extend_from_slice(selection.as_bytes());
        expected.extend_from_slice(PASTE_END);

        assert_eq!(output.get_ref(), &expected);

        // Verify the UTF-8 content is preserved
        let inner_start = PASTE_START.len();
        let inner_end = output.get_ref().len() - PASTE_END.len();
        let inner = &output.get_ref()[inner_start..inner_end];
        assert_eq!(std::str::from_utf8(inner).unwrap(), selection);
    }

    #[test]
    fn test_inject_multiline_content() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        let selection = "line1\nline2\nline3";
        injector.inject(&mut output, selection).unwrap();

        assert_eq!(output.get_ref(), selection.as_bytes());
    }

    #[test]
    fn test_inject_multiline_content_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());

        let selection = "line1\nline2\nline3";
        injector.inject(&mut output, selection).unwrap();

        let mut expected = Vec::new();
        expected.extend_from_slice(PASTE_START);
        expected.extend_from_slice(selection.as_bytes());
        expected.extend_from_slice(PASTE_END);

        assert_eq!(output.get_ref(), &expected);
    }

    #[test]
    fn test_inject_escape_sequences_in_content() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());

        // Content that contains escape sequences
        let selection = "echo \x1b[31mred\x1b[0m";
        injector.inject(&mut output, selection).unwrap();

        let mut expected = Vec::new();
        expected.extend_from_slice(PASTE_START);
        expected.extend_from_slice(selection.as_bytes());
        expected.extend_from_slice(PASTE_END);

        assert_eq!(output.get_ref(), &expected);
    }

    #[test]
    fn test_sync_with_tracker_disabled() {
        let tracker = BracketedPasteTracker::new();
        let mut injector = SelectionInjector::new();

        // Tracker is disabled by default
        injector.sync_with_tracker(&tracker);
        assert!(!injector.is_bracketed_paste_enabled());
    }

    #[test]
    fn test_sync_with_tracker_enabled() {
        let mut tracker = BracketedPasteTracker::new();
        let mut injector = SelectionInjector::new();

        // Enable bracketed paste in tracker
        tracker.update_from_output(b"\x1b[?2004h");
        assert!(tracker.is_enabled());

        // Sync and verify
        injector.sync_with_tracker(&tracker);
        assert!(injector.is_bracketed_paste_enabled());
    }

    #[test]
    fn test_sync_with_tracker_toggle() {
        let mut tracker = BracketedPasteTracker::new();
        let mut injector = SelectionInjector::new();

        // Enable
        tracker.update_from_output(b"\x1b[?2004h");
        injector.sync_with_tracker(&tracker);
        assert!(injector.is_bracketed_paste_enabled());

        // Disable
        tracker.update_from_output(b"\x1b[?2004l");
        injector.sync_with_tracker(&tracker);
        assert!(!injector.is_bracketed_paste_enabled());

        // Re-enable
        tracker.update_from_output(b"\x1b[?2004h");
        injector.sync_with_tracker(&tracker);
        assert!(injector.is_bracketed_paste_enabled());
    }

    #[test]
    fn test_inject_long_content() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        // Generate a long command
        let selection = "echo ".to_owned() + &"a".repeat(10000);
        injector.inject(&mut output, &selection).unwrap();

        assert_eq!(output.get_ref().len(), selection.len());
        assert_eq!(output.get_ref(), selection.as_bytes());
    }

    #[test]
    fn test_inject_long_content_with_bracketed_paste() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());

        // Generate a long command
        let selection = "echo ".to_owned() + &"a".repeat(10000);
        injector.inject(&mut output, &selection).unwrap();

        let expected_len = PASTE_START.len() + selection.len() + PASTE_END.len();
        assert_eq!(output.get_ref().len(), expected_len);

        // Verify structure
        assert!(output.get_ref().starts_with(PASTE_START));
        assert!(output.get_ref().ends_with(PASTE_END));
    }

    #[test]
    fn test_inject_tabs_and_special_whitespace() {
        let injector = SelectionInjector::new();
        let mut output = Cursor::new(Vec::new());

        let selection = "command\twith\ttabs\r\nand\r\ncarriage returns";
        injector.inject(&mut output, selection).unwrap();

        assert_eq!(output.get_ref(), selection.as_bytes());
    }

    #[test]
    fn test_inject_with_execute_preserves_bracketed_paste_order() {
        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut output = Cursor::new(Vec::new());
        injector.inject_with_execute(&mut output, "test").unwrap();

        // The newline should come AFTER the bracketed paste end sequence
        let result = output.get_ref();

        // Find positions
        let paste_start_pos = result
            .windows(PASTE_START.len())
            .position(|w| w == PASTE_START);
        let paste_end_pos = result.windows(PASTE_END.len()).position(|w| w == PASTE_END);
        let newline_pos = result.iter().position(|&b| b == b'\n');

        assert!(paste_start_pos.is_some());
        assert!(paste_end_pos.is_some());
        assert!(newline_pos.is_some());

        // Verify order: start < end < newline
        assert!(paste_start_pos.unwrap() < paste_end_pos.unwrap());
        assert!(paste_end_pos.unwrap() < newline_pos.unwrap());
    }

    /// Test that simulates a write error.
    #[test]
    fn test_inject_write_error() {
        use std::io::{Error, ErrorKind};

        struct FailingWriter;

        impl Write for FailingWriter {
            fn write(&mut self, _buf: &[u8]) -> Result<usize> {
                Err(Error::new(ErrorKind::BrokenPipe, "write failed"))
            }

            fn flush(&mut self) -> Result<()> {
                Ok(())
            }
        }

        let injector = SelectionInjector::new();
        let mut writer = FailingWriter;

        let result = injector.inject(&mut writer, "test");
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().kind(), ErrorKind::BrokenPipe);
    }

    #[test]
    fn test_inject_with_bracketed_paste_write_error() {
        use std::io::{Error, ErrorKind};

        struct FailingWriter {
            write_count: usize,
        }

        impl Write for FailingWriter {
            fn write(&mut self, buf: &[u8]) -> Result<usize> {
                self.write_count += 1;
                // Fail on the second write (the content)
                if self.write_count > 1 {
                    Err(Error::new(ErrorKind::BrokenPipe, "write failed"))
                } else {
                    Ok(buf.len())
                }
            }

            fn flush(&mut self) -> Result<()> {
                Ok(())
            }
        }

        let mut injector = SelectionInjector::new();
        injector.set_bracketed_paste(true);

        let mut writer = FailingWriter { write_count: 0 };

        let result = injector.inject(&mut writer, "test");
        assert!(result.is_err());
    }
}
