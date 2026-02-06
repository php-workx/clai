//! clai-wrap: PTY wrapper for intelligent terminal assistance
//!
//! This binary wraps the user's shell in a pseudo-terminal to provide
//! intelligent command suggestions, history search, and autocomplete features.

use std::io::{Read, Write};
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result};
use clai_wrap::cli::{Cli, Commands, OperationMode};
use clai_wrap::denylist::Denylist;
use clai_wrap::hotkey::{HotkeyConfig, HotkeyParser};
use clai_wrap::osc133::Osc133Parser;
use clai_wrap::pty_host::PtyHost;
use clai_wrap::ring_buffer::SpscRingBuffer;
use clai_wrap::selection_inject::SelectionInjector;
#[cfg(unix)]
use clai_wrap::standalone::{StandaloneReason, StandaloneState};
use tracing::{debug, error, info, warn, Level};
use tracing_subscriber::EnvFilter;

#[cfg(unix)]
use clai_wrap::raw_mode::enter_raw_mode;
#[cfg(unix)]
use clai_wrap::resize::ResizeHandler;
#[cfg(unix)]
use clai_wrap::signals::{SignalEvent, SignalHandler};

/// Default buffer capacity for output capture (4 MiB)
const DEFAULT_BUFFER_CAP: usize = 4 * 1024 * 1024;

/// I/O buffer size for reads
const IO_BUFFER_SIZE: usize = 4096;

/// Warning printed when clai-wrap is started from inside another clai-wrap session.
const NESTED_WRAPPER_WARNING: &str = "clai-wrap: nested wrapper detected (CLAI_WRAP already set)";

fn main() -> Result<()> {
    // Parse CLI arguments
    let cli = Cli::parse_args();

    // Validate arguments
    cli.validate().context("Invalid command line arguments")?;

    // Initialize logging based on debug flag
    init_logging(&cli)?;

    // Handle subcommands
    if let Some(ref command) = cli.command {
        return handle_subcommand(command);
    }

    // Log startup information
    info!("clai-wrap starting in {} mode", cli.operation_mode());
    debug!("Shell: {:?}", cli.shell_path());
    debug!("Login shell: {}", cli.login_shell);
    debug!("Buffer capacity: {} bytes", cli.buffer_cap);
    debug!("Daemon enabled: {}", cli.daemon_enabled());
    debug!("UI enabled: {}", cli.ui_enabled());
    if let Some(warning) = nested_wrapper_warning_message() {
        eprintln!("{warning}");
    }

    // Run the main wrapper logic based on operation mode
    match cli.operation_mode() {
        OperationMode::Full => {
            info!("Running in full mode with daemon connection");
            run_full_mode(&cli)
        }
        OperationMode::Standalone => {
            info!("Running in standalone mode (no daemon)");
            run_standalone_mode(&cli)
        }
        OperationMode::Passthrough => {
            info!("Running in passthrough mode");
            run_passthrough_mode(&cli)
        }
    }
}

/// Initialize logging based on CLI options
fn init_logging(cli: &Cli) -> Result<()> {
    let level = if cli.is_debug() {
        Level::DEBUG
    } else {
        Level::WARN
    };

    let filter = EnvFilter::from_default_env().add_directive(level.into());

    tracing_subscriber::fmt()
        .with_env_filter(filter)
        .with_writer(std::io::stderr)
        .try_init()
        .ok(); // Ignore error if already initialized

    Ok(())
}

/// Handle subcommands (version, reset-terminal)
fn handle_subcommand(command: &Commands) -> Result<()> {
    match command {
        Commands::Version => {
            println!("clai-wrap {}", env!("CARGO_PKG_VERSION"));
            Ok(())
        }
        Commands::ResetTerminal => {
            reset_terminal()?;
            println!("Terminal state reset");
            Ok(())
        }
    }
}

/// Reset terminal to a sane state
fn reset_terminal() -> Result<()> {
    let mut stdout = std::io::stdout();

    // Reset terminal modes
    write!(stdout, "\x1b[?25h")?; // Show cursor
    write!(stdout, "\x1b[?1049l")?; // Exit alternate screen
    write!(stdout, "\x1b[0m")?; // Reset attributes
    write!(stdout, "\x1b[?7h")?; // Enable line wrap
    write!(stdout, "\x1b[2J")?; // Clear screen
    write!(stdout, "\x1b[H")?; // Cursor home

    stdout.flush()?;

    #[cfg(unix)]
    {
        let _ = std::process::Command::new("stty").arg("sane").status();
    }

    Ok(())
}

/// Run in full mode with daemon connection and all features
fn run_full_mode(cli: &Cli) -> Result<()> {
    // For now, full mode falls back to standalone since daemon isn't implemented
    debug!("Full mode configuration:");
    debug!("  Daemon socket: {:?}", cli.daemon_socket);
    debug!("  Daemon timeout: {}ms", cli.daemon_timeout);
    debug!("  Hotkey: {:?}", cli.hotkey);
    debug!("  Hotkey timeout: {}ms", cli.hotkey_timeout);

    warn!("Daemon connection not yet implemented, running in standalone mode");
    run_standalone_mode(cli)
}

