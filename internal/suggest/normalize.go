// Package suggest provides command suggestion ranking and scoring.
package suggest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
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

// Normalize normalizes a command for comparison and deduplication.
// It lowercases the command, trims whitespace, and normalizes variable arguments.
func Normalize(cmd string) string {
	// Trim whitespace
	cmd = strings.TrimSpace(cmd)

	// Lowercase
	cmd = strings.ToLower(cmd)

	// Split into parts
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}

	// Keep the base command and common flags, but normalize variable arguments
	normalized := make([]string, 0, len(parts))
	for i, part := range parts {
		// Keep the first part (the command itself) always
		if i == 0 {
			normalized = append(normalized, part)
			continue
		}

		// Keep flags (start with -)
		if strings.HasPrefix(part, "-") {
			normalized = append(normalized, part)
			continue
		}

		// Skip paths that look like absolute paths or home-relative paths
		if strings.HasPrefix(part, "/") || strings.HasPrefix(part, "~") {
			// Replace with a placeholder for normalization
			normalized = append(normalized, "<path>")
			continue
		}

		// Skip things that look like URLs
		if strings.Contains(part, "://") {
			normalized = append(normalized, "<url>")
			continue
		}

		// Skip things that look like numbers (e.g., PIDs, port numbers)
		if isNumeric(part) {
			normalized = append(normalized, "<num>")
			continue
		}

		// Keep other arguments
		normalized = append(normalized, part)
	}

	return strings.Join(normalized, " ")
}

// Hash generates a SHA256 hash of a command string.
func Hash(cmd string) string {
	normalized := Normalize(cmd)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

// isNumeric checks if a string contains only digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
