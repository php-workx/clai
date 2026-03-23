//! History-backed picker for clai-wrap.
//!
//! This module integrates the Picker UI with shell history data, providing
//! an interactive command history browser.
//!
//! # Example
//!
//! ```rust,no_run
//! use clai_wrap::history_picker::HistoryPicker;
//!
//! // Create from shell's default history
//! let picker = HistoryPicker::from_default_history("zsh").unwrap();
//!
//! // Or create from explicit history entries
//! use clai_wrap::history_parser::HistoryEntry;
//! let entries = vec![
//!     HistoryEntry::new("git status"),
//!     HistoryEntry::new("ls -la"),
//! ];
//! let picker = HistoryPicker::from_history(entries);
//! ```

use std::collections::HashSet;
use std::path::{Path, PathBuf};

use thiserror::Error;

use crate::history_parser::{detect_and_parse, HistoryEntry, HistoryParseError};
use crate::picker::{Picker, PickerItem};

/// Default maximum number of history entries to load.
const DEFAULT_MAX_ENTRIES: usize = 10_000;

/// Errors that can occur when creating a `HistoryPicker`.
#[derive(Debug, Error)]
pub enum HistoryPickerError {
    /// Failed to parse history file.
    #[error("failed to parse history: {0}")]
    ParseError(#[from] HistoryParseError),

    /// Could not determine home directory.
    #[error("could not determine home directory")]
    NoHomeDirectory,

    /// Unsupported shell.
    #[error("unsupported shell: {0}")]
    UnsupportedShell(String),

    /// History file not found.
    #[error("history file not found: {0}")]
    FileNotFound(PathBuf),
}

/// An interactive picker backed by shell history.
///
/// This struct wraps a `Picker` and provides convenient methods for
/// loading history from files and converting history entries to picker items.
#[derive(Debug)]
pub struct HistoryPicker {
    /// The underlying picker UI.
    picker: Picker,
    /// The original history entries (for reference).
    history_entries: Vec<HistoryEntry>,
}

impl HistoryPicker {
    /// Creates a new `HistoryPicker` from a vector of history entries.
    ///
    /// The entries are processed as follows:
    /// 1. Reversed to show most recent first
    /// 2. Consecutive duplicates removed
    /// 3. Limited to `DEFAULT_MAX_ENTRIES`
    /// 4. Converted to `PickerItem`s
    #[must_use]
    #[allow(clippy::needless_pass_by_value)]
    pub fn from_history(entries: Vec<HistoryEntry>) -> Self {
        let processed = process_history_entries(&entries, DEFAULT_MAX_ENTRIES);
        let items = entries_to_picker_items(&processed);
        let picker = Picker::new(items);

        Self {
            picker,
            history_entries: processed,
        }
    }

    /// Creates a new `HistoryPicker` from a vector of history entries with an initial query.
    ///
    /// The entries are processed the same as `from_history`, and the picker
    /// is initialized with the given search query.
    #[must_use]
    #[allow(clippy::needless_pass_by_value)]
    pub fn from_history_with_query(entries: Vec<HistoryEntry>, query: impl Into<String>) -> Self {
        let processed = process_history_entries(&entries, DEFAULT_MAX_ENTRIES);
        let items = entries_to_picker_items(&processed);
        let picker = Picker::with_query(items, query);

        Self {
            picker,
            history_entries: processed,
        }
    }

    /// Creates a new `HistoryPicker` from a history file.
    ///
    /// The file format is auto-detected based on path and content.
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be read or parsed.
    pub fn from_file(path: &Path) -> Result<Self, HistoryPickerError> {
        if !path.exists() {
            return Err(HistoryPickerError::FileNotFound(path.to_path_buf()));
        }

        let entries = detect_and_parse(path)?;
        Ok(Self::from_history(entries))
    }

