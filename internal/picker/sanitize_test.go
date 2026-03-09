package picker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripANSI_SGR(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"bold", "\x1b[1mhello\x1b[0m", "hello"},
		{"color", "\x1b[31mred\x1b[0m", "red"},
		{"multiple SGR", "\x1b[1;31;42mfancy\x1b[0m", "fancy"},
		{"mixed", "before\x1b[32mgreen\x1b[0mafter", "beforegreenafter"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StripANSI(tt.input))
		})
	}
}

func TestStripANSI_OSC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"OSC with BEL", "\x1b]0;title\x07text", "text"},
		{"OSC with ST", "\x1b]0;title\x1b\\text", "text"},
		{"OSC hyperlink", "\x1b]8;;https://example.com\x07link\x1b]8;;\x07", "link"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StripANSI(tt.input))
		})
	}
}

func TestStripANSI_Charset(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"charset G0 ASCII", "\x1b(Bhello", "hello"},
		{"charset G1", "\x1b)Bhello", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StripANSI(tt.input))
		})
	}
}

func TestValidateUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"valid ASCII", "hello", "hello"},
		{"valid UTF-8", "cafe\u0301", "cafe\u0301"},
		{"invalid byte", "hello\x80world", "hello\uFFFDworld"},
		{"invalid continuation", "hello\xc3world", "hello\uFFFDworld"},
		{"all valid", "good \u00e9 text", "good \u00e9 text"},
		{"empty", "", ""},
		{"multiple invalid", "\x80\x81ok", "\uFFFD\uFFFDok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ValidateUTF8(tt.input))
		})
	}
}

func TestPrettyEscapeLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no escapes", "echo hi", "echo hi"},
		{"octal csi", "printf '\\033[2mhi\\033[0m\\n'", "printf '<ESC>[2mhi<ESC>[0m\\n'"},
		{"octal osc", "printf '\\033]0;title\\007'", "printf '<ESC>]0;title\\007'"},
		{"hex csi", "printf '\\x1b[2mhi\\x1B[0m'", "printf '<ESC>[2mhi<ESC>[0m'"},
		{"short e csi", "printf '\\e[2mhi\\e[0m'", "printf '<ESC>[2mhi<ESC>[0m'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PrettyEscapeLiterals(tt.input))
		})
	}
}

func TestMiddleTruncate_ASCII(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		maxWidth int
	}{
		{"fits exactly", "abcde", "abcde", 5},
		{"fits with room", "abc", "abc", 10},
		{"needs truncation", "abcdefghij", "abc\u2026hij", 7},
		{"max 3", "abcdef", "a\u2026f", 3},
		{"max 2", "abcdef", "ab", 2},
		{"max 1", "abcdef", "a", 1},
		{"max 0", "abcdef", "", 0},
		{"empty string", "", "", 5},
		{"single char", "x", "x", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MiddleTruncate(tt.input, tt.maxWidth))
		})
	}
}

func TestMiddleTruncate_CJK(t *testing.T) {
	// CJK characters are 2 columns wide.
	tests := []struct {
		name     string
		input    string
		want     string
		maxWidth int
	}{
		// "\u4f60\u597d\u4e16\u754c" = 8 columns. maxWidth=7 => head=3cols, ellipsis=1, tail=3cols
		// head: "\u4f60" (2 cols) + can't fit another CJK (2) in 3 cols => "\u4f60" takes 2;
		// actually head budget = (7-1+1)/2 = 3 cols, "\u4f60" = 2 cols, next char is "\u597d" = 2 cols, 2+2=4 > 3, so head = "\u4f60"
		// tail budget = (7-1)/2 = 3 cols. From the right: "\u754c" = 2 cols, "\u4e16" = 2 cols, 2+2=4 > 3, so tail = "\u754c"
		{"CJK truncation", "\u4f60\u597d\u4e16\u754c", "\u4f60\u2026\u754c", 7},
		{"CJK fits", "\u4f60\u597d", "\u4f60\u597d", 4},
		{"CJK exactly", "\u4f60\u597d", "\u4f60\u597d", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MiddleTruncate(tt.input, tt.maxWidth))
		})
	}
}

func TestMiddleTruncate_Emoji(t *testing.T) {
	// Many emoji are 2 columns wide.
	tests := []struct {
		name     string
		input    string
		want     string
		maxWidth int
	}{
		{"emoji fits", "\U0001f600 hi", "\U0001f600 hi", 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MiddleTruncate(tt.input, tt.maxWidth)
			assert.Equal(t, tt.want, got)
		})
	}
}
