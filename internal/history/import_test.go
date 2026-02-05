package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Bash history import tests ---

func TestImportBashHistory_Basic(t *testing.T) {
	content := `ls -la
git status
echo hello
`
	path := writeTempFile(t, content)
	entries, err := ImportBashHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 3)
	assert.Equal(t, "ls -la", entries[0].Command)
	assert.Equal(t, "git status", entries[1].Command)
	assert.Equal(t, "echo hello", entries[2].Command)

	// No timestamps in basic format
	assert.True(t, entries[0].Timestamp.IsZero())
}

func TestImportBashHistory_WithTimestamps(t *testing.T) {
	// Bash stores timestamps as #<unix_ts> on the line before command
	content := `#1706000001
ls -la
#1706000002
git status
echo hello
`
	path := writeTempFile(t, content)
	entries, err := ImportBashHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 3)

	// First command has timestamp
	assert.Equal(t, "ls -la", entries[0].Command)
	assert.Equal(t, time.Unix(1706000001, 0), entries[0].Timestamp)

	// Second command has timestamp
	assert.Equal(t, "git status", entries[1].Command)
	assert.Equal(t, time.Unix(1706000002, 0), entries[1].Timestamp)

	// Third command has no timestamp (no preceding #ts line)
	assert.Equal(t, "echo hello", entries[2].Command)
	assert.True(t, entries[2].Timestamp.IsZero())
}

func TestImportBashHistory_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	entries, err := ImportBashHistory(path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestImportBashHistory_NonExistent(t *testing.T) {
	entries, err := ImportBashHistory("/nonexistent/path/to/history")
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestImportBashHistory_SkipsEmptyLines(t *testing.T) {
	content := `ls -la

git status

`
	path := writeTempFile(t, content)
	entries, err := ImportBashHistory(path)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestImportBashHistory_CommentNotTimestamp(t *testing.T) {
	// Lines starting with # but not followed by digits are comments
	content := `# This is a comment
ls -la
#notanumber
git status
`
	path := writeTempFile(t, content)
	entries, err := ImportBashHistory(path)
	require.NoError(t, err)

	// Comments are treated as commands (bash doesn't distinguish)
	assert.Len(t, entries, 4)
}

// --- Zsh history import tests ---

func TestImportZshHistory_Extended(t *testing.T) {
	// Extended history format: `: <timestamp>:<duration>;<command>`
	content := `: 1706000001:0;ls -la
: 1706000002:5;git status
: 1706000003:10;echo hello
`
	path := writeTempFile(t, content)
	entries, err := ImportZshHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 3)

	assert.Equal(t, "ls -la", entries[0].Command)
	assert.Equal(t, time.Unix(1706000001, 0), entries[0].Timestamp)

	assert.Equal(t, "git status", entries[1].Command)
	assert.Equal(t, time.Unix(1706000002, 0), entries[1].Timestamp)

	assert.Equal(t, "echo hello", entries[2].Command)
	assert.Equal(t, time.Unix(1706000003, 0), entries[2].Timestamp)
}

func TestImportZshHistory_Plain(t *testing.T) {
	// Plain format without timestamps
	content := `ls -la
git status
echo hello
`
	path := writeTempFile(t, content)
	entries, err := ImportZshHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 3)
	assert.Equal(t, "ls -la", entries[0].Command)
	assert.True(t, entries[0].Timestamp.IsZero())
}

func TestImportZshHistory_Multiline(t *testing.T) {
	// Multiline commands use backslash continuation
	content := `: 1706000001:0;docker run \
--name test \
alpine
: 1706000002:0;ls -la
`
	path := writeTempFile(t, content)
	entries, err := ImportZshHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 2)
	assert.Equal(t, "docker run \n--name test \nalpine", entries[0].Command)
	assert.Equal(t, time.Unix(1706000001, 0), entries[0].Timestamp)
}

func TestImportZshHistory_EscapedBackslash(t *testing.T) {
	// Double backslash at end is literal, not continuation
	// The zsh history file stores the command as-is (with both backslashes)
	content := `: 1706000001:0;echo path\\
: 1706000002:0;ls -la
`
	path := writeTempFile(t, content)
	entries, err := ImportZshHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 2)
	// Zsh stores commands literally, double backslash is preserved
	assert.Equal(t, `echo path\\`, entries[0].Command)
}

