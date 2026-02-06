package workflow

import (
	"sync"
	"time"
)

// TrackerConfig configures the session workflow tracker.
type TrackerConfig struct {
	// StaleAfterCommands is the number of commands without advancement before
	// an active workflow is considered stale and deactivated (default: 5).
	StaleAfterCommands int

	// MaxCandidates is the maximum number of next-step candidates returned
	// as suggestions (default: 3).
	MaxCandidates int

	// ActivationTimeoutMs is how long (ms) an active workflow remains valid
	// without any advancement before it times out (default: 600000 = 10 min).
	ActivationTimeoutMs int64
}

// DefaultTrackerConfig returns the default tracker configuration.
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		StaleAfterCommands:  5,
		MaxCandidates:       3,
		ActivationTimeoutMs: 600000,
	}
}

// activeWorkflow tracks progress through a single workflow pattern.
type activeWorkflow struct {
	Pattern              Pattern
	CurrentStep          int   // Index of the last completed step (0-based).
	CommandsSinceAdvance int   // Commands executed since last step advancement.
	ActivatedAtMs        int64 // Timestamp when the workflow was activated.
	LastAdvancedMs       int64 // Timestamp of the last step advancement.
}

// Tracker maintains in-memory state of active workflows for a session.
// When a user executes a command matching step N of a known workflow,
// the tracker activates it and provides next-step candidates.
type Tracker struct {
	mu       sync.Mutex
	cfg      TrackerConfig
	patterns []Pattern // Known workflow patterns (loaded from DB).
	active   []*activeWorkflow

	// nowFn returns current time in ms; overridable for testing.
	nowFn func() int64
}

// NewTracker creates a new session tracker with the given patterns and config.
func NewTracker(patterns []Pattern, cfg TrackerConfig) *Tracker {
	return &Tracker{
		cfg:      cfg,
		patterns: patterns,
		nowFn:    func() int64 { return time.Now().UnixMilli() },
	}
}

// SetPatterns replaces the known patterns. This is used to refresh patterns
// after a new mining pass.
func (t *Tracker) SetPatterns(patterns []Pattern) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.patterns = patterns
}

// Candidate represents a next-step suggestion from an active workflow.
type Candidate struct {
	PatternID    string `json:"pattern_id"`
	TemplateID   string `json:"template_id"`
	DisplayName  string `json:"display_name"`
	StepIndex    int    `json:"step_index"`
	TotalSteps   int    `json:"total_steps"`
	WorkflowName string `json:"workflow_name"` // Human-readable summary.
}

// OnCommand is called when the user executes a command. It updates active
// workflows and tries to activate new ones.
// Returns the list of next-step candidates (capped at MaxCandidates).
func (t *Tracker) OnCommand(templateID string) []Candidate {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFn()

	// Step 1: Advance or stale existing active workflows.
	t.advanceActive(templateID, now)

	// Step 2: Clean up stale/timed-out workflows.
	t.cleanStale(now)

	// Step 3: Try to activate new workflows.
	t.tryActivate(templateID, now)

	// Step 4: Collect next-step candidates.
	return t.collectCandidates()
}

// ActiveWorkflows returns the number of currently active workflows.
// Useful for diagnostics and testing.
func (t *Tracker) ActiveWorkflows() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.active)
}

// advanceActive advances or increments the stale counter for active workflows.
func (t *Tracker) advanceActive(templateID string, nowMs int64) {
	for _, aw := range t.active {
		nextStep := aw.CurrentStep + 1
		if nextStep < len(aw.Pattern.TemplateIDs) &&
			aw.Pattern.TemplateIDs[nextStep] == templateID {
			// Advance the workflow.
			aw.CurrentStep = nextStep
			aw.CommandsSinceAdvance = 0
			aw.LastAdvancedMs = nowMs
		} else {
			// Not the expected step; increment stale counter.
			aw.CommandsSinceAdvance++
		}
	}
}

// cleanStale removes workflows that are stale (too many commands without
// advancement) or timed out.
func (t *Tracker) cleanStale(nowMs int64) {
	kept := t.active[:0]
	for _, aw := range t.active {
		// Remove if stale.
		if aw.CommandsSinceAdvance >= t.cfg.StaleAfterCommands {
			continue
		}
		// Remove if timed out.
		if t.cfg.ActivationTimeoutMs > 0 && (nowMs-aw.LastAdvancedMs) > t.cfg.ActivationTimeoutMs {
			continue
		}
		// Remove if completed (all steps done).
		if aw.CurrentStep >= len(aw.Pattern.TemplateIDs)-1 {
			continue
		}
		kept = append(kept, aw)
	}
	t.active = kept
}

// tryActivate checks if the current command matches step 0 of any known
// pattern that is not already active.
func (t *Tracker) tryActivate(templateID string, nowMs int64) {
	activeIDs := make(map[string]bool)
	for _, aw := range t.active {
		activeIDs[aw.Pattern.PatternID] = true
	}

	for _, p := range t.patterns {
		if activeIDs[p.PatternID] {
			continue
		}
		if len(p.TemplateIDs) == 0 {
			continue
		}
		// Check if this command matches step 0.
		if p.TemplateIDs[0] == templateID {
			t.active = append(t.active, &activeWorkflow{
				Pattern:              p,
				CurrentStep:          0,
				CommandsSinceAdvance: 0,
				ActivatedAtMs:        nowMs,
				LastAdvancedMs:       nowMs,
			})
		}
	}
}

// collectCandidates gathers next-step suggestions from all active workflows.
func (t *Tracker) collectCandidates() []Candidate {
	seen := make(map[string]bool)
	var candidates []Candidate

	for _, aw := range t.active {
		nextStep := aw.CurrentStep + 1
		if nextStep >= len(aw.Pattern.TemplateIDs) {
			continue
		}

		tid := aw.Pattern.TemplateIDs[nextStep]
		if seen[tid] {
			continue
		}
		seen[tid] = true

		displayName := ""
		if nextStep < len(aw.Pattern.DisplayNames) {
			displayName = aw.Pattern.DisplayNames[nextStep]
		}

		workflowName := workflowSummary(aw.Pattern.DisplayNames)

		candidates = append(candidates, Candidate{
			PatternID:    aw.Pattern.PatternID,
			TemplateID:   tid,
			DisplayName:  displayName,
			StepIndex:    nextStep,
			TotalSteps:   len(aw.Pattern.TemplateIDs),
			WorkflowName: workflowName,
		})

		if len(candidates) >= t.cfg.MaxCandidates {
			break
		}
	}

	return candidates
}

// workflowSummary creates a human-readable summary of a workflow.
func workflowSummary(displayNames []string) string {
	if len(displayNames) == 0 {
		return ""
	}
	if len(displayNames) <= 3 {
		return joinNames(displayNames)
	}
	// For longer workflows, show first two + " ... " + last.
	return displayNames[0] + " -> " + displayNames[1] + " -> ... -> " + displayNames[len(displayNames)-1]
}

// joinNames joins display names with " -> ".
func joinNames(names []string) string {
	result := ""
	for i, n := range names {
		if i > 0 {
			result += " -> "
		}
		result += n
	}
	return result
}
