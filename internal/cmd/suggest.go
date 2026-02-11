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

	pb "github.com/runger/clai/gen/clai/v1"
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

	prefix := parseSuggestPrefix(args)
	lastCmd, lastCmdNorm := resolveLastCommand()
	format := resolveSuggestFormat()

	if integrationDisabled() {
		return outputIntegrationDisabled(format)
	}

	hint := buildSuggestTimingHint()

	// Empty prefix - return cached AI suggestion
	if prefix == "" {
		return outputCachedSuggestion(format, hint, lastCmd, lastCmdNorm)
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

func parseSuggestPrefix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func resolveLastCommand() (string, string) {
	lastCmd := strings.TrimSpace(os.Getenv("CLAI_LAST_COMMAND"))
	if lastCmd == "" {
		return "", ""
	}
	return lastCmd, strings.TrimSpace(normalize.NormalizeSimple(lastCmd))
}

func resolveSuggestFormat() string {
	format := suggestFormat
	if suggestJSON && format == "text" {
		return "json"
	}
	return format
}

func outputIntegrationDisabled(format string) error {
	if format != "json" {
		return nil
	}
	return writeSuggestJSON(nil, nil)
}

func buildSuggestTimingHint() *timing.TimingHint {
	cfg, err := config.Load()
	if err != nil || !cfg.Suggestions.AdaptiveTimingEnabled {
		return nil
	}
	machine := getSessionTimingMachine()
	if machine == nil {
		return nil
	}
	nowMs := time.Now().UnixMilli()
	_, _ = machine.OnKeystroke(nowMs)
	h := machine.Hint()
	return &h
}

func outputCachedSuggestion(format string, hint *timing.TimingHint, lastCmd, lastCmdNorm string) error {
	suggestion, _ := cache.ReadSuggestion()
	if suggestion != "" && shouldSuppressLastCmd(suggestion, lastCmd, lastCmdNorm) {
		suggestion = ""
	}
	if format == "json" {
		return writeCachedSuggestionJSON(suggestion, hint)
	}
	if suggestion != "" {
		fmt.Println(suggestion)
	}
	return nil
}

func writeCachedSuggestionJSON(suggestion string, hint *timing.TimingHint) error {
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
	if strings.TrimSpace(s.Recency) != "" {
		meta += "  · " + strings.TrimSpace(s.Recency)
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
	Recency     string           `json:"recency,omitempty"`
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
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	client, err := ipc.NewClient()
	if err != nil {
		return nil
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	daemonSuggestions := client.Suggest(ctx, sessionID, cwd, prefix, len(prefix), false, limit)
	if len(daemonSuggestions) == 0 {
		return nil
	}

	includeReasons := shouldIncludeSuggestReasons()
	results := make([]suggestOutput, len(daemonSuggestions))
	for i, s := range daemonSuggestions {
		results[i] = daemonSuggestionToOutput(s, includeReasons)
	}

	return results
}

func shouldIncludeSuggestReasons() bool {
	if suggestExplain {
		return true
	}
	cfg, err := config.Load()
	return err == nil && cfg.Suggestions.ExplainEnabled
}

func daemonSuggestionToOutput(s *pb.Suggestion, includeReasons bool) suggestOutput {
	if s == nil {
		return suggestOutput{}
	}
	cwdMatch, recency := deriveSuggestionMeta(s)
	out := suggestOutput{
		Text:        s.Text,
		Source:      s.Source,
		Score:       float64(s.Score),
		Description: s.Description,
		Risk:        s.Risk,
		CwdMatch:    cwdMatch,
		Recency:     recency,
	}
	if includeReasons {
		out.Reasons = daemonReasonsToExplain(s.Reasons)
	}
	return out
}

func deriveSuggestionMeta(s *pb.Suggestion) (bool, string) {
	cwdMatch := strings.TrimSpace(s.Source) == "cwd"
	recency := ""
	for _, r := range s.Reasons {
		if r == nil {
			continue
		}
		switch strings.TrimSpace(r.Type) {
		case suggest2.ReasonDirTransition, suggest2.ReasonDirFrequency:
			cwdMatch = true
		case "recency":
			if recency == "" {
				recency = strings.TrimSpace(r.Description)
			}
		}
	}
	return cwdMatch, recency
}

func daemonReasonsToExplain(reasons []*pb.SuggestionReason) []explain.Reason {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]explain.Reason, 0, len(reasons))
	for _, r := range reasons {
		if r == nil {
			continue
		}
		out = append(out, explain.Reason{
			Tag:          r.Type,
			Description:  r.Description,
			Contribution: float64(r.Contribution),
		})
	}
	return out
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
