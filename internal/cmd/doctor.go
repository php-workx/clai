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
	Use:    "doctor",
	Short:  "Check clai installation and dependencies",
	Hidden: true,
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

type checkResult struct {
	name    string
	status  string // "ok", "warn", "error"
	message string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Printf("%sclai Doctor%s\n", colorBold, colorReset)
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println()

	// Collect all check results (separate appends for readability with mixed single/slice returns)
	results := make([]checkResult, 0, 16)
	results = append(results, checkBinary())
	results = append(results, checkDirectories()...)
	//nolint:gocritic // appendCombine: separate appends intentional for mixed single/slice returns
	results = append(results, checkConfiguration())
	results = append(results, checkShellIntegrationDoctor())
	results = append(results, checkDaemon())
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

	// Check base directory
	if _, err := os.Stat(paths.BaseDir); os.IsNotExist(err) {
		results = append(results, checkResult{
			name:    "Data directory",
			status:  "warn",
			message: fmt.Sprintf("Missing: %s (will be created when needed)", paths.BaseDir),
		})
	} else if err != nil {
		results = append(results, checkResult{
			name:    "Data directory",
			status:  "error",
			message: fmt.Sprintf("Error accessing: %s", paths.BaseDir),
		})
	} else {
		results = append(results, checkResult{
			name:    "Data directory",
			status:  "ok",
			message: paths.BaseDir,
		})
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
