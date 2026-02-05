//! Assistant Comment Rendering for clai-wrap.
//!
//! This module displays inline comments from the AI assistant alongside command output.
//! It supports different comment types with appropriate styling and can render comments
//! in both terminal output (shell comment syntax) and picker UI (ratatui widgets).
//!
//! # Comment Types
//!
//! | Type | Description | Style |
//! |------|-------------|-------|
//! | `Explanation` | Explains command output or errors | Blue, info icon |
//! | `Warning` | Warns about potential issues | Yellow, warning icon |
//! | `Suggestion` | Suggests a fix or alternative | Green, suggestion icon |
//! | `Error` | Indicates an error condition | Red, error icon |
//!
//! # Shell Comment Syntax
//!
//! The module outputs comments using shell-appropriate syntax:
//!
//! | Shell | Comment Prefix |
//! |-------|---------------|
//! | bash/zsh/fish | `#` |
//! | PowerShell | `#` |
//! | cmd.exe | `REM ` |
//! | Other/unknown | `#` (fallback) |
//!
//! # Example
//!
//! ```rust
//! use clai_wrap::assistant_comment::{AssistantComment, CommentType, CommentRenderer, Shell};
//!
//! // Create a comment
//! let comment = AssistantComment::suggestion("cmd-123", "git push")
//!     .with_explanation("Push your changes to the remote repository");
//!
//! // Render as shell comment
//! let renderer = CommentRenderer::new(Shell::Bash);
//! let output = renderer.render_shell_comment(&comment);
//! assert!(output.starts_with("# clai suggestion:"));
//! ```
//!
//! # UI Rendering
//!
//! Comments can also be rendered as ratatui widgets for display in the picker UI
//! with colors, borders, and icons appropriate to the comment type.

use ratatui::{
    layout::{Alignment, Constraint, Direction, Layout, Rect},
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, Borders, Paragraph, Wrap},
    Frame,
};

use crate::color_detect::ColorSupport;
use crate::suggestion_receiver::{Suggestion, SuggestionType};

/// The type of assistant comment.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum CommentType {
    /// Explains command output or errors.
    Explanation,
    /// Warns about potential issues.
    Warning,
    /// Suggests a fix or alternative command.
    #[default]
    Suggestion,
    /// Indicates an error condition.
    Error,
}

impl CommentType {
    /// Returns a human-readable label for this comment type.
    #[must_use]
    pub const fn label(&self) -> &'static str {
        match self {
            Self::Explanation => "explanation",
            Self::Warning => "warning",
            Self::Suggestion => "suggestion",
            Self::Error => "error",
        }
    }

    /// Returns an icon/prefix character for this comment type.
    ///
    /// Uses ASCII characters for maximum terminal compatibility.
    #[must_use]
    pub const fn icon(&self) -> &'static str {
        match self {
            Self::Explanation => "[i]",
            Self::Warning => "[!]",
            Self::Suggestion => "[>]",
            Self::Error => "[x]",
        }
    }

    /// Returns the primary color for this comment type.
    #[must_use]
    pub const fn color(&self) -> Color {
        match self {
            Self::Explanation => Color::Blue,
            Self::Warning => Color::Yellow,
            Self::Suggestion => Color::Green,
            Self::Error => Color::Red,
        }
    }

    /// Returns the foreground color for text on this comment's background.
    #[must_use]
    pub const fn text_color(&self) -> Color {
        Color::White
    }
}

impl std::fmt::Display for CommentType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.label())
    }
}

impl From<SuggestionType> for CommentType {
    fn from(st: SuggestionType) -> Self {
        match st {
            SuggestionType::CommandFix => Self::Suggestion,
            SuggestionType::CommandCompletion => Self::Suggestion,
            SuggestionType::CommandExplanation => Self::Explanation,
            SuggestionType::HistorySuggestion => Self::Suggestion,
        }
    }
}

/// The shell type for determining comment syntax.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum Shell {
    /// Bash shell (uses `#` comments).
    #[default]
    Bash,
    /// Zsh shell (uses `#` comments).
    Zsh,
    /// Fish shell (uses `#` comments).
    Fish,
    /// PowerShell (uses `#` comments).
    PowerShell,
    /// Windows cmd.exe (uses `REM ` comments).
    Cmd,
    /// Unknown shell (uses `#` as fallback).
    Unknown,
}

impl Shell {
    /// Returns the comment prefix for this shell.
    #[must_use]
    pub const fn comment_prefix(&self) -> &'static str {
        match self {
            Self::Cmd => "REM ",
            Self::Bash | Self::Zsh | Self::Fish | Self::PowerShell | Self::Unknown => "# ",
        }
    }

    /// Detects the shell type from a shell path or name.
    #[must_use]
    pub fn from_shell_path(path: &str) -> Self {
        let basename = path.rsplit(['/', '\\']).next().unwrap_or(path);
        // Convert to lowercase first to handle case variations like .EXE, .Exe
        let basename_lower = basename.to_lowercase();
        let shell_name = basename_lower
            .strip_suffix(".exe")
            .unwrap_or(&basename_lower);

        match shell_name {
            "bash" | "sh" => Self::Bash,
            "zsh" => Self::Zsh,
            "fish" => Self::Fish,
            "powershell" | "pwsh" => Self::PowerShell,
            "cmd" => Self::Cmd,
            _ => Self::Unknown,
        }
    }

    /// Detects the shell type from the SHELL environment variable.
    #[must_use]
    pub fn from_env() -> Self {
        std::env::var("SHELL")
            .map(|s| Self::from_shell_path(&s))
            .unwrap_or(Self::Unknown)
    }
}

