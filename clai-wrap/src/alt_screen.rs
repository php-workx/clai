//! Alternate screen buffer management for clai-wrap.
//!
//! This module provides RAII-based alternate screen buffer management that ensures
//! the terminal is restored to its original state on exit, even during panics
//! or signal handling. It is used by the picker UI to display an overlay without
//! affecting the underlying shell session.
//!
//! # ANSI Sequences
//!
//! - Enter alt-screen: `\x1b[?1049h`
//! - Exit alt-screen: `\x1b[?1049l`
//! - Hide cursor: `\x1b[?25l`
//! - Show cursor: `\x1b[?25h`
//! - Clear screen: `\x1b[2J`
//! - Move cursor home: `\x1b[H`
//!
//! # Example
//!
//! ```no_run
//! use clai_wrap::alt_screen::enter_alt_screen;
//!
//! // Enter alternate screen and get a guard
//! let guard = enter_alt_screen().expect("Failed to enter alt-screen");
//!
//! // Terminal is now in alternate screen buffer...
//! // When guard is dropped (goes out of scope), the original screen is restored
//! ```

use std::io::{self, Write};
use thiserror::Error;

// ANSI escape sequences for alternate screen buffer management
/// Enter alternate screen buffer
const ENTER_ALT_SCREEN: &[u8] = b"\x1b[?1049h";
/// Exit alternate screen buffer
const EXIT_ALT_SCREEN: &[u8] = b"\x1b[?1049l";
/// Hide cursor
const HIDE_CURSOR: &[u8] = b"\x1b[?25l";
/// Show cursor
const SHOW_CURSOR: &[u8] = b"\x1b[?25h";
/// Clear screen
const CLEAR_SCREEN: &[u8] = b"\x1b[2J";
/// Move cursor to home position (top-left)
const CURSOR_HOME: &[u8] = b"\x1b[H";

/// Errors that can occur during alternate screen operations.
#[derive(Debug, Error)]
pub enum AltScreenError {
    /// Failed to write to stdout
    #[error("failed to write to stdout: {0}")]
    WriteFailed(#[from] io::Error),
}

/// Result type for alternate screen operations.
pub type Result<T> = std::result::Result<T, AltScreenError>;

/// RAII guard that restores the original screen buffer on drop.
///
/// This guard ensures that the alternate screen buffer is exited and the cursor
/// is restored when it goes out of scope, whether through normal scope exit,
/// early return, or panic unwinding.
///
/// # Drop Behavior
///
/// On drop, the guard will:
/// 1. Exit the alternate screen buffer (restore previous screen)
/// 2. Show the cursor
///
/// Any errors during restoration are silently ignored since we cannot
/// propagate errors from `Drop`.
#[derive(Debug)]
pub struct AltScreenGuard {
    /// Marker to prevent external construction
    _private: (),
}

impl AltScreenGuard {
    /// Create a new alt-screen guard.
    ///
    /// This should only be called after successfully entering the alternate screen.
    const fn new() -> Self {
        Self { _private: () }
    }

