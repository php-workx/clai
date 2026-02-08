package cmd

import (
	"github.com/spf13/cobra"
)

// Command group IDs
const (
	groupCore  = "core"
	groupSetup = "setup"
)

var rootCmd = &cobra.Command{
	Use:   "clai",
	Short: "fish-like intelligence for any shell",
	Long: `clai - fish-like intelligence for any shell
  - ?describe task → get the right command
  - ↑↓ smart suggestions matching context`,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Define command groups
	rootCmd.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Core Commands:"},
		&cobra.Group{ID: groupSetup, Title: "Setup & Configuration:"},
	)

	// Core commands
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(cmdCmd)
	rootCmd.AddCommand(suggestCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(onCmd)
	rootCmd.AddCommand(offCmd)

	// Setup commands
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)

	// Hidden commands (still functional but not shown in help)
	rootCmd.AddCommand(daemonCmd)       // Go daemon (claid)
	rootCmd.AddCommand(claudeDaemonCmd) // Claude CLI daemon
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(doctorCmd)
}
