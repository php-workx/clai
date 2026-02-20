// Package api provides HTTP/IPC endpoints for the suggestions engine.
// This file implements input validation per spec Section 13.4.
package api

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
)

// Validation limits per spec Section 13.4.
const (
	MaxSessionIDLen   = 128
	MaxCommandIDLen   = 128
	MaxCwdLen         = 4096
	MaxCommandLen     = 10240 // 10KB
	MaxGitBranchLen   = 256
	MaxGitRepoNameLen = 256
	MaxGitRepoRootLen = 4096
	MaxRepoKeyLen     = 512

	MinExitCode = -128
	MaxExitCode = 255

	MaxDurationMs = 86400000 // 24 hours

	MinLimit     = 1
	MaxLimit     = 50
	DefaultLimit = 10

	// MaxTimeFutureMs is the maximum allowed time in the future (5 minutes).
	MaxTimeFutureMs = 5 * 60 * 1000

	errExceedsMaxLengthFmt = "exceeds max length %d"
	errRequiredNonEmpty    = "is required and must be non-empty"
)

// sessionIDPattern matches alphanumeric characters and dashes.
var sessionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// ValidationError represents a structured validation error per spec Section 13.3.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("E_INVALID_ARGUMENT: %s: %s", e.Field, e.Message)
}

// ToAPIError converts a ValidationError to the proto ApiError message.
func (e *ValidationError) ToAPIError() *pb.ApiError {
	return &pb.ApiError{
		Code:      "E_INVALID_ARGUMENT",
		Message:   fmt.Sprintf("%s: %s", e.Field, e.Message),
		Retryable: false,
	}
}

// ValidationResult holds the result of validating a request.
// It collects hard errors (which block processing) and warnings
// (where values were clamped to valid ranges).
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationWarning
}

// ValidationWarning is logged but does not reject the request.
// The offending value is clamped to a valid range instead.
type ValidationWarning struct {
	Field   string
	Message string
}

// HasErrors returns true if there are any validation errors.
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// FirstError returns the first validation error, or nil.
func (r *ValidationResult) FirstError() *ValidationError {
	if len(r.Errors) == 0 {
		return nil
	}
	return &r.Errors[0]
}

// addError adds a hard validation error.
func (r *ValidationResult) addError(field, message string) {
	r.Errors = append(r.Errors, ValidationError{Field: field, Message: message})
}

// addWarning adds a clamping warning.
func (r *ValidationResult) addWarning(field, message string) {
	r.Warnings = append(r.Warnings, ValidationWarning{Field: field, Message: message})
}

// LogWarnings logs all warnings to the given logger.
func (r *ValidationResult) LogWarnings(logger *slog.Logger) {
	for _, w := range r.Warnings {
		logger.Warn("input validation clamped value",
			"field", w.Field,
			"detail", w.Message,
		)
	}
}

// ValidateCommandStartRequest validates a CommandStartRequest per spec Section 13.4.
// Fields that can be clamped are adjusted in-place on the request.
func ValidateCommandStartRequest(req *pb.CommandStartRequest) *ValidationResult {
	result := &ValidationResult{}

	// session_id: required, non-empty, max 128 chars, alphanumeric + dashes
	validateSessionID(result, req.SessionId)

	// command_id: required, non-empty, max 128 chars
	validateCommandID(result, req.CommandId)

	// cwd: required, valid absolute path, max 4096 chars
	validateCwd(result, req.Cwd)

	// command: required, non-empty, max 10KB
	validateCommand(result, req.Command)

	// ts_unix_ms: required, positive, not more than 5 min in the future
	validateTimestamp(result, req.TsUnixMs, "ts_unix_ms")

	// git_branch: optional, max 256 chars
	if req.GitBranch != "" {
		validateOptionalString(result, req.GitBranch, "git_branch", MaxGitBranchLen)
	}

	// git_repo_name: optional, max 256 chars
	if req.GitRepoName != "" {
		validateOptionalString(result, req.GitRepoName, "git_repo_name", MaxGitRepoNameLen)
	}

	// git_repo_root: optional, valid path, max 4096 chars
	if req.GitRepoRoot != "" {
		validateOptionalPath(result, req.GitRepoRoot, "git_repo_root", MaxGitRepoRootLen)
	}

	return result
}

