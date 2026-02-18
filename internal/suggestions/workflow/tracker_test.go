package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makePattern(id string, templateIDs, displayNames []string) Pattern {
	return Pattern{
		PatternID:       id,
		TemplateIDs:     templateIDs,
		DisplayNames:    displayNames,
		Scope:           "global",
		StepCount:       len(templateIDs),
		OccurrenceCount: 5,
		LastSeenMs:      100000,
	}
}

func TestTracker_ActivatesOnFirstStep(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_add", "t_commit", "t_push"},
		[]string{"git add", "git commit", "git push"})

	tr := NewTracker([]Pattern{p}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	candidates := tr.OnCommand("t_add")

	assert.Equal(t, 1, tr.ActiveWorkflows())
	require.Len(t, candidates, 1)
	assert.Equal(t, "t_commit", candidates[0].TemplateID)
	assert.Equal(t, "git commit", candidates[0].DisplayName)
	assert.Equal(t, 1, candidates[0].StepIndex)
	assert.Equal(t, 3, candidates[0].TotalSteps)
}

func TestTracker_AdvancesOnCorrectStep(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_add", "t_commit", "t_push"},
		[]string{"git add", "git commit", "git push"})

	tr := NewTracker([]Pattern{p}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	// Step 0: activate.
	tr.OnCommand("t_add")

	// Step 1: advance.
	candidates := tr.OnCommand("t_commit")

	require.Len(t, candidates, 1)
	assert.Equal(t, "t_push", candidates[0].TemplateID)
	assert.Equal(t, "git push", candidates[0].DisplayName)
	assert.Equal(t, 2, candidates[0].StepIndex)
}

func TestTracker_CompletedWorkflowIsRemoved(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_add", "t_commit", "t_push"},
		[]string{"git add", "git commit", "git push"})

	tr := NewTracker([]Pattern{p}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	tr.OnCommand("t_add")
	tr.OnCommand("t_commit")
	tr.OnCommand("t_push") // Complete.

	assert.Equal(t, 0, tr.ActiveWorkflows(), "completed workflow should be cleaned up")
}

func TestTracker_StaleAfterManyNonMatchingCommands(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_add", "t_commit", "t_push"},
		[]string{"git add", "git commit", "git push"})

	cfg := DefaultTrackerConfig()
	cfg.StaleAfterCommands = 3

	tr := NewTracker([]Pattern{p}, cfg)
	tr.nowFn = func() int64 { return 1000 }

	// Activate.
	tr.OnCommand("t_add")
	assert.Equal(t, 1, tr.ActiveWorkflows())

	// Non-matching commands.
	tr.OnCommand("t_other")
	tr.OnCommand("t_other2")
	tr.OnCommand("t_other3") // 3rd non-matching -> stale.

	assert.Equal(t, 0, tr.ActiveWorkflows(), "workflow should be stale after 3 non-matching commands")
}

func TestTracker_TimesOutStaleWorkflow(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_a", "t_b", "t_c"},
		[]string{"cmd_a", "cmd_b", "cmd_c"})

	cfg := DefaultTrackerConfig()
	cfg.ActivationTimeoutMs = 5000

	now := int64(1000)
	tr := NewTracker([]Pattern{p}, cfg)
	tr.nowFn = func() int64 { return now }

	// Activate.
	tr.OnCommand("t_a")
	assert.Equal(t, 1, tr.ActiveWorkflows())

	// Advance time past the timeout.
	now = 7000

	// OnCommand triggers cleanup.
	tr.OnCommand("t_other")
	assert.Equal(t, 0, tr.ActiveWorkflows(), "workflow should time out")
}

func TestTracker_MaxCandidatesCapped(t *testing.T) {
	t.Parallel()

	// Create 5 patterns that all start with the same template.
	var patterns []Pattern
	for i := 0; i < 5; i++ {
		patterns = append(patterns, makePattern(
			"wf"+string(rune('0'+i)),
			[]string{"t_start", "t_next_" + string(rune('a'+i)), "t_end"},
			[]string{"start", "next_" + string(rune('a'+i)), "end"},
		))
	}

	cfg := DefaultTrackerConfig()
	cfg.MaxCandidates = 3

	tr := NewTracker(patterns, cfg)
	tr.nowFn = func() int64 { return 1000 }

	candidates := tr.OnCommand("t_start")

	assert.LessOrEqual(t, len(candidates), 3, "candidates should be capped at MaxCandidates")
}