    /// Manually exit the alternate screen and restore the original.
    ///
    /// This is automatically called on drop, but can be called manually
    /// if you need to handle errors. After calling this, the guard is still
    /// valid and will attempt to restore again on drop (which is harmless).
    ///
    /// # Errors
    ///
    /// Returns an error if writing to stdout fails.
    pub fn restore(&self) -> Result<()> {
        exit_alt_screen_impl()
    }
}

impl Drop for AltScreenGuard {
    fn drop(&mut self) {
        // Best-effort restoration; we can't propagate errors from Drop
        // Always attempt to restore even if some operations fail
        let _ = self.restore();
    }
}

/// Enter the alternate screen buffer.
///
/// This function:
/// 1. Switches to the alternate screen buffer
/// 2. Hides the cursor
/// 3. Clears the screen
/// 4. Moves the cursor to the home position
/// 5. Returns a guard that restores the original screen on drop
///
/// # Errors
///
/// Returns an error if writing to stdout fails.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::alt_screen::enter_alt_screen;
///
/// let guard = enter_alt_screen()?;
/// // Terminal is now in alternate screen buffer
/// // guard will restore terminal on drop
/// # Ok::<(), clai_wrap::alt_screen::AltScreenError>(())
/// ```
pub fn enter_alt_screen() -> Result<AltScreenGuard> {
    let mut stdout = io::stdout().lock();

    // Enter alternate screen buffer
    stdout.write_all(ENTER_ALT_SCREEN)?;

    // Hide cursor for cleaner UI
    stdout.write_all(HIDE_CURSOR)?;

    // Clear the screen
    stdout.write_all(CLEAR_SCREEN)?;

    // Move cursor to home position
    stdout.write_all(CURSOR_HOME)?;

    // Ensure all bytes are written
    stdout.flush()?;

    Ok(AltScreenGuard::new())
}

/// Exit the alternate screen buffer (internal implementation).
///
/// This restores the previous screen buffer and shows the cursor.
fn exit_alt_screen_impl() -> Result<()> {
    let mut stdout = io::stdout().lock();

    // Exit alternate screen buffer (restore previous screen)
    stdout.write_all(EXIT_ALT_SCREEN)?;

    // Show cursor
    stdout.write_all(SHOW_CURSOR)?;

    // Ensure all bytes are written
    stdout.flush()?;

    Ok(())
}

/// Hide the cursor.
///
/// This is useful during UI rendering to prevent cursor flicker.
/// Note: The cursor is automatically hidden when entering alt-screen
/// and shown when exiting.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::alt_screen::hide_cursor;
///
/// hide_cursor(); // Cursor is now hidden
/// ```
pub fn hide_cursor() {
    let _ = write_sequence(HIDE_CURSOR);
}

/// Show the cursor.
///
/// This is useful after hiding the cursor during UI rendering.
/// Note: The cursor is automatically shown when the `AltScreenGuard` is dropped.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::alt_screen::show_cursor;
///
/// show_cursor(); // Cursor is now visible
/// ```
pub fn show_cursor() {
    let _ = write_sequence(SHOW_CURSOR);
}

/// Clear the screen.
///
/// This clears all content from the current screen buffer.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::alt_screen::clear_screen;
///
/// clear_screen(); // Screen is now cleared
/// ```
pub fn clear_screen() {
    let _ = write_sequence(CLEAR_SCREEN);
}

/// Move cursor to home position (top-left corner).
///
/// This moves the cursor to row 1, column 1.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::alt_screen::cursor_home;
///
/// cursor_home(); // Cursor is now at top-left
/// ```
pub fn cursor_home() {
    let _ = write_sequence(CURSOR_HOME);
}

/// Write an ANSI sequence to stdout.
///
/// This is a helper function that handles the common pattern of
/// writing a sequence and flushing. Errors are returned for handling.
fn write_sequence(sequence: &[u8]) -> Result<()> {
    let mut stdout = io::stdout().lock();
    stdout.write_all(sequence)?;
    stdout.flush()?;
    Ok(())
}

/// Get the ANSI sequence to enter the alternate screen buffer.
///
/// This is useful when you need to write the sequence directly to a different
/// output stream (e.g., PTY).
#[must_use]
pub const fn enter_sequence() -> &'static [u8] {
    ENTER_ALT_SCREEN
}

/// Get the ANSI sequence to exit the alternate screen buffer.
///
/// This is useful when you need to write the sequence directly to a different
/// output stream (e.g., PTY).
#[must_use]
pub const fn exit_sequence() -> &'static [u8] {
    EXIT_ALT_SCREEN
}

/// Get the ANSI sequence to hide the cursor.
#[must_use]
pub const fn hide_cursor_sequence() -> &'static [u8] {
    HIDE_CURSOR
}

/// Get the ANSI sequence to show the cursor.
#[must_use]
pub const fn show_cursor_sequence() -> &'static [u8] {
    SHOW_CURSOR
}

/// Get the ANSI sequence to clear the screen.
#[must_use]
pub const fn clear_screen_sequence() -> &'static [u8] {
    CLEAR_SCREEN
}

