// Package history provides shell history parsing and import functionality.
package history

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxImportEntries is the maximum number of entries to import from a history file.
const MaxImportEntries = 25000

// ImportEntry represents a single history entry with optional timestamp.
type ImportEntry struct {
	Command   string
	Timestamp time.Time // Zero value if timestamp not available
}

// ImportBashHistory reads and parses a bash history file.
// Bash history format: one command per line.
// With HISTTIMEFORMAT: timestamp lines start with #<unix_ts>.
// Returns up to MaxImportEntries most recent entries.
func ImportBashHistory(path string) ([]ImportEntry, error) {
	if path == "" {
		path = bashHistoryPath()
	}
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var entries []ImportEntry
	var pendingTimestamp time.Time

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Check for timestamp marker: #<unix_ts>
		if strings.HasPrefix(line, "#") && len(line) > 1 {
			if ts, err := strconv.ParseInt(line[1:], 10, 64); err == nil {
				pendingTimestamp = time.Unix(ts, 0)
				continue
			}
		}

		// Regular command line
		entries = append(entries, ImportEntry{
			Command:   line,
			Timestamp: pendingTimestamp,
		})
		pendingTimestamp = time.Time{} // Reset for next entry
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Return most recent entries (last MaxImportEntries)
	return trimToLimit(entries, MaxImportEntries), nil
}

