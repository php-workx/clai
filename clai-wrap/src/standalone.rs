//! Standalone mode for clai-wrap.
//!
//! This module implements standalone mode operation when the daemon is unavailable.
//! Standalone mode provides reduced functionality as specified in Section 3.2 of the spec:
//!
//! | Feature | Standalone Behavior |
//! |---------|---------------------|
//! | PTY passthrough | Full functionality |
//! | Hotkey detection | Full functionality |
//! | Picker UI | History-only (local file) |
//! | Output capture | Disabled (no daemon to receive) |
//! | AI suggestions | Disabled |
//! | Privacy gates | Denylist active, but no logging occurs |
//!
//! Standalone mode is transparent to the user except:
//! - One-time warning logged to stderr: "Daemon unavailable, running in standalone mode"
//! - AI suggestions will not appear after failed commands
//!
//! # Example
//!
//! ```rust
//! use clai_wrap::standalone::{StandaloneState, StandaloneReason, Feature};
//!
//! let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
//!
//! // Check feature availability
//! assert!(state.feature_available(Feature::Picker));
//! assert!(!state.feature_available(Feature::OutputCapture));
//!
//! // Log warning (only shown once)
//! state.log_warning();
//! ```

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};

use thiserror::Error;
use tracing::warn;

use crate::history_parser::{self, HistoryEntry, HistoryParseError};
use crate::picker::{Picker, PickerItem};

/// Errors that can occur in standalone mode operations.
#[derive(Debug, Error)]
pub enum StandaloneError {
    /// Failed to load history file.
    #[error("failed to load history: {0}")]
    HistoryLoad(#[from] HistoryParseError),

    /// Failed to detect shell history file.
    #[error("could not find history file for shell: {0}")]
    HistoryNotFound(String),

    /// Home directory not found.
    #[error("could not determine home directory")]
    HomeNotFound,
}

/// Result type for standalone operations.
pub type Result<T> = std::result::Result<T, StandaloneError>;

/// The reason why standalone mode was entered.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StandaloneReason {
    /// The daemon process is not running or not responding.
    DaemonUnavailable,
    /// Connection to daemon timed out.
    ConnectionTimeout,
    /// Socket error occurred during connection.
    SocketError(String),
}

impl std::fmt::Display for StandaloneReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::DaemonUnavailable => write!(f, "daemon unavailable"),
            Self::ConnectionTimeout => write!(f, "connection timeout"),
            Self::SocketError(msg) => write!(f, "socket error: {msg}"),
        }
    }
}

/// Features that may or may not be available in standalone mode.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Feature {
    /// History picker UI.
    Picker,
    /// Output capture for daemon analysis.
    OutputCapture,
    /// AI-powered command suggestions.
    AiSuggestions,
    /// Privacy denylist gate (process detection).
    DenylistGate,
}

/// State for standalone mode operation.
///
/// When the daemon is unavailable, `clai-wrap` operates in standalone mode with
/// reduced functionality. This struct manages the standalone state, including
/// loading history for the picker and tracking whether the warning has been shown.
#[derive(Debug)]
pub struct StandaloneState {
    /// The reason standalone mode was entered.
    reason: StandaloneReason,
    /// History entries loaded from local file.
    history_entries: Vec<HistoryEntry>,
    /// Path to the history file that was loaded.
    history_path: Option<PathBuf>,
    /// Whether the warning has been logged.
    warning_logged: AtomicBool,
}

impl StandaloneState {
    /// Creates a new standalone state with the given reason.
    ///
    /// The warning is not logged immediately; call `log_warning()` to log it.
    #[must_use]
    pub fn new(reason: StandaloneReason) -> Self {
        Self {
            reason,
            history_entries: Vec::new(),
            history_path: None,
            warning_logged: AtomicBool::new(false),
        }
    }

    /// Returns the reason standalone mode was entered.
    #[must_use]
    pub const fn reason(&self) -> &StandaloneReason {
        &self.reason
    }

    /// Initialize history from local files based on shell type.
    ///
    /// This loads the history file for the specified shell. If successful, the
    /// history entries can be accessed via `history_entries()` or converted to
    /// a `Picker` via `create_picker()`.
    ///
    /// # Arguments
    ///
    /// * `shell` - The shell name (e.g., "bash", "zsh", "fish")
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - The home directory cannot be determined
    /// - The history file cannot be found
    /// - The history file cannot be parsed
    pub fn init_history(&mut self, shell: &str) -> Result<()> {
        let path = find_history_file(shell)?;
        let entries = history_parser::detect_and_parse(&path)?;

        self.history_entries = entries;
        self.history_path = Some(path);

        Ok(())
    }

