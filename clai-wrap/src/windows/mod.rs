//! Windows-specific modules for clai-wrap.
//!
//! This module contains platform-specific implementations for Windows,
//! including ConPTY integration, console event handling, process detection,
//! and other Windows-specific functionality.
//!
//! # Modules
//!
//! - [`conpty`]: ConPTY availability checking and utilities
//! - [`console_events`]: Console control event handling (Ctrl-C, Ctrl-Break, etc.)
//! - [`process_detect`]: Foreground process detection using Tool Help Library

#![cfg(windows)]

pub mod console_events;
pub mod conpty;
pub mod process_detect;

pub use console_events::{has_console, ConsoleEvent, ConsoleEventError, ConsoleEventHandler};
pub use conpty::{
    enable_virtual_terminal_processing, get_availability_diagnostic, get_windows_build_number,
    is_build_supported, is_conpty_available, ConptyError, CONPTY_MIN_BUILD,
};
pub use process_detect::{
    extract_exe_name, get_foreground_process, get_foreground_process_or, get_process_image_name,
    get_process_name, process_exists, ProcessDetectError,
};