// ImportZshHistory reads and parses a zsh history file.
// Zsh extended history format: `: <timestamp>:<duration>;<command>`
// Handles multiline commands with backslash continuation.
// Returns up to MaxImportEntries most recent entries.
func ImportZshHistory(path string) ([]ImportEntry, error) {
	if path == "" {
		path = zshHistoryPath()
	}
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var p importParser
	for scanner.Scan() {
		p.processLine(scanner.Text())
	}

	// Flush any pending multiline command
	if p.multilineCmd.Len() > 0 {
		p.entries = append(p.entries, ImportEntry{
			Command:   strings.TrimSuffix(p.multilineCmd.String(), "\n"),
			Timestamp: p.pendingTimestamp,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return trimToLimit(p.entries, MaxImportEntries), nil
}

// importParser accumulates parsed history entries with timestamps.
type importParser struct {
	multilineCmd     strings.Builder
	pendingTimestamp time.Time
	entries          []ImportEntry
}

// processLine parses a single zsh history file line.
func (p *importParser) processLine(line string) {
	if p.multilineCmd.Len() > 0 {
		p.continueMultiline(line)
		return
	}
	p.parseFreshLine(line)
}

// continueMultiline appends to an in-progress multiline command.
func (p *importParser) continueMultiline(line string) {
	if hasUnescapedTrailingBackslash(line) {
		p.multilineCmd.WriteString(line[:len(line)-1])
		p.multilineCmd.WriteString("\n")
		return
	}
	p.multilineCmd.WriteString(line)
	p.entries = append(p.entries, ImportEntry{
		Command:   p.multilineCmd.String(),
		Timestamp: p.pendingTimestamp,
	})
	p.multilineCmd.Reset()
	p.pendingTimestamp = time.Time{}
}

// parseFreshLine handles a line that is not part of an ongoing multiline command.
func (p *importParser) parseFreshLine(line string) {
	// Extended history format: `: <timestamp>:<duration>;<command>`
	if strings.HasPrefix(line, ": ") {
		if idx := strings.Index(line, ";"); idx != -1 {
			// Parse timestamp from `: <ts>:<dur>;`
			meta := line[2:idx] // "<ts>:<dur>"
			if colonIdx := strings.Index(meta, ":"); colonIdx != -1 {
				if ts, err := strconv.ParseInt(meta[:colonIdx], 10, 64); err == nil {
					p.pendingTimestamp = time.Unix(ts, 0)
				}
			}
			p.addCommand(line[idx+1:])
			return
		}
	}
	p.addCommand(line)
}

// addCommand adds a command, starting multiline accumulation if it ends with backslash.
func (p *importParser) addCommand(cmd string) {
	if hasUnescapedTrailingBackslash(cmd) {
		p.multilineCmd.WriteString(cmd[:len(cmd)-1])
		p.multilineCmd.WriteString("\n")
		return
	}
	if cmd != "" {
		p.entries = append(p.entries, ImportEntry{
			Command:   cmd,
			Timestamp: p.pendingTimestamp,
		})
		p.pendingTimestamp = time.Time{}
	}
}

// ImportFishHistory reads and parses a fish shell history file.
// Fish history format (pseudo-YAML):
//
//   - cmd: <command>
//     when: <unix_timestamp>
//
// Returns up to MaxImportEntries most recent entries.
func ImportFishHistory(path string) ([]ImportEntry, error) {
	if path == "" {
		path = fishHistoryPath()
	}
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var entries []ImportEntry
	var currentCmd string
	var currentTimestamp time.Time

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	inPaths := false // Track if we're in the paths section
	for scanner.Scan() {
		line := scanner.Text()

		// New entry starts with "- cmd: "
		if strings.HasPrefix(line, "- cmd: ") {
			// Save previous entry if exists
			if currentCmd != "" {
				entries = append(entries, ImportEntry{
					Command:   decodeFishEscapes(currentCmd),
					Timestamp: currentTimestamp,
				})
			}
			currentCmd = strings.TrimPrefix(line, "- cmd: ")
			currentTimestamp = time.Time{}
			inPaths = false
			continue
		}

		// Timestamp line: "  when: <ts>"
		if strings.HasPrefix(line, "  when: ") {
			tsStr := strings.TrimPrefix(line, "  when: ")
			if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
				currentTimestamp = time.Unix(ts, 0)
			}
			inPaths = false
			continue
		}

		// Paths section start
		if strings.HasPrefix(line, "  paths:") {
			inPaths = true
			continue
		}

		// Skip paths entries (lines starting with "    - ")
		if inPaths && strings.HasPrefix(line, "    - ") {
			continue
		}

		// Any other indented line when in paths section
		if inPaths && strings.HasPrefix(line, "    ") {
			continue
		}

		// Reset paths flag if we hit a non-indented line
		if !strings.HasPrefix(line, " ") {
			inPaths = false
		}
	}

	// Save final entry
	if currentCmd != "" {
		entries = append(entries, ImportEntry{
			Command:   decodeFishEscapes(currentCmd),
			Timestamp: currentTimestamp,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return trimToLimit(entries, MaxImportEntries), nil
}

// decodeFishEscapes decodes fish shell escape sequences.
// Fish uses: \\ for literal backslash, \n for newline.
func decodeFishEscapes(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				result.WriteByte('\\')
				i += 2
			case 'n':
				result.WriteByte('\n')
				i += 2
			default:
				result.WriteByte(s[i])
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// bashHistoryPath returns the path to bash history file.
func bashHistoryPath() string {
	if histFile := os.Getenv("HISTFILE"); histFile != "" {
		return histFile
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".bash_history")
}

// fishHistoryPath returns the path to fish history file.
func fishHistoryPath() string {
	// Fish uses XDG_DATA_HOME/fish/fish_history
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "fish", "fish_history")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "fish", "fish_history")
}

// trimToLimit returns the last n entries from a slice.
// If len(entries) <= n, returns the original slice.
func trimToLimit(entries []ImportEntry, n int) []ImportEntry {
	if len(entries) <= n {
		return entries
	}
	return entries[len(entries)-n:]
}

// DetectShell returns the shell name based on SHELL env or current shell.
func DetectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	base := filepath.Base(shell)
	switch base {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	default:
		return ""
	}
}

// ImportForShell imports history for the specified shell.
// Shell can be "bash", "zsh", "fish", or "auto" (detect from SHELL env).
func ImportForShell(shell string) ([]ImportEntry, error) {
	if shell == "auto" || shell == "" {
		shell = DetectShell()
	}

	switch shell {
	case "bash":
		return ImportBashHistory("")
	case "zsh":
		return ImportZshHistory("")
	case "fish":
		return ImportFishHistory("")
	default:
		return nil, nil
	}
}
