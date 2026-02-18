package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/ipc"
)

const daemonFailedFmt = " %sfailed%s\n"

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
	RunE: func(cmd *cobra.Command, _ []string) error {
		paths := config.DefaultPaths()

		// If the process is alive but the socket is missing, treat it as unhealthy
		// and restart. This can happen if the socket path was unlinked while the
		// daemon is still running, leaving it unreachable.
		socketPresent, socketErr := socketExists(paths)
		if socketErr != nil {
			return socketErr
		}
		running := daemon.IsRunning()
		if running && socketPresent {
			fmt.Printf("Daemon: %salready running%s\n", colorCyan, colorReset)
			return nil
		}
		if running && !socketPresent {
			fmt.Printf("Daemon: %sunhealthy%s (socket missing), restarting...\n", colorYellow, colorReset)
			_ = daemon.Stop() // best-effort; Stop() now falls back to lock PID
		}

		fmt.Print("Starting daemon...")
		err := ipc.SpawnAndWaitContext(cmd.Context(), 5*time.Second)
		if err != nil {
			fmt.Printf(daemonFailedFmt, colorRed, colorReset)
			return err
		}
		fmt.Printf(" %srunning%s\n", colorGreen, colorReset)
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the clai daemon",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !daemon.IsRunning() {
			fmt.Printf("Daemon: %snot running%s\n", colorDim, colorReset)
			return nil
		}

		fmt.Print("Stopping daemon...")
		err := daemon.Stop()
		if err != nil {
			fmt.Printf(daemonFailedFmt, colorRed, colorReset)
			return err
		}
		fmt.Printf(" %sstopped%s\n", colorGreen, colorReset)
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the clai daemon",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Stop if running
		if daemon.IsRunning() {
			fmt.Print("Stopping daemon...")
			if err := daemon.Stop(); err != nil {
				fmt.Printf(daemonFailedFmt, colorRed, colorReset)
				return err
			}
			fmt.Printf(" %sstopped%s\n", colorGreen, colorReset)
		}

		// Start
		fmt.Print("Starting daemon...")
		err := ipc.SpawnAndWaitContext(cmd.Context(), 5*time.Second)
		if err != nil {
			fmt.Printf(daemonFailedFmt, colorRed, colorReset)
			return err
		}
		fmt.Printf(" %srunning%s\n", colorGreen, colorReset)
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Run: func(_ *cobra.Command, _ []string) {
		paths := config.DefaultPaths()

		if daemon.IsRunning() {
			fmt.Printf("Daemon: %srunning%s\n", colorGreen, colorReset)
			if pid, err := daemon.ReadPID(paths.PIDFile()); err == nil {
				fmt.Printf("  PID:    %d\n", pid)
			}
			fmt.Printf("  Socket: %s\n", paths.SocketFile())
			if exists, err := socketExists(paths); err != nil {
				fmt.Printf("  Socket: %scheck failed%s (%v)\n", colorYellow, colorReset, err)
			} else if !exists {
				fmt.Printf("  Socket: %smissing%s\n", colorYellow, colorReset)
			}
		} else {
			fmt.Printf("Daemon: %snot running%s\n", colorDim, colorReset)
		}
	},
}

func socketExists(paths *config.Paths) (bool, error) {
	_, err := os.Stat(paths.SocketFile())
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to stat daemon socket %q: %w", paths.SocketFile(), err)
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
}
