//! Fish shell integration injection for OSC 133 semantic prompt support.
//!
//! This module provides the `FishInjector` which handles OSC 133 integration
//! for Fish shell. While modern Fish versions may support OSC 133 natively,
//! clai-wrap injects hooks consistently to guarantee deterministic markers
//! across terminals and Fish builds.
//!
//! # OSC 133 Sequences
//!
//! - `\e]133;A\a` - Prompt start
//! - `\e]133;B\a` - Input start (end of prompt)
//! - `\e]133;C\a` - Output start (command execution begins)
//! - `\e]133;D;$status\a` - Finished (command completed with exit code)
//!
//! # Usage
//!
//! ```no_run
//! use clai_wrap::shell_inject::FishInjector;
//!
//! let injector = FishInjector::new().expect("failed to create injector");
//!
//! let args = injector.shell_args();
//! // Launch fish with: fish --init-command "..."
//! ```

use std::path::Path;
use std::process::Command;

use thiserror::Error;

/// Errors that can occur during Fish injection setup.
#[derive(Debug, Error)]
pub enum FishInjectorError {
    /// Fish is not installed or not found in PATH
    #[error("fish not found: {0}")]
    FishNotFound(#[source] std::io::Error),

    /// Failed to parse Fish version from output
    #[error("failed to parse fish version from: {output}")]
    VersionParseFailed { output: String },
}

/// The Fish version where native OSC 133 support was added.
///
/// This is retained for diagnostics/telemetry; hook injection is still applied
/// for deterministic behavior.
const NATIVE_OSC133_VERSION: (u32, u32) = (3, 6);

/// Fish shell integration injector.
///
/// Detects the Fish version and provides shell arguments for OSC 133
/// integration.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::shell_inject::FishInjector;
///
/// let injector = FishInjector::new().expect("failed to create injector");
///
/// let args = injector.shell_args();
/// println!("Use fish with args: {:?}", args);
/// ```
#[derive(Debug, Clone)]
pub struct FishInjector {
    /// Detected Fish version as (major, minor)
    fish_version: Option<(u32, u32)>,

    /// Whether Fish has native OSC 133 support (version >= 3.6)
    native_osc133: bool,
}

impl FishInjector {
    /// Creates a new `FishInjector` by detecting the installed Fish version.
    ///
    /// This runs `fish --version` to detect the installed version and
    /// determines whether native OSC 133 support is available.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - Fish is not installed or not found in PATH
    /// - The version output cannot be parsed
    pub fn new() -> Result<Self, FishInjectorError> {
        let version = Self::detect_version_via("fish")?;
        let native_osc133 = Self::version_has_native_osc133(version);

        Ok(Self {
            fish_version: Some(version),
            native_osc133,
        })
    }

    /// Creates a new `FishInjector` by detecting version from a shell path.
    ///
    /// This is preferred when the target shell path may not match `PATH`.
    pub fn for_shell_path(shell_path: &Path) -> Result<Self, FishInjectorError> {
        let version = Self::detect_version_via(shell_path)?;
        let native_osc133 = Self::version_has_native_osc133(version);

        Ok(Self {
            fish_version: Some(version),
            native_osc133,
        })
    }

    /// Creates a `FishInjector` without detecting the Fish version.
    ///
    /// This is useful when Fish detection fails but you still want to
    /// attempt injection. The injector will assume injection is needed.
    #[must_use]
    pub const fn without_detection() -> Self {
        Self {
            fish_version: None,
            native_osc133: false,
        }
    }

    /// Creates a `FishInjector` with a known version.
    ///
    /// This is primarily useful for testing.
    #[must_use]
    pub const fn with_version(major: u32, minor: u32) -> Self {
        let version = (major, minor);
        let native_osc133 = Self::version_has_native_osc133(version);

        Self {
            fish_version: Some(version),
            native_osc133,
        }
    }

    /// Returns whether Fish has native OSC 133 support.
    ///
    /// Fish 3.6 and later emit OSC 133 sequences natively, so no
    /// injection is needed.
    #[must_use]
    pub const fn has_native_osc133(&self) -> bool {
        self.native_osc133
    }

