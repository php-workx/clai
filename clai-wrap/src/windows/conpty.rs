//! Windows ConPTY integration for clai-wrap.
//!
//! This module provides Windows-specific PTY support using the ConPTY API.
//! It primarily provides utilities for checking ConPTY availability and
//! version requirements.
//!
//! # Requirements
//!
//! ConPTY requires Windows 10 version 1809 (build 17763) or later.
//!
//! # Usage
//!
//! The actual PTY creation and management is handled by the `portable-pty` crate,
//! which automatically uses ConPTY on Windows. This module provides:
//!
//! - Availability checking via [`is_conpty_available`]
//! - Build version validation via [`get_windows_build_number`]
//! - Error types for ConPTY-specific failures
//!
//! # Platform Support
//!
//! This module is only available on Windows (`#[cfg(windows)]`).

#![cfg(windows)]

use thiserror::Error;
use windows_sys::Win32::Foundation::{FALSE, INVALID_HANDLE_VALUE};
use windows_sys::Win32::System::Console::{
    GetConsoleMode, GetStdHandle, SetConsoleMode, ENABLE_VIRTUAL_TERMINAL_PROCESSING,
    STD_OUTPUT_HANDLE,
};

/// Minimum Windows 10 build version required for ConPTY.
///
/// ConPTY was introduced in Windows 10 version 1809 (build 17763).
pub const CONPTY_MIN_BUILD: u32 = 17763;

/// Errors related to ConPTY operations.
#[derive(Debug, Error)]
pub enum ConptyError {
    /// ConPTY is not available on this system.
    ///
    /// This typically means the Windows version is older than Windows 10 1809,
    /// or the console does not support virtual terminal processing.
    #[error(
        "ConPTY not available. Requires Windows 10 version 1809 (build {CONPTY_MIN_BUILD}) or later"
    )]
    NotAvailable,

    /// Failed to initialize ConPTY.
    ///
    /// This can occur if `portable-pty` fails to create a ConPTY instance.
    #[error("failed to initialize ConPTY: {0}")]
    InitFailed(String),

    /// Failed to resize ConPTY.
    ///
    /// Resize failures are typically logged and the operation continues
    /// with the current size.
    #[error("failed to resize ConPTY: {0}")]
    ResizeFailed(String),

    /// Console operation failed.
    #[error("console operation failed: {0}")]
    ConsoleError(String),
}

/// Result type for ConPTY operations.
pub type Result<T> = std::result::Result<T, ConptyError>;

/// Checks if ConPTY is available on this system.
///
/// ConPTY requires Windows 10 version 1809 (build 17763) or later. This function
/// checks availability by attempting to enable virtual terminal processing on
/// the console, which is a prerequisite for ConPTY functionality.
///
/// # Returns
///
/// `true` if ConPTY is available and can be used, `false` otherwise.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::windows::conpty::is_conpty_available;
///
/// if is_conpty_available() {
///     println!("ConPTY is available");
/// } else {
///     eprintln!("ConPTY not available - requires Windows 10 1809+");
/// }
/// ```
pub fn is_conpty_available() -> bool {
    // SAFETY: GetStdHandle, GetConsoleMode, and SetConsoleMode are safe Windows API calls.
    // We're only reading/writing console mode flags, which is a safe operation.
    #[allow(unsafe_code)]
    unsafe {
        let handle = GetStdHandle(STD_OUTPUT_HANDLE);
        if handle == INVALID_HANDLE_VALUE || handle.is_null() {
            return false;
        }

        let mut mode: u32 = 0;
        if GetConsoleMode(handle, &mut mode) == FALSE {
            return false;
        }

        // Try to enable virtual terminal processing
        // This is a good proxy for ConPTY availability
        let new_mode = mode | ENABLE_VIRTUAL_TERMINAL_PROCESSING;
        if SetConsoleMode(handle, new_mode) == FALSE {
            return false;
        }

        // Restore original mode
        SetConsoleMode(handle, mode);

        true
    }
}

