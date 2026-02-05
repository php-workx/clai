//! Shell history file parser for clai-wrap.
//!
//! This module provides parsers for shell history files in various formats:
//!
//! - **Bash plain**: One command per line
//! - **Bash timestamped**: `#timestamp\ncommand` pairs
//! - **Zsh extended**: `: timestamp:0;command` format
//! - **Fish**: YAML-like format in `fish_history`
//!
//! # Example
//!
//! ```rust
//! use clai_wrap::history_parser::{parse_bash_history, HistoryEntry};
//!
//! let content = "ls -la\ngit status\necho hello";
//! let entries = parse_bash_history(content);
//! assert_eq!(entries.len(), 3);
//! assert_eq!(entries[0].command, "ls -la");
//! ```

use std::path::Path;

use thiserror::Error;
use tracing::warn;

/// A single entry from a shell history file.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryEntry {
    /// The command that was executed.
    pub command: String,
    /// Unix timestamp when the command was executed, if available.
    pub timestamp: Option<i64>,
}

impl HistoryEntry {
    /// Creates a new history entry with just a command (no timestamp).
    #[must_use]
    pub fn new(command: impl Into<String>) -> Self {
        Self {
            command: command.into(),
            timestamp: None,
        }
    }

    /// Creates a new history entry with a command and timestamp.
    #[must_use]
    pub fn with_timestamp(command: impl Into<String>, timestamp: i64) -> Self {
        Self {
            command: command.into(),
            timestamp: Some(timestamp),
        }
    }
}

