// Package cmd implements the CLI commands for clai.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/extract"
)

var askCmd = &cobra.Command{
	Use:     "ask <question>",
	Short:   "Ask Claude a question with terminal context",
	GroupID: groupCore,
	Long: `Ask Claude a question, automatically including terminal context
like working directory and shell information.

Examples:
  clai ask "How do I find large files?"
  clai ask "What's the difference between grep and ripgrep?"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAsk,
}

var recentCommands string

func init() {
	askCmd.Flags().StringVar(&recentCommands, "context", "", "Recent commands to include as context")
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	// Get system info
	pwd, _ := os.Getwd()
	shell := os.Getenv("SHELL")

	// Build context
	var contextBuilder strings.Builder
	_, _ = fmt.Fprintf(&contextBuilder, "Working directory: %s\n", pwd)
	_, _ = fmt.Fprintf(&contextBuilder, "Shell: %s\n", shell)

	if recentCommands != "" {
		_, _ = fmt.Fprintf(&contextBuilder, "Recent commands:\n%s\n", recentCommands)
	}

	_, _ = fmt.Fprintf(&contextBuilder, "\nQuestion: %s", question)

	// Set up context with Ctrl+C handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Query Claude with interruptible context
	response, err := claude.QueryFast(ctx, contextBuilder.String())
	if err != nil {
		if err.Error() == "interrupted" {
			fmt.Printf("\n%sCancelled%s\n", colorDim, colorReset)
			return nil // User cancelled, not an error
		}
		fmt.Printf("%sError: %s%s\n", colorRed, err.Error(), colorReset)
		return err
	}

	fmt.Println(response)

	// Extract any command suggestion from the response for Tab completion
	if suggestion := extract.Suggestion(response); suggestion != "" {
		if err := cache.WriteSuggestion(suggestion); err != nil {
			// Non-fatal, just log
			fmt.Fprintf(os.Stderr, "Warning: could not cache suggestion: %v\n", err)
		}
	}

	return nil
}