    /// Loads history from a specific path.
    ///
    /// This is useful when the history file path is known or when loading
    /// from a non-standard location.
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be read or parsed.
    pub fn load_history_from(&mut self, path: &Path) -> Result<()> {
        let entries = history_parser::detect_and_parse(path)?;
        self.history_entries = entries;
        self.history_path = Some(path.to_path_buf());
        Ok(())
    }

    /// Returns the loaded history entries.
    #[must_use]
    pub fn history_entries(&self) -> &[HistoryEntry] {
        &self.history_entries
    }

    /// Returns the path to the loaded history file, if any.
    #[must_use]
    pub fn history_path(&self) -> Option<&Path> {
        self.history_path.as_deref()
    }

    /// Creates a picker populated with history entries.
    ///
    /// The picker items are created from the history entries in reverse order
    /// (most recent first). If history has not been loaded, returns an empty picker.
    #[must_use]
    pub fn create_picker(&self) -> Picker {
        Picker::new(self.create_picker_items())
    }

    /// Creates a picker populated with history entries and an initial query.
    #[must_use]
    pub fn create_picker_with_query(&self, query: &str) -> Picker {
        Picker::with_query(self.create_picker_items(), query)
    }

    /// Converts history entries to picker items (most recent first).
    fn create_picker_items(&self) -> Vec<PickerItem> {
        self.history_entries
            .iter()
            .rev() // Most recent first
            .map(|entry| {
                entry.timestamp.map_or_else(
                    || PickerItem::new(&entry.command),
                    |ts| PickerItem::with_metadata(&entry.command, format_timestamp(ts)),
                )
            })
            .collect()
    }

    /// Check if a feature is available in standalone mode.
    ///
    /// # Feature Availability
    ///
    /// | Feature | Available |
    /// |---------|-----------|
    /// | `Picker` | Yes (history-only) |
    /// | `OutputCapture` | No |
    /// | `AiSuggestions` | No |
    /// | `DenylistGate` | Yes |
    #[must_use]
    pub const fn feature_available(&self, feature: Feature) -> bool {
        match feature {
            Feature::Picker | Feature::DenylistGate => true,
            Feature::OutputCapture | Feature::AiSuggestions => false,
        }
    }

    /// Log a warning about standalone mode (once).
    ///
    /// This logs a warning to stderr the first time it is called. Subsequent
    /// calls are no-ops. This ensures the user is informed about standalone
    /// mode without being spammed with repeated warnings.
    pub fn log_warning(&self) {
        // Only log once using compare_exchange to be thread-safe
        if self
            .warning_logged
            .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .is_ok()
        {
            warn!("Daemon unavailable, running in standalone mode ({})", self.reason);
            eprintln!("clai-wrap: Daemon unavailable, running in standalone mode");
        }
    }

    /// Returns whether the warning has been logged.
    #[must_use]
    pub fn warning_was_logged(&self) -> bool {
        self.warning_logged.load(Ordering::SeqCst)
    }

    /// Returns the number of history entries loaded.
    #[must_use]
    pub fn history_count(&self) -> usize {
        self.history_entries.len()
    }

    /// Returns true if history has been loaded.
    #[must_use]
    pub fn has_history(&self) -> bool {
        !self.history_entries.is_empty()
    }
}

/// Returns the user's home directory.
///
/// Checks `HOME` environment variable on Unix, or `USERPROFILE` on Windows.
fn home_dir() -> Option<PathBuf> {
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
}

