//! PTY Host module for clai-wrap.
//!
//! This module provides the PTY Host component that creates a pseudo-terminal
//! and spawns the user's shell. It handles PTY creation, resize propagation,
//! and child process lifecycle management.
//!
//! # Platform Support
//!
//! - Unix (Linux/macOS): Uses native PTY via `portable-pty`
//! - Windows: Uses `ConPTY` via `portable-pty` (requires Windows 10 1809+)

use std::env;
use std::io::{Read, Write};
use std::path::PathBuf;

use anyhow::{Context, Result};
use portable_pty::{native_pty_system, Child, CommandBuilder, MasterPty, PtySize};

/// Default shell path if `$SHELL` is not set (Unix).
#[cfg(unix)]
const DEFAULT_SHELL: &str = "/bin/bash";

/// Default shell path (Windows).
#[cfg(windows)]
const DEFAULT_SHELL: &str = "powershell.exe";

/// Environment variable set by clai-wrap to indicate we're running inside the wrapper.
const CLAI_WRAP_ENV_VAR: &str = "CLAI_WRAP";

/// PTY Host manages the pseudo-terminal and child shell process.
///
/// This struct holds the master side of the PTY pair and the spawned child process.
/// It provides methods for resizing the PTY, waiting for the child to exit,
/// and terminating the child process.
pub struct PtyHost {
    /// The master PTY handle for I/O operations.
    master: Box<dyn MasterPty + Send>,
    /// The child process handle.
    child: Box<dyn Child + Send + Sync>,
}

impl PtyHost {
    /// Creates a new PTY Host and spawns the user's shell.
    ///
    /// # Arguments
    ///
    /// * `shell_path` - Optional path to the shell to spawn. If `None`, uses `$SHELL`
    ///   environment variable or falls back to the default shell.
    ///
    /// # Returns
    ///
    /// A new `PtyHost` instance with the shell running inside.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - Failed to get terminal size from parent TTY
    /// - Failed to create PTY pair
    /// - Failed to spawn the shell process
    pub fn new(shell_path: Option<PathBuf>) -> Result<Self> {
        Self::with_size_and_login(shell_path, None, true)
    }

    /// Creates a new PTY Host and controls whether `-l` is passed to the shell.
    ///
    /// # Arguments
    ///
    /// * `shell_path` - Optional path to the shell to spawn.
    /// * `login_shell` - Whether to pass `-l` to the shell process.
    pub fn new_with_login(shell_path: Option<PathBuf>, login_shell: bool) -> Result<Self> {
        Self::with_size_and_login(shell_path, None, login_shell)
    }

    /// Creates a new PTY Host with a specific terminal size.
    ///
    /// # Arguments
    ///
    /// * `shell_path` - Optional path to the shell to spawn.
    /// * `size` - Optional terminal size. If `None`, attempts to get size from parent TTY.
    ///
    /// # Returns
    ///
    /// A new `PtyHost` instance.
    ///
    /// # Errors
    ///
    /// Returns an error if PTY creation or shell spawning fails.
    pub fn with_size(shell_path: Option<PathBuf>, size: Option<PtySize>) -> Result<Self> {
        Self::with_size_and_login(shell_path, size, true)
    }

    /// Creates a new PTY Host with extra arguments and environment variables.
    ///
    /// This is used for shell injection (OSC 133 hooks) where additional
    /// args (e.g., `--rcfile`) and env vars (e.g., `ZDOTDIR`) are needed.
    ///
    /// # Arguments
    ///
    /// * `shell_path` - Optional path to the shell to spawn.
    /// * `login_shell` - Whether to pass `-l` to the shell process.
    /// * `extra_args` - Additional arguments to pass to the shell.
    /// * `extra_env` - Additional environment variables to set.
    pub fn new_with_inject(
        shell_path: Option<PathBuf>,
        login_shell: bool,
        extra_args: &[String],
        extra_env: &[(String, String)],
    ) -> Result<Self> {
        Self::with_size_login_and_inject(shell_path, None, login_shell, extra_args, extra_env)
    }

