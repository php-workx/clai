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

	if strings.TrimSpace(output) != "[]" {
		t.Fatalf("expected empty JSON array, got %q", output)
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

	var out []suggestOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(out))
	}
	if out[0].Text != "rm -rf /" {
		t.Fatalf("unexpected suggestion: %q", out[0].Text)
	}
	if out[0].Risk != "destructive" {
		t.Fatalf("expected destructive risk, got %q", out[0].Risk)
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

	if strings.TrimSpace(output) != "[]" {
		t.Fatalf("expected empty JSON array, got %q", output)
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
