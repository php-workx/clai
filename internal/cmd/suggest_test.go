package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/config"
)

func TestRunSuggest_DisabledByEnv_JSON(t *testing.T) {
	withSuggestGlobals(t, suggestGlobals{limit: 5, json: true})
	t.Setenv("CLAI_OFF", "1")
	t.Setenv("CLAI_SESSION_ID", "")

	output := captureStdout(t, func() {
		if err := runSuggest(suggestCmd, []string{"git"}); err != nil {
			t.Fatalf("runSuggest error: %v", err)
		}
	})

	var resp suggestJSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(resp.Suggestions) != 0 {
		t.Fatalf("expected empty suggestions, got %d", len(resp.Suggestions))
	}
}

func TestRunSuggest_EmptyPrefix_UsesCache(t *testing.T) {
	withSuggestGlobals(t, suggestGlobals{limit: 1, json: false})
	cacheDir := t.TempDir()
	t.Setenv("CLAI_CACHE", cacheDir)
	t.Setenv("CLAI_HOME", t.TempDir()) // Isolate from user config

	if err := cache.WriteSuggestion("git status"); err != nil {
		t.Fatalf("WriteSuggestion error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runSuggest(suggestCmd, nil); err != nil {
			t.Fatalf("runSuggest error: %v", err)
		}
	})

	if strings.TrimSpace(output) != "git status" {
		t.Fatalf("expected cached suggestion, got %q", output)
	}
}

func TestRunSuggest_HistoryFallback_JSONRisk(t *testing.T) {
	withSuggestGlobals(t, suggestGlobals{limit: 2, json: true})
	t.Setenv("CLAI_HOME", t.TempDir())  // Isolate from user config
	t.Setenv("CLAI_CACHE", t.TempDir()) // Isolate from user session-off state
	histFile := filepath.Join(t.TempDir(), "zsh_history")
	content := strings.Join([]string{
		": 1700000000:0;echo hello",
		": 1700000001:0;rm -rf /",
		": 1700000002:0;git status",
	}, "\n")
	if err := os.WriteFile(histFile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	t.Setenv("HISTFILE", histFile)
	t.Setenv("CLAI_SESSION_ID", "")

	output := captureStdout(t, func() {
		if err := runSuggest(suggestCmd, []string{"rm"}); err != nil {
			t.Fatalf("runSuggest error: %v", err)
		}
	})

	var resp suggestJSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(resp.Suggestions))
	}
	if resp.Suggestions[0].Text != "rm -rf /" {
		t.Fatalf("unexpected suggestion: %q", resp.Suggestions[0].Text)
	}
	if resp.Suggestions[0].Risk != "destructive" {
		t.Fatalf("expected destructive risk, got %q", resp.Suggestions[0].Risk)
	}
}

func TestRunSuggest_DisabledByConfig(t *testing.T) {
	withSuggestGlobals(t, suggestGlobals{limit: 1, json: true})
	home := t.TempDir()
	t.Setenv("CLAI_HOME", home)
	cacheDir := filepath.Join(home, "cache")
	t.Setenv("CLAI_CACHE", cacheDir)

	cfg := config.DefaultConfig()
	cfg.Suggestions.Enabled = false
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save config error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runSuggest(suggestCmd, []string{"git"}); err != nil {
			t.Fatalf("runSuggest error: %v", err)
		}
	})

	var resp suggestJSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(resp.Suggestions) != 0 {
		t.Fatalf("expected empty suggestions, got %d", len(resp.Suggestions))
	}
}

func TestRunSuggest_HistoryFallback_NoDaemon(t *testing.T) {
	withSuggestGlobals(t, suggestGlobals{limit: 2, json: false})
	t.Setenv("CLAI_HOME", t.TempDir())
	t.Setenv("CLAI_CACHE", t.TempDir())
	t.Setenv("CLAI_SESSION_ID", "")

	histFile := filepath.Join(t.TempDir(), "zsh_history")
	content := strings.Join([]string{
		": 1700000000:0;git status",
		": 1700000001:0;git diff",
	}, "\n")
	if err := os.WriteFile(histFile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	t.Setenv("HISTFILE", histFile)

	output := captureStdout(t, func() {
		if err := runSuggest(suggestCmd, []string{"git"}); err != nil {
			t.Fatalf("runSuggest error: %v", err)
		}
	})

	if strings.TrimSpace(output) == "" {
		t.Fatalf("expected history fallback suggestion, got empty output")
	}
}

func TestOutputSuggestions_GhostFormat(t *testing.T) {
	out := captureStdout(t, func() {
		suggestions := []suggestOutput{
			{Text: "make install", Source: "global", Score: 0.47},
			{Text: "rm -rf /", Source: "global", Score: 0.99, Risk: "destructive"},
		}
		if err := outputSuggestions(suggestions, "ghost", nil); err != nil {
			t.Fatalf("outputSuggestions error: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "make install\t· global  · score 0.47" {
		t.Fatalf("unexpected ghost line 1: %q", lines[0])
	}
	if lines[1] != "rm -rf /\t· global  · score 0.99  · [!] destructive" {
		t.Fatalf("unexpected ghost line 2: %q", lines[1])
	}
}