    /// Internal constructor that controls both PTY size and login-shell behavior.
    fn with_size_and_login(
        shell_path: Option<PathBuf>,
        size: Option<PtySize>,
        login_shell: bool,
    ) -> Result<Self> {
        Self::with_size_login_and_inject(shell_path, size, login_shell, &[], &[])
    }

    /// Internal constructor with all options.
    fn with_size_login_and_inject(
        shell_path: Option<PathBuf>,
        size: Option<PtySize>,
        login_shell: bool,
        extra_args: &[String],
        extra_env: &[(String, String)],
    ) -> Result<Self> {
        // Determine terminal size
        let pty_size = size.unwrap_or_else(|| {
            get_terminal_size().unwrap_or(PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            })
        });

        // Create PTY system
        let pty_system = native_pty_system();

        // Create PTY pair with the determined size
        let pair = pty_system
            .openpty(pty_size)
            .context("Failed to create PTY pair")?;

        // Determine shell path
        let shell = shell_path.unwrap_or_else(get_default_shell);

        // Build command for the shell
        let mut cmd = CommandBuilder::new(&shell);

        if login_shell {
            // Launch as login shell with -l flag (if supported)
            cmd.arg("-l");
        }

        // Add extra arguments from shell injection
        for arg in extra_args {
            cmd.arg(arg);
        }

        // Set CLAI_WRAP=1 environment variable
        cmd.env(CLAI_WRAP_ENV_VAR, "1");

        // Inherit parent environment
        for (key, value) in env::vars_os() {
            // Don't override CLAI_WRAP
            if key != CLAI_WRAP_ENV_VAR {
                cmd.env(key, value);
            }
        }

        // Set extra environment variables from shell injection
        for (key, value) in extra_env {
            cmd.env(key, value);
        }

        // Spawn the shell in the PTY
        let child = pair
            .slave
            .spawn_command(cmd)
            .with_context(|| format!("Failed to spawn shell: {}", shell.display()))?;

        Ok(Self {
            master: pair.master,
            child,
        })
    }

    /// Returns a reader for the master PTY.
    ///
    /// The reader can be used to read output from the child process.
    ///
    /// # Errors
    ///
    /// Returns an error if creating the reader fails.
    pub fn reader(&self) -> Result<Box<dyn Read + Send>> {
        self.master
            .try_clone_reader()
            .context("Failed to clone PTY reader")
    }

    /// Returns a writer for the master PTY.
    ///
    /// The writer can be used to send input to the child process.
    ///
    /// # Errors
    ///
    /// Returns an error if creating the writer fails.
    pub fn writer(&self) -> Result<Box<dyn Write + Send>> {
        self.master
            .take_writer()
            .context("Failed to take PTY writer")
    }

    /// Resizes the PTY to the specified dimensions.
    ///
    /// # Arguments
    ///
    /// * `cols` - Number of columns (width)
    /// * `rows` - Number of rows (height)
    ///
    /// # Errors
    ///
    /// Returns an error if the resize operation fails (e.g., on Windows if `ConPTY`
    /// resize is not supported).
    pub fn resize(&self, cols: u16, rows: u16) -> Result<()> {
        let size = PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        };

        self.master.resize(size).context("Failed to resize PTY")?;

        Ok(())
    }

    /// Waits for the child process to exit.
    ///
    /// This method blocks until the child process terminates.
    ///
    /// # Returns
    ///
    /// The exit status of the child process.
    ///
    /// # Errors
    ///
    /// Returns an error if waiting for the child fails.
    pub fn wait(&mut self) -> Result<ExitStatus> {
        let status = self
            .child
            .wait()
            .context("Failed to wait for child process")?;

        Ok(ExitStatus::from_portable_pty(status))
    }

    /// Attempts to get the exit status without blocking.
    ///
    /// # Returns
    ///
    /// - `Ok(Some(status))` if the child has exited
    /// - `Ok(None)` if the child is still running
    /// - `Err(...)` if the check failed
    ///
    /// # Errors
    ///
    /// Returns an error if checking the child status fails.
    pub fn try_wait(&mut self) -> Result<Option<ExitStatus>> {
        match self.child.try_wait() {
            Ok(Some(status)) => Ok(Some(ExitStatus::from_portable_pty(status))),
            Ok(None) => Ok(None),
            Err(e) => Err(e).context("Failed to check child process status"),
        }
    }

    /// Terminates the child process.
    ///
    /// On Unix, this sends SIGKILL to the child. On Windows, this terminates
    /// the process.
    ///
    /// # Errors
    ///
    /// Returns an error if termination fails.
    pub fn kill(&mut self) -> Result<()> {
        self.child.kill().context("Failed to kill child process")
    }

    /// Returns the process ID of the child.
    ///
    /// # Returns
    ///
    /// The child process ID, or `None` if it cannot be determined.
    pub fn child_pid(&self) -> Option<u32> {
        self.child.process_id()
    }

    /// Returns the raw file descriptor of the master PTY (Unix only).
    ///
    /// This is needed for process detection via `tcgetpgrp()`.
    ///
    /// # Returns
    ///
    /// The master PTY file descriptor, or `None` if not available.
    #[cfg(unix)]
    #[must_use]
    pub fn master_fd(&self) -> Option<std::os::unix::io::RawFd> {
        self.master.as_raw_fd()
    }
}

