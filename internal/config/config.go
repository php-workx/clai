package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the clai configuration.
type Config struct {
	Daemon      DaemonConfig      `yaml:"daemon"`
	Client      ClientConfig      `yaml:"client"`
	AI          AIConfig          `yaml:"ai"`
	Suggestions SuggestionsConfig `yaml:"suggestions"`
	Privacy     PrivacyConfig     `yaml:"privacy"`
	History     HistoryConfig     `yaml:"history"`
}

// DaemonConfig holds daemon-related settings.
type DaemonConfig struct {
	IdleTimeoutMins int    `yaml:"idle_timeout_mins"` // Auto-shutdown after idle (0 = never)
	SocketPath      string `yaml:"socket_path"`       // Unix socket path (overrides default)
	LogLevel        string `yaml:"log_level"`         // debug, info, warn, error
	LogFile         string `yaml:"log_file"`          // Log file path (overrides default)
}

// ClientConfig holds client-related settings.
type ClientConfig struct {
	SuggestTimeoutMs int  `yaml:"suggest_timeout_ms"` // Max wait for suggestions
	ConnectTimeoutMs int  `yaml:"connect_timeout_ms"` // Socket connection timeout
	FireAndForget    bool `yaml:"fire_and_forget"`    // Don't wait for logging acks
	AutoStartDaemon  bool `yaml:"auto_start_daemon"`  // Auto-start daemon if not running
}

// AIConfig holds AI-related settings.
type AIConfig struct {
	Enabled       bool   `yaml:"enabled"`         // Must opt-in to AI features
	Provider      string `yaml:"provider"`        // anthropic or auto (Claude CLI only)
	Model         string `yaml:"model"`           // Provider-specific model
	AutoDiagnose  bool   `yaml:"auto_diagnose"`   // Auto-trigger diagnosis on non-zero exit
	CacheTTLHours int    `yaml:"cache_ttl_hours"` // AI response cache lifetime
}

// SuggestionsConfig holds suggestion-related settings.
type SuggestionsConfig struct {
	Enabled         bool `yaml:"enabled"`           // Master toggle for shell integration (on/off)
	MaxHistory      int  `yaml:"max_history"`       // Max history-based suggestions
	MaxAI           int  `yaml:"max_ai"`            // Max AI-generated suggestions
	ShowRiskWarning bool `yaml:"show_risk_warning"` // Highlight destructive commands
}

// PrivacyConfig holds privacy-related settings.
type PrivacyConfig struct {
	SanitizeAICalls bool `yaml:"sanitize_ai_calls"` // Apply regex sanitization before AI calls
}

// TabDef defines a tab in the history picker.
type TabDef struct {
	ID       string            `yaml:"id"`
	Label    string            `yaml:"label"`
	Provider string            `yaml:"provider"`
	Args     map[string]string `yaml:"args"`
}

// HistoryConfig holds history picker settings.
type HistoryConfig struct {
	PickerBackend       string   `yaml:"picker_backend"`         // builtin, fzf, or clai
	PickerOpenOnEmpty   bool     `yaml:"picker_open_on_empty"`   // Open picker when search is empty
	PickerPageSize      int      `yaml:"picker_page_size"`       // Number of items per page
	PickerCaseSensitive bool     `yaml:"picker_case_sensitive"`  // Case-sensitive search
	PickerTabs          []TabDef `yaml:"picker_tabs"`            // Tab definitions
	UpArrowOpensHistory bool     `yaml:"up_arrow_opens_history"` // Up arrow opens TUI picker (default: false)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Daemon: DaemonConfig{
			IdleTimeoutMins: 0,  // Never timeout - daemon runs until shell exits
			SocketPath:      "", // Use default from paths
			LogLevel:        "info",
			LogFile:         "", // Use default from paths
		},
		Client: ClientConfig{
			SuggestTimeoutMs: 50,
			ConnectTimeoutMs: 10,
			FireAndForget:    true,
			AutoStartDaemon:  true,
		},
		AI: AIConfig{
			Enabled:       false, // Must opt-in
			Provider:      "auto",
			Model:         "",
			AutoDiagnose:  false,
			CacheTTLHours: 24,
		},
		Suggestions: SuggestionsConfig{
			Enabled:         true,
			MaxHistory:      5,
			MaxAI:           3,
			ShowRiskWarning: true,
		},
		Privacy: PrivacyConfig{
			SanitizeAICalls: true,
		},
		History: HistoryConfig{
			PickerBackend:       "builtin",
			PickerOpenOnEmpty:   false,
			PickerPageSize:      100,
			PickerCaseSensitive: false,
			PickerTabs: []TabDef{
				{ID: "session", Label: "Session", Provider: "history", Args: map[string]string{"session": "$CLAI_SESSION_ID"}},
				{ID: "global", Label: "Global", Provider: "history", Args: map[string]string{"global": "true"}},
			},
		},
	}
}

