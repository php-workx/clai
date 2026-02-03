package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Check defaults
	if cfg.Daemon.IdleTimeoutMins != 0 {
		t.Errorf("Expected idle_timeout_mins=0, got %d", cfg.Daemon.IdleTimeoutMins)
	}
	if cfg.Daemon.LogLevel != "info" {
		t.Errorf("Expected log_level=info, got %s", cfg.Daemon.LogLevel)
	}
	if cfg.Client.SuggestTimeoutMs != 50 {
		t.Errorf("Expected suggest_timeout_ms=50, got %d", cfg.Client.SuggestTimeoutMs)
	}
	if cfg.Client.ConnectTimeoutMs != 10 {
		t.Errorf("Expected connect_timeout_ms=10, got %d", cfg.Client.ConnectTimeoutMs)
	}
	if !cfg.Client.FireAndForget {
		t.Error("Expected fire_and_forget=true")
	}
	if cfg.AI.Enabled {
		t.Error("Expected ai.enabled=false by default")
	}
	if cfg.AI.Provider != "auto" {
		t.Errorf("Expected provider=auto, got %s", cfg.AI.Provider)
	}
	if cfg.AI.CacheTTLHours != 24 {
		t.Errorf("Expected cache_ttl_hours=24, got %d", cfg.AI.CacheTTLHours)
	}
	if !cfg.Privacy.SanitizeAICalls {
		t.Error("Expected sanitize_ai_calls=true")
	}
}

func TestConfigGet(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		key      string
		expected string
	}{
		{"daemon.idle_timeout_mins", "0"},
		{"daemon.log_level", "info"},
		{"client.suggest_timeout_ms", "50"},
		{"client.fire_and_forget", "true"},
		{"ai.enabled", "false"},
		{"ai.provider", "auto"},
		{"suggestions.max_history", "5"},
		{"privacy.sanitize_ai_calls", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestConfigGetInvalidKey(t *testing.T) {
	cfg := DefaultConfig()

	tests := []string{
		"invalid",
		"invalid.key",
		"daemon.invalid",
		"ai.invalid_field",
	}

	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			_, err := cfg.Get(key)
			if err == nil {
				t.Errorf("Get(%q) should have failed", key)
			}
		})
	}
}

