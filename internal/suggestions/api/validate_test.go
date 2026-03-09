package api

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/runger/clai/gen/clai/v1"
)

// --- session_id validation ---

func TestValidateSessionID_Empty(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	assert.Equal(t, "session_id", result.FirstError().Field)
	assert.Contains(t, result.FirstError().Message, "required")
}

func TestValidateSessionID_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: strings.Repeat("a", MaxSessionIDLen+1),
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	assert.Equal(t, "session_id", result.FirstError().Field)
	assert.Contains(t, result.FirstError().Message, "max length")
}

func TestValidateSessionID_InvalidChars(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session@invalid!",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	assert.Equal(t, "session_id", result.FirstError().Field)
	assert.Contains(t, result.FirstError().Message, "alphanumeric")
}

func TestValidateSessionID_ValidWithDashes(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "abc-123-def-456",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

func TestValidateSessionID_MaxLength(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: strings.Repeat("a", MaxSessionIDLen),
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

// --- command_id validation ---

func TestValidateCommandID_Empty(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCommandIDError := false
	for _, e := range result.Errors {
		if e.Field == "command_id" {
			hasCommandIDError = true
			assert.Contains(t, e.Message, "required")
		}
	}
	assert.True(t, hasCommandIDError, "expected command_id error")
}

func TestValidateCommandID_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: strings.Repeat("x", MaxCommandIDLen+1),
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCommandIDError := false
	for _, e := range result.Errors {
		if e.Field == "command_id" {
			hasCommandIDError = true
			assert.Contains(t, e.Message, "max length")
		}
	}
	assert.True(t, hasCommandIDError, "expected command_id error")
}

// --- cwd validation ---

func TestValidateCwd_Empty(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCwdError := false
	for _, e := range result.Errors {
		if e.Field == "cwd" {
			hasCwdError = true
			assert.Contains(t, e.Message, "required")
		}
	}
	assert.True(t, hasCwdError, "expected cwd error")
}

func TestValidateCwd_RelativePath(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "relative/path",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCwdError := false
	for _, e := range result.Errors {
		if e.Field == "cwd" {
			hasCwdError = true
			assert.Contains(t, e.Message, "absolute path")
		}
	}
	assert.True(t, hasCwdError, "expected cwd error")
}

func TestValidateCwd_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/" + strings.Repeat("a", MaxCwdLen),
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCwdError := false
	for _, e := range result.Errors {
		if e.Field == "cwd" {
			hasCwdError = true
			assert.Contains(t, e.Message, "max length")
		}
	}
	assert.True(t, hasCwdError, "expected cwd error")
}

func TestValidateCwd_ValidAbsolutePath(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/home/user/project",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

// --- command validation ---

func TestValidateCommand_Empty(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCommandError := false
	for _, e := range result.Errors {
		if e.Field == "command" {
			hasCommandError = true
			assert.Contains(t, e.Message, "required")
		}
	}
	assert.True(t, hasCommandError, "expected command error")
}

func TestValidateCommand_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   strings.Repeat("x", MaxCommandLen+1),
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasCommandError := false
	for _, e := range result.Errors {
		if e.Field == "command" {
			hasCommandError = true
			assert.Contains(t, e.Message, "max length")
		}
	}
	assert.True(t, hasCommandError, "expected command error")
}

// --- ts_unix_ms validation ---

func TestValidateTimestamp_Zero(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  0,
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasTSError := false
	for _, e := range result.Errors {
		if e.Field == "ts_unix_ms" {
			hasTSError = true
			assert.Contains(t, e.Message, "positive")
		}
	}
	assert.True(t, hasTSError, "expected ts_unix_ms error")
}

func TestValidateTimestamp_Negative(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  -100,
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasTSError := false
	for _, e := range result.Errors {
		if e.Field == "ts_unix_ms" {
			hasTSError = true
		}
	}
	assert.True(t, hasTSError, "expected ts_unix_ms error")
}

func TestValidateTimestamp_TooFarInFuture(t *testing.T) {
	t.Parallel()
	futureMs := time.Now().Add(10 * time.Minute).UnixMilli()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  futureMs,
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasTSError := false
	for _, e := range result.Errors {
		if e.Field == "ts_unix_ms" {
			hasTSError = true
			assert.Contains(t, e.Message, "future")
		}
	}
	assert.True(t, hasTSError, "expected ts_unix_ms error")
}

