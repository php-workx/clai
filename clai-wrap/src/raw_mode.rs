//! Terminal raw mode controller for clai-wrap.
//!
//! This module provides RAII-based terminal raw mode management that ensures
//! the terminal is restored to its original state on exit, even during panics
//! or signal handling.
//!
//! # Example
//!
//! ```no_run
//! use clai_wrap::raw_mode::enter_raw_mode;
//!
//! // Enter raw mode and get a guard
//! let guard = enter_raw_mode().expect("Failed to enter raw mode");
//!
//! // Terminal is now in raw mode...
//! // When guard is dropped (goes out of scope), terminal is restored
//! ```

use thiserror::Error;

/// Errors that can occur during raw mode operations.
#[derive(Debug, Error)]
pub enum RawModeError {
    /// stdin is not a TTY
    #[error("stdin is not a TTY")]
    StdinNotTty,

    /// stdout is not a TTY
    #[error("stdout is not a TTY")]
    StdoutNotTty,

    /// Failed to get terminal attributes
    #[cfg(unix)]
    #[error("failed to get terminal attributes: {0}")]
    GetAttrFailed(std::io::Error),

    /// Failed to set terminal attributes
    #[cfg(unix)]
    #[error("failed to set terminal attributes: {0}")]
    SetAttrFailed(std::io::Error),

    /// Platform not supported
    #[cfg(not(unix))]
    #[error("raw mode not supported on this platform")]
    UnsupportedPlatform,
}

/// Result type for raw mode operations.
pub type Result<T> = std::result::Result<T, RawModeError>;

/// Information about which streams are TTYs.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct TtyStatus {
    /// Whether stdin is a TTY.
    pub stdin: bool,
    /// Whether stdout is a TTY.
    pub stdout: bool,
    /// Whether stderr is a TTY.
    pub stderr: bool,
}

impl TtyStatus {
    /// Check if all standard streams are TTYs.
    #[must_use]
    pub const fn all_tty(&self) -> bool {
        self.stdin && self.stdout && self.stderr
    }

    /// Check if any stream is a TTY.
    #[must_use]
    pub const fn any_tty(&self) -> bool {
        self.stdin || self.stdout || self.stderr
    }

    /// Check if the minimum requirements for the wrapper are met.
    ///
    /// According to the spec:
    /// - stdin must be TTY for hotkey detection
    /// - stdout must be TTY for picker UI
    #[must_use]
    pub const fn meets_minimum_requirements(&self) -> bool {
        self.stdin && self.stdout
    }
}

/// Detect which standard streams are TTYs.
#[must_use]
pub fn detect_tty() -> TtyStatus {
    #[cfg(unix)]
    {
        TtyStatus {
            stdin: unix::is_tty(libc::STDIN_FILENO),
            stdout: unix::is_tty(libc::STDOUT_FILENO),
            stderr: unix::is_tty(libc::STDERR_FILENO),
        }
    }

    #[cfg(not(unix))]
    {
        // On non-Unix platforms, stub returns false for all
        TtyStatus {
            stdin: false,
            stdout: false,
            stderr: false,
        }
    }
}

/// RAII guard that restores terminal settings on drop.
///
/// This guard holds the original terminal settings and ensures they are
/// restored when the guard is dropped, whether through normal scope exit,
/// early return, or panic unwinding.
#[cfg(unix)]
pub struct RawModeGuard {
    /// Original terminal settings to restore.
    original_termios: libc::termios,
    /// File descriptor for the terminal (stdin).
    fd: std::os::unix::io::RawFd,
}

#[cfg(unix)]
impl RawModeGuard {
    /// Create a new raw mode guard with the given original settings.
    const fn new(original_termios: libc::termios, fd: std::os::unix::io::RawFd) -> Self {
        Self {
            original_termios,
            fd,
        }
    }

    /// Get a reference to the original termios settings.
    #[must_use]
    pub const fn original_termios(&self) -> &libc::termios {
        &self.original_termios
    }

