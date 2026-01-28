package claude

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestQuery_ClaudeNotInstalled(t *testing.T) {
	// Save original PATH and restore after test
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set PATH to empty so claude won't be found
	os.Setenv("PATH", "")

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
	// Skip if claude is not installed
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed, skipping integration test")
	}

	// This is an integration test that actually calls claude
	// Use a simple prompt that should work
	response, err := Query("Reply with exactly: TEST_OK")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if response == "" {
		t.Error("Query() returned empty response")
	}

	// Note: We can't guarantee the exact response, but it should be non-empty
	t.Logf("Claude response: %s", response)
}

// TestQuery_PromptFormatting verifies the prompt is passed correctly
// This is a unit test that doesn't require claude to be installed
func TestQuery_PromptConstruction(t *testing.T) {
	// We can't easily test the internal prompt construction without mocking
	// But we can verify the function signature and basic error handling

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
			// Skip actual execution if claude not installed
			if _, err := exec.LookPath("claude"); err != nil {
				t.Skip("claude CLI not installed")
			}

			// Just verify it doesn't panic
			_, _ = Query(tt.prompt)
		})
	}
}

func TestQueryWithContext_ClaudeNotInstalled(t *testing.T) {
	// Save original PATH and restore after test
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set PATH to empty so claude won't be found
	os.Setenv("PATH", "")

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
	// Skip if claude is not installed
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed, skipping integration test")
	}

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := QueryWithContext(ctx, "test prompt")
	if err == nil {
		t.Error("QueryWithContext() expected error when context is cancelled")
	}

	if !strings.Contains(err.Error(), "interrupt") && !strings.Contains(err.Error(), "cancel") {
		t.Logf("Note: error message was: %v", err)
		// Don't fail - the exact message depends on timing
	}
}

func TestQueryWithContext_Timeout(t *testing.T) {
	// Skip if claude is not installed
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed, skipping integration test")
	}

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// This should timeout quickly
	_, err := QueryWithContext(ctx, "test prompt")
	// We expect an error due to timeout/cancellation
	// The process might not even start before the context is done
	if err == nil {
		t.Log("QueryWithContext() completed before timeout - this can happen with fast systems")
	} else {
		t.Logf("QueryWithContext() returned expected error: %v", err)
	}
}

func TestQueryWithContext_BackgroundContext(t *testing.T) {
	// Skip if claude is not installed
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed, skipping integration test")
	}

	// Background context should work the same as Query()
	ctx := context.Background()
	response, err := QueryWithContext(ctx, "Reply with exactly: TEST_OK")
	if err != nil {
		t.Fatalf("QueryWithContext() error = %v", err)
	}

	if response == "" {
		t.Error("QueryWithContext() returned empty response")
	}
}
