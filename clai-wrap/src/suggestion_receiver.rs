//! Suggestion receiver for clai-wrap.
//!
//! This module receives AI-powered suggestions from the daemon via JSON-RPC notifications
//! and queues them for display in the picker UI. It handles:
//!
//! - Receiving `suggestion.available` notifications from the daemon
//! - Parsing suggestion payloads (command suggestions, completions, explanations)
//! - Queuing suggestions with priorities for display
//! - Integration with the picker UI
//!
//! # Suggestion Types
//!
//! The receiver supports multiple suggestion types:
//!
//! | Type | Description | Priority |
//! |------|-------------|----------|
//! | `CommandFix` | Fix for a failed command | Highest (0) |
//! | `CommandCompletion` | Completion for partial command | High (1) |
//! | `CommandExplanation` | Explanation of command output | Medium (2) |
//! | `HistorySuggestion` | Suggestion based on history | Low (3) |
//!
//! # Example
//!
//! ```rust,ignore
//! use clai_wrap::suggestion_receiver::{SuggestionReceiver, SuggestionType};
//! use clai_wrap::daemon_client::DaemonClient;
//!
//! // Create receiver with daemon client
//! let client = DaemonClient::connect_default()?;
//! let mut receiver = SuggestionReceiver::new();
//!
//! // Poll for suggestions
//! if let Some(notification) = client.poll_notifications()? {
//!     receiver.handle_notification(&notification);
//! }
//!
//! // Get suggestions for a command
//! let suggestions = receiver.suggestions_for_command("cmd-123");
//! ```
//!
//! # Thread Safety
//!
//! The `SuggestionReceiver` is designed for single-threaded use. For concurrent
//! access from multiple threads, wrap it in appropriate synchronization primitives
//! (e.g., `Mutex` or `RwLock`).

use std::collections::VecDeque;

use serde::{Deserialize, Serialize};
use tracing::{debug, trace, warn};

use crate::jsonrpc::Notification;
use crate::picker::PickerItem;

/// Maximum number of suggestions to keep in the queue per command.
pub const MAX_SUGGESTIONS_PER_COMMAND: usize = 10;

/// Maximum total number of suggestions to keep in the queue.
pub const MAX_TOTAL_SUGGESTIONS: usize = 100;

/// The type of suggestion received from the daemon.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SuggestionType {
    /// Fix for a failed command (e.g., typo correction).
    #[default]
    CommandFix,
    /// Completion for a partially typed command.
    CommandCompletion,
    /// Explanation of command output or error.
    CommandExplanation,
    /// Suggestion based on command history.
    HistorySuggestion,
}

impl SuggestionType {
    /// Returns the priority of this suggestion type (lower is higher priority).
    #[must_use]
    pub const fn priority(&self) -> u8 {
        match self {
            Self::CommandFix => 0,
            Self::CommandCompletion => 1,
            Self::CommandExplanation => 2,
            Self::HistorySuggestion => 3,
        }
    }

    /// Returns a human-readable label for this suggestion type.
    #[must_use]
    pub const fn label(&self) -> &'static str {
        match self {
            Self::CommandFix => "fix",
            Self::CommandCompletion => "completion",
            Self::CommandExplanation => "explanation",
            Self::HistorySuggestion => "history",
        }
    }
}


impl std::fmt::Display for SuggestionType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.label())
    }
}

/// A single suggestion received from the daemon.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Suggestion {
    /// The command ID this suggestion is associated with.
    pub command_id: String,
    /// The suggestion text (typically a command to run).
    pub text: String,
    /// The type of suggestion.
    #[serde(default)]
    pub suggestion_type: SuggestionType,
    /// Optional explanation of why this suggestion is being made.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub explanation: Option<String>,
    /// Confidence score (0.0 to 1.0, where 1.0 is highest confidence).
    #[serde(default = "default_confidence")]
    pub confidence: f32,
}

const fn default_confidence() -> f32 {
    1.0
}

