//! clai-wrap: PTY wrapper for clai terminal assistant
//!
//! This crate provides a pseudo-terminal wrapper that interposes between
//! the user's shell and the terminal emulator, enabling intelligent
//! command assistance.

pub mod bracketed_paste;
pub mod hotkey;
pub mod osc133;
pub mod ring_buffer;

pub use osc133::{Osc133Parser, Osc133State};
pub use ring_buffer::SpscRingBuffer;
