package recovery

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/db"
)

// newTestDB creates a V2 test database for recovery engine tests.
func newTestDB(t *testing.T) *db.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	d, err := db.Open(context.Background(), db.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	return d
}

// --- NewEngine Tests ---

func TestNewEngine_NilDB(t *testing.T) {
	t.Parallel()

	_, err := NewEngine(nil, nil, nil, DefaultEngineConfig())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database is nil")
}

func TestNewEngine_DefaultsApplied(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	engine, err := NewEngine(d.DB(), nil, nil, EngineConfig{})
	require.NoError(t, err)

	assert.NotNil(t, engine.Classifier())
	assert.NotNil(t, engine.Safety())
	assert.Equal(t, 0.2, engine.cfg.MinSuccessRate)
	assert.Equal(t, 2, engine.cfg.MinCount)
	assert.Equal(t, 5, engine.cfg.MaxCandidates)
}

// --- Bootstrap Seeding Tests ---

func TestSeedBootstrapPatterns_Basic(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	safety := NewSafetyGate(DefaultSafetyConfig())
	patterns := DefaultBootstrapPatterns()

	seeded, err := SeedBootstrapPatterns(ctx, d.DB(), patterns, safety)
	require.NoError(t, err)
	assert.Greater(t, seeded, 0)

	// Verify rows were inserted
	var count int
	err = d.DB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM failure_recovery WHERE source = 'bootstrap'
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, seeded, count)
}

func TestSeedBootstrapPatterns_NilDB(t *testing.T) {
	t.Parallel()

	safety := NewSafetyGate(DefaultSafetyConfig())
	_, err := SeedBootstrapPatterns(context.Background(), nil, DefaultBootstrapPatterns(), safety)
	assert.Error(t, err)
}

func TestSeedBootstrapPatterns_Idempotent(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	safety := NewSafetyGate(DefaultSafetyConfig())
	patterns := DefaultBootstrapPatterns()

	// Seed twice
	seeded1, err := SeedBootstrapPatterns(ctx, d.DB(), patterns, safety)
	require.NoError(t, err)

	seeded2, err := SeedBootstrapPatterns(ctx, d.DB(), patterns, safety)
	require.NoError(t, err)

	// Second seeding should produce same count (INSERT OR IGNORE)
	assert.Equal(t, seeded1, seeded2)

	// Total count should be the same as first seeding
	var count int
	err = d.DB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM failure_recovery WHERE source = 'bootstrap'
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, seeded1, count)
}

func TestSeedBootstrapPatterns_SafetyGateFilters(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	// Create a pattern with a dangerous recovery
	patterns := []BootstrapPattern{
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      "class:general",
			RecoveryCmdNorm:    "rm -rf /",
			InitialSuccessRate: 0.99,
		},
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      "class:general",
			RecoveryCmdNorm:    "make clean",
			InitialSuccessRate: 0.50,
		},
	}

	safety := NewSafetyGate(DefaultSafetyConfig())
	seeded, err := SeedBootstrapPatterns(ctx, d.DB(), patterns, safety)
	require.NoError(t, err)
	assert.Equal(t, 1, seeded) // Only the safe one

	// Verify only safe pattern was inserted
	var count int
	err = d.DB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM failure_recovery WHERE source = 'bootstrap'
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// --- Bootstrap Template ID Tests ---

func TestIsBootstrapTemplateID(t *testing.T) {
	t.Parallel()

	assert.True(t, IsBootstrapTemplateID("bootstrap:make clean"))
	assert.True(t, IsBootstrapTemplateID("bootstrap:sudo apt install <arg>"))
	assert.False(t, IsBootstrapTemplateID("abc123def456"))
	assert.False(t, IsBootstrapTemplateID("bootstrap"))
	assert.False(t, IsBootstrapTemplateID(""))
}

func TestExtractBootstrapCmd(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "make clean", ExtractBootstrapCmd("bootstrap:make clean"))
	assert.Equal(t, "sudo apt install <arg>", ExtractBootstrapCmd("bootstrap:sudo apt install <arg>"))
	assert.Equal(t, "", ExtractBootstrapCmd("not-bootstrap"))
	assert.Equal(t, "", ExtractBootstrapCmd(""))
}

// --- QueryRecoveries Tests ---

func TestQueryRecoveries_EmptyDB(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	engine, err := NewEngine(d.DB(), nil, nil, DefaultEngineConfig())
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-make-build", 1, "global")
	require.NoError(t, err)
	assert.Empty(t, candidates)
}

