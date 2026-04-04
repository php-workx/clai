//! JSON-RPC 2.0 protocol implementation for clai-wrap <-> clai-daemon communication.
//!
//! This module implements the IPC protocol specified in Section 3.4 of the tech spec:
//! - JSON-RPC 2.0 over Unix domain sockets (or named pipes on Windows)
//! - UTF-8 JSON, newline-delimited
//! - Max message size: 1 MiB
//!
//! # Message Types
//!
//! - [`Request`]: Messages from wrapper to daemon that expect a response
//! - [`Response`]: Daemon replies to requests
//! - [`Notification`]: Daemon-to-wrapper messages that don't expect a reply
//!
//! # Error Handling
//!
//! The [`RpcError`] type encapsulates JSON-RPC error responses with standard
//! error codes defined in [`RpcErrorCode`].

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Maximum message size in bytes (1 MiB per spec).
pub const MAX_MESSAGE_SIZE: usize = 1024 * 1024;

/// JSON-RPC protocol version string.
pub const JSONRPC_VERSION: &str = "2.0";

/// Errors that can occur during JSON-RPC message processing.
#[derive(Debug, Error)]
pub enum JsonRpcError {
    /// Message exceeds the maximum allowed size.
    #[error("message size {0} exceeds maximum of {MAX_MESSAGE_SIZE} bytes")]
    MessageTooLarge(usize),

    /// Failed to serialize a message to JSON.
    #[error("serialization failed: {0}")]
    SerializationFailed(#[from] serde_json::Error),

    /// Message is not valid UTF-8.
    #[error("message is not valid UTF-8: {0}")]
    InvalidUtf8(#[from] std::string::FromUtf8Error),

    /// Response has both result and error, or neither (violates JSON-RPC spec).
    #[error("{0}")]
    InvalidResponse(&'static str),
}

/// Standard JSON-RPC 2.0 error codes.
///
/// These codes are defined in the JSON-RPC 2.0 specification, with
/// custom application-specific codes in the -32000 range.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[repr(i32)]
pub enum RpcErrorCode {
    /// Invalid JSON was received.
    ParseError = -32700,
    /// The JSON sent is not a valid Request object.
    InvalidRequest = -32600,
    /// The method does not exist or is not available.
    MethodNotFound = -32601,
    /// Invalid method parameter(s).
    InvalidParams = -32602,
    /// Internal JSON-RPC error.
    InternalError = -32603,
    /// Daemon is busy processing another request (retry with backoff).
    DaemonBusy = -32000,
    /// The specified command ID was not found.
    CommandNotFound = -32001,
}

impl RpcErrorCode {
    /// Returns the numeric value of this error code.
    #[must_use]
    pub const fn code(self) -> i32 {
        self as i32
    }

    /// Returns a human-readable message for this error code.
    #[must_use]
    pub const fn message(self) -> &'static str {
        match self {
            Self::ParseError => "Parse error",
            Self::InvalidRequest => "Invalid Request",
            Self::MethodNotFound => "Method not found",
            Self::InvalidParams => "Invalid params",
            Self::InternalError => "Internal error",
            Self::DaemonBusy => "Daemon busy",
            Self::CommandNotFound => "Command not found",
        }
    }

    /// Attempts to convert a numeric code to an `RpcErrorCode`.
    #[must_use]
    pub const fn from_code(code: i32) -> Option<Self> {
        match code {
            -32700 => Some(Self::ParseError),
            -32600 => Some(Self::InvalidRequest),
            -32601 => Some(Self::MethodNotFound),
            -32602 => Some(Self::InvalidParams),
            -32603 => Some(Self::InternalError),
            -32000 => Some(Self::DaemonBusy),
            -32001 => Some(Self::CommandNotFound),
            _ => None,
        }
    }
}

/// A JSON-RPC 2.0 error object.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RpcError {
    /// The error code.
    pub code: i32,
    /// A short description of the error.
    pub message: String,
    /// Additional information about the error (optional).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
}

impl RpcError {
    /// Creates a new `RpcError` from an error code.
    #[must_use]
    pub fn from_code(code: RpcErrorCode) -> Self {
        Self {
            code: code.code(),
            message: code.message().to_string(),
            data: None,
        }
    }

