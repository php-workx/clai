//! Process detection module for clai-wrap.
//!
//! This module provides functionality to detect the foreground process running
//! in a PTY. This is used for the privacy gate (Section 7.1 of the spec) to
//! determine whether output capture should be paused for sensitive applications
//! like `ssh`, `vim`, `sudo`, etc.
//!
//! # Platform Support
//!
//! - **Linux**: Uses `/proc/{pid}/comm` where pid = `tcgetpgrp(master_fd)`
//! - **macOS**: Uses `proc_name()` from libproc
//!
//! # Failure Handling
//!
//! Process detection may fail due to permissions, race conditions, or platform quirks.
//! All failures are handled gracefully with appropriate fallbacks:
//!
//! | Failure | Handling |
//! |---------|----------|
//! | `/proc/{pid}/comm` unreadable | Fall back to shell name |
//! | `tcgetpgrp()` returns -1 | Assume shell is foreground process |
//! | Process name is empty | Use "unknown" |

use std::os::unix::io::RawFd;

use thiserror::Error;

/// Errors that can occur during process detection.
#[derive(Debug, Error)]
pub enum ProcessDetectError {
    /// Failed to get the foreground process group ID.
    #[error("failed to get foreground process group: {0}")]
    TcgetpgrpFailed(std::io::Error),

    /// Failed to read process name from /proc.
    #[cfg(target_os = "linux")]
    #[error("failed to read /proc/{pid}/comm: {source}")]
    ProcReadFailed {
        pid: libc::pid_t,
        source: std::io::Error,
    },

    /// Failed to get process name via libproc.
    #[cfg(target_os = "macos")]
    #[error("proc_name failed for pid {pid}")]
    ProcNameFailed { pid: libc::pid_t },

    /// The process name was empty.
    #[error("process name is empty for pid {0}")]
    EmptyProcessName(libc::pid_t),

    /// Platform not supported.
    #[cfg(not(any(target_os = "linux", target_os = "macos")))]
    #[error("process detection not supported on this platform")]
    UnsupportedPlatform,
}

/// Result type for process detection operations.
pub type Result<T> = std::result::Result<T, ProcessDetectError>;

/// Get the foreground process group ID for a PTY.
///
/// This function uses `tcgetpgrp()` to get the process group ID of the
/// foreground process group associated with the given terminal (PTY master fd).
///
/// # Arguments
///
/// * `master_fd` - The file descriptor of the PTY master.
///
/// # Returns
///
/// The process group ID of the foreground process group.
///
/// # Errors
///
/// Returns an error if `tcgetpgrp()` fails (e.g., fd is not a valid terminal).
#[cfg(unix)]
pub fn get_foreground_pgid(master_fd: RawFd) -> Result<libc::pid_t> {
    // SAFETY: tcgetpgrp is safe to call with any file descriptor.
    // It returns -1 and sets errno on error, which we handle.
    let pgid = unsafe { libc::tcgetpgrp(master_fd) };

    if pgid == -1 {
        return Err(ProcessDetectError::TcgetpgrpFailed(
            std::io::Error::last_os_error(),
        ));
    }

    Ok(pgid)
}

/// Get the process name for a given PID on Linux.
///
/// This function reads `/proc/{pid}/comm` to get the process name.
/// The comm file contains the command name (up to 15 characters) of the process.
///
/// # Arguments
///
/// * `pid` - The process ID.
///
/// # Returns
///
/// The process name as a String.
///
/// # Errors
///
/// Returns an error if the file cannot be read or the name is empty.
#[cfg(target_os = "linux")]
pub fn get_process_name(pid: libc::pid_t) -> Result<String> {
    use std::fs;

    let comm_path = format!("/proc/{pid}/comm");

    let name = fs::read_to_string(&comm_path)
        .map_err(|e| ProcessDetectError::ProcReadFailed { pid, source: e })?
        .trim()
        .to_string();

    if name.is_empty() {
        return Err(ProcessDetectError::EmptyProcessName(pid));
    }

    Ok(name)
}

