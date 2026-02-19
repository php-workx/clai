// Package alias provides shell alias capture, storage, expansion, and
// reverse-mapping for the clai suggestions engine.
//
// Per spec appendix 20.6, alias maps are captured per shell and used for:
//   - Normalization: expanding aliases before template learning
//   - Rendering: suggesting alias-preferred forms to users
//   - Mid-session re-snapshot after alias/unalias/abbr commands
package alias

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// CaptureTimeout is the maximum time to wait for a shell alias capture command.
const CaptureTimeout = 5 * time.Second

// AliasMap is a mapping from alias name to its expansion.
type AliasMap map[string]string

// ParseSnapshot parses shell-provided alias output into an AliasMap.
// Supported shells: bash, zsh, fish.
func ParseSnapshot(shell, raw string) AliasMap {
	switch shell {
	case "bash":
		return parseBashAliases(raw)
	case "zsh":
		return parseZshAliases(raw)
	case "fish":
		return parseFishAbbreviations(raw)
	default:
		return make(AliasMap)
	}
}

// Capture captures the current alias map from the given shell.
// Supported shells: bash, zsh, fish.
// Returns an empty map (not nil) if no aliases are found or the shell is unsupported.
func Capture(ctx context.Context, shell string) (AliasMap, error) {
	ctx, cancel := context.WithTimeout(ctx, CaptureTimeout)
	defer cancel()

	switch shell {
	case "bash":
		return captureBash(ctx)
	case "zsh":
		return captureZsh(ctx)
	case "fish":
		return captureFish(ctx)
	default:
		return make(AliasMap), nil
	}
}

// captureBash captures aliases from bash using `bash -ic 'alias -p'`.
// Output format: alias name='expansion'
func captureBash(ctx context.Context) (AliasMap, error) {
	out, err := runShellCommand(ctx, "bash", "-ic", "alias -p")
	if err != nil {
		return make(AliasMap), fmt.Errorf("bash alias capture: %w", err)
	}
	return parseBashAliases(out), nil
}

// captureZsh captures aliases from zsh using `zsh -ic 'alias -rL'`.
// Output format: name=expansion  (one per line)
func captureZsh(ctx context.Context) (AliasMap, error) {
	out, err := runShellCommand(ctx, "zsh", "-ic", "alias -rL")
	if err != nil {
		return make(AliasMap), fmt.Errorf("zsh alias capture: %w", err)
	}
	return parseZshAliases(out), nil
}

// captureFish captures abbreviations from fish using `fish -c 'abbr --show'`.
// Output format: abbr -a -- name expansion
func captureFish(ctx context.Context) (AliasMap, error) {
	out, err := runShellCommand(ctx, "fish", "-c", "abbr --show")
	if err != nil {
		return make(AliasMap), fmt.Errorf("fish alias capture: %w", err)
	}
	return parseFishAbbreviations(out), nil
}

// runShellCommand executes a command and returns its stdout output.
func runShellCommand(ctx context.Context, name string, args ...string) (string, error) {
	//nolint:gosec // shell binary/args are selected from a fixed allowlist by caller.
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exec %s: %w (stderr: %s)", name, err, stderr.String())
	}
	return stdout.String(), nil
}

// parseBashAliases parses the output of `alias -p` in bash.
// Format: alias name='expansion'
func parseBashAliases(output string) AliasMap {
	aliases := make(AliasMap)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "alias ") {
			continue
		}
		// Remove "alias " prefix
		line = line[len("alias "):]

		// Split on first '='
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 1 {
			continue
		}
		name := line[:eqIdx]
		value := line[eqIdx+1:]

		// Remove surrounding quotes (single or double)
		value = unquote(value)
		if name != "" {
			aliases[name] = value
		}
	}
	return aliases
}

// parseZshAliases parses the output of `alias -rL` in zsh.
// Format: name=expansion  (one per line)
// Zsh may also output: name='expansion with spaces'
func parseZshAliases(output string) AliasMap {
	aliases := make(AliasMap)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Split on first '='
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 1 {
			continue
		}
		name := line[:eqIdx]
		value := line[eqIdx+1:]

		// Remove surrounding quotes (single or double)
		value = unquote(value)
		if name != "" {
			aliases[name] = value
		}
	}
	return aliases
}

