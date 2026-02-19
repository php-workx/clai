package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const minOneFallbackFmt = "must be >= 1, got %d; falling back to default %d"

// Config represents the clai configuration.
type Config struct {
	Daemon      DaemonConfig      `yaml:"daemon"`
	AI          AIConfig          `yaml:"ai"`
	Workflows   WorkflowsConfig   `yaml:"workflows"`
	History     HistoryConfig     `yaml:"history"`
	Suggestions SuggestionsConfig `yaml:"suggestions"`
	Client      ClientConfig      `yaml:"client"`
	Privacy     PrivacyConfig     `yaml:"privacy"`
}

// DaemonConfig holds daemon-related settings.
type DaemonConfig struct {
	SocketPath      string `yaml:"socket_path"`
	LogLevel        string `yaml:"log_level"`
	LogFile         string `yaml:"log_file"`
	IdleTimeoutMins int    `yaml:"idle_timeout_mins"`
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
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	CacheTTLHours int    `yaml:"cache_ttl_hours"`
	Enabled       bool   `yaml:"enabled"`
	AutoDiagnose  bool   `yaml:"auto_diagnose"`
}

// SuggestionsWeights holds ranking weight configuration.
type SuggestionsWeights struct {
	Transition          float64 `yaml:"transition"`            // Transition probability weight
	Frequency           float64 `yaml:"frequency"`             // Frequency weight
	Success             float64 `yaml:"success"`               // Success rate weight
	Prefix              float64 `yaml:"prefix"`                // Prefix match weight
	Affinity            float64 `yaml:"affinity"`              // Affinity weight
	Task                float64 `yaml:"task"`                  // Task context weight
	Feedback            float64 `yaml:"feedback"`              // Feedback weight
	RiskPenalty         float64 `yaml:"risk_penalty"`          // Risk penalty weight
	ProjectTypeAffinity float64 `yaml:"project_type_affinity"` // Project type affinity weight
	FailureRecovery     float64 `yaml:"failure_recovery"`      // Failure recovery weight
}

