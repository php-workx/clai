package explain

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/suggest"
)

// buildSuggestion creates a Suggestion with a given total score and score
// breakdown. Since scoreInfo is unexported, we use the exported
// ScoreBreakdown via a helper that constructs a suggestion with known
// breakdown values by using the addCandidate path. For unit tests in the
// explain package we use a simpler approach: we construct a Suggestion
// whose ScoreBreakdown values are set through the package-level test
// helper.
//
// Because we are in a different package and scoreInfo is unexported, we
// rely on the fact that Suggestion.ScoreBreakdown() returns an exported
// struct. We create suggestions with known breakdowns by using
// SuggestionForTest.
func buildSuggestion(totalScore float64, breakdown suggest.ScoreBreakdown) *suggest.Suggestion {
	s := suggest.SuggestionForTest(totalScore, &breakdown)
	return &s
}

func TestExplain_SingleDominantFeature(t *testing.T) {
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition: 100.0,
	})

	reasons := Explain(s, DefaultConfig(), "git add")

	require.Len(t, reasons, 1)
	assert.Equal(t, suggest.ReasonRepoTransition, reasons[0].Tag)
	assert.InDelta(t, 1.0, reasons[0].Contribution, 0.001)
	assert.Contains(t, reasons[0].Description, "git add")
	assert.Contains(t, reasons[0].Description, "this repo")
}

func TestExplain_MultipleFeatures(t *testing.T) {
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition:   60.0,
		GlobalTransition: 25.0,
		RepoFrequency:    15.0,
	})

	reasons := Explain(s, DefaultConfig(), "make build")

	require.Len(t, reasons, 3)
	// Should be sorted by contribution descending.
	assert.Equal(t, suggest.ReasonRepoTransition, reasons[0].Tag)
	assert.Equal(t, suggest.ReasonGlobalTransition, reasons[1].Tag)
	assert.Equal(t, suggest.ReasonRepoFrequency, reasons[2].Tag)

	assert.InDelta(t, 0.60, reasons[0].Contribution, 0.001)
	assert.InDelta(t, 0.25, reasons[1].Contribution, 0.001)
	assert.InDelta(t, 0.15, reasons[2].Contribution, 0.001)
}

func TestExplain_MinContributionFiltering(t *testing.T) {
	// Total score = 100. One feature at 3% should be dropped with default
	// MinContribution of 0.05.
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition:  80.0,
		RepoFrequency:   17.0,
		GlobalFrequency: 3.0, // 3% < 5% threshold
	})

	reasons := Explain(s, DefaultConfig(), "git push")

	require.Len(t, reasons, 2)
	for _, r := range reasons {
		assert.NotEqual(t, suggest.ReasonGlobalFrequency, r.Tag,
			"low contribution feature should be filtered out")
	}
}

func TestExplain_MinContributionCustomThreshold(t *testing.T) {
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition:  80.0,
		RepoFrequency:   17.0,
		GlobalFrequency: 3.0,
	})

	cfg := Config{
		MaxReasons:        5,
		MinContribution:   0.02, // Lower threshold: 2%
		IncludeAmplifiers: true,
	}
	reasons := Explain(s, cfg, "")

	require.Len(t, reasons, 3, "all three features should pass the lower threshold")
}

func TestExplain_MaxReasonsCapping(t *testing.T) {
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition:   30.0,
		GlobalTransition: 25.0,
		RepoFrequency:    20.0,
		GlobalFrequency:  15.0,
		DirTransition:    10.0,
	})

	cfg := Config{
		MaxReasons:        2,
		MinContribution:   0.01,
		IncludeAmplifiers: true,
	}
	reasons := Explain(s, cfg, "npm test")

	require.Len(t, reasons, 2, "should be capped at MaxReasons=2")
	// Top two by absolute contribution.
	assert.Equal(t, suggest.ReasonRepoTransition, reasons[0].Tag)
	assert.Equal(t, suggest.ReasonGlobalTransition, reasons[1].Tag)
}

