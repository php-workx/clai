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

	histFile := filepath.Join(t.TempDir(), "zsh_history")
	content := strings.Join([]string{
		": 1700000000:0;echo hello",
		": 1700000001:0;git status",
		": 1700000002:0;npm test",
	}, "\n")
	if err := os.WriteFile(histFile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	t.Setenv("HISTFILE", histFile)

	output := captureStdout(t, func() {
		if err := runSearch(searchCmd, []string{"git"}); err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
	})

	if !strings.Contains(output, "git status") {
		t.Fatalf("expected git status in output, got %q", output)
	}
}
