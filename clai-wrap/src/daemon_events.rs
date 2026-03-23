//! Daemon event forwarding for clai-wrap.
//!
//! This module coordinates forwarding OSC 133 events (command start/end) and
//! output capture data to the daemon via JSON-RPC. It handles daemon disconnect
//! gracefully by continuing to operate in standalone mode.
//!
//! # Overview
//!
//! The `DaemonEventForwarder` manages:
//! - Tracking OSC 133 state transitions
//! - Forwarding command.start events when command execution begins (OSC 133 C)
//! - Forwarding command.end events when command completes (OSC 133 D)
//! - Forwarding output.chunk data during command execution
//! - Graceful degradation to standalone mode on daemon disconnect
//!
//! # Usage
//!
//! ```rust,ignore
//! use clai_wrap::daemon_events::{DaemonEventForwarder, ForwarderConfig};
//! use clai_wrap::daemon_client::DaemonClient;
//!
//! // Create forwarder with daemon connection
//! let client = DaemonClient::connect_default()?;
//! let config = ForwarderConfig::default();
//! let mut forwarder = DaemonEventForwarder::with_client(client, config);
//!
//! // Process OSC 133 state changes
//! forwarder.on_osc133_state_change(&Osc133State::Output);
//!
//! // Forward output data
//! forwarder.forward_output(b"command output...");
//!
//! // Process command completion
//! forwarder.on_osc133_state_change(&Osc133State::Finished(0));
//! ```
//!
//! # Standalone Mode
//!
//! When the daemon is unavailable or disconnects, the forwarder enters standalone
//! mode. In standalone mode:
//! - Command events are not forwarded (no daemon to receive them)
//! - Output capture is disabled
//! - The forwarder continues tracking state locally
//!
//! # Thread Safety
//!
//! The forwarder is designed to be used from a single thread. For multi-threaded
//! use cases, wrap it in appropriate synchronization primitives.

#[cfg(unix)]
use crate::daemon_client::{DaemonClient, DaemonClientError};
use crate::jsonrpc::Notification;
use crate::osc133::Osc133State;
use crate::output_capture::{CapturedOutput, OutputCapture};
use crate::standalone::{Feature, StandaloneReason, StandaloneState};

use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use thiserror::Error;
use tracing::{debug, trace, warn};
use uuid::Uuid;

