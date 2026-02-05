//! Zsh shell integration injector.
//!
//! This module provides the [`ZshInjector`] struct that creates a temporary
//! ZDOTDIR with wrapper configuration files to inject OSC 133 shell
//! integration sequences into Zsh.
//!
//! # OSC 133 Sequences
//!
//! The injected hooks emit the following OSC 133 sequences:
//!
//! - `\e]133;A\a` - Prompt start (before prompt is displayed)
//! - `\e]133;B\a` - Input start (after prompt, before command input)
//! - `\e]133;C\a` - Output start (command is running)
//! - `\e]133;D;$?\a` - Finished (command completed with exit code)
//!
//! # ZDOTDIR Method
//!
//! Zsh uses the `ZDOTDIR` environment variable to locate its configuration
//! files. The injector:
//!
//! 1. Creates a temporary directory with wrapper configuration files
//! 2. The wrapper `.zshenv` sources the user's `~/.zshenv`, then resets
//!    `ZDOTDIR` back to `$HOME` so other dotfiles (`.zprofile`, `.zlogin`)
//!    are found in the normal location
//! 3. The wrapper `.zshrc` sources the user's `~/.zshrc`, then injects
//!    the OSC 133 prompt hooks

use std::fs::File;
use std::io::Write;
use std::path::Path;

use tempfile::TempDir;
use thiserror::Error;

/// Errors that can occur during Zsh injection setup.
#[derive(Debug, Error)]
pub enum ZshInjectorError {
    /// Failed to create temporary directory
    #[error("failed to create temporary directory: {0}")]
    TempDirCreation(#[source] std::io::Error),

    /// Failed to write configuration file
    #[error("failed to write {file}: {source}")]
    FileWrite {
        file: &'static str,
        #[source]
        source: std::io::Error,
    },
}

/// Zsh shell integration injector.
///
/// Creates a temporary ZDOTDIR with wrapper configuration files that inject
/// OSC 133 shell integration sequences into Zsh.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::shell_inject::ZshInjector;
///
/// let injector = ZshInjector::new().expect("failed to create injector");
///
/// // Get environment variables to set when spawning Zsh
/// for (key, value) in injector.env_vars() {
///     println!("{}={}", key, value);
/// }
/// ```
///
/// The temporary directory is automatically cleaned up when the `ZshInjector`
/// is dropped.
#[derive(Debug)]
pub struct ZshInjector {
    temp_dir: TempDir,
}

impl ZshInjector {
    /// Creates a new Zsh injector with temporary configuration files.
    ///
    /// This creates a temporary directory containing:
    /// - `.zshenv`: Sources user's .zshenv, resets ZDOTDIR, sets up early hooks
    /// - `.zshrc`: Sources user's .zshrc, injects OSC 133 prompt functions
    ///
    /// # Errors
    ///
    /// Returns an error if the temporary directory or configuration files
    /// cannot be created.
    pub fn new() -> Result<Self, ZshInjectorError> {
        let temp_dir =
            TempDir::with_prefix("clai-zsh-").map_err(ZshInjectorError::TempDirCreation)?;

        Self::write_zshenv(temp_dir.path())?;
        Self::write_zshrc(temp_dir.path())?;

        Ok(Self { temp_dir })
    }

    /// Returns environment variables to set when spawning the shell.
    ///
    /// Currently returns a single variable:
    /// - `ZDOTDIR`: Set to the temporary directory path
    pub fn env_vars(&self) -> Vec<(String, String)> {
        vec![(
            "ZDOTDIR".to_string(),
            self.temp_dir.path().to_string_lossy().into_owned(),
        )]
    }

    /// Returns the path to the temporary ZDOTDIR.
    pub fn zdotdir(&self) -> &Path {
        self.temp_dir.path()
    }

