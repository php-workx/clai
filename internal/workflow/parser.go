package workflow

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// ParseWorkflow parses YAML bytes into a WorkflowDef.
// It uses KnownFields(true) for strict parsing (D17/P1-2): unknown
// YAML fields produce a parse error, preventing silent typos.
func ParseWorkflow(data []byte) (*WorkflowDef, error) {
	var wf WorkflowDef
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&wf); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return &wf, nil
}

// unsafePathChars matches characters unsafe in filenames on any platform:
//
//	/ \ : * ? " < > | and control characters (< 0x20)
var unsafePathChars = regexp.MustCompile(`[/\\:*?"<>|` + "\x00-\x1f" + `]`)

// windowsReserved contains names that are reserved on Windows filesystems.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

const maxPathComponentLen = 200

// sanitizePathComponent makes a string safe for use in filenames on all platforms (SS7.5).
// It replaces unsafe characters, handles Windows-reserved names, removes
// leading/trailing dots and spaces, and truncates to 200 characters.
func sanitizePathComponent(s string) string {
	if s == "" {
		return "_"
	}

	// Replace unsafe characters with underscore.
	s = unsafePathChars.ReplaceAllString(s, "_")

	// Replace ".." sequences to prevent path traversal.
	s = strings.ReplaceAll(s, "..", "_")

	// Trim leading/trailing dots and spaces (problematic on Windows).
	s = strings.TrimFunc(s, func(r rune) bool {
		return r == '.' || r == ' '
	})

	if s == "" {
		return "_"
	}

	// Check for Windows-reserved names (case-insensitive, with or without extension).
	baseName := s
	if idx := strings.IndexByte(baseName, '.'); idx >= 0 {
		baseName = baseName[:idx]
	}
	if windowsReserved[strings.ToUpper(baseName)] {
		s = "_" + s
	}

	// Truncate to maxPathComponentLen characters, respecting UTF-8 boundaries.
	if utf8.RuneCountInString(s) > maxPathComponentLen {
		runes := []rune(s)
		s = string(runes[:maxPathComponentLen])
	}

	// Final cleanup: collapse runs of underscores.
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}

	// Remove non-printable characters that may have survived.
	s = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return '_'
		}
		return r
	}, s)

	if s == "" {
		return "_"
	}

	return s
}