// SuggestionsConfig holds suggestion-related settings.
type SuggestionsConfig struct {
	SocketPath                      string             `yaml:"socket_path"`
	IncognitoMode                   string             `yaml:"incognito_mode"`
	ScorerVersion                   string             `yaml:"scorer_version"`
	SearchTagVocabularyPath         string             `yaml:"search_tag_vocabulary_path"`
	SearchFTSTokenizer              string             `yaml:"search_fts_tokenizer"`
	TaskPlaybookPath                string             `yaml:"task_playbook_path"`
	PickerView                      string             `yaml:"picker_view"`
	ShimMode                        string             `yaml:"shim_mode"`
	Weights                         SuggestionsWeights `yaml:"weights"`
	DismissalLearnedHalflifeHrs     int                `yaml:"dismissal_learned_halflife_hours"`
	FailureRecoveryMinCount         int                `yaml:"failure_recovery_min_count"`
	IngestSyncWaitMs                int                `yaml:"ingest_sync_wait_ms"`
	MaxAI                           int                `yaml:"max_ai"`
	CmdRawMaxBytes                  int                `yaml:"cmd_raw_max_bytes"`
	HookConnectTimeoutMs            int                `yaml:"hook_connect_timeout_ms"`
	HardTimeoutMs                   int                `yaml:"hard_timeout_ms"`
	DecayHalfLifeHours              int                `yaml:"decay_half_life_hours"`
	FeedbackBoostAccept             float64            `yaml:"feedback_boost_accept"`
	FeedbackPenaltyDismiss          float64            `yaml:"feedback_penalty_dismiss"`
	SlotMaxValuesPerSlot            int                `yaml:"slot_max_values_per_slot"`
	FeedbackMatchWindowMs           int                `yaml:"feedback_match_window_ms"`
	CacheMemoryBudgetMB             int                `yaml:"cache_memory_budget_mb"`
	OnlineLearningEta               float64            `yaml:"online_learning_eta"`
	OnlineLearningEtaDecayConst     int                `yaml:"online_learning_eta_decay_constant"`
	OnlineLearningEtaFloor          float64            `yaml:"online_learning_eta_floor"`
	OnlineLearningMinSamples        int                `yaml:"online_learning_min_samples"`
	WeightMin                       float64            `yaml:"weight_min"`
	WeightMax                       float64            `yaml:"weight_max"`
	WeightRiskMin                   float64            `yaml:"weight_risk_min"`
	WeightRiskMax                   float64            `yaml:"weight_risk_max"`
	SlotCorrelationMinConf          float64            `yaml:"slot_correlation_min_confidence"`
	BurstEventsThreshold            int                `yaml:"burst_events_threshold"`
	BurstWindowMs                   int                `yaml:"burst_window_ms"`
	BurstQuietMs                    int                `yaml:"burst_quiet_ms"`
	IngestQueueMaxEvents            int                `yaml:"ingest_queue_max_events"`
	IngestQueueMaxBytes             int                `yaml:"ingest_queue_max_bytes"`
	SQLiteBusyTimeoutMs             int                `yaml:"sqlite_busy_timeout_ms"`
	CacheTTLMs                      int                `yaml:"cache_ttl_ms"`
	TaskPlaybookBoost               float64            `yaml:"task_playbook_boost"`
	MaintenanceVacuumThresholdMB    int                `yaml:"maintenance_vacuum_threshold_mb"`
	SearchFallbackScanLimit         int                `yaml:"search_fallback_scan_limit"`
	MaxResults                      int                `yaml:"max_results"`
	MaintenanceIntervalMs           int                `yaml:"maintenance_interval_ms"`
	RetentionMaxEvents              int                `yaml:"retention_max_events"`
	RetentionDays                   int                `yaml:"retention_days"`
	DiscoveryMaxConfidenceThreshold float64            `yaml:"discovery_max_confidence_threshold"`
	ProjectTypeCacheTTLMs           int                `yaml:"project_type_cache_ttl_ms"`
	DiscoveryCooldownHours          int                `yaml:"discovery_cooldown_hours"`
	PipelineMaxSegments             int                `yaml:"pipeline_max_segments"`
	PipelinePatternMinCount         int                `yaml:"pipeline_pattern_min_count"`
	TaskPlaybookWorkflowSeedCount   int                `yaml:"task_playbook_workflow_seed_count"`
	HookWriteTimeoutMs              int                `yaml:"hook_write_timeout_ms"`
	TaskPlaybookAfterBoost          float64            `yaml:"task_playbook_after_boost"`
	ExplainMinContribution          float64            `yaml:"explain_min_contribution"`
	WorkflowMinSteps                int                `yaml:"workflow_min_steps"`
	WorkflowMaxSteps                int                `yaml:"workflow_max_steps"`
	WorkflowMinOccurrences          int                `yaml:"workflow_min_occurrences"`
	WorkflowMaxGap                  int                `yaml:"workflow_max_gap"`
	WorkflowActivationTimeoutMs     int                `yaml:"workflow_activation_timeout_ms"`
	WorkflowBoost                   float64            `yaml:"workflow_boost"`
	WorkflowMineIntervalMs          int                `yaml:"workflow_mine_interval_ms"`
	ExplainMaxReasons               int                `yaml:"explain_max_reasons"`
	TypingFastThresholdCPS          float64            `yaml:"typing_fast_threshold_cps"`
	TypingPauseThresholdMs          int                `yaml:"typing_pause_threshold_ms"`
	TypingEagerPrefixLength         int                `yaml:"typing_eager_prefix_length"`
	DirectoryScopeMaxDepth          int                `yaml:"directory_scope_max_depth"`
	AliasMaxExpansionDepth          int                `yaml:"alias_max_expansion_depth"`
	DismissalTemporaryHalflifeMs    int                `yaml:"dismissal_temporary_halflife_ms"`
	DismissalLearnedThreshold       int                `yaml:"dismissal_learned_threshold"`
	MaxHistory                      int                `yaml:"max_history"`
	TaskPlaybookEnabled             bool               `yaml:"task_playbook_enabled"`
	SearchDescribeEnabled           bool               `yaml:"search_describe_enabled"`
	AliasResolutionEnabled          bool               `yaml:"alias_resolution_enabled"`
	ShowRiskWarning                 bool               `yaml:"show_risk_warning"`
	ExplainEnabled                  bool               `yaml:"explain_enabled"`
	AdaptiveTimingEnabled           bool               `yaml:"adaptive_timing_enabled"`
	AliasRenderPreferred            bool               `yaml:"alias_render_preferred"`
	TaskPlaybookExtendedEnabled     bool               `yaml:"task_playbook_extended_enabled"`
	FailureRecoveryBootstrapEnabled bool               `yaml:"failure_recovery_bootstrap_enabled"`
	FailureRecoveryEnabled          bool               `yaml:"failure_recovery_enabled"`
	DirectoryScopingEnabled         bool               `yaml:"directory_scoping_enabled"`
	DiscoveryEnabled                bool               `yaml:"discovery_enabled"`
	Enabled                         bool               `yaml:"enabled"`
	PipelineAwarenessEnabled        bool               `yaml:"pipeline_awareness_enabled"`
	DiscoverySourcePlaybook         bool               `yaml:"discovery_source_playbook"`
	DiscoverySourceToolCommon       bool               `yaml:"discovery_source_tool_common"`
	DiscoverySourceProjectType      bool               `yaml:"discovery_source_project_type"`
	SearchAutoModeMerge             bool               `yaml:"search_auto_mode_merge"`
	WorkflowDetectionEnabled        bool               `yaml:"workflow_detection_enabled"`
	SearchFTSEnabled                bool               `yaml:"search_fts_enabled"`
	ProjectTypeDetectionEnabled     bool               `yaml:"project_type_detection_enabled"`
	OnlineLearningEnabled           bool               `yaml:"online_learning_enabled"`
	InteractiveRequireTTY           bool               `yaml:"interactive_require_tty"`
	RedactSensitiveTokens           bool               `yaml:"redact_sensitive_tokens"`
}