impl std::fmt::Display for Shell {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let name = match self {
            Self::Bash => "bash",
            Self::Zsh => "zsh",
            Self::Fish => "fish",
            Self::PowerShell => "powershell",
            Self::Cmd => "cmd",
            Self::Unknown => "unknown",
        };
        write!(f, "{name}")
    }
}

/// An assistant comment to be displayed to the user.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AssistantComment {
    /// The command ID this comment is associated with.
    pub command_id: String,
    /// The primary text content of the comment (e.g., suggested command).
    pub text: String,
    /// The type of comment.
    pub comment_type: CommentType,
    /// Optional detailed explanation.
    pub explanation: Option<String>,
}

impl AssistantComment {
    /// Creates a new assistant comment.
    #[must_use]
    pub fn new(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            comment_type: CommentType::default(),
            explanation: None,
        }
    }

    /// Creates a suggestion comment.
    #[must_use]
    pub fn suggestion(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            comment_type: CommentType::Suggestion,
            explanation: None,
        }
    }

    /// Creates an explanation comment.
    #[must_use]
    pub fn explanation(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            comment_type: CommentType::Explanation,
            explanation: None,
        }
    }

    /// Creates a warning comment.
    #[must_use]
    pub fn warning(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            comment_type: CommentType::Warning,
            explanation: None,
        }
    }

    /// Creates an error comment.
    #[must_use]
    pub fn error(command_id: impl Into<String>, text: impl Into<String>) -> Self {
        Self {
            command_id: command_id.into(),
            text: text.into(),
            comment_type: CommentType::Error,
            explanation: None,
        }
    }

    /// Sets the comment type.
    #[must_use]
    pub const fn with_type(mut self, comment_type: CommentType) -> Self {
        self.comment_type = comment_type;
        self
    }

    /// Sets the explanation.
    #[must_use]
    pub fn with_explanation(mut self, explanation: impl Into<String>) -> Self {
        self.explanation = Some(explanation.into());
        self
    }

    /// Returns the primary color for this comment.
    #[must_use]
    pub const fn color(&self) -> Color {
        self.comment_type.color()
    }

    /// Returns the icon for this comment.
    #[must_use]
    pub const fn icon(&self) -> &'static str {
        self.comment_type.icon()
    }
}

impl From<&Suggestion> for AssistantComment {
    fn from(suggestion: &Suggestion) -> Self {
        Self {
            command_id: suggestion.command_id.clone(),
            text: suggestion.text.clone(),
            comment_type: suggestion.suggestion_type.into(),
            explanation: suggestion.explanation.clone(),
        }
    }
}

impl From<Suggestion> for AssistantComment {
    fn from(suggestion: Suggestion) -> Self {
        Self {
            command_id: suggestion.command_id,
            text: suggestion.text,
            comment_type: suggestion.suggestion_type.into(),
            explanation: suggestion.explanation,
        }
    }
}

/// Renderer for assistant comments.
///
/// This struct provides methods for rendering comments as shell comments
/// (for terminal output) and as ratatui widgets (for picker UI).
#[derive(Debug, Clone)]
pub struct CommentRenderer {
    /// The shell type for comment syntax.
    shell: Shell,
    /// The color support level.
    color_support: ColorSupport,
    /// Prefix for clai comments.
    prefix: String,
}

impl CommentRenderer {
    /// Creates a new comment renderer for the given shell.
    #[must_use]
    pub fn new(shell: Shell) -> Self {
        Self {
            shell,
            color_support: ColorSupport::default(),
            prefix: "clai".to_string(),
        }
    }

    /// Creates a renderer with detected shell from environment.
    #[must_use]
    pub fn from_env() -> Self {
        Self::new(Shell::from_env())
    }

    /// Sets the color support level.
    #[must_use]
    pub const fn with_color_support(mut self, support: ColorSupport) -> Self {
        self.color_support = support;
        self
    }

    /// Sets a custom prefix for comments.
    #[must_use]
    pub fn with_prefix(mut self, prefix: impl Into<String>) -> Self {
        self.prefix = prefix.into();
        self
    }

    /// Returns the current shell.
    #[must_use]
    pub const fn shell(&self) -> Shell {
        self.shell
    }

    /// Returns the current color support.
    #[must_use]
    pub const fn color_support(&self) -> ColorSupport {
        self.color_support
    }

