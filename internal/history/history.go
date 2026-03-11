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
	suggestions := Suggestions(prefix, 1)
	if len(suggestions) > 0 {
		return suggestions[0]
	}
	return ""
}

// Search finds up to `limit` unique history entries containing query as substring
// Returns entries in order from most recent to oldest
func Search(query string, limit int) []string {
	if query == "" || limit <= 0 {
		return nil
	}

	histFile := zshHistoryPath()
	if histFile == "" {
		return nil
	}

	entries, err := readZshHistory(histFile)
	if err != nil {
		return nil
	}

	// Use a map to track seen commands (deduplication)
	seen := make(map[string]bool)
	var results []string

	// Case-insensitive search
	queryLower := strings.ToLower(query)

	// Search from most recent (end of file)
	for i := len(entries) - 1; i >= 0 && len(results) < limit; i-- {
		entry := entries[i]
		if strings.Contains(strings.ToLower(entry), queryLower) {
			// Skip duplicates
			if !seen[entry] {
				seen[entry] = true
				results = append(results, entry)
			}
		}
	}

	return results
}

// Suggestions finds up to `limit` unique history entries starting with prefix
// Returns entries in order from most recent to oldest
func Suggestions(prefix string, limit int) []string {
	if prefix == "" || limit <= 0 {
		return nil
	}

	histFile := zshHistoryPath()
	if histFile == "" {
		return nil
	}

	entries, err := readZshHistory(histFile)
	if err != nil {
		return nil
	}

	// Use a map to track seen commands (deduplication)
	seen := make(map[string]bool)
	var results []string

	// Search from most recent (end of file)
	for i := len(entries) - 1; i >= 0 && len(results) < limit; i-- {
		entry := entries[i]
		if strings.HasPrefix(entry, prefix) && entry != prefix {
			// Skip duplicates
			if !seen[entry] {
				seen[entry] = true
				results = append(results, entry)
			}
		}
	}

	return results
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

// hasTrailingBackslash returns true if s ends with a backslash character.
// In zsh history files, a trailing backslash before a newline is always the
// escape marker for an embedded newline (line continuation). Zsh does not
// escape backslashes in history — it only uses \<newline> to represent
// newlines within a command. So "cmd \\\n" means the command text has a
// literal \ followed by a newline (two chars: the \ is command text, the
// second \ is the escape marker that gets stripped on read).
func hasTrailingBackslash(s string) bool {
	return s != "" && s[len(s)-1] == '\\'
}

// historyParser accumulates parsed history entries, handling multiline commands
type historyParser struct {
	multilineCmd strings.Builder
	entries      []string
}

// processLine parses a single history file line, accumulating entries
func (p *historyParser) processLine(line string) {
	if p.multilineCmd.Len() > 0 {
		p.continueMultiline(line)
		return
	}
	p.parseFreshLine(line)
}

// continueMultiline appends to an in-progress multiline command
func (p *historyParser) continueMultiline(line string) {
	if hasTrailingBackslash(line) {
		p.multilineCmd.WriteString(line[:len(line)-1])
		p.multilineCmd.WriteString("\n")
		return
	}
	p.multilineCmd.WriteString(line)
	p.entries = append(p.entries, p.multilineCmd.String())
	p.multilineCmd.Reset()
}

// parseFreshLine handles a line that is not part of an ongoing multiline command
func (p *historyParser) parseFreshLine(line string) {
	if strings.HasPrefix(line, ": ") {
		if idx := strings.Index(line, ";"); idx != -1 {
			p.addCommand(line[idx+1:])
			return
		}
	}
	p.addCommand(line)
}

// addCommand adds a command, starting a multiline accumulation if it ends with backslash
func (p *historyParser) addCommand(cmd string) {
	if hasTrailingBackslash(cmd) {
		p.multilineCmd.WriteString(cmd[:len(cmd)-1])
		p.multilineCmd.WriteString("\n")
		return
	}
	if cmd != "" {
		p.entries = append(p.entries, cmd)
	}
}

// readZshHistory reads and parses zsh history file
// Handles the extended history format: : timestamp:0;command
func readZshHistory(path string) ([]string, error) {
	file, err := os.Open(path) //nolint:gosec // G304: path is from user's HISTFILE or well-known default
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var p historyParser
	for scanner.Scan() {
		p.processLine(scanner.Text())
	}

	if p.multilineCmd.Len() > 0 {
		p.entries = append(p.entries, strings.TrimSuffix(p.multilineCmd.String(), "\n"))
	}

	return p.entries, scanner.Err()
}