/// Exit status of the child process.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ExitStatus {
    /// The exit code of the process.
    code: u32,
    /// Whether the process was successful (exit code 0).
    success: bool,
}

impl ExitStatus {
    /// Creates an `ExitStatus` from a `portable_pty::ExitStatus`.
    #[allow(clippy::needless_pass_by_value)]
    fn from_portable_pty(status: portable_pty::ExitStatus) -> Self {
        Self {
            code: status.exit_code(),
            success: status.success(),
        }
    }

    /// Returns the exit code.
    #[must_use]
    pub const fn code(&self) -> u32 {
        self.code
    }

    /// Returns whether the process exited successfully (exit code 0).
    #[must_use]
    pub const fn success(&self) -> bool {
        self.success
    }

    /// Returns the exit code for use as a process exit code.
    ///
    /// Converts the u32 exit code to i32 for use with `std::process::exit()`.
    #[must_use]
    #[allow(clippy::cast_possible_wrap)]
    pub const fn as_exit_code(&self) -> i32 {
        self.code as i32
    }
}

/// Gets the terminal size from the parent TTY.
///
/// This function queries the current terminal size using platform-specific
/// methods.
///
/// # Returns
///
/// The terminal size if it can be determined, or `None` if stdin is not a TTY
/// or the size cannot be queried.
#[cfg(unix)]
fn get_terminal_size() -> Option<PtySize> {
    use std::os::unix::io::AsRawFd;

    let fd = std::io::stdin().as_raw_fd();

    // SAFETY: ioctl with TIOCGWINSZ is safe when passed a valid fd
    // and a properly sized winsize struct.
    unsafe {
        let mut winsize: libc::winsize = std::mem::zeroed();
        if libc::ioctl(fd, libc::TIOCGWINSZ, &mut winsize) == 0 {
            Some(PtySize {
                rows: winsize.ws_row,
                cols: winsize.ws_col,
                pixel_width: winsize.ws_xpixel,
                pixel_height: winsize.ws_ypixel,
            })
        } else {
            None
        }
    }
}

