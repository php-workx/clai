package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove shell integration",
	Long: `Remove clai shell integration from your shell configuration file.

This command removes the source line from your shell's rc file (.zshrc, .bashrc, etc.)
and optionally removes the hook files.

Examples:
  clai uninstall`,
	RunE: runUninstall,
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	paths := config.DefaultPaths()

	// RC files to check
	rcFiles := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}

	removed := false
	for _, rcFile := range rcFiles {
		wasRemoved, err := removeFromRCFile(rcFile, paths.HooksDir())
		if err != nil {
			fmt.Printf("Warning: failed to process %s: %v\n", rcFile, err)
			continue
		}
		if wasRemoved {
			fmt.Printf("Removed clai from: %s\n", rcFile)
			removed = true
		}
	}

	// Remove hook files
	hookFiles := []string{
		filepath.Join(paths.HooksDir(), "clai.zsh"),
		filepath.Join(paths.HooksDir(), "clai.bash"),
		filepath.Join(paths.HooksDir(), "clai.fish"),
	}

	for _, hookFile := range hookFiles {
		if _, err := os.Stat(hookFile); err == nil {
			if err := os.Remove(hookFile); err != nil {
				fmt.Printf("Warning: failed to remove %s: %v\n", hookFile, err)
			} else {
				fmt.Printf("Removed hook file: %s\n", hookFile)
			}
		}
	}

	if !removed {
		fmt.Println("No clai installation found in shell configuration files.")
		return nil
	}

	fmt.Printf("\n%sUninstalled successfully!%s\n", colorGreen, colorReset)
	fmt.Println("Please restart your terminal or source your rc file.")

	return nil
}

func removeFromRCFile(rcFile, hooksDir string) (bool, error) {
	// Read the file
	content, err := os.ReadFile(rcFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// Patterns to remove
	patterns := []string{
		"source \"" + hooksDir,
		"source '" + hooksDir,
		". " + hooksDir,
		"eval \"$(clai init",
		"clai init zsh",
		"clai init bash",
		"clai init fish",
		"# clai shell integration",
	}

	// Process line by line
	var newLines []string
	removed := false
	scanner := bufio.NewScanner(strings.NewReader(string(content)))

	for scanner.Scan() {
		line := scanner.Text()
		shouldRemove := false

		for _, pattern := range patterns {
			if strings.Contains(line, pattern) {
				shouldRemove = true
				removed = true
				break
			}
		}

		if !shouldRemove {
			newLines = append(newLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	if !removed {
		return false, nil
	}

	// Remove consecutive empty lines at the end
	for len(newLines) > 0 && strings.TrimSpace(newLines[len(newLines)-1]) == "" {
		newLines = newLines[:len(newLines)-1]
	}

	// Write back
	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}

	if err := os.WriteFile(rcFile, []byte(newContent), 0644); err != nil {
		return false, err
	}

	return true, nil
}
