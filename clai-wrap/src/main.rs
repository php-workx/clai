//! clai-wrap: PTY wrapper for intelligent terminal assistance
//!
//! This binary wraps the user's shell in a pseudo-terminal to provide
//! intelligent command suggestions, history search, and autocomplete features.

use std::io::Write;
use std::path::Path;
use std::sync::mpsc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{Context, Result};
#[cfg(unix)]
use clai_wrap::alt_screen::{enter_alt_screen, AltScreenGuard};
#[cfg(unix)]
use clai_wrap::assistant_comment::{CommentManager, CommentRenderer, Shell};
#[cfg(unix)]
use clai_wrap::bracketed_paste::BracketedPasteTracker;
use clai_wrap::cli::{Cli, Commands, OperationMode};
use clai_wrap::config::Config;
#[cfg(unix)]
use clai_wrap::daemon_client::{DaemonClient, DaemonClientError};
#[cfg(unix)]
use clai_wrap::daemon_events::{DaemonEventForwarder, ForwarderConfig};
#[cfg(unix)]
use clai_wrap::echo_gap::EchoGapDetector;
use clai_wrap::hotkey::HotkeyConfig;
#[cfg(unix)]
use clai_wrap::hotkey::CHORD_COMPLETIONS_BYTE;
#[cfg(unix)]
use clai_wrap::input_router::{InputEvent, InputRouter};
#[cfg(unix)]
use clai_wrap::io_threads::{IoEvent, IoThreads};
use clai_wrap::osc133::{Osc133Parser, Osc133State};
#[cfg(unix)]
use clai_wrap::output_capture::OutputCapture;
#[cfg(unix)]
use clai_wrap::picker::Picker;
#[cfg(unix)]
use clai_wrap::process_detect::get_foreground_process_or;
use clai_wrap::pty_host::PtyHost;
use clai_wrap::selection_inject::SelectionInjector;
#[cfg(unix)]
use clai_wrap::standalone::{StandaloneReason, StandaloneState};
#[cfg(unix)]
use clai_wrap::suggestion_receiver::SuggestionReceiver;
#[cfg(unix)]
use clai_wrap::temp_dir::TempDirManager;
#[cfg(unix)]
use ratatui::{backend::CrosstermBackend, Terminal};
#[cfg(unix)]
use std::collections::VecDeque;
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

#[cfg(unix)]
const IO_EVENT_TIMEOUT: Duration = Duration::from_millis(5);

#[cfg(unix)]
const PICKER_ESCAPE_TIMEOUT: Duration = Duration::from_millis(30);

#[cfg(unix)]
const OSC133_STARTUP_WATCHDOG_TIMEOUT: Duration = Duration::from_millis(500);

