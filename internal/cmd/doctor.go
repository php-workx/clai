package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check clai installation and dependencies",
	Long: `Run diagnostic checks on your clai installation.

This command checks:
- Binary installation
- Shell integration
- Daemon status
- AI provider availability
- Configuration validity
- File permissions

Examples:
  clai doctor`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

type checkResult struct {
	name    string
	status  string // "ok", "warn", "error"
	message string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Printf("%sclai Doctor%s\n", colorBold, colorReset)
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println()

	results := make([]checkResult, 0, 16)

	// Check binary
	results = append(results, checkBinary())

	// Check directories
	results = append(results, checkDirectories()...)

	// Check configuration
	results = append(results, checkConfiguration())

	// Check shell integration
	results = append(results, checkShellIntegrationDoctor())

	// Check daemon
	results = append(results, checkDaemon())

	// Check AI providers
	results = append(results, checkAIProviders()...)

	// Print results
	hasErrors := false
	hasWarnings := false

	for _, r := range results {
		var statusIcon string
		switch r.status {
		case "ok":
			statusIcon = colorGreen + "[OK]" + colorReset
		case "warn":
			statusIcon = colorYellow + "[WARN]" + colorReset
			hasWarnings = true
		case "error":
			statusIcon = colorRed + "[ERROR]" + colorReset
			hasErrors = true
		}

		fmt.Printf("  %s %s\n", statusIcon, r.name)
		if r.message != "" {
			fmt.Printf("       %s%s%s\n", colorDim, r.message, colorReset)
		}
	}

	fmt.Println()

	if hasErrors {
		fmt.Printf("%sSome checks failed. Please fix the errors above.%s\n", colorRed, colorReset)
		return fmt.Errorf("doctor found errors")
	}

	if hasWarnings {
		fmt.Printf("%sAll critical checks passed, but there are warnings.%s\n", colorYellow, colorReset)
	} else {
		fmt.Printf("%sAll checks passed!%s\n", colorGreen, colorReset)
	}

	return nil
}

func checkBinary() checkResult {
	// Check if clai is in PATH
	path, err := exec.LookPath("clai")
	if err != nil {
		return checkResult{
			name:    "clai binary",
			status:  "error",
			message: "clai not found in PATH",
		}
	}

	return checkResult{
		name:    "clai binary",
		status:  "ok",
		message: path,
	}
}

func checkDirectories() []checkResult {
	var results []checkResult
	paths := config.DefaultPaths()

	// Check directories (no side effects - just report status)
	dirs := []struct {
		name string
		path string
	}{
		{"Config directory", paths.ConfigDir},
		{"Data directory", paths.DataDir},
		{"Cache directory", paths.CacheDir},
		{"Runtime directory", paths.RuntimeDir},
	}

	for _, d := range dirs {
		if _, err := os.Stat(d.path); os.IsNotExist(err) {
			results = append(results, checkResult{
				name:    d.name,
				status:  "warn",
				message: fmt.Sprintf("Missing: %s (will be created when needed)", d.path),
			})
		} else if err != nil {
			results = append(results, checkResult{
				name:    d.name,
				status:  "error",
				message: fmt.Sprintf("Error accessing: %s", d.path),
			})
		} else {
			results = append(results, checkResult{
				name:    d.name,
				status:  "ok",
				message: d.path,
			})
		}
	}

	return results
}

func checkConfiguration() checkResult {
	paths := config.DefaultPaths()
	configFile := paths.ConfigFile()

	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		return checkResult{
			name:    "Configuration",
			status:  "error",
			message: fmt.Sprintf("Failed to load: %v", err),
		}
	}

	if err := cfg.Validate(); err != nil {
		return checkResult{
			name:    "Configuration",
			status:  "error",
			message: fmt.Sprintf("Invalid: %v", err),
		}
	}

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return checkResult{
			name:    "Configuration",
			status:  "ok",
			message: "Using defaults (no config file)",
		}
	}

	return checkResult{
		name:    "Configuration",
		status:  "ok",
		message: configFile,
	}
}

func checkShellIntegrationDoctor() checkResult {
	shells := checkShellIntegration()

	if len(shells) == 0 {
		return checkResult{
			name:    "Shell integration",
			status:  "warn",
			message: "Not installed. Run 'clai install' to set up.",
		}
	}

	return checkResult{
		name:    "Shell integration",
		status:  "ok",
		message: strings.Join(shells, ", "),
	}
}

func checkDaemon() checkResult {
	if daemon.IsRunning() {
		return checkResult{
			name:    "Daemon",
			status:  "ok",
			message: "Running",
		}
	}

	return checkResult{
		name:    "Daemon",
		status:  "warn",
		message: "Not running. Will start automatically when needed.",
	}
}

func checkAIProviders() []checkResult {
	var results []checkResult

	// Check Claude CLI (only supported AI provider)
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		results = append(results, checkResult{
			name:    "Claude CLI",
			status:  "error",
			message: "Not found. Install from https://claude.ai/cli",
		})
	} else {
		results = append(results, checkResult{
			name:    "Claude CLI",
			status:  "ok",
			message: claudePath,
		})
	}

	return results
}