// PrivacyConfig holds privacy-related settings.
type PrivacyConfig struct {
	SanitizeAICalls bool `yaml:"sanitize_ai_calls"` // Apply regex sanitization before AI calls
}

// TabDef defines a tab in the history picker.
type TabDef struct {
	Args     map[string]string `yaml:"args"`
	ID       string            `yaml:"id"`
	Label    string            `yaml:"label"`
	Provider string            `yaml:"provider"`
}

// WorkflowsConfig holds workflow execution settings.
type WorkflowsConfig struct {
	DefaultMode       string   `yaml:"default_mode"`
	DefaultShell      string   `yaml:"default_shell"`
	LogDir            string   `yaml:"log_dir"`
	SecretFile        string   `yaml:"secret_file"`
	SearchPaths       []string `yaml:"search_paths"`
	RetainRuns        int      `yaml:"retain_runs"`
	Enabled           bool     `yaml:"enabled"`
	StrictPermissions bool     `yaml:"strict_permissions"`
}

// HistoryConfig holds history picker settings.
type HistoryConfig struct {
	PickerBackend         string   `yaml:"picker_backend"`
	UpArrowTrigger        string   `yaml:"up_arrow_trigger"`
	PickerTabs            []TabDef `yaml:"picker_tabs"`
	PickerPageSize        int      `yaml:"picker_page_size"`
	UpArrowDoubleWindowMs int      `yaml:"up_arrow_double_window_ms"`
	PickerOpenOnEmpty     bool     `yaml:"picker_open_on_empty"`
	PickerCaseSensitive   bool     `yaml:"picker_case_sensitive"`
	UpArrowOpensHistory   bool     `yaml:"up_arrow_opens_history"`
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
		Suggestions: DefaultSuggestionsConfig(),
		Privacy: PrivacyConfig{
			SanitizeAICalls: true,
		},
		Workflows: WorkflowsConfig{
			Enabled:      false,
			DefaultMode:  "interactive",
			DefaultShell: "",
			LogDir:       "",
			RetainRuns:   100,
		},
		History: HistoryConfig{
			PickerBackend:         "builtin",
			PickerOpenOnEmpty:     false,
			PickerPageSize:        100,
			PickerCaseSensitive:   false,
			UpArrowTrigger:        "single",
			UpArrowDoubleWindowMs: 250,
			PickerTabs: []TabDef{
				{ID: "session", Label: "Session", Provider: "history", Args: map[string]string{"session": "$CLAI_SESSION_ID"}},
				{ID: "global", Label: "Global", Provider: "history", Args: map[string]string{"global": "true"}},
			},
		},
	}
}

