package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// Default values tests - verify all V2 defaults match spec Section 16
// ============================================================================

func TestDefaultSuggestionsConfig_Core(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "Enabled", s.Enabled, true)
	assertInt(t, "MaxResults", s.MaxResults, 5)
	assertInt(t, "CacheTTLMs", s.CacheTTLMs, 30000)
	assertInt(t, "HardTimeoutMs", s.HardTimeoutMs, 150)
	assertStr(t, "PickerView", s.PickerView, "detailed")
}

func TestDefaultSuggestionsConfig_HookTransport(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "HookConnectTimeoutMs", s.HookConnectTimeoutMs, 15)
	assertInt(t, "HookWriteTimeoutMs", s.HookWriteTimeoutMs, 20)
	assertStr(t, "SocketPath", s.SocketPath, "")
	assertInt(t, "IngestSyncWaitMs", s.IngestSyncWaitMs, 5)
	assertBool(t, "InteractiveRequireTTY", s.InteractiveRequireTTY, true)
	assertInt(t, "CmdRawMaxBytes", s.CmdRawMaxBytes, 16384)
	assertStr(t, "ShimMode", s.ShimMode, "auto")
}

func TestDefaultSuggestionsConfig_Weights(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertFloat(t, "Weights.Transition", s.Weights.Transition, 0.30)
	assertFloat(t, "Weights.Frequency", s.Weights.Frequency, 0.20)
	assertFloat(t, "Weights.Success", s.Weights.Success, 0.10)
	assertFloat(t, "Weights.Prefix", s.Weights.Prefix, 0.15)
	assertFloat(t, "Weights.Affinity", s.Weights.Affinity, 0.10)
	assertFloat(t, "Weights.Task", s.Weights.Task, 0.05)
	assertFloat(t, "Weights.Feedback", s.Weights.Feedback, 0.15)
	assertFloat(t, "Weights.RiskPenalty", s.Weights.RiskPenalty, 0.20)
	assertFloat(t, "Weights.ProjectTypeAffinity", s.Weights.ProjectTypeAffinity, 0.08)
	assertFloat(t, "Weights.FailureRecovery", s.Weights.FailureRecovery, 0.12)
}

func TestDefaultSuggestionsConfig_Learning(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "DecayHalfLifeHours", s.DecayHalfLifeHours, 168)
	assertFloat(t, "FeedbackBoostAccept", s.FeedbackBoostAccept, 0.10)
	assertFloat(t, "FeedbackPenaltyDismiss", s.FeedbackPenaltyDismiss, 0.08)
	assertInt(t, "SlotMaxValuesPerSlot", s.SlotMaxValuesPerSlot, 20)
	assertInt(t, "FeedbackMatchWindowMs", s.FeedbackMatchWindowMs, 5000)
	assertBool(t, "OnlineLearningEnabled", s.OnlineLearningEnabled, true)
	assertFloat(t, "OnlineLearningEta", s.OnlineLearningEta, 0.02)
	assertInt(t, "OnlineLearningEtaDecayConst", s.OnlineLearningEtaDecayConst, 500)
	assertFloat(t, "OnlineLearningEtaFloor", s.OnlineLearningEtaFloor, 0.001)
	assertInt(t, "OnlineLearningMinSamples", s.OnlineLearningMinSamples, 30)
	assertFloat(t, "WeightMin", s.WeightMin, 0.00)
	assertFloat(t, "WeightMax", s.WeightMax, 0.60)
	assertFloat(t, "WeightRiskMin", s.WeightRiskMin, 0.10)
	assertFloat(t, "WeightRiskMax", s.WeightRiskMax, 0.60)
	assertFloat(t, "SlotCorrelationMinConf", s.SlotCorrelationMinConf, 0.65)
}

func TestDefaultSuggestionsConfig_Backpressure(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "BurstEventsThreshold", s.BurstEventsThreshold, 10)
	assertInt(t, "BurstWindowMs", s.BurstWindowMs, 100)
	assertInt(t, "BurstQuietMs", s.BurstQuietMs, 500)
	assertInt(t, "IngestQueueMaxEvents", s.IngestQueueMaxEvents, 8192)
	assertInt(t, "IngestQueueMaxBytes", s.IngestQueueMaxBytes, 8388608)
}

func TestDefaultSuggestionsConfig_TaskDiscovery(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "TaskPlaybookEnabled", s.TaskPlaybookEnabled, true)
	assertStr(t, "TaskPlaybookPath", s.TaskPlaybookPath, ".clai/tasks.yaml")
	assertFloat(t, "TaskPlaybookBoost", s.TaskPlaybookBoost, 0.20)
}