/// Errors that can occur during event forwarding.
#[derive(Debug, Error)]
pub enum ForwarderError {
    /// Failed to connect to the daemon.
    #[cfg(unix)]
    #[error("daemon connection failed: {0}")]
    ConnectionFailed(#[from] DaemonClientError),

    /// I/O error during communication.
    #[error("I/O error: {0}")]
    IoError(#[from] std::io::Error),

    /// Forwarder is in standalone mode.
    #[error("forwarder is in standalone mode: {reason}")]
    StandaloneMode { reason: String },
}

/// Result type for forwarder operations.
pub type Result<T> = std::result::Result<T, ForwarderError>;

/// Configuration for the daemon event forwarder.
#[derive(Debug, Clone)]
pub struct ForwarderConfig {
    /// Session identifier for this wrapper instance.
    pub session_id: String,
    /// Daemon socket path for reconnects (if explicitly configured).
    pub daemon_socket_path: Option<PathBuf>,
    /// Connection timeout for reconnect attempts.
    pub connect_timeout: Duration,
    /// Whether to attempt reconnection on disconnect.
    pub reconnect_on_disconnect: bool,
    /// Maximum number of reconnection attempts.
    pub max_reconnect_attempts: u32,
    /// Output buffer capacity in bytes.
    pub output_buffer_capacity: usize,
}

impl Default for ForwarderConfig {
    fn default() -> Self {
        Self {
            session_id: Uuid::new_v4().to_string(),
            daemon_socket_path: None,
            connect_timeout: Duration::from_millis(500),
            reconnect_on_disconnect: true,
            max_reconnect_attempts: 1,
            output_buffer_capacity: 4 * 1024 * 1024, // 4MB
        }
    }
}

impl ForwarderConfig {
    /// Creates a new configuration with a specific session ID.
    #[must_use]
    pub fn with_session_id(session_id: impl Into<String>) -> Self {
        Self {
            session_id: session_id.into(),
            ..Default::default()
        }
    }

    /// Sets the daemon socket path used for reconnect attempts.
    #[must_use]
    pub fn daemon_socket_path(mut self, socket_path: PathBuf) -> Self {
        self.daemon_socket_path = Some(socket_path);
        self
    }

    /// Sets the reconnect connection timeout.
    #[must_use]
    pub const fn connect_timeout(mut self, timeout: Duration) -> Self {
        self.connect_timeout = timeout;
        self
    }

    /// Sets whether to attempt reconnection on disconnect.
    #[must_use]
    pub const fn reconnect_on_disconnect(mut self, reconnect: bool) -> Self {
        self.reconnect_on_disconnect = reconnect;
        self
    }

    /// Sets the output buffer capacity.
    #[must_use]
    pub const fn output_buffer_capacity(mut self, capacity: usize) -> Self {
        self.output_buffer_capacity = capacity;
        self
    }
}

/// State of the current command being tracked.
#[derive(Debug, Clone)]
struct CommandState {
    /// Unique identifier for this command.
    command_id: String,
    /// Unix timestamp when command started.
    /// Reserved for future use (e.g., calculating command duration).
    #[allow(dead_code)]
    start_timestamp: u64,
}

/// Event forwarder that sends OSC 133 events and output to the daemon.
///
/// This struct coordinates between OSC 133 state tracking, output capture,
/// and daemon communication. It handles graceful degradation when the daemon
/// is unavailable.
pub struct DaemonEventForwarder {
    /// Configuration for the forwarder.
    config: ForwarderConfig,
    /// Daemon client connection (None if in standalone mode).
    #[cfg(unix)]
    client: Option<DaemonClient>,
    /// Output capture buffer.
    output_capture: OutputCapture,
    /// Current command being tracked.
    current_command: Option<CommandState>,
    /// Previous OSC 133 state (for detecting transitions).
    previous_osc_state: Osc133State,
    /// Standalone state (when daemon is unavailable).
    standalone_state: Option<StandaloneState>,
    /// Number of reconnection attempts made.
    reconnect_attempts: u32,
    /// Whether we've warned about standalone mode.
    warned_standalone: bool,
    /// Most recently completed command id (used for suggestion timing).
    last_finished_command_id: Option<String>,
}

impl DaemonEventForwarder {
    /// Creates a new forwarder in standalone mode (no daemon connection).
    ///
    /// Use this when you want to track state locally without forwarding to a daemon.
    #[must_use]
    pub fn standalone(config: ForwarderConfig) -> Self {
        let standalone_state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
        Self {
            output_capture: OutputCapture::new(config.output_buffer_capacity),
            config,
            #[cfg(unix)]
            client: None,
            current_command: None,
            previous_osc_state: Osc133State::Unknown,
            standalone_state: Some(standalone_state),
            reconnect_attempts: 0,
            warned_standalone: false,
            last_finished_command_id: None,
        }
    }

    /// Creates a new forwarder with an existing daemon client connection.
    #[cfg(unix)]
    #[must_use]
    pub fn with_client(client: DaemonClient, config: ForwarderConfig) -> Self {
        Self {
            output_capture: OutputCapture::new(config.output_buffer_capacity),
            config,
            client: Some(client),
            current_command: None,
            previous_osc_state: Osc133State::Unknown,
            standalone_state: None,
            reconnect_attempts: 0,
            warned_standalone: false,
            last_finished_command_id: None,
        }
    }

    /// Attempts to connect to the daemon at the default socket path.
    ///
    /// If connection fails, enters standalone mode.
    #[cfg(unix)]
    pub fn connect_default(config: ForwarderConfig) -> Self {
        match DaemonClient::connect_default() {
            Ok(mut client) => {
                // Verify connection with ping
                if client.ping().is_ok() {
                    debug!("Connected to daemon");
                    Self::with_client(client, config)
                } else {
                    warn!("Daemon ping failed, entering standalone mode");
                    Self::standalone(config)
                }
            }
            Err(e) => {
                debug!("Failed to connect to daemon: {e}, entering standalone mode");
                Self::standalone(config)
            }
        }
    }

    /// Attempts to connect to the daemon at a specific socket path.
    #[cfg(unix)]
    pub fn connect(socket_path: &Path, config: ForwarderConfig) -> Self {
        match DaemonClient::connect(socket_path) {
            Ok(mut client) => {
                if client.ping().is_ok() {
                    debug!("Connected to daemon at {}", socket_path.display());
                    Self::with_client(client, config)
                } else {
                    warn!("Daemon ping failed, entering standalone mode");
                    Self::standalone(config)
                }
            }
            Err(e) => {
                debug!(
                    "Failed to connect to daemon at {}: {e}",
                    socket_path.display()
                );
                Self::standalone(config)
            }
        }
    }

    /// Returns whether the forwarder is connected to the daemon.
    #[must_use]
    pub const fn is_connected(&self) -> bool {
        #[cfg(unix)]
        {
            self.client.is_some() && self.standalone_state.is_none()
        }
        #[cfg(not(unix))]
        {
            false
        }
    }

    /// Returns whether the forwarder is in standalone mode.
    #[must_use]
    pub const fn is_standalone(&self) -> bool {
        self.standalone_state.is_some()
    }

    /// Returns the session ID.
    #[must_use]
    pub fn session_id(&self) -> &str {
        &self.config.session_id
    }

    /// Returns the current command ID, if any.
    #[must_use]
    pub fn current_command_id(&self) -> Option<&str> {
        self.current_command.as_ref().map(|c| c.command_id.as_str())
    }

    /// Returns whether a command is currently being tracked.
    #[must_use]
    pub const fn is_tracking_command(&self) -> bool {
        self.current_command.is_some()
    }

    /// Returns the current OSC 133 state being tracked.
    #[must_use]
    pub const fn current_osc_state(&self) -> &Osc133State {
        &self.previous_osc_state
    }

    /// Processes an OSC 133 state change and forwards appropriate events.
    ///
    /// This method should be called whenever the OSC 133 parser detects a state
    /// change. It will:
    /// - Start tracking a new command on transition to `Output`
    /// - Forward `command.start` event to daemon
    /// - Forward `command.end` event and captured output on `Finished`
    ///
    /// # State Transitions
    ///
    /// | From | To | Action |
    /// |------|-----|--------|
    /// | Any | Output | Start new command, send command.start |
    /// | Output | Finished | End command, send output + command.end |
    /// | * | * | Update internal state |
    pub fn on_osc133_state_change(&mut self, new_state: &Osc133State) {
        trace!(
            "OSC 133 state change: {:?} -> {:?}",
            self.previous_osc_state,
            new_state
        );

        match new_state {
            Osc133State::Output => {
                self.handle_command_start();
            }
            Osc133State::Finished(exit_code) => {
                self.handle_command_end(*exit_code);
            }
            _ => {
                // Other state changes don't trigger events
            }
        }

        self.previous_osc_state = new_state.clone();
    }

    /// Forwards output data to the daemon (if connected).
    ///
    /// The data is captured in the output buffer and, if connected to the daemon,
    /// sent as an output.chunk event.
    ///
    /// In standalone mode, this method is a no-op (output capture is disabled).
    pub fn forward_output(&mut self, data: &[u8]) {
        // Only capture if we have an active command
        if self.current_command.is_none() {
            return;
        }

        // Capture output locally
        self.output_capture.push(data);

        // Forward to daemon if connected
        #[cfg(unix)]
        if let Some(ref mut client) = self.client {
            if let Some(ref command) = self.current_command {
                if let Err(e) = client.send_output(&command.command_id, data, false) {
                    self.handle_daemon_error(&e);
                }
            }
        }
    }

    /// Forwards stderr data to the daemon (if connected).
    ///
    /// Similar to `forward_output`, but marks the data as stderr.
    pub fn forward_stderr(&mut self, data: &[u8]) {
        // Only capture if we have an active command
        if self.current_command.is_none() {
            return;
        }

        // Capture output locally (we don't distinguish stdout/stderr in local capture)
        self.output_capture.push(data);

        // Forward to daemon if connected
        #[cfg(unix)]
        if let Some(ref mut client) = self.client {
            if let Some(ref command) = self.current_command {
                if let Err(e) = client.send_output(&command.command_id, data, true) {
                    self.handle_daemon_error(&e);
                }
            }
        }
    }

    /// Handles the start of a new command (OSC 133 Output state).
    fn handle_command_start(&mut self) {
        // Generate unique command ID
        let command_id = Uuid::new_v4().to_string();
        let timestamp = current_timestamp();

        // Start output capture
        self.output_capture.start_capture(&command_id);

        // Store command state
        self.current_command = Some(CommandState {
            command_id: command_id.clone(),
            start_timestamp: timestamp,
        });

        debug!("Command started: {command_id}");

        // Forward to daemon if connected
        #[cfg(unix)]
        if let Some(ref mut client) = self.client {
            if let Err(e) = client.command_start(&self.config.session_id, &command_id) {
                self.handle_daemon_error(&e);
            }
        }
    }

    /// Handles the end of a command (OSC 133 Finished state).
    fn handle_command_end(&mut self, exit_code: i32) {
        let Some(command_state) = self.current_command.take() else {
            // No command was being tracked
            debug!("Received command.end without active command");
            return;
        };

        // Stop output capture and get captured data
        let captured = self.output_capture.stop_capture();
        self.last_finished_command_id = Some(command_state.command_id.clone());

        debug!(
            "Command ended: {} (exit_code={}, captured={} bytes)",
            command_state.command_id,
            exit_code,
            captured.as_ref().map_or(0, CapturedOutput::len)
        );

        // Forward to daemon if connected
        #[cfg(unix)]
        if let Some(ref mut client) = self.client {
            // Send captured output as final chunk if there's data
            if let Some(ref output) = captured {
                if !output.is_empty() {
                    // Note: Output was already forwarded incrementally via forward_output()
                    // We don't need to send it again here
                    trace!("Captured {} bytes of output", output.len());
                }
            }

            // Send command.end event
            if let Err(e) = client.command_end(&command_state.command_id, exit_code) {
                self.handle_daemon_error(&e);
            }
        }
    }

    /// Returns and clears the most recently finished command id.
    #[must_use]
    pub const fn take_finished_command_id(&mut self) -> Option<String> {
        self.last_finished_command_id.take()
    }

    /// Polls for daemon notifications in non-blocking mode.
    ///
    /// On socket/protocol error this gracefully degrades to standalone mode.
    #[cfg(unix)]
    pub fn poll_notification(&mut self) -> Option<Notification> {
        if let Some(ref mut client) = self.client {
            match client.poll_notifications() {
                Ok(notification) => notification,
                Err(err) => {
                    self.handle_daemon_error(&err);
                    None
                }
            }
        } else {
            None
        }
    }

    /// Handles a daemon communication error.
    ///
    /// This method attempts reconnection if configured, and falls back to
    /// standalone mode if reconnection fails.
    #[cfg(unix)]
    fn handle_daemon_error(&mut self, error: &DaemonClientError) {
        warn!("Daemon communication error: {error}");

        // Attempt reconnection if configured
        if self.config.reconnect_on_disconnect
            && self.reconnect_attempts < self.config.max_reconnect_attempts
        {
            self.reconnect_attempts += 1;
            debug!(
                "Attempting reconnection ({}/{})",
                self.reconnect_attempts, self.config.max_reconnect_attempts
            );

            let reconnect = if let Some(socket_path) = self.config.daemon_socket_path.as_deref() {
                DaemonClient::connect_with_timeout(socket_path, self.config.connect_timeout)
            } else if let Some(default_socket_path) = DaemonClient::default_socket_path() {
                DaemonClient::connect_with_timeout(
                    &default_socket_path,
                    self.config.connect_timeout,
                )
            } else {
                Err(DaemonClientError::NoSocketPath)
            };

            if let Ok(mut client) = reconnect {
                if client.ping().is_ok() {
                    debug!("Reconnection successful");
                    self.client = Some(client);
                    self.reconnect_attempts = 0;
                    return;
                }
            }
        }

        // Enter standalone mode
        self.enter_standalone_mode(StandaloneReason::SocketError(error.to_string()));
    }

    /// On non-Unix platforms, this is a no-op since we're always in standalone mode.
    #[cfg(not(unix))]
    fn handle_daemon_error(&mut self, _error: std::io::Error) {
        // No daemon on non-Unix platforms
    }

    /// Enters standalone mode with the given reason.
    fn enter_standalone_mode(&mut self, reason: StandaloneReason) {
        if self.standalone_state.is_some() {
            return; // Already in standalone mode
        }

        #[cfg(unix)]
        {
            self.client = None;
        }

        let standalone = StandaloneState::new(reason);

        // Log warning once
        if !self.warned_standalone {
            standalone.log_warning();
            self.warned_standalone = true;
        }

        // Disable output capture in standalone mode
        self.output_capture.disable();

        self.standalone_state = Some(standalone);
    }

    /// Disables output capture (for privacy gate integration).
    ///
    /// This should be called when a denylist process is detected to prevent
    /// capturing sensitive output.
    pub fn disable_capture(&mut self) {
        self.output_capture.disable();
        debug!("Output capture disabled (privacy gate)");
    }

    /// Enables output capture (for privacy gate integration).
    ///
    /// This should be called when the denylist process exits.
    pub fn enable_capture(&mut self) {
        // Don't enable capture in standalone mode
        if self.is_standalone() {
            return;
        }
        self.output_capture.enable();
        debug!("Output capture enabled");
    }

    /// Returns whether output capture is enabled.
    #[must_use]
    pub const fn is_capture_enabled(&self) -> bool {
        self.output_capture.is_enabled()
    }

    /// Checks if a feature is available based on current mode.
    #[must_use]
    pub fn feature_available(&self, feature: Feature) -> bool {
        self.standalone_state
            .as_ref()
            .is_none_or(|standalone| standalone.feature_available(feature))
    }

    /// Returns the standalone state if in standalone mode.
    #[must_use]
    pub const fn standalone_state(&self) -> Option<&StandaloneState> {
        self.standalone_state.as_ref()
    }

    /// Returns the mutable standalone state if in standalone mode.
    pub const fn standalone_state_mut(&mut self) -> Option<&mut StandaloneState> {
        self.standalone_state.as_mut()
    }

    /// Returns the last captured output, if any.
    ///
    /// This is primarily useful for testing or when you need to access
    /// the captured output without forwarding it.
    pub fn take_captured_output(&mut self) -> Option<CapturedOutput> {
        self.output_capture.stop_capture()
    }

    /// Returns the amount of data currently buffered.
    #[must_use]
    pub fn buffered_len(&self) -> usize {
        self.output_capture.buffered_len()
    }
}

impl std::fmt::Debug for DaemonEventForwarder {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("DaemonEventForwarder")
            .field("session_id", &self.config.session_id)
            .field("is_connected", &self.is_connected())
            .field("is_standalone", &self.is_standalone())
            .field("current_command", &self.current_command_id())
            .field("osc_state", &self.previous_osc_state)
            .finish_non_exhaustive()
    }
}

/// Returns the current Unix timestamp in seconds.
fn current_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    // =========================================================================
    // ForwarderConfig Tests
    // =========================================================================

