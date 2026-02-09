package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFlags_EmptyArgs(t *testing.T) {
	result := parseFlags([]string{})
	assert.Empty(t, result)
}

func TestParseFlags_KeyEqualsValue(t *testing.T) {
	result := parseFlags([]string{"--session-id=abc123", "--cwd=/tmp"})
	assert.Equal(t, "abc123", result["session-id"])
	assert.Equal(t, "/tmp", result["cwd"])
}

func TestParseFlags_KeyValueSeparate(t *testing.T) {
	result := parseFlags([]string{"--session-id", "abc123", "--cwd", "/tmp"})
	assert.Equal(t, "abc123", result["session-id"])
	assert.Equal(t, "/tmp", result["cwd"])
}

func TestParseFlags_BooleanFlag(t *testing.T) {
	result := parseFlags([]string{"--if-not-exists", "--force"})
	assert.Equal(t, "true", result["if-not-exists"])
	assert.Equal(t, "true", result["force"])
}

func TestParseFlags_MixedFormats(t *testing.T) {
	result := parseFlags([]string{
		"--session-id=abc123",
		"--cwd", "/tmp",
		"--force",
		"--shell=bash",
	})
	assert.Equal(t, "abc123", result["session-id"])
	assert.Equal(t, "/tmp", result["cwd"])
	assert.Equal(t, "true", result["force"])
	assert.Equal(t, "bash", result["shell"])
}

func TestParseFlags_DuplicateKeysLastWins(t *testing.T) {
	result := parseFlags([]string{"--key=first", "--key=second"})
	assert.Equal(t, "second", result["key"])
}

func TestParseFlags_EmptyValue(t *testing.T) {
	result := parseFlags([]string{"--key="})
	assert.Equal(t, "", result["key"])
}

func TestParseFlags_ValueWithEquals(t *testing.T) {
	// Value containing equals sign should be preserved
	result := parseFlags([]string{"--command=echo foo=bar"})
	assert.Equal(t, "echo foo=bar", result["command"])
}

func TestParseFlags_IgnoresNonFlags(t *testing.T) {
	result := parseFlags([]string{"positional", "--flag=value", "another"})
	assert.Equal(t, "value", result["flag"])
	assert.NotContains(t, result, "positional")
	assert.NotContains(t, result, "another")
}

func TestParseFlags_BooleanAtEnd(t *testing.T) {
	// Boolean flag at end of args (no next arg to check)
	result := parseFlags([]string{"--verbose"})
	assert.Equal(t, "true", result["verbose"])
}

func TestParseFlags_ValueStartsWithDash(t *testing.T) {
	// If next arg starts with --, treat current as boolean
	result := parseFlags([]string{"--flag1", "--flag2"})
	assert.Equal(t, "true", result["flag1"])
	assert.Equal(t, "true", result["flag2"])
}

func TestParseFlags_SingleDashValue(t *testing.T) {
	result := parseFlags([]string{"--flag", "-value"})
	assert.Equal(t, "-value", result["flag"])
}
