//! clai-wrap: PTY wrapper for intelligent terminal assistance
//!
//! This binary wraps the user's shell in a pseudo-terminal to provide
//! intelligent command suggestions, history search, and autocomplete features.

use anyhow::{Context, Result};
use clai_wrap::cli::{Cli, Commands, OperationMode};
use tracing::{debug, info, Level};
use tracing_subscriber::EnvFilter;

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
        Level::INFO
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
    use std::io::Write;
    let mut stdout = std::io::stdout();

    // Reset terminal modes
    // Show cursor
    write!(stdout, "\x1b[?25h")?;
    // Exit alternate screen if active
    write!(stdout, "\x1b[?1049l")?;
    // Reset character attributes
    write!(stdout, "\x1b[0m")?;
    // Enable line wrap
    write!(stdout, "\x1b[?7h")?;
    // Clear screen
    write!(stdout, "\x1b[2J")?;
    // Move cursor to home
    write!(stdout, "\x1b[H")?;

    stdout.flush()?;

    // Try to restore terminal settings via stty
    #[cfg(unix)]
    {
        let _ = std::process::Command::new("stty").arg("sane").status();
    }

    Ok(())
}

/// Run in full mode with daemon connection and all features
fn run_full_mode(cli: &Cli) -> Result<()> {
    // TODO: Implement full mode with daemon connection
    debug!("Full mode configuration:");
    debug!("  Daemon socket: {:?}", cli.daemon_socket);
    debug!("  Daemon timeout: {}ms", cli.daemon_timeout);
    debug!("  Hotkey: {:?}", cli.hotkey);
    debug!("  Hotkey timeout: {}ms", cli.hotkey_timeout);

    info!("Full mode not yet implemented, falling back to standalone mode");
    run_standalone_mode(cli)
}

/// Run in standalone mode without daemon connection
fn run_standalone_mode(cli: &Cli) -> Result<()> {
    // TODO: Implement standalone mode with picker UI
    debug!("Standalone mode configuration:");
    debug!("  History file: {:?}", cli.history_file);
    debug!("  Execute on select: {}", cli.execute_on_select);

    info!("Standalone mode not yet implemented, falling back to passthrough mode");
    run_passthrough_mode(cli)
}

/// Run in passthrough mode (pure PTY forwarding)
fn run_passthrough_mode(cli: &Cli) -> Result<()> {
    // TODO: Implement passthrough mode
    debug!("Passthrough mode configuration:");
    debug!("  Shell: {:?}", cli.shell_path());
    debug!("  Login shell: {}", cli.login_shell);

    // For now, just print a message
    println!("clai-wrap passthrough mode");
    println!("Shell: {:?}", cli.shell_path());
    println!("Operation mode: {}", cli.operation_mode());

    Ok(())
}