    /// Creates a new `HistoryPicker` from a history file with an initial query.
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be read or parsed.
    pub fn from_file_with_query(
        path: &Path,
        query: impl Into<String>,
    ) -> Result<Self, HistoryPickerError> {
        if !path.exists() {
            return Err(HistoryPickerError::FileNotFound(path.to_path_buf()));
        }

        let entries = detect_and_parse(path)?;
        Ok(Self::from_history_with_query(entries, query))
    }

    /// Creates a new `HistoryPicker` from the default history file for the given shell.
    ///
    /// Supported shells:
    /// - `bash`: `~/.bash_history`
    /// - `zsh`: `~/.zsh_history`
    /// - `fish`: `~/.local/share/fish/fish_history`
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - The shell is not supported
    /// - The home directory cannot be determined
    /// - The history file cannot be read or parsed
    pub fn from_default_history(shell: &str) -> Result<Self, HistoryPickerError> {
        let path = default_history_path(shell)?;
        Self::from_file(&path)
    }

    /// Creates a new `HistoryPicker` from the default history file with an initial query.
    ///
    /// # Errors
    ///
    /// Returns an error if the history file cannot be loaded.
    pub fn from_default_history_with_query(
        shell: &str,
        query: impl Into<String>,
    ) -> Result<Self, HistoryPickerError> {
        let path = default_history_path(shell)?;
        Self::from_file_with_query(&path, query)
    }

    /// Returns the currently selected command, if any.
    #[must_use]
    pub fn selected_command(&self) -> Option<&str> {
        self.picker.selected_item().map(|item| item.text.as_str())
    }

    /// Returns the currently selected history entry, if any.
    #[must_use]
    pub fn selected_entry(&self) -> Option<&HistoryEntry> {
        // The picker's filtered indices may not match our history_entries indices
        // when a filter is active. We need to map through the picker's selected item.
        self.picker
            .selected_item()
            .and_then(|item| self.history_entries.iter().find(|e| e.command == item.text))
    }

    /// Moves the selection to the previous item (up).
    pub const fn select_prev(&mut self) {
        self.picker.select_prev();
    }

    /// Moves the selection to the next item (down).
    pub const fn select_next(&mut self) {
        self.picker.select_next();
    }

    /// Updates the search query and filters the history.
    pub fn update_query(&mut self, query: &str) {
        self.picker.update_query(query);
    }

    /// Appends a character to the search query.
    pub fn push_char(&mut self, c: char) {
        self.picker.push_char(c);
    }

    /// Removes the last character from the search query (backspace).
    pub fn pop_char(&mut self) {
        self.picker.pop_char();
    }

    /// Returns the current search query.
    #[must_use]
    pub fn query(&self) -> &str {
        self.picker.query()
    }

    /// Returns the number of items matching the current filter.
    #[must_use]
    pub const fn filtered_count(&self) -> usize {
        self.picker.filtered_count()
    }

    /// Returns the total number of history entries.
    #[must_use]
    pub const fn total_count(&self) -> usize {
        self.picker.total_count()
    }

    /// Returns true if there are no history entries.
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.picker.is_empty()
    }

    /// Returns true if no items match the current filter.
    #[must_use]
    pub const fn is_filtered_empty(&self) -> bool {
        self.picker.is_filtered_empty()
    }

    /// Returns a reference to the underlying picker for rendering.
    #[must_use]
    pub const fn picker(&self) -> &Picker {
        &self.picker
    }

    /// Returns a mutable reference to the underlying picker for rendering.
    pub const fn picker_mut(&mut self) -> &mut Picker {
        &mut self.picker
    }

    /// Returns the history entries.
    #[must_use]
    pub fn history_entries(&self) -> &[HistoryEntry] {
        &self.history_entries
    }
}

/// Returns the default history file path for the given shell.
fn default_history_path(shell: &str) -> Result<PathBuf, HistoryPickerError> {
    let home = home_dir().ok_or(HistoryPickerError::NoHomeDirectory)?;

    let path = match shell.to_lowercase().as_str() {
        "bash" => home.join(".bash_history"),
        "zsh" => home.join(".zsh_history"),
        "fish" => home.join(".local/share/fish/fish_history"),
        _ => return Err(HistoryPickerError::UnsupportedShell(shell.to_string())),
    };

    Ok(path)
}

