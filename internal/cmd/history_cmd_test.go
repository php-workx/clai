package cmd

import (
	"testing"
)

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{0, "0ms"},
		{100, "100ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{5000, "5.0s"},
		{59000, "59.0s"},
		{60000, "1m0s"},
		{90000, "1m30s"},
		{3600000, "60m0s"},
	}

	for _, tt := range tests {
		result := formatDurationMs(tt.ms)
		if result != tt.expected {
			t.Errorf("formatDurationMs(%d) = %q, want %q", tt.ms, result, tt.expected)
		}
	}
}
