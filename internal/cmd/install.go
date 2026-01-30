package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

var (
	installShell string
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install shell integration",
	Long: `Install clai shell integration into your shell configuration file.

This command adds a source line to your shell's rc file (.zshrc, .bashrc, etc.)
that loads the clai shell hooks on startup.

By default, the command detects your current shell. Use --shell to specify
a different shell.

Examples:
  clai install              # Auto-detect shell
  clai install --shell=zsh  # Install for zsh
  clai install --shell=bash # Install for bash`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installShell, "shell", "", "Shell to install for (zsh, bash)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Detect shell if not specified
	shell := installShell
	if shell == "" {
		shell = detectShell()
	}

	if shell == "" {
		return fmt.Errorf("could not detect shell, please specify with --shell=zsh or --shell=bash")
	}

	// Validate shell
	switch shell {
	case "zsh", "bash":
		// OK
	default:
		return fmt.Errorf("unsupported shell: %s (supported: zsh, bash)", shell)
	}

	paths := config.DefaultPaths()

	// Ensure directories exist
	if err := paths.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Write hook file
	hookFile := filepath.Join(paths.HooksDir(), fmt.Sprintf("clai.%s", shell))
	hookContent, err := getHookContent(shell)
	if err != nil {
		return fmt.Errorf("failed to get hook content: %w", err)
	}

	if err := os.WriteFile(hookFile, []byte(hookContent), 0644); err != nil {
		return fmt.Errorf("failed to write hook file: %w", err)
	}
	fmt.Printf("Wrote hook file: %s\n", hookFile)

	// Get rc file path
	rcFile := getRCFile(shell)
	if rcFile == "" {
		return fmt.Errorf("could not determine rc file for %s", shell)
	}

	// Check if already installed
	sourceLine := fmt.Sprintf(`source "%s"`, hookFile)
	evalLine := fmt.Sprintf(`eval "$(clai init %s)"`, shell)

	installed, installedLine, err := isInstalled(rcFile, hookFile, shell)
	if err != nil {
		return fmt.Errorf("failed to check rc file: %w", err)
	}

	if installed {
		fmt.Printf("clai is already installed in %s\n", rcFile)
		fmt.Printf("  Line: %s\n", installedLine)
		return nil
	}

	// Append source line to rc file
	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", rcFile, err)
	}
	defer f.Close()

	// Add newline before if file doesn't end with one
	info, _ := f.Stat()
	if info.Size() > 0 {
		// Read last byte to check for newline
		content, _ := os.ReadFile(rcFile)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("failed to write to %s: %w", rcFile, err)
			}
		}
	}

	// Write the source line with a comment
	installLine := fmt.Sprintf("\n# clai shell integration\n%s\n", sourceLine)
	if _, err := f.WriteString(installLine); err != nil {
		return fmt.Errorf("failed to write to %s: %w", rcFile, err)
	}

	fmt.Printf("%sInstalled successfully!%s\n", colorGreen, colorReset)
	fmt.Printf("  Added to: %s\n", rcFile)
	fmt.Printf("\nTo activate, either:\n")
	fmt.Printf("  1. Start a new terminal session, or\n")
	fmt.Printf("  2. Run: %s%s%s\n", colorCyan, evalLine, colorReset)

	return nil
}

func detectShell() string {
	// Check SHELL environment variable
	shell := os.Getenv("SHELL")
	if shell != "" {
		base := filepath.Base(shell)
		switch base {
		case "zsh", "bash":
			return base
		}
	}

	// On Windows, default to bash (Git Bash is common)
	if runtime.GOOS == "windows" {
		return "bash"
	}

	return ""
}

func getRCFile(shell string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch shell {
	case "zsh":
		// Check for .zshrc first, then .zprofile
		zshrc := filepath.Join(home, ".zshrc")
		if _, err := os.Stat(zshrc); err == nil {
			return zshrc
		}
		// Create .zshrc if it doesn't exist
		return zshrc
	case "bash":
		// Check for .bashrc first
		bashrc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(bashrc); err == nil {
			return bashrc
		}
		// On macOS, check .bash_profile
		if runtime.GOOS == "darwin" {
			bashProfile := filepath.Join(home, ".bash_profile")
			if _, err := os.Stat(bashProfile); err == nil {
				return bashProfile
			}
		}
		// Default to .bashrc
		return bashrc
	default:
		return ""
	}
}

func getHookContent(shell string) (string, error) {
	// Use the embedded shell scripts
	filename := fmt.Sprintf("shell/%s/clai.%s", shell, shell)
	content, err := shellScripts.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("shell script not found: %s", filename)
	}
	return string(content), nil
}

func isInstalled(rcFile, hookFile, shell string) (bool, string, error) {
	f, err := os.Open(rcFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	defer f.Close()

	// Patterns to look for
	patterns := []string{
		fmt.Sprintf(`source "%s"`, hookFile),
		fmt.Sprintf(`source '%s'`, hookFile),
		fmt.Sprintf(". %s", hookFile),
		fmt.Sprintf(`eval "$(clai init %s)"`, shell),
		"clai init " + shell,
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, pattern := range patterns {
			if strings.Contains(line, pattern) {
				return true, line, nil
			}
		}
	}

	return false, "", scanner.Err()
}
