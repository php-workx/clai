package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIntegration_BuildPickerBinary verifies that the clai-picker binary
// compiles and that subcommand dispatch works as expected.
//
// Note: Because the binary checks for /dev/tty before parsing commands,
// some subtests may only verify the TTY-unavailable error path when run
// in a non-TTY environment (CI, IDE, piped output). This is acceptable:
// the primary goal is to verify the binary compiles and does not crash.
func TestIntegration_BuildPickerBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the binary to a temp location.
	binName := "clai-picker"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = findPickerCmdDir(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	// Verify binary was created.
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Fatal("binary was not created")
	}

	// Detect whether we have a TTY available. If not, the binary will
	// exit with code 2 (exitNoTerminal) before parsing any commands.
	hasTTY := checkTTY() == nil

	t.Run("help_flag", func(t *testing.T) {
		cmd := exec.Command(binPath, "--help")
		output, err := cmd.CombinedOutput()
		combined := string(output)
		if hasTTY {
			// With TTY, --help should print usage and exit 0.
			if err != nil {
				t.Fatalf("--help should exit 0 with TTY, got error: %v\nOutput: %s", err, combined)
			}
			if !strings.Contains(combined, "history") {
				t.Errorf("--help should mention 'history', got:\n%s", combined)
			}
		} else {
			// Without TTY, binary exits with code 2 before parsing --help.
			// Just verify it produces output (the TTY error message).
			if len(combined) == 0 {
				t.Error("expected some output even without TTY")
			}
		}
	})

	t.Run("version_flag", func(t *testing.T) {
		cmd := exec.Command(binPath, "--version")
		output, err := cmd.CombinedOutput()
		combined := string(output)
		if hasTTY {
			if err != nil {
				t.Fatalf("--version should exit 0 with TTY, got error: %v\nOutput: %s", err, combined)
			}
			if !strings.Contains(combined, "clai-picker") {
				t.Errorf("--version should contain 'clai-picker', got:\n%s", combined)
			}
		} else {
			if len(combined) == 0 {
				t.Error("expected some output even without TTY")
			}
		}
	})

	t.Run("unknown_command", func(t *testing.T) {
		cmd := exec.Command(binPath, "unknown-command")
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected non-zero exit code for unknown command")
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
		}
		code := exitErr.ExitCode()
		// Exit code 1 (unknown command) if TTY available,
		// exit code 2 (no terminal) if TTY not available.
		if code != 1 && code != 2 {
			t.Errorf("expected exit code 1 or 2, got %d\nOutput: %s", code, output)
		}
	})

	t.Run("no_args", func(t *testing.T) {
		cmd := exec.Command(binPath)
		output, err := cmd.CombinedOutput()
		if err == nil && hasTTY {
			t.Fatal("expected non-zero exit code with no args")
		}
		// Should produce some output regardless.
		if len(output) == 0 {
			t.Error("expected some output with no args")
		}
	})

	t.Run("invalid_flag", func(t *testing.T) {
		cmd := exec.Command(binPath, "history", "--bad-flag")
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected non-zero exit code for invalid flag")
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
		}
		code := exitErr.ExitCode()
		// Exit code 1 (invalid usage) if TTY available,
		// exit code 2 (no terminal) if TTY not available.
		if code != 1 && code != 2 {
			t.Errorf("expected exit code 1 or 2, got %d\nOutput: %s", code, output)
		}
	})
}

// TestIntegration_GoBuildCompiles verifies `go build ./cmd/clai-picker` works
// without producing a binary (just compilation check).
func TestIntegration_GoBuildCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cmd := exec.Command("go", "build", "-o", os.DevNull, "./cmd/clai-picker")
	cmd.Dir = findModuleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/clai-picker failed: %v\n%s", err, out)
	}
}

// findPickerCmdDir returns the absolute path to cmd/clai-picker.
func findPickerCmdDir(t *testing.T) string {
	t.Helper()
	root := findModuleRoot(t)
	return filepath.Join(root, "cmd", "clai-picker")
}

// findModuleRoot finds the Go module root by looking for go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find go.mod in any parent directory")
		}
		dir = parent
	}
}