    /// Creates a new `RpcError` with a custom message.
    #[must_use]
    pub fn with_message(code: RpcErrorCode, message: impl Into<String>) -> Self {
        Self {
            code: code.code(),
            message: message.into(),
            data: None,
        }
    }

    /// Creates a new `RpcError` with additional data.
    #[must_use]
    pub fn with_data(code: RpcErrorCode, data: serde_json::Value) -> Self {
        Self {
            code: code.code(),
            message: code.message().to_string(),
            data: Some(data),
        }
    }
}

/// A JSON-RPC 2.0 request message (wrapper -> daemon).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Request {
    /// JSON-RPC version (always "2.0").
    pub jsonrpc: String,
    /// Request identifier for matching responses.
    pub id: u64,
    /// The method to invoke.
    pub method: String,
    /// Method parameters.
    pub params: serde_json::Value,
}

impl Request {
    /// Creates a new request with the given method and parameters.
    #[must_use]
    pub fn new(id: u64, method: impl Into<String>, params: serde_json::Value) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            id,
            method: method.into(),
            params,
        }
    }

    /// Serializes this request to a newline-delimited JSON string.
    ///
    /// # Errors
    ///
    /// Returns an error if serialization fails or the message exceeds the max size.
    pub fn to_line(&self) -> Result<String, JsonRpcError> {
        let json = serde_json::to_string(self)?;
        if json.len() > MAX_MESSAGE_SIZE {
            return Err(JsonRpcError::MessageTooLarge(json.len()));
        }
        Ok(json + "\n")
    }

    /// Deserializes a request from a JSON string.
    ///
    /// # Errors
    ///
    /// Returns an error if deserialization fails or the message exceeds the max size.
    pub fn parse(s: &str) -> Result<Self, JsonRpcError> {
        if s.len() > MAX_MESSAGE_SIZE {
            return Err(JsonRpcError::MessageTooLarge(s.len()));
        }
        Ok(serde_json::from_str(s)?)
    }
}

/// A JSON-RPC 2.0 response message (daemon -> wrapper).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Response {
    /// JSON-RPC version (always "2.0").
    pub jsonrpc: String,
    /// Request identifier this response corresponds to.
    pub id: u64,
    /// The result of a successful request (mutually exclusive with error).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<serde_json::Value>,
    /// Error information for failed requests (mutually exclusive with result).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<RpcError>,
}

impl Response {
    /// Creates a successful response with the given result.
    #[must_use]
    pub fn success(id: u64, result: serde_json::Value) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            id,
            result: Some(result),
            error: None,
        }
    }

    /// Creates an error response.
    #[must_use]
    pub fn error(id: u64, error: RpcError) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            id,
            result: None,
            error: Some(error),
        }
    }

    /// Returns true if this response indicates success.
    #[must_use]
    pub const fn is_success(&self) -> bool {
        self.result.is_some() && self.error.is_none()
    }

    /// Returns true if this response indicates an error.
    #[must_use]
    pub const fn is_error(&self) -> bool {
        self.error.is_some()
    }

    /// Serializes this response to a newline-delimited JSON string.
    ///
    /// # Errors
    ///
    /// Returns an error if serialization fails or the message exceeds the max size.
    pub fn to_line(&self) -> Result<String, JsonRpcError> {
        let json = serde_json::to_string(self)?;
        if json.len() > MAX_MESSAGE_SIZE {
            return Err(JsonRpcError::MessageTooLarge(json.len()));
        }
        Ok(json + "\n")
    }

    /// Deserializes a response from a JSON string.
    ///
    /// # Errors
    ///
    /// Returns an error if deserialization fails or the message exceeds the max size.
    pub fn parse(s: &str) -> Result<Self, JsonRpcError> {
        if s.len() > MAX_MESSAGE_SIZE {
            return Err(JsonRpcError::MessageTooLarge(s.len()));
        }
        let resp: Self = serde_json::from_str(s)?;
        match (&resp.result, &resp.error) {
            (Some(_), Some(_)) => Err(JsonRpcError::InvalidResponse(
                "JSON-RPC response has both result and error",
            )),
            (None, None) => Err(JsonRpcError::InvalidResponse(
                "JSON-RPC response has neither result nor error",
            )),
            _ => Ok(resp),
        }
    }
}

