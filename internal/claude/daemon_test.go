package claude

import (
	"os"
	"testing"
	"time"
)

func TestIdleTimeout(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{"default when unset", "", defaultIdleTimeout},
		{"valid duration 30m", "30m", 30 * time.Minute},
		{"valid duration 4h", "4h", 4 * time.Hour},
		{"valid duration 24h", "24h", 24 * time.Hour},
		{"valid duration 90s", "90s", 90 * time.Second},
		{"invalid string falls back to default", "notaduration", defaultIdleTimeout},
		{"zero falls back to default", "0s", defaultIdleTimeout},
		{"negative falls back to default", "-5m", defaultIdleTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := os.Getenv("AI_TERMINAL_IDLE_TIMEOUT")
			defer os.Setenv("AI_TERMINAL_IDLE_TIMEOUT", original)

			if tt.envValue == "" {
				os.Unsetenv("AI_TERMINAL_IDLE_TIMEOUT")
			} else {
				os.Setenv("AI_TERMINAL_IDLE_TIMEOUT", tt.envValue)
			}

			got := idleTimeout()
			if got != tt.expected {
				t.Errorf("idleTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultIdleTimeout(t *testing.T) {
	if defaultIdleTimeout != 2*time.Hour {
		t.Errorf("defaultIdleTimeout = %v, want 2h", defaultIdleTimeout)
	}
}