/// Errors that can occur when parsing history files.
#[derive(Debug, Error)]
pub enum HistoryParseError {
    /// The file contains invalid UTF-8 sequences.
    #[error("file contains invalid UTF-8: {0}")]
    InvalidUtf8(#[from] std::string::FromUtf8Error),

    /// The file could not be read.
    #[error("failed to read file: {0}")]
    IoError(#[from] std::io::Error),

    /// Could not detect the history format.
    #[error("unable to detect history format for file: {0}")]
    UnknownFormat(String),
}

/// Parses bash history in plain text format (one command per line).
///
/// Empty lines are skipped. Lines starting with `#` are treated as comments
/// unless followed by a valid timestamp pattern (for timestamped bash history).
///
/// # Arguments
///
/// * `content` - The raw content of the bash history file
///
/// # Returns
///
/// A vector of `HistoryEntry` structs, one per valid command
#[must_use]
pub fn parse_bash_history(content: &str) -> Vec<HistoryEntry> {
    let lines: Vec<&str> = content.lines().collect();
    let mut entries = Vec::new();

    // Check if this appears to be timestamped format
    if is_bash_timestamped(&lines) {
        return parse_bash_timestamped_internal(&lines);
    }

    // Plain text format: one command per line
    for line in lines {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        // In plain format, lines starting with # are comments (not timestamps)
        if trimmed.starts_with('#') {
            continue;
        }
        entries.push(HistoryEntry::new(line.to_string()));
    }

    entries
}

/// Parses bash history in timestamped format.
///
/// The timestamped format uses pairs of lines:
/// ```text
/// #1234567890
/// ls -la
/// ```
///
/// If a timestamp line is malformed or missing its command, a warning is logged
/// and the line is skipped.
///
/// # Arguments
///
/// * `content` - The raw content of the bash history file
///
/// # Returns
///
/// A vector of `HistoryEntry` structs with timestamps
#[must_use]
pub fn parse_bash_timestamped(content: &str) -> Vec<HistoryEntry> {
    let lines: Vec<&str> = content.lines().collect();
    parse_bash_timestamped_internal(&lines)
}

/// Internal implementation for parsing timestamped bash history.
fn parse_bash_timestamped_internal(lines: &[&str]) -> Vec<HistoryEntry> {
    let mut entries = Vec::new();
    let mut i = 0;

    while i < lines.len() {
        let line = lines[i].trim();

        // Check for timestamp line
        if line.starts_with('#') {
            if let Some(timestamp) = parse_bash_timestamp(line) {
                // Look for the command on the next line
                if i + 1 < lines.len() {
                    let command = lines[i + 1];
                    if !command.is_empty() && !command.starts_with('#') {
                        entries.push(HistoryEntry::with_timestamp(command.to_string(), timestamp));
                        i += 2;
                        continue;
                    }
                }
                // Timestamp without valid command
                warn!(
                    "bash history: timestamp at line {} without valid command",
                    i + 1
                );
            }
            // Invalid timestamp format or standalone comment
            i += 1;
            continue;
        }

        // Non-timestamp line in timestamped history - might be orphaned command
        if !line.is_empty() {
            entries.push(HistoryEntry::new(lines[i].to_string()));
        }
        i += 1;
    }

    entries
}

/// Checks if the bash history appears to be in timestamped format.
fn is_bash_timestamped(lines: &[&str]) -> bool {
    // Look at first few non-empty lines to detect format
    let mut timestamp_count = 0;
    let mut checked = 0;

    for line in lines {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        if parse_bash_timestamp(trimmed).is_some() {
            timestamp_count += 1;
        }
        checked += 1;
        if checked >= 10 {
            break;
        }
    }

    // If more than half of the first lines we checked are timestamps,
    // this is likely timestamped format
    checked > 0 && timestamp_count * 2 >= checked
}

/// Parses a bash timestamp line (e.g., "#1234567890").
///
/// Returns `Some(timestamp)` if the line is a valid timestamp, `None` otherwise.
fn parse_bash_timestamp(line: &str) -> Option<i64> {
    let trimmed = line.trim();
    if !trimmed.starts_with('#') {
        return None;
    }
    let rest = trimmed.strip_prefix('#')?;
    // Must be all digits
    if rest.chars().all(|c| c.is_ascii_digit()) && !rest.is_empty() {
        rest.parse().ok()
    } else {
        None
    }
}

/// Parses zsh history in extended format.
///
/// Zsh extended history format uses the pattern `: timestamp:0;command`.
/// It also supports multi-line commands where continuation lines start with a backslash.
///
/// Plain text lines (without the extended format prefix) are also supported.
///
/// # Arguments
///
/// * `content` - The raw content of the zsh history file
///
/// # Returns
///
/// A vector of `HistoryEntry` structs
#[must_use]
pub fn parse_zsh_history(content: &str) -> Vec<HistoryEntry> {
    let mut entries = Vec::new();
    let mut lines_iter = content.lines().peekable();

    while let Some(line) = lines_iter.next() {
        // Check for extended format: `: timestamp:0;command`
        if let Some(entry) = parse_zsh_extended_line(line, &mut lines_iter) {
            entries.push(entry);
        } else {
            // Plain text format - but skip lines that look like extended format
            // (they were parsed but had empty command)
            let trimmed = line.trim();
            if !trimmed.is_empty() && !trimmed.starts_with(": ") {
                entries.push(HistoryEntry::new(line.to_string()));
            }
        }
    }

    entries
}

/// Parses a single zsh extended format line.
///
/// Returns `None` if the line doesn't match the extended format.
fn parse_zsh_extended_line<'a, I>(
    line: &str,
    remaining: &mut std::iter::Peekable<I>,
) -> Option<HistoryEntry>
where
    I: Iterator<Item = &'a str>,
{
    // Extended format: `: timestamp:0;command`
    // The timestamp can optionally have `:0` or `:duration` after it
    let trimmed = line.trim();

    if !trimmed.starts_with(": ") {
        return None;
    }

    let rest = &trimmed[2..]; // Skip ": "

    // Find the semicolon that separates metadata from command
    let semicolon_pos = rest.find(';')?;
    let metadata = &rest[..semicolon_pos];
    let mut command = rest[semicolon_pos + 1..].to_string();

    // Parse timestamp from metadata (format: "timestamp:duration" or just "timestamp")
    let timestamp: Option<i64> = metadata.find(':').map_or_else(
        || metadata.parse().ok(),
        |colon_pos| metadata[..colon_pos].parse().ok(),
    );

    // Handle multi-line commands (continuation lines end with backslash)
    while command.ends_with('\\') {
        if let Some(next_line) = remaining.next() {
            command.pop(); // Remove trailing backslash
            command.push('\n');
            command.push_str(next_line);
        } else {
            break;
        }
    }

    if command.is_empty() {
        warn!("zsh history: empty command found");
        return None;
    }

    if let Some(ts) = timestamp {
        Some(HistoryEntry::with_timestamp(command, ts))
    } else {
        warn!("zsh history: invalid timestamp in line: {}", line);
        Some(HistoryEntry::new(command))
    }
}

/// Parses fish shell history in its YAML-like format.
///
/// Fish history format:
/// ```text
/// - cmd: ls -la
///   when: 1234567890
/// - cmd: git status
///   when: 1234567891
/// ```
///
/// Multi-line commands use the YAML multi-line string format with `\n` escapes.
///
/// # Arguments
///
/// * `content` - The raw content of the `fish_history` file
///
/// # Returns
///
/// A vector of `HistoryEntry` structs
#[must_use]
pub fn parse_fish_history(content: &str) -> Vec<HistoryEntry> {
    let mut entries = Vec::new();
    let mut current_cmd: Option<String> = None;
    let mut current_when: Option<i64> = None;

    for line in content.lines() {
        let trimmed = line.trim();

        // New entry marker
        if trimmed.starts_with("- cmd:") {
            // Save previous entry if exists
            if let Some(cmd) = current_cmd.take() {
                entries.push(match current_when.take() {
                    Some(ts) => HistoryEntry::with_timestamp(cmd, ts),
                    None => HistoryEntry::new(cmd),
                });
            }

            // Parse the new command
            let cmd = trimmed.strip_prefix("- cmd:").unwrap().trim();
            current_cmd = Some(unescape_fish_string(cmd));
            current_when = None;
        } else if trimmed.starts_with("when:") {
            // Timestamp for current entry
            if let Some(ts_str) = trimmed.strip_prefix("when:") {
                if let Ok(ts) = ts_str.trim().parse::<i64>() {
                    current_when = Some(ts);
                } else {
                    warn!("fish history: invalid timestamp: {}", ts_str.trim());
                }
            }
        } else if trimmed.starts_with("paths:") {
            // Fish also stores paths for some commands - skip these
            continue;
        } else if !trimmed.is_empty() && !trimmed.starts_with('-') {
            // Could be continuation of a multi-line value or other metadata
            // For now, we ignore unknown lines
        }
    }

    // Don't forget the last entry
    if let Some(cmd) = current_cmd.take() {
        entries.push(match current_when.take() {
            Some(ts) => HistoryEntry::with_timestamp(cmd, ts),
            None => HistoryEntry::new(cmd),
        });
    }

    entries
}

/// Unescapes fish history string encoding.
///
/// Fish encodes special characters:
/// - `\\n` -> newline
/// - `\\t` -> tab
/// - `\\\\` -> backslash
fn unescape_fish_string(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let mut chars = s.chars().peekable();

    while let Some(c) = chars.next() {
        if c == '\\' {
            match chars.peek() {
                Some('n') => {
                    chars.next();
                    result.push('\n');
                }
                Some('t') => {
                    chars.next();
                    result.push('\t');
                }
                Some('\\') => {
                    chars.next();
                    result.push('\\');
                }
                _ => {
                    // Unknown escape, keep as-is
                    result.push(c);
                }
            }
        } else {
            result.push(c);
        }
    }

    result
}

/// Detects the shell history format from a file path and parses it.
///
/// Detection is based on:
/// 1. File path hints (`.bash_history`, `.zsh_history`, `fish_history`)
/// 2. Content analysis (first few lines)
///
/// # Arguments
///
/// * `path` - Path to the history file
///
/// # Returns
///
/// A vector of `HistoryEntry` structs, or an error if the file cannot be read
/// or the format cannot be detected.
///
/// # Errors
///
/// Returns an error if:
/// - The file cannot be read
/// - The file contains invalid UTF-8
/// - The format cannot be detected
pub fn detect_and_parse(path: &Path) -> Result<Vec<HistoryEntry>, HistoryParseError> {
    let content = std::fs::read(path)?;

    // Check for valid UTF-8
    let content = String::from_utf8(content)?;

    // Try to detect format from filename
    let filename = path.file_name().and_then(|n| n.to_str()).unwrap_or("");

    let path_str = path.to_string_lossy();

    if filename.contains("bash_history") || path_str.contains(".bash_history") {
        return Ok(parse_bash_history(&content));
    }

    if filename.contains("zsh_history")
        || filename.contains("zhistory")
        || path_str.contains(".zsh_history")
        || path_str.contains(".zhistory")
    {
        return Ok(parse_zsh_history(&content));
    }

    if filename.contains("fish_history") || path_str.contains("fish/fish_history") {
        return Ok(parse_fish_history(&content));
    }

    // Try to detect from content
    if let Some(format) = detect_format_from_content(&content) {
        return match format {
            DetectedFormat::BashPlain => Ok(parse_bash_history(&content)),
            DetectedFormat::BashTimestamped => Ok(parse_bash_timestamped(&content)),
            DetectedFormat::Zsh => Ok(parse_zsh_history(&content)),
            DetectedFormat::Fish => Ok(parse_fish_history(&content)),
        };
    }

    // Default to bash plain text as last resort
    warn!(
        "history: could not detect format for {:?}, defaulting to bash plain",
        path
    );
    Ok(parse_bash_history(&content))
}

/// Detected history format.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum DetectedFormat {
    BashPlain,
    BashTimestamped,
    Zsh,
    Fish,
}

