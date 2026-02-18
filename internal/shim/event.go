package shim

import (
	"encoding/json"
	"fmt"
)

// ShimEvent represents a JSON event received on stdin in persistent mode.
// The Type field determines which gRPC call to dispatch to.
type ShimEvent struct {
	// Type is the event type, determining dispatch.
	// Supported values: session_start, session_end, command_start, command_end.
	Type string `json:"type"`

	// SessionID identifies the shell session.
	SessionID string `json:"session_id"`

	// CommandID identifies the command within the session (command_start, command_end).
	CommandID string `json:"command_id,omitempty"`

	// Cwd is the current working directory.
	Cwd string `json:"cwd,omitempty"`

	// Shell is the shell type (session_start).
	Shell string `json:"shell,omitempty"`

	// Command is the raw command string (command_start).
	Command string `json:"command,omitempty"`

	// ExitCode is the command exit code (command_end).
	ExitCode int `json:"exit_code,omitempty"`

	// DurationMs is the command duration in milliseconds (command_end).
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Git context (command_start).
	GitBranch     string `json:"git_branch,omitempty"`
	GitRepoName   string `json:"git_repo_name,omitempty"`
	GitRepoRoot   string `json:"git_repo_root,omitempty"`
	PrevCommandID string `json:"prev_command_id,omitempty"`
}

// Event type constants.
const (
	EventSessionStart = "session_start"
	EventSessionEnd   = "session_end"
	EventCommandStart = "command_start"
	EventCommandEnd   = "command_end"
)

// ParseShimEvent parses a JSON line into a ShimEvent and validates
// that the type field is present and recognized.
func ParseShimEvent(data []byte) (*ShimEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty event data")
	}

	var ev ShimEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	switch ev.Type {
	case EventSessionStart, EventSessionEnd, EventCommandStart, EventCommandEnd:
		// valid
	case "":
		return nil, fmt.Errorf("missing required field: type")
	default:
		return nil, fmt.Errorf("unknown event type: %q", ev.Type)
	}

	if ev.SessionID == "" {
		return nil, fmt.Errorf("missing required field: session_id")
	}

	return &ev, nil
}
