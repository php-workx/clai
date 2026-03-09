//! End-to-end mode switching tests for clai-wrap.
//!
//! These tests verify:
//! - Standalone mode operation (no daemon)
//! - Passthrough fallback mode
//! - TTY detection and non-TTY handling
//! - Mode transitions
//!
//! Tests marked with `#[ignore]` require an interactive TTY environment
//! and should be run manually with `cargo test -- --ignored`.

use std::env;
use std::io::Write;
#[cfg(unix)]
use std::os::fd::{FromRawFd, RawFd};
use std::path::PathBuf;
use std::process::{Command, Output, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use tempfile::NamedTempFile;

use clai_wrap::passthrough::{
    check_passthrough_needed, check_shell_support, should_use_passthrough, PassthroughReason,
};
use clai_wrap::raw_mode::{detect_tty, TtyStatus};
use clai_wrap::standalone::{Feature, StandaloneError, StandaloneReason, StandaloneState};

static ENV_LOCK: Mutex<()> = Mutex::new(());

#[cfg(unix)]
fn clai_wrap_binary() -> Option<PathBuf> {
    for key in ["CARGO_BIN_EXE_clai-wrap", "CARGO_BIN_EXE_clai_wrap"] {
        if let Some(path) = std::env::var_os(key).map(PathBuf::from) {
            if path.exists() {
                return Some(path);
            }
        }
    }

    let current_exe = std::env::current_exe().ok()?;
    let deps_dir = current_exe.parent()?;
    let target_dir = deps_dir.parent()?;
    let binary = target_dir.join("clai-wrap");
    if binary.exists() {
        return Some(binary);
    }

    None
}

#[cfg(unix)]
fn run_with_timeout(mut child: std::process::Child, timeout: Duration) -> Output {
    let start = Instant::now();
    while start.elapsed() < timeout {
        match child.try_wait() {
            Ok(Some(_)) => return child.wait_with_output().expect("collect child output"),
            Ok(None) => std::thread::sleep(Duration::from_millis(10)),
            Err(e) => panic!("failed to poll child: {e}"),
        }
    }
    let _ = child.kill();
    child
        .wait_with_output()
        .expect("collect killed child output")
}

#[cfg(unix)]
struct PtyPair {
    master: RawFd,
    slave: RawFd,
}

#[cfg(unix)]
impl PtyPair {
    fn new() -> Option<Self> {
        let mut master: libc::c_int = -1;
        let mut slave: libc::c_int = -1;
        let result = unsafe {
            libc::openpty(
                &mut master,
                &mut slave,
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
            )
        };
        if result != 0 {
            return None;
        }
        Some(Self { master, slave })
    }
}

#[cfg(unix)]
impl Drop for PtyPair {
    fn drop(&mut self) {
        unsafe {
            libc::close(self.master);
            libc::close(self.slave);
        }
    }
}

#[cfg(unix)]
fn get_termios(fd: RawFd) -> libc::termios {
    let mut termios = std::mem::MaybeUninit::<libc::termios>::uninit();
    let result = unsafe { libc::tcgetattr(fd, termios.as_mut_ptr()) };
    assert_eq!(result, 0, "tcgetattr failed");
    unsafe { termios.assume_init() }
}

#[cfg(unix)]
fn set_termios_now(fd: RawFd, termios: &libc::termios) {
    let result = unsafe { libc::tcsetattr(fd, libc::TCSANOW, termios) };
    assert_eq!(result, 0, "tcsetattr failed");
}

// ============================================================================
// TTY Detection Tests
// ============================================================================

#[test]
fn test_detect_tty_returns_valid_status() {
    let status = detect_tty();

    // In test environment, we typically don't have a TTY
    // but the function should not panic
    let _ = status.stdin;
    let _ = status.stdout;
    let _ = status.stderr;
}

#[test]
fn test_tty_status_struct() {
    // Verify TtyStatus fields are accessible
    let status = TtyStatus {
        stdin: true,
        stdout: true,
        stderr: true,
    };

    assert!(status.stdin);
    assert!(status.stdout);
    assert!(status.stderr);
}

// ============================================================================
// Passthrough Mode Detection Tests
// ============================================================================

#[test]
fn test_passthrough_reason_not_needed() {
    let reason = PassthroughReason::NotNeeded;

    assert!(!reason.needs_passthrough());
    assert!(reason.description().contains("Full functionality"));
}

#[test]
fn test_passthrough_reason_dumb_terminal() {
    let reason = PassthroughReason::DumbTerminal;

    assert!(reason.needs_passthrough());
    assert!(reason.description().contains("dumb"));
}

#[test]
fn test_passthrough_reason_non_tty_stdin() {
    let reason = PassthroughReason::NonTtyStdin;

    assert!(reason.needs_passthrough());
    assert!(reason.description().contains("stdin"));
}

#[test]
fn test_passthrough_reason_non_tty_stdout() {
    let reason = PassthroughReason::NonTtyStdout;

    assert!(reason.needs_passthrough());
    assert!(reason.description().contains("stdout"));
}

#[test]
fn test_passthrough_reason_unsupported_shell() {
    let reason = PassthroughReason::UnsupportedShell("cmd.exe".to_string());

    assert!(reason.needs_passthrough());
    assert!(reason.description().contains("cmd.exe"));
    assert!(reason.description().contains("OSC 133"));
}

#[test]
fn test_should_use_passthrough_dumb_term() {
    let _lock = ENV_LOCK.lock().expect("lock env");

    // Save original TERM
    let original = env::var("TERM").ok();

    // Test with TERM=dumb
    env::set_var("TERM", "dumb");
    let reason = should_use_passthrough();
    assert!(matches!(reason, PassthroughReason::DumbTerminal));

    // Restore
    if let Some(term) = original {
        env::set_var("TERM", term);
    } else {
        env::remove_var("TERM");
    }
}

#[test]
fn test_should_use_passthrough_unset_term() {
    let _lock = ENV_LOCK.lock().expect("lock env");

    // Save original TERM
    let original = env::var("TERM").ok();

    // Test with TERM unset
    env::remove_var("TERM");
    let reason = should_use_passthrough();
    assert!(matches!(reason, PassthroughReason::DumbTerminal));

    // Restore
    if let Some(term) = original {
        env::set_var("TERM", term);
    }
}

#[test]
fn test_should_use_passthrough_valid_term() {
    let _lock = ENV_LOCK.lock().expect("lock env");

    // Save original TERM
    let original = env::var("TERM").ok();

    // Test with valid TERM
    env::set_var("TERM", "xterm-256color");
    let reason = should_use_passthrough();

    // With valid TERM, it checks TTY status
    // In test environment, we likely get NonTtyStdin or NonTtyStdout
    assert!(!matches!(reason, PassthroughReason::DumbTerminal));

    // Restore
    if let Some(term) = original {
        env::set_var("TERM", term);
    }
}

// ============================================================================
// Shell Support Tests
// ============================================================================

#[test]
fn test_check_shell_support_bash() {
    let path = PathBuf::from("/bin/bash");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_shell_support_zsh() {
    let path = PathBuf::from("/usr/bin/zsh");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_shell_support_fish() {
    let path = PathBuf::from("/usr/local/bin/fish");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_shell_support_sh() {
    let path = PathBuf::from("/bin/sh");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_shell_support_powershell() {
    let path = PathBuf::from("powershell.exe");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_shell_support_pwsh() {
    let path = PathBuf::from("/usr/local/bin/pwsh");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_shell_support_cmd() {
    let path = PathBuf::from("cmd.exe");
    let reason = check_shell_support(&path);

    assert!(reason.is_some());
    let reason = reason.unwrap();
    assert!(matches!(reason, PassthroughReason::UnsupportedShell(s) if s == "cmd.exe"));
}

#[test]
fn test_check_shell_support_unknown() {
    let path = PathBuf::from("/bin/weirdshell");
    let reason = check_shell_support(&path);

    assert!(reason.is_some());
    assert!(matches!(
        reason,
        Some(PassthroughReason::UnsupportedShell(s)) if s == "weirdshell"
    ));
}

#[test]
fn test_check_shell_support_case_insensitive() {
    // Uppercase should work
    let path = PathBuf::from("/bin/BASH");
    assert!(check_shell_support(&path).is_none());

    let path = PathBuf::from("/bin/ZSH");
    assert!(check_shell_support(&path).is_none());
}

#[test]
fn test_check_passthrough_needed_with_unsupported_shell() {
    let _lock = ENV_LOCK.lock().expect("lock env");

    // Save original TERM
    let original = env::var("TERM").ok();

    // Set valid TERM to avoid DumbTerminal
    env::set_var("TERM", "xterm-256color");

    let path = PathBuf::from("cmd.exe");
    let reason = check_passthrough_needed(Some(&path));

    // Should return UnsupportedShell (or NonTtyStdin/NonTtyStdout in test env)
    assert!(reason.needs_passthrough());

    // Restore
    if let Some(term) = original {
        env::set_var("TERM", term);
    }
}

// ============================================================================
// Standalone Mode Tests
// ============================================================================

#[test]
fn test_standalone_reason_display() {
    assert_eq!(
        StandaloneReason::DaemonUnavailable.to_string(),
        "daemon unavailable"
    );
    assert_eq!(
        StandaloneReason::ConnectionTimeout.to_string(),
        "connection timeout"
    );
    assert_eq!(
        StandaloneReason::SocketError("test error".to_string()).to_string(),
        "socket error: test error"
    );
}

#[test]
fn test_standalone_reason_equality() {
    assert_eq!(
        StandaloneReason::DaemonUnavailable,
        StandaloneReason::DaemonUnavailable
    );
    assert_ne!(
        StandaloneReason::DaemonUnavailable,
        StandaloneReason::ConnectionTimeout
    );

    let err1 = StandaloneReason::SocketError("error".to_string());
    let err2 = StandaloneReason::SocketError("error".to_string());
    assert_eq!(err1, err2);
}

#[test]
fn test_standalone_state_initial() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert_eq!(*state.reason(), StandaloneReason::DaemonUnavailable);
    assert!(!state.has_history());
    assert_eq!(state.history_count(), 0);
    assert!(state.history_entries().is_empty());
    assert!(state.history_path().is_none());
    assert!(!state.warning_was_logged());
}

#[test]
fn test_standalone_feature_picker_available() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert!(state.feature_available(Feature::Picker));
}

#[test]
fn test_standalone_feature_denylist_available() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert!(state.feature_available(Feature::DenylistGate));
}

#[test]
fn test_standalone_feature_output_capture_unavailable() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert!(!state.feature_available(Feature::OutputCapture));
}

#[test]
fn test_standalone_feature_ai_suggestions_unavailable() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    assert!(!state.feature_available(Feature::AiSuggestions));
}

#[test]
fn test_standalone_all_reasons_same_features() {
    let reasons = [
        StandaloneReason::DaemonUnavailable,
        StandaloneReason::ConnectionTimeout,
        StandaloneReason::SocketError("test".to_string()),
    ];

    for reason in reasons {
        let state = StandaloneState::new(reason);

        // All reasons should have the same feature availability
        assert!(state.feature_available(Feature::Picker));
        assert!(state.feature_available(Feature::DenylistGate));
        assert!(!state.feature_available(Feature::OutputCapture));
        assert!(!state.feature_available(Feature::AiSuggestions));
    }
}

#[test]
fn test_standalone_load_history() {
    let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
    writeln!(file, "ls -la").unwrap();
    writeln!(file, "git status").unwrap();
    file.flush().unwrap();

    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.load_history_from(file.path()).unwrap();

    assert!(state.has_history());
    assert_eq!(state.history_count(), 2);
    assert_eq!(state.history_path(), Some(file.path()));
}

#[test]
fn test_standalone_load_nonexistent_file() {
    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    let result = state.load_history_from(PathBuf::from("/nonexistent/file").as_path());

    assert!(result.is_err());
}

#[test]
fn test_standalone_warning_thread_safe() {
    use std::sync::Arc;
    use std::thread;

    let state = Arc::new(StandaloneState::new(StandaloneReason::DaemonUnavailable));

    // Spawn multiple threads to log warning
    let handles: Vec<_> = (0..10)
        .map(|_| {
            let state = Arc::clone(&state);
            thread::spawn(move || {
                state.log_warning();
            })
        })
        .collect();

    for handle in handles {
        handle.join().unwrap();
    }

    // Should be logged exactly once
    assert!(state.warning_was_logged());
}

#[test]
fn test_standalone_create_empty_picker() {
    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    let picker = state.create_picker();

    assert!(picker.is_empty());
    assert_eq!(picker.total_count(), 0);
}

#[test]
fn test_standalone_error_display() {
    let err = StandaloneError::HistoryNotFound("bash".to_string());
    assert!(err.to_string().contains("bash"));

    let err = StandaloneError::HomeNotFound;
    assert!(err.to_string().contains("home directory"));
}

// ============================================================================
// Mode Transition Tests
// ============================================================================

#[test]
fn test_mode_transition_daemon_to_standalone() {
    // Simulates what happens when daemon becomes unavailable

    // Initially might try to connect to daemon (not implemented here)
    // On failure, create standalone state

    let state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    // Verify standalone mode is properly initialized
    assert!(state.feature_available(Feature::Picker));
    assert!(!state.feature_available(Feature::AiSuggestions));

    // Log warning (once)
    state.log_warning();
    assert!(state.warning_was_logged());
}

#[test]
fn test_mode_transition_with_history() {
    let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
    writeln!(file, "command1").unwrap();
    writeln!(file, "command2").unwrap();
    writeln!(file, "command3").unwrap();
    file.flush().unwrap();

    // Simulate transitioning to standalone mode with history
    let mut state = StandaloneState::new(StandaloneReason::ConnectionTimeout);
    state.load_history_from(file.path()).unwrap();

    // Verify history is available in standalone mode
    let picker = state.create_picker();
    assert_eq!(picker.total_count(), 3);
}

// ============================================================================
// Environment Variable Tests
// ============================================================================

#[test]
fn test_term_environment_handling() {
    let _lock = ENV_LOCK.lock().expect("lock env");

    let original = env::var("TERM").ok();

    // Test various TERM values
    let test_cases = [
        ("dumb", true),
        ("xterm", false),
        ("xterm-256color", false),
        ("screen", false),
        ("linux", false),
    ];

    for (term_value, should_be_dumb) in test_cases {
        env::set_var("TERM", term_value);
        let reason = should_use_passthrough();

        if should_be_dumb {
            assert!(
                matches!(reason, PassthroughReason::DumbTerminal),
                "TERM={} should be DumbTerminal, got {:?}",
                term_value,
                reason
            );
        } else {
            assert!(
                !matches!(reason, PassthroughReason::DumbTerminal),
                "TERM={} should NOT be DumbTerminal, got {:?}",
                term_value,
                reason
            );
        }
    }

    // Restore
    if let Some(term) = original {
        env::set_var("TERM", term);
    } else {
        env::remove_var("TERM");
    }
}

#[test]
#[cfg(unix)]
fn test_non_tty_without_force_non_tty_fails_raw_mode() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: clai-wrap binary unavailable");
        return;
    };

    let child = Command::new(binary)
        .args([
            "--standalone",
            "--shell",
            "/bin/sh",
            "--login-shell",
            "false",
        ])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn clai-wrap");

    let output = run_with_timeout(child, Duration::from_secs(5));
    let stderr = String::from_utf8_lossy(&output.stderr);

    assert!(
        !output.status.success(),
        "Expected failure when raw mode is required in non-TTY"
    );
    assert!(
        stderr.contains("use --force-non-tty"),
        "Expected non-TTY guidance message, got: {stderr}"
    );
}

