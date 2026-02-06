//! End-to-end PTY spawning and I/O tests for clai-wrap.
//!
//! These tests verify:
//! - Shell spawning and basic I/O passthrough
//! - Resize propagation to child PTY
//! - Environment variable inheritance (CLAI_WRAP=1)
//! - Exit status propagation
//! - Non-blocking I/O behavior
//!
//! Tests marked with `#[ignore]` require an interactive TTY environment
//! and should be run manually with `cargo test -- --ignored`.

use std::io::{Read, Write};
use std::path::PathBuf;
use std::time::{Duration, Instant};

use portable_pty::{native_pty_system, CommandBuilder, PtySize};
use tempfile::NamedTempFile;

#[cfg(unix)]
use std::fs;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;

/// Default timeout for PTY operations.
const PTY_TIMEOUT: Duration = Duration::from_secs(5);

/// Buffer size for reading PTY output.
const READ_BUFFER_SIZE: usize = 4096;

/// Default PTY size for tests.
fn default_pty_size() -> PtySize {
    PtySize {
        rows: 24,
        cols: 80,
        pixel_width: 0,
        pixel_height: 0,
    }
}

/// Helper to read output from PTY until a marker is found or timeout.
fn read_until_marker(
    reader: &mut dyn Read,
    marker: &str,
    timeout: Duration,
) -> Result<String, String> {
    let mut output = String::new();
    let mut buf = [0u8; READ_BUFFER_SIZE];
    let start = Instant::now();

    while start.elapsed() < timeout {
        match reader.read(&mut buf) {
            Ok(0) => break, // EOF
            Ok(n) => {
                output.push_str(&String::from_utf8_lossy(&buf[..n]));
                if output.contains(marker) {
                    return Ok(output);
                }
            }
            Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                std::thread::sleep(Duration::from_millis(10));
                continue;
            }
            Err(ref e) if e.kind() == std::io::ErrorKind::Interrupted => {
                continue;
            }
            Err(e) => {
                return Err(format!("Read error: {e}"));
            }
        }
    }

    if output.contains(marker) {
        Ok(output)
    } else {
        Err(format!(
            "Timeout waiting for marker '{}'. Got: {}",
            marker, output
        ))
    }
}

/// Helper to drain remaining output from PTY (non-blocking).
fn drain_output(reader: &mut dyn Read, max_duration: Duration) -> String {
    let mut output = String::new();
    let mut buf = [0u8; READ_BUFFER_SIZE];
    let start = Instant::now();

    while start.elapsed() < max_duration {
        match reader.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => {
                output.push_str(&String::from_utf8_lossy(&buf[..n]));
            }
            Err(ref e)
                if e.kind() == std::io::ErrorKind::WouldBlock
                    || e.kind() == std::io::ErrorKind::Interrupted =>
            {
                std::thread::sleep(Duration::from_millis(10));
                // If we have output and hit a block, we're probably done
                if !output.is_empty() {
                    break;
                }
                continue;
            }
            Err(_) => break,
        }
    }

    output
}

fn wait_for_exit<C: portable_pty::Child + ?Sized>(
    child: &mut C,
    timeout: Duration,
) -> Option<portable_pty::ExitStatus> {
    let start = Instant::now();
    while start.elapsed() < timeout {
        match child.try_wait() {
            Ok(Some(status)) => return Some(status),
            Ok(None) => std::thread::sleep(Duration::from_millis(10)),
            Err(_) => return None,
        }
    }
    None
}

fn wait_for_exit_or_kill<C: portable_pty::Child + ?Sized>(
    child: &mut C,
    timeout: Duration,
) -> portable_pty::ExitStatus {
    if let Some(status) = wait_for_exit(child, timeout) {
        return status;
    }

    let _ = child.kill();
    child.wait().expect("wait child after timeout/kill")
}

