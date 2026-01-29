package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuggestion(t *testing.T) {
	// Create temp history file
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, ".zsh_history")

	// Write test history in zsh extended format
	histContent := `: 1706000001:0;ls -la
: 1706000002:0;git status
: 1706000003:0;git commit -m "test"
: 1706000004:0;git push origin main
: 1706000005:0;npm install express
: 1706000006:0;npm run build
`
	if err := os.WriteFile(histFile, []byte(histContent), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	// Set HISTFILE to our test file
	oldHistFile := os.Getenv("HISTFILE")
	os.Setenv("HISTFILE", histFile)
	defer os.Setenv("HISTFILE", oldHistFile)

	tests := []struct {
		prefix   string
		expected string
	}{
		{"git s", "git status"},
		{"git c", "git commit -m \"test\""},
		{"git p", "git push origin main"},
		{"npm i", "npm install express"},
		{"npm r", "npm run build"},
		{"ls", "ls -la"},
		{"xyz", ""},        // No match
		{"", ""},           // Empty prefix
		{"git status", ""}, // Exact match should not suggest itself
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			result := Suggestion(tt.prefix)
			if result != tt.expected {
				t.Errorf("Suggestion(%q) = %q, want %q", tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestSuggestion_PlainHistory(t *testing.T) {
	// Create temp history file with plain format (no timestamps)
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, ".zsh_history")

	histContent := `ls -la
git status
git commit -m "test"
`
	if err := os.WriteFile(histFile, []byte(histContent), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	oldHistFile := os.Getenv("HISTFILE")
	os.Setenv("HISTFILE", histFile)
	defer os.Setenv("HISTFILE", oldHistFile)

	result := Suggestion("git s")
	if result != "git status" {
		t.Errorf("Suggestion(\"git s\") = %q, want \"git status\"", result)
	}
}

func TestSuggestion_MostRecent(t *testing.T) {
	// Verify it returns the most recent match
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, ".zsh_history")

	histContent := `: 1706000001:0;git status
: 1706000002:0;git stash
: 1706000003:0;git stash pop
`
	if err := os.WriteFile(histFile, []byte(histContent), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	oldHistFile := os.Getenv("HISTFILE")
	os.Setenv("HISTFILE", histFile)
	defer os.Setenv("HISTFILE", oldHistFile)

	// Should return most recent match (git stash pop)
	result := Suggestion("git st")
	if result != "git stash pop" {
		t.Errorf("Suggestion(\"git st\") = %q, want \"git stash pop\"", result)
	}
}

func TestSuggestion_TrailingMultiline(t *testing.T) {
	// Verify trailing unfinished multiline command is preserved
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, ".zsh_history")

	// History file ending with incomplete multiline command (no final newline)
	histContent := `: 1706000001:0;echo "hello"
: 1706000002:0;docker run \
  --name test \
  alpine`
	if err := os.WriteFile(histFile, []byte(histContent), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	oldHistFile := os.Getenv("HISTFILE")
	os.Setenv("HISTFILE", histFile)
	defer os.Setenv("HISTFILE", oldHistFile)

	// Should find the multiline docker command
	result := Suggestion("docker")
	expected := "docker run \n  --name test \n  alpine"
	if result != expected {
		t.Errorf("Suggestion(\"docker\") = %q, want %q", result, expected)
	}
}
