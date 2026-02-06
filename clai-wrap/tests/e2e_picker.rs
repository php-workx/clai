//! End-to-end Picker UI integration tests for clai-wrap.
//!
//! These tests verify:
//! - History loading and filtering in picker
//! - Hotkey detection triggering picker
//! - OSC 133 sequence detection
//! - Selection injection back into PTY
//!
//! Tests marked with `#[ignore]` require an interactive TTY environment
//! and should be run manually with `cargo test -- --ignored`.

use std::io::Write;
use std::time::Duration;

use tempfile::NamedTempFile;

use clai_wrap::history_parser;
use clai_wrap::hotkey::{HotkeyConfig, HotkeyEvent, HotkeyParser, HotkeyType, CHORD_FIRST_BYTE};
use clai_wrap::osc133::{Osc133Parser, Osc133State};
use clai_wrap::picker::{Picker, PickerItem};
use clai_wrap::selection_inject::SelectionInjector;
use clai_wrap::standalone::{Feature, StandaloneReason, StandaloneState};

// ============================================================================
// Test Fixtures
// ============================================================================

/// Create a mock bash history file with test commands.
fn create_mock_bash_history() -> NamedTempFile {
    let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
    writeln!(file, "ls -la").unwrap();
    writeln!(file, "git status").unwrap();
    writeln!(file, "git commit -m 'test'").unwrap();
    writeln!(file, "cargo build").unwrap();
    writeln!(file, "cargo test").unwrap();
    writeln!(file, "echo hello world").unwrap();
    writeln!(file, "cd /tmp").unwrap();
    writeln!(file, "vim README.md").unwrap();
    file.flush().unwrap();
    file
}

/// Create a mock bash history file with timestamps.
fn create_mock_timestamped_history() -> NamedTempFile {
    let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
    writeln!(file, "#1700000000").unwrap();
    writeln!(file, "git status").unwrap();
    writeln!(file, "#1700000100").unwrap();
    writeln!(file, "git commit -m 'fix bug'").unwrap();
    writeln!(file, "#1700000200").unwrap();
    writeln!(file, "cargo test --release").unwrap();
    file.flush().unwrap();
    file
}

/// Create a mock zsh history file.
fn create_mock_zsh_history() -> NamedTempFile {
    let mut file = NamedTempFile::with_suffix(".zsh_history").unwrap();
    writeln!(file, ": 1700000000:0;ls -la").unwrap();
    writeln!(file, ": 1700000100:0;git status").unwrap();
    writeln!(file, ": 1700000200:0;docker ps").unwrap();
    writeln!(file, ": 1700000300:0;kubectl get pods").unwrap();
    file.flush().unwrap();
    file
}

/// Create a mock fish history file.
fn create_mock_fish_history() -> NamedTempFile {
    let mut file = NamedTempFile::with_suffix("fish_history").unwrap();
    writeln!(file, "- cmd: ls -la").unwrap();
    writeln!(file, "  when: 1700000000").unwrap();
    writeln!(file, "- cmd: git status").unwrap();
    writeln!(file, "  when: 1700000100").unwrap();
    writeln!(file, "- cmd: npm install").unwrap();
    writeln!(file, "  when: 1700000200").unwrap();
    file.flush().unwrap();
    file
}

// ============================================================================
// History Loading Tests
// ============================================================================

#[test]
fn test_load_bash_history() {
    let history_file = create_mock_bash_history();

    let entries = history_parser::detect_and_parse(history_file.path())
        .expect("Failed to parse bash history");

    assert_eq!(entries.len(), 8, "Should have 8 history entries");
    assert_eq!(entries[0].command, "ls -la");
    assert_eq!(entries[1].command, "git status");
    assert_eq!(entries[7].command, "vim README.md");
}

#[test]
fn test_load_timestamped_bash_history() {
    let history_file = create_mock_timestamped_history();

    let entries = history_parser::detect_and_parse(history_file.path())
        .expect("Failed to parse timestamped history");

    assert_eq!(entries.len(), 3, "Should have 3 history entries");
    assert_eq!(entries[0].command, "git status");
    assert_eq!(entries[0].timestamp, Some(1_700_000_000));
    assert_eq!(entries[2].timestamp, Some(1_700_000_200));
}

