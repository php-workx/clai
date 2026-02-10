package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/ipc"
	"github.com/runger/clai/internal/sanitize"
	"github.com/runger/clai/internal/suggestions/explain"
	"github.com/runger/clai/internal/suggestions/normalize"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
	"github.com/runger/clai/internal/suggestions/timing"
)

var (
	suggestLimit   int
	suggestJSON    bool
	suggestFormat  string
	suggestExplain bool

	// sessionTimingMu protects sessionTimingMachines.
	sessionTimingMu sync.Mutex
	// sessionTimingMachines maps session IDs to their typing cadence state machines.
	// The CLI process is short-lived per invocation, but the map is kept for
	// potential long-lived callers (e.g. tests, daemon embedding).
	sessionTimingMachines = make(map[string]*timing.Machine)
)

var suggestCmd = &cobra.Command{
	Use:     "suggest [prefix]",
	Short:   "Get command suggestion from session history or shell history",
	GroupID: groupCore,
	Long: `Get a command suggestion based on the current input prefix.

When the daemon is running, returns session-aware suggestions.
Falls back to shell history file if daemon is unavailable.
When prefix is empty, returns any cached AI suggestion.

This command is designed for shell integration (fast, minimal output).

Examples:
  clai suggest "git st"       # Returns "git status" from session/history
  clai suggest ""             # Returns cached AI suggestion if any
  clai suggest --limit 5 git  # Returns up to 5 suggestions`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSuggest,
}

func init() {
	suggestCmd.Flags().IntVarP(&suggestLimit, "limit", "n", 1, "maximum number of suggestions to return")
	suggestCmd.Flags().BoolVar(&suggestJSON, "json", false, "output suggestions as JSON (deprecated: use --format=json)")
	suggestCmd.Flags().StringVar(&suggestFormat, "format", "text", "output format: text, json, fzf, or ghost")
	suggestCmd.Flags().StringVar(&colorMode, "color", "auto", "color output: auto, always, or never")
	suggestCmd.Flags().BoolVar(&suggestExplain, "explain", false, "include reasons explaining why each suggestion was ranked")
}

func runSuggest(cmd *cobra.Command, args []string) error {
	applyColorMode()

	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	// Best-effort suppression: never suggest the exact last executed command.
	// This is defense-in-depth for cases where the daemon is down (history fallback),
	// session tracking is unavailable, or the AI cache repeats the last command.
	lastCmd := strings.TrimSpace(os.Getenv("CLAI_LAST_COMMAND"))
	lastCmdNorm := ""
	if lastCmd != "" {
		lastCmdNorm = strings.TrimSpace(normalize.NormalizeSimple(lastCmd))
	}

	// Determine output format (--json flag for backwards compat)
	format := suggestFormat
	if suggestJSON && format == "text" {
		format = "json"
	}

	if integrationDisabled() {
		if format == "json" {
			return writeSuggestJSON(nil, nil)
		}
		return nil
	}

	// Record keystroke in the timing state machine for this session.
	// Each invocation of "clai suggest" implicitly signals a keystroke.
	var hint *timing.TimingHint
	if cfg, err := config.Load(); err == nil && cfg.Suggestions.AdaptiveTimingEnabled {
		machine := getSessionTimingMachine()
		if machine != nil {
			nowMs := time.Now().UnixMilli()
			_, _ = machine.OnKeystroke(nowMs)
			h := machine.Hint()
			hint = &h
		}
	}

	// Empty prefix - return cached AI suggestion
	if prefix == "" {
		suggestion, _ := cache.ReadSuggestion()
		if suggestion != "" && shouldSuppressLastCmd(suggestion, lastCmd, lastCmdNorm) {
			suggestion = ""
		}
		if format == "json" {
			if suggestion == "" {
				return writeSuggestJSON(nil, hint)
			}
			return writeSuggestJSON([]suggestOutput{{
				Text:        suggestion,
				Source:      "ai",
				Score:       0,
				Description: "",
				Risk:        riskFromText(suggestion),
			}}, hint)
		}
		if suggestion != "" {
			fmt.Println(suggestion)
		}
		return nil
	}

	// Try daemon first for session-aware suggestions
	suggestions := getSuggestionsFromDaemon(prefix, suggestLimit)

	// Fall back to shell history if daemon returned nothing
	if len(suggestions) == 0 {
		suggestions = getSuggestionsFromHistory(prefix, suggestLimit)
	}

	// Defense-in-depth: suppress the last command regardless of source.
	if len(suggestions) > 0 && lastCmd != "" {
		suggestions = filterSuppressedSuggestions(suggestions, lastCmd, lastCmdNorm)
	}

	// Output based on format
	return outputSuggestions(suggestions, format, hint)
}

