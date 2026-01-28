package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/runger/ai-terminal/internal/cache"
	"github.com/runger/ai-terminal/internal/extract"
)

func TestDiagnoseCmd_RequiresArgs(t *testing.T) {
	// Test that diagnose requires at least one argument
	err := diagnoseCmd.Args(diagnoseCmd, []string{})
	if err == nil {
		t.Error("diagnose should require at least 1 argument")
	}

	// Test that diagnose accepts 1 argument
	err = diagnoseCmd.Args(diagnoseCmd, []string{"npm run build"})
	if err != nil {
		t.Errorf("diagnose should accept 1 argument, got error: %v", err)
	}

	// Test that diagnose accepts 2 arguments
	err = diagnoseCmd.Args(diagnoseCmd, []string{"npm run build", "1"})
	if err != nil {
		t.Errorf("diagnose should accept 2 arguments, got error: %v", err)
	}

	// Test that diagnose rejects 3+ arguments
	err = diagnoseCmd.Args(diagnoseCmd, []string{"a", "b", "c"})
	if err == nil {
		t.Error("diagnose should reject more than 2 arguments")
	}
}

func TestAskCmd_RequiresArgs(t *testing.T) {
	// Test that ask requires at least one argument
	err := askCmd.Args(askCmd, []string{})
	if err == nil {
		t.Error("ask should require at least 1 argument")
	}

	// Test that ask accepts arguments
	err = askCmd.Args(askCmd, []string{"How do I list files?"})
	if err != nil {
		t.Errorf("ask should accept arguments, got error: %v", err)
	}
}

func TestInitCmd_ValidArgs(t *testing.T) {
	// Test valid shells
	validShells := []string{"zsh", "bash", "fish"}
	for _, shell := range validShells {
		err := initCmd.Args(initCmd, []string{shell})
		if err != nil {
			t.Errorf("init should accept %q, got error: %v", shell, err)
		}
	}

	// Test that init requires exactly 1 argument
	err := initCmd.Args(initCmd, []string{})
	if err == nil {
		t.Error("init should require exactly 1 argument")
	}

	err = initCmd.Args(initCmd, []string{"zsh", "bash"})
	if err == nil {
		t.Error("init should reject more than 1 argument")
	}
}

func TestInitCmd_InvalidShell(t *testing.T) {
	err := runInit(initCmd, []string{"powershell"})
	if err == nil {
		t.Error("init powershell should fail")
	}
}

func TestExtractCmd_WithExtractPackage(t *testing.T) {
	// Test extract logic directly using the extract package
	tests := []struct {
		name           string
		input          string
		wantSuggestion string
	}{
		{
			name:           "backticks",
			input:          "Run `npm install express` to install",
			wantSuggestion: "npm install express",
		},
		{
			name:           "install command",
			input:          "pip install requests",
			wantSuggestion: "pip install requests",
		},
		{
			name:           "prefixed command",
			input:          "Run: npm start",
			wantSuggestion: "npm start",
		},
		{
			name:           "dollar prefix",
			input:          "$ npm run dev",
			wantSuggestion: "npm run dev",
		},
		{
			name:           "no match",
			input:          "Just some regular text",
			wantSuggestion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extract.Suggestion(tt.input)
			if got != tt.wantSuggestion {
				t.Errorf("extract.Suggestion() = %q, want %q", got, tt.wantSuggestion)
			}
		})
	}
}

func TestCacheIntegration(t *testing.T) {
	// Set up temp cache dir
	tmpDir := t.TempDir()
	os.Setenv("AI_TERMINAL_CACHE", tmpDir)
	defer os.Unsetenv("AI_TERMINAL_CACHE")

	// Test suggestion workflow
	testSuggestion := "npm install express"

	err := cache.WriteSuggestion(testSuggestion)
	if err != nil {
		t.Fatalf("WriteSuggestion failed: %v", err)
	}

	got, err := cache.ReadSuggestion()
	if err != nil {
		t.Fatalf("ReadSuggestion failed: %v", err)
	}

	if got != testSuggestion {
		t.Errorf("ReadSuggestion() = %q, want %q", got, testSuggestion)
	}

	// Test last output workflow
	testOutput := "npm ERR! code ENOENT\nnpm ERR! missing script"

	err = cache.WriteLastOutput(testOutput)
	if err != nil {
		t.Fatalf("WriteLastOutput failed: %v", err)
	}

	gotOutput, err := cache.ReadLastOutput(50)
	if err != nil {
		t.Fatalf("ReadLastOutput failed: %v", err)
	}

	if gotOutput != testOutput {
		t.Errorf("ReadLastOutput() = %q, want %q", gotOutput, testOutput)
	}
}

func TestDiagnoseCmd_ReadsCache(t *testing.T) {
	// Set up temp cache dir
	tmpDir := t.TempDir()
	os.Setenv("AI_TERMINAL_CACHE", tmpDir)
	defer os.Unsetenv("AI_TERMINAL_CACHE")

	// Write mock error output
	mockOutput := "npm ERR! code ENOENT\nnpm ERR! missing script: build"
	err := cache.WriteLastOutput(mockOutput)
	if err != nil {
		t.Fatalf("Failed to write mock output: %v", err)
	}

	// Verify cache file exists
	lastOutputFile := filepath.Join(tmpDir, "last_output")
	data, err := os.ReadFile(lastOutputFile)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	if string(data) != mockOutput {
		t.Errorf("Cache file content = %q, want %q", string(data), mockOutput)
	}
}

func TestExtractCmd_CachesOutput(t *testing.T) {
	// Set up temp cache dir
	tmpDir := t.TempDir()
	os.Setenv("AI_TERMINAL_CACHE", tmpDir)
	defer os.Unsetenv("AI_TERMINAL_CACHE")

	// Test the cache writing directly
	testInput := "Run `npm install express` to install"

	err := cache.WriteLastOutput(testInput)
	if err != nil {
		t.Fatalf("WriteLastOutput failed: %v", err)
	}

	suggestion := extract.Suggestion(testInput)
	err = cache.WriteSuggestion(suggestion)
	if err != nil {
		t.Fatalf("WriteSuggestion failed: %v", err)
	}

	// Verify last output was cached
	gotOutput, err := cache.ReadLastOutput(50)
	if err != nil {
		t.Fatalf("ReadLastOutput failed: %v", err)
	}
	if gotOutput != testInput {
		t.Errorf("Cached output = %q, want %q", gotOutput, testInput)
	}

	// Verify suggestion was cached
	gotSuggestion, err := cache.ReadSuggestion()
	if err != nil {
		t.Fatalf("ReadSuggestion failed: %v", err)
	}
	if gotSuggestion != "npm install express" {
		t.Errorf("Cached suggestion = %q, want %q", gotSuggestion, "npm install express")
	}
}
