//! Integration tests for privacy gates, output capture lifecycle,
//! and the suggestion-to-comment rendering pipeline.
//!
//! These tests verify:
//! - Denylist + echo-gap + output capture integration through OSC 133 state machine
//! - `SuggestionReceiver` -> `CommentManager` -> `CommentRenderer` pipeline
//! - Shell-specific comment rendering for all shell types
//!
//! Covers beads issues: ai-terminal-39t (Privacy & Output Capture)
//! and ai-terminal-r7r (Assistant Comment / Suggestion).

use std::time::Instant;

use clai_wrap::assistant_comment::{
    AssistantComment, CommentManager, CommentRenderer, CommentType, Shell,
};
use clai_wrap::denylist::{Denylist, MatchType};
use clai_wrap::echo_gap::{EchoGapDetector, DEFAULT_THRESHOLD_MS};
use clai_wrap::osc133::{Osc133Parser, Osc133State};
use clai_wrap::output_capture::OutputCapture;
use clai_wrap::suggestion_receiver::{Suggestion, SuggestionReceiver, SuggestionType};

// ============================================================================
// Privacy & Output Capture Integration (ai-terminal-39t)
// ============================================================================

#[test]
fn test_capture_lifecycle_through_osc133_states() {
    let mut osc133 = Osc133Parser::new();
    let mut capture = OutputCapture::new(4096);
    let mut cmd_counter: u64 = 0;

    // Prompt -> Input -> Output (start capture) -> data -> Finished (stop capture)
    osc133.process_bytes(b"\x1b]133;A\x07");
    assert_eq!(osc133.current_state(), &Osc133State::Prompt);

    osc133.process_bytes(b"\x1b]133;B\x07");
    assert_eq!(osc133.current_state(), &Osc133State::Input);

    osc133.process_bytes(b"\x1b]133;C\x07");
    assert_eq!(osc133.current_state(), &Osc133State::Output);

    // Start capture on Output state
    cmd_counter += 1;
    let cmd_id = format!("cmd-{cmd_counter}");
    capture.start_capture(&cmd_id);
    assert!(capture.is_capturing());

    // Push output data
    capture.push(b"total 42\ndrwxr-xr-x 2 user user 4096 Jan 1 file.txt\n");

    // Finished state stops capture
    osc133.process_bytes(b"\x1b]133;D;0\x07");
    assert_eq!(osc133.current_state(), &Osc133State::Finished(0));

    let captured = capture.stop_capture().expect("should have captured output");
    assert_eq!(captured.command_id, "cmd-1");
    assert!(!captured.truncated);
    assert!(!captured.is_empty());
    let output_text = captured.as_string_lossy();
    assert!(output_text.contains("total 42"));
    assert!(output_text.contains("file.txt"));
}

#[test]
fn test_denylist_disables_capture_on_output_state() {
    let mut denylist = Denylist::new();
    denylist.add("ssh", MatchType::Exact);
    denylist.add("vim", MatchType::Exact);
    let mut capture = OutputCapture::new(4096);

    // Simulate denied process detected on Output transition
    let fg_process = "ssh";
    assert!(denylist.is_denied(fg_process));

    capture.disable();
    assert!(!capture.is_enabled());

    // Trying to start capture while disabled should not capture
    capture.start_capture("cmd-1");
    assert!(!capture.is_capturing());

    // Re-enable on Finished/Prompt
    capture.enable();
    assert!(capture.is_enabled());

    // Now capture works again
    capture.start_capture("cmd-2");
    assert!(capture.is_capturing());
    capture.push(b"output data");
    let captured = capture.stop_capture().unwrap();
    assert_eq!(captured.command_id, "cmd-2");
    assert!(!captured.is_empty());
}

#[test]
fn test_denylist_allows_non_denied_process() {
    let mut denylist = Denylist::new();
    denylist.add("ssh", MatchType::Exact);
    denylist.add("vim", MatchType::Exact);

    assert!(!denylist.is_denied("ls"));
    assert!(!denylist.is_denied("grep"));
    assert!(!denylist.is_denied("cargo"));
    assert!(denylist.is_denied("ssh"));
    assert!(denylist.is_denied("vim"));
}

