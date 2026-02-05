//! CLI interface for clai-wrap
//!
//! This module provides command-line argument parsing using clap.
//! It supports the various modes of operation defined in the technical spec:
//!
//! - Default mode: wrap shell with hotkey and picker UI
//! - Standalone mode: operate without daemon connection
//! - Passthrough mode: pure passthrough for testing
//!
//! Environment variables provide fallbacks for many options:
//! - `SHELL`: default shell path
//! - `CLAI_DEBUG`: enable debug logging
//! - `CLAI_HOTKEY`: override default hotkey chord
//! - `CLAI_SOCKET`: override daemon socket path

use clap::{Parser, Subcommand};
use std::path::PathBuf;

/// Default buffer capacity in bytes (2 MiB)
pub const DEFAULT_BUFFER_CAP: usize = 2 * 1024 * 1024;

/// Default daemon connection timeout in milliseconds
pub const DEFAULT_DAEMON_TIMEOUT_MS: u64 = 500;

/// Default hotkey chord timeout in milliseconds
pub const DEFAULT_HOTKEY_TIMEOUT_MS: u64 = 500;

/// clai-wrap: PTY wrapper for intelligent terminal assistance
///
/// Wraps your shell in a pseudo-terminal to provide intelligent command
/// suggestions, history search, and autocomplete features.
#[derive(Parser, Debug, Clone, PartialEq, Eq)]
#[allow(clippy::struct_excessive_bools)] // CLI options naturally have many boolean flags
#[command(
    name = "clai-wrap",
    author,
    version,
    about = "PTY wrapper for intelligent terminal assistance",
    long_about = "clai-wrap wraps your shell in a pseudo-terminal to provide intelligent command \
                  suggestions, history search, and autocomplete features. It intercepts a \
                  configurable hotkey chord to display an instant picker UI."
)]
pub struct Cli {
    /// Shell to launch (defaults to $SHELL or /bin/bash)
    #[arg(long, short = 's', env = "SHELL")]
    pub shell: Option<PathBuf>,

    /// Launch as a login shell
    #[arg(long, default_value_t = true)]
    pub login_shell: bool,

    /// Hotkey chord to trigger picker (e.g., "ctrl-\\ h")
    #[arg(long, env = "CLAI_HOTKEY")]
    pub hotkey: Option<String>,

    /// Output buffer capacity in bytes
    #[arg(long, default_value_t = DEFAULT_BUFFER_CAP)]
    pub buffer_cap: usize,

    /// Execute command immediately after selection
    #[arg(long)]
    pub execute_on_select: bool,

    /// Path to history file
    #[arg(long)]
    pub history_file: Option<PathBuf>,

    /// Unix socket path for daemon connection
    #[arg(long, env = "CLAI_SOCKET")]
    pub daemon_socket: Option<PathBuf>,

    /// Disable daemon connection (standalone mode with picker only)
    #[arg(long)]
    pub no_daemon: bool,

    /// Alias for --no-daemon (force standalone mode)
    #[arg(long)]
    pub standalone: bool,

    /// Disable picker UI entirely (still capture output if daemon connected)
    #[arg(long)]
    pub no_ui: bool,

    /// Run without TTY requirement (pure passthrough mode)
    #[arg(long)]
    pub force_non_tty: bool,

    /// Alias for --force-non-tty (passthrough mode for testing)
    #[arg(long)]
    pub passthrough: bool,

    /// Enable debug logging
    #[arg(long, env = "CLAI_DEBUG")]
    pub debug: bool,

    /// Daemon connection timeout in milliseconds
    #[arg(long, default_value_t = DEFAULT_DAEMON_TIMEOUT_MS)]
    pub daemon_timeout: u64,

    /// Hotkey chord timeout in milliseconds
    #[arg(long, default_value_t = DEFAULT_HOTKEY_TIMEOUT_MS)]
    pub hotkey_timeout: u64,

    /// Subcommand to run
    #[command(subcommand)]
    pub command: Option<Commands>,
}

/// Subcommands for clai-wrap
#[derive(Subcommand, Debug, Clone, PartialEq, Eq)]
pub enum Commands {
    /// Show version information
    Version,

    /// Reset terminal state (useful after abnormal exit)
    #[command(name = "reset-terminal")]
    ResetTerminal,
}