func shouldSuppressLastCmd(suggestion, lastCmd, lastCmdNorm string) bool {
	if strings.TrimSpace(suggestion) == "" || strings.TrimSpace(lastCmd) == "" {
		return false
	}
	sNorm := strings.TrimSpace(normalize.NormalizeSimple(suggestion))
	// Prefer normalized comparison when we have it; fall back to raw equality.
	if lastCmdNorm != "" && sNorm != "" {
		return sNorm == lastCmdNorm
	}
	return strings.TrimSpace(suggestion) == strings.TrimSpace(lastCmd)
}

func filterSuppressedSuggestions(suggestions []suggestOutput, lastCmd, lastCmdNorm string) []suggestOutput {
	out := suggestions[:0]
	for _, s := range suggestions {
		if shouldSuppressLastCmd(s.Text, lastCmd, lastCmdNorm) {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatGhostMeta(s suggestOutput) string {
	src := strings.TrimSpace(s.Source)
	if src == "" {
		src = "unknown"
	}

	score := s.Score
	if math.IsNaN(score) || math.IsInf(score, 0) {
		score = 0
	}

	meta := fmt.Sprintf("· %s  · score %.2f", src, score)
	if s.CwdMatch {
		meta += "  · cwd"
	}
	risk := strings.TrimSpace(strings.ToLower(s.Risk))
	if risk == "destructive" {
		meta += "  · [!] destructive"
	}
	return meta
}

// outputSuggestions formats and outputs suggestions based on format type.
func outputSuggestions(suggestions []suggestOutput, format string, hint *timing.TimingHint) error {
	switch format {
	case "json":
		return writeSuggestJSON(suggestions, hint)
	case "fzf":
		// fzf format: plain commands, one per line (for piping to fzf)
		for _, s := range suggestions {
			fmt.Println(s.Text)
		}
	case "ghost":
		// ghost format: one suggestion per line as "command<TAB>meta".
		// This is used for inline ghost text in shells where accepting the
		// suggestion must insert only the command text, but the UI can display
		// additional metadata.
		for _, s := range suggestions {
			fmt.Printf("%s\t%s\n", s.Text, formatGhostMeta(s))
		}
	case "text":
		// text format: numbered list with metadata
		for i, s := range suggestions {
			reasons := s.Source
			if s.Risk != "" {
				reasons += ", " + s.Risk
			}
			fmt.Printf("%d. %s (%s)\n", i+1, s.Text, reasons)
		}
	default:
		// Unknown format, treat as fzf (plain output)
		for _, s := range suggestions {
			fmt.Println(s.Text)
		}
	}
	return nil
}

type suggestOutput struct {
	Text        string           `json:"text"`
	Source      string           `json:"source"`
	Score       float64          `json:"score"`
	Description string           `json:"description"`
	Risk        string           `json:"risk"`
	CwdMatch    bool             `json:"cwd_match,omitempty"`
	Reasons     []explain.Reason `json:"reasons,omitempty"`
}

// suggestJSONResponse wraps suggestions with optional timing hint for JSON output.
type suggestJSONResponse struct {
	Suggestions []suggestOutput    `json:"suggestions"`
	TimingHint  *timing.TimingHint `json:"timing_hint,omitempty"`
}

func riskFromText(text string) string {
	if sanitize.IsDestructive(text) {
		return "destructive"
	}
	return ""
}

func writeSuggestJSON(suggestions []suggestOutput, hint *timing.TimingHint) error {
	if suggestions == nil {
		suggestions = []suggestOutput{}
	}
	resp := suggestJSONResponse{
		Suggestions: suggestions,
		TimingHint:  hint,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(resp)
}

func getSuggestionsFromHistory(prefix string, limit int) []suggestOutput {
	results := history.Suggestions(prefix, limit)
	if len(results) == 0 {
		return nil
	}
	suggestions := make([]suggestOutput, 0, len(results))
	for _, s := range results {
		suggestions = append(suggestions, suggestOutput{
			Text:        s,
			Source:      "global",
			Score:       0,
			Description: "",
			Risk:        riskFromText(s),
		})
	}
	return suggestions
}

// getSuggestionsFromDaemon tries to get suggestions from the running daemon.
// Returns nil if daemon is unavailable or returns no results.
func getSuggestionsFromDaemon(prefix string, limit int) []suggestOutput {
	// Need session ID from environment
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		return nil
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	// Try to connect to daemon
	client, err := ipc.NewClient()
	if err != nil {
		return nil // Daemon not available
	}
	defer client.Close()

	// Get suggestions from daemon (short timeout for shell integration responsiveness)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	daemonSuggestions := client.Suggest(ctx, sessionID, cwd, prefix, len(prefix), false, limit)
	if len(daemonSuggestions) == 0 {
		return nil
	}

	// Determine whether to include reasons in output.
	includeReasons := suggestExplain
	if !includeReasons {
		if cfg, err := config.Load(); err == nil {
			includeReasons = cfg.Suggestions.ExplainEnabled
		}
	}

	// Convert to suggestOutput slice
	results := make([]suggestOutput, len(daemonSuggestions))
	for i, s := range daemonSuggestions {
		cwdMatch := false
		for _, r := range s.Reasons {
			switch strings.TrimSpace(r.Type) {
			case suggest2.ReasonDirTransition, suggest2.ReasonDirFrequency:
				cwdMatch = true
			}
		}
		if strings.TrimSpace(s.Source) == "cwd" {
			cwdMatch = true
		}

		out := suggestOutput{
			Text:        s.Text,
			Source:      s.Source,
			Score:       float64(s.Score),
			Description: s.Description,
			Risk:        s.Risk,
			CwdMatch:    cwdMatch,
		}

		// Convert daemon-provided reasons to explain.Reason when enabled.
		if includeReasons && len(s.Reasons) > 0 {
			out.Reasons = make([]explain.Reason, len(s.Reasons))
			for j, r := range s.Reasons {
				out.Reasons[j] = explain.Reason{
					Tag:          r.Type,
					Description:  r.Description,
					Contribution: float64(r.Contribution),
				}
			}
		}

		results[i] = out
	}

	return results
}

// getSessionTimingMachine returns the timing state machine for the current
// session. Creates one on first access. Returns nil if no session ID is set.
func getSessionTimingMachine() *timing.Machine {
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		return nil
	}

	sessionTimingMu.Lock()
	defer sessionTimingMu.Unlock()

	m, ok := sessionTimingMachines[sessionID]
	if !ok {
		cfg, err := config.Load()
		var tc timing.Config
		if err == nil {
			// Convert CPS threshold to inter-keystroke ms threshold:
			// e.g. 6.0 CPS => ~167ms per keystroke => threshold at 1000/6.0
			if cfg.Suggestions.TypingFastThresholdCPS > 0 {
				tc.FastThresholdMs = int64(1000.0 / cfg.Suggestions.TypingFastThresholdCPS)
			}
			if cfg.Suggestions.TypingPauseThresholdMs > 0 {
				tc.PauseThresholdMs = int64(cfg.Suggestions.TypingPauseThresholdMs)
			}
		}
		m = timing.NewMachine(tc)
		sessionTimingMachines[sessionID] = m
	}
	return m
}

func integrationDisabled() bool {
	if os.Getenv("CLAI_OFF") == "1" {
		return true
	}
	if cache.SessionOff() {
		return true
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	return !cfg.Suggestions.Enabled
}