/// A JSON-RPC 2.0 notification message (daemon -> wrapper, no response expected).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Notification {
    /// JSON-RPC version (always "2.0").
    pub jsonrpc: String,
    /// The method being notified.
    pub method: String,
    /// Notification parameters.
    pub params: serde_json::Value,
}

impl Notification {
    /// Creates a new notification with the given method and parameters.
    #[must_use]
    pub fn new(method: impl Into<String>, params: serde_json::Value) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: method.into(),
            params,
        }
    }

    /// Serializes this notification to a newline-delimited JSON string.
    ///
    /// # Errors
    ///
    /// Returns an error if serialization fails or the message exceeds the max size.
    pub fn to_line(&self) -> Result<String, JsonRpcError> {
        let json = serde_json::to_string(self)?;
        if json.len() > MAX_MESSAGE_SIZE {
            return Err(JsonRpcError::MessageTooLarge(json.len()));
        }
        Ok(json + "\n")
    }

    /// Deserializes a notification from a JSON string.
    ///
    /// # Errors
    ///
    /// Returns an error if deserialization fails or the message exceeds the max size.
    pub fn parse(s: &str) -> Result<Self, JsonRpcError> {
        if s.len() > MAX_MESSAGE_SIZE {
            return Err(JsonRpcError::MessageTooLarge(s.len()));
        }
        Ok(serde_json::from_str(s)?)
    }
}

// =============================================================================
// Request Builders
// =============================================================================

/// Creates a `ping` request.
///
/// The daemon should respond with `{"pong": true}`.
#[must_use]
pub fn ping_request(id: u64) -> Request {
    Request::new(id, "ping", serde_json::json!({}))
}

/// Creates a `command.start` request.
///
/// Notifies the daemon that a command has started executing.
#[must_use]
pub fn command_start_request(
    id: u64,
    session_id: &str,
    command_id: &str,
    timestamp: u64,
) -> Request {
    Request::new(
        id,
        "command.start",
        serde_json::json!({
            "session_id": session_id,
            "command_id": command_id,
            "timestamp": timestamp
        }),
    )
}

/// Creates a `command.end` request.
///
/// Notifies the daemon that a command has finished executing.
#[must_use]
pub fn command_end_request(id: u64, command_id: &str, exit_code: i32, timestamp: u64) -> Request {
    Request::new(
        id,
        "command.end",
        serde_json::json!({
            "command_id": command_id,
            "exit_code": exit_code,
            "timestamp": timestamp
        }),
    )
}

/// Creates an `output.chunk` request.
///
/// Sends a chunk of command output to the daemon for analysis.
/// The data is base64-encoded to handle binary content safely.
#[must_use]
pub fn output_chunk_request(
    id: u64,
    command_id: &str,
    data_base64: &str,
    is_stderr: bool,
) -> Request {
    Request::new(
        id,
        "output.chunk",
        serde_json::json!({
            "command_id": command_id,
            "data_base64": data_base64,
            "is_stderr": is_stderr
        }),
    )
}

// =============================================================================
// Notification Builders
// =============================================================================

/// Creates a `suggestion.available` notification.
///
/// Sent by the daemon to the wrapper when an AI suggestion is ready.
#[must_use]
pub fn suggestion_available_notification(command_id: &str, suggestion: &str) -> Notification {
    Notification::new(
        "suggestion.available",
        serde_json::json!({
            "command_id": command_id,
            "suggestion": suggestion
        }),
    )
}

// =============================================================================
// Response Builders
// =============================================================================

/// Creates a successful `ping` response.
#[must_use]
pub fn pong_response(id: u64) -> Response {
    Response::success(id, serde_json::json!({"pong": true}))
}