impl Suggestion {
    /// Creates a new suggestion with the given command ID and text.
    #[must_use]
    pub fn new(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            suggestion_type: SuggestionType::default(),
            explanation: None,
            confidence: 1.0,
        }
    }

    /// Creates a command fix suggestion.
    #[must_use]
    pub fn command_fix(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            suggestion_type: SuggestionType::CommandFix,
            explanation: None,
            confidence: 1.0,
        }
    }

    /// Creates a command completion suggestion.
    #[must_use]
    pub fn command_completion(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            suggestion_type: SuggestionType::CommandCompletion,
            explanation: None,
            confidence: 1.0,
        }
    }

    /// Creates an explanation suggestion.
    #[must_use]
    pub fn explanation(
        command_id: impl Into<String>,
        text: impl Into<String>,
        explanation: impl Into<String>,
    ) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            suggestion_type: SuggestionType::CommandExplanation,
            explanation: Some(explanation.into()),
            confidence: 1.0,
        }
    }

    /// Sets the suggestion type.
    #[must_use]
    pub const fn with_type(mut self, suggestion_type: SuggestionType) -> Self {
        self.suggestion_type = suggestion_type;
        self
    }

    /// Sets the explanation.
    #[must_use]
    pub fn with_explanation(mut self, explanation: impl Into<String>) -> Self {
        self.explanation = Some(explanation.into());
        self
    }

    /// Sets the confidence score.
    #[must_use]
    pub const fn with_confidence(mut self, confidence: f32) -> Self {
        self.confidence = confidence.clamp(0.0, 1.0);
        self
    }

    /// Returns the priority for sorting (combines type priority and confidence).
    ///
    /// Lower values are higher priority.
    #[must_use]
    pub fn sort_priority(&self) -> (u8, i32) {
        // Primary sort by type priority, secondary by inverse confidence
        // (multiply by 100 to preserve precision when converting to int)
        #[allow(clippy::cast_possible_truncation)]
        let confidence_inverse = ((1.0 - self.confidence) * 100.0) as i32;
        (self.suggestion_type.priority(), confidence_inverse)
    }

    /// Converts this suggestion to a `PickerItem` for display.
    #[must_use]
    pub fn to_picker_item(&self) -> PickerItem {
        let metadata = self.explanation.as_ref().map_or_else(
            || format!("[{}]", self.suggestion_type.label()),
            |exp| format!("[{}] {exp}", self.suggestion_type.label()),
        );
        PickerItem::with_metadata(&self.text, metadata)
    }
}

impl PartialOrd for Suggestion {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.sort_priority().cmp(&other.sort_priority()))
    }
}

/// Receiver for daemon suggestions.
///
/// This struct manages a queue of suggestions received from the daemon and provides
/// methods to access and filter them. Suggestions are automatically sorted by priority.
#[derive(Debug, Default)]
pub struct SuggestionReceiver {
    /// Queue of received suggestions (sorted by priority).
    suggestions: VecDeque<Suggestion>,
    /// Whether new suggestions are available since last check.
    has_new_suggestions: bool,
    /// The most recently added command ID (for quick access).
    last_command_id: Option<String>,
}

impl SuggestionReceiver {
    /// Creates a new, empty suggestion receiver.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Handles a JSON-RPC notification from the daemon.
    ///
    /// If the notification is a `suggestion.available` notification, the suggestion
    /// is parsed and added to the queue.
    ///
    /// # Returns
    ///
    /// Returns `true` if the notification was handled (was a suggestion notification),
    /// `false` otherwise.
    pub fn handle_notification(&mut self, notification: &Notification) -> bool {
        if notification.method != "suggestion.available" {
            trace!(
                "Ignoring non-suggestion notification: {}",
                notification.method
            );
            return false;
        }

        match self.parse_suggestion_notification(notification) {
            Ok(suggestion) => {
                self.add_suggestion(suggestion);
                true
            }
            Err(e) => {
                warn!("Failed to parse suggestion notification: {e}");
                false
            }
        }
    }

    /// Parses a suggestion from a notification's params.
    #[allow(clippy::unused_self)]
    fn parse_suggestion_notification(
        &self,
        notification: &Notification,
    ) -> Result<Suggestion, ParseError> {
        let params = &notification.params;

        // Extract required fields
        let command_id = params
            .get("command_id")
            .and_then(|v| v.as_str())
            .ok_or(ParseError::MissingField("command_id"))?
            .to_string();

        let text = params
            .get("suggestion")
            .and_then(|v| v.as_str())
            .ok_or(ParseError::MissingField("suggestion"))?
            .to_string();

        // Extract optional fields
        let suggestion_type = params
            .get("type")
            .and_then(|v| serde_json::from_value(v.clone()).ok())
            .unwrap_or_default();

        let explanation = params
            .get("explanation")
            .and_then(|v| v.as_str())
            .map(String::from);

        #[allow(clippy::cast_possible_truncation)]
        let confidence = params
            .get("confidence")
            .and_then(serde_json::Value::as_f64)
            .map_or(1.0, |c| c as f32);

        Ok(Suggestion {
            command_id,
            text,
            suggestion_type,
            explanation,
            confidence,
        })
    }

