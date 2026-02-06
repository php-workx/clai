package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/history"
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
	Ts      int64  `json:"ts,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	RepoKey string `json:"repo_key,omitempty"`
}

type searchResponse struct {
	Results   []searchOutput `json:"results"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
}

func runSearch(cmd *cobra.Command, args []string) error {
	applyColorMode()

	query := args[0]

	// Use history search for now (FTS5 will be added later)
	results := history.Search(query, searchLimit)

	if searchJSON {
		return writeSearchJSON(results)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	for _, r := range results {
		fmt.Println(r)
	}

	return nil
}

func writeSearchJSON(results []string) error {
	output := make([]searchOutput, len(results))
	for i, r := range results {
		output[i] = searchOutput{
			CmdRaw: r,
		}
	}

	resp := searchResponse{
		Results:   output,
		Total:     len(output),
		Truncated: len(output) >= searchLimit,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(resp)
}
