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

	// Daemon status
	fmt.Printf("\n%sDaemon:%s\n", colorBold, colorReset)
	if daemon.IsRunning() {
		fmt.Printf("  Status:  %srunning%s\n", colorGreen, colorReset)
		// Try to get PID
		pidFile := paths.PIDFile()
		if data, err := os.ReadFile(pidFile); err == nil {
			fmt.Printf("  PID:     %s\n", strings.TrimSpace(string(data)))
		}
	} else {
		fmt.Printf("  Status:  %snot running%s\n", colorDim, colorReset)
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
	cacheDir := paths.CacheDir
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

func checkShellIntegrationWithPaths(paths *config.Paths) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var installed []string

	// Check zsh
	zshrc := filepath.Join(home, ".zshrc")
	if ok, _, _ := isInstalled(zshrc, filepath.Join(paths.HooksDir(), "clai.zsh"), "zsh"); ok {
		installed = append(installed, "zsh (.zshrc)")
	}

	// Check bash
	bashrc := filepath.Join(home, ".bashrc")
	if ok, _, _ := isInstalled(bashrc, filepath.Join(paths.HooksDir(), "clai.bash"), "bash"); ok {
		installed = append(installed, "bash (.bashrc)")
	}

	bashProfile := filepath.Join(home, ".bash_profile")
	if ok, _, _ := isInstalled(bashProfile, filepath.Join(paths.HooksDir(), "clai.bash"), "bash"); ok {
		installed = append(installed, "bash (.bash_profile)")
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
