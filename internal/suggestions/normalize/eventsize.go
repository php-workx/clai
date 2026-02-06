package normalize

// DefaultMaxEventSize is the default maximum event size in bytes (10KB).
const DefaultMaxEventSize = 10 * 1024

// EnforceEventSize truncates a command string if it exceeds maxBytes.
// Returns the (potentially truncated) string and a flag indicating truncation.
// If maxBytes is <= 0, DefaultMaxEventSize is used.
func EnforceEventSize(cmdRaw string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxEventSize
	}
	if len(cmdRaw) <= maxBytes {
		return cmdRaw, false
	}
	return cmdRaw[:maxBytes], true
}
