//! Configuration file support for clai-wrap.
//!
//! This module provides TOML-based configuration file loading and merging with CLI arguments.
//! Configuration files are searched in standard locations with CLI arguments taking precedence.
//!
//! # Configuration File Locations
//!
//! Configuration files are searched in the following order (first found wins):
//!
//! 1. `~/.config/clai/wrap.toml` (XDG standard on Unix, %APPDATA%/clai/wrap.toml on Windows)
//! 2. `~/.clai-wrap.toml` (legacy fallback)
//!
//! # Configuration Options
//!
//! ```toml
//! # Hotkey chord to trigger picker (e.g., "ctrl-\\ h")
//! hotkey = "ctrl-\\ h"
//!
//! # Output buffer capacity in bytes (default: 2 MiB)
//! buffer_capacity = 2097152
//!
//! # Unix socket path for daemon connection
//! daemon_socket = "/run/user/1000/clai/daemon.sock"
//!
//! # Execute command immediately after selection
//! execute_on_select = false
//!
//! # Denylist patterns for privacy (processes to exclude from capture)
//! # Format: "type:pattern" where type is exact, prefix, or contains
//! # Lines without a type prefix default to exact matching
//! denylist = [
//!     "exact:ssh",
//!     "prefix:docker",
//!     "contains:password",
//!     "mysql",  # defaults to exact
//! ]
//! ```
//!
//! # Merging with CLI Arguments
//!
//! CLI arguments always take precedence over configuration file values.
//! If a CLI argument is not provided, the configuration file value is used.
//! If neither is provided, the default value is used.

use serde::Deserialize;
use std::path::{Path, PathBuf};
use thiserror::Error;

use crate::cli::{Cli, DEFAULT_BUFFER_CAP};
use crate::denylist::{Denylist, MatchType};

/// Default hotkey chord
pub const DEFAULT_HOTKEY: &str = "ctrl-\\ h";

/// Errors that can occur when loading configuration
#[derive(Debug, Error)]
pub enum ConfigError {
    /// Failed to read configuration file
    #[error("failed to read config file: {0}")]
    ReadError(#[from] std::io::Error),

    /// Failed to parse TOML configuration
    #[error("failed to parse config file: {0}")]
    ParseError(#[from] toml::de::Error),

    /// Invalid configuration value
    #[error("invalid configuration: {0}")]
    InvalidConfig(String),
}

/// Configuration file structure (TOML)
#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct ConfigFile {
    /// Hotkey chord to trigger picker (e.g., "ctrl-\\ h")
    pub hotkey: Option<String>,

    /// Output buffer capacity in bytes
    pub buffer_capacity: Option<usize>,

    /// Unix socket path for daemon connection
    pub daemon_socket: Option<PathBuf>,

    /// Denylist patterns for privacy
    pub denylist: Option<Vec<String>>,

    /// Execute command immediately after selection
    pub execute_on_select: Option<bool>,
}

impl ConfigFile {
    /// Load configuration from a file path
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be read or parsed.
    pub fn load(path: &Path) -> Result<Self, ConfigError> {
        let contents = std::fs::read_to_string(path)?;
        let config: Self = toml::from_str(&contents)?;
        config.validate()?;
        Ok(config)
    }

    /// Validate the configuration
    fn validate(&self) -> Result<(), ConfigError> {
        // Validate buffer capacity
        if let Some(cap) = self.buffer_capacity {
            if cap == 0 {
                return Err(ConfigError::InvalidConfig(
                    "buffer_capacity must be greater than 0".to_string(),
                ));
            }
        }

        // Validate hotkey
        if let Some(ref hotkey) = self.hotkey {
            if hotkey.is_empty() {
                return Err(ConfigError::InvalidConfig(
                    "hotkey cannot be empty".to_string(),
                ));
            }
        }

        // Validate denylist patterns
        if let Some(ref patterns) = self.denylist {
            for pattern in patterns {
                if pattern.is_empty() {
                    return Err(ConfigError::InvalidConfig(
                        "denylist pattern cannot be empty".to_string(),
                    ));
                }
            }
        }

        Ok(())
    }

