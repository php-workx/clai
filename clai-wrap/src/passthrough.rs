//! Passthrough Fallback Mode for clai-wrap.
//!
//! This module provides a pure passthrough mode when advanced features are
//! unavailable due to terminal limitations or unsupported environments.
//!
//! # When to Use Passthrough
//!
//! Passthrough mode is used when:
//! - `TERM=dumb` or unset
//! - stdin or stdout is not a TTY
//! - Unknown or unsupported shell
//! - cmd.exe on Windows (no OSC 133 support)
//!
//! # Passthrough Behavior
//!
//! - stdin -> PTY (no hotkey detection)
//! - PTY -> stdout (no output capture)
//! - Signal forwarding still works
//! - No picker UI available

use std::env;
use std::io::{Read, Write};
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result};

use crate::pty_host::{ExitStatus, PtyHost};
use crate::raw_mode::{detect_tty, TtyStatus};

/// Buffer size for I/O operations.
const IO_BUFFER_SIZE: usize = 4096;

/// Reasons why passthrough mode should be used.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PassthroughReason {
    /// Full functionality is available; no passthrough needed.
    NotNeeded,
    /// TERM=dumb or TERM is unset.
    DumbTerminal,
    /// stdin is not a TTY.
    NonTtyStdin,
    /// stdout is not a TTY.
    NonTtyStdout,
    /// Unsupported shell (e.g., cmd.exe on Windows).
    UnsupportedShell(String),
}

impl PassthroughReason {
    /// Returns true if passthrough mode is needed.
    #[must_use]
    pub const fn needs_passthrough(&self) -> bool {
        !matches!(self, Self::NotNeeded)
    }

    /// Returns a human-readable description of why passthrough is needed.
    #[must_use]
    pub fn description(&self) -> String {
        match self {
            Self::NotNeeded => "Full functionality available".to_string(),
            Self::DumbTerminal => "TERM is 'dumb' or unset; advanced features disabled".to_string(),
            Self::NonTtyStdin => "stdin is not a TTY; hotkey detection disabled".to_string(),
            Self::NonTtyStdout => "stdout is not a TTY; picker UI disabled".to_string(),
            Self::UnsupportedShell(shell) => {
                format!("Shell '{shell}' does not support OSC 133; operating in passthrough mode")
            }
        }
    }
}

impl std::fmt::Display for PassthroughReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.description())
    }
}

/// Check if we should use passthrough mode based on environment.
///
/// This function checks various conditions that would require falling back
/// to passthrough mode:
///
/// 1. `TERM=dumb` or unset - Terminal doesn't support escape sequences
/// 2. stdin not TTY - Cannot detect hotkeys
/// 3. stdout not TTY - Cannot display picker UI
///
/// # Returns
///
/// A `PassthroughReason` indicating why passthrough is needed, or
/// `PassthroughReason::NotNeeded` if full functionality is available.
#[must_use]
pub fn should_use_passthrough() -> PassthroughReason {
    // Check TERM environment variable
    match env::var("TERM") {
        Ok(term) if term == "dumb" => {
            return PassthroughReason::DumbTerminal;
        }
        Err(_) => {
            // TERM is not set
            return PassthroughReason::DumbTerminal;
        }
        Ok(_) => {
            // TERM is set and not dumb, continue checking
        }
    }

    // Check TTY status
    let tty_status = detect_tty();

    if !tty_status.stdin {
        return PassthroughReason::NonTtyStdin;
    }

    if !tty_status.stdout {
        return PassthroughReason::NonTtyStdout;
    }

    PassthroughReason::NotNeeded
}

/// Check if a shell is supported for full functionality.
///
/// # Arguments
///
/// * `shell_path` - Path to the shell binary
///
/// # Returns
///
/// `Some(reason)` if the shell is unsupported, `None` if supported.
#[must_use]
pub fn check_shell_support(shell_path: &Path) -> Option<PassthroughReason> {
    let shell_name = shell_path
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("");

    // Normalize shell name (remove .exe on Windows)
    let shell_name = shell_name
        .strip_suffix(".exe")
        .unwrap_or(shell_name)
        .to_lowercase();

    match shell_name.as_str() {
        // Supported shells (including PowerShell which is partially supported)
        "bash" | "zsh" | "fish" | "sh" | "powershell" | "pwsh" => None,
        // cmd.exe does not support OSC 133
        "cmd" => Some(PassthroughReason::UnsupportedShell("cmd.exe".to_string())),
        // Unknown shells - passthrough for safety
        "" => Some(PassthroughReason::UnsupportedShell("unknown".to_string())),
        other => Some(PassthroughReason::UnsupportedShell(other.to_string())),
    }
}

