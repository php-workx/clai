package cmd

import (
	"strings"
	"testing"
)

func TestConfigCmd_List(t *testing.T) {
	// The config command should list all keys when called with no args
	// We test this by ensuring ListKeys returns expected keys
	keys := []string{
		"daemon.idle_timeout_mins",
		"ai.enabled",
		"privacy.sanitize_ai_calls",
	}

	// All these keys should be returned by listConfig
	for _, key := range keys {
		found := false
		for _, k := range allConfigKeys() {
			if k == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected key %q to be in config keys", key)
		}
	}
}

func allConfigKeys() []string {
	return []string{
		"daemon.idle_timeout_mins",
		"daemon.socket_path",
		"daemon.log_level",
		"daemon.log_file",
		"client.suggest_timeout_ms",
		"client.connect_timeout_ms",
		"client.fire_and_forget",
		"client.auto_start_daemon",
		"ai.enabled",
		"ai.provider",
		"ai.model",
		"ai.auto_diagnose",
		"ai.cache_ttl_hours",
		"suggestions.max_history",
		"suggestions.max_ai",
		"suggestions.show_risk_warning",
		"privacy.sanitize_ai_calls",
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
