// Package claude provides integration with the Claude CLI for AI-powered queries.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
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

// jsonResult is the structure returned by `claude --print --output-format json`.
type jsonResult struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// QueryWithContext sends a prompt to Claude CLI with context support for cancellation.
// Uses --output-format json to reliably capture the response text. Plain --print mode
// can return empty stdout in some Claude Code versions even when the model produces
// output (text goes into assistant messages but not into the result stream).
func QueryWithContext(ctx context.Context, prompt string) (string, error) {
	// Check if claude CLI is available
	if _, err := exec.LookPath("claude"); err != nil {
		return "", fmt.Errorf("'claude' CLI not found. Install Claude Code: https://docs.anthropic.com/en/docs/claude-code")
	}

	cmd := exec.CommandContext(ctx, "claude", "--print", "--output-format", "json", "--max-turns", "1")
	cmd.Env = FilterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.Canceled {
			return "", fmt.Errorf("interrupted")
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("claude error: %s", stderr.String())
		}
		return "", fmt.Errorf("failed to get response from Claude: %w", err)
	}

	raw := stdout.String()

	// Parse JSON envelope to extract the result text.
	var jr jsonResult
	if err := json.Unmarshal([]byte(raw), &jr); err != nil {
		// If JSON parsing fails, fall back to treating raw output as the response.
		result := strings.TrimSpace(raw)
		if result == "" {
			return "", fmt.Errorf("claude returned unparseable empty response")
		}
		return result, nil
	}

	if jr.IsError {
		return "", fmt.Errorf("claude returned error: %s", jr.Result)
	}

	result := strings.TrimSpace(jr.Result)
	if result == "" {
		return "", fmt.Errorf("claude returned empty result (model produced no text output)")
	}

	return result, nil
}

// ExtractJSONResult parses the JSON envelope from `claude --print --output-format json`
// and returns the result text. Falls back to treating raw input as plain text.
func ExtractJSONResult(raw string) (string, error) {
	var jr jsonResult
	if err := json.Unmarshal([]byte(raw), &jr); err != nil {
		result := strings.TrimSpace(raw)
		if result == "" {
			return "", fmt.Errorf("claude returned unparseable empty response")
		}
		return result, nil
	}

	if jr.IsError {
		return "", fmt.Errorf("claude returned error: %s", jr.Result)
	}

	result := strings.TrimSpace(jr.Result)
	if result == "" {
		return "", fmt.Errorf("claude returned empty result (model produced no text output)")
	}

	return result, nil
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