#[cfg(unix)]
fn clai_wrap_binary() -> Option<PathBuf> {
    for key in ["CARGO_BIN_EXE_clai-wrap", "CARGO_BIN_EXE_clai_wrap"] {
        if let Some(path) = std::env::var_os(key).map(PathBuf::from) {
            if path.exists() {
                return Some(path);
            }
        }
    }

    // Fallback for environments that don't set CARGO_BIN_EXE_* for integration tests.
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
fn create_shell_script(content: &str) -> NamedTempFile {
    let script = NamedTempFile::new().expect("create temp shell script");
    fs::write(script.path(), content).expect("write shell script");
    fs::set_permissions(script.path(), fs::Permissions::from_mode(0o755))
        .expect("set script executable");
    script
}

#[cfg(unix)]
fn spawn_clai_wrap_shell() -> Option<(
    Box<dyn portable_pty::MasterPty + Send>,
    Box<dyn portable_pty::Child + Send + Sync>,
)> {
    let binary = clai_wrap_binary()?;
    let pty_system = native_pty_system();
    let pair = pty_system.openpty(default_pty_size()).ok()?;

    let mut cmd = CommandBuilder::new(binary);
    cmd.args([
        "--standalone",
        "--shell",
        "/bin/sh",
        "--login-shell",
        "false",
        "--no-ui",
    ]);

    let child = pair.slave.spawn_command(cmd).ok()?;
    Some((pair.master, child))
}

#[cfg(unix)]
fn spawn_clai_wrap_bash_shell() -> Option<(
    Box<dyn portable_pty::MasterPty + Send>,
    Box<dyn portable_pty::Child + Send + Sync>,
)> {
    let binary = clai_wrap_binary()?;
    if !PathBuf::from("/bin/bash").exists() {
        return None;
    }

    let pty_system = native_pty_system();
    let pair = pty_system.openpty(default_pty_size()).ok()?;

    let mut cmd = CommandBuilder::new(binary);
    cmd.args([
        "--standalone",
        "--shell",
        "/bin/bash",
        "--login-shell",
        "false",
        "--no-ui",
    ]);

    let child = pair.slave.spawn_command(cmd).ok()?;
    Some((pair.master, child))
}

// ============================================================================
// Basic PTY Spawning Tests
// ============================================================================

#[test]
fn test_pty_spawn_echo_command() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new("echo");
    cmd.arg("hello_e2e_test");

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn echo");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output = read_until_marker(&mut *reader, "hello_e2e_test", PTY_TIMEOUT)
        .expect("Failed to read output");

    assert!(
        output.contains("hello_e2e_test"),
        "Expected output to contain 'hello_e2e_test', got: {}",
        output
    );

    let status = child.wait().expect("Failed to wait for child");
    assert!(status.success(), "Echo command should succeed");
}

#[test]
fn test_pty_spawn_with_exit_code() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    #[cfg(unix)]
    let cmd = {
        let mut cmd = CommandBuilder::new("sh");
        cmd.args(["-c", "exit 42"]);
        cmd
    };

    #[cfg(windows)]
    let cmd = {
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", "exit 42"]);
        cmd
    };

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");
    let status = child.wait().expect("Failed to wait");

    assert!(!status.success(), "Exit code 42 should not be success");
    assert_eq!(status.exit_code(), 42u32, "Exit code should be 42");
}

#[test]
fn test_pty_io_passthrough_basic() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    // Use cat to echo back input
    #[cfg(unix)]
    let cmd = {
        let cmd = CommandBuilder::new("cat");
        cmd
    };

    #[cfg(windows)]
    let cmd = {
        // Windows doesn't have cat, use findstr with /v flag to output all lines
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", "findstr ."]);
        cmd
    };

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn cat");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");
    let mut writer = pair.master.take_writer().expect("Failed to get writer");

    // Write test input
    let test_input = "e2e_passthrough_test\n";
    writer
        .write_all(test_input.as_bytes())
        .expect("Failed to write");
    writer.flush().expect("Failed to flush");

    // Read output (cat should echo it back)
    let output = read_until_marker(&mut *reader, "e2e_passthrough_test", PTY_TIMEOUT);

    // Kill the child
    let _ = child.kill();
    let _ = child.wait();

    assert!(
        output.is_ok(),
        "Should read echoed content: {:?}",
        output.err()
    );
}

