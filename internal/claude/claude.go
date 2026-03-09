// Package claude provides integration with the Claude CLI for AI-powered queries.
package claude

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Query sends a prompt to Claude CLI and returns the response
// This is a convenience wrapper around QueryWithContext using context.Background()
func Query(prompt string) (string, error) {
	return QueryWithContext(context.Background(), prompt)
}

// QueryWithContext sends a prompt to Claude CLI with context support for cancellation
// Use this when you need to support Ctrl+C interruption
func QueryWithContext(ctx context.Context, prompt string) (string, error) {
	// Check if claude CLI is available
	if _, err := exec.LookPath("claude"); err != nil {
		return "", fmt.Errorf("'claude' CLI not found. Install Claude Code: https://docs.anthropic.com/en/docs/claude-code")
	}

	cmd := exec.CommandContext(ctx, "claude", "--print")
	cmd.Env = FilterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if context was cancelled (user pressed Ctrl+C)
		if ctx.Err() == context.Canceled {
			return "", fmt.Errorf("interrupted")
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("claude error: %s", stderr.String())
		}
		return "", fmt.Errorf("failed to get response from Claude: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// FilterEnv returns a copy of env with the named variables removed.
func FilterEnv(env []string, keys ...string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		skip := false
		for _, key := range keys {
			if strings.HasPrefix(e, key+"=") {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
