package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/runger/ai-terminal/internal/cache"
	"github.com/runger/ai-terminal/internal/claude"
	"github.com/spf13/cobra"
)

var voiceCmd = &cobra.Command{
	Use:   "voice <natural language input>",
	Short: "Convert natural language to a terminal command",
	Long: `Convert speech-to-text or natural language input into proper terminal commands.

This is useful when using voice input, as speech-to-text often produces
natural language like "list all files" instead of actual commands.

The converted command is also cached for Tab completion.

Examples:
  ai-terminal voice "list all files in the current directory"
  ai-terminal voice "show me the git status"
  ai-terminal voice "find all Python files"
  ai-terminal voice "install the requests package with pip"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runVoice,
}

func runVoice(cmd *cobra.Command, args []string) error {
	input := strings.Join(args, " ")

	// Get system info for context
	pwd, _ := os.Getwd()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}

	// Build prompt
	prompt := fmt.Sprintf(`Convert this natural language to a terminal command.
Output ONLY the command, nothing else. No explanation, no backticks, just the raw command.

Working directory: %s
Shell: %s
Input: "%s"`, pwd, shell, input)

	// Set up context with Ctrl+C handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Query Claude (use fast daemon if available)
	response, err := claude.QueryFast(ctx, prompt)
	if err != nil {
		if err.Error() == "interrupted" {
			fmt.Printf("\n%sCancelled%s\n", colorDim, colorReset)
			return nil
		}
		fmt.Printf("%sError: %s%s\n", colorRed, err.Error(), colorReset)
		return err
	}

	// Clean up the response (remove any stray backticks, newlines, etc.)
	command := cleanCommand(response)

	// Print the command
	fmt.Println(command)

	// Cache for Tab completion
	if command != "" {
		if err := cache.WriteSuggestion(command); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not cache suggestion: %v\n", err)
		}
	}

	return nil
}

// cleanCommand removes common artifacts from AI response
func cleanCommand(s string) string {
	s = strings.TrimSpace(s)

	// Remove triple backticks first (more specific)
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")

	// Remove single backticks
	s = strings.TrimPrefix(s, "`")
	s = strings.TrimSuffix(s, "`")

	s = strings.TrimSpace(s)

	// Remove common prefixes
	s = strings.TrimPrefix(s, "$ ")
	s = strings.TrimPrefix(s, "bash\n")
	s = strings.TrimPrefix(s, "sh\n")
	s = strings.TrimPrefix(s, "zsh\n")

	// Take only the first line if multiple
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[:idx]
	}

	return strings.TrimSpace(s)
}
