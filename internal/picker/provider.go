// Package picker provides an interactive TUI for browsing and selecting
// shell history entries and command suggestions.
package picker

import "context"

// Item is a pickable entry in the TUI.
//
// Value is the string inserted into the shell when selected.
// Display is what the picker renders (defaults to Value when empty).
type Item struct {
	Value   string
	Display string
	Details []string
}

func (it Item) displayText() string {
	if it.Display != "" {
		return PrettyEscapeLiterals(it.Display)
	}
	return PrettyEscapeLiterals(it.Value)
}

// Provider is the interface for data sources that supply items to the picker.
// Implementations might fetch from shell history, a daemon, or any other source.
type Provider interface {
	Fetch(ctx context.Context, req Request) (Response, error)
}

// Request describes what items the picker wants from a Provider.
type Request struct {
	Options   map[string]string // Tab-specific options (session_id, global flag, etc.)
	Query     string            // Search filter
	TabID     string            // Active tab identifier
	RequestID uint64            // Monotonically increasing, for stale response detection
	Limit     int
	Offset    int
}

// Response carries items back from a Provider.
type Response struct {
	Items     []Item // Pickable items
	RequestID uint64 // Must match Request.RequestID to be accepted
	AtEnd     bool   // No more pages available
}