    /// Adds a suggestion to the queue.
    ///
    /// Suggestions are automatically sorted by priority. If the queue exceeds
    /// the maximum size, the lowest-priority suggestions are removed.
    pub fn add_suggestion(&mut self, suggestion: Suggestion) {
        debug!(
            "Adding suggestion for command {}: {} (type={}, confidence={})",
            suggestion.command_id,
            suggestion.text,
            suggestion.suggestion_type,
            suggestion.confidence
        );

        self.last_command_id = Some(suggestion.command_id.clone());
        self.has_new_suggestions = true;

        // Add to queue
        self.suggestions.push_back(suggestion);

        // Sort by priority
        self.sort_suggestions();

        // Enforce maximum size
        while self.suggestions.len() > MAX_TOTAL_SUGGESTIONS {
            self.suggestions.pop_back();
        }

        // Enforce per-command limit
        self.enforce_per_command_limit();
    }

    /// Sorts suggestions by priority (highest priority first).
    fn sort_suggestions(&mut self) {
        // Convert to Vec for sorting, then back to VecDeque
        let mut vec: Vec<_> = self.suggestions.drain(..).collect();
        vec.sort_by_key(Suggestion::sort_priority);
        self.suggestions = vec.into();
    }

    /// Enforces the per-command suggestion limit.
    fn enforce_per_command_limit(&mut self) {
        use std::collections::HashMap;

        // Count suggestions per command
        let mut counts: HashMap<&str, usize> = HashMap::new();
        for suggestion in &self.suggestions {
            *counts.entry(&suggestion.command_id).or_insert(0) += 1;
        }

        // Find commands that exceed the limit
        let over_limit: Vec<_> = counts
            .iter()
            .filter(|(_, &count)| count > MAX_SUGGESTIONS_PER_COMMAND)
            .map(|(&cmd, _)| cmd.to_string())
            .collect();

        // Remove excess suggestions (lowest priority) for each over-limit command
        for cmd_id in over_limit {
            let mut count = 0;
            self.suggestions.retain(|s| {
                if s.command_id == cmd_id {
                    count += 1;
                    count <= MAX_SUGGESTIONS_PER_COMMAND
                } else {
                    true
                }
            });
        }
    }

    /// Returns all suggestions for a specific command.
    #[must_use]
    pub fn suggestions_for_command(&self, command_id: &str) -> Vec<&Suggestion> {
        self.suggestions
            .iter()
            .filter(|s| s.command_id == command_id)
            .collect()
    }

    /// Returns the first (highest priority) suggestion for a command.
    #[must_use]
    pub fn first_suggestion_for_command(&self, command_id: &str) -> Option<&Suggestion> {
        self.suggestions.iter().find(|s| s.command_id == command_id)
    }

    /// Returns all suggestions.
    #[must_use]
    pub fn all_suggestions(&self) -> Vec<&Suggestion> {
        self.suggestions.iter().collect()
    }

    /// Returns the number of suggestions in the queue.
    #[must_use]
    pub fn len(&self) -> usize {
        self.suggestions.len()
    }

    /// Returns true if the queue is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.suggestions.is_empty()
    }

    /// Returns true if new suggestions are available since last check.
    ///
    /// This flag is reset after calling this method.
    pub const fn has_new_suggestions(&mut self) -> bool {
        let result = self.has_new_suggestions;
        self.has_new_suggestions = false;
        result
    }

    /// Returns the command ID of the most recently added suggestion.
    #[must_use]
    pub fn last_command_id(&self) -> Option<&str> {
        self.last_command_id.as_deref()
    }

    /// Removes all suggestions for a specific command.
    ///
    /// Returns the number of suggestions removed.
    pub fn remove_suggestions_for_command(&mut self, command_id: &str) -> usize {
        let initial_len = self.suggestions.len();
        self.suggestions.retain(|s| s.command_id != command_id);
        initial_len - self.suggestions.len()
    }

    /// Clears all suggestions.
    pub fn clear(&mut self) {
        self.suggestions.clear();
        self.has_new_suggestions = false;
        self.last_command_id = None;
    }

    /// Converts all suggestions for a command to picker items.
    #[must_use]
    pub fn to_picker_items_for_command(&self, command_id: &str) -> Vec<PickerItem> {
        self.suggestions_for_command(command_id)
            .into_iter()
            .map(Suggestion::to_picker_item)
            .collect()
    }

    /// Converts all suggestions to picker items.
    #[must_use]
    pub fn to_picker_items(&self) -> Vec<PickerItem> {
        self.suggestions
            .iter()
            .map(Suggestion::to_picker_item)
            .collect()
    }

    /// Returns suggestions filtered by type.
    #[must_use]
    pub fn suggestions_by_type(&self, suggestion_type: SuggestionType) -> Vec<&Suggestion> {
        self.suggestions
            .iter()
            .filter(|s| s.suggestion_type == suggestion_type)
            .collect()
    }

    /// Returns the number of suggestions for a specific command.
    #[must_use]
    pub fn count_for_command(&self, command_id: &str) -> usize {
        self.suggestions
            .iter()
            .filter(|s| s.command_id == command_id)
            .count()
    }
}