func TestDefaultSuggestionsConfig_Search(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "SearchFTSEnabled", s.SearchFTSEnabled, true)
	assertInt(t, "SearchFallbackScanLimit", s.SearchFallbackScanLimit, 2000)
	assertStr(t, "SearchFTSTokenizer", s.SearchFTSTokenizer, "trigram")
	assertBool(t, "SearchDescribeEnabled", s.SearchDescribeEnabled, true)
	assertBool(t, "SearchAutoModeMerge", s.SearchAutoModeMerge, true)
	assertStr(t, "SearchTagVocabularyPath", s.SearchTagVocabularyPath, "")
}

func TestDefaultSuggestionsConfig_ProjectType(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "ProjectTypeDetectionEnabled", s.ProjectTypeDetectionEnabled, true)
	assertInt(t, "ProjectTypeCacheTTLMs", s.ProjectTypeCacheTTLMs, 60000)
}

func TestDefaultSuggestionsConfig_Pipeline(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "PipelineAwarenessEnabled", s.PipelineAwarenessEnabled, true)
	assertInt(t, "PipelineMaxSegments", s.PipelineMaxSegments, 8)
	assertInt(t, "PipelinePatternMinCount", s.PipelinePatternMinCount, 2)
}

func TestDefaultSuggestionsConfig_FailureRecovery(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "FailureRecoveryEnabled", s.FailureRecoveryEnabled, true)
	assertBool(t, "FailureRecoveryBootstrapEnabled", s.FailureRecoveryBootstrapEnabled, true)
	assertInt(t, "FailureRecoveryMinCount", s.FailureRecoveryMinCount, 2)
}

func TestDefaultSuggestionsConfig_Workflow(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "WorkflowDetectionEnabled", s.WorkflowDetectionEnabled, true)
	assertInt(t, "WorkflowMinSteps", s.WorkflowMinSteps, 3)
	assertInt(t, "WorkflowMaxSteps", s.WorkflowMaxSteps, 6)
	assertInt(t, "WorkflowMinOccurrences", s.WorkflowMinOccurrences, 3)
	assertInt(t, "WorkflowMaxGap", s.WorkflowMaxGap, 2)
	assertInt(t, "WorkflowActivationTimeoutMs", s.WorkflowActivationTimeoutMs, 600000)
	assertFloat(t, "WorkflowBoost", s.WorkflowBoost, 0.25)
	assertInt(t, "WorkflowMineIntervalMs", s.WorkflowMineIntervalMs, 600000)
}

func TestDefaultSuggestionsConfig_AdaptiveTiming(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "AdaptiveTimingEnabled", s.AdaptiveTimingEnabled, true)
	assertFloat(t, "TypingFastThresholdCPS", s.TypingFastThresholdCPS, 6.0)
	assertInt(t, "TypingPauseThresholdMs", s.TypingPauseThresholdMs, 300)
	assertInt(t, "TypingEagerPrefixLength", s.TypingEagerPrefixLength, 3)
}

func TestDefaultSuggestionsConfig_Alias(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "AliasResolutionEnabled", s.AliasResolutionEnabled, true)
	assertInt(t, "AliasMaxExpansionDepth", s.AliasMaxExpansionDepth, 3)
	assertBool(t, "AliasRenderPreferred", s.AliasRenderPreferred, true)
}

func TestDefaultSuggestionsConfig_Dismissal(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "DismissalLearnedThreshold", s.DismissalLearnedThreshold, 3)
	assertInt(t, "DismissalLearnedHalflifeHrs", s.DismissalLearnedHalflifeHrs, 720)
	assertInt(t, "DismissalTemporaryHalflifeMs", s.DismissalTemporaryHalflifeMs, 1800000)
}

func TestDefaultSuggestionsConfig_DirectoryScope(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "DirectoryScopingEnabled", s.DirectoryScopingEnabled, true)
	assertInt(t, "DirectoryScopeMaxDepth", s.DirectoryScopeMaxDepth, 3)
}

func TestDefaultSuggestionsConfig_Explainability(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "ExplainEnabled", s.ExplainEnabled, true)
	assertInt(t, "ExplainMaxReasons", s.ExplainMaxReasons, 3)
	assertFloat(t, "ExplainMinContribution", s.ExplainMinContribution, 0.05)
}

func TestDefaultSuggestionsConfig_ExtendedPlaybook(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "TaskPlaybookExtendedEnabled", s.TaskPlaybookExtendedEnabled, true)
	assertFloat(t, "TaskPlaybookAfterBoost", s.TaskPlaybookAfterBoost, 0.30)
	assertInt(t, "TaskPlaybookWorkflowSeedCount", s.TaskPlaybookWorkflowSeedCount, 100)
}

