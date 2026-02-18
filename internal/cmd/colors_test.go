package cmd

import (
	"testing"
)

func TestApplyColorMode_Always(t *testing.T) {
	// Save originals
	origMode := colorMode
	origRed := colorRed
	t.Cleanup(func() {
		colorMode = origMode
		colorRed = origRed
	})

	// Force disable first
	disableColors()
	if colorRed != "" {
		t.Fatal("expected colors disabled")
	}

	// Apply "always" mode — should re-enable
	colorMode = "always"
	applyColorMode()

	if colorRed == "" {
		t.Error("applyColorMode(\"always\") should enable colors even when auto would disable")
	}
}

func TestApplyColorMode_Never(t *testing.T) {
	origMode := colorMode
	origRed := colorRed
	t.Cleanup(func() {
		colorMode = origMode
		colorRed = origRed
	})

	// Force enable first
	enableColors()
	if colorRed == "" {
		t.Fatal("expected colors enabled")
	}

	// Apply "never" mode — should disable
	colorMode = "never"
	applyColorMode()

	if colorRed != "" {
		t.Error("applyColorMode(\"never\") should disable colors")
	}
}

func TestApplyColorMode_Auto(t *testing.T) {
	origMode := colorMode
	origRed := colorRed
	t.Cleanup(func() {
		colorMode = origMode
		colorRed = origRed
	})

	colorMode = "auto"
	applyColorMode()

	// In test, stdout is a pipe, so auto should disable colors
	if colorRed != "" {
		t.Error("applyColorMode(\"auto\") should disable colors when stdout is not a TTY")
	}
}

func TestEnableDisableColors(t *testing.T) {
	origRed := colorRed
	origGreen := colorGreen
	t.Cleanup(func() {
		colorRed = origRed
		colorGreen = origGreen
	})

	disableColors()
	if colorRed != "" || colorGreen != "" {
		t.Error("disableColors should clear all color codes")
	}

	enableColors()
	if colorRed == "" || colorGreen == "" {
		t.Error("enableColors should set color codes")
	}
}

func TestShouldDisableColors_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if !shouldDisableColors() {
		t.Error("shouldDisableColors should return true when NO_COLOR is set")
	}
}

func TestShouldDisableColors_TermDumb(t *testing.T) {
	t.Setenv("TERM", "dumb")
	// Unset NO_COLOR to isolate this test
	t.Setenv("NO_COLOR", "")
	if !shouldDisableColors() {
		t.Error("shouldDisableColors should return true when TERM=dumb")
	}
}

func TestTerminalWidth_Fallback(t *testing.T) {
	// When running in a test, stdout is a pipe, so ioctl should fail.
	// If COLUMNS is not set, should fall back to 80.
	t.Setenv("COLUMNS", "")
	w := terminalWidth()
	if w != 80 {
		t.Errorf("terminalWidth() = %d, want 80 (fallback)", w)
	}
}

func TestTerminalWidth_FromEnv(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	w := terminalWidth()
	if w != 120 {
		t.Errorf("terminalWidth() = %d, want 120 (from $COLUMNS)", w)
	}
}

func TestTerminalWidth_InvalidEnv(t *testing.T) {
	t.Setenv("COLUMNS", "notanumber")
	w := terminalWidth()
	if w != 80 {
		t.Errorf("terminalWidth() = %d, want 80 (fallback for invalid $COLUMNS)", w)
	}
}