    /// Returns the detected Fish version as (major, minor).
    ///
    /// Returns `None` if the version could not be detected.
    #[must_use]
    pub const fn version(&self) -> Option<(u32, u32)> {
        self.fish_version
    }

    /// Returns a human-readable version string.
    ///
    /// Returns "unknown" if the version could not be detected.
    #[must_use]
    pub fn version_string(&self) -> String {
        match self.fish_version {
            Some((major, minor)) => format!("{major}.{minor}"),
            None => "unknown".to_string(),
        }
    }

    /// Returns the shell arguments for OSC 133 injection.
    ///
    /// Returns `["--init-command", "<script>"]` where `<script>` contains OSC
    /// 133 hooks.
    ///
    /// We inject for all versions (including those with native support) to
    /// keep command-boundary behavior deterministic.
    pub fn shell_args(&self) -> Vec<String> {
        vec![
            "--init-command".to_string(),
            Self::init_script().to_string(),
        ]
    }

    /// Detects the Fish version by running `<shell> --version`.
    ///
    /// Expected output format: "fish, version X.Y.Z"
    fn detect_version_via(
        shell: impl AsRef<std::ffi::OsStr>,
    ) -> Result<(u32, u32), FishInjectorError> {
        let output = Command::new(shell)
            .arg("--version")
            .output()
            .map_err(FishInjectorError::FishNotFound)?;

        let version_str = String::from_utf8_lossy(&output.stdout);
        Self::parse_version(&version_str)
    }

    /// Parses a Fish version string.
    ///
    /// Expected format: "fish, version X.Y.Z" or "fish, version X.Y.Z-..."
    pub(crate) fn parse_version(output: &str) -> Result<(u32, u32), FishInjectorError> {
        // Look for "version X.Y.Z" pattern
        let output = output.trim();

        // Find "version " and extract the version number
        let version_prefix = "version ";
        let version_start =
            output
                .find(version_prefix)
                .ok_or_else(|| FishInjectorError::VersionParseFailed {
                    output: output.to_string(),
                })?;

        let version_part = &output[version_start + version_prefix.len()..];

        // Extract just the version number (stop at whitespace or end)
        let version_end = version_part
            .find(|c: char| c.is_whitespace())
            .unwrap_or(version_part.len());
        let version_str = &version_part[..version_end];

        // Parse X.Y.Z or X.Y.Z-suffix
        let parts: Vec<&str> = version_str.split('.').collect();
        if parts.len() < 2 {
            return Err(FishInjectorError::VersionParseFailed {
                output: output.to_string(),
            });
        }

        let major = parts[0]
            .parse::<u32>()
            .map_err(|_| FishInjectorError::VersionParseFailed {
                output: output.to_string(),
            })?;

        // Minor might have a suffix like "6-123-g1234567" for development versions
        let minor_str = parts[1].split('-').next().unwrap_or(parts[1]);
        let minor =
            minor_str
                .parse::<u32>()
                .map_err(|_| FishInjectorError::VersionParseFailed {
                    output: output.to_string(),
                })?;

        Ok((major, minor))
    }

    /// Returns whether the given version has native OSC 133 support.
    const fn version_has_native_osc133(version: (u32, u32)) -> bool {
        version.0 > NATIVE_OSC133_VERSION.0
            || (version.0 == NATIVE_OSC133_VERSION.0 && version.1 >= NATIVE_OSC133_VERSION.1)
    }