func TestDefaultSuggestionsConfig_Discovery(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertBool(t, "DiscoveryEnabled", s.DiscoveryEnabled, true)
	assertInt(t, "DiscoveryCooldownHours", s.DiscoveryCooldownHours, 24)
	assertFloat(t, "DiscoveryMaxConfidenceThreshold", s.DiscoveryMaxConfidenceThreshold, 0.3)
	assertBool(t, "DiscoverySourceProjectType", s.DiscoverySourceProjectType, true)
	assertBool(t, "DiscoverySourcePlaybook", s.DiscoverySourcePlaybook, true)
	assertBool(t, "DiscoverySourceToolCommon", s.DiscoverySourceToolCommon, true)
}

func TestDefaultSuggestionsConfig_Storage(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "RetentionDays", s.RetentionDays, 90)
	assertInt(t, "RetentionMaxEvents", s.RetentionMaxEvents, 500000)
	assertInt(t, "MaintenanceIntervalMs", s.MaintenanceIntervalMs, 300000)
	assertInt(t, "MaintenanceVacuumThresholdMB", s.MaintenanceVacuumThresholdMB, 100)
	assertInt(t, "SQLiteBusyTimeoutMs", s.SQLiteBusyTimeoutMs, 50)
}

func TestDefaultSuggestionsConfig_Cache(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "CacheMemoryBudgetMB", s.CacheMemoryBudgetMB, 50)
}

func TestDefaultSuggestionsConfig_Privacy(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertStr(t, "IncognitoMode", s.IncognitoMode, "ephemeral")
	assertBool(t, "RedactSensitiveTokens", s.RedactSensitiveTokens, true)
}

func TestDefaultSuggestionsConfig_Legacy(t *testing.T) {
	s := DefaultSuggestionsConfig()

	assertInt(t, "MaxHistory", s.MaxHistory, 5)
	assertInt(t, "MaxAI", s.MaxAI, 3)
	assertBool(t, "ShowRiskWarning", s.ShowRiskWarning, true)
}

// ============================================================================
// Validation tests - Section 16.1
// ============================================================================

func TestValidateAndFix_DefaultsProduceNoWarnings(t *testing.T) {
	s := DefaultSuggestionsConfig()
	warnings := s.ValidateAndFix()
	if len(warnings) != 0 {
		t.Errorf("DefaultSuggestionsConfig should produce no warnings, got %d:", len(warnings))
		for _, w := range warnings {
			t.Errorf("  %s: %s", w.Field, w.Message)
		}
	}
}

func TestValidateAndFix_NegativeTimeouts(t *testing.T) {
	defaults := DefaultSuggestionsConfig()
	tests := []struct {
		modify func(*SuggestionsConfig)
		check  func(*SuggestionsConfig) int
		name   string
		field  string
		defVal int
	}{
		{func(s *SuggestionsConfig) { s.HardTimeoutMs = 0 }, func(s *SuggestionsConfig) int { return s.HardTimeoutMs }, "hard_timeout_ms=0", "hard_timeout_ms", defaults.HardTimeoutMs},
		{func(s *SuggestionsConfig) { s.HardTimeoutMs = -1 }, func(s *SuggestionsConfig) int { return s.HardTimeoutMs }, "hard_timeout_ms=-1", "hard_timeout_ms", defaults.HardTimeoutMs},
		{func(s *SuggestionsConfig) { s.HookConnectTimeoutMs = 0 }, func(s *SuggestionsConfig) int { return s.HookConnectTimeoutMs }, "hook_connect_timeout_ms=0", "hook_connect_timeout_ms", defaults.HookConnectTimeoutMs},
		{func(s *SuggestionsConfig) { s.HookWriteTimeoutMs = -5 }, func(s *SuggestionsConfig) int { return s.HookWriteTimeoutMs }, "hook_write_timeout_ms=-5", "hook_write_timeout_ms", defaults.HookWriteTimeoutMs},
		{func(s *SuggestionsConfig) { s.IngestSyncWaitMs = 0 }, func(s *SuggestionsConfig) int { return s.IngestSyncWaitMs }, "ingest_sync_wait_ms=0", "ingest_sync_wait_ms", defaults.IngestSyncWaitMs},
		{func(s *SuggestionsConfig) { s.CacheTTLMs = -100 }, func(s *SuggestionsConfig) int { return s.CacheTTLMs }, "cache_ttl_ms=-100", "cache_ttl_ms", defaults.CacheTTLMs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			tt.modify(&s)
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, tt.field)
			if got := tt.check(&s); got != tt.defVal {
				t.Errorf("after validation, %s = %d, want default %d", tt.field, got, tt.defVal)
			}
		})
	}
}

