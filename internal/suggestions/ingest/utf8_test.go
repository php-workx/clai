package ingest

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToLossyUTF8_ValidUTF8Passthrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"ascii only", "hello world"},
		{"ascii with punctuation", "git commit -m \"fix: bug\""},
		{"ascii with numbers", "echo 12345"},
		{"multi-byte latin", "caf\u00e9"}, // cafe with accent
		{"german", "Gr\u00fc\u00df Gott"},
		{"french", "d\u00e9j\u00e0 vu"},
		{"cyrillic", "\u041f\u0440\u0438\u0432\u0435\u0442"}, // Привет
		{"chinese", "\u4e2d\u6587"},                          // 中文
		{"japanese hiragana", "\u3053\u3093\u306b\u3061\u306f"},
		{"japanese kanji", "\u65e5\u672c\u8a9e"},
		{"korean", "\uc548\ub155\ud558\uc138\uc694"}, // 안녕하세요
		{"arabic", "\u0645\u0631\u062d\u0628\u0627"},
		{"hebrew", "\u05e9\u05dc\u05d5\u05dd"},
		{"thai", "\u0e2a\u0e27\u0e31\u0e2a\u0e14\u0e35"},
		{"emoji simple", "\U0001F600"},                                                  // 😀
		{"emoji complex", "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466"}, // family emoji
		{"emoji flag", "\U0001F1FA\U0001F1F8"},                                          // US flag
		{"mixed ascii and emoji", "Hello \U0001F44B World"},
		{"mixed scripts", "Hello \u4e16\u754c \U0001F30D"},
		{"newlines and tabs", "line1\nline2\ttabbed"},
		{"shell command with unicode", "echo '\u4e2d\u6587' | grep \u6587"},
		{"path with spaces", "/home/user/My Documents/file.txt"},
		{"special chars", "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
		{"escape sequences", "\\n\\t\\r"},
		{"zero-width joiner", "a\u200Db"},                    // ZWJ
		{"BOM", "\uFEFFhello"},                               // Byte Order Mark
		{"mathematical symbols", "\u221a\u03c0\u2248\u221e"}, // √π≈∞
		{"currency symbols", "\u20ac\u00a3\u00a5\u20bf"},     // €£¥₿
		{"arrows", "\u2190\u2191\u2192\u2193"},               // ←↑→↓
		{"box drawing", "\u250c\u2510\u2514\u2518"},          // ┌┐└┘
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToLossyUTF8([]byte(tt.input))
			if result != tt.input {
				t.Errorf("ToLossyUTF8(%q) = %q, want %q", tt.input, result, tt.input)
			}
		})
	}
}

