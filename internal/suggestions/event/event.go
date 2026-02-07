// Package event defines the command event types for the suggestions engine.
// These events are ingested from shell hooks via clai-hook and processed by
// the daemon for command suggestions.
package event

// Shell represents the supported shell types.
type Shell string

const (
	ShellBash Shell = "bash"
	ShellZsh  Shell = "zsh"
	ShellFish Shell = "fish"
)

// ValidShell returns true if s is a valid shell type.
func ValidShell(s string) bool {
	switch Shell(s) {
	case ShellBash, ShellZsh, ShellFish:
		return true
	default:
		return false
	}
}

// CommandEvent represents a command execution event captured from shell hooks.
// This is serialized to NDJSON and sent to the daemon for ingestion.
//
// See spec Section 15.1 for the JSON format.
type CommandEvent struct {
	// Version is the event format version (currently 1).
	Version int `json:"v"`

	// Type is the event type (e.g., "command_end").
	Type string `json:"type"`

	// Ts is the timestamp in Unix milliseconds.
	Ts int64 `json:"ts"`

	// SessionID identifies the shell session.
	SessionID string `json:"session_id"`

	// Shell is the shell type (bash, zsh, fish).
	Shell Shell `json:"shell"`

	// Cwd is the current working directory.
	Cwd string `json:"cwd"`

	// CmdRaw is the raw command string as entered by the user.
	CmdRaw string `json:"cmd_raw"`

	// RepoKey is the git repository identifier (empty if not in a repo).
	RepoKey string `json:"repo_key,omitempty"`

	// Branch is the current git branch (empty if not in a repo).
	Branch string `json:"branch,omitempty"`

	// ExitCode is the exit code of the command.
	ExitCode int `json:"exit_code"`

	// DurationMs is the command duration in milliseconds (optional).
	DurationMs *int64 `json:"duration_ms,omitempty"`

	// Ephemeral indicates if this is an incognito/ephemeral event.
	// Ephemeral events are used for in-memory session context only
	// and are never persisted to disk.
	Ephemeral bool `json:"ephemeral"`
}

// EventType constants for the Type field.
const (
	EventTypeCommandEnd = "command_end"
)

// EventVersion is the current event format version.
const EventVersion = 1

// NewCommandEvent creates a new CommandEvent with default values.
func NewCommandEvent() *CommandEvent {
	return &CommandEvent{
		Version: EventVersion,
		Type:    EventTypeCommandEnd,
	}
}