func TestValidateAndFix_WeightsClamping(t *testing.T) {
	tests := []struct {
		name     string
		modify   func(*SuggestionsConfig)
		field    string
		expected float64
	}{
		{"negative_transition", func(s *SuggestionsConfig) { s.Weights.Transition = -0.5 }, "weights.transition", 0.0},
		{"above_one_frequency", func(s *SuggestionsConfig) { s.Weights.Frequency = 1.5 }, "weights.frequency", 1.0},
		{"negative_risk_penalty", func(s *SuggestionsConfig) { s.Weights.RiskPenalty = -0.01 }, "weights.risk_penalty", 0.0},
		{"above_one_feedback", func(s *SuggestionsConfig) { s.Weights.Feedback = 2.0 }, "weights.feedback", 1.0},
		{"above_one_project_type", func(s *SuggestionsConfig) { s.Weights.ProjectTypeAffinity = 99.0 }, "weights.project_type_affinity", 1.0},
		{"negative_failure_recovery", func(s *SuggestionsConfig) { s.Weights.FailureRecovery = -1.0 }, "weights.failure_recovery", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			tt.modify(&s)
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, tt.field)
			// Check the weight was clamped
			got := getWeightByName(&s, tt.field)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("%s = %f, want %f", tt.field, got, tt.expected)
			}
		})
	}
}

func TestValidateAndFix_ValidWeightsNoWarning(t *testing.T) {
	s := DefaultSuggestionsConfig()
	// All default weights are in [0.0, 1.0]
	warnings := s.ValidateAndFix()
	for _, w := range warnings {
		if len(w.Field) > 7 && w.Field[:7] == "weights" {
			t.Errorf("unexpected weight warning: %s: %s", w.Field, w.Message)
		}
	}
}

func TestValidateAndFix_ZeroCounts(t *testing.T) {
	defaults := DefaultSuggestionsConfig()
	tests := []struct {
		name   string
		modify func(*SuggestionsConfig)
		field  string
		defVal int
	}{
		{"max_results=0", func(s *SuggestionsConfig) { s.MaxResults = 0 }, "max_results", defaults.MaxResults},
		{"ingest_queue_max_events=0", func(s *SuggestionsConfig) { s.IngestQueueMaxEvents = 0 }, "ingest_queue_max_events", defaults.IngestQueueMaxEvents},
		{"burst_events_threshold=-1", func(s *SuggestionsConfig) { s.BurstEventsThreshold = -1 }, "burst_events_threshold", defaults.BurstEventsThreshold},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			tt.modify(&s)
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, tt.field)
		})
	}
}

func TestValidateAndFix_ByteSizes(t *testing.T) {
	defaults := DefaultSuggestionsConfig()
	tests := []struct {
		name   string
		modify func(*SuggestionsConfig)
		field  string
		defVal int
	}{
		{"cmd_raw_max_bytes=0", func(s *SuggestionsConfig) { s.CmdRawMaxBytes = 0 }, "cmd_raw_max_bytes", defaults.CmdRawMaxBytes},
		{"ingest_queue_max_bytes=-1", func(s *SuggestionsConfig) { s.IngestQueueMaxBytes = -1 }, "ingest_queue_max_bytes", defaults.IngestQueueMaxBytes},
		{"cache_memory_budget_mb=0", func(s *SuggestionsConfig) { s.CacheMemoryBudgetMB = 0 }, "cache_memory_budget_mb", defaults.CacheMemoryBudgetMB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			tt.modify(&s)
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, tt.field)
		})
	}
}

func TestValidateAndFix_RetentionDays(t *testing.T) {
	// 0 is valid (disables time pruning)
	s := DefaultSuggestionsConfig()
	s.RetentionDays = 0
	warnings := s.ValidateAndFix()
	assertNoWarning(t, warnings, "retention_days")

	// Negative is invalid
	s = DefaultSuggestionsConfig()
	s.RetentionDays = -1
	warnings = s.ValidateAndFix()
	assertWarningPresent(t, warnings, "retention_days")
	if s.RetentionDays != 90 {
		t.Errorf("retention_days = %d, want default 90", s.RetentionDays)
	}
}

