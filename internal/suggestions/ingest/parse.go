package ingest

import (
	"encoding/json"
	"fmt"

	"github.com/runger/clai/internal/suggestions/event"
)

// ParseEvent parses a JSON byte slice into a CommandEvent and validates it.
// It returns the parsed event or an error if parsing or validation fails.
//
// This function handles NDJSON lines from shell hooks via clai-hook.
// The expected JSON format is defined in spec Section 15.1:
//
//	{
//	  "v": 1,
//	  "type": "command_end",
//	  "ts": 1730000000123,
//	  "session_id": "uuid",
//	  "shell": "zsh",
//	  "cwd": "/path",
//	  "cmd_raw": "git commit -m \"fix\"",
//	  "exit_code": 0,
//	  "duration_ms": 420,
//	  "ephemeral": false
//	}
func ParseEvent(data []byte) (*event.CommandEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input data")
	}

	var ev event.CommandEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if err := ValidateEvent(&ev); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &ev, nil
}

// ParseEventLine parses a single NDJSON line into a CommandEvent.
// It trims any trailing newline before parsing.
// This is a convenience wrapper around ParseEvent for processing NDJSON streams.
func ParseEventLine(line []byte) (*event.CommandEvent, error) {
	// Trim trailing newline if present (common in NDJSON)
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	// Also trim carriage return for Windows compatibility
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return ParseEvent(line)
}
