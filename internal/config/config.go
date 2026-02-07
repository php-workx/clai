package config

import (
	"errors"
	"fmt"
	"log"
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
	// Legacy fields (pre-V2)
	MaxHistory      int  `yaml:"max_history"`       // Max history-based suggestions
	MaxAI           int  `yaml:"max_ai"`            // Max AI-generated suggestions
	ShowRiskWarning bool `yaml:"show_risk_warning"` // Highlight destructive commands

	// --- Core ---
	Enabled       bool `yaml:"enabled"`         // Master toggle for shell integration
	MaxResults    int  `yaml:"max_results"`     // Max suggestion results returned
	CacheTTLMs    int  `yaml:"cache_ttl_ms"`    // Cache time-to-live in ms
	HardTimeoutMs int  `yaml:"hard_timeout_ms"` // Hard timeout for suggestion generation in ms

	// --- Hook/transport ---
	HookConnectTimeoutMs  int    `yaml:"hook_connect_timeout_ms"` // Socket connect timeout in ms
	HookWriteTimeoutMs    int    `yaml:"hook_write_timeout_ms"`   // Socket write timeout in ms
	SocketPath            string `yaml:"socket_path"`             // Unix socket path (empty = auto)
	IngestSyncWaitMs      int    `yaml:"ingest_sync_wait_ms"`     // Sync wait for ingest in ms
	InteractiveRequireTTY bool   `yaml:"interactive_require_tty"` // Require TTY for interactive mode
	CmdRawMaxBytes        int    `yaml:"cmd_raw_max_bytes"`       // Max raw command size in bytes
	ShimMode              string `yaml:"shim_mode"`               // auto|persistent|oneshot

	// --- Ranking weights ---
	Weights SuggestionsWeights `yaml:"weights"` // Ranking weight configuration

	// --- Learning ---
	DecayHalfLifeHours          int     `yaml:"decay_half_life_hours"`              // Decay half-life in hours
	FeedbackBoostAccept         float64 `yaml:"feedback_boost_accept"`              // Boost for accepted suggestions
	FeedbackPenaltyDismiss      float64 `yaml:"feedback_penalty_dismiss"`           // Penalty for dismissed suggestions
	SlotMaxValuesPerSlot        int     `yaml:"slot_max_values_per_slot"`           // Max values per slot
	FeedbackMatchWindowMs       int     `yaml:"feedback_match_window_ms"`           // Feedback match window in ms
	OnlineLearningEnabled       bool    `yaml:"online_learning_enabled"`            // Enable online weight learning
	OnlineLearningEta           float64 `yaml:"online_learning_eta"`                // Online learning rate
	OnlineLearningEtaDecayConst int     `yaml:"online_learning_eta_decay_constant"` // Eta decay constant (samples)
	OnlineLearningEtaFloor      float64 `yaml:"online_learning_eta_floor"`          // Minimum learning rate
	OnlineLearningMinSamples    int     `yaml:"online_learning_min_samples"`        // Min samples before learning
	WeightMin                   float64 `yaml:"weight_min"`                         // Min learned weight value
	WeightMax                   float64 `yaml:"weight_max"`                         // Max learned weight value
	WeightRiskMin               float64 `yaml:"weight_risk_min"`                    // Min risk weight value
	WeightRiskMax               float64 `yaml:"weight_risk_max"`                    // Max risk weight value
	SlotCorrelationMinConf      float64 `yaml:"slot_correlation_min_confidence"`    // Min correlation confidence

	// --- Backpressure ---
	BurstEventsThreshold int `yaml:"burst_events_threshold"`  // Events before backpressure
	BurstWindowMs        int `yaml:"burst_window_ms"`         // Burst detection window in ms
	BurstQuietMs         int `yaml:"burst_quiet_ms"`          // Quiet period after burst in ms
	IngestQueueMaxEvents int `yaml:"ingest_queue_max_events"` // Max events in ingest queue
	IngestQueueMaxBytes  int `yaml:"ingest_queue_max_bytes"`  // Max bytes in ingest queue

	// --- Task discovery ---
	TaskPlaybookEnabled bool    `yaml:"task_playbook_enabled"` // Enable task playbooks
	TaskPlaybookPath    string  `yaml:"task_playbook_path"`    // Path to task playbook file
	TaskPlaybookBoost   float64 `yaml:"task_playbook_boost"`   // Boost for playbook matches

	// --- Search ---
	SearchFTSEnabled        bool   `yaml:"search_fts_enabled"`         // Enable full-text search
	SearchFallbackScanLimit int    `yaml:"search_fallback_scan_limit"` // Fallback scan limit
	SearchFTSTokenizer      string `yaml:"search_fts_tokenizer"`       // trigram|unicode61
	SearchDescribeEnabled   bool   `yaml:"search_describe_enabled"`    // Enable describe search
	SearchAutoModeMerge     bool   `yaml:"search_auto_mode_merge"`     // Auto-merge search modes
	SearchTagVocabularyPath string `yaml:"search_tag_vocabulary_path"` // Tag vocabulary path

	// --- Project type ---
	ProjectTypeDetectionEnabled bool `yaml:"project_type_detection_enabled"` // Enable project type detection
	ProjectTypeCacheTTLMs       int  `yaml:"project_type_cache_ttl_ms"`      // Project type cache TTL in ms

	// --- Pipeline ---
	PipelineAwarenessEnabled bool `yaml:"pipeline_awareness_enabled"` // Enable pipeline awareness
	PipelineMaxSegments      int  `yaml:"pipeline_max_segments"`      // Max pipeline segments
	PipelinePatternMinCount  int  `yaml:"pipeline_pattern_min_count"` // Min pattern count

	// --- Failure recovery ---
	FailureRecoveryEnabled          bool `yaml:"failure_recovery_enabled"`           // Enable failure recovery
	FailureRecoveryBootstrapEnabled bool `yaml:"failure_recovery_bootstrap_enabled"` // Enable bootstrap recovery
	FailureRecoveryMinCount         int  `yaml:"failure_recovery_min_count"`         // Min failures before recovery

	// --- Workflow ---
	WorkflowDetectionEnabled    bool    `yaml:"workflow_detection_enabled"`     // Enable workflow detection
	WorkflowMinSteps            int     `yaml:"workflow_min_steps"`             // Min steps in a workflow
	WorkflowMaxSteps            int     `yaml:"workflow_max_steps"`             // Max steps in a workflow
	WorkflowMinOccurrences      int     `yaml:"workflow_min_occurrences"`       // Min occurrences to learn workflow
	WorkflowMaxGap              int     `yaml:"workflow_max_gap"`               // Max gap between workflow steps
	WorkflowActivationTimeoutMs int     `yaml:"workflow_activation_timeout_ms"` // Workflow activation timeout in ms
	WorkflowBoost               float64 `yaml:"workflow_boost"`                 // Boost for workflow matches
	WorkflowMineIntervalMs      int     `yaml:"workflow_mine_interval_ms"`      // Workflow mining interval in ms

	// --- Adaptive timing ---
	AdaptiveTimingEnabled   bool    `yaml:"adaptive_timing_enabled"`    // Enable adaptive timing
	TypingFastThresholdCPS  float64 `yaml:"typing_fast_threshold_cps"`  // Fast typing threshold (chars/sec)
	TypingPauseThresholdMs  int     `yaml:"typing_pause_threshold_ms"`  // Pause detection threshold in ms
	TypingEagerPrefixLength int     `yaml:"typing_eager_prefix_length"` // Eager prefix length

	// --- Alias ---
	AliasResolutionEnabled bool `yaml:"alias_resolution_enabled"`  // Enable alias resolution
	AliasMaxExpansionDepth int  `yaml:"alias_max_expansion_depth"` // Max alias expansion depth
	AliasRenderPreferred   bool `yaml:"alias_render_preferred"`    // Prefer alias rendering

	// --- Dismissal ---
	DismissalLearnedThreshold    int `yaml:"dismissal_learned_threshold"`      // Dismissals before learned
	DismissalLearnedHalflifeHrs  int `yaml:"dismissal_learned_halflife_hours"` // Learned dismissal halflife in hours
	DismissalTemporaryHalflifeMs int `yaml:"dismissal_temporary_halflife_ms"`  // Temporary dismissal halflife in ms

	// --- Directory scope ---
	DirectoryScopingEnabled bool `yaml:"directory_scoping_enabled"` // Enable directory scoping
	DirectoryScopeMaxDepth  int  `yaml:"directory_scope_max_depth"` // Max directory scope depth

	// --- Scorer version ---
	ScorerVersion string `yaml:"scorer_version"` // v1|v2|blend - which suggestion scorer to use

	// --- Explainability ---
	ExplainEnabled         bool    `yaml:"explain_enabled"`          // Enable explainability
	ExplainMaxReasons      int     `yaml:"explain_max_reasons"`      // Max reasons in explanation
	ExplainMinContribution float64 `yaml:"explain_min_contribution"` // Min contribution to show

	// --- Extended playbook ---
	TaskPlaybookExtendedEnabled   bool    `yaml:"task_playbook_extended_enabled"`    // Enable extended playbooks
	TaskPlaybookAfterBoost        float64 `yaml:"task_playbook_after_boost"`         // After-failure boost
	TaskPlaybookWorkflowSeedCount int     `yaml:"task_playbook_workflow_seed_count"` // Workflow seed count

	// --- Discovery ---
	DiscoveryEnabled                bool    `yaml:"discovery_enabled"`                  // Enable discovery
	DiscoveryCooldownHours          int     `yaml:"discovery_cooldown_hours"`           // Cooldown between discoveries in hours
	DiscoveryMaxConfidenceThreshold float64 `yaml:"discovery_max_confidence_threshold"` // Max confidence for discovery
	DiscoverySourceProjectType      bool    `yaml:"discovery_source_project_type"`      // Source: project type
	DiscoverySourcePlaybook         bool    `yaml:"discovery_source_playbook"`          // Source: playbook
	DiscoverySourceToolCommon       bool    `yaml:"discovery_source_tool_common"`       // Source: common tools

	// --- Storage ---
	RetentionDays                int `yaml:"retention_days"`                  // History retention in days
	RetentionMaxEvents           int `yaml:"retention_max_events"`            // Max events to retain
	MaintenanceIntervalMs        int `yaml:"maintenance_interval_ms"`         // Maintenance interval in ms
	MaintenanceVacuumThresholdMB int `yaml:"maintenance_vacuum_threshold_mb"` // Vacuum threshold in MB
	SQLiteBusyTimeoutMs          int `yaml:"sqlite_busy_timeout_ms"`          // SQLite busy timeout in ms

	// --- Cache ---
	CacheMemoryBudgetMB int `yaml:"cache_memory_budget_mb"` // Memory budget for caches in MB

	// --- Privacy ---
	IncognitoMode         string `yaml:"incognito_mode"`          // off|ephemeral|no_send
	RedactSensitiveTokens bool   `yaml:"redact_sensitive_tokens"` // Redact sensitive tokens
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
		Suggestions: DefaultSuggestionsConfig(),
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

// DefaultSuggestionsConfig returns the default suggestions configuration
// with all values matching the spec (Section 16).
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
		ScorerVersion: "v1",

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

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.ApplyEnvOverrides()
			return cfg, nil // Return defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
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
	case "scorer_version":
		return c.Suggestions.ScorerVersion, nil
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
	case "scorer_version":
		if !isValidScorerVersion(value) {
			return fmt.Errorf("invalid scorer_version: %s (must be v1, v2, or blend)", value)
		}
		c.Suggestions.ScorerVersion = value
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
	if v := os.Getenv("CLAI_SOCKET_PATH"); v != "" {
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
		"history.picker_backend",
		"history.picker_open_on_empty",
		"history.picker_page_size",
		"history.picker_case_sensitive",
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
		w := ValidationWarning{Field: field, Message: msg}
		warnings = append(warnings, w)
		log.Printf("WARN config: suggestions.%s: %s", field, msg)
	}

	// --- Timeouts (must be >= 1) ---
	timeouts := []struct {
		name string
		val  *int
		def  int
	}{
		{"hard_timeout_ms", &s.HardTimeoutMs, defaults.HardTimeoutMs},
		{"hook_connect_timeout_ms", &s.HookConnectTimeoutMs, defaults.HookConnectTimeoutMs},
		{"hook_write_timeout_ms", &s.HookWriteTimeoutMs, defaults.HookWriteTimeoutMs},
		{"ingest_sync_wait_ms", &s.IngestSyncWaitMs, defaults.IngestSyncWaitMs},
		{"cache_ttl_ms", &s.CacheTTLMs, defaults.CacheTTLMs},
	}
	for _, t := range timeouts {
		if *t.val < 1 {
			warn(t.name, fmt.Sprintf("must be >= 1, got %d; falling back to default %d", *t.val, t.def))
			*t.val = t.def
		}
	}

	// --- Weights (clamp to [0.0, 1.0]) ---
	weightFields := []struct {
		name string
		val  *float64
	}{
		{"weights.transition", &s.Weights.Transition},
		{"weights.frequency", &s.Weights.Frequency},
		{"weights.success", &s.Weights.Success},
		{"weights.prefix", &s.Weights.Prefix},
		{"weights.affinity", &s.Weights.Affinity},
		{"weights.task", &s.Weights.Task},
		{"weights.feedback", &s.Weights.Feedback},
		{"weights.risk_penalty", &s.Weights.RiskPenalty},
		{"weights.project_type_affinity", &s.Weights.ProjectTypeAffinity},
		{"weights.failure_recovery", &s.Weights.FailureRecovery},
	}
	for _, w := range weightFields {
		if *w.val < 0.0 {
			warn(w.name, fmt.Sprintf("must be >= 0.0, got %f; clamping to 0.0", *w.val))
			*w.val = 0.0
		}
		if *w.val > 1.0 {
			warn(w.name, fmt.Sprintf("must be <= 1.0, got %f; clamping to 1.0", *w.val))
			*w.val = 1.0
		}
	}

	// --- Counts (must be >= 1) ---
	counts := []struct {
		name string
		val  *int
		def  int
	}{
		{"max_results", &s.MaxResults, defaults.MaxResults},
		{"ingest_queue_max_events", &s.IngestQueueMaxEvents, defaults.IngestQueueMaxEvents},
		{"burst_events_threshold", &s.BurstEventsThreshold, defaults.BurstEventsThreshold},
	}
	for _, c := range counts {
		if *c.val < 1 {
			warn(c.name, fmt.Sprintf("must be >= 1, got %d; falling back to default %d", *c.val, c.def))
			*c.val = c.def
		}
	}

	// --- Byte sizes (must be >= 1) ---
	byteSizes := []struct {
		name string
		val  *int
		def  int
	}{
		{"cmd_raw_max_bytes", &s.CmdRawMaxBytes, defaults.CmdRawMaxBytes},
		{"ingest_queue_max_bytes", &s.IngestQueueMaxBytes, defaults.IngestQueueMaxBytes},
		{"cache_memory_budget_mb", &s.CacheMemoryBudgetMB, defaults.CacheMemoryBudgetMB},
	}
	for _, b := range byteSizes {
		if *b.val < 1 {
			warn(b.name, fmt.Sprintf("must be >= 1, got %d; falling back to default %d", *b.val, b.def))
			*b.val = b.def
		}
	}

	// --- Retention days (>= 0; 0 = disable time pruning) ---
	if s.RetentionDays < 0 {
		warn("retention_days", fmt.Sprintf("must be >= 0, got %d; falling back to default %d", s.RetentionDays, defaults.RetentionDays))
		s.RetentionDays = defaults.RetentionDays
	}

	// --- Retention max events (>= 1000) ---
	if s.RetentionMaxEvents < 1000 {
		warn("retention_max_events", fmt.Sprintf("must be >= 1000, got %d; clamping to 1000", s.RetentionMaxEvents))
		s.RetentionMaxEvents = 1000
	}

	// --- Online learning eta (0.0, 1.0] ---
	if s.OnlineLearningEta <= 0.0 || s.OnlineLearningEta > 1.0 {
		warn("online_learning_eta", fmt.Sprintf("must be in (0.0, 1.0], got %f; falling back to default %f", s.OnlineLearningEta, defaults.OnlineLearningEta))
		s.OnlineLearningEta = defaults.OnlineLearningEta
	}

	// --- Online learning min samples (>= 1) ---
	if s.OnlineLearningMinSamples < 1 {
		warn("online_learning_min_samples", fmt.Sprintf("must be >= 1, got %d; falling back to default %d", s.OnlineLearningMinSamples, defaults.OnlineLearningMinSamples))
		s.OnlineLearningMinSamples = defaults.OnlineLearningMinSamples
	}

	// --- Workflow min/max steps range ---
	if s.WorkflowMinSteps > s.WorkflowMaxSteps {
		warn("workflow_min_steps/workflow_max_steps", fmt.Sprintf("min (%d) > max (%d); falling back to defaults min=%d, max=%d",
			s.WorkflowMinSteps, s.WorkflowMaxSteps, defaults.WorkflowMinSteps, defaults.WorkflowMaxSteps))
		s.WorkflowMinSteps = defaults.WorkflowMinSteps
		s.WorkflowMaxSteps = defaults.WorkflowMaxSteps
	}

	// --- Pipeline max segments (clamp to [2, 32]) ---
	if s.PipelineMaxSegments < 2 {
		warn("pipeline_max_segments", fmt.Sprintf("must be >= 2, got %d; clamping to 2", s.PipelineMaxSegments))
		s.PipelineMaxSegments = 2
	}
	if s.PipelineMaxSegments > 32 {
		warn("pipeline_max_segments", fmt.Sprintf("must be <= 32, got %d; clamping to 32", s.PipelineMaxSegments))
		s.PipelineMaxSegments = 32
	}

	// --- Directory scope max depth (clamp to [1, 10]) ---
	if s.DirectoryScopeMaxDepth < 1 {
		warn("directory_scope_max_depth", fmt.Sprintf("must be >= 1, got %d; clamping to 1", s.DirectoryScopeMaxDepth))
		s.DirectoryScopeMaxDepth = 1
	}
	if s.DirectoryScopeMaxDepth > 10 {
		warn("directory_scope_max_depth", fmt.Sprintf("must be <= 10, got %d; clamping to 10", s.DirectoryScopeMaxDepth))
		s.DirectoryScopeMaxDepth = 10
	}

	// --- Enum: incognito_mode ---
	if !isValidIncognitoMode(s.IncognitoMode) {
		warn("incognito_mode", fmt.Sprintf("must be off, ephemeral, or no_send, got %q; falling back to default %q", s.IncognitoMode, defaults.IncognitoMode))
		s.IncognitoMode = defaults.IncognitoMode
	}

	// --- Enum: shim_mode ---
	if !isValidShimMode(s.ShimMode) {
		warn("shim_mode", fmt.Sprintf("must be auto, persistent, or oneshot, got %q; falling back to auto", s.ShimMode))
		s.ShimMode = "auto"
	}

	// --- Enum: scorer_version ---
	if !isValidScorerVersion(s.ScorerVersion) {
		warn("scorer_version", fmt.Sprintf("must be v1, v2, or blend, got %q; falling back to v1", s.ScorerVersion))
		s.ScorerVersion = "v1"
	}

	// --- Enum: search_fts_tokenizer ---
	if !isValidFTSTokenizer(s.SearchFTSTokenizer) {
		warn("search_fts_tokenizer", fmt.Sprintf("must be trigram or unicode61, got %q; falling back to trigram", s.SearchFTSTokenizer))
		s.SearchFTSTokenizer = "trigram"
	}

	return warnings
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
	case "v1", "v2", "blend":
		return true
	default:
		return false
	}
}