func TestValidateAndFix_RetentionMaxEvents(t *testing.T) {
	// Below 1000 gets clamped
	s := DefaultSuggestionsConfig()
	s.RetentionMaxEvents = 500
	warnings := s.ValidateAndFix()
	assertWarningPresent(t, warnings, "retention_max_events")
	if s.RetentionMaxEvents != 1000 {
		t.Errorf("retention_max_events = %d, want 1000", s.RetentionMaxEvents)
	}

	// Exactly 1000 is valid
	s = DefaultSuggestionsConfig()
	s.RetentionMaxEvents = 1000
	warnings = s.ValidateAndFix()
	assertNoWarning(t, warnings, "retention_max_events")
}

func TestValidateAndFix_OnlineLearningEta(t *testing.T) {
	defaults := DefaultSuggestionsConfig()

	tests := []struct {
		name    string
		eta     float64
		wantFix bool
	}{
		{"zero", 0.0, true},
		{"negative", -0.01, true},
		{"above_one", 1.1, true},
		{"exactly_one", 1.0, false},
		{"valid_small", 0.001, false},
		{"valid_default", 0.02, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.OnlineLearningEta = tt.eta
			warnings := s.ValidateAndFix()
			if tt.wantFix {
				assertWarningPresent(t, warnings, "online_learning_eta")
				if s.OnlineLearningEta != defaults.OnlineLearningEta {
					t.Errorf("eta = %f, want default %f", s.OnlineLearningEta, defaults.OnlineLearningEta)
				}
			} else {
				assertNoWarning(t, warnings, "online_learning_eta")
			}
		})
	}
}

func TestValidateAndFix_OnlineLearningMinSamples(t *testing.T) {
	s := DefaultSuggestionsConfig()
	s.OnlineLearningMinSamples = 0
	warnings := s.ValidateAndFix()
	assertWarningPresent(t, warnings, "online_learning_min_samples")
	if s.OnlineLearningMinSamples != 30 {
		t.Errorf("min_samples = %d, want default 30", s.OnlineLearningMinSamples)
	}
}

func TestValidateAndFix_WorkflowMinMaxSteps(t *testing.T) {
	// min > max: both fall back to defaults
	s := DefaultSuggestionsConfig()
	s.WorkflowMinSteps = 10
	s.WorkflowMaxSteps = 5
	warnings := s.ValidateAndFix()
	assertWarningPresent(t, warnings, "workflow_min_steps/workflow_max_steps")
	if s.WorkflowMinSteps != 3 {
		t.Errorf("WorkflowMinSteps = %d, want default 3", s.WorkflowMinSteps)
	}
	if s.WorkflowMaxSteps != 6 {
		t.Errorf("WorkflowMaxSteps = %d, want default 6", s.WorkflowMaxSteps)
	}

	// min == max: valid
	s = DefaultSuggestionsConfig()
	s.WorkflowMinSteps = 4
	s.WorkflowMaxSteps = 4
	warnings = s.ValidateAndFix()
	assertNoWarning(t, warnings, "workflow_min_steps/workflow_max_steps")
}

func TestValidateAndFix_PipelineMaxSegments(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected int
		hasWarn  bool
	}{
		{"below_min", 1, 2, true},
		{"at_min", 2, 2, false},
		{"normal", 8, 8, false},
		{"at_max", 32, 32, false},
		{"above_max", 100, 32, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.PipelineMaxSegments = tt.value
			warnings := s.ValidateAndFix()
			if tt.hasWarn {
				assertWarningPresent(t, warnings, "pipeline_max_segments")
			} else {
				assertNoWarning(t, warnings, "pipeline_max_segments")
			}
			if s.PipelineMaxSegments != tt.expected {
				t.Errorf("PipelineMaxSegments = %d, want %d", s.PipelineMaxSegments, tt.expected)
			}
		})
	}
}

func TestValidateAndFix_DirectoryScopeMaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected int
		hasWarn  bool
	}{
		{"below_min", 0, 1, true},
		{"at_min", 1, 1, false},
		{"normal", 3, 3, false},
		{"at_max", 10, 10, false},
		{"above_max", 20, 10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.DirectoryScopeMaxDepth = tt.value
			warnings := s.ValidateAndFix()
			if tt.hasWarn {
				assertWarningPresent(t, warnings, "directory_scope_max_depth")
			} else {
				assertNoWarning(t, warnings, "directory_scope_max_depth")
			}
			if s.DirectoryScopeMaxDepth != tt.expected {
				t.Errorf("DirectoryScopeMaxDepth = %d, want %d", s.DirectoryScopeMaxDepth, tt.expected)
			}
		})
	}
}