#[test]
fn test_load_zsh_history() {
    let history_file = create_mock_zsh_history();

    let entries =
        history_parser::detect_and_parse(history_file.path()).expect("Failed to parse zsh history");

    assert_eq!(entries.len(), 4, "Should have 4 history entries");
    assert_eq!(entries[0].command, "ls -la");
    assert_eq!(entries[0].timestamp, Some(1_700_000_000));
    assert_eq!(entries[3].command, "kubectl get pods");
}

#[test]
fn test_load_fish_history() {
    let history_file = create_mock_fish_history();

    let entries = history_parser::detect_and_parse(history_file.path())
        .expect("Failed to parse fish history");

    assert_eq!(entries.len(), 3, "Should have 3 history entries");
    assert_eq!(entries[0].command, "ls -la");
    assert_eq!(entries[2].command, "npm install");
}

#[test]
fn test_history_invalid_utf8() {
    let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
    // Write invalid UTF-8
    file.write_all(&[0x80, 0x81, 0x82]).unwrap();
    file.flush().unwrap();

    let result = history_parser::detect_and_parse(file.path());
    assert!(result.is_err(), "Should fail on invalid UTF-8");
}

#[test]
fn test_history_empty_file() {
    let file = NamedTempFile::with_suffix(".bash_history").unwrap();
    // File is empty

    let entries =
        history_parser::detect_and_parse(file.path()).expect("Should handle empty file");

    assert!(entries.is_empty(), "Should return empty vector");
}

// ============================================================================
// Picker Integration Tests
// ============================================================================

#[test]
fn test_picker_from_history() {
    let history_file = create_mock_bash_history();

    let entries = history_parser::detect_and_parse(history_file.path())
        .expect("Failed to parse history");

    let items: Vec<PickerItem> = entries
        .iter()
        .rev() // Most recent first
        .map(|e| PickerItem::new(&e.command))
        .collect();

    let picker = Picker::new(items);

    assert_eq!(picker.total_count(), 8);
    assert!(!picker.is_empty());

    // First item should be most recent (vim README.md)
    let selected = picker.selected_item().expect("Should have selection");
    assert_eq!(selected.text, "vim README.md");
}

#[test]
fn test_picker_filter_git_commands() {
    let history_file = create_mock_bash_history();

    let entries = history_parser::detect_and_parse(history_file.path())
        .expect("Failed to parse history");

    let items: Vec<PickerItem> = entries.iter().map(|e| PickerItem::new(&e.command)).collect();

    let mut picker = Picker::new(items);
    picker.update_query("git");

    // Should filter to git commands only
    assert_eq!(picker.filtered_count(), 2);

    let selected = picker.selected_item().expect("Should have selection");
    assert!(
        selected.text.contains("git"),
        "Selected should contain git"
    );
}

#[test]
fn test_picker_filter_cjk_wide_characters() {
    let items = vec![
        PickerItem::new("echo \u{4e2d}\u{6587}"),
        PickerItem::new("echo ascii"),
        PickerItem::new("echo \u{65e5}\u{672c}\u{8a9e}"),
    ];
    let mut picker = Picker::new(items);

    picker.update_query("\u{4e2d}");

    assert_eq!(picker.filtered_count(), 1);
    let selected = picker.selected_item().expect("Should have selection");
    assert_eq!(selected.text, "echo \u{4e2d}\u{6587}");
}

#[test]
fn test_picker_filter_emoji_entries() {
    let items = vec![
        PickerItem::new("echo deploy \u{1f680}"),
        PickerItem::new("echo smoke-test"),
        PickerItem::new("echo success \u{2705}"),
    ];
    let mut picker = Picker::new(items);

    picker.update_query("\u{1f680}");

    assert_eq!(picker.filtered_count(), 1);
    let selected = picker.selected_item().expect("Should have selection");
    assert_eq!(selected.text, "echo deploy \u{1f680}");
}

#[test]
fn test_picker_navigation() {
    let items = vec![
        PickerItem::new("first"),
        PickerItem::new("second"),
        PickerItem::new("third"),
    ];

    let mut picker = Picker::new(items);

    assert_eq!(picker.selected_item().unwrap().text, "first");

    picker.select_next();
    assert_eq!(picker.selected_item().unwrap().text, "second");

    picker.select_next();
    assert_eq!(picker.selected_item().unwrap().text, "third");

    picker.select_next(); // Should wrap
    assert_eq!(picker.selected_item().unwrap().text, "first");

    picker.select_prev(); // Should wrap back
    assert_eq!(picker.selected_item().unwrap().text, "third");
}

