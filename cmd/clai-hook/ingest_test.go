package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIngestArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantStdin bool
		wantErr   bool
	}{
		{
			name:      "no args",
			args:      []string{},
			wantStdin: false,
			wantErr:   false,
		},
		{
			name:      "cmd-stdin flag",
			args:      []string{"--cmd-stdin"},
			wantStdin: true,
			wantErr:   false,
		},
		{
			name:      "unknown flag",
			args:      []string{"--unknown"},
			wantStdin: false,
			wantErr:   true,
		},
		{
			name:      "short unknown flag",
			args:      []string{"-x"},
			wantStdin: false,
			wantErr:   true,
		},
		{
			name:      "positional args ignored",
			args:      []string{"foo", "bar"},
			wantStdin: false,
			wantErr:   false,
		},
		{
			name:      "mixed args",
			args:      []string{"foo", "--cmd-stdin", "bar"},
			wantStdin: true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseIngestArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStdin, cfg.cmdStdin)
		})
	}
}

func TestReadIngestEnv(t *testing.T) {
	// Helper to set env vars and clean them up
	setEnv := func(vars map[string]string) func() {
		old := make(map[string]string)
		for k := range vars {
			old[k] = os.Getenv(k)
		}
		for k, v := range vars {
			os.Setenv(k, v)
		}
		return func() {
			for k, v := range old {
				if v == "" {
					os.Unsetenv(k)
				} else {
					os.Setenv(k, v)
				}
			}
		}
	}

	t.Run("all required fields present", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "git status",
			"CLAI_CWD":        "/home/user/project",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc123",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		ev, err := readIngestEnv(cfg)
		require.NoError(t, err)

		assert.Equal(t, "git status", ev.CmdRaw)
		assert.Equal(t, "/home/user/project", ev.Cwd)
		assert.Equal(t, 0, ev.ExitCode)
		assert.Equal(t, int64(1730000000123), ev.TS)
		assert.Equal(t, "zsh", string(ev.Shell))
		assert.Equal(t, "abc123", ev.SessionID)
		assert.Nil(t, ev.DurationMs)
		assert.False(t, ev.Ephemeral)
		assert.Equal(t, 1, ev.Version)
		assert.Equal(t, "command_end", ev.Type)
	})

	t.Run("with optional fields", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":         "npm test",
			"CLAI_CWD":         "/home/user/project",
			"CLAI_EXIT":        "1",
			"CLAI_TS":          "1730000000123",
			"CLAI_SHELL":       "bash",
			"CLAI_SESSION_ID":  "session-456",
			"CLAI_DURATION_MS": "1500",
			"CLAI_EPHEMERAL":   "1",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		ev, err := readIngestEnv(cfg)
		require.NoError(t, err)

		assert.Equal(t, "npm test", ev.CmdRaw)
		assert.Equal(t, 1, ev.ExitCode)
		assert.Equal(t, "bash", string(ev.Shell))
		require.NotNil(t, ev.DurationMs)
		assert.Equal(t, int64(1500), *ev.DurationMs)
		assert.True(t, ev.Ephemeral)
	})

	t.Run("missing CLAI_CMD", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "",
			"CLAI_CWD":        "/home/user",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_CMD")
	})

	t.Run("missing CLAI_CWD", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_CWD")
	})

	t.Run("missing CLAI_EXIT", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_EXIT")
	})

	t.Run("invalid CLAI_EXIT", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "not-a-number",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_EXIT")
		assert.Contains(t, err.Error(), "integer")
	})

	t.Run("missing CLAI_TS", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_TS")
	})

	t.Run("invalid CLAI_TS", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "not-a-timestamp",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_TS")
		assert.Contains(t, err.Error(), "integer")
	})

	t.Run("missing CLAI_SHELL", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_SHELL")
	})

	t.Run("invalid CLAI_SHELL", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "powershell",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_SHELL")
		assert.Contains(t, err.Error(), "bash, zsh, fish")
	})

	t.Run("missing CLAI_SESSION_ID", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_SESSION_ID")
	})

	t.Run("invalid CLAI_DURATION_MS", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":         "ls",
			"CLAI_CWD":         "/home",
			"CLAI_EXIT":        "0",
			"CLAI_TS":          "1730000000123",
			"CLAI_SHELL":       "zsh",
			"CLAI_SESSION_ID":  "abc",
			"CLAI_DURATION_MS": "not-a-number",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		_, err := readIngestEnv(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CLAI_DURATION_MS")
		assert.Contains(t, err.Error(), "integer")
	})

	t.Run("fish shell is valid", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "echo hello",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "fish",
			"CLAI_SESSION_ID": "abc",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		ev, err := readIngestEnv(cfg)
		require.NoError(t, err)
		assert.Equal(t, "fish", string(ev.Shell))
	})

	t.Run("non-1 ephemeral value is false", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"CLAI_CMD":        "ls",
			"CLAI_CWD":        "/home",
			"CLAI_EXIT":       "0",
			"CLAI_TS":         "1730000000123",
			"CLAI_SHELL":      "zsh",
			"CLAI_SESSION_ID": "abc",
			"CLAI_EPHEMERAL":  "0",
		})
		defer cleanup()

		cfg := &ingestConfig{cmdStdin: false}
		ev, err := readIngestEnv(cfg)
		require.NoError(t, err)
		assert.False(t, ev.Ephemeral)
	})
}

func TestToValidUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid ASCII",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "valid UTF-8 with unicode",
			input: "hello \u4e16\u754c", // hello 世界
			want:  "hello \u4e16\u754c",
		},
		{
			name:  "valid UTF-8 with emoji",
			input: "hello \U0001F44B", // hello 👋
			want:  "hello \U0001F44B",
		},
		{
			name:  "invalid UTF-8 byte",
			input: "hello \xff world",
			want:  "hello \ufffd world",
		},
		{
			name:  "multiple invalid bytes",
			input: "\x80\x81\x82",
			want:  "\ufffd\ufffd\ufffd",
		},
		{
			name:  "mixed valid and invalid",
			input: "a\xffb\xfec",
			want:  "a\ufffdb\ufffdc",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "truncated UTF-8 sequence",
			input: "abc\xc3", // incomplete 2-byte sequence
			want:  "abc\ufffd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toValidUTF8(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunIngestNoRecord(t *testing.T) {
	// Save and restore env
	oldVal := os.Getenv("CLAI_NO_RECORD")
	defer func() {
		if oldVal == "" {
			os.Unsetenv("CLAI_NO_RECORD")
		} else {
			os.Setenv("CLAI_NO_RECORD", oldVal)
		}
	}()

	os.Setenv("CLAI_NO_RECORD", "1")

	// Should return 0 even without other required env vars
	exitCode := runIngest([]string{})
	assert.Equal(t, 0, exitCode)
}
