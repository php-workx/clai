package workflow

import (
	"testing"
)

func TestIsDangerousEnvKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"PATH", true},
		{"path", true}, // case-insensitive
		{"Path", true}, // case-insensitive
		{"HOME", true},
		{"LD_PRELOAD", true},
		{"LD_LIBRARY_PATH", true},
		{"DYLD_INSERT_LIBRARIES", true},
		{"SSH_AUTH_SOCK", true},
		{"TMPDIR", true},
		{"SHELL", true},
		{"USER", true},
		{"LOGNAME", true},
		{"GPG_AGENT_INFO", true},
		{"DYLD_LIBRARY_PATH", true},
		{"DYLD_FRAMEWORK_PATH", true},
		// Safe keys
		{"MY_VAR", false},
		{"RESULT", false},
		{"GOPATH", false},
		{"PATHEXT", false}, // not exact match
		{"MY_HOME", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := IsDangerousEnvKey(tt.key)
			if got != tt.want {
				t.Errorf("IsDangerousEnvKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