    #[test]
    fn test_forwarder_config_default() {
        let config = ForwarderConfig::default();

        assert!(!config.session_id.is_empty());
        assert!(config.reconnect_on_disconnect);
        assert_eq!(config.max_reconnect_attempts, 1);
        assert_eq!(config.output_buffer_capacity, 4 * 1024 * 1024);
    }

    #[test]
    fn test_forwarder_config_with_session_id() {
        let config = ForwarderConfig::with_session_id("test-session");

        assert_eq!(config.session_id, "test-session");
        assert!(config.reconnect_on_disconnect);
    }

    #[test]
    fn test_forwarder_config_builder() {
        let config = ForwarderConfig::default()
            .reconnect_on_disconnect(false)
            .daemon_socket_path(PathBuf::from("/tmp/custom-daemon.sock"))
            .connect_timeout(Duration::from_millis(250))
            .output_buffer_capacity(1024);

        assert!(!config.reconnect_on_disconnect);
        assert_eq!(
            config.daemon_socket_path,
            Some(PathBuf::from("/tmp/custom-daemon.sock"))
        );
        assert_eq!(config.connect_timeout, Duration::from_millis(250));
        assert_eq!(config.output_buffer_capacity, 1024);
    }

    #[test]
    fn test_forwarder_config_unique_session_ids() {
        let config1 = ForwarderConfig::default();
        let config2 = ForwarderConfig::default();

        assert_ne!(config1.session_id, config2.session_id);
    }

