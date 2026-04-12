//! Interactive Denylist for clai-wrap.
//!
//! This module provides functionality to pause output capture for sensitive processes.
//! This implements Privacy Gate 1 (Section 7.1 of the spec): the Interactive Denylist.
//!
//! # Overview
//!
//! The denylist tracks process names that should not have their output captured.
//! When the foreground process matches a denylisted pattern, the ring buffer is paused
//! and no data is recorded, though output still flows to the screen normally.
//!
//! # Default Denylist
//!
//! The default denylist includes:
//! - `ssh`, `scp`, `sftp` - Remote access commands
//! - `mysql`, `psql` - Database clients
//! - `passwd` - Password changes
//! - `vim`, `nano`, `less`, `more` - Text editors and pagers
//! - `htop`, `top` - System monitors
//! - `docker login` - Container authentication
//! - `sudo` - Privileged execution (when prompting for password)
//!
//! # Matching Rules
//!
//! - Case-insensitive matching
//! - Path prefix stripped (only basename matched)
//! - Process arguments stripped (first word only matched)

use std::path::Path;

/// The type of matching to use for a deny pattern.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MatchType {
    /// Match process name exactly (case-insensitive).
    Exact,
    /// Match if process name starts with pattern (case-insensitive).
    Prefix,
    /// Match if process name contains pattern (case-insensitive).
    Contains,
}

/// A single pattern in the denylist.
#[derive(Debug, Clone)]
pub struct DenyPattern {
    /// The pattern name to match against.
    pub name: String,
    /// The type of matching to use.
    pub match_type: MatchType,
}

impl DenyPattern {
    /// Creates a new deny pattern.
    #[must_use]
    pub fn new(name: impl Into<String>, match_type: MatchType) -> Self {
        Self {
            name: name.into().to_lowercase(),
            match_type,
        }
    }

    /// Check if this pattern matches the given process name.
    ///
    /// The process name is normalized (lowercase, basename only) before matching.
    fn matches(&self, process_name: &str) -> bool {
        let normalized = process_name.to_lowercase();

        match self.match_type {
            MatchType::Exact => normalized == self.name,
            MatchType::Prefix => normalized.starts_with(&self.name),
            MatchType::Contains => normalized.contains(&self.name),
        }
    }
}

/// A denylist of process names that should have output capture paused.
///
/// When the foreground process matches a denylisted pattern, output capture
/// is paused but output still flows to the terminal normally.
#[derive(Debug, Clone, Default)]
pub struct Denylist {
    patterns: Vec<DenyPattern>,
}

impl Denylist {
    /// Creates a new empty denylist.
    #[must_use]
    pub const fn new() -> Self {
        Self {
            patterns: Vec::new(),
        }
    }

    /// Creates the default denylist with standard sensitive process patterns.
    ///
    /// This includes all processes mentioned in Section 7.1 of the spec:
    /// - `ssh`, `scp`, `sftp` - Remote access commands
    /// - `mysql`, `psql` - Database clients
    /// - `passwd` - Password changes
    /// - `vim`, `nano`, `less`, `more` - Text editors and pagers
    /// - `htop`, `top` - System monitors
    /// - `docker` (with "login" in args) - Container authentication
    /// - `sudo` - Privileged execution
    #[must_use]
    pub fn with_defaults() -> Self {
        let mut denylist = Self::new();

        // Remote access commands (exact match)
        denylist.add("ssh", MatchType::Exact);
        denylist.add("scp", MatchType::Exact);
        denylist.add("sftp", MatchType::Exact);

        // Database clients (exact match)
        denylist.add("mysql", MatchType::Exact);
        denylist.add("psql", MatchType::Exact);

        // Password utilities (exact match)
        denylist.add("passwd", MatchType::Exact);

        // Text editors (exact match)
        denylist.add("vim", MatchType::Exact);
        denylist.add("nvim", MatchType::Exact);
        denylist.add("nano", MatchType::Exact);

        // Pagers (exact match)
        denylist.add("less", MatchType::Exact);
        denylist.add("more", MatchType::Exact);

        // System monitors (exact match)
        denylist.add("htop", MatchType::Exact);
        denylist.add("top", MatchType::Exact);

        // Docker (exact match - we can't check args from process name alone)
        // Note: "docker login" detection requires argument checking which
        // is beyond the scope of process name matching. We include "docker"
        // as a prefix match to be safe.
        denylist.add("docker", MatchType::Exact);

        // Privileged execution (exact match)
        denylist.add("sudo", MatchType::Exact);
        denylist.add("su", MatchType::Exact);
        denylist.add("doas", MatchType::Exact);

        denylist
    }

