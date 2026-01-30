package cmd

import (
	"strings"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestConfigCmd_List(t *testing.T) {
	// The config command should list all keys when called with no args
	// We test against the actual production key list from the config package
	keys := config.ListKeys()

	// Verify we have a reasonable number of keys
	if len(keys) == 0 {
		t.Error("config.ListKeys() returned empty list")
	}

	// Verify some expected core keys are present
	expectedKeys := []string{
		"daemon.idle_timeout_mins",
		"ai.enabled",
		"privacy.sanitize_ai_calls",
	}

	for _, expected := range expectedKeys {
		found := false
		for _, k := range keys {
			if k == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected key %q to be in config.ListKeys() result", expected)
		}
	}
}

func TestFormatBool(t *testing.T) {
	tests := []struct {
		input    bool
		contains string
	}{
		{true, "enabled"},
		{false, "disabled"},
	}

	for _, tt := range tests {
		result := formatBool(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("formatBool(%v) = %q, should contain %q", tt.input, result, tt.contains)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		result := formatSize(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
		}
	}
}
