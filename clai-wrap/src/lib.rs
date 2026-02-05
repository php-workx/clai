//! clai-wrap: PTY wrapper for clai terminal assistant
//!
//! This crate provides a pseudo-terminal wrapper that interposes between
//! the user's shell and the terminal emulator, enabling intelligent
//! command assistance.

pub mod alt_screen;
pub mod bracketed_paste;
pub mod color_detect;
pub mod denylist;
pub mod echo_gap;
pub mod history_parser;
pub mod history_picker;
pub mod hotkey;
pub mod input_router;
pub mod io_threads;
pub mod jsonrpc;
pub mod osc133;
pub mod output_capture;
pub mod passthrough;
pub mod picker;
pub mod pty_host;
pub mod raw_mode;
pub mod resize;
pub mod ring_buffer;
pub mod selection_inject;
pub mod shell_inject;
pub mod standalone;
pub mod temp_dir;

#[cfg(unix)]
pub mod daemon_client;
#[cfg(unix)]
pub mod process_detect;
#[cfg(unix)]
pub mod signals;

pub use color_detect::{detect_color_support, ColorSupport};
pub use denylist::{Denylist, DenyPattern, MatchType};
pub use echo_gap::{EchoGapConfig, EchoGapDetector, EchoGapState};
pub use history_parser::{
    detect_and_parse, parse_bash_history, parse_bash_timestamped, parse_fish_history,
    parse_zsh_history, HistoryEntry, HistoryParseError,
};
pub use history_picker::{HistoryPicker, HistoryPickerError};
pub use jsonrpc::{
    Notification, Request, Response, RpcError, RpcErrorCode, command_end_request,
    command_start_request, output_chunk_request, ping_request, suggestion_available_notification,
};
pub use osc133::{Osc133Parser, Osc133State};
pub use passthrough::{
    PassthroughMode, PassthroughReason, check_passthrough_needed, check_shell_support,
    get_tty_status, should_use_passthrough,
};
pub use picker::{Picker, PickerItem, PickerResult};
pub use pty_host::{ExitStatus, PtyHost};
pub use raw_mode::{detect_tty, enter_raw_mode, RawModeError, RawModeGuard, TtyStatus};
pub use output_capture::{CapturedOutput, OutputCapture, DEFAULT_CAPACITY as OUTPUT_CAPTURE_DEFAULT_CAPACITY};
pub use ring_buffer::SpscRingBuffer;
pub use selection_inject::SelectionInjector;
pub use input_router::{InputEvent, InputRouter};
pub use io_threads::{IoEvent, IoState, IoThreads, OutputBuffer};
pub use standalone::{Feature, StandaloneError, StandaloneReason, StandaloneState};

#[cfg(unix)]
pub use daemon_client::{DaemonClient, DaemonClientError, DEFAULT_CONNECT_TIMEOUT_MS};
#[cfg(unix)]
pub use process_detect::{
    ProcessDetectError, get_foreground_pgid, get_foreground_process, get_foreground_process_or,
    get_process_name,
};
#[cfg(unix)]
pub use signals::{SignalError, SignalEvent, SignalHandler};