#[test]
fn test_picker_incremental_search() {
    let items = vec![
        PickerItem::new("git status"),
        PickerItem::new("git commit"),
        PickerItem::new("grep pattern"),
        PickerItem::new("cargo build"),
    ];

    let mut picker = Picker::new(items);

    // Type 'g' - should match git status, git commit, grep, cargo (all contain 'g')
    picker.push_char('g');
    assert_eq!(picker.filtered_count(), 4);

    // Type 'i' -> 'gi' - should match git status, git commit
    picker.push_char('i');
    assert_eq!(picker.filtered_count(), 2);

    // Type 't' -> 'git' - should still match git commands
    picker.push_char('t');
    assert_eq!(picker.filtered_count(), 2);

    // Backspace -> 'gi'
    picker.pop_char();
    assert_eq!(picker.query(), "gi");
    assert_eq!(picker.filtered_count(), 2);
}

#[test]
fn test_picker_with_initial_query() {
    let items = vec![
        PickerItem::new("git status"),
        PickerItem::new("cargo build"),
        PickerItem::new("git commit"),
    ];

    let picker = Picker::with_query(items, "git");

    assert_eq!(picker.query(), "git");
    assert_eq!(picker.filtered_count(), 2);
}

// ============================================================================
// Hotkey Detection Tests
// ============================================================================

#[test]
fn test_hotkey_chord_history() {
    let mut parser = HotkeyParser::new();

    // Send Ctrl-\ (0x1C)
    let event = parser.process_byte(CHORD_FIRST_BYTE);
    assert!(event.is_none(), "Should buffer first byte");
    assert!(parser.is_waiting(), "Should be waiting");

    // Send 'h' for history
    let event = parser.process_byte(b'h');
    assert_eq!(
        event,
        Some(HotkeyEvent::Triggered(HotkeyType::History)),
        "Should trigger history"
    );
}

#[test]
fn test_hotkey_chord_completions() {
    let mut parser = HotkeyParser::new();

    let event = parser.process_byte(CHORD_FIRST_BYTE);
    assert!(event.is_none());

    let event = parser.process_byte(b'c');
    assert_eq!(
        event,
        Some(HotkeyEvent::Triggered(HotkeyType::Completions)),
        "Should trigger completions"
    );
}

#[test]
fn test_hotkey_timeout() {
    let config = HotkeyConfig {
        timeout: Duration::from_millis(10),
        ..Default::default()
    };
    let mut parser = HotkeyParser::with_config(config);

    // Send first byte
    let event = parser.process_byte(CHORD_FIRST_BYTE);
    assert!(event.is_none());

    // Wait for timeout
    std::thread::sleep(Duration::from_millis(20));

    // Send second byte - should forward both due to timeout
    let event = parser.process_byte(b'h');
    assert_eq!(
        event,
        Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE, b'h'])),
        "Should forward both bytes after timeout"
    );
}

#[test]
fn test_hotkey_cancel_with_escape() {
    let mut parser = HotkeyParser::new();

    let event = parser.process_byte(CHORD_FIRST_BYTE);
    assert!(event.is_none());

    // Escape cancels the chord
    let event = parser.process_byte(0x1B);
    assert_eq!(
        event,
        Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE, 0x1B])),
        "Should forward both bytes on cancel"
    );
}

#[test]
fn test_hotkey_invalid_second_byte() {
    let mut parser = HotkeyParser::new();

    let event = parser.process_byte(CHORD_FIRST_BYTE);
    assert!(event.is_none());

    // Invalid second byte
    let event = parser.process_byte(b'x');
    assert_eq!(
        event,
        Some(HotkeyEvent::Forward(vec![CHORD_FIRST_BYTE, b'x'])),
        "Should forward both bytes on invalid second byte"
    );
}

#[test]
fn test_hotkey_no_byte_loss() {
    let mut parser = HotkeyParser::new();

    // Normal input followed by chord
    let input = [b'a', b'b', CHORD_FIRST_BYTE, b'h', b'c'];
    let events = parser.process_bytes(&input);

    // Should have: Forward(a), Forward(b), Triggered(History), Forward(c)
    assert_eq!(events.len(), 4);
    assert_eq!(events[0], HotkeyEvent::Forward(vec![b'a']));
    assert_eq!(events[1], HotkeyEvent::Forward(vec![b'b']));
    assert_eq!(events[2], HotkeyEvent::Triggered(HotkeyType::History));
    assert_eq!(events[3], HotkeyEvent::Forward(vec![b'c']));
}