func TestImportZshHistory_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	entries, err := ImportZshHistory(path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// --- Fish history import tests ---

func TestImportFishHistory_Basic(t *testing.T) {
	content := `- cmd: ls -la
  when: 1706000001
- cmd: git status
  when: 1706000002
- cmd: echo hello
  when: 1706000003
`
	path := writeTempFile(t, content)
	entries, err := ImportFishHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 3)

	assert.Equal(t, "ls -la", entries[0].Command)
	assert.Equal(t, time.Unix(1706000001, 0), entries[0].Timestamp)

	assert.Equal(t, "git status", entries[1].Command)
	assert.Equal(t, time.Unix(1706000002, 0), entries[1].Timestamp)

	assert.Equal(t, "echo hello", entries[2].Command)
	assert.Equal(t, time.Unix(1706000003, 0), entries[2].Timestamp)
}

func TestImportFishHistory_WithPaths(t *testing.T) {
	// Fish also stores paths, which we ignore
	content := `- cmd: ls -la
  when: 1706000001
  paths:
    - /home/user
- cmd: git status
  when: 1706000002
`
	path := writeTempFile(t, content)
	entries, err := ImportFishHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 2)
	assert.Equal(t, "ls -la", entries[0].Command)
	assert.Equal(t, "git status", entries[1].Command)
}

func TestImportFishHistory_Escapes(t *testing.T) {
	// Fish escapes backslashes as \\ and newlines as \n (literal characters)
	// In Go strings, we need to double-escape: \\n in file = \ and n chars
	content := "- cmd: echo hello\\nworld\n  when: 1706000001\n- cmd: echo path\\\\name\n  when: 1706000002\n"
	path := writeTempFile(t, content)
	entries, err := ImportFishHistory(path)
	require.NoError(t, err)

	assert.Len(t, entries, 2)
	// \n in fish file becomes actual newline
	assert.Equal(t, "echo hello\nworld", entries[0].Command)
	// \\ in fish file becomes single backslash
	assert.Equal(t, `echo path\name`, entries[1].Command)
}

func TestImportFishHistory_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	entries, err := ImportFishHistory(path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestImportFishHistory_NonExistent(t *testing.T) {
	entries, err := ImportFishHistory("/nonexistent/path/to/history")
	require.NoError(t, err)
	assert.Nil(t, entries)
}

// --- Limit tests ---

func TestImportBashHistory_LimitEntries(t *testing.T) {
	// Create more than MaxImportEntries
	var content string
	for i := 0; i < MaxImportEntries+100; i++ {
		content += "command\n"
	}
	path := writeTempFile(t, content)
	entries, err := ImportBashHistory(path)
	require.NoError(t, err)

	// Should be limited to MaxImportEntries
	assert.Len(t, entries, MaxImportEntries)
}

func TestTrimToLimit(t *testing.T) {
	entries := []ImportEntry{
		{Command: "a"},
		{Command: "b"},
		{Command: "c"},
		{Command: "d"},
		{Command: "e"},
	}

	// Trim to 3 should keep last 3
	result := trimToLimit(entries, 3)
	assert.Len(t, result, 3)
	assert.Equal(t, "c", result[0].Command)
	assert.Equal(t, "d", result[1].Command)
	assert.Equal(t, "e", result[2].Command)

	// Trim to more than length should return original
	result = trimToLimit(entries, 10)
	assert.Len(t, result, 5)
}

// --- ImportForShell tests ---

func TestImportForShell_Auto(t *testing.T) {
	// Set SHELL to zsh for testing
	oldShell := os.Getenv("SHELL")
	defer os.Setenv("SHELL", oldShell)

	os.Setenv("SHELL", "/bin/zsh")
	shell := DetectShell()
	assert.Equal(t, "zsh", shell)

	os.Setenv("SHELL", "/usr/local/bin/bash")
	shell = DetectShell()
	assert.Equal(t, "bash", shell)

	os.Setenv("SHELL", "/usr/bin/fish")
	shell = DetectShell()
	assert.Equal(t, "fish", shell)
}

func TestDecodeFishEscapes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello\\nworld", "hello\nworld"},
		{"path\\\\name", `path\name`},
		{"a\\nb\\nc", "a\nb\nc"},
		{"trailing\\", "trailing\\"},
		{"", ""},
	}

	for _, tc := range tests {
		result := decodeFishEscapes(tc.input)
		assert.Equal(t, tc.expected, result, "input: %q", tc.input)
	}
}

// --- Helper functions ---

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}
