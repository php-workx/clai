package cmd

import (
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestConfigCmd_List(t *testing.T) {
	// The config command should list user-facing keys only
	keys := config.ListKeys()

	// Should have exactly the user-facing keys
	expectedKeys := []string{
		"suggestions.enabled",
		"suggestions.max_history",
		"suggestions.show_risk_warning",
		"suggestions.scorer_version",
		"history.picker_backend",
		"history.picker_open_on_empty",
		"history.picker_page_size",
		"history.picker_case_sensitive",
		"history.up_arrow_trigger",
		"history.up_arrow_double_window_ms",
	}

	if len(keys) != len(expectedKeys) {
		t.Errorf("Expected %d keys, got %d: %v", len(expectedKeys), len(keys), keys)
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
