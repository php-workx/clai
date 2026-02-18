package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigManager_NewConfigManager(t *testing.T) {
	t.Parallel()

	cm := NewConfigManager(ConfigManagerOptions{})
	assert.NotNil(t, cm)
	assert.Contains(t, cm.path, "discovery.yaml")
}

func TestConfigManager_Load_NotFound(t *testing.T) {
	t.Parallel()

	cm := NewConfigManager(ConfigManagerOptions{
		Path: "/nonexistent/path/discovery.yaml",
	})

	err := cm.Load()
	require.NoError(t, err) // Missing config is not an error
	assert.NotNil(t, cm.Get())
	assert.Empty(t, cm.GetEntries())
}

func TestConfigManager_Load_ValidConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "discovery.yaml")

	config := `
- file_pattern: "Justfile"
  kind: "just"
  runner: "just --list --json"
  parser:
    type: "json_keys"
    path: "recipes"
  timeout_ms: 300
- file_pattern: "*.gradle"
  kind: "gradle"
  runner: "gradle tasks --all"
  parser:
    type: "regex_lines"
    pattern: "^(\\w+)\\s+-\\s+(.+)$"
`
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0644))

	cm := NewConfigManager(ConfigManagerOptions{Path: configPath})
	err := cm.Load()
	require.NoError(t, err)

	entries := cm.GetEntries()
	assert.Len(t, entries, 2)

	assert.Equal(t, "Justfile", entries[0].FilePattern)
	assert.Equal(t, "just", entries[0].Kind)
	assert.Equal(t, "just --list --json", entries[0].Runner)
	assert.Equal(t, ParserTypeJSONKeys, entries[0].Parser.Type)
	assert.Equal(t, "recipes", entries[0].Parser.Path)
	assert.Equal(t, 300, entries[0].TimeoutMs)

	assert.Equal(t, "*.gradle", entries[1].FilePattern)
	assert.Equal(t, "gradle", entries[1].Kind)
	assert.Equal(t, ParserTypeRegexLines, entries[1].Parser.Type)
	assert.NotNil(t, entries[1].Parser.compiledRegex)
}

func TestConfigManager_Load_InvalidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "discovery.yaml")

	require.NoError(t, os.WriteFile(configPath, []byte("invalid: yaml: syntax: ["), 0644))

	cm := NewConfigManager(ConfigManagerOptions{Path: configPath})
	err := cm.Load()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigInvalid)
}

func TestConfigManager_Load_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		errMsg string
	}{
		{
			name:   "missing file_pattern",
			config: `[{kind: "just", runner: "just --list", parser: {type: "json_keys", path: "."}}]`,
			errMsg: "file_pattern is required",
		},
		{
			name:   "missing kind",
			config: `[{file_pattern: "Justfile", runner: "just --list", parser: {type: "json_keys", path: "."}}]`,
			errMsg: "kind is required",
		},
		{
			name:   "missing runner",
			config: `[{file_pattern: "Justfile", kind: "just", parser: {type: "json_keys", path: "."}}]`,
			errMsg: "runner is required",
		},
		{
			name:   "missing parser type",
			config: `[{file_pattern: "Justfile", kind: "just", runner: "just --list", parser: {path: "."}}]`,
			errMsg: "parser.type is required",
		},
		{
			name:   "json_keys missing path",
			config: `[{file_pattern: "Justfile", kind: "just", runner: "just --list", parser: {type: "json_keys"}}]`,
			errMsg: "parser.path is required",
		},
		{
			name:   "regex_lines missing pattern",
			config: `[{file_pattern: "Justfile", kind: "just", runner: "just --list", parser: {type: "regex_lines"}}]`,
			errMsg: "parser.pattern is required",
		},
		{
			name:   "invalid regex pattern",
			config: `[{file_pattern: "Justfile", kind: "just", runner: "just --list", parser: {type: "regex_lines", pattern: "[invalid"}}]`,
			errMsg: "invalid regex pattern",
		},
		{
			name:   "unknown parser type",
			config: `[{file_pattern: "Justfile", kind: "just", runner: "just --list", parser: {type: "unknown"}}]`,
			errMsg: "unknown parser type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "discovery.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(tc.config), 0644))

			cm := NewConfigManager(ConfigManagerOptions{Path: configPath})
			err := cm.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestConfigManager_GetEntriesForFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "discovery.yaml")

	config := `
- file_pattern: "Justfile"
  kind: "just"
  runner: "just --list"
  parser:
    type: "json_keys"
    path: "."
- file_pattern: "*.gradle"
  kind: "gradle"
  runner: "gradle tasks"
  parser:
    type: "regex_lines"
    pattern: "^(\\w+)$"
- file_pattern: "package.json"
  kind: "npm"
  runner: "cat package.json"
  parser:
    type: "json_keys"
    path: "scripts"
`
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0644))

	cm := NewConfigManager(ConfigManagerOptions{Path: configPath})
	require.NoError(t, cm.Load())

	// Test exact match
	entries := cm.GetEntriesForFile("Justfile")
	assert.Len(t, entries, 1)
	assert.Equal(t, "just", entries[0].Kind)

	// Test glob match
	entries = cm.GetEntriesForFile("build.gradle")
	assert.Len(t, entries, 1)
	assert.Equal(t, "gradle", entries[0].Kind)

	// Test no match
	entries = cm.GetEntriesForFile("Makefile")
	assert.Empty(t, entries)

	// Test another exact match
	entries = cm.GetEntriesForFile("package.json")
	assert.Len(t, entries, 1)
	assert.Equal(t, "npm", entries[0].Kind)
}