// DefaultSuggestionsConfig returns the default suggestions configuration
// with all values matching the spec (Section 16).
//
//nolint:funlen // Declarative defaults table kept in one place for clarity.
func DefaultSuggestionsConfig() SuggestionsConfig {
	return SuggestionsConfig{
		// Legacy fields
		MaxHistory:      5,
		MaxAI:           3,
		ShowRiskWarning: true,

		// Core
		Enabled:       true,
		MaxResults:    5,
		CacheTTLMs:    30000,
		HardTimeoutMs: 150,

		// UI
		PickerView: "detailed",

		// Hook/transport
		HookConnectTimeoutMs:  15,
		HookWriteTimeoutMs:    20,
		SocketPath:            "",
		IngestSyncWaitMs:      5,
		InteractiveRequireTTY: true,
		CmdRawMaxBytes:        16384,
		ShimMode:              "auto",

		// Ranking weights
		Weights: SuggestionsWeights{
			Transition:          0.30,
			Frequency:           0.20,
			Success:             0.10,
			Prefix:              0.15,
			Affinity:            0.10,
			Task:                0.05,
			Feedback:            0.15,
			RiskPenalty:         0.20,
			ProjectTypeAffinity: 0.08,
			FailureRecovery:     0.12,
		},

		// Learning
		DecayHalfLifeHours:          168,
		FeedbackBoostAccept:         0.10,
		FeedbackPenaltyDismiss:      0.08,
		SlotMaxValuesPerSlot:        20,
		FeedbackMatchWindowMs:       5000,
		OnlineLearningEnabled:       true,
		OnlineLearningEta:           0.02,
		OnlineLearningEtaDecayConst: 500,
		OnlineLearningEtaFloor:      0.001,
		OnlineLearningMinSamples:    30,
		WeightMin:                   0.00,
		WeightMax:                   0.60,
		WeightRiskMin:               0.10,
		WeightRiskMax:               0.60,
		SlotCorrelationMinConf:      0.65,

		// Backpressure
		BurstEventsThreshold: 10,
		BurstWindowMs:        100,
		BurstQuietMs:         500,
		IngestQueueMaxEvents: 8192,
		IngestQueueMaxBytes:  8388608,

		// Task discovery
		TaskPlaybookEnabled: true,
		TaskPlaybookPath:    ".clai/tasks.yaml",
		TaskPlaybookBoost:   0.20,

		// Search
		SearchFTSEnabled:        true,
		SearchFallbackScanLimit: 2000,
		SearchFTSTokenizer:      "trigram",
		SearchDescribeEnabled:   true,
		SearchAutoModeMerge:     true,
		SearchTagVocabularyPath: "",

		// Project type
		ProjectTypeDetectionEnabled: true,
		ProjectTypeCacheTTLMs:       60000,

		// Pipeline
		PipelineAwarenessEnabled: true,
		PipelineMaxSegments:      8,
		PipelinePatternMinCount:  2,

		// Failure recovery
		FailureRecoveryEnabled:          true,
		FailureRecoveryBootstrapEnabled: true,
		FailureRecoveryMinCount:         2,

		// Workflow
		WorkflowDetectionEnabled:    true,
		WorkflowMinSteps:            3,
		WorkflowMaxSteps:            6,
		WorkflowMinOccurrences:      3,
		WorkflowMaxGap:              2,
		WorkflowActivationTimeoutMs: 600000,
		WorkflowBoost:               0.25,
		WorkflowMineIntervalMs:      600000,

		// Adaptive timing
		AdaptiveTimingEnabled:   true,
		TypingFastThresholdCPS:  6.0,
		TypingPauseThresholdMs:  300,
		TypingEagerPrefixLength: 3,

		// Alias
		AliasResolutionEnabled: true,
		AliasMaxExpansionDepth: 3,
		AliasRenderPreferred:   true,

		// Dismissal
		DismissalLearnedThreshold:    3,
		DismissalLearnedHalflifeHrs:  720,
		DismissalTemporaryHalflifeMs: 1800000,

		// Directory scope
		DirectoryScopingEnabled: true,
		DirectoryScopeMaxDepth:  3,

		// Scorer version
		ScorerVersion: "v2",

		// Explainability
		ExplainEnabled:         true,
		ExplainMaxReasons:      3,
		ExplainMinContribution: 0.05,

		// Extended playbook
		TaskPlaybookExtendedEnabled:   true,
		TaskPlaybookAfterBoost:        0.30,
		TaskPlaybookWorkflowSeedCount: 100,

		// Discovery
		DiscoveryEnabled:                true,
		DiscoveryCooldownHours:          24,
		DiscoveryMaxConfidenceThreshold: 0.3,
		DiscoverySourceProjectType:      true,
		DiscoverySourcePlaybook:         true,
		DiscoverySourceToolCommon:       true,

		// Storage
		RetentionDays:                90,
		RetentionMaxEvents:           500000,
		MaintenanceIntervalMs:        300000,
		MaintenanceVacuumThresholdMB: 100,
		SQLiteBusyTimeoutMs:          50,

		// Cache
		CacheMemoryBudgetMB: 50,

		// Privacy
		IncognitoMode:         "ephemeral",
		RedactSensitiveTokens: true,
	}
}

// Load loads configuration from the default path.
func Load() (*Config, error) {
	paths := DefaultPaths()
	return LoadFromFile(paths.ConfigFile())
}

// LoadFromFile loads configuration from the specified file.
// If the file doesn't exist, returns default configuration.
// Environment variable overrides are applied after file loading.
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path) //nolint:gosec // G304: config file path is from trusted source
	if err != nil {
		if os.IsNotExist(err) {
			cfg.ApplyEnvOverrides()
			if err = cfg.Validate(); err != nil {
				return nil, fmt.Errorf("invalid config from environment: %w", err)
			}
			return cfg, nil // Return defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err = yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.ApplyEnvOverrides()

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
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: config directory needs standard permissions
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // G306: config file must be readable by user
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
	case "workflows":
		return c.getWorkflowsField(field)
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
	case "workflows":
		return c.setWorkflowsField(field, value)
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
	case "scorer_version":
		return c.Suggestions.ScorerVersion, nil
	case "picker_view":
		return c.Suggestions.PickerView, nil
	default:
		return "", fmt.Errorf("unknown field: suggestions.%s", field)
	}
}

