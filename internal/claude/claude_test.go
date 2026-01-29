package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockClaudeScript creates a mock claude script for testing
func mockClaudeScript(t *testing.T, response string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "claude")

	script := fmt.Sprintf(`#!/bin/sh
cat <<'EOF'
%s
EOF
exit %d
`, response, exitCode)

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}
	return dir
}

// withMockClaude runs a test function with a mock claude in PATH
func withMockClaude(t *testing.T, response string, exitCode int, fn func()) {
	t.Helper()
	mockDir := mockClaudeScript(t, response, exitCode)

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+originalPath)

	fn()
}

func TestQuery_ClaudeNotInstalled(t *testing.T) {
	// Set PATH to empty so claude won't be found
	t.Setenv("PATH", "")

	_, err := Query("test prompt")
	if err == nil {
		t.Error("Query() expected error when claude not installed, got nil")
	}

	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("Error should mention claude, got: %v", err)
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestQuery_ClaudeInstalled(t *testing.T) {
	withMockClaude(t, "TEST_OK", 0, func() {
		response, err := Query("Reply with exactly: TEST_OK")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}

		if response != "TEST_OK" {
			t.Errorf("Query() = %q, want %q", response, "TEST_OK")
		}
	})
}

// TestQuery_PromptConstruction verifies various prompt formats work correctly.
func TestQuery_PromptConstruction(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"empty prompt", ""},
		{"simple prompt", "Hello"},
		{"multiline prompt", "Line 1\nLine 2\nLine 3"},
		{"prompt with special chars", "What does `npm install` do?"},
		{"prompt with code block", "```\ncode here\n```"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMockClaude(t, "mock response", 0, func() {
				response, err := Query(tt.prompt)
				if err != nil {
					t.Errorf("Query(%q) error = %v", tt.prompt, err)
				}
				if response != "mock response" {
					t.Errorf("Query(%q) = %q, want %q", tt.prompt, response, "mock response")
				}
			})
		})
	}
}

func TestQueryWithContext_ClaudeNotInstalled(t *testing.T) {
	// Set PATH to empty so claude won't be found
	t.Setenv("PATH", "")

	ctx := context.Background()
	_, err := QueryWithContext(ctx, "test prompt")
	if err == nil {
		t.Error("QueryWithContext() expected error when claude not installed, got nil")
	}

	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("Error should mention claude, got: %v", err)
	}
}

func TestQueryWithContext_CancelledContext(t *testing.T) {
	withMockClaude(t, "should not appear", 0, func() {
		// Create an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := QueryWithContext(ctx, "test prompt")
		if err == nil {
			t.Error("QueryWithContext() expected error when context is cancelled")
		}
	})
}

func TestQueryWithContext_Timeout(t *testing.T) {
	// Create a mock that sleeps longer than the timeout
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "claude")
	script := `#!/bin/sh
sleep 10
echo "should not appear"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+originalPath)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := QueryWithContext(ctx, "test prompt")
	if err == nil {
		t.Error("QueryWithContext() expected error when context times out")
	}
}

func TestQueryWithContext_BackgroundContext(t *testing.T) {
	withMockClaude(t, "TEST_OK", 0, func() {
		// Background context should work the same as Query()
		ctx := context.Background()
		response, err := QueryWithContext(ctx, "test prompt")
		if err != nil {
			t.Fatalf("QueryWithContext() error = %v", err)
		}

		if response != "TEST_OK" {
			t.Errorf("QueryWithContext() = %q, want %q", response, "TEST_OK")
		}
	})
}

// TestQuery_ClaudeError tests error handling when claude returns an error
func TestQuery_ClaudeError(t *testing.T) {
	withMockClaude(t, "error message", 1, func() {
		_, err := Query("test prompt")
		if err == nil {
			t.Error("Query() expected error when claude exits non-zero")
		}
	})
}