func TestConfigManager_Reload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "discovery.yaml")

	config1 := `
- file_pattern: "Justfile"
  kind: "just"
  runner: "just --list"
  parser:
    type: "json_keys"
    path: "."
`
	require.NoError(t, os.WriteFile(configPath, []byte(config1), 0644))

	reloadCalled := false
	cm := NewConfigManager(ConfigManagerOptions{
		Path: configPath,
		OnReload: func(cfg *Config) {
			reloadCalled = true
		},
	})

	require.NoError(t, cm.Load())
	assert.Len(t, cm.GetEntries(), 1)

	// Update config
	config2 := `
- file_pattern: "Justfile"
  kind: "just"
  runner: "just --list"
  parser:
    type: "json_keys"
    path: "."
- file_pattern: "Taskfile.yml"
  kind: "task"
  runner: "task --list"
  parser:
    type: "regex_lines"
    pattern: "^\\* (\\w+):"
`
	require.NoError(t, os.WriteFile(configPath, []byte(config2), 0644))

	// Reload
	require.NoError(t, cm.Reload())
	assert.True(t, reloadCalled)
	assert.Len(t, cm.GetEntries(), 2)
}

func TestConfigManager_ObjectFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "discovery.yaml")

	// Test with object format (entries key)
	config := `
entries:
  - file_pattern: "Justfile"
    kind: "just"
    runner: "just --list"
    parser:
      type: "json_keys"
      path: "recipes"
`
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0644))

	cm := NewConfigManager(ConfigManagerOptions{Path: configPath})
	require.NoError(t, cm.Load())

	entries := cm.GetEntries()
	assert.Len(t, entries, 1)
	assert.Equal(t, "Justfile", entries[0].FilePattern)
}

func TestExpandPath(t *testing.T) {
	t.Parallel()

	// Test tilde expansion
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	expanded := expandPath("~/.config/clai/discovery.yaml")
	assert.Equal(t, filepath.Join(home, ".config/clai/discovery.yaml"), expanded)

	// Test no tilde
	expanded = expandPath("/absolute/path")
	assert.Equal(t, "/absolute/path", expanded)
}

func TestParserTypeConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "json_keys", ParserTypeJSONKeys)
	assert.Equal(t, "json_array", ParserTypeJSONArray)
	assert.Equal(t, "regex_lines", ParserTypeRegexLines)
	assert.Equal(t, "make_qp", ParserTypeMakeQP)
}