    /// Parse denylist patterns into a Denylist
    ///
    /// Pattern format:
    /// - `exact:name` - Match process name exactly
    /// - `prefix:name` - Match if process name starts with pattern
    /// - `contains:name` - Match if process name contains pattern
    /// - `name` (no prefix) - Defaults to exact matching
    #[must_use]
    pub fn parse_denylist(&self) -> Denylist {
        let mut denylist = Denylist::new();

        if let Some(ref patterns) = self.denylist {
            for pattern in patterns {
                let trimmed = pattern.trim();
                if trimmed.is_empty() {
                    continue;
                }

                // Parse the pattern type and name
                #[allow(clippy::option_if_let_else)]
                let (match_type, name) = if let Some(rest) = trimmed.strip_prefix("exact:") {
                    (MatchType::Exact, rest.trim())
                } else if let Some(rest) = trimmed.strip_prefix("prefix:") {
                    (MatchType::Prefix, rest.trim())
                } else if let Some(rest) = trimmed.strip_prefix("contains:") {
                    (MatchType::Contains, rest.trim())
                } else {
                    // Default to exact match
                    (MatchType::Exact, trimmed)
                };

                if !name.is_empty() {
                    denylist.add(name, match_type);
                }
            }
        }

        denylist
    }
}

/// Merged configuration from file and CLI arguments
///
/// This struct holds the final configuration after merging file and CLI values.
/// CLI arguments always take precedence over file values.
#[derive(Debug, Clone)]
pub struct Config {
    /// Hotkey chord to trigger picker
    pub hotkey: String,

    /// Output buffer capacity in bytes
    pub buffer_capacity: usize,

    /// Unix socket path for daemon connection
    pub daemon_socket: Option<PathBuf>,

    /// Denylist patterns for privacy
    pub denylist: Denylist,

    /// Execute command immediately after selection
    pub execute_on_select: bool,

    /// Path to the configuration file that was loaded (if any)
    pub config_path: Option<PathBuf>,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            hotkey: DEFAULT_HOTKEY.to_string(),
            buffer_capacity: DEFAULT_BUFFER_CAP,
            daemon_socket: None,
            denylist: Denylist::with_defaults(),
            execute_on_select: false,
            config_path: None,
        }
    }
}

impl Config {
    /// Create a new configuration with default values
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Load configuration from the default locations
    ///
    /// Searches for configuration files in standard locations.
    /// Returns a default configuration if no file is found.
    #[must_use]
    pub fn load_default() -> Self {
        find_config_file().map_or_else(Self::default, |path| {
            match ConfigFile::load(&path) {
                Ok(file_config) => {
                    let mut config = Self::from_file(&file_config);
                    config.config_path = Some(path);
                    config
                }
                Err(e) => {
                    tracing::warn!("Failed to load config file: {e}");
                    Self::default()
                }
            }
        })
    }

    /// Load configuration from a specific file path
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be read or parsed.
    pub fn load_from_path(path: &Path) -> Result<Self, ConfigError> {
        let file_config = ConfigFile::load(path)?;
        let mut config = Self::from_file(&file_config);
        config.config_path = Some(path.to_path_buf());
        Ok(config)
    }

    /// Create configuration from a loaded config file
    fn from_file(file: &ConfigFile) -> Self {
        let mut config = Self::default();

        if let Some(ref hotkey) = file.hotkey {
            config.hotkey.clone_from(hotkey);
        }

        if let Some(cap) = file.buffer_capacity {
            config.buffer_capacity = cap;
        }

        if let Some(ref socket) = file.daemon_socket {
            config.daemon_socket = Some(socket.clone());
        }

        if let Some(exec) = file.execute_on_select {
            config.execute_on_select = exec;
        }

        // Merge file denylist with defaults
        if file.denylist.is_some() {
            let file_denylist = file.parse_denylist();
            config.denylist.merge(&file_denylist);
        }

        config
    }

    /// Merge CLI arguments into this configuration
    ///
    /// CLI arguments take precedence over file values.
    pub fn merge_cli(&mut self, cli: &Cli) {
        // Hotkey from CLI takes precedence
        if let Some(ref hotkey) = cli.hotkey {
            self.hotkey.clone_from(hotkey);
        }

        // Buffer capacity from CLI (only if different from default, since CLI always has a value)
        if cli.buffer_cap != DEFAULT_BUFFER_CAP {
            self.buffer_capacity = cli.buffer_cap;
        }

        // Daemon socket from CLI takes precedence
        if let Some(ref socket) = cli.daemon_socket {
            self.daemon_socket = Some(socket.clone());
        }

        // Execute on select from CLI takes precedence if true
        if cli.execute_on_select {
            self.execute_on_select = true;
        }
    }