func TestValidateAndFix_IncognitoMode(t *testing.T) {
	validModes := []string{"off", "ephemeral", "no_send"}
	for _, mode := range validModes {
		t.Run("valid_"+mode, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.IncognitoMode = mode
			warnings := s.ValidateAndFix()
			assertNoWarning(t, warnings, "incognito_mode")
			if s.IncognitoMode != mode {
				t.Errorf("IncognitoMode = %q, want %q", s.IncognitoMode, mode)
			}
		})
	}

	invalidModes := []string{"", "full", "ON", "disabled"}
	for _, mode := range invalidModes {
		t.Run("invalid_"+mode, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.IncognitoMode = mode
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, "incognito_mode")
			if s.IncognitoMode != "ephemeral" {
				t.Errorf("IncognitoMode = %q, want default ephemeral", s.IncognitoMode)
			}
		})
	}
}

func TestValidateAndFix_ShimMode(t *testing.T) {
	validModes := []string{"auto", "persistent", "oneshot"}
	for _, mode := range validModes {
		t.Run("valid_"+mode, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.ShimMode = mode
			warnings := s.ValidateAndFix()
			assertNoWarning(t, warnings, "shim_mode")
		})
	}

	invalidModes := []string{"", "manual", "AUTO"}
	for _, mode := range invalidModes {
		t.Run("invalid_"+mode, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.ShimMode = mode
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, "shim_mode")
			if s.ShimMode != "auto" {
				t.Errorf("ShimMode = %q, want auto", s.ShimMode)
			}
		})
	}
}

func TestValidateAndFix_SearchFTSTokenizer(t *testing.T) {
	validTokenizers := []string{"trigram", "unicode61"}
	for _, tok := range validTokenizers {
		t.Run("valid_"+tok, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.SearchFTSTokenizer = tok
			warnings := s.ValidateAndFix()
			assertNoWarning(t, warnings, "search_fts_tokenizer")
		})
	}

	invalidTokenizers := []string{"", "porter", "TRIGRAM"}
	for _, tok := range invalidTokenizers {
		t.Run("invalid_"+tok, func(t *testing.T) {
			s := DefaultSuggestionsConfig()
			s.SearchFTSTokenizer = tok
			warnings := s.ValidateAndFix()
			assertWarningPresent(t, warnings, "search_fts_tokenizer")
			if s.SearchFTSTokenizer != "trigram" {
				t.Errorf("SearchFTSTokenizer = %q, want trigram", s.SearchFTSTokenizer)
			}
		})
	}
}

func TestValidateAndFix_NeverPreventsStartup(t *testing.T) {
	// Create a maximally broken config
	s := SuggestionsConfig{
		HardTimeoutMs:            -1,
		HookConnectTimeoutMs:     -1,
		HookWriteTimeoutMs:       -1,
		IngestSyncWaitMs:         -1,
		CacheTTLMs:               -1,
		MaxResults:               0,
		IngestQueueMaxEvents:     0,
		BurstEventsThreshold:     0,
		CmdRawMaxBytes:           0,
		IngestQueueMaxBytes:      0,
		CacheMemoryBudgetMB:      0,
		RetentionDays:            -5,
		RetentionMaxEvents:       10,
		OnlineLearningEta:        0.0,
		OnlineLearningMinSamples: 0,
		WorkflowMinSteps:         10,
		WorkflowMaxSteps:         2,
		PipelineMaxSegments:      0,
		DirectoryScopeMaxDepth:   0,
		IncognitoMode:            "invalid",
		ShimMode:                 "invalid",
		SearchFTSTokenizer:       "invalid",
	}

	// Should not panic and should produce warnings
	warnings := s.ValidateAndFix()
	if len(warnings) == 0 {
		t.Error("expected warnings for maximally broken config")
	}

	// All values should be valid after fix
	defaults := DefaultSuggestionsConfig()
	if s.HardTimeoutMs != defaults.HardTimeoutMs {
		t.Errorf("HardTimeoutMs = %d, want default %d", s.HardTimeoutMs, defaults.HardTimeoutMs)
	}
	if s.IncognitoMode != "ephemeral" {
		t.Errorf("IncognitoMode = %q, want ephemeral", s.IncognitoMode)
	}
	if s.ShimMode != "auto" {
		t.Errorf("ShimMode = %q, want auto", s.ShimMode)
	}
}

// ============================================================================
// Environment variable override tests
// ============================================================================