func TestValidateTimestamp_Valid(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

func TestValidateTimestamp_SlightlyInFuture(t *testing.T) {
	t.Parallel()
	// 1 minute in the future should be fine (within 5 min window)
	futureMs := time.Now().Add(1 * time.Minute).UnixMilli()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  futureMs,
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

// --- exit_code validation (CommandEndRequest) ---

func TestValidateExitCode_Valid(t *testing.T) {
	t.Parallel()
	for _, code := range []int32{0, 1, 127, 255, -1, -128} {
		req := &pb.CommandEndRequest{
			SessionId: "session-1",
			CommandId: "cmd-1",
			TsUnixMs:  time.Now().UnixMilli(),
			ExitCode:  code,
		}
		result := ValidateCommandEndRequest(req)
		hasExitCodeError := false
		for _, e := range result.Errors {
			if e.Field == "exit_code" {
				hasExitCodeError = true
			}
		}
		assert.False(t, hasExitCodeError, "exit_code %d should be valid", code)
	}
}

func TestValidateExitCode_TooLow(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		TsUnixMs:  time.Now().UnixMilli(),
		ExitCode:  -129,
	}
	result := ValidateCommandEndRequest(req)
	hasExitCodeError := false
	for _, e := range result.Errors {
		if e.Field == "exit_code" {
			hasExitCodeError = true
			assert.Contains(t, e.Message, "-128")
		}
	}
	assert.True(t, hasExitCodeError, "expected exit_code error")
}

func TestValidateExitCode_TooHigh(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		TsUnixMs:  time.Now().UnixMilli(),
		ExitCode:  256,
	}
	result := ValidateCommandEndRequest(req)
	hasExitCodeError := false
	for _, e := range result.Errors {
		if e.Field == "exit_code" {
			hasExitCodeError = true
			assert.Contains(t, e.Message, "255")
		}
	}
	assert.True(t, hasExitCodeError, "expected exit_code error")
}

// --- duration_ms validation (CommandEndRequest) ---

func TestValidateDurationMs_Negative_Clamped(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId:  "session-1",
		CommandId:  "cmd-1",
		TsUnixMs:   time.Now().UnixMilli(),
		DurationMs: -100,
	}
	result := ValidateCommandEndRequest(req)
	assert.Equal(t, int64(0), req.DurationMs, "should be clamped to 0")
	assert.Len(t, result.Warnings, 1)
	assert.Equal(t, "duration_ms", result.Warnings[0].Field)
}

func TestValidateDurationMs_ExceedsMax_Clamped(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId:  "session-1",
		CommandId:  "cmd-1",
		TsUnixMs:   time.Now().UnixMilli(),
		DurationMs: int64(MaxDurationMs) + 1000,
	}
	result := ValidateCommandEndRequest(req)
	assert.Equal(t, int64(MaxDurationMs), req.DurationMs, "should be clamped to max")
	assert.Len(t, result.Warnings, 1)
	assert.Equal(t, "duration_ms", result.Warnings[0].Field)
}

func TestValidateDurationMs_ValidZero(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId:  "session-1",
		CommandId:  "cmd-1",
		TsUnixMs:   time.Now().UnixMilli(),
		DurationMs: 0,
	}
	result := ValidateCommandEndRequest(req)
	assert.Len(t, result.Warnings, 0)
}

func TestValidateDurationMs_ValidNormal(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId:  "session-1",
		CommandId:  "cmd-1",
		TsUnixMs:   time.Now().UnixMilli(),
		DurationMs: 5000,
	}
	result := ValidateCommandEndRequest(req)
	assert.Equal(t, int64(5000), req.DurationMs)
	assert.Len(t, result.Warnings, 0)
}

// --- limit (max_results) validation (SuggestRequest) ---

func TestValidateLimit_Zero_DefaultsTo10(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:  "session-1",
		Cwd:        "/tmp",
		MaxResults: 0,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(DefaultLimit), req.MaxResults)
}

func TestValidateLimit_BelowMin_Clamped(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:  "session-1",
		Cwd:        "/tmp",
		MaxResults: -5,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(MinLimit), req.MaxResults)
	assert.True(t, len(result.Warnings) > 0)
}

