// Package history provides fast shell history searching
package history

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Suggestion finds the most recent history entry starting with prefix
// Returns empty string if no match found
func Suggestion(prefix string) string {
	if prefix == "" {
		return ""
	}

	histFile := zshHistoryPath()
	if histFile == "" {
		return ""
	}

	entries, err := readZshHistory(histFile)
	if err != nil {
		return ""
	}

	// Search from most recent (end of file)
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if strings.HasPrefix(entry, prefix) && entry != prefix {
			return entry
		}
	}

	return ""
}

// zshHistoryPath returns the path to zsh history file
func zshHistoryPath() string {
	// Check HISTFILE env var first
	if histFile := os.Getenv("HISTFILE"); histFile != "" {
		return histFile
	}

	// Default to ~/.zsh_history
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".zsh_history")
}

// readZshHistory reads and parses zsh history file
// Handles the extended history format: : timestamp:0;command
func readZshHistory(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []string
	scanner := bufio.NewScanner(file)

	// Increase buffer size for long commands
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var multilineCmd strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// Handle multiline commands (lines ending with \)
		if multilineCmd.Len() > 0 {
			// Continue multiline
			if strings.HasSuffix(line, "\\") {
				multilineCmd.WriteString(strings.TrimSuffix(line, "\\"))
				multilineCmd.WriteString("\n")
				continue
			}
			// End of multiline
			multilineCmd.WriteString(line)
			entries = append(entries, multilineCmd.String())
			multilineCmd.Reset()
			continue
		}

		// Parse zsh extended history format: : timestamp:0;command
		if strings.HasPrefix(line, ": ") {
			if idx := strings.Index(line, ";"); idx != -1 {
				cmd := line[idx+1:]
				if strings.HasSuffix(cmd, "\\") {
					// Start of multiline command
					multilineCmd.WriteString(strings.TrimSuffix(cmd, "\\"))
					multilineCmd.WriteString("\n")
					continue
				}
				entries = append(entries, cmd)
				continue
			}
		}

		// Plain history format (no timestamp)
		if strings.HasSuffix(line, "\\") {
			multilineCmd.WriteString(strings.TrimSuffix(line, "\\"))
			multilineCmd.WriteString("\n")
			continue
		}

		if line != "" {
			entries = append(entries, line)
		}
	}

	return entries, scanner.Err()
}
