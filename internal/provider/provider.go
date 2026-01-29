// Package provider implements AI provider adapters for text-to-command,
// next step prediction, and error diagnosis.
package provider

import (
	"context"
	"time"
)

// DefaultTimeout is the default timeout for AI provider calls
const DefaultTimeout = 10 * time.Second

// Provider defines the interface for AI providers
type Provider interface {
	// Name returns the provider name (e.g., "anthropic", "openai", "google")
	Name() string

	// Available checks if the provider is available (CLI found or API key present)
	Available() bool

	// TextToCommand converts natural language to shell commands
	TextToCommand(ctx context.Context, req *TextToCommandRequest) (*TextToCommandResponse, error)

	// NextStep predicts the next command based on the last command and exit code
	NextStep(ctx context.Context, req *NextStepRequest) (*NextStepResponse, error)

	// Diagnose analyzes a failed command and suggests fixes
	Diagnose(ctx context.Context, req *DiagnoseRequest) (*DiagnoseResponse, error)
}

// CommandContext represents context about a previously executed command
type CommandContext struct {
	Command  string
	ExitCode int
}

// TextToCommandRequest is the request for text-to-command conversion
type TextToCommandRequest struct {
	Prompt     string
	CWD        string
	OS         string
	Shell      string
	RecentCmds []CommandContext
}

// TextToCommandResponse is the response from text-to-command conversion
type TextToCommandResponse struct {
	Suggestions  []Suggestion
	ProviderName string
	LatencyMs    int64
}

// NextStepRequest is the request for next step prediction
type NextStepRequest struct {
	SessionID    string
	LastCommand  string
	LastExitCode int
	CWD          string
	OS           string
	Shell        string
	RecentCmds   []CommandContext
}

// NextStepResponse is the response from next step prediction
type NextStepResponse struct {
	Suggestions  []Suggestion
	ProviderName string
	LatencyMs    int64
}

// DiagnoseRequest is the request for error diagnosis
type DiagnoseRequest struct {
	SessionID  string
	Command    string
	ExitCode   int
	CWD        string
	OS         string
	Shell      string
	StdErr     string
	RecentCmds []CommandContext
}

// DiagnoseResponse is the response from error diagnosis
type DiagnoseResponse struct {
	Explanation  string
	Fixes        []Suggestion
	ProviderName string
	LatencyMs    int64
}

// Suggestion represents a command suggestion
type Suggestion struct {
	Text        string  // The suggested command
	Description string  // Optional description
	Source      string  // "history", "ai"
	Score       float64 // Ranking score (0.0 to 1.0)
	Risk        string  // "safe", "destructive"
}