    /// Writes the wrapper .zshenv file.
    ///
    /// The .zshenv file is sourced for ALL Zsh invocations (interactive,
    /// non-interactive, login, non-login). It:
    ///
    /// 1. Saves our temp ZDOTDIR for later use in .zshrc
    /// 2. Sources the user's real ~/.zshenv if it exists
    /// 3. Resets ZDOTDIR to $HOME so .zprofile, .zlogin, etc. are found
    fn write_zshenv(dir: &Path) -> Result<(), ZshInjectorError> {
        let path = dir.join(".zshenv");
        let mut file = File::create(path).map_err(|e| ZshInjectorError::FileWrite {
            file: ".zshenv",
            source: e,
        })?;

        // The content of the wrapper .zshenv
        let content = r#"# clai-wrap: Zsh shell integration wrapper
# This file is auto-generated and sources the user's real .zshenv

# Save our temp ZDOTDIR for use in .zshrc
__CLAI_ZDOTDIR="$ZDOTDIR"

# Source user's real .zshenv first
[[ -f ~/.zshenv ]] && source ~/.zshenv

# Reset ZDOTDIR so subsequent files (.zprofile, .zlogin) load from ~
export ZDOTDIR="$HOME"
"#;

        file.write_all(content.as_bytes())
            .map_err(|e| ZshInjectorError::FileWrite {
                file: ".zshenv",
                source: e,
            })?;

        Ok(())
    }

    /// Writes the wrapper .zshrc file.
    ///
    /// The .zshrc file is sourced for interactive shells. It:
    ///
    /// 1. Sources the user's real ~/.zshrc if it exists
    /// 2. Injects OSC 133 prompt hooks AFTER user config (so our hooks
    ///    run after any user prompt customization)
    fn write_zshrc(dir: &Path) -> Result<(), ZshInjectorError> {
        let path = dir.join(".zshrc");
        let mut file = File::create(path).map_err(|e| ZshInjectorError::FileWrite {
            file: ".zshrc",
            source: e,
        })?;

        // The content of the wrapper .zshrc
        //
        // OSC 133 sequences:
        // - A: Prompt start
        // - B: Input start (after prompt)
        // - C: Output start (command running)
        // - D;N: Finished with exit code N
        //
        // Hook functions:
        // - precmd: Called before each prompt. Emits D (with previous exit code)
        //   then A (prompt start)
        // - preexec: Called after user enters command, before execution.
        //   Emits C (output start)
        //
        // PROMPT modification:
        // - Append B sequence to end of PROMPT so it's emitted after the prompt
        //   but before user input
        let content = r#"# clai-wrap: Zsh shell integration wrapper
# This file is auto-generated and sources the user's real .zshrc

# Source user's real .zshrc
[[ -f ~/.zshrc ]] && source ~/.zshrc

# --- OSC 133 Shell Integration ---
# Injected by clai-wrap for command tracking

# Track if we've already set up hooks (avoid double-injection)
if [[ -z "$__CLAI_OSC133_SETUP" ]]; then
    __CLAI_OSC133_SETUP=1

    # Store the last exit code for D sequence
    __clai_last_exit=0

    # precmd: Called before each prompt display
    # Emits: D (finished with exit code) then A (prompt start)
    __clai_precmd() {
        __clai_last_exit=$?
        # Emit OSC 133;D with exit code (finished)
        print -Pn '\e]133;D;%?\a'
        # Emit OSC 133;A (prompt start)
        print -Pn '\e]133;A\a'
    }

    # preexec: Called after command entered, before execution
    # Emits: C (output start / command running)
    __clai_preexec() {
        # Emit OSC 133;C (output start)
        print -Pn '\e]133;C\a'
    }

    # Add our hooks to the hook arrays (preserving any existing hooks)
    autoload -Uz add-zsh-hook
    add-zsh-hook precmd __clai_precmd
    add-zsh-hook preexec __clai_preexec

    # Append OSC 133;B to PROMPT (input start, after prompt)
    # Use %{ %} to mark as zero-width so Zsh counts columns correctly
    PROMPT="${PROMPT}%{$(print -Pn '\e]133;B\a')%}"
fi
"#;

        file.write_all(content.as_bytes())
            .map_err(|e| ZshInjectorError::FileWrite {
                file: ".zshrc",
                source: e,
            })?;

        Ok(())
    }