/// Gets the current Windows build number.
///
/// This can be used to verify ConPTY compatibility. The minimum required
/// build for ConPTY is 17763 (Windows 10 version 1809).
///
/// # Returns
///
/// The Windows build number, or `None` if it cannot be determined.
///
/// # Example
///
/// ```no_run
/// use clai_wrap::windows::conpty::{get_windows_build_number, CONPTY_MIN_BUILD};
///
/// if let Some(build) = get_windows_build_number() {
///     if build >= CONPTY_MIN_BUILD {
///         println!("Build {} supports ConPTY", build);
///     } else {
///         println!("Build {} is too old for ConPTY", build);
///     }
/// }
/// ```
pub fn get_windows_build_number() -> Option<u32> {
    // Use the RtlGetVersion API to get accurate version info
    // This works even when the app manifest doesn't declare Windows 10 compatibility

    #[repr(C)]
    struct OsVersionInfoExW {
        dw_os_version_info_size: u32,
        dw_major_version: u32,
        dw_minor_version: u32,
        dw_build_number: u32,
        dw_platform_id: u32,
        sz_csd_version: [u16; 128],
        w_service_pack_major: u16,
        w_service_pack_minor: u16,
        w_suite_mask: u16,
        w_product_type: u8,
        w_reserved: u8,
    }

    #[link(name = "ntdll")]
    extern "system" {
        fn RtlGetVersion(lp_version_information: *mut OsVersionInfoExW) -> i32;
    }

    // SAFETY: RtlGetVersion is a safe Windows API call that fills in the version struct.
    // We're providing a properly sized and initialized struct.
    #[allow(unsafe_code)]
    unsafe {
        let mut info: OsVersionInfoExW = std::mem::zeroed();
        info.dw_os_version_info_size = std::mem::size_of::<OsVersionInfoExW>() as u32;

        if RtlGetVersion(&mut info) == 0 {
            Some(info.dw_build_number)
        } else {
            None
        }
    }
}

/// Checks if the current Windows build supports ConPTY.
///
/// This function combines [`get_windows_build_number`] with the minimum
/// build requirement check.
///
/// # Returns
///
/// `true` if the Windows build number is at least [`CONPTY_MIN_BUILD`],
/// `false` otherwise (including if the build number cannot be determined).
///
/// # Example
///
/// ```no_run
/// use clai_wrap::windows::conpty::is_build_supported;
///
/// if !is_build_supported() {
///     eprintln!("Your Windows version does not support ConPTY");
///     std::process::exit(1);
/// }
/// ```
pub fn is_build_supported() -> bool {
    get_windows_build_number().is_some_and(|build| build >= CONPTY_MIN_BUILD)
}

/// Enables virtual terminal processing on the console.
///
/// This is required for proper ANSI escape sequence handling in the terminal.
/// ConPTY and the picker UI both require virtual terminal processing to be enabled.
///
/// # Returns
///
/// `Ok(())` if virtual terminal processing was enabled successfully,
/// `Err(ConptyError)` otherwise.
///
/// # Note
///
/// This function modifies global console state. The change persists until
/// the console mode is explicitly changed or the process exits.
pub fn enable_virtual_terminal_processing() -> Result<()> {
    // SAFETY: GetStdHandle, GetConsoleMode, and SetConsoleMode are safe Windows API calls.
    #[allow(unsafe_code)]
    unsafe {
        let handle = GetStdHandle(STD_OUTPUT_HANDLE);
        if handle == INVALID_HANDLE_VALUE || handle.is_null() {
            return Err(ConptyError::ConsoleError(
                "failed to get stdout handle".to_string(),
            ));
        }

        let mut mode: u32 = 0;
        if GetConsoleMode(handle, &mut mode) == FALSE {
            return Err(ConptyError::ConsoleError(
                "failed to get console mode".to_string(),
            ));
        }

        let new_mode = mode | ENABLE_VIRTUAL_TERMINAL_PROCESSING;
        if SetConsoleMode(handle, new_mode) == FALSE {
            return Err(ConptyError::ConsoleError(
                "failed to enable virtual terminal processing".to_string(),
            ));
        }

        Ok(())
    }
}

/// Returns a human-readable description of ConPTY availability.
///
/// This function provides diagnostic information that can be shown to users
/// when ConPTY is not available.
///
/// # Returns
///
/// A string describing ConPTY availability status.
pub fn get_availability_diagnostic() -> String {
    let build_info = match get_windows_build_number() {
        Some(build) if build >= CONPTY_MIN_BUILD => {
            format!("Windows build {} (supported)", build)
        }
        Some(build) => {
            format!(
                "Windows build {} (too old, requires build {} or later)",
                build, CONPTY_MIN_BUILD
            )
        }
        None => "Windows build unknown".to_string(),
    };

    let vt_available = if is_conpty_available() {
        "Virtual terminal processing: available"
    } else {
        "Virtual terminal processing: not available"
    };

    format!("{}\n{}", build_info, vt_available)
}