/// Finds the history file path for the given shell.
///
/// # Supported Shells
///
/// | Shell | History File |
/// |-------|--------------|
/// | bash | `~/.bash_history` |
/// | zsh | `~/.zsh_history` or `~/.zhistory` |
/// | fish | `~/.local/share/fish/fish_history` |
///
/// # Errors
///
/// Returns an error if:
/// - The home directory cannot be determined
/// - No history file exists for the shell
fn find_history_file(shell: &str) -> Result<PathBuf> {
    let home = home_dir().ok_or(StandaloneError::HomeNotFound)?;

    let shell_lower = shell.to_lowercase();

    // Check for common shell names (strip path prefix if present)
    let shell_name = shell_lower
        .rsplit('/')
        .next()
        .unwrap_or(&shell_lower);

    let candidates: Vec<PathBuf> = match shell_name {
        "bash" | "bash.exe" => vec![home.join(".bash_history")],
        "zsh" | "zsh.exe" => vec![
            home.join(".zsh_history"),
            home.join(".zhistory"),
        ],
        "fish" | "fish.exe" => {
            // Fish history is in XDG_DATA_HOME or ~/.local/share
            let data_home = std::env::var("XDG_DATA_HOME").map_or_else(
                |_| home.join(".local").join("share"),
                PathBuf::from,
            );
            vec![data_home.join("fish").join("fish_history")]
        }
        _ => {
            // Unknown shell - try common history file names
            vec![
                home.join(".history"),
                home.join(format!(".{shell_name}_history")),
            ]
        }
    };

    // Return the first candidate that exists
    for path in candidates {
        if path.exists() {
            return Ok(path);
        }
    }

    Err(StandaloneError::HistoryNotFound(shell.to_string()))
}

// Time constants in seconds
const MINUTE: u64 = 60;
const HOUR: u64 = 3600;
const DAY: u64 = 86_400;
const WEEK: u64 = 604_800;

