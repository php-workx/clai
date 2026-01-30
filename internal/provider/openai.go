package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/runger/clai/internal/sanitize"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	sanitizer  *sanitize.Sanitizer
	cliPath    string
	model      string
	cliChecked bool
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{
		sanitizer: sanitize.NewSanitizer(),
		model:     "gpt-4o", // Default to GPT-4o
	}
}

// NewOpenAIProviderWithModel creates an OpenAI provider with a specific model
func NewOpenAIProviderWithModel(model string) *OpenAIProvider {
	return &OpenAIProvider{
		sanitizer: sanitize.NewSanitizer(),
		model:     model,
	}
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Available checks if OpenAI CLI is available or API key is set
func (p *OpenAIProvider) Available() bool {
	// Check for OpenAI CLI (if available)
	if path, err := exec.LookPath("openai"); err == nil {
		p.cliPath = path
		return true
	}

	// Fallback: check for API key
	return os.Getenv("OPENAI_API_KEY") != ""
}

// TextToCommand converts natural language to shell commands
func (p *OpenAIProvider) TextToCommand(ctx context.Context, req *TextToCommandRequest) (*TextToCommandResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize the prompt
	sanitizedPrompt := p.sanitizer.Sanitize(req.Prompt)
	fullPrompt := builder.BuildTextToCommandPrompt(sanitizedPrompt)

	// Query OpenAI
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
func (p *OpenAIProvider) NextStep(ctx context.Context, req *NextStepRequest) (*NextStepResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize the last command
	sanitizedCmd := p.sanitizer.Sanitize(req.LastCommand)
	fullPrompt := builder.BuildNextStepPrompt(sanitizedCmd, req.LastExitCode)

	// Query OpenAI
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
func (p *OpenAIProvider) Diagnose(ctx context.Context, req *DiagnoseRequest) (*DiagnoseResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize inputs
	sanitizedCmd := p.sanitizer.Sanitize(req.Command)
	sanitizedStderr := p.sanitizer.Sanitize(req.StdErr)
	fullPrompt := builder.BuildDiagnosePrompt(sanitizedCmd, req.ExitCode, sanitizedStderr)

	// Query OpenAI
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

// query sends a prompt to OpenAI
func (p *OpenAIProvider) query(ctx context.Context, prompt string) (string, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	// Lazily resolve CLI path if not already checked
	if !p.cliChecked {
		if path, err := exec.LookPath("openai"); err == nil {
			p.cliPath = path
		}
		p.cliChecked = true
	}

	// Check if CLI is available
	if p.cliPath != "" {
		return p.queryViaCLI(ctx, prompt)
	}

	// Check for API key - if present, we could implement direct API calls
	// For now, return an error indicating CLI-only support
	if os.Getenv("OPENAI_API_KEY") != "" {
		return "", fmt.Errorf("openai direct API not yet implemented; install OpenAI CLI: pip install openai && openai migrate")
	}

	return "", fmt.Errorf("openai provider not available: install OpenAI CLI (pip install openai) or set OPENAI_API_KEY")
}

// queryViaCLI uses the OpenAI CLI to make requests
func (p *OpenAIProvider) queryViaCLI(ctx context.Context, prompt string) (string, error) {
	// OpenAI CLI command format
	args := []string{"api", "chat.completions.create",
		"-m", p.model,
		"-g", "user", prompt,
	}

	cmd := exec.CommandContext(ctx, "openai", args...)

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
			return "", fmt.Errorf("openai error: %s", strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("failed to get response from OpenAI: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// parseCommandResponse parses a response into suggestions
func (p *OpenAIProvider) parseCommandResponse(response string) []Suggestion {
	return ParseCommandResponse(response)
}

// parseDiagnoseResponse parses a diagnosis response
func (p *OpenAIProvider) parseDiagnoseResponse(response string) (string, []Suggestion) {
	return ParseDiagnoseResponse(response)
}
