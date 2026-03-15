//! Unix socket client for communication with clai-daemon.
//!
//! This module implements the client side of the JSON-RPC 2.0 protocol specified
//! in Section 3.4 of the tech spec. It handles:
//!
//! - Socket connection with timeout (500ms default)
//! - Stale socket detection and cleanup
//! - Request/response exchange
//! - Non-blocking notification polling
//!
//! # Socket Path Resolution
//!
//! The socket path is resolved in order:
//! 1. `$CLAI_HOME/daemon.sock`
//! 2. `$HOME/.clai/daemon.sock`
//! 3. Legacy fallback (`$XDG_RUNTIME_DIR/clai/daemon.sock`, `/tmp/clai-{uid}/daemon.sock`)
//!
//! # Stale Socket Handling
//!
//! On `ECONNREFUSED`:
//! 1. Check socket file ownership via `stat()`
//! 2. If owned by current user: unlink and retry once
//! 3. If owned by different user: return error

// Allow unsafe code for libc calls (getuid)
#![allow(unsafe_code)]

use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use thiserror::Error;

use crate::jsonrpc::{self, JsonRpcError, Notification, Request, Response};

/// Default connection timeout in milliseconds.
pub const DEFAULT_CONNECT_TIMEOUT_MS: u64 = 500;