#[test]
fn test_pty_resize() {
    let pty_system = native_pty_system();

    let initial_size = PtySize {
        rows: 24,
        cols: 80,
        pixel_width: 0,
        pixel_height: 0,
    };

    let pair = pty_system
        .openpty(initial_size)
        .expect("Failed to create PTY");

    // Resize to new dimensions
    let new_size = PtySize {
        rows: 40,
        cols: 120,
        pixel_width: 0,
        pixel_height: 0,
    };

    let result = pair.master.resize(new_size);
    assert!(result.is_ok(), "Resize should succeed: {:?}", result.err());

    // Resize to minimum valid dimensions
    let min_size = PtySize {
        rows: 1,
        cols: 1,
        pixel_width: 0,
        pixel_height: 0,
    };

    let result = pair.master.resize(min_size);
    assert!(
        result.is_ok(),
        "Resize to minimum should succeed: {:?}",
        result.err()
    );
}

#[test]
fn test_pty_rapid_resize() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    // Spawn a simple command that exits quickly
    #[cfg(unix)]
    let cmd = {
        let mut cmd = CommandBuilder::new("sh");
        cmd.args(["-c", "exit 0"]);
        cmd
    };

    #[cfg(windows)]
    let cmd = {
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", "exit 0"]);
        cmd
    };

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

    // Perform rapid resizes (10 in 100ms as per spec)
    for i in 0..10 {
        let size = PtySize {
            rows: 20 + (i % 10) as u16,
            cols: 80 + (i % 20) as u16,
            pixel_width: 0,
            pixel_height: 0,
        };
        let result = pair.master.resize(size);
        assert!(
            result.is_ok(),
            "Rapid resize {} should succeed: {:?}",
            i,
            result.err()
        );
        std::thread::sleep(Duration::from_millis(10));
    }

    // Wait for command to complete with timeout
    let start = Instant::now();
    while start.elapsed() < Duration::from_secs(2) {
        if let Ok(Some(_)) = child.try_wait() {
            break;
        }
        std::thread::sleep(Duration::from_millis(10));
    }

    // Kill if still running
    let _ = child.kill();
    let _ = child.wait();
}

// ============================================================================
// Environment Variable Tests
// ============================================================================

#[test]
fn test_pty_clai_wrap_env_var() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    #[cfg(unix)]
    let mut cmd = {
        let mut cmd = CommandBuilder::new("sh");
        cmd.args(["-c", "echo CLAI_WRAP=$CLAI_WRAP"]);
        cmd
    };

    #[cfg(windows)]
    let mut cmd = {
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", "echo CLAI_WRAP=%CLAI_WRAP%"]);
        cmd
    };

    // Set the CLAI_WRAP environment variable
    cmd.env("CLAI_WRAP", "1");

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output =
        read_until_marker(&mut *reader, "CLAI_WRAP=1", PTY_TIMEOUT).expect("Failed to read output");

    let _ = child.wait();

    assert!(
        output.contains("CLAI_WRAP=1"),
        "Expected CLAI_WRAP=1 in output, got: {}",
        output
    );
}

#[test]
fn test_pty_env_inheritance() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let test_var = format!("CLAI_E2E_TEST_VAR_{}", std::process::id());
    let test_value = "e2e_test_value_42";

    #[cfg(unix)]
    let mut cmd = {
        let mut cmd = CommandBuilder::new("sh");
        cmd.args(["-c", &format!("echo ${test_var}")]);
        cmd
    };

    #[cfg(windows)]
    let mut cmd = {
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", &format!("echo %{test_var}%")]);
        cmd
    };

    cmd.env(&test_var, test_value);

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output =
        read_until_marker(&mut *reader, test_value, PTY_TIMEOUT).expect("Failed to read output");

    let _ = child.wait();

    assert!(
        output.contains(test_value),
        "Expected test value in output, got: {}",
        output
    );
}