    /// Load configuration from default locations and merge with CLI arguments
    ///
    /// This is the main entry point for loading configuration.
    /// It loads the configuration file (if found), then merges CLI arguments.
    #[must_use]
    pub fn load_and_merge(cli: &Cli) -> Self {
        let mut config = Self::load_default();
        config.merge_cli(cli);
        config
    }
}

/// Find the configuration file from standard locations
///
/// Searches in order:
/// 1. `$XDG_CONFIG_HOME/clai/wrap.toml` (or `~/.config/clai/wrap.toml`)
/// 2. `~/.clai-wrap.toml`
///
/// On Windows:
/// 1. `%APPDATA%/clai/wrap.toml`
/// 2. `~/.clai-wrap.toml`
#[must_use]
pub fn find_config_file() -> Option<PathBuf> {
    // Try XDG config location first
    if let Some(path) = get_xdg_config_path() {
        if path.exists() {
            return Some(path);
        }
    }

    // Try legacy location
    if let Some(path) = get_legacy_config_path() {
        if path.exists() {
            return Some(path);
        }
    }

    None
}

/// Get the XDG-standard configuration path
///
/// Returns `$XDG_CONFIG_HOME/clai/wrap.toml` on Unix or `%APPDATA%/clai/wrap.toml` on Windows.
#[must_use]
pub fn get_xdg_config_path() -> Option<PathBuf> {
    #[cfg(unix)]
    {
        // Try XDG_CONFIG_HOME first, then fall back to ~/.config
        if let Ok(xdg_config) = std::env::var("XDG_CONFIG_HOME") {
            let path = PathBuf::from(xdg_config).join("clai").join("wrap.toml");
            return Some(path);
        }

        // Fall back to ~/.config
        if let Some(home) = home_dir() {
            let path = home.join(".config").join("clai").join("wrap.toml");
            return Some(path);
        }

        None
    }

    #[cfg(windows)]
    {
        // Use %APPDATA% on Windows
        if let Ok(appdata) = std::env::var("APPDATA") {
            let path = PathBuf::from(appdata).join("clai").join("wrap.toml");
            return Some(path);
        }

        None
    }

    #[cfg(not(any(unix, windows)))]
    {
        None
    }
}

/// Get the legacy configuration path
///
/// Returns `~/.clai-wrap.toml`
#[must_use]
pub fn get_legacy_config_path() -> Option<PathBuf> {
    home_dir().map(|home| home.join(".clai-wrap.toml"))
}