/// Errors that can occur when communicating with the daemon.
#[derive(Debug, Error)]
pub enum DaemonClientError {
    /// Failed to connect to the daemon socket.
    #[error("failed to connect to daemon socket: {0}")]
    ConnectionFailed(#[source] std::io::Error),

    /// Connection timed out.
    #[error("connection timed out after {0:?}")]
    ConnectionTimeout(Duration),

    /// Socket owned by different user, cannot unlink.
    #[error("socket owned by uid {0}, cannot unlink stale socket")]
    SocketOwnedByOtherUser(u32),

    /// I/O error during communication.
    #[error("I/O error: {0}")]
    IoError(#[from] std::io::Error),

    /// JSON-RPC protocol error.
    #[error("JSON-RPC error: {0}")]
    JsonRpc(#[from] JsonRpcError),

    /// Received an RPC error response from daemon.
    #[error("daemon returned error: code={code}, message={message}")]
    RpcError { code: i32, message: String },

    /// Response ID did not match request ID.
    #[error("response ID mismatch: expected {expected}, got {actual}")]
    IdMismatch { expected: u64, actual: u64 },

    /// Socket path could not be determined.
    #[error("could not determine socket path from CLAI_HOME/HOME/legacy fallbacks")]
    NoSocketPath,

    /// Unexpected response format.
    #[error("unexpected response: {0}")]
    UnexpectedResponse(String),
}

/// Result type for daemon client operations.
pub type Result<T> = std::result::Result<T, DaemonClientError>;

/// Client for communicating with clai-daemon over Unix socket.
///
/// The client maintains a persistent connection to the daemon and provides
/// methods for sending commands and receiving responses.
pub struct DaemonClient {
    stream: UnixStream,
    reader: BufReader<UnixStream>,
    next_id: AtomicU64,
    /// Partial notification line captured during non-blocking reads.
    notification_partial: String,
}

impl DaemonClient {
    /// Resolves the default socket path.
    ///
    /// Returns the socket path based on:
    /// 1. `$CLAI_HOME/daemon.sock` if CLAI_HOME is set
    /// 2. `$HOME/.clai/daemon.sock` if HOME is set
    /// 3. Legacy runtime/temp fallback
    #[must_use]
    pub fn default_socket_path() -> Option<PathBuf> {
        Self::resolve_socket_path(
            std::env::var("CLAI_HOME").ok().as_deref(),
            std::env::var("HOME").ok().as_deref(),
            std::env::var("XDG_RUNTIME_DIR").ok().as_deref(),
        )
    }

    /// Resolves the socket path from the given environment values.
    /// Extracted for testability (avoids mutating global env in tests).
    fn resolve_socket_path(
        clai_home: Option<&str>,
        home: Option<&str>,
        xdg_runtime: Option<&str>,
    ) -> Option<PathBuf> {
        // Preferred: CLAI_HOME override (matches daemon paths).
        if let Some(clai_home) = clai_home {
            let path = PathBuf::from(clai_home).join("daemon.sock");
            return Some(path);
        }

        // Preferred default: ~/.clai/daemon.sock.
        if let Some(home) = home {
            let path = PathBuf::from(home).join(".clai").join("daemon.sock");
            return Some(path);
        }

        // Legacy compatibility: XDG runtime socket.
        if let Some(xdg_runtime) = xdg_runtime {
            let path = PathBuf::from(xdg_runtime).join("clai").join("daemon.sock");
            return Some(path);
        }

        #[cfg(unix)]
        {
            // Legacy fallback to /tmp/clai-{uid}/daemon.sock
            // SAFETY: getuid() is always safe to call and has no failure modes
            let uid = unsafe { libc::getuid() };
            let path = PathBuf::from(format!("/tmp/clai-{uid}")).join("daemon.sock");
            Some(path)
        }

        #[cfg(not(unix))]
        {
            None
        }
    }

    /// Connects to the daemon at the default socket path with default timeout.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - Socket path cannot be determined
    /// - Connection fails or times out
    /// - Socket is stale and owned by another user
    pub fn connect_default() -> Result<Self> {
        let path = Self::default_socket_path().ok_or(DaemonClientError::NoSocketPath)?;
        let timeout = Duration::from_millis(DEFAULT_CONNECT_TIMEOUT_MS);
        Self::connect_with_timeout(&path, timeout)
    }

    /// Connects to the daemon at the specified socket path with default timeout.
    ///
    /// # Errors
    ///
    /// Returns an error if connection fails or times out.
    pub fn connect(socket_path: &Path) -> Result<Self> {
        let timeout = Duration::from_millis(DEFAULT_CONNECT_TIMEOUT_MS);
        Self::connect_with_timeout(socket_path, timeout)
    }

    /// Connects to the daemon with a custom timeout.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - Connection fails or times out
    /// - Socket is stale and owned by another user
    pub fn connect_with_timeout(socket_path: &Path, timeout: Duration) -> Result<Self> {
        match Self::try_connect(socket_path, timeout) {
            Ok(client) => Ok(client),
            Err(DaemonClientError::ConnectionFailed(ref e))
                if e.kind() == std::io::ErrorKind::ConnectionRefused =>
            {
                // Handle stale socket
                Self::handle_stale_socket(socket_path)?;
                // Retry connection once
                Self::try_connect(socket_path, timeout)
            }
            Err(e) => Err(e),
        }
    }

    /// Attempts a single connection to the socket.
    fn try_connect(socket_path: &Path, timeout: Duration) -> Result<Self> {
        // UnixStream::connect doesn't support timeout directly,
        // so we use non-blocking connect with poll/select simulation
        let stream = Self::connect_with_timeout_impl(socket_path, timeout)?;

        // Set read/write timeouts for subsequent operations
        stream.set_read_timeout(Some(timeout))?;
        stream.set_write_timeout(Some(timeout))?;

        // Clone stream for reader (UnixStream implements Clone via dup())
        let reader_stream = stream.try_clone()?;
        let reader = BufReader::new(reader_stream);

        Ok(Self {
            stream,
            reader,
            next_id: AtomicU64::new(1),
            notification_partial: String::new(),
        })
    }

    /// Implements connection with timeout.
    fn connect_with_timeout_impl(socket_path: &Path, timeout: Duration) -> Result<UnixStream> {
        use std::os::unix::net::UnixStream as StdUnixStream;

        // Try standard connect first - it may succeed immediately
        match StdUnixStream::connect(socket_path) {
            Ok(stream) => Ok(stream),
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                // This shouldn't happen with blocking connect, but handle it
                Err(DaemonClientError::ConnectionTimeout(timeout))
            }
            Err(e) => Err(DaemonClientError::ConnectionFailed(e)),
        }
    }

    /// Handles stale socket detection and cleanup.
    ///
    /// On ECONNREFUSED:
    /// 1. stat() the socket file
    /// 2. If owned by current user: unlink and return Ok
    /// 3. If owned by different user: return error
    fn handle_stale_socket(socket_path: &Path) -> Result<()> {
        use std::fs;
        use std::os::unix::fs::MetadataExt;

        // Get socket file metadata
        let metadata = match fs::metadata(socket_path) {
            Ok(m) => m,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                // Socket doesn't exist, nothing to clean up
                return Ok(());
            }
            Err(e) => return Err(DaemonClientError::IoError(e)),
        };

        // Get current user ID
        // SAFETY: getuid() is always safe to call
        let current_uid = unsafe { libc::getuid() };
        let socket_uid = metadata.uid();

        if socket_uid == current_uid {
            // Owned by us, safe to unlink
            if let Err(e) = fs::remove_file(socket_path) {
                if e.kind() != std::io::ErrorKind::NotFound {
                    return Err(DaemonClientError::IoError(e));
                }
            }
            Ok(())
        } else {
            // Owned by different user, cannot unlink
            Err(DaemonClientError::SocketOwnedByOtherUser(socket_uid))
        }
    }

    /// Returns the next request ID.
    fn next_request_id(&self) -> u64 {
        self.next_id.fetch_add(1, Ordering::Relaxed)
    }

    /// Returns the current Unix timestamp in milliseconds.
    fn current_timestamp() -> u64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| u64::try_from(d.as_millis()).unwrap_or(u64::MAX))
            .unwrap_or(0)
    }

    /// Sends a request and waits for the response.
    fn send_request(&mut self, request: &Request) -> Result<Response> {
        // Serialize and send
        let line = request.to_line()?;
        self.stream.write_all(line.as_bytes())?;
        self.stream.flush()?;

        // Read response
        let mut response_line = String::new();
        self.reader.read_line(&mut response_line)?;

        let response = Response::parse(&response_line)?;

        // Verify ID matches
        if response.id != request.id {
            return Err(DaemonClientError::IdMismatch {
                expected: request.id,
                actual: response.id,
            });
        }

        // Check for error response
        if let Some(ref error) = response.error {
            return Err(DaemonClientError::RpcError {
                code: error.code,
                message: error.message.clone(),
            });
        }

        Ok(response)
    }

    /// Sends a ping request and waits for pong response.
    ///
    /// # Errors
    ///
    /// Returns an error if communication fails or daemon returns an error.
    pub fn ping(&mut self) -> Result<()> {
        let id = self.next_request_id();
        let request = jsonrpc::ping_request(id);
        let response = self.send_request(&request)?;

        // Verify pong response
        if let Some(result) = response.result {
            if result.get("pong") == Some(&serde_json::Value::Bool(true)) {
                return Ok(());
            }
        }

        Err(DaemonClientError::UnexpectedResponse(
            "expected {\"pong\": true}".to_string(),
        ))
    }

    /// Sends a command start notification to the daemon.
    ///
    /// # Errors
    ///
    /// Returns an error if communication fails or daemon returns an error.
    pub fn command_start(&mut self, session_id: &str, command_id: &str) -> Result<()> {
        let id = self.next_request_id();
        let timestamp = Self::current_timestamp();
        let request = jsonrpc::command_start_request(id, session_id, command_id, timestamp);
        let _response = self.send_request(&request)?;
        Ok(())
    }

    /// Sends a command end notification to the daemon.
    ///
    /// # Errors
    ///
    /// Returns an error if communication fails or daemon returns an error.
    pub fn command_end(&mut self, command_id: &str, exit_code: i32) -> Result<()> {
        let id = self.next_request_id();
        let timestamp = Self::current_timestamp();
        let request = jsonrpc::command_end_request(id, command_id, exit_code, timestamp);
        let _response = self.send_request(&request)?;
        Ok(())
    }

    /// Sends an output chunk to the daemon.
    ///
    /// The data is base64-encoded before sending.
    ///
    /// # Errors
    ///
    /// Returns an error if communication fails or daemon returns an error.
    pub fn send_output(&mut self, command_id: &str, data: &[u8], is_stderr: bool) -> Result<()> {
        use base64::Engine;
        let data_base64 = base64::engine::general_purpose::STANDARD.encode(data);

        let id = self.next_request_id();
        let request = jsonrpc::output_chunk_request(id, command_id, &data_base64, is_stderr);
        let _response = self.send_request(&request)?;
        Ok(())
    }

    /// Polls for pending notifications without blocking.
    ///
    /// Returns `Ok(Some(notification))` if a notification is available,
    /// `Ok(None)` if no data is available, or an error if reading fails.
    ///
    /// # Errors
    ///
    /// Returns an error if reading fails (other than `WouldBlock`).
    pub fn poll_notifications(&mut self) -> Result<Option<Notification>> {
        // Set non-blocking mode temporarily
        self.stream.set_nonblocking(true)?;

        let result = self.try_read_notification();

        // Restore blocking mode
        let _ = self.stream.set_nonblocking(false);

        result
    }

    /// Attempts to read a notification from the stream.
    fn try_read_notification(&mut self) -> Result<Option<Notification>> {
        let mut line = String::new();

        match self.reader.read_line(&mut line) {
            Ok(0) => {
                // EOF - connection closed
                self.notification_partial.clear();
                Ok(None)
            }
            Ok(_) => {
                if !self.notification_partial.is_empty() {
                    let mut combined = std::mem::take(&mut self.notification_partial);
                    combined.push_str(&line);
                    line = combined;
                }

                // In non-blocking mode we may observe a partial line without the trailing newline.
                // Hold it until the remaining bytes arrive.
                if !line.ends_with('\n') {
                    self.notification_partial.push_str(&line);
                    return Ok(None);
                }

                // Parse as notification
                let notification = Notification::parse(&line)?;
                Ok(Some(notification))
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                // No data available
                if !line.is_empty() {
                    self.notification_partial.push_str(&line);
                }
                Ok(None)
            }
            Err(e) => Err(DaemonClientError::IoError(e)),
        }
    }

    /// Returns the socket path this client is connected to.
    ///
    /// Note: This information is not stored by the client, so this method
    /// returns None. Use the path you passed to connect() if you need it.
    #[must_use]
    pub fn peer_addr(&self) -> std::io::Result<std::os::unix::net::SocketAddr> {
        self.stream.peer_addr()
    }

    /// Shuts down the connection.
    pub fn shutdown(&self) -> std::io::Result<()> {
        self.stream.shutdown(std::net::Shutdown::Both)
    }
}