/// Get the process name for a given PID on macOS.
///
/// This function uses `proc_name()` from libproc to get the process name.
///
/// # Arguments
///
/// * `pid` - The process ID.
///
/// # Returns
///
/// The process name as a String.
///
/// # Errors
///
/// Returns an error if `proc_name()` fails or the name is empty.
#[cfg(target_os = "macos")]
pub fn get_process_name(pid: libc::pid_t) -> Result<String> {
    // Buffer size for process name. MAXCOMLEN is typically 16 on macOS,
    // but we use a larger buffer to be safe.
    const PROC_NAME_BUFFER_SIZE: usize = 256;

    let mut buffer = [0u8; PROC_NAME_BUFFER_SIZE];

    // SAFETY: proc_name is a stable macOS API that takes a pid, a buffer,
    // and buffer size. It returns the length of the name on success, or 0 on error.
    let len = unsafe {
        macos::proc_name(
            pid,
            buffer.as_mut_ptr().cast::<libc::c_void>(),
            u32::try_from(buffer.len()).unwrap_or(u32::MAX),
        )
    };

    if len <= 0 {
        return Err(ProcessDetectError::ProcNameFailed { pid });
    }

    // Convert to string, handling potential invalid UTF-8
    // len is guaranteed to be > 0 here, so the cast is safe
    #[allow(clippy::cast_sign_loss)]
    let name_bytes = &buffer[..len as usize];
    let name = String::from_utf8_lossy(name_bytes).trim().to_string();

    if name.is_empty() {
        return Err(ProcessDetectError::EmptyProcessName(pid));
    }

    Ok(name)
}

/// Get the foreground process name for a PTY.
///
/// This is the main function to use for process detection. It combines
/// `get_foreground_pgid()` and `get_process_name()` to get the name of
/// the process currently in the foreground of the PTY.
///
/// # Arguments
///
/// * `master_fd` - The file descriptor of the PTY master.
///
/// # Returns
///
/// The name of the foreground process.
///
/// # Errors
///
/// Returns an error if either getting the PGID or the process name fails.
#[cfg(unix)]
pub fn get_foreground_process(master_fd: RawFd) -> Result<String> {
    let pgid = get_foreground_pgid(master_fd)?;
    get_process_name(pgid)
}

/// Get the foreground process name with fallback.
///
/// This function attempts to get the foreground process name, but returns
/// a fallback value instead of an error if detection fails.
///
/// # Arguments
///
/// * `master_fd` - The file descriptor of the PTY master.
/// * `fallback` - The fallback name to use if detection fails.
///
/// # Returns
///
/// The name of the foreground process, or the fallback if detection fails.
#[cfg(unix)]
pub fn get_foreground_process_or(master_fd: RawFd, fallback: &str) -> String {
    get_foreground_process(master_fd).unwrap_or_else(|_| fallback.to_string())
}

/// macOS-specific FFI bindings for libproc.
#[cfg(target_os = "macos")]
mod macos {
    use libc::{c_int, c_void, pid_t};

