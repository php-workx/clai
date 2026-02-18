package ingest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/event"
)

// validEventJSON returns valid JSON for a CommandEvent with optional overrides.
func validEventJSON(t *testing.T, overrides map[string]interface{}) []byte {
	t.Helper()

	m := map[string]interface{}{
		"v":          1,
		"type":       "command_end",
		"ts":         int64(1730000000123),
		"session_id": "test-session-123",
		"shell":      "zsh",
		"cwd":        "/home/user/project",
		"cmd_raw":    "git status",
		"exit_code":  0,
		"ephemeral":  false,
	}

	for k, v := range overrides {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}

	data, err := json.Marshal(m)
	require.NoError(t, err)
	return data
}

func TestParseEvent_ValidMinimal(t *testing.T) {
	data := validEventJSON(t, nil)

	ev, err := ParseEvent(data)
	require.NoError(t, err)
	require.NotNil(t, ev)

	assert.Equal(t, 1, ev.Version)
	assert.Equal(t, "command_end", ev.Type)
	assert.Equal(t, int64(1730000000123), ev.Ts)
	assert.Equal(t, "test-session-123", ev.SessionID)
	assert.Equal(t, event.ShellZsh, ev.Shell)
	assert.Equal(t, "/home/user/project", ev.Cwd)
	assert.Equal(t, "git status", ev.CmdRaw)
	assert.Equal(t, 0, ev.ExitCode)
	assert.Nil(t, ev.DurationMs)
	assert.False(t, ev.Ephemeral)
}

func TestParseEvent_ValidFull(t *testing.T) {
	data := validEventJSON(t, map[string]interface{}{
		"duration_ms": int64(1500),
		"ephemeral":   true,
	})

	ev, err := ParseEvent(data)
	require.NoError(t, err)
	require.NotNil(t, ev)

	require.NotNil(t, ev.DurationMs)
	assert.Equal(t, int64(1500), *ev.DurationMs)
	assert.True(t, ev.Ephemeral)
}

func TestParseEvent_AllShells(t *testing.T) {
	tests := []struct {
		shell    string
		expected event.Shell
	}{
		{"bash", event.ShellBash},
		{"zsh", event.ShellZsh},
		{"fish", event.ShellFish},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			data := validEventJSON(t, map[string]interface{}{
				"shell": tt.shell,
			})

			ev, err := ParseEvent(data)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, ev.Shell)
		})
	}
}

func TestParseEvent_InvalidVersion(t *testing.T) {
	tests := []struct {
		name    string
		version interface{}
	}{
		{"version 0", 0},
		{"version 2", 2},
		{"version -1", -1},
		{"version 99", 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validEventJSON(t, map[string]interface{}{
				"v": tt.version,
			})

			ev, err := ParseEvent(data)
			assert.Nil(t, ev)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidVersion))
		})
	}
}

func TestParseEvent_InvalidShell(t *testing.T) {
	tests := []struct {
		name  string
		shell string
	}{
		{"uppercase bash", "BASH"},
		{"uppercase zsh", "ZSH"},
		{"uppercase fish", "FISH"},
		{"powershell", "powershell"},
		{"sh", "sh"},
		{"ksh", "ksh"},
		{"tcsh", "tcsh"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validEventJSON(t, map[string]interface{}{
				"shell": tt.shell,
			})

			ev, err := ParseEvent(data)
			assert.Nil(t, ev)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidShell))
		})
	}
}

func TestParseEvent_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		fieldToOmit string
		expectedErr error
	}{
		{"missing type", "type", ErrMissingType},
		{"missing session_id", "session_id", ErrMissingSessionID},
		{"missing shell", "shell", ErrMissingShell},
		{"missing cwd", "cwd", ErrMissingCwd},
		{"missing cmd_raw", "cmd_raw", ErrMissingCmdRaw},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validEventJSON(t, map[string]interface{}{
				tt.fieldToOmit: nil, // nil removes the field
			})

			ev, err := ParseEvent(data)
			assert.Nil(t, ev)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, tt.expectedErr))
		})
	}
}

