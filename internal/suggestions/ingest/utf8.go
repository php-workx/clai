// Package ingest provides utilities for ingesting command events from shell hooks.
// This includes UTF-8 sanitization and other data preparation functions.
package ingest

import (
	"unicode/utf8"
)

// ToLossyUTF8 converts arbitrary byte data to a valid UTF-8 string.
// Invalid UTF-8 sequences are replaced with the Unicode replacement character (U+FFFD).
// NUL bytes (0x00) are also replaced with U+FFFD to prevent issues with C-style
// string handling and JSON encoding.
//
// This function preserves valid UTF-8 exactly, including multi-byte characters and emoji.
// It is designed to be efficient for large inputs by minimizing allocations when
// the input is already valid UTF-8.
func ToLossyUTF8(data []byte) string {
	// Fast path: if input is valid UTF-8 and has no NUL bytes, return as-is
	if utf8.Valid(data) && !containsNUL(data) {
		return string(data)
	}

	// Slow path: process byte by byte, replacing invalid sequences and NUL bytes
	// Pre-allocate result buffer with same capacity as input
	result := make([]byte, 0, len(data))

	for len(data) > 0 {
		// Check for NUL byte first.
		if data[0] == 0 {
			result = append(result, replacementChar...)
			data = data[1:]
			continue
		}

		// Try to decode a valid UTF-8 rune.
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 sequence - replace with U+FFFD
			result = append(result, replacementChar...)
			data = data[1:]
		} else {
			// Valid UTF-8 sequence - copy the bytes.
			result = append(result, data[:size]...)
			data = data[size:]
		}
	}

	return string(result)
}

// replacementChar is the UTF-8 encoding of U+FFFD (Unicode Replacement Character)
var replacementChar = []byte{0xEF, 0xBF, 0xBD}

// containsNUL reports whether data contains any NUL (0x00) bytes.
func containsNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
