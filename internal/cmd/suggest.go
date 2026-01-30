package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/history"
)

var (
	suggestLimit int
)

var suggestCmd = &cobra.Command{
	Use:   "suggest [prefix]",
	Short: "Get command suggestion from history or AI cache",
	Long: `Get a command suggestion based on the current input prefix.

When prefix is provided, searches shell history for matching commands.
When prefix is empty, returns any cached AI suggestion.

This command is designed for shell integration (fast, minimal output).

Examples:
  clai suggest "git st"       # Returns "git status" if in history
  clai suggest ""             # Returns cached AI suggestion if any
  clai suggest --limit 5 git  # Returns up to 5 suggestions`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSuggest,
}

func init() {
	suggestCmd.Flags().IntVarP(&suggestLimit, "limit", "n", 1, "maximum number of suggestions to return")
	rootCmd.AddCommand(suggestCmd)
}

func runSuggest(cmd *cobra.Command, args []string) error {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	// Handle multiple suggestions
	if suggestLimit > 1 {
		suggestions := history.Suggestions(prefix, suggestLimit)
		for _, s := range suggestions {
			fmt.Println(s)
		}
		return nil
	}

	// Single suggestion (original behavior)
	var suggestion string

	if prefix == "" {
		// No prefix - return AI suggestion if available
		// Ignore error - silent fail means no suggestion
		suggestion, _ = cache.ReadSuggestion()
	} else {
		// Have prefix - search history
		suggestion = history.Suggestion(prefix)
	}

	if suggestion != "" {
		fmt.Println(suggestion)
	}

	return nil
}
