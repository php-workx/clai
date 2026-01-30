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

// AnthropicProvider implements the Provider interface for Claude/Anthropic
type AnthropicProvider struct {
	sanitizer *sanitize.Sanitizer
	cliPath   string
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

// Available checks if Claude CLI is available or API key is set
func (p *AnthropicProvider) Available() bool {
	// First check for Claude CLI
	if path, err := exec.LookPath("claude"); err == nil {
		p.cliPath = path
		return true
	}

	// Fallback: check for API key (for future direct API support)
	return os.Getenv("ANTHROPIC_API_KEY") != ""
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

// query sends a prompt to Claude CLI
func (p *AnthropicProvider) query(ctx context.Context, prompt string) (string, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	// Build command arguments
	args := []string{"--print"}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
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
	suggestions := make([]Suggestion, 0, 3)

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

		// Remove common command prefixes like "1.", "- ", "$ "
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
			Source: SourceAI,
			Score:  1.0 - float64(len(suggestions))*0.1, // Decreasing score by order
			Risk:   risk,
		})

		// Limit to 3 suggestions
		if len(suggestions) >= 3 {
			break
		}
	}

	return suggestions
}

// parseDiagnoseResponse parses a diagnosis response
func (p *AnthropicProvider) parseDiagnoseResponse(response string) (string, []Suggestion) {
	var explanation strings.Builder
	var fixes []Suggestion

	lines := strings.Split(response, "\n")
	inFixes := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this looks like a command (starts with $ or is numbered after fixes section)
		if strings.HasPrefix(line, "$ ") {
			inFixes = true
			cmd := strings.TrimPrefix(line, "$ ")
			risk := "safe"
			if sanitize.IsDestructive(cmd) {
				risk = "destructive"
			}
			fixes = append(fixes, Suggestion{
				Text:   cmd,
				Source: SourceAI,
				Score:  1.0 - float64(len(fixes))*0.1,
				Risk:   risk,
			})
			continue
		}

		// Check for numbered/bulleted fix commands - these also start the fixes section
		cleaned := cleanCommandPrefix(line)
		if startsFixSection(line) && cleaned != "" && !strings.HasPrefix(line, "#") {
			inFixes = true
			risk := "safe"
			if sanitize.IsDestructive(cleaned) {
				risk = "destructive"
			}
			fixes = append(fixes, Suggestion{
				Text:   cleaned,
				Source: SourceAI,
				Score:  max(0.1, 1.0-float64(len(fixes))*0.1),
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

// startsFixSection returns true if the line looks like the start of a numbered or bulleted list
func startsFixSection(line string) bool {
	if len(line) < 2 {
		return false
	}
	// Check for numbered patterns: "1.", "1)", "2.", "2)", etc.
	if line[0] >= '1' && line[0] <= '9' {
		if line[1] == '.' || line[1] == ')' {
			return true
		}
		if len(line) >= 3 && (line[2] == '.' || line[2] == ')') {
			return true
		}
	}
	// Check for bullet patterns: "- ", "* "
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return true
	}
	return false
}

// cleanCommandPrefix removes common prefixes from command lines
func cleanCommandPrefix(line string) string {
	// Remove numbered prefixes like "1. ", "2) "
	if len(line) >= 3 && line[0] >= '1' && line[0] <= '9' {
		if line[1] == '.' || line[1] == ')' {
			line = strings.TrimSpace(line[2:])
		} else if len(line) >= 4 && line[2] == '.' || line[2] == ')' {
			line = strings.TrimSpace(line[3:])
		}
	}

	// Remove bullet prefixes
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "* ")
	line = strings.TrimPrefix(line, "$ ")

	// Remove markdown code backticks
	line = strings.Trim(line, "`")

	return strings.TrimSpace(line)
}