// ============================================================================
// Shell Spawning Tests (require shell to be installed)
// ============================================================================

#[test]
#[cfg(unix)]
fn test_pty_spawn_sh_shell() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new("/bin/sh");
    cmd.args(["-c", "echo shell_spawn_test && exit 0"]);

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn shell");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output = read_until_marker(&mut *reader, "shell_spawn_test", PTY_TIMEOUT)
        .expect("Failed to read output");

    let status = child.wait().expect("Failed to wait");

    assert!(output.contains("shell_spawn_test"), "Output: {}", output);
    assert!(status.success());
}

#[test]
#[cfg(unix)]
fn test_pty_spawn_bash_if_available() {
    // Check if bash is available
    let bash_path = PathBuf::from("/bin/bash");
    if !bash_path.exists() {
        eprintln!("Skipping test: bash not found at /bin/bash");
        return;
    }

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new("/bin/bash");
    cmd.args(["-c", "echo bash_e2e_test"]);

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn bash");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output = read_until_marker(&mut *reader, "bash_e2e_test", PTY_TIMEOUT)
        .expect("Failed to read output");

    let _ = child.wait();

    assert!(output.contains("bash_e2e_test"), "Output: {}", output);
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_login_shell_disabled_does_not_pass_l_flag() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: CARGO_BIN_EXE_clai-wrap not available");
        return;
    };

    let script = create_shell_script("#!/bin/sh\nprintf 'ARGC=%s\\nARG1=%s\\n' \"$#\" \"$1\"\n");

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new(binary);
    cmd.args([
        "--standalone",
        "--shell",
        script.path().to_str().expect("utf8 script path"),
        "--login-shell",
        "false",
    ]);

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn clai-wrap");
    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output = read_until_marker(&mut *reader, "ARGC=", PTY_TIMEOUT).expect("Failed to read");
    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(5));

    assert!(
        output.contains("ARGC=0"),
        "Expected no extra shell args, got output: {output}"
    );
    assert!(
        status.exit_code() == 0 || status.exit_code() == 1,
        "Expected normal shell termination, got code {}",
        status.exit_code()
    );
}

#[test]
#[cfg(unix)]
#[ignore = "Can block in PTY read loop on some runners; covered by unit tests for warning message"]
fn test_clai_wrap_warns_on_nested_wrapper_env() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: CARGO_BIN_EXE_clai-wrap not available");
        return;
    };

    let script = create_shell_script("#!/bin/sh\necho nested_warning_probe\n");

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new(binary);
    cmd.args([
        "--standalone",
        "--shell",
        script.path().to_str().expect("utf8 script path"),
        "--login-shell",
        "false",
    ]);
    cmd.env("CLAI_WRAP", "1");

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn clai-wrap");
    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output = read_until_marker(
        &mut *reader,
        "clai-wrap: nested wrapper detected (CLAI_WRAP already set)",
        PTY_TIMEOUT,
    )
    .expect("Failed to read nested warning");
    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(5));

    assert!(
        output.contains("clai-wrap: nested wrapper detected (CLAI_WRAP already set)"),
        "Expected nested warning in output, got: {output}"
    );
    assert!(status.success(), "Expected successful exit");
}