func TestExplain_AllReasonTagDescriptions(t *testing.T) {
	tests := []struct {
		tag        string
		prevCmd    string
		wantSubstr string
		breakdown  suggest.ScoreBreakdown
	}{
		{
			tag:        suggest.ReasonRepoTransition,
			breakdown:  suggest.ScoreBreakdown{RepoTransition: 100},
			prevCmd:    "git add",
			wantSubstr: "Commonly follows 'git add' in this repo",
		},
		{
			tag:        suggest.ReasonGlobalTransition,
			breakdown:  suggest.ScoreBreakdown{GlobalTransition: 100},
			prevCmd:    "ls",
			wantSubstr: "Commonly follows 'ls'",
		},
		{
			tag:        suggest.ReasonDirTransition,
			breakdown:  suggest.ScoreBreakdown{DirTransition: 100},
			prevCmd:    "cd src",
			wantSubstr: "Commonly follows 'cd src' in this directory",
		},
		{
			tag:        suggest.ReasonRepoFrequency,
			breakdown:  suggest.ScoreBreakdown{RepoFrequency: 100},
			prevCmd:    "",
			wantSubstr: "Frequently used in this repo",
		},
		{
			tag:        suggest.ReasonGlobalFrequency,
			breakdown:  suggest.ScoreBreakdown{GlobalFrequency: 100},
			prevCmd:    "",
			wantSubstr: "Frequently used command",
		},
		{
			tag:        suggest.ReasonDirFrequency,
			breakdown:  suggest.ScoreBreakdown{DirFrequency: 100},
			prevCmd:    "",
			wantSubstr: "Frequently used in this directory",
		},
		{
			tag:        suggest.ReasonProjectTask,
			breakdown:  suggest.ScoreBreakdown{ProjectTask: 100},
			prevCmd:    "",
			wantSubstr: "From project playbook",
		},
		{
			tag:        suggest.ReasonDangerous,
			breakdown:  suggest.ScoreBreakdown{Dangerous: -50},
			prevCmd:    "",
			wantSubstr: "Flagged as potentially destructive",
		},
		{
			tag:        suggest.ReasonWorkflowBoost,
			breakdown:  suggest.ScoreBreakdown{WorkflowBoost: 100},
			prevCmd:    "",
			wantSubstr: "Part of active workflow",
		},
		{
			tag:        suggest.ReasonPipelineConf,
			breakdown:  suggest.ScoreBreakdown{PipelineConf: 100},
			prevCmd:    "",
			wantSubstr: "Common next step in pipeline",
		},
		{
			tag:        suggest.ReasonDismissalPenalty,
			breakdown:  suggest.ScoreBreakdown{DismissalPenalty: -30},
			prevCmd:    "",
			wantSubstr: "Adjusted based on your feedback",
		},
		{
			tag:        suggest.ReasonRecoveryBoost,
			breakdown:  suggest.ScoreBreakdown{RecoveryBoost: 100},
			prevCmd:    "",
			wantSubstr: "Recovery suggestion after error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			// For negative-score features (dangerous, dismissal), we need
			// to set the total score to a positive value that accounts for
			// the penalty to make the suggestion valid.
			totalScore := 100.0
			if tt.breakdown.Dangerous != 0 || tt.breakdown.DismissalPenalty != 0 {
				// Ensure total is positive so Explain doesn't return nil.
				totalScore = 50.0
			}

			s := buildSuggestion(totalScore, tt.breakdown)
			cfg := Config{
				MaxReasons:        1,
				MinContribution:   0.0, // Accept any contribution.
				IncludeAmplifiers: true,
			}

			reasons := Explain(s, cfg, tt.prevCmd)

			require.NotEmpty(t, reasons, "expected at least one reason for tag %s", tt.tag)
			assert.Equal(t, tt.tag, reasons[0].Tag)
			assert.Contains(t, reasons[0].Description, tt.wantSubstr)
		})
	}
}

