// Package explain generates human-readable reasons for why a suggestion
// was ranked. It extracts the top N weighted feature contributions from
// a scored Suggestion and produces Reason structs suitable for CLI JSON
// output and shell integration "Why This?" hints.
//
// See spec appendix Section 20.9 (Explainable Suggestions).
package explain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/runger/clai/internal/suggestions/suggest"
)

// Reason describes a single human-readable explanation for why a
// suggestion was ranked.
type Reason struct {
	Tag          string  `json:"tag"`          // e.g. "repo_trans", "workflow", "pipeline"
	Description  string  `json:"description"`  // human-readable: "Often run after 'git add'"
	Contribution float64 `json:"contribution"` // weighted score contribution (0.0-1.0)
}

// Config controls how reasons are generated.
type Config struct {
	MaxReasons        int     // max reasons to include (default 3)
	MinContribution   float64 // drop below this threshold (default 0.05)
	IncludeAmplifiers bool    // include amplifier info (default true)
}

// DefaultConfig returns the default explainability configuration.
func DefaultConfig() Config {
	return Config{
		MaxReasons:        3,
		MinContribution:   0.05,
		IncludeAmplifiers: true,
	}
}

// featureEntry pairs a reason tag with its raw score contribution.
type featureEntry struct {
	tag   string
	score float64
}

// Explain extracts the top N weighted feature contributions from a scored
// suggestion and generates human-readable reason descriptions.
//
// The prevCmd parameter is used to fill in the "{prev_cmd}" placeholder
// in transition-based reason descriptions. It may be empty.
func Explain(s *suggest.Suggestion, cfg Config, prevCmd string) []Reason {
	if cfg.MaxReasons <= 0 {
		cfg.MaxReasons = 3
	}

	breakdown := s.ScoreBreakdown()
	totalScore := s.Score
	if totalScore <= 0 {
		return nil
	}

	// Collect all non-zero feature contributions.
	features := collectFeatures(&breakdown, cfg.IncludeAmplifiers)
	if len(features) == 0 {
		return nil
	}

	// Sort by absolute contribution descending.
	sort.Slice(features, func(i, j int) bool {
		return abs(features[i].score) > abs(features[j].score)
	})

	// Normalize contributions relative to total score and filter.
	reasons := make([]Reason, 0, cfg.MaxReasons)
	for _, f := range features {
		if len(reasons) >= cfg.MaxReasons {
			break
		}

		contribution := f.score / totalScore
		// For negative contributions (penalties), use absolute value for
		// threshold comparison but keep the sign in the output.
		if abs(contribution) < cfg.MinContribution {
			continue
		}

		reasons = append(reasons, Reason{
			Tag:          f.tag,
			Description:  descriptionForTag(f.tag, prevCmd),
			Contribution: contribution,
		})
	}

	return reasons
}

// collectFeatures gathers all non-zero score features from the breakdown.
func collectFeatures(b *suggest.ScoreBreakdown, includeAmplifiers bool) []featureEntry {
	entries := make([]featureEntry, 0, 12)

	addIfNonZero := func(tag string, val float64) {
		if val != 0 {
			entries = append(entries, featureEntry{tag: tag, score: val})
		}
	}

	// Core features (always included).
	addIfNonZero(suggest.ReasonRepoTransition, b.RepoTransition)
	addIfNonZero(suggest.ReasonGlobalTransition, b.GlobalTransition)
	addIfNonZero(suggest.ReasonDirTransition, b.DirTransition)
	addIfNonZero(suggest.ReasonRepoFrequency, b.RepoFrequency)
	addIfNonZero(suggest.ReasonGlobalFrequency, b.GlobalFrequency)
	addIfNonZero(suggest.ReasonDirFrequency, b.DirFrequency)
	addIfNonZero(suggest.ReasonProjectTask, b.ProjectTask)
	addIfNonZero(suggest.ReasonDangerous, b.Dangerous)

	// Amplifiers (gated by config).
	if includeAmplifiers {
		addIfNonZero(suggest.ReasonWorkflowBoost, b.WorkflowBoost)
		addIfNonZero(suggest.ReasonPipelineConf, b.PipelineConf)
		addIfNonZero(suggest.ReasonDismissalPenalty, b.DismissalPenalty)
		addIfNonZero(suggest.ReasonRecoveryBoost, b.RecoveryBoost)
	}

	return entries
}

// descriptionForTag returns a human-readable description template for a
// given reason tag. The prevCmd parameter is substituted into transition
// descriptions.
func descriptionForTag(tag, prevCmd string) string {
	// Sanitize prevCmd for display (truncate long commands).
	displayCmd := prevCmd
	if len(displayCmd) > 40 {
		displayCmd = displayCmd[:37] + "..."
	}

	switch tag {
	case suggest.ReasonRepoTransition:
		if displayCmd != "" {
			return fmt.Sprintf("Commonly follows '%s' in this repo", displayCmd)
		}
		return "Commonly follows previous command in this repo"
	case suggest.ReasonGlobalTransition:
		if displayCmd != "" {
			return fmt.Sprintf("Commonly follows '%s'", displayCmd)
		}
		return "Commonly follows previous command"
	case suggest.ReasonDirTransition:
		if displayCmd != "" {
			return fmt.Sprintf("Commonly follows '%s' in this directory", displayCmd)
		}
		return "Commonly follows previous command in this directory"
	case suggest.ReasonRepoFrequency:
		return "Frequently used in this repo"
	case suggest.ReasonGlobalFrequency:
		return "Frequently used command"
	case suggest.ReasonDirFrequency:
		return "Frequently used in this directory"
	case suggest.ReasonProjectTask:
		return "From project playbook"
	case suggest.ReasonDangerous:
		return "Flagged as potentially destructive"
	case suggest.ReasonWorkflowBoost:
		return "Part of active workflow"
	case suggest.ReasonPipelineConf:
		return "Common next step in pipeline"
	case suggest.ReasonDismissalPenalty:
		return "Adjusted based on your feedback"
	case suggest.ReasonRecoveryBoost:
		return "Recovery suggestion after error"
	default:
		// Unknown tag — use the tag itself as a fallback description.
		return strings.ReplaceAll(tag, "_", " ")
	}
}

// abs returns the absolute value of a float64.
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
