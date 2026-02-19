// Package recovery implements the failure recovery pattern engine for the
// clai suggestions system. It classifies exit codes, seeds bootstrap recovery
// patterns, queries recovery candidates, and enforces safety gates.
package recovery

import "fmt"

// FailureClass represents a semantic failure classification.
type FailureClass string

// Standard failure classes derived from Unix exit code conventions.
const (
	ClassGeneral       FailureClass = "general"        // exit 1: generic error
	ClassMisuse        FailureClass = "misuse"         // exit 2: shell builtin misuse
	ClassNotExecutable FailureClass = "not_executable" // exit 126: command not executable
	ClassNotFound      FailureClass = "not_found"      // exit 127: command not found
	ClassSignal        FailureClass = "signal"         // exit 128+N: killed by signal N
	ClassSIGINT        FailureClass = "sigint"         // exit 130: Ctrl+C
	ClassSIGKILL       FailureClass = "sigkill"        // exit 137: kill -9
	ClassSIGSEGV       FailureClass = "sigsegv"        // exit 139: segfault
	ClassPermission    FailureClass = "permission"     // exit 1 with permission context
	ClassTimeout       FailureClass = "timeout"        // exit 124: timeout
	ClassUnknown       FailureClass = "unknown"        // unrecognized exit code
)

// ExitCodeMapping maps an exit code to a FailureClass.
type ExitCodeMapping struct {
	Class FailureClass
	Code  int
}

// defaultMappings contains the standard exit code to failure class mappings.
// These follow Unix conventions (IEEE Std 1003.1, bash manual).
var defaultMappings = map[int]FailureClass{
	1:   ClassGeneral,
	2:   ClassMisuse,
	124: ClassTimeout,
	126: ClassNotExecutable,
	127: ClassNotFound,
	130: ClassSIGINT,
	137: ClassSIGKILL,
	139: ClassSIGSEGV,
}

// Classifier maps exit codes to semantic failure classes.
type Classifier struct {
	// custom holds user-configurable overrides; checked first.
	custom map[int]FailureClass
}

// NewClassifier creates an exit code classifier with optional custom mappings.
// Custom mappings take precedence over defaults.
func NewClassifier(custom []ExitCodeMapping) *Classifier {
	c := &Classifier{
		custom: make(map[int]FailureClass),
	}
	for _, m := range custom {
		c.custom[m.Code] = m.Class
	}
	return c
}

// Classify returns the FailureClass for the given exit code.
//
// Resolution order:
//  1. Custom mapping
//  2. Default mapping (standard Unix codes)
//  3. Signal range: 128 < code < 192 => ClassSignal
//  4. ClassUnknown
func (c *Classifier) Classify(exitCode int) FailureClass {
	// 1. Custom override
	if cls, ok := c.custom[exitCode]; ok {
		return cls
	}

	// 2. Default mapping
	if cls, ok := defaultMappings[exitCode]; ok {
		return cls
	}

	// 3. Signal range: 128+N where N is signal number (1..64)
	if exitCode > 128 && exitCode < 192 {
		return ClassSignal
	}

	return ClassUnknown
}

// ClassifyToKey returns the exit_code_class string used in the failure_recovery
// table. It combines the class name with the raw code for precise lookup.
// Format: "class:<name>" (e.g., "class:not_found", "class:signal")
func (c *Classifier) ClassifyToKey(exitCode int) string {
	cls := c.Classify(exitCode)
	return fmt.Sprintf("class:%s", cls)
}

// SignalNumber extracts the signal number from an exit code in the 128+N range.
// Returns -1 if the code is not in the signal range.
func SignalNumber(exitCode int) int {
	if exitCode > 128 && exitCode < 192 {
		return exitCode - 128
	}
	return -1
}

// IsSignalExit returns true if the exit code indicates the process was killed
// by a signal (128 < code < 192).
func IsSignalExit(exitCode int) bool {
	return exitCode > 128 && exitCode < 192
}