#[test]
fn test_echo_gap_detector_normal_typing() {
    let mut detector = EchoGapDetector::new(DEFAULT_THRESHOLD_MS);
    let now = Instant::now();

    // Normal typing: input byte followed quickly by output echo
    detector.record_input(b'h', now);
    detector.record_output(b'h', now);

    assert!(!detector.is_secure_mode());
    assert_eq!(detector.bytes_to_scrub(), 0);
}

#[test]
fn test_echo_gap_detector_password_prompt() {
    let mut detector = EchoGapDetector::new(10); // 10ms threshold for fast test

    let t0 = Instant::now();

    // Simulate password prompt: input bytes with no echo
    detector.record_input(b'p', t0);
    detector.record_input(b'a', t0);
    detector.record_input(b's', t0);

    // Wait well beyond threshold to avoid flakiness
    std::thread::sleep(std::time::Duration::from_millis(50));
    let t1 = Instant::now();

    // Check timeout triggers secure mode
    let triggered = detector.check_timeout(t1);
    assert!(
        triggered,
        "check_timeout should return true when threshold exceeded"
    );
    assert!(
        detector.is_secure_mode(),
        "Should enter secure mode when input has no echo"
    );
    assert!(detector.bytes_to_scrub() > 0, "Should have bytes to scrub");
}

#[test]
fn test_capture_disabled_during_echo_gap_secure_mode() {
    let mut capture = OutputCapture::new(4096);

    // Start capturing
    capture.start_capture("cmd-1");
    capture.push(b"visible output");

    // Echo-gap enters secure mode -> disable capture
    capture.disable();
    assert!(!capture.is_enabled());

    // Push more data (should be ignored since disabled)
    capture.push(b"password characters");

    // Stop and check: stop_capture returns None when disabled mid-capture
    // because disable() clears the buffer
    let result = capture.stop_capture();
    assert!(result.is_none(), "Capture should be cleared when disabled");
}

#[test]
fn test_full_privacy_gate_workflow() {
    let mut denylist = Denylist::new();
    denylist.add("sudo", MatchType::Exact);
    let mut osc133 = Osc133Parser::new();
    let mut capture = OutputCapture::new(4096);
    let mut cmd_counter: u64 = 0;

    // --- Command 1: allowed process ---
    osc133.process_bytes(b"\x1b]133;C\x07");
    let fg = "ls";
    let denied = denylist.is_denied(fg);
    assert!(!denied);
    cmd_counter += 1;
    capture.start_capture(&format!("cmd-{cmd_counter}"));
    capture.push(b"file1\nfile2\n");

    osc133.process_bytes(b"\x1b]133;D;0\x07");
    let captured = capture.stop_capture().unwrap();
    assert_eq!(captured.command_id, "cmd-1");
    assert!(captured.as_string_lossy().contains("file1"));

    // Re-enable if it was disabled
    if !capture.is_enabled() {
        capture.enable();
    }

    // --- Command 2: denied process ---
    osc133.process_bytes(b"\x1b]133;C\x07");
    let fg = "sudo";
    let denied = denylist.is_denied(fg);
    assert!(denied);
    capture.disable();

    osc133.process_bytes(b"\x1b]133;D;0\x07");
    let captured = capture.stop_capture();
    assert!(captured.is_none(), "Should not capture denied process");

    // Re-enable after Finished
    capture.enable();
    assert!(capture.is_enabled());

    // --- Command 3: allowed again ---
    osc133.process_bytes(b"\x1b]133;C\x07");
    cmd_counter += 1;
    capture.start_capture(&format!("cmd-{cmd_counter}"));
    capture.push(b"back to normal\n");

    osc133.process_bytes(b"\x1b]133;D;0\x07");
    let captured = capture.stop_capture().unwrap();
    // cmd_counter is 2 because we skipped starting capture for the denied command
    assert_eq!(captured.command_id, "cmd-2");
    assert!(captured.as_string_lossy().contains("back to normal"));
}

