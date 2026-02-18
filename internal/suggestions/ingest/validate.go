// Package ingest provides NDJSON parsing and validation for command events.
// It handles event ingestion from shell hooks via clai-shim.
package ingest

import (
	"errors"
	"fmt"

	"github.com/runger/clai/internal/suggestions/event"
)

// Validation errors returned by ValidateEvent.
var (
	// ErrInvalidVersion indicates the event version is not supported.
	ErrInvalidVersion = errors.New("invalid event version")

	// ErrMissingType indicates the event type field is missing or empty.
	ErrMissingType = errors.New("missing required field: type")

	// ErrMissingTimestamp indicates the timestamp field is missing or zero.
	ErrMissingTimestamp = errors.New("missing required field: ts")

	// ErrMissingSessionID indicates the session_id field is missing or empty.
	ErrMissingSessionID = errors.New("missing required field: session_id")

	// ErrMissingShell indicates the shell field is missing or empty.
	ErrMissingShell = errors.New("missing required field: shell")

	// ErrInvalidShell indicates the shell field contains an unsupported value.
	ErrInvalidShell = errors.New("invalid shell: must be bash, zsh, or fish")

	// ErrMissingCwd indicates the cwd field is missing or empty.
	ErrMissingCwd = errors.New("missing required field: cwd")

	// ErrMissingCmdRaw indicates the cmd_raw field is missing or empty.
	ErrMissingCmdRaw = errors.New("missing required field: cmd_raw")
)

// ValidateEvent validates that all required fields are present and valid.
// It returns nil if the event is valid, or an error describing the validation failure.
//
// Required fields per spec Section 15.1:
//   - v: must be 1 (current event version)
//   - type: must be non-empty (e.g., "command_end")
//   - ts: must be non-zero (Unix milliseconds)
//   - session_id: must be non-empty
//   - shell: must be bash, zsh, or fish
//   - cwd: must be non-empty
//   - cmd_raw: must be non-empty
//   - exit_code: always present (zero is valid)
//
// Optional fields (not validated for presence):
//   - duration_ms: command duration in milliseconds
//   - ephemeral: whether the event should be persisted
func ValidateEvent(ev *event.CommandEvent) error {
	if ev == nil {
		return errors.New("event is nil")
	}

	// Validate version (must be exactly 1)
	if ev.Version != event.EventVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrInvalidVersion, ev.Version, event.EventVersion)
	}

	// Validate required string fields
	if ev.Type == "" {
		return ErrMissingType
	}

	if ev.Ts == 0 {
		return ErrMissingTimestamp
	}

	if ev.SessionID == "" {
		return ErrMissingSessionID
	}

	if ev.Shell == "" {
		return ErrMissingShell
	}

	// Validate shell enum
	if !event.ValidShell(string(ev.Shell)) {
		return fmt.Errorf("%w: got %q", ErrInvalidShell, ev.Shell)
	}

	if ev.Cwd == "" {
		return ErrMissingCwd
	}

	if ev.CmdRaw == "" {
		return ErrMissingCmdRaw
	}

	// exit_code is an int and zero is a valid value, so no validation needed

	return nil
}