// parseFishAbbreviations parses the output of `abbr --show` in fish.
// Format: abbr -a -- name expansion
// Or older fish: abbr name expansion
func parseFishAbbreviations(output string) AliasMap {
	aliases := make(AliasMap)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parseFishAbbreviationLine(aliases, line)
	}
	return aliases
}

func parseFishAbbreviationLine(aliases AliasMap, line string) {
	if strings.HasPrefix(line, "abbr -a -- ") {
		addFishAbbreviation(aliases, line[len("abbr -a -- "):])
		return
	}
	if strings.HasPrefix(line, "abbr -a ") {
		parseModernFishAbbreviation(aliases, line[len("abbr -a "):])
		return
	}
	if strings.HasPrefix(line, "abbr ") {
		addFishAbbreviation(aliases, line[len("abbr "):])
	}
}

func parseModernFishAbbreviation(aliases AliasMap, rest string) {
	if dashIdx := strings.Index(rest, " -- "); dashIdx > 0 {
		name := strings.TrimSpace(rest[:dashIdx])
		expansion := strings.TrimSpace(rest[dashIdx+4:])
		if name != "" {
			aliases[name] = unquote(expansion)
		}
		return
	}
	addFishAbbreviation(aliases, rest)
}

func addFishAbbreviation(aliases AliasMap, text string) {
	name, expansion := splitFirstWord(text)
	if name == "" {
		return
	}
	aliases[name] = unquote(expansion)
}

// unquote removes matching surrounding quotes from a string.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '\'' && s[len(s)-1] == '\'') ||
		(s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

// splitFirstWord splits a string into the first whitespace-delimited word
// and the remaining text.
func splitFirstWord(s string) (first, rest string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	idx := strings.IndexAny(s, " \t")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.TrimSpace(s[idx+1:])
}

// ShouldResnapshot returns true if the given command should trigger an alias re-capture.
// This detects commands beginning with alias, unalias, or abbr.
func ShouldResnapshot(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	first, _ := splitFirstWord(cmd)
	first = strings.ToLower(first)
	switch first {
	case "alias", "unalias", "abbr":
		return true
	}
	return false
}

// ReverseEntry is a reverse mapping from expansion to alias name.
// When multiple aliases map to the same expansion, the shortest alias name wins.
// The returned map is sorted by expansion length (longest first) for
// greedy prefix matching during rendering.
type ReverseEntry struct {
	Expansion string
	AliasName string
}

// BuildReverseMap builds a sorted list of reverse alias entries from an alias map.
// Entries are sorted by expansion length descending so the longest expansions
// are matched first during rendering (greedy matching).
func BuildReverseMap(aliases AliasMap) []ReverseEntry {
	if len(aliases) == 0 {
		return nil
	}

	// Build expansion -> shortest alias name
	best := make(map[string]string)
	for name, expansion := range aliases {
		expansion = strings.TrimSpace(expansion)
		if expansion == "" {
			continue
		}
		existing, ok := best[expansion]
		if !ok || len(name) < len(existing) {
			best[expansion] = name
		}
	}

	entries := make([]ReverseEntry, 0, len(best))
	for expansion, name := range best {
		entries = append(entries, ReverseEntry{
			Expansion: expansion,
			AliasName: name,
		})
	}

	// Sort by expansion length descending (longest first for greedy matching)
	sort.Slice(entries, func(i, j int) bool {
		if len(entries[i].Expansion) != len(entries[j].Expansion) {
			return len(entries[i].Expansion) > len(entries[j].Expansion)
		}
		return entries[i].Expansion < entries[j].Expansion
	})

	return entries
}

// RenderWithAliases rewrites a command to use aliases where possible.
// It performs prefix matching using the reverse map, replacing the longest
// matching command prefix with the corresponding alias.
//
// Example: if alias gs='git status', then "git status --short" becomes "gs --short".
func RenderWithAliases(cmd string, reverseMap []ReverseEntry) string {
	if len(reverseMap) == 0 || cmd == "" {
		return cmd
	}

	cmd = strings.TrimSpace(cmd)

	for _, entry := range reverseMap {
		// Check if cmd starts with this expansion
		if !strings.HasPrefix(cmd, entry.Expansion) {
			continue
		}

		rest := cmd[len(entry.Expansion):]
		// The expansion must match at a word boundary:
		// either the rest is empty, or it starts with whitespace
		if rest == "" {
			return entry.AliasName
		}
		if rest[0] == ' ' || rest[0] == '\t' {
			return entry.AliasName + rest
		}
	}

	return cmd
}
