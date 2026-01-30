// Package suggest provides command suggestion ranking and scoring.
package suggest

import (
	"strings"
)

// GetToolPrefix extracts the base command (tool) from a command string.
// This is used for tool affinity scoring.
// Examples:
//   - "git status" -> "git"
//   - "docker run -d nginx" -> "docker"
//   - "kubectl get pods" -> "kubectl"
func GetToolPrefix(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}

	// Handle environment variable prefixes like "FOO=bar cmd"
	for {
		if idx := strings.IndexByte(cmd, '='); idx != -1 {
			// Check if there's a space before the '='
			spaceIdx := strings.IndexByte(cmd, ' ')
			if spaceIdx == -1 || spaceIdx > idx {
				// This looks like VAR=value, skip to next word
				afterSpace := strings.IndexByte(cmd, ' ')
				if afterSpace == -1 {
					// No more words, the whole thing is VAR=value
					return ""
				}
				cmd = strings.TrimSpace(cmd[afterSpace+1:])
				continue
			}
		}
		break
	}

	// Extract first word
	if idx := strings.IndexByte(cmd, ' '); idx != -1 {
		return strings.ToLower(cmd[:idx])
	}
	return strings.ToLower(cmd)
}

// NormalizeForDisplay normalizes a command for display purposes.
// Unlike storage.NormalizeCommand (which is for deduplication),
// this preserves original arguments but normalizes whitespace.
func NormalizeForDisplay(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}

	// Normalize multiple spaces to single space
	parts := strings.Fields(cmd)
	return strings.Join(parts, " ")
}

// DeduplicateKey generates a key for deduplication purposes.
// Commands that should be considered duplicates will have the same key.
// This uses the command hash from the storage layer.
func DeduplicateKey(normalizedCmd string) string {
	return normalizedCmd
}