    /// Renders a comment as a shell comment string.
    ///
    /// The format is: `<comment_prefix> <prefix> <type>: <text>`
    ///
    /// For example: `# clai suggestion: git push`
    #[must_use]
    pub fn render_shell_comment(&self, comment: &AssistantComment) -> String {
        let prefix = self.shell.comment_prefix();
        let label = comment.comment_type.label();
        let mut output = format!("{prefix}{} {label}: {}", self.prefix, comment.text);

        if let Some(ref explanation) = comment.explanation {
            // Add explanation as a continuation comment
            output.push('\n');
            output.push_str(prefix);
            output.push_str("  ");
            output.push_str(explanation);
        }

        output
    }

    /// Renders a comment as bytes suitable for writing to a PTY.
    ///
    /// This includes a leading newline if `prepend_newline` is true.
    #[must_use]
    pub fn render_for_pty(&self, comment: &AssistantComment, prepend_newline: bool) -> Vec<u8> {
        let mut output = Vec::new();
        if prepend_newline {
            output.push(b'\n');
        }
        output.extend(self.render_shell_comment(comment).as_bytes());
        output.push(b'\n');
        output
    }

    /// Renders a comment as a ratatui widget.
    ///
    /// This creates a bordered box with the comment content, colored
    /// appropriately for the comment type.
    pub fn render_widget(&self, frame: &mut Frame, area: Rect, comment: &AssistantComment) {
        let (block_style, text_style) = self.styles_for_comment(comment);

        let title = format!(" {} {} ", comment.icon(), comment.comment_type.label());

        let block = Block::default()
            .borders(Borders::ALL)
            .border_style(block_style)
            .title(title)
            .title_alignment(Alignment::Left);

        let inner_area = block.inner(area);
        frame.render_widget(block, area);

        // Render content inside the block
        self.render_content(frame, inner_area, comment, text_style);
    }

    /// Renders a compact comment widget (single line, no border).
    pub fn render_compact(&self, frame: &mut Frame, area: Rect, comment: &AssistantComment) {
        let (_, text_style) = self.styles_for_comment(comment);

        let icon_span = Span::styled(
            format!("{} ", comment.icon()),
            text_style.add_modifier(Modifier::BOLD),
        );
        let text_span = Span::styled(&comment.text, text_style);

        let line = Line::from(vec![icon_span, text_span]);
        let paragraph = Paragraph::new(line);

        frame.render_widget(paragraph, area);
    }

    /// Returns the styles for a comment based on type and color support.
    fn styles_for_comment(&self, comment: &AssistantComment) -> (Style, Style) {
        if !self.color_support.has_colors() {
            // No color support - use plain styles
            return (
                Style::default(),
                Style::default().add_modifier(Modifier::BOLD),
            );
        }

        let color = comment.color();
        let block_style = Style::default().fg(color);
        let text_style = Style::default().fg(color);

        (block_style, text_style)
    }

    /// Renders the comment content (text and optional explanation).
    #[allow(clippy::unused_self)] // Kept as method for API consistency and future extensions
    fn render_content(
        &self,
        frame: &mut Frame,
        area: Rect,
        comment: &AssistantComment,
        text_style: Style,
    ) {
        let has_explanation = comment.explanation.is_some();

        if has_explanation {
            // Split area for text and explanation
            let chunks = Layout::default()
                .direction(Direction::Vertical)
                .constraints([Constraint::Length(1), Constraint::Min(1)])
                .split(area);

            // Primary text (bold)
            let text_line = Line::from(Span::styled(
                &comment.text,
                text_style.add_modifier(Modifier::BOLD),
            ));
            let text_paragraph = Paragraph::new(text_line);
            frame.render_widget(text_paragraph, chunks[0]);

            // Explanation (dimmed)
            if let Some(ref explanation) = comment.explanation {
                let exp_style = text_style.add_modifier(Modifier::DIM);
                let exp_paragraph = Paragraph::new(explanation.as_str())
                    .style(exp_style)
                    .wrap(Wrap { trim: true });
                frame.render_widget(exp_paragraph, chunks[1]);
            }
        } else {
            // Just the primary text
            let text_line = Line::from(Span::styled(
                &comment.text,
                text_style.add_modifier(Modifier::BOLD),
            ));
            let text_paragraph = Paragraph::new(text_line);
            frame.render_widget(text_paragraph, area);
        }
    }

    /// Calculates the required height for a comment widget.
    #[must_use]
    #[allow(clippy::cast_possible_truncation)]
    pub fn required_height(&self, comment: &AssistantComment, width: u16) -> u16 {
        // Border: 2 lines (top + bottom)
        // Text: 1 line
        // Explanation: wrapped lines based on width
        let mut height: u16 = 3; // 2 border + 1 text

        if let Some(ref explanation) = comment.explanation {
            let inner_width = width.saturating_sub(2) as usize; // Account for borders
            if inner_width > 0 {
                let wrapped_lines = explanation.len().div_ceil(inner_width);
                height += wrapped_lines as u16;
            }
        }

        height
    }
}

impl Default for CommentRenderer {
    fn default() -> Self {
        Self::from_env()
    }
}

/// Manages a collection of comments for display.
#[derive(Debug, Default)]
pub struct CommentManager {
    /// Comments to be displayed.
    comments: Vec<AssistantComment>,
    /// The renderer to use.
    renderer: CommentRenderer,
}