/// Run in standalone mode without daemon connection
#[cfg(unix)]
fn run_standalone_mode(cli: &Cli) -> Result<()> {
    // Get shell path
    let shell_path = cli.shell_path();
    debug!("Using shell: {:?}", shell_path);

    let standalone_state = init_standalone_history(cli, &shell_path)?;
    if standalone_state.has_history() {
        debug!(
            "Loaded {} history entries from {:?}",
            standalone_state.history_count(),
            standalone_state.history_path()
        );
    }

    // Initialize denylist for privacy gate
    let _denylist = Denylist::with_defaults();

    // Create PTY and spawn shell
    let mut pty_host = PtyHost::new_with_login(Some(shell_path.clone()), cli.login_shell)
        .context("Failed to create PTY")?;

    info!("Shell spawned with PID: {:?}", pty_host.child_pid());

    // Get PTY reader and writer
    let mut pty_reader = pty_host.reader().context("Failed to get PTY reader")?;
    let pty_writer = pty_host.writer().context("Failed to get PTY writer")?;
    let pty_writer = Arc::new(std::sync::Mutex::new(pty_writer));

    // Enter raw mode
    let _raw_guard = enter_raw_mode().context("Failed to enter raw mode")?;

    // Install signal handlers
    let signal_handler = SignalHandler::new().context("Failed to install signal handlers")?;

    // Create resize handler
    let resize_handler = Arc::new(ResizeHandler::new());

    // Create hotkey parser
    let hotkey_config = HotkeyConfig {
        timeout: Duration::from_millis(cli.hotkey_timeout),
        ..Default::default()
    };
    let mut _hotkey_parser = HotkeyParser::with_config(hotkey_config);

    // Create OSC 133 parser for command tracking
    let mut osc133_parser = Osc133Parser::new();

    // Create output capture buffer
    let buffer_cap = if cli.buffer_cap > 0 {
        cli.buffer_cap
    } else {
        DEFAULT_BUFFER_CAP
    };
    let mut output_buffer = SpscRingBuffer::new(buffer_cap);

    // Create selection injector
    let mut _selection_injector = SelectionInjector::new();

    // Shutdown flag
    let shutdown = Arc::new(AtomicBool::new(false));
    let shutdown_clone = Arc::clone(&shutdown);

    // Start stdin reader thread
    let pty_writer_clone = Arc::clone(&pty_writer);
    let stdin_thread = thread::spawn(move || {
        let mut stdin = std::io::stdin();
        let mut buf = [0u8; IO_BUFFER_SIZE];

        while !shutdown_clone.load(Ordering::SeqCst) {
            match stdin.read(&mut buf) {
                Ok(0) => break, // EOF
                Ok(n) => {
                    if let Ok(mut writer) = pty_writer_clone.lock() {
                        if writer.write_all(&buf[..n]).is_err() {
                            break;
                        }
                        let _ = writer.flush();
                    }
                }
                Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
                Err(_) => break,
            }
        }
    });

    // Main event loop
    let mut stdout = std::io::stdout();
    let mut pty_buf = [0u8; IO_BUFFER_SIZE];
    let picker_open = false;

    loop {
        // Check for signals
        if let Ok(Some(event)) = signal_handler.try_recv() {
            match event {
                SignalEvent::Resize => {
                    // Get terminal size and resize PTY
                    if let Some((cols, rows)) = get_terminal_size() {
                        resize_handler.on_resize(cols, rows);
                    }
                }
                SignalEvent::ChildExit => {
                    debug!("Child process exited");
                    break;
                }
                SignalEvent::Interrupt => {
                    // Forward Ctrl-C to child (already handled by PTY)
                    debug!("Received SIGINT");
                }
                SignalEvent::Terminate | SignalEvent::Hangup => {
                    debug!("Received termination signal");
                    break;
                }
                SignalEvent::Suspend => {
                    debug!("Received SIGTSTP");
                    if picker_open {
                        // When picker UI is integrated, close it here before suspend
                        // to avoid corrupted terminal state on resume
                    }
                }
                SignalEvent::Continue => {
                    // Re-enter raw mode handled automatically
                    debug!("Resumed from suspend");
                }
            }
        }

        // Check resize debounce timer
        if let Some((cols, rows)) = resize_handler.tick() {
            if let Err(e) = pty_host.resize(cols, rows) {
                warn!("Failed to resize PTY: {}", e);
            }
        }

        // Check for child exit
        if let Ok(Some(status)) = pty_host.try_wait() {
            info!("Shell exited with status: {:?}", status.code());
            shutdown.store(true, Ordering::SeqCst);

            // Return the shell's exit code
            std::process::exit(status.as_exit_code());
        }

        // Read from PTY (non-blocking would be better, but we'll use a small timeout)
        match pty_reader.read(&mut pty_buf) {
            Ok(0) => {
                debug!("PTY EOF");
                break;
            }
            Ok(n) => {
                let data = &pty_buf[..n];

                // Process through OSC 133 parser
                osc133_parser.process_bytes(data);

                // Store in output buffer (for future AI analysis)
                if !picker_open {
                    output_buffer.push(data);
                }

                // Forward to stdout
                if !picker_open {
                    stdout.write_all(data)?;
                    stdout.flush()?;
                }
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                // No data available, continue
            }
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {
                continue;
            }
            Err(e) => {
                error!("PTY read error: {}", e);
                break;
            }
        }

        // Small sleep to prevent busy loop
        thread::sleep(Duration::from_millis(1));
    }

    // Cleanup
    shutdown.store(true, Ordering::SeqCst);
    let _ = stdin_thread.join();

    Ok(())
}