func TestExplain_EmptySuggestion(t *testing.T) {
	// Zero score suggestion should return no reasons.
	s := buildSuggestion(0.0, suggest.ScoreBreakdown{})

	reasons := Explain(s, DefaultConfig(), "")
	assert.Empty(t, reasons)
}

func TestExplain_NoFeatures(t *testing.T) {
	// Positive score but no feature contributions (all zeros).
	// This is theoretically impossible in practice, but the code should
	// handle it gracefully.
	s := buildSuggestion(10.0, suggest.ScoreBreakdown{})

	reasons := Explain(s, DefaultConfig(), "")
	assert.Empty(t, reasons)
}

func TestExplain_AmplifierInclusion(t *testing.T) {
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition: 50.0,
		WorkflowBoost:  30.0,
		PipelineConf:   20.0,
	})

	t.Run("amplifiers enabled", func(t *testing.T) {
		cfg := Config{
			MaxReasons:        5,
			MinContribution:   0.01,
			IncludeAmplifiers: true,
		}
		reasons := Explain(s, cfg, "")

		tags := reasonTags(reasons)
		assert.Contains(t, tags, suggest.ReasonWorkflowBoost)
		assert.Contains(t, tags, suggest.ReasonPipelineConf)
	})

	t.Run("amplifiers disabled", func(t *testing.T) {
		cfg := Config{
			MaxReasons:        5,
			MinContribution:   0.01,
			IncludeAmplifiers: false,
		}
		reasons := Explain(s, cfg, "")

		tags := reasonTags(reasons)
		assert.NotContains(t, tags, suggest.ReasonWorkflowBoost)
		assert.NotContains(t, tags, suggest.ReasonPipelineConf)
		// Only the core feature should be present.
		assert.Contains(t, tags, suggest.ReasonRepoTransition)
	})
}

func TestExplain_DismissalPenaltyAmplifier(t *testing.T) {
	// Suggestion with a positive base score and a dismissal penalty.
	s := buildSuggestion(70.0, suggest.ScoreBreakdown{
		RepoTransition:   100.0,
		DismissalPenalty: -30.0,
	})

	cfg := Config{
		MaxReasons:        3,
		MinContribution:   0.01,
		IncludeAmplifiers: true,
	}
	reasons := Explain(s, cfg, "git commit")

	require.Len(t, reasons, 2)

	// Dismissal penalty should have negative contribution.
	var dismissalReason *Reason
	for i := range reasons {
		if reasons[i].Tag == suggest.ReasonDismissalPenalty {
			dismissalReason = &reasons[i]
			break
		}
	}
	require.NotNil(t, dismissalReason, "expected dismissal penalty reason")
	assert.Less(t, dismissalReason.Contribution, 0.0,
		"dismissal penalty contribution should be negative")
	assert.Contains(t, dismissalReason.Description, "feedback")
}

func TestExplain_DefaultMaxReasonsZero(t *testing.T) {
	// MaxReasons=0 should default to 3.
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition:   40.0,
		GlobalTransition: 30.0,
		RepoFrequency:    20.0,
		GlobalFrequency:  10.0,
	})

	cfg := Config{
		MaxReasons:        0, // Should default to 3.
		MinContribution:   0.01,
		IncludeAmplifiers: true,
	}
	reasons := Explain(s, cfg, "")

	assert.Len(t, reasons, 3, "MaxReasons=0 should default to 3")
}

func TestExplain_TransitionWithEmptyPrevCmd(t *testing.T) {
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition: 100.0,
	})

	reasons := Explain(s, DefaultConfig(), "")

	require.Len(t, reasons, 1)
	assert.Equal(t, "Commonly follows previous command in this repo", reasons[0].Description)
}

