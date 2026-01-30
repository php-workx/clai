package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config [key] [value]",
	Short: "Get or set configuration values",
	Long: `Get or set clai configuration values.

Without arguments, lists all configuration keys.
With one argument, shows the value of that key.
With two arguments, sets the key to the value.

Configuration is stored in ~/.config/clai/config.yaml (XDG compliant).

Keys are in the format: section.key
Sections: daemon, client, ai, suggestions, privacy

Examples:
  clai config                        # List all keys
  clai config ai.enabled             # Get ai.enabled value
  clai config ai.enabled true        # Enable AI features
  clai config daemon.idle_timeout_mins 30`,
	Args: cobra.MaximumNArgs(2),
	RunE: runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	switch len(args) {
	case 0:
		// List all keys
		return listConfig(cfg, paths)
	case 1:
		// Get value
		return getConfig(cfg, args[0])
	case 2:
		// Set value
		return setConfig(cfg, paths, args[0], args[1])
	}

	return nil
}

func listConfig(cfg *config.Config, paths *config.Paths) error {
	fmt.Printf("%sConfiguration Keys%s\n", colorBold, colorReset)
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println()

	keys := config.ListKeys()
	var failedKeys []string
	for _, key := range keys {
		value, err := cfg.Get(key)
		if err != nil {
			failedKeys = append(failedKeys, key)
			continue
		}

		// Format empty values
		displayValue := value
		if displayValue == "" {
			displayValue = colorDim + "(not set)" + colorReset
		}

		fmt.Printf("  %s%s%s = %s\n", colorCyan, key, colorReset, displayValue)
	}

	if len(failedKeys) > 0 {
		fmt.Printf("\n%sWarning:%s Failed to retrieve keys: %s\n", colorYellow, colorReset, strings.Join(failedKeys, ", "))
	}

	fmt.Println()
	fmt.Printf("Config file: %s\n", paths.ConfigFile())

	return nil
}

func getConfig(cfg *config.Config, key string) error {
	value, err := cfg.Get(key)
	if err != nil {
		return err
	}

	if value == "" {
		fmt.Printf("%s(not set)%s\n", colorDim, colorReset)
	} else {
		fmt.Println(value)
	}

	return nil
}

func setConfig(cfg *config.Config, paths *config.Paths, key, value string) error {
	if err := cfg.Set(key, value); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Ensure directories exist before saving
	if err := paths.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	if err := cfg.SaveToFile(paths.ConfigFile()); err != nil {
		return err
	}

	fmt.Printf("%s%s%s = %s\n", colorCyan, key, colorReset, value)
	fmt.Printf("Saved to: %s\n", paths.ConfigFile())

	return nil
}