func (c *Config) setSuggestionsField(field, value string) error {
	switch field {
	case "enabled":
		return c.setSuggestionsEnabled(value)
	case "max_history":
		return c.setSuggestionsMaxHistory(value)
	case "max_ai":
		return c.setSuggestionsMaxAI(value)
	case "show_risk_warning":
		return c.setSuggestionsShowRiskWarning(value)
	case "scorer_version":
		return c.setSuggestionsScorerVersion(value)
	case "picker_view":
		return c.setSuggestionsPickerView(value)
	default:
		return fmt.Errorf("unknown field: suggestions.%s", field)
	}
}

func (c *Config) setSuggestionsEnabled(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value for enabled: %w", err)
	}
	c.Suggestions.Enabled = v
	return nil
}

func (c *Config) setSuggestionsMaxHistory(value string) error {
	v, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for max_history: %w", err)
	}
	if v < 0 {
		return fmt.Errorf("invalid max_history: must be non-negative")
	}
	c.Suggestions.MaxHistory = v
	return nil
}

func (c *Config) setSuggestionsMaxAI(value string) error {
	v, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for max_ai: %w", err)
	}
	if v < 0 {
		return fmt.Errorf("invalid max_ai: must be non-negative")
	}
	c.Suggestions.MaxAI = v
	return nil
}

func (c *Config) setSuggestionsShowRiskWarning(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value for show_risk_warning: %w", err)
	}
	c.Suggestions.ShowRiskWarning = v
	return nil
}

func (c *Config) setSuggestionsScorerVersion(value string) error {
	if !isValidScorerVersion(value) {
		return fmt.Errorf("invalid scorer_version: %s (must be v1 or v2)", value)
	}
	c.Suggestions.ScorerVersion = value
	return nil
}

func (c *Config) setSuggestionsPickerView(value string) error {
	if !isValidSuggestionsPickerView(value) {
		return fmt.Errorf("invalid picker_view: %s (must be compact or detailed)", value)
	}
	c.Suggestions.PickerView = value
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
	case "up_arrow_trigger":
		return c.History.UpArrowTrigger, nil
	case "up_arrow_double_window_ms":
		return strconv.Itoa(c.History.UpArrowDoubleWindowMs), nil
	default:
		return "", fmt.Errorf("unknown field: history.%s", field)
	}
}

func (c *Config) setHistoryField(field, value string) error {
	switch field {
	case "picker_backend":
		return c.setHistoryPickerBackend(value)
	case "picker_open_on_empty":
		return c.setHistoryPickerOpenOnEmpty(value)
	case "picker_page_size":
		return c.setHistoryPickerPageSize(value)
	case "picker_case_sensitive":
		return c.setHistoryPickerCaseSensitive(value)
	case "up_arrow_opens_history":
		return c.setHistoryUpArrowOpensHistory(value)
	case "up_arrow_trigger":
		return c.setHistoryUpArrowTrigger(value)
	case "up_arrow_double_window_ms":
		return c.setHistoryUpArrowDoubleWindowMs(value)
	default:
		return fmt.Errorf("unknown field: history.%s", field)
	}
}

func (c *Config) setHistoryPickerBackend(value string) error {
	if !isValidPickerBackend(value) {
		return fmt.Errorf("invalid picker_backend: %s (must be builtin, fzf, or clai)", value)
	}
	c.History.PickerBackend = value
	return nil
}

func (c *Config) setHistoryPickerOpenOnEmpty(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value for picker_open_on_empty: %w", err)
	}
	c.History.PickerOpenOnEmpty = v
	return nil
}

func (c *Config) setHistoryPickerPageSize(value string) error {
	v, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for picker_page_size: %w", err)
	}
	orig := v
	if v < 20 {
		v = 20
	}
	if v > 500 {
		v = 500
	}
	if v != orig {
		slog.Warn("config: clamped history.picker_page_size", "input", orig, "clamped", v)
	}
	c.History.PickerPageSize = v
	return nil
}

func (c *Config) setHistoryPickerCaseSensitive(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value for picker_case_sensitive: %w", err)
	}
	c.History.PickerCaseSensitive = v
	return nil
}

func (c *Config) setHistoryUpArrowOpensHistory(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value for up_arrow_opens_history: %w", err)
	}
	c.History.UpArrowOpensHistory = v
	return nil
}