/// Gets the user's home directory.
fn home_dir() -> Option<PathBuf> {
    // Try HOME environment variable first (works on Unix and sometimes Windows)
    std::env::var_os("HOME").map(PathBuf::from).or({
        // Fallback for Windows
        #[cfg(windows)]
        {
            std::env::var_os("USERPROFILE").map(PathBuf::from)
        }
        #[cfg(not(windows))]
        {
            None
        }
    })
}

/// Processes history entries for display in the picker.
///
/// This function:
/// 1. Reverses the entries (most recent first)
/// 2. Removes consecutive duplicates
/// 3. Limits to `max_entries`
fn process_history_entries(entries: &[HistoryEntry], max_entries: usize) -> Vec<HistoryEntry> {
    let mut result = Vec::with_capacity(entries.len().min(max_entries));
    let mut seen: HashSet<&str> = HashSet::new();

    // Reverse to get most recent first
    for entry in entries.iter().rev() {
        // Skip if we've already seen this exact command
        if seen.contains(entry.command.as_str()) {
            continue;
        }

        // Skip empty commands
        if entry.command.trim().is_empty() {
            continue;
        }

        seen.insert(&entry.command);
        result.push(entry.clone());

        if result.len() >= max_entries {
            break;
        }
    }

    result
}

/// Converts history entries to picker items.
fn entries_to_picker_items(entries: &[HistoryEntry]) -> Vec<PickerItem> {
    entries
        .iter()
        .map(|entry| {
            entry.timestamp.map_or_else(
                || PickerItem::new(&entry.command),
                |ts| {
                    let formatted = format_timestamp(ts);
                    PickerItem::with_metadata(&entry.command, formatted)
                },
            )
        })
        .collect()
}