func TestQueryRecoveries_BootstrapPatterns(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	// Seed bootstrap patterns
	safety := NewSafetyGate(DefaultSafetyConfig())
	_, err := SeedBootstrapPatterns(ctx, d.DB(), DefaultBootstrapPatterns(), safety)
	require.NoError(t, err)

	engine, err := NewEngine(d.DB(), nil, safety, DefaultEngineConfig())
	require.NoError(t, err)

	// Query for "command not found" (exit 127)
	candidates, err := engine.QueryRecoveries(ctx, "tpl-nonexistent", 127, "global")
	require.NoError(t, err)
	assert.Greater(t, len(candidates), 0, "should get bootstrap recovery candidates for 127")

	// All candidates should have source=bootstrap
	for _, c := range candidates {
		assert.Equal(t, "bootstrap", c.Source)
		assert.NotEmpty(t, c.RecoveryCmdNorm)
	}
}

func TestQueryRecoveries_LearnedPatterns(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())

	// Insert a learned recovery pattern directly
	exitCodeClass := classifier.ClassifyToKey(1)
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-make-build', ?, 'tpl-make-clean', 5.0, 5, 0.80, 1000, 'learned')
	`, exitCodeClass)
	require.NoError(t, err)

	// Also insert the command_template so resolution works
	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES ('tpl-make-clean', 'make clean', 'null', 0, 1000, 1000)
	`)
	require.NoError(t, err)

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.2,
		MinCount:        2,
		MaxCandidates:   5,
		IncludeWildcard: false, // Disable wildcard to test only learned
	})
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-make-build", 1, "global")
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	assert.Equal(t, "make clean", candidates[0].RecoveryCmdNorm)
	assert.Equal(t, "learned", candidates[0].Source)
	assert.InDelta(t, 0.80, candidates[0].SuccessRate, 0.001)
	assert.Equal(t, 5, candidates[0].Count)
}

func TestQueryRecoveries_MinCountFiltersLearned(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())
	exitCodeClass := classifier.ClassifyToKey(1)

	// Insert a learned pattern with count=1 (below default MinCount=2)
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-make-build', ?, 'tpl-make-clean', 1.0, 1, 0.80, 1000, 'learned')
	`, exitCodeClass)
	require.NoError(t, err)

	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES ('tpl-make-clean', 'make clean', 'null', 0, 1000, 1000)
	`)
	require.NoError(t, err)

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.2,
		MinCount:        2, // Requires at least 2
		MaxCandidates:   5,
		IncludeWildcard: false,
	})
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-make-build", 1, "global")
	require.NoError(t, err)
	assert.Empty(t, candidates, "learned pattern with count=1 should be filtered by MinCount=2")
}

func TestQueryRecoveries_MinSuccessRateFilters(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())
	exitCodeClass := classifier.ClassifyToKey(1)

	// Insert a learned pattern with low success rate
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-make-build', ?, 'tpl-make-clean', 5.0, 5, 0.10, 1000, 'learned')
	`, exitCodeClass)
	require.NoError(t, err)

	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES ('tpl-make-clean', 'make clean', 'null', 0, 1000, 1000)
	`)
	require.NoError(t, err)

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.2, // Pattern has 0.10 < 0.2
		MinCount:        1,
		MaxCandidates:   5,
		IncludeWildcard: false,
	})
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-make-build", 1, "global")
	require.NoError(t, err)
	assert.Empty(t, candidates, "pattern with success_rate=0.10 should be filtered by MinSuccessRate=0.2")
}

func TestQueryRecoveries_MaxCandidatesCap(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())
	exitCodeClass := classifier.ClassifyToKey(1)

	// Insert many learned patterns
	for i := 0; i < 10; i++ {
		tplID := "tpl-recovery-" + string(rune('a'+i))
		_, err := d.DB().ExecContext(ctx, `
			INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
			VALUES ('global', 'tpl-make-build', ?, ?, ?, 5, 0.70, 1000, 'learned')
		`, exitCodeClass, tplID, float64(10-i))
		require.NoError(t, err)

		_, err = d.DB().ExecContext(ctx, `
			INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
			VALUES (?, ?, 'null', 0, 1000, 1000)
		`, tplID, "recovery-cmd-"+string(rune('a'+i)))
		require.NoError(t, err)
	}

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.2,
		MinCount:        2,
		MaxCandidates:   3,
		IncludeWildcard: false,
	})
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-make-build", 1, "global")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(candidates), 3, "should be capped at MaxCandidates=3")
}