// ============================================================================
// OSC 133 Sequence Detection Tests
// ============================================================================

#[test]
fn test_osc133_state_transitions() {
    let mut parser = Osc133Parser::new();

    // Initial state
    assert_eq!(parser.current_state(), &Osc133State::Unknown);

    // PROMPT (A)
    parser.process_bytes(b"\x1b]133;A\x07");
    assert_eq!(parser.current_state(), &Osc133State::Prompt);

    // INPUT (B)
    parser.process_bytes(b"\x1b]133;B\x07");
    assert_eq!(parser.current_state(), &Osc133State::Input);

    // OUTPUT (C)
    parser.process_bytes(b"\x1b]133;C\x07");
    assert_eq!(parser.current_state(), &Osc133State::Output);

    // FINISHED (D) with exit code
    parser.process_bytes(b"\x1b]133;D;0\x07");
    assert_eq!(parser.current_state(), &Osc133State::Finished(0));
}

#[test]
fn test_osc133_split_packet() {
    let mut parser = Osc133Parser::new();

    // Split the sequence across multiple reads (as mentioned in spec Section 3.5)
    parser.process_bytes(b"\x1b]");
    assert_eq!(parser.current_state(), &Osc133State::Unknown);

    parser.process_bytes(b"133;");
    assert_eq!(parser.current_state(), &Osc133State::Unknown);

    parser.process_bytes(b"A\x07");
    assert_eq!(parser.current_state(), &Osc133State::Prompt);
}

#[test]
fn test_osc133_bel_terminator() {
    let mut parser = Osc133Parser::new();

    // BEL terminator (\x07)
    parser.process_bytes(b"\x1b]133;A\x07");
    assert_eq!(parser.current_state(), &Osc133State::Prompt);
}

#[test]
fn test_osc133_st_terminator() {
    let mut parser = Osc133Parser::new();

    // ST terminator (\x1b\\)
    parser.process_bytes(b"\x1b]133;A\x1b\\");
    assert_eq!(parser.current_state(), &Osc133State::Prompt);
}

#[test]
fn test_osc133_with_exit_code() {
    let mut parser = Osc133Parser::new();

    parser.process_bytes(b"\x1b]133;D;42\x07");
    assert_eq!(parser.current_state(), &Osc133State::Finished(42));

    parser.process_bytes(b"\x1b]133;D;127\x07");
    assert_eq!(parser.current_state(), &Osc133State::Finished(127));
}

#[test]
fn test_osc133_interleaved_content() {
    let mut parser = Osc133Parser::new();

    // OSC 133 sequences interleaved with regular terminal output
    parser.process_bytes(b"\x1b]133;A\x07$ ");
    assert_eq!(parser.current_state(), &Osc133State::Prompt);

    parser.process_bytes(b"ls -la\x1b]133;B\x07\x1b]133;C\x07");
    assert_eq!(parser.current_state(), &Osc133State::Output);

    parser.process_bytes(b"file1.txt\nfile2.txt\n\x1b]133;D;0\x07");
    assert_eq!(parser.current_state(), &Osc133State::Finished(0));
}

// ============================================================================
// Selection Injection Tests
// ============================================================================

#[test]
fn test_selection_inject_raw() {
    let injector = SelectionInjector::new();
    let mut output = std::io::Cursor::new(Vec::new());

    injector.inject(&mut output, "echo hello").unwrap();

    assert_eq!(output.get_ref(), b"echo hello");
}

#[test]
fn test_selection_inject_bracketed_paste() {
    let mut injector = SelectionInjector::new();
    injector.set_bracketed_paste(true);

    let mut output = std::io::Cursor::new(Vec::new());
    injector.inject(&mut output, "echo hello").unwrap();

    // Should be wrapped with bracketed paste sequences
    assert!(output.get_ref().starts_with(b"\x1b[200~"));
    assert!(output.get_ref().ends_with(b"\x1b[201~"));
    assert!(String::from_utf8_lossy(output.get_ref()).contains("echo hello"));
}

#[test]
fn test_selection_inject_with_execute() {
    let injector = SelectionInjector::new();
    let mut output = std::io::Cursor::new(Vec::new());

    injector.inject_with_execute(&mut output, "ls -la").unwrap();

    assert_eq!(output.get_ref(), b"ls -la\n");
}

