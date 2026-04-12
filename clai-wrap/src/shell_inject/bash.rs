//! Bash shell integration injection for OSC 133 semantic prompt support.
//!
//! This module provides the `BashInjector` which creates a temporary init file
//! that sources system and user bashrc files, then injects OSC 133 hooks
//! for semantic shell integration.
//!
//! # OSC 133 Sequences
//!
//! - `\e]133;A\a` - Prompt start
//! - `\e]133;B\a` - Input start (end of prompt)
//! - `\e]133;C\a` - Output start (command execution begins)
//! - `\e]133;D;$?\a` - Finished (command completed with exit code)
//!
//! # Usage
//!
//! ```no_run
//! use clai_wrap::shell_inject::BashInjector;
//!
//! let injector = BashInjector::new().expect("failed to create injector");
//! let args = injector.shell_args();
//! // Launch bash with: bash --rcfile <path>
//! ```

use std::fs::{self, File};
use std::io::Write;
use std::path::{Path, PathBuf};

use thiserror::Error;

use crate::temp_dir::{TempDirError, TempDirManager};

/// Errors that can occur during Bash injection setup.
#[derive(Debug, Error)]
pub enum BashInjectorError {
    /// Failed to create managed temporary directory
    #[error("failed to create managed temp directory: {0}")]
    TempDir(#[from] TempDirError),

    /// Failed to create init file
    #[error("failed to create init file: {0}")]
    FileCreation(#[source] std::io::Error),

    /// Failed to write init file contents
    #[error("failed to write init file: {0}")]
    FileWrite(#[source] std::io::Error),

    /// Failed to set permissions on temp directory
    #[error("failed to set permissions: {0}")]
    Permissions(#[source] std::io::Error),
}

/// The Bash init file content that will be sourced via --rcfile.
///
/// This script:
/// 1. Sources system bashrc files (Debian/Ubuntu and RHEL/CentOS locations)
/// 2. Sources user's ~/.bashrc if it exists
/// 3. Sets up OSC 133 prompt hooks for semantic shell integration
const BASH_INIT_CONTENT: &str = r#"# clai-wrap bash integration
# This file is auto-generated - do not edit

# Source system bashrc first (Debian/Ubuntu location)
[ -f /etc/bash.bashrc ] && . /etc/bash.bashrc

# Source system bashrc (RHEL/CentOS location)
[ -f /etc/bashrc ] && . /etc/bashrc

# Source user's bashrc
[ -f ~/.bashrc ] && . ~/.bashrc

# clai shell integration - OSC 133 semantic prompt sequences
# These sequences allow clai-wrap to track command boundaries

# Called before each prompt is displayed
# Outputs: D (finished with exit code) then A (prompt start)
__clai_prompt_command() {
    local exit_code=$?
    printf '\e]133;D;%d\a' "$exit_code"
    printf '\e]133;A\a'
}

# Prepend our function to PROMPT_COMMAND
# This ensures it runs before any user-defined prompt commands
PROMPT_COMMAND="__clai_prompt_command${PROMPT_COMMAND:+;$PROMPT_COMMAND}"

# Append OSC 133;B to PS1 (marks end of prompt / start of user input)
# The \[...\] tells bash these are non-printing characters for proper line wrapping
PS1="${PS1}\[\e]133;B\a\]"

# Use DEBUG trap to mark start of command output
# This runs just before each command is executed
trap 'printf "\e]133;C\a"' DEBUG
"#;

/// Filename for the generated init script.
const INIT_FILENAME: &str = "init.bash";

/// Bash shell integration injector.
///
/// Creates a temporary directory containing an init script that sources
/// system and user bashrc files, then injects OSC 133 hooks. The temporary
/// directory is automatically cleaned up when the `BashInjector` is dropped.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::shell_inject::BashInjector;
///
/// let injector = BashInjector::new().expect("failed to create injector");
///
/// // Get the arguments to pass to bash
/// let args = injector.shell_args();
/// assert_eq!(args.len(), 2);
/// assert_eq!(args[0], "--rcfile");
///
/// // The injector must be kept alive while bash is running
/// // because it owns the temp directory
/// ```
pub struct BashInjector {
    /// Managed session-scoped temp directory owner.
    #[allow(dead_code)] // Keeps session temp dir alive for injector lifetime.
    manager: TempDirManager,

    /// Shell-specific directory for bash injection files.
    shell_dir: PathBuf,

    /// Path to the generated rcfile.
    rcfile_path: PathBuf,
}

impl BashInjector {
    /// Creates a new `BashInjector` with a temporary init file.
    ///
    /// This creates a temporary directory with appropriate permissions (0700 on Unix)
    /// and writes the bash init script to it.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - The temporary directory cannot be created
    /// - The init file cannot be written
    /// - Permissions cannot be set (Unix only)
    pub fn new() -> Result<Self, BashInjectorError> {
        let manager = TempDirManager::new()?;
        let shell_dir = manager.shell_dir("bash")?;
        let rcfile_path = shell_dir.join(INIT_FILENAME);

        // Write the init file
        let mut file = File::create(&rcfile_path).map_err(BashInjectorError::FileCreation)?;

        file.write_all(BASH_INIT_CONTENT.as_bytes())
            .map_err(BashInjectorError::FileWrite)?;

        // Set file permissions to 0600 on Unix
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let permissions = fs::Permissions::from_mode(0o600);
            fs::set_permissions(&rcfile_path, permissions)
                .map_err(BashInjectorError::Permissions)?;
        }

        Ok(Self {
            manager,
            shell_dir,
            rcfile_path,
        })
    }

    /// Returns the arguments to pass to bash for using the injected rcfile.
    ///
    /// This returns `["--rcfile", "/path/to/temp/init.bash"]`.
    ///
    /// # Example
    ///
    /// ```no_run
    /// use clai_wrap::shell_inject::BashInjector;
    /// use std::process::Command;
    ///
    /// let injector = BashInjector::new().unwrap();
    /// let args = injector.shell_args();
    ///
    /// // Launch bash with the injected rcfile
    /// let mut cmd = Command::new("bash");
    /// cmd.args(&args);
    /// ```
    pub fn shell_args(&self) -> Vec<String> {
        vec![
            "--rcfile".to_string(),
            self.rcfile_path.to_string_lossy().into_owned(),
        ]
    }

    /// Returns a reference to the rcfile path.
    ///
    /// This is the path to the temporary init script that should be
    /// passed to bash via `--rcfile`.
    pub fn rcfile(&self) -> &Path {
        &self.rcfile_path
    }

    /// Returns a reference to the temporary directory.
    ///
    /// The temporary directory is automatically cleaned up when the
    /// `BashInjector` is dropped.
    pub fn temp_dir(&self) -> &Path {
        self.shell_dir.as_path()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_new_creates_temp_file() {
        let injector = BashInjector::new().expect("failed to create injector");

        // Verify temp directory exists
        assert!(injector.temp_dir().exists());
        assert!(injector.temp_dir().is_dir());

        // Verify rcfile exists
        assert!(injector.rcfile().exists());
        assert!(injector.rcfile().is_file());
    }

    #[test]
    fn test_init_content_sources_system_bashrc() {
        let injector = BashInjector::new().expect("failed to create injector");
        let content = fs::read_to_string(injector.rcfile()).expect("failed to read rcfile");

        // Check for Debian/Ubuntu location
        assert!(
            content.contains("[ -f /etc/bash.bashrc ] && . /etc/bash.bashrc"),
            "should source /etc/bash.bashrc"
        );

        // Check for RHEL/CentOS location
        assert!(
            content.contains("[ -f /etc/bashrc ] && . /etc/bashrc"),
            "should source /etc/bashrc"
        );
    }

    #[test]
    fn test_init_content_sources_user_bashrc() {
        let injector = BashInjector::new().expect("failed to create injector");
        let content = fs::read_to_string(injector.rcfile()).expect("failed to read rcfile");

        assert!(
            content.contains("[ -f ~/.bashrc ] && . ~/.bashrc"),
            "should source ~/.bashrc"
        );
    }

    #[test]
    fn test_init_content_has_osc133_hooks() {
        let injector = BashInjector::new().expect("failed to create injector");
        let content = fs::read_to_string(injector.rcfile()).expect("failed to read rcfile");

        // Check for prompt command function
        assert!(
            content.contains("__clai_prompt_command()"),
            "should define __clai_prompt_command function"
        );

        // Check for OSC 133 A (prompt start)
        assert!(
            content.contains(r"printf '\e]133;A\a'"),
            "should emit OSC 133;A"
        );

        // Check for OSC 133 B (input start) in PS1
        assert!(
            content.contains(r"\[\e]133;B\a\]"),
            "should add OSC 133;B to PS1"
        );

        // Check for OSC 133 C (output start) in DEBUG trap
        assert!(
            content.contains(r#"trap 'printf "\e]133;C\a"' DEBUG"#),
            "should set DEBUG trap for OSC 133;C"
        );

        // Check for OSC 133 D (finished with exit code)
        assert!(
            content.contains(r"printf '\e]133;D;%d\a'"),
            "should emit OSC 133;D with exit code"
        );
    }

    #[test]
    fn test_init_preserves_existing_prompt_command() {
        let injector = BashInjector::new().expect("failed to create injector");
        let content = fs::read_to_string(injector.rcfile()).expect("failed to read rcfile");

        // Should append to existing PROMPT_COMMAND, not replace it
        assert!(
            content.contains("${PROMPT_COMMAND:+;$PROMPT_COMMAND}"),
            "should preserve existing PROMPT_COMMAND"
        );
    }

    #[test]
    fn test_shell_args_returns_correct_values() {
        let injector = BashInjector::new().expect("failed to create injector");
        let args = injector.shell_args();

        assert_eq!(args.len(), 2);
        assert_eq!(args[0], "--rcfile");
        assert!(
            args[1].ends_with("init.bash"),
            "second arg should be path to init.bash, got: {}",
            args[1]
        );
        assert!(
            args[1].contains("/bash/") || args[1].contains("\\bash\\"),
            "path should contain managed bash subdirectory, got: {}",
            args[1]
        );
    }

    #[test]
    fn test_rcfile_returns_correct_path() {
        let injector = BashInjector::new().expect("failed to create injector");

        assert_eq!(
            injector.rcfile().file_name().and_then(|n| n.to_str()),
            Some("init.bash")
        );
        assert!(injector.rcfile().starts_with(injector.temp_dir()));
    }

    #[test]
    fn test_cleanup_on_drop() {
        let temp_dir_path;
        let rcfile_path;

        {
            let injector = BashInjector::new().expect("failed to create injector");
            temp_dir_path = injector.temp_dir().to_path_buf();
            rcfile_path = injector.rcfile().to_path_buf();

            // Verify files exist while injector is alive
            assert!(temp_dir_path.exists());
            assert!(rcfile_path.exists());
        }

        // After drop, temp directory should be cleaned up
        assert!(
            !temp_dir_path.exists(),
            "temp directory should be cleaned up on drop"
        );
        assert!(!rcfile_path.exists(), "rcfile should be cleaned up on drop");
    }

    #[cfg(unix)]
    #[test]
    fn test_permissions_are_secure() {
        use std::os::unix::fs::PermissionsExt;

        let injector = BashInjector::new().expect("failed to create injector");

        // Check directory permissions (should be 0700)
        let dir_metadata = fs::metadata(injector.temp_dir()).expect("failed to get dir metadata");
        let dir_mode = dir_metadata.permissions().mode() & 0o777;
        assert_eq!(
            dir_mode, 0o700,
            "temp directory should have 0700 permissions, got {dir_mode:o}"
        );

        // Check file permissions (should be 0600)
        let file_metadata = fs::metadata(injector.rcfile()).expect("failed to get file metadata");
        let file_mode = file_metadata.permissions().mode() & 0o777;
        assert_eq!(
            file_mode, 0o600,
            "rcfile should have 0600 permissions, got {file_mode:o}"
        );
    }

    #[test]
    fn test_multiple_injectors_are_independent() {
        let injector1 = BashInjector::new().expect("failed to create injector 1");
        let injector2 = BashInjector::new().expect("failed to create injector 2");

        // Each should have its own temp directory
        assert_ne!(
            injector1.temp_dir(),
            injector2.temp_dir(),
            "injectors should have different temp directories"
        );

        // Each should have its own rcfile
        assert_ne!(
            injector1.rcfile(),
            injector2.rcfile(),
            "injectors should have different rcfiles"
        );

        // Both should exist
        assert!(injector1.rcfile().exists());
        assert!(injector2.rcfile().exists());
    }

    #[test]
    fn test_init_content_is_valid_bash() {
        // This test validates the structure of the init content
        // We can't actually run bash in unit tests, but we can check syntax patterns

        let injector = BashInjector::new().expect("failed to create injector");
        let content = fs::read_to_string(injector.rcfile()).expect("failed to read rcfile");

        // Check for balanced brackets in conditionals
        let open_brackets = content.matches("[ ").count();
        let close_brackets = content.matches(" ]").count();
        assert_eq!(
            open_brackets, close_brackets,
            "should have balanced brackets"
        );

        // Check for function definition pattern
        assert!(
            content.contains("() {") && content.contains('}'),
            "should have properly defined function"
        );

        // Check that local variable is declared
        assert!(
            content.contains("local exit_code=$?"),
            "should capture exit code in local variable"
        );
    }
}