/// Get the user's home directory
fn home_dir() -> Option<PathBuf> {
    #[cfg(unix)]
    {
        std::env::var("HOME").ok().map(PathBuf::from)
    }

    #[cfg(windows)]
    {
        std::env::var("USERPROFILE").ok().map(PathBuf::from)
    }

    #[cfg(not(any(unix, windows)))]
    {
        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn test_config_file_defaults() {
        let config = ConfigFile::default();
        assert!(config.hotkey.is_none());
        assert!(config.buffer_capacity.is_none());
        assert!(config.daemon_socket.is_none());
        assert!(config.denylist.is_none());
        assert!(config.execute_on_select.is_none());
    }

    #[test]
    fn test_config_defaults() {
        let config = Config::default();
        assert_eq!(config.hotkey, DEFAULT_HOTKEY);
        assert_eq!(config.buffer_capacity, DEFAULT_BUFFER_CAP);
        assert!(config.daemon_socket.is_none());
        assert!(!config.execute_on_select);
        assert!(config.config_path.is_none());
        // Default denylist should have entries
        assert!(!config.denylist.is_empty());
    }

    #[test]
    fn test_load_config_file() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = r#"
hotkey = "ctrl-a"
buffer_capacity = 4194304
daemon_socket = "/tmp/clai.sock"
execute_on_select = true
denylist = ["ssh", "exact:mysql", "prefix:docker", "contains:password"]
"#;

        std::fs::write(&config_path, config_content).unwrap();

        let file_config = ConfigFile::load(&config_path).unwrap();

        assert_eq!(file_config.hotkey, Some("ctrl-a".to_string()));
        assert_eq!(file_config.buffer_capacity, Some(4_194_304));
        assert_eq!(
            file_config.daemon_socket,
            Some(PathBuf::from("/tmp/clai.sock"))
        );
        assert_eq!(file_config.execute_on_select, Some(true));
        assert_eq!(
            file_config.denylist,
            Some(vec![
                "ssh".to_string(),
                "exact:mysql".to_string(),
                "prefix:docker".to_string(),
                "contains:password".to_string(),
            ])
        );
    }

    #[test]
    fn test_load_partial_config() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        // Only specify hotkey
        let config_content = r#"
hotkey = "ctrl-b"
"#;

        std::fs::write(&config_path, config_content).unwrap();

        let file_config = ConfigFile::load(&config_path).unwrap();

        assert_eq!(file_config.hotkey, Some("ctrl-b".to_string()));
        assert!(file_config.buffer_capacity.is_none());
        assert!(file_config.daemon_socket.is_none());
        assert!(file_config.execute_on_select.is_none());
        assert!(file_config.denylist.is_none());
    }

    #[test]
    fn test_load_empty_config() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        std::fs::write(&config_path, "").unwrap();

        let file_config = ConfigFile::load(&config_path).unwrap();

        assert!(file_config.hotkey.is_none());
        assert!(file_config.buffer_capacity.is_none());
    }

    #[test]
    fn test_config_validation_zero_buffer() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = "buffer_capacity = 0";
        std::fs::write(&config_path, config_content).unwrap();

        let result = ConfigFile::load(&config_path);
        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("buffer_capacity"));
    }

    #[test]
    fn test_config_validation_empty_hotkey() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = r#"hotkey = """#;
        std::fs::write(&config_path, config_content).unwrap();

        let result = ConfigFile::load(&config_path);
        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("hotkey"));
    }

    #[test]
    fn test_config_validation_empty_denylist_pattern() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = r#"denylist = ["ssh", ""]"#;
        std::fs::write(&config_path, config_content).unwrap();

        let result = ConfigFile::load(&config_path);
        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("denylist"));
    }

    #[test]
    fn test_parse_denylist_exact() {
        let file_config = ConfigFile {
            denylist: Some(vec!["ssh".to_string(), "exact:mysql".to_string()]),
            ..Default::default()
        };

        let denylist = file_config.parse_denylist();

        assert!(denylist.is_denied("ssh"));
        assert!(denylist.is_denied("mysql"));
        assert!(!denylist.is_denied("sshd")); // Exact match only
        assert!(!denylist.is_denied("mysqld")); // Exact match only
    }

    #[test]
    fn test_parse_denylist_prefix() {
        let file_config = ConfigFile {
            denylist: Some(vec!["prefix:docker".to_string()]),
            ..Default::default()
        };

        let denylist = file_config.parse_denylist();

        assert!(denylist.is_denied("docker"));
        assert!(denylist.is_denied("dockerd"));
        assert!(denylist.is_denied("docker-compose"));
        assert!(!denylist.is_denied("podman-docker"));
    }

    #[test]
    fn test_parse_denylist_contains() {
        let file_config = ConfigFile {
            denylist: Some(vec!["contains:password".to_string()]),
            ..Default::default()
        };

        let denylist = file_config.parse_denylist();

        assert!(denylist.is_denied("password"));
        assert!(denylist.is_denied("change-password"));
        assert!(denylist.is_denied("password-manager"));
        assert!(!denylist.is_denied("passwd"));
    }

    #[test]
    fn test_parse_denylist_empty_patterns_ignored() {
        let file_config = ConfigFile {
            denylist: Some(vec![
                "ssh".to_string(),
                String::new(),
                "  ".to_string(),
                "exact:".to_string(),
                "mysql".to_string(),
            ]),
            ..Default::default()
        };

        let denylist = file_config.parse_denylist();

        assert_eq!(denylist.len(), 2);
        assert!(denylist.is_denied("ssh"));
        assert!(denylist.is_denied("mysql"));
    }

    #[test]
    fn test_config_from_file() {
        let file_config = ConfigFile {
            hotkey: Some("ctrl-x".to_string()),
            buffer_capacity: Some(1_000_000),
            daemon_socket: Some(PathBuf::from("/custom/socket")),
            execute_on_select: Some(true),
            denylist: Some(vec!["custom-app".to_string()]),
        };

        let config = Config::from_file(&file_config);

        assert_eq!(config.hotkey, "ctrl-x");
        assert_eq!(config.buffer_capacity, 1_000_000);
        assert_eq!(config.daemon_socket, Some(PathBuf::from("/custom/socket")));
        assert!(config.execute_on_select);

        // Should have default denylist entries plus custom one
        assert!(config.denylist.is_denied("ssh")); // Default
        assert!(config.denylist.is_denied("custom-app")); // Custom
    }

    #[test]
    fn test_config_from_file_partial() {
        let file_config = ConfigFile {
            hotkey: Some("ctrl-y".to_string()),
            ..Default::default()
        };

        let config = Config::from_file(&file_config);

        assert_eq!(config.hotkey, "ctrl-y");
        // Other values should be defaults
        assert_eq!(config.buffer_capacity, DEFAULT_BUFFER_CAP);
        assert!(config.daemon_socket.is_none());
        assert!(!config.execute_on_select);
    }

    #[test]
    fn test_merge_cli_hotkey() {
        let mut config = Config::default();
        let cli = Cli::parse_from_args(["clai-wrap", "--hotkey", "ctrl-z"]);

        config.merge_cli(&cli);

        assert_eq!(config.hotkey, "ctrl-z");
    }

    #[test]
    fn test_merge_cli_buffer_cap() {
        let mut config = Config::default();
        let cli = Cli::parse_from_args(["clai-wrap", "--buffer-cap", "8388608"]);

        config.merge_cli(&cli);

        assert_eq!(config.buffer_capacity, 8_388_608);
    }

    #[test]
    fn test_merge_cli_default_buffer_cap_no_override() {
        let mut config = Config {
            buffer_capacity: 1_000_000, // Custom value from file
            ..Default::default()
        };
        // CLI with default buffer cap should not override
        let cli = Cli::parse_from_args(["clai-wrap"]);

        config.merge_cli(&cli);

        // Should keep file value since CLI is default
        assert_eq!(config.buffer_capacity, 1_000_000);
    }

    #[test]
    fn test_merge_cli_daemon_socket() {
        let mut config = Config::default();
        let cli = Cli::parse_from_args(["clai-wrap", "--daemon-socket", "/cli/socket"]);

        config.merge_cli(&cli);

        assert_eq!(config.daemon_socket, Some(PathBuf::from("/cli/socket")));
    }

    #[test]
    fn test_merge_cli_daemon_socket_overrides_file() {
        let mut config = Config {
            daemon_socket: Some(PathBuf::from("/file/socket")),
            ..Default::default()
        };
        let cli = Cli::parse_from_args(["clai-wrap", "--daemon-socket", "/cli/socket"]);

        config.merge_cli(&cli);

        assert_eq!(config.daemon_socket, Some(PathBuf::from("/cli/socket")));
    }

    #[test]
    fn test_merge_cli_execute_on_select() {
        let mut config = Config::default();
        let cli = Cli::parse_from_args(["clai-wrap", "--execute-on-select"]);

        config.merge_cli(&cli);

        assert!(config.execute_on_select);
    }

    #[test]
    fn test_merge_cli_execute_on_select_preserves_file_value() {
        let mut config = Config {
            execute_on_select: true,
            ..Default::default()
        };
        // CLI without --execute-on-select should preserve file value
        let cli = Cli::parse_from_args(["clai-wrap"]);

        config.merge_cli(&cli);

        assert!(config.execute_on_select);
    }

    #[test]
    fn test_merge_cli_full() {
        let file_config = ConfigFile {
            hotkey: Some("ctrl-f".to_string()),
            buffer_capacity: Some(500_000),
            daemon_socket: Some(PathBuf::from("/file/socket")),
            execute_on_select: Some(false),
            denylist: Some(vec!["file-app".to_string()]),
        };

        let mut config = Config::from_file(&file_config);

        let cli = Cli::parse_from_args([
            "clai-wrap",
            "--hotkey",
            "ctrl-c",
            "--buffer-cap",
            "1000000",
            "--daemon-socket",
            "/cli/socket",
            "--execute-on-select",
        ]);

        config.merge_cli(&cli);

        // CLI values should override file values
        assert_eq!(config.hotkey, "ctrl-c");
        assert_eq!(config.buffer_capacity, 1_000_000);
        assert_eq!(config.daemon_socket, Some(PathBuf::from("/cli/socket")));
        assert!(config.execute_on_select);

        // Denylist should still have file entries
        assert!(config.denylist.is_denied("file-app"));
    }

    #[test]
    fn test_load_from_path() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = r#"