#[cfg(unix)]
const MAX_PENDING_COMMENT_COMMANDS: usize = 128;

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
    if let Some(locale_warning) = locale_warning_message() {
        warn!("{locale_warning}");
    }

    #[cfg(unix)]
    {
        match TempDirManager::cleanup_stale() {
            Ok(cleaned) if cleaned > 0 => {
                warn!("Cleaned up {cleaned} stale shell injection temp directorie(s)");
            }
            Ok(_) => {}
            Err(err) => {
                // Startup should continue even if cleanup fails.
                warn!("Failed to clean up stale shell injection temp directories: {err}");
            }
        }
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
#[cfg(unix)]
fn run_full_mode(cli: &Cli) -> Result<()> {
    debug!("Full mode configuration:");
    debug!("  Daemon socket: {:?}", cli.daemon_socket);
    debug!("  Daemon timeout: {}ms", cli.daemon_timeout);
    debug!("  Hotkey: {:?}", cli.hotkey);
    debug!("  Hotkey timeout: {}ms", cli.hotkey_timeout);

    let config = Config::load_and_merge(cli);
    let socket_path = config
        .daemon_socket
        .clone()
        .or_else(DaemonClient::default_socket_path)
        .context("No daemon socket path configured")?;

    let timeout = Duration::from_millis(cli.daemon_timeout);
    let mut connected_forwarder = None;
    let mut last_err = None;
    let mut fallback_reason = StandaloneReason::DaemonUnavailable;

    for attempt in 0..=1 {
        match DaemonClient::connect_with_timeout(&socket_path, timeout) {
            Ok(mut client) => match client.ping() {
                Ok(()) => {
                    info!(
                        "Connected to daemon over JSON-RPC at {}",
                        socket_path.display()
                    );
                    connected_forwarder = Some(DaemonEventForwarder::with_client(
                        client,
                        ForwarderConfig::default()
                            .daemon_socket_path(socket_path.clone())
                            .connect_timeout(timeout),
                    ));
                    break;
                }
                Err(err) => {
                    last_err = Some(err.to_string());
                    fallback_reason = map_daemon_error_to_standalone_reason(&err);
                    warn!("Daemon ping failed on attempt {}/2: {}", attempt + 1, err);
                }
            },
            Err(err) => {
                last_err = Some(err.to_string());
                fallback_reason = map_daemon_error_to_standalone_reason(&err);
                warn!(
                    "Daemon connect failed on attempt {}/2 ({}): {}",
                    attempt + 1,
                    socket_path.display(),
                    err
                );
            }
        }
    }

    if connected_forwarder.is_none() {
        if let Some(err) = last_err {
            warn!("Falling back to standalone mode after daemon connection failure: {err}");
        } else {
            warn!("Falling back to standalone mode (daemon unavailable)");
        }
    }

    run_unix_mode(cli, connected_forwarder, fallback_reason)
}

#[cfg(not(unix))]
fn run_full_mode(cli: &Cli) -> Result<()> {
    warn!("Full mode daemon integration is currently Unix-only; using standalone mode");
    run_standalone_mode(cli)
}

/// Run in standalone mode without daemon connection
#[cfg(unix)]
fn run_standalone_mode(cli: &Cli) -> Result<()> {
    run_unix_mode(cli, None, StandaloneReason::DaemonUnavailable)
}

#[cfg(unix)]
fn run_unix_mode(
    cli: &Cli,
    mut daemon_forwarder: Option<DaemonEventForwarder>,
    standalone_reason: StandaloneReason,
) -> Result<()> {
    // Load config file and merge with CLI arguments (CLI wins)
    let config = Config::load_and_merge(cli);
    if let Some(ref path) = config.config_path {
        info!("Loaded configuration from {:?}", path);
    }
    debug!(
        "Merged config: hotkey={:?}, buffer_capacity={}, execute_on_select={}",
        config.hotkey, config.buffer_capacity, config.execute_on_select
    );

    // Get shell path
    let shell_path = cli.shell_path();
    debug!("Using shell: {:?}", shell_path);

    let standalone_state = init_standalone_history(cli, &shell_path, standalone_reason)?;
    if standalone_state.has_history() {
        debug!(
            "Loaded {} history entries from {:?}",
            standalone_state.history_count(),
            standalone_state.history_path()
        );
    }
    if daemon_forwarder.is_none() {
        standalone_state.log_warning();
    }

    // Initialize denylist for privacy gate (from merged config)
    let denylist = config.denylist.clone();

    // Set up shell injection for OSC 133 hooks
    let shell_inject = setup_shell_injection(&shell_path);

    // Create PTY and spawn shell (with injection args/env if available)
    let (extra_args, extra_env) = match &shell_inject {
        Some(ShellInjection::Bash(ref injector)) => (injector.shell_args(), Vec::new()),
        Some(ShellInjection::Zsh(ref injector)) => (Vec::new(), injector.env_vars()),
        Some(ShellInjection::Fish(ref injector)) => (injector.shell_args(), Vec::new()),
        None => (Vec::new(), Vec::new()),
    };

    let mut pty_host = PtyHost::new_with_inject(
        Some(shell_path.clone()),
        cli.login_shell,
        &extra_args,
        &extra_env,
    )
    .context("Failed to create PTY")?;

    info!("Shell spawned with PID: {:?}", pty_host.child_pid());

    // Get master PTY fd for process detection
    let master_fd = pty_host.master_fd();

    // Get PTY reader and writer
    let pty_reader = pty_host.reader().context("Failed to get PTY reader")?;
    let pty_writer = pty_host.writer().context("Failed to get PTY writer")?;

    let tty_status = clai_wrap::raw_mode::detect_tty();
    if !tty_status.any_tty() && !cli.force_non_tty {
        anyhow::bail!(
            "stdin/stdout/stderr are not TTYs; use --force-non-tty for pure passthrough mode"
        );
    }
    let term_dumb = std::env::var("TERM")
        .map(|term| term.trim().is_empty() || term == "dumb")
        .unwrap_or(true);

    let mut hotkey_enabled = cli.ui_enabled();
    let mut picker_enabled = cli.ui_enabled();

    if !tty_status.stdin {
        warn!("stdin is not a TTY; hotkey detection is disabled");
        hotkey_enabled = false;
    }
    if !tty_status.stdout {
        warn!("stdout is not a TTY; picker UI is disabled");
        picker_enabled = false;
    }
    if term_dumb {
        warn!("TERM is unset or dumb; picker UI is disabled");
        picker_enabled = false;
    }
    if !picker_enabled && hotkey_enabled {
        hotkey_enabled = false;
        warn!("Hotkey detection is disabled because picker UI is unavailable");
    }

    // Enter raw mode only when stdin is interactive.
    let _raw_guard = if tty_status.stdin {
        Some(enter_raw_mode().context("Failed to enter raw mode")?)
    } else {
        None
    };

    // Install signal handlers
    let signal_handler = SignalHandler::new().context("Failed to install signal handlers")?;

    // Create resize handler
    let resize_handler = Arc::new(ResizeHandler::new());

    // Create event-driven I/O threads so stdin and PTY output are handled without blocking.
    let buffer_cap = if config.buffer_capacity > 0 {
        config.buffer_capacity
    } else {
        DEFAULT_BUFFER_CAP
    };
    let mut io_threads = IoThreads::new(pty_reader, pty_writer, buffer_cap)
        .context("Failed to start I/O threads")?;

    let hotkey_config = build_hotkey_config_from(&config, cli);
    let (input_event_tx, input_event_rx) = mpsc::channel();
    let mut input_router = InputRouter::new(hotkey_config, input_event_tx);

    // Create OSC 133 parser for command tracking
    let mut osc133_parser = Osc133Parser::new();
    let mut bracketed_paste = BracketedPasteTracker::new();

    // Create echo-gap detector for password prompt detection (Privacy Gate 2)
    let mut echo_gap = EchoGapDetector::new(clai_wrap::echo_gap::DEFAULT_THRESHOLD_MS);

    // Create output capture buffer for AI analysis
    let mut output_capture = OutputCapture::new(buffer_cap);
    if daemon_forwarder.is_none() {
        output_capture.disable();
    }

    // Create suggestion receiver for daemon suggestions (no-op in standalone mode)
    let mut suggestion_receiver = SuggestionReceiver::new();

    // Create comment renderer and manager for displaying assistant comments
    let shell_type = Shell::from_shell_path(&shell_path.to_string_lossy());
    let comment_renderer = CommentRenderer::new(shell_type);
    let mut comment_manager = CommentManager::with_renderer(comment_renderer);

    // Create selection injector
    let mut selection_injector = SelectionInjector::new();

    // Main event loop
    let mut stdout = std::io::stdout();
    let mut picker_session = None;
    let mut ui_picker_parser = PickerInputParser::default();
    let mut child_exit_status = None;
    let mut osc133_watchdog_fired = false;
    let startup_instant = Instant::now();
    let mut pending_comment_commands: VecDeque<String> = VecDeque::new();
    let mut picker_overflow_warning_emitted = false;

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
                    // SIGCHLD is advisory; resolve actual status through try_wait().
                    debug!("Received child-exit signal");
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
                    if picker_session.is_some() {
                        close_picker_session(
                            &mut picker_session,
                            &mut io_threads,
                            &mut picker_overflow_warning_emitted,
                            &mut stdout,
                        )?;
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

        // Check echo-gap timeout for password prompt detection
        if echo_gap.check_timeout(Instant::now()) && echo_gap.is_secure_mode() {
            debug!(
                "Echo-gap: entered secure mode, {} byte(s) to scrub",
                echo_gap.bytes_to_scrub()
            );
        }

        if !osc133_watchdog_fired
            && startup_instant.elapsed() >= OSC133_STARTUP_WATCHDOG_TIMEOUT
            && matches!(osc133_parser.current_state(), Osc133State::Unknown)
        {
            osc133_watchdog_fired = true;
            warn!(
                "OSC 133 startup watchdog fired after {}ms; disabling capture and suggestion features for this session",
                OSC133_STARTUP_WATCHDOG_TIMEOUT.as_millis()
            );
            if let Some(forwarder) = daemon_forwarder.as_mut() {
                forwarder.disable_capture();
            }
            output_capture.disable();
        }

        // Check for child exit
        if let Ok(Some(status)) = pty_host.try_wait() {
            info!("Shell exited with status: {:?}", status.code());
            child_exit_status = Some(status);
            break;
        }

        if let Some(event) = io_threads.recv_event_timeout(IO_EVENT_TIMEOUT) {
            handle_io_event(
                event,
                cli,
                &config,
                hotkey_enabled,
                picker_enabled,
                daemon_capture_enabled(&daemon_forwarder, osc133_watchdog_fired),
                master_fd,
                &denylist,
                &mut io_threads,
                &mut osc133_parser,
                &mut bracketed_paste,
                &mut echo_gap,
                &mut output_capture,
                &mut daemon_forwarder,
                &mut suggestion_receiver,
                &mut comment_manager,
                &mut pending_comment_commands,
                &mut selection_injector,
                &mut input_router,
                &input_event_rx,
                &mut ui_picker_parser,
                &standalone_state,
                &mut picker_overflow_warning_emitted,
                &mut picker_session,
                &mut stdout,
            )?;
        }

        while let Some(event) = io_threads.try_recv_event() {
            handle_io_event(
                event,
                cli,
                &config,
                hotkey_enabled,
                picker_enabled,
                daemon_capture_enabled(&daemon_forwarder, osc133_watchdog_fired),
                master_fd,
                &denylist,
                &mut io_threads,
                &mut osc133_parser,
                &mut bracketed_paste,
                &mut echo_gap,
                &mut output_capture,
                &mut daemon_forwarder,
                &mut suggestion_receiver,
                &mut comment_manager,
                &mut pending_comment_commands,
                &mut selection_injector,
                &mut input_router,
                &input_event_rx,
                &mut ui_picker_parser,
                &standalone_state,
                &mut picker_overflow_warning_emitted,
                &mut picker_session,
                &mut stdout,
            )?;
        }

        if let Some(forwarder) = daemon_forwarder.as_mut() {
            while let Some(notification) = forwarder.poll_notification() {
                suggestion_receiver.handle_notification(&notification);
            }
        }

        if picker_session.is_none()
            && matches!(
                osc133_parser.current_state(),
                Osc133State::Prompt | Osc133State::Input
            )
        {
            render_pending_comments(
                &mut pending_comment_commands,
                &mut suggestion_receiver,
                &mut comment_manager,
                &mut stdout,
            )?;
        }

        // Flush a lone ESC (cancel) if the picker is open and no follow-up bytes arrived.
        if let Some(session) = picker_session.as_mut() {
            for key in ui_picker_parser.check_timeout() {
                let should_close =
                    handle_picker_key(key, session, &io_threads, &selection_injector, &config)?;
                if should_close {
                    close_picker_session(
                        &mut picker_session,
                        &mut io_threads,
                        &mut picker_overflow_warning_emitted,
                        &mut stdout,
                    )?;
                    break;
                }
            }
        } else if hotkey_enabled {
            // Emit timeout-forwarded bytes for incomplete hotkey chords.
            input_router.check_timeout()?;
            process_input_router_events(
                &input_event_rx,
                &mut io_threads,
                &mut picker_session,
                &standalone_state,
                picker_enabled,
                &mut picker_overflow_warning_emitted,
                &mut stdout,
            )?;
        }
    }

    if picker_session.is_some() {
        close_picker_session(
            &mut picker_session,
            &mut io_threads,
            &mut picker_overflow_warning_emitted,
            &mut stdout,
        )?;
    }

    io_threads.shutdown();

    if let Some(status) = child_exit_status.or_else(|| pty_host.try_wait().ok().flatten()) {
        std::process::exit(status.as_exit_code());
    }

    Ok(())
}

#[cfg(unix)]
#[allow(clippy::too_many_arguments)]
fn handle_io_event(
    event: IoEvent,
    _cli: &Cli,
    config: &Config,
    hotkey_enabled: bool,
    picker_enabled: bool,
    capture_features_enabled: bool,
    master_fd: Option<std::os::unix::io::RawFd>,
    denylist: &clai_wrap::denylist::Denylist,
    io_threads: &mut IoThreads,
    osc133_parser: &mut Osc133Parser,
    bracketed_paste: &mut BracketedPasteTracker,
    echo_gap: &mut EchoGapDetector,
    output_capture: &mut OutputCapture,
    daemon_forwarder: &mut Option<DaemonEventForwarder>,
    suggestion_receiver: &mut SuggestionReceiver,
    comment_manager: &mut CommentManager,
    pending_comment_commands: &mut VecDeque<String>,
    selection_injector: &mut SelectionInjector,
    input_router: &mut InputRouter,
    input_event_rx: &std::sync::mpsc::Receiver<InputEvent>,
    ui_picker_parser: &mut PickerInputParser,
    standalone_state: &StandaloneState,
    picker_overflow_warning_emitted: &mut bool,
    picker_session: &mut Option<PickerSession>,
    stdout: &mut std::io::Stdout,
) -> Result<()> {
    match event {
        IoEvent::PtyOutput(data) => {
            let prev_osc_state = osc133_parser.current_state().clone();
            osc133_parser.process_bytes(&data);
            bracketed_paste.update_from_output(&data);
            selection_injector.sync_with_tracker(bracketed_paste);

            // Handle OSC 133 state transitions for output capture and denylist
            let new_osc_state = osc133_parser.current_state();
            if *new_osc_state != prev_osc_state {
                if capture_features_enabled {
                    if let Some(forwarder) = daemon_forwarder.as_mut() {
                        forwarder.on_osc133_state_change(new_osc_state);
                    }
                }

                match new_osc_state {
                    Osc133State::Output => {
                        // Command started executing — check denylist
                        let denied = if let Some(fd) = master_fd {
                            let fg_process = get_foreground_process_or(fd, "shell");
                            let is_denied = denylist.is_denied(&fg_process);
                            if is_denied {
                                debug!("Privacy gate: foreground process {:?} is denylisted, capture disabled", fg_process);
                                output_capture.disable();
                            }
                            is_denied
                        } else {
                            false
                        };

                        if denied {
                            if let Some(forwarder) = daemon_forwarder.as_mut() {
                                forwarder.disable_capture();
                            }
                        } else if capture_features_enabled {
                            output_capture.enable();
                            if let Some(forwarder) = daemon_forwarder.as_mut() {
                                forwarder.enable_capture();
                            }
                        }
                    }
                    Osc133State::Finished(exit_code) => {
                        if capture_features_enabled && *exit_code != 0 {
                            if let Some(forwarder) = daemon_forwarder.as_mut() {
                                if let Some(command_id) = forwarder.take_finished_command_id() {
                                    if !pending_comment_commands
                                        .iter()
                                        .any(|existing| existing == &command_id)
                                    {
                                        if pending_comment_commands.len()
                                            >= MAX_PENDING_COMMENT_COMMANDS
                                        {
                                            let _ = pending_comment_commands.pop_front();
                                            warn!(
                                                "Pending assistant-comment queue reached capacity; dropping oldest pending command"
                                            );
                                        }
                                        pending_comment_commands.push_back(command_id);
                                    }
                                }
                            }
                        }
                    }
                    Osc133State::Prompt => {
                        if capture_features_enabled {
                            if let Some(forwarder) = daemon_forwarder.as_mut() {
                                forwarder.enable_capture();
                            }
                        }
                        if picker_session.is_none() {
                            render_pending_comments(
                                pending_comment_commands,
                                suggestion_receiver,
                                comment_manager,
                                stdout,
                            )?;
                        }
                    }
                    _ => {}
                }
            }

            // Feed output bytes to daemon/output capture and echo-gap detector.
            if capture_features_enabled {
                if let Some(forwarder) = daemon_forwarder.as_mut() {
                    forwarder.forward_output(&data);
                }
            }
            if output_capture.is_enabled() {
                output_capture.push(&data);
            }
            let now = Instant::now();
            for &byte in &data {
                echo_gap.record_output(byte, now);
            }

            // Scrub captured output if echo-gap enters secure mode
            if echo_gap.is_secure_mode() && echo_gap.bytes_to_scrub() > 0 {
                output_capture.disable();
                if let Some(forwarder) = daemon_forwarder.as_mut() {
                    forwarder.disable_capture();
                }
                debug!(
                    "Echo-gap secure mode: disabled output capture, {} bytes to scrub",
                    echo_gap.bytes_to_scrub()
                );
            }

            if picker_session.is_some() {
                let overflowed = io_threads.buffer_output(&data);
                if overflowed && !*picker_overflow_warning_emitted {
                    warn!(
                        "PTY output buffer overflowed while picker was open; truncating oldest buffered output"
                    );
                    *picker_overflow_warning_emitted = true;
                }
            } else {
                stdout.write_all(&data)?;
                stdout.flush()?;
            }
        }
        IoEvent::StdinInput(data) => {
            // Feed input bytes to echo-gap detector
            let now = Instant::now();
            for &byte in &data {
                echo_gap.record_input(byte, now);
            }

            if let Some(session) = picker_session.as_mut() {
                for key in ui_picker_parser.feed(&data) {
                    let should_close =
                        handle_picker_key(key, session, io_threads, selection_injector, config)?;
                    if should_close {
                        close_picker_session(
                            picker_session,
                            io_threads,
                            picker_overflow_warning_emitted,
                            stdout,
                        )?;
                        break;
                    }
                }
            } else if hotkey_enabled {
                input_router.process_bytes(&data)?;
                process_input_router_events(
                    input_event_rx,
                    io_threads,
                    picker_session,
                    standalone_state,
                    picker_enabled,
                    picker_overflow_warning_emitted,
                    stdout,
                )?;
            } else {
                io_threads.send_to_pty(data)?;
            }
        }
        IoEvent::PtyEof => {
            debug!("PTY EOF");
        }
        IoEvent::StdinEof => {
            debug!("stdin EOF");
        }
        IoEvent::PtyReadError(err) => {
            error!("PTY read error: {}", err);
        }
        IoEvent::StdinReadError(err) => {
            error!("stdin read error: {}", err);
        }
    }
    Ok(())
}

#[cfg(unix)]
fn process_input_router_events(
    input_event_rx: &std::sync::mpsc::Receiver<InputEvent>,
    io_threads: &mut IoThreads,
    picker_session: &mut Option<PickerSession>,
    standalone_state: &StandaloneState,
    picker_enabled: bool,
    picker_overflow_warning_emitted: &mut bool,
    stdout: &mut std::io::Stdout,
) -> Result<()> {
    while let Ok(input_event) = input_event_rx.try_recv() {
        match input_event {
            InputEvent::ForwardToPty(bytes) => {
                debug!(
                    "forwarding {} byte(s) from input router to PTY",
                    bytes.len()
                );
                io_threads.send_to_pty(bytes)?
            }
            InputEvent::OpenHistoryPicker => {
                debug!("hotkey triggered history picker");
                if !picker_enabled {
                    warn!("History picker is disabled in this terminal mode");
                    continue;
                }
                if picker_session.is_none() {
                    *picker_session = Some(PickerSession::open(standalone_state.create_picker())?);
                    *picker_overflow_warning_emitted = false;
                    io_threads.set_picker_open(true);
                }
            }
            InputEvent::OpenCompletionsPicker => {
                debug!("hotkey triggered completions picker");
                if !picker_enabled {
                    warn!("Completions picker is disabled in this terminal mode");
                    continue;
                }
                warn!(
                    "Completions picker is unavailable in standalone mode; opening history picker"
                );
                if picker_session.is_none() {
                    *picker_session = Some(PickerSession::open(standalone_state.create_picker())?);
                    *picker_overflow_warning_emitted = false;
                    io_threads.set_picker_open(true);
                }
            }
        }
    }

    if picker_session.is_none() {
        let pending = io_threads.drain_output_buffer();
        if !pending.is_empty() {
            stdout.write_all(&pending)?;
            stdout.flush()?;
        }
    }

    Ok(())
}

#[cfg(unix)]
fn close_picker_session(
    picker_session: &mut Option<PickerSession>,
    io_threads: &mut IoThreads,
    picker_overflow_warning_emitted: &mut bool,
    stdout: &mut std::io::Stdout,
) -> Result<()> {
    if picker_session.take().is_some() {
        io_threads.set_picker_open(false);
        *picker_overflow_warning_emitted = false;
        let pending = io_threads.drain_output_buffer();
        if !pending.is_empty() {
            stdout.write_all(&pending)?;
            stdout.flush()?;
        }
    }
    Ok(())
}

#[cfg(unix)]
fn build_hotkey_config_from(config: &Config, cli: &Cli) -> HotkeyConfig {
    let mut hotkey_config = HotkeyConfig {
        timeout: Duration::from_millis(cli.hotkey_timeout),
        ..Default::default()
    };

    let spec = &config.hotkey;
    if let Some((first_byte, second_byte)) = parse_hotkey_spec(spec) {
        hotkey_config.first_byte = first_byte;
        hotkey_config.history_byte = second_byte;
        hotkey_config.completions_byte = CHORD_COMPLETIONS_BYTE;
        debug!("Using custom hotkey chord: first=0x{first_byte:02x}, second=0x{second_byte:02x}");
    } else if spec != clai_wrap::config::DEFAULT_HOTKEY {
        // Only warn if the user explicitly set a non-default hotkey that's invalid
        warn!(
            "Invalid hotkey value {:?}; expected format like \"ctrl-\\\\ h\"",
            spec
        );
    }

    hotkey_config
}

#[cfg(unix)]
fn parse_hotkey_spec(spec: &str) -> Option<(u8, u8)> {
    let mut parts = spec.split_whitespace();
    let first = parts.next()?;
    let second = parts.next()?;
    if parts.next().is_some() {
        return None;
    }

    let first_byte = parse_ctrl_token(first)?;
    let second_byte = parse_single_ascii(second)?;
    Some((first_byte, second_byte))
}

#[cfg(unix)]
fn parse_ctrl_token(token: &str) -> Option<u8> {
    let lower = token.to_ascii_lowercase();
    let inner = lower.strip_prefix("ctrl-")?;

    if inner.len() != 1 {
        return None;
    }

    let byte = inner.as_bytes()[0];
    match byte {
        b'@' => Some(0x00),
        b'a'..=b'z' => Some(byte - b'a' + 1),
        b'[' => Some(0x1b),
        b'\\' => Some(0x1c),
        b']' => Some(0x1d),
        b'^' => Some(0x1e),
        b'_' => Some(0x1f),
        b'?' => Some(0x7f),
        _ => None,
    }
}

#[cfg(unix)]
fn parse_single_ascii(token: &str) -> Option<u8> {
    let mut chars = token.chars();
    let ch = chars.next()?;
    if chars.next().is_some() || !ch.is_ascii() {
        return None;
    }
    Some(ch as u8)
}

#[cfg(unix)]
#[derive(Debug)]
struct PickerSession {
    picker: Picker,
    terminal: Terminal<CrosstermBackend<std::io::Stdout>>,
    _alt_guard: AltScreenGuard,
}

#[cfg(unix)]
impl PickerSession {
    fn open(picker: Picker) -> Result<Self> {
        let alt_guard = enter_alt_screen().context("Failed to enter alternate screen")?;
        let backend = CrosstermBackend::new(std::io::stdout());
        let mut terminal =
            Terminal::new(backend).context("Failed to initialize picker terminal")?;
        terminal
            .clear()
            .context("Failed to clear picker terminal")?;

        let mut session = Self {
            picker,
            terminal,
            _alt_guard: alt_guard,
        };
        session.render()?;
        Ok(session)
    }

    fn render(&mut self) -> Result<()> {
        self.terminal
            .draw(|frame| {
                let area = frame.area();
                self.picker.render(frame, area);
            })
            .context("Failed to render picker UI")?;
        Ok(())
    }
}

#[cfg(unix)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum PickerKey {
    Up,
    Down,
    Enter,
    Escape,
    Backspace,
    Char(char),
}

