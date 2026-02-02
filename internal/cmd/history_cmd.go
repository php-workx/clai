package cmd

import (
	"context"
	"fmt"
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
)

var historyCmd = &cobra.Command{
	Use:     "history [query]",
	Short:   "Show command history",
	GroupID: groupCore,
	Long: `Show command history from the clai database.

Without arguments, shows the most recent commands.
With a query argument, filters commands matching the prefix.

The history is stored in the local SQLite database and includes
commands from all shell sessions by default. Use --session to
filter to a specific session.

Examples:
  clai history                    # Show last 20 commands
  clai history --limit=50         # Show last 50 commands
  clai history git                # Show commands starting with "git"
  clai history --cwd=/tmp         # Show commands from /tmp directory
  clai history --session=$CLAI_SESSION_ID  # Show current session only`,
	RunE: runHistory,
}

func init() {
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 20, "Maximum number of commands to show")
	historyCmd.Flags().StringVar(&historyCWD, "cwd", "", "Filter by working directory")
	historyCmd.Flags().StringVar(&historySession, "session", "", "Filter by session ID (use $CLAI_SESSION_ID for current session)")
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

	// Build query
	query := storage.CommandQuery{
		Limit: historyLimit,
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
	if historySession != "" {
		query.SessionID = &historySession
	}

	// Execute query
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	commands, err := store.QueryCommands(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}

	if len(commands) == 0 {
		if historySession != "" {
			// Session-specific query returned no results
			if len(args) > 0 {
				fmt.Printf("No commands matching '%s' in this session.\n", args[0])
			} else {
				fmt.Println("No commands logged in this session yet.")
			}
			fmt.Println("Tip: Use 'history --global' for shell's native history.")
		} else if len(args) > 0 {
			fmt.Printf("No commands found matching '%s'\n", args[0])
		} else {
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
	fmt.Printf("%sShowing %d command(s)%s\n", colorDim, len(commands), colorReset)

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