hotkey = "ctrl-l"
buffer_capacity = 2000000
"#;

        std::fs::write(&config_path, config_content).unwrap();

        let config = Config::load_from_path(&config_path).unwrap();

        assert_eq!(config.hotkey, "ctrl-l");
        assert_eq!(config.buffer_capacity, 2_000_000);
        assert_eq!(config.config_path, Some(config_path));
    }

    #[test]
    fn test_load_from_path_not_found() {
        let result = Config::load_from_path(Path::new("/nonexistent/path/wrap.toml"));
        assert!(result.is_err());
    }

    #[test]
    fn test_load_from_path_invalid_toml() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        // Invalid TOML syntax
        std::fs::write(&config_path, "this is not valid = toml [").unwrap();

        let result = Config::load_from_path(&config_path);
        assert!(result.is_err());
    }

    #[test]
    fn test_load_and_merge() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = r#"
hotkey = "ctrl-m"
buffer_capacity = 3000000
"#;

        std::fs::write(&config_path, config_content).unwrap();

        // Load from specific path (simulating find_config_file)
        let mut config = Config::load_from_path(&config_path).unwrap();

        // Merge CLI args that override some values
        let cli = Cli::parse_from_args(["clai-wrap", "--hotkey", "ctrl-n"]);
        config.merge_cli(&cli);

        // CLI hotkey should override file
        assert_eq!(config.hotkey, "ctrl-n");
        // File buffer capacity should be preserved (CLI is default)
        assert_eq!(config.buffer_capacity, 3_000_000);
    }

    #[test]
    fn test_config_error_display() {
        let io_err = ConfigError::ReadError(std::io::Error::new(
            std::io::ErrorKind::NotFound,
            "not found",
        ));
        assert!(io_err.to_string().contains("read config file"));

        let invalid_err = ConfigError::InvalidConfig("test error".to_string());
        assert!(invalid_err.to_string().contains("test error"));
    }

    #[test]
    fn test_get_xdg_config_path_with_env() {
        // Save current env vars
        let original_xdg = std::env::var("XDG_CONFIG_HOME").ok();

        // Set XDG_CONFIG_HOME
        std::env::set_var("XDG_CONFIG_HOME", "/custom/config");

        #[cfg(unix)]
        {
            let path = get_xdg_config_path();
            assert_eq!(path, Some(PathBuf::from("/custom/config/clai/wrap.toml")));
        }

        // Restore env var
        if let Some(val) = original_xdg {
            std::env::set_var("XDG_CONFIG_HOME", val);
        } else {
            std::env::remove_var("XDG_CONFIG_HOME");
        }
    }

    #[test]
    fn test_get_legacy_config_path() {
        #[cfg(unix)]
        {
            if let Ok(home) = std::env::var("HOME") {
                let path = get_legacy_config_path();
                assert_eq!(
                    path,
                    Some(PathBuf::from(format!("{home}/.clai-wrap.toml")))
                );
            }
        }
    }

    #[test]
    fn test_find_config_file_not_found() {
        // Save current env vars
        let original_xdg = std::env::var("XDG_CONFIG_HOME").ok();
        let original_home = std::env::var("HOME").ok();

        // Set to nonexistent paths
        std::env::set_var("XDG_CONFIG_HOME", "/nonexistent/xdg");
        std::env::set_var("HOME", "/nonexistent/home");

        let path = find_config_file();
        assert!(path.is_none());

        // Restore env vars
        if let Some(val) = original_xdg {
            std::env::set_var("XDG_CONFIG_HOME", val);
        } else {
            std::env::remove_var("XDG_CONFIG_HOME");
        }
        if let Some(val) = original_home {
            std::env::set_var("HOME", val);
        } else {
            std::env::remove_var("HOME");
        }
    }

    #[test]
    fn test_find_config_file_xdg_priority() {
        let temp_dir = TempDir::new().unwrap();

        // Create XDG config path
        let xdg_dir = temp_dir.path().join("xdg");
        let xdg_config_dir = xdg_dir.join("clai");
        std::fs::create_dir_all(&xdg_config_dir).unwrap();
        let xdg_config_path = xdg_config_dir.join("wrap.toml");
        std::fs::write(&xdg_config_path, "hotkey = \"xdg\"").unwrap();

        // Create legacy config path
        let home_dir = temp_dir.path().join("home");
        std::fs::create_dir_all(&home_dir).unwrap();
        let legacy_path = home_dir.join(".clai-wrap.toml");
        std::fs::write(&legacy_path, "hotkey = \"legacy\"").unwrap();

        // Save current env vars
        let original_xdg = std::env::var("XDG_CONFIG_HOME").ok();
        let original_home = std::env::var("HOME").ok();

        // Set env vars to temp dirs
        std::env::set_var("XDG_CONFIG_HOME", &xdg_dir);
        std::env::set_var("HOME", &home_dir);

        let found = find_config_file();
        assert_eq!(found, Some(xdg_config_path));

        // Restore env vars
        if let Some(val) = original_xdg {
            std::env::set_var("XDG_CONFIG_HOME", val);
        } else {
            std::env::remove_var("XDG_CONFIG_HOME");
        }
        if let Some(val) = original_home {
            std::env::set_var("HOME", val);
        } else {
            std::env::remove_var("HOME");
        }
    }

    #[test]
    fn test_find_config_file_legacy_fallback() {
        let temp_dir = TempDir::new().unwrap();

        // Create only legacy config path (no XDG)
        let home_dir = temp_dir.path().join("home");
        std::fs::create_dir_all(&home_dir).unwrap();
        let legacy_path = home_dir.join(".clai-wrap.toml");
        std::fs::write(&legacy_path, "hotkey = \"legacy\"").unwrap();

        // Save current env vars
        let original_xdg = std::env::var("XDG_CONFIG_HOME").ok();
        let original_home = std::env::var("HOME").ok();

        // Set env vars - XDG to nonexistent, HOME to temp
        std::env::set_var("XDG_CONFIG_HOME", "/nonexistent/xdg");
        std::env::set_var("HOME", &home_dir);

        let found = find_config_file();
        assert_eq!(found, Some(legacy_path));

        // Restore env vars
        if let Some(val) = original_xdg {
            std::env::set_var("XDG_CONFIG_HOME", val);
        } else {
            std::env::remove_var("XDG_CONFIG_HOME");
        }
        if let Some(val) = original_home {
            std::env::set_var("HOME", val);
        } else {
            std::env::remove_var("HOME");
        }
    }

    #[test]
    fn test_config_with_comments_in_toml() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        let config_content = r#"
