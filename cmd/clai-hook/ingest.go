package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/runger/clai/internal/suggestions/event"
)

// ingestConfig holds the parsed configuration for the ingest command.
type ingestConfig struct {
	cmdStdin bool // Read command from stdin instead of CLAI_CMD
}

// parseIngestArgs parses the command line arguments for the ingest command.
func parseIngestArgs(args []string) (*ingestConfig, error) {
	cfg := &ingestConfig{}

	for _, arg := range args {
		switch arg {
		case "--cmd-stdin":
			cfg.cmdStdin = true
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown flag: %s", arg)
			}
			// Ignore positional arguments
		}
	}

	return cfg, nil
}

// runIngest handles the ingest subcommand.
// It reads command event data from environment variables (and optionally stdin),
// validates the input, builds an event struct, and returns success.
//
// Exit codes:
//   - 0: Success (or daemon unavailable - silent drop)
//   - 1: Invalid arguments
func runIngest(args []string) int {
	// Check for no-record flag first
	if os.Getenv("CLAI_NO_RECORD") == "1" {
		// Skip ingestion entirely - this is expected behavior
		return 0
	}

	// Parse command line arguments
	cfg, err := parseIngestArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-hook ingest: %v\n", err)
		return 1
	}

	// Read and validate environment variables
	ev, err := readIngestEnv(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-hook ingest: %v\n", err)
		return 1
	}

	// Event is now built and validated.
	// In the future (SUGG-006), we will send this to the daemon.
	// For now, we just return success.
	_ = ev

	return 0
}

// readIngestEnv reads the environment variables and builds a CommandEvent.
// It returns an error if any required field is missing or invalid.
func readIngestEnv(cfg *ingestConfig) (*event.CommandEvent, error) {
	ev := event.NewCommandEvent()

	cmdRaw, err := readCommandRaw(cfg)
	if err != nil {
		return nil, err
	}

	// Perform lossy UTF-8 conversion (spec requirement 6.3)
	ev.CmdRaw = toValidUTF8(cmdRaw)

	cwd, err := readRequiredEnv("CLAI_CWD")
	if err != nil {
		return nil, err
	}
	exitCode, err := readRequiredInt("CLAI_EXIT")
	if err != nil {
		return nil, err
	}
	ts, err := readRequiredInt64("CLAI_TS")
	if err != nil {
		return nil, err
	}
	shell, err := readRequiredShell()
	if err != nil {
		return nil, err
	}
	sessionID, err := readRequiredEnv("CLAI_SESSION_ID")
	if err != nil {
		return nil, err
	}

	ev.Cwd = cwd
	ev.ExitCode = exitCode
	ev.TS = ts
	ev.Shell = shell
	ev.SessionID = sessionID

	// Read optional fields
	if durationStr := os.Getenv("CLAI_DURATION_MS"); durationStr != "" {
		duration, err := strconv.ParseInt(durationStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CLAI_DURATION_MS must be an integer: %w", err)
		}
		ev.DurationMs = &duration
	}

	if os.Getenv("CLAI_EPHEMERAL") == "1" {
		ev.Ephemeral = true
	}

	return ev, nil
}

func readCommandRaw(cfg *ingestConfig) (string, error) {
	if !cfg.cmdStdin {
		cmdRaw := os.Getenv("CLAI_CMD")
		if cmdRaw == "" {
			return "", fmt.Errorf("CLAI_CMD is required (or use --cmd-stdin)")
		}
		return cmdRaw, nil
	}

	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read command from stdin: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}

func readRequiredEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return v, nil
}

func readRequiredInt(name string) (int, error) {
	v, err := readRequiredEnv(name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

func readRequiredInt64(name string) (int64, error) {
	v, err := readRequiredEnv(name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

func readRequiredShell() (event.Shell, error) {
	v, err := readRequiredEnv("CLAI_SHELL")
	if err != nil {
		return "", err
	}
	if !event.ValidShell(v) {
		return "", fmt.Errorf("CLAI_SHELL must be one of: bash, zsh, fish")
	}
	return event.Shell(v), nil
}

// toValidUTF8 performs lossy UTF-8 conversion by replacing invalid bytes
// with the Unicode replacement character (U+FFFD).
// This ensures the string can be safely encoded to JSON.
func toValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	// Build a new string with invalid bytes replaced
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid byte, replace with replacement character
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteRune(r)
		}
		i += size
	}

	return b.String()
}