func (c *Config) setHistoryUpArrowTrigger(value string) error {
	if !isValidUpArrowTrigger(value) {
		return fmt.Errorf("invalid up_arrow_trigger: %s (must be single or double)", value)
	}
	c.History.UpArrowTrigger = value
	return nil
}

func (c *Config) setHistoryUpArrowDoubleWindowMs(value string) error {
	v, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for up_arrow_double_window_ms: %w", err)
	}
	orig := v
	if v < 50 {
		v = 50
	}
	if v > 1000 {
		v = 1000
	}
	if v != orig {
		slog.Warn("config: clamped history.up_arrow_double_window_ms", "input", orig, "clamped", v)
	}
	c.History.UpArrowDoubleWindowMs = v
	return nil
}

func (c *Config) getWorkflowsField(field string) (string, error) {
	switch field {
	case "enabled":
		return strconv.FormatBool(c.Workflows.Enabled), nil
	case "default_mode":
		return c.Workflows.DefaultMode, nil
	case "default_shell":
		return c.Workflows.DefaultShell, nil
	case "log_dir":
		return c.Workflows.LogDir, nil
	case "search_paths":
		return strings.Join(c.Workflows.SearchPaths, ","), nil
	case "retain_runs":
		return strconv.Itoa(c.Workflows.RetainRuns), nil
	case "strict_permissions":
		return strconv.FormatBool(c.Workflows.StrictPermissions), nil
	case "secret_file":
		return c.Workflows.SecretFile, nil
	default:
		return "", fmt.Errorf("unknown field: workflows.%s", field)
	}
}

func (c *Config) setWorkflowsField(field, value string) error {
	switch field {
	case "enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for enabled: %w", err)
		}
		c.Workflows.Enabled = v
	case "default_mode":
		if !isValidWorkflowMode(value) {
			return fmt.Errorf("invalid default_mode: %s (must be interactive or non-interactive-fail)", value)
		}
		c.Workflows.DefaultMode = value
	case "default_shell":
		c.Workflows.DefaultShell = value
	case "log_dir":
		c.Workflows.LogDir = value
	case "search_paths":
		if value == "" {
			c.Workflows.SearchPaths = nil
		} else {
			c.Workflows.SearchPaths = strings.Split(value, ",")
		}
	case "retain_runs":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for retain_runs: %w", err)
		}
		if v <= 0 {
			return fmt.Errorf("invalid retain_runs: must be > 0")
		}
		c.Workflows.RetainRuns = v
	case "strict_permissions":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for strict_permissions: %w", err)
		}
		c.Workflows.StrictPermissions = v
	case "secret_file":
		c.Workflows.SecretFile = value
	default:
		return fmt.Errorf("unknown field: workflows.%s", field)
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

	// Validate V2 suggestions config (never returns error; falls back to defaults with warnings)
	c.Suggestions.ValidateAndFix()

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
	if !isValidUpArrowTrigger(c.History.UpArrowTrigger) {
		return fmt.Errorf("history.up_arrow_trigger must be single or double (got: %s)", c.History.UpArrowTrigger)
	}
	if c.History.UpArrowDoubleWindowMs < 50 {
		c.History.UpArrowDoubleWindowMs = 50
	}
	if c.History.UpArrowDoubleWindowMs > 1000 {
		c.History.UpArrowDoubleWindowMs = 1000
	}

	if c.Workflows.DefaultMode == "" || !isValidWorkflowMode(c.Workflows.DefaultMode) {
		return fmt.Errorf("workflows.default_mode must be \"interactive\" or \"non-interactive-fail\" (got: %q)", c.Workflows.DefaultMode)
	}
	if c.Workflows.RetainRuns <= 0 {
		return errors.New("invalid retain_runs: must be > 0")
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

func isValidUpArrowTrigger(v string) bool {
	switch v {
	case "single", "double":
		return true
	default:
		return false
	}
}

func isValidWorkflowMode(mode string) bool {
	switch mode {
	case "interactive", "non-interactive-fail":
		return true
	default:
		return false
	}
}

// ApplyEnvOverrides applies environment variable overrides to the config.
// Environment variables override config file values per spec Section 16.
func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("CLAI_SUGGESTIONS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Suggestions.Enabled = b
		}
	}
	if v := os.Getenv("CLAI_DEBUG"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && b {
			c.Daemon.LogLevel = "debug"
		}
	}
	if v := os.Getenv("CLAI_LOG_LEVEL"); v != "" {
		if isValidLogLevel(v) {
			c.Daemon.LogLevel = v
		}
	}
	if v := os.Getenv("CLAI_SOCKET"); v != "" {
		c.Daemon.SocketPath = v
	}
}