// ============================================================================
// Suggestion -> Comment Pipeline (ai-terminal-r7r)
// ============================================================================

#[test]
fn test_suggestion_to_comment_conversion() {
    let suggestion = Suggestion::command_fix("cmd-1", "git push")
        .with_explanation("Push your changes to remote");

    let comment: AssistantComment = (&suggestion).into();

    assert_eq!(comment.command_id, "cmd-1");
    assert_eq!(comment.text, "git push");
    assert_eq!(comment.comment_type, CommentType::Suggestion);
    assert_eq!(
        comment.explanation,
        Some("Push your changes to remote".to_string())
    );
}

#[test]
fn test_suggestion_type_to_comment_type_mapping() {
    let mappings = [
        (SuggestionType::CommandFix, CommentType::Suggestion),
        (SuggestionType::CommandCompletion, CommentType::Suggestion),
        (SuggestionType::CommandExplanation, CommentType::Explanation),
        (SuggestionType::HistorySuggestion, CommentType::Suggestion),
    ];

    for (suggestion_type, expected_comment_type) in mappings {
        let suggestion = Suggestion::new("cmd", "text").with_type(suggestion_type);
        let comment: AssistantComment = suggestion.into();
        assert_eq!(
            comment.comment_type, expected_comment_type,
            "SuggestionType::{suggestion_type:?} should map to CommentType::{expected_comment_type:?}"
        );
    }
}

#[test]
fn test_comment_renderer_all_shells() {
    let comment = AssistantComment::suggestion("cmd-1", "git push");

    let shells_and_expected = [
        (Shell::Bash, "# clai suggestion: git push"),
        (Shell::Zsh, "# clai suggestion: git push"),
        (Shell::Fish, "# clai suggestion: git push"),
        (Shell::PowerShell, "# clai suggestion: git push"),
        (Shell::Cmd, "REM clai suggestion: git push"),
        (Shell::Unknown, "# clai suggestion: git push"),
    ];

    for (shell, expected) in shells_and_expected {
        let renderer = CommentRenderer::new(shell);
        let output = renderer.render_shell_comment(&comment);
        assert_eq!(output, expected, "Failed for shell: {shell}");
    }
}

#[test]
fn test_comment_renderer_with_explanation() {
    let renderer = CommentRenderer::new(Shell::Bash);
    let comment = AssistantComment::suggestion("cmd-1", "git push")
        .with_explanation("Push changes to origin");

    let output = renderer.render_shell_comment(&comment);
    let lines: Vec<&str> = output.lines().collect();

    assert_eq!(lines.len(), 2);
    assert_eq!(lines[0], "# clai suggestion: git push");
    assert_eq!(lines[1], "#   Push changes to origin");
}

#[test]
fn test_comment_renderer_pty_bytes() {
    let renderer = CommentRenderer::new(Shell::Bash);
    let comment = AssistantComment::suggestion("cmd-1", "git push");

    let bytes = renderer.render_for_pty(&comment, true);
    assert_eq!(bytes[0], b'\n', "Should start with newline");
    assert!(bytes.ends_with(b"\n"), "Should end with newline");

    let content = String::from_utf8_lossy(&bytes[1..bytes.len() - 1]);
    assert_eq!(content, "# clai suggestion: git push");
}

#[test]
fn test_comment_manager_add_from_suggestion() {
    let renderer = CommentRenderer::new(Shell::Bash);
    let mut manager = CommentManager::with_renderer(renderer);

    let suggestion =
        Suggestion::command_fix("cmd-1", "git push").with_explanation("Push your changes");

    manager.add_from_suggestion(&suggestion);

    assert_eq!(manager.len(), 1);
    let comments = manager.comments_for_command("cmd-1");
    assert_eq!(comments.len(), 1);
    assert_eq!(comments[0].text, "git push");
}

