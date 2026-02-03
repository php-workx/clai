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
	Use:     "status",
	Short:   "Show clai status",
	GroupID: groupSetup,
	Long: `Show the current status of clai, including:
- Binary and Claude CLI availability
- Shell integration status
- Daemon status
- Storage and configuration

Examples:
  clai status`,
	RunE: runStatus,
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

	checks := make([]statusCheck, 0, 6)

	// Check Claude CLI
	checks = append(checks, checkClaudeCLI())

	// Check shell integration
	checks = append(checks, checkShellStatus(paths))

	// Check session ID
	checks = append(checks, checkSessionID())

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

func checkSessionID() statusCheck {
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		return statusCheck{
			name:    "Session",
			status:  "warn",
			message: "not set (shell integration not active)",
		}
	}
	// Show shortened session ID for readability
	shortID := sessionID
	if len(sessionID) > 8 {
		shortID = sessionID[:8]
	}
	return statusCheck{
		name:    "Session",
		status:  "ok",
		message: shortID,
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

	detection := DetectShell()
	if detection.Active {
		return []string{fmt.Sprintf("%s (active)", detection.Shell)}
	}

	type shellConfig struct {
		name    string
		rcFiles []string
	}

	shells := []shellConfig{
		{"zsh", []string{filepath.Join(home, ".zshrc")}},
		{"bash", []string{
			filepath.Join(home, ".bashrc"),
			filepath.Join(home, ".bash_profile"),
		}},
		{"fish", []string{filepath.Join(home, ".config", "fish", "config.fish")}},
	}

	var installed []string
	for _, sh := range shells {
		if detection.Shell != "" && detection.Shell != sh.name {
			continue
		}
		hookFile := filepath.Join(paths.HooksDir(), "clai."+sh.name)
		for _, rc := range sh.rcFiles {
			if ok, _, _ := isInstalled(rc, hookFile, sh.name); ok {
				installed = append(installed, sh.name)
				break
			}
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