func TestConfigSet(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"daemon.idle_timeout_mins", "30", "30"},
		{"daemon.log_level", "debug", "debug"},
		{"client.suggest_timeout_ms", "100", "100"},
		{"client.fire_and_forget", "false", "false"},
		{"ai.enabled", "true", "true"},
		{"ai.provider", "anthropic", "anthropic"},
		{"suggestions.max_history", "10", "10"},
		{"privacy.sanitize_ai_calls", "false", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			err := cfg.Set(tt.key, tt.value)
			if err != nil {
				t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
				return
			}

			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("After Set, Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestConfigSetInvalidValue(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		key   string
		value string
	}{
		{"daemon.idle_timeout_mins", "invalid"},
		{"daemon.log_level", "invalid_level"},
		{"client.suggest_timeout_ms", "not_a_number"},
		{"client.fire_and_forget", "not_bool"},
		{"ai.enabled", "yes"}, // Must be true/false
		{"ai.provider", "invalid_provider"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			err := cfg.Set(tt.key, tt.value)
			if err == nil {
				t.Errorf("Set(%q, %q) should have failed", tt.key, tt.value)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "default is valid",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "negative idle timeout",
			modify: func(c *Config) {
				c.Daemon.IdleTimeoutMins = -1
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			modify: func(c *Config) {
				c.Daemon.LogLevel = "invalid"
			},
			wantErr: true,
		},
		{
			name: "negative suggest timeout",
			modify: func(c *Config) {
				c.Client.SuggestTimeoutMs = -1
			},
			wantErr: true,
		},
		{
			name: "invalid provider",
			modify: func(c *Config) {
				c.AI.Provider = "invalid"
			},
			wantErr: true,
		},
		{
			name: "zero idle timeout is valid",
			modify: func(c *Config) {
				c.Daemon.IdleTimeoutMins = 0
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadFromFile_NonExistent(t *testing.T) {
	cfg, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile should return defaults for nonexistent file: %v", err)
	}

	// Should have default values
	if cfg.Daemon.IdleTimeoutMins != 0 {
		t.Errorf("Expected default idle_timeout_mins=0, got %d", cfg.Daemon.IdleTimeoutMins)
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "clai-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config with custom values
	cfg := DefaultConfig()
	cfg.Daemon.IdleTimeoutMins = 45
	cfg.AI.Enabled = true
	cfg.AI.Provider = "anthropic"

	// Save
	err = cfg.SaveToFile(configFile)
	if err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Load
	loaded, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Verify
	if loaded.Daemon.IdleTimeoutMins != 45 {
		t.Errorf("Expected idle_timeout_mins=45, got %d", loaded.Daemon.IdleTimeoutMins)
	}
	if !loaded.AI.Enabled {
		t.Error("Expected ai.enabled=true")
	}
	if loaded.AI.Provider != "anthropic" {
		t.Errorf("Expected provider=anthropic, got %s", loaded.AI.Provider)
	}
}

func TestListKeys(t *testing.T) {
	keys := ListKeys()

	if len(keys) == 0 {
		t.Error("ListKeys returned empty list")
	}

	// Check that only user-facing keys are present
	// Internal settings (daemon, client, ai, privacy) are not exposed
	expectedKeys := []string{
		"suggestions.max_history",
		"suggestions.show_risk_warning",
	}

	if len(keys) != len(expectedKeys) {
		t.Errorf("ListKeys returned %d keys, want %d: %v", len(keys), len(expectedKeys), keys)
	}

	for _, expected := range expectedKeys {
		found := false
		for _, key := range keys {
			if key == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected key %q not in ListKeys result", expected)
		}
	}
}

func TestValidLogLevels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error"}

	for _, level := range validLevels {
		if !isValidLogLevel(level) {
			t.Errorf("isValidLogLevel(%q) = false, want true", level)
		}
	}

	invalidLevels := []string{"trace", "INFO", "Debug", "warning", ""}
	for _, level := range invalidLevels {
		if isValidLogLevel(level) {
			t.Errorf("isValidLogLevel(%q) = true, want false", level)
		}
	}
}

func TestValidProviders(t *testing.T) {
	validProviders := []string{"anthropic", "auto"}

	for _, provider := range validProviders {
		if !isValidProvider(provider) {
			t.Errorf("isValidProvider(%q) = false, want true", provider)
		}
	}

	invalidProviders := []string{"claude", "gpt4", "gemini", "ANTHROPIC", ""}
	for _, provider := range invalidProviders {
		if isValidProvider(provider) {
			t.Errorf("isValidProvider(%q) = true, want false", provider)
		}
	}
}

// ============================================================================
// Comprehensive Get/Set tests for all sections
// ============================================================================

func TestGetAllDaemonFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Daemon.IdleTimeoutMins = 30
	cfg.Daemon.SocketPath = "/tmp/custom.sock"
	cfg.Daemon.LogLevel = "debug"
	cfg.Daemon.LogFile = "/var/log/clai.log"

	tests := []struct {
		key      string
		expected string
	}{
		{"daemon.idle_timeout_mins", "30"},
		{"daemon.socket_path", "/tmp/custom.sock"},
		{"daemon.log_level", "debug"},
		{"daemon.log_file", "/var/log/clai.log"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestSetAllDaemonFields(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"daemon.idle_timeout_mins", "60", "60"},
		{"daemon.socket_path", "/custom/path.sock", "/custom/path.sock"},
		{"daemon.log_level", "warn", "warn"},
		{"daemon.log_level", "error", "error"},
		{"daemon.log_file", "/tmp/test.log", "/tmp/test.log"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err != nil {
				t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
				return
			}

			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("After Set, Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestGetAllClientFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Client.SuggestTimeoutMs = 100
	cfg.Client.ConnectTimeoutMs = 25
	cfg.Client.FireAndForget = false
	cfg.Client.AutoStartDaemon = false

	tests := []struct {
		key      string
		expected string
	}{
		{"client.suggest_timeout_ms", "100"},
		{"client.connect_timeout_ms", "25"},
		{"client.fire_and_forget", "false"},
		{"client.auto_start_daemon", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestSetAllClientFields(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"client.suggest_timeout_ms", "200", "200"},
		{"client.connect_timeout_ms", "50", "50"},
		{"client.fire_and_forget", "false", "false"},
		{"client.fire_and_forget", "true", "true"},
		{"client.auto_start_daemon", "false", "false"},
		{"client.auto_start_daemon", "true", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err != nil {
				t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
				return
			}

			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("After Set, Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestGetAllAIFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AI.Enabled = true
	cfg.AI.Provider = "anthropic"
	cfg.AI.Model = "claude-3-opus"
	cfg.AI.AutoDiagnose = true
	cfg.AI.CacheTTLHours = 48

	tests := []struct {
		key      string
		expected string
	}{
		{"ai.enabled", "true"},
		{"ai.provider", "anthropic"},
		{"ai.model", "claude-3-opus"},
		{"ai.auto_diagnose", "true"},
		{"ai.cache_ttl_hours", "48"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestSetAllAIFields(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"ai.enabled", "true", "true"},
		{"ai.enabled", "false", "false"},
		{"ai.provider", "anthropic", "anthropic"},
		{"ai.provider", "auto", "auto"},
		{"ai.model", "gpt-4", "gpt-4"},
		{"ai.model", "", ""},
		{"ai.auto_diagnose", "true", "true"},
		{"ai.auto_diagnose", "false", "false"},
		{"ai.cache_ttl_hours", "72", "72"},
		{"ai.cache_ttl_hours", "0", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err != nil {
				t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
				return
			}

			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("After Set, Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestGetAllSuggestionsFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Suggestions.MaxHistory = 10
	cfg.Suggestions.MaxAI = 5
	cfg.Suggestions.ShowRiskWarning = false

	tests := []struct {
		key      string
		expected string
	}{
		{"suggestions.max_history", "10"},
		{"suggestions.max_ai", "5"},
		{"suggestions.show_risk_warning", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestSetAllSuggestionsFields(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"suggestions.max_history", "20", "20"},
		{"suggestions.max_history", "0", "0"},
		{"suggestions.max_ai", "10", "10"},
		{"suggestions.max_ai", "0", "0"},
		{"suggestions.show_risk_warning", "true", "true"},
		{"suggestions.show_risk_warning", "false", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err != nil {
				t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
				return
			}

			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("After Set, Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestGetAllPrivacyFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Privacy.SanitizeAICalls = false

	tests := []struct {
		key      string
		expected string
	}{
		{"privacy.sanitize_ai_calls", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestSetAllPrivacyFields(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"privacy.sanitize_ai_calls", "true", "true"},
		{"privacy.sanitize_ai_calls", "false", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err != nil {
				t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
				return
			}

			got, err := cfg.Get(tt.key)
			if err != nil {
				t.Errorf("Get(%q) error: %v", tt.key, err)
				return
			}
			if got != tt.expected {
				t.Errorf("After Set, Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Invalid key format tests
// ============================================================================

func TestGetInvalidKeyFormats(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		key  string
	}{
		{"no_dot", "daemonidletimeoutmins"},
		{"empty_string", ""},
		{"only_section", "daemon"},
		{"only_dot", "."},
		{"leading_dot", ".idle_timeout_mins"},
		{"trailing_dot", "daemon."},
		{"multiple_dots", "daemon.idle.timeout"},
		{"three_parts", "daemon.idle.timeout_mins"},
		{"spaces", "daemon .idle_timeout_mins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cfg.Get(tt.key)
			if err == nil {
				t.Errorf("Get(%q) should have returned an error", tt.key)
			}
		})
	}
}

func TestSetInvalidKeyFormats(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		key  string
	}{
		{"no_dot", "daemonidletimeoutmins"},
		{"empty_string", ""},
		{"only_section", "daemon"},
		{"only_dot", "."},
		{"leading_dot", ".idle_timeout_mins"},
		{"trailing_dot", "daemon."},
		{"multiple_dots", "daemon.idle.timeout"},
		{"three_parts", "daemon.idle.timeout_mins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfg.Set(tt.key, "value")
			if err == nil {
				t.Errorf("Set(%q, \"value\") should have returned an error", tt.key)
			}
		})
	}
}

func TestGetUnknownSection(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		key  string
	}{
		{"unknown_section", "unknown.field"},
		{"typo_section", "deamon.idle_timeout_mins"},
		{"capitalized_section", "Daemon.idle_timeout_mins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cfg.Get(tt.key)
			if err == nil {
				t.Errorf("Get(%q) should have returned an error", tt.key)
			}
		})
	}
}

func TestSetUnknownSection(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		key  string
	}{
		{"unknown_section", "unknown.field"},
		{"typo_section", "deamon.idle_timeout_mins"},
		{"capitalized_section", "Daemon.idle_timeout_mins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfg.Set(tt.key, "value")
			if err == nil {
				t.Errorf("Set(%q, \"value\") should have returned an error", tt.key)
			}
		})
	}
}

func TestGetUnknownFieldInSection(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		key  string
	}{
		{"daemon_unknown", "daemon.unknown_field"},
		{"client_unknown", "client.unknown_field"},
		{"ai_unknown", "ai.unknown_field"},
		{"suggestions_unknown", "suggestions.unknown_field"},
		{"privacy_unknown", "privacy.unknown_field"},
		{"daemon_typo", "daemon.idle_timeout"},
		{"ai_typo", "ai.enable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cfg.Get(tt.key)
			if err == nil {
				t.Errorf("Get(%q) should have returned an error", tt.key)
			}
		})
	}
}

func TestSetUnknownFieldInSection(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		key  string
	}{
		{"daemon_unknown", "daemon.unknown_field"},
		{"client_unknown", "client.unknown_field"},
		{"ai_unknown", "ai.unknown_field"},
		{"suggestions_unknown", "suggestions.unknown_field"},
		{"privacy_unknown", "privacy.unknown_field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfg.Set(tt.key, "value")
			if err == nil {
				t.Errorf("Set(%q, \"value\") should have returned an error", tt.key)
			}
		})
	}
}

// ============================================================================
// Invalid value tests
// ============================================================================

func TestSetInvalidIntegerValues(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"daemon.idle_timeout_mins", "not_a_number"},
		{"daemon.idle_timeout_mins", "12.5"},
		{"daemon.idle_timeout_mins", ""},
		{"daemon.idle_timeout_mins", "abc123"},
		{"client.suggest_timeout_ms", "invalid"},
		{"client.connect_timeout_ms", "3.14"},
		{"ai.cache_ttl_hours", "twenty"},
		{"suggestions.max_history", "five"},
		{"suggestions.max_ai", "1.5"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err == nil {
				t.Errorf("Set(%q, %q) should have returned an error for invalid integer", tt.key, tt.value)
			}
		})
	}
}

func TestSetInvalidBooleanValues(t *testing.T) {
	// Note: Go's strconv.ParseBool accepts: 1, 0, t, f, T, F, true, false, TRUE, FALSE, True, False
	// So we only test values that are truly invalid
	tests := []struct {
		key   string
		value string
	}{
		{"client.fire_and_forget", "yes"},
		{"client.fire_and_forget", "no"},
		{"client.fire_and_forget", ""},
		{"client.auto_start_daemon", "YES"},
		{"ai.enabled", "enable"},
		{"ai.auto_diagnose", "on"},
		{"suggestions.show_risk_warning", "off"},
		{"privacy.sanitize_ai_calls", "maybe"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err == nil {
				t.Errorf("Set(%q, %q) should have returned an error for invalid boolean", tt.key, tt.value)
			}
		})
	}
}

func TestSetInvalidLogLevel(t *testing.T) {
	tests := []struct {
		value string
	}{
		{"trace"},
		{"DEBUG"},
		{"Info"},
		{"WARNING"},
		{"fatal"},
		{""},
		{"verbose"},
	}

	for _, tt := range tests {
		t.Run("log_level="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set("daemon.log_level", tt.value)
			if err == nil {
				t.Errorf("Set(\"daemon.log_level\", %q) should have returned an error", tt.value)
			}
		})
	}
}

func TestSetInvalidProvider(t *testing.T) {
	tests := []struct {
		value string
	}{
		{"claude"},
		{"gpt4"},
		{"gemini"},
		{"ANTHROPIC"},
		{"OpenAI"},
		{""},
		{"azure"},
		{"local"},
	}

	for _, tt := range tests {
		t.Run("provider="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set("ai.provider", tt.value)
			if err == nil {
				t.Errorf("Set(\"ai.provider\", %q) should have returned an error", tt.value)
			}
		})
	}
}

// ============================================================================
// Validation tests
// ============================================================================

func TestValidateAllErrors(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr string
	}{
		{
			name:    "default_is_valid",
			modify:  func(c *Config) {},
			wantErr: "",
		},
		{
			name: "negative_idle_timeout",
			modify: func(c *Config) {
				c.Daemon.IdleTimeoutMins = -1
			},
			wantErr: "daemon.idle_timeout_mins must be >= 0",
		},
		{
			name: "invalid_log_level_empty",
			modify: func(c *Config) {
				c.Daemon.LogLevel = ""
			},
			wantErr: "daemon.log_level must be debug, info, warn, or error",
		},
		{
			name: "invalid_log_level_unknown",
			modify: func(c *Config) {
				c.Daemon.LogLevel = "trace"
			},
			wantErr: "daemon.log_level must be debug, info, warn, or error",
		},
		{
			name: "negative_suggest_timeout",
			modify: func(c *Config) {
				c.Client.SuggestTimeoutMs = -1
			},
			wantErr: "client.suggest_timeout_ms must be >= 0",
		},
		{
			name: "negative_connect_timeout",
			modify: func(c *Config) {
				c.Client.ConnectTimeoutMs = -1
			},
			wantErr: "client.connect_timeout_ms must be >= 0",
		},
		{
			name: "invalid_provider_empty",
			modify: func(c *Config) {
				c.AI.Provider = ""
			},
			wantErr: "ai.provider must be anthropic or auto",
		},
		{
			name: "invalid_provider_unknown",
			modify: func(c *Config) {
				c.AI.Provider = "unknown"
			},
			wantErr: "ai.provider must be anthropic or auto",
		},
		{
			name: "negative_cache_ttl",
			modify: func(c *Config) {
				c.AI.CacheTTLHours = -1
			},
			wantErr: "ai.cache_ttl_hours must be >= 0",
		},
		{
			name: "negative_max_history",
			modify: func(c *Config) {
				c.Suggestions.MaxHistory = -1
			},
			wantErr: "suggestions.max_history must be >= 0",
		},
		{
			name: "negative_max_ai",
			modify: func(c *Config) {
				c.Suggestions.MaxAI = -1
			},
			wantErr: "suggestions.max_ai must be >= 0",
		},
		{
			name: "zero_values_are_valid",
			modify: func(c *Config) {
				c.Daemon.IdleTimeoutMins = 0
				c.Client.SuggestTimeoutMs = 0
				c.Client.ConnectTimeoutMs = 0
				c.AI.CacheTTLHours = 0
				c.Suggestions.MaxHistory = 0
				c.Suggestions.MaxAI = 0
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() error = nil, want error containing %q", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// File I/O tests
// ============================================================================

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML
	invalidYAML := `
daemon:
  idle_timeout_mins: [not valid yaml
  this is broken
`
	if err := os.WriteFile(configFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write invalid YAML: %v", err)
	}

	_, err = LoadFromFile(configFile)
	if err == nil {
		t.Error("LoadFromFile should have returned an error for invalid YAML")
	}
}

func TestLoadFromFile_PartialConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write partial config - only daemon section
	partialYAML := `
daemon:
  idle_timeout_mins: 99
  log_level: debug
`
	if err := os.WriteFile(configFile, []byte(partialYAML), 0644); err != nil {
		t.Fatalf("Failed to write partial YAML: %v", err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Check that specified values were loaded
	if cfg.Daemon.IdleTimeoutMins != 99 {
		t.Errorf("Expected idle_timeout_mins=99, got %d", cfg.Daemon.IdleTimeoutMins)
	}
	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("Expected log_level=debug, got %s", cfg.Daemon.LogLevel)
	}

	// Check that other sections have default values
	if cfg.Client.SuggestTimeoutMs != 50 {
		t.Errorf("Expected default suggest_timeout_ms=50, got %d", cfg.Client.SuggestTimeoutMs)
	}
	if cfg.AI.Provider != "auto" {
		t.Errorf("Expected default provider=auto, got %s", cfg.AI.Provider)
	}
}

func TestLoadFromFile_EmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write empty file
	if err := os.WriteFile(configFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed for empty file: %v", err)
	}

	// Should have default values
	if cfg.Daemon.IdleTimeoutMins != 0 {
		t.Errorf("Expected default idle_timeout_mins=0, got %d", cfg.Daemon.IdleTimeoutMins)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config with all custom values
	cfg := &Config{
		Daemon: DaemonConfig{
			IdleTimeoutMins: 99,
			SocketPath:      "/custom/socket.sock",
			LogLevel:        "debug",
			LogFile:         "/custom/log.log",
		},
		Client: ClientConfig{
			SuggestTimeoutMs: 200,
			ConnectTimeoutMs: 50,
			FireAndForget:    false,
			AutoStartDaemon:  false,
		},
		AI: AIConfig{
			Enabled:       true,
			Provider:      "anthropic",
			Model:         "claude-3-opus",
			AutoDiagnose:  true,
			CacheTTLHours: 72,
		},
		Suggestions: SuggestionsConfig{
			MaxHistory:      15,
			MaxAI:           8,
			ShowRiskWarning: false,
		},
		Privacy: PrivacyConfig{
			SanitizeAICalls: false,
		},
	}

	// Save
	if err := cfg.SaveToFile(configFile); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Load
	loaded, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Verify all values
	if loaded.Daemon.IdleTimeoutMins != 99 {
		t.Errorf("Daemon.IdleTimeoutMins: got %d, want 99", loaded.Daemon.IdleTimeoutMins)
	}
	if loaded.Daemon.SocketPath != "/custom/socket.sock" {
		t.Errorf("Daemon.SocketPath: got %s, want /custom/socket.sock", loaded.Daemon.SocketPath)
	}
	if loaded.Daemon.LogLevel != "debug" {
		t.Errorf("Daemon.LogLevel: got %s, want debug", loaded.Daemon.LogLevel)
	}
	if loaded.Daemon.LogFile != "/custom/log.log" {
		t.Errorf("Daemon.LogFile: got %s, want /custom/log.log", loaded.Daemon.LogFile)
	}

	if loaded.Client.SuggestTimeoutMs != 200 {
		t.Errorf("Client.SuggestTimeoutMs: got %d, want 200", loaded.Client.SuggestTimeoutMs)
	}
	if loaded.Client.ConnectTimeoutMs != 50 {
		t.Errorf("Client.ConnectTimeoutMs: got %d, want 50", loaded.Client.ConnectTimeoutMs)
	}
	if loaded.Client.FireAndForget != false {
		t.Errorf("Client.FireAndForget: got %v, want false", loaded.Client.FireAndForget)
	}
	if loaded.Client.AutoStartDaemon != false {
		t.Errorf("Client.AutoStartDaemon: got %v, want false", loaded.Client.AutoStartDaemon)
	}

	if loaded.AI.Enabled != true {
		t.Errorf("AI.Enabled: got %v, want true", loaded.AI.Enabled)
	}
	if loaded.AI.Provider != "anthropic" {
		t.Errorf("AI.Provider: got %s, want anthropic", loaded.AI.Provider)
	}
	if loaded.AI.Model != "claude-3-opus" {
		t.Errorf("AI.Model: got %s, want claude-3-opus", loaded.AI.Model)
	}
	if loaded.AI.AutoDiagnose != true {
		t.Errorf("AI.AutoDiagnose: got %v, want true", loaded.AI.AutoDiagnose)
	}
	if loaded.AI.CacheTTLHours != 72 {
		t.Errorf("AI.CacheTTLHours: got %d, want 72", loaded.AI.CacheTTLHours)
	}

	if loaded.Suggestions.MaxHistory != 15 {
		t.Errorf("Suggestions.MaxHistory: got %d, want 15", loaded.Suggestions.MaxHistory)
	}
	if loaded.Suggestions.MaxAI != 8 {
		t.Errorf("Suggestions.MaxAI: got %d, want 8", loaded.Suggestions.MaxAI)
	}
	if loaded.Suggestions.ShowRiskWarning != false {
		t.Errorf("Suggestions.ShowRiskWarning: got %v, want false", loaded.Suggestions.ShowRiskWarning)
	}

	if loaded.Privacy.SanitizeAICalls != false {
		t.Errorf("Privacy.SanitizeAICalls: got %v, want false", loaded.Privacy.SanitizeAICalls)
	}
}

func TestLoadFromFile_ReadError(t *testing.T) {
	// Try to load from a directory (which should fail)
	tmpDir, err := os.MkdirTemp("", "clai-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory and try to read it as a file
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	_, err = LoadFromFile(subDir)
	if err == nil {
		t.Error("LoadFromFile should have returned an error when reading a directory")
	}
}

// ============================================================================
// ListKeys tests
// ============================================================================

func TestListKeysComplete(t *testing.T) {
	keys := ListKeys()

	// Only user-facing keys are exposed via ListKeys()
	// Internal settings (daemon, client, ai, privacy) are not exposed
	expectedKeys := []string{
		"suggestions.max_history",
		"suggestions.show_risk_warning",
	}

	if len(keys) != len(expectedKeys) {
		t.Errorf("ListKeys returned %d keys, want %d: %v", len(keys), len(expectedKeys), keys)
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}

	for _, expected := range expectedKeys {
		if !keySet[expected] {
			t.Errorf("ListKeys missing expected key: %s", expected)
		}
	}
}

func TestListKeysAllGettable(t *testing.T) {
	cfg := DefaultConfig()
	keys := ListKeys()

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			_, err := cfg.Get(key)
			if err != nil {
				t.Errorf("Get(%q) failed for key from ListKeys: %v", key, err)
			}
		})
	}
}

func TestListKeysAllSettable(t *testing.T) {
	keys := ListKeys()

	// Map of user-facing keys to valid test values
	// Only the keys exposed by ListKeys() need to be here
	testValues := map[string]string{
		"suggestions.max_history":       "10",
		"suggestions.show_risk_warning": "false",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			cfg := DefaultConfig()
			value, ok := testValues[key]
			if !ok {
				t.Fatalf("No test value defined for key: %s", key)
			}

			err := cfg.Set(key, value)
			if err != nil {
				t.Errorf("Set(%q, %q) failed for key from ListKeys: %v", key, value, err)
			}
		})
	}
}

// ============================================================================
// Default config tests
// ============================================================================

func TestDefaultConfigValues(t *testing.T) {
	cfg := DefaultConfig()

	// Test all default values comprehensively
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		// Daemon defaults
		{"Daemon.IdleTimeoutMins", cfg.Daemon.IdleTimeoutMins, 0},
		{"Daemon.SocketPath", cfg.Daemon.SocketPath, ""},
		{"Daemon.LogLevel", cfg.Daemon.LogLevel, "info"},
		{"Daemon.LogFile", cfg.Daemon.LogFile, ""},
		// Client defaults
		{"Client.SuggestTimeoutMs", cfg.Client.SuggestTimeoutMs, 50},
		{"Client.ConnectTimeoutMs", cfg.Client.ConnectTimeoutMs, 10},
		{"Client.FireAndForget", cfg.Client.FireAndForget, true},
		{"Client.AutoStartDaemon", cfg.Client.AutoStartDaemon, true},
		// AI defaults
		{"AI.Enabled", cfg.AI.Enabled, false},
		{"AI.Provider", cfg.AI.Provider, "auto"},
		{"AI.Model", cfg.AI.Model, ""},
		{"AI.AutoDiagnose", cfg.AI.AutoDiagnose, false},
		{"AI.CacheTTLHours", cfg.AI.CacheTTLHours, 24},
		// Suggestions defaults
		{"Suggestions.MaxHistory", cfg.Suggestions.MaxHistory, 5},
		{"Suggestions.MaxAI", cfg.Suggestions.MaxAI, 3},
		{"Suggestions.ShowRiskWarning", cfg.Suggestions.ShowRiskWarning, true},
		// Privacy defaults
		{"Privacy.SanitizeAICalls", cfg.Privacy.SanitizeAICalls, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid, but Validate() returned: %v", err)
	}
}