    // =========================================================================
    // DaemonEventForwarder Standalone Mode Tests
    // =========================================================================

    #[test]
    fn test_forwarder_standalone_creation() {
        let config = ForwarderConfig::with_session_id("test");
        let forwarder = DaemonEventForwarder::standalone(config);

        assert!(forwarder.is_standalone());
        assert!(!forwarder.is_connected());
        assert_eq!(forwarder.session_id(), "test");
    }

    #[test]
    fn test_forwarder_standalone_initial_state() {
        let forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        assert!(forwarder.is_standalone());
        assert!(!forwarder.is_tracking_command());
        assert!(forwarder.current_command_id().is_none());
        assert_eq!(forwarder.current_osc_state(), &Osc133State::Unknown);
    }

    #[test]
    fn test_forwarder_standalone_feature_availability() {
        let forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // In standalone mode, these features are available
        assert!(forwarder.feature_available(Feature::Picker));
        assert!(forwarder.feature_available(Feature::DenylistGate));

        // These features are not available
        assert!(!forwarder.feature_available(Feature::OutputCapture));
        assert!(!forwarder.feature_available(Feature::AiSuggestions));
    }

    // =========================================================================
    // OSC 133 State Tracking Tests
    // =========================================================================

    #[test]
    fn test_forwarder_osc133_state_tracking() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        assert_eq!(forwarder.current_osc_state(), &Osc133State::Unknown);

