package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

var ptyCmd = &cobra.Command{
	Use:     "pty",
	Short:   "Manage PTY wrapper auto-start",
	GroupID: groupSetup,
	Long: `Configure whether interactive shell sessions auto-start in clai PTY mode.

When enabled, new interactive shells that source 'clai init <shell>' will
automatically exec clai-wrap (if available). This change applies to new
sessions; existing shells are unchanged.`,
}

var ptyOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Enable PTY auto-wrap for new sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		return setPTYEnabled(true)
	},
}

var ptyOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Disable PTY auto-wrap for new sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		return setPTYEnabled(false)
	},
}

var ptyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show PTY auto-wrap status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		state := "off"
		if cfg.PTY.Enabled {
			state = "on"
		}
		fmt.Printf("pty.enabled = %s\n", state)

		if os.Getenv("CLAI_WRAP") == "1" {
			fmt.Println("current session: running inside clai-wrap")
		} else {
			fmt.Println("current session: not running inside clai-wrap")
		}

		fmt.Println("note: pty on/off applies to new shell sessions")
		return nil
	},
}

func init() {
	ptyCmd.AddCommand(ptyOnCmd)
	ptyCmd.AddCommand(ptyOffCmd)
	ptyCmd.AddCommand(ptyStatusCmd)
}

func setPTYEnabled(enabled bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg.PTY.Enabled = enabled
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if enabled {
		fmt.Println("PTY auto-wrap enabled for new shell sessions")
	} else {
		fmt.Println("PTY auto-wrap disabled for new shell sessions")
	}
	return nil
}
