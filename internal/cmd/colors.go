package cmd

import (
	"os"
	"runtime"
	"strconv"
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

// colorMode holds the --color flag value. Valid values: "auto", "always", "never".
var colorMode = "auto"

func init() {
	// Disable colors if not a terminal or on Windows without ANSI support
	if shouldDisableColors() {
		disableColors()
	}
}

// applyColorMode applies the --color flag value to color state.
// Call this after cobra flag parsing to honor the user's explicit preference.
func applyColorMode() {
	switch colorMode {
	case "always":
		enableColors()
	case "never":
		disableColors()
	default: // "auto"
		if shouldDisableColors() {
			disableColors()
		} else {
			enableColors()
		}
	}
}

func enableColors() {
	colorRed = "\033[0;31m"
	colorGreen = "\033[0;32m"
	colorYellow = "\033[0;33m"
	colorCyan = "\033[0;36m"
	colorDim = "\033[2m"
	colorBold = "\033[1m"
	colorReset = "\033[0m"
}

func disableColors() {
	colorRed = ""
	colorGreen = ""
	colorYellow = ""
	colorCyan = ""
	colorDim = ""
	colorBold = ""
	colorReset = ""
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

	// Check if stdout is a TTY (not a pipe or file)
	if info, err := os.Stdout.Stat(); err == nil {
		if (info.Mode() & os.ModeCharDevice) == 0 {
			return true
		}
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

// terminalWidth returns the width of the terminal in columns.
// Uses platform-specific ioctl via getTermWidthIoctl (build-tagged),
// falls back to $COLUMNS, then to 80.
func terminalWidth() int {
	// Try ioctl first (platform-specific, defined in colors_unix.go / colors_windows.go)
	if w := getTermWidthIoctl(); w > 0 {
		return w
	}
	// Fall back to $COLUMNS environment variable
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if w, err := strconv.Atoi(cols); err == nil && w > 0 {
			return w
		}
	}
	return 80
}