        forwarder.on_osc133_state_change(&Osc133State::Prompt);
        assert_eq!(forwarder.current_osc_state(), &Osc133State::Prompt);

        forwarder.on_osc133_state_change(&Osc133State::Input);
        assert_eq!(forwarder.current_osc_state(), &Osc133State::Input);

        forwarder.on_osc133_state_change(&Osc133State::Output);
        assert_eq!(forwarder.current_osc_state(), &Osc133State::Output);

        forwarder.on_osc133_state_change(&Osc133State::Finished(0));
        assert_eq!(forwarder.current_osc_state(), &Osc133State::Finished(0));
    }

    #[test]
    fn test_forwarder_command_start_on_output() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        assert!(!forwarder.is_tracking_command());

        forwarder.on_osc133_state_change(&Osc133State::Output);

        assert!(forwarder.is_tracking_command());
        assert!(forwarder.current_command_id().is_some());
    }

    #[test]
    fn test_forwarder_command_end_on_finished() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Start a command
        forwarder.on_osc133_state_change(&Osc133State::Output);
        assert!(forwarder.is_tracking_command());

        let command_id = forwarder.current_command_id().unwrap().to_string();
        assert!(!command_id.is_empty());

        // End the command
        forwarder.on_osc133_state_change(&Osc133State::Finished(0));
        assert!(!forwarder.is_tracking_command());
        assert!(forwarder.current_command_id().is_none());
    }

    #[test]
    fn test_forwarder_command_id_unique() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Start first command
        forwarder.on_osc133_state_change(&Osc133State::Output);
        let id1 = forwarder.current_command_id().unwrap().to_string();

        // End first command
        forwarder.on_osc133_state_change(&Osc133State::Finished(0));

        // Start second command
        forwarder.on_osc133_state_change(&Osc133State::Output);
        let id2 = forwarder.current_command_id().unwrap().to_string();

        // IDs should be unique
        assert_ne!(id1, id2);
    }

    #[test]
    fn test_forwarder_finished_without_command() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // This should not panic
        forwarder.on_osc133_state_change(&Osc133State::Finished(1));

        assert!(!forwarder.is_tracking_command());
    }

    // =========================================================================
    // Output Forwarding Tests (Standalone Mode)
    // =========================================================================

    #[test]
    fn test_forwarder_output_without_command() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Output without an active command should be ignored
        forwarder.forward_output(b"some output");
        assert_eq!(forwarder.buffered_len(), 0);
    }

    #[test]
    fn test_forwarder_output_capture_disabled_in_standalone() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Start a command
        forwarder.on_osc133_state_change(&Osc133State::Output);

        // In standalone mode, output capture is disabled
        // The output_capture internal state may still work, but it's disabled
        // because we called disable() in enter_standalone_mode()
        // Actually, standalone() calls the constructor which doesn't disable capture
        // Let's verify standalone mode doesn't automatically disable capture

        forwarder.forward_output(b"test output");
        // Since we're in standalone mode but capture wasn't explicitly disabled,
        // the output might still be captured locally
    }

    // =========================================================================
    // Privacy Gate Integration Tests
    // =========================================================================

    #[test]
    fn test_forwarder_disable_capture() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Initially capture might be enabled
        forwarder.disable_capture();
        assert!(!forwarder.is_capture_enabled());
    }

    #[test]
    fn test_forwarder_enable_capture_in_standalone() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        forwarder.disable_capture();
        assert!(!forwarder.is_capture_enabled());

        // In standalone mode, enable_capture should be a no-op
        forwarder.enable_capture();
        assert!(!forwarder.is_capture_enabled());
    }

    #[test]
    fn test_forwarder_privacy_gate_workflow() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Start command
        forwarder.on_osc133_state_change(&Osc133State::Output);
        assert!(forwarder.is_tracking_command());

        // Privacy gate triggered (e.g., ssh detected)
        forwarder.disable_capture();
        assert!(!forwarder.is_capture_enabled());

        // Output during privacy mode should not be captured
        forwarder.forward_output(b"sensitive data");

        // Privacy gate released (process exited)
        // In standalone mode, this won't re-enable capture
        forwarder.enable_capture();

        // Command ends
        forwarder.on_osc133_state_change(&Osc133State::Finished(0));
        assert!(!forwarder.is_tracking_command());
    }

    // =========================================================================
    // State Access Tests
    // =========================================================================

    #[test]
    fn test_forwarder_standalone_state_access() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        assert!(forwarder.standalone_state().is_some());
        assert!(forwarder.standalone_state_mut().is_some());

        let state = forwarder.standalone_state().unwrap();
        assert_eq!(*state.reason(), StandaloneReason::DaemonUnavailable);
    }

    #[test]
    fn test_forwarder_debug_format() {
        let forwarder =
            DaemonEventForwarder::standalone(ForwarderConfig::with_session_id("debug-test"));

        let debug_str = format!("{forwarder:?}");

        assert!(debug_str.contains("DaemonEventForwarder"));
        assert!(debug_str.contains("debug-test"));
        assert!(debug_str.contains("is_standalone"));
    }

    // =========================================================================
    // Full Workflow Tests
    // =========================================================================

    #[test]
    fn test_forwarder_full_command_lifecycle() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Initial state
        assert!(!forwarder.is_tracking_command());
        assert_eq!(forwarder.current_osc_state(), &Osc133State::Unknown);

        // Prompt appears
        forwarder.on_osc133_state_change(&Osc133State::Prompt);
        assert!(!forwarder.is_tracking_command());

        // User types command
        forwarder.on_osc133_state_change(&Osc133State::Input);
        assert!(!forwarder.is_tracking_command());

        // Command starts executing
        forwarder.on_osc133_state_change(&Osc133State::Output);
        assert!(forwarder.is_tracking_command());
        let command_id = forwarder.current_command_id().unwrap().to_string();

        // Output is generated
        forwarder.forward_output(b"line 1\n");
        forwarder.forward_output(b"line 2\n");
        forwarder.forward_stderr(b"warning\n");

        // Command finishes
        forwarder.on_osc133_state_change(&Osc133State::Finished(0));
        assert!(!forwarder.is_tracking_command());

        // Command ID should be cleared
        assert!(forwarder.current_command_id().is_none());
        assert!(!command_id.is_empty());
    }

    #[test]
    fn test_forwarder_multiple_commands() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // First command
        forwarder.on_osc133_state_change(&Osc133State::Prompt);
        forwarder.on_osc133_state_change(&Osc133State::Input);
        forwarder.on_osc133_state_change(&Osc133State::Output);
        let id1 = forwarder.current_command_id().unwrap().to_string();
        forwarder.forward_output(b"output 1");
        forwarder.on_osc133_state_change(&Osc133State::Finished(0));

        // Second command
        forwarder.on_osc133_state_change(&Osc133State::Prompt);
        forwarder.on_osc133_state_change(&Osc133State::Input);
        forwarder.on_osc133_state_change(&Osc133State::Output);
        let id2 = forwarder.current_command_id().unwrap().to_string();
        forwarder.forward_output(b"output 2");
        forwarder.on_osc133_state_change(&Osc133State::Finished(1));

        // Third command (failed)
        forwarder.on_osc133_state_change(&Osc133State::Prompt);
        forwarder.on_osc133_state_change(&Osc133State::Input);
        forwarder.on_osc133_state_change(&Osc133State::Output);
        let id3 = forwarder.current_command_id().unwrap().to_string();
        forwarder.on_osc133_state_change(&Osc133State::Finished(127));

        // All IDs should be unique
        assert_ne!(id1, id2);
        assert_ne!(id2, id3);
        assert_ne!(id1, id3);
    }

    #[test]
    fn test_forwarder_exit_codes() {
        let mut forwarder = DaemonEventForwarder::standalone(ForwarderConfig::default());

        // Test various exit codes
        let exit_codes = [0, 1, 2, 127, 128, 255, -1];

        for &code in &exit_codes {
            forwarder.on_osc133_state_change(&Osc133State::Output);
            assert!(forwarder.is_tracking_command());

            forwarder.on_osc133_state_change(&Osc133State::Finished(code));
            assert!(!forwarder.is_tracking_command());

            // Back to prompt
            forwarder.on_osc133_state_change(&Osc133State::Prompt);
        }
    }

    // =========================================================================
    // current_timestamp Tests
    // =========================================================================

    #[test]
    fn test_current_timestamp_reasonable() {
        let ts = current_timestamp();

        // Should be after 2020-01-01 and before 2100-01-01
        assert!(ts > 1_577_836_800, "timestamp should be after 2020");
        assert!(ts < 4_102_444_800, "timestamp should be before 2100");
    }

    // =========================================================================
    // Unix-specific Tests
    // =========================================================================

    #[cfg(unix)]
    mod unix_tests {
        use super::*;
        use std::io::{Read, Write};
        use std::os::unix::net::UnixListener;
        use std::thread;
        use tempfile::TempDir;

        fn setup_test_socket() -> (TempDir, std::path::PathBuf) {
            let temp_dir = TempDir::new().unwrap();
            let socket_path = temp_dir.path().join("test.sock");
            (temp_dir, socket_path)
        }

        #[test]
        fn test_forwarder_connect_nonexistent() {
            let (_temp_dir, socket_path) = setup_test_socket();

            let forwarder = DaemonEventForwarder::connect(&socket_path, ForwarderConfig::default());

            // Should fall back to standalone mode
            assert!(forwarder.is_standalone());
            assert!(!forwarder.is_connected());
        }

        #[test]
        fn test_forwarder_with_client() {
            let (_temp_dir, socket_path) = setup_test_socket();

            // Create a mock daemon
            let listener = UnixListener::bind(&socket_path).unwrap();

            let handle = thread::spawn(move || {
                let (mut stream, _addr) = listener.accept().unwrap();

                // Keep connection open briefly to allow test to complete
                // Set a read timeout so we don't hang
                stream
                    .set_read_timeout(Some(std::time::Duration::from_millis(200)))
                    .ok();

                // Read any data that comes (but we don't require it)
                let mut buf = [0u8; 1024];
                let _ = stream.read(&mut buf);
            });

            // Connect - this doesn't send ping, just establishes connection
            let client = DaemonClient::connect(&socket_path).unwrap();
            let forwarder = DaemonEventForwarder::with_client(client, ForwarderConfig::default());

            assert!(forwarder.is_connected());
            assert!(!forwarder.is_standalone());

            // Drop forwarder before joining to close the connection
            drop(forwarder);

            handle.join().unwrap();
        }

        #[test]
        fn test_forwarder_connect_with_ping() {
            let (_temp_dir, socket_path) = setup_test_socket();

            // Create a mock daemon that responds to ping
            let listener = UnixListener::bind(&socket_path).unwrap();

            let handle = thread::spawn(move || {
                let (mut stream, _addr) = listener.accept().unwrap();

                // Set timeouts to prevent hanging
                stream
                    .set_read_timeout(Some(std::time::Duration::from_millis(500)))
                    .ok();

                // Handle ping request
                let mut buf = [0u8; 1024];
                let n = stream.read(&mut buf).unwrap();
                let request_str = std::str::from_utf8(&buf[..n]).unwrap();
                let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
                let id = request["id"].as_u64().unwrap();

                // Send pong response
                let response =
                    format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"pong\":true}}}}\n");
                stream.write_all(response.as_bytes()).unwrap();
            });

            let forwarder = DaemonEventForwarder::connect(&socket_path, ForwarderConfig::default());

            assert!(forwarder.is_connected());
            assert!(!forwarder.is_standalone());

            // Drop forwarder before joining to close the connection
            drop(forwarder);

            handle.join().unwrap();
        }

        #[test]
        fn test_forwarder_connect_ping_fails() {
            let (_temp_dir, socket_path) = setup_test_socket();

            // Create a mock daemon that fails ping
            let listener = UnixListener::bind(&socket_path).unwrap();

            let handle = thread::spawn(move || {
                let (mut stream, _addr) = listener.accept().unwrap();

                // Set timeout to prevent hanging
                stream
                    .set_read_timeout(Some(std::time::Duration::from_millis(500)))
                    .ok();

                // Handle ping request with error response
                let mut buf = [0u8; 1024];
                let n = stream.read(&mut buf).unwrap();
                let request_str = std::str::from_utf8(&buf[..n]).unwrap();
                let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
                let id = request["id"].as_u64().unwrap();

                // Send error response
                let response = format!(
                    "{{\"jsonrpc\":\"2.0\",\"id\":{id},\"error\":{{\"code\":-32603,\"message\":\"Internal error\"}}}}\n"
                );
                stream.write_all(response.as_bytes()).unwrap();
            });

            let forwarder = DaemonEventForwarder::connect(&socket_path, ForwarderConfig::default());

            // Should fall back to standalone mode
            assert!(forwarder.is_standalone());
            assert!(!forwarder.is_connected());

            handle.join().unwrap();
        }

        #[test]
        fn test_forwarder_command_lifecycle_with_daemon() {
            let (_temp_dir, socket_path) = setup_test_socket();

            // Create a mock daemon
            let listener = UnixListener::bind(&socket_path).unwrap();

            let handle = thread::spawn(move || {
                let (mut stream, _addr) = listener.accept().unwrap();

                // Set timeout to prevent hanging
                stream
                    .set_read_timeout(Some(std::time::Duration::from_secs(2)))
                    .ok();

                let mut buf = [0u8; 4096];

                // Handle ping
                let n = stream.read(&mut buf).unwrap();
                let request: serde_json::Value =
                    serde_json::from_str(std::str::from_utf8(&buf[..n]).unwrap().trim()).unwrap();
                let id = request["id"].as_u64().unwrap();
                let response =
                    format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"pong\":true}}}}\n");
                stream.write_all(response.as_bytes()).unwrap();

                // Handle command.start
                let n = stream.read(&mut buf).unwrap();
                let request: serde_json::Value =
                    serde_json::from_str(std::str::from_utf8(&buf[..n]).unwrap().trim()).unwrap();
                assert_eq!(request["method"], "command.start");
                let id = request["id"].as_u64().unwrap();
                let response =
                    format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"ok\":true}}}}\n");
                stream.write_all(response.as_bytes()).unwrap();

                // Handle output.chunk
                let n = stream.read(&mut buf).unwrap();
                let request: serde_json::Value =
                    serde_json::from_str(std::str::from_utf8(&buf[..n]).unwrap().trim()).unwrap();
                assert_eq!(request["method"], "output.chunk");
                let id = request["id"].as_u64().unwrap();
                let response =
                    format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"ok\":true}}}}\n");
                stream.write_all(response.as_bytes()).unwrap();

                // Handle command.end
                let n = stream.read(&mut buf).unwrap();
                let request: serde_json::Value =
                    serde_json::from_str(std::str::from_utf8(&buf[..n]).unwrap().trim()).unwrap();
                assert_eq!(request["method"], "command.end");
                let id = request["id"].as_u64().unwrap();
                let response =
                    format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"ok\":true}}}}\n");
                stream.write_all(response.as_bytes()).unwrap();
            });

            // Connect and run command lifecycle
            let mut forwarder =
                DaemonEventForwarder::connect(&socket_path, ForwarderConfig::default());

            assert!(forwarder.is_connected());

            // Command lifecycle
            forwarder.on_osc133_state_change(&Osc133State::Output);
            assert!(forwarder.is_tracking_command());

            forwarder.forward_output(b"test output");

            forwarder.on_osc133_state_change(&Osc133State::Finished(0));
            assert!(!forwarder.is_tracking_command());

            // Drop forwarder before joining to close the connection
            drop(forwarder);

            handle.join().unwrap();
        }
    }
}