// Load loads configuration from the default path.
func Load() (*Config, error) {
	paths := DefaultPaths()
	return LoadFromFile(paths.ConfigFile())
}

// LoadFromFile loads configuration from the specified file.
// If the file doesn't exist, returns default configuration.
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Return defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration to the default path.
func (c *Config) Save() error {
	paths := DefaultPaths()
	return c.SaveToFile(paths.ConfigFile())
}

// SaveToFile saves the configuration to the specified file.
func (c *Config) SaveToFile(path string) error {
	// Derive directory from path and ensure it exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Get retrieves a configuration value by dot-separated key.
// For example: "daemon.idle_timeout_mins" or "ai.enabled"
func (c *Config) Get(key string) (string, error) {
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return "", errors.New("key must be in format 'section.key'")
	}

	section, field := parts[0], parts[1]

	switch section {
	case "daemon":
		return c.getDaemonField(field)
	case "client":
		return c.getClientField(field)
	case "ai":
		return c.getAIField(field)
	case "suggestions":
		return c.getSuggestionsField(field)
	case "privacy":
		return c.getPrivacyField(field)
	case "history":
		return c.getHistoryField(field)
	default:
		return "", fmt.Errorf("unknown section: %s", section)
	}
}

// Set sets a configuration value by dot-separated key.
func (c *Config) Set(key, value string) error {
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return errors.New("key must be in format 'section.key'")
	}

	section, field := parts[0], parts[1]

	switch section {
	case "daemon":
		return c.setDaemonField(field, value)
	case "client":
		return c.setClientField(field, value)
	case "ai":
		return c.setAIField(field, value)
	case "suggestions":
		return c.setSuggestionsField(field, value)
	case "privacy":
		return c.setPrivacyField(field, value)
	case "history":
		return c.setHistoryField(field, value)
	default:
		return fmt.Errorf("unknown section: %s", section)
	}
}

func (c *Config) getDaemonField(field string) (string, error) {
	switch field {
	case "idle_timeout_mins":
		return strconv.Itoa(c.Daemon.IdleTimeoutMins), nil
	case "socket_path":
		return c.Daemon.SocketPath, nil
	case "log_level":
		return c.Daemon.LogLevel, nil
	case "log_file":
		return c.Daemon.LogFile, nil
	default:
		return "", fmt.Errorf("unknown field: daemon.%s", field)
	}
}

func (c *Config) setDaemonField(field, value string) error {
	switch field {
	case "idle_timeout_mins":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for idle_timeout_mins: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("invalid idle_timeout_mins: must be non-negative")
		}
		c.Daemon.IdleTimeoutMins = v
	case "socket_path":
		c.Daemon.SocketPath = value
	case "log_level":
		if !isValidLogLevel(value) {
			return fmt.Errorf("invalid log_level: %s (must be debug, info, warn, or error)", value)
		}
		c.Daemon.LogLevel = value
	case "log_file":
		c.Daemon.LogFile = value
	default:
		return fmt.Errorf("unknown field: daemon.%s", field)
	}
	return nil
}

