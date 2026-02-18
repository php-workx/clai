package suggest

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/score"

	_ "modernc.org/sqlite"
)

// TestProperty_Determinism verifies that the same input always produces
// the same output. Per spec Invariant I3: Deterministic Ranking.
func TestProperty_Determinism(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Populate data
	commands := []string{
		"git status", "git commit", "git push", "git pull",
		"npm install", "npm test", "npm run build",
		"make build", "make test", "make lint",
	}
	for _, cmd := range commands {
		for i := 0; i < 3; i++ {
			require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
		}
	}
	// Add transitions
	require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git add .", "git commit", nowMs))
	require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git commit", "git push", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestCtx := SuggestContext{
		LastCmd: "git add .",
		NowMs:   nowMs,
	}

	// Run 100 times with identical input
	var firstResult []Suggestion
	for i := 0; i < 100; i++ {
		suggestions, err := scorer.Suggest(ctx, suggestCtx)
		require.NoError(t, err)

		if i == 0 {
			firstResult = suggestions
		} else {
			require.Equal(t, len(firstResult), len(suggestions),
				"iteration %d: result count differs", i)
			for j := range firstResult {
				assert.Equal(t, firstResult[j].Command, suggestions[j].Command,
					"iteration %d, position %d: command differs", i, j)
				assert.Equal(t, firstResult[j].Score, suggestions[j].Score,
					"iteration %d, position %d: score differs", i, j)
				assert.Equal(t, firstResult[j].Confidence, suggestions[j].Confidence,
					"iteration %d, position %d: confidence differs", i, j)
			}
		}
	}
}

// TestProperty_Monotonicity verifies that higher frequency leads to higher
// or equal score, all else being equal.
func TestProperty_Monotonicity(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add "cmdA" with low frequency and "cmdB" with high frequency
	for i := 0; i < 2; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "cmdA", nowMs))
	}
	for i := 0; i < 20; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "cmdB", nowMs))
	}

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(suggestions), 2, "need at least 2 suggestions")

	// Find scores for cmdA and cmdB
	var scoreA, scoreB float64
	for _, sug := range suggestions {
		switch sug.Command {
		case "cmdA":
			scoreA = sug.Score
		case "cmdB":
			scoreB = sug.Score
		}
	}

	// Higher frequency (cmdB) should produce higher or equal score
	assert.Greater(t, scoreB, scoreA,
		"higher frequency command (cmdB=%.2f) should have higher score than lower frequency (cmdA=%.2f)",
		scoreB, scoreA)
}

// TestProperty_BoundedScores verifies that all confidence values are in [0.0, 1.0].
func TestProperty_BoundedScores(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Populate with diverse data to generate various score magnitudes
	commands := []string{
		"git status", "git commit", "git push", "git pull", "git log",
		"npm install", "npm test", "npm run build", "npm run dev",
		"docker build", "docker run", "docker ps", "docker stop",
		"make build", "make test", "make lint", "make clean",
		"curl https://api.example.com", "ls -la", "cd /tmp",
	}

	for _, cmd := range commands {
		freq := len(cmd) % 5 // Vary frequency by command length
		for i := 0; i <= freq; i++ {
			require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
		}
	}

	// Add transitions
	for i := 0; i < len(commands)-1; i++ {
		require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, commands[i], commands[i+1], nowMs))
	}

	cfg := DefaultScorerConfig()
	cfg.TopK = MaxTopK // Get maximum number of results

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, cfg)
	require.NoError(t, err)

	// Test with various contexts
	contexts := []SuggestContext{
		{NowMs: nowMs},
		{LastCmd: "git status", NowMs: nowMs},
		{LastCmd: "npm install", Prefix: "gi", NowMs: nowMs},
		{LastCmd: "docker build", NowMs: nowMs},
	}

	for _, suggestCtx := range contexts {
		suggestions, err := scorer.Suggest(ctx, suggestCtx)
		require.NoError(t, err)

		for _, sug := range suggestions {
			assert.GreaterOrEqual(t, sug.Confidence, 0.0,
				"confidence should be >= 0.0 for %q (got %.4f)", sug.Command, sug.Confidence)
			assert.LessOrEqual(t, sug.Confidence, 1.0,
				"confidence should be <= 1.0 for %q (got %.4f)", sug.Command, sug.Confidence)
		}
	}
}