func TestParseEvent_EmptyRequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		field       string
		value       interface{}
		expectedErr error
	}{
		{"empty type", "type", "", ErrMissingType},
		{"empty session_id", "session_id", "", ErrMissingSessionID},
		{"empty shell", "shell", "", ErrMissingShell},
		{"empty cwd", "cwd", "", ErrMissingCwd},
		{"empty cmd_raw", "cmd_raw", "", ErrMissingCmdRaw},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validEventJSON(t, map[string]interface{}{
				tt.field: tt.value,
			})

			ev, err := ParseEvent(data)
			assert.Nil(t, ev)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, tt.expectedErr))
		})
	}
}

func TestParseEvent_MissingTimestamp(t *testing.T) {
	// When ts is omitted, JSON unmarshal sets it to 0
	data := validEventJSON(t, map[string]interface{}{
		"ts": nil,
	})

	ev, err := ParseEvent(data)
	assert.Nil(t, ev)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingTimestamp))
}

func TestParseEvent_ZeroTimestamp(t *testing.T) {
	data := validEventJSON(t, map[string]interface{}{
		"ts": 0,
	})

	ev, err := ParseEvent(data)
	assert.Nil(t, ev)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingTimestamp))
}

func TestParseEvent_OptionalFields(t *testing.T) {
	t.Run("duration_ms omitted", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"duration_ms": nil,
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.Nil(t, ev.DurationMs)
	})

	t.Run("duration_ms zero", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"duration_ms": int64(0),
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		require.NotNil(t, ev.DurationMs)
		assert.Equal(t, int64(0), *ev.DurationMs)
	})

	t.Run("ephemeral omitted defaults to false", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"ephemeral": nil,
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.False(t, ev.Ephemeral)
	})

	t.Run("ephemeral true", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"ephemeral": true,
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.True(t, ev.Ephemeral)
	})
}

func TestParseEvent_ExitCodeVariations(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
	}{
		{"exit code 0", 0},
		{"exit code 1", 1},
		{"exit code 127", 127},
		{"exit code 255", 255},
		{"exit code -1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validEventJSON(t, map[string]interface{}{
				"exit_code": tt.exitCode,
			})

			ev, err := ParseEvent(data)
			require.NoError(t, err)
			assert.Equal(t, tt.exitCode, ev.ExitCode)
		})
	}
}

