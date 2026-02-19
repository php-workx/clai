// Package suggest provides command suggestion ranking and scoring.
package suggest

import (
	"strings"

	"github.com/runger/clai/internal/cmdutil"
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
// Unlike NormalizeCommand (which is for deduplication),
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
// Currently returns the input verbatim; callers should pre-normalize via NormalizeCommand.
// For hash-based deduplication, use Hash instead.
func DeduplicateKey(normalizedCmd string) string {
	return normalizedCmd
}

// NormalizeCommand normalizes a command for comparison and deduplication.
// It lowercases the command, trims whitespace, and normalizes variable arguments.
// This is a re-export of cmdutil.NormalizeCommand.
var NormalizeCommand = cmdutil.NormalizeCommand

// Normalize is an alias for NormalizeCommand for backward compatibility.
//
// Deprecated: Use NormalizeCommand instead.
func Normalize(cmd string) string {
	return cmdutil.NormalizeCommand(cmd)
}

// HashCommand generates a SHA256 hash of a normalized command string.
// The command should already be normalized before calling this function.
// This is a re-export of cmdutil.HashCommand.
var HashCommand = cmdutil.HashCommand

// Hash normalizes a command and then generates a SHA256 hash.
// This is a convenience function that combines NormalizeCommand and HashCommand.
func Hash(cmd string) string {
	normalized := cmdutil.NormalizeCommand(cmd)
	return cmdutil.HashCommand(normalized)
}

// IsNumeric checks if a string contains only digits.
// This is a re-export of cmdutil.IsNumeric.
var IsNumeric = cmdutil.IsNumeric
