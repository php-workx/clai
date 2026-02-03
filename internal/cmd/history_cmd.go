package cmd

import (
	"context"
	"encoding/json"
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
	historyFormat  string
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
  clai history -s failure         # Show only failed commands
  clai history --format json      # Emit JSON entries for picker use`,
	RunE: runHistory,
}

func init() {
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 20, "Maximum number of commands to show")
	historyCmd.Flags().StringVarP(&historyCWD, "cwd", "c", "", "Filter by working directory")
	historyCmd.Flags().StringVar(&historySession, "session", "", "Filter by specific session ID")
	historyCmd.Flags().BoolVarP(&historyGlobal, "global", "g", false, "Show history across all sessions")
	historyCmd.Flags().StringVarP(&historyStatus, "status", "s", "", "Filter by status: 'success' or 'failure'")
	historyCmd.Flags().StringVar(&historyFormat, "format", "raw", "Output format: raw or json")
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

	format := strings.ToLower(strings.TrimSpace(historyFormat))
	if format == "" {
		format = "raw"
	}
	switch format {
	case "raw":
		for _, c := range commands {
			fmt.Println(c.Command)
		}
		return nil
	case "json":
		entries := make([]historyOutput, 0, len(commands))
		source := historySource(historyGlobal, historyCWD, historySession)
		for _, c := range commands {
			entries = append(entries, historyOutput{
				Text:     c.Command,
				Cwd:      c.CWD,
				TsUnixMs: c.TsStartUnixMs,
				ExitCode: c.ExitCode,
				Source:   source,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(entries)
	default:
		return fmt.Errorf("invalid format: %s (use raw or json)", historyFormat)
	}
}

type historyOutput struct {
	Text     string `json:"text"`
	Cwd      string `json:"cwd"`
	TsUnixMs int64  `json:"ts_unix_ms"`
	ExitCode *int   `json:"exit_code"`
	Source   string `json:"source"`
}

func historySource(global bool, cwd, session string) string {
	if session != "" {
		return "session"
	}
	if cwd != "" {
		return "cwd"
	}
	if global {
		return "global"
	}
	return "session"
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

	// Format git context (branch @ repo)
	gitContext := ""
	if c.GitBranch != nil && *c.GitBranch != "" {
		gitContext = *c.GitBranch
		if c.GitRepoName != nil && *c.GitRepoName != "" {
			gitContext += " @ " + *c.GitRepoName
		}
	}

	// Print formatted line
	fmt.Printf("%s%s%s  [%s]  %s", colorDim, timestamp, colorReset, exitCode, c.Command)

	if duration != "" {
		fmt.Printf("  %s(%s)%s", colorDim, duration, colorReset)
	}

	if gitContext != "" {
		fmt.Printf("  %s%s%s", colorCyan, gitContext, colorReset)
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