#[test]
fn test_selection_inject_utf8() {
    let injector = SelectionInjector::new();
    let mut output = std::io::Cursor::new(Vec::new());

    let selection = "echo '\u{4e2d}\u{6587} \u{1f600}'";
    injector.inject(&mut output, selection).unwrap();

    assert_eq!(output.get_ref(), selection.as_bytes());
}

#[test]
fn test_selection_inject_special_chars() {
    let injector = SelectionInjector::new();
    let mut output = std::io::Cursor::new(Vec::new());

    let selection = r#"echo "hello $USER" | grep -v 'test'"#;
    injector.inject(&mut output, selection).unwrap();

    assert_eq!(output.get_ref(), selection.as_bytes());
}

// ============================================================================
// Standalone Mode Tests
// ============================================================================

#[test]
fn test_standalone_state_creation() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert!(!state.has_history());
    assert_eq!(state.history_count(), 0);
    assert!(!state.warning_was_logged());
}

#[test]
fn test_standalone_feature_availability() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    // Picker is available in standalone mode
    assert!(state.feature_available(Feature::Picker));
    assert!(state.feature_available(Feature::DenylistGate));

    // These are NOT available in standalone mode
    assert!(!state.feature_available(Feature::OutputCapture));
    assert!(!state.feature_available(Feature::AiSuggestions));
}

#[test]
fn test_standalone_load_history() {
    let history_file = create_mock_bash_history();

    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.load_history_from(history_file.path()).unwrap();

    assert!(state.has_history());
    assert_eq!(state.history_count(), 8);
}

#[test]
fn test_standalone_create_picker() {
    let history_file = create_mock_bash_history();

    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.load_history_from(history_file.path()).unwrap();

    let picker = state.create_picker();

    assert_eq!(picker.total_count(), 8);
    // Most recent should be first
    let selected = picker.selected_item().expect("Should have selection");
    assert_eq!(selected.text, "vim README.md");
}

#[test]
fn test_standalone_create_picker_with_query() {
    let history_file = create_mock_bash_history();

    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.load_history_from(history_file.path()).unwrap();

    let picker = state.create_picker_with_query("git");

    assert_eq!(picker.filtered_count(), 2);
}

#[test]
fn test_standalone_warning_logged_once() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert!(!state.warning_was_logged());

    state.log_warning();
    assert!(state.warning_was_logged());

    // Calling again should not change the flag
    state.log_warning();
    assert!(state.warning_was_logged());
}

// ============================================================================
// Integration Test: Full Flow
// ============================================================================

#[test]
fn test_full_picker_flow() {
    // 1. Create history file
    let history_file = create_mock_bash_history();

    // 2. Load history into standalone state
    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.load_history_from(history_file.path()).unwrap();

    // 3. Create picker
    let mut picker = state.create_picker();
    assert_eq!(picker.total_count(), 8);

    // 4. Simulate user filtering
    picker.update_query("git");
    assert_eq!(picker.filtered_count(), 2);

    // 5. Navigate to select a command
    picker.select_next();
    let selected = picker.selected_item().expect("Should have selection");
    assert!(selected.text.contains("git"));

    // 6. Inject selection
    let injector = SelectionInjector::new();
    let mut output = std::io::Cursor::new(Vec::new());
    injector.inject(&mut output, &selected.text).unwrap();

    // Verify injection
    assert_eq!(
        String::from_utf8_lossy(output.get_ref()),
        selected.text
    );
}

// ============================================================================
// Interactive UI Tests (require TTY)
// ============================================================================

/// Test the full hotkey -> picker -> inject flow.
/// Requires an interactive TTY.
#[test]
#[ignore]
fn test_interactive_picker_ui() {
    // This test would require spawning a PTY and testing the full UI flow
    // It's marked as ignored because it requires interactive TTY

    let history_file = create_mock_bash_history();

    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.load_history_from(history_file.path()).unwrap();

    let picker = state.create_picker();
    assert!(!picker.is_empty());

    // In a real test, we would:
    // 1. Spawn a PTY with a test shell
    // 2. Send the hotkey chord (Ctrl-\ h)
    // 3. Verify picker UI appears
    // 4. Send arrow keys to navigate
    // 5. Send Enter to select
    // 6. Verify selection is injected into PTY

    println!("Interactive picker UI test placeholder");
}