func TestExplain_LongPrevCmdTruncation(t *testing.T) {
	longCmd := "kubectl get pods --namespace=my-very-long-namespace-name --output=wide"
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		GlobalTransition: 100.0,
	})

	reasons := Explain(s, DefaultConfig(), longCmd)

	require.Len(t, reasons, 1)
	// The description should contain a truncated version of the command.
	assert.Contains(t, reasons[0].Description, "...")
	assert.LessOrEqual(t, len(reasons[0].Description), 100,
		"description should not be excessively long")
}

func TestExplain_ContributionsNormalized(t *testing.T) {
	s := buildSuggestion(200.0, suggest.ScoreBreakdown{
		RepoTransition: 120.0,
		RepoFrequency:  80.0,
	})

	reasons := Explain(s, DefaultConfig(), "")

	require.Len(t, reasons, 2)
	// Contributions should sum to 1.0 (all features included).
	total := 0.0
	for _, r := range reasons {
		total += r.Contribution
	}
	assert.InDelta(t, 1.0, total, 0.001)
}

func TestExplain_NegativeScoreSuggestion(t *testing.T) {
	// A suggestion with a net-negative score (e.g., all penalties).
	s := buildSuggestion(-10.0, suggest.ScoreBreakdown{
		Dangerous: -10.0,
	})

	reasons := Explain(s, DefaultConfig(), "")
	assert.Empty(t, reasons, "negative total score should return no reasons")
}

func TestExplain_VerySmallContributions(t *testing.T) {
	s := buildSuggestion(1000.0, suggest.ScoreBreakdown{
		RepoTransition:  990.0,
		GlobalFrequency: 10.0, // 1% contribution, below default 5% threshold
	})

	reasons := Explain(s, DefaultConfig(), "")

	require.Len(t, reasons, 1)
	assert.Equal(t, suggest.ReasonRepoTransition, reasons[0].Tag)
}

func TestExplain_RecoveryBoostAfterError(t *testing.T) {
	s := buildSuggestion(150.0, suggest.ScoreBreakdown{
		RecoveryBoost:    100.0,
		GlobalTransition: 50.0,
	})

	cfg := Config{
		MaxReasons:        3,
		MinContribution:   0.01,
		IncludeAmplifiers: true,
	}
	reasons := Explain(s, cfg, "make test")

	require.Len(t, reasons, 2)
	assert.Equal(t, suggest.ReasonRecoveryBoost, reasons[0].Tag)
	assert.Contains(t, reasons[0].Description, "Recovery suggestion after error")
	assert.InDelta(t, 100.0/150.0, reasons[0].Contribution, 0.001)
}

func TestExplain_AbsoluteValueForPenaltyThreshold(t *testing.T) {
	// A penalty of -6% should still be included (abs(6%) > 5% threshold).
	s := buildSuggestion(100.0, suggest.ScoreBreakdown{
		RepoTransition:   106.0,
		DismissalPenalty: -6.0,
	})

	cfg := Config{
		MaxReasons:        3,
		MinContribution:   0.05,
		IncludeAmplifiers: true,
	}
	reasons := Explain(s, cfg, "")

	tags := reasonTags(reasons)
	assert.Contains(t, tags, suggest.ReasonDismissalPenalty,
		"penalty with |contribution| > threshold should be included")
}

func TestExplain_InfNaNSafety(t *testing.T) {
	// Edge case: if somehow total score is very small.
	s := buildSuggestion(math.SmallestNonzeroFloat64, suggest.ScoreBreakdown{
		RepoTransition: math.SmallestNonzeroFloat64,
	})

	// Should not panic.
	reasons := Explain(s, DefaultConfig(), "")
	assert.NotNil(t, reasons)
}

// reasonTags extracts the tags from a slice of reasons for easy assertion.
func reasonTags(reasons []Reason) []string {
	tags := make([]string, len(reasons))
	for i, r := range reasons {
		tags[i] = r.Tag
	}
	return tags
}
