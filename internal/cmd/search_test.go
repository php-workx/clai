package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSearch_HistoryFallback_NoDaemon(t *testing.T) {
	// runSearch uses history file directly; this verifies it works without a daemon.
	searchJSON = false
	searchLimit = 5
	t.Cleanup(func() {
		searchJSON = false
		searchLimit = 20
	})

	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, "zsh_history")
	content := strings.Join([]string{
		": 1700000000:0;echo hello",
		": 1700000001:0;git status",
		": 1700000002:0;npm test",
	}, "\n")
	if err := os.WriteFile(histFile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	t.Setenv("HISTFILE", histFile)
	// Point HOME to a clean temp dir so searchCommandsFromDB finds no
	// existing suggest DB (~/.clai/suggestions_v2.db) and falls back
	// to history file search.
	t.Setenv("HOME", tmpDir)

	output := captureStdout(t, func() {
		if err := runSearch(searchCmd, []string{"git"}); err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
	})

	if !strings.Contains(output, "git status") {
		t.Fatalf("expected git status in output, got %q", output)
	}
}

func TestRunSearch_InvalidLimit(t *testing.T) {
	searchJSON = false
	searchLimit = 0
	t.Cleanup(func() {
		searchJSON = false
		searchLimit = 20
	})

	err := runSearch(searchCmd, []string{"git"})
	if err == nil {
		t.Fatal("expected error for non-positive --limit")
	}
	if !strings.Contains(err.Error(), "invalid --limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}