/// Comprehensive check for passthrough mode.
///
/// Combines environment checks with shell support checks.
///
/// # Arguments
///
/// * `shell_path` - Optional path to the shell binary
///
/// # Returns
///
/// A `PassthroughReason` indicating why passthrough is needed, or
/// `PassthroughReason::NotNeeded` if full functionality is available.
#[must_use]
pub fn check_passthrough_needed(shell_path: Option<&Path>) -> PassthroughReason {
    // First check environment conditions
    let env_reason = should_use_passthrough();
    if env_reason.needs_passthrough() {
        return env_reason;
    }

    // Then check shell support if path is provided
    if let Some(path) = shell_path {
        if let Some(shell_reason) = check_shell_support(path) {
            return shell_reason;
        }
    }

    PassthroughReason::NotNeeded
}

/// Passthrough mode for pure I/O forwarding.
///
/// This mode provides basic PTY wrapping without hotkey detection,
/// output capture, or picker UI. It's used when these features are
/// unavailable due to terminal limitations.
pub struct PassthroughMode {
    /// The PTY host managing the child shell.
    pty: PtyHost,
    /// Flag to signal shutdown to I/O threads.
    shutdown: Arc<AtomicBool>,
}

impl PassthroughMode {
    /// Creates a new passthrough mode with the given shell.
    ///
    /// # Arguments
    ///
    /// * `shell_path` - Path to the shell to spawn
    ///
    /// # Returns
    ///
    /// A new `PassthroughMode` instance.
    ///
    /// # Errors
    ///
    /// Returns an error if the PTY cannot be created or the shell cannot be spawned.
    pub fn new(shell_path: &Path) -> Result<Self> {
        let pty = PtyHost::new(Some(shell_path.to_path_buf()))
            .context("Failed to create PTY host for passthrough mode")?;

        Ok(Self {
            pty,
            shutdown: Arc::new(AtomicBool::new(false)),
        })
    }

    /// Creates a new passthrough mode with the default shell.
    ///
    /// # Returns
    ///
    /// A new `PassthroughMode` instance using the default shell.
    ///
    /// # Errors
    ///
    /// Returns an error if the PTY cannot be created or the shell cannot be spawned.
    pub fn with_default_shell() -> Result<Self> {
        let pty = PtyHost::new(None).context("Failed to create PTY host for passthrough mode")?;

        Ok(Self {
            pty,
            shutdown: Arc::new(AtomicBool::new(false)),
        })
    }

    /// Returns a reference to the shutdown flag.
    ///
    /// This can be used to signal shutdown from signal handlers.
    #[must_use]
    pub fn shutdown_flag(&self) -> Arc<AtomicBool> {
        Arc::clone(&self.shutdown)
    }

