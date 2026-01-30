package provider

import (
	"strings"

	"github.com/runger/clai/internal/sanitize"
)

// skipLinePrefixes contains common non-command line prefixes to skip during parsing
var skipLinePrefixes = []string{
	"#",
	"//",
	"Here",
	"The",
	"This",
	"Note:",
	"---",
}

// shouldSkipLine returns true if the line should be skipped during command parsing
func shouldSkipLine(line string) bool {
	for _, prefix := range skipLinePrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// startsFixSection returns true if the line looks like the start of a numbered or bulleted list
func startsFixSection(line string) bool {
	if len(line) < 2 {
		return false
	}
	// Check for numbered patterns: "1.", "1)", "2.", "2)", etc.
	if line[0] >= '1' && line[0] <= '9' {
		if line[1] == '.' || line[1] == ')' {
			return true
		}
		if len(line) >= 3 && (line[2] == '.' || line[2] == ')') {
			return true
		}
	}
	// Check for bullet patterns: "- ", "* "
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return true
	}
	return false
}

// cleanCommandPrefix removes common prefixes from command lines
func cleanCommandPrefix(line string) string {
	// Remove numbered prefixes like "1. ", "2) "
	if len(line) >= 3 && line[0] >= '1' && line[0] <= '9' {
		if line[1] == '.' || line[1] == ')' {
			line = strings.TrimSpace(line[2:])
		} else if len(line) >= 4 && line[2] == '.' || line[2] == ')' {
			line = strings.TrimSpace(line[3:])
		}
	}

	// Remove bullet prefixes
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "* ")
	line = strings.TrimPrefix(line, "$ ")

	// Remove markdown code backticks
	line = strings.Trim(line, "`")

	return strings.TrimSpace(line)
}

// createSuggestion creates a Suggestion with appropriate risk level
func createSuggestion(text string, index int) Suggestion {
	risk := "safe"
	if sanitize.IsDestructive(text) {
		risk = "destructive"
	}
	return Suggestion{
		Text:   text,
		Source: SourceAI,
		Score:  max(0.1, 1.0-float64(index)*0.1),
		Risk:   risk,
	}
}

// ParseCommandResponse parses an AI response into command suggestions.
// This is shared across all providers as they all expect the same response format.
func ParseCommandResponse(response string) []Suggestion {
	suggestions := make([]Suggestion, 0, 3)

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip common non-command prefixes
		if shouldSkipLine(line) {
			continue
		}

		// Remove common command prefixes
		cleaned := cleanCommandPrefix(line)
		if cleaned == "" {
			continue
		}

		suggestions = append(suggestions, createSuggestion(cleaned, len(suggestions)))

		// Limit to 3 suggestions
		if len(suggestions) >= 3 {
			break
		}
	}

	return suggestions
}

// ParseDiagnoseResponse parses an AI diagnosis response into an explanation and fix suggestions.
// This is shared across all providers as they all expect the same response format.
func ParseDiagnoseResponse(response string) (string, []Suggestion) {
	var explanation strings.Builder
	var fixes []Suggestion

	lines := strings.Split(response, "\n")
	inFixes := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this looks like a command (starts with $ or is numbered after fixes section)
		if strings.HasPrefix(line, "$ ") {
			inFixes = true
			cmd := strings.TrimPrefix(line, "$ ")
			fixes = append(fixes, createSuggestion(cmd, len(fixes)))
			continue
		}

		// Check for numbered/bulleted fix commands - these also start the fixes section
		cleaned := cleanCommandPrefix(line)
		if startsFixSection(line) && cleaned != "" && !strings.HasPrefix(line, "#") {
			inFixes = true
			fixes = append(fixes, createSuggestion(cleaned, len(fixes)))
			continue
		}

		// Otherwise, it's part of the explanation
		if !inFixes {
			if explanation.Len() > 0 {
				explanation.WriteString(" ")
			}
			explanation.WriteString(line)
		}
	}

	return strings.TrimSpace(explanation.String()), fixes
}
