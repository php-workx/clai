// clai-hook is the shell hook binary for ingesting command events.
// It reads command execution data from environment variables or stdin
// and sends events to the daemon for processing.
//
// This binary is designed for minimal startup time and fire-and-forget
// behavior. It never blocks the user's shell prompt.
//
// Subcommands:
//   - ingest: Ingest a command execution event
//   - session-start: Request a session ID from the daemon
//
// See specs/tech_suggestions_v3.md for details.
package main

import (
	"fmt"
	"os"
)

// Version info - injected at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var exitCode int
	switch cmd {
	case "ingest":
		exitCode = runIngest(args)
	case "session-start":
		exitCode = runSessionStart(args)
	case "version", "--version", "-v":
		printVersion()
		exitCode = 0
	case "help", "--help", "-h":
		printUsage()
		exitCode = 0
	default:
		fmt.Fprintf(os.Stderr, "clai-hook: unknown command: %s\n", cmd)
		printUsage()
		exitCode = 1
	}

	os.Exit(exitCode)
}

func printVersion() {
	fmt.Printf("clai-hook %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `clai-hook - Shell hook for clai command ingestion

Usage: clai-hook <command> [flags...]

Commands:
  ingest           Ingest a command event from environment variables
  session-start    Request a session ID from daemon

Environment variables for 'ingest':
  CLAI_CMD         Raw command string (required unless --cmd-stdin)
  CLAI_CWD         Current working directory (required)
  CLAI_EXIT        Exit code of command (required)
  CLAI_TS          Timestamp in Unix milliseconds (required)
  CLAI_SHELL       Shell type: bash, zsh, fish (required)
  CLAI_SESSION_ID  Session identifier (required)
  CLAI_DURATION_MS Command duration in milliseconds (optional)
  CLAI_EPHEMERAL   If "1", event is ephemeral/incognito (optional)
  CLAI_NO_RECORD   If "1", skip ingestion entirely (optional)

Flags for 'ingest':
  --cmd-stdin      Read command from stdin instead of CLAI_CMD

Exit codes:
  0  Success (or daemon unavailable - silent drop)
  1  Invalid arguments

For more information, see: https://github.com/runger/clai`)
}
