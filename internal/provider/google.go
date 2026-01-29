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

// GoogleProvider implements the Provider interface for Google Gemini
type GoogleProvider struct {
	sanitizer *sanitize.Sanitizer
	cliPath   string
	model     string
}

// NewGoogleProvider creates a new Google/Gemini provider
func NewGoogleProvider() *GoogleProvider {
	return &GoogleProvider{
		sanitizer: sanitize.NewSanitizer(),
		model:     "gemini-pro", // Default model
	}
}

// NewGoogleProviderWithModel creates a Google provider with a specific model
func NewGoogleProviderWithModel(model string) *GoogleProvider {
	return &GoogleProvider{
		sanitizer: sanitize.NewSanitizer(),
		model:     model,
	}
}

// Name returns the provider name
func (p *GoogleProvider) Name() string {
	return "google"
}

// Available checks if Gemini CLI is available or API key is set
func (p *GoogleProvider) Available() bool {
	// Check for Gemini CLI
	if path, err := exec.LookPath("gemini"); err == nil {
		p.cliPath = path
		return true
	}

	// Also check for gcloud with generative AI capabilities
	if path, err := exec.LookPath("gcloud"); err == nil {
		// Verify gcloud is configured with a project
		cmd := exec.Command(path, "config", "get-value", "project")
		if output, err := cmd.Output(); err == nil && len(output) > 0 {
			p.cliPath = path
			return true
		}
	}

	// Fallback: check for API key
	return os.Getenv("GOOGLE_API_KEY") != "" || os.Getenv("GEMINI_API_KEY") != ""
}

// TextToCommand converts natural language to shell commands
func (p *GoogleProvider) TextToCommand(ctx context.Context, req *TextToCommandRequest) (*TextToCommandResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize the prompt
	sanitizedPrompt := p.sanitizer.Sanitize(req.Prompt)
	fullPrompt := builder.BuildTextToCommandPrompt(sanitizedPrompt)

	// Query Gemini
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
func (p *GoogleProvider) NextStep(ctx context.Context, req *NextStepRequest) (*NextStepResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize the last command
	sanitizedCmd := p.sanitizer.Sanitize(req.LastCommand)
	fullPrompt := builder.BuildNextStepPrompt(sanitizedCmd, req.LastExitCode)

	// Query Gemini
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
func (p *GoogleProvider) Diagnose(ctx context.Context, req *DiagnoseRequest) (*DiagnoseResponse, error) {
	start := time.Now()

	// Build context
	builder := NewContextBuilder(req.OS, req.Shell, req.CWD, TrimRecentCommands(req.RecentCmds))

	// Sanitize inputs
	sanitizedCmd := p.sanitizer.Sanitize(req.Command)
	sanitizedStderr := p.sanitizer.Sanitize(req.StdErr)
	fullPrompt := builder.BuildDiagnosePrompt(sanitizedCmd, req.ExitCode, sanitizedStderr)

	// Query Gemini
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

// query sends a prompt to Gemini
func (p *GoogleProvider) query(ctx context.Context, prompt string) (string, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	// Try CLI first
	if p.cliPath != "" {
		return p.queryViaCLI(ctx, prompt)
	}

	// Check for API key - if present, we could implement direct API calls
	if os.Getenv("GOOGLE_API_KEY") != "" || os.Getenv("GEMINI_API_KEY") != "" {
		return "", fmt.Errorf("google direct API not yet implemented; install Gemini CLI: https://github.com/google-gemini/gemini-cli")
	}

	return "", fmt.Errorf("google provider not available: install Gemini CLI (https://github.com/google-gemini/gemini-cli) or set GOOGLE_API_KEY/GEMINI_API_KEY")
}

// queryViaCLI uses the Gemini CLI or gcloud to make requests
func (p *GoogleProvider) queryViaCLI(ctx context.Context, prompt string) (string, error) {
	var cmd *exec.Cmd

	// Check which CLI we're using
	if strings.HasSuffix(p.cliPath, "gemini") || strings.Contains(p.cliPath, "gemini") {
		// Gemini CLI format (hypothetical, adjust to actual CLI)
		cmd = exec.CommandContext(ctx, p.cliPath, "prompt", prompt)
	} else {
		// gcloud format for Vertex AI
		cmd = exec.CommandContext(ctx, p.cliPath,
			"ai", "language-models", "predict",
			"--model", p.model,
			"--prompt", prompt,
		)
	}

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
			return "", fmt.Errorf("gemini error: %s", strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("failed to get response from Gemini: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// parseCommandResponse parses a response into suggestions
func (p *GoogleProvider) parseCommandResponse(response string) []Suggestion {
	var suggestions []Suggestion

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip common non-command prefixes
		if strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "//") ||
			strings.HasPrefix(line, "Here") ||
			strings.HasPrefix(line, "The") ||
			strings.HasPrefix(line, "This") ||
			strings.HasPrefix(line, "Note:") ||
			strings.HasPrefix(line, "---") {
			continue
		}

		// Remove common command prefixes
		cleaned := cleanCommandPrefix(line)
		if cleaned == "" {
			continue
		}

		// Determine risk level
		risk := "safe"
		if sanitize.IsDestructive(cleaned) {
			risk = "destructive"
		}

		suggestions = append(suggestions, Suggestion{
			Text:   cleaned,
			Source: "ai",
			Score:  1.0 - float64(len(suggestions))*0.1,
			Risk:   risk,
		})

		if len(suggestions) >= 3 {
			break
		}
	}

	return suggestions
}

// parseDiagnoseResponse parses a diagnosis response
func (p *GoogleProvider) parseDiagnoseResponse(response string) (string, []Suggestion) {
	var explanation strings.Builder
	var fixes []Suggestion

	lines := strings.Split(response, "\n")
	inFixes := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this looks like a command
		if strings.HasPrefix(line, "$ ") {
			inFixes = true
			cmd := strings.TrimPrefix(line, "$ ")
			risk := "safe"
			if sanitize.IsDestructive(cmd) {
				risk = "destructive"
			}
			fixes = append(fixes, Suggestion{
				Text:   cmd,
				Source: "ai",
				Score:  1.0 - float64(len(fixes))*0.1,
				Risk:   risk,
			})
			continue
		}

		// Check for numbered fix commands
		cleaned := cleanCommandPrefix(line)
		if inFixes && cleaned != "" && !strings.HasPrefix(line, "#") {
			risk := "safe"
			if sanitize.IsDestructive(cleaned) {
				risk = "destructive"
			}
			fixes = append(fixes, Suggestion{
				Text:   cleaned,
				Source: "ai",
				Score:  1.0 - float64(len(fixes))*0.1,
				Risk:   risk,
			})
			continue
		}

		// Otherwise, it's part of the explanation
		if !inFixes {
			if explanation.Len() > 0 {
				explanation.WriteString(" ")
			}
			explanation.WriteString(line)
		}
	}

	return strings.TrimSpace(explanation.String()), fixes
}
