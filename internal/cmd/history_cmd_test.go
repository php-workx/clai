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

func TestHistoryCmd_Flags(t *testing.T) {
	// Verify all expected flags are registered
	expectedFlags := []struct {
		name      string
		shorthand string
	}{
		{"limit", "n"},
		{"cwd", "c"},
		{"session", "s"},
		{"global", "g"},
	}

	for _, f := range expectedFlags {
		flag := historyCmd.Flags().Lookup(f.name)
		if flag == nil {
			t.Errorf("Expected flag --%s to be registered", f.name)
			continue
		}
		if flag.Shorthand != f.shorthand {
			t.Errorf("Flag --%s: expected shorthand -%s, got -%s", f.name, f.shorthand, flag.Shorthand)
		}
	}
}

func TestHistoryCmd_DefaultLimit(t *testing.T) {
	flag := historyCmd.Flags().Lookup("limit")
	if flag == nil {
		t.Fatal("limit flag not found")
	}
	if flag.DefValue != "20" {
		t.Errorf("Expected default limit=20, got %s", flag.DefValue)
	}
}

func TestHistoryCmd_GlobalDefault(t *testing.T) {
	flag := historyCmd.Flags().Lookup("global")
	if flag == nil {
		t.Fatal("global flag not found")
	}
	if flag.DefValue != "false" {
		t.Errorf("Expected default global=false, got %s", flag.DefValue)
	}
}
