package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ShellDetection contains the result of shell detection.
type ShellDetection struct {
	Shell     string // "zsh", "bash", "fish", or ""
	Confident bool   // true if detected via parent process, false if fell back to $SHELL
	Active    bool   // true if clai shell integration is active in this shell
}

// DetectShell detects the current shell and whether clai is active in it.
// This is the single source of truth for shell detection across all commands.
func DetectShell() ShellDetection {
	result := ShellDetection{}

	// 1. Detect the actual shell via parent process (most reliable)
	if shell := detectParentShell(); shell != "" {
		result.Shell = shell
		result.Confident = true
	}

	// 2. If parent detection failed, try $SHELL (login shell - less reliable)
	if result.Shell == "" {
		if shellEnv := os.Getenv("SHELL"); shellEnv != "" {
			base := filepath.Base(shellEnv)
			if isKnownShell(base) {
				result.Shell = base
				result.Confident = false // This is login shell, might not be current
			}
		}
	}

	// 3. On Windows, default to bash (Git Bash is common)
	if result.Shell == "" && runtime.GOOS == "windows" {
		result.Shell = "bash"
		result.Confident = false
	}

	// 4. Check if clai integration is active in this shell
	// CLAI_CURRENT_SHELL is set by shell integration scripts
	claiShell := os.Getenv("CLAI_CURRENT_SHELL")
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if claiShell == result.Shell && sessionID != "" {
		result.Active = true
	}

	return result
}

// detectParentShell detects the shell by checking the parent process name.
func detectParentShell() string {
	ppid := os.Getppid()
	if ppid <= 0 {
		return ""
	}

	// Try reading from /proc (Linux)
	commPath := fmt.Sprintf("/proc/%d/comm", ppid)
	if data, err := os.ReadFile(commPath); err == nil { //nolint:gosec // G304: /proc path constructed from trusted PID
		name := strings.TrimSpace(string(data))
		if shell := extractShellName(name); shell != "" {
			return shell
		}
	}

	// Fall back to ps command (macOS, BSD)
	cmd := exec.Command("ps", "-p", fmt.Sprintf("%d", ppid), "-o", "comm=") //nolint:gosec // G204: ppid is from os.Getppid()
	if output, err := cmd.Output(); err == nil {
		name := strings.TrimSpace(string(output))
		if shell := extractShellName(name); shell != "" {
			return shell
		}
	}

	return ""
}

// extractShellName extracts the shell name from a process path or name.
func extractShellName(name string) string {
	// Handle paths like /bin/zsh or /usr/local/bin/bash
	base := filepath.Base(name)
	// Handle login shell indicator (e.g., -zsh -> zsh)
	base = strings.TrimPrefix(base, "-")
	// Handle names like "bash-3.2" or "zsh-5.9"
	if idx := strings.Index(base, "-"); idx > 0 {
		base = base[:idx]
	}
	// Only return if it's a known shell
	if isKnownShell(base) {
		return base
	}
	return ""
}

// isKnownShell returns true if the shell name is one we support.
func isKnownShell(name string) bool {
	switch name {
	case "zsh", "bash", "fish":
		return true
	}
	return false
}

// SupportedShells returns the list of supported shell names.
func SupportedShells() []string {
	return []string{"zsh", "bash", "fish"}
}
