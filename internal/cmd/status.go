package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show clai status",
	Long: `Show the current status of clai, including:
- Daemon status (running/stopped)
- Configuration file location
- Database location
- Shell integration status

Examples:
  clai status`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	cfg, _ := config.Load() // Ignore error, use defaults

	fmt.Printf("%sclai Status%s\n", colorBold, colorReset)
	fmt.Println(strings.Repeat("-", 40))

	// Daemon status (clai daemon - handles shell integration requests)
	fmt.Printf("\n%sDaemon:%s\n", colorBold, colorReset)
	if daemon.IsRunning() {
		fmt.Printf("  Status:  %srunning%s\n", colorGreen, colorReset)
	} else {
		fmt.Printf("  Status:  %snot running%s\n", colorDim, colorReset)
		fmt.Printf("  Starts automatically when shell integration loads.\n")
	}

	// Configuration
	fmt.Printf("\n%sConfiguration:%s\n", colorBold, colorReset)
	configFile := paths.ConfigFile()
	if _, err := os.Stat(configFile); err == nil {
		fmt.Printf("  File:    %s\n", configFile)
	} else {
		fmt.Printf("  File:    %s (not found, using defaults)\n", configFile)
	}
	fmt.Printf("  AI:      %s\n", formatBool(cfg.AI.Enabled))
	fmt.Printf("  Provider: %s\n", cfg.AI.Provider)

	// Storage
	fmt.Printf("\n%sStorage:%s\n", colorBold, colorReset)
	dbFile := paths.DatabaseFile()
	if info, err := os.Stat(dbFile); err == nil {
		fmt.Printf("  Database: %s (%s)\n", dbFile, formatSize(info.Size()))
	} else {
		fmt.Printf("  Database: %s (not created)\n", dbFile)
	}

	// Cache
	cacheDir := paths.CacheDir()
	if info, err := os.Stat(cacheDir); err == nil && info.IsDir() {
		fmt.Printf("  Cache:    %s\n", cacheDir)
	}

	// Shell integration
	fmt.Printf("\n%sShell Integration:%s\n", colorBold, colorReset)
	shells := checkShellIntegrationWithPaths(paths)
	if len(shells) == 0 {
		fmt.Printf("  Status:  %snot installed%s\n", colorDim, colorReset)
		fmt.Printf("  Run 'clai install' to set up shell integration.\n")
	} else {
		fmt.Printf("  Status:  %sinstalled%s\n", colorGreen, colorReset)
		for _, s := range shells {
			fmt.Printf("  - %s\n", s)
		}
	}

	// Quick stats if database exists
	if _, err := os.Stat(dbFile); err == nil {
		printQuickStats(paths)
	}

	return nil
}

// detectActualShell detects the actual running shell.
// First tries parent process detection (most reliable for child processes),
// then falls back to shell version env vars (which may not be exported).
func detectActualShell() string {
	// Check parent process - when user runs `clai doctor` from bash,
	// the parent process IS bash
	if shell := detectParentShell(); shell != "" {
		return shell
	}
	// Fall back to shell-specific version variables
	// Note: These are usually not exported, so this rarely works for child processes
	if os.Getenv("BASH_VERSION") != "" {
		return "bash"
	}
	if os.Getenv("ZSH_VERSION") != "" {
		return "zsh"
	}
	if os.Getenv("FISH_VERSION") != "" {
		return "fish"
	}
	return ""
}

// detectParentShell detects the shell by checking the parent process name.
func detectParentShell() string {
	ppid := os.Getppid()
	if ppid <= 0 {
		return ""
	}

	// Try reading from /proc (Linux)
	commPath := fmt.Sprintf("/proc/%d/comm", ppid)
	if data, err := os.ReadFile(commPath); err == nil {
		name := strings.TrimSpace(string(data))
		return extractShellName(name)
	}

	// Fall back to ps command (macOS, BSD)
	cmd := exec.Command("ps", "-p", fmt.Sprintf("%d", ppid), "-o", "comm=")
	if output, err := cmd.Output(); err == nil {
		name := strings.TrimSpace(string(output))
		return extractShellName(name)
	}

	return ""
}