impl CommentManager {
    /// Creates a new comment manager.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Creates a comment manager with a specific renderer.
    #[must_use]
    pub fn with_renderer(renderer: CommentRenderer) -> Self {
        Self {
            comments: Vec::new(),
            renderer,
        }
    }

    /// Sets the renderer.
    pub fn set_renderer(&mut self, renderer: CommentRenderer) {
        self.renderer = renderer;
    }

    /// Returns the renderer.
    #[must_use]
    pub const fn renderer(&self) -> &CommentRenderer {
        &self.renderer
    }

    /// Adds a comment to the manager.
    pub fn add_comment(&mut self, comment: AssistantComment) {
        self.comments.push(comment);
    }

    /// Adds a comment from a suggestion.
    pub fn add_from_suggestion(&mut self, suggestion: &Suggestion) {
        self.comments.push(suggestion.into());
    }

    /// Returns all comments.
    #[must_use]
    pub fn comments(&self) -> &[AssistantComment] {
        &self.comments
    }

    /// Returns comments for a specific command.
    #[must_use]
    pub fn comments_for_command(&self, command_id: &str) -> Vec<&AssistantComment> {
        self.comments
            .iter()
            .filter(|c| c.command_id == command_id)
            .collect()
    }

    /// Returns the first comment for a command.
    #[must_use]
    pub fn first_comment_for_command(&self, command_id: &str) -> Option<&AssistantComment> {
        self.comments.iter().find(|c| c.command_id == command_id)
    }

    /// Returns the number of comments.
    #[must_use]
    pub fn len(&self) -> usize {
        self.comments.len()
    }