func TestValidateLimit_AboveMax_Clamped(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:  "session-1",
		Cwd:        "/tmp",
		MaxResults: 100,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(MaxLimit), req.MaxResults)
	assert.True(t, len(result.Warnings) > 0)
}

func TestValidateLimit_ValidInRange(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:  "session-1",
		Cwd:        "/tmp",
		MaxResults: 25,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(25), req.MaxResults)
}

// --- cursor_pos validation (SuggestRequest) ---

func TestValidateCursorPos_Negative_Clamped(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		Buffer:    "hello",
		CursorPos: -1,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(0), req.CursorPos)
	hasCursorWarning := false
	for _, w := range result.Warnings {
		if w.Field == "cursor_pos" {
			hasCursorWarning = true
		}
	}
	assert.True(t, hasCursorWarning)
}

func TestValidateCursorPos_ExceedsBufferLen_Clamped(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		Buffer:    "hello",
		CursorPos: 10,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(5), req.CursorPos) // len("hello") == 5
	hasCursorWarning := false
	for _, w := range result.Warnings {
		if w.Field == "cursor_pos" {
			hasCursorWarning = true
		}
	}
	assert.True(t, hasCursorWarning)
}

func TestValidateCursorPos_Valid(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		Buffer:    "hello",
		CursorPos: 3,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Equal(t, int32(3), req.CursorPos)
}

func TestValidateCursorPos_EmptyBuffer_ZeroOk(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		CursorPos: 0,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
}

// --- git_branch validation ---

func TestValidateGitBranch_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
		GitBranch: strings.Repeat("b", MaxGitBranchLen+1),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasBranchError := false
	for _, e := range result.Errors {
		if e.Field == "git_branch" {
			hasBranchError = true
		}
	}
	assert.True(t, hasBranchError)
}

func TestValidateGitBranch_Valid(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
		GitBranch: "feature/my-branch",
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

func TestValidateGitBranch_Empty_NoError(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId: "session-1",
		CommandId: "cmd-1",
		Cwd:       "/tmp",
		Command:   "ls",
		TsUnixMs:  time.Now().UnixMilli(),
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

// --- git_repo_name validation ---

func TestValidateGitRepoName_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId:   "session-1",
		CommandId:   "cmd-1",
		Cwd:         "/tmp",
		Command:     "ls",
		TsUnixMs:    time.Now().UnixMilli(),
		GitRepoName: strings.Repeat("r", MaxGitRepoNameLen+1),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasRepoNameError := false
	for _, e := range result.Errors {
		if e.Field == "git_repo_name" {
			hasRepoNameError = true
		}
	}
	assert.True(t, hasRepoNameError)
}

func TestValidateGitRepoName_Valid(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId:   "session-1",
		CommandId:   "cmd-1",
		Cwd:         "/tmp",
		Command:     "ls",
		TsUnixMs:    time.Now().UnixMilli(),
		GitRepoName: "my-repo",
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

// --- git_repo_root validation ---

func TestValidateGitRepoRoot_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId:   "session-1",
		CommandId:   "cmd-1",
		Cwd:         "/tmp",
		Command:     "ls",
		TsUnixMs:    time.Now().UnixMilli(),
		GitRepoRoot: "/" + strings.Repeat("r", MaxGitRepoRootLen),
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasRepoRootError := false
	for _, e := range result.Errors {
		if e.Field == "git_repo_root" {
			hasRepoRootError = true
		}
	}
	assert.True(t, hasRepoRootError)
}

func TestValidateGitRepoRoot_RelativePath(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId:   "session-1",
		CommandId:   "cmd-1",
		Cwd:         "/tmp",
		Command:     "ls",
		TsUnixMs:    time.Now().UnixMilli(),
		GitRepoRoot: "relative/repo",
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	hasRepoRootError := false
	for _, e := range result.Errors {
		if e.Field == "git_repo_root" {
			hasRepoRootError = true
		}
	}
	assert.True(t, hasRepoRootError)
}

func TestValidateGitRepoRoot_ValidAbsolutePath(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId:   "session-1",
		CommandId:   "cmd-1",
		Cwd:         "/tmp",
		Command:     "ls",
		TsUnixMs:    time.Now().UnixMilli(),
		GitRepoRoot: "/home/user/repo",
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
}

// --- repo_key validation (SuggestRequest) ---

func TestValidateRepoKey_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		RepoKey:   strings.Repeat("k", MaxRepoKeyLen+1),
	}
	result := ValidateSuggestRequest(req)
	require.True(t, result.HasErrors())
	hasRepoKeyError := false
	for _, e := range result.Errors {
		if e.Field == "repo_key" {
			hasRepoKeyError = true
		}
	}
	assert.True(t, hasRepoKeyError)
}

