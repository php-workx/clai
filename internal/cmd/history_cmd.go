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
	historyOffset  int
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
	historyCmd.Flags().IntVar(&historyOffset, "offset", 0, "Skip this many results (for pagination)")
	historyCmd.Flags().StringVarP(&historyCWD, "cwd", "c", "", "Filter by working directory")
	historyCmd.Flags().StringVar(&historySession, "session", "", "Filter by specific session ID")
	historyCmd.Flags().BoolVarP(&historyGlobal, "global", "g", false, "Show history across all sessions")
	historyCmd.Flags().StringVarP(&historyStatus, "status", "s", "", "Filter by status: 'success' or 'failure'")
	historyCmd.Flags().StringVar(&historyFormat, "format", "raw", "Output format: raw or json")
}

func runHistory(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()

	store, err := storage.NewSQLiteStore(paths.DatabaseFile())
	if err != nil {
		fmt.Printf("No history available. Database not found at: %s\n", paths.DatabaseFile())
		return nil
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionID := historySession
	if sessionID == "" && !historyGlobal {
		sessionID = os.Getenv("CLAI_SESSION_ID")
	}

	if sessionID != "" {
		sessionID, err = resolveSessionID(ctx, store, sessionID)
		if err != nil {
			return err
		}
	}

	query, err := buildHistoryQuery(args)
	if err != nil {
		return err
	}
	if sessionID != "" {
		query.SessionID = &sessionID
	}

	commands, err := store.QueryCommands(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}

	return outputHistory(commands)
}

func resolveSessionID(ctx context.Context, store *storage.SQLiteStore, rawID string) (string, error) {
	session, err := store.GetSession(ctx, rawID)
	if err == nil {
		return session.SessionID, nil
	}
	if !errors.Is(err, storage.ErrSessionNotFound) {
		return "", err
	}
	if len(rawID) >= 36 {
		return "", fmt.Errorf("session not found (%s): %w", rawID, storage.ErrSessionNotFound)
	}
	session, err = store.GetSessionByPrefix(ctx, rawID)
	if err == nil {
		return session.SessionID, nil
	}
	if errors.Is(err, storage.ErrSessionNotFound) {
		return "", fmt.Errorf("session not found (%s): %w", rawID, storage.ErrSessionNotFound)
	}
	if errors.Is(err, storage.ErrAmbiguousSession) {
		return "", fmt.Errorf("ambiguous session prefix (%s): %w", rawID, storage.ErrAmbiguousSession)
	}
	return "", err
}

func buildHistoryQuery(args []string) (storage.CommandQuery, error) {
	if historyLimit < 0 {
		return storage.CommandQuery{}, fmt.Errorf("invalid limit: must be >= 0")
	}
	if historyOffset < 0 {
		return storage.CommandQuery{}, fmt.Errorf("invalid offset: must be >= 0")
	}

	query := storage.CommandQuery{
		Limit:  historyLimit,
		Offset: historyOffset,
	}

	switch historyStatus {
	case "success":
		query.SuccessOnly = true
	case "failure":
		query.FailureOnly = true
	case "":
		// No filter
	default:
		return query, fmt.Errorf("invalid status: %s (use 'success' or 'failure')", historyStatus)
	}

	if len(args) > 0 {
		query.Prefix = strings.ToLower(args[0])
	}
	if historyCWD != "" {
		query.CWD = &historyCWD
	}

	return query, nil
}

func outputHistory(commands []storage.Command) error {
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
