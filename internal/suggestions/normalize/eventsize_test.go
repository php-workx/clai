package normalize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnforceEventSize_WithinLimit(t *testing.T) {
	cmd := "ls -la"
	result, truncated := EnforceEventSize(cmd, 1024)
	assert.Equal(t, cmd, result)
	assert.False(t, truncated)
}

func TestEnforceEventSize_ExceedsLimit(t *testing.T) {
	cmd := strings.Repeat("x", 200)
	result, truncated := EnforceEventSize(cmd, 100)
	assert.Len(t, result, 100)
	assert.True(t, truncated)
}

func TestEnforceEventSize_ExactLimit(t *testing.T) {
	cmd := strings.Repeat("x", 100)
	result, truncated := EnforceEventSize(cmd, 100)
	assert.Len(t, result, 100)
	assert.False(t, truncated)
}

func TestEnforceEventSize_DefaultLimit(t *testing.T) {
	shortCmd := "ls -la"
	result, truncated := EnforceEventSize(shortCmd, 0)
	assert.Equal(t, shortCmd, result)
	assert.False(t, truncated)

	longCmd := strings.Repeat("x", 20000)
	result, truncated = EnforceEventSize(longCmd, 0)
	assert.Len(t, result, DefaultMaxEventSize)
	assert.True(t, truncated)

	result, truncated = EnforceEventSize(longCmd, -1)
	assert.Len(t, result, DefaultMaxEventSize)
	assert.True(t, truncated)
}

func TestEnforceEventSize_PreservesContent(t *testing.T) {
	cmd := "echo hello world"
	result, truncated := EnforceEventSize(cmd, 10)
	assert.True(t, truncated)
	assert.Equal(t, "echo hello", result)
}

func TestEnforceEventSize_EmptyString(t *testing.T) {
	result, truncated := EnforceEventSize("", 100)
	assert.Equal(t, "", result)
	assert.False(t, truncated)
}