    // Link against libproc (part of System framework on macOS)
    #[link(name = "proc", kind = "dylib")]
    extern "C" {
        /// Get the name of a process.
        ///
        /// # Arguments
        ///
        /// * `pid` - The process ID.
        /// * `buffer` - Buffer to store the process name.
        /// * `buffersize` - Size of the buffer.
        ///
        /// # Returns
        ///
        /// The length of the process name on success, or 0 on error.
        pub fn proc_name(pid: pid_t, buffer: *mut c_void, buffersize: u32) -> c_int;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_get_process_name_current_process() {
        // Get our own process name
        // std::process::id() returns u32, libc::pid_t is i32 on most platforms
        // For typical process IDs this conversion is safe (PIDs are < 2^31)
        #[allow(clippy::cast_possible_wrap)]
        let pid = std::process::id() as libc::pid_t;
        let result = get_process_name(pid);

        // Should succeed for our own process
        assert!(
            result.is_ok(),
            "Failed to get process name for current process: {result:?}"
        );

        let name = result.unwrap();
        // The name should not be empty
        assert!(!name.is_empty(), "Process name should not be empty");

        // On most systems running cargo test, the process name will contain "test"
        // or be the test binary name. We just verify it's a reasonable string.
        assert!(
            name.len() < 256,
            "Process name should be reasonably short: {name}"
        );

        // Print for debugging
        eprintln!("Current process name: {name}");
    }

    #[test]
    fn test_get_process_name_invalid_pid() {
        // Use an invalid PID (very large number that shouldn't exist)
        let invalid_pid = 999_999_999;
        let result = get_process_name(invalid_pid);

        // Should fail for invalid PID
        assert!(result.is_err(), "Should fail for invalid PID");
    }

    #[test]
    fn test_get_process_name_init_process() {
        // PID 1 is always the init process (launchd on macOS, systemd/init on Linux)
        let result = get_process_name(1);

        // This might fail due to permissions, but if it succeeds, verify the name
        if let Ok(name) = result {
            assert!(!name.is_empty(), "Init process name should not be empty");
            eprintln!("Init process name: {name}");
        } else {
            eprintln!("Could not read init process name (may be permission denied)");
        }
    }

    #[test]
    fn test_get_foreground_pgid_invalid_fd() {
        // Use an invalid file descriptor
        let invalid_fd = -1;
        let result = get_foreground_pgid(invalid_fd);

        // Should fail for invalid fd
        assert!(result.is_err(), "Should fail for invalid fd");

        if let Err(ProcessDetectError::TcgetpgrpFailed(e)) = result {
            // Error should be EBADF (Bad file descriptor) or similar
            eprintln!("Expected error for invalid fd: {e}");
        }
    }

    #[test]
    fn test_get_foreground_pgid_non_tty_fd() {
        // Create a pipe - neither end is a TTY
        let mut fds = [0i32; 2];
        // SAFETY: pipe() is safe to call with a valid pointer to an array of 2 ints
        let result = unsafe { libc::pipe(fds.as_mut_ptr()) };
        assert_eq!(result, 0, "Failed to create pipe");

        let read_fd = fds[0];
        let write_fd = fds[1];

        // tcgetpgrp should fail on a pipe (not a TTY)
        let result = get_foreground_pgid(read_fd);
        assert!(result.is_err(), "Should fail for non-TTY fd (pipe)");

        // Clean up
        // SAFETY: close() is safe to call with valid file descriptors
        unsafe {
            libc::close(read_fd);
            libc::close(write_fd);
        }
    }

    #[test]
    fn test_get_foreground_process_or_fallback() {
        // With an invalid fd, should return the fallback
        let fallback = "bash";
        let name = get_foreground_process_or(-1, fallback);
        assert_eq!(name, fallback, "Should return fallback for invalid fd");
    }

    #[test]
    fn test_error_display() {
        let error = ProcessDetectError::TcgetpgrpFailed(std::io::Error::from_raw_os_error(9));
        let display = error.to_string();
        assert!(
            display.contains("foreground process group"),
            "Error display should mention foreground process group: {display}"
        );

        #[cfg(target_os = "linux")]
        {
            let error = ProcessDetectError::ProcReadFailed {
                pid: 1234,
                source: std::io::Error::from_raw_os_error(2),
            };
            let display = error.to_string();
            assert!(
                display.contains("1234"),
                "Error display should contain pid: {display}"
            );
            assert!(
                display.contains("/proc/"),
                "Error display should mention /proc/: {display}"
            );
        }

        #[cfg(target_os = "macos")]
        {
            let error = ProcessDetectError::ProcNameFailed { pid: 1234 };
            let display = error.to_string();
            assert!(
                display.contains("1234"),
                "Error display should contain pid: {display}"
            );
        }

        let error = ProcessDetectError::EmptyProcessName(1234);
        let display = error.to_string();
        assert!(
            display.contains("empty"),
            "Error display should mention empty: {display}"
        );
        assert!(
            display.contains("1234"),
            "Error display should contain pid: {display}"
        );
    }

    /// Integration test that requires a real PTY.
    /// This test is ignored by default and can be run with:
    /// `cargo test --lib -- --ignored test_with_real_pty`
    #[test]
    #[ignore = "requires real PTY"]
    fn test_with_real_pty() {
        use portable_pty::{native_pty_system, CommandBuilder, PtySize};

        let pty_system = native_pty_system();
        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        // Spawn a simple command
        let mut cmd = CommandBuilder::new("sleep");
        cmd.arg("1");

        let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

        // Give the process time to start
        std::thread::sleep(std::time::Duration::from_millis(100));

        // Try to get the foreground process
        // Note: This may not work in all environments because the PTY master fd
        // from portable-pty might not support tcgetpgrp directly
        // Note: portable-pty doesn't expose raw fd directly, so this test
        // demonstrates the API but may not work with all PTY implementations.
        // To properly test this, we would need to use the nix crate or
        // lower-level PTY APIs that expose the raw fd.
        eprintln!("PTY created, child spawned");

        // Clean up
        let _ = child.wait();
    }
}
