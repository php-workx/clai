package picker

import "context"

// Provider is the interface for data sources that supply items to the picker.
// Implementations might fetch from shell history, a daemon, or any other source.
type Provider interface {
	Fetch(ctx context.Context, req Request) (Response, error)
}

// Request describes what items the picker wants from a Provider.
type Request struct {
	RequestID uint64            // Monotonically increasing, for stale response detection
	Query     string            // Search filter
	TabID     string            // Active tab identifier
	Options   map[string]string // Tab-specific options (session_id, global flag, etc.)
	Limit     int
	Offset    int
}

// Response carries items back from a Provider.
type Response struct {
	RequestID uint64   // Must match Request.RequestID to be accepted
	Items     []string // Command strings
	AtEnd     bool     // No more pages available
}
