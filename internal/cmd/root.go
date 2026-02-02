package cmd

import (
	"github.com/spf13/cobra"
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
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(cmdCmd)
	rootCmd.AddCommand(daemonCmd)
}