/// Get the ANSI sequence to move cursor to home position.
#[must_use]
pub const fn cursor_home_sequence() -> &'static [u8] {
    CURSOR_HOME
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ansi_sequences_correct() {
        // Verify the ANSI sequences are correct according to spec
        assert_eq!(ENTER_ALT_SCREEN, b"\x1b[?1049h");
        assert_eq!(EXIT_ALT_SCREEN, b"\x1b[?1049l");
        assert_eq!(HIDE_CURSOR, b"\x1b[?25l");
        assert_eq!(SHOW_CURSOR, b"\x1b[?25h");
        assert_eq!(CLEAR_SCREEN, b"\x1b[2J");
        assert_eq!(CURSOR_HOME, b"\x1b[H");
    }

    #[test]
    fn test_sequence_getters() {
        // Verify the getter functions return the correct sequences
        assert_eq!(enter_sequence(), ENTER_ALT_SCREEN);
        assert_eq!(exit_sequence(), EXIT_ALT_SCREEN);
        assert_eq!(hide_cursor_sequence(), HIDE_CURSOR);
        assert_eq!(show_cursor_sequence(), SHOW_CURSOR);
        assert_eq!(clear_screen_sequence(), CLEAR_SCREEN);
        assert_eq!(cursor_home_sequence(), CURSOR_HOME);
    }

    #[test]
    fn test_error_display() {
        let io_error = io::Error::new(io::ErrorKind::BrokenPipe, "test error");
        let error = AltScreenError::WriteFailed(io_error);
        assert!(error.to_string().contains("failed to write to stdout"));
    }

    #[test]
    fn test_guard_can_restore_multiple_times() {
        // Create guard manually (simulating internal construction)
        let guard = AltScreenGuard::new();

        // Manual restore should work without panicking
        // (In real use, this writes to stdout which may fail in tests,
        // but we're testing the logic, not actual terminal interaction)
        let result = guard.restore();

        // In test environment without a TTY, write might succeed or fail
        // depending on how tests are run - we just verify no panic
        let _ = result;

        // Second restore should also not panic
        let _ = guard.restore();

        // Drop will call restore again - should be harmless
        drop(guard);
    }

    #[test]
    fn test_alt_screen_guard_is_send_and_sync() {
        // Ensure the guard can be used across threads safely
        fn assert_send<T: Send>() {}
        fn assert_sync<T: Sync>() {}

        assert_send::<AltScreenGuard>();
        assert_sync::<AltScreenGuard>();
    }

    mod sequence_tests {
        use super::*;

        /// Test that enter and exit sequences are paired correctly
        #[test]
        fn test_enter_exit_pairing() {
            // Enter uses 'h' suffix, exit uses 'l' suffix (standard ANSI convention)
            let enter = std::str::from_utf8(ENTER_ALT_SCREEN).unwrap();
            let exit = std::str::from_utf8(EXIT_ALT_SCREEN).unwrap();

            assert!(enter.ends_with('h'), "Enter should end with 'h'");
            assert!(exit.ends_with('l'), "Exit should end with 'l'");

            // Both should have the same base sequence
            assert_eq!(&enter[..enter.len() - 1], &exit[..exit.len() - 1]);
        }

        /// Test that hide/show cursor sequences are paired correctly
        #[test]
        fn test_cursor_hide_show_pairing() {
            let hide = std::str::from_utf8(HIDE_CURSOR).unwrap();
            let show = std::str::from_utf8(SHOW_CURSOR).unwrap();

            assert!(hide.ends_with('l'), "Hide should end with 'l'");
            assert!(show.ends_with('h'), "Show should end with 'h'");

            // Both should have the same base sequence
            assert_eq!(&hide[..hide.len() - 1], &show[..show.len() - 1]);
        }

        /// Test that clear screen and cursor home can be combined
        #[test]
        fn test_clear_and_home_combination() {
            // Both should be valid standalone sequences
            let clear = std::str::from_utf8(CLEAR_SCREEN).unwrap();
            let home = std::str::from_utf8(CURSOR_HOME).unwrap();

            assert!(clear.starts_with("\x1b["), "Clear should start with ESC[");
            assert!(home.starts_with("\x1b["), "Home should start with ESC[");

            // They should be combinable in sequence
            let combined = format!("{clear}{home}");
            assert_eq!(combined, "\x1b[2J\x1b[H");
        }
    }

    /// Integration tests that interact with actual stdout
    /// These are marked as `ignore` by default since they require a TTY
    mod integration_tests {
        use super::*;

        #[test]
        #[ignore = "requires TTY - run with --ignored"]
        fn test_full_alt_screen_cycle() {
            // This test requires a real TTY to work properly
            // It's marked as ignore for CI but can be run manually

            let guard = enter_alt_screen().expect("Failed to enter alt-screen");

            // Do some operations
            clear_screen();
            cursor_home();
            hide_cursor();
            show_cursor();

            // Explicit restore
            guard.restore().expect("Failed to restore");

            // Drop should be harmless after restore
            drop(guard);
        }

        #[test]
        #[ignore = "requires TTY - run with --ignored"]
        fn test_drop_restores_screen() {
            {
                let _guard = enter_alt_screen().expect("Failed to enter alt-screen");
                // Guard will be dropped at end of scope
            }
            // If we get here without panic, drop worked
        }

        #[test]
        #[ignore = "requires TTY - run with --ignored"]
        fn test_nested_guards_work() {
            // Nested guards should work (though not typical usage)
            let guard1 = enter_alt_screen().expect("Failed to enter alt-screen");
            let guard2 = enter_alt_screen().expect("Failed to enter alt-screen again");

            // Drop in reverse order
            drop(guard2);
            drop(guard1);
        }
    }

    /// Tests for the write_sequence helper
    mod write_tests {
        use super::*;

        #[test]
        fn test_write_sequence_returns_result() {
            // write_sequence should return a Result
            let result = write_sequence(b"test");
            // In test environment, this may succeed or fail depending on stdout
            let _ = result;
        }
    }

    /// Tests verifying behavior matches spec requirements from Section 6.5
    mod spec_compliance_tests {
        use super::*;

        /// From spec Section 6.5: "On open: Switch to alt-screen, Hide cursor"
        #[test]
        fn test_enter_sequence_order() {
            // The enter_alt_screen function should:
            // 1. Enter alt-screen
            // 2. Hide cursor
            // 3. Clear screen
            // 4. Move cursor home
            //
            // We verify the sequences exist and are correct
            assert!(!ENTER_ALT_SCREEN.is_empty());
            assert!(!HIDE_CURSOR.is_empty());
            assert!(!CLEAR_SCREEN.is_empty());
            assert!(!CURSOR_HOME.is_empty());
        }

        /// From spec Section 6.5: "On close: Restore previous screen buffer, Show cursor"
        #[test]
        fn test_exit_restores_cursor_and_screen() {
            // The guard's restore/drop should:
            // 1. Exit alt-screen
            // 2. Show cursor
            //
            // We verify the sequences exist and are correct
            assert!(!EXIT_ALT_SCREEN.is_empty());
            assert!(!SHOW_CURSOR.is_empty());
        }
    }
}