// ValidateCommandEndRequest validates a CommandEndRequest per spec Section 13.4.
// Fields that can be clamped are adjusted in-place on the request.
func ValidateCommandEndRequest(req *pb.CommandEndRequest) *ValidationResult {
	result := &ValidationResult{}

	// session_id: required, non-empty, max 128 chars, alphanumeric + dashes
	validateSessionID(result, req.SessionId)

	// command_id: required, non-empty, max 128 chars
	validateCommandID(result, req.CommandId)

	// ts_unix_ms: required, positive, not more than 5 min in the future
	validateTimestamp(result, req.TsUnixMs, "ts_unix_ms")

	// exit_code: optional (0 is default in proto), range [-128, 255]
	if req.ExitCode < int32(MinExitCode) || req.ExitCode > int32(MaxExitCode) {
		result.addError("exit_code", fmt.Sprintf("must be between %d and %d, got %d", MinExitCode, MaxExitCode, req.ExitCode))
	}

	// duration_ms: optional, non-negative, clamp to max 86400000 (24h)
	if req.DurationMs < 0 {
		result.addWarning("duration_ms", fmt.Sprintf("must be non-negative, got %d; clamping to 0", req.DurationMs))
		req.DurationMs = 0
	} else if req.DurationMs > int64(MaxDurationMs) {
		result.addWarning("duration_ms", fmt.Sprintf("exceeds max %d, got %d; clamping", MaxDurationMs, req.DurationMs))
		req.DurationMs = int64(MaxDurationMs)
	}

	return result
}

// ValidateSuggestRequest validates a SuggestRequest per spec Section 13.4.
// Fields that can be clamped are adjusted in-place on the request.
func ValidateSuggestRequest(req *pb.SuggestRequest) *ValidationResult {
	result := &ValidationResult{}

	// session_id: required, non-empty, max 128 chars, alphanumeric + dashes
	validateSessionID(result, req.SessionId)

	// cwd: required, valid absolute path, max 4096 chars
	validateCwd(result, req.Cwd)

	// buffer (command): optional for suggest, but if present max 10KB
	if len(req.Buffer) > MaxCommandLen {
		result.addError("buffer", fmt.Sprintf(errExceedsMaxLengthFmt, MaxCommandLen))
	}

	// max_results (limit): optional, range [1, 50], default 10
	switch {
	case req.MaxResults == 0:
		// Apply default - this is a clamp, not an error
		req.MaxResults = int32(DefaultLimit)
		result.addWarning("max_results", "not set; defaulting to 10")
	case req.MaxResults < int32(MinLimit):
		result.addWarning("max_results", fmt.Sprintf("below minimum %d, got %d; clamping to %d", MinLimit, req.MaxResults, MinLimit))
		req.MaxResults = int32(MinLimit)
	case req.MaxResults > int32(MaxLimit):
		result.addWarning("max_results", fmt.Sprintf("exceeds maximum %d, got %d; clamping to %d", MaxLimit, req.MaxResults, MaxLimit))
		req.MaxResults = int32(MaxLimit)
	}

	// cursor_pos: optional, non-negative, clamp to command (buffer) length
	if req.CursorPos < 0 {
		result.addWarning("cursor_pos", fmt.Sprintf("must be non-negative, got %d; clamping to 0", req.CursorPos))
		req.CursorPos = 0
	} else if req.Buffer != "" && int(req.CursorPos) > len(req.Buffer) {
		result.addWarning("cursor_pos", fmt.Sprintf("exceeds buffer length %d, got %d; clamping", len(req.Buffer), req.CursorPos))
		req.CursorPos = int32(len(req.Buffer)) //nolint:gosec // buffer length bounded by MaxCommandLen (10KB)
	}

	// repo_key: optional, max 512 chars
	if req.RepoKey != "" {
		validateOptionalString(result, req.RepoKey, "repo_key", MaxRepoKeyLen)
	}

	// include_low_confidence: boolean, no validation needed

	return result
}