    /// Returns true if there are no comments.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.comments.is_empty()
    }

    /// Clears all comments.
    pub fn clear(&mut self) {
        self.comments.clear();
    }

    /// Removes comments for a specific command.
    ///
    /// Returns the number of comments removed.
    pub fn remove_for_command(&mut self, command_id: &str) -> usize {
        let initial_len = self.comments.len();
        self.comments.retain(|c| c.command_id != command_id);
        initial_len - self.comments.len()
    }

    /// Renders all comments as shell comments.
    #[must_use]
    pub fn render_all_shell_comments(&self) -> String {
        self.comments
            .iter()
            .map(|c| self.renderer.render_shell_comment(c))
            .collect::<Vec<_>>()
            .join("\n")
    }

    /// Renders comments for a command as shell comments.
    #[must_use]
    pub fn render_shell_comments_for_command(&self, command_id: &str) -> String {
        self.comments_for_command(command_id)
            .iter()
            .map(|c| self.renderer.render_shell_comment(c))
            .collect::<Vec<_>>()
            .join("\n")
    }

    /// Renders comments in a ratatui frame.
    ///
    /// Comments are rendered vertically stacked within the given area.
    pub fn render_all(&self, frame: &mut Frame, area: Rect) {
        if self.comments.is_empty() {
            return;
        }

        // Calculate height per comment
        let comment_heights: Vec<u16> = self
            .comments
            .iter()
            .map(|c| self.renderer.required_height(c, area.width))
            .collect();

        let total_height: u16 = comment_heights.iter().sum();

        // If not enough space, render compactly
        if total_height > area.height {
            self.render_compact(frame, area);
            return;
        }

        // Create layout constraints
        let constraints: Vec<Constraint> = comment_heights
            .iter()
            .map(|&h| Constraint::Length(h))
            .collect();

        let chunks = Layout::default()
            .direction(Direction::Vertical)
            .constraints(constraints)
            .split(area);

        for (i, comment) in self.comments.iter().enumerate() {
            self.renderer.render_widget(frame, chunks[i], comment);
        }
    }

    /// Renders comments compactly (one line each).
    fn render_compact(&self, frame: &mut Frame, area: Rect) {
        let constraints: Vec<Constraint> = self
            .comments
            .iter()
            .map(|_| Constraint::Length(1))
            .collect();

        let chunks = Layout::default()
            .direction(Direction::Vertical)
            .constraints(constraints)
            .split(area);

        for (i, comment) in self.comments.iter().enumerate() {
            if i < chunks.len() {
                self.renderer.render_compact(frame, chunks[i], comment);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // =========================================================================
    // CommentType Tests
    // =========================================================================

    #[test]
    fn test_comment_type_label() {
        assert_eq!(CommentType::Explanation.label(), "explanation");
        assert_eq!(CommentType::Warning.label(), "warning");
        assert_eq!(CommentType::Suggestion.label(), "suggestion");
        assert_eq!(CommentType::Error.label(), "error");
    }

    #[test]
    fn test_comment_type_icon() {
        assert_eq!(CommentType::Explanation.icon(), "[i]");
        assert_eq!(CommentType::Warning.icon(), "[!]");
        assert_eq!(CommentType::Suggestion.icon(), "[>]");
        assert_eq!(CommentType::Error.icon(), "[x]");
    }

    #[test]
    fn test_comment_type_color() {
        assert_eq!(CommentType::Explanation.color(), Color::Blue);
        assert_eq!(CommentType::Warning.color(), Color::Yellow);
        assert_eq!(CommentType::Suggestion.color(), Color::Green);
        assert_eq!(CommentType::Error.color(), Color::Red);
    }

    #[test]
    fn test_comment_type_display() {
        assert_eq!(CommentType::Suggestion.to_string(), "suggestion");
        assert_eq!(CommentType::Warning.to_string(), "warning");
    }

    #[test]
    fn test_comment_type_default() {
        assert_eq!(CommentType::default(), CommentType::Suggestion);
    }

    #[test]
    fn test_comment_type_from_suggestion_type() {
        assert_eq!(
            CommentType::from(SuggestionType::CommandFix),
            CommentType::Suggestion
        );
        assert_eq!(
            CommentType::from(SuggestionType::CommandCompletion),
            CommentType::Suggestion
        );
        assert_eq!(
            CommentType::from(SuggestionType::CommandExplanation),
            CommentType::Explanation
        );
        assert_eq!(
            CommentType::from(SuggestionType::HistorySuggestion),
            CommentType::Suggestion
        );
    }

    // =========================================================================
    // Shell Tests
    // =========================================================================

    #[test]
    fn test_shell_comment_prefix() {
        assert_eq!(Shell::Bash.comment_prefix(), "# ");
        assert_eq!(Shell::Zsh.comment_prefix(), "# ");
        assert_eq!(Shell::Fish.comment_prefix(), "# ");
        assert_eq!(Shell::PowerShell.comment_prefix(), "# ");
        assert_eq!(Shell::Cmd.comment_prefix(), "REM ");
        assert_eq!(Shell::Unknown.comment_prefix(), "# ");
    }

    #[test]
    fn test_shell_from_shell_path() {
        assert_eq!(Shell::from_shell_path("/bin/bash"), Shell::Bash);
        assert_eq!(Shell::from_shell_path("/usr/bin/zsh"), Shell::Zsh);
        assert_eq!(Shell::from_shell_path("/usr/local/bin/fish"), Shell::Fish);
        assert_eq!(
            Shell::from_shell_path("C:\\Windows\\System32\\cmd.exe"),
            Shell::Cmd
        );
        assert_eq!(
            Shell::from_shell_path("C:\\Program Files\\PowerShell\\7\\pwsh.exe"),
            Shell::PowerShell
        );
        assert_eq!(Shell::from_shell_path("/bin/sh"), Shell::Bash);
        assert_eq!(Shell::from_shell_path("/bin/custom-shell"), Shell::Unknown);
    }

    #[test]
    fn test_shell_from_shell_path_case_insensitive() {
        assert_eq!(Shell::from_shell_path("/bin/BASH"), Shell::Bash);
        assert_eq!(Shell::from_shell_path("/bin/ZSH"), Shell::Zsh);
        assert_eq!(Shell::from_shell_path("CMD.EXE"), Shell::Cmd);
        assert_eq!(Shell::from_shell_path("cmd.EXE"), Shell::Cmd);
        assert_eq!(Shell::from_shell_path("CMD.exe"), Shell::Cmd);
        assert_eq!(Shell::from_shell_path("Cmd.Exe"), Shell::Cmd);
    }

    #[test]
    fn test_shell_display() {
        assert_eq!(Shell::Bash.to_string(), "bash");
        assert_eq!(Shell::Zsh.to_string(), "zsh");
        assert_eq!(Shell::Fish.to_string(), "fish");
        assert_eq!(Shell::PowerShell.to_string(), "powershell");
        assert_eq!(Shell::Cmd.to_string(), "cmd");
        assert_eq!(Shell::Unknown.to_string(), "unknown");
    }

    #[test]
    fn test_shell_default() {
        assert_eq!(Shell::default(), Shell::Bash);
    }

    // =========================================================================
    // AssistantComment Tests
    // =========================================================================

    #[test]
    fn test_assistant_comment_new() {
        let comment = AssistantComment::new("cmd-123", "git push");

        assert_eq!(comment.command_id, "cmd-123");
        assert_eq!(comment.text, "git push");
        assert_eq!(comment.comment_type, CommentType::Suggestion);
        assert!(comment.explanation.is_none());
    }

    #[test]
    fn test_assistant_comment_suggestion() {
        let comment = AssistantComment::suggestion("cmd-1", "git push");

        assert_eq!(comment.comment_type, CommentType::Suggestion);
    }

    #[test]
    fn test_assistant_comment_explanation() {
        let comment = AssistantComment::explanation("cmd-2", "Files listed");

        assert_eq!(comment.comment_type, CommentType::Explanation);
    }

    #[test]
    fn test_assistant_comment_warning() {
        let comment = AssistantComment::warning("cmd-3", "This may take a while");

        assert_eq!(comment.comment_type, CommentType::Warning);
    }

    #[test]
    fn test_assistant_comment_error() {
        let comment = AssistantComment::error("cmd-4", "Command failed");

        assert_eq!(comment.comment_type, CommentType::Error);
    }

    #[test]
    fn test_assistant_comment_with_type() {
        let comment = AssistantComment::new("cmd", "text").with_type(CommentType::Warning);

        assert_eq!(comment.comment_type, CommentType::Warning);
    }

    #[test]
    fn test_assistant_comment_with_explanation() {
        let comment =
            AssistantComment::new("cmd", "text").with_explanation("This is an explanation");

        assert_eq!(
            comment.explanation,
            Some("This is an explanation".to_string())
        );
    }

    #[test]
    fn test_assistant_comment_color() {
        let suggestion = AssistantComment::suggestion("cmd", "text");
        let warning = AssistantComment::warning("cmd", "text");
        let explanation = AssistantComment::explanation("cmd", "text");
        let error = AssistantComment::error("cmd", "text");

        assert_eq!(suggestion.color(), Color::Green);
        assert_eq!(warning.color(), Color::Yellow);
        assert_eq!(explanation.color(), Color::Blue);
        assert_eq!(error.color(), Color::Red);
    }

    #[test]
    fn test_assistant_comment_icon() {
        let suggestion = AssistantComment::suggestion("cmd", "text");
        let warning = AssistantComment::warning("cmd", "text");

        assert_eq!(suggestion.icon(), "[>]");
        assert_eq!(warning.icon(), "[!]");
    }

    #[test]
    fn test_assistant_comment_from_suggestion() {
        let suggestion = Suggestion::new("cmd-123", "git push")
            .with_type(SuggestionType::CommandFix)
            .with_explanation("Push your changes");

        let comment: AssistantComment = suggestion.into();

        assert_eq!(comment.command_id, "cmd-123");
        assert_eq!(comment.text, "git push");
        assert_eq!(comment.comment_type, CommentType::Suggestion);
        assert_eq!(comment.explanation, Some("Push your changes".to_string()));
    }

    #[test]
    fn test_assistant_comment_from_suggestion_ref() {
        let suggestion = Suggestion::explanation("cmd-456", "ls -la", "Lists all files");

        let comment: AssistantComment = (&suggestion).into();

        assert_eq!(comment.command_id, "cmd-456");
        assert_eq!(comment.text, "ls -la");
        assert_eq!(comment.comment_type, CommentType::Explanation);
        assert_eq!(comment.explanation, Some("Lists all files".to_string()));
    }

    // =========================================================================
    // CommentRenderer Tests
    // =========================================================================

    #[test]
    fn test_renderer_new() {
        let renderer = CommentRenderer::new(Shell::Bash);

        assert_eq!(renderer.shell(), Shell::Bash);
        assert_eq!(renderer.color_support(), ColorSupport::default());
    }

    #[test]
    fn test_renderer_with_color_support() {
        let renderer = CommentRenderer::new(Shell::Zsh).with_color_support(ColorSupport::TrueColor);

        assert_eq!(renderer.color_support(), ColorSupport::TrueColor);
    }

    #[test]
    fn test_renderer_with_prefix() {
        let renderer = CommentRenderer::new(Shell::Bash).with_prefix("myai");

        let comment = AssistantComment::suggestion("cmd", "git push");
        let output = renderer.render_shell_comment(&comment);

        assert!(output.starts_with("# myai suggestion:"));
    }

    #[test]
    fn test_render_shell_comment_bash() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd-1", "git push");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "# clai suggestion: git push");
    }

    #[test]
    fn test_render_shell_comment_cmd() {
        let renderer = CommentRenderer::new(Shell::Cmd);
        let comment = AssistantComment::suggestion("cmd-1", "git push");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "REM clai suggestion: git push");
    }

    #[test]
    fn test_render_shell_comment_with_explanation() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment =
            AssistantComment::suggestion("cmd-1", "git push").with_explanation("Push your changes");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "# clai suggestion: git push\n#   Push your changes");
    }

    #[test]
    fn test_render_shell_comment_explanation_type() {
        let renderer = CommentRenderer::new(Shell::Zsh);
        let comment = AssistantComment::explanation("cmd-2", "The command succeeded");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "# clai explanation: The command succeeded");
    }

    #[test]
    fn test_render_shell_comment_warning_type() {
        let renderer = CommentRenderer::new(Shell::Fish);
        let comment = AssistantComment::warning("cmd-3", "This may be slow");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "# clai warning: This may be slow");
    }

    #[test]
    fn test_render_shell_comment_error_type() {
        let renderer = CommentRenderer::new(Shell::PowerShell);
        let comment = AssistantComment::error("cmd-4", "Failed to execute");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "# clai error: Failed to execute");
    }

    #[test]
    fn test_render_for_pty_with_newline() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "git push");

        let output = renderer.render_for_pty(&comment, true);

        assert_eq!(output[0], b'\n');
        assert!(output.ends_with(b"\n"));
        let content = String::from_utf8_lossy(&output[1..output.len() - 1]);
        assert_eq!(content, "# clai suggestion: git push");
    }

    #[test]
    fn test_render_for_pty_without_newline() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "git push");

        let output = renderer.render_for_pty(&comment, false);

        assert_ne!(output[0], b'\n');
        assert!(output.ends_with(b"\n"));
    }

    #[test]
    fn test_required_height_simple() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "git push");

        let height = renderer.required_height(&comment, 80);

        assert_eq!(height, 3); // 2 border + 1 text
    }

    #[test]
    fn test_required_height_with_explanation() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment =
            AssistantComment::suggestion("cmd", "git push").with_explanation("Short explanation");

        let height = renderer.required_height(&comment, 80);

        assert!(height >= 4); // 2 border + 1 text + at least 1 explanation line
    }

    #[test]
    fn test_required_height_narrow_width() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "git push")
            .with_explanation("This is a much longer explanation that will need to wrap");

        let height_wide = renderer.required_height(&comment, 80);
        let height_narrow = renderer.required_height(&comment, 20);

        assert!(height_narrow > height_wide);
    }

    // =========================================================================
    // CommentManager Tests
    // =========================================================================

    #[test]
    fn test_manager_new() {
        let manager = CommentManager::new();

        assert!(manager.is_empty());
        assert_eq!(manager.len(), 0);
    }

    #[test]
    fn test_manager_with_renderer() {
        let renderer = CommentRenderer::new(Shell::Zsh);
        let manager = CommentManager::with_renderer(renderer);

        assert_eq!(manager.renderer().shell(), Shell::Zsh);
    }

    #[test]
    fn test_manager_add_comment() {
        let mut manager = CommentManager::new();
        let comment = AssistantComment::suggestion("cmd-1", "git push");

        manager.add_comment(comment);

        assert_eq!(manager.len(), 1);
        assert!(!manager.is_empty());
    }

    #[test]
    fn test_manager_add_from_suggestion() {
        let mut manager = CommentManager::new();
        let suggestion = Suggestion::command_fix("cmd-1", "git push");

        manager.add_from_suggestion(&suggestion);

        assert_eq!(manager.len(), 1);
        let comments = manager.comments();
        assert_eq!(comments[0].text, "git push");
    }

    #[test]
    fn test_manager_comments_for_command() {
        let mut manager = CommentManager::new();

        manager.add_comment(AssistantComment::suggestion("cmd-1", "s1"));
        manager.add_comment(AssistantComment::suggestion("cmd-2", "s2"));
        manager.add_comment(AssistantComment::suggestion("cmd-1", "s3"));

        let cmd1_comments = manager.comments_for_command("cmd-1");
        assert_eq!(cmd1_comments.len(), 2);

        let cmd2_comments = manager.comments_for_command("cmd-2");
        assert_eq!(cmd2_comments.len(), 1);

        let cmd3_comments = manager.comments_for_command("cmd-3");
        assert!(cmd3_comments.is_empty());
    }

    #[test]
    fn test_manager_first_comment_for_command() {
        let mut manager = CommentManager::new();

        manager.add_comment(AssistantComment::suggestion("cmd-1", "first"));
        manager.add_comment(AssistantComment::suggestion("cmd-1", "second"));

        let first = manager.first_comment_for_command("cmd-1");
        assert!(first.is_some());
        assert_eq!(first.unwrap().text, "first");

        let none = manager.first_comment_for_command("cmd-999");
        assert!(none.is_none());
    }

    #[test]
    fn test_manager_clear() {
        let mut manager = CommentManager::new();

        manager.add_comment(AssistantComment::suggestion("cmd-1", "s1"));
        manager.add_comment(AssistantComment::suggestion("cmd-2", "s2"));

        assert_eq!(manager.len(), 2);

        manager.clear();

        assert!(manager.is_empty());
        assert_eq!(manager.len(), 0);
    }

    #[test]
    fn test_manager_remove_for_command() {
        let mut manager = CommentManager::new();

        manager.add_comment(AssistantComment::suggestion("cmd-1", "s1"));
        manager.add_comment(AssistantComment::suggestion("cmd-2", "s2"));
        manager.add_comment(AssistantComment::suggestion("cmd-1", "s3"));

        let removed = manager.remove_for_command("cmd-1");

        assert_eq!(removed, 2);
        assert_eq!(manager.len(), 1);
        assert!(manager.comments_for_command("cmd-1").is_empty());
        assert_eq!(manager.comments_for_command("cmd-2").len(), 1);
    }

    #[test]
    fn test_manager_remove_for_nonexistent_command() {
        let mut manager = CommentManager::new();

        manager.add_comment(AssistantComment::suggestion("cmd-1", "s1"));

        let removed = manager.remove_for_command("cmd-999");

        assert_eq!(removed, 0);
        assert_eq!(manager.len(), 1);
    }

    #[test]
    fn test_manager_render_all_shell_comments() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let mut manager = CommentManager::with_renderer(renderer);

        manager.add_comment(AssistantComment::suggestion("cmd-1", "git push"));
        manager.add_comment(AssistantComment::warning("cmd-2", "be careful"));

        let output = manager.render_all_shell_comments();

        assert!(output.contains("# clai suggestion: git push"));
        assert!(output.contains("# clai warning: be careful"));
    }

    #[test]
    fn test_manager_render_shell_comments_for_command() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let mut manager = CommentManager::with_renderer(renderer);

        manager.add_comment(AssistantComment::suggestion("cmd-1", "s1"));
        manager.add_comment(AssistantComment::suggestion("cmd-2", "s2"));
        manager.add_comment(AssistantComment::suggestion("cmd-1", "s3"));

        let output = manager.render_shell_comments_for_command("cmd-1");

        assert!(output.contains("s1"));
        assert!(output.contains("s3"));
        assert!(!output.contains("s2"));
    }

    #[test]
    fn test_manager_set_renderer() {
        let mut manager = CommentManager::new();
        assert_eq!(manager.renderer().shell(), Shell::from_env());

        let new_renderer = CommentRenderer::new(Shell::Cmd);
        manager.set_renderer(new_renderer);

        assert_eq!(manager.renderer().shell(), Shell::Cmd);
    }

    // =========================================================================
    // Integration Tests
    // =========================================================================

    #[test]
    fn test_full_workflow() {
        // Create suggestions
        let suggestions = vec![
            Suggestion::command_fix("cmd-1", "git push").with_explanation("Push changes"),
            Suggestion::command_completion("cmd-1", "git pull"),
            Suggestion::explanation("cmd-2", "Files listed", "Shows all files"),
        ];

        // Create manager with bash renderer
        let renderer = CommentRenderer::new(Shell::Bash);
        let mut manager = CommentManager::with_renderer(renderer);

        // Add suggestions as comments
        for suggestion in &suggestions {
            manager.add_from_suggestion(suggestion);
        }

        assert_eq!(manager.len(), 3);

        // Get comments for cmd-1
        let cmd1_comments = manager.comments_for_command("cmd-1");
        assert_eq!(cmd1_comments.len(), 2);

        // Render shell comments
        let shell_output = manager.render_shell_comments_for_command("cmd-1");
        assert!(shell_output.contains("# clai suggestion: git push"));
        assert!(shell_output.contains("Push changes"));
        assert!(shell_output.contains("# clai suggestion: git pull"));

        // Remove cmd-1 comments
        let removed = manager.remove_for_command("cmd-1");
        assert_eq!(removed, 2);
        assert_eq!(manager.len(), 1);

        // Verify only cmd-2 remains
        let remaining = manager.first_comment_for_command("cmd-2");
        assert!(remaining.is_some());
        assert_eq!(remaining.unwrap().text, "Files listed");
    }

    #[test]
    fn test_different_shells() {
        let comment = AssistantComment::suggestion("cmd", "git push");

        let shells = [
            (Shell::Bash, "# clai suggestion: git push"),
            (Shell::Zsh, "# clai suggestion: git push"),
            (Shell::Fish, "# clai suggestion: git push"),
            (Shell::PowerShell, "# clai suggestion: git push"),
            (Shell::Cmd, "REM clai suggestion: git push"),
            (Shell::Unknown, "# clai suggestion: git push"),
        ];

        for (shell, expected) in shells {
            let renderer = CommentRenderer::new(shell);
            let output = renderer.render_shell_comment(&comment);
            assert_eq!(output, expected, "Failed for shell: {shell}");
        }
    }

    #[test]
    fn test_multiline_explanation_rendering() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "npm install")
            .with_explanation("Installs all dependencies listed in package.json");

        let output = renderer.render_shell_comment(&comment);

        let lines: Vec<&str> = output.lines().collect();
        assert_eq!(lines.len(), 2);
        assert_eq!(lines[0], "# clai suggestion: npm install");
        assert!(lines[1].starts_with("#   "));
    }

    #[test]
    fn test_comment_type_preservation() {
        let types = [
            (CommentType::Suggestion, "suggestion"),
            (CommentType::Warning, "warning"),
            (CommentType::Explanation, "explanation"),
            (CommentType::Error, "error"),
        ];

        let renderer = CommentRenderer::new(Shell::Bash);

        for (comment_type, expected_label) in types {
            let comment = AssistantComment::new("cmd", "text").with_type(comment_type);
            let output = renderer.render_shell_comment(&comment);
            assert!(
                output.contains(expected_label),
                "Expected '{expected_label}' in output for {comment_type:?}"
            );
        }
    }

    #[test]
    fn test_unicode_in_comments() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "echo '\u{4e2d}\u{6587}'")
            .with_explanation("Outputs Chinese text \u{1f600}");

        let output = renderer.render_shell_comment(&comment);

        assert!(output.contains("\u{4e2d}\u{6587}"));
        assert!(output.contains("\u{1f600}"));
    }

    #[test]
    fn test_empty_text_comment() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::new("cmd", "");

        let output = renderer.render_shell_comment(&comment);

        assert_eq!(output, "# clai suggestion: ");
    }

    #[test]
    fn test_special_characters_in_comment() {
        let renderer = CommentRenderer::new(Shell::Bash);
        let comment = AssistantComment::suggestion("cmd", "git commit -m 'fix: \"quoted\" text'");

        let output = renderer.render_shell_comment(&comment);

        assert!(output.contains("\"quoted\""));
        assert!(output.contains("'fix:"));
    }
}