// TestProperty_TopKInvariant verifies that len(results) <= requested TopK.
func TestProperty_TopKInvariant(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add many commands to ensure we have more candidates than any TopK
	for i := 0; i < 50; i++ {
		cmd := "cmd" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
	}

	// Test with various TopK values
	topKValues := []int{1, 2, 3, 5, 7, MaxTopK}
	for _, topK := range topKValues {
		cfg := DefaultScorerConfig()
		cfg.TopK = topK

		scorer, err := NewScorer(ScorerDependencies{
			DB:        db,
			FreqStore: freqStore,
		}, cfg)
		require.NoError(t, err)

		suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
		require.NoError(t, err)
		assert.LessOrEqual(t, len(suggestions), topK,
			"results count (%d) should be <= TopK (%d)", len(suggestions), topK)
	}
}

// TestProperty_TopKClamp verifies that TopK is clamped to MaxTopK.
func TestProperty_TopKClamp(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	overValues := []int{MaxTopK + 1, 100, 999, 50000}
	for _, v := range overValues {
		scorer, err := NewScorer(ScorerDependencies{DB: db}, ScorerConfig{TopK: v})
		require.NoError(t, err)
		assert.Equal(t, MaxTopK, scorer.TopK(),
			"TopK=%d should be clamped to MaxTopK=%d", v, MaxTopK)
	}
}

// TestProperty_Ordering verifies that results are sorted by score descending.
// Per spec Section 7.6: Primary sort key is score DESC.
func TestProperty_Ordering(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Create commands with clearly different frequencies
	for i := 0; i < 10; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
	}
	for i := 0; i < 5; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "npm test", nowMs))
	}
	for i := 0; i < 1; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "make lint", nowMs))
	}

	// Add transitions
	require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git add", "git status", nowMs))

	cfg := DefaultScorerConfig()
	cfg.TopK = MaxTopK

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, cfg)
	require.NoError(t, err)

	// Multiple contexts to test ordering
	contexts := []SuggestContext{
		{NowMs: nowMs},
		{LastCmd: "git add", NowMs: nowMs},
	}

	for _, suggestCtx := range contexts {
		suggestions, err := scorer.Suggest(ctx, suggestCtx)
		require.NoError(t, err)

		if len(suggestions) < 2 {
			continue
		}

		// Verify descending order by score
		isSorted := sort.SliceIsSorted(suggestions, func(i, j int) bool {
			if suggestions[i].Score != suggestions[j].Score {
				return suggestions[i].Score > suggestions[j].Score
			}
			if suggestions[i].frequency != suggestions[j].frequency {
				return suggestions[i].frequency > suggestions[j].frequency
			}
			if suggestions[i].lastSeenMs != suggestions[j].lastSeenMs {
				return suggestions[i].lastSeenMs > suggestions[j].lastSeenMs
			}
			return suggestions[i].Command < suggestions[j].Command
		})
		assert.True(t, isSorted, "suggestions should be sorted by score DESC with deterministic tie-breaking")
	}
}

// TestProperty_NoDuplicateCommands verifies that no two suggestions have
// the same command text (after near-duplicate suppression).
func TestProperty_NoDuplicateCommands(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add commands from multiple sources to increase chance of duplicates
	commands := []string{"git status", "git commit", "git push", "npm test", "make build"}
	for _, cmd := range commands {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
		require.NoError(t, freqStore.Update(ctx, "repo:/test", cmd, nowMs))
	}
	for i := 0; i < len(commands)-1; i++ {
		require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, commands[i], commands[i+1], nowMs))
	}

	cfg := DefaultScorerConfig()
	cfg.TopK = MaxTopK

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, cfg)
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastCmd: "git status",
		RepoKey: "repo:/test",
		NowMs:   nowMs,
	})
	require.NoError(t, err)

	seen := make(map[string]bool)
	for _, sug := range suggestions {
		assert.False(t, seen[sug.Command],
			"duplicate command found: %q", sug.Command)
		seen[sug.Command] = true
	}
}