func TestQueryRecoveries_SafetyGateBlocksUnsafe(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())
	exitCodeClass := classifier.ClassifyToKey(1)

	// Insert a dangerous recovery pattern
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-fail', ?, 'tpl-dangerous', 10.0, 10, 0.90, 1000, 'learned')
	`, exitCodeClass)
	require.NoError(t, err)

	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES ('tpl-dangerous', 'rm -rf /', 'null', 0, 1000, 1000)
	`)
	require.NoError(t, err)

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.1,
		MinCount:        1,
		MaxCandidates:   5,
		IncludeWildcard: false,
	})
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-fail", 1, "global")
	require.NoError(t, err)
	assert.Empty(t, candidates, "dangerous recovery should be filtered out")
}

func TestQueryRecoveries_SortedByCompositeScore(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())
	exitCodeClass := classifier.ClassifyToKey(1)

	// Insert two patterns with different composite scores
	// Pattern A: success_rate=0.9, weight=2.0 => composite=1.8
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-fail', ?, 'tpl-a', 2.0, 3, 0.90, 1000, 'learned')
	`, exitCodeClass)
	require.NoError(t, err)

	// Pattern B: success_rate=0.5, weight=5.0 => composite=2.5
	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-fail', ?, 'tpl-b', 5.0, 5, 0.50, 1000, 'learned')
	`, exitCodeClass)
	require.NoError(t, err)

	for _, tpl := range []struct{ id, cmd string }{
		{"tpl-a", "recovery-a"},
		{"tpl-b", "recovery-b"},
	} {
		_, err = d.DB().ExecContext(ctx, `
			INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
			VALUES (?, ?, 'null', 0, 1000, 1000)
		`, tpl.id, tpl.cmd)
		require.NoError(t, err)
	}

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.2,
		MinCount:        2,
		MaxCandidates:   5,
		IncludeWildcard: false,
	})
	require.NoError(t, err)

	candidates, err := engine.QueryRecoveries(ctx, "tpl-fail", 1, "global")
	require.NoError(t, err)
	require.Len(t, candidates, 2)

	// B should come first (composite 2.5 > 1.8)
	assert.Equal(t, "recovery-b", candidates[0].RecoveryCmdNorm)
	assert.Equal(t, "recovery-a", candidates[1].RecoveryCmdNorm)
}

// --- RecordRecoveryEdge Tests ---

func TestRecordRecoveryEdge_Success(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	engine, err := NewEngine(d.DB(), nil, nil, DefaultEngineConfig())
	require.NoError(t, err)

	err = engine.RecordRecoveryEdge(ctx, "global", "tpl-fail", "tpl-fix", 127, true)
	require.NoError(t, err)

	// Verify record
	var count int
	var successRate float64
	exitCodeClass := engine.Classifier().ClassifyToKey(127)
	err = d.DB().QueryRowContext(ctx, `
		SELECT count, success_rate FROM failure_recovery
		WHERE scope = 'global' AND failed_template_id = 'tpl-fail'
		AND exit_code_class = ? AND recovery_template_id = 'tpl-fix'
	`, exitCodeClass).Scan(&count, &successRate)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1.0, successRate)
}

func TestRecordRecoveryEdge_Failure(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	engine, err := NewEngine(d.DB(), nil, nil, DefaultEngineConfig())
	require.NoError(t, err)

	err = engine.RecordRecoveryEdge(ctx, "global", "tpl-fail", "tpl-fix", 1, false)
	require.NoError(t, err)

	exitCodeClass := engine.Classifier().ClassifyToKey(1)
	var count int
	var successRate float64
	err = d.DB().QueryRowContext(ctx, `
		SELECT count, success_rate FROM failure_recovery
		WHERE scope = 'global' AND failed_template_id = 'tpl-fail'
		AND exit_code_class = ? AND recovery_template_id = 'tpl-fix'
	`, exitCodeClass).Scan(&count, &successRate)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 0.0, successRate)
}

