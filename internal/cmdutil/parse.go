package cmdutil

import (
	"strings"
)

// IsSudo returns true if the command starts with "sudo".
func IsSudo(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	return strings.HasPrefix(cmd, "sudo ") || cmd == "sudo"
}

// CountPipes returns the number of pipe operators (|) in a command.
// It handles quoted strings to avoid counting pipes inside quotes.
func CountPipes(cmd string) int {
	count := 0
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	for _, r := range cmd {
		if escaped {
			escaped = false
			continue
		}

		switch r {
		case '\\':
			escaped = true
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '|':
			if !inSingleQuote && !inDoubleQuote {
				count++
			}
		}
	}

	return count
}

// CountWords returns the number of words/arguments in a command.
// This uses simple whitespace splitting after trimming.
func CountWords(cmd string) int {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return 0
	}
	return len(strings.Fields(cmd))
}
