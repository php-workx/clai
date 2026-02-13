package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/sanitize"
)

// AnthropicProvider implements the Provider interface for Claude/Anthropic
type AnthropicProvider struct {
	sanitizer *sanitize.Sanitizer
	cliPath   string
	cliOnce   sync.Once
	model     string
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider() *AnthropicProvider {
	return &AnthropicProvider{
		sanitizer: sanitize.NewSanitizer(),
		model:     "", // Use default model
	}
}

// NewAnthropicProviderWithModel creates an Anthropic provider with a specific model
func NewAnthropicProviderWithModel(model string) *AnthropicProvider {
	return &AnthropicProvider{
		sanitizer: sanitize.NewSanitizer(),
		model:     model,
	}
}

// Name returns the provider name
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// Available checks if Claude CLI is available
func (p *AnthropicProvider) Available() bool {
	p.cliOnce.Do(func() {
		if path, err := exec.LookPath("claude"); err == nil {
			p.cliPath = path
		}
	})
	return p.cliPath != ""
}

// TextToCommand converts natural language to shell commands
func (p *AnthropicProvider) TextToCommand(ctx context.Context, req *TextToCommandRequest) (*TextToCommandResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize the prompt
	sanitizedPrompt := p.sanitizer.Sanitize(req.Prompt)
	fullPrompt := builder.BuildTextToCommandPrompt(sanitizedPrompt)

	// Query Claude
	response, err := p.query(ctx, fullPrompt)
	if err != nil {
		return nil, err
	}

	// Parse response into suggestions
	suggestions := p.parseCommandResponse(response)

	return &TextToCommandResponse{
		Suggestions:  suggestions,
		ProviderName: p.Name(),
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

// NextStep predicts the next command
func (p *AnthropicProvider) NextStep(ctx context.Context, req *NextStepRequest) (*NextStepResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize the last command
	sanitizedCmd := p.sanitizer.Sanitize(req.LastCommand)
	fullPrompt := builder.BuildNextStepPrompt(sanitizedCmd, req.LastExitCode)

	// Query Claude
	response, err := p.query(ctx, fullPrompt)
	if err != nil {
		return nil, err
	}

	// Parse response into suggestions
	suggestions := p.parseCommandResponse(response)

	return &NextStepResponse{
		Suggestions:  suggestions,
		ProviderName: p.Name(),
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

// Diagnose analyzes a failed command
func (p *AnthropicProvider) Diagnose(ctx context.Context, req *DiagnoseRequest) (*DiagnoseResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize inputs
	sanitizedCmd := p.sanitizer.Sanitize(req.Command)
	sanitizedStderr := p.sanitizer.Sanitize(req.StdErr)
	fullPrompt := builder.BuildDiagnosePrompt(sanitizedCmd, req.ExitCode, sanitizedStderr)

	// Query Claude
	response, err := p.query(ctx, fullPrompt)
	if err != nil {
		return nil, err
	}

	// Parse diagnosis response
	explanation, fixes := p.parseDiagnoseResponse(response)

	return &DiagnoseResponse{
		Explanation:  explanation,
		Fixes:        fixes,
		ProviderName: p.Name(),
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

// query sends a prompt to Claude CLI, using the daemon when no custom model is set.
func (p *AnthropicProvider) query(ctx context.Context, prompt string) (string, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	// When using the default model, route through the daemon for speed
	if p.model == "" {
		return claude.QueryFast(ctx, prompt)
	}

	// Custom model requested — must use direct CLI to pass --model flag
	args := []string{"--print", "--model", p.model}

	cmd := exec.CommandContext(ctx, p.cliPath, args...) //nolint:gosec // G204: cliPath is Claude CLI binary
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.Canceled {
			return "", fmt.Errorf("interrupted")
		}
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout: AI request took longer than %v", DefaultTimeout)
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("claude error: %s", strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("failed to get response from Claude: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// parseCommandResponse parses a response into suggestions
func (p *AnthropicProvider) parseCommandResponse(response string) []Suggestion {
	return ParseCommandResponse(response)
}

// parseDiagnoseResponse parses a diagnosis response
func (p *AnthropicProvider) parseDiagnoseResponse(response string) (string, []Suggestion) {
	return ParseDiagnoseResponse(response)
}

// filterEnv returns a copy of env with the named variables removed.
func filterEnv(env []string, keys ...string) []string {
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