impl Cli {
    /// Parse command line arguments
    pub fn parse_args() -> Self {
        Self::parse()
    }

    /// Parse from an iterator (useful for testing)
    pub fn parse_from_args<I, T>(args: I) -> Self
    where
        I: IntoIterator<Item = T>,
        T: Into<std::ffi::OsString> + Clone,
    {
        Self::parse_from(args)
    }

    /// Try to parse from an iterator, returning an error on failure
    pub fn try_parse_from_args<I, T>(args: I) -> Result<Self, clap::Error>
    where
        I: IntoIterator<Item = T>,
        T: Into<std::ffi::OsString> + Clone,
    {
        Self::try_parse_from(args)
    }

    /// Get the shell path, falling back to /bin/bash if not specified
    #[must_use]
    pub fn shell_path(&self) -> PathBuf {
        self.shell
            .clone()
            .unwrap_or_else(|| PathBuf::from("/bin/bash"))
    }

    /// Check if standalone mode is requested (via --standalone or --no-daemon)
    #[must_use]
    pub const fn is_standalone(&self) -> bool {
        self.standalone || self.no_daemon
    }

    /// Check if passthrough mode is requested (via --passthrough or --force-non-tty)
    #[must_use]
    pub const fn is_passthrough(&self) -> bool {
        self.passthrough || self.force_non_tty
    }

    /// Check if debug mode is enabled
    #[must_use]
    pub const fn is_debug(&self) -> bool {
        self.debug
    }

    /// Check if the picker UI should be enabled
    #[must_use]
    pub const fn ui_enabled(&self) -> bool {
        !self.no_ui && !self.is_passthrough()
    }

    /// Check if daemon connection should be attempted
    #[must_use]
    pub const fn daemon_enabled(&self) -> bool {
        !self.is_standalone() && !self.is_passthrough()
    }

    /// Get the effective operation mode
    #[must_use]
    pub const fn operation_mode(&self) -> OperationMode {
        if self.is_passthrough() {
            OperationMode::Passthrough
        } else if self.is_standalone() {
            OperationMode::Standalone
        } else {
            OperationMode::Full
        }
    }

    /// Validate the CLI arguments, returning an error if invalid
    pub fn validate(&self) -> Result<(), CliError> {
        // Check for conflicting options
        if self.is_passthrough() && !self.no_ui {
            // Passthrough mode implies no UI, but this isn't an error
            // The UI will be disabled automatically
        }

        // Validate buffer capacity
        if self.buffer_cap == 0 {
            return Err(CliError::InvalidBufferCap(
                "buffer capacity must be greater than 0".to_string(),
            ));
        }

        // Validate shell path if specified
        if let Some(ref shell) = self.shell {
            if shell.as_os_str().is_empty() {
                return Err(CliError::InvalidShellPath(
                    "shell path cannot be empty".to_string(),
                ));
            }
        }

        // Validate hotkey if specified
        if let Some(ref hotkey) = self.hotkey {
            if hotkey.is_empty() {
                return Err(CliError::InvalidHotkey(
                    "hotkey cannot be empty".to_string(),
                ));
            }
        }

        Ok(())
    }
}

/// Operation mode for clai-wrap
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OperationMode {
    /// Full mode: daemon connection, UI, and all features
    Full,
    /// Standalone mode: no daemon, UI with local history only
    Standalone,
    /// Passthrough mode: pure passthrough, no UI, no hotkey
    Passthrough,
}

impl std::fmt::Display for OperationMode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Full => write!(f, "full"),
            Self::Standalone => write!(f, "standalone"),
            Self::Passthrough => write!(f, "passthrough"),
        }
    }
}