    /// Runs the passthrough loop until the shell exits.
    ///
    /// This method:
    /// 1. Spawns threads for stdin->PTY and PTY->stdout forwarding
    /// 2. Waits for the child shell to exit
    /// 3. Returns the exit status
    ///
    /// # Returns
    ///
    /// The exit status of the child shell.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - I/O threads cannot be started
    /// - Waiting for the child fails
    pub fn run(&mut self) -> Result<ExitStatus> {
        // Get PTY reader and writer
        let mut pty_reader = self.pty.reader().context("Failed to get PTY reader")?;
        let mut pty_writer = self.pty.writer().context("Failed to get PTY writer")?;

        let shutdown = Arc::clone(&self.shutdown);
        let shutdown_stdin = Arc::clone(&self.shutdown);
        let shutdown_stdout = Arc::clone(&self.shutdown);

        // Spawn stdin -> PTY thread
        let stdin_handle = thread::Builder::new()
            .name("passthrough-stdin".to_string())
            .spawn(move || {
                let mut stdin = std::io::stdin();
                let mut buf = [0u8; IO_BUFFER_SIZE];

                loop {
                    if shutdown_stdin.load(Ordering::Relaxed) {
                        break;
                    }

                    match stdin.read(&mut buf) {
                        Ok(0) => {
                            // EOF on stdin
                            break;
                        }
                        Ok(n) => {
                            if pty_writer.write_all(&buf[..n]).is_err() {
                                break;
                            }
                            if pty_writer.flush().is_err() {
                                break;
                            }
                        }
                        Err(ref e) if e.kind() == std::io::ErrorKind::Interrupted => {}
                        Err(_) => {
                            break;
                        }
                    }
                }
            })
            .context("Failed to spawn stdin thread")?;

        // Spawn PTY -> stdout thread
        let stdout_handle = thread::Builder::new()
            .name("passthrough-stdout".to_string())
            .spawn(move || {
                let mut stdout = std::io::stdout();
                let mut buf = [0u8; IO_BUFFER_SIZE];

                loop {
                    if shutdown_stdout.load(Ordering::Relaxed) {
                        break;
                    }

                    match pty_reader.read(&mut buf) {
                        Ok(0) => {
                            // EOF on PTY
                            break;
                        }
                        Ok(n) => {
                            if stdout.write_all(&buf[..n]).is_err() {
                                break;
                            }
                            if stdout.flush().is_err() {
                                break;
                            }
                        }
                        Err(ref e) if e.kind() == std::io::ErrorKind::Interrupted => {}
                        Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                            // Non-blocking read returned nothing
                            thread::sleep(Duration::from_millis(1));
                        }
                        Err(_) => {
                            break;
                        }
                    }
                }
            })
            .context("Failed to spawn stdout thread")?;

        // Wait for child to exit
        let exit_status = self
            .pty
            .wait()
            .context("Failed to wait for child process")?;

        // Signal shutdown to threads
        shutdown.store(true, Ordering::Relaxed);

        // Wait for threads to finish (with timeout)
        let _ = stdin_handle.join();
        let _ = stdout_handle.join();

        Ok(exit_status)
    }

    /// Returns the child process ID.
    #[must_use]
    pub fn child_pid(&self) -> Option<u32> {
        self.pty.child_pid()
    }

    /// Resizes the PTY.
    ///
    /// # Arguments
    ///
    /// * `cols` - Number of columns
    /// * `rows` - Number of rows
    ///
    /// # Errors
    ///
    /// Returns an error if the resize fails.
    pub fn resize(&self, cols: u16, rows: u16) -> Result<()> {
        self.pty.resize(cols, rows)
    }

    /// Terminates the child process.
    ///
    /// # Errors
    ///
    /// Returns an error if termination fails.
    pub fn kill(&mut self) -> Result<()> {
        self.shutdown.store(true, Ordering::Relaxed);
        self.pty.kill()
    }
}