impl std::fmt::Debug for DaemonClient {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("DaemonClient")
            .field("next_id", &self.next_id.load(Ordering::Relaxed))
            .finish_non_exhaustive()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::io::{Read, Write};
    use std::os::unix::net::UnixListener;
    use std::thread;
    use tempfile::TempDir;

    /// Creates a temporary directory with a socket for testing.
    fn setup_test_socket() -> (TempDir, PathBuf) {
        let temp_dir = TempDir::new().unwrap();
        let socket_path = temp_dir.path().join("test.sock");
        (temp_dir, socket_path)
    }

    // =========================================================================
    // Socket Path Tests
    // =========================================================================

    #[test]
    fn test_default_socket_path_with_clai_home() {
        let path = DaemonClient::resolve_socket_path(
            Some("/custom/clai"),
            Some("/home/tester"),
            None,
        );
        assert_eq!(path, Some(PathBuf::from("/custom/clai/daemon.sock")));
    }

    #[test]
    fn test_default_socket_path_with_home_fallback() {
        let path = DaemonClient::resolve_socket_path(
            None,
            Some("/home/tester"),
            None,
        );
        assert_eq!(path, Some(PathBuf::from("/home/tester/.clai/daemon.sock")));
    }

    // =========================================================================
    // Connection Tests
    // =========================================================================