func TestApplyEnvOverrides_SuggestionsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		expected bool
	}{
		{"disable", "false", false},
		{"enable", "true", true},
		{"zero", "0", false},
		{"one", "1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			t.Setenv("CLAI_SUGGESTIONS_ENABLED", tt.envVal)
			cfg.ApplyEnvOverrides()
			if cfg.Suggestions.Enabled != tt.expected {
				t.Errorf("CLAI_SUGGESTIONS_ENABLED=%q: Enabled = %v, want %v", tt.envVal, cfg.Suggestions.Enabled, tt.expected)
			}
		})
	}
}

func TestApplyEnvOverrides_SuggestionsEnabledInvalid(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("CLAI_SUGGESTIONS_ENABLED", "not-a-bool")
	cfg.ApplyEnvOverrides()
	// Should remain at default (true)
	if !cfg.Suggestions.Enabled {
		t.Error("invalid CLAI_SUGGESTIONS_ENABLED should not change value")
	}
}

func TestApplyEnvOverrides_Debug(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("CLAI_DEBUG", "true")
	cfg.ApplyEnvOverrides()
	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("CLAI_DEBUG=true: LogLevel = %q, want debug", cfg.Daemon.LogLevel)
	}
}

func TestApplyEnvOverrides_DebugFalse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Daemon.LogLevel = "warn"
	t.Setenv("CLAI_DEBUG", "false")
	cfg.ApplyEnvOverrides()
	// false should not change log level
	if cfg.Daemon.LogLevel != "warn" {
		t.Errorf("CLAI_DEBUG=false: LogLevel = %q, want warn (unchanged)", cfg.Daemon.LogLevel)
	}
}

func TestApplyEnvOverrides_LogLevel(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("CLAI_LOG_LEVEL", "error")
	cfg.ApplyEnvOverrides()
	if cfg.Daemon.LogLevel != "error" {
		t.Errorf("CLAI_LOG_LEVEL=error: LogLevel = %q, want error", cfg.Daemon.LogLevel)
	}
}

func TestApplyEnvOverrides_LogLevelInvalid(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("CLAI_LOG_LEVEL", "trace")
	cfg.ApplyEnvOverrides()
	// Should remain at default
	if cfg.Daemon.LogLevel != "info" {
		t.Errorf("invalid CLAI_LOG_LEVEL: LogLevel = %q, want info", cfg.Daemon.LogLevel)
	}
}

func TestApplyEnvOverrides_SocketPath(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("CLAI_SOCKET", "/tmp/custom.sock")
	cfg.ApplyEnvOverrides()
	if cfg.Daemon.SocketPath != "/tmp/custom.sock" {
		t.Errorf("CLAI_SOCKET: SocketPath = %q, want /tmp/custom.sock", cfg.Daemon.SocketPath)
	}
}

func TestApplyEnvOverrides_DebugAndLogLevel(t *testing.T) {
	// CLAI_LOG_LEVEL should take precedence over CLAI_DEBUG since it runs after
	cfg := DefaultConfig()
	t.Setenv("CLAI_DEBUG", "true")
	t.Setenv("CLAI_LOG_LEVEL", "warn")
	cfg.ApplyEnvOverrides()
	if cfg.Daemon.LogLevel != "warn" {
		t.Errorf("with both DEBUG and LOG_LEVEL set, LOG_LEVEL should win: got %q, want warn", cfg.Daemon.LogLevel)
	}
}

func TestApplyEnvOverrides_NoEnvVarsSet(t *testing.T) {
	cfg := DefaultConfig()
	// Unset all env vars
	for _, env := range []string{"CLAI_SUGGESTIONS_ENABLED", "CLAI_DEBUG", "CLAI_LOG_LEVEL", "CLAI_SOCKET"} {
		t.Setenv(env, "")
		os.Unsetenv(env)
	}
	cfg.ApplyEnvOverrides()
	// Should remain at defaults
	if !cfg.Suggestions.Enabled {
		t.Error("Suggestions.Enabled should remain true")
	}
	if cfg.Daemon.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.Daemon.LogLevel)
	}
}

// ============================================================================
// YAML round-trip test for V2 config
// ============================================================================

func TestSuggestionsConfigYAMLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Suggestions.MaxResults = 10
	cfg.Suggestions.HardTimeoutMs = 200
	cfg.Suggestions.Weights.Transition = 0.50
	cfg.Suggestions.ShimMode = "persistent"
	cfg.Suggestions.IncognitoMode = "off"
	cfg.Suggestions.WorkflowMinSteps = 2
	cfg.Suggestions.WorkflowMaxSteps = 8
	cfg.Suggestions.PipelineMaxSegments = 16

	if err := cfg.SaveToFile(configFile); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	assertInt(t, "MaxResults", loaded.Suggestions.MaxResults, 10)
	assertInt(t, "HardTimeoutMs", loaded.Suggestions.HardTimeoutMs, 200)
	assertFloat(t, "Weights.Transition", loaded.Suggestions.Weights.Transition, 0.50)
	assertStr(t, "ShimMode", loaded.Suggestions.ShimMode, "persistent")
	assertStr(t, "IncognitoMode", loaded.Suggestions.IncognitoMode, "off")
	assertInt(t, "WorkflowMinSteps", loaded.Suggestions.WorkflowMinSteps, 2)
	assertInt(t, "WorkflowMaxSteps", loaded.Suggestions.WorkflowMaxSteps, 8)
	assertInt(t, "PipelineMaxSegments", loaded.Suggestions.PipelineMaxSegments, 16)
}

func TestLoadFromFile_AppliesEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write a config with suggestions enabled
	cfg := DefaultConfig()
	if err := cfg.SaveToFile(configFile); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Env var should override file
	t.Setenv("CLAI_SUGGESTIONS_ENABLED", "false")

	loaded, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if loaded.Suggestions.Enabled {
		t.Error("expected suggestions disabled via env override")
	}
}

func TestLoadFromFile_NonExistentAppliesEnvOverrides(t *testing.T) {
	t.Setenv("CLAI_SOCKET", "/env/socket.sock")

	cfg, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if cfg.Daemon.SocketPath != "/env/socket.sock" {
		t.Errorf("SocketPath = %q, want /env/socket.sock", cfg.Daemon.SocketPath)
	}
}

// ============================================================================
// Enum validator tests
// ============================================================================

func TestIsValidIncognitoMode(t *testing.T) {
	valid := []string{"off", "ephemeral", "no_send"}
	for _, m := range valid {
		if !isValidIncognitoMode(m) {
			t.Errorf("isValidIncognitoMode(%q) = false, want true", m)
		}
	}
	invalid := []string{"", "OFF", "Ephemeral", "disabled", "all"}
	for _, m := range invalid {
		if isValidIncognitoMode(m) {
			t.Errorf("isValidIncognitoMode(%q) = true, want false", m)
		}
	}
}

func TestIsValidShimMode(t *testing.T) {
	valid := []string{"auto", "persistent", "oneshot"}
	for _, m := range valid {
		if !isValidShimMode(m) {
			t.Errorf("isValidShimMode(%q) = false, want true", m)
		}
	}
	invalid := []string{"", "AUTO", "manual", "fork"}
	for _, m := range invalid {
		if isValidShimMode(m) {
			t.Errorf("isValidShimMode(%q) = true, want false", m)
		}
	}
}

func TestIsValidFTSTokenizer(t *testing.T) {
	valid := []string{"trigram", "unicode61"}
	for _, m := range valid {
		if !isValidFTSTokenizer(m) {
			t.Errorf("isValidFTSTokenizer(%q) = false, want true", m)
		}
	}
	invalid := []string{"", "TRIGRAM", "porter", "icu"}
	for _, m := range invalid {
		if isValidFTSTokenizer(m) {
			t.Errorf("isValidFTSTokenizer(%q) = true, want false", m)
		}
	}
}

// ============================================================================
// Test helpers
// ============================================================================

func assertBool(t *testing.T, name string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func assertInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Errorf("%s = %f, want %f", name, got, want)
	}
}

func assertStr(t *testing.T, name string, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func assertWarningPresent(t *testing.T, warnings []ValidationWarning, field string) {
	t.Helper()
	for _, w := range warnings {
		if w.Field == field {
			return
		}
	}
	t.Errorf("expected warning for field %q, but none found", field)
}

func assertNoWarning(t *testing.T, warnings []ValidationWarning, field string) {
	t.Helper()
	for _, w := range warnings {
		if w.Field == field {
			t.Errorf("unexpected warning for field %q: %s", field, w.Message)
			return
		}
	}
}

func getWeightByName(s *SuggestionsConfig, name string) float64 {
	switch name {
	case "weights.transition":
		return s.Weights.Transition
	case "weights.frequency":
		return s.Weights.Frequency
	case "weights.success":
		return s.Weights.Success
	case "weights.prefix":
		return s.Weights.Prefix
	case "weights.affinity":
		return s.Weights.Affinity
	case "weights.task":
		return s.Weights.Task
	case "weights.feedback":
		return s.Weights.Feedback
	case "weights.risk_penalty":
		return s.Weights.RiskPenalty
	case "weights.project_type_affinity":
		return s.Weights.ProjectTypeAffinity
	case "weights.failure_recovery":
		return s.Weights.FailureRecovery
	default:
		return -999
	}
}