func TestValidateRepoKey_Valid(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		RepoKey:   "github.com/user/repo",
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
}

func TestValidateRepoKey_Empty_NoError(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
}

// --- include_low_confidence validation (no validation needed) ---

func TestValidateIncludeLowConfidence_True(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:            "session-1",
		Cwd:                  "/tmp",
		IncludeLowConfidence: true,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
}

func TestValidateIncludeLowConfidence_False(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:            "session-1",
		Cwd:                  "/tmp",
		IncludeLowConfidence: false,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
}

// --- buffer (command) validation in SuggestRequest ---

func TestValidateBuffer_TooLong(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
		Buffer:    strings.Repeat("x", MaxCommandLen+1),
	}
	result := ValidateSuggestRequest(req)
	require.True(t, result.HasErrors())
	hasBufferError := false
	for _, e := range result.Errors {
		if e.Field == "buffer" {
			hasBufferError = true
		}
	}
	assert.True(t, hasBufferError)
}

func TestValidateBuffer_Empty_NoError(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId: "session-1",
		Cwd:       "/tmp",
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
}

// --- Full valid requests ---

func TestValidateCommandStartRequest_AllValid(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		SessionId:   "abc-123",
		CommandId:   "cmd-456",
		TsUnixMs:    time.Now().UnixMilli(),
		Cwd:         "/home/user",
		Command:     "git status",
		GitBranch:   "main",
		GitRepoName: "clai",
		GitRepoRoot: "/home/user/clai",
	}
	result := ValidateCommandStartRequest(req)
	assert.False(t, result.HasErrors())
	assert.Len(t, result.Warnings, 0)
}

func TestValidateCommandEndRequest_AllValid(t *testing.T) {
	t.Parallel()
	req := &pb.CommandEndRequest{
		SessionId:  "abc-123",
		CommandId:  "cmd-456",
		TsUnixMs:   time.Now().UnixMilli(),
		ExitCode:   0,
		DurationMs: 1500,
	}
	result := ValidateCommandEndRequest(req)
	assert.False(t, result.HasErrors())
	assert.Len(t, result.Warnings, 0)
}

func TestValidateSuggestRequest_AllValid(t *testing.T) {
	t.Parallel()
	req := &pb.SuggestRequest{
		SessionId:            "abc-123",
		Cwd:                  "/home/user",
		Buffer:               "git sta",
		CursorPos:            7,
		MaxResults:           10,
		RepoKey:              "github.com/user/repo",
		IncludeLowConfidence: true,
	}
	result := ValidateSuggestRequest(req)
	assert.False(t, result.HasErrors())
	assert.Len(t, result.Warnings, 0)
}

// --- ValidationError methods ---

func TestValidationError_Error(t *testing.T) {
	t.Parallel()
	e := &ValidationError{Field: "session_id", Message: "is required"}
	assert.Equal(t, "E_INVALID_ARGUMENT: session_id: is required", e.Error())
}

func TestValidationError_ToAPIError(t *testing.T) {
	t.Parallel()
	e := &ValidationError{Field: "session_id", Message: "is required"}
	apiErr := e.ToAPIError()
	assert.Equal(t, "E_INVALID_ARGUMENT", apiErr.Code)
	assert.Contains(t, apiErr.Message, "session_id")
	assert.Contains(t, apiErr.Message, "is required")
	assert.False(t, apiErr.Retryable)
}

func TestValidationResult_HasErrors(t *testing.T) {
	t.Parallel()
	r := &ValidationResult{}
	assert.False(t, r.HasErrors())
	r.addError("field", "msg")
	assert.True(t, r.HasErrors())
}

