package discovery

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config file errors.
var (
	ErrConfigNotFound = errors.New("discovery config not found")
	ErrConfigInvalid  = errors.New("discovery config invalid")
)

// Default config location.
const DefaultConfigPath = "~/.config/clai/discovery.yaml"

// Parser types per spec Section 10.2.2.
const (
	ParserTypeJSONKeys   = "json_keys"
	ParserTypeJSONArray  = "json_array"
	ParserTypeRegexLines = "regex_lines"
	ParserTypeMakeQP     = "make_qp"
)

// ConfigEntry represents a single discovery rule from the config file.
// Per spec Section 10.2.
type ConfigEntry struct {
	// FilePattern is the filename to match (e.g., "Justfile", "*.gradle").
	FilePattern string `yaml:"file_pattern"`

	// Kind is the task source identifier (e.g., "just", "gradle").
	Kind string `yaml:"kind"`

	// Runner is the command to execute for discovery.
	Runner string `yaml:"runner"`

	// Parser defines how to parse the runner output.
	Parser ParserConfig `yaml:"parser"`

	// TimeoutMs overrides the default timeout (optional).
	TimeoutMs int `yaml:"timeout_ms,omitempty"`

	// MaxOutputBytes overrides the default output limit (optional).
	MaxOutputBytes int64 `yaml:"max_output_bytes,omitempty"`
}

// ParserConfig defines how to parse discovery output.
type ParserConfig struct {
	// Type is the parser type: json_keys, json_array, regex_lines, make_qp.
	Type string `yaml:"type"`

	// Path is the JSON path for json_keys and json_array parsers.
	Path string `yaml:"path,omitempty"`

	// Pattern is the regex pattern for regex_lines parser.
	// First capture group defines the task name.
	Pattern string `yaml:"pattern,omitempty"`

	// compiledRegex is the compiled regex (internal).
	compiledRegex *regexp.Regexp
}

// Config represents the full discovery configuration.
type Config struct {
	Entries []ConfigEntry `yaml:"entries"`
}

// ConfigManager manages the discovery configuration with hot-reload support.
type ConfigManager struct {
	mu       sync.RWMutex
	config   *Config
	path     string
	logger   *slog.Logger
	onReload func(*Config) // Callback for hot-reload (e.g., SIGHUP)
}

// ConfigManagerOptions configures the config manager.
type ConfigManagerOptions struct {
	Path     string
	Logger   *slog.Logger
	OnReload func(*Config)
}

// NewConfigManager creates a new config manager.
func NewConfigManager(opts ConfigManagerOptions) *ConfigManager {
	if opts.Path == "" {
		opts.Path = expandPath(DefaultConfigPath)
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return &ConfigManager{
		path:     opts.Path,
		logger:   opts.Logger,
		onReload: opts.OnReload,
	}
}

// Load loads the config from disk.
// Returns ErrConfigNotFound if the file doesn't exist.
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.path)
	if err != nil {
		if os.IsNotExist(err) {
			cm.config = &Config{} // Empty config is valid
			return nil            // Not an error per spec - just no custom rules
		}
		return fmt.Errorf("%w: %w", ErrConfigNotFound, err)
	}

	config, err := parseConfig(data)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfigInvalid, err)
	}

	cm.config = config
	cm.logger.Info("discovery config loaded",
		"path", cm.path,
		"entries", len(config.Entries))

	return nil
}

// Reload reloads the config from disk and calls the onReload callback.
// Used for SIGHUP hot-reload.
func (cm *ConfigManager) Reload() error {
	if err := cm.Load(); err != nil {
		return err
	}

	if cm.onReload != nil {
		cm.mu.RLock()
		config := cm.config
		cm.mu.RUnlock()
		cm.onReload(config)
	}

	return nil
}

// Get returns the current config (thread-safe).
func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// GetEntries returns all config entries (thread-safe).
func (cm *ConfigManager) GetEntries() []ConfigEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return nil
	}
	return cm.config.Entries
}

// GetEntriesForFile returns config entries matching the given filename.
func (cm *ConfigManager) GetEntriesForFile(filename string) []ConfigEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return nil
	}

	var matches []ConfigEntry
	for _, entry := range cm.config.Entries {
		matched, _ := filepath.Match(entry.FilePattern, filename)
		if matched {
			matches = append(matches, entry)
		}
	}
	return matches
}

// parseConfig parses and validates YAML config data.
func parseConfig(data []byte) (*Config, error) {
	// Try parsing as a list directly (common format)
	var entries []ConfigEntry
	if err := yaml.Unmarshal(data, &entries); err == nil && len(entries) > 0 {
		config := &Config{Entries: entries}
		return validateConfig(config)
	}

	// Try parsing as object with entries key
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return validateConfig(&config)
}

// validateConfig validates the config and compiles regexes.
func validateConfig(config *Config) (*Config, error) {
	for i := range config.Entries {
		entry := &config.Entries[i]

		// Validate required fields
		if entry.FilePattern == "" {
			return nil, fmt.Errorf("entry %d: file_pattern is required", i)
		}
		if entry.Kind == "" {
			return nil, fmt.Errorf("entry %d: kind is required", i)
		}
		if entry.Runner == "" {
			return nil, fmt.Errorf("entry %d: runner is required", i)
		}
		if entry.Parser.Type == "" {
			return nil, fmt.Errorf("entry %d: parser.type is required", i)
		}

		// Validate parser type
		switch entry.Parser.Type {
		case ParserTypeJSONKeys, ParserTypeJSONArray:
			if entry.Parser.Path == "" {
				return nil, fmt.Errorf("entry %d: parser.path is required for %s", i, entry.Parser.Type)
			}
		case ParserTypeRegexLines:
			if entry.Parser.Pattern == "" {
				return nil, fmt.Errorf("entry %d: parser.pattern is required for regex_lines", i)
			}
			regex, err := regexp.Compile(entry.Parser.Pattern)
			if err != nil {
				return nil, fmt.Errorf("entry %d: invalid regex pattern: %w", i, err)
			}
			entry.Parser.compiledRegex = regex
		case ParserTypeMakeQP:
			// No additional fields required
		default:
			return nil, fmt.Errorf("entry %d: unknown parser type: %s", i, entry.Parser.Type)
		}

		// Validate file pattern is valid
		if _, err := filepath.Match(entry.FilePattern, "test"); err != nil {
			return nil, fmt.Errorf("entry %d: invalid file pattern: %w", i, err)
		}
	}

	return config, nil
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
