package event

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidShell(t *testing.T) {
	tests := []struct {
		shell string
		valid bool
	}{
		{"bash", true},
		{"zsh", true},
		{"fish", true},
		{"BASH", false}, // case sensitive
		{"ZSH", false},
		{"FISH", false},
		{"powershell", false},
		{"sh", false},
		{"", false},
		{"ksh", false},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			assert.Equal(t, tt.valid, ValidShell(tt.shell))
		})
	}
}

func TestNewCommandEvent(t *testing.T) {
	ev := NewCommandEvent()

	assert.Equal(t, EventVersion, ev.Version)
	assert.Equal(t, EventTypeCommandEnd, ev.Type)
	assert.Equal(t, int64(0), ev.TS)
	assert.Equal(t, "", ev.SessionID)
	assert.Equal(t, Shell(""), ev.Shell)
	assert.Equal(t, "", ev.Cwd)
	assert.Equal(t, "", ev.CmdRaw)
	assert.Equal(t, 0, ev.ExitCode)
	assert.Nil(t, ev.DurationMs)
	assert.False(t, ev.Ephemeral)
}

func TestCommandEventJSONSerialization(t *testing.T) { //nolint:funlen // roundtrip serialization subtests
	t.Run("minimal event", func(t *testing.T) {
		ev := &CommandEvent{
			Version:   1,
			Type:      EventTypeCommandEnd,
			TS:        1730000000123,
			SessionID: "abc-123",
			Shell:     ShellZsh,
			Cwd:       "/home/user",
			CmdRaw:    "git status",
			ExitCode:  0,
			Ephemeral: false,
		}

		data, err := json.Marshal(ev)
		require.NoError(t, err)

		// Verify expected JSON structure
		var m map[string]interface{}
		err = json.Unmarshal(data, &m)
		require.NoError(t, err)

		assert.Equal(t, float64(1), m["v"])
		assert.Equal(t, "command_end", m["type"])
		assert.Equal(t, float64(1730000000123), m["ts"])
		assert.Equal(t, "abc-123", m["session_id"])
		assert.Equal(t, "zsh", m["shell"])
		assert.Equal(t, "/home/user", m["cwd"])
		assert.Equal(t, "git status", m["cmd_raw"])
		assert.Equal(t, float64(0), m["exit_code"])
		assert.Equal(t, false, m["ephemeral"])
		// duration_ms should be omitted when nil
		_, hasDuration := m["duration_ms"]
		assert.False(t, hasDuration)
	})

	t.Run("full event with duration", func(t *testing.T) {
		duration := int64(1500)
		ev := &CommandEvent{
			Version:    1,
			Type:       EventTypeCommandEnd,
			TS:         1730000000123,
			SessionID:  "session-456",
			Shell:      ShellBash,
			Cwd:        "/home/user/project",
			CmdRaw:     "npm test",
			ExitCode:   1,
			DurationMs: &duration,
			Ephemeral:  true,
		}

		data, err := json.Marshal(ev)
		require.NoError(t, err)

		// Verify expected JSON structure
		var m map[string]interface{}
		err = json.Unmarshal(data, &m)
		require.NoError(t, err)

		assert.Equal(t, float64(1), m["v"])
		assert.Equal(t, "command_end", m["type"])
		assert.Equal(t, float64(1730000000123), m["ts"])
		assert.Equal(t, "session-456", m["session_id"])
		assert.Equal(t, "bash", m["shell"])
		assert.Equal(t, "/home/user/project", m["cwd"])
		assert.Equal(t, "npm test", m["cmd_raw"])
		assert.Equal(t, float64(1), m["exit_code"])
		assert.Equal(t, float64(1500), m["duration_ms"])
		assert.Equal(t, true, m["ephemeral"])
	})

	t.Run("roundtrip serialization", func(t *testing.T) {
		duration := int64(2500)
		original := &CommandEvent{
			Version:    1,
			Type:       EventTypeCommandEnd,
			TS:         1730000000999,
			SessionID:  "roundtrip-test",
			Shell:      ShellFish,
			Cwd:        "/tmp/test",
			CmdRaw:     "echo 'hello world'",
			ExitCode:   127,
			DurationMs: &duration,
			Ephemeral:  true,
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var parsed CommandEvent
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, original.Version, parsed.Version)
		assert.Equal(t, original.Type, parsed.Type)
		assert.Equal(t, original.TS, parsed.TS)
		assert.Equal(t, original.SessionID, parsed.SessionID)
		assert.Equal(t, original.Shell, parsed.Shell)
		assert.Equal(t, original.Cwd, parsed.Cwd)
		assert.Equal(t, original.CmdRaw, parsed.CmdRaw)
		assert.Equal(t, original.ExitCode, parsed.ExitCode)
		require.NotNil(t, parsed.DurationMs)
		assert.Equal(t, *original.DurationMs, *parsed.DurationMs)
		assert.Equal(t, original.Ephemeral, parsed.Ephemeral)
	})
}

func TestShellConstants(t *testing.T) {
	// Verify shell constants match expected values
	assert.Equal(t, Shell("bash"), ShellBash)
	assert.Equal(t, Shell("zsh"), ShellZsh)
	assert.Equal(t, Shell("fish"), ShellFish)
}

func TestEventConstants(t *testing.T) {
	assert.Equal(t, "command_end", EventTypeCommandEnd)
	assert.Equal(t, 1, EventVersion)
}
