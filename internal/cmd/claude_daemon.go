package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/claude"
)

var claudeDaemonCmd = &cobra.Command{
	Use:    "claude-daemon",
	Short:  "Manage the background Claude daemon for faster responses",
	Hidden: true,
	Long: `Manage the background Claude daemon process.

The daemon keeps a Claude CLI process running in the background to eliminate
startup overhead for subsequent queries (especially useful for voice commands).`,
}

var claudeDaemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if claude.IsDaemonRunning() {
			fmt.Printf("Daemon: %salready running%s\n", colorCyan, colorReset)
			return nil
		}
		fmt.Print("Starting Claude daemon...")
		err := claude.StartDaemonProcess()
		if err != nil {
			fmt.Printf(" %sfailed%s\n", colorRed, colorReset)
			return err
		}
		fmt.Printf(" %sready%s\n", colorCyan, colorReset)
		fmt.Println("Daemon will auto-stop after 2 minutes of inactivity.")
		return nil
	},
}

var claudeDaemonRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Run the daemon (internal use)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return claude.RunDaemon(cmd.Context())
	},
}

var claudeDaemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background daemon",
	Run: func(cmd *cobra.Command, args []string) {
		claude.StopDaemon()
		fmt.Println("Daemon stopped.")
	},
}

var claudeDaemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	Run: func(cmd *cobra.Command, args []string) {
		if claude.IsDaemonRunning() {
			fmt.Printf("Daemon: %srunning%s\n", colorCyan, colorReset)
		} else {
			fmt.Printf("Daemon: %snot running%s\n", colorDim, colorReset)
		}
	},
}

func init() {
	claudeDaemonCmd.AddCommand(claudeDaemonStartCmd)
	claudeDaemonCmd.AddCommand(claudeDaemonStopCmd)
	claudeDaemonCmd.AddCommand(claudeDaemonStatusCmd)
	claudeDaemonCmd.AddCommand(claudeDaemonRunCmd)
}