// TestProperty_EmptyContextNoPanic verifies that various empty/nil contexts
// do not cause panics or errors.
func TestProperty_EmptyContextNoPanic(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := time.Now().UnixMilli()

	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// All of these should produce valid results without panicking
	edgeCases := []SuggestContext{
		{},
		{NowMs: 0},
		{SessionID: "", RepoKey: "", LastCmd: ""},
		{Prefix: "nonexistent-command-prefix-xyz"},
		{LastCmd: "git status", NowMs: nowMs},
		{LastCmd: "", Prefix: "", NowMs: nowMs},
		{LastTemplateID: "nonexistent-template-id", NowMs: nowMs},
		{DirScopeKey: "dir:nonexistent", NowMs: nowMs},
		{Scope: "nonexistent-scope", NowMs: nowMs},
	}

	for i, sc := range edgeCases {
		suggestions, err := scorer.Suggest(ctx, sc)
		require.NoError(t, err, "edge case %d should not error", i)
		// Results count should always respect TopK
		assert.LessOrEqual(t, len(suggestions), scorer.TopK(),
			"edge case %d: result count exceeds TopK", i)
	}
}

// TestProperty_ConfidenceConsistency verifies that higher scores produce
// higher or equal confidence values.
func TestProperty_ConfidenceConsistency(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{DB: db}, DefaultScorerConfig())
	require.NoError(t, err)

	// Create suggestions with increasing score magnitudes
	testCases := []struct {
		name         string
		score        float64
		sourceCount  int
		expectHigher bool // whether confidence should be higher than previous
	}{
		{"zero_score_zero_sources", 0, 0, false},
		{"low_score_one_source", 10, 1, true},
		{"mid_score_three_sources", 50, 3, true},
		{"high_score_five_sources", 200, 5, true},
		{"very_high_score_all_sources", 500, 10, true},
	}

	var prevConfidence float64
	for _, tc := range testCases {
		sug := &Suggestion{Score: tc.score}
		// Set score sources based on sourceCount
		if tc.sourceCount >= 1 {
			sug.scores.repoTransition = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 2 {
			sug.scores.globalTransition = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 3 {
			sug.scores.repoFrequency = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 4 {
			sug.scores.globalFrequency = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 5 {
			sug.scores.projectTask = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 6 {
			sug.scores.dirTransition = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 7 {
			sug.scores.dirFrequency = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 8 {
			sug.scores.workflowBoost = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 9 {
			sug.scores.pipelineConf = tc.score / float64(tc.sourceCount)
		}
		if tc.sourceCount >= 10 {
			sug.scores.recoveryBoost = tc.score / float64(tc.sourceCount)
		}

		confidence := scorer.calculateConfidence(sug)

		// Always bounded
		assert.GreaterOrEqual(t, confidence, 0.0, "%s: confidence >= 0", tc.name)
		assert.LessOrEqual(t, confidence, 1.0, "%s: confidence <= 1", tc.name)

		// Higher score + more sources should generally produce higher confidence
		if tc.expectHigher {
			assert.GreaterOrEqual(t, confidence, prevConfidence,
				"%s: confidence (%.4f) should be >= previous (%.4f)", tc.name, confidence, prevConfidence)
		}
		prevConfidence = confidence
	}
}

// TestProperty_DangerousPenaltyAlwaysApplied verifies that dangerous commands
// always receive a penalty regardless of their score magnitude.
// Per spec Invariant I6: Risk Label Integrity.
func TestProperty_DangerousPenaltyAlwaysApplied(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add dangerous commands with very high frequency
	dangerousCommands := []string{
		"rm -rf /", "rm -rf /*", "rm -rf .", "rm -rf *",
		"dd if=/dev/zero", "mkfs", "chmod -R 777", "chmod 777",
	}

	for _, cmd := range dangerousCommands {
		for i := 0; i < 50; i++ {
			require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
		}
	}

	// Add a safe command with lower frequency
	for i := 0; i < 5; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "ls -la", nowMs))
	}

	cfg := DefaultScorerConfig()
	cfg.TopK = MaxTopK

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, cfg)
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)

	for _, sug := range suggestions {
		if scorer.isDangerous(sug.Command) {
			assert.Contains(t, sug.Reasons, ReasonDangerous,
				"dangerous command %q should have dangerous reason tag", sug.Command)
		}
	}
}