    /// Manually restore the terminal to its original settings.
    ///
    /// This is automatically called on drop, but can be called manually
    /// if needed. After calling this, the guard is still valid and will
    /// attempt to restore again on drop (which is harmless).
    pub fn restore(&self) -> Result<()> {
        unix::restore_termios(self.fd, &self.original_termios)
    }
}

#[cfg(unix)]
impl Drop for RawModeGuard {
    fn drop(&mut self) {
        // Best-effort restoration; we can't propagate errors from Drop
        let _ = self.restore();
    }
}

/// Stub guard for non-Unix platforms.
#[cfg(not(unix))]
pub struct RawModeGuard {
    _private: (),
}

#[cfg(not(unix))]
impl RawModeGuard {
    /// Manually restore the terminal (no-op on non-Unix).
    pub fn restore(&self) -> Result<()> {
        Ok(())
    }
}

#[cfg(not(unix))]
impl Drop for RawModeGuard {
    fn drop(&mut self) {
        // No-op on non-Unix platforms
    }
}

/// Enter raw mode on the terminal.
///
/// This function:
/// 1. Checks that stdin is a TTY (required for raw mode)
/// 2. Saves the current terminal attributes
/// 3. Sets raw mode (disables canonical mode, echo, and signal generation)
/// 4. Returns a guard that restores settings on drop
///
/// # Errors
///
/// Returns an error if:
/// - stdin is not a TTY
/// - Terminal attributes cannot be retrieved or set
///
/// # Example
///
/// ```no_run
/// use clai_wrap::raw_mode::enter_raw_mode;
///
/// let guard = enter_raw_mode()?;
/// // Terminal is now in raw mode
/// // guard will restore terminal on drop
/// # Ok::<(), clai_wrap::raw_mode::RawModeError>(())
/// ```
pub fn enter_raw_mode() -> Result<RawModeGuard> {
    #[cfg(unix)]
    {
        unix::enter_raw_mode_impl()
    }

    #[cfg(not(unix))]
    {
        Err(RawModeError::UnsupportedPlatform)
    }
}

/// Enter raw mode with custom requirements checking.
///
/// This is like `enter_raw_mode` but allows specifying whether stdout
/// must also be a TTY. This is useful for cases where you want to
/// enter raw mode but don't need the picker UI.
///
/// # Arguments
///
/// * `require_stdout_tty` - If true, also checks that stdout is a TTY
///
/// # Errors
///
/// Returns an error if:
/// - stdin is not a TTY
/// - stdout is not a TTY (if `require_stdout_tty` is true)
/// - Terminal attributes cannot be retrieved or set
pub fn enter_raw_mode_with_requirements(require_stdout_tty: bool) -> Result<RawModeGuard> {
    let tty_status = detect_tty();

    if !tty_status.stdin {
        return Err(RawModeError::StdinNotTty);
    }

    if require_stdout_tty && !tty_status.stdout {
        return Err(RawModeError::StdoutNotTty);
    }

    #[cfg(unix)]
    {
        unix::enter_raw_mode_impl()
    }

    #[cfg(not(unix))]
    {
        Err(RawModeError::UnsupportedPlatform)
    }
}

#[cfg(unix)]
mod unix {
    use super::{RawModeError, RawModeGuard, Result};
    use std::os::unix::io::RawFd;

    /// Check if a file descriptor is a TTY.
    pub fn is_tty(fd: RawFd) -> bool {
        // SAFETY: isatty is safe to call with any file descriptor
        unsafe { libc::isatty(fd) == 1 }
    }

    /// Get the current terminal attributes.
    fn get_termios(fd: RawFd) -> Result<libc::termios> {
        // SAFETY: We're passing a valid pointer to an uninitialized termios struct
        // which tcgetattr will fill in. Using MaybeUninit ensures we don't read
        // uninitialized memory.
        let mut termios = std::mem::MaybeUninit::<libc::termios>::uninit();
        let result = unsafe { libc::tcgetattr(fd, termios.as_mut_ptr()) };

        if result == -1 {
            return Err(RawModeError::GetAttrFailed(std::io::Error::last_os_error()));
        }

        // SAFETY: tcgetattr succeeded, so termios is now initialized
        Ok(unsafe { termios.assume_init() })
    }