# This is a comment
hotkey = "ctrl-h"  # Inline comment

# Buffer settings
buffer_capacity = 4194304

# More comments
denylist = [
    "ssh",  # Remote access
    "vim",  # Editor
]
"#;

        std::fs::write(&config_path, config_content).unwrap();

        let file_config = ConfigFile::load(&config_path).unwrap();

        assert_eq!(file_config.hotkey, Some("ctrl-h".to_string()));
        assert_eq!(file_config.buffer_capacity, Some(4_194_304));
        assert_eq!(
            file_config.denylist,
            Some(vec!["ssh".to_string(), "vim".to_string()])
        );
    }

    #[test]
    fn test_config_file_equality() {
        let config1 = ConfigFile {
            hotkey: Some("ctrl-a".to_string()),
            buffer_capacity: Some(1000),
            daemon_socket: None,
            denylist: Some(vec!["ssh".to_string()]),
            execute_on_select: Some(true),
        };

        let config2 = ConfigFile {
            hotkey: Some("ctrl-a".to_string()),
            buffer_capacity: Some(1000),
            daemon_socket: None,
            denylist: Some(vec!["ssh".to_string()]),
            execute_on_select: Some(true),
        };

        let config3 = ConfigFile {
            hotkey: Some("ctrl-b".to_string()),
            ..Default::default()
        };

        assert_eq!(config1, config2);
        assert_ne!(config1, config3);
    }

    #[test]
    fn test_config_new() {
        let config = Config::new();
        assert_eq!(config.hotkey, DEFAULT_HOTKEY);
        assert_eq!(config.buffer_capacity, DEFAULT_BUFFER_CAP);
    }

    #[test]
    fn test_toml_with_extra_fields_ignored() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("wrap.toml");

        // Include an unknown field
        let config_content = r#"
hotkey = "ctrl-u"
unknown_field = "should be ignored"
another_unknown = 42
"#;

        std::fs::write(&config_path, config_content).unwrap();

        // Should parse successfully, ignoring unknown fields
        let file_config = ConfigFile::load(&config_path).unwrap();
        assert_eq!(file_config.hotkey, Some("ctrl-u".to_string()));
    }
}
