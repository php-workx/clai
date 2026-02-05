package main

import (
	"fmt"
	"os"
	"strings"
)

// sessionStartConfig holds the parsed configuration for the session-start command.
type sessionStartConfig struct {
	// No flags for now, but struct exists for future expansion
}

// parseSessionStartArgs parses the command line arguments for session-start.
func parseSessionStartArgs(args []string) (*sessionStartConfig, error) {
	cfg := &sessionStartConfig{}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return nil, fmt.Errorf("unknown flag: %s", arg)
		}
		// Ignore positional arguments
	}

	return cfg, nil
}

// runSessionStart handles the session-start subcommand.
// It requests a session ID from the daemon and writes it to a session file.
//
// Per spec Section 6.5, Strategy A (daemon-assigned):
//   - Shell calls `clai-hook session-start` once per shell instance.
//   - clai-hook asks daemon for a session ID with micro-timeout.
//   - clai-hook writes session ID to a temp file.
//   - Shell hook reads it and exports CLAI_SESSION_ID for future calls.
//
// If daemon is not reachable, shell hooks should fall back to Strategy B
// (shell-local generation).
//
// Exit codes:
//   - 0: Success (or daemon unavailable - caller should use fallback)
//   - 1: Invalid arguments
func runSessionStart(args []string) int {
	// Parse command line arguments
	_, err := parseSessionStartArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-hook session-start: %v\n", err)
		return 1
	}

	// For now, this is a skeleton that will be implemented in SUGG-006.
	// The daemon transport layer needs to be implemented first.
	//
	// Future implementation will:
	// 1. Connect to daemon with micro-timeout (15ms)
	// 2. Request a new session ID
	// 3. Write it to ${XDG_RUNTIME_DIR}/clai/session.$PPID
	// 4. Print success or fail silently

	return 0
}