func TestToLossyUTF8_InvalidSequencesReplaced(t *testing.T) {
	t.Parallel()

	replacement := "\uFFFD" // U+FFFD

	tests := []struct {
		name   string
		input  []byte
		output string
	}{
		{
			name:   "single invalid byte 0x80",
			input:  []byte{0x80},
			output: replacement,
		},
		{
			name:   "single invalid byte 0xFF",
			input:  []byte{0xFF},
			output: replacement,
		},
		{
			name:   "invalid byte 0xFE",
			input:  []byte{0xFE},
			output: replacement,
		},
		{
			name:   "truncated 2-byte sequence",
			input:  []byte{0xC2}, // Start of 2-byte sequence, missing continuation
			output: replacement,
		},
		{
			name:   "truncated 3-byte sequence missing 1",
			input:  []byte{0xE0, 0xA0}, // Start of 3-byte, missing last byte
			output: replacement + replacement,
		},
		{
			name:   "truncated 3-byte sequence missing 2",
			input:  []byte{0xE0}, // Start of 3-byte, missing 2 bytes
			output: replacement,
		},
		{
			name:   "truncated 4-byte sequence missing 1",
			input:  []byte{0xF0, 0x90, 0x80}, // Start of 4-byte, missing last byte
			output: replacement + replacement + replacement,
		},
		{
			name:   "truncated 4-byte sequence missing 2",
			input:  []byte{0xF0, 0x90}, // Start of 4-byte, missing 2 bytes
			output: replacement + replacement,
		},
		{
			name:   "truncated 4-byte sequence missing 3",
			input:  []byte{0xF0}, // Start of 4-byte, missing 3 bytes
			output: replacement,
		},
		{
			name:   "invalid continuation byte",
			input:  []byte{0xC2, 0x00}, // 2-byte start followed by NUL (invalid continuation)
			output: replacement + replacement,
		},
		{
			name:   "overlong encoding for ASCII",
			input:  []byte{0xC0, 0xAF}, // Overlong encoding of '/'
			output: replacement + replacement,
		},
		{
			name:   "overlong encoding 3-byte for 2-byte char",
			input:  []byte{0xE0, 0x80, 0xAF}, // Overlong encoding
			output: replacement + replacement + replacement,
		},
		{
			name:   "invalid start byte followed by valid",
			input:  []byte{0x80, 'a', 'b', 'c'},
			output: replacement + "abc",
		},
		{
			name:   "valid followed by invalid",
			input:  []byte{'a', 'b', 'c', 0x80},
			output: "abc" + replacement,
		},
		{
			name:   "multiple consecutive invalid bytes",
			input:  []byte{0x80, 0x81, 0x82},
			output: replacement + replacement + replacement,
		},
		{
			name:   "surrogate pair (invalid in UTF-8)",
			input:  []byte{0xED, 0xA0, 0x80}, // U+D800 encoded (invalid)
			output: replacement + replacement + replacement,
		},
		{
			name:   "code point above U+10FFFF",
			input:  []byte{0xF4, 0x90, 0x80, 0x80}, // Beyond Unicode range
			output: replacement + replacement + replacement + replacement,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToLossyUTF8(tt.input)
			if result != tt.output {
				t.Errorf("ToLossyUTF8(%v) = %q, want %q", tt.input, result, tt.output)
			}
			// Verify result is valid UTF-8
			if !utf8.ValidString(result) {
				t.Errorf("ToLossyUTF8(%v) produced invalid UTF-8: %q", tt.input, result)
			}
		})
	}
}

func TestToLossyUTF8_MixedValidInvalid(t *testing.T) {
	t.Parallel()

	replacement := "\uFFFD"

	tests := []struct {
		name   string
		input  []byte
		output string
	}{
		{
			name:   "invalid between valid ASCII",
			input:  []byte{'h', 'e', 0x80, 'l', 'l', 'o'},
			output: "he" + replacement + "llo",
		},
		{
			name:   "invalid between valid multi-byte",
			input:  append(append([]byte("\u4e2d"), 0x80), []byte("\u6587")...),
			output: "\u4e2d" + replacement + "\u6587",
		},
		{
			name:   "invalid at start, valid emoji at end",
			input:  append([]byte{0xFF}, []byte("\U0001F600")...),
			output: replacement + "\U0001F600",
		},
		{
			name:   "valid emoji at start, invalid at end",
			input:  append([]byte("\U0001F600"), 0xFF),
			output: "\U0001F600" + replacement,
		},
		{
			name:   "alternating valid and invalid",
			input:  []byte{'a', 0x80, 'b', 0x81, 'c'},
			output: "a" + replacement + "b" + replacement + "c",
		},
		{
			name:   "valid UTF-8 command with trailing invalid",
			input:  append([]byte("git commit -m \"test\""), 0x80, 0x81),
			output: "git commit -m \"test\"" + replacement + replacement,
		},
		{
			name:   "valid path with embedded invalid byte",
			input:  []byte{'/', 'h', 'o', 'm', 'e', '/', 0x80, 'u', 's', 'e', 'r'},
			output: "/home/" + replacement + "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToLossyUTF8(tt.input)
			if result != tt.output {
				t.Errorf("ToLossyUTF8(%v) = %q, want %q", tt.input, result, tt.output)
			}
			if !utf8.ValidString(result) {
				t.Errorf("ToLossyUTF8(%v) produced invalid UTF-8: %q", tt.input, result)
			}
		})
	}
}

