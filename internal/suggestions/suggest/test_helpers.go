package suggest

// SuggestionForTest creates a Suggestion with a known total score and score
// breakdown. This is intended for use in tests outside the suggest package
// (e.g. the explain package) that need to construct suggestions with specific
// per-feature score contributions.
func SuggestionForTest(totalScore float64, b ScoreBreakdown) Suggestion {
	return Suggestion{
		Command: "test-command",
		Score:   totalScore,
		scores: scoreInfo{
			repoTransition:   b.RepoTransition,
			globalTransition: b.GlobalTransition,
			repoFrequency:    b.RepoFrequency,
			globalFrequency:  b.GlobalFrequency,
			projectTask:      b.ProjectTask,
			dangerous:        b.Dangerous,
			dirTransition:    b.DirTransition,
			dirFrequency:     b.DirFrequency,
			workflowBoost:    b.WorkflowBoost,
			pipelineConf:     b.PipelineConf,
			dismissalPenalty: b.DismissalPenalty,
			recoveryBoost:    b.RecoveryBoost,
		},
	}
}