// ListKeys returns user-facing configuration keys.
// Internal settings (daemon, client, ai, privacy) are not exposed.
func ListKeys() []string {
	return []string{
		"suggestions.enabled",
		"suggestions.max_history",
		"suggestions.show_risk_warning",
		"suggestions.scorer_version",
		"suggestions.picker_view",
		"history.picker_backend",
		"history.picker_open_on_empty",
		"history.picker_page_size",
		"history.picker_case_sensitive",
		"history.up_arrow_trigger",
		"history.up_arrow_double_window_ms",
	}
}

// ValidationWarning represents a config validation warning.
type ValidationWarning struct {
	Field   string
	Message string
}

// ValidateAndFix validates V2 suggestions config values.
// Invalid values are fixed by falling back to defaults or clamping.
// Returns a list of warnings for diagnostics. Validation never prevents startup.
func (s *SuggestionsConfig) ValidateAndFix() []ValidationWarning {
	defaults := DefaultSuggestionsConfig()
	var warnings []ValidationWarning

	warn := func(field, msg string) {
		warnings = append(warnings, ValidationWarning{Field: field, Message: msg})
		slog.Warn("config validation warning", "section", "suggestions", "field", field, "message", msg)
	}

	s.validateMinOneIntFields(warn, &defaults)
	s.validateWeightFields(warn)
	s.validateScalarFields(warn, &defaults)
	s.validateEnumFields(warn, &defaults)

	return warnings
}

// validateMinOneIntFields validates integer fields that must be >= 1, falling
// back to their default values when invalid.
func (s *SuggestionsConfig) validateMinOneIntFields(warn func(string, string), defaults *SuggestionsConfig) {
	fields := []struct {
		val  *int
		name string
		def  int
	}{
		// Timeouts
		{&s.HardTimeoutMs, "hard_timeout_ms", defaults.HardTimeoutMs},
		{&s.HookConnectTimeoutMs, "hook_connect_timeout_ms", defaults.HookConnectTimeoutMs},
		{&s.HookWriteTimeoutMs, "hook_write_timeout_ms", defaults.HookWriteTimeoutMs},
		{&s.IngestSyncWaitMs, "ingest_sync_wait_ms", defaults.IngestSyncWaitMs},
		{&s.CacheTTLMs, "cache_ttl_ms", defaults.CacheTTLMs},
		// Counts
		{&s.MaxResults, "max_results", defaults.MaxResults},
		{&s.IngestQueueMaxEvents, "ingest_queue_max_events", defaults.IngestQueueMaxEvents},
		{&s.BurstEventsThreshold, "burst_events_threshold", defaults.BurstEventsThreshold},
		// Byte sizes
		{&s.CmdRawMaxBytes, "cmd_raw_max_bytes", defaults.CmdRawMaxBytes},
		{&s.IngestQueueMaxBytes, "ingest_queue_max_bytes", defaults.IngestQueueMaxBytes},
		{&s.CacheMemoryBudgetMB, "cache_memory_budget_mb", defaults.CacheMemoryBudgetMB},
		// Online learning
		{&s.OnlineLearningMinSamples, "online_learning_min_samples", defaults.OnlineLearningMinSamples},
	}
	for _, f := range fields {
		if *f.val < 1 {
			warn(f.name, fmt.Sprintf(minOneFallbackFmt, *f.val, f.def))
			*f.val = f.def
		}
	}
}

// validateWeightFields clamps weight values to [0.0, 1.0].
func (s *SuggestionsConfig) validateWeightFields(warn func(string, string)) {
	fields := []struct {
		val  *float64
		name string
	}{
		{&s.Weights.Transition, "weights.transition"},
		{&s.Weights.Frequency, "weights.frequency"},
		{&s.Weights.Success, "weights.success"},
		{&s.Weights.Prefix, "weights.prefix"},
		{&s.Weights.Affinity, "weights.affinity"},
		{&s.Weights.Task, "weights.task"},
		{&s.Weights.Feedback, "weights.feedback"},
		{&s.Weights.RiskPenalty, "weights.risk_penalty"},
		{&s.Weights.ProjectTypeAffinity, "weights.project_type_affinity"},
		{&s.Weights.FailureRecovery, "weights.failure_recovery"},
	}
	for _, f := range fields {
		if *f.val < 0.0 {
			warn(f.name, fmt.Sprintf("must be >= 0.0, got %f; clamping to 0.0", *f.val))
			*f.val = 0.0
		}
		if *f.val > 1.0 {
			warn(f.name, fmt.Sprintf("must be <= 1.0, got %f; clamping to 1.0", *f.val))
			*f.val = 1.0
		}
	}
}