/// CLI-related errors
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum CliError {
    /// Invalid buffer capacity
    #[error("invalid buffer capacity: {0}")]
    InvalidBufferCap(String),

    /// Invalid shell path
    #[error("invalid shell path: {0}")]
    InvalidShellPath(String),

    /// Invalid hotkey specification
    #[error("invalid hotkey: {0}")]
    InvalidHotkey(String),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_args() {
        let cli = Cli::parse_from_args(["clai-wrap"]);
        // Note: cli.shell may be set from SHELL env var, so we test other defaults
        assert!(cli.login_shell);
        assert!(cli.hotkey.is_none());
        assert_eq!(cli.buffer_cap, DEFAULT_BUFFER_CAP);
        assert!(!cli.execute_on_select);
        assert!(!cli.no_daemon);
        assert!(!cli.standalone);
        assert!(!cli.no_ui);
        assert!(!cli.force_non_tty);
        assert!(!cli.passthrough);
        // Note: cli.debug may be set from CLAI_DEBUG env var
        assert!(cli.command.is_none());
    }

    #[test]
    fn test_shell_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--shell", "/bin/zsh"]);
        assert_eq!(cli.shell, Some(PathBuf::from("/bin/zsh")));
        assert_eq!(cli.shell_path(), PathBuf::from("/bin/zsh"));
    }

    #[test]
    fn test_shell_option_short() {
        let cli = Cli::parse_from_args(["clai-wrap", "-s", "/bin/fish"]);
        assert_eq!(cli.shell, Some(PathBuf::from("/bin/fish")));
    }

    #[test]
    fn test_shell_path_fallback_no_env() {
        // Test the shell_path method when shell is explicitly None
        let mut cli = Cli::parse_from_args(["clai-wrap"]);
        cli.shell = None;
        assert_eq!(cli.shell_path(), PathBuf::from("/bin/bash"));
    }

    #[test]
    fn test_shell_path_returns_shell_when_set() {
        // Test that shell_path returns the shell when explicitly set
        let cli = Cli::parse_from_args(["clai-wrap", "--shell", "/usr/local/bin/fish"]);
        assert_eq!(cli.shell_path(), PathBuf::from("/usr/local/bin/fish"));
    }

    #[test]
    fn test_standalone_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--standalone"]);
        assert!(cli.standalone);
        assert!(cli.is_standalone());
        assert_eq!(cli.operation_mode(), OperationMode::Standalone);
    }

    #[test]
    fn test_no_daemon_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--no-daemon"]);
        assert!(cli.no_daemon);
        assert!(cli.is_standalone());
        assert_eq!(cli.operation_mode(), OperationMode::Standalone);
    }

    #[test]
    fn test_passthrough_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--passthrough"]);
        assert!(cli.passthrough);
        assert!(cli.is_passthrough());
        assert_eq!(cli.operation_mode(), OperationMode::Passthrough);
    }

    #[test]
    fn test_force_non_tty_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--force-non-tty"]);
        assert!(cli.force_non_tty);
        assert!(cli.is_passthrough());
        assert_eq!(cli.operation_mode(), OperationMode::Passthrough);
    }

    #[test]
    fn test_debug_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--debug"]);
        assert!(cli.debug);
        assert!(cli.is_debug());
    }

    #[test]
    fn test_no_ui_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--no-ui"]);
        assert!(cli.no_ui);
        assert!(!cli.ui_enabled());
    }

    #[test]
    fn test_ui_disabled_in_passthrough() {
        let cli = Cli::parse_from_args(["clai-wrap", "--passthrough"]);
        assert!(!cli.ui_enabled());
    }

    #[test]
    fn test_daemon_disabled_in_standalone() {
        let cli = Cli::parse_from_args(["clai-wrap", "--standalone"]);
        assert!(!cli.daemon_enabled());
    }

    #[test]
    fn test_daemon_disabled_in_passthrough() {
        let cli = Cli::parse_from_args(["clai-wrap", "--passthrough"]);
        assert!(!cli.daemon_enabled());
    }

    #[test]
    fn test_buffer_cap_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--buffer-cap", "4194304"]);
        assert_eq!(cli.buffer_cap, 4_194_304);
    }

    #[test]
    fn test_execute_on_select_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--execute-on-select"]);
        assert!(cli.execute_on_select);
    }

    #[test]
    fn test_login_shell_flag() {
        let cli = Cli::parse_from_args(["clai-wrap", "--login-shell"]);
        assert!(cli.login_shell);
    }

    #[test]
    fn test_hotkey_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--hotkey", "ctrl-\\ h"]);
        assert_eq!(cli.hotkey, Some("ctrl-\\ h".to_string()));
    }

    #[test]
    fn test_history_file_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--history-file", "/custom/history"]);
        assert_eq!(cli.history_file, Some(PathBuf::from("/custom/history")));
    }

    #[test]
    fn test_daemon_socket_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--daemon-socket", "/tmp/clai.sock"]);
        assert_eq!(cli.daemon_socket, Some(PathBuf::from("/tmp/clai.sock")));
    }

    #[test]
    fn test_daemon_timeout_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--daemon-timeout", "1000"]);
        assert_eq!(cli.daemon_timeout, 1000);
    }

    #[test]
    fn test_hotkey_timeout_option() {
        let cli = Cli::parse_from_args(["clai-wrap", "--hotkey-timeout", "750"]);
        assert_eq!(cli.hotkey_timeout, 750);
    }

    #[test]
    fn test_version_subcommand() {
        let cli = Cli::parse_from_args(["clai-wrap", "version"]);
        assert_eq!(cli.command, Some(Commands::Version));
    }

    #[test]
    fn test_reset_terminal_subcommand() {
        let cli = Cli::parse_from_args(["clai-wrap", "reset-terminal"]);
        assert_eq!(cli.command, Some(Commands::ResetTerminal));
    }

    #[test]
    fn test_combined_options() {
        let cli = Cli::parse_from_args([
            "clai-wrap",
            "--shell",
            "/bin/zsh",
            "--standalone",
            "--debug",
            "--buffer-cap",
            "1048576",
        ]);
        assert_eq!(cli.shell, Some(PathBuf::from("/bin/zsh")));
        assert!(cli.standalone);
        assert!(cli.debug);
        assert_eq!(cli.buffer_cap, 1_048_576);
        assert!(cli.is_standalone());
        assert!(cli.is_debug());
        assert_eq!(cli.operation_mode(), OperationMode::Standalone);
    }

    #[test]
    fn test_validate_success() {
        let cli = Cli::parse_from_args(["clai-wrap"]);
        assert!(cli.validate().is_ok());
    }

    #[test]
    fn test_validate_zero_buffer_cap() {
        let mut cli = Cli::parse_from_args(["clai-wrap"]);
        cli.buffer_cap = 0;
        let result = cli.validate();
        assert!(result.is_err());
        assert!(matches!(result.unwrap_err(), CliError::InvalidBufferCap(_)));
    }

    #[test]
    fn test_validate_empty_shell() {
        let mut cli = Cli::parse_from_args(["clai-wrap"]);
        cli.shell = Some(PathBuf::from(""));
        let result = cli.validate();
        assert!(result.is_err());
        assert!(matches!(result.unwrap_err(), CliError::InvalidShellPath(_)));
    }

    #[test]
    fn test_validate_empty_hotkey() {
        let mut cli = Cli::parse_from_args(["clai-wrap"]);
        cli.hotkey = Some(String::new());
        let result = cli.validate();
        assert!(result.is_err());
        assert!(matches!(result.unwrap_err(), CliError::InvalidHotkey(_)));
    }

    #[test]
    fn test_operation_mode_full() {
        let cli = Cli::parse_from_args(["clai-wrap"]);
        assert_eq!(cli.operation_mode(), OperationMode::Full);
    }

    #[test]
    fn test_operation_mode_display() {
        assert_eq!(format!("{}", OperationMode::Full), "full");
        assert_eq!(format!("{}", OperationMode::Standalone), "standalone");
        assert_eq!(format!("{}", OperationMode::Passthrough), "passthrough");
    }

    #[test]
    fn test_cli_error_display() {
        let err = CliError::InvalidBufferCap("test".to_string());
        assert_eq!(err.to_string(), "invalid buffer capacity: test");

        let err = CliError::InvalidShellPath("test".to_string());
        assert_eq!(err.to_string(), "invalid shell path: test");

        let err = CliError::InvalidHotkey("test".to_string());
        assert_eq!(err.to_string(), "invalid hotkey: test");
    }

    #[test]
    fn test_try_parse_invalid_args() {
        let result = Cli::try_parse_from_args(["clai-wrap", "--invalid-option"]);
        assert!(result.is_err());
    }

    #[test]
    fn test_try_parse_valid_args() {
        let result = Cli::try_parse_from_args(["clai-wrap", "--debug"]);
        assert!(result.is_ok());
        assert!(result.unwrap().debug);
    }
}
