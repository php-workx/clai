package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/extract"
)

// ANSI colors
const (
	colorRed    = "\033[0;31m"
	colorCyan   = "\033[0;36m"
	colorYellow = "\033[38;5;214m"
	colorDim    = "\033[2m"
	colorReset  = "\033[0m"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose <command> [exit_code]",
	Short: "Diagnose a failed command using Claude",
	Long: `Diagnose a failed command by analyzing its error output with Claude.

Examples:
  clai diagnose "npm run build" 1
  clai diagnose "python script.py" 127`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runDiagnose,
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	command := args[0]
	exitCode := "1"
	if len(args) > 1 {
		exitCode = args[1]
	}

	// Get error output from cache
	errorOutput, err := cache.ReadLastOutput(50)
	if err != nil {
		errorOutput = "(no output captured)"
	}

	// Get system info
	pwd, _ := os.Getwd()
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell == "" {
		shell = "unknown"
	}
	osName := runtime.GOOS

	// Build prompt
	prompt := fmt.Sprintf(`I ran this command in my terminal and it failed:

Command: %s
Exit code: %s
Working directory: %s
Shell: %s
OS: %s

Error output:
%s%s%s

Please:
1. Briefly explain what went wrong (1-2 sentences max)
2. Provide the corrected command to fix it

Be concise. Format as:
**Problem**: <explanation>
**Fix**: %s<corrected command>%s`, command, exitCode, pwd, shell, osName, "```\n", errorOutput, "\n```", "`", "`")

	// Print header
	fmt.Printf("%s━━━ AI Diagnosis ━━━%s\n", colorCyan, colorReset)

	// Set up context with Ctrl+C handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Query Claude with interruptible context
	response, err := claude.QueryWithContext(ctx, prompt)
	if err != nil {
		if err.Error() == "interrupted" {
			fmt.Printf("\n%sAnalysis cancelled%s\n", colorDim, colorReset)
			return nil // User cancelled, not an error
		}
		fmt.Printf("%sError: %s%s\n", colorRed, err.Error(), colorReset)
		return err
	}

	fmt.Println(response)
	fmt.Printf("%s━━━━━━━━━━━━━━━━━━━━%s\n", colorCyan, colorReset)

	// Extract any command suggestion from the response for Tab completion
	if suggestion := extract.Suggestion(response); suggestion != "" {
		if err := cache.WriteSuggestion(suggestion); err != nil {
			// Non-fatal, just log
			fmt.Fprintf(os.Stderr, "Warning: could not cache suggestion: %v\n", err)
		}
	}

	return nil
}