/// Errors that can occur when parsing suggestion notifications.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ParseError {
    /// A required field is missing from the notification.
    MissingField(&'static str),
    /// A field has an invalid value.
    InvalidField(&'static str, String),
}

impl std::fmt::Display for ParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::MissingField(field) => write!(f, "missing required field: {field}"),
            Self::InvalidField(field, reason) => write!(f, "invalid field '{field}': {reason}"),
        }
    }
}

impl std::error::Error for ParseError {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::jsonrpc::JSONRPC_VERSION;

    // =========================================================================
    // SuggestionType Tests
    // =========================================================================

    #[test]
    fn test_suggestion_type_priority() {
        assert_eq!(SuggestionType::CommandFix.priority(), 0);
        assert_eq!(SuggestionType::CommandCompletion.priority(), 1);
        assert_eq!(SuggestionType::CommandExplanation.priority(), 2);
        assert_eq!(SuggestionType::HistorySuggestion.priority(), 3);

        // CommandFix should have highest priority (lowest value)
        assert!(
            SuggestionType::CommandFix.priority() < SuggestionType::CommandCompletion.priority()
        );
        assert!(
            SuggestionType::CommandCompletion.priority()
                < SuggestionType::CommandExplanation.priority()
        );
        assert!(
            SuggestionType::CommandExplanation.priority()
                < SuggestionType::HistorySuggestion.priority()
        );
    }

    #[test]
    fn test_suggestion_type_label() {
        assert_eq!(SuggestionType::CommandFix.label(), "fix");
        assert_eq!(SuggestionType::CommandCompletion.label(), "completion");
        assert_eq!(SuggestionType::CommandExplanation.label(), "explanation");
        assert_eq!(SuggestionType::HistorySuggestion.label(), "history");
    }

    #[test]
    fn test_suggestion_type_display() {
        assert_eq!(SuggestionType::CommandFix.to_string(), "fix");
        assert_eq!(SuggestionType::CommandCompletion.to_string(), "completion");
    }

    #[test]
    fn test_suggestion_type_default() {
        assert_eq!(SuggestionType::default(), SuggestionType::CommandFix);
    }

