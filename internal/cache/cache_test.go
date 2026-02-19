package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDir(t *testing.T) {
	t.Run("uses CLAI_CACHE env var when set", func(t *testing.T) {
		customDir := "/custom/cache/dir"
		os.Setenv("CLAI_CACHE", customDir)
		defer os.Unsetenv("CLAI_CACHE")

		got := Dir()
		if got != customDir {
			t.Errorf("Dir() = %q, want %q", got, customDir)
		}
	})

	t.Run("uses default when env var not set", func(t *testing.T) {
		os.Unsetenv("CLAI_CACHE")

		got := Dir()
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".cache", "clai")

		if got != want {
			t.Errorf("Dir() = %q, want %q", got, want)
		}
	})
}

func TestEnsureDir(t *testing.T) {
	// Use a temp directory for testing
	tmpDir := t.TempDir()
	testCacheDir := filepath.Join(tmpDir, "test-cache")
	os.Setenv("CLAI_CACHE", testCacheDir)
	defer os.Unsetenv("CLAI_CACHE")

	err := EnsureDir()
	if err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	// Verify directory was created
	info, err := os.Stat(testCacheDir)
	if err != nil {
		t.Fatalf("Directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected directory, got file")
	}

	// Should be idempotent
	err = EnsureDir()
	if err != nil {
		t.Errorf("Second EnsureDir() error = %v", err)
	}
}

func TestSuggestionFile(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	got := SuggestionFile()
	want := filepath.Join(tmpDir, "suggestion")

	if got != want {
		t.Errorf("SuggestionFile() = %q, want %q", got, want)
	}
}

func TestLastOutputFile(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	got := LastOutputFile()
	want := filepath.Join(tmpDir, "last_output")

	if got != want {
		t.Errorf("LastOutputFile() = %q, want %q", got, want)
	}
}

func TestWriteAndReadSuggestion(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	tests := []struct {
		name       string
		suggestion string
	}{
		{"simple command", "npm install express"},
		{"command with flags", "pip install --upgrade requests"},
		{"empty suggestion", ""},
		{"command with special chars", "grep -r 'pattern' ./src"},
		{"multiword command", "docker run -it --rm ubuntu:latest bash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WriteSuggestion(tt.suggestion)
			if err != nil {
				t.Fatalf("WriteSuggestion() error = %v", err)
			}

			got, err := ReadSuggestion()
			if err != nil {
				t.Fatalf("ReadSuggestion() error = %v", err)
			}

			if got != tt.suggestion {
				t.Errorf("ReadSuggestion() = %q, want %q", got, tt.suggestion)
			}
		})
	}
}

func TestReadSuggestion_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	// Don't create the file, just try to read
	got, err := ReadSuggestion()
	if err != nil {
		t.Errorf("ReadSuggestion() error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("ReadSuggestion() = %q, want empty string", got)
	}
}

func TestClearSuggestion(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	// Write a suggestion first
	err := WriteSuggestion("npm install")
	if err != nil {
		t.Fatalf("WriteSuggestion() error = %v", err)
	}

	// Clear it
	err = ClearSuggestion()
	if err != nil {
		t.Fatalf("ClearSuggestion() error = %v", err)
	}

	// Verify it's empty
	got, err := ReadSuggestion()
	if err != nil {
		t.Fatalf("ReadSuggestion() error = %v", err)
	}
	if got != "" {
		t.Errorf("After ClearSuggestion(), ReadSuggestion() = %q, want empty", got)
	}
}

func TestWriteAndReadLastOutput(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	tests := []struct {
		name     string
		output   string
		want     string
		maxLines int
	}{
		{
			name:     "simple output",
			output:   "Hello World",
			want:     "Hello World",
			maxLines: 50,
		},
		{
			name:     "multiline output within limit",
			output:   "line1\nline2\nline3",
			want:     "line1\nline2\nline3",
			maxLines: 50,
		},
		{
			name:     "multiline output exceeds limit",
			output:   "line1\nline2\nline3\nline4\nline5",
			want:     "line3\nline4\nline5",
			maxLines: 3,
		},
		{
			name:     "empty output",
			output:   "",
			want:     "",
			maxLines: 50,
		},
		{
			name:     "output with error messages",
			output:   "npm ERR! code ENOENT\nnpm ERR! syscall open\nnpm ERR! path /package.json",
			want:     "npm ERR! code ENOENT\nnpm ERR! syscall open\nnpm ERR! path /package.json",
			maxLines: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WriteLastOutput(tt.output)
			if err != nil {
				t.Fatalf("WriteLastOutput() error = %v", err)
			}

			got, err := ReadLastOutput(tt.maxLines)
			if err != nil {
				t.Fatalf("ReadLastOutput() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("ReadLastOutput(%d) = %q, want %q", tt.maxLines, got, tt.want)
			}
		})
	}
}

func TestReadLastOutput_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	got, err := ReadLastOutput(50)
	if err != nil {
		t.Errorf("ReadLastOutput() error = %v, want nil", err)
	}
	if got != "(no output captured)" {
		t.Errorf("ReadLastOutput() = %q, want %q", got, "(no output captured)")
	}
}

func TestReadLastOutput_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	// Create a file with 100 lines
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, "line"+string(rune('0'+i%10)))
	}
	output := strings.Join(lines, "\n")

	err := WriteLastOutput(output)
	if err != nil {
		t.Fatalf("WriteLastOutput() error = %v", err)
	}

	// Read only last 10 lines
	got, err := ReadLastOutput(10)
	if err != nil {
		t.Fatalf("ReadLastOutput() error = %v", err)
	}

	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 10 {
		t.Errorf("ReadLastOutput(10) returned %d lines, want 10", len(gotLines))
	}
}

func TestSuggestion_WhitespaceHandling(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAI_CACHE", tmpDir)
	defer os.Unsetenv("CLAI_CACHE")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"leading whitespace", "  npm install", "npm install"},
		{"trailing whitespace", "npm install  ", "npm install"},
		{"both whitespace", "  npm install  ", "npm install"},
		{"newline", "npm install\n", "npm install"},
		{"multiple newlines", "npm install\n\n", "npm install"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WriteSuggestion(tt.input)
			if err != nil {
				t.Fatalf("WriteSuggestion() error = %v", err)
			}

			got, err := ReadSuggestion()
			if err != nil {
				t.Fatalf("ReadSuggestion() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("ReadSuggestion() = %q, want %q", got, tt.want)
			}
		})
	}
}