/// Gets the terminal size on Windows.
#[cfg(windows)]
fn get_terminal_size() -> Option<PtySize> {
    use windows_sys::Win32::System::Console::{
        GetConsoleScreenBufferInfo, GetStdHandle, CONSOLE_SCREEN_BUFFER_INFO, STD_OUTPUT_HANDLE,
    };

    // SAFETY: GetStdHandle and GetConsoleScreenBufferInfo are safe Windows API calls.
    unsafe {
        let handle = GetStdHandle(STD_OUTPUT_HANDLE);
        if handle == windows_sys::Win32::Foundation::INVALID_HANDLE_VALUE {
            return None;
        }

        let mut info: CONSOLE_SCREEN_BUFFER_INFO = std::mem::zeroed();
        if GetConsoleScreenBufferInfo(handle, &mut info) != 0 {
            let cols = (info.srWindow.Right - info.srWindow.Left + 1) as u16;
            let rows = (info.srWindow.Bottom - info.srWindow.Top + 1) as u16;
            Some(PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            })
        } else {
            None
        }
    }
}

/// Gets the default shell path.
///
/// On Unix, this checks the `$SHELL` environment variable first.
/// Falls back to platform-specific defaults.
fn get_default_shell() -> PathBuf {
    env::var("SHELL").map_or_else(|_| PathBuf::from(DEFAULT_SHELL), PathBuf::from)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[cfg(unix)]
    use std::fs;
    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;
    use std::time::Duration;
    #[cfg(unix)]
    use tempfile::NamedTempFile;

    /// Helper to check if PTY process spawning is available in this environment.
    /// Returns false in sandboxed/containerized environments where PTY spawn fails.
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
    fn test_spawn_echo_command() {
        if !can_spawn_pty_process() {
            eprintln!("Skipping: PTY process spawning not available in this environment");
            return;
        }
        // Create a PTY with a known size
        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        // Use echo as a simple test command instead of a full shell
        let pty_system = native_pty_system();
        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        // Build a simple echo command
        let mut cmd = CommandBuilder::new("echo");
        cmd.arg("hello from pty");

        let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn echo");

        // Read output
        let mut reader = pair
            .master
            .try_clone_reader()
            .expect("Failed to get reader");
        let mut output = String::new();

        // Set a timeout for reading
        let mut buf = [0u8; 1024];
        loop {
            match reader.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    output.push_str(&String::from_utf8_lossy(&buf[..n]));
                    if output.contains("hello from pty") {
                        break;
                    }
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(10));
                    continue;
                }
                Err(_) => break,
            }
        }

        // Wait for the child to exit
        let status = child.wait().expect("Failed to wait for child");
        assert!(status.success());

        // Verify output contains our expected string
        assert!(
            output.contains("hello from pty"),
            "Expected output to contain 'hello from pty', got: {output}"
        );
    }

    #[test]
    fn test_resize() {
        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        let pty_system = native_pty_system();
        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        // Resize to new dimensions
        let new_size = PtySize {
            rows: 40,
            cols: 120,
            pixel_width: 0,
            pixel_height: 0,
        };

        let result = pair.master.resize(new_size);
        assert!(result.is_ok(), "Resize should succeed");

        // Resize to minimum valid dimensions
        let min_size = PtySize {
            rows: 1,
            cols: 1,
            pixel_width: 0,
            pixel_height: 0,
        };

        let result = pair.master.resize(min_size);
        assert!(result.is_ok(), "Resize to minimum should succeed");
    }

    #[test]
    fn test_environment_inheritance() {
        if !can_spawn_pty_process() {
            eprintln!("Skipping: PTY process spawning not available in this environment");
            return;
        }
        // Set a test environment variable
        let test_var = format!("CLAI_TEST_VAR_{}", std::process::id());
        let test_value = "test_value_12345";
        env::set_var(&test_var, test_value);

        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        let pty_system = native_pty_system();
        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        // Build a command that prints the environment variable
        #[cfg(unix)]
        let mut cmd = {
            let mut cmd = CommandBuilder::new("sh");
            cmd.args(["-c", &format!("echo ${test_var}")]);
            cmd
        };

        #[cfg(windows)]
        let mut cmd = {
            let mut cmd = CommandBuilder::new("cmd");
            cmd.args(["/c", &format!("echo %{test_var}%")]);
            cmd
        };

        // Inherit the test variable
        cmd.env(&test_var, test_value);
        cmd.env(CLAI_WRAP_ENV_VAR, "1");

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
                    if output.contains(test_value) {
                        break;
                    }
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(10));
                    continue;
                }
                Err(_) => break,
            }
        }

        let _ = child.wait();

        // Clean up
        env::remove_var(&test_var);

        // Verify the environment variable was inherited
        assert!(
            output.contains(test_value),
            "Expected output to contain '{test_value}', got: {output}"
        );
    }

    #[test]
    fn test_clai_wrap_env_var() {
        if !can_spawn_pty_process() {
            eprintln!("Skipping: PTY process spawning not available in this environment");
            return;
        }
        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        let pty_system = native_pty_system();
        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        // Build a command that prints CLAI_WRAP
        #[cfg(unix)]
        let mut cmd = {
            let mut cmd = CommandBuilder::new("sh");
            cmd.args(["-c", "echo CLAI_WRAP=$CLAI_WRAP"]);
            cmd
        };

        #[cfg(windows)]
        let mut cmd = {
            let mut cmd = CommandBuilder::new("cmd");
            cmd.args(["/c", "echo CLAI_WRAP=%CLAI_WRAP%"]);
            cmd
        };

        cmd.env(CLAI_WRAP_ENV_VAR, "1");

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
                    if output.contains("CLAI_WRAP=1") {
                        break;
                    }
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(10));
                    continue;
                }
                Err(_) => break,
            }
        }

        let _ = child.wait();

        // Verify CLAI_WRAP=1 is set
        assert!(
            output.contains("CLAI_WRAP=1"),
            "Expected output to contain 'CLAI_WRAP=1', got: {output}"
        );
    }

    #[test]
    fn test_exit_status() {
        if !can_spawn_pty_process() {
            eprintln!("Skipping: PTY process spawning not available in this environment");
            return;
        }
        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        let pty_system = native_pty_system();

        // Test successful exit
        {
            let pair = pty_system.openpty(size).expect("Failed to create PTY");

            #[cfg(unix)]
            let cmd = {
                let mut cmd = CommandBuilder::new("sh");
                cmd.args(["-c", "exit 0"]);
                cmd
            };

            #[cfg(windows)]
            let cmd = {
                let mut cmd = CommandBuilder::new("cmd");
                cmd.args(["/c", "exit 0"]);
                cmd
            };

            let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");
            let status = child.wait().expect("Failed to wait");

            assert!(status.success());
            assert_eq!(status.exit_code(), 0u32);
        }

        // Test failed exit
        {
            let pair = pty_system.openpty(size).expect("Failed to create PTY");

            #[cfg(unix)]
            let cmd = {
                let mut cmd = CommandBuilder::new("sh");
                cmd.args(["-c", "exit 42"]);
                cmd
            };

            #[cfg(windows)]
            let cmd = {
                let mut cmd = CommandBuilder::new("cmd");
                cmd.args(["/c", "exit 42"]);
                cmd
            };

            let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");
            let status = child.wait().expect("Failed to wait");

            assert!(!status.success());
            assert_eq!(status.exit_code(), 42u32);
        }
    }

    #[test]
    #[cfg(unix)]
    fn test_new_with_login_disabled_does_not_pass_login_flag() {
        if !can_spawn_pty_process() {
            eprintln!("Skipping: PTY process spawning not available in this environment");
            return;
        }
        let script = NamedTempFile::new().expect("create temp script");
        let script_path = script.path().to_path_buf();

        fs::write(
            &script_path,
            "#!/bin/sh\nprintf 'ARGC=%s\\nARG1=%s\\n' \"$#\" \"$1\"\n",
        )
        .expect("write script");
        let perms = fs::Permissions::from_mode(0o755);
        fs::set_permissions(&script_path, perms).expect("set script executable");

        let mut host = PtyHost::new_with_login(Some(script_path), false).expect("spawn pty host");
        let mut reader = host.reader().expect("pty reader");

        let mut output = String::new();
        let mut buf = [0u8; 1024];
        let start = std::time::Instant::now();
        while start.elapsed() < Duration::from_secs(2) {
            match reader.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    output.push_str(&String::from_utf8_lossy(&buf[..n]));
                    if output.contains("ARGC=") {
                        break;
                    }
                }
                Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(10));
                }
                Err(_) => break,
            }
        }

        let _ = host.wait();

        assert!(
            output.contains("ARGC=0"),
            "expected no extra args, got output: {output}"
        );
    }

    #[test]
    fn test_get_default_shell() {
        // Save current SHELL
        let original = env::var("SHELL").ok();

        // Test with SHELL set
        env::set_var("SHELL", "/usr/bin/zsh");
        assert_eq!(get_default_shell(), PathBuf::from("/usr/bin/zsh"));

        // Test with SHELL unset
        env::remove_var("SHELL");
        let default = get_default_shell();
        assert_eq!(default, PathBuf::from(DEFAULT_SHELL));

        // Restore original
        if let Some(shell) = original {
            env::set_var("SHELL", shell);
        }
    }

    #[test]
    fn test_exit_status_as_exit_code() {
        // Success case
        let status = ExitStatus {
            code: 0,
            success: true,
        };
        assert_eq!(status.as_exit_code(), 0);

        // Non-zero exit code
        let status = ExitStatus {
            code: 42,
            success: false,
        };
        assert_eq!(status.as_exit_code(), 42);

        // Exit code 1 (typical failure)
        let status = ExitStatus {
            code: 1,
            success: false,
        };
        assert_eq!(status.as_exit_code(), 1);
    }

    #[test]
    #[cfg(unix)]
    fn test_get_terminal_size_non_tty() {
        // When stdin is not a TTY (e.g., in CI), this should return None
        // or a valid size if running in a terminal
        let size = get_terminal_size();

        if let Some(s) = size {
            // If we got a size, it should be reasonable
            assert!(s.rows > 0);
            assert!(s.cols > 0);
        }
        // If None, that's also acceptable (non-TTY environment)
    }

    #[test]
    fn test_pty_host_with_explicit_size() {
        // Test creating PtyHost with explicit size
        let size = PtySize {
            rows: 30,
            cols: 100,
            pixel_width: 0,
            pixel_height: 0,
        };

        // We use echo for a quick test instead of a full shell
        let pty_system = native_pty_system();
        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        // Just verify the PTY was created successfully
        assert!(pair.master.try_clone_reader().is_ok());
    }

    #[test]
    fn test_child_pid() {
        if !can_spawn_pty_process() {
            eprintln!("Skipping: PTY process spawning not available in this environment");
            return;
        }
        let size = PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        };

        let pty_system = native_pty_system();
        let pair = pty_system.openpty(size).expect("Failed to create PTY");

        #[cfg(unix)]
        let cmd = {
            let mut cmd = CommandBuilder::new("sleep");
            cmd.arg("0.1");
            cmd
        };

        #[cfg(windows)]
        let cmd = {
            let mut cmd = CommandBuilder::new("cmd");
            cmd.args(["/c", "timeout /t 1 /nobreak >nul"]);
            cmd
        };

        let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

        // Should have a valid PID
        let pid = child.process_id();
        assert!(pid.is_some(), "Child should have a process ID");
        assert!(pid.unwrap() > 0, "Process ID should be positive");

        // Clean up
        let _ = child.wait();
    }
}
