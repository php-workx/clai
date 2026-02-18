package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveFromRCFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-uninstall-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hooksDir := filepath.Join(tmpDir, "hooks")

	tests := []struct {
		name     string
		content  string
		expected string
		removed  bool
	}{
		{
			name:     "empty file",
			content:  "",
			expected: "",
			removed:  false,
		},
		{
			name:     "no clai content",
			content:  "export PATH=$PATH:/usr/local/bin\nalias ll='ls -la'\n",
			expected: "export PATH=$PATH:/usr/local/bin\nalias ll='ls -la'\n",
			removed:  false,
		},
		{
			name:     "with source line",
			content:  "# Config\nsource \"" + hooksDir + "/clai.zsh\"\nalias ll='ls -la'\n",
			expected: "# Config\nalias ll='ls -la'\n",
			removed:  true,
		},
		{
			name:     "with eval init",
			content:  "export PATH=$PATH\neval \"$(clai init zsh)\"\nalias ll='ls -la'\n",
			expected: "export PATH=$PATH\nalias ll='ls -la'\n",
			removed:  true,
		},
		{
			name:     "with comment marker",
			content:  "# Config\n# clai shell integration\nsource something\n",
			expected: "# Config\nsource something\n",
			removed:  true,
		},
		{
			name:     "multiple clai lines",
			content:  "# clai shell integration\nsource \"" + hooksDir + "/clai.zsh\"\neval \"$(clai init zsh)\"\nalias ll='ls -la'\n",
			expected: "alias ll='ls -la'\n",
			removed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rcFile := filepath.Join(tmpDir, "test_rc")
			if err := os.WriteFile(rcFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			removed, err := removeFromRCFile(rcFile, hooksDir)
			if err != nil {
				t.Fatalf("removeFromRCFile error: %v", err)
			}

			if removed != tt.removed {
				t.Errorf("removeFromRCFile() removed = %v, want %v", removed, tt.removed)
			}

			// Read back and compare
			content, err := os.ReadFile(rcFile)
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}

			got := string(content)
			if got != tt.expected {
				t.Errorf("After removal:\ngot: %q\nwant: %q", got, tt.expected)
			}
		})
	}
}

func TestRemoveFromRCFile_NonExistent(t *testing.T) {
	removed, err := removeFromRCFile("/nonexistent/path", "/hooks")
	if err != nil {
		t.Fatalf("Should not error for nonexistent file: %v", err)
	}
	if removed {
		t.Error("Should return false for nonexistent file")
	}
}

func TestRemoveFromRCFile_PreservesOtherContent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-uninstall-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rcFile := filepath.Join(tmpDir, ".zshrc")
	hooksDir := filepath.Join(tmpDir, "hooks")

	content := `# My shell config

# PATH setup
export PATH=$PATH:/usr/local/bin

# clai shell integration
source "` + hooksDir + `/clai.zsh"

# Aliases
alias ll='ls -la'
alias gst='git status'

# Load other tools
source ~/.fzf.zsh
`

	if writeErr := os.WriteFile(rcFile, []byte(content), 0644); writeErr != nil {
		t.Fatalf("Failed to write: %v", writeErr)
	}

	removed, err := removeFromRCFile(rcFile, hooksDir)
	if err != nil {
		t.Fatalf("removeFromRCFile error: %v", err)
	}
	if !removed {
		t.Error("Should have removed clai content")
	}

	// Read back
	result, _ := os.ReadFile(rcFile)
	resultStr := string(result)

	// Should not contain clai lines
	if strings.Contains(resultStr, "clai") {
		t.Error("Result should not contain 'clai'")
	}

	// Should still contain other content
	checks := []string{
		"PATH setup",
		"export PATH",
		"Aliases",
		"alias ll",
		"alias gst",
		"fzf.zsh",
	}

	for _, check := range checks {
		if !strings.Contains(resultStr, check) {
			t.Errorf("Result should contain %q", check)
		}
	}
}