// validateScalarFields validates individual numeric fields with custom ranges.
func (s *SuggestionsConfig) validateScalarFields(warn func(string, string), defaults *SuggestionsConfig) {
	if s.RetentionDays < 0 {
		warn("retention_days", fmt.Sprintf("must be >= 0, got %d; falling back to default %d", s.RetentionDays, defaults.RetentionDays))
		s.RetentionDays = defaults.RetentionDays
	}
	if s.RetentionMaxEvents < 1000 {
		warn("retention_max_events", fmt.Sprintf("must be >= 1000, got %d; clamping to 1000", s.RetentionMaxEvents))
		s.RetentionMaxEvents = 1000
	}
	if s.OnlineLearningEta <= 0.0 || s.OnlineLearningEta > 1.0 {
		warn("online_learning_eta", fmt.Sprintf("must be in (0.0, 1.0], got %f; falling back to default %f", s.OnlineLearningEta, defaults.OnlineLearningEta))
		s.OnlineLearningEta = defaults.OnlineLearningEta
	}
	if s.WorkflowMinSteps > s.WorkflowMaxSteps {
		warn("workflow_min_steps/workflow_max_steps", fmt.Sprintf("min (%d) > max (%d); falling back to defaults min=%d, max=%d",
			s.WorkflowMinSteps, s.WorkflowMaxSteps, defaults.WorkflowMinSteps, defaults.WorkflowMaxSteps))
		s.WorkflowMinSteps = defaults.WorkflowMinSteps
		s.WorkflowMaxSteps = defaults.WorkflowMaxSteps
	}
	clampIntRange(warn, "pipeline_max_segments", &s.PipelineMaxSegments, 2, 32)
	clampIntRange(warn, "directory_scope_max_depth", &s.DirectoryScopeMaxDepth, 1, 10)
}

// validateEnumFields validates string fields that must match a set of allowed values.
func (s *SuggestionsConfig) validateEnumFields(warn func(string, string), defaults *SuggestionsConfig) {
	if !isValidIncognitoMode(s.IncognitoMode) {
		warn("incognito_mode", fmt.Sprintf("must be off, ephemeral, or no_send, got %q; falling back to default %q", s.IncognitoMode, defaults.IncognitoMode))
		s.IncognitoMode = defaults.IncognitoMode
	}
	if !isValidShimMode(s.ShimMode) {
		warn("shim_mode", fmt.Sprintf("must be auto, persistent, or oneshot, got %q; falling back to default %q", s.ShimMode, defaults.ShimMode))
		s.ShimMode = defaults.ShimMode
	}
	if !isValidScorerVersion(s.ScorerVersion) {
		warn("scorer_version", fmt.Sprintf("must be v1 or v2, got %q; falling back to default %q", s.ScorerVersion, defaults.ScorerVersion))
		s.ScorerVersion = defaults.ScorerVersion
	}
	if !isValidFTSTokenizer(s.SearchFTSTokenizer) {
		warn(
			"search_fts_tokenizer",
			fmt.Sprintf(
				"must be trigram or unicode61, got %q; falling back to default %q",
				s.SearchFTSTokenizer,
				defaults.SearchFTSTokenizer,
			),
		)
		s.SearchFTSTokenizer = defaults.SearchFTSTokenizer
	}
	if !isValidSuggestionsPickerView(s.PickerView) {
		warn("picker_view", fmt.Sprintf("must be compact or detailed, got %q; falling back to %q", s.PickerView, defaults.PickerView))
		s.PickerView = defaults.PickerView
	}
}

// clampIntRange clamps an integer field to [minValue, maxValue], emitting a warning if adjusted.
func clampIntRange(warn func(string, string), name string, val *int, minValue, maxValue int) {
	if *val < minValue {
		warn(name, fmt.Sprintf("must be >= %d, got %d; clamping to %d", minValue, *val, minValue))
		*val = minValue
	}
	if *val > maxValue {
		warn(name, fmt.Sprintf("must be <= %d, got %d; clamping to %d", maxValue, *val, maxValue))
		*val = maxValue
	}
}

func isValidIncognitoMode(mode string) bool {
	switch mode {
	case "off", "ephemeral", "no_send":
		return true
	default:
		return false
	}
}

func isValidShimMode(mode string) bool {
	switch mode {
	case "auto", "persistent", "oneshot":
		return true
	default:
		return false
	}
}

func isValidFTSTokenizer(tokenizer string) bool {
	switch tokenizer {
	case "trigram", "unicode61":
		return true
	default:
		return false
	}
}

func isValidScorerVersion(version string) bool {
	switch version {
	case "v1", "v2":
		return true
	default:
		return false
	}
}

func isValidSuggestionsPickerView(v string) bool {
	switch v {
	case "compact", "detailed":
		return true
	default:
		return false
	}
}