    /// Set terminal attributes.
    fn set_termios(fd: RawFd, termios: &libc::termios) -> Result<()> {
        // SAFETY: We're passing a valid pointer to an initialized termios struct
        let result = unsafe { libc::tcsetattr(fd, libc::TCSANOW, termios) };

        if result == -1 {
            return Err(RawModeError::SetAttrFailed(std::io::Error::last_os_error()));
        }

        Ok(())
    }

    /// Restore terminal attributes.
    pub fn restore_termios(fd: RawFd, termios: &libc::termios) -> Result<()> {
        // Use TCSAFLUSH to discard any pending input/output
        // SAFETY: We're passing a valid pointer to an initialized termios struct
        let result = unsafe { libc::tcsetattr(fd, libc::TCSAFLUSH, termios) };

        if result == -1 {
            return Err(RawModeError::SetAttrFailed(std::io::Error::last_os_error()));
        }

        Ok(())
    }

    /// Apply raw mode settings to a termios struct.
    ///
    /// This is equivalent to `cfmakeraw()` but implemented manually for clarity
    /// and control over exactly which flags are set.
    const fn make_raw(termios: &mut libc::termios) {
        // Input modes: disable break signal, CR to NL, parity check, strip 8th bit,
        // and software flow control
        termios.c_iflag &= !(libc::IGNBRK
            | libc::BRKINT
            | libc::PARMRK
            | libc::ISTRIP
            | libc::INLCR
            | libc::IGNCR
            | libc::ICRNL
            | libc::IXON);

        // Output modes: disable post-processing
        termios.c_oflag &= !libc::OPOST;

        // Local modes: disable echo, canonical mode, extended input processing,
        // and signal generation
        termios.c_lflag &= !(libc::ECHO | libc::ECHONL | libc::ICANON | libc::ISIG | libc::IEXTEN);

        // Control modes: disable parity, set 8-bit characters
        termios.c_cflag &= !(libc::CSIZE | libc::PARENB);
        termios.c_cflag |= libc::CS8;

        // Special characters: set minimum bytes and timeout for read
        // VMIN = 1: read returns when at least 1 byte is available
        // VTIME = 0: no timeout
        termios.c_cc[libc::VMIN] = 1;
        termios.c_cc[libc::VTIME] = 0;
    }

