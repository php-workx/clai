package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSessionStartArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no args",
			args:    []string{},
			wantErr: false,
		},
		{
			name:    "unknown flag",
			args:    []string{"--unknown"},
			wantErr: true,
		},
		{
			name:    "short unknown flag",
			args:    []string{"-x"},
			wantErr: true,
		},
		{
			name:    "positional args ignored",
			args:    []string{"foo", "bar"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSessionStartArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRunSessionStart(t *testing.T) {
	// For now, session-start is a skeleton that always returns 0
	// Once transport is implemented, we'll add more comprehensive tests

	t.Run("returns success", func(t *testing.T) {
		exitCode := runSessionStart([]string{})
		assert.Equal(t, 0, exitCode)
	})

	t.Run("returns error on unknown flag", func(t *testing.T) {
		exitCode := runSessionStart([]string{"--unknown"})
		assert.Equal(t, 1, exitCode)
	})
}