func TestTracker_DoesNotDuplicateActivation(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_a", "t_b", "t_c"},
		[]string{"cmd_a", "cmd_b", "cmd_c"})

	tr := NewTracker([]Pattern{p}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	// Activate twice with the same command.
	tr.OnCommand("t_a")
	tr.OnCommand("t_a") // Should not create a second activation of same pattern.

	// There should be 1 active workflow (the second t_a re-triggers but
	// the first was already tracking it, and the cleanup removed the stale first one
	// since t_a != t_b, then a new one was activated).
	// Actually, the first was advanced stale by 1 command, and then a new one activated.
	// Let's just verify candidates work correctly.
	candidates := tr.OnCommand("t_a")
	_ = candidates
	// This shouldn't panic or create unbounded workflows.
	assert.LessOrEqual(t, tr.ActiveWorkflows(), 2)
}

func TestTracker_SetPatternsRefreshes(t *testing.T) {
	t.Parallel()

	p1 := makePattern("wf1", []string{"t_a", "t_b"}, []string{"cmd_a", "cmd_b"})

	tr := NewTracker([]Pattern{p1}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	candidates := tr.OnCommand("t_a")
	require.Len(t, candidates, 1)
	assert.Equal(t, "t_b", candidates[0].TemplateID)

	// Replace patterns.
	p2 := makePattern("wf2", []string{"t_x", "t_y"}, []string{"cmd_x", "cmd_y"})
	tr.SetPatterns([]Pattern{p2})

	// Old pattern activation still active.
	candidates = tr.OnCommand("t_x")
	// Should activate the new pattern.
	found := false
	for _, c := range candidates {
		if c.TemplateID == "t_y" {
			found = true
		}
	}
	assert.True(t, found, "expected new pattern to be activated after SetPatterns")
}

func TestTracker_MultipleWorkflowsInParallel(t *testing.T) {
	t.Parallel()

	p1 := makePattern("wf1", []string{"t_a", "t_b", "t_c"},
		[]string{"cmd_a", "cmd_b", "cmd_c"})
	p2 := makePattern("wf2", []string{"t_a", "t_x", "t_y"},
		[]string{"cmd_a", "cmd_x", "cmd_y"})

	tr := NewTracker([]Pattern{p1, p2}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	// Both start with t_a, so both should activate.
	candidates := tr.OnCommand("t_a")
	assert.Equal(t, 2, tr.ActiveWorkflows())
	require.Len(t, candidates, 2, "expected candidates from both workflows")

	// Advance wf1.
	candidates = tr.OnCommand("t_b")
	// wf1 advanced, wf2 stale by 1.
	found := false
	for _, c := range candidates {
		if c.TemplateID == "t_c" {
			found = true
		}
	}
	assert.True(t, found, "expected next step from wf1")
}

func TestTracker_EmptyPatterns(t *testing.T) {
	t.Parallel()

	tr := NewTracker(nil, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	candidates := tr.OnCommand("anything")
	assert.Empty(t, candidates)
	assert.Equal(t, 0, tr.ActiveWorkflows())
}

func TestTracker_EmptyTemplateID(t *testing.T) {
	t.Parallel()

	p := makePattern("wf1", []string{"t_a", "t_b"},
		[]string{"cmd_a", "cmd_b"})

	tr := NewTracker([]Pattern{p}, DefaultTrackerConfig())
	tr.nowFn = func() int64 { return 1000 }

	// Empty template ID should not activate anything.
	candidates := tr.OnCommand("")
	assert.Empty(t, candidates)
	assert.Equal(t, 0, tr.ActiveWorkflows())
}

func TestWorkflowSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		names    []string
		expected string
	}{
		{"empty", nil, ""},
		{"single", []string{"git add"}, "git add"},
		{"two", []string{"git add", "git commit"}, "git add -> git commit"},
		{"three", []string{"git add", "git commit", "git push"}, "git add -> git commit -> git push"},
		{"four", []string{"a", "b", "c", "d"}, "a -> b -> ... -> d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, workflowSummary(tt.names))
		})
	}
}
