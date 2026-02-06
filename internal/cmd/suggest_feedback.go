package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/ipc"
)

var (
	sfAction    string
	sfSuggested string
	sfExecuted  string
	sfPrefix    string
	sfLatencyMs int64
)

var suggestFeedbackCmd = &cobra.Command{
	Use:    "suggest-feedback",
	Short:  "Record feedback on a suggestion",
	Hidden: true,
	RunE:   runSuggestFeedback,
}

func init() {
	suggestFeedbackCmd.Flags().StringVar(&sfAction, "action", "", "feedback action (accepted, dismissed, edited, never, unblock, ignored, timeout)")
	suggestFeedbackCmd.Flags().StringVar(&sfSuggested, "suggested", "", "the suggested text")
	suggestFeedbackCmd.Flags().StringVar(&sfExecuted, "executed", "", "the executed text (optional)")
	suggestFeedbackCmd.Flags().StringVar(&sfPrefix, "prefix", "", "the prompt prefix (optional)")
	suggestFeedbackCmd.Flags().Int64Var(&sfLatencyMs, "latency-ms", 0, "latency in milliseconds (optional)")
}

func runSuggestFeedback(cmd *cobra.Command, args []string) error {
	if integrationDisabled() {
		return nil
	}

	// Validate required fields
	if sfAction == "" {
		return fmt.Errorf("--action is required")
	}
	if sfSuggested == "" {
		return fmt.Errorf("--suggested is required")
	}

	// Validate action value
	validActions := map[string]bool{
		"accepted": true, "dismissed": true, "edited": true,
		"never": true, "unblock": true, "ignored": true, "timeout": true,
	}
	if !validActions[sfAction] {
		return fmt.Errorf("invalid action: %q", sfAction)
	}

	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		// No session - silently ignore
		return nil
	}

	client, err := ipc.NewClient()
	if err != nil {
		// Daemon not available - silently ignore
		return nil
	}
	defer client.Close()

	ok, err := client.RecordFeedbackSync(context.Background(), sessionID, sfAction, sfSuggested, sfExecuted, sfPrefix, sfLatencyMs)
	if err != nil {
		// Silently ignore daemon errors
		return nil
	}
	_ = ok

	return nil
}