#[test]
#[cfg(unix)]
fn test_force_non_tty_allows_passthrough_with_piped_io() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: clai-wrap binary unavailable");
        return;
    };

    let mut child = Command::new(binary)
        .args(["--force-non-tty", "--shell", "/bin/sh"])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn clai-wrap");

    let stdin = child.stdin.as_mut().expect("child stdin");
    stdin
        .write_all(b"echo FORCE_NON_TTY_OK\nexit\n")
        .expect("write piped commands");

    let output = run_with_timeout(child, Duration::from_secs(5));
    let stdout = String::from_utf8_lossy(&output.stdout);

    assert!(
        stdout.contains("FORCE_NON_TTY_OK"),
        "Expected passthrough command output, got: {stdout}"
    );
}

#[test]
#[cfg(unix)]
fn test_non_utf8_locale_logs_warning_in_debug_mode() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: clai-wrap binary unavailable");
        return;
    };

    let mut child = Command::new(binary)
        .args(["--force-non-tty", "--debug", "--shell", "/bin/sh"])
        .env("LC_ALL", "C")
        .env("LANG", "C")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn clai-wrap");

    let stdin = child.stdin.as_mut().expect("child stdin");
    stdin
        .write_all(b"echo LOCALE_WARNING_OK\nexit\n")
        .expect("write locale warning test commands");

    let output = run_with_timeout(child, Duration::from_secs(5));
    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    assert!(
        stdout.contains("LOCALE_WARNING_OK"),
        "Expected passthrough output, got: {stdout}"
    );
    assert!(
        stderr.contains("non-UTF-8 locale detected"),
        "Expected non-UTF8 locale warning in debug logs, got: {stderr}"
    );
}