    /// Persists the temporary directory so it won't be deleted on drop.
    ///
    /// Returns the path to the directory. The caller is responsible for
    /// cleaning up the directory.
    ///
    /// This is useful for testing or when you need the directory to outlive
    /// the injector.
    #[cfg(test)]
    #[allow(deprecated)] // into_path is deprecated in favor of keep(), but keep() doesn't return the path
    pub fn persist(self) -> std::path::PathBuf {
        self.temp_dir.into_path()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_injector_creation() {
        let injector = ZshInjector::new().expect("failed to create injector");

        // Verify temp dir exists
        assert!(injector.zdotdir().exists());
        assert!(injector.zdotdir().is_dir());
    }

    #[test]
    fn test_env_vars() {
        let injector = ZshInjector::new().expect("failed to create injector");
        let env_vars = injector.env_vars();

        // Should have exactly one var: ZDOTDIR
        assert_eq!(env_vars.len(), 1);
        assert_eq!(env_vars[0].0, "ZDOTDIR");

        // ZDOTDIR should point to the temp dir
        assert_eq!(env_vars[0].1, injector.zdotdir().to_string_lossy());
    }

    #[test]
    fn test_zshenv_created() {
        let injector = ZshInjector::new().expect("failed to create injector");
        let zshenv_path = injector.zdotdir().join(".zshenv");

        assert!(zshenv_path.exists());
        assert!(zshenv_path.is_file());
    }

    #[test]
    fn test_zshenv_content() {
        let injector = ZshInjector::new().expect("failed to create injector");
        let content =
            fs::read_to_string(injector.zdotdir().join(".zshenv")).expect("failed to read .zshenv");

        // Should source user's .zshenv
        assert!(
            content.contains("[[ -f ~/.zshenv ]] && source ~/.zshenv"),
            "should source user's .zshenv"
        );

        // Should reset ZDOTDIR to HOME
        assert!(
            content.contains(r#"export ZDOTDIR="$HOME""#),
            "should reset ZDOTDIR to HOME"
        );

        // Should save our ZDOTDIR for later
        assert!(
            content.contains("__CLAI_ZDOTDIR"),
            "should save temp ZDOTDIR"
        );
    }

    #[test]
    fn test_zshrc_created() {
        let injector = ZshInjector::new().expect("failed to create injector");
        let zshrc_path = injector.zdotdir().join(".zshrc");

        assert!(zshrc_path.exists());
        assert!(zshrc_path.is_file());
    }

    #[test]
    fn test_zshrc_content() {
        let injector = ZshInjector::new().expect("failed to create injector");
        let content =
            fs::read_to_string(injector.zdotdir().join(".zshrc")).expect("failed to read .zshrc");

        // Should source user's .zshrc
        assert!(
            content.contains("[[ -f ~/.zshrc ]] && source ~/.zshrc"),
            "should source user's .zshrc"
        );

        // Should have precmd hook for A sequence (prompt start)
        assert!(content.contains("__clai_precmd"), "should have precmd hook");
        assert!(
            content.contains(r"'\e]133;A\a'"),
            "precmd should emit OSC 133;A"
        );

        // Should have preexec hook for C sequence (output start)
        assert!(
            content.contains("__clai_preexec"),
            "should have preexec hook"
        );
        assert!(
            content.contains(r"'\e]133;C\a'"),
            "preexec should emit OSC 133;C"
        );

        // Should emit D sequence (finished with exit code)
        assert!(
            content.contains(r"'\e]133;D;%?\a'"),
            "should emit OSC 133;D with exit code"
        );

        // Should modify PROMPT to include B sequence
        assert!(
            content.contains(r"'\e]133;B\a'"),
            "should emit OSC 133;B in PROMPT"
        );

        // Should use add-zsh-hook for proper hook management
        assert!(
            content.contains("add-zsh-hook precmd"),
            "should use add-zsh-hook for precmd"
        );
        assert!(
            content.contains("add-zsh-hook preexec"),
            "should use add-zsh-hook for preexec"
        );
    }

    #[test]
    fn test_zshrc_prevents_double_injection() {
        let injector = ZshInjector::new().expect("failed to create injector");
        let content =
            fs::read_to_string(injector.zdotdir().join(".zshrc")).expect("failed to read .zshrc");

        // Should check for existing setup to prevent double injection
        assert!(
            content.contains("__CLAI_OSC133_SETUP"),
            "should have guard against double injection"
        );
    }

    #[test]
    fn test_cleanup_on_drop() {
        let path = {
            let injector = ZshInjector::new().expect("failed to create injector");
            let path = injector.zdotdir().to_path_buf();
            assert!(path.exists());
            path
            // injector dropped here
        };

        // After drop, the temp directory should be cleaned up
        assert!(
            !path.exists(),
            "temp directory should be cleaned up on drop"
        );
    }

    #[test]
    fn test_persist() {
        let path = {
            let injector = ZshInjector::new().expect("failed to create injector");
            injector.persist()
        };

        // After persist and drop, the directory should still exist
        assert!(path.exists(), "directory should persist after persist()");

        // Clean up manually
        fs::remove_dir_all(&path).expect("failed to clean up");
    }
}
