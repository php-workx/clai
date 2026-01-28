package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ai-terminal",
	Short: "AI-powered terminal assistant",
	Long: `AI Terminal integrates Claude into your shell to provide:
- Automatic error diagnosis for failed commands
- Command suggestion extraction from output
- Natural language questions with terminal context`,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(diagnoseCmd)
	rootCmd.AddCommand(extractCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(voiceCmd)
	rootCmd.AddCommand(daemonCmd)
}
