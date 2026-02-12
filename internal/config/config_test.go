package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if !cfg.Suggestions.Enabled {
		t.Error("Expected suggestions.enabled=true")
	}
	if !cfg.Privacy.SanitizeAICalls {
		t.Error("Expected sanitize_ai_calls=true")
	}
}

// ============================================================================
// Unified Get/Set tests - covers all config fields
// ============================================================================

func TestConfigGet(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		key      string
		expected string
	}{
		// Daemon section
		{"daemon.idle_timeout_mins", "0"},
		{"daemon.log_level", "info"},
		{"daemon.socket_path", ""},
		{"daemon.log_file", ""},
		// Client section
		{"client.suggest_timeout_ms", "50"},
		{"client.connect_timeout_ms", "10"},
		{"client.fire_and_forget", "true"},
		{"client.auto_start_daemon", "true"},
		// AI section
		{"ai.enabled", "false"},
		{"ai.provider", "auto"},
		{"ai.model", ""},
		{"ai.auto_diagnose", "false"},
		{"ai.cache_ttl_hours", "24"},
		// Suggestions section
		{"suggestions.enabled", "true"},
		{"suggestions.max_history", "5"},
		{"suggestions.max_ai", "3"},
		{"suggestions.show_risk_warning", "true"},
		// Privacy section
		{"privacy.sanitize_ai_calls", "true"},
		// History section
		{"history.picker_backend", "builtin"},
		{"history.picker_open_on_empty", "false"},
		{"history.picker_page_size", "100"},
		{"history.picker_case_sensitive", "false"},
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

func TestConfigSet(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		// Daemon section
		{"daemon.idle_timeout_mins", "30", "30"},
		{"daemon.idle_timeout_mins", "0", "0"},
		{"daemon.socket_path", "/custom/path.sock", "/custom/path.sock"},
		{"daemon.log_level", "debug", "debug"},
		{"daemon.log_level", "warn", "warn"},
		{"daemon.log_level", "error", "error"},
		{"daemon.log_file", "/tmp/test.log", "/tmp/test.log"},
		// Client section
		{"client.suggest_timeout_ms", "100", "100"},
		{"client.connect_timeout_ms", "50", "50"},
		{"client.fire_and_forget", "false", "false"},
		{"client.fire_and_forget", "true", "true"},
		{"client.auto_start_daemon", "false", "false"},
		// AI section
		{"ai.enabled", "true", "true"},
		{"ai.enabled", "false", "false"},
		{"ai.provider", "anthropic", "anthropic"},
		{"ai.provider", "auto", "auto"},
		{"ai.model", "gpt-4", "gpt-4"},
		{"ai.model", "", ""},
		{"ai.auto_diagnose", "true", "true"},
		{"ai.cache_ttl_hours", "72", "72"},
		{"ai.cache_ttl_hours", "0", "0"},
		// Suggestions section
		{"suggestions.enabled", "false", "false"},
		{"suggestions.max_history", "10", "10"},
		{"suggestions.max_history", "0", "0"},
		{"suggestions.max_ai", "10", "10"},
		{"suggestions.show_risk_warning", "false", "false"},
		// Privacy section
		{"privacy.sanitize_ai_calls", "false", "false"},
		{"privacy.sanitize_ai_calls", "true", "true"},
		// History section
		{"history.picker_backend", "fzf", "fzf"},
		{"history.picker_backend", "clai", "clai"},
		{"history.picker_backend", "builtin", "builtin"},
		{"history.picker_open_on_empty", "true", "true"},
		{"history.picker_page_size", "50", "50"},
		{"history.picker_case_sensitive", "true", "true"},
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

func TestConfigGetWithCustomValues(t *testing.T) {
	cfg := DefaultConfig()
	// Set custom values
	cfg.Daemon.IdleTimeoutMins = 30
	cfg.Daemon.SocketPath = "/tmp/custom.sock"
	cfg.Daemon.LogLevel = "debug"
	cfg.Daemon.LogFile = "/var/log/clai.log"
	cfg.Client.SuggestTimeoutMs = 100
	cfg.Client.ConnectTimeoutMs = 25
	cfg.Client.FireAndForget = false
	cfg.Client.AutoStartDaemon = false
	cfg.AI.Enabled = true
	cfg.AI.Provider = "anthropic"
	cfg.AI.Model = "claude-3-opus"
	cfg.AI.AutoDiagnose = true
	cfg.AI.CacheTTLHours = 48
	cfg.Suggestions.MaxHistory = 10
	cfg.Suggestions.MaxAI = 5
	cfg.Suggestions.ShowRiskWarning = false
	cfg.Privacy.SanitizeAICalls = false

	tests := []struct {
		key      string
		expected string
	}{
		{"daemon.idle_timeout_mins", "30"},
		{"daemon.socket_path", "/tmp/custom.sock"},
		{"daemon.log_level", "debug"},
		{"daemon.log_file", "/var/log/clai.log"},
		{"client.suggest_timeout_ms", "100"},
		{"client.connect_timeout_ms", "25"},
		{"client.fire_and_forget", "false"},
		{"client.auto_start_daemon", "false"},
		{"ai.enabled", "true"},
		{"ai.provider", "anthropic"},
		{"ai.model", "claude-3-opus"},
		{"ai.auto_diagnose", "true"},
		{"ai.cache_ttl_hours", "48"},
		{"suggestions.max_history", "10"},
		{"suggestions.max_ai", "5"},
		{"suggestions.show_risk_warning", "false"},
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

// ============================================================================
// Invalid key tests
// ============================================================================

func TestConfigGetInvalidKey(t *testing.T) {
	cfg := DefaultConfig()

	tests := []string{
		// Invalid format
		"invalid",
		"invalid.key",
		"",
		".",
		".idle_timeout_mins",
		"daemon.",
		"daemon.idle.timeout",
		"daemon.idle.timeout_mins",
		"daemon .idle_timeout_mins",
		"daemonidletimeoutmins",
		// Unknown section
		"unknown.field",
		"deamon.idle_timeout_mins", // typo
		"Daemon.idle_timeout_mins", // capitalized
		// Unknown field in valid section
		"daemon.unknown_field",
		"daemon.idle_timeout", // typo
		"client.unknown_field",
		"ai.unknown_field",
		"ai.enable", // typo
		"suggestions.unknown_field",
		"privacy.unknown_field",
		"history.unknown_field",
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

func TestConfigSetInvalidKey(t *testing.T) {
	cfg := DefaultConfig()

	tests := []string{
		// Invalid format
		"daemonidletimeoutmins",
		"",
		"daemon",
		".",
		".idle_timeout_mins",
		"daemon.",
		"daemon.idle.timeout",
		"daemon.idle.timeout_mins",
		// Unknown section
		"unknown.field",
		"deamon.idle_timeout_mins",
		"Daemon.idle_timeout_mins",
		// Unknown field
		"daemon.unknown_field",
		"client.unknown_field",
		"ai.unknown_field",
		"suggestions.unknown_field",
		"privacy.unknown_field",
		"history.unknown_field",
	}

	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			err := cfg.Set(key, "value")
			if err == nil {
				t.Errorf("Set(%q, \"value\") should have failed", key)
			}
		})
	}
}

// ============================================================================
// Invalid value tests
// ============================================================================

func TestConfigSetInvalidValue(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		// Invalid integers
		{"daemon.idle_timeout_mins", "not_a_number"},
		{"daemon.idle_timeout_mins", "12.5"},
		{"daemon.idle_timeout_mins", ""},
		{"daemon.idle_timeout_mins", "abc123"},
		{"client.suggest_timeout_ms", "invalid"},
		{"client.connect_timeout_ms", "3.14"},
		{"ai.cache_ttl_hours", "twenty"},
		{"suggestions.max_history", "five"},
		{"suggestions.max_ai", "1.5"},
		{"history.picker_page_size", "not_a_number"},
		// Invalid booleans (Go's strconv.ParseBool accepts: 1,0,t,f,T,F,true,false,TRUE,FALSE,True,False)
		{"client.fire_and_forget", "yes"},
		{"client.fire_and_forget", "no"},
		{"client.fire_and_forget", ""},
		{"client.auto_start_daemon", "YES"},
		{"ai.enabled", "enable"},
		{"ai.auto_diagnose", "on"},
		{"suggestions.show_risk_warning", "off"},
		{"privacy.sanitize_ai_calls", "maybe"},
		{"history.picker_open_on_empty", "yes"},
		{"history.picker_case_sensitive", "maybe"},
		// Invalid log level
		{"daemon.log_level", "trace"},
		{"daemon.log_level", "DEBUG"},
		{"daemon.log_level", "Info"},
		{"daemon.log_level", "WARNING"},
		{"daemon.log_level", "fatal"},
		{"daemon.log_level", ""},
		// Invalid provider
		{"ai.provider", "claude"},
		{"ai.provider", "gpt4"},
		{"ai.provider", "gemini"},
		{"ai.provider", "ANTHROPIC"},
		{"ai.provider", ""},
		// Invalid picker backend
		{"history.picker_backend", "invalid"},
		{"history.picker_backend", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err == nil {
				t.Errorf("Set(%q, %q) should have failed", tt.key, tt.value)
			}
		})
	}
}

// ============================================================================
// Validation tests
// ============================================================================

func TestConfigValidate(t *testing.T) {
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
			name:    "negative_idle_timeout",
			modify:  func(c *Config) { c.Daemon.IdleTimeoutMins = -1 },
			wantErr: "daemon.idle_timeout_mins must be >= 0",
		},
		{
			name:    "invalid_log_level_empty",
			modify:  func(c *Config) { c.Daemon.LogLevel = "" },
			wantErr: "daemon.log_level must be debug, info, warn, or error",
		},
		{
			name:    "invalid_log_level_unknown",
			modify:  func(c *Config) { c.Daemon.LogLevel = "trace" },
			wantErr: "daemon.log_level must be debug, info, warn, or error",
		},
		{
			name:    "negative_suggest_timeout",
			modify:  func(c *Config) { c.Client.SuggestTimeoutMs = -1 },
			wantErr: "client.suggest_timeout_ms must be >= 0",
		},
		{
			name:    "negative_connect_timeout",
			modify:  func(c *Config) { c.Client.ConnectTimeoutMs = -1 },
			wantErr: "client.connect_timeout_ms must be >= 0",
		},
		{
			name:    "invalid_provider_empty",
			modify:  func(c *Config) { c.AI.Provider = "" },
			wantErr: "ai.provider must be anthropic or auto",
		},
		{
			name:    "invalid_provider_unknown",
			modify:  func(c *Config) { c.AI.Provider = "unknown" },
			wantErr: "ai.provider must be anthropic or auto",
		},
		{
			name:    "negative_cache_ttl",
			modify:  func(c *Config) { c.AI.CacheTTLHours = -1 },
			wantErr: "ai.cache_ttl_hours must be >= 0",
		},
		{
			name:    "negative_max_history",
			modify:  func(c *Config) { c.Suggestions.MaxHistory = -1 },
			wantErr: "suggestions.max_history must be >= 0",
		},
		{
			name:    "negative_max_ai",
			modify:  func(c *Config) { c.Suggestions.MaxAI = -1 },
			wantErr: "suggestions.max_ai must be >= 0",
		},
		{
			name:    "invalid_picker_backend",
			modify:  func(c *Config) { c.History.PickerBackend = "invalid" },
			wantErr: "history.picker_backend",
		},
		{
			name:    "empty_picker_backend",
			modify:  func(c *Config) { c.History.PickerBackend = "" },
			wantErr: "history.picker_backend",
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
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestValidatePickerPageSizeClamping(t *testing.T) {
	tests := []struct {
		name        string
		pageSize    int
		wantClamped int
	}{
		{"below_minimum", 5, 20},
		{"at_minimum", 20, 20},
		{"normal", 100, 100},
		{"at_maximum", 500, 500},
		{"above_maximum", 999, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.History.PickerPageSize = tt.pageSize
			err := cfg.Validate()
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
			if cfg.History.PickerPageSize != tt.wantClamped {
				t.Errorf("PickerPageSize = %d, want %d", cfg.History.PickerPageSize, tt.wantClamped)
			}
		})
	}
}

func TestSetPickerPageSizeClamping(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"below_minimum", "5", "20"},
		{"at_minimum", "20", "20"},
		{"normal", "100", "100"},
		{"at_maximum", "500", "500"},
		{"above_maximum", "999", "500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set("history.picker_page_size", tt.value)
			if err != nil {
				t.Errorf("Set picker_page_size=%q error: %v", tt.value, err)
				return
			}
			got, _ := cfg.Get("history.picker_page_size")
			if got != tt.expected {
				t.Errorf("picker_page_size=%q: got %q, want %q", tt.value, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Validator helper tests
// ============================================================================

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

func TestValidPickerBackends(t *testing.T) {
	validBackends := []string{"builtin", "fzf", "clai"}
	for _, backend := range validBackends {
		if !isValidPickerBackend(backend) {
			t.Errorf("isValidPickerBackend(%q) = false, want true", backend)
		}
	}

	invalidBackends := []string{"BUILTIN", "Fzf", "custom", ""}
	for _, backend := range invalidBackends {
		if isValidPickerBackend(backend) {
			t.Errorf("isValidPickerBackend(%q) = true, want false", backend)
		}
	}
}

// ============================================================================
// File I/O tests
// ============================================================================

func TestLoadFromFile_NonExistent(t *testing.T) {
	cfg, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile should return defaults for nonexistent file: %v", err)
	}

	if cfg.Daemon.IdleTimeoutMins != 0 {
		t.Errorf("Expected default idle_timeout_mins=0, got %d", cfg.Daemon.IdleTimeoutMins)
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `
daemon:
  idle_timeout_mins: [not valid yaml
  this is broken
`
	if err := os.WriteFile(configFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write invalid YAML: %v", err)
	}

	_, err := LoadFromFile(configFile)
	if err == nil {
		t.Error("LoadFromFile should have returned an error for invalid YAML")
	}
}

func TestLoadFromFile_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

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
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed for empty file: %v", err)
	}

	if cfg.Daemon.IdleTimeoutMins != 0 {
		t.Errorf("Expected default idle_timeout_mins=0, got %d", cfg.Daemon.IdleTimeoutMins)
	}
}

func TestLoadFromFile_ReadError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory and try to read it as a file
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	_, err := LoadFromFile(subDir)
	if err == nil {
		t.Error("LoadFromFile should have returned an error when reading a directory")
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config with custom values
	cfg := DefaultConfig()
	cfg.Daemon.IdleTimeoutMins = 45
	cfg.AI.Enabled = true
	cfg.AI.Provider = "anthropic"

	// Save
	err := cfg.SaveToFile(configFile)
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

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
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
		History: HistoryConfig{
			PickerBackend:       "fzf",
			PickerOpenOnEmpty:   true,
			PickerPageSize:      50,
			PickerCaseSensitive: true,
			PickerTabs: []TabDef{
				{ID: "session", Label: "Session", Provider: "history", Args: map[string]string{"session": "$CLAI_SESSION_ID"}},
			},
		},
		Workflows: WorkflowsConfig{
			Enabled:           true,
			StrictPermissions: true,
			DefaultMode:       "interactive",
			RetainRuns:        50,
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

	// Verify all Daemon values
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

	// Verify all Client values
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

	// Verify all AI values
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

	// Verify all Suggestions values
	if loaded.Suggestions.MaxHistory != 15 {
		t.Errorf("Suggestions.MaxHistory: got %d, want 15", loaded.Suggestions.MaxHistory)
	}
	if loaded.Suggestions.MaxAI != 8 {
		t.Errorf("Suggestions.MaxAI: got %d, want 8", loaded.Suggestions.MaxAI)
	}
	if loaded.Suggestions.ShowRiskWarning != false {
		t.Errorf("Suggestions.ShowRiskWarning: got %v, want false", loaded.Suggestions.ShowRiskWarning)
	}

	// Verify Privacy
	if loaded.Privacy.SanitizeAICalls != false {
		t.Errorf("Privacy.SanitizeAICalls: got %v, want false", loaded.Privacy.SanitizeAICalls)
	}

	// Verify all History values
	if loaded.History.PickerBackend != "fzf" {
		t.Errorf("History.PickerBackend: got %s, want fzf", loaded.History.PickerBackend)
	}
	if loaded.History.PickerOpenOnEmpty != true {
		t.Errorf("History.PickerOpenOnEmpty: got %v, want true", loaded.History.PickerOpenOnEmpty)
	}
	if loaded.History.PickerPageSize != 50 {
		t.Errorf("History.PickerPageSize: got %d, want 50", loaded.History.PickerPageSize)
	}
	if loaded.History.PickerCaseSensitive != true {
		t.Errorf("History.PickerCaseSensitive: got %v, want true", loaded.History.PickerCaseSensitive)
	}

	// Verify Workflows values round-trip.
	if loaded.Workflows.Enabled != true {
		t.Errorf("Workflows.Enabled: got %v, want true", loaded.Workflows.Enabled)
	}
	if loaded.Workflows.StrictPermissions != true {
		t.Errorf("Workflows.StrictPermissions: got %v, want true", loaded.Workflows.StrictPermissions)
	}
	if loaded.Workflows.DefaultMode != "interactive" {
		t.Errorf("Workflows.DefaultMode: got %s, want interactive", loaded.Workflows.DefaultMode)
	}
	if loaded.Workflows.RetainRuns != 50 {
		t.Errorf("Workflows.RetainRuns: got %d, want 50", loaded.Workflows.RetainRuns)
	}
}

// ============================================================================
// ListKeys tests
// ============================================================================

func TestListKeys(t *testing.T) {
	keys := ListKeys()

	if len(keys) == 0 {
		t.Error("ListKeys returned empty list")
	}

	// Only user-facing keys are exposed via ListKeys()
	expectedKeys := []string{
		"suggestions.enabled",
		"suggestions.max_history",
		"suggestions.show_risk_warning",
		"history.picker_backend",
		"history.picker_open_on_empty",
		"history.picker_page_size",
		"history.picker_case_sensitive",
		"history.up_arrow_opens_history",
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

	testValues := map[string]string{
		"suggestions.enabled":            "false",
		"suggestions.max_history":        "10",
		"suggestions.show_risk_warning":  "false",
		"history.picker_backend":         "fzf",
		"history.picker_open_on_empty":   "true",
		"history.picker_page_size":       "50",
		"history.picker_case_sensitive":  "true",
		"history.up_arrow_opens_history": "true",
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
// Default history config tests
// ============================================================================

func TestDefaultHistoryConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.History.PickerBackend != "builtin" {
		t.Errorf("Expected picker_backend=builtin, got %s", cfg.History.PickerBackend)
	}
	if cfg.History.PickerOpenOnEmpty {
		t.Error("Expected picker_open_on_empty=false")
	}
	if cfg.History.PickerPageSize != 100 {
		t.Errorf("Expected picker_page_size=100, got %d", cfg.History.PickerPageSize)
	}
	if cfg.History.PickerCaseSensitive {
		t.Error("Expected picker_case_sensitive=false")
	}
	if len(cfg.History.PickerTabs) != 2 {
		t.Fatalf("Expected 2 default tabs, got %d", len(cfg.History.PickerTabs))
	}
	if cfg.History.PickerTabs[0].ID != "session" {
		t.Errorf("Expected first tab id=session, got %s", cfg.History.PickerTabs[0].ID)
	}
	if cfg.History.PickerTabs[1].ID != "global" {
		t.Errorf("Expected second tab id=global, got %s", cfg.History.PickerTabs[1].ID)
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid, but Validate() returned: %v", err)
	}
}

// ============================================================================
// Workflow config tests
// ============================================================================

func TestDefaultWorkflowsConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Workflows.Enabled {
		t.Error("Expected workflows.enabled=false by default")
	}
	if cfg.Workflows.DefaultMode != "interactive" {
		t.Errorf("Expected workflows.default_mode=interactive, got %s", cfg.Workflows.DefaultMode)
	}
	if cfg.Workflows.DefaultShell != "" {
		t.Errorf("Expected workflows.default_shell empty, got %s", cfg.Workflows.DefaultShell)
	}
	if cfg.Workflows.LogDir != "" {
		t.Errorf("Expected workflows.log_dir empty, got %s", cfg.Workflows.LogDir)
	}
	if cfg.Workflows.RetainRuns != 100 {
		t.Errorf("Expected workflows.retain_runs=100, got %d", cfg.Workflows.RetainRuns)
	}
	if cfg.Workflows.StrictPermissions {
		t.Error("Expected workflows.strict_permissions=false by default")
	}
	if cfg.Workflows.SecretFile != "" {
		t.Errorf("Expected workflows.secret_file empty, got %s", cfg.Workflows.SecretFile)
	}
	if cfg.Workflows.SearchPaths != nil {
		t.Errorf("Expected workflows.search_paths nil, got %v", cfg.Workflows.SearchPaths)
	}
}

func TestWorkflowsConfigGet(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		key      string
		expected string
	}{
		{"workflows.enabled", "false"},
		{"workflows.default_mode", "interactive"},
		{"workflows.default_shell", ""},
		{"workflows.log_dir", ""},
		{"workflows.search_paths", ""},
		{"workflows.retain_runs", "100"},
		{"workflows.strict_permissions", "false"},
		{"workflows.secret_file", ""},
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

func TestWorkflowsConfigSet(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"workflows.enabled", "true", "true"},
		{"workflows.enabled", "false", "false"},
		{"workflows.default_mode", "interactive", "interactive"},
		{"workflows.default_mode", "non-interactive-fail", "non-interactive-fail"},
		{"workflows.default_shell", "/bin/bash", "/bin/bash"},
		{"workflows.default_shell", "", ""},
		{"workflows.log_dir", "/tmp/logs", "/tmp/logs"},
		{"workflows.log_dir", "", ""},
		{"workflows.search_paths", "/a,/b,/c", "/a,/b,/c"},
		{"workflows.search_paths", "", ""},
		{"workflows.retain_runs", "50", "50"},
		{"workflows.retain_runs", "1", "1"},
		{"workflows.strict_permissions", "true", "true"},
		{"workflows.strict_permissions", "false", "false"},
		{"workflows.secret_file", "/home/user/.secrets", "/home/user/.secrets"},
		{"workflows.secret_file", "", ""},
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

func TestWorkflowsConfigSetInvalid(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		// Invalid booleans
		{"workflows.enabled", "yes"},
		{"workflows.strict_permissions", "maybe"},
		// Invalid mode
		{"workflows.default_mode", "non-interactive-auto"},
		{"workflows.default_mode", "batch"},
		{"workflows.default_mode", ""},
		// Invalid retain_runs
		{"workflows.retain_runs", "not_a_number"},
		{"workflows.retain_runs", "0"},
		{"workflows.retain_runs", "-1"},
		// Unknown field
		{"workflows.unknown_field", "value"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := cfg.Set(tt.key, tt.value)
			if err == nil {
				t.Errorf("Set(%q, %q) should have failed", tt.key, tt.value)
			}
		})
	}
}

func TestWorkflowsConfigGetUnknownField(t *testing.T) {
	cfg := DefaultConfig()
	_, err := cfg.Get("workflows.unknown_field")
	if err == nil {
		t.Error("Get(workflows.unknown_field) should have failed")
	}
}

func TestWorkflowsConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr string
	}{
		{
			name:    "valid_interactive",
			modify:  func(c *Config) { c.Workflows.DefaultMode = "interactive" },
			wantErr: "",
		},
		{
			name:    "valid_non_interactive_fail",
			modify:  func(c *Config) { c.Workflows.DefaultMode = "non-interactive-fail" },
			wantErr: "",
		},
		{
			name:    "rejects_non_interactive_auto",
			modify:  func(c *Config) { c.Workflows.DefaultMode = "non-interactive-auto" },
			wantErr: "workflows.default_mode must be \"interactive\" or \"non-interactive-fail\"",
		},
		{
			name:    "rejects_invalid_mode",
			modify:  func(c *Config) { c.Workflows.DefaultMode = "batch" },
			wantErr: "workflows.default_mode must be \"interactive\" or \"non-interactive-fail\"",
		},
		{
			name:    "rejects_empty_mode",
			modify:  func(c *Config) { c.Workflows.DefaultMode = "" },
			wantErr: "workflows.default_mode must be \"interactive\" or \"non-interactive-fail\"",
		},
		{
			name:    "rejects_negative_retain_runs",
			modify:  func(c *Config) { c.Workflows.RetainRuns = -1 },
			wantErr: "invalid retain_runs: must be > 0",
		},
		{
			name: "rejects_zero_retain_runs",
			modify: func(c *Config) {
				c.Workflows.RetainRuns = 0
			},
			wantErr: "invalid retain_runs: must be > 0",
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
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestWorkflowsConfigYAMLParsing(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
workflows:
  enabled: true
  default_mode: non-interactive-fail
  default_shell: /bin/zsh
  log_dir: /tmp/wf-logs
  search_paths:
    - /home/user/workflows
    - /etc/clai/workflows
  retain_runs: 50
  strict_permissions: true
  secret_file: /home/user/.secrets
`
	if err := os.WriteFile(configFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML: %v", err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if !cfg.Workflows.Enabled {
		t.Error("Expected workflows.enabled=true")
	}
	if cfg.Workflows.DefaultMode != "non-interactive-fail" {
		t.Errorf("Expected default_mode=non-interactive-fail, got %s", cfg.Workflows.DefaultMode)
	}
	if cfg.Workflows.DefaultShell != "/bin/zsh" {
		t.Errorf("Expected default_shell=/bin/zsh, got %s", cfg.Workflows.DefaultShell)
	}
	if cfg.Workflows.LogDir != "/tmp/wf-logs" {
		t.Errorf("Expected log_dir=/tmp/wf-logs, got %s", cfg.Workflows.LogDir)
	}
	if len(cfg.Workflows.SearchPaths) != 2 {
		t.Fatalf("Expected 2 search paths, got %d", len(cfg.Workflows.SearchPaths))
	}
	if cfg.Workflows.SearchPaths[0] != "/home/user/workflows" {
		t.Errorf("Expected search_paths[0]=/home/user/workflows, got %s", cfg.Workflows.SearchPaths[0])
	}
	if cfg.Workflows.SearchPaths[1] != "/etc/clai/workflows" {
		t.Errorf("Expected search_paths[1]=/etc/clai/workflows, got %s", cfg.Workflows.SearchPaths[1])
	}
	if cfg.Workflows.RetainRuns != 50 {
		t.Errorf("Expected retain_runs=50, got %d", cfg.Workflows.RetainRuns)
	}
	if !cfg.Workflows.StrictPermissions {
		t.Error("Expected strict_permissions=true")
	}
	if cfg.Workflows.SecretFile != "/home/user/.secrets" {
		t.Errorf("Expected secret_file=/home/user/.secrets, got %s", cfg.Workflows.SecretFile)
	}
}

func TestWorkflowsConfigYAMLPartial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
workflows:
  enabled: true
`
	if err := os.WriteFile(configFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML: %v", err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if !cfg.Workflows.Enabled {
		t.Error("Expected workflows.enabled=true")
	}
	// retain_runs is inherited from DefaultConfig because it is omitted in YAML.
	if cfg.Workflows.RetainRuns != 100 {
		t.Errorf("Expected retain_runs inherited as 100, got %d", cfg.Workflows.RetainRuns)
	}
}

func TestValidWorkflowModes(t *testing.T) {
	validModes := []string{"interactive", "non-interactive-fail"}
	for _, mode := range validModes {
		if !isValidWorkflowMode(mode) {
			t.Errorf("isValidWorkflowMode(%q) = false, want true", mode)
		}
	}

	invalidModes := []string{"non-interactive-auto", "batch", "auto", "INTERACTIVE", ""}
	for _, mode := range invalidModes {
		if isValidWorkflowMode(mode) {
			t.Errorf("isValidWorkflowMode(%q) = true, want false", mode)
		}
	}
}

func TestWorkflowsConfigSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Workflows.Enabled = true
	cfg.Workflows.DefaultMode = "non-interactive-fail"
	cfg.Workflows.DefaultShell = "/bin/bash"
	cfg.Workflows.LogDir = "/tmp/logs"
	cfg.Workflows.SearchPaths = []string{"/a", "/b"}
	cfg.Workflows.RetainRuns = 25
	cfg.Workflows.StrictPermissions = true
	cfg.Workflows.SecretFile = "/home/user/.secrets"

	if err := cfg.SaveToFile(configFile); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if !loaded.Workflows.Enabled {
		t.Error("Expected workflows.enabled=true after round-trip")
	}
	if loaded.Workflows.DefaultMode != "non-interactive-fail" {
		t.Errorf("Expected default_mode=non-interactive-fail, got %s", loaded.Workflows.DefaultMode)
	}
	if loaded.Workflows.DefaultShell != "/bin/bash" {
		t.Errorf("Expected default_shell=/bin/bash, got %s", loaded.Workflows.DefaultShell)
	}
	if loaded.Workflows.LogDir != "/tmp/logs" {
		t.Errorf("Expected log_dir=/tmp/logs, got %s", loaded.Workflows.LogDir)
	}
	if len(loaded.Workflows.SearchPaths) != 2 || loaded.Workflows.SearchPaths[0] != "/a" || loaded.Workflows.SearchPaths[1] != "/b" {
		t.Errorf("Expected search_paths=[/a, /b], got %v", loaded.Workflows.SearchPaths)
	}
	if loaded.Workflows.RetainRuns != 25 {
		t.Errorf("Expected retain_runs=25, got %d", loaded.Workflows.RetainRuns)
	}
	if !loaded.Workflows.StrictPermissions {
		t.Error("Expected strict_permissions=true after round-trip")
	}
	if loaded.Workflows.SecretFile != "/home/user/.secrets" {
		t.Errorf("Expected secret_file=/home/user/.secrets, got %s", loaded.Workflows.SecretFile)
	}
}
