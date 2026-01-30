// Package cmdutil provides shared command utility functions.
package cmdutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// NormalizeCommand normalizes a command for comparison and deduplication.
// It lowercases the command, trims whitespace, and normalizes variable arguments.
func NormalizeCommand(cmd string) string {
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
		if IsNumeric(part) {
			normalized = append(normalized, "<num>")
			continue
		}

		// Keep other arguments
		normalized = append(normalized, part)
	}

	return strings.Join(normalized, " ")
}

// HashCommand generates a SHA256 hash of a normalized command string.
// The command should already be normalized before calling this function.
func HashCommand(normalizedCmd string) string {
	hash := sha256.Sum256([]byte(normalizedCmd))
	return hex.EncodeToString(hash[:])
}

// IsNumeric checks if a string contains only digits.
func IsNumeric(s string) bool {
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