// validateSessionID validates session_id: required, non-empty, max 128 chars, alphanumeric + dashes.
func validateSessionID(result *ValidationResult, sessionID string) {
	if sessionID == "" {
		result.addError("session_id", errRequiredNonEmpty)
		return
	}
	if len(sessionID) > MaxSessionIDLen {
		result.addError("session_id", fmt.Sprintf(errExceedsMaxLengthFmt, MaxSessionIDLen))
		return
	}
	if !sessionIDPattern.MatchString(sessionID) {
		result.addError("session_id", "must contain only alphanumeric characters and dashes")
	}
}

// validateCommandID validates command_id: required, non-empty, max 128 chars.
func validateCommandID(result *ValidationResult, commandID string) {
	if commandID == "" {
		result.addError("command_id", errRequiredNonEmpty)
		return
	}
	if len(commandID) > MaxCommandIDLen {
		result.addError("command_id", fmt.Sprintf(errExceedsMaxLengthFmt, MaxCommandIDLen))
	}
}

// validateCwd validates cwd: required, valid absolute path, max 4096 chars.
func validateCwd(result *ValidationResult, cwd string) {
	if cwd == "" {
		result.addError("cwd", errRequiredNonEmpty)
		return
	}
	if len(cwd) > MaxCwdLen {
		result.addError("cwd", fmt.Sprintf(errExceedsMaxLengthFmt, MaxCwdLen))
		return
	}
	if !filepath.IsAbs(cwd) {
		result.addError("cwd", "must be an absolute path")
	}
}

// validateCommand validates command: required, non-empty, max 10KB.
func validateCommand(result *ValidationResult, command string) {
	if command == "" {
		result.addError("command", errRequiredNonEmpty)
		return
	}
	if len(command) > MaxCommandLen {
		result.addError("command", fmt.Sprintf(errExceedsMaxLengthFmt, MaxCommandLen))
	}
}

// validateTimestamp validates ts_unix_ms: required, positive, not more than 5 min in the future.
func validateTimestamp(result *ValidationResult, tsUnixMs int64, field string) {
	if tsUnixMs <= 0 {
		result.addError(field, "must be a positive unix timestamp in milliseconds")
		return
	}
	nowMs := time.Now().UnixMilli()
	if tsUnixMs > nowMs+int64(MaxTimeFutureMs) {
		result.addError(field, "must not be more than 5 minutes in the future")
	}
}

// validateOptionalString validates an optional string field with a max length.
func validateOptionalString(result *ValidationResult, value, field string, maxLen int) {
	if len(value) > maxLen {
		result.addError(field, fmt.Sprintf(errExceedsMaxLengthFmt, maxLen))
	}
}

// validateOptionalPath validates an optional path field: valid path, max length.
func validateOptionalPath(result *ValidationResult, value, field string, maxLen int) {
	if len(value) > maxLen {
		result.addError(field, fmt.Sprintf(errExceedsMaxLengthFmt, maxLen))
		return
	}
	if !filepath.IsAbs(value) {
		result.addError(field, "must be an absolute path")
	}
}

// ValidationErrorResponse represents a structured error response for validation failures.
// This is a JSON-friendly wrapper around ApiError for HTTP endpoints.
type ValidationErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// NewValidationErrorResponse creates a ValidationErrorResponse from a ValidationError.
func NewValidationErrorResponse(ve *ValidationError) ValidationErrorResponse {
	return ValidationErrorResponse{
		Error:   "E_INVALID_ARGUMENT",
		Code:    "E_INVALID_ARGUMENT",
		Field:   ve.Field,
		Message: ve.Message,
	}
}

// CleanStringField trims whitespace and control characters from a string field.
// This is a pre-processing step before validation.
func CleanStringField(s string) string {
	return strings.TrimSpace(s)
}
