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
- Binary and Claude CLI availability
- Shell integration status
- Daemon status
- Storage and configuration

Examples:
  clai status`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

type statusCheck struct {
	name    string
	status  string // "ok", "warn", "error"
	message string
}

func runStatus(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()

	fmt.Printf("%sclai status%s\n", colorBold, colorReset)
	fmt.Println(strings.Repeat("-", 40))

	checks := make([]statusCheck, 0, 5)

	// Check Claude CLI
	checks = append(checks, checkClaudeCLI())

	// Check shell integration
	checks = append(checks, checkShellStatus(paths))

	// Check daemon
	checks = append(checks, checkDaemonStatus())

	// Check storage
	checks = append(checks, checkStorage(paths))

	// Check configuration
	checks = append(checks, checkConfig(paths))

	// Print results
	hasErrors := false
	hasWarnings := false

	for _, c := range checks {
		var statusIcon string
		switch c.status {
		case "ok":
			statusIcon = colorGreen + "[OK]" + colorReset
		case "warn":
			statusIcon = colorYellow + "[WARN]" + colorReset
			hasWarnings = true
		case "error":
			statusIcon = colorRed + "[ERROR]" + colorReset
			hasErrors = true
		}

		fmt.Printf("  %s %-12s %s%s%s\n", statusIcon, c.name, colorDim, c.message, colorReset)
	}

	fmt.Println()

	if hasErrors {
		fmt.Printf("%sSome checks failed.%s\n", colorRed, colorReset)
		return fmt.Errorf("status check found errors")
	}

	if hasWarnings {
		fmt.Printf("%sAll critical checks passed.%s\n", colorYellow, colorReset)
	} else {
		fmt.Printf("%sAll checks passed!%s\n", colorGreen, colorReset)
	}

	return nil
}

func checkClaudeCLI() statusCheck {
	path, err := exec.LookPath("claude")
	if err != nil {
		return statusCheck{
			name:    "Claude CLI",
			status:  "error",
			message: "not found (install from claude.ai/cli)",
		}
	}
	return statusCheck{
		name:    "Claude CLI",
		status:  "ok",
		message: path,
	}
}

func checkShellStatus(paths *config.Paths) statusCheck {
	shells := checkShellIntegrationWithPaths(paths)
	if len(shells) == 0 {
		return statusCheck{
			name:    "Shell",
			status:  "warn",
			message: "not installed (run 'clai install')",
		}
	}
	// Compact format - just show the shell names
	return statusCheck{
		name:    "Shell",
		status:  "ok",
		message: strings.Join(shells, ", "),
	}
}

func checkDaemonStatus() statusCheck {
	if daemon.IsRunning() {
		return statusCheck{
			name:    "Daemon",
			status:  "ok",
			message: "running",
		}
	}
	return statusCheck{
		name:    "Daemon",
		status:  "warn",
		message: "not running (starts automatically)",
	}
}

func checkStorage(paths *config.Paths) statusCheck {
	baseDir := paths.BaseDir
	dbFile := paths.DatabaseFile()

	// Check if base directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return statusCheck{
			name:    "Storage",
			status:  "warn",
			message: fmt.Sprintf("%s (will be created)", baseDir),
		}
	}

	// Check database size
	dbSize := ""
	if info, err := os.Stat(dbFile); err == nil {
		dbSize = fmt.Sprintf(" (db: %s)", formatSize(info.Size()))
	}

	return statusCheck{
		name:    "Storage",
		status:  "ok",
		message: baseDir + dbSize,
	}
}

func checkConfig(paths *config.Paths) statusCheck {
	configFile := paths.ConfigFile()

	// Try to load and validate config
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		return statusCheck{
			name:    "Config",
			status:  "error",
			message: fmt.Sprintf("failed to load: %v", err),
		}
	}

	if err := cfg.Validate(); err != nil {
		return statusCheck{
			name:    "Config",
			status:  "error",
			message: fmt.Sprintf("invalid: %v", err),
		}
	}

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return statusCheck{
			name:    "Config",
			status:  "ok",
			message: "using defaults",
		}
	}

	return statusCheck{
		name:    "Config",
		status:  "ok",
		message: configFile,
	}
}

func checkShellIntegrationWithPaths(paths *config.Paths) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Use consolidated shell detection
	detection := DetectShell()
	var installed []string

	// Check if clai integration is active in THIS shell
	if detection.Active {
		installed = append(installed, fmt.Sprintf("%s (active)", detection.Shell))
		return installed
	}

	// Not an active session - check RC files for the current shell only
	shellToCheck := detection.Shell

	// Check zsh
	if shellToCheck == "" || shellToCheck == "zsh" {
		zshrc := filepath.Join(home, ".zshrc")
		if ok, _, _ := isInstalled(zshrc, filepath.Join(paths.HooksDir(), "clai.zsh"), "zsh"); ok {
			installed = append(installed, "zsh")
		}
	}

	// Check bash
	if shellToCheck == "" || shellToCheck == "bash" {
		bashrc := filepath.Join(home, ".bashrc")
		if ok, _, _ := isInstalled(bashrc, filepath.Join(paths.HooksDir(), "clai.bash"), "bash"); ok {
			installed = append(installed, "bash")
		}

		bashProfile := filepath.Join(home, ".bash_profile")
		if ok, _, _ := isInstalled(bashProfile, filepath.Join(paths.HooksDir(), "clai.bash"), "bash"); ok {
			if len(installed) == 0 || installed[len(installed)-1] != "bash" {
				installed = append(installed, "bash")
			}
		}
	}

	// Check fish
	if shellToCheck == "" || shellToCheck == "fish" {
		fishConfig := filepath.Join(home, ".config", "fish", "config.fish")
		if ok, _, _ := isInstalled(fishConfig, filepath.Join(paths.HooksDir(), "clai.fish"), "fish"); ok {
			installed = append(installed, "fish")
		}
	}

	return installed
}

func checkShellIntegration() []string {
	return checkShellIntegrationWithPaths(config.DefaultPaths())
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
