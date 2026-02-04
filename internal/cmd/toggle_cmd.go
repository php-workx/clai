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
	Short:   "Enable suggestions",
	GroupID: groupCore,
	RunE: func(cmd *cobra.Command, args []string) error {
		return toggleSuggestions(true, onSessionOnly)
	},
}

var offCmd = &cobra.Command{
	Use:     "off",
	Short:   "Disable suggestions",
	GroupID: groupCore,
	RunE: func(cmd *cobra.Command, args []string) error {
		return toggleSuggestions(false, offSessionOnly)
	},
}

func init() {
	onCmd.Flags().BoolVar(&onSessionOnly, "session", false, "Enable suggestions for this session only")
	offCmd.Flags().BoolVar(&offSessionOnly, "session", false, "Disable suggestions for this session only")
}

func toggleSuggestions(enable bool, sessionOnly bool) error {
	if sessionOnly {
		if err := cache.SetSessionOff(!enable); err != nil {
			return err
		}
		if enable {
			fmt.Println("Session suggestions enabled")
		} else {
			fmt.Println("Session suggestions disabled")
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
	if enable {
		fmt.Println("Suggestions enabled")
	} else {
		fmt.Println("Suggestions disabled")
	}
	return nil
}
