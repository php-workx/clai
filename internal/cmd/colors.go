package cmd

import (
	"os"
	"runtime"
)

// ANSI color codes for terminal output.
// These are initialized in init() and may be disabled on certain platforms.
var (
	colorRed    = "\033[0;31m"
	colorGreen  = "\033[0;32m"
	colorYellow = "\033[0;33m"
	colorCyan   = "\033[0;36m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

func init() {
	// Disable colors if not a terminal or on Windows without ANSI support
	if shouldDisableColors() {
		colorRed = ""
		colorGreen = ""
		colorYellow = ""
		colorCyan = ""
		colorDim = ""
		colorBold = ""
		colorReset = ""
	}
}

func shouldDisableColors() bool {
	// Check NO_COLOR environment variable (https://no-color.org/)
	if os.Getenv("NO_COLOR") != "" {
		return true
	}

	// Check TERM=dumb
	if os.Getenv("TERM") == "dumb" {
		return true
	}

	// On Windows, check if ANSI is supported
	if runtime.GOOS == "windows" {
		// Windows Terminal and newer terminals support ANSI
		// Check for common indicators
		if os.Getenv("WT_SESSION") != "" {
			return false // Windows Terminal supports ANSI
		}
		if os.Getenv("TERM_PROGRAM") != "" {
			return false // Modern terminal emulator
		}
		// Disable by default on older Windows consoles
		return os.Getenv("ANSICON") == "" && os.Getenv("ConEmuANSI") != "ON"
	}

	return false
}