    #[test]
    fn test_suggestion_type_serialization() {
        let json = serde_json::to_string(&SuggestionType::CommandFix).unwrap();
        assert_eq!(json, r#""command_fix""#);

        let json = serde_json::to_string(&SuggestionType::CommandCompletion).unwrap();
        assert_eq!(json, r#""command_completion""#);
    }

    #[test]
    fn test_suggestion_type_deserialization() {
        let suggestion_type: SuggestionType = serde_json::from_str(r#""command_fix""#).unwrap();
        assert_eq!(suggestion_type, SuggestionType::CommandFix);

        let suggestion_type: SuggestionType =
            serde_json::from_str(r#""history_suggestion""#).unwrap();
        assert_eq!(suggestion_type, SuggestionType::HistorySuggestion);
    }

    // =========================================================================
    // Suggestion Tests
    // =========================================================================

    #[test]
    fn test_suggestion_new() {
        let suggestion = Suggestion::new("cmd-123", "git push");

        assert_eq!(suggestion.command_id, "cmd-123");
        assert_eq!(suggestion.text, "git push");
        assert_eq!(suggestion.suggestion_type, SuggestionType::CommandFix);
        assert!(suggestion.explanation.is_none());
        assert!((suggestion.confidence - 1.0).abs() < f32::EPSILON);
    }

    #[test]
    fn test_suggestion_command_fix() {
        let suggestion = Suggestion::command_fix("cmd-1", "git push");
        assert_eq!(suggestion.suggestion_type, SuggestionType::CommandFix);
    }

    #[test]
    fn test_suggestion_command_completion() {
        let suggestion = Suggestion::command_completion("cmd-2", "git status");
        assert_eq!(
            suggestion.suggestion_type,
            SuggestionType::CommandCompletion
        );
    }

    #[test]
    fn test_suggestion_explanation() {
        let suggestion = Suggestion::explanation("cmd-3", "ls -la", "Lists all files");

        assert_eq!(
            suggestion.suggestion_type,
            SuggestionType::CommandExplanation
        );
        assert_eq!(suggestion.explanation, Some("Lists all files".to_string()));
    }

    #[test]
    fn test_suggestion_with_type() {
        let suggestion =
            Suggestion::new("cmd", "text").with_type(SuggestionType::HistorySuggestion);

        assert_eq!(
            suggestion.suggestion_type,
            SuggestionType::HistorySuggestion
        );
    }

    #[test]
    fn test_suggestion_with_explanation() {
        let suggestion = Suggestion::new("cmd", "text").with_explanation("This is an explanation");

        assert_eq!(
            suggestion.explanation,
            Some("This is an explanation".to_string())
        );
    }

    #[test]
    fn test_suggestion_with_confidence() {
        let suggestion = Suggestion::new("cmd", "text").with_confidence(0.75);
        assert!((suggestion.confidence - 0.75).abs() < f32::EPSILON);
    }

    #[test]
    fn test_suggestion_with_confidence_clamped() {
        let suggestion = Suggestion::new("cmd", "text").with_confidence(1.5);
        assert!((suggestion.confidence - 1.0).abs() < f32::EPSILON);

        let suggestion = Suggestion::new("cmd", "text").with_confidence(-0.5);
        assert!(suggestion.confidence.abs() < f32::EPSILON);
    }

    #[test]
    fn test_suggestion_sort_priority() {
        let fix = Suggestion::command_fix("cmd", "fix");
        let completion = Suggestion::command_completion("cmd", "completion");
        let explanation = Suggestion::explanation("cmd", "exp", "explain");
        let history =
            Suggestion::new("cmd", "history").with_type(SuggestionType::HistorySuggestion);

        assert!(fix.sort_priority() < completion.sort_priority());
        assert!(completion.sort_priority() < explanation.sort_priority());
        assert!(explanation.sort_priority() < history.sort_priority());
    }

    #[test]
    fn test_suggestion_sort_priority_with_confidence() {
        let high_conf = Suggestion::command_fix("cmd", "high").with_confidence(1.0);
        let low_conf = Suggestion::command_fix("cmd", "low").with_confidence(0.5);

        // Higher confidence should have lower (better) priority
        assert!(high_conf.sort_priority() < low_conf.sort_priority());
    }

    #[test]
    fn test_suggestion_ordering() {
        let fix = Suggestion::command_fix("cmd", "fix");
        let completion = Suggestion::command_completion("cmd", "completion");

        assert!(fix < completion);

        let mut suggestions = vec![completion.clone(), fix.clone()];
        suggestions.sort_by(|a, b| a.sort_priority().cmp(&b.sort_priority()));
        assert_eq!(suggestions[0].text, "fix");
        assert_eq!(suggestions[1].text, "completion");
    }

    #[test]
    fn test_suggestion_to_picker_item() {
        let suggestion = Suggestion::new("cmd", "git push");
        let item = suggestion.to_picker_item();

        assert_eq!(item.text, "git push");
        assert!(item.metadata.as_ref().unwrap().contains("[fix]"));
    }

    #[test]
    fn test_suggestion_to_picker_item_with_explanation() {
        let suggestion = Suggestion::explanation("cmd", "ls -la", "Lists files");
        let item = suggestion.to_picker_item();

        assert_eq!(item.text, "ls -la");
        let metadata = item.metadata.unwrap();
        assert!(metadata.contains("[explanation]"));
        assert!(metadata.contains("Lists files"));
    }

    #[test]
    fn test_suggestion_serialization() {
        let suggestion = Suggestion::new("cmd-123", "git push")
            .with_type(SuggestionType::CommandFix)
            .with_explanation("Push changes")
            .with_confidence(0.9);

        let json = serde_json::to_string(&suggestion).unwrap();
        let deserialized: Suggestion = serde_json::from_str(&json).unwrap();

        assert_eq!(suggestion, deserialized);
    }

    #[test]
    fn test_suggestion_deserialization_minimal() {
        let json = r#"{"command_id":"cmd-1","text":"git status"}"#;
        let suggestion: Suggestion = serde_json::from_str(json).unwrap();

        assert_eq!(suggestion.command_id, "cmd-1");
        assert_eq!(suggestion.text, "git status");
        assert_eq!(suggestion.suggestion_type, SuggestionType::CommandFix);
        assert!(suggestion.explanation.is_none());
        assert!((suggestion.confidence - 1.0).abs() < f32::EPSILON);
    }

    // =========================================================================
    // SuggestionReceiver Tests
    // =========================================================================

    fn create_test_notification(command_id: &str, suggestion: &str) -> Notification {
        Notification {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: "suggestion.available".to_string(),
            params: serde_json::json!({
                "command_id": command_id,
                "suggestion": suggestion
            }),
        }
    }

    fn create_full_notification(
        command_id: &str,
        suggestion: &str,
        suggestion_type: &str,
        explanation: Option<&str>,
        confidence: Option<f64>,
    ) -> Notification {
        let mut params = serde_json::json!({
            "command_id": command_id,
            "suggestion": suggestion,
            "type": suggestion_type
        });

        if let Some(exp) = explanation {
            params["explanation"] = serde_json::Value::String(exp.to_string());
        }
        if let Some(conf) = confidence {
            params["confidence"] = serde_json::Value::from(conf);
        }

        Notification {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: "suggestion.available".to_string(),
            params,
        }
    }

    #[test]
    fn test_receiver_new() {
        let receiver = SuggestionReceiver::new();

        assert!(receiver.is_empty());
        assert_eq!(receiver.len(), 0);
        assert!(receiver.last_command_id().is_none());
    }

    #[test]
    fn test_receiver_handle_notification() {
        let mut receiver = SuggestionReceiver::new();
        let notification = create_test_notification("cmd-1", "git push");

        let handled = receiver.handle_notification(&notification);

        assert!(handled);
        assert_eq!(receiver.len(), 1);
        assert_eq!(receiver.last_command_id(), Some("cmd-1"));
    }

    #[test]
    fn test_receiver_handle_wrong_notification() {
        let mut receiver = SuggestionReceiver::new();
        let notification = Notification {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: "other.method".to_string(),
            params: serde_json::json!({}),
        };

        let handled = receiver.handle_notification(&notification);

        assert!(!handled);
        assert!(receiver.is_empty());
    }

    #[test]
    fn test_receiver_handle_invalid_notification() {
        let mut receiver = SuggestionReceiver::new();

        // Missing command_id
        let notification = Notification {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: "suggestion.available".to_string(),
            params: serde_json::json!({
                "suggestion": "git push"
            }),
        };

        let handled = receiver.handle_notification(&notification);
        assert!(!handled);
        assert!(receiver.is_empty());

        // Missing suggestion
        let notification = Notification {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: "suggestion.available".to_string(),
            params: serde_json::json!({
                "command_id": "cmd-1"
            }),
        };

        let handled = receiver.handle_notification(&notification);
        assert!(!handled);
        assert!(receiver.is_empty());
    }

    #[test]
    fn test_receiver_handle_full_notification() {
        let mut receiver = SuggestionReceiver::new();
        let notification = create_full_notification(
            "cmd-1",
            "git push",
            "command_fix",
            Some("Push your changes"),
            Some(0.95),
        );

        let handled = receiver.handle_notification(&notification);

        assert!(handled);
        assert_eq!(receiver.len(), 1);

        let suggestion = receiver.first_suggestion_for_command("cmd-1").unwrap();
        assert_eq!(suggestion.text, "git push");
        assert_eq!(suggestion.suggestion_type, SuggestionType::CommandFix);
        assert_eq!(
            suggestion.explanation,
            Some("Push your changes".to_string())
        );
        assert!((suggestion.confidence - 0.95).abs() < f32::EPSILON);
    }

    #[test]
    fn test_receiver_add_suggestion() {
        let mut receiver = SuggestionReceiver::new();
        let suggestion = Suggestion::new("cmd-1", "git push");

        receiver.add_suggestion(suggestion);

        assert_eq!(receiver.len(), 1);
        assert_eq!(receiver.last_command_id(), Some("cmd-1"));
    }

    #[test]
    fn test_receiver_suggestions_for_command() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(Suggestion::new("cmd-1", "suggestion 1"));
        receiver.add_suggestion(Suggestion::new("cmd-2", "suggestion 2"));
        receiver.add_suggestion(Suggestion::new("cmd-1", "suggestion 3"));

        let cmd1_suggestions = receiver.suggestions_for_command("cmd-1");
        assert_eq!(cmd1_suggestions.len(), 2);

        let cmd2_suggestions = receiver.suggestions_for_command("cmd-2");
        assert_eq!(cmd2_suggestions.len(), 1);

        let cmd3_suggestions = receiver.suggestions_for_command("cmd-3");
        assert!(cmd3_suggestions.is_empty());
    }

    #[test]
    fn test_receiver_first_suggestion_for_command() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(
            Suggestion::new("cmd-1", "low priority").with_type(SuggestionType::HistorySuggestion),
        );
        receiver.add_suggestion(
            Suggestion::new("cmd-1", "high priority").with_type(SuggestionType::CommandFix),
        );

        let first = receiver.first_suggestion_for_command("cmd-1").unwrap();
        assert_eq!(first.text, "high priority");
    }

    #[test]
    fn test_receiver_has_new_suggestions() {
        let mut receiver = SuggestionReceiver::new();

        assert!(!receiver.has_new_suggestions());

        receiver.add_suggestion(Suggestion::new("cmd-1", "test"));
        assert!(receiver.has_new_suggestions());

        // Flag should be reset after checking
        assert!(!receiver.has_new_suggestions());
    }

    #[test]
    fn test_receiver_remove_suggestions_for_command() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(Suggestion::new("cmd-1", "s1"));
        receiver.add_suggestion(Suggestion::new("cmd-2", "s2"));
        receiver.add_suggestion(Suggestion::new("cmd-1", "s3"));

        assert_eq!(receiver.len(), 3);

        let removed = receiver.remove_suggestions_for_command("cmd-1");

        assert_eq!(removed, 2);
        assert_eq!(receiver.len(), 1);
        assert!(receiver.suggestions_for_command("cmd-1").is_empty());
        assert_eq!(receiver.suggestions_for_command("cmd-2").len(), 1);
    }

    #[test]
    fn test_receiver_clear() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(Suggestion::new("cmd-1", "s1"));
        receiver.add_suggestion(Suggestion::new("cmd-2", "s2"));

        assert_eq!(receiver.len(), 2);

        receiver.clear();

        assert!(receiver.is_empty());
        assert!(receiver.last_command_id().is_none());
        assert!(!receiver.has_new_suggestions());
    }

