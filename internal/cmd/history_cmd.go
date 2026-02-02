package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/storage"
)

var (
	historyLimit   int
	historyCWD     string
	historySession string
	historyGlobal  bool
	historyStatus  string
)

var historyCmd = &cobra.Command{
	Use:     "history [query]",
	Short:   "Show command history",
	GroupID: groupCore,
	Long: `Show command history from the clai database.

Without arguments, shows the most recent commands from the current session.
With a query argument, filters commands matching the prefix.

By default, history is scoped to the current shell session (using $CLAI_SESSION_ID).
Use -g/--global to show history across all sessions.

Examples:
  clai history                    # Show last 20 commands (current session)
  clai history -g                 # Show history across all sessions
  clai history --limit=50         # Show last 50 commands
  clai history git                # Show commands starting with "git"
  clai history -c /tmp            # Show commands from /tmp directory
  clai history --session=abc123   # Show specific session (8+ char prefix)
  clai history -s success         # Show only successful commands
  clai history -s failure         # Show only failed commands`,
	RunE: runHistory,
}

func init() {
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 20, "Maximum number of commands to show")
	historyCmd.Flags().StringVarP(&historyCWD, "cwd", "c", "", "Filter by working directory")
	historyCmd.Flags().StringVar(&historySession, "session", "", "Filter by specific session ID")
	historyCmd.Flags().BoolVarP(&historyGlobal, "global", "g", false, "Show history across all sessions")
	historyCmd.Flags().StringVarP(&historyStatus, "status", "s", "", "Filter by status: 'success' or 'failure'")
}

func runHistory(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()

	// Open database
	store, err := storage.NewSQLiteStore(paths.DatabaseFile())
	if err != nil {
		fmt.Printf("No history available. Database not found at: %s\n", paths.DatabaseFile())
		return nil
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Determine session ID to use
	sessionID := historySession
	if sessionID == "" && !historyGlobal {
		// Default to current session from environment
		sessionID = os.Getenv("CLAI_SESSION_ID")
	}

	// Validate and resolve session if specified
	if sessionID != "" {
		session, err := store.GetSession(ctx, sessionID)
		if err != nil {
			if errors.Is(err, storage.ErrSessionNotFound) {
				// Try prefix match for short IDs (< 36 chars, full UUID length)
				if len(sessionID) < 36 {
					session, err = store.GetSessionByPrefix(ctx, sessionID)
					if err != nil {
						if errors.Is(err, storage.ErrSessionNotFound) {
							return fmt.Errorf("session not found (%s)", sessionID)
						}
						if errors.Is(err, storage.ErrAmbiguousSession) {
							return fmt.Errorf("ambiguous session prefix (%s)", sessionID)
						}
						return err
					}
					// Use the full session ID for the query
					sessionID = session.SessionID
				} else {
					return fmt.Errorf("session not found (%s)", sessionID)
				}
			} else {
				return err
			}
		} else {
			sessionID = session.SessionID
		}
	}

	// Build query
	query := storage.CommandQuery{
		Limit: historyLimit,
	}

	// Add status filter if provided
	switch historyStatus {
	case "success":
		query.SuccessOnly = true
	case "failure":
		query.FailureOnly = true
	case "":
		// No filter - show all
	default:
		return fmt.Errorf("invalid status: %s (use 'success' or 'failure')", historyStatus)
	}

	// Add prefix filter if provided
	if len(args) > 0 {
		query.Prefix = strings.ToLower(args[0])
	}

	// Add CWD filter if provided
	if historyCWD != "" {
		query.CWD = &historyCWD
	}

	// Add session filter if provided
	if sessionID != "" {
		query.SessionID = &sessionID
	}

	// Execute query
	commands, err := store.QueryCommands(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}

	if len(commands) == 0 {
		// Show appropriate message based on filters used
		if historyCWD != "" {
			fmt.Printf("%sWarning: No commands found in directory: %s%s\n", colorYellow, historyCWD, colorReset)
		}
		if len(args) > 0 {
			fmt.Printf("%sWarning: No commands found matching '%s'%s\n", colorYellow, args[0], colorReset)
		}

		if sessionID != "" && historyCWD == "" && len(args) == 0 {
			fmt.Println("No commands logged in this session yet.")
			fmt.Println("Tip: Use 'clai history -g' to show history across all sessions.")
		} else if historyCWD == "" && len(args) == 0 {
			fmt.Println("No command history available.")
		}
		return nil
	}

	// Print commands (most recent last for typical terminal usage)
	// Reverse the order since we want oldest at top
	for i := len(commands) - 1; i >= 0; i-- {
		c := commands[i]
		printCommand(c)
	}

	fmt.Println()
	scope := ""
	if sessionID != "" {
		scope = " (current session)"
	} else {
		scope = " (all sessions)"
	}
	fmt.Printf("%sShowing %d command(s)%s%s\n", colorDim, len(commands), scope, colorReset)

	return nil
}

func printCommand(c storage.Command) {
	// Format timestamp
	t := time.UnixMilli(c.TsStartUnixMs)
	timestamp := t.Format("2006-01-02 15:04:05")

	// Format exit code
	exitCode := ""
	if c.ExitCode != nil {
		if *c.ExitCode == 0 {
			exitCode = colorGreen + "0" + colorReset
		} else {
			exitCode = colorRed + fmt.Sprintf("%d", *c.ExitCode) + colorReset
		}
	} else {
		exitCode = colorDim + "-" + colorReset
	}

	// Format duration
	duration := ""
	if c.DurationMs != nil {
		duration = formatDurationMs(*c.DurationMs)
	}

	// Print formatted line
	fmt.Printf("%s%s%s  [%s]  %s", colorDim, timestamp, colorReset, exitCode, c.Command)

	if duration != "" {
		fmt.Printf("  %s(%s)%s", colorDim, duration, colorReset)
	}

	fmt.Println()
}

func formatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