func TestParseEvent_InvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty input", []byte{}},
		{"nil input", nil},
		{"invalid json", []byte(`{invalid}`)},
		{"incomplete json", []byte(`{"v": 1`)},
		{"just a number", []byte(`123`)},
		{"just a string", []byte(`"hello"`)},
		{"array instead of object", []byte(`[1, 2, 3]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := ParseEvent(tt.data)
			assert.Nil(t, ev)
			assert.Error(t, err)
		})
	}
}

func TestParseEvent_SpecialCharacters(t *testing.T) {
	t.Run("command with quotes", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"cmd_raw": `git commit -m "fix: \"quoted\" work"`,
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.Equal(t, `git commit -m "fix: \"quoted\" work"`, ev.CmdRaw)
	})

	t.Run("command with newlines", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"cmd_raw": "echo 'line1\nline2'",
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.Contains(t, ev.CmdRaw, "\n")
	})

	t.Run("command with unicode", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"cmd_raw": "echo 'hello' ",
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.Contains(t, ev.CmdRaw, "")
	})

	t.Run("cwd with spaces", func(t *testing.T) {
		data := validEventJSON(t, map[string]interface{}{
			"cwd": "/home/user/My Documents/project",
		})

		ev, err := ParseEvent(data)
		require.NoError(t, err)
		assert.Equal(t, "/home/user/My Documents/project", ev.Cwd)
	})
}

func TestParseEventLine(t *testing.T) {
	t.Run("with trailing newline", func(t *testing.T) {
		data := validEventJSON(t, nil)
		data = append(data, '\n')

		ev, err := ParseEventLine(data)
		require.NoError(t, err)
		require.NotNil(t, ev)
	})

	t.Run("with crlf", func(t *testing.T) {
		data := validEventJSON(t, nil)
		data = append(data, '\r', '\n')

		ev, err := ParseEventLine(data)
		require.NoError(t, err)
		require.NotNil(t, ev)
	})

	t.Run("without newline", func(t *testing.T) {
		data := validEventJSON(t, nil)

		ev, err := ParseEventLine(data)
		require.NoError(t, err)
		require.NotNil(t, ev)
	})
}

func TestValidateEvent_NilEvent(t *testing.T) {
	err := ValidateEvent(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestValidateEvent_ValidEvent(t *testing.T) {
	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1730000000123,
		SessionID: "session-123",
		Shell:     event.ShellBash,
		Cwd:       "/home/user",
		CmdRaw:    "ls -la",
		ExitCode:  0,
	}

	err := ValidateEvent(ev)
	assert.NoError(t, err)
}

func TestParseEvent_LargeCommand(t *testing.T) {
	largeCmd := strings.Repeat("x", 40000)
	data := validEventJSON(t, map[string]interface{}{
		"cmd_raw": largeCmd,
	})

	ev, err := ParseEvent(data)
	require.NoError(t, err)
	assert.Equal(t, largeCmd, ev.CmdRaw)
}

// FuzzParseEvent performs fuzz testing on the ParseEvent function.
// Run with: go test -fuzz=FuzzParseEvent -fuzztime=30s
func FuzzParseEvent(f *testing.F) {
	// Seed with valid JSON
	validJSON := `{"v":1,"type":"command_end","ts":1730000000123,"session_id":"test","shell":"zsh","cwd":"/home","cmd_raw":"ls","exit_code":0}`
	f.Add([]byte(validJSON))

	// Seed with variations
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"v":0}`))
	f.Add([]byte(`{"v":1,"type":"","ts":0}`))
	f.Add([]byte(`{"v":1,"type":"command_end","ts":123,"session_id":"s","shell":"invalid","cwd":"/","cmd_raw":"x","exit_code":0}`))
	f.Add([]byte(`not json`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// ParseEvent should never panic, only return errors
		ev, err := ParseEvent(data)

		// If we got an event, validate it's internally consistent
		if ev != nil && err == nil {
			// Ensure required fields are present
			assert.Equal(t, event.EventVersion, ev.Version)
			assert.NotEmpty(t, ev.Type)
			assert.NotZero(t, ev.Ts)
			assert.NotEmpty(t, ev.SessionID)
			assert.NotEmpty(t, ev.Shell)
			assert.NotEmpty(t, ev.Cwd)
			assert.NotEmpty(t, ev.CmdRaw)
			assert.True(t, event.ValidShell(string(ev.Shell)))
		}
	})
}

// FuzzParseEventLine performs fuzz testing on ParseEventLine.
func FuzzParseEventLine(f *testing.F) {
	validJSON := `{"v":1,"type":"command_end","ts":1730000000123,"session_id":"test","shell":"bash","cwd":"/","cmd_raw":"pwd","exit_code":0}`
	f.Add([]byte(validJSON + "\n"))
	f.Add([]byte(validJSON + "\r\n"))
	f.Add([]byte(validJSON))
	f.Add([]byte("\n"))
	f.Add([]byte("\r\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// ParseEventLine should never panic
		_, _ = ParseEventLine(data)
	})
}

// Benchmark tests
func BenchmarkParseEvent(b *testing.B) {
	data := []byte(`{"v":1,"type":"command_end","ts":1730000000123,"session_id":"bench-session","shell":"zsh","cwd":"/home/user/project","cmd_raw":"git status","exit_code":0,"duration_ms":150,"ephemeral":false}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseEvent(data)
	}
}

func BenchmarkValidateEvent(b *testing.B) {
	duration := int64(150)
	ev := &event.CommandEvent{
		Version:    1,
		Type:       event.EventTypeCommandEnd,
		Ts:         1730000000123,
		SessionID:  "bench-session",
		Shell:      event.ShellZsh,
		Cwd:        "/home/user/project",
		CmdRaw:     "git status",
		ExitCode:   0,
		DurationMs: &duration,
		Ephemeral:  false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateEvent(ev)
	}
}