#[test]
fn test_comment_manager_render_and_cleanup() {
    let renderer = CommentRenderer::new(Shell::Bash);
    let mut manager = CommentManager::with_renderer(renderer);

    // Add suggestions for two commands
    manager.add_from_suggestion(&Suggestion::command_fix("cmd-1", "git push"));
    manager.add_from_suggestion(&Suggestion::command_completion("cmd-1", "git pull"));
    manager.add_from_suggestion(&Suggestion::command_fix("cmd-2", "cargo test"));

    assert_eq!(manager.len(), 3);

    // Render comments for cmd-1
    let output = manager.render_shell_comments_for_command("cmd-1");
    assert!(output.contains("git push"));
    assert!(output.contains("git pull"));
    assert!(!output.contains("cargo test"));

    // Cleanup cmd-1
    let removed = manager.remove_for_command("cmd-1");
    assert_eq!(removed, 2);
    assert_eq!(manager.len(), 1);

    // cmd-2 still present
    let remaining = manager.comments_for_command("cmd-2");
    assert_eq!(remaining.len(), 1);
}

#[test]
fn test_shell_detection_from_path() {
    assert_eq!(Shell::from_shell_path("/bin/bash"), Shell::Bash);
    assert_eq!(Shell::from_shell_path("/usr/bin/zsh"), Shell::Zsh);
    assert_eq!(Shell::from_shell_path("/usr/local/bin/fish"), Shell::Fish);
    assert_eq!(Shell::from_shell_path("/bin/sh"), Shell::Bash);
    assert_eq!(
        Shell::from_shell_path("C:\\Windows\\System32\\cmd.exe"),
        Shell::Cmd
    );
    assert_eq!(Shell::from_shell_path("/bin/unknown"), Shell::Unknown);
}

#[test]
fn test_full_suggestion_to_rendered_output_pipeline() {
    // This test exercises the complete pipeline:
    // 1. SuggestionReceiver receives a notification
    // 2. Suggestions are retrieved for a command
    // 3. CommentManager converts them to comments
    // 4. CommentRenderer produces shell-appropriate output

    let mut receiver = SuggestionReceiver::new();

    // Add suggestions (simulating daemon notifications)
    receiver.add_suggestion(
        Suggestion::command_fix("cmd-1", "git push --force-with-lease")
            .with_explanation("Use --force-with-lease instead of --force for safety")
            .with_confidence(0.95),
    );
    receiver.add_suggestion(
        Suggestion::command_completion("cmd-1", "git push origin main").with_confidence(0.8),
    );

    assert!(receiver.has_new_suggestions());
    assert_eq!(receiver.count_for_command("cmd-1"), 2);

    // Get suggestions for the finished command
    let suggestions = receiver.suggestions_for_command("cmd-1");
    assert_eq!(suggestions.len(), 2);

    // Convert to comments via manager
    let renderer = CommentRenderer::new(Shell::Zsh);
    let mut manager = CommentManager::with_renderer(renderer);
    for suggestion in &suggestions {
        manager.add_from_suggestion(suggestion);
    }

    // Render shell output
    let output = manager.render_shell_comments_for_command("cmd-1");
    assert!(output.contains("# clai suggestion: git push --force-with-lease"));
    assert!(output.contains("--force-with-lease instead of --force"));
    assert!(output.contains("# clai suggestion: git push origin main"));

    // Cleanup both sides
    receiver.remove_suggestions_for_command("cmd-1");
    manager.remove_for_command("cmd-1");
    assert!(receiver.is_empty());
    assert!(manager.is_empty());
}

#[test]
fn test_all_comment_types_render_correctly() {
    let renderer = CommentRenderer::new(Shell::Bash);

    let cases = [
        (
            AssistantComment::suggestion("cmd", "git push"),
            "suggestion",
        ),
        (
            AssistantComment::warning("cmd", "disk nearly full"),
            "warning",
        ),
        (
            AssistantComment::explanation("cmd", "lists files"),
            "explanation",
        ),
        (AssistantComment::error("cmd", "command not found"), "error"),
    ];

    for (comment, expected_label) in cases {
        let output = renderer.render_shell_comment(&comment);
        assert!(
            output.contains(expected_label),
            "Output should contain '{expected_label}': {output}"
        );
        assert!(
            output.starts_with("# clai"),
            "Should start with shell comment prefix: {output}"
        );
    }
}