func TestValidationResult_FirstError(t *testing.T) {
	t.Parallel()
	r := &ValidationResult{}
	assert.Nil(t, r.FirstError())
	r.addError("first", "msg1")
	r.addError("second", "msg2")
	assert.Equal(t, "first", r.FirstError().Field)
}

func TestNewValidationErrorResponse(t *testing.T) {
	t.Parallel()
	ve := &ValidationError{Field: "cwd", Message: "must be absolute path"}
	resp := NewValidationErrorResponse(ve)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.Error)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.Code)
	assert.Equal(t, "cwd", resp.Field)
	assert.Equal(t, "must be absolute path", resp.Message)
}

// --- Multiple errors in one request ---

func TestValidateCommandStartRequest_MultipleErrors(t *testing.T) {
	t.Parallel()
	req := &pb.CommandStartRequest{
		// All required fields missing/invalid
		SessionId: "",
		CommandId: "",
		Cwd:       "",
		Command:   "",
		TsUnixMs:  0,
	}
	result := ValidateCommandStartRequest(req)
	require.True(t, result.HasErrors())
	// Should have errors for: session_id, command_id, cwd, command, ts_unix_ms
	assert.GreaterOrEqual(t, len(result.Errors), 5)

	fieldSet := make(map[string]bool)
	for _, e := range result.Errors {
		fieldSet[e.Field] = true
	}
	assert.True(t, fieldSet["session_id"])
	assert.True(t, fieldSet["command_id"])
	assert.True(t, fieldSet["cwd"])
	assert.True(t, fieldSet["command"])
	assert.True(t, fieldSet["ts_unix_ms"])
}

// --- CleanStringField ---

func TestCleanStringField(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", CleanStringField("  hello  "))
	assert.Equal(t, "hello", CleanStringField("hello"))
	assert.Equal(t, "", CleanStringField("   "))
	assert.Equal(t, "hello world", CleanStringField("\thello world\n"))
}

// --- Boundary clamping comprehensive test ---

func TestBoundaryClamping_DurationMs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    int64
		expected int64
		warnLen  int
	}{
		{"zero", 0, 0, 0},
		{"positive normal", 5000, 5000, 0},
		{"at max", int64(MaxDurationMs), int64(MaxDurationMs), 0},
		{"above max", int64(MaxDurationMs) + 1, int64(MaxDurationMs), 1},
		{"negative", -1, 0, 1},
		{"very negative", -999999, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &pb.CommandEndRequest{
				SessionId:  "session-1",
				CommandId:  "cmd-1",
				TsUnixMs:   time.Now().UnixMilli(),
				DurationMs: tt.input,
			}
			result := ValidateCommandEndRequest(req)
			assert.Equal(t, tt.expected, req.DurationMs)
			durationWarnings := 0
			for _, w := range result.Warnings {
				if w.Field == "duration_ms" {
					durationWarnings++
				}
			}
			assert.Equal(t, tt.warnLen, durationWarnings)
		})
	}
}

func TestBoundaryClamping_MaxResults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    int32
		expected int32
	}{
		{"zero defaults to 10", 0, int32(DefaultLimit)},
		{"below min clamps to 1", -5, int32(MinLimit)},
		{"at min", 1, 1},
		{"normal", 25, 25},
		{"at max", 50, 50},
		{"above max clamps to 50", 100, int32(MaxLimit)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &pb.SuggestRequest{
				SessionId:  "session-1",
				Cwd:        "/tmp",
				MaxResults: tt.input,
			}
			ValidateSuggestRequest(req)
			assert.Equal(t, tt.expected, req.MaxResults)
		})
	}
}

func TestBoundaryClamping_CursorPos(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		buffer   string
		input    int32
		expected int32
	}{
		{"zero with empty buffer", "", 0, 0},
		{"zero with content", "hello", 0, 0},
		{"at buffer length", "hello", 5, 5},
		{"exceeds buffer length", "hello", 10, 5},
		{"negative clamped to 0", "hello", -3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &pb.SuggestRequest{
				SessionId: "session-1",
				Cwd:       "/tmp",
				Buffer:    tt.buffer,
				CursorPos: tt.input,
			}
			ValidateSuggestRequest(req)
			assert.Equal(t, tt.expected, req.CursorPos)
		})
	}
}
