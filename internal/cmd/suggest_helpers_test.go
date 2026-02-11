package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/config"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
	"github.com/runger/clai/internal/suggestions/timing"
)

func TestWriteCachedSuggestionJSON_Empty(t *testing.T) {
	output := captureStdout(t, func() {
		if err := writeCachedSuggestionJSON("", nil); err != nil {
			t.Fatalf("writeCachedSuggestionJSON() error = %v", err)
		}
	})

	var resp suggestJSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if len(resp.Suggestions) != 0 {
		t.Fatalf("suggestions len = %d, want 0", len(resp.Suggestions))
	}
}

func TestWriteCachedSuggestionJSON_WithSuggestion(t *testing.T) {
	output := captureStdout(t, func() {
		if err := writeCachedSuggestionJSON("rm -rf /tmp/x", nil); err != nil {
			t.Fatalf("writeCachedSuggestionJSON() error = %v", err)
		}
	})

	var resp suggestJSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("suggestions len = %d, want 1", len(resp.Suggestions))
	}
	if resp.Suggestions[0].Risk != "destructive" {
		t.Fatalf("risk = %q, want destructive", resp.Suggestions[0].Risk)
	}
}

func TestShouldSuppressLastCmd(t *testing.T) {
	tests := []struct {
		name       string
		suggestion string
		lastCmd    string
		lastNorm   string
		want       bool
	}{
		{name: "empty suggestion", suggestion: "", lastCmd: "git status", lastNorm: "git status", want: false},
		{name: "empty last cmd", suggestion: "git status", lastCmd: "", lastNorm: "", want: false},
		{name: "normalized equality", suggestion: "git   status", lastCmd: "git status", lastNorm: "git status", want: true},
		{name: "raw equality fallback", suggestion: "make test", lastCmd: "make test", lastNorm: "", want: true},
		{name: "different commands", suggestion: "make test", lastCmd: "make build", lastNorm: "make build", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSuppressLastCmd(tc.suggestion, tc.lastCmd, tc.lastNorm)
			if got != tc.want {
				t.Fatalf("shouldSuppressLastCmd() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterSuppressedSuggestions(t *testing.T) {
	suggestions := []suggestOutput{
		{Text: "git status"},
		{Text: "make test"},
		{Text: "git status"},
	}

	filtered := filterSuppressedSuggestions(suggestions, "git status", "git status")
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].Text != "make test" {
		t.Fatalf("filtered[0] = %q, want make test", filtered[0].Text)
	}

	none := filterSuppressedSuggestions([]suggestOutput{{Text: "git status"}}, "git status", "git status")
	if none != nil {
		t.Fatalf("expected nil when all suggestions are suppressed, got %#v", none)
	}
}

func TestOutputPlainSuggestions(t *testing.T) {
	output := captureStdout(t, func() {
		outputPlainSuggestions([]suggestOutput{
			{Text: "git status"},
			{Text: "make test"},
		})
	})

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2 (output=%q)", len(lines), output)
	}
	if lines[0] != "git status" || lines[1] != "make test" {
		t.Fatalf("unexpected output lines: %q", output)
	}
}

func TestShouldIncludeSuggestReasons(t *testing.T) {
	t.Run("flag enabled", func(t *testing.T) {
		old := suggestExplain
		suggestExplain = true
		t.Cleanup(func() { suggestExplain = old })

		if !shouldIncludeSuggestReasons() {
			t.Fatalf("shouldIncludeSuggestReasons() = false, want true when --explain is set")
		}
	})

	t.Run("config enabled", func(t *testing.T) {
		old := suggestExplain
		suggestExplain = false
		t.Cleanup(func() { suggestExplain = old })

		home := t.TempDir()
		t.Setenv("CLAI_HOME", home)
		cfg := config.DefaultConfig()
		cfg.Suggestions.ExplainEnabled = true
		if err := cfg.SaveToFile(filepath.Join(home, "config.yaml")); err != nil {
			t.Fatalf("SaveToFile() error = %v", err)
		}

		if !shouldIncludeSuggestReasons() {
			t.Fatalf("shouldIncludeSuggestReasons() = false, want true from config")
		}
	})

	t.Run("config load error", func(t *testing.T) {
		old := suggestExplain
		suggestExplain = false
		t.Cleanup(func() { suggestExplain = old })

		home := t.TempDir()
		t.Setenv("CLAI_HOME", home)
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("invalid: ["), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		if shouldIncludeSuggestReasons() {
			t.Fatalf("shouldIncludeSuggestReasons() = true, want false when config load fails")
		}
	})
}

func TestDaemonSuggestionToOutput(t *testing.T) {
	s := &pb.Suggestion{
		Text:        "make test",
		Source:      "global",
		Score:       0.75,
		Description: "from history",
		Risk:        "",
		Reasons: []*pb.SuggestionReason{
			{Type: "recency", Description: "last 2h ago", Contribution: 0.4},
			nil,
		},
	}

	out := daemonSuggestionToOutput(s, false)
	if len(out.Reasons) != 0 {
		t.Fatalf("expected no reasons when includeReasons=false, got %d", len(out.Reasons))
	}
	if out.Recency != "last 2h ago" {
		t.Fatalf("recency = %q, want %q", out.Recency, "last 2h ago")
	}

	withReasons := daemonSuggestionToOutput(s, true)
	if len(withReasons.Reasons) != 1 {
		t.Fatalf("reasons len = %d, want 1", len(withReasons.Reasons))
	}
}

func TestDeriveSuggestionMeta(t *testing.T) {
	s := &pb.Suggestion{
		Source: "global",
		Reasons: []*pb.SuggestionReason{
			{Type: suggest2.ReasonDirTransition, Description: "same cwd", Contribution: 0.2},
			{Type: "recency", Description: "last 16s ago", Contribution: 0.3},
		},
	}

	cwdMatch, recency := deriveSuggestionMeta(s)
	if !cwdMatch {
		t.Fatalf("cwdMatch = false, want true")
	}
	if recency != "last 16s ago" {
		t.Fatalf("recency = %q, want %q", recency, "last 16s ago")
	}
}

func TestDaemonReasonsToExplain(t *testing.T) {
	out := daemonReasonsToExplain(nil)
	if out != nil {
		t.Fatalf("expected nil for nil input, got %#v", out)
	}

	in := []*pb.SuggestionReason{
		{Type: "frequency", Description: "freq 4", Contribution: 0.7},
		nil,
	}
	out = daemonReasonsToExplain(in)
	if len(out) != 1 {
		t.Fatalf("reasons len = %d, want 1", len(out))
	}
	if out[0].Tag != "frequency" {
		t.Fatalf("tag = %q, want frequency", out[0].Tag)
	}
}

func TestGetSessionTimingMachine(t *testing.T) {
	sessionTimingMu.Lock()
	sessionTimingMachines = make(map[string]*timing.Machine)
	sessionTimingMu.Unlock()

	t.Run("no session id", func(t *testing.T) {
		t.Setenv("CLAI_SESSION_ID", "")
		if m := getSessionTimingMachine(); m != nil {
			t.Fatalf("expected nil machine when no session id")
		}
	})

	t.Run("reuse per session", func(t *testing.T) {
		t.Setenv("CLAI_SESSION_ID", "session-123")
		home := t.TempDir()
		t.Setenv("CLAI_HOME", home)

		cfg := config.DefaultConfig()
		cfg.Suggestions.TypingFastThresholdCPS = 5.0
		cfg.Suggestions.TypingPauseThresholdMs = 250
		if err := cfg.SaveToFile(filepath.Join(home, "config.yaml")); err != nil {
			t.Fatalf("SaveToFile() error = %v", err)
		}

		m1 := getSessionTimingMachine()
		m2 := getSessionTimingMachine()
		if m1 == nil || m2 == nil {
			t.Fatalf("expected non-nil timing machine")
		}
		if m1 != m2 {
			t.Fatalf("expected same timing machine pointer for same session")
		}
	})
}
