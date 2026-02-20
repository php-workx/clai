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
	Use:     "install",
	Short:   "Install shell integration",
	GroupID: groupSetup,
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
	installCmd.Flags().StringVar(&installShell, "shell", "", "Shell to install for (zsh, bash, fish)")
}

func runInstall(cmd *cobra.Command, args []string) error {
	shell, err := resolveInstallShell()
	if err != nil {
		return err
	}

	paths := config.DefaultPaths()
	if err = paths.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	hookFile := filepath.Join(paths.HooksDir(), fmt.Sprintf("clai.%s", shell))
	hookContent, err := getHookContent(shell)
	if err != nil {
		return fmt.Errorf("failed to get hook content: %w", err)
	}

	if err := os.WriteFile(hookFile, []byte(hookContent), 0o644); err != nil { //nolint:gosec // G306: hook file must be readable by shell
		return fmt.Errorf("failed to write hook file: %w", err)
	}
	fmt.Printf("Wrote hook file: %s\n", hookFile)

	rcFiles := getRCFiles(shell)
	if len(rcFiles) == 0 {
		return fmt.Errorf("could not determine rc file for %s", shell)
	}

	sourceLine := fmt.Sprintf("source %q", hookFile)
	allInstalled := true
	addedFiles := make([]string, 0, len(rcFiles))
	for _, rcFile := range rcFiles {
		installed, installedLine, err := isInstalled(rcFile, hookFile, shell)
		if err != nil {
			return fmt.Errorf("failed to check rc file: %w", err)
		}
		if installed {
			fmt.Printf("clai is already installed in %s\n", rcFile)
			fmt.Printf("  Line: %s\n", installedLine)
			continue
		}
		allInstalled = false

		if err := appendToRCFile(rcFile, sourceLine); err != nil {
			return err
		}
		addedFiles = append(addedFiles, rcFile)
	}
	if allInstalled {
		return nil
	}

	fmt.Printf("%sInstalled successfully!%s\n", colorGreen, colorReset)
	for _, f := range addedFiles {
		fmt.Printf("  Added to: %s\n", f)
	}
	fmt.Printf("\nTo activate, either:\n")
	fmt.Printf("  1. Start a new terminal session, or\n")
	fmt.Printf("  2. Run: %s%s%s\n", colorCyan, evalCommand(shell), colorReset)

	return nil
}

func resolveInstallShell() (string, error) {
	shell := installShell
	if shell == "" {
		detection := DetectShell()
		shell = detection.Shell
		if shell != "" && !detection.Confident {
			var err error
			shell, err = confirmShellChoice(shell)
			if err != nil {
				return "", err
			}
		}
	}

	if shell == "" {
		return "", fmt.Errorf("could not detect shell, please specify with --shell=zsh or --shell=bash")
	}

	switch shell {
	case "zsh", "bash", "fish":
		return shell, nil
	default:
		return "", fmt.Errorf("unsupported shell: %s (supported: zsh, bash, fish)", shell)
	}
}

func confirmShellChoice(detected string) (string, error) {
	fmt.Printf("Detected shell: %s (from login shell)\n", detected)
	fmt.Printf("Is this the shell you want to install for? [Y/n] ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "n" && response != "no" {
		return detected, nil
	}

	for {
		fmt.Println("\nSupported shells: zsh, bash, fish")
		fmt.Print("Which shell would you like to install for? ")
		shell, _ := reader.ReadString('\n')
		shell = strings.TrimSpace(strings.ToLower(shell))

		switch shell {
		case "zsh", "bash", "fish":
			return shell, nil
		default:
			fmt.Printf("Invalid shell: %q\n", shell)
		}
	}
}

func evalCommand(shell string) string {
	if shell == "fish" {
		return "clai init fish | source"
	}
	return fmt.Sprintf(`eval "$(clai init %s)"`, shell)
}

func appendToRCFile(rcFile, sourceLine string) error {
	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // G304: rc file path from shell config
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", rcFile, err)
	}
	defer f.Close()

	if err := ensureTrailingNewline(f, rcFile); err != nil {
		return err
	}

	installLine := fmt.Sprintf("\n# clai shell integration\n%s\n", sourceLine)
	if _, err := f.WriteString(installLine); err != nil {
		return fmt.Errorf("failed to write to %s: %w", rcFile, err)
	}

	return nil
}

func ensureTrailingNewline(f *os.File, rcFile string) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", rcFile, err)
	}
	if info.Size() == 0 {
		return nil
	}

	content, err := os.ReadFile(rcFile) //nolint:gosec // G304: rc file path from shell config
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", rcFile, err)
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to %s: %w", rcFile, err)
		}
	}
	return nil
}

func getRCFiles(shell string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	switch shell {
	case "zsh":
		return []string{filepath.Join(home, ".zshrc")}
	case "bash":
		bashrc := filepath.Join(home, ".bashrc")
		bashProfile := filepath.Join(home, ".bash_profile")
		if runtime.GOOS == "darwin" {
			// macOS Terminal.app opens login shells which read
			// .bash_profile, but typing `bash` from zsh starts a
			// non-login interactive shell which only reads .bashrc.
			// Install to both so clai loads in either case.
			return []string{bashProfile, bashrc}
		}
		// Linux: .bashrc is sourced by interactive non-login shells
		return []string{bashrc}
	case "fish":
		// Fish config is in ~/.config/fish/config.fish
		configDir := filepath.Join(home, ".config", "fish")
		if err := os.MkdirAll(configDir, 0o755); err != nil { //nolint:gosec // G301: user config directory needs standard permissions
			return nil
		}
		return []string{filepath.Join(configDir, "config.fish")}
	default:
		return nil
	}
}

func getHookContent(shell string) (string, error) {
	// Write a thin loader that delegates to `clai init <shell>`.
	// This ensures template replacement (e.g. {{CLAI_SESSION_ID}})
	// happens at shell startup time so each session gets a unique ID.
	switch shell {
	case "fish":
		return "# clai shell integration (loader)\nclai init fish | source\n", nil
	case "zsh", "bash":
		return fmt.Sprintf("# clai shell integration (loader)\neval \"$(clai init %s)\"\n", shell), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

func isInstalled(rcFile, hookFile, shell string) (installed bool, reason string, err error) {
	f, err := os.Open(rcFile) //nolint:gosec // G304: rc file path from shell config
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	defer f.Close()

	// Patterns to look for (shell syntax, not Go strings)
	patterns := []string{
		fmt.Sprintf("source %q", hookFile),
		fmt.Sprintf(`source '%s'`, hookFile),
		fmt.Sprintf(". %s", hookFile),
		"clai init " + shell,
	}

	// Add shell-specific patterns
	if shell == "fish" {
		patterns = append(patterns, "clai init fish | source")
	} else {
		patterns = append(patterns, fmt.Sprintf(`eval "$(clai init %s)"`, shell))
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