#[cfg(unix)]
#[derive(Debug, Default)]
struct PickerInputParser {
    pending: Vec<u8>,
    escape_started: Option<std::time::Instant>,
}

#[cfg(unix)]
impl PickerInputParser {
    fn feed(&mut self, bytes: &[u8]) -> Vec<PickerKey> {
        self.pending.extend_from_slice(bytes);
        self.parse_ready(false)
    }

    fn check_timeout(&mut self) -> Vec<PickerKey> {
        self.parse_ready(true)
    }

    fn parse_ready(&mut self, allow_escape_timeout: bool) -> Vec<PickerKey> {
        let mut keys = Vec::new();

        loop {
            if self.pending.is_empty() {
                self.escape_started = None;
                break;
            }

            if self.pending[0] == 0x1b {
                if self.pending.len() >= 3
                    && (self.pending[1] == b'[' || self.pending[1] == b'O')
                    && (self.pending[2] == b'A'
                        || self.pending[2] == b'B'
                        || self.pending[2] == b'C'
                        || self.pending[2] == b'D')
                {
                    let key = match self.pending[2] {
                        b'A' => Some(PickerKey::Up),
                        b'B' => Some(PickerKey::Down),
                        _ => None,
                    };
                    self.pending.drain(..3);
                    self.escape_started = None;
                    if let Some(key) = key {
                        keys.push(key);
                    }
                    continue;
                }

                if self.pending.len() == 1 {
                    let started = self
                        .escape_started
                        .get_or_insert_with(std::time::Instant::now);
                    if allow_escape_timeout && started.elapsed() >= PICKER_ESCAPE_TIMEOUT {
                        self.pending.drain(..1);
                        self.escape_started = None;
                        keys.push(PickerKey::Escape);
                    }
                    break;
                }

                // Unknown ESC sequence: treat as cancel and continue parsing remaining bytes.
                self.pending.drain(..1);
                self.escape_started = None;
                keys.push(PickerKey::Escape);
                continue;
            }

            self.escape_started = None;

            match self.pending[0] {
                b'\r' | b'\n' => {
                    self.pending.drain(..1);
                    keys.push(PickerKey::Enter);
                }
                0x7f | 0x08 => {
                    self.pending.drain(..1);
                    keys.push(PickerKey::Backspace);
                }
                b if b >= 0x20 && b < 0x80 => {
                    self.pending.drain(..1);
                    keys.push(PickerKey::Char(char::from(b)));
                }
                b => {
                    if let Some(utf8_len) = utf8_expected_len(b) {
                        if self.pending.len() < utf8_len {
                            break;
                        }

                        let bytes = &self.pending[..utf8_len];
                        if let Ok(s) = std::str::from_utf8(bytes) {
                            if let Some(ch) = s.chars().next() {
                                keys.push(PickerKey::Char(ch));
                            }
                            self.pending.drain(..utf8_len);
                        } else {
                            self.pending.drain(..1);
                        }
                    } else {
                        // Ignore control bytes we don't explicitly map.
                        self.pending.drain(..1);
                    }
                }
            }
        }

        keys
    }
}

