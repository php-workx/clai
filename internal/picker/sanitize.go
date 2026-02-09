package picker

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// ansiRE matches ANSI escape sequences:
//   - CSI sequences: ESC [ ... final_byte  (covers SGR like \x1b[31m)
//   - OSC sequences: ESC ] ... (ST | BEL)
//   - Charset sequences: ESC ( B, ESC ) B, etc.
//   - Other two-byte escapes: ESC followed by a single byte in [#()*+\-./]
var ansiRE = regexp.MustCompile(`\x1b(?:` +
	`\[[0-9;]*[A-Za-z]` + // CSI sequences (SGR, cursor, etc.)
	`|` +
	`\].*?(?:\x1b\\|\x07)` + // OSC sequences (terminated by ST or BEL)
	`|` +
	`[()][A-B0-2]` + // Charset designation sequences
	`|` +
	`[#()*+\-./][A-Za-z0-9]` + // Other two-byte escape sequences
	`)`)

// StripANSI removes ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// ValidateUTF8 replaces invalid UTF-8 byte sequences with the Unicode
// replacement character (U+FFFD).
func ValidateUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			b.WriteRune(utf8.RuneError)
			i++
		} else {
			b.WriteRune(r)
			i += size
		}
	}
	return b.String()
}

// PrettyEscapeLiterals replaces common *literal* escape-sequence spellings in
// shell commands (like "\033[" or "\x1b[") with a more readable token.
//
// This is intended for display-only rendering in pickers; it must not be used
// on strings that will be executed.
func PrettyEscapeLiterals(s string) string {
	if s == "" {
		return s
	}
	// Common ANSI escape spellings found in shell commands, including printf.
	// Note: these are *literal* backslashes, not actual ESC bytes.
	r := strings.NewReplacer(
		"\\033[", "<ESC>[",
		"\\033]", "<ESC>]",
		"\\x1b[", "<ESC>[",
		"\\x1B[", "<ESC>[",
		"\\x1b]", "<ESC>]",
		"\\x1B]", "<ESC>]",
		"\\e[", "<ESC>[",
		"\\e]", "<ESC>]",
	)
	return r.Replace(s)
}

// MiddleTruncate truncates a string in the middle with an ellipsis character
// if its display width exceeds maxWidth. It is display-width-aware, correctly
// handling CJK characters and emoji that occupy two columns.
//
// If maxWidth < 3 (minimum for "x...x"), the string is simply truncated from
// the right to fit maxWidth.
func MiddleTruncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	sw := runewidth.StringWidth(s)
	if sw <= maxWidth {
		return s
	}

	const ellipsis = "\u2026" // "..."
	const ellipsisWidth = 1

	// Not enough room for head + ellipsis + tail: just hard-truncate.
	if maxWidth < 3 {
		return runewidthTruncate(s, maxWidth)
	}

	// Split available width between head and tail around the ellipsis.
	// Give one extra column to the head when maxWidth-1 is odd.
	remaining := maxWidth - ellipsisWidth
	headWidth := (remaining + 1) / 2
	tailWidth := remaining / 2

	head := runewidthTruncate(s, headWidth)
	tail := runewidthTruncateRight(s, tailWidth)

	return head + ellipsis + tail
}

// runewidthTruncate returns the longest prefix of s whose display width
// does not exceed maxWidth.
func runewidthTruncate(s string, maxWidth int) string {
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxWidth {
			return s[:i]
		}
		w += rw
	}
	return s
}

// runewidthTruncateRight returns the longest suffix of s whose display width
// does not exceed maxWidth.
func runewidthTruncateRight(s string, maxWidth int) string {
	runes := []rune(s)
	w := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if w+rw > maxWidth {
			break
		}
		w += rw
		start = i
	}
	return string(runes[start:])
}