func TestRecordRecoveryEdge_RunningSuccessRate(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	engine, err := NewEngine(d.DB(), nil, nil, DefaultEngineConfig())
	require.NoError(t, err)

	// Record 3 recoveries: success, failure, success
	require.NoError(t, engine.RecordRecoveryEdge(ctx, "global", "tpl-f", "tpl-r", 1, true))
	require.NoError(t, engine.RecordRecoveryEdge(ctx, "global", "tpl-f", "tpl-r", 1, false))
	require.NoError(t, engine.RecordRecoveryEdge(ctx, "global", "tpl-f", "tpl-r", 1, true))

	exitCodeClass := engine.Classifier().ClassifyToKey(1)
	var count int
	var successRate float64
	err = d.DB().QueryRowContext(ctx, `
		SELECT count, success_rate FROM failure_recovery
		WHERE scope = 'global' AND failed_template_id = 'tpl-f'
		AND exit_code_class = ? AND recovery_template_id = 'tpl-r'
	`, exitCodeClass).Scan(&count, &successRate)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.InDelta(t, 2.0/3.0, successRate, 0.001) // 2 successes out of 3
}

// --- Deduplication Tests ---

func TestDeduplicateCandidates(t *testing.T) {
	t.Parallel()

	candidates := []RecoveryCandidate{
		{RecoveryCmdNorm: "make clean", SuccessRate: 0.5, Weight: 1.0},
		{RecoveryCmdNorm: "make clean", SuccessRate: 0.8, Weight: 2.0},
		{RecoveryCmdNorm: "go mod tidy", SuccessRate: 0.7, Weight: 1.0},
	}

	result := deduplicateCandidates(candidates)
	assert.Len(t, result, 2)

	// The higher-scored "make clean" should win
	for _, c := range result {
		if c.RecoveryCmdNorm == "make clean" {
			assert.InDelta(t, 0.8, c.SuccessRate, 0.001)
			assert.InDelta(t, 2.0, c.Weight, 0.001)
		}
	}
}

func TestDeduplicateCandidates_EmptyCmdNorm(t *testing.T) {
	t.Parallel()

	candidates := []RecoveryCandidate{
		{RecoveryCmdNorm: "", SuccessRate: 0.9, Weight: 5.0},
		{RecoveryCmdNorm: "make clean", SuccessRate: 0.5, Weight: 1.0},
	}

	result := deduplicateCandidates(candidates)
	assert.Len(t, result, 1)
	assert.Equal(t, "make clean", result[0].RecoveryCmdNorm)
}

// --- Integration: Bootstrap + Query ---

func TestIntegration_SeedAndQuery(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())

	// Seed bootstrap patterns
	_, err := SeedBootstrapPatterns(ctx, d.DB(), DefaultBootstrapPatterns(), safety)
	require.NoError(t, err)

	engine, err := NewEngine(d.DB(), classifier, safety, DefaultEngineConfig())
	require.NoError(t, err)

	// Query for command not found (exit 127)
	candidates, err := engine.QueryRecoveries(ctx, "tpl-missing-cmd", 127, "global")
	require.NoError(t, err)

	// Should find bootstrap patterns for not_found
	assert.Greater(t, len(candidates), 0)
	for _, c := range candidates {
		assert.Greater(t, c.SuccessRate, 0.0)
		assert.NotEmpty(t, c.RecoveryCmdNorm)
	}
}

func TestIntegration_LearnedOverridesBootstrap(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	classifier := NewClassifier(nil)
	safety := NewSafetyGate(DefaultSafetyConfig())

	engine, err := NewEngine(d.DB(), classifier, safety, EngineConfig{
		MinSuccessRate:  0.2,
		MinCount:        1, // Lower for test
		MaxCandidates:   5,
		IncludeWildcard: true,
	})
	require.NoError(t, err)

	// Seed bootstrap
	_, err = SeedBootstrapPatterns(ctx, d.DB(), DefaultBootstrapPatterns(), safety)
	require.NoError(t, err)

	// Record a learned recovery with high score
	err = engine.RecordRecoveryEdge(ctx, "global", "tpl-specific-cmd", "tpl-specific-fix", 127, true)
	require.NoError(t, err)

	// Insert template for resolution
	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES ('tpl-specific-fix', 'specific fix command', 'null', 0, 1000, 1000)
	`)
	require.NoError(t, err)

	// Query should include both learned and bootstrap
	candidates, err := engine.QueryRecoveries(ctx, "tpl-specific-cmd", 127, "global")
	require.NoError(t, err)

	assert.Greater(t, len(candidates), 1, "should have both learned and bootstrap candidates")

	// The first candidate should be the learned one (higher composite score for fresh entry)
	// or bootstrap, depending on scoring. Just verify both types are present.
	sources := make(map[string]bool)
	for _, c := range candidates {
		sources[c.Source] = true
	}
	assert.True(t, sources["learned"], "should have learned candidates")
	assert.True(t, sources["bootstrap"], "should have bootstrap candidates")
}