/// Creates an acknowledgment response for commands.
#[must_use]
pub fn ack_response(id: u64) -> Response {
    Response::success(id, serde_json::json!({"ok": true}))
}

/// Creates an error response from an error code.
#[must_use]
pub fn error_response(id: u64, code: RpcErrorCode) -> Response {
    Response::error(id, RpcError::from_code(code))
}

/// Creates an error response with a custom message.
#[must_use]
pub fn error_response_with_message(id: u64, code: RpcErrorCode, message: &str) -> Response {
    Response::error(id, RpcError::with_message(code, message))
}

// =============================================================================
// Message Validation
// =============================================================================

/// Validates that a message size is within the allowed limit.
///
/// # Errors
///
/// Returns `JsonRpcError::MessageTooLarge` if the size exceeds `MAX_MESSAGE_SIZE`.
pub const fn validate_message_size(size: usize) -> Result<(), JsonRpcError> {
    if size > MAX_MESSAGE_SIZE {
        Err(JsonRpcError::MessageTooLarge(size))
    } else {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // =========================================================================
    // RpcErrorCode Tests
    // =========================================================================

    #[test]
    fn test_error_code_values() {
        assert_eq!(RpcErrorCode::ParseError.code(), -32700);
        assert_eq!(RpcErrorCode::InvalidRequest.code(), -32600);
        assert_eq!(RpcErrorCode::MethodNotFound.code(), -32601);
        assert_eq!(RpcErrorCode::InvalidParams.code(), -32602);
        assert_eq!(RpcErrorCode::InternalError.code(), -32603);
        assert_eq!(RpcErrorCode::DaemonBusy.code(), -32000);
        assert_eq!(RpcErrorCode::CommandNotFound.code(), -32001);
    }

    #[test]
    fn test_error_code_from_code() {
        assert_eq!(
            RpcErrorCode::from_code(-32700),
            Some(RpcErrorCode::ParseError)
        );
        assert_eq!(
            RpcErrorCode::from_code(-32600),
            Some(RpcErrorCode::InvalidRequest)
        );
        assert_eq!(
            RpcErrorCode::from_code(-32601),
            Some(RpcErrorCode::MethodNotFound)
        );
        assert_eq!(
            RpcErrorCode::from_code(-32602),
            Some(RpcErrorCode::InvalidParams)
        );
        assert_eq!(
            RpcErrorCode::from_code(-32603),
            Some(RpcErrorCode::InternalError)
        );
        assert_eq!(
            RpcErrorCode::from_code(-32000),
            Some(RpcErrorCode::DaemonBusy)
        );
        assert_eq!(
            RpcErrorCode::from_code(-32001),
            Some(RpcErrorCode::CommandNotFound)
        );
        assert_eq!(RpcErrorCode::from_code(-99999), None);
        assert_eq!(RpcErrorCode::from_code(0), None);
    }

    #[test]
    fn test_error_code_messages() {
        assert_eq!(RpcErrorCode::ParseError.message(), "Parse error");
        assert_eq!(RpcErrorCode::InvalidRequest.message(), "Invalid Request");
        assert_eq!(RpcErrorCode::MethodNotFound.message(), "Method not found");
        assert_eq!(RpcErrorCode::InvalidParams.message(), "Invalid params");
        assert_eq!(RpcErrorCode::InternalError.message(), "Internal error");
        assert_eq!(RpcErrorCode::DaemonBusy.message(), "Daemon busy");
        assert_eq!(RpcErrorCode::CommandNotFound.message(), "Command not found");
    }

    // =========================================================================
    // RpcError Tests
    // =========================================================================

    #[test]
    fn test_rpc_error_from_code() {
        let error = RpcError::from_code(RpcErrorCode::ParseError);
        assert_eq!(error.code, -32700);
        assert_eq!(error.message, "Parse error");
        assert!(error.data.is_none());
    }

    #[test]
    fn test_rpc_error_with_message() {
        let error = RpcError::with_message(RpcErrorCode::InvalidParams, "custom message");
        assert_eq!(error.code, -32602);
        assert_eq!(error.message, "custom message");
        assert!(error.data.is_none());
    }

    #[test]
    fn test_rpc_error_with_data() {
        let data = serde_json::json!({"field": "value"});
        let error = RpcError::with_data(RpcErrorCode::InternalError, data.clone());
        assert_eq!(error.code, -32603);
        assert_eq!(error.message, "Internal error");
        assert_eq!(error.data, Some(data));
    }

    #[test]
    fn test_rpc_error_serialization() {
        let error = RpcError::from_code(RpcErrorCode::ParseError);
        let json = serde_json::to_string(&error).unwrap();
        let deserialized: RpcError = serde_json::from_str(&json).unwrap();
        assert_eq!(error, deserialized);
    }

    #[test]
    fn test_rpc_error_with_data_serialization() {
        let error = RpcError::with_data(
            RpcErrorCode::InvalidParams,
            serde_json::json!({"missing": ["field1", "field2"]}),
        );
        let json = serde_json::to_string(&error).unwrap();
        assert!(json.contains("\"data\""));
        let deserialized: RpcError = serde_json::from_str(&json).unwrap();
        assert_eq!(error, deserialized);
    }

    #[test]
    fn test_rpc_error_without_data_skips_field() {
        let error = RpcError::from_code(RpcErrorCode::ParseError);
        let json = serde_json::to_string(&error).unwrap();
        assert!(!json.contains("\"data\""));
    }

    // =========================================================================
    // Request Tests
    // =========================================================================

    #[test]
    fn test_request_new() {
        let request = Request::new(1, "test.method", serde_json::json!({"key": "value"}));
        assert_eq!(request.jsonrpc, "2.0");
        assert_eq!(request.id, 1);
        assert_eq!(request.method, "test.method");
        assert_eq!(request.params, serde_json::json!({"key": "value"}));
    }

    #[test]
    fn test_request_serialization() {
        let request = Request::new(42, "ping", serde_json::json!({}));
        let json = serde_json::to_string(&request).unwrap();
        assert!(json.contains("\"jsonrpc\":\"2.0\""));
        assert!(json.contains("\"id\":42"));
        assert!(json.contains("\"method\":\"ping\""));
        assert!(json.contains("\"params\":{}"));
    }

    #[test]
    fn test_request_deserialization() {
        let json = r#"{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}"#;
        let request = Request::parse(json).unwrap();
        assert_eq!(request.jsonrpc, "2.0");
        assert_eq!(request.id, 1);
        assert_eq!(request.method, "ping");
        assert_eq!(request.params, serde_json::json!({}));
    }

    #[test]
    fn test_request_roundtrip() {
        let original = Request::new(
            123,
            "command.start",
            serde_json::json!({
                "session_id": "sess1",
                "command_id": "cmd1",
                "timestamp": 1_234_567_890
            }),
        );
        let json = serde_json::to_string(&original).unwrap();
        let deserialized: Request = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_request_to_line() {
        let request = Request::new(1, "ping", serde_json::json!({}));
        let line = request.to_line().unwrap();
        assert!(line.ends_with('\n'));
        assert!(!line.trim().is_empty());
    }

    #[test]
    fn test_request_with_unknown_fields_ignored() {
        // Per spec: wrapper MUST accept responses with unknown fields (ignore them)
        let json =
            r#"{"jsonrpc":"2.0","id":1,"method":"ping","params":{},"unknown_field":"ignored"}"#;
        let request = Request::parse(json).unwrap();
        assert_eq!(request.method, "ping");
    }

    // =========================================================================
    // Response Tests
    // =========================================================================

    #[test]
    fn test_response_success() {
        let response = Response::success(1, serde_json::json!({"pong": true}));
        assert_eq!(response.jsonrpc, "2.0");
        assert_eq!(response.id, 1);
        assert!(response.is_success());
        assert!(!response.is_error());
        assert_eq!(response.result, Some(serde_json::json!({"pong": true})));
        assert!(response.error.is_none());
    }

    #[test]
    fn test_response_error() {
        let error = RpcError::from_code(RpcErrorCode::MethodNotFound);
        let response = Response::error(2, error.clone());
        assert_eq!(response.jsonrpc, "2.0");
        assert_eq!(response.id, 2);
        assert!(!response.is_success());
        assert!(response.is_error());
        assert!(response.result.is_none());
        assert_eq!(response.error, Some(error));
    }

    #[test]
    fn test_response_serialization_success() {
        let response = Response::success(1, serde_json::json!({"data": "value"}));
        let json = serde_json::to_string(&response).unwrap();
        assert!(json.contains("\"result\""));
        assert!(!json.contains("\"error\""));
    }

    #[test]
    fn test_response_serialization_error() {
        let response = Response::error(1, RpcError::from_code(RpcErrorCode::ParseError));
        let json = serde_json::to_string(&response).unwrap();
        assert!(!json.contains("\"result\""));
        assert!(json.contains("\"error\""));
    }

    #[test]
    fn test_response_deserialization_success() {
        let json = r#"{"jsonrpc":"2.0","id":1,"result":{"pong":true}}"#;
        let response = Response::parse(json).unwrap();
        assert!(response.is_success());
        assert_eq!(response.result, Some(serde_json::json!({"pong": true})));
    }

    #[test]
    fn test_response_deserialization_error() {
        let json =
            r#"{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}"#;
        let response = Response::parse(json).unwrap();
        assert!(response.is_error());
        let error = response.error.unwrap();
        assert_eq!(error.code, -32601);
        assert_eq!(error.message, "Method not found");
    }

    #[test]
    fn test_response_roundtrip() {
        let original = Response::success(999, serde_json::json!({"ok": true}));
        let json = serde_json::to_string(&original).unwrap();
        let deserialized: Response = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_response_to_line() {
        let response = Response::success(1, serde_json::json!({}));
        let line = response.to_line().unwrap();
        assert!(line.ends_with('\n'));
    }

    #[test]
    fn test_response_with_unknown_fields_ignored() {
        let json = r#"{"jsonrpc":"2.0","id":1,"result":{"ok":true},"extra":"ignored"}"#;
        let response = Response::parse(json).unwrap();
        assert!(response.is_success());
    }

    #[test]
    fn test_response_parse_result_only_ok() {
        let json = r#"{"jsonrpc":"2.0","id":1,"result":{"pong":true}}"#;
        let response = Response::parse(json).unwrap();
        assert!(response.is_success());
    }

    #[test]
    fn test_response_parse_error_only_ok() {
        let json =
            r#"{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}"#;
        let response = Response::parse(json).unwrap();
        assert!(response.is_error());
    }

    #[test]
    fn test_response_parse_both_result_and_error_err() {
        let json =
            r#"{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-32600,"message":"bad"}}"#;
        let result = Response::parse(json);
        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("both result and error"));
    }

    #[test]
    fn test_response_parse_neither_result_nor_error_err() {
        let json = r#"{"jsonrpc":"2.0","id":1}"#;
        let result = Response::parse(json);
        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("neither result nor error"));
    }

    // =========================================================================
    // Notification Tests
    // =========================================================================

    #[test]
    fn test_notification_new() {
        let notification =
            Notification::new("suggestion.available", serde_json::json!({"key": "value"}));
        assert_eq!(notification.jsonrpc, "2.0");
        assert_eq!(notification.method, "suggestion.available");
        assert_eq!(notification.params, serde_json::json!({"key": "value"}));
    }

    #[test]
    fn test_notification_serialization() {
        let notification = Notification::new(
            "suggestion.available",
            serde_json::json!({
                "command_id": "cmd1",
                "suggestion": "git push"
            }),
        );
        let json = serde_json::to_string(&notification).unwrap();
        assert!(json.contains("\"jsonrpc\":\"2.0\""));
        assert!(json.contains("\"method\":\"suggestion.available\""));
        assert!(!json.contains("\"id\""));
    }

    #[test]
    fn test_notification_deserialization() {
        let json = r#"{"jsonrpc":"2.0","method":"suggestion.available","params":{"command_id":"cmd1","suggestion":"git push"}}"#;
        let notification = Notification::parse(json).unwrap();
        assert_eq!(notification.method, "suggestion.available");
        assert_eq!(notification.params["command_id"], "cmd1");
        assert_eq!(notification.params["suggestion"], "git push");
    }

    #[test]
    fn test_notification_roundtrip() {
        let original = Notification::new(
            "suggestion.available",
            serde_json::json!({"command_id": "x", "suggestion": "y"}),
        );
        let json = serde_json::to_string(&original).unwrap();
        let deserialized: Notification = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_notification_to_line() {
        let notification = Notification::new("test", serde_json::json!({}));
        let line = notification.to_line().unwrap();
        assert!(line.ends_with('\n'));
    }

    // =========================================================================
    // Builder Function Tests
    // =========================================================================

    #[test]
    fn test_ping_request() {
        let request = ping_request(1);
        assert_eq!(request.method, "ping");
        assert_eq!(request.params, serde_json::json!({}));
    }

    #[test]
    fn test_command_start_request() {
        let request = command_start_request(1, "session-123", "cmd-456", 1_234_567_890);
        assert_eq!(request.method, "command.start");
        assert_eq!(request.params["session_id"], "session-123");
        assert_eq!(request.params["command_id"], "cmd-456");
        assert_eq!(request.params["timestamp"], 1_234_567_890);
    }

    #[test]
    fn test_command_end_request() {
        let request = command_end_request(2, "cmd-456", 0, 1_234_567_899);
        assert_eq!(request.method, "command.end");
        assert_eq!(request.params["command_id"], "cmd-456");
        assert_eq!(request.params["exit_code"], 0);
        assert_eq!(request.params["timestamp"], 1_234_567_899);
    }

    #[test]
    fn test_command_end_request_with_error_exit() {
        let request = command_end_request(3, "cmd-789", 1, 1_234_567_899);
        assert_eq!(request.params["exit_code"], 1);
    }

    #[test]
    fn test_output_chunk_request() {
        let request = output_chunk_request(4, "cmd-456", "SGVsbG8gV29ybGQ=", false);
        assert_eq!(request.method, "output.chunk");
        assert_eq!(request.params["command_id"], "cmd-456");
        assert_eq!(request.params["data_base64"], "SGVsbG8gV29ybGQ=");
        assert_eq!(request.params["is_stderr"], false);
    }

    #[test]
    fn test_output_chunk_request_stderr() {
        let request = output_chunk_request(5, "cmd-456", "RXJyb3I=", true);
        assert_eq!(request.params["is_stderr"], true);
    }

    #[test]
    fn test_suggestion_available_notification() {
        let notification = suggestion_available_notification("cmd-456", "git push");
        assert_eq!(notification.method, "suggestion.available");
        assert_eq!(notification.params["command_id"], "cmd-456");
        assert_eq!(notification.params["suggestion"], "git push");
    }

    #[test]
    fn test_pong_response() {
        let response = pong_response(1);
        assert!(response.is_success());
        assert_eq!(response.result, Some(serde_json::json!({"pong": true})));
    }

    #[test]
    fn test_ack_response() {
        let response = ack_response(2);
        assert!(response.is_success());
        assert_eq!(response.result, Some(serde_json::json!({"ok": true})));
    }

    #[test]
    fn test_error_response() {
        let response = error_response(3, RpcErrorCode::MethodNotFound);
        assert!(response.is_error());
        let error = response.error.unwrap();
        assert_eq!(error.code, -32601);
        assert_eq!(error.message, "Method not found");
    }

    #[test]
    fn test_error_response_with_message() {
        let response =
            error_response_with_message(4, RpcErrorCode::InvalidParams, "missing 'command_id'");
        assert!(response.is_error());
        let error = response.error.unwrap();
        assert_eq!(error.code, -32602);
        assert_eq!(error.message, "missing 'command_id'");
    }

    // =========================================================================
    // Message Size Validation Tests
    // =========================================================================

    #[test]
    fn test_validate_message_size_ok() {
        assert!(validate_message_size(0).is_ok());
        assert!(validate_message_size(1024).is_ok());
        assert!(validate_message_size(MAX_MESSAGE_SIZE).is_ok());
    }

    #[test]
    fn test_validate_message_size_too_large() {
        let result = validate_message_size(MAX_MESSAGE_SIZE + 1);
        assert!(result.is_err());
        match result {
            Err(JsonRpcError::MessageTooLarge(size)) => {
                assert_eq!(size, MAX_MESSAGE_SIZE + 1);
            }
            _ => panic!("expected MessageTooLarge error"),
        }
    }

    #[test]
    fn test_request_too_large() {
        // Create a request with a very large params field
        let large_data = "x".repeat(MAX_MESSAGE_SIZE + 1);
        let request = Request::new(1, "test", serde_json::json!({"data": large_data}));
        let result = request.to_line();
        assert!(matches!(result, Err(JsonRpcError::MessageTooLarge(_))));
    }

    #[test]
    fn test_request_from_str_too_large() {
        let large_json = format!(
            r#"{{"jsonrpc":"2.0","id":1,"method":"test","params":{{"data":"{}"}}}}"#,
            "x".repeat(MAX_MESSAGE_SIZE)
        );
        let result = Request::parse(&large_json);
        assert!(matches!(result, Err(JsonRpcError::MessageTooLarge(_))));
    }

    #[test]
    fn test_max_message_size_constant() {
        assert_eq!(MAX_MESSAGE_SIZE, 1024 * 1024); // 1 MiB
    }

    // =========================================================================
    // Edge Cases Tests
    // =========================================================================

    #[test]
    fn test_request_with_unicode() {
        let request = Request::new(
            1,
            "test",
            serde_json::json!({"emoji": "🚀", "cjk": "日本語"}),
        );
        let line = request.to_line().unwrap();
        let parsed: Request = serde_json::from_str(line.trim()).unwrap();
        assert_eq!(parsed.params["emoji"], "🚀");
        assert_eq!(parsed.params["cjk"], "日本語");
    }

    #[test]
    fn test_request_with_special_characters() {
        let request = Request::new(
            1,
            "test",
            serde_json::json!({"newlines": "line1\nline2", "tabs": "col1\tcol2"}),
        );
        let json = serde_json::to_string(&request).unwrap();
        let parsed: Request = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.params["newlines"], "line1\nline2");
        assert_eq!(parsed.params["tabs"], "col1\tcol2");
    }

    #[test]
    fn test_request_with_empty_params() {
        let request = Request::new(1, "ping", serde_json::Value::Null);
        let json = serde_json::to_string(&request).unwrap();
        assert!(json.contains("\"params\":null"));
    }

    #[test]
    fn test_response_with_null_result() {
        let response = Response::success(1, serde_json::Value::Null);
        assert!(response.is_success());
        assert_eq!(response.result, Some(serde_json::Value::Null));
    }

    #[test]
    fn test_negative_exit_code() {
        let request = command_end_request(1, "cmd-1", -1, 1_234_567_890);
        assert_eq!(request.params["exit_code"], -1);
    }

    #[test]
    fn test_large_exit_code() {
        let request = command_end_request(1, "cmd-1", 255, 1_234_567_890);
        assert_eq!(request.params["exit_code"], 255);
    }

    #[test]
    fn test_json_rpc_version_constant() {
        assert_eq!(JSONRPC_VERSION, "2.0");
    }

    #[test]
    fn test_all_requests_have_correct_version() {
        let requests = [
            ping_request(1),
            command_start_request(2, "s", "c", 0),
            command_end_request(3, "c", 0, 0),
            output_chunk_request(4, "c", "data", false),
        ];

        for request in requests {
            assert_eq!(request.jsonrpc, "2.0");
        }
    }

    #[test]
    fn test_all_responses_have_correct_version() {
        let responses = [
            pong_response(1),
            ack_response(2),
            error_response(3, RpcErrorCode::ParseError),
        ];

        for response in responses {
            assert_eq!(response.jsonrpc, "2.0");
        }
    }

    #[test]
    fn test_all_notifications_have_correct_version() {
        let notification = suggestion_available_notification("cmd", "suggestion");
        assert_eq!(notification.jsonrpc, "2.0");
    }
}
