//! OSC 133 escape sequence parser for shell integration.
//!
//! OSC 133 is a semantic prompt protocol that allows shells to communicate
//! their state to terminal emulators. The sequences are:
//!
//! - `\x1b]133;A\x07` or `\x1b]133;A\x1b\\` - Prompt start
//! - `\x1b]133;B\x07` or `\x1b]133;B\x1b\\` - Input (command entered)
//! - `\x1b]133;C\x07` or `\x1b]133;C\x1b\\` - Output start
//! - `\x1b]133;D;N\x07` or `\x1b]133;D;N\x1b\\` - Finished with exit code N

use vte::{Parser, Perform};

/// State of the shell as reported by OSC 133 sequences.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub enum Osc133State {
    /// Initial state, no OSC 133 sequences seen yet.
    #[default]
    Unknown,
    /// Shell is displaying the prompt (OSC 133;A).
    Prompt,
    /// User is entering input after the prompt (OSC 133;B).
    Input,
    /// Command is executing and producing output (OSC 133;C).
    Output,
    /// Command has finished with the given exit code (OSC 133;D;N).
    Finished(i32),
}

/// Parser for OSC 133 escape sequences.
///
/// This parser uses the `vte` crate to handle ANSI escape sequence parsing
/// and tracks the current shell state based on OSC 133 sequences.
pub struct Osc133Parser {
    parser: Parser,
    state: Osc133State,
}

impl Default for Osc133Parser {
    fn default() -> Self {
        Self::new()
    }
}

impl Osc133Parser {
    /// Create a new OSC 133 parser.
    #[must_use]
    pub fn new() -> Self {
        Self {
            parser: Parser::new(),
            state: Osc133State::Unknown,
        }
    }

    /// Get the current OSC 133 state.
    #[must_use]
    pub const fn current_state(&self) -> &Osc133State {
        &self.state
    }

    /// Process a slice of bytes through the parser.
    ///
    /// This feeds bytes to the VTE parser, which will call back into our
    /// `Perform` implementation when escape sequences are detected.
    pub fn process_bytes(&mut self, bytes: &[u8]) {
        // Create a handler that can update our state
        let mut handler = Osc133Handler {
            state: &mut self.state,
        };

        for byte in bytes {
            self.parser.advance(&mut handler, *byte);
        }
    }
}

/// Internal handler for VTE callbacks.
struct Osc133Handler<'a> {
    state: &'a mut Osc133State,
}