#[cfg(test)]
#[cfg(windows)]
mod tests {
    use super::*;

    #[test]
    fn test_conpty_min_build_constant() {
        // Verify the constant matches the documented requirement
        assert_eq!(CONPTY_MIN_BUILD, 17763);
    }

    #[test]
    fn test_is_conpty_available() {
        // On Windows 10 1809+, this should return true
        // On older versions or non-console environments, it may return false
        let available = is_conpty_available();
        println!("ConPTY available: {}", available);
        // Just verify it doesn't panic
    }

    #[test]
    fn test_get_windows_build_number() {
        let build = get_windows_build_number();

        // Should be able to get the build number on any Windows system
        assert!(
            build.is_some(),
            "Should be able to get Windows build number"
        );

        let build = build.unwrap();
        println!("Windows build number: {}", build);

        // Build number should be reasonable (at least Windows 10 RTM = 10240)
        assert!(build >= 10240, "Build number should be reasonable: {}", build);
    }

    #[test]
    fn test_is_build_supported() {
        let supported = is_build_supported();
        let build = get_windows_build_number();

        // Verify consistency
        if let Some(build) = build {
            assert_eq!(
                supported,
                build >= CONPTY_MIN_BUILD,
                "is_build_supported should match build comparison"
            );
        }

        println!("Build supported: {}", supported);
    }

    #[test]
    fn test_enable_virtual_terminal_processing() {
        // This might fail in some test environments (e.g., no console)
        // but it should not panic
        let result = enable_virtual_terminal_processing();
        println!(
            "Enable VT processing result: {:?}",
            result.is_ok()
        );
    }

    #[test]
    fn test_get_availability_diagnostic() {
        let diagnostic = get_availability_diagnostic();

        // Should contain build information
        assert!(
            diagnostic.contains("Windows build"),
            "Diagnostic should mention Windows build"
        );

        // Should contain VT processing status
        assert!(
            diagnostic.contains("Virtual terminal processing"),
            "Diagnostic should mention VT processing"
        );

        println!("Diagnostic:\n{}", diagnostic);
    }

    #[test]
    fn test_conpty_error_display() {
        let error = ConptyError::NotAvailable;
        let display = error.to_string();
        assert!(
            display.contains("ConPTY not available"),
            "Error should mention ConPTY not available: {}",
            display
        );
        assert!(
            display.contains("17763"),
            "Error should mention minimum build: {}",
            display
        );

        let error = ConptyError::InitFailed("test error".to_string());
        let display = error.to_string();
        assert!(
            display.contains("initialize ConPTY"),
            "Error should mention initialize: {}",
            display
        );
        assert!(
            display.contains("test error"),
            "Error should contain message: {}",
            display
        );

        let error = ConptyError::ResizeFailed("resize failed".to_string());
        let display = error.to_string();
        assert!(
            display.contains("resize ConPTY"),
            "Error should mention resize: {}",
            display
        );

        let error = ConptyError::ConsoleError("console error".to_string());
        let display = error.to_string();
        assert!(
            display.contains("console operation"),
            "Error should mention console: {}",
            display
        );
    }

    #[test]
    fn test_error_debug() {
        let error = ConptyError::NotAvailable;
        let debug = format!("{:?}", error);
        assert!(debug.contains("NotAvailable"));

        let error = ConptyError::InitFailed("test".to_string());
        let debug = format!("{:?}", error);
        assert!(debug.contains("InitFailed"));
        assert!(debug.contains("test"));
    }

    /// Integration test that verifies ConPTY detection is consistent.
    #[test]
    fn test_conpty_detection_consistency() {
        // Run multiple times to ensure consistency
        for _ in 0..5 {
            let available1 = is_conpty_available();
            let available2 = is_conpty_available();
            assert_eq!(
                available1, available2,
                "ConPTY availability should be consistent"
            );
        }
    }

    /// Test that we handle the case where stdout is not a console.
    /// This is harder to test directly, but we verify the function handles
    /// edge cases gracefully.
    #[test]
    fn test_handles_non_console_gracefully() {
        // This test primarily verifies no panics occur
        // The actual availability depends on the test environment
        let _ = is_conpty_available();
        let _ = get_windows_build_number();
        let _ = is_build_supported();
        let _ = get_availability_diagnostic();
        let _ = enable_virtual_terminal_processing();
    }
}