#[cfg(not(unix))]
fn run_standalone_mode(cli: &Cli) -> Result<()> {
    // Windows standalone mode - simplified for now
    warn!("Standalone mode on Windows not fully implemented");
    run_passthrough_mode(cli)
}

/// Run in passthrough mode (pure PTY forwarding, no hotkeys or picker)
#[cfg(unix)]
fn run_passthrough_mode(cli: &Cli) -> Result<()> {
    use clai_wrap::passthrough::{check_passthrough_needed, PassthroughMode};

    let shell_path = cli.shell_path();

    // Check if we should warn about passthrough
    let reason = check_passthrough_needed(Some(&shell_path));
    if reason.needs_passthrough() {
        debug!("Passthrough mode reason: {}", reason);
    }

    // Create and run passthrough mode
    let mut passthrough =
        PassthroughMode::new(&shell_path).context("Failed to create passthrough mode")?;

    let status = passthrough.run().context("Passthrough mode failed")?;

    std::process::exit(status.as_exit_code());
}

#[cfg(not(unix))]
fn run_passthrough_mode(cli: &Cli) -> Result<()> {
    let shell_path = cli.shell_path();

    // On Windows, just spawn the shell directly
    let status = std::process::Command::new(&shell_path)
        .status()
        .context("Failed to spawn shell")?;

    std::process::exit(status.code().unwrap_or(1));
}

/// Get current terminal size
#[cfg(unix)]
fn get_terminal_size() -> Option<(u16, u16)> {
    use libc::{ioctl, winsize, STDOUT_FILENO, TIOCGWINSZ};

    unsafe {
        let mut ws: winsize = std::mem::zeroed();
        if ioctl(STDOUT_FILENO, TIOCGWINSZ, &mut ws) == 0 {
            Some((ws.ws_col, ws.ws_row))
        } else {
            None
        }
    }
}

#[cfg(not(unix))]
fn get_terminal_size() -> Option<(u16, u16)> {
    // Use crossterm for Windows
    crossterm::terminal::size().ok()
}

fn nested_wrapper_warning_message() -> Option<&'static str> {
    if std::env::var_os("CLAI_WRAP").is_some() {
        Some(NESTED_WRAPPER_WARNING)
    } else {
        None
    }
}

#[cfg(unix)]
fn init_standalone_history(cli: &Cli, shell_path: &Path) -> Result<StandaloneState> {
    let mut state = StandaloneState::new(StandaloneReason::DaemonUnavailable);

    if let Some(history_path) = cli.history_file.as_deref() {
        state
            .load_history_from(history_path)
            .with_context(|| format!("failed to load history file {:?}", history_path))?;
        return Ok(state);
    }

    if let Some(shell_name) = shell_path.file_name().and_then(|name| name.to_str()) {
        if let Err(err) = state.init_history(shell_name) {
            debug!("No local history loaded for shell {shell_name}: {err}");
        }
    }

    Ok(state)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(unix)]
    use std::path::Path;
    use std::sync::Mutex;
    use tempfile::NamedTempFile;

    static ENV_LOCK: Mutex<()> = Mutex::new(());

    #[test]
    fn test_nested_wrapper_warning_message_set() {
        let _lock = ENV_LOCK.lock().expect("lock env");
        std::env::set_var("CLAI_WRAP", "1");
        let warning = nested_wrapper_warning_message();
        std::env::remove_var("CLAI_WRAP");

        assert_eq!(
            warning,
            Some("clai-wrap: nested wrapper detected (CLAI_WRAP already set)")
        );
    }

    #[test]
    fn test_nested_wrapper_warning_message_unset() {
        let _lock = ENV_LOCK.lock().expect("lock env");
        std::env::remove_var("CLAI_WRAP");
        let warning = nested_wrapper_warning_message();
        assert!(warning.is_none());
    }

    #[test]
    #[cfg(unix)]
    fn test_init_standalone_history_uses_history_file_cli_option() {
        let mut history = NamedTempFile::new().expect("create temp history");
        use std::io::Write as _;
        writeln!(history, "git status").expect("write history entry");
        writeln!(history, "cargo test").expect("write history entry");
        history.flush().expect("flush history file");

        let cli = Cli::parse_from_args([
            "clai-wrap",
            "--history-file",
            history.path().to_str().expect("utf8 history path"),
        ]);

        let state = init_standalone_history(&cli, Path::new("/bin/bash"))
            .expect("initialize standalone history");

        assert!(state.has_history(), "history should be loaded from --history-file");
        assert_eq!(state.history_count(), 2);
        assert_eq!(state.history_path(), Some(history.path()));
    }
}