impl Perform for Osc133Handler<'_> {
    fn print(&mut self, _c: char) {
        // Regular printable character, ignore for state tracking
    }

    fn execute(&mut self, _byte: u8) {
        // C0/C1 control codes, ignore for state tracking
    }

    fn hook(&mut self, _params: &vte::Params, _intermediates: &[u8], _ignore: bool, _action: char) {
        // DCS hook, ignore for state tracking
    }

    fn put(&mut self, _byte: u8) {
        // DCS put, ignore for state tracking
    }

    fn unhook(&mut self) {
        // DCS unhook, ignore for state tracking
    }

    fn osc_dispatch(&mut self, params: &[&[u8]], _bell_terminated: bool) {
        // OSC sequences come in as params split by ';'
        // For OSC 133, we expect params like ["133", "A"] or ["133", "D", "0"]

        // Check if this is OSC 133
        if params.is_empty() {
            return;
        }

        // Convert first param to string to check for "133"
        let first = String::from_utf8_lossy(params[0]);
        if first != "133" {
            return;
        }

        // Get the command letter
        if params.len() < 2 {
            return;
        }

        let command = String::from_utf8_lossy(params[1]);

        match command.as_ref() {
            "A" => *self.state = Osc133State::Prompt,
            "B" => *self.state = Osc133State::Input,
            "C" => *self.state = Osc133State::Output,
            "D" => {
                // D can optionally have an exit code: D;N
                let exit_code = if params.len() >= 3 {
                    String::from_utf8_lossy(params[2])
                        .parse()
                        .unwrap_or(0)
                } else {
                    0
                };
                *self.state = Osc133State::Finished(exit_code);
            }
            _ => {
                // Unknown OSC 133 command, ignore
            }
        }
    }

    fn csi_dispatch(
        &mut self,
        _params: &vte::Params,
        _intermediates: &[u8],
        _ignore: bool,
        _action: char,
    ) {
        // CSI sequences, ignore for state tracking
    }

    fn esc_dispatch(&mut self, _intermediates: &[u8], _ignore: bool, _byte: u8) {
        // ESC sequences, ignore for state tracking
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_transitions() {
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

        // Back to PROMPT
        parser.process_bytes(b"\x1b]133;A\x07");
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        // FINISHED with non-zero exit code
        parser.process_bytes(b"\x1b]133;D;127\x07");
        assert_eq!(parser.current_state(), &Osc133State::Finished(127));
    }

    #[test]
    fn test_split_packet() {
        let mut parser = Osc133Parser::new();

        // Split the escape sequence across multiple reads
        parser.process_bytes(b"\x1b]");
        assert_eq!(parser.current_state(), &Osc133State::Unknown);

        parser.process_bytes(b"133;");
        assert_eq!(parser.current_state(), &Osc133State::Unknown);

        parser.process_bytes(b"A\x07");
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        // Split in different places
        let mut parser2 = Osc133Parser::new();
        parser2.process_bytes(b"\x1b]133");
        parser2.process_bytes(b";B");
        parser2.process_bytes(b"\x07");
        assert_eq!(parser2.current_state(), &Osc133State::Input);
    }

    #[test]
    fn test_bel_terminator() {
        let mut parser = Osc133Parser::new();

        // BEL terminator (\x07)
        parser.process_bytes(b"\x1b]133;A\x07");
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        parser.process_bytes(b"\x1b]133;B\x07");
        assert_eq!(parser.current_state(), &Osc133State::Input);

        parser.process_bytes(b"\x1b]133;C\x07");
        assert_eq!(parser.current_state(), &Osc133State::Output);

        parser.process_bytes(b"\x1b]133;D;42\x07");
        assert_eq!(parser.current_state(), &Osc133State::Finished(42));
    }

    #[test]
    fn test_st_terminator() {
        let mut parser = Osc133Parser::new();

        // ST terminator (\x1b\\)
        parser.process_bytes(b"\x1b]133;A\x1b\\");
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        parser.process_bytes(b"\x1b]133;B\x1b\\");
        assert_eq!(parser.current_state(), &Osc133State::Input);

        parser.process_bytes(b"\x1b]133;C\x1b\\");
        assert_eq!(parser.current_state(), &Osc133State::Output);

        parser.process_bytes(b"\x1b]133;D;1\x1b\\");
        assert_eq!(parser.current_state(), &Osc133State::Finished(1));
    }

    #[test]
    fn test_malformed() {
        let mut parser = Osc133Parser::new();

        // Empty OSC
        parser.process_bytes(b"\x1b]\x07");
        assert_eq!(parser.current_state(), &Osc133State::Unknown);

        // OSC 133 without command
        parser.process_bytes(b"\x1b]133\x07");
        assert_eq!(parser.current_state(), &Osc133State::Unknown);

        // Unknown command letter
        parser.process_bytes(b"\x1b]133;X\x07");
        assert_eq!(parser.current_state(), &Osc133State::Unknown);

        // Different OSC number
        parser.process_bytes(b"\x1b]0;title\x07");
        assert_eq!(parser.current_state(), &Osc133State::Unknown);

        // Incomplete escape sequence followed by valid one
        parser.process_bytes(b"\x1b]133\x1b]133;A\x07");
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        // D without exit code (should default to 0)
        let mut parser2 = Osc133Parser::new();
        parser2.process_bytes(b"\x1b]133;D\x07");
        assert_eq!(parser2.current_state(), &Osc133State::Finished(0));

        // D with invalid exit code (should default to 0)
        let mut parser3 = Osc133Parser::new();
        parser3.process_bytes(b"\x1b]133;D;abc\x07");
        assert_eq!(parser3.current_state(), &Osc133State::Finished(0));
    }

    #[test]
    fn test_utf8_lossy() {
        let mut parser = Osc133Parser::new();

        // Mix valid OSC 133 with invalid UTF-8 in the stream
        // Invalid UTF-8 sequence: 0x80 is not a valid start byte
        let mut bytes = Vec::new();
        bytes.extend_from_slice(b"some text \x80\x81\x82 more text");
        bytes.extend_from_slice(b"\x1b]133;A\x07");
        bytes.extend_from_slice(b"output with \xff invalid \xfe bytes");

        parser.process_bytes(&bytes);
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        // The parser should not crash and should still detect the state change
        parser.process_bytes(b"\x1b]133;B\x07");
        assert_eq!(parser.current_state(), &Osc133State::Input);
    }

    #[test]
    fn test_interleaved_content() {
        let mut parser = Osc133Parser::new();

        // OSC 133 sequences interleaved with regular terminal output
        parser.process_bytes(b"\x1b]133;A\x07$ ");
        assert_eq!(parser.current_state(), &Osc133State::Prompt);

        parser.process_bytes(b"ls -la\x1b]133;B\x07\x1b]133;C\x07");
        assert_eq!(parser.current_state(), &Osc133State::Output);

        parser.process_bytes(b"file1.txt\nfile2.txt\n\x1b]133;D;0\x07");
        assert_eq!(parser.current_state(), &Osc133State::Finished(0));
    }

    #[test]
    fn test_negative_exit_code() {
        let mut parser = Osc133Parser::new();

        // Negative exit codes (signal-based termination often uses negative values)
        parser.process_bytes(b"\x1b]133;D;-1\x07");
        assert_eq!(parser.current_state(), &Osc133State::Finished(-1));
    }
}