#[cfg(unix)]
fn utf8_expected_len(first: u8) -> Option<usize> {
    match first {
        0xC2..=0xDF => Some(2),
        0xE0..=0xEF => Some(3),
        0xF0..=0xF4 => Some(4),
        _ => None,
    }
}

#[cfg(unix)]
fn handle_picker_key(
    key: PickerKey,
    session: &mut PickerSession,
    io_threads: &IoThreads,
    selection_injector: &SelectionInjector,
    config: &Config,
) -> Result<bool> {
    match key {
        PickerKey::Up => {
            session.picker.select_prev();
            session.render()?;
            Ok(false)
        }
        PickerKey::Down => {
            session.picker.select_next();
            session.render()?;
            Ok(false)
        }
        PickerKey::Backspace => {
            session.picker.pop_char();
            session.render()?;
            Ok(false)
        }
        PickerKey::Char(ch) => {
            session.picker.push_char(ch);
            session.render()?;
            Ok(false)
        }
        PickerKey::Escape => Ok(true),
        PickerKey::Enter => {
            if let Some(selection) = session.picker.selected_item().map(|item| item.text.clone()) {
                let mut injected = Vec::new();
                if config.execute_on_select {
                    selection_injector
                        .inject_with_execute(&mut injected, &selection)
                        .context("Failed to inject selected command with execute")?;
                } else {
                    selection_injector
                        .inject(&mut injected, &selection)
                        .context("Failed to inject selected command")?;
                }
                io_threads.send_to_pty(injected)?;
            }
            Ok(true)
        }
    }
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

fn locale_warning_message() -> Option<String> {
    let locale = std::env::var("LC_ALL")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .map(|value| ("LC_ALL", value))
        .or_else(|| {
            std::env::var("LANG")
                .ok()
                .filter(|value| !value.trim().is_empty())
                .map(|value| ("LANG", value))
        })?;

    if is_utf8_locale(&locale.1) {
        return None;
    }

    Some(format!(
        "non-UTF-8 locale detected ({}={}); output will use lossy UTF-8 conversion when needed",
        locale.0, locale.1
    ))
}

fn is_utf8_locale(locale_value: &str) -> bool {
    let normalized = locale_value.to_ascii_lowercase();
    normalized.contains("utf-8") || normalized.contains("utf8")
}

#[cfg(unix)]
fn init_standalone_history(
    cli: &Cli,
    shell_path: &Path,
    standalone_reason: StandaloneReason,
) -> Result<StandaloneState> {
    let mut state = StandaloneState::new(standalone_reason);

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

#[cfg(unix)]
fn daemon_capture_enabled(
    daemon_forwarder: &Option<DaemonEventForwarder>,
    osc133_watchdog_fired: bool,
) -> bool {
    if osc133_watchdog_fired {
        return false;
    }
    daemon_forwarder
        .as_ref()
        .is_some_and(|forwarder| !forwarder.is_standalone())
}

#[cfg(unix)]
fn map_daemon_error_to_standalone_reason(error: &DaemonClientError) -> StandaloneReason {
    match error {
        DaemonClientError::ConnectionTimeout(_) => StandaloneReason::ConnectionTimeout,
        _ => StandaloneReason::SocketError(error.to_string()),
    }
}

#[cfg(unix)]
fn render_pending_comments(
    pending_comment_commands: &mut VecDeque<String>,
    suggestion_receiver: &mut SuggestionReceiver,
    comment_manager: &mut CommentManager,
    stdout: &mut std::io::Stdout,
) -> Result<()> {
    if pending_comment_commands.is_empty() {
        return Ok(());
    }

    let mut still_pending = VecDeque::new();

    while let Some(command_id) = pending_comment_commands.pop_front() {
        let suggestions = suggestion_receiver.suggestions_for_command(&command_id);
        if suggestions.is_empty() {
            still_pending.push_back(command_id);
            continue;
        }

        for suggestion in suggestions {
            comment_manager.add_from_suggestion(suggestion);
        }

        let shell_output = comment_manager.render_shell_comments_for_command(&command_id);
        if !shell_output.is_empty() {
            let comment_bytes = format!("\n{shell_output}\n");
            stdout.write_all(comment_bytes.as_bytes())?;
            stdout.flush()?;
            debug!("Rendered assistant comment for {}", command_id);
        }

        suggestion_receiver.remove_suggestions_for_command(&command_id);
        comment_manager.remove_for_command(&command_id);
    }

    *pending_comment_commands = still_pending;
    Ok(())
}

/// Shell injection types for OSC 133 hook scripts.
#[cfg(unix)]
#[allow(dead_code)]
enum ShellInjection {
    Bash(clai_wrap::shell_inject::BashInjector),
    Zsh(clai_wrap::shell_inject::ZshInjector),
    Fish(clai_wrap::shell_inject::FishInjector),
}

/// Detect the shell type and create the appropriate injector.
///
/// Returns `None` if the shell is not supported or injection fails.
#[cfg(unix)]
fn setup_shell_injection(shell_path: &Path) -> Option<ShellInjection> {
    let shell_name = shell_path.file_name().and_then(|name| name.to_str())?;

    match shell_name {
        "bash" => match clai_wrap::shell_inject::BashInjector::new() {
            Ok(injector) => {
                info!("Shell injection: bash OSC 133 hooks enabled");
                Some(ShellInjection::Bash(injector))
            }
            Err(e) => {
                warn!("Failed to create bash injector: {e}");
                None
            }
        },
        "zsh" => match clai_wrap::shell_inject::ZshInjector::new() {
            Ok(injector) => {
                info!("Shell injection: zsh OSC 133 hooks enabled");
                Some(ShellInjection::Zsh(injector))
            }
            Err(e) => {
                warn!("Failed to create zsh injector: {e}");
                None
            }
        },
        "fish" => match clai_wrap::shell_inject::FishInjector::for_shell_path(shell_path) {
            Ok(injector) => {
                info!("Shell injection: fish OSC 133 hooks enabled");
                Some(ShellInjection::Fish(injector))
            }
            Err(e) => {
                warn!(
                    "Failed to detect fish version from {}: {}; using fallback fish injection",
                    shell_path.display(),
                    e
                );
                Some(ShellInjection::Fish(
                    clai_wrap::shell_inject::FishInjector::without_detection(),
                ))
            }
        },
        _ => {
            debug!("Shell injection: no injector for shell {:?}", shell_name);
            None
        }
    }
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
    fn test_locale_warning_message_for_non_utf8_lang() {
        let _lock = ENV_LOCK.lock().expect("lock env");
        std::env::remove_var("LC_ALL");
        std::env::set_var("LANG", "C");

        let warning = locale_warning_message();

        std::env::remove_var("LANG");
        assert!(warning.is_some());
        let warning_text = warning.unwrap();
        assert!(warning_text.contains("non-UTF-8 locale detected"));
        assert!(warning_text.contains("LANG=C"));
    }

    #[test]
    fn test_locale_warning_message_prefers_lc_all() {
        let _lock = ENV_LOCK.lock().expect("lock env");
        std::env::set_var("LC_ALL", "C");
        std::env::set_var("LANG", "en_US.UTF-8");

        let warning = locale_warning_message();

        std::env::remove_var("LC_ALL");
        std::env::remove_var("LANG");
        assert!(warning.is_some());
        assert!(warning.unwrap().contains("LC_ALL=C"));
    }

    #[test]
    fn test_locale_warning_message_not_emitted_for_utf8() {
        let _lock = ENV_LOCK.lock().expect("lock env");
        std::env::set_var("LANG", "en_US.UTF-8");
        std::env::remove_var("LC_ALL");

        let warning = locale_warning_message();

        std::env::remove_var("LANG");
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

        let state = init_standalone_history(
            &cli,
            Path::new("/bin/bash"),
            StandaloneReason::DaemonUnavailable,
        )
        .expect("initialize standalone history");

        assert!(
            state.has_history(),
            "history should be loaded from --history-file"
        );
        assert_eq!(state.history_count(), 2);
        assert_eq!(state.history_path(), Some(history.path()));
    }

    #[test]
    #[cfg(unix)]
    fn test_parse_hotkey_spec_default_shape() {
        assert_eq!(parse_hotkey_spec("ctrl-\\ h"), Some((0x1c, b'h')));
        assert_eq!(parse_hotkey_spec("ctrl-] h"), Some((0x1d, b'h')));
    }

    #[test]
    #[cfg(unix)]
    fn test_parse_hotkey_spec_rejects_invalid_shape() {
        assert!(parse_hotkey_spec("ctrl-\\").is_none());
        assert!(parse_hotkey_spec("ctrl-\\ history").is_none());
        assert!(parse_hotkey_spec("alt-h").is_none());
    }

    #[test]
    #[cfg(unix)]
    fn test_build_hotkey_config_uses_custom_first_and_second_bytes() {
        let cli = Cli::parse_from_args([
            "clai-wrap",
            "--standalone",
            "--hotkey",
            "ctrl-] h",
            "--hotkey-timeout",
            "77",
        ]);

        let config = Config::load_and_merge(&cli);
        let hotkey = build_hotkey_config_from(&config, &cli);
        assert_eq!(hotkey.first_byte, 0x1d);
        assert_eq!(hotkey.history_byte, b'h');
        assert_eq!(hotkey.completions_byte, CHORD_COMPLETIONS_BYTE);
        assert_eq!(hotkey.timeout, Duration::from_millis(77));
    }

    #[test]
    #[cfg(unix)]
    fn test_picker_input_parser_maps_arrow_and_enter_keys() {
        let mut parser = PickerInputParser::default();
        let keys = parser.feed(&[0x1b, b'[', b'A', 0x1b, b'[', b'B', b'\r']);

        assert_eq!(keys, vec![PickerKey::Up, PickerKey::Down, PickerKey::Enter]);
    }

    #[test]
    #[cfg(unix)]
    fn test_picker_input_parser_handles_escape_timeout_and_utf8() {
        let mut parser = PickerInputParser::default();
        assert!(
            parser.feed(&[0x1b]).is_empty(),
            "lone ESC should wait briefly"
        );

        std::thread::sleep(PICKER_ESCAPE_TIMEOUT + Duration::from_millis(5));
        assert_eq!(parser.check_timeout(), vec![PickerKey::Escape]);

        let keys = parser.feed("中".as_bytes());
        assert_eq!(keys, vec![PickerKey::Char('中')]);
    }
}