#[test]
#[cfg(unix)]
fn test_reset_terminal_restores_termios_on_corrupted_tty() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: clai-wrap binary unavailable");
        return;
    };
    let Some(pty) = PtyPair::new() else {
        eprintln!("Skipping test: failed to allocate pseudo TTY");
        return;
    };

    let original = get_termios(pty.slave);
    let mut corrupted = original;
    corrupted.c_lflag &= !(libc::ICANON | libc::ECHO);
    set_termios_now(pty.slave, &corrupted);

    let stdin_fd = unsafe { libc::dup(pty.slave) };
    let stdout_fd = unsafe { libc::dup(pty.slave) };
    let stderr_fd = unsafe { libc::dup(pty.slave) };
    assert!(
        stdin_fd >= 0 && stdout_fd >= 0 && stderr_fd >= 0,
        "dup failed"
    );

    let mut child = Command::new(binary)
        .arg("reset-terminal")
        .stdin(unsafe { Stdio::from(std::fs::File::from_raw_fd(stdin_fd)) })
        .stdout(unsafe { Stdio::from(std::fs::File::from_raw_fd(stdout_fd)) })
        .stderr(unsafe { Stdio::from(std::fs::File::from_raw_fd(stderr_fd)) })
        .spawn()
        .expect("spawn clai-wrap reset-terminal");

    let status = child.wait().expect("wait for reset-terminal");
    assert!(status.success(), "reset-terminal should succeed");

    let restored = get_termios(pty.slave);
    assert_eq!(restored.c_lflag & libc::ICANON, libc::ICANON);
    assert_eq!(restored.c_lflag & libc::ECHO, libc::ECHO);
}

