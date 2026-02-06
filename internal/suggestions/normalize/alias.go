package normalize

import "strings"

// DefaultMaxAliasDepth is the default maximum alias expansion depth.
const DefaultMaxAliasDepth = 5

// AliasExpander expands shell aliases with bounded depth and cycle detection.
type AliasExpander struct {
	// Aliases maps alias names to their expansions.
	Aliases map[string]string

	// MaxDepth limits expansion depth (default: DefaultMaxAliasDepth).
	MaxDepth int
}

// Expand expands the first token of cmd if it matches an alias.
// Returns the expanded command and whether any expansion occurred.
// Expansion is bounded by MaxDepth and detects cycles.
func (e *AliasExpander) Expand(cmd string) (string, bool) {
	if e == nil || len(e.Aliases) == 0 {
		return cmd, false
	}

	maxDepth := e.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxAliasDepth
	}

	seen := make(map[string]bool)
	expanded := false

	for depth := 0; depth < maxDepth; depth++ {
		first, rest := splitFirstToken(cmd)
		if first == "" {
			break
		}

		// Cycle detection
		if seen[first] {
			break
		}

		expansion, ok := e.Aliases[first]
		if !ok {
			break
		}

		seen[first] = true
		expanded = true

		if rest != "" {
			cmd = expansion + " " + rest
		} else {
			cmd = expansion
		}
	}

	return cmd, expanded
}

// splitFirstToken splits a command into the first whitespace-delimited token
// and the rest of the string.
func splitFirstToken(cmd string) (string, string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", ""
	}

	idx := strings.IndexAny(cmd, " \t")
	if idx < 0 {
		return cmd, ""
	}
	return cmd[:idx], strings.TrimSpace(cmd[idx+1:])
}