func TestToLossyUTF8_NULByteHandling(t *testing.T) {
	t.Parallel()

	replacement := "\uFFFD"

	tests := []struct {
		name   string
		input  []byte
		output string
	}{
		{
			name:   "single NUL byte",
			input:  []byte{0x00},
			output: replacement,
		},
		{
			name:   "NUL at start",
			input:  []byte{0x00, 'a', 'b', 'c'},
			output: replacement + "abc",
		},
		{
			name:   "NUL at end",
			input:  []byte{'a', 'b', 'c', 0x00},
			output: "abc" + replacement,
		},
		{
			name:   "NUL in middle",
			input:  []byte{'a', 'b', 0x00, 'c', 'd'},
			output: "ab" + replacement + "cd",
		},
		{
			name:   "multiple NUL bytes",
			input:  []byte{0x00, 0x00, 0x00},
			output: replacement + replacement + replacement,
		},
		{
			name:   "NUL between multi-byte chars",
			input:  []byte{0xE4, 0xB8, 0xAD, 0x00, 0xE6, 0x96, 0x87}, // 中\x00文
			output: "\u4e2d" + replacement + "\u6587",
		},
		{
			name:   "NUL after emoji",
			input:  append([]byte("\U0001F600"), 0x00),
			output: "\U0001F600" + replacement,
		},
		{
			name:   "NUL and invalid byte together",
			input:  []byte{0x00, 0x80},
			output: replacement + replacement,
		},
		{
			name:   "command with embedded NUL",
			input:  []byte{'e', 'c', 'h', 'o', ' ', 0x00, 'h', 'i'},
			output: "echo " + replacement + "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToLossyUTF8(tt.input)
			if result != tt.output {
				t.Errorf("ToLossyUTF8(%v) = %q, want %q", tt.input, result, tt.output)
			}
			if !utf8.ValidString(result) {
				t.Errorf("ToLossyUTF8(%v) produced invalid UTF-8: %q", tt.input, result)
			}
			// Verify no NUL bytes in output
			if strings.Contains(result, "\x00") {
				t.Errorf("ToLossyUTF8(%v) contains NUL byte in output", tt.input)
			}
		})
	}
}

func TestToLossyUTF8_EmptyInput(t *testing.T) {
	t.Parallel()

	result := ToLossyUTF8([]byte{})
	if result != "" {
		t.Errorf("ToLossyUTF8([]) = %q, want empty string", result)
	}

	result = ToLossyUTF8(nil)
	if result != "" {
		t.Errorf("ToLossyUTF8(nil) = %q, want empty string", result)
	}
}

func TestToLossyUTF8_OutputIsValidUTF8(t *testing.T) {
	t.Parallel()

	// Test with various byte patterns to ensure output is always valid UTF-8
	testCases := [][]byte{
		{},
		{0x00},
		{0x80},
		{0xFF},
		{0xC0, 0x80},
		{0xE0, 0x80, 0x80},
		{0xF0, 0x80, 0x80, 0x80},
		{0xFE, 0xFF},
		bytes.Repeat([]byte{0x80}, 100),
		bytes.Repeat([]byte{0xFF}, 100),
		append([]byte("hello"), bytes.Repeat([]byte{0x80}, 50)...),
	}

	for i, tc := range testCases {
		result := ToLossyUTF8(tc)
		if !utf8.ValidString(result) {
			t.Errorf("Test case %d: ToLossyUTF8(%v) produced invalid UTF-8: %q", i, tc, result)
		}
	}
}

func TestToLossyUTF8_LargeInput(t *testing.T) {
	t.Parallel()

	// Test with large valid UTF-8 input
	largeValid := bytes.Repeat([]byte("Hello, \u4e16\u754c! \U0001F600 "), 10000)
	result := ToLossyUTF8(largeValid)
	if result != string(largeValid) {
		t.Errorf("Large valid input was modified")
	}

	// Test with large input containing invalid bytes
	largeInvalid := bytes.Repeat([]byte{0x80, 'a'}, 10000)
	result = ToLossyUTF8(largeInvalid)
	if !utf8.ValidString(result) {
		t.Errorf("Large invalid input produced invalid UTF-8")
	}
	// Should have 10000 replacement chars and 10000 'a's
	expectedLen := 10000*3 + 10000 // 3 bytes per replacement char + 1 byte per 'a'
	if len(result) != expectedLen {
		t.Errorf("Large invalid input: len(result) = %d, want %d", len(result), expectedLen)
	}
}