func (c *Config) getClientField(field string) (string, error) {
	switch field {
	case "suggest_timeout_ms":
		return strconv.Itoa(c.Client.SuggestTimeoutMs), nil
	case "connect_timeout_ms":
		return strconv.Itoa(c.Client.ConnectTimeoutMs), nil
	case "fire_and_forget":
		return strconv.FormatBool(c.Client.FireAndForget), nil
	case "auto_start_daemon":
		return strconv.FormatBool(c.Client.AutoStartDaemon), nil
	default:
		return "", fmt.Errorf("unknown field: client.%s", field)
	}
}

func (c *Config) setClientField(field, value string) error {
	switch field {
	case "suggest_timeout_ms":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for suggest_timeout_ms: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("invalid suggest_timeout_ms: must be non-negative")
		}
		c.Client.SuggestTimeoutMs = v
	case "connect_timeout_ms":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for connect_timeout_ms: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("invalid connect_timeout_ms: must be non-negative")
		}
		c.Client.ConnectTimeoutMs = v
	case "fire_and_forget":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for fire_and_forget: %w", err)
		}
		c.Client.FireAndForget = v
	case "auto_start_daemon":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for auto_start_daemon: %w", err)
		}
		c.Client.AutoStartDaemon = v
	default:
		return fmt.Errorf("unknown field: client.%s", field)
	}
	return nil
}

func (c *Config) getAIField(field string) (string, error) {
	switch field {
	case "enabled":
		return strconv.FormatBool(c.AI.Enabled), nil
	case "provider":
		return c.AI.Provider, nil
	case "model":
		return c.AI.Model, nil
	case "auto_diagnose":
		return strconv.FormatBool(c.AI.AutoDiagnose), nil
	case "cache_ttl_hours":
		return strconv.Itoa(c.AI.CacheTTLHours), nil
	default:
		return "", fmt.Errorf("unknown field: ai.%s", field)
	}
}

func (c *Config) setAIField(field, value string) error {
	switch field {
	case "enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for enabled: %w", err)
		}
		c.AI.Enabled = v
	case "provider":
		if !isValidProvider(value) {
			return fmt.Errorf("invalid provider: %s (must be anthropic or auto)", value)
		}
		c.AI.Provider = value
	case "model":
		c.AI.Model = value
	case "auto_diagnose":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for auto_diagnose: %w", err)
		}
		c.AI.AutoDiagnose = v
	case "cache_ttl_hours":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for cache_ttl_hours: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("invalid cache_ttl_hours: must be non-negative")
		}
		c.AI.CacheTTLHours = v
	default:
		return fmt.Errorf("unknown field: ai.%s", field)
	}
	return nil
}

func (c *Config) getSuggestionsField(field string) (string, error) {
	switch field {
	case "enabled":
		return strconv.FormatBool(c.Suggestions.Enabled), nil
	case "max_history":
		return strconv.Itoa(c.Suggestions.MaxHistory), nil
	case "max_ai":
		return strconv.Itoa(c.Suggestions.MaxAI), nil
	case "show_risk_warning":
		return strconv.FormatBool(c.Suggestions.ShowRiskWarning), nil
	default:
		return "", fmt.Errorf("unknown field: suggestions.%s", field)
	}
}

func (c *Config) setSuggestionsField(field, value string) error {
	switch field {
	case "enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for enabled: %w", err)
		}
		c.Suggestions.Enabled = v
	case "max_history":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for max_history: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("invalid max_history: must be non-negative")
		}
		c.Suggestions.MaxHistory = v
	case "max_ai":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for max_ai: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("invalid max_ai: must be non-negative")
		}
		c.Suggestions.MaxAI = v
	case "show_risk_warning":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for show_risk_warning: %w", err)
		}
		c.Suggestions.ShowRiskWarning = v
	default:
		return fmt.Errorf("unknown field: suggestions.%s", field)
	}
	return nil
}

func (c *Config) getPrivacyField(field string) (string, error) {
	switch field {
	case "sanitize_ai_calls":
		return strconv.FormatBool(c.Privacy.SanitizeAICalls), nil
	default:
		return "", fmt.Errorf("unknown field: privacy.%s", field)
	}
}

func (c *Config) setPrivacyField(field, value string) error {
	switch field {
	case "sanitize_ai_calls":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for sanitize_ai_calls: %w", err)
		}
		c.Privacy.SanitizeAICalls = v
	default:
		return fmt.Errorf("unknown field: privacy.%s", field)
	}
	return nil
}