/// Formats a Unix timestamp for display.
fn format_timestamp(timestamp: i64) -> String {
    // Use a simple relative time format
    use std::time::{Duration, SystemTime, UNIX_EPOCH};

    let now_secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or(Duration::ZERO)
        .as_secs();
    let now = i64::try_from(now_secs).unwrap_or(i64::MAX);

    let diff = now - timestamp;

    if diff < 0 {
        return "future".to_string();
    }

    let diff = diff.cast_unsigned();

    if diff < 60 {
        "just now".to_string()
    } else if diff < 3600 {
        let mins = diff / 60;
        if mins == 1 {
            "1 min ago".to_string()
        } else {
            format!("{mins} mins ago")
        }
    } else if diff < 86400 {
        let hours = diff / 3600;
        if hours == 1 {
            "1 hour ago".to_string()
        } else {
            format!("{hours} hours ago")
        }
    } else if diff < 604_800 {
        let days = diff / 86400;
        if days == 1 {
            "1 day ago".to_string()
        } else {
            format!("{days} days ago")
        }
    } else if diff < 2_592_000 {
        let weeks = diff / 604_800;
        if weeks == 1 {
            "1 week ago".to_string()
        } else {
            format!("{weeks} weeks ago")
        }
    } else if diff < 31_536_000 {
        let months = diff / 2_592_000;
        if months == 1 {
            "1 month ago".to_string()
        } else {
            format!("{months} months ago")
        }
    } else {
        let years = diff / 31_536_000;
        if years == 1 {
            "1 year ago".to_string()
        } else {
            format!("{years} years ago")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // ========== HistoryPicker Construction Tests ==========

    #[test]
    fn test_from_history_empty() {
        let picker = HistoryPicker::from_history(vec![]);
        assert!(picker.is_empty());
        assert!(picker.selected_command().is_none());
    }

    #[test]
    fn test_from_history_basic() {
        let entries = vec![
            HistoryEntry::new("first"),
            HistoryEntry::new("second"),
            HistoryEntry::new("third"),
        ];
        let picker = HistoryPicker::from_history(entries);

        assert_eq!(picker.total_count(), 3);
        // Most recent (third) should be first
        assert_eq!(picker.selected_command(), Some("third"));
    }

    #[test]
    fn test_from_history_with_timestamps() {
        let entries = vec![
            HistoryEntry::with_timestamp("old command", 1_000_000_000),
            HistoryEntry::with_timestamp("new command", 1_700_000_000),
        ];
        let picker = HistoryPicker::from_history(entries);

        assert_eq!(picker.total_count(), 2);
        // Most recent should be first
        assert_eq!(picker.selected_command(), Some("new command"));
    }

    #[test]
    fn test_from_history_with_query() {
        let entries = vec![
            HistoryEntry::new("git status"),
            HistoryEntry::new("ls -la"),
            HistoryEntry::new("git commit"),
        ];
        let picker = HistoryPicker::from_history_with_query(entries, "git");

        assert_eq!(picker.filtered_count(), 2);
        assert_eq!(picker.query(), "git");
    }

    // ========== Deduplication Tests ==========

    #[test]
    fn test_deduplication_removes_consecutive_duplicates() {
        let entries = vec![
            HistoryEntry::new("ls"),
            HistoryEntry::new("ls"),
            HistoryEntry::new("ls"),
            HistoryEntry::new("pwd"),
        ];
        let picker = HistoryPicker::from_history(entries);

        // Should have only 2 unique commands
        assert_eq!(picker.total_count(), 2);
    }

    #[test]
    fn test_deduplication_keeps_first_occurrence() {
        // When reversed, we should keep the most recent occurrence
        let entries = vec![
            HistoryEntry::with_timestamp("ls", 1000),
            HistoryEntry::with_timestamp("ls", 2000),
        ];
        let picker = HistoryPicker::from_history(entries);

        assert_eq!(picker.total_count(), 1);
        // The most recent (timestamp 2000) should be kept
        let entry = picker.selected_entry().unwrap();
        assert_eq!(entry.timestamp, Some(2000));
    }

    #[test]
    fn test_deduplication_non_consecutive() {
        let entries = vec![
            HistoryEntry::new("ls"),
            HistoryEntry::new("pwd"),
            HistoryEntry::new("ls"), // Duplicate of first
        ];
        let picker = HistoryPicker::from_history(entries);

        // All duplicates should be removed, keeping most recent
        assert_eq!(picker.total_count(), 2);
        // Most recent ls should be first after reversal
        assert_eq!(picker.selected_command(), Some("ls"));
    }

    #[test]
    fn test_skips_empty_commands() {
        let entries = vec![
            HistoryEntry::new("ls"),
            HistoryEntry::new(""),
            HistoryEntry::new("   "),
            HistoryEntry::new("pwd"),
        ];
        let picker = HistoryPicker::from_history(entries);

        assert_eq!(picker.total_count(), 2);
    }

    // ========== Navigation Tests ==========

    #[test]
    fn test_select_next() {
        let entries = vec![
            HistoryEntry::new("first"),
            HistoryEntry::new("second"),
            HistoryEntry::new("third"),
        ];
        let mut picker = HistoryPicker::from_history(entries);

        // After reversal: third, second, first
        assert_eq!(picker.selected_command(), Some("third"));

        picker.select_next();
        assert_eq!(picker.selected_command(), Some("second"));

        picker.select_next();
        assert_eq!(picker.selected_command(), Some("first"));
    }

    #[test]
    fn test_select_prev() {
        let entries = vec![
            HistoryEntry::new("first"),
            HistoryEntry::new("second"),
            HistoryEntry::new("third"),
        ];
        let mut picker = HistoryPicker::from_history(entries);

        // Start at first item (third after reversal), go to last
        picker.select_prev();
        assert_eq!(picker.selected_command(), Some("first"));
    }

    #[test]
    fn test_update_query() {
        let entries = vec![
            HistoryEntry::new("git status"),
            HistoryEntry::new("ls -la"),
            HistoryEntry::new("git commit"),
        ];
        let mut picker = HistoryPicker::from_history(entries);

        picker.update_query("git");
        assert_eq!(picker.filtered_count(), 2);

        picker.update_query("status");
        assert_eq!(picker.filtered_count(), 1);

        picker.update_query("");
        assert_eq!(picker.filtered_count(), 3);
    }

    #[test]
    fn test_push_and_pop_char() {
        let entries = vec![
            HistoryEntry::new("git status"),
            HistoryEntry::new("grep pattern"),
        ];
        let mut picker = HistoryPicker::from_history(entries);

        picker.push_char('g');
        picker.push_char('i');
        picker.push_char('t');
        assert_eq!(picker.query(), "git");
        assert_eq!(picker.filtered_count(), 1);

        picker.pop_char();
        picker.pop_char();
        assert_eq!(picker.query(), "g");
        assert_eq!(picker.filtered_count(), 2);
    }

    // ========== Default Path Tests ==========

    #[test]
    fn test_default_history_path_bash() {
        if home_dir().is_some() {
            let path = default_history_path("bash").unwrap();
            assert!(path.to_string_lossy().contains(".bash_history"));
        }
    }

    #[test]
    fn test_default_history_path_zsh() {
        if home_dir().is_some() {
            let path = default_history_path("zsh").unwrap();
            assert!(path.to_string_lossy().contains(".zsh_history"));
        }
    }

    #[test]
    fn test_default_history_path_fish() {
        if home_dir().is_some() {
            let path = default_history_path("fish").unwrap();
            assert!(path.to_string_lossy().contains("fish_history"));
        }
    }

    #[test]
    fn test_default_history_path_case_insensitive() {
        if home_dir().is_some() {
            let path = default_history_path("BASH").unwrap();
            assert!(path.to_string_lossy().contains(".bash_history"));

            let path = default_history_path("Zsh").unwrap();
            assert!(path.to_string_lossy().contains(".zsh_history"));
        }
    }

    #[test]
    fn test_default_history_path_unsupported_shell() {
        let result = default_history_path("unknown_shell");
        assert!(matches!(
            result,
            Err(HistoryPickerError::UnsupportedShell(_))
        ));
    }

    // ========== File Loading Tests ==========

    #[test]
    fn test_from_file_not_found() {
        let result = HistoryPicker::from_file(Path::new("/nonexistent/path/history"));
        assert!(matches!(result, Err(HistoryPickerError::FileNotFound(_))));
    }

    #[test]
    fn test_from_file_valid() {
        use std::io::Write;
        use tempfile::NamedTempFile;

        let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(file, "ls -la").unwrap();
        writeln!(file, "git status").unwrap();
        writeln!(file, "echo hello").unwrap();
        file.flush().unwrap();

        let picker = HistoryPicker::from_file(file.path()).unwrap();
        assert_eq!(picker.total_count(), 3);
        // Most recent (echo hello) should be first
        assert_eq!(picker.selected_command(), Some("echo hello"));
    }

    #[test]
    fn test_from_file_with_query() {
        use std::io::Write;
        use tempfile::NamedTempFile;

        let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
        writeln!(file, "git status").unwrap();
        writeln!(file, "ls -la").unwrap();
        writeln!(file, "git commit").unwrap();
        file.flush().unwrap();

        let picker = HistoryPicker::from_file_with_query(file.path(), "git").unwrap();
        assert_eq!(picker.filtered_count(), 2);
        assert_eq!(picker.query(), "git");
    }

    // ========== Timestamp Formatting Tests ==========

    #[test]
    fn test_format_timestamp_just_now() {
        use std::time::{SystemTime, UNIX_EPOCH};
        #[allow(clippy::cast_possible_wrap)]
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        let formatted = format_timestamp(now);
        assert_eq!(formatted, "just now");
    }

    #[test]
    fn test_format_timestamp_minutes() {
        use std::time::{SystemTime, UNIX_EPOCH};
        #[allow(clippy::cast_possible_wrap)]
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        let formatted = format_timestamp(now - 120);
        assert_eq!(formatted, "2 mins ago");

        let formatted = format_timestamp(now - 60);
        assert_eq!(formatted, "1 min ago");
    }

    #[test]
    fn test_format_timestamp_hours() {
        use std::time::{SystemTime, UNIX_EPOCH};
        #[allow(clippy::cast_possible_wrap)]
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        let formatted = format_timestamp(now - 3600);
        assert_eq!(formatted, "1 hour ago");

        let formatted = format_timestamp(now - 7200);
        assert_eq!(formatted, "2 hours ago");
    }

    #[test]
    fn test_format_timestamp_days() {
        use std::time::{SystemTime, UNIX_EPOCH};
        #[allow(clippy::cast_possible_wrap)]
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        let formatted = format_timestamp(now - 86400);
        assert_eq!(formatted, "1 day ago");

        let formatted = format_timestamp(now - 172_800);
        assert_eq!(formatted, "2 days ago");
    }

    #[test]
    fn test_format_timestamp_future() {
        use std::time::{SystemTime, UNIX_EPOCH};
        #[allow(clippy::cast_possible_wrap)]
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        let formatted = format_timestamp(now + 1000);
        assert_eq!(formatted, "future");
    }

    // ========== Picker Access Tests ==========

    #[test]
    fn test_picker_access() {
        let entries = vec![HistoryEntry::new("ls")];
        let mut picker = HistoryPicker::from_history(entries);

        assert_eq!(picker.picker().total_count(), 1);
        assert_eq!(picker.picker_mut().total_count(), 1);
    }

    #[test]
    fn test_history_entries_access() {
        let entries = vec![HistoryEntry::new("ls"), HistoryEntry::new("pwd")];
        let picker = HistoryPicker::from_history(entries);

        // After processing, most recent is first
        let history = picker.history_entries();
        assert_eq!(history.len(), 2);
        assert_eq!(history[0].command, "pwd");
        assert_eq!(history[1].command, "ls");
    }

    // ========== Edge Cases ==========

    #[test]
    fn test_large_history() {
        // Create more entries than DEFAULT_MAX_ENTRIES
        let entries: Vec<HistoryEntry> = (0..15_000)
            .map(|i| HistoryEntry::new(format!("command {i}")))
            .collect();
        let picker = HistoryPicker::from_history(entries);

        // Should be limited to DEFAULT_MAX_ENTRIES
        assert_eq!(picker.total_count(), DEFAULT_MAX_ENTRIES);
    }

    #[test]
    fn test_unicode_commands() {
        let entries = vec![
            HistoryEntry::new("echo \u{4e2d}\u{6587}"),
            HistoryEntry::new("echo \u{1f600}"),
        ];
        let picker = HistoryPicker::from_history(entries);

        assert_eq!(picker.total_count(), 2);
        // Check that unicode is preserved
        let cmd = picker.selected_command().unwrap();
        assert!(cmd.contains('\u{1f600}'));
    }

    #[test]
    fn test_selected_entry_with_filter() {
        let entries = vec![
            HistoryEntry::with_timestamp("git status", 1000),
            HistoryEntry::with_timestamp("ls -la", 2000),
            HistoryEntry::with_timestamp("git commit", 3000),
        ];
        let mut picker = HistoryPicker::from_history(entries);

        picker.update_query("git");

        let entry = picker.selected_entry().unwrap();
        assert_eq!(entry.command, "git commit");
        assert_eq!(entry.timestamp, Some(3000));
    }

    #[test]
    fn test_is_filtered_empty() {
        let entries = vec![HistoryEntry::new("ls")];
        let mut picker = HistoryPicker::from_history(entries);

        assert!(!picker.is_filtered_empty());

        picker.update_query("nonexistent");
        assert!(picker.is_filtered_empty());
    }
}