// ============================================================================
// Interactive Mode Tests (require TTY)
// ============================================================================

/// Test full mode detection with real TTY.
/// Requires interactive TTY environment.
#[test]
#[ignore]
fn test_interactive_mode_detection() {
    // In a real TTY, detect_tty should return all true
    let status = detect_tty();

    // When running interactively, all should be TTY
    assert!(
        status.stdin,
        "stdin should be TTY when running interactively"
    );
    assert!(
        status.stdout,
        "stdout should be TTY when running interactively"
    );
    assert!(
        status.stderr,
        "stderr should be TTY when running interactively"
    );

    // And passthrough should not be needed
    let reason = should_use_passthrough();
    assert!(
        matches!(reason, PassthroughReason::NotNeeded),
        "With TTY, should not need passthrough: {:?}",
        reason
    );
}

/// Test passthrough mode with real shell.
/// Requires interactive TTY environment.
#[test]
#[ignore]
#[cfg(unix)]
fn test_interactive_passthrough_mode() {
    use clai_wrap::passthrough::PassthroughMode;

    let path = PathBuf::from("/bin/sh");

    // Create passthrough mode
    let result = PassthroughMode::new(&path);
    assert!(result.is_ok(), "Should create passthrough mode");

    let mut mode = result.unwrap();

    // Get child PID
    let pid = mode.child_pid();
    assert!(pid.is_some(), "Should have child PID");
    assert!(pid.unwrap() > 0);

    // Get shutdown flag
    let flag = mode.shutdown_flag();
    assert!(!flag.load(std::sync::atomic::Ordering::Relaxed));

    // Kill the child
    let _ = mode.kill();
}

