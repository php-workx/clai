package cmd

import (
	"fmt"
	"os"
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

// detectCurrentShell returns the current shell name (zsh, bash, fish, etc.)
// First checks CLAI_CURRENT_SHELL (set by shell integration scripts),
// then falls back to SHELL environment variable.
func detectCurrentShell() string {
	// CLAI_CURRENT_SHELL is set by shell integration scripts and reflects
	// the actual running shell, not the login shell
	if shell := os.Getenv("CLAI_CURRENT_SHELL"); shell != "" {
		return shell
	}
	// Fall back to SHELL (login shell) - less accurate but better than nothing
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

	currentShell := detectCurrentShell()
	var installed []string

	// If CLAI_CURRENT_SHELL and CLAI_SESSION_ID are set, shell integration is active
	// even if not installed in RC files (e.g., running via eval "$(clai init zsh)")
	// Note: We use CLAI_CURRENT_SHELL directly (not detectCurrentShell) because
	// an "active session" requires the shell integration to have run and set this var.
	claiCurrentShell := os.Getenv("CLAI_CURRENT_SHELL")
	if claiCurrentShell != "" && os.Getenv("CLAI_SESSION_ID") != "" {
		installed = append(installed, fmt.Sprintf("%s (active session)", claiCurrentShell))
		return installed
	}

	// Check zsh
	if currentShell == "" || currentShell == "zsh" {
		zshrc := filepath.Join(home, ".zshrc")
		if ok, _, _ := isInstalled(zshrc, filepath.Join(paths.HooksDir(), "clai.zsh"), "zsh"); ok {
			installed = append(installed, "zsh (.zshrc)")
		}
	}

	// Check bash
	if currentShell == "" || currentShell == "bash" {
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
	if currentShell == "" || currentShell == "fish" {
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