    /// Add a pattern to the denylist.
    ///
    /// # Arguments
    ///
    /// * `name` - The pattern name to match
    /// * `match_type` - The type of matching to use
    pub fn add(&mut self, name: &str, match_type: MatchType) {
        self.patterns.push(DenyPattern::new(name, match_type));
    }

    /// Check if a process name should be denied (have output capture paused).
    ///
    /// The process name is normalized before checking:
    /// - Path prefix stripped (e.g., "/usr/bin/vim" -> "vim")
    /// - Arguments stripped (e.g., "vim file.txt" -> "vim")
    /// - Case normalized (e.g., "VIM" -> "vim")
    ///
    /// # Arguments
    ///
    /// * `process_name` - The process name (may include path and/or arguments)
    ///
    /// # Returns
    ///
    /// `true` if the process matches a denylist pattern, `false` otherwise.
    #[must_use]
    pub fn is_denied(&self, process_name: &str) -> bool {
        let normalized = Self::normalize_process_name(process_name);
        self.patterns.iter().any(|p| p.matches(&normalized))
    }

    /// Load additional patterns from a configuration file.
    ///
    /// The file format is one pattern per line:
    /// ```text
    /// # Comment lines start with #
    /// exact:ssh
    /// prefix:docker
    /// contains:password
    /// ```
    ///
    /// Lines without a prefix default to exact matching.
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be read.
    pub fn load_from_file(path: &Path) -> std::io::Result<Self> {
        let contents = std::fs::read_to_string(path)?;
        let mut denylist = Self::new();

        for line in contents.lines() {
            let line = line.trim();

            // Skip empty lines and comments
            if line.is_empty() || line.starts_with('#') {
                continue;
            }

            // Parse the pattern type and name
            // Using if-else chain for readability
            #[allow(clippy::option_if_let_else)]
            let (match_type, name) = if let Some(rest) = line.strip_prefix("exact:") {
                (MatchType::Exact, rest.trim())
            } else if let Some(rest) = line.strip_prefix("prefix:") {
                (MatchType::Prefix, rest.trim())
            } else if let Some(rest) = line.strip_prefix("contains:") {
                (MatchType::Contains, rest.trim())
            } else {
                // Default to exact match
                (MatchType::Exact, line)
            };

            if !name.is_empty() {
                denylist.add(name, match_type);
            }
        }

        Ok(denylist)
    }

    /// Merge another denylist into this one.
    ///
    /// All patterns from the other denylist are added to this one.
    pub fn merge(&mut self, other: &Self) {
        self.patterns.extend(other.patterns.iter().cloned());
    }

    /// Returns the number of patterns in the denylist.
    #[must_use]
    pub const fn len(&self) -> usize {
        self.patterns.len()
    }

