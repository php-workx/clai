//! Color Detection Module
//!
//! This module detects terminal color support by examining environment variables.
//! It follows a priority-based detection algorithm as specified in the clai-wrap
//! technical specification (Section 6.5).
//!
//! Detection Priority:
//! 1. `NO_COLOR` env var set -> No colors
//! 2. `COLORTERM=truecolor` or `24bit` -> `TrueColor` (24-bit, 16M colors)
//! 3. `TERM` contains `256color` -> 256 colors
//! 4. `TERM=dumb` or unset -> No colors
//! 5. Fallback -> Basic 16 colors

use std::env;

/// Represents the level of color support available in the terminal.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum ColorSupport {
    /// No color support - either `NO_COLOR` is set, `TERM=dumb`, or terminal
    /// doesn't support colors.
    None,
    /// Basic 16-color support (ANSI colors).
    #[default]
    Basic16,
    /// Extended 256-color support (xterm-256color and similar).
    Colors256,
    /// True color support (24-bit, 16 million colors).
    TrueColor,
}

impl ColorSupport {
    /// Returns the maximum number of colors supported by this color level.
    #[must_use]
    pub const fn max_colors(&self) -> u32 {
        match self {
            Self::None => 0,
            Self::Basic16 => 16,
            Self::Colors256 => 256,
            Self::TrueColor => 16_777_216, // 2^24
        }
    }

    /// Returns true if any colors are supported.
    #[must_use]
    pub const fn has_colors(&self) -> bool {
        !matches!(self, Self::None)
    }

    /// Returns true if 256 or more colors are supported.
    #[must_use]
    pub const fn has_256_colors(&self) -> bool {
        matches!(self, Self::Colors256 | Self::TrueColor)
    }

    /// Returns true if true color (24-bit) is supported.
    #[must_use]
    pub const fn has_true_color(&self) -> bool {
        matches!(self, Self::TrueColor)
    }
}

/// Detects the terminal's color support level based on environment variables.
///
/// This function checks environment variables in the following priority order:
///
/// 1. `NO_COLOR` - If set (to any value), returns `ColorSupport::None`
/// 2. `COLORTERM` - If `truecolor` or `24bit` (case-insensitive), returns `ColorSupport::TrueColor`
/// 3. `TERM` - If contains `256color`, returns `ColorSupport::Colors256`
/// 4. `TERM` - If `dumb` or unset, returns `ColorSupport::None`
/// 5. Fallback - Returns `ColorSupport::Basic16`
///
/// # Examples
///
/// ```
/// use clai_wrap::color_detect::detect_color_support;
///
/// // The result depends on the current environment
/// let support = detect_color_support();
/// if support.has_colors() {
///     println!("Terminal supports {} colors", support.max_colors());
/// }
/// ```
#[must_use]
pub fn detect_color_support() -> ColorSupport {
    detect_color_support_from_env(&EnvReader::Real)
}

/// Internal trait for reading environment variables, allowing for testing.
trait EnvReaderTrait {
    fn get(&self, key: &str) -> Option<String>;
}

/// Real environment reader for production use.
struct RealEnvReader;

impl EnvReaderTrait for RealEnvReader {
    fn get(&self, key: &str) -> Option<String> {
        env::var(key).ok()
    }
}

/// Environment reader enum for dependency injection in tests.
enum EnvReader {
    Real,
    #[cfg(test)]
    Mock(std::collections::HashMap<String, String>),
}

impl EnvReaderTrait for EnvReader {
    fn get(&self, key: &str) -> Option<String> {
        match self {
            Self::Real => env::var(key).ok(),
            #[cfg(test)]
            Self::Mock(map) => map.get(key).cloned(),
        }
    }
}

