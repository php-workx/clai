package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/extract"
)

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract suggested commands from stdin",
	Long: `Extract suggested commands from command output.

This command reads from stdin, passes it through to stdout,
saves it to cache for error analysis, and extracts any
suggested commands it finds.

Examples:
  npm install 2>&1 | clai extract
  cat error.log | clai extract`,
	RunE: runExtract,
}

func runExtract(cmd *cobra.Command, args []string) error {
	// Read all input
	reader := bufio.NewReader(os.Stdin)
	var output strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				output.WriteString(line)
				break
			}
			return err
		}
		output.WriteString(line)
	}

	content := output.String()

	// Pass through the output so the user sees it
	fmt.Print(content)

	// Save for potential error analysis later
	if err := cache.WriteLastOutput(content); err != nil {
		// Non-fatal, continue
		fmt.Fprintf(os.Stderr, "Warning: could not save output to cache: %v\n", err)
	}

	// Extract suggested command using the extract package
	suggestion := extract.Suggestion(content)

	// Write suggestion (or clear if none found)
	if err := cache.WriteSuggestion(suggestion); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save suggestion to cache: %v\n", err)
	}

	return nil
}