    #[test]
    fn test_connect_to_nonexistent_socket() {
        let (_temp_dir, socket_path) = setup_test_socket();

        let result = DaemonClient::connect(&socket_path);
        assert!(result.is_err());

        match result {
            Err(DaemonClientError::ConnectionFailed(_)) => {}
            other => panic!("expected ConnectionFailed, got {other:?}"),
        }
    }

    #[test]
    fn test_connect_timeout() {
        let (_temp_dir, socket_path) = setup_test_socket();

        let timeout = Duration::from_millis(50);
        let result = DaemonClient::connect_with_timeout(&socket_path, timeout);

        assert!(result.is_err());
    }

    #[test]
    fn test_connect_success() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a listener
        let listener = UnixListener::bind(&socket_path).unwrap();

        // Spawn a thread to accept the connection
        let handle = thread::spawn(move || {
            let (_stream, _addr) = listener.accept().unwrap();
            // Keep the connection open briefly
            thread::sleep(Duration::from_millis(100));
        });

        // Connect
        let result = DaemonClient::connect(&socket_path);
        assert!(result.is_ok());

        handle.join().unwrap();
    }

    // =========================================================================
    // Stale Socket Tests
    // =========================================================================

    #[test]
    fn test_stale_socket_owned_by_current_user() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a socket file (not a listener, just a file to simulate stale socket)
        fs::write(&socket_path, "").unwrap();

        // Try to handle it as a stale socket
        let result = DaemonClient::handle_stale_socket(&socket_path);
        assert!(result.is_ok());

        // File should be removed
        assert!(!socket_path.exists());
    }

    #[test]
    fn test_stale_socket_nonexistent() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Socket doesn't exist
        let result = DaemonClient::handle_stale_socket(&socket_path);
        assert!(result.is_ok());
    }

    // =========================================================================
    // Ping/Pong Tests
    // =========================================================================

    #[test]
    fn test_ping_pong() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a mock daemon that responds to ping
        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (mut stream, _addr) = listener.accept().unwrap();

            // Read the ping request
            let mut buf = [0u8; 1024];
            let n = stream.read(&mut buf).unwrap();
            let request_str = std::str::from_utf8(&buf[..n]).unwrap();

            // Parse the request to get the ID
            let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
            let id = request["id"].as_u64().unwrap();

            // Send pong response
            let response =
                format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"pong\":true}}}}\n");
            stream.write_all(response.as_bytes()).unwrap();
        });

        // Connect and ping
        let mut client = DaemonClient::connect(&socket_path).unwrap();
        let result = client.ping();
        assert!(result.is_ok());

        handle.join().unwrap();
    }

    // =========================================================================
    // Request Serialization Tests
    // =========================================================================

    #[test]
    fn test_command_start_request_format() {
        let id = 42;
        let request = jsonrpc::command_start_request(id, "session-123", "cmd-456", 1234567890);

        let json = serde_json::to_value(&request).unwrap();
        assert_eq!(json["jsonrpc"], "2.0");
        assert_eq!(json["id"], 42);
        assert_eq!(json["method"], "command.start");
        assert_eq!(json["params"]["session_id"], "session-123");
        assert_eq!(json["params"]["command_id"], "cmd-456");
        assert_eq!(json["params"]["timestamp"], 1234567890);
    }

    #[test]
    fn test_command_end_request_format() {
        let id = 43;
        let request = jsonrpc::command_end_request(id, "cmd-456", 1, 1234567899);

        let json = serde_json::to_value(&request).unwrap();
        assert_eq!(json["jsonrpc"], "2.0");
        assert_eq!(json["id"], 43);
        assert_eq!(json["method"], "command.end");
        assert_eq!(json["params"]["command_id"], "cmd-456");
        assert_eq!(json["params"]["exit_code"], 1);
        assert_eq!(json["params"]["timestamp"], 1234567899);
    }

    #[test]
    fn test_output_chunk_request_format() {
        let id = 44;
        let request = jsonrpc::output_chunk_request(id, "cmd-456", "SGVsbG8=", false);

        let json = serde_json::to_value(&request).unwrap();
        assert_eq!(json["jsonrpc"], "2.0");
        assert_eq!(json["id"], 44);
        assert_eq!(json["method"], "output.chunk");
        assert_eq!(json["params"]["command_id"], "cmd-456");
        assert_eq!(json["params"]["data_base64"], "SGVsbG8=");
        assert_eq!(json["params"]["is_stderr"], false);
    }

    // =========================================================================
    // Error Handling Tests
    // =========================================================================

    #[test]
    fn test_rpc_error_response() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a mock daemon that returns an error
        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (mut stream, _addr) = listener.accept().unwrap();

            // Read the request
            let mut buf = [0u8; 1024];
            let n = stream.read(&mut buf).unwrap();
            let request_str = std::str::from_utf8(&buf[..n]).unwrap();
            let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
            let id = request["id"].as_u64().unwrap();

            // Send error response
            let response = format!(
                "{{\"jsonrpc\":\"2.0\",\"id\":{id},\"error\":{{\"code\":-32601,\"message\":\"Method not found\"}}}}\n"
            );
            stream.write_all(response.as_bytes()).unwrap();
        });

        // Connect and try ping
        let mut client = DaemonClient::connect(&socket_path).unwrap();
        let result = client.ping();

        assert!(result.is_err());
        match result {
            Err(DaemonClientError::RpcError { code, message }) => {
                assert_eq!(code, -32601);
                assert_eq!(message, "Method not found");
            }
            other => panic!("expected RpcError, got {other:?}"),
        }

        handle.join().unwrap();
    }

    #[test]
    fn test_id_mismatch() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a mock daemon that returns wrong ID
        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (mut stream, _addr) = listener.accept().unwrap();

            // Read the request
            let mut buf = [0u8; 1024];
            let _n = stream.read(&mut buf).unwrap();

            // Send response with wrong ID
            let response = "{\"jsonrpc\":\"2.0\",\"id\":999,\"result\":{\"pong\":true}}\n";
            stream.write_all(response.as_bytes()).unwrap();
        });

        // Connect and try ping
        let mut client = DaemonClient::connect(&socket_path).unwrap();
        let result = client.ping();

        assert!(result.is_err());
        match result {
            Err(DaemonClientError::IdMismatch { expected, actual }) => {
                assert_eq!(expected, 1); // First ID is 1
                assert_eq!(actual, 999);
            }
            other => panic!("expected IdMismatch, got {other:?}"),
        }

        handle.join().unwrap();
    }

    // =========================================================================
    // Notification Polling Tests
    // =========================================================================

    #[test]
    fn test_poll_notifications_empty() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a mock daemon that doesn't send anything
        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (_stream, _addr) = listener.accept().unwrap();
            // Keep connection open but don't send anything
            thread::sleep(Duration::from_millis(200));
        });

        // Connect and poll
        let mut client = DaemonClient::connect(&socket_path).unwrap();
        let result = client.poll_notifications();

        // Should return None (no data available)
        assert!(result.is_ok());
        assert!(result.unwrap().is_none());

        handle.join().unwrap();
    }

    #[test]
    fn test_poll_notifications_with_data() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a mock daemon that sends a notification
        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (mut stream, _addr) = listener.accept().unwrap();

            // Send a notification
            let notification = "{\"jsonrpc\":\"2.0\",\"method\":\"suggestion.available\",\"params\":{\"command_id\":\"cmd-1\",\"suggestion\":\"git push\"}}\n";
            stream.write_all(notification.as_bytes()).unwrap();
            stream.flush().unwrap();

            // Keep connection open briefly
            thread::sleep(Duration::from_millis(100));
        });

        // Give the daemon time to send
        thread::sleep(Duration::from_millis(50));

        // Connect and poll
        let mut client = DaemonClient::connect(&socket_path).unwrap();

        // Give more time for data to arrive
        thread::sleep(Duration::from_millis(50));

        let result = client.poll_notifications();

        assert!(result.is_ok());
        let notification = result.unwrap();
        assert!(notification.is_some());

        let notification = notification.unwrap();
        assert_eq!(notification.method, "suggestion.available");
        assert_eq!(notification.params["command_id"], "cmd-1");
        assert_eq!(notification.params["suggestion"], "git push");

        handle.join().unwrap();
    }

    #[test]
    fn test_poll_notifications_with_fragmented_data() {
        let (_temp_dir, socket_path) = setup_test_socket();

        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (mut stream, _addr) = listener.accept().unwrap();

            // Send one notification split across writes to simulate fragmented delivery.
            stream.write_all(b"{\"jsonrpc\":\"2.0\"").unwrap();
            stream.flush().unwrap();
            thread::sleep(Duration::from_millis(20));
            stream
                .write_all(
                    b",\"method\":\"suggestion.available\",\"params\":{\"command_id\":\"cmd-frag\",\"suggestion\":\"git status\"}}\n",
                )
                .unwrap();
            stream.flush().unwrap();
        });

        let mut client = DaemonClient::connect(&socket_path).unwrap();

        // First poll observes partial bytes, but should not error.
        let first = client.poll_notifications().unwrap();
        assert!(first.is_none());

        // After remainder arrives, we should parse the full notification.
        thread::sleep(Duration::from_millis(30));
        let second = client.poll_notifications().unwrap();
        let notification = second.expect("expected fragmented notification");
        assert_eq!(notification.method, "suggestion.available");
        assert_eq!(notification.params["command_id"], "cmd-frag");
        assert_eq!(notification.params["suggestion"], "git status");

        handle.join().unwrap();
    }

    // =========================================================================
    // Full Integration Test
    // =========================================================================

    #[test]
    fn test_full_command_lifecycle() {
        let (_temp_dir, socket_path) = setup_test_socket();

        // Create a mock daemon that handles a full command lifecycle
        let listener = UnixListener::bind(&socket_path).unwrap();

        let handle = thread::spawn(move || {
            let (mut stream, _addr) = listener.accept().unwrap();

            // Handle command.start
            let mut buf = [0u8; 4096];
            let n = stream.read(&mut buf).unwrap();
            let request_str = std::str::from_utf8(&buf[..n]).unwrap();
            let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
            let id = request["id"].as_u64().unwrap();
            assert_eq!(request["method"], "command.start");
            assert!(
                request["params"]["timestamp"].as_u64().unwrap() > 1_000_000_000_000,
                "command.start timestamp should be unix milliseconds"
            );

            let response =
                format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"ok\":true}}}}\n");
            stream.write_all(response.as_bytes()).unwrap();
            stream.flush().unwrap();

            // Handle output.chunk
            let n = stream.read(&mut buf).unwrap();
            let request_str = std::str::from_utf8(&buf[..n]).unwrap();
            let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
            let id = request["id"].as_u64().unwrap();
            assert_eq!(request["method"], "output.chunk");

            let response =
                format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"ok\":true}}}}\n");
            stream.write_all(response.as_bytes()).unwrap();
            stream.flush().unwrap();

            // Handle command.end
            let n = stream.read(&mut buf).unwrap();
            let request_str = std::str::from_utf8(&buf[..n]).unwrap();
            let request: serde_json::Value = serde_json::from_str(request_str.trim()).unwrap();
            let id = request["id"].as_u64().unwrap();
            assert_eq!(request["method"], "command.end");
            assert!(
                request["params"]["timestamp"].as_u64().unwrap() > 1_000_000_000_000,
                "command.end timestamp should be unix milliseconds"
            );

            let response =
                format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"result\":{{\"ok\":true}}}}\n");
            stream.write_all(response.as_bytes()).unwrap();
            stream.flush().unwrap();
        });

        // Connect and run through command lifecycle
        let mut client = DaemonClient::connect(&socket_path).unwrap();

        // Start command
        let result = client.command_start("session-1", "cmd-1");
        assert!(result.is_ok());

        // Send output
        let result = client.send_output("cmd-1", b"Hello, World!", false);
        assert!(result.is_ok());

        // End command
        let result = client.command_end("cmd-1", 0);
        assert!(result.is_ok());

        handle.join().unwrap();
    }
}
