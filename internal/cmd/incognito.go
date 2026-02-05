package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Mode determines the behavior of incognito mode.
// NoSend: hooks skip ingestion entirely
// Ephemeral: hooks send events but they're not persisted (recommended)
const (
	IncognitoModeEphemeral = "ephemeral" // default: events sent with ephemeral=true
	IncognitoModeNoSend    = "nosend"    // skip ingestion entirely
)

var incognitoNoSend bool

var incognitoCmd = &cobra.Command{
	Use:   "incognito [on|off|status]",
	Short: "Toggle incognito mode for shell suggestions",
	Long: `Toggle incognito mode for shell suggestions.

When incognito is ON:
  - By default, commands are still sent to the daemon but with ephemeral=true
  - Ephemeral events are used for in-memory session context only
  - Commands are NEVER persisted to disk or used for future suggestions

Modes:
  --ephemeral (default): Send events with ephemeral=true, keeps current-session suggestions useful
  --no-send: Skip sending events entirely (simplest but loses current-session suggestions)

Usage:
  eval "$(clai incognito on)"     # Enable incognito mode (ephemeral)
  eval "$(clai incognito on --no-send)"  # Enable incognito mode (no-send)
  eval "$(clai incognito off)"    # Disable incognito mode
  clai incognito status           # Check current status
`,
	GroupID: groupCore,
	RunE:    runIncognito,
}

func init() {
	incognitoCmd.Flags().BoolVar(&incognitoNoSend, "no-send", false, "Skip sending events entirely (default: ephemeral mode)")

	rootCmd.AddCommand(incognitoCmd)
}

func runIncognito(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		// Show status when no args
		return showIncognitoStatus()
	}

	action := args[0]
	switch action {
	case "on":
		return enableIncognito(incognitoNoSend)
	case "off":
		return disableIncognito()
	case "status":
		return showIncognitoStatus()
	default:
		return fmt.Errorf("unknown action: %s (use on, off, or status)", action)
	}
}

// enableIncognito outputs shell commands to enable incognito mode.
// The output is meant to be eval'd by the shell.
func enableIncognito(noSend bool) error {
	if noSend {
		// No-send mode: skip ingestion entirely
		fmt.Println("export CLAI_NO_RECORD=1")
		fmt.Println("unset CLAI_EPHEMERAL")
		fmt.Fprintln(os.Stderr, "Incognito mode enabled (no-send): commands will not be recorded")
	} else {
		// Ephemeral mode (default): send with ephemeral=true
		fmt.Println("export CLAI_EPHEMERAL=1")
		fmt.Println("unset CLAI_NO_RECORD")
		fmt.Fprintln(os.Stderr, "Incognito mode enabled (ephemeral): commands will not be persisted")
	}
	return nil
}

// disableIncognito outputs shell commands to disable incognito mode.
func disableIncognito() error {
	fmt.Println("unset CLAI_NO_RECORD")
	fmt.Println("unset CLAI_EPHEMERAL")
	fmt.Fprintln(os.Stderr, "Incognito mode disabled: commands will be recorded normally")
	return nil
}

// showIncognitoStatus shows the current incognito mode status.
func showIncognitoStatus() error {
	noRecord := os.Getenv("CLAI_NO_RECORD")
	ephemeral := os.Getenv("CLAI_EPHEMERAL")

	if noRecord == "1" {
		fmt.Println("Incognito mode: ON (no-send)")
		fmt.Println("Commands are not being sent to the daemon")
		return nil
	}

	if ephemeral == "1" {
		fmt.Println("Incognito mode: ON (ephemeral)")
		fmt.Println("Commands are sent but not persisted to disk")
		return nil
	}

	fmt.Println("Incognito mode: OFF")
	fmt.Println("Commands are recorded and persisted normally")
	return nil
}

// IsIncognito returns true if incognito mode is enabled.
func IsIncognito() bool {
	return os.Getenv("CLAI_NO_RECORD") == "1" || os.Getenv("CLAI_EPHEMERAL") == "1"
}

// IsNoRecord returns true if CLAI_NO_RECORD is set.
func IsNoRecord() bool {
	return os.Getenv("CLAI_NO_RECORD") == "1"
}

// IsEphemeral returns true if CLAI_EPHEMERAL is set.
func IsEphemeral() bool {
	return os.Getenv("CLAI_EPHEMERAL") == "1"
}