/// Attempts to detect the history format from content.
fn detect_format_from_content(content: &str) -> Option<DetectedFormat> {
    let mut lines = content.lines().take(20);

    // Check for fish format (YAML-like with "- cmd:")
    let first_non_empty: Vec<&str> = content
        .lines()
        .filter(|l| !l.trim().is_empty())
        .take(5)
        .collect();

    if first_non_empty
        .iter()
        .any(|l| l.trim().starts_with("- cmd:"))
    {
        return Some(DetectedFormat::Fish);
    }

    // Check for zsh extended format (": timestamp:0;command")
    if first_non_empty.iter().any(|l| {
        let t = l.trim();
        t.starts_with(": ") && t.contains(';')
    }) {
        return Some(DetectedFormat::Zsh);
    }

    // Check for bash timestamped format
    let lines_vec: Vec<&str> = content.lines().collect();
    if is_bash_timestamped(&lines_vec) {
        return Some(DetectedFormat::BashTimestamped);
    }

    // If we have content but couldn't determine format, assume bash plain
    if lines.next().is_some() {
        return Some(DetectedFormat::BashPlain);
    }

    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::NamedTempFile;

    // ========== Bash Plain Format Tests ==========

    #[test]
    fn test_bash_plain_basic() {
        let content = "ls -la\ngit status\necho hello";
        let entries = parse_bash_history(content);

        assert_eq!(entries.len(), 3);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, None);
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[2].command, "echo hello");
    }

    #[test]
    fn test_bash_plain_with_empty_lines() {
        let content = "ls -la\n\ngit status\n\n\necho hello\n";
        let entries = parse_bash_history(content);

        assert_eq!(entries.len(), 3);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[2].command, "echo hello");
    }

    #[test]
    fn test_bash_plain_preserves_whitespace() {
        let content = "  ls -la  \ngit status";
        let entries = parse_bash_history(content);

        assert_eq!(entries.len(), 2);
        // Should preserve the original whitespace in the command
        assert_eq!(entries[0].command, "  ls -la  ");
    }

    #[test]
    fn test_bash_plain_empty_file() {
        let content = "";
        let entries = parse_bash_history(content);
        assert!(entries.is_empty());
    }

    #[test]
    fn test_bash_plain_only_empty_lines() {
        let content = "\n\n\n";
        let entries = parse_bash_history(content);
        assert!(entries.is_empty());
    }

    #[test]
    fn test_bash_plain_comments_skipped() {
        // In plain bash history, lines starting with # that don't look like timestamps
        // are treated as comments
        let content = "ls\n# this is a comment\ngit status";
        let entries = parse_bash_history(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls");
        assert_eq!(entries[1].command, "git status");
    }

    // ========== Bash Timestamped Format Tests ==========

    #[test]
    fn test_bash_timestamped_basic() {
        let content = "#1234567890\nls -la\n#1234567891\ngit status";
        let entries = parse_bash_timestamped(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, Some(1234567890));
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[1].timestamp, Some(1234567891));
    }

    #[test]
    fn test_bash_timestamped_auto_detection() {
        // parse_bash_history should auto-detect timestamped format
        let content = "#1234567890\nls -la\n#1234567891\ngit status";
        let entries = parse_bash_history(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].timestamp, Some(1234567890));
    }

    #[test]
    fn test_bash_timestamped_with_empty_lines() {
        let content = "#1234567890\nls -la\n\n#1234567891\ngit status\n";
        let entries = parse_bash_timestamped(content);

        assert_eq!(entries.len(), 2);
    }

    #[test]
    fn test_bash_timestamped_orphaned_timestamp() {
        // Timestamp without following command
        let content = "#1234567890\n#1234567891\ngit status";
        let entries = parse_bash_timestamped(content);

        // First timestamp has no command, second one does
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "git status");
        assert_eq!(entries[0].timestamp, Some(1234567891));
    }

    #[test]
    fn test_bash_timestamped_invalid_timestamp() {
        let content = "#notanumber\nls -la\n#1234567890\ngit status";
        let entries = parse_bash_timestamped(content);

        // "ls -la" appears as orphaned command (invalid timestamp treated as comment)
        // "git status" has valid timestamp
        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, None);
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[1].timestamp, Some(1234567890));
    }

    // ========== Zsh Extended Format Tests ==========

    #[test]
    fn test_zsh_extended_basic() {
        let content = ": 1234567890:0;ls -la\n: 1234567891:0;git status";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, Some(1234567890));
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[1].timestamp, Some(1234567891));
    }

    #[test]
    fn test_zsh_extended_without_duration() {
        let content = ": 1234567890;ls -la";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, Some(1234567890));
    }

    #[test]
    fn test_zsh_extended_multiline() {
        let content = ": 1234567890:0;echo 'hello\\\nworld'";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 1);
        // The backslash continuation should be handled
        assert!(entries[0].command.contains("hello"));
        assert!(entries[0].command.contains("world"));
    }

    #[test]
    fn test_zsh_plain_fallback() {
        // Zsh can also have plain text history
        let content = "ls -la\ngit status";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, None);
    }

    #[test]
    fn test_zsh_mixed_format() {
        // Mix of extended and plain format (unusual but should handle)
        let content = ": 1234567890:0;ls -la\ngit status\n: 1234567891:0;echo hi";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 3);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, Some(1234567890));
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[1].timestamp, None);
        assert_eq!(entries[2].command, "echo hi");
        assert_eq!(entries[2].timestamp, Some(1234567891));
    }

    #[test]
    fn test_zsh_empty_command() {
        let content = ": 1234567890:0;\n: 1234567891:0;git status";
        let entries = parse_zsh_history(content);

        // Empty command should be skipped
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "git status");
    }

    #[test]
    fn test_zsh_command_with_semicolons() {
        // Commands can contain semicolons
        let content = ": 1234567890:0;echo a; echo b; echo c";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "echo a; echo b; echo c");
    }

    // ========== Fish Format Tests ==========

    #[test]
    fn test_fish_basic() {
        let content = "- cmd: ls -la\n  when: 1234567890\n- cmd: git status\n  when: 1234567891";
        let entries = parse_fish_history(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, Some(1234567890));
        assert_eq!(entries[1].command, "git status");
        assert_eq!(entries[1].timestamp, Some(1234567891));
    }

    #[test]
    fn test_fish_without_timestamp() {
        let content = "- cmd: ls -la\n- cmd: git status";
        let entries = parse_fish_history(content);

        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].command, "ls -la");
        assert_eq!(entries[0].timestamp, None);
    }

    #[test]
    fn test_fish_with_paths() {
        // Fish stores paths for some commands, should be ignored
        let content = "- cmd: ls -la\n  when: 1234567890\n  paths:\n    - /some/path";
        let entries = parse_fish_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "ls -la");
    }

    #[test]
    fn test_fish_escaped_characters() {
        let content = "- cmd: echo hello\\nworld\n  when: 1234567890";
        let entries = parse_fish_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "echo hello\nworld");
    }

    #[test]
    fn test_fish_escaped_tab() {
        let content = "- cmd: echo hello\\tworld\n  when: 1234567890";
        let entries = parse_fish_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "echo hello\tworld");
    }

    #[test]
    fn test_fish_escaped_backslash() {
        let content = "- cmd: echo hello\\\\world\n  when: 1234567890";
        let entries = parse_fish_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command, "echo hello\\world");
    }

    #[test]
    fn test_fish_empty_file() {
        let content = "";
        let entries = parse_fish_history(content);
        assert!(entries.is_empty());
    }

    // ========== Empty File Tests ==========

    #[test]
    fn test_empty_files() {
        assert!(parse_bash_history("").is_empty());
        assert!(parse_bash_timestamped("").is_empty());
        assert!(parse_zsh_history("").is_empty());
        assert!(parse_fish_history("").is_empty());
    }

    // ========== Invalid UTF-8 Tests ==========

    #[test]
    fn test_detect_and_parse_invalid_utf8() {
        // Create a temp file with invalid UTF-8
        let mut file = NamedTempFile::new().unwrap();
        file.write_all(&[0x80, 0x81, 0x82]).unwrap();
        file.flush().unwrap();

        let result = detect_and_parse(file.path());
        assert!(result.is_err());

        // Should be an InvalidUtf8 error
        match result {
            Err(HistoryParseError::InvalidUtf8(_)) => {}
            _ => panic!("Expected InvalidUtf8 error"),
        }
    }

    // ========== Mixed/Malformed Content Tests ==========

    #[test]
    fn test_bash_malformed_recovers() {
        // Various malformed content that shouldn't crash
        let content = "\x00\x01\x02\nls -la\n\t\t\t\ngit status";
        let entries = parse_bash_history(content);

        // Should still parse valid commands
        assert!(entries.len() >= 2);
    }

    #[test]
    fn test_zsh_malformed_line() {
        // Malformed zsh extended line
        let content = ": invalid\n: 1234567890:0;ls -la";
        let entries = parse_zsh_history(content);

        // Should recover and parse valid entries
        assert!(!entries.is_empty());
    }

    #[test]
    fn test_fish_malformed_yaml() {
        // Invalid YAML-like content
        let content =
            "not yaml\n- cmd: ls -la\n  when: invalid\n- cmd: git status\n  when: 1234567890";
        let entries = parse_fish_history(content);

        // Should parse what it can
        assert!(entries.len() >= 1);
    }

    // ========== Detection Tests ==========

    #[test]
    fn test_detect_bash_plain_from_content() {
        let content = "ls -la\ngit status\necho hello";
        let format = detect_format_from_content(content);
        assert_eq!(format, Some(DetectedFormat::BashPlain));
    }

    #[test]
    fn test_detect_bash_timestamped_from_content() {
        let content = "#1234567890\nls -la\n#1234567891\ngit status";
        let format = detect_format_from_content(content);
        assert_eq!(format, Some(DetectedFormat::BashTimestamped));
    }

    #[test]
    fn test_detect_zsh_from_content() {
        let content = ": 1234567890:0;ls -la\n: 1234567891:0;git status";
        let format = detect_format_from_content(content);
        assert_eq!(format, Some(DetectedFormat::Zsh));
    }

    #[test]
    fn test_detect_fish_from_content() {
        let content = "- cmd: ls -la\n  when: 1234567890";
        let format = detect_format_from_content(content);
        assert_eq!(format, Some(DetectedFormat::Fish));
    }

    // ========== File Path Detection Tests ==========

    #[test]
    fn test_detect_and_parse_bash_by_path() {
        let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
        file.write_all(b"ls -la\ngit status").unwrap();
        file.flush().unwrap();

        let entries = detect_and_parse(file.path()).unwrap();
        assert_eq!(entries.len(), 2);
    }

    #[test]
    fn test_detect_and_parse_zsh_by_path() {
        let mut file = NamedTempFile::with_suffix(".zsh_history").unwrap();
        file.write_all(b": 1234567890:0;ls -la").unwrap();
        file.flush().unwrap();

        let entries = detect_and_parse(file.path()).unwrap();
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].timestamp, Some(1234567890));
    }

    #[test]
    fn test_detect_and_parse_fish_by_path() {
        // Create in a temp dir with fish_history name
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("fish_history");
        std::fs::write(&path, "- cmd: ls -la\n  when: 1234567890").unwrap();

        let entries = detect_and_parse(&path).unwrap();
        assert_eq!(entries.len(), 1);
    }

    #[test]
    fn test_detect_and_parse_empty_file() {
        let file = NamedTempFile::with_suffix(".bash_history").unwrap();
        // File is empty

        let entries = detect_and_parse(file.path()).unwrap();
        assert!(entries.is_empty());
    }

    #[test]
    fn test_detect_and_parse_nonexistent_file() {
        let result = detect_and_parse(Path::new("/nonexistent/file"));
        assert!(result.is_err());

        match result {
            Err(HistoryParseError::IoError(_)) => {}
            _ => panic!("Expected IoError"),
        }
    }

    // ========== Edge Cases ==========

    #[test]
    fn test_very_long_command() {
        let long_cmd = "x".repeat(10_000);
        let content = format!("{long_cmd}");
        let entries = parse_bash_history(&content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].command.len(), 10_000);
    }

    #[test]
    fn test_unicode_commands() {
        let content = "echo \u{4e2d}\u{6587}\necho \u{1f600}";
        let entries = parse_bash_history(content);

        assert_eq!(entries.len(), 2);
        assert!(entries[0].command.contains('\u{4e2d}'));
        assert!(entries[1].command.contains('\u{1f600}'));
    }

    #[test]
    fn test_history_entry_constructors() {
        let e1 = HistoryEntry::new("ls -la");
        assert_eq!(e1.command, "ls -la");
        assert_eq!(e1.timestamp, None);

        let e2 = HistoryEntry::with_timestamp("git status", 1234567890);
        assert_eq!(e2.command, "git status");
        assert_eq!(e2.timestamp, Some(1234567890));
    }

    #[test]
    fn test_unescape_fish_string() {
        assert_eq!(unescape_fish_string("hello"), "hello");
        assert_eq!(unescape_fish_string("hello\\nworld"), "hello\nworld");
        assert_eq!(unescape_fish_string("hello\\tworld"), "hello\tworld");
        assert_eq!(unescape_fish_string("hello\\\\world"), "hello\\world");
        assert_eq!(unescape_fish_string("\\n\\t\\\\"), "\n\t\\");
        // Unknown escape should be preserved
        assert_eq!(unescape_fish_string("hello\\xworld"), "hello\\xworld");
    }

    #[test]
    fn test_zsh_large_timestamp() {
        // Test with a large timestamp (year 2100+)
        let content = ": 4102444800:0;future command";
        let entries = parse_zsh_history(content);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].timestamp, Some(4102444800));
    }

    #[test]
    fn test_bash_timestamp_edge_cases() {
        // Zero timestamp
        let content = "#0\nls";
        let entries = parse_bash_timestamped(content);
        assert_eq!(entries[0].timestamp, Some(0));

        // Large timestamp
        let content = "#9999999999999\nls";
        let entries = parse_bash_timestamped(content);
        assert_eq!(entries[0].timestamp, Some(9999999999999));
    }
}