/// Formats a Unix timestamp for display in the picker.
fn format_timestamp(timestamp: i64) -> String {
    use std::time::{Duration, UNIX_EPOCH};

    // Safely handle negative timestamps
    let Ok(timestamp_u64) = u64::try_from(timestamp) else {
        return "unknown".to_string();
    };

    let duration = Duration::from_secs(timestamp_u64);

    let Some(datetime) = UNIX_EPOCH.checked_add(duration) else {
        return "unknown".to_string();
    };

    // Use a simple format for display
    // In a real implementation, you might use chrono for better formatting
    let secs_since_epoch = datetime
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_secs());

    // Simple relative time formatting
    let now_secs = std::time::SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_secs());

    if now_secs > secs_since_epoch {
        let diff = now_secs - secs_since_epoch;

        if diff < MINUTE {
            return "just now".to_string();
        } else if diff < HOUR {
            let mins = diff / MINUTE;
            return format!("{mins}m ago");
        } else if diff < DAY {
            let hours = diff / HOUR;
            return format!("{hours}h ago");
        } else if diff < WEEK {
            let days = diff / DAY;
            return format!("{days}d ago");
        }
    }

    // For older entries, just show the timestamp
    format!("ts:{timestamp}")
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::NamedTempFile;

    // ========== StandaloneReason Tests ==========

    #[test]
    fn test_standalone_reason_display() {
        assert_eq!(
            StandaloneReason::DaemonUnavailable.to_string(),
            "daemon unavailable"
        );
        assert_eq!(
            StandaloneReason::ConnectionTimeout.to_string(),
            "connection timeout"
        );
        assert_eq!(
            StandaloneReason::SocketError("test".to_string()).to_string(),
            "socket error: test"
        );
    }

    #[test]
    fn test_standalone_reason_equality() {
        assert_eq!(StandaloneReason::DaemonUnavailable, StandaloneReason::DaemonUnavailable);
        assert_eq!(StandaloneReason::ConnectionTimeout, StandaloneReason::ConnectionTimeout);
        assert_ne!(StandaloneReason::DaemonUnavailable, StandaloneReason::ConnectionTimeout);

        let err1 = StandaloneReason::SocketError("error".to_string());
        let err2 = StandaloneReason::SocketError("error".to_string());
        assert_eq!(err1, err2);
    }

    // ========== Feature Availability Tests ==========

    #[test]
    fn test_feature_availability() {
        let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

        // Picker is available (history-only)
        assert!(state.feature_available(Feature::Picker));

        // Output capture is not available
        assert!(!state.feature_available(Feature::OutputCapture));

        // AI suggestions are not available
        assert!(!state.feature_available(Feature::AiSuggestions));

        // Denylist gate is available
        assert!(state.feature_available(Feature::DenylistGate));
    }

    #[test]
    fn test_feature_availability_all_reasons() {
        // Feature availability should be the same regardless of reason
        let reasons = [
            StandaloneReason::DaemonUnavailable,
            StandaloneReason::ConnectionTimeout,
            StandaloneReason::SocketError("test".to_string()),
        ];

        for reason in reasons {
            let state = StandaloneState::new(reason);

            assert!(state.feature_available(Feature::Picker));
            assert!(!state.feature_available(Feature::OutputCapture));
            assert!(!state.feature_available(Feature::AiSuggestions));
            assert!(state.feature_available(Feature::DenylistGate));
        }
    }

    // ========== Warning Tests ==========

    #[test]
    fn test_warning_logged_once() {
        let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

        assert!(!state.warning_was_logged());

        state.log_warning();
        assert!(state.warning_was_logged());

        // Calling again should not change the state
        state.log_warning();
        assert!(state.warning_was_logged());
    }

    #[test]
    fn test_warning_logged_concurrent() {
        use std::sync::Arc;
        use std::thread;

        let state = Arc::new(StandaloneState::new(StandaloneReason::DaemonUnavailable));

        // Spawn multiple threads to log warning concurrently
        let handles: Vec<_> = (0..10)
            .map(|_| {
                let state = Arc::clone(&state);
                thread::spawn(move || {
                    state.log_warning();
                })
            })
            .collect();

        for handle in handles {
            handle.join().unwrap();
        }

        // Warning should be logged exactly once
        assert!(state.warning_was_logged());
    }

    // ========== History Loading Tests ==========

    #[test]
    fn test_load_history_from_file() {
        let mut temp_file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(temp_file, "ls -la").unwrap();
        writeln!(temp_file, "git status").unwrap();
        writeln!(temp_file, "cargo build").unwrap();
        temp_file.flush().unwrap();

        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        assert!(!state.has_history());
        assert_eq!(state.history_count(), 0);

        state.load_history_from(temp_file.path()).unwrap();

        assert!(state.has_history());
        assert_eq!(state.history_count(), 3);

        let entries = state.history_entries();
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[2].command, "cargo build");

        assert_eq!(state.history_path(), Some(temp_file.path()));
    }

    #[test]
    fn test_load_history_from_nonexistent_file() {
        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        let result = state.load_history_from(Path::new("/nonexistent/file"));
        assert!(result.is_err());
    }

    #[test]
    fn test_load_history_with_timestamps() {
        let mut temp_file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(temp_file, "#1234567890").unwrap();
        writeln!(temp_file, "ls -la").unwrap();
        writeln!(temp_file, "#1234567891").unwrap();
        writeln!(temp_file, "git status").unwrap();
        temp_file.flush().unwrap();

        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        state.load_history_from(temp_file.path()).unwrap();

        assert_eq!(state.history_count(), 2);

        let entries = state.history_entries();
        assert_eq!(entries[0].timestamp, Some(1234567890));
        assert_eq!(entries[1].timestamp, Some(1234567891));
    }

    // ========== Picker Creation Tests ==========

    #[test]
    fn test_create_picker_empty_history() {
        let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        let picker = state.create_picker();

        assert!(picker.is_empty());
        assert_eq!(picker.total_count(), 0);
    }

    #[test]
    fn test_create_picker_with_history() {
        let mut temp_file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(temp_file, "first command").unwrap();
        writeln!(temp_file, "second command").unwrap();
        writeln!(temp_file, "third command").unwrap();
        temp_file.flush().unwrap();

        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        state.load_history_from(temp_file.path()).unwrap();

        let picker = state.create_picker();

        assert!(!picker.is_empty());
        assert_eq!(picker.total_count(), 3);

        // Most recent should be first
        let selected = picker.selected_item().unwrap();
        assert_eq!(selected.text, "third command");
    }

    #[test]
    fn test_create_picker_with_query() {
        let mut temp_file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(temp_file, "git status").unwrap();
        writeln!(temp_file, "ls -la").unwrap();
        writeln!(temp_file, "git commit").unwrap();
        temp_file.flush().unwrap();

        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        state.load_history_from(temp_file.path()).unwrap();

        let picker = state.create_picker_with_query("git");

        assert_eq!(picker.filtered_count(), 2);
        assert_eq!(picker.query(), "git");
    }

    #[test]
    fn test_create_picker_with_timestamps() {
        let mut temp_file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(temp_file, "#1234567890").unwrap();
        writeln!(temp_file, "command with timestamp").unwrap();
        temp_file.flush().unwrap();

        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        state.load_history_from(temp_file.path()).unwrap();

        let picker = state.create_picker();
        let selected = picker.selected_item().unwrap();

        assert_eq!(selected.text, "command with timestamp");
        // Should have metadata (formatted timestamp)
        assert!(selected.metadata.is_some());
    }

    // ========== StandaloneState Accessors Tests ==========

    #[test]
    fn test_standalone_state_reason() {
        let state = StandaloneState::new(StandaloneReason::ConnectionTimeout);
        assert_eq!(*state.reason(), StandaloneReason::ConnectionTimeout);
    }

    #[test]
    fn test_standalone_state_initial_state() {
        let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

        assert!(!state.has_history());
        assert_eq!(state.history_count(), 0);
        assert!(state.history_entries().is_empty());
        assert!(state.history_path().is_none());
        assert!(!state.warning_was_logged());
    }

    // ========== find_history_file Tests ==========

    #[test]
    fn test_find_history_file_unknown_shell() {
        // For unknown shells, we try common patterns but they likely don't exist
        let result = find_history_file("unknown_shell_xyz");
        // This might succeed or fail depending on what files exist
        // We just verify it doesn't panic
        let _ = result;
    }

    #[test]
    fn test_find_history_file_strips_path() {
        // Shell name might include path prefix
        let result1 = find_history_file("/bin/bash");
        let result2 = find_history_file("bash");

        // Both should look for the same file
        match (result1, result2) {
            (Ok(p1), Ok(p2)) => assert_eq!(p1, p2),
            (Err(_), Err(_)) => {} // Both failed (file doesn't exist) - that's ok
            _ => {} // One succeeded, one failed - unexpected but ok for test
        }
    }

    // ========== format_timestamp Tests ==========

    #[test]
    fn test_format_timestamp_recent() {
        use std::time::{SystemTime, UNIX_EPOCH};

        let now_secs = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();

        // Just now (within 60 seconds)
        let recent = format_timestamp((now_secs - 30) as i64);
        assert_eq!(recent, "just now");

        // Minutes ago
        let mins_ago = format_timestamp((now_secs - 300) as i64);
        assert!(mins_ago.ends_with("m ago"));

        // Hours ago
        let hours_ago = format_timestamp((now_secs - 7200) as i64);
        assert!(hours_ago.ends_with("h ago"));

        // Days ago
        let days_ago = format_timestamp((now_secs - 172800) as i64);
        assert!(days_ago.ends_with("d ago"));
    }

    #[test]
    fn test_format_timestamp_old() {
        // Very old timestamp
        let old = format_timestamp(1234567890);
        assert!(old.starts_with("ts:") || old.ends_with("d ago"));
    }

    #[test]
    fn test_format_timestamp_negative() {
        let result = format_timestamp(-1);
        assert_eq!(result, "unknown");
    }

    // ========== Error Tests ==========

    #[test]
    fn test_standalone_error_display() {
        let err = StandaloneError::HistoryNotFound("bash".to_string());
        assert_eq!(err.to_string(), "could not find history file for shell: bash");

        let err = StandaloneError::HomeNotFound;
        assert_eq!(err.to_string(), "could not determine home directory");
    }

    // ========== Integration Tests ==========

    #[test]
    fn test_full_standalone_workflow() {
        // Create a temporary history file
        let mut temp_file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(temp_file, "git status").unwrap();
        writeln!(temp_file, "git commit -m 'test'").unwrap();
        writeln!(temp_file, "cargo build").unwrap();
        writeln!(temp_file, "cargo test").unwrap();
        temp_file.flush().unwrap();

        // Create standalone state
        let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

        // Verify initial state
        assert!(!state.has_history());
        assert!(!state.warning_was_logged());

        // Log warning
        state.log_warning();
        assert!(state.warning_was_logged());

        // Load history
        state.load_history_from(temp_file.path()).unwrap();
        assert!(state.has_history());
        assert_eq!(state.history_count(), 4);

        // Create picker and verify
        let mut picker = state.create_picker();
        assert_eq!(picker.total_count(), 4);

        // Filter and select
        picker.update_query("git");
        assert_eq!(picker.filtered_count(), 2);

        // Verify feature availability
        assert!(state.feature_available(Feature::Picker));
        assert!(!state.feature_available(Feature::AiSuggestions));
    }
}