    #[test]
    fn test_receiver_sorting() {
        let mut receiver = SuggestionReceiver::new();

        // Add in reverse priority order
        receiver.add_suggestion(
            Suggestion::new("cmd-1", "history").with_type(SuggestionType::HistorySuggestion),
        );
        receiver.add_suggestion(
            Suggestion::new("cmd-1", "explanation").with_type(SuggestionType::CommandExplanation),
        );
        receiver.add_suggestion(
            Suggestion::new("cmd-1", "completion").with_type(SuggestionType::CommandCompletion),
        );
        receiver
            .add_suggestion(Suggestion::new("cmd-1", "fix").with_type(SuggestionType::CommandFix));

        let all = receiver.all_suggestions();
        assert_eq!(all[0].text, "fix");
        assert_eq!(all[1].text, "completion");
        assert_eq!(all[2].text, "explanation");
        assert_eq!(all[3].text, "history");
    }

    #[test]
    fn test_receiver_per_command_limit() {
        let mut receiver = SuggestionReceiver::new();

        // Add more than MAX_SUGGESTIONS_PER_COMMAND for one command
        for i in 0..(MAX_SUGGESTIONS_PER_COMMAND + 5) {
            receiver.add_suggestion(Suggestion::new("cmd-1", format!("suggestion {i}")));
        }

        assert_eq!(
            receiver.count_for_command("cmd-1"),
            MAX_SUGGESTIONS_PER_COMMAND
        );
    }

