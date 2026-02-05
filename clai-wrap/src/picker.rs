//! Basic picker UI for clai-wrap.
//!
//! This module provides an interactive picker interface for selecting items
//! from a list, with support for incremental search, arrow navigation, and
//! instant display using ratatui.
//!
//! # Example
//!
//! ```rust,no_run
//! use clai_wrap::picker::{Picker, PickerItem};
//!
//! let items = vec![
//!     PickerItem::new("git status"),
//!     PickerItem::with_metadata("git commit -m 'test'", "2024-01-15 10:30"),
//!     PickerItem::new("ls -la"),
//! ];
//!
//! let mut picker = Picker::new(items);
//! picker.update_query("git");
//! // Now only items containing "git" are visible
//! ```

use ratatui::{
    Frame,
    layout::{Constraint, Direction, Layout, Rect},
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, Borders, List, ListItem, ListState, Paragraph},
};

/// A single item that can be displayed and selected in the picker.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PickerItem {
    /// The main text content of the item.
    pub text: String,
    /// Optional metadata (e.g., timestamp) displayed alongside the item.
    pub metadata: Option<String>,
}

impl PickerItem {
    /// Creates a new picker item with just text content.
    #[must_use]
    pub fn new(text: impl Into<String>) -> Self {
        Self {
            text: text.into(),
            metadata: None,
        }
    }

    /// Creates a new picker item with text and metadata.
    #[must_use]
    pub fn with_metadata(text: impl Into<String>, metadata: impl Into<String>) -> Self {
        Self {
            text: text.into(),
            metadata: Some(metadata.into()),
        }
    }
}

/// The result of running the picker.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PickerResult {
    /// User selected an item (contains the selected item's text).
    Selected(String),
    /// User cancelled the picker (pressed Escape).
    Cancelled,
}

/// An interactive picker for selecting items from a list.
///
/// The picker supports:
/// - Incremental search (filter as you type)
/// - Arrow key navigation (up/down)
/// - Enter to select
/// - Escape to cancel
#[derive(Debug)]
pub struct Picker {
    /// All available items.
    items: Vec<PickerItem>,
    /// Index of the currently selected item within `filtered_indices`.
    selected: usize,
    /// Scroll offset for the viewport.
    scroll_offset: usize,
    /// Current search query.
    search_query: String,
    /// Indices into `items` that match the current query.
    filtered_indices: Vec<usize>,
    /// List state for ratatui.
    list_state: ListState,
}

impl Picker {
    /// Creates a new picker with the given items.
    ///
    /// Initially, all items are visible (no filtering).
    #[must_use]
    pub fn new(items: Vec<PickerItem>) -> Self {
        let filtered_indices: Vec<usize> = (0..items.len()).collect();
        let mut list_state = ListState::default();
        if !filtered_indices.is_empty() {
            list_state.select(Some(0));
        }

        Self {
            items,
            selected: 0,
            scroll_offset: 0,
            search_query: String::new(),
            filtered_indices,
            list_state,
        }
    }

    /// Creates a new picker with the given items and an initial search query.
    #[must_use]
    pub fn with_query(items: Vec<PickerItem>, query: impl Into<String>) -> Self {
        let mut picker = Self::new(items);
        let query = query.into();
        if !query.is_empty() {
            picker.update_query(&query);
        }
        picker
    }

    /// Moves the selection up by one item.
    ///
    /// If at the top, wraps to the bottom.
    pub fn select_prev(&mut self) {
        if self.filtered_indices.is_empty() {
            return;
        }

        if self.selected > 0 {
            self.selected -= 1;
        } else {
            // Wrap to bottom
            self.selected = self.filtered_indices.len() - 1;
        }
        self.list_state.select(Some(self.selected));
        self.adjust_scroll_offset();
    }

    /// Moves the selection down by one item.
    ///
    /// If at the bottom, wraps to the top.
    pub fn select_next(&mut self) {
        if self.filtered_indices.is_empty() {
            return;
        }

        if self.selected < self.filtered_indices.len() - 1 {
            self.selected += 1;
        } else {
            // Wrap to top
            self.selected = 0;
        }
        self.list_state.select(Some(self.selected));
        self.adjust_scroll_offset();
    }