    /// Returns the Fish init script for OSC 133 injection.
    ///
    /// This script defines Fish functions for:
    /// - `fish_prompt` wrapper that emits OSC 133;A before prompt and 133;B after
    /// - `fish_preexec` that emits OSC 133;C before command execution
    /// - `fish_postexec` that emits OSC 133;D with exit status
    const fn init_script() -> &'static str {
        // Fish uses functions for prompt hooks:
        // - fish_prompt: called to generate the prompt
        // - fish_preexec: called before command execution (Fish 2.2+)
        // - fish_postexec: called after command execution (Fish 2.2+)
        //
        // We wrap fish_prompt to emit A before and B after, and use
        // preexec/postexec for C and D sequences.
        r"
# clai-wrap: OSC 133 shell integration for Fish
# This is injected automatically for Fish < 3.6

# Emit OSC 133;D (finished with exit code) and 133;A (prompt start)
# Called before each prompt via fish_prompt wrapper
function __clai_osc133_prompt_start
    # Get the exit status from the last command
    set -l last_status $status
    # Emit OSC 133;D with exit code (finished)
    printf '\e]133;D;%d\a' $last_status
    # Emit OSC 133;A (prompt start)
    printf '\e]133;A\a'
end

# Emit OSC 133;B (input start / end of prompt)
function __clai_osc133_prompt_end
    printf '\e]133;B\a'
end

# Emit OSC 133;C (output start / command running)
function __clai_osc133_preexec --on-event fish_preexec
    printf '\e]133;C\a'
end

# Wrap the existing fish_prompt function
# We save the original and create a wrapper that emits OSC sequences
if functions -q fish_prompt
    functions -c fish_prompt __clai_original_fish_prompt
    function fish_prompt
        __clai_osc133_prompt_start
        __clai_original_fish_prompt
        __clai_osc133_prompt_end
    end
else
    # No existing prompt, create a minimal one with OSC 133
    function fish_prompt
        __clai_osc133_prompt_start
        echo -n '> '
        __clai_osc133_prompt_end
    end
end
"
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_version_standard() {
        let output = "fish, version 3.6.0";
        let version = FishInjector::parse_version(output).expect("failed to parse version");
        assert_eq!(version, (3, 6));
    }

    #[test]
    fn test_parse_version_with_newline() {
        let output = "fish, version 3.6.0\n";
        let version = FishInjector::parse_version(output).expect("failed to parse version");
        assert_eq!(version, (3, 6));
    }

    #[test]
    fn test_parse_version_older() {
        let output = "fish, version 3.5.1";
        let version = FishInjector::parse_version(output).expect("failed to parse version");
        assert_eq!(version, (3, 5));
    }

    #[test]
    fn test_parse_version_very_old() {
        let output = "fish, version 2.7.1";
        let version = FishInjector::parse_version(output).expect("failed to parse version");
        assert_eq!(version, (2, 7));
    }

    #[test]
    fn test_parse_version_development() {
        // Development/nightly versions may have suffixes
        let output = "fish, version 3.7.0-123-g1234567";
        let version = FishInjector::parse_version(output).expect("failed to parse version");
        assert_eq!(version, (3, 7));
    }

    #[test]
    fn test_parse_version_invalid_no_version() {
        let output = "fish shell";
        let result = FishInjector::parse_version(output);
        assert!(result.is_err());
    }

    #[test]
    fn test_parse_version_invalid_format() {
        let output = "fish, version abc";
        let result = FishInjector::parse_version(output);
        assert!(result.is_err());
    }

    #[test]
    fn test_parse_version_single_number() {
        let output = "fish, version 3";
        let result = FishInjector::parse_version(output);
        assert!(result.is_err());
    }

    #[test]
    fn test_native_osc133_detection_3_6() {
        let injector = FishInjector::with_version(3, 6);
        assert!(
            injector.has_native_osc133(),
            "Fish 3.6 should have native OSC 133"
        );
    }

    #[test]
    fn test_native_osc133_detection_3_7() {
        let injector = FishInjector::with_version(3, 7);
        assert!(
            injector.has_native_osc133(),
            "Fish 3.7 should have native OSC 133"
        );
    }

    #[test]
    fn test_native_osc133_detection_4_0() {
        let injector = FishInjector::with_version(4, 0);
        assert!(
            injector.has_native_osc133(),
            "Fish 4.0 should have native OSC 133"
        );
    }

    #[test]
    fn test_native_osc133_detection_3_5() {
        let injector = FishInjector::with_version(3, 5);
        assert!(
            !injector.has_native_osc133(),
            "Fish 3.5 should NOT have native OSC 133"
        );
    }

    #[test]
    fn test_native_osc133_detection_2_7() {
        let injector = FishInjector::with_version(2, 7);
        assert!(
            !injector.has_native_osc133(),
            "Fish 2.7 should NOT have native OSC 133"
        );
    }