    /// Implementation of raw mode entry for Unix.
    pub fn enter_raw_mode_impl() -> Result<RawModeGuard> {
        let fd = libc::STDIN_FILENO;

        // Check that stdin is a TTY
        if !is_tty(fd) {
            return Err(RawModeError::StdinNotTty);
        }

        // Get current terminal settings
        let original_termios = get_termios(fd)?;

        // Create raw mode settings
        let mut raw_termios = original_termios;
        make_raw(&mut raw_termios);

        // Apply raw mode settings
        set_termios(fd, &raw_termios)?;

        Ok(RawModeGuard::new(original_termios, fd))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tty_status_methods() {
        let all_tty = TtyStatus {
            stdin: true,
            stdout: true,
            stderr: true,
        };
        assert!(all_tty.all_tty());
        assert!(all_tty.any_tty());
        assert!(all_tty.meets_minimum_requirements());

        let none_tty = TtyStatus {
            stdin: false,
            stdout: false,
            stderr: false,
        };
        assert!(!none_tty.all_tty());
        assert!(!none_tty.any_tty());
        assert!(!none_tty.meets_minimum_requirements());

        let stdin_only = TtyStatus {
            stdin: true,
            stdout: false,
            stderr: false,
        };
        assert!(!stdin_only.all_tty());
        assert!(stdin_only.any_tty());
        assert!(!stdin_only.meets_minimum_requirements());

        let stdin_stdout = TtyStatus {
            stdin: true,
            stdout: true,
            stderr: false,
        };
        assert!(!stdin_stdout.all_tty());
        assert!(stdin_stdout.any_tty());
        assert!(stdin_stdout.meets_minimum_requirements());
    }

    #[test]
    fn test_detect_tty_in_test_environment() {
        // In test environment, we're typically running with pipes
        let status = detect_tty();

        // We can't assert specific values since it depends on how tests are run,
        // but we can verify the function doesn't panic and returns valid values
        let _ = status.stdin;
        let _ = status.stdout;
        let _ = status.stderr;
    }

    #[test]
    fn test_raw_mode_error_display() {
        let error = RawModeError::StdinNotTty;
        assert_eq!(error.to_string(), "stdin is not a TTY");

        let error = RawModeError::StdoutNotTty;
        assert_eq!(error.to_string(), "stdout is not a TTY");
    }

    #[cfg(unix)]
    mod unix_tests {
        use super::*;
        use std::os::unix::io::RawFd;

        struct PtyPair {
            master: RawFd,
            slave: RawFd,
        }

        impl PtyPair {
            fn new() -> Option<Self> {
                let mut master: libc::c_int = -1;
                let mut slave: libc::c_int = -1;

                // SAFETY: openpty() is called with valid pointers for master/slave fds.
                let result = unsafe {
                    libc::openpty(
                        &raw mut master,
                        &raw mut slave,
                        std::ptr::null_mut(),
                        std::ptr::null_mut(),
                        std::ptr::null_mut(),
                    )
                };

                if result == -1 {
                    return None;
                }

                Some(Self { master, slave })
            }
        }

        impl Drop for PtyPair {
            fn drop(&mut self) {
                // SAFETY: closing valid file descriptors is safe.
                unsafe {
                    libc::close(self.master);
                    libc::close(self.slave);
                }
            }
        }

        fn get_termios(fd: RawFd) -> libc::termios {
            let mut termios = std::mem::MaybeUninit::<libc::termios>::uninit();
            // SAFETY: tcgetattr() is called with a valid fd and output pointer.
            let result = unsafe { libc::tcgetattr(fd, termios.as_mut_ptr()) };
            assert_eq!(result, 0, "Failed to get termios for fd {fd}");
            // SAFETY: tcgetattr succeeded and initialized termios.
            unsafe { termios.assume_init() }
        }

        fn set_termios_now(fd: RawFd, termios: &libc::termios) {
            // SAFETY: tcsetattr() is called with a valid fd and termios pointer.
            let result = unsafe { libc::tcsetattr(fd, libc::TCSANOW, termios) };
            assert_eq!(result, 0, "Failed to set termios for fd {fd}");
        }

        #[test]
        fn test_is_tty_with_pipe() {
            // Create a pipe - neither end should be a TTY
            let mut fds = [0i32; 2];
            // SAFETY: pipe() is safe to call with a valid pointer to an array of 2 ints
            let result = unsafe { libc::pipe(fds.as_mut_ptr()) };
            assert_eq!(result, 0, "Failed to create pipe");

            let read_fd = fds[0];
            let write_fd = fds[1];

            // Neither end of a pipe is a TTY
            assert!(!unix::is_tty(read_fd));
            assert!(!unix::is_tty(write_fd));

            // Clean up
            // SAFETY: close() is safe to call with valid file descriptors
            unsafe {
                libc::close(read_fd);
                libc::close(write_fd);
            }
        }

        #[test]
        fn test_enter_raw_mode_with_pipe() {
            // When stdin is a pipe (like in tests), raw mode should fail
            // We can't easily test this in the general case since stdin might
            // actually be a TTY depending on how tests are run

            // Test with explicit non-TTY requirement
            let status = detect_tty();
            if !status.stdin {
                let result = enter_raw_mode();
                assert!(matches!(result, Err(RawModeError::StdinNotTty)));
            }
        }

        #[test]
        fn test_enter_raw_mode_with_requirements_stdout() {
            let status = detect_tty();

            // If stdout is not a TTY, requiring it should fail
            if !status.stdout {
                let result = enter_raw_mode_with_requirements(true);
                // Could be either StdinNotTty or StdoutNotTty depending on stdin
                assert!(result.is_err());
            }

            // Not requiring stdout should only check stdin
            if status.stdin && !status.stdout {
                let result = enter_raw_mode_with_requirements(false);
                // Should succeed since we only require stdin
                assert!(result.is_ok());
                // Guard will restore on drop
            }
        }

        #[test]
        fn test_raw_mode_guard_restore_with_pseudo_tty() {
            let Some(pty) = PtyPair::new() else {
                eprintln!("Skipping test: failed to allocate pseudo TTY");
                return;
            };

            let original = get_termios(pty.slave);

            // Simulate raw-like changes on the PTY slave before restoration.
            let mut modified = original;
            modified.c_lflag &= !(libc::ICANON | libc::ECHO);
            set_termios_now(pty.slave, &modified);

            let current = get_termios(pty.slave);
            assert_eq!(current.c_lflag & libc::ICANON, 0);
            assert_eq!(current.c_lflag & libc::ECHO, 0);

            let guard = RawModeGuard::new(original, pty.slave);
            guard.restore().expect("manual restore should succeed");
            drop(guard);

            let restored = get_termios(pty.slave);
            assert_eq!(
                restored.c_lflag & libc::ICANON,
                original.c_lflag & libc::ICANON
            );
            assert_eq!(restored.c_lflag & libc::ECHO, original.c_lflag & libc::ECHO);
        }

        #[test]
        fn test_drop_restores_settings_on_pseudo_tty() {
            let Some(pty) = PtyPair::new() else {
                eprintln!("Skipping test: failed to allocate pseudo TTY");
                return;
            };

            let original = get_termios(pty.slave);

            // Simulate raw-like mode prior to guard drop restoration.
            let mut modified = original;
            modified.c_lflag &= !(libc::ICANON | libc::ECHO);
            set_termios_now(pty.slave, &modified);

            {
                let _guard = RawModeGuard::new(original, pty.slave);
                // Guard drops at scope end and must restore original settings.
            }

            let restored = get_termios(pty.slave);
            assert_eq!(
                restored.c_lflag & libc::ICANON,
                original.c_lflag & libc::ICANON
            );
            assert_eq!(restored.c_lflag & libc::ECHO, original.c_lflag & libc::ECHO);
        }

        #[test]
        fn test_drop_restores_settings_on_unwind_pseudo_tty() {
            let Some(pty) = PtyPair::new() else {
                eprintln!("Skipping test: failed to allocate pseudo TTY");
                return;
            };

            let original = get_termios(pty.slave);

            let mut modified = original;
            modified.c_lflag &= !(libc::ICANON | libc::ECHO);
            set_termios_now(pty.slave, &modified);

            let panic_result = std::panic::catch_unwind(|| {
                let _guard = RawModeGuard::new(original, pty.slave);
                panic!("simulated abrupt unwind");
            });
            assert!(panic_result.is_err(), "panic path should trigger unwind");

            let restored = get_termios(pty.slave);
            assert_eq!(
                restored.c_lflag & libc::ICANON,
                original.c_lflag & libc::ICANON
            );
            assert_eq!(restored.c_lflag & libc::ECHO, original.c_lflag & libc::ECHO);
        }
    }

    #[cfg(not(unix))]
    mod non_unix_tests {
        use super::*;

        #[test]
        fn test_non_unix_returns_unsupported() {
            let result = enter_raw_mode();
            assert!(matches!(result, Err(RawModeError::UnsupportedPlatform)));
        }

        #[test]
        fn test_non_unix_tty_detection() {
            let status = detect_tty();
            // On non-Unix, all should return false (stub implementation)
            assert!(!status.stdin);
            assert!(!status.stdout);
            assert!(!status.stderr);
        }
    }
}
