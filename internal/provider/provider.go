// Package provider implements AI provider adapters for text-to-command,
// next step prediction, and error diagnosis.
package provider

import (
	"context"
	"time"
)

// DefaultTimeout is the default timeout for AI provider calls
const DefaultTimeout = 10 * time.Second

// Source constants for suggestion origins
const (
	SourceHistory = "history" // Historical command
	SourceAI      = "ai"      // AI-generated suggestion
)

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
	ProviderName string
	Suggestions  []Suggestion
	LatencyMs    int64
}

// NextStepRequest is the request for next step prediction
type NextStepRequest struct {
	SessionID    string
	LastCommand  string
	CWD          string
	OS           string
	Shell        string
	RecentCmds   []CommandContext
	LastExitCode int
}

// NextStepResponse is the response from next step prediction
type NextStepResponse struct {
	ProviderName string
	Suggestions  []Suggestion
	LatencyMs    int64
}

// DiagnoseRequest is the request for error diagnosis
type DiagnoseRequest struct {
	SessionID  string
	Command    string
	CWD        string
	OS         string
	Shell      string
	StdErr     string
	RecentCmds []CommandContext
	ExitCode   int
}

// DiagnoseResponse is the response from error diagnosis
type DiagnoseResponse struct {
	Explanation  string
	ProviderName string
	Fixes        []Suggestion
	LatencyMs    int64
}

// Suggestion represents a command suggestion
type Suggestion struct {
	Text        string  // The suggested command
	Description string  // Optional description
	Source      string  // SourceHistory or SourceAI
	Risk        string  // "safe", "destructive"
	Score       float64 // Ranking score (0.0 to 1.0)
}
