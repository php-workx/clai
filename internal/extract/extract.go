// Package extract provides command extraction from terminal output
package extract

import (
	"regexp"
	"strings"
)

// Pattern represents a command extraction pattern
type Pattern struct {
	Name    string
	Regex   *regexp.Regexp
	Process func(string) string
}

// Patterns contains all extraction patterns in priority order
var Patterns = []Pattern{
	{
		// Pattern 1: Commands in backticks
		// Matches: `npm install express`
		Name:  "backticks",
		Regex: regexp.MustCompile("`([^`]+)`"),
		Process: func(s string) string {
			// Extract content between backticks, must start with letter
			matches := regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(s, -1)
			for _, m := range matches {
				if len(m) > 1 && regexp.MustCompile(`^[a-zA-Z]`).MatchString(m[1]) {
					return m[1]
				}
			}
			return ""
		},
	},
	{
		// Pattern 2: Common install/run commands
		// Matches: pip install requests, npm install, brew install, cargo install, go install, etc.
		Name:    "install",
		Regex:   regexp.MustCompile(`(?i)(pip3?|python3? -m pip|npm|yarn|pnpm|brew|cargo|go|apt-get|apt|dnf|pacman -S)\s+install\s+[a-zA-Z0-9_@/.:=-]+(\s+[a-zA-Z0-9_@/.:=-]+)*`),
		Process: func(s string) string { return s },
	},
	{
		// Pattern 3: "Run:" or "Execute:" or "Try:" prefixed commands
		// Matches: Run: npm start, Try: python app.py, Execute: ./setup.sh
		Name:  "prefixed",
		Regex: regexp.MustCompile(`(?i)(run|execute|try|use):\s*([a-zA-Z./][^\n]+)`),
		Process: func(s string) string {
			re := regexp.MustCompile(`(?i)(run|execute|try|use):\s*`)
			return re.ReplaceAllString(s, "")
		},
	},
	{
		// Pattern 4: Lines starting with $ (documentation examples)
		// Matches: $ npm run dev
		Name:  "dollar",
		Regex: regexp.MustCompile(`(?m)^\s*\$\s+(.+)$`),
		Process: func(s string) string {
			re := regexp.MustCompile(`^\s*\$\s+`)
			return re.ReplaceAllString(s, "")
		},
	},
	{
		// Pattern 5: "To install" or "To fix" followed by a command
		// Matches: To install, run: npm install foo
		Name:  "to-prefix",
		Regex: regexp.MustCompile(`(?i)to\s+(install|fix|run|start|build)[^:]*:\s*([a-zA-Z][^\n]+)`),
		Process: func(s string) string {
			re := regexp.MustCompile(`(?i)to\s+(install|fix|run|start|build)[^:]*:\s*`)
			return re.ReplaceAllString(s, "")
		},
	},
}

// Suggestion extracts a suggested command from the given content
// Returns empty string if no suggestion found
func Suggestion(content string) string {
	for _, p := range Patterns {
		matches := p.Regex.FindAllString(content, -1)
		if len(matches) > 0 {
			// Use the last match (most likely to be the relevant one)
			match := matches[len(matches)-1]
			suggestion := p.Process(match)
			if suggestion != "" {
				return Clean(suggestion)
			}
		}
	}
	return ""
}

// Clean removes trailing punctuation and whitespace from a suggestion
func Clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ".,;:")
	return s
}

// SuggestionWithPattern extracts a suggestion and returns which pattern matched
// Useful for debugging and testing
func SuggestionWithPattern(content string) (suggestion string, patternName string) {
	for _, p := range Patterns {
		matches := p.Regex.FindAllString(content, -1)
		if len(matches) > 0 {
			match := matches[len(matches)-1]
			suggestion := p.Process(match)
			if suggestion != "" {
				return Clean(suggestion), p.Name
			}
		}
	}
	return "", ""
}
