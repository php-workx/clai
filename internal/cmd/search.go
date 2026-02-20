package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/storage"
)

var (
	searchJSON  bool
	searchRepo  bool
	searchLimit int
)

var searchCmd = &cobra.Command{
	Use:     "search <query>",
	Short:   "Search command history",
	GroupID: groupCore,
	Long: `Search command history for matching commands.

Currently uses shell history file search. FTS5 full-text search
will be enabled in a future release when daemon supports it.

Examples:
  clai search "docker run"        # Search for docker commands
  clai search --json git          # Output as JSON
  clai search --limit 50 make     # Return up to 50 results`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "output results as JSON")
	searchCmd.Flags().BoolVar(&searchRepo, "repo", false, "search only within current repository (future)")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 20, "maximum number of results")
	searchCmd.Flags().StringVar(&colorMode, "color", "auto", "color output: auto, always, or never")

	rootCmd.AddCommand(searchCmd)
}

type searchOutput struct {
	CmdRaw  string `json:"cmd_raw"`
	Cwd     string `json:"cwd,omitempty"`
	RepoKey string `json:"repo_key,omitempty"`
	TS      int64  `json:"ts,omitempty"`
}

type searchResponse struct {
	Results   []searchOutput `json:"results"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
}

func runSearch(cmd *cobra.Command, args []string) error {
	applyColorMode()

	if searchLimit <= 0 {
		return fmt.Errorf("invalid --limit: must be > 0")
	}

	query := args[0]

	results := searchCommands(query, searchLimit)

	if searchJSON {
		return writeSearchJSON(results)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	for _, r := range results {
		fmt.Println(r.CmdRaw)
	}

	return nil
}

func writeSearchJSON(results []searchOutput) error {
	resp := searchResponse{
		Results:   results,
		Total:     len(results),
		Truncated: len(results) >= searchLimit,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(resp)
}

func searchCommands(query string, limit int) []searchOutput {
	merged := make([]searchOutput, 0, limit)
	seen := make(map[string]struct{}, limit)

	add := func(cmd searchOutput) {
		key := strings.TrimSpace(cmd.CmdRaw)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, cmd)
	}

	// Prefer DB-backed results first (freshly ingested runtime data),
	// then fall back to shell history for backward compatibility.
	if dbResults, err := searchCommandsFromDB(query, limit); err == nil {
		for _, r := range dbResults {
			if len(merged) >= limit {
				break
			}
			add(r)
		}
	}
	for _, r := range history.Search(query, limit) {
		if len(merged) >= limit {
			break
		}
		add(searchOutput{CmdRaw: r})
	}

	return merged
}

func searchCommandsFromDB(query string, limit int) ([]searchOutput, error) {
	paths := config.DefaultPaths()
	store, err := storage.NewSQLiteStore(paths.DatabaseFile())
	if err != nil {
		return nil, err
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	q := storage.CommandQuery{
		Limit:     limit,
		Substring: strings.ToLower(strings.TrimSpace(query)),
	}
	if searchRepo {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil && strings.TrimSpace(cwd) != "" {
			q.CWD = &cwd
		}
	}

	commands, err := store.QueryCommands(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make([]searchOutput, 0, len(commands))
	for i := range commands {
		row := searchOutput{
			CmdRaw: commands[i].Command,
			Cwd:    commands[i].CWD,
			TS:     commands[i].TSStartUnixMs,
		}
		if commands[i].GitRepoRoot != nil {
			row.RepoKey = *commands[i].GitRepoRoot
		}
		out = append(out, row)
	}
	return out, nil
}