    #[test]
    fn test_receiver_total_limit() {
        let mut receiver = SuggestionReceiver::new();

        // Add more than MAX_TOTAL_SUGGESTIONS
        for i in 0..(MAX_TOTAL_SUGGESTIONS + 10) {
            receiver.add_suggestion(Suggestion::new(
                format!("cmd-{i}"),
                format!("suggestion {i}"),
            ));
        }

        assert!(receiver.len() <= MAX_TOTAL_SUGGESTIONS);
    }

    #[test]
    fn test_receiver_to_picker_items() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(Suggestion::new("cmd-1", "git status"));
        receiver.add_suggestion(Suggestion::new("cmd-2", "git push"));

        let items = receiver.to_picker_items();
        assert_eq!(items.len(), 2);
        assert!(items.iter().any(|i| i.text == "git status"));
        assert!(items.iter().any(|i| i.text == "git push"));
    }

    #[test]
    fn test_receiver_to_picker_items_for_command() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(Suggestion::new("cmd-1", "git status"));
        receiver.add_suggestion(Suggestion::new("cmd-2", "git push"));
        receiver.add_suggestion(Suggestion::new("cmd-1", "git diff"));

        let items = receiver.to_picker_items_for_command("cmd-1");
        assert_eq!(items.len(), 2);
        assert!(items
            .iter()
            .all(|i| i.text == "git status" || i.text == "git diff"));
    }

    #[test]
    fn test_receiver_suggestions_by_type() {
        let mut receiver = SuggestionReceiver::new();

        receiver
            .add_suggestion(Suggestion::new("cmd-1", "fix1").with_type(SuggestionType::CommandFix));
        receiver
            .add_suggestion(Suggestion::new("cmd-2", "fix2").with_type(SuggestionType::CommandFix));
        receiver.add_suggestion(
            Suggestion::new("cmd-3", "completion").with_type(SuggestionType::CommandCompletion),
        );

        let fixes = receiver.suggestions_by_type(SuggestionType::CommandFix);
        assert_eq!(fixes.len(), 2);

        let completions = receiver.suggestions_by_type(SuggestionType::CommandCompletion);
        assert_eq!(completions.len(), 1);

        let explanations = receiver.suggestions_by_type(SuggestionType::CommandExplanation);
        assert!(explanations.is_empty());
    }

    #[test]
    fn test_receiver_count_for_command() {
        let mut receiver = SuggestionReceiver::new();

        receiver.add_suggestion(Suggestion::new("cmd-1", "s1"));
        receiver.add_suggestion(Suggestion::new("cmd-1", "s2"));
        receiver.add_suggestion(Suggestion::new("cmd-2", "s3"));

        assert_eq!(receiver.count_for_command("cmd-1"), 2);
        assert_eq!(receiver.count_for_command("cmd-2"), 1);
        assert_eq!(receiver.count_for_command("cmd-3"), 0);
    }

    // =========================================================================
    // ParseError Tests
    // =========================================================================

    #[test]
    fn test_parse_error_display() {
        let err = ParseError::MissingField("command_id");
        assert_eq!(err.to_string(), "missing required field: command_id");

        let err = ParseError::InvalidField("type", "unknown value".to_string());
        assert_eq!(err.to_string(), "invalid field 'type': unknown value");
    }

    #[test]
    fn test_parse_error_equality() {
        assert_eq!(
            ParseError::MissingField("foo"),
            ParseError::MissingField("foo")
        );
        assert_ne!(
            ParseError::MissingField("foo"),
            ParseError::MissingField("bar")
        );
    }

    // =========================================================================
    // Integration Tests
    // =========================================================================

    #[test]
    fn test_full_workflow() {
        let mut receiver = SuggestionReceiver::new();

        // Receive multiple notifications
        let notifications = vec![
            create_full_notification(
                "cmd-1",
                "git push",
                "command_fix",
                Some("Push changes"),
                Some(0.9),
            ),
            create_full_notification("cmd-1", "git pull", "command_completion", None, Some(0.7)),
            create_full_notification("cmd-2", "ls -la", "history_suggestion", None, Some(0.8)),
        ];

        for notification in &notifications {
            receiver.handle_notification(notification);
        }

        assert_eq!(receiver.len(), 3);
        assert!(receiver.has_new_suggestions());

        // Check suggestions are sorted by priority
        let cmd1_suggestions = receiver.suggestions_for_command("cmd-1");
        assert_eq!(cmd1_suggestions.len(), 2);
        assert_eq!(cmd1_suggestions[0].text, "git push"); // fix > completion

        // Convert to picker items
        let items = receiver.to_picker_items();
        assert_eq!(items.len(), 3);

        // Remove command 1 suggestions
        let removed = receiver.remove_suggestions_for_command("cmd-1");
        assert_eq!(removed, 2);
        assert_eq!(receiver.len(), 1);

        // Clear all
        receiver.clear();
        assert!(receiver.is_empty());
    }

    #[test]
    fn test_concurrent_commands() {
        let mut receiver = SuggestionReceiver::new();

        // Simulate suggestions arriving for multiple concurrent commands
        for i in 0..5 {
            for j in 0..3 {
                receiver.add_suggestion(Suggestion::new(
                    format!("cmd-{i}"),
                    format!("suggestion-{i}-{j}"),
                ));
            }
        }

        assert_eq!(receiver.len(), 15);

        for i in 0..5 {
            assert_eq!(receiver.count_for_command(&format!("cmd-{i}")), 3);
        }
    }
}
