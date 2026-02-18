package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/config"
)

var (
	onSessionOnly  bool
	offSessionOnly bool
)

var onCmd = &cobra.Command{
	Use:     "on",
	Short:   "Enable clai shell integration",
	GroupID: groupCore,
	RunE: func(cmd *cobra.Command, args []string) error {
		return toggleIntegration(true, onSessionOnly)
	},
}

var offCmd = &cobra.Command{
	Use:     "off",
	Short:   "Disable clai shell integration",
	GroupID: groupCore,
	RunE: func(cmd *cobra.Command, args []string) error {
		return toggleIntegration(false, offSessionOnly)
	},
}

func init() {
	onCmd.Flags().BoolVar(&onSessionOnly, "session", false, "Enable for this session only")
	offCmd.Flags().BoolVar(&offSessionOnly, "session", false, "Disable for this session only")
}

func toggleIntegration(enable bool, sessionOnly bool) error {
	if sessionOnly {
		if err := cache.SetSessionOff(!enable); err != nil {
			return err
		}
		if enable {
			fmt.Println("Session integrations enabled")
		} else {
			fmt.Println("Session integrations disabled")
		}
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.Suggestions.Enabled = enable
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	// Also clear/set session flag so all disable sources are consistent.
	if err := cache.SetSessionOff(!enable); err != nil {
		return fmt.Errorf("failed to update session flag: %w", err)
	}
	if enable {
		fmt.Println("Integrations enabled")
	} else {
		fmt.Println("Integrations disabled")
	}
	return nil
}
