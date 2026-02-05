package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/ipc"
	"github.com/runger/clai/internal/sanitize"
)

var (
	suggestLimit  int
	suggestJSON   bool
	suggestFormat string
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
	suggestCmd.Flags().BoolVar(&suggestJSON, "json", false, "output suggestions as JSON (deprecated: use --format=json)")
	suggestCmd.Flags().StringVar(&suggestFormat, "format", "text", "output format: text, json, or fzf")
}

func runSuggest(cmd *cobra.Command, args []string) error {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	// Determine output format (--json flag for backwards compat)
	format := suggestFormat
	if suggestJSON && format == "text" {
		format = "json"
	}

	if integrationDisabled() {
		if format == "json" {
			return writeSuggestJSON(nil)
		}
		return nil
	}

	// Empty prefix - return cached AI suggestion
	if prefix == "" {
		suggestion, _ := cache.ReadSuggestion()
		if format == "json" {
			if suggestion == "" {
				return writeSuggestJSON(nil)
			}
			return writeSuggestJSON([]suggestOutput{{
				Text:        suggestion,
				Source:      "ai",
				Score:       0,
				Description: "",
				Risk:        riskFromText(suggestion),
			}})
		}
		if suggestion != "" {
			fmt.Println(suggestion)
		}
		return nil
	}

	// Try daemon first for session-aware suggestions
	suggestions := getSuggestionsFromDaemon(prefix, suggestLimit)

	// Fall back to shell history if daemon returned nothing
	if len(suggestions) == 0 {
		suggestions = getSuggestionsFromHistory(prefix, suggestLimit)
	}

	// Output based on format
	return outputSuggestions(suggestions, format)
}

// outputSuggestions formats and outputs suggestions based on format type.
func outputSuggestions(suggestions []suggestOutput, format string) error {
	switch format {
	case "json":
		return writeSuggestJSON(suggestions)
	case "fzf":
		// fzf format: plain commands, one per line (for piping to fzf)
		for _, s := range suggestions {
			fmt.Println(s.Text)
		}
	case "text":
		// text format: numbered list with metadata
		for i, s := range suggestions {
			reasons := s.Source
			if s.Risk != "" {
				reasons += ", " + s.Risk
			}
			fmt.Printf("%d. %s (%s)\n", i+1, s.Text, reasons)
		}
	default:
		// Unknown format, treat as fzf (plain output)
		for _, s := range suggestions {
			fmt.Println(s.Text)
		}
	}
	return nil
}

type suggestOutput struct {
	Text        string  `json:"text"`
	Source      string  `json:"source"`
	Score       float64 `json:"score"`
	Description string  `json:"description"`
	Risk        string  `json:"risk"`
}

func riskFromText(text string) string {
	if sanitize.IsDestructive(text) {
		return "destructive"
	}
	return ""
}

func writeSuggestJSON(suggestions []suggestOutput) error {
	if suggestions == nil {
		suggestions = []suggestOutput{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(suggestions)
}

func getSuggestionsFromHistory(prefix string, limit int) []suggestOutput {
	results := history.Suggestions(prefix, limit)
	if len(results) == 0 {
		return nil
	}
	suggestions := make([]suggestOutput, 0, len(results))
	for _, s := range results {
		suggestions = append(suggestions, suggestOutput{
			Text:        s,
			Source:      "global",
			Score:       0,
			Description: "",
			Risk:        riskFromText(s),
		})
	}
	return suggestions
}

// getSuggestionsFromDaemon tries to get suggestions from the running daemon.
// Returns nil if daemon is unavailable or returns no results.
func getSuggestionsFromDaemon(prefix string, limit int) []suggestOutput {
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

	// Get suggestions from daemon (short timeout for shell integration responsiveness)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	daemonSuggestions := client.Suggest(ctx, sessionID, cwd, prefix, len(prefix), false, limit)
	if len(daemonSuggestions) == 0 {
		return nil
	}

	// Convert to string slice
	results := make([]suggestOutput, len(daemonSuggestions))
	for i, s := range daemonSuggestions {
		results[i] = suggestOutput{
			Text:        s.Text,
			Source:      s.Source,
			Score:       float64(s.Score),
			Description: s.Description,
			Risk:        s.Risk,
		}
	}

	return results
}

func integrationDisabled() bool {
	if os.Getenv("CLAI_OFF") == "1" {
		return true
	}
	if cache.SessionOff() {
		return true
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	return !cfg.Suggestions.Enabled
}