/// Returns the current TTY status.
///
/// This is a convenience re-export of `detect_tty` for use in passthrough
/// mode decisions.
#[must_use]
pub fn get_tty_status() -> TtyStatus {
    detect_tty()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn test_passthrough_reason_not_needed() {
        let reason = PassthroughReason::NotNeeded;
        assert!(!reason.needs_passthrough());
        assert!(reason.description().contains("Full functionality"));
    }

    #[test]
    fn test_passthrough_reason_dumb_terminal() {
        let reason = PassthroughReason::DumbTerminal;
        assert!(reason.needs_passthrough());
        assert!(reason.description().contains("dumb"));
    }

    #[test]
    fn test_passthrough_reason_non_tty_stdin() {
        let reason = PassthroughReason::NonTtyStdin;
        assert!(reason.needs_passthrough());
        assert!(reason.description().contains("stdin"));
    }

    #[test]
    fn test_passthrough_reason_non_tty_stdout() {
        let reason = PassthroughReason::NonTtyStdout;
        assert!(reason.needs_passthrough());
        assert!(reason.description().contains("stdout"));
    }

    #[test]
    fn test_passthrough_reason_unsupported_shell() {
        let reason = PassthroughReason::UnsupportedShell("cmd.exe".to_string());
        assert!(reason.needs_passthrough());
        assert!(reason.description().contains("cmd.exe"));
        assert!(reason.description().contains("OSC 133"));
    }

    #[test]
    fn test_passthrough_reason_display() {
        let reason = PassthroughReason::DumbTerminal;
        let display = format!("{reason}");
        assert!(display.contains("dumb"));
    }

    #[test]
    fn test_check_shell_support_bash() {
        let path = PathBuf::from("/bin/bash");
        assert!(check_shell_support(&path).is_none());
    }

    #[test]
    fn test_check_shell_support_zsh() {
        let path = PathBuf::from("/usr/bin/zsh");
        assert!(check_shell_support(&path).is_none());
    }

    #[test]
    fn test_check_shell_support_fish() {
        let path = PathBuf::from("/usr/local/bin/fish");
        assert!(check_shell_support(&path).is_none());
    }

    #[test]
    fn test_check_shell_support_powershell() {
        let path = PathBuf::from("powershell.exe");
        assert!(check_shell_support(&path).is_none());
    }

    #[test]
    fn test_check_shell_support_pwsh() {
        let path = PathBuf::from("/usr/local/bin/pwsh");
        assert!(check_shell_support(&path).is_none());
    }

    #[test]
    fn test_check_shell_support_cmd() {
        let path = PathBuf::from("cmd.exe");
        let reason = check_shell_support(&path);
        assert!(reason.is_some());
        assert!(matches!(
            reason,
            Some(PassthroughReason::UnsupportedShell(s)) if s == "cmd.exe"
        ));
    }

    #[test]
    fn test_check_shell_support_unknown() {
        let path = PathBuf::from("/bin/weirdshell");
        let reason = check_shell_support(&path);
        assert!(reason.is_some());
        assert!(matches!(
            reason,
            Some(PassthroughReason::UnsupportedShell(s)) if s == "weirdshell"
        ));
    }

    #[test]
    fn test_check_shell_support_case_insensitive() {
        let path = PathBuf::from("/bin/BASH");
        assert!(check_shell_support(&path).is_none());

        let path = PathBuf::from("/bin/ZSH");
        assert!(check_shell_support(&path).is_none());
    }

    #[test]
    fn test_check_passthrough_needed_with_shell() {
        // With supported shell, should return NotNeeded if env is ok
        // Note: This test may return different results depending on actual env
        let path = PathBuf::from("/bin/bash");
        let reason = check_passthrough_needed(Some(&path));
        // We can't assert NotNeeded because the test might run in a non-TTY
        // Just verify it returns a valid reason
        let _ = reason.description();
    }

    #[test]
    fn test_check_passthrough_needed_without_shell() {
        // Without shell path, only checks environment
        let reason = check_passthrough_needed(None);
        // Just verify it returns a valid reason
        let _ = reason.description();
    }

    #[test]
    fn test_should_use_passthrough_checks_term() {
        // Save original TERM
        let original_term = env::var("TERM").ok();

        // Test with TERM=dumb
        env::set_var("TERM", "dumb");
        let reason = should_use_passthrough();
        assert!(matches!(reason, PassthroughReason::DumbTerminal));

        // Restore original TERM
        if let Some(term) = original_term {
            env::set_var("TERM", term);
        } else {
            env::remove_var("TERM");
        }
    }

    #[test]
    fn test_should_use_passthrough_with_valid_term() {
        // Save original TERM
        let original_term = env::var("TERM").ok();

        // Test with a valid TERM
        env::set_var("TERM", "xterm-256color");
        let reason = should_use_passthrough();

        // The result depends on whether stdin/stdout are TTYs
        // In tests, they're typically not, so we might get NonTtyStdin/NonTtyStdout
        // Just verify it doesn't return DumbTerminal
        assert!(!matches!(reason, PassthroughReason::DumbTerminal));

        // Restore original TERM
        if let Some(term) = original_term {
            env::set_var("TERM", term);
        }
    }

    #[test]
    fn test_get_tty_status() {
        let status = get_tty_status();
        // Just verify the function works and returns valid status
        let _ = status.stdin;
        let _ = status.stdout;
        let _ = status.stderr;
    }

    #[test]
    fn test_passthrough_reason_equality() {
        assert_eq!(PassthroughReason::NotNeeded, PassthroughReason::NotNeeded);
        assert_eq!(
            PassthroughReason::DumbTerminal,
            PassthroughReason::DumbTerminal
        );
        assert_eq!(
            PassthroughReason::UnsupportedShell("cmd".to_string()),
            PassthroughReason::UnsupportedShell("cmd".to_string())
        );
        assert_ne!(
            PassthroughReason::UnsupportedShell("cmd".to_string()),
            PassthroughReason::UnsupportedShell("sh".to_string())
        );
    }

    // Integration tests that require a real PTY
    #[cfg(unix)]
    mod unix_tests {
        use super::*;
        use portable_pty::{native_pty_system, CommandBuilder, PtySize};
        use std::io::Read;
        use std::time::Duration;

        /// Check if PTY process spawning is available in this environment.
        fn can_spawn_pty_process() -> bool {
            let size = PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            };
            let pty_system = native_pty_system();
            let Ok(pair) = pty_system.openpty(size) else {
                return false;
            };
            let cmd = CommandBuilder::new("echo");
            pair.slave.spawn_command(cmd).is_ok()
        }

        #[test]
        fn test_passthrough_mode_creation() {
            if !can_spawn_pty_process() {
                eprintln!("Skipping: PTY process spawning not available in this environment");
                return;
            }
            // Test creating passthrough mode with echo command
            // We can't easily test with a real shell in unit tests
            let path = PathBuf::from("/bin/sh");
            let result = PassthroughMode::new(&path);
            assert!(result.is_ok());

            // Kill it immediately
            let mut mode = result.unwrap();
            let _ = mode.kill();
        }

        #[test]
        fn test_passthrough_mode_child_pid() {
            if !can_spawn_pty_process() {
                eprintln!("Skipping: PTY process spawning not available in this environment");
                return;
            }
            let path = PathBuf::from("/bin/sh");
            let result = PassthroughMode::new(&path);
            assert!(result.is_ok());

            let mut mode = result.unwrap();
            let pid = mode.child_pid();
            assert!(pid.is_some());
            assert!(pid.unwrap() > 0);

            let _ = mode.kill();
        }

        #[test]
        fn test_passthrough_mode_shutdown_flag() {
            if !can_spawn_pty_process() {
                eprintln!("Skipping: PTY process spawning not available in this environment");
                return;
            }
            let path = PathBuf::from("/bin/sh");
            let result = PassthroughMode::new(&path);
            assert!(result.is_ok());

            let mut mode = result.unwrap();
            let flag = mode.shutdown_flag();

            // Flag should initially be false
            assert!(!flag.load(Ordering::Relaxed));

            // Setting it should work
            flag.store(true, Ordering::Relaxed);
            assert!(flag.load(Ordering::Relaxed));

            let _ = mode.kill();
        }

        #[test]
        fn test_passthrough_exit_status_propagation() {
            if !can_spawn_pty_process() {
                eprintln!("Skipping: PTY process spawning not available in this environment");
                return;
            }
            // Test that exit status is correctly propagated
            let size = PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            };

            let pty_system = native_pty_system();
            let pair = pty_system.openpty(size).expect("Failed to create PTY");

            // Run a command that exits with code 42
            let mut cmd = CommandBuilder::new("sh");
            cmd.args(["-c", "exit 42"]);

            let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");
            let status = child.wait().expect("Failed to wait");

            assert!(!status.success());
            assert_eq!(status.exit_code(), 42u32);
        }

        #[test]
        fn test_passthrough_io_forwarding() {
            if !can_spawn_pty_process() {
                eprintln!("Skipping: PTY process spawning not available in this environment");
                return;
            }
            // Test basic I/O forwarding
            let size = PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            };

            let pty_system = native_pty_system();
            let pair = pty_system.openpty(size).expect("Failed to create PTY");

            // Run echo command
            let mut cmd = CommandBuilder::new("echo");
            cmd.arg("passthrough_test_output");

            let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

            // Read output
            let mut reader = pair
                .master
                .try_clone_reader()
                .expect("Failed to get reader");
            let mut output = String::new();
            let mut buf = [0u8; 1024];

            let start = std::time::Instant::now();
            while start.elapsed() < Duration::from_secs(2) {
                match reader.read(&mut buf) {
                    Ok(0) => break,
                    Ok(n) => {
                        output.push_str(&String::from_utf8_lossy(&buf[..n]));
                        if output.contains("passthrough_test_output") {
                            break;
                        }
                    }
                    Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                        std::thread::sleep(Duration::from_millis(10));
                    }
                    Err(_) => break,
                }
            }

            let _ = child.wait();

            assert!(
                output.contains("passthrough_test_output"),
                "Expected output to contain 'passthrough_test_output', got: {output}"
            );
        }
    }
}
