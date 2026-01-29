package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/history"
)

var suggestCmd = &cobra.Command{
	Use:   "suggest [prefix]",
	Short: "Get command suggestion from history or AI cache",
	Long: `Get a command suggestion based on the current input prefix.

When prefix is provided, searches shell history for matching commands.
When prefix is empty, returns any cached AI suggestion.

This command is designed for shell integration (fast, minimal output).

Examples:
  clai suggest "git st"    # Returns "git status" if in history
  clai suggest ""          # Returns cached AI suggestion if any`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSuggest,
}

func init() {
	rootCmd.AddCommand(suggestCmd)
}

func runSuggest(cmd *cobra.Command, args []string) error {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	var suggestion string

	if prefix == "" {
		// No prefix - return AI suggestion if available
		var err error
		suggestion, err = cache.ReadSuggestion()
		if err != nil {
			return nil // Silent fail, no suggestion
		}
	} else {
		// Have prefix - search history
		suggestion = history.Suggestion(prefix)
	}

	if suggestion != "" {
		fmt.Println(suggestion)
	}

	return nil
}