/// Internal implementation of color detection that accepts an environment reader.
fn detect_color_support_from_env(env: &EnvReader) -> ColorSupport {
    // Priority 1: NO_COLOR takes precedence over everything
    // Per https://no-color.org/, the presence of the variable (regardless of value)
    // should disable colors
    if env.get("NO_COLOR").is_some() {
        return ColorSupport::None;
    }

    // Priority 2: Check COLORTERM for truecolor/24bit support (case-insensitive)
    if let Some(colorterm) = env.get("COLORTERM") {
        let colorterm_lower = colorterm.to_lowercase();
        if colorterm_lower == "truecolor" || colorterm_lower == "24bit" {
            return ColorSupport::TrueColor;
        }
    }

    // Get TERM for remaining checks
    let term = env.get("TERM");

    // Priority 3: Check if TERM contains "256color"
    if let Some(ref term_value) = term {
        // Check for 256color in TERM (case-insensitive for robustness)
        if term_value.to_lowercase().contains("256color") {
            return ColorSupport::Colors256;
        }
    }

    // Priority 4: Check for dumb terminal or unset TERM
    match term {
        None => return ColorSupport::None,
        Some(ref t) if t == "dumb" || t.is_empty() => return ColorSupport::None,
        _ => {}
    }

    // Priority 5: Fallback to basic 16 colors
    // Note: For MVP, we skip terminfo lookup and assume basic color support
    ColorSupport::Basic16
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    /// Helper to create a mock environment for testing.
    fn mock_env(vars: &[(&str, &str)]) -> EnvReader {
        let map: HashMap<String, String> = vars
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        EnvReader::Mock(map)
    }

    // ========================================================================
    // NO_COLOR tests (Priority 1)
    // ========================================================================

    #[test]
    fn test_no_color_takes_priority() {
        // NO_COLOR should override everything, even if truecolor is set
        let env = mock_env(&[
            ("NO_COLOR", "1"),
            ("COLORTERM", "truecolor"),
            ("TERM", "xterm-256color"),
        ]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::None);
    }

    #[test]
    fn test_no_color_empty_value() {
        // Per no-color.org spec, presence of the variable matters, not its value
        let env = mock_env(&[("NO_COLOR", ""), ("TERM", "xterm-256color")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::None);
    }

    #[test]
    fn test_no_color_any_value() {
        // Any value should disable colors
        let env = mock_env(&[("NO_COLOR", "false"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::None);
    }

    // ========================================================================
    // COLORTERM truecolor tests (Priority 2)
    // ========================================================================

    #[test]
    fn test_colorterm_truecolor() {
        let env = mock_env(&[("COLORTERM", "truecolor"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);
    }

    #[test]
    fn test_colorterm_24bit() {
        let env = mock_env(&[("COLORTERM", "24bit"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);
    }

    #[test]
    fn test_colorterm_case_insensitive_truecolor() {
        let env = mock_env(&[("COLORTERM", "TrueColor"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);

        let env = mock_env(&[("COLORTERM", "TRUECOLOR"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);
    }

    #[test]
    fn test_colorterm_case_insensitive_24bit() {
        let env = mock_env(&[("COLORTERM", "24BIT"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);
    }

    #[test]
    fn test_colorterm_other_value() {
        // Other COLORTERM values don't imply truecolor
        let env = mock_env(&[("COLORTERM", "gnome-terminal"), ("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::Basic16);
    }

    // ========================================================================
    // TERM 256color tests (Priority 3)
    // ========================================================================

    #[test]
    fn test_term_256color() {
        let env = mock_env(&[("TERM", "xterm-256color")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::Colors256);
    }

    #[test]
    fn test_term_256color_other_terminals() {
        // Various terminals that support 256 colors
        let terminals = [
            "screen-256color",
            "tmux-256color",
            "rxvt-unicode-256color",
            "gnome-256color",
        ];

        for term in terminals {
            let env = mock_env(&[("TERM", term)]);
            assert_eq!(
                detect_color_support_from_env(&env),
                ColorSupport::Colors256,
                "Failed for TERM={}",
                term
            );
        }
    }

    #[test]
    fn test_term_256color_case_insensitive() {
        // While unusual, handle case differences
        let env = mock_env(&[("TERM", "xterm-256COLOR")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::Colors256);
    }

    // ========================================================================
    // Dumb terminal tests (Priority 4)
    // ========================================================================

    #[test]
    fn test_term_dumb() {
        let env = mock_env(&[("TERM", "dumb")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::None);
    }

    #[test]
    fn test_term_unset() {
        let env = mock_env(&[]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::None);
    }

    #[test]
    fn test_term_empty() {
        let env = mock_env(&[("TERM", "")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::None);
    }

    // ========================================================================
    // Fallback to Basic16 tests (Priority 5)
    // ========================================================================

    #[test]
    fn test_fallback_to_basic16() {
        let env = mock_env(&[("TERM", "xterm")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::Basic16);
    }

    #[test]
    fn test_basic_terminals() {
        // Various terminals that should get basic 16 color support
        let terminals = ["xterm", "vt100", "linux", "screen", "ansi"];

        for term in terminals {
            let env = mock_env(&[("TERM", term)]);
            assert_eq!(
                detect_color_support_from_env(&env),
                ColorSupport::Basic16,
                "Failed for TERM={}",
                term
            );
        }
    }

    // ========================================================================
    // ColorSupport method tests
    // ========================================================================

    #[test]
    fn test_max_colors() {
        assert_eq!(ColorSupport::None.max_colors(), 0);
        assert_eq!(ColorSupport::Basic16.max_colors(), 16);
        assert_eq!(ColorSupport::Colors256.max_colors(), 256);
        assert_eq!(ColorSupport::TrueColor.max_colors(), 16_777_216);
    }

    #[test]
    fn test_has_colors() {
        assert!(!ColorSupport::None.has_colors());
        assert!(ColorSupport::Basic16.has_colors());
        assert!(ColorSupport::Colors256.has_colors());
        assert!(ColorSupport::TrueColor.has_colors());
    }

    #[test]
    fn test_has_256_colors() {
        assert!(!ColorSupport::None.has_256_colors());
        assert!(!ColorSupport::Basic16.has_256_colors());
        assert!(ColorSupport::Colors256.has_256_colors());
        assert!(ColorSupport::TrueColor.has_256_colors());
    }

    #[test]
    fn test_has_true_color() {
        assert!(!ColorSupport::None.has_true_color());
        assert!(!ColorSupport::Basic16.has_true_color());
        assert!(!ColorSupport::Colors256.has_true_color());
        assert!(ColorSupport::TrueColor.has_true_color());
    }

    #[test]
    fn test_default() {
        assert_eq!(ColorSupport::default(), ColorSupport::Basic16);
    }

    // ========================================================================
    // Complex scenario tests
    // ========================================================================

    #[test]
    fn test_colorterm_without_term() {
        // COLORTERM truecolor should work even without TERM
        // (though this is unusual in practice)
        let env = mock_env(&[("COLORTERM", "truecolor")]);
        // Without TERM, we'd fall through to None, but COLORTERM is checked first
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);
    }

    #[test]
    fn test_256color_term_with_non_truecolor_colorterm() {
        // If COLORTERM is set but not truecolor/24bit, and TERM is 256color
        let env = mock_env(&[
            ("COLORTERM", "gnome-terminal"),
            ("TERM", "xterm-256color"),
        ]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::Colors256);
    }

    #[test]
    fn test_truecolor_overrides_256color_term() {
        // COLORTERM=truecolor should take precedence over 256color TERM
        let env = mock_env(&[("COLORTERM", "truecolor"), ("TERM", "xterm-256color")]);
        assert_eq!(detect_color_support_from_env(&env), ColorSupport::TrueColor);
    }
}
