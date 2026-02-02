package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/ipc"
)

var (
	suggestLimit int
)

var suggestCmd = &cobra.Command{
	Use:     "suggest [prefix]",
	Short:   "Get command suggestion from session history or shell history",
	GroupID: groupCore,
	Long: `Get a command suggestion based on the current input prefix.

When the daemon is running, returns session-aware suggestions.
Falls back to shell history file if daemon is unavailable.
When prefix is empty, returns any cached AI suggestion.

This command is designed for shell integration (fast, minimal output).

Examples:
  clai suggest "git st"       # Returns "git status" from session/history
  clai suggest ""             # Returns cached AI suggestion if any
  clai suggest --limit 5 git  # Returns up to 5 suggestions`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSuggest,
}

func init() {
	suggestCmd.Flags().IntVarP(&suggestLimit, "limit", "n", 1, "maximum number of suggestions to return")
}

func runSuggest(cmd *cobra.Command, args []string) error {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	// Empty prefix - return cached AI suggestion
	if prefix == "" {
		suggestion, _ := cache.ReadSuggestion()
		if suggestion != "" {
			fmt.Println(suggestion)
		}
		return nil
	}

	// Try daemon first for session-aware suggestions
	suggestions := getSuggestionsFromDaemon(prefix, suggestLimit)

	// Fall back to shell history if daemon returned nothing
	if len(suggestions) == 0 {
		suggestions = history.Suggestions(prefix, suggestLimit)
	}

	// Output suggestions
	for _, s := range suggestions {
		fmt.Println(s)
	}

	return nil
}

// getSuggestionsFromDaemon tries to get suggestions from the running daemon.
// Returns nil if daemon is unavailable or returns no results.
func getSuggestionsFromDaemon(prefix string, limit int) []string {
	// Need session ID from environment
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		return nil
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	// Try to connect to daemon
	client, err := ipc.NewClient()
	if err != nil {
		return nil // Daemon not available
	}
	defer client.Close()

	// Get suggestions from daemon
	ctx := context.Background()
	daemonSuggestions := client.Suggest(ctx, sessionID, cwd, prefix, len(prefix), false, limit)
	if len(daemonSuggestions) == 0 {
		return nil
	}

	// Convert to string slice
	results := make([]string, len(daemonSuggestions))
	for i, s := range daemonSuggestions {
		results[i] = s.Text
	}

	return results
}