#[test]
#[cfg(unix)]
#[ignore = "Can block in PTY read loop on some runners; behavior covered by CLI/unit tests"]
fn test_clai_wrap_fails_fast_with_invalid_history_file() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: CARGO_BIN_EXE_clai-wrap not available");
        return;
    };

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new(binary);
    cmd.args([
        "--standalone",
        "--history-file",
        "/definitely/nonexistent/history-for-e2e-test",
    ]);

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn clai-wrap");
    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    let output = read_until_marker(&mut *reader, "failed to load history file", PTY_TIMEOUT)
        .expect("Failed to read error output");
    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(5));

    assert!(
        output.contains("failed to load history file"),
        "Expected history file load error, got output: {output}"
    );
    assert!(!status.success(), "Expected non-zero exit for invalid history file");
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_io_simple_echo_passthrough() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    writer
        .write_all(b"echo io_passthrough_ok\n")
        .expect("Failed to write");
    writer.flush().expect("Failed to flush");

    let output =
        read_until_marker(&mut *reader, "io_passthrough_ok", PTY_TIMEOUT).expect("read output");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output.contains("io_passthrough_ok"), "Output: {output}");
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_io_interactive_read_input() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    writer
        .write_all(b"printf 'PROMPT:'; read value; echo VALUE:$value\n")
        .expect("Failed to write command");
    writer.flush().expect("Failed to flush command");
    std::thread::sleep(Duration::from_millis(100));

    writer
        .write_all(b"typed_value\n")
        .expect("Failed to write input");
    writer.flush().expect("Failed to flush input");

    let output = read_until_marker(&mut *reader, "VALUE:typed_value", PTY_TIMEOUT)
        .expect("Failed to read interactive output");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output.contains("VALUE:typed_value"), "Output: {output}");
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_io_ansi_color_passthrough() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    writer
        .write_all(b"printf '\\033[31mRED_TEXT\\033[0m\\n'\n")
        .expect("Failed to write command");
    writer.flush().expect("Failed to flush command");

    let output = read_until_marker(&mut *reader, "\u{1b}[31mRED_TEXT", PTY_TIMEOUT)
        .expect("Failed to read ANSI output");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(
        output.contains("\u{1b}[31mRED_TEXT"),
        "Expected ANSI sequence passthrough, got: {output}"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_io_ctrl_c_interrupts_running_command() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    writer.write_all(b"sleep 10\n").expect("write sleep");
    writer.flush().expect("flush sleep");
    std::thread::sleep(Duration::from_millis(200));

    writer.write_all(&[0x03]).expect("write Ctrl-C");
    writer.flush().expect("flush Ctrl-C");
    std::thread::sleep(Duration::from_millis(100));

    writer
        .write_all(b"echo AFTER_CTRL_C\n")
        .expect("write echo after interrupt");
    writer.flush().expect("flush echo after interrupt");

    let output = read_until_marker(&mut *reader, "AFTER_CTRL_C", Duration::from_secs(8))
        .expect("Expected command after interrupt");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output.contains("AFTER_CTRL_C"), "Output: {output}");
}

