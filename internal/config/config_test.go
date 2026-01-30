package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Check defaults
	if cfg.Daemon.IdleTimeoutMins != 20 {
		t.Errorf("Expected idle_timeout_mins=20, got %d", cfg.Daemon.IdleTimeoutMins)
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
		{"daemon.idle_timeout_mins", "20"},
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
	if cfg.Daemon.IdleTimeoutMins != 20 {
		t.Errorf("Expected default idle_timeout_mins=20, got %d", cfg.Daemon.IdleTimeoutMins)
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

	// Check some expected keys are present
	expectedKeys := []string{
		"daemon.idle_timeout_mins",
		"ai.enabled",
		"privacy.sanitize_ai_calls",
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
	validProviders := []string{"anthropic", "openai", "google", "auto"}

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
