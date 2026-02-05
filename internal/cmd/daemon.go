package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/ipc"
)

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Manage the clai background daemon",
	Hidden: true,
	Long: `Manage the clai background daemon (claid).

The daemon handles shell integration, command history, and suggestions.
It starts automatically when needed but can be managed manually.

Subcommands:
  start    Start the daemon
  stop     Stop the daemon
  restart  Restart the daemon
  status   Show daemon status`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the clai daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemon.IsRunning() {
			fmt.Printf("Daemon: %salready running%s\n", colorCyan, colorReset)
			return nil
		}

		fmt.Print("Starting daemon...")
		err := ipc.SpawnAndWait(5 * time.Second)
		if err != nil {
			fmt.Printf(" %sfailed%s\n", colorRed, colorReset)
			return err
		}
		fmt.Printf(" %srunning%s\n", colorGreen, colorReset)
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the clai daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !daemon.IsRunning() {
			fmt.Printf("Daemon: %snot running%s\n", colorDim, colorReset)
			return nil
		}

		fmt.Print("Stopping daemon...")
		err := daemon.Stop()
		if err != nil {
			fmt.Printf(" %sfailed%s\n", colorRed, colorReset)
			return err
		}
		fmt.Printf(" %sstopped%s\n", colorGreen, colorReset)
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the clai daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running
		if daemon.IsRunning() {
			fmt.Print("Stopping daemon...")
			if err := daemon.Stop(); err != nil {
				fmt.Printf(" %sfailed%s\n", colorRed, colorReset)
				return err
			}
			fmt.Printf(" %sstopped%s\n", colorGreen, colorReset)
		}

		// Start
		fmt.Print("Starting daemon...")
		err := ipc.SpawnAndWait(5 * time.Second)
		if err != nil {
			fmt.Printf(" %sfailed%s\n", colorRed, colorReset)
			return err
		}
		fmt.Printf(" %srunning%s\n", colorGreen, colorReset)
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Run: func(cmd *cobra.Command, args []string) {
		paths := config.DefaultPaths()

		if daemon.IsRunning() {
			fmt.Printf("Daemon: %srunning%s\n", colorGreen, colorReset)
			if pid, err := daemon.ReadPID(paths.PIDFile()); err == nil {
				fmt.Printf("  PID:    %d\n", pid)
			}
			fmt.Printf("  Socket: %s\n", paths.SocketFile())
		} else {
			fmt.Printf("Daemon: %snot running%s\n", colorDim, colorReset)
		}
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
}