    /// Updates the search query and filters the items.
    ///
    /// The filter is case-insensitive and matches items that contain
    /// the query as a substring.
    pub fn update_query(&mut self, query: &str) {
        self.search_query = query.to_string();
        self.apply_filter();
    }

    /// Appends a character to the search query.
    pub fn push_char(&mut self, c: char) {
        self.search_query.push(c);
        self.apply_filter();
    }

    /// Removes the last character from the search query (backspace).
    pub fn pop_char(&mut self) {
        self.search_query.pop();
        self.apply_filter();
    }

    /// Gets the current search query.
    #[must_use]
    pub fn query(&self) -> &str {
        &self.search_query
    }

    /// Returns the currently selected item, if any.
    #[must_use]
    pub fn selected_item(&self) -> Option<&PickerItem> {
        if self.filtered_indices.is_empty() {
            return None;
        }

        self.filtered_indices
            .get(self.selected)
            .and_then(|&idx| self.items.get(idx))
    }

    /// Returns the number of items matching the current filter.
    #[must_use]
    pub fn filtered_count(&self) -> usize {
        self.filtered_indices.len()
    }

    /// Returns the total number of items (unfiltered).
    #[must_use]
    pub fn total_count(&self) -> usize {
        self.items.len()
    }

    /// Returns true if the picker has no items.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.items.is_empty()
    }

    /// Returns true if no items match the current filter.
    #[must_use]
    pub fn is_filtered_empty(&self) -> bool {
        self.filtered_indices.is_empty()
    }

    /// Renders the picker to the terminal using ratatui.
    pub fn render(&mut self, frame: &mut Frame, area: Rect) {
        // Split the area into search box and list
        let chunks = Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(3), // Search box
                Constraint::Min(1),    // List
            ])
            .split(area);

        self.render_search_box(frame, chunks[0]);
        self.render_list(frame, chunks[1]);
    }

    /// Renders just the search box.
    fn render_search_box(&self, frame: &mut Frame, area: Rect) {
        let search_text = format!("> {}", self.search_query);
        let cursor_style = Style::default().add_modifier(Modifier::REVERSED);

        // Create a line with the search text and a cursor
        let spans = vec![
            Span::raw(&search_text),
            Span::styled(" ", cursor_style), // Cursor block
        ];

        let search_paragraph = Paragraph::new(Line::from(spans))
            .block(
                Block::default()
                    .borders(Borders::ALL)
                    .title(format!(
                        " Search ({}/{}) ",
                        self.filtered_count(),
                        self.total_count()
                    )),
            )
            .style(Style::default().fg(Color::White));

        frame.render_widget(search_paragraph, area);
    }

    /// Renders the filtered item list.
    fn render_list(&mut self, frame: &mut Frame, area: Rect) {
        let items: Vec<ListItem> = self
            .filtered_indices
            .iter()
            .map(|&idx| {
                let item = &self.items[idx];
                let content = item.metadata.as_ref().map_or_else(
                    || Line::from(item.text.as_str()),
                    |meta| {
                        Line::from(vec![
                            Span::raw(&item.text),
                            Span::styled(
                                format!("  {meta}"),
                                Style::default().fg(Color::DarkGray),
                            ),
                        ])
                    },
                );
                ListItem::new(content)
            })
            .collect();

        let list = List::new(items)
            .block(Block::default().borders(Borders::ALL).title(" History "))
            .highlight_style(
                Style::default()
                    .bg(Color::Blue)
                    .fg(Color::White)
                    .add_modifier(Modifier::BOLD),
            )
            .highlight_symbol("> ");

        frame.render_stateful_widget(list, area, &mut self.list_state);
    }

    /// Applies the current search query filter to the items.
    fn apply_filter(&mut self) {
        let query_lower = self.search_query.to_lowercase();

        self.filtered_indices = self
            .items
            .iter()
            .enumerate()
            .filter(|(_, item)| {
                if query_lower.is_empty() {
                    true
                } else {
                    item.text.to_lowercase().contains(&query_lower)
                }
            })
            .map(|(idx, _)| idx)
            .collect();

        // Reset selection to the first item (or none if empty)
        if self.filtered_indices.is_empty() {
            self.selected = 0;
            self.list_state.select(None);
        } else {
            self.selected = 0;
            self.list_state.select(Some(0));
        }
        self.scroll_offset = 0;
    }

    /// Adjusts the scroll offset to keep the selection visible.
    fn adjust_scroll_offset(&mut self) {
        // This is handled by ratatui's ListState, but we track scroll_offset
        // for potential future use or custom rendering
        if self.selected < self.scroll_offset {
            self.scroll_offset = self.selected;
        }
        // Note: We don't know viewport height here; ratatui handles this
    }

    /// Adjusts the scroll offset based on the visible viewport height.
    #[allow(dead_code)]
    pub fn adjust_scroll_for_viewport(&mut self, viewport_height: usize) {
        if viewport_height == 0 {
            return;
        }

        // Ensure selected item is visible
        if self.selected < self.scroll_offset {
            self.scroll_offset = self.selected;
        } else if self.selected >= self.scroll_offset + viewport_height {
            self.scroll_offset = self.selected - viewport_height + 1;
        }
    }

    /// Returns the current scroll offset.
    #[must_use]
    pub const fn scroll_offset(&self) -> usize {
        self.scroll_offset
    }

    /// Returns the current selection index within the filtered list.
    #[must_use]
    pub const fn selected_index(&self) -> usize {
        self.selected
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // ========== PickerItem Tests ==========

    #[test]
    fn test_picker_item_new() {
        let item = PickerItem::new("test command");
        assert_eq!(item.text, "test command");
        assert!(item.metadata.is_none());
    }

    #[test]
    fn test_picker_item_with_metadata() {
        let item = PickerItem::with_metadata("git status", "2024-01-15");
        assert_eq!(item.text, "git status");
        assert_eq!(item.metadata, Some("2024-01-15".to_string()));
    }

    #[test]
    fn test_picker_item_clone_and_eq() {
        let item1 = PickerItem::with_metadata("cmd", "meta");
        let item2 = item1.clone();
        assert_eq!(item1, item2);
    }

    // ========== Picker Construction Tests ==========

    #[test]
    fn test_picker_new_empty() {
        let picker = Picker::new(vec![]);
        assert!(picker.is_empty());
        assert!(picker.is_filtered_empty());
        assert_eq!(picker.total_count(), 0);
        assert_eq!(picker.filtered_count(), 0);
        assert!(picker.selected_item().is_none());
    }

    #[test]
    fn test_picker_new_with_items() {
        let items = vec![
            PickerItem::new("first"),
            PickerItem::new("second"),
            PickerItem::new("third"),
        ];
        let picker = Picker::new(items);

        assert!(!picker.is_empty());
        assert!(!picker.is_filtered_empty());
        assert_eq!(picker.total_count(), 3);
        assert_eq!(picker.filtered_count(), 3);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("first"));
    }

    #[test]
    fn test_picker_with_query() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("ls -la"),
            PickerItem::new("git commit"),
        ];
        let picker = Picker::with_query(items, "git");

        assert_eq!(picker.filtered_count(), 2);
        assert_eq!(picker.query(), "git");
    }

    // ========== Selection Navigation Tests ==========

    #[test]
    fn test_select_next() {
        let items = vec![
            PickerItem::new("first"),
            PickerItem::new("second"),
            PickerItem::new("third"),
        ];
        let mut picker = Picker::new(items);

        assert_eq!(picker.selected_index(), 0);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("first"));

        picker.select_next();
        assert_eq!(picker.selected_index(), 1);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("second"));

        picker.select_next();
        assert_eq!(picker.selected_index(), 2);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("third"));
    }

    #[test]
    fn test_select_next_wraps() {
        let items = vec![
            PickerItem::new("first"),
            PickerItem::new("second"),
        ];
        let mut picker = Picker::new(items);

        picker.select_next(); // -> second
        picker.select_next(); // -> first (wrap)

        assert_eq!(picker.selected_index(), 0);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("first"));
    }

    #[test]
    fn test_select_prev() {
        let items = vec![
            PickerItem::new("first"),
            PickerItem::new("second"),
            PickerItem::new("third"),
        ];
        let mut picker = Picker::new(items);

        // Start at first, go to end, then back
        picker.select_next();
        picker.select_next();
        assert_eq!(picker.selected_index(), 2);

        picker.select_prev();
        assert_eq!(picker.selected_index(), 1);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("second"));

        picker.select_prev();
        assert_eq!(picker.selected_index(), 0);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("first"));
    }

    #[test]
    fn test_select_prev_wraps() {
        let items = vec![
            PickerItem::new("first"),
            PickerItem::new("second"),
        ];
        let mut picker = Picker::new(items);

        // From first, go up should wrap to last
        picker.select_prev();

        assert_eq!(picker.selected_index(), 1);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("second"));
    }

    #[test]
    fn test_selection_on_empty_list() {
        let mut picker = Picker::new(vec![]);

        // Should not panic
        picker.select_next();
        picker.select_prev();

        assert!(picker.selected_item().is_none());
        assert_eq!(picker.selected_index(), 0);
    }

    // ========== Filtering Tests ==========

    #[test]
    fn test_update_query_filters_items() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("ls -la"),
            PickerItem::new("git commit"),
            PickerItem::new("cargo build"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("git");

        assert_eq!(picker.filtered_count(), 2);
        // Selection should reset to first matching item
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("git status"));
    }

    #[test]
    fn test_filter_case_insensitive() {
        let items = vec![
            PickerItem::new("Git Status"),
            PickerItem::new("ls -la"),
            PickerItem::new("GIT COMMIT"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("git");

        assert_eq!(picker.filtered_count(), 2);
    }

    #[test]
    fn test_filter_no_matches() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("ls -la"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("xyz");

        assert_eq!(picker.filtered_count(), 0);
        assert!(picker.is_filtered_empty());
        assert!(picker.selected_item().is_none());
    }

    #[test]
    fn test_filter_empty_query_shows_all() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("ls -la"),
            PickerItem::new("cargo build"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("git");
        assert_eq!(picker.filtered_count(), 1);

        picker.update_query("");
        assert_eq!(picker.filtered_count(), 3);
    }

    #[test]
    fn test_push_char() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("git commit"),
            PickerItem::new("grep pattern"),
        ];
        let mut picker = Picker::new(items);

        picker.push_char('g');
        assert_eq!(picker.query(), "g");
        assert_eq!(picker.filtered_count(), 3); // All contain 'g'

        picker.push_char('i');
        assert_eq!(picker.query(), "gi");
        assert_eq!(picker.filtered_count(), 2); // git status, git commit

        picker.push_char('t');
        assert_eq!(picker.query(), "git");
        assert_eq!(picker.filtered_count(), 2);
    }

    #[test]
    fn test_pop_char() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("grep pattern"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("git");
        assert_eq!(picker.filtered_count(), 1);

        picker.pop_char(); // "gi"
        assert_eq!(picker.query(), "gi");
        assert_eq!(picker.filtered_count(), 1);

        picker.pop_char(); // "g"
        picker.pop_char(); // ""
        assert_eq!(picker.query(), "");
        assert_eq!(picker.filtered_count(), 2);

        // Pop on empty should not panic
        picker.pop_char();
        assert_eq!(picker.query(), "");
    }

    // ========== Selection After Filter Tests ==========

    #[test]
    fn test_selection_resets_after_filter() {
        let items = vec![
            PickerItem::new("alpha"),
            PickerItem::new("beta"),
            PickerItem::new("gamma"),
        ];
        let mut picker = Picker::new(items);

        picker.select_next();
        picker.select_next();
        assert_eq!(picker.selected_index(), 2);

        picker.update_query("a"); // alpha, gamma

        assert_eq!(picker.selected_index(), 0);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("alpha"));
    }

    #[test]
    fn test_navigation_after_filter() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("ls -la"),
            PickerItem::new("git commit"),
            PickerItem::new("cargo build"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("git");
        assert_eq!(picker.filtered_count(), 2);

        // Navigate within filtered items
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("git status"));

        picker.select_next();
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("git commit"));

        picker.select_next(); // Wrap
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("git status"));
    }

    // ========== Scroll Offset Tests ==========

    #[test]
    fn test_scroll_offset_initial() {
        let items: Vec<PickerItem> = (0..100)
            .map(|i| PickerItem::new(format!("item {i}")))
            .collect();
        let picker = Picker::new(items);

        assert_eq!(picker.scroll_offset(), 0);
    }

    #[test]
    fn test_scroll_offset_adjusts_for_viewport() {
        let items: Vec<PickerItem> = (0..100)
            .map(|i| PickerItem::new(format!("item {i}")))
            .collect();
        let mut picker = Picker::new(items);

        // Move selection beyond viewport
        for _ in 0..20 {
            picker.select_next();
        }

        picker.adjust_scroll_for_viewport(10);

        // Selection is at 20, viewport is 10, scroll should adjust
        assert!(picker.scroll_offset() <= picker.selected_index());
        assert!(picker.selected_index() < picker.scroll_offset() + 10);
    }

    #[test]
    fn test_scroll_offset_zero_viewport() {
        let items: Vec<PickerItem> = (0..10)
            .map(|i| PickerItem::new(format!("item {i}")))
            .collect();
        let mut picker = Picker::new(items);

        picker.select_next();
        picker.select_next();

        // Should not panic with zero viewport
        picker.adjust_scroll_for_viewport(0);
    }

    // ========== Edge Cases ==========

    #[test]
    fn test_unicode_items() {
        let items = vec![
            PickerItem::new("echo \u{4e2d}\u{6587}"), // Chinese
            PickerItem::new("echo \u{1f600}"),         // Emoji
            PickerItem::new("normal text"),
        ];
        let mut picker = Picker::new(items);

        assert_eq!(picker.total_count(), 3);

        picker.update_query("\u{4e2d}");
        assert_eq!(picker.filtered_count(), 1);
    }

    #[test]
    fn test_very_long_item() {
        let long_text = "x".repeat(10_000);
        let items = vec![PickerItem::new(long_text.clone())];
        let picker = Picker::new(items);

        assert_eq!(picker.selected_item().map(|i| i.text.len()), Some(10_000));
    }

    #[test]
    fn test_special_characters_in_query() {
        let items = vec![
            PickerItem::new("git commit -m 'message'"),
            PickerItem::new("echo \"hello\""),
            PickerItem::new("ls -la | grep test"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("'");
        assert_eq!(picker.filtered_count(), 1);

        picker.update_query("|");
        assert_eq!(picker.filtered_count(), 1);
    }

    #[test]
    fn test_metadata_preserved_through_filter() {
        let items = vec![
            PickerItem::with_metadata("git status", "10:30"),
            PickerItem::with_metadata("ls -la", "10:31"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("git");

        let selected = picker.selected_item().unwrap();
        assert_eq!(selected.text, "git status");
        assert_eq!(selected.metadata, Some("10:30".to_string()));
    }

    #[test]
    fn test_single_item_list() {
        let items = vec![PickerItem::new("only one")];
        let mut picker = Picker::new(items);

        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("only one"));

        // Navigation should stay on the same item (wrapping)
        picker.select_next();
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("only one"));

        picker.select_prev();
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("only one"));
    }

    #[test]
    fn test_filter_to_single_item_then_navigate() {
        let items = vec![
            PickerItem::new("unique"),
            PickerItem::new("other"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("unique");
        assert_eq!(picker.filtered_count(), 1);

        // Navigation should wrap to itself
        picker.select_next();
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("unique"));
    }

    // ========== State Consistency Tests ==========

    #[test]
    fn test_filter_then_clear_restores_state() {
        let items = vec![
            PickerItem::new("alpha"),
            PickerItem::new("beta"),
            PickerItem::new("gamma"),
        ];
        let mut picker = Picker::new(items);

        picker.update_query("xyz"); // No matches
        assert!(picker.is_filtered_empty());

        picker.update_query(""); // Clear filter
        assert_eq!(picker.filtered_count(), 3);
        assert_eq!(picker.selected_index(), 0);
    }

    #[test]
    fn test_rapid_filter_changes() {
        let items = vec![
            PickerItem::new("git status"),
            PickerItem::new("git commit"),
            PickerItem::new("ls -la"),
        ];
        let mut picker = Picker::new(items);

        // Simulate rapid typing
        picker.push_char('g');
        picker.push_char('i');
        picker.push_char('t');
        picker.pop_char();
        picker.pop_char();
        picker.push_char('i');
        picker.push_char('t');
        picker.push_char(' ');
        picker.push_char('s');

        assert_eq!(picker.query(), "git s");
        assert_eq!(picker.filtered_count(), 1);
        assert_eq!(picker.selected_item().map(|i| i.text.as_str()), Some("git status"));
    }
}