/// Test full workflow: detect mode -> enter standalone -> use picker.
/// Requires interactive TTY environment.
#[test]
#[ignore]
fn test_interactive_full_workflow() {
    use std::io::Write;

    // 1. Check if we need passthrough
    let passthrough_reason = should_use_passthrough();
    println!("Passthrough reason: {:?}", passthrough_reason);

    // 2. Enter standalone mode (simulating daemon unavailable)
    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);
    state.log_warning();

    // 3. Create some test history
    let mut file = NamedTempFile::with_suffix(".bash_history").unwrap();
    writeln!(file, "git status").unwrap();
    writeln!(file, "cargo build").unwrap();
    writeln!(file, "cargo test").unwrap();
    file.flush().unwrap();

    state.load_history_from(file.path()).unwrap();

    // 4. Create and use picker
    let mut picker = state.create_picker();
    assert!(!picker.is_empty());

    // 5. Filter
    picker.update_query("cargo");
    assert_eq!(picker.filtered_count(), 2);

    // 6. Select
    let selected = picker.selected_item().expect("Should have selection");
    println!("Selected: {}", selected.text);
    assert!(selected.text.contains("cargo"));

    // 7. Feature check
    assert!(state.feature_available(Feature::Picker));
    assert!(!state.feature_available(Feature::AiSuggestions));
}
