//! Shell integration injection for OSC 133 semantic prompt support.
//!
//! This module provides injectors for various shells that enable OSC 133
//! semantic prompt sequences without requiring user configuration.
//!
//! # Supported Shells
//!
//! - **Bash**: Uses `--rcfile` to inject init script
//! - **Zsh**: Uses `ZDOTDIR` wrapper
//! - **Fish**: (future) Uses `--init-command` or native OSC 133 (Fish >= 3.6)
//!
//! # OSC 133 Sequences
//!
//! OSC 133 is a semantic prompt specification that allows terminal emulators
//! and PTY wrappers to understand command boundaries:
//!
//! | Sequence | Meaning |
//! |----------|---------|
//! | `\e]133;A\a` | Prompt start |
//! | `\e]133;B\a` | Input start (end of prompt) |
//! | `\e]133;C\a` | Output start (command execution begins) |
//! | `\e]133;D;N\a` | Finished (command completed with exit code N) |

mod bash;
mod zsh;

pub use bash::{BashInjector, BashInjectorError};
pub use zsh::{ZshInjector, ZshInjectorError};