    /// Returns `true` if the denylist has no patterns.
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.patterns.is_empty()
    }

    /// Normalize a process name for matching.
    ///
    /// This performs:
    /// 1. Strip arguments (e.g., "vim file.txt" -> "vim")
    /// 2. Strip path prefix (e.g., "/usr/bin/vim" -> "vim")
    /// 3. Strip `.exe` extension on Windows (e.g., "vim.exe" -> "vim")
    ///
    /// Note: For Windows paths with spaces (e.g., "C:\Program Files\vim\vim.exe"),
    /// the path should be quoted or passed without spaces in arguments.
    /// In practice, process names from the OS don't include arguments.
    fn normalize_process_name(process_name: &str) -> String {
        let trimmed = process_name.trim();
        if trimmed.is_empty() {
            return String::new();
        }

        // First, try to identify if this looks like a Windows path with spaces.
        // Windows paths typically start with a drive letter followed by colon.
        let is_windows_path =
            trimmed.len() >= 2 && trimmed.chars().nth(1) == Some(':') && trimmed.contains('\\');

        let command = if is_windows_path {
            // For Windows paths, don't split on whitespace first - the whole
            // thing is likely a path. Just extract the basename.
            trimmed
        } else {
            // For Unix-style input or simple commands, split on whitespace first
            // to separate the command from arguments.
            trimmed.split_whitespace().next().unwrap_or(trimmed)
        };

        // Strip path prefix (take basename)
        // Handle both Unix (/) and Windows (\) path separators
        let basename = command.rsplit(['/', '\\']).next().unwrap_or(command);

        // Strip .exe extension (common on Windows) - case insensitive
        // We check the lowercase version of the basename to handle any case variation
        #[allow(clippy::case_sensitive_file_extension_comparisons)]
        let name = if basename.len() > 4 && basename.to_ascii_lowercase().ends_with(".exe") {
            &basename[..basename.len() - 4]
        } else {
            basename
        };

        name.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_denylist_contains_expected_entries() {
        let denylist = Denylist::with_defaults();

        // Remote access
        assert!(denylist.is_denied("ssh"), "ssh should be denied");
        assert!(denylist.is_denied("scp"), "scp should be denied");
        assert!(denylist.is_denied("sftp"), "sftp should be denied");

        // Database clients
        assert!(denylist.is_denied("mysql"), "mysql should be denied");
        assert!(denylist.is_denied("psql"), "psql should be denied");

        // Password utilities
        assert!(denylist.is_denied("passwd"), "passwd should be denied");

        // Text editors
        assert!(denylist.is_denied("vim"), "vim should be denied");
        assert!(denylist.is_denied("nvim"), "nvim should be denied");
        assert!(denylist.is_denied("nano"), "nano should be denied");

        // Pagers
        assert!(denylist.is_denied("less"), "less should be denied");
        assert!(denylist.is_denied("more"), "more should be denied");

        // System monitors
        assert!(denylist.is_denied("htop"), "htop should be denied");
        assert!(denylist.is_denied("top"), "top should be denied");

        // Container authentication
        assert!(denylist.is_denied("docker"), "docker should be denied");

        // Privileged execution
        assert!(denylist.is_denied("sudo"), "sudo should be denied");
        assert!(denylist.is_denied("su"), "su should be denied");
        assert!(denylist.is_denied("doas"), "doas should be denied");
    }

    #[test]
    fn test_exact_match() {
        let mut denylist = Denylist::new();
        denylist.add("vim", MatchType::Exact);

        assert!(denylist.is_denied("vim"), "vim should match exactly");
        assert!(
            !denylist.is_denied("vimx"),
            "vimx should not match exact vim"
        );
        assert!(
            !denylist.is_denied("xvim"),
            "xvim should not match exact vim"
        );
        assert!(
            !denylist.is_denied("gvim"),
            "gvim should not match exact vim"
        );
    }

    #[test]
    fn test_prefix_match() {
        let mut denylist = Denylist::new();
        denylist.add("docker", MatchType::Prefix);

        assert!(
            denylist.is_denied("docker"),
            "docker should match prefix docker"
        );
        assert!(
            denylist.is_denied("dockerd"),
            "dockerd should match prefix docker"
        );
        assert!(
            denylist.is_denied("docker-compose"),
            "docker-compose should match prefix docker"
        );
        assert!(
            !denylist.is_denied("podman-docker"),
            "podman-docker should not match prefix docker"
        );
    }

    #[test]
    fn test_contains_match() {
        let mut denylist = Denylist::new();
        denylist.add("password", MatchType::Contains);

        assert!(
            denylist.is_denied("password"),
            "password should match contains password"
        );
        assert!(
            denylist.is_denied("change-password"),
            "change-password should match contains password"
        );
        assert!(
            denylist.is_denied("password-manager"),
            "password-manager should match contains password"
        );
        assert!(
            denylist.is_denied("my-password-tool"),
            "my-password-tool should match contains password"
        );
        assert!(
            !denylist.is_denied("passwd"),
            "passwd should not match contains password"
        );
    }

    #[test]
    fn test_case_insensitivity() {
        let mut denylist = Denylist::new();
        denylist.add("vim", MatchType::Exact);

        assert!(denylist.is_denied("vim"), "vim should match");
        assert!(
            denylist.is_denied("VIM"),
            "VIM should match (case insensitive)"
        );
        assert!(
            denylist.is_denied("Vim"),
            "Vim should match (case insensitive)"
        );
        assert!(
            denylist.is_denied("vIm"),
            "vIm should match (case insensitive)"
        );
    }

    #[test]
    fn test_path_stripping() {
        let denylist = Denylist::with_defaults();

        // Unix paths
        assert!(
            denylist.is_denied("/usr/bin/vim"),
            "/usr/bin/vim should be denied"
        );
        assert!(
            denylist.is_denied("/usr/local/bin/ssh"),
            "/usr/local/bin/ssh should be denied"
        );
        assert!(
            denylist.is_denied("/bin/nano"),
            "/bin/nano should be denied"
        );

        // Windows paths
        assert!(
            denylist.is_denied("C:\\Program Files\\vim\\vim.exe"),
            "Windows path to vim should be denied"
        );
        assert!(
            denylist.is_denied("C:\\Windows\\System32\\ssh.exe"),
            "Windows path to ssh should be denied"
        );
    }

    #[test]
    fn test_with_process_arguments() {
        let denylist = Denylist::with_defaults();

        // Commands with arguments
        assert!(
            denylist.is_denied("vim file.txt"),
            "vim with argument should be denied"
        );
        assert!(
            denylist.is_denied("ssh user@host"),
            "ssh with argument should be denied"
        );
        assert!(
            denylist.is_denied("mysql -u root -p"),
            "mysql with arguments should be denied"
        );
        assert!(
            denylist.is_denied("less /var/log/syslog"),
            "less with argument should be denied"
        );

        // Full paths with arguments
        assert!(
            denylist.is_denied("/usr/bin/vim -R file.txt"),
            "/usr/bin/vim with arguments should be denied"
        );
    }

    #[test]
    fn test_not_denied() {
        let denylist = Denylist::with_defaults();

        // Common commands that should NOT be denied
        assert!(!denylist.is_denied("ls"), "ls should not be denied");
        assert!(!denylist.is_denied("cat"), "cat should not be denied");
        assert!(!denylist.is_denied("grep"), "grep should not be denied");
        assert!(!denylist.is_denied("echo"), "echo should not be denied");
        assert!(!denylist.is_denied("git"), "git should not be denied");
        assert!(!denylist.is_denied("cargo"), "cargo should not be denied");
        assert!(!denylist.is_denied("make"), "make should not be denied");
    }

    #[test]
    fn test_empty_denylist() {
        let denylist = Denylist::new();

        assert!(denylist.is_empty(), "new denylist should be empty");
        assert_eq!(denylist.len(), 0, "new denylist should have 0 patterns");
        assert!(
            !denylist.is_denied("ssh"),
            "empty denylist should deny nothing"
        );
    }

    #[test]
    fn test_add_pattern() {
        let mut denylist = Denylist::new();
        assert!(denylist.is_empty());

        denylist.add("custom", MatchType::Exact);
        assert_eq!(denylist.len(), 1);
        assert!(denylist.is_denied("custom"));

        denylist.add("prefix", MatchType::Prefix);
        assert_eq!(denylist.len(), 2);
        assert!(denylist.is_denied("prefixsomething"));
    }

    #[test]
    fn test_merge_denylists() {
        let mut denylist1 = Denylist::new();
        denylist1.add("ssh", MatchType::Exact);

        let mut denylist2 = Denylist::new();
        denylist2.add("mysql", MatchType::Exact);
        denylist2.add("psql", MatchType::Exact);

        assert_eq!(denylist1.len(), 1);
        denylist1.merge(&denylist2);
        assert_eq!(denylist1.len(), 3);

        assert!(denylist1.is_denied("ssh"));
        assert!(denylist1.is_denied("mysql"));
        assert!(denylist1.is_denied("psql"));
    }

    #[test]
    fn test_load_from_file() {
        use std::io::Write;

        // Create a temporary file with denylist patterns
        let mut temp_file = tempfile::NamedTempFile::new().unwrap();
        writeln!(temp_file, "# This is a comment").unwrap();
        writeln!(temp_file, "exact:custom-app").unwrap();
        writeln!(temp_file, "prefix:my-prefix").unwrap();
        writeln!(temp_file, "contains:secret").unwrap();
        writeln!(temp_file).unwrap(); // empty line
        writeln!(temp_file, "default-exact").unwrap();
        temp_file.flush().unwrap();

        let denylist = Denylist::load_from_file(temp_file.path()).unwrap();

        assert_eq!(denylist.len(), 4);
        assert!(
            denylist.is_denied("custom-app"),
            "exact pattern should work"
        );
        assert!(
            !denylist.is_denied("custom-app-extended"),
            "exact should not match extended"
        );
        assert!(
            denylist.is_denied("my-prefix-app"),
            "prefix pattern should work"
        );
        assert!(
            denylist.is_denied("has-secret-inside"),
            "contains pattern should work"
        );
        assert!(
            denylist.is_denied("default-exact"),
            "default (exact) pattern should work"
        );
    }

    #[test]
    fn test_load_from_nonexistent_file() {
        let result = Denylist::load_from_file(Path::new("/nonexistent/path/file.txt"));
        assert!(result.is_err(), "loading nonexistent file should fail");
    }

    #[test]
    fn test_normalize_process_name() {
        // Basic process name
        assert_eq!(Denylist::normalize_process_name("vim"), "vim");

        // With path
        assert_eq!(Denylist::normalize_process_name("/usr/bin/vim"), "vim");
        assert_eq!(
            Denylist::normalize_process_name("/usr/local/bin/ssh"),
            "ssh"
        );

        // With arguments
        assert_eq!(Denylist::normalize_process_name("vim file.txt"), "vim");
        assert_eq!(Denylist::normalize_process_name("ssh user@host"), "ssh");

        // With path and arguments
        assert_eq!(
            Denylist::normalize_process_name("/usr/bin/vim file.txt"),
            "vim"
        );

        // Windows path (strips .exe extension)
        assert_eq!(
            Denylist::normalize_process_name("C:\\Windows\\vim.exe"),
            "vim"
        );
        assert_eq!(
            Denylist::normalize_process_name("C:\\Windows\\ssh.EXE"),
            "ssh"
        );

        // Empty string
        assert_eq!(Denylist::normalize_process_name(""), "");

        // Only whitespace
        assert_eq!(Denylist::normalize_process_name("   "), "");
    }

    #[test]
    fn test_default_denylist_length() {
        let denylist = Denylist::with_defaults();
        // Count expected entries: ssh, scp, sftp, mysql, psql, passwd, vim, nvim, nano,
        // less, more, htop, top, docker, sudo, su, doas = 17
        assert!(
            denylist.len() >= 17,
            "default denylist should have at least 17 patterns, got {}",
            denylist.len()
        );
    }

    #[test]
    fn test_deny_pattern_new_normalizes_case() {
        let pattern = DenyPattern::new("SSH", MatchType::Exact);
        assert_eq!(pattern.name, "ssh", "pattern name should be lowercase");

        let pattern2 = DenyPattern::new("MyApp", MatchType::Contains);
        assert_eq!(pattern2.name, "myapp", "pattern name should be lowercase");
    }
}