// extractShellName extracts the shell name from a process path or name.
func extractShellName(name string) string {
	// Handle paths like /bin/zsh or /usr/local/bin/bash
	base := filepath.Base(name)
	// Handle names like "bash-3.2" or "zsh-5.9"
	if idx := strings.Index(base, "-"); idx > 0 {
		base = base[:idx]
	}
	// Only return if it's a known shell
	switch base {
	case "zsh", "bash", "fish":
		return base
	}
	return ""
}

// detectCurrentShell returns the current shell name (zsh, bash, fish, etc.)
// Uses shell-specific env vars first, then CLAI_CURRENT_SHELL, then $SHELL.
func detectCurrentShell() string {
	// First try to detect actual shell via version variables
	if shell := detectActualShell(); shell != "" {
		return shell
	}
	// CLAI_CURRENT_SHELL is set by shell integration scripts
	// Note: This may be inherited from parent shell, so less reliable
	if shell := os.Getenv("CLAI_CURRENT_SHELL"); shell != "" {
		return shell
	}
	// Fall back to SHELL (login shell) - least accurate
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	// Extract shell name from path (e.g., /bin/zsh -> zsh)
	return filepath.Base(shell)
}

func checkShellIntegrationWithPaths(paths *config.Paths) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Detect the ACTUAL shell we're running in (using version env vars)
	actualShell := detectActualShell()
	var installed []string

	// Check if clai integration is active in THIS shell
	// CLAI_CURRENT_SHELL/CLAI_SESSION_ID might be inherited from parent shell,
	// so we only report "active session" if it matches the actual shell
	claiCurrentShell := os.Getenv("CLAI_CURRENT_SHELL")
	sessionID := os.Getenv("CLAI_SESSION_ID")

	if actualShell != "" && claiCurrentShell == actualShell && sessionID != "" {
		// Active session in the current shell
		installed = append(installed, fmt.Sprintf("%s (active session)", actualShell))
		return installed
	}

	// Not an active session - check RC files for the current shell only
	// We focus on the current shell, not all shells
	shellToCheck := actualShell
	if shellToCheck == "" {
		shellToCheck = detectCurrentShell() // Fall back to other detection methods
	}

	// Check zsh
	if shellToCheck == "" || shellToCheck == "zsh" {
		zshrc := filepath.Join(home, ".zshrc")
		if ok, _, _ := isInstalled(zshrc, filepath.Join(paths.HooksDir(), "clai.zsh"), "zsh"); ok {
			installed = append(installed, "zsh (.zshrc)")
		}
	}

	// Check bash
	if shellToCheck == "" || shellToCheck == "bash" {
		bashrc := filepath.Join(home, ".bashrc")
		if ok, _, _ := isInstalled(bashrc, filepath.Join(paths.HooksDir(), "clai.bash"), "bash"); ok {
			installed = append(installed, "bash (.bashrc)")
		}

		bashProfile := filepath.Join(home, ".bash_profile")
		if ok, _, _ := isInstalled(bashProfile, filepath.Join(paths.HooksDir(), "clai.bash"), "bash"); ok {
			installed = append(installed, "bash (.bash_profile)")
		}
	}

	// Check fish
	if shellToCheck == "" || shellToCheck == "fish" {
		fishConfig := filepath.Join(home, ".config", "fish", "config.fish")
		if ok, _, _ := isInstalled(fishConfig, filepath.Join(paths.HooksDir(), "clai.fish"), "fish"); ok {
			installed = append(installed, "fish (config.fish)")
		}
	}

	return installed
}

func checkShellIntegration() []string {
	return checkShellIntegrationWithPaths(config.DefaultPaths())
}

func printQuickStats(paths *config.Paths) {
	// This would ideally query the database, but for now we'll keep it simple
	fmt.Printf("\n%sQuick Stats:%s\n", colorBold, colorReset)
	fmt.Printf("  Run 'clai history --limit=5' to see recent commands.\n")
}

func formatBool(b bool) string {
	if b {
		return colorGreen + "enabled" + colorReset
	}
	return colorDim + "disabled" + colorReset
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