func TestContainsNUL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    []byte
		contains bool
	}{
		{[]byte{}, false},
		{nil, false},
		{[]byte("hello"), false},
		{[]byte{0x00}, true},
		{[]byte{'a', 0x00, 'b'}, true},
		{[]byte{0x00, 'a'}, true},
		{[]byte{'a', 0x00}, true},
		{[]byte{0x01, 0x02, 0x03}, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := containsNUL(tt.input)
			if result != tt.contains {
				t.Errorf("containsNUL(%v) = %v, want %v", tt.input, result, tt.contains)
			}
		})
	}
}

// FuzzToLossyUTF8 runs fuzz tests to ensure the function never panics
// and always produces valid UTF-8 output.
func FuzzToLossyUTF8(f *testing.F) {
	// Seed with various interesting byte sequences
	f.Add([]byte{})
	f.Add([]byte("hello world"))
	f.Add([]byte{0x00})
	f.Add([]byte{0x80})
	f.Add([]byte{0xFF})
	f.Add([]byte{0xC2, 0xA9})                     // Valid 2-byte: ©
	f.Add([]byte{0xE4, 0xB8, 0xAD})               // Valid 3-byte: 中
	f.Add([]byte{0xF0, 0x9F, 0x98, 0x80})         // Valid 4-byte: 😀
	f.Add([]byte{0xC0, 0x80})                     // Invalid: overlong NUL
	f.Add([]byte{0xED, 0xA0, 0x80})               // Invalid: surrogate
	f.Add([]byte{0xF4, 0x90, 0x80, 0x80})         // Invalid: beyond Unicode
	f.Add([]byte("hello\x00world"))               // Embedded NUL
	f.Add([]byte("valid\x80invalid\x81test"))     // Mixed
	f.Add([]byte("\xE4\xB8\xAD\x00\xE6\x96\x87")) // Multi-byte with embedded NUL

	f.Fuzz(func(t *testing.T, data []byte) {
		result := ToLossyUTF8(data)

		// Output must be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("ToLossyUTF8(%v) produced invalid UTF-8: %q", data, result)
		}

		// Output must not contain NUL bytes
		if strings.Contains(result, "\x00") {
			t.Errorf("ToLossyUTF8(%v) contains NUL byte in output: %q", data, result)
		}

		// If input is valid UTF-8 without NUL bytes, output should equal input
		if utf8.Valid(data) && !containsNUL(data) {
			if result != string(data) {
				t.Errorf("ToLossyUTF8(%q) modified valid UTF-8 input: got %q", string(data), result)
			}
		}
	})
}

// Benchmark tests for performance verification

func BenchmarkToLossyUTF8_ValidASCII(b *testing.B) {
	data := []byte("git commit -m \"fix: update README with installation instructions\"")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToLossyUTF8(data)
	}
}

func BenchmarkToLossyUTF8_ValidMultiByte(b *testing.B) {
	data := []byte("Hello, \u4e16\u754c! \U0001F600 \u0420\u0443\u0441\u0441\u043a\u0438\u0439")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToLossyUTF8(data)
	}
}

func BenchmarkToLossyUTF8_InvalidBytes(b *testing.B) {
	data := bytes.Repeat([]byte{0x80, 'a'}, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToLossyUTF8(data)
	}
}

func BenchmarkToLossyUTF8_LargeValidASCII(b *testing.B) {
	data := bytes.Repeat([]byte("command -flag --option=value "), 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToLossyUTF8(data)
	}
}

func BenchmarkToLossyUTF8_LargeMixed(b *testing.B) {
	// Mix of valid and invalid bytes
	data := make([]byte, 10000)
	for i := range data {
		if i%10 == 0 {
			data[i] = 0x80 // Invalid every 10th byte
		} else {
			data[i] = byte('a' + (i % 26))
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToLossyUTF8(data)
	}
}

func BenchmarkToLossyUTF8_WithNUL(b *testing.B) {
	data := []byte("command\x00with\x00embedded\x00nulls")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToLossyUTF8(data)
	}
}