    #[test]
    fn test_shell_args_native() {
        let injector = FishInjector::with_version(3, 6);
        let args = injector.shell_args();
        assert_eq!(args.len(), 2, "Fish 3.6+ should still inject hooks");
        assert_eq!(args[0], "--init-command");
        assert!(args[1].contains("__clai_osc133"));
    }

    #[test]
    fn test_shell_args_needs_injection() {
        let injector = FishInjector::with_version(3, 5);
        let args = injector.shell_args();

        assert_eq!(args.len(), 2, "should return two arguments");
        assert_eq!(args[0], "--init-command");
        assert!(
            args[1].contains("__clai_osc133"),
            "init command should contain OSC 133 hooks"
        );
    }

    #[test]
    fn test_without_detection() {
        let injector = FishInjector::without_detection();

        assert!(
            injector.version().is_none(),
            "version should be None without detection"
        );
        assert!(
            !injector.has_native_osc133(),
            "should assume no native OSC 133 without detection"
        );
        assert!(
            !injector.shell_args().is_empty(),
            "should return injection args without detection"
        );
    }

    #[test]
    fn test_for_shell_path_detects_version() {
        // Skip if fish is not installed
        if std::process::Command::new("fish")
            .arg("--version")
            .output()
            .is_err()
        {
            eprintln!("Skipping: fish not installed");
            return;
        }
        let injector = FishInjector::for_shell_path(Path::new("fish"));
        assert!(
            injector.is_ok(),
            "for_shell_path should work when fish is on PATH"
        );
    }

    #[test]
    fn test_version_string_known() {
        let injector = FishInjector::with_version(3, 6);
        assert_eq!(injector.version_string(), "3.6");
    }

    #[test]
    fn test_version_string_unknown() {
        let injector = FishInjector::without_detection();
        assert_eq!(injector.version_string(), "unknown");
    }

    #[test]
    fn test_version_getter() {
        let injector = FishInjector::with_version(3, 5);
        assert_eq!(injector.version(), Some((3, 5)));
    }

    #[test]
    fn test_init_script_contains_osc133_sequences() {
        let script = FishInjector::init_script();

        // Check for OSC 133;A (prompt start)
        assert!(
            script.contains(r"'\e]133;A\a'"),
            "should emit OSC 133;A (prompt start)"
        );

        // Check for OSC 133;B (input start)
        assert!(
            script.contains(r"'\e]133;B\a'"),
            "should emit OSC 133;B (input start)"
        );

        // Check for OSC 133;C (output start)
        assert!(
            script.contains(r"'\e]133;C\a'"),
            "should emit OSC 133;C (output start)"
        );

        // Check for OSC 133;D (finished with exit code)
        assert!(
            script.contains(r"'\e]133;D;%d\a'"),
            "should emit OSC 133;D with exit code"
        );
    }

    #[test]
    fn test_init_script_has_preexec_hook() {
        let script = FishInjector::init_script();
        assert!(
            script.contains("--on-event fish_preexec"),
            "should have fish_preexec event hook"
        );
    }

    #[test]
    fn test_init_script_wraps_fish_prompt() {
        let script = FishInjector::init_script();
        assert!(
            script.contains("functions -c fish_prompt __clai_original_fish_prompt"),
            "should copy original fish_prompt"
        );
    }

    #[test]
    fn test_init_script_has_fallback_prompt() {
        let script = FishInjector::init_script();
        // Should have an else branch for when no fish_prompt exists
        assert!(
            script.contains("else") && script.contains("echo -n"),
            "should have fallback prompt when fish_prompt doesn't exist"
        );
    }

    #[test]
    fn test_clone() {
        let injector1 = FishInjector::with_version(3, 6);
        let injector2 = injector1.clone();

        assert_eq!(injector1.version(), injector2.version());
        assert_eq!(injector1.has_native_osc133(), injector2.has_native_osc133());
    }

    #[test]
    fn test_debug_impl() {
        let injector = FishInjector::with_version(3, 6);
        let debug_str = format!("{injector:?}");

        assert!(debug_str.contains("FishInjector"));
        assert!(debug_str.contains("fish_version"));
        assert!(debug_str.contains("native_osc133"));
    }
}