func (c *Config) getHistoryField(field string) (string, error) {
	switch field {
	case "picker_backend":
		return c.History.PickerBackend, nil
	case "picker_open_on_empty":
		return strconv.FormatBool(c.History.PickerOpenOnEmpty), nil
	case "picker_page_size":
		return strconv.Itoa(c.History.PickerPageSize), nil
	case "picker_case_sensitive":
		return strconv.FormatBool(c.History.PickerCaseSensitive), nil
	case "up_arrow_opens_history":
		return strconv.FormatBool(c.History.UpArrowOpensHistory), nil
	default:
		return "", fmt.Errorf("unknown field: history.%s", field)
	}
}

func (c *Config) setHistoryField(field, value string) error {
	switch field {
	case "picker_backend":
		if !isValidPickerBackend(value) {
			return fmt.Errorf("invalid picker_backend: %s (must be builtin, fzf, or clai)", value)
		}
		c.History.PickerBackend = value
	case "picker_open_on_empty":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for picker_open_on_empty: %w", err)
		}
		c.History.PickerOpenOnEmpty = v
	case "picker_page_size":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for picker_page_size: %w", err)
		}
		if v < 20 {
			v = 20
		}
		if v > 500 {
			v = 500
		}
		c.History.PickerPageSize = v
	case "picker_case_sensitive":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for picker_case_sensitive: %w", err)
		}
		c.History.PickerCaseSensitive = v
	case "up_arrow_opens_history":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for up_arrow_opens_history: %w", err)
		}
		c.History.UpArrowOpensHistory = v
	default:
		return fmt.Errorf("unknown field: history.%s", field)
	}
	return nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Daemon.IdleTimeoutMins < 0 {
		return errors.New("daemon.idle_timeout_mins must be >= 0")
	}

	if !isValidLogLevel(c.Daemon.LogLevel) {
		return fmt.Errorf("daemon.log_level must be debug, info, warn, or error (got: %s)", c.Daemon.LogLevel)
	}

	if c.Client.SuggestTimeoutMs < 0 {
		return errors.New("client.suggest_timeout_ms must be >= 0")
	}

	if c.Client.ConnectTimeoutMs < 0 {
		return errors.New("client.connect_timeout_ms must be >= 0")
	}

	if !isValidProvider(c.AI.Provider) {
		return fmt.Errorf("ai.provider must be anthropic or auto (got: %s)", c.AI.Provider)
	}

	if c.AI.CacheTTLHours < 0 {
		return errors.New("ai.cache_ttl_hours must be >= 0")
	}

	if c.Suggestions.MaxHistory < 0 {
		return errors.New("suggestions.max_history must be >= 0")
	}

	if c.Suggestions.MaxAI < 0 {
		return errors.New("suggestions.max_ai must be >= 0")
	}

	// Clamp picker page size to [20, 500]
	if c.History.PickerPageSize < 20 {
		c.History.PickerPageSize = 20
	}
	if c.History.PickerPageSize > 500 {
		c.History.PickerPageSize = 500
	}

	if !isValidPickerBackend(c.History.PickerBackend) {
		return fmt.Errorf("history.picker_backend must be builtin, fzf, or clai (got: %s)", c.History.PickerBackend)
	}

	return nil
}

func isValidLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

func isValidProvider(provider string) bool {
	switch provider {
	case "anthropic", "auto":
		return true
	default:
		return false
	}
}

func isValidPickerBackend(backend string) bool {
	switch backend {
	case "builtin", "fzf", "clai":
		return true
	default:
		return false
	}
}

// ListKeys returns user-facing configuration keys.
// Internal settings (daemon, client, ai, privacy) are not exposed.
func ListKeys() []string {
	return []string{
		"suggestions.enabled",
		"suggestions.max_history",
		"suggestions.show_risk_warning",
		"history.picker_backend",
		"history.picker_open_on_empty",
		"history.picker_page_size",
		"history.picker_case_sensitive",
		"history.up_arrow_opens_history",
	}
}
