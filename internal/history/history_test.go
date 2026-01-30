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

func TestSuggestions_Multiple(t *testing.T) {
	// Test returning multiple suggestions
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, ".zsh_history")

	histContent := `: 1706000001:0;git status
: 1706000002:0;git stash
: 1706000003:0;git stash pop
: 1706000004:0;git stash list
: 1706000005:0;git switch main
: 1706000006:0;git status --short
`
	if err := os.WriteFile(histFile, []byte(histContent), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	oldHistFile := os.Getenv("HISTFILE")
	os.Setenv("HISTFILE", histFile)
	defer os.Setenv("HISTFILE", oldHistFile)

	tests := []struct {
		name     string
		prefix   string
		limit    int
		expected []string
	}{
		{
			name:     "multiple git commands",
			prefix:   "git st",
			limit:    5,
			expected: []string{"git status --short", "git stash list", "git stash pop", "git stash", "git status"},
		},
		{
			name:     "limit to 2",
			prefix:   "git st",
			limit:    2,
			expected: []string{"git status --short", "git stash list"},
		},
		{
			name:     "limit to 1 (same as Suggestion)",
			prefix:   "git st",
			limit:    1,
			expected: []string{"git status --short"},
		},
		{
			name:     "no matches",
			prefix:   "xyz",
			limit:    5,
			expected: nil,
		},
		{
			name:     "empty prefix",
			prefix:   "",
			limit:    5,
			expected: nil,
		},
		{
			name:     "zero limit",
			prefix:   "git",
			limit:    0,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Suggestions(tt.prefix, tt.limit)
			if len(result) != len(tt.expected) {
				t.Errorf("Suggestions(%q, %d) returned %d results, want %d", tt.prefix, tt.limit, len(result), len(tt.expected))
				return
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("Suggestions(%q, %d)[%d] = %q, want %q", tt.prefix, tt.limit, i, result[i], exp)
				}
			}
		})
	}
}

func TestSuggestions_Deduplication(t *testing.T) {
	// Test that duplicate commands are deduplicated
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, ".zsh_history")

	// Same command repeated multiple times
	histContent := `: 1706000001:0;git status
: 1706000002:0;git status
: 1706000003:0;git status
: 1706000004:0;git stash
: 1706000005:0;git status
`
	if err := os.WriteFile(histFile, []byte(histContent), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	oldHistFile := os.Getenv("HISTFILE")
	os.Setenv("HISTFILE", histFile)
	defer os.Setenv("HISTFILE", oldHistFile)

	// Should return only unique commands
	result := Suggestions("git", 5)
	expected := []string{"git status", "git stash"}

	if len(result) != len(expected) {
		t.Errorf("Suggestions with duplicates returned %d results, want %d: %v", len(result), len(expected), result)
		return
	}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("result[%d] = %q, want %q", i, result[i], exp)
		}
	}
}