#[test]
#[cfg(unix)]
#[ignore = "Ctrl-D EOF behavior in standalone wrapper is not stable yet"]
fn test_clai_wrap_io_ctrl_d_sends_eof_and_exits() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut writer = master.take_writer().expect("Failed to get writer");
    writer.write_all(&[0x04]).expect("write Ctrl-D (1)");
    writer.flush().expect("flush Ctrl-D (1)");
    std::thread::sleep(Duration::from_millis(50));
    writer.write_all(&[0x04]).expect("write Ctrl-D (2)");
    writer.flush().expect("flush Ctrl-D (2)");

    let exited = wait_for_exit(&mut *child, Duration::from_secs(3));
    assert!(exited.is_some(), "Expected shell to exit on Ctrl-D");
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_io_tab_completion_completes_filename() {
    let Some((master, mut child)) = spawn_clai_wrap_bash_shell() else {
        eprintln!("Skipping test: clai-wrap binary or /bin/bash unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    let tempdir = tempfile::tempdir().expect("create temp dir");
    let dir = tempdir.path().to_str().expect("utf8 temp dir path");

    writer
        .write_all(format!("cd {dir}\n").as_bytes())
        .expect("write cd");
    writer.flush().expect("flush cd");
    std::thread::sleep(Duration::from_millis(100));

    writer
        .write_all(b"printf 'TAB_COMPLETION_OK\\n' > completion_target_file\n")
        .expect("write file creation");
    writer.flush().expect("flush file creation");
    std::thread::sleep(Duration::from_millis(100));

    writer
        .write_all(b"cat completion_target_f\t\n")
        .expect("write tab completion command");
    writer.flush().expect("flush tab completion command");

    let output = read_until_marker(&mut *reader, "TAB_COMPLETION_OK", Duration::from_secs(8))
        .expect("Expected filename tab completion to work");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(
        output.contains("TAB_COMPLETION_OK"),
        "Expected tab-completed file read output, got: {output}"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_io_line_editing_arrows_backspace_ctrl_a_ctrl_e() {
    let Some((master, mut child)) = spawn_clai_wrap_bash_shell() else {
        eprintln!("Skipping test: clai-wrap binary or /bin/bash unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    writer.write_all(b"echo helo").expect("write line-edit test");
    writer
        .write_all(b"\x1b[D\x08ll\n")
        .expect("write arrow/backspace edits");
    writer.flush().expect("flush arrow/backspace edits");
    let output_hello = read_until_marker(&mut *reader, "hello", Duration::from_secs(8))
        .expect("Expected edited line to become 'hello'");

    writer
        .write_all(b"echo core\x01printf 'CTRL_A_OK '; \x05; echo CTRL_E_OK\n")
        .expect("write ctrl-a/ctrl-e edits");
    writer.flush().expect("flush ctrl-a/ctrl-e edits");
    let output_ctrl = read_until_marker(&mut *reader, "CTRL_E_OK", Duration::from_secs(8))
        .expect("Expected ctrl-a/ctrl-e edited command output");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output_hello.contains("hello"), "Output: {output_hello}");
    assert!(
        output_ctrl.contains("CTRL_A_OK") && output_ctrl.contains("CTRL_E_OK"),
        "Expected ctrl-a/ctrl-e markers, got: {output_ctrl}"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_signal_sigpipe_does_not_break_shell() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    writer
        .write_all(b"yes | head -n 1 >/dev/null; echo AFTER_SIGPIPE\n")
        .expect("write pipeline");
    writer.flush().expect("flush pipeline");

    let output = read_until_marker(&mut *reader, "AFTER_SIGPIPE", Duration::from_secs(8))
        .expect("Expected shell to continue after SIGPIPE in pipeline");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output.contains("AFTER_SIGPIPE"), "Output: {output}");
}

#[test]
#[cfg(unix)]
#[ignore = "Known behavior: standalone wrapper does not currently propagate child non-zero exit code"]
fn test_clai_wrap_signal_child_exit_code_passthrough() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut writer = master.take_writer().expect("Failed to get writer");
    writer.write_all(b"exit 42\n").expect("write exit code");
    writer.flush().expect("flush exit code");

    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(3));
    assert_eq!(
        status.exit_code(),
        42,
        "Expected clai-wrap to propagate child exit code"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_signal_child_killed_by_signal_exits_nonzero() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut writer = master.take_writer().expect("Failed to get writer");
    writer
        .write_all(b"kill -9 $$\n")
        .expect("write self-kill command");
    writer.flush().expect("flush self-kill command");

    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(5));
    assert!(
        !status.success(),
        "Expected non-zero exit when child shell is killed by signal"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_signal_sigterm_from_outside_exits() {
    let Some((_master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    std::thread::sleep(Duration::from_millis(150));

    let Some(pid) = child.process_id() else {
        eprintln!("Skipping test: no child pid available");
        return;
    };

    let kill_status = std::process::Command::new("kill")
        .args(["-TERM", &pid.to_string()])
        .status()
        .expect("send SIGTERM");
    assert!(kill_status.success(), "kill -TERM should succeed");

    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(5));
    assert!(
        !status.success(),
        "Expected non-zero exit after external SIGTERM"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_signal_sighup_from_outside_exits() {
    let Some((_master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    std::thread::sleep(Duration::from_millis(150));

    let Some(pid) = child.process_id() else {
        eprintln!("Skipping test: no child pid available");
        return;
    };

    let kill_status = std::process::Command::new("kill")
        .args(["-HUP", &pid.to_string()])
        .status()
        .expect("send SIGHUP");
    assert!(kill_status.success(), "kill -HUP should succeed");

    let status = wait_for_exit_or_kill(&mut *child, Duration::from_secs(5));
    assert!(
        !status.success(),
        "Expected non-zero exit after external SIGHUP"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_resize_propagates_to_child_stty_size() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    master
        .resize(PtySize {
            rows: 41,
            cols: 123,
            pixel_width: 0,
            pixel_height: 0,
        })
        .expect("resize pty");

    writer.write_all(b"stty size\n").expect("write stty size");
    writer.flush().expect("flush stty size");

    let output = read_until_marker(&mut *reader, "41 123", Duration::from_secs(8))
        .expect("Expected resized terminal dimensions in child");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output.contains("41 123"), "Output: {output}");
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_resize_rapid_updates_keep_trailing_size() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    for (rows, cols) in [(30, 90), (35, 100), (37, 110), (39, 115), (40, 120)] {
        master
            .resize(PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            })
            .expect("rapid resize step");
        std::thread::sleep(Duration::from_millis(5));
    }

    writer.write_all(b"stty size\n").expect("write stty size");
    writer.flush().expect("flush stty size");

    let output = read_until_marker(&mut *reader, "40 120", Duration::from_secs(8))
        .expect("Expected trailing resize dimensions in child");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(output.contains("40 120"), "Output: {output}");
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_encoding_utf8_passthrough() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    // UTF-8 bytes for "é✓" in octal escapes.
    writer
        .write_all(b"printf 'UTF8_OK:\\303\\251\\342\\234\\223\\n'\n")
        .expect("write utf8 command");
    writer.flush().expect("flush utf8 command");

    let output = read_until_marker(&mut *reader, "UTF8_OK:", Duration::from_secs(8))
        .expect("Expected UTF-8 payload to pass through");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(
        output.contains("UTF8_OK:"),
        "Expected UTF-8 marker in output, got: {output}"
    );
}

#[test]
#[cfg(unix)]
fn test_clai_wrap_encoding_invalid_utf8_bytes_do_not_crash() {
    let Some((master, mut child)) = spawn_clai_wrap_shell() else {
        eprintln!("Skipping test: clai-wrap binary or shell unavailable");
        return;
    };

    let mut reader = master.try_clone_reader().expect("Failed to get reader");
    let mut writer = master.take_writer().expect("Failed to get writer");

    // Emit bytes that are invalid standalone UTF-8 and then verify session stays alive.
    writer
        .write_all(b"printf '\\200\\201INVALID_BYTES\\n'; echo AFTER_INVALID_UTF8\n")
        .expect("write invalid utf8 command");
    writer.flush().expect("flush invalid utf8 command");

    let output = read_until_marker(&mut *reader, "AFTER_INVALID_UTF8", Duration::from_secs(8))
        .expect("Expected shell to continue after invalid UTF-8 bytes");

    let _ = child.kill();
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));

    assert!(
        output.contains("AFTER_INVALID_UTF8"),
        "Expected post-invalid-bytes marker, got: {output}"
    );
}

#[test]
#[cfg(unix)]
#[ignore = "Locale warning behavior for non-UTF8 locales is not implemented/stable yet"]
fn test_clai_wrap_encoding_non_utf8_locale_warning() {
    let Some(binary) = clai_wrap_binary() else {
        eprintln!("Skipping test: clai-wrap binary unavailable");
        return;
    };

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new(binary);
    cmd.args(["--standalone", "--shell", "/bin/sh", "--login-shell", "false"]);
    cmd.env("LANG", "C");
    cmd.env("LC_ALL", "C");

    let mut child = pair.slave.spawn_command(cmd).expect("spawn clai-wrap");
    let mut writer = pair.master.take_writer().expect("writer");
    writer.write_all(b"exit\n").expect("write exit");
    writer.flush().expect("flush exit");
    let _ = wait_for_exit_or_kill(&mut *child, Duration::from_secs(2));
}

// ============================================================================
// Stress Tests
// ============================================================================

#[test]
fn test_pty_high_output_volume() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    // Generate a lot of output
    #[cfg(unix)]
    let cmd = {
        let mut cmd = CommandBuilder::new("sh");
        // Generate ~10KB of output
        cmd.args([
            "-c",
            "for i in $(seq 1 100); do echo \"Line $i: $(printf 'x%.0s' $(seq 1 90))\"; done",
        ]);
        cmd
    };

    #[cfg(windows)]
    let cmd = {
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", "for /L %i in (1,1,100) do @echo Line %i"]);
        cmd
    };

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");

    // Drain all output
    let output = drain_output(&mut *reader, Duration::from_secs(10));

    let status = child.wait().expect("Failed to wait");

    // Should have received significant output
    assert!(output.len() > 1000, "Output length: {}", output.len());
    assert!(status.success());
}

#[test]
fn test_pty_child_pid() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    #[cfg(unix)]
    let cmd = {
        let mut cmd = CommandBuilder::new("sleep");
        cmd.arg("0.1");
        cmd
    };

    #[cfg(windows)]
    let cmd = {
        let mut cmd = CommandBuilder::new("cmd");
        cmd.args(["/c", "timeout /t 1 /nobreak >nul"]);
        cmd
    };

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

    let pid = child.process_id();
    assert!(pid.is_some(), "Child should have a process ID");
    assert!(pid.unwrap() > 0, "Process ID should be positive");

    let _ = child.wait();
}

// ============================================================================
// Interactive Shell Tests (require interactive TTY)
// ============================================================================

/// Test interactive shell session.
/// This test requires an interactive TTY and should be run manually.
#[test]
#[ignore]
fn test_interactive_shell_session() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    #[cfg(unix)]
    let cmd = {
        let mut cmd = CommandBuilder::new("/bin/sh");
        cmd.env("PS1", "$ ");
        cmd
    };

    #[cfg(windows)]
    let cmd = { CommandBuilder::new("cmd") };

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn shell");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");
    let mut writer = pair.master.take_writer().expect("Failed to get writer");

    // Wait for prompt
    std::thread::sleep(Duration::from_millis(500));
    let _ = drain_output(&mut *reader, Duration::from_millis(500));

    // Send a command
    writer
        .write_all(b"echo interactive_test\n")
        .expect("Failed to write");
    writer.flush().expect("Failed to flush");

    // Read response
    let output = read_until_marker(&mut *reader, "interactive_test", PTY_TIMEOUT)
        .expect("Failed to read output");

    // Exit the shell
    writer.write_all(b"exit\n").expect("Failed to write exit");
    writer.flush().expect("Failed to flush");

    let status = child.wait().expect("Failed to wait");

    assert!(
        output.contains("interactive_test"),
        "Output: {}",
        output
    );
    assert!(status.success());
}

/// Test that PTY handles signal-like situations.
/// This test requires an interactive TTY.
#[test]
#[ignore]
#[cfg(unix)]
fn test_pty_signal_handling() {
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(default_pty_size())
        .expect("Failed to create PTY");

    let mut cmd = CommandBuilder::new("sh");
    cmd.args([
        "-c",
        "trap 'echo SIGINT_RECEIVED' INT; sleep 10; echo COMPLETED",
    ]);

    let mut child = pair.slave.spawn_command(cmd).expect("Failed to spawn");

    let mut reader = pair
        .master
        .try_clone_reader()
        .expect("Failed to get reader");
    let mut writer = pair.master.take_writer().expect("Failed to get writer");

    // Give the shell time to set up the trap
    std::thread::sleep(Duration::from_millis(200));

    // Send Ctrl-C (0x03)
    writer.write_all(&[0x03]).expect("Failed to write Ctrl-C");
    writer.flush().expect("Failed to flush");

    // Should see SIGINT_RECEIVED
    let output = read_until_marker(&mut *reader, "SIGINT_RECEIVED", PTY_TIMEOUT);

    let _ = child.kill();
    let _ = child.wait();

    assert!(
        output.is_ok(),
        "Should receive SIGINT handler output: {:?}",
        output.err()
    );
}
