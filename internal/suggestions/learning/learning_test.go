package learning

import (
	"context"
	"database/sql"
	"math"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// tolerance for floating-point comparisons.
const epsilon = 1e-9

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE rank_weight_profile (
			profile_key               TEXT PRIMARY KEY,
			scope                     TEXT NOT NULL,
			updated_ms                INTEGER NOT NULL,
			w_transition              REAL NOT NULL,
			w_frequency               REAL NOT NULL,
			w_success                 REAL NOT NULL,
			w_prefix                  REAL NOT NULL,
			w_affinity                REAL NOT NULL,
			w_task                    REAL NOT NULL,
			w_feedback                REAL NOT NULL,
			w_project_type_affinity   REAL NOT NULL,
			w_failure_recovery        REAL NOT NULL,
			w_risk_penalty            REAL NOT NULL,
			sample_count              INTEGER NOT NULL,
			learning_rate             REAL NOT NULL
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// -----------------------------------------------------------------------
// Eta decay formula tests
// -----------------------------------------------------------------------

func TestEtaDecayFormula(t *testing.T) {
	tests := []struct {
		name        string
		initial     float64
		decayConst  float64
		floor       float64
		sampleCount int64
		expectedEta float64
	}{
		{
			name:        "zero samples",
			initial:     0.02,
			decayConst:  500,
			floor:       0.001,
			sampleCount: 0,
			expectedEta: 0.02,
		},
		{
			name:        "500 samples - halved",
			initial:     0.02,
			decayConst:  500,
			floor:       0.001,
			sampleCount: 500,
			// 0.02 / (1 + 500/500) = 0.02 / 2 = 0.01
			expectedEta: 0.01,
		},
		{
			name:        "2000 samples",
			initial:     0.02,
			decayConst:  500,
			floor:       0.001,
			sampleCount: 2000,
			// 0.02 / (1 + 2000/500) = 0.02 / 5 = 0.004
			expectedEta: 0.004,
		},
		{
			name:        "very large sample count hits floor",
			initial:     0.02,
			decayConst:  500,
			floor:       0.001,
			sampleCount: 100000,
			// 0.02 / (1 + 100000/500) = 0.02 / 201 ≈ 0.0000995 < 0.001
			expectedEta: 0.001,
		},
		{
			name:        "floor prevents zero eta",
			initial:     0.02,
			decayConst:  500,
			floor:       0.005,
			sampleCount: 10000,
			// 0.02 / (1 + 10000/500) = 0.02 / 21 ≈ 0.000952 < 0.005
			expectedEta: 0.005,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eta := effectiveEta(tt.initial, tt.decayConst, tt.floor, tt.sampleCount)
			if math.Abs(eta-tt.expectedEta) > epsilon {
				t.Errorf("effectiveEta(%v, %v, %v, %v) = %v, want %v",
					tt.initial, tt.decayConst, tt.floor, tt.sampleCount, eta, tt.expectedEta)
			}
		})
	}
}

func TestLearnerEta(t *testing.T) {
	cfg := DefaultConfig()
	dw := DefaultWeights()
	learner := NewLearner(&dw, cfg, nil)

	eta0 := learner.Eta()
	if math.Abs(eta0-0.02) > epsilon {
		t.Errorf("initial eta = %v, want 0.02", eta0)
	}
}

// -----------------------------------------------------------------------
// Weight clamping tests
// -----------------------------------------------------------------------

func TestWeightClampingAtBoundaries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WeightMin = 0.0
	cfg.WeightMax = 0.60
	cfg.WeightRiskMin = 0.10
	cfg.WeightRiskMax = 0.60
	cfg.MinSamples = 0 // Allow immediate updates

	// Start with weights at max so a positive push clamps at max.
	initial := Weights{
		Transition:          0.60,
		Frequency:           0.60,
		Success:             0.60,
		Prefix:              0.60,
		Affinity:            0.60,
		Task:                0.60,
		Feedback:            0.60,
		ProjectTypeAffinity: 0.60,
		FailureRecovery:     0.60,
		RiskPenalty:         0.60,
	}
	learner := NewLearner(&initial, cfg, nil)

	// Push all features positive hard.
	fPos := FeatureVector{
		Transition: 1.0, Frequency: 1.0, Success: 1.0, Prefix: 1.0,
		Affinity: 1.0, Task: 1.0, Feedback: 1.0,
		ProjectTypeAffinity: 1.0, FailureRecovery: 1.0, RiskPenalty: 1.0,
	}
	fNeg := FeatureVector{} // all zeros

	learner.Update(context.Background(), "test", &fPos, &fNeg)

	w := learner.Weights()
	// After renormalization non-penalty weights should preserve their original sum.
	// But each individual weight should be <= WeightMax.
	checkRange := func(name string, v, lo, hi float64) {
		t.Helper()
		if v < lo-epsilon || v > hi+epsilon {
			t.Errorf("%s = %v, want [%v, %v]", name, v, lo, hi)
		}
	}
	checkRange("Transition", w.Transition, cfg.WeightMin, cfg.WeightMax)
	checkRange("Frequency", w.Frequency, cfg.WeightMin, cfg.WeightMax)
	checkRange("RiskPenalty", w.RiskPenalty, cfg.WeightRiskMin, cfg.WeightRiskMax)
}

func TestWeightClampingLowerBound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	// Start with low weights so a negative push clamps at min.
	initial := Weights{
		Transition:  0.01,
		Frequency:   0.01,
		Success:     0.01,
		Prefix:      0.01,
		Affinity:    0.01,
		Task:        0.01,
		Feedback:    0.01,
		RiskPenalty: 0.11,
	}
	learner := NewLearner(&initial, cfg, nil)

	// Push all features negative hard.
	fNeg := FeatureVector{
		Transition: 1.0, Frequency: 1.0, Success: 1.0, Prefix: 1.0,
		Affinity: 1.0, Task: 1.0, Feedback: 1.0, RiskPenalty: 1.0,
	}
	fPos := FeatureVector{} // all zeros

	learner.Update(context.Background(), "test", &fPos, &fNeg)

	w := learner.Weights()
	// Non-penalty weights clamped to 0.0
	if w.Transition < -epsilon {
		t.Errorf("Transition = %v, expected >= 0", w.Transition)
	}
	// Risk penalty clamped to WeightRiskMin
	if w.RiskPenalty < cfg.WeightRiskMin-epsilon {
		t.Errorf("RiskPenalty = %v, expected >= %v", w.RiskPenalty, cfg.WeightRiskMin)
	}
}

// -----------------------------------------------------------------------
// Renormalization tests
// -----------------------------------------------------------------------

func TestRenormalizationPreservesSum(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	initial := DefaultWeights()
	sumBefore := nonPenaltySum(&initial)

	learner := NewLearner(&initial, cfg, nil)

	// Apply several updates and verify the non-penalty sum is preserved.
	for i := 0; i < 5; i++ {
		fPos := FeatureVector{Transition: 0.8, Frequency: 0.3}
		fNeg := FeatureVector{Task: 0.5, Success: 0.4}
		learner.Update(context.Background(), "test", &fPos, &fNeg)
	}

	w := learner.Weights()
	sumAfter := nonPenaltySum(&w)

	if math.Abs(sumAfter-sumBefore) > 0.01 {
		t.Errorf("non-penalty sum changed: before=%v, after=%v (delta=%v)",
			sumBefore, sumAfter, sumAfter-sumBefore)
	}
}

func TestRenormalizationWithZeroSum(_ *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	// All non-penalty weights are zero; renormalization should not panic.
	initial := Weights{RiskPenalty: 0.20}
	learner := NewLearner(&initial, cfg, nil)

	fPos := FeatureVector{Transition: 0.5}
	fNeg := FeatureVector{}

	// Should not panic.
	learner.Update(context.Background(), "test", &fPos, &fNeg)
}

// -----------------------------------------------------------------------
// Low-sample freeze tests
// -----------------------------------------------------------------------

func TestLowSampleFreeze(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 10

	initial := DefaultWeights()
	learner := NewLearner(&initial, cfg, nil)

	fPos := FeatureVector{Transition: 1.0, Frequency: 1.0}
	fNeg := FeatureVector{}

	// Apply 9 updates (below threshold): weights should not change.
	for i := 0; i < 9; i++ {
		learner.Update(context.Background(), "test", &fPos, &fNeg)
	}

	w := learner.Weights()
	if math.Abs(w.Transition-initial.Transition) > epsilon {
		t.Errorf("weights changed during freeze: Transition=%v, want %v",
			w.Transition, initial.Transition)
	}

	if learner.SampleCount() != 9 {
		t.Errorf("sample count = %v, want 9", learner.SampleCount())
	}

	// 10th update should trigger actual weight change.
	learner.Update(context.Background(), "test", &fPos, &fNeg)

	w2 := learner.Weights()
	if math.Abs(w2.Transition-initial.Transition) < epsilon {
		t.Error("weights did not change after reaching MinSamples")
	}
}

// -----------------------------------------------------------------------
// Pairwise update direction tests
// -----------------------------------------------------------------------

func TestPairwiseUpdateIncreasesAcceptedFeature(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	initial := DefaultWeights()
	learner := NewLearner(&initial, cfg, nil)

	// Positive signal for Transition feature.
	fPos := FeatureVector{Transition: 0.9}
	fNeg := FeatureVector{Transition: 0.1}

	learner.Update(context.Background(), "test", &fPos, &fNeg)

	w := learner.Weights()
	// Transition should have increased (before renormalization adjusts everything).
	// After renormalization the relative weight of Transition should be higher.
	// We check that the ratio Transition/sum is greater than before.
	initialRatio := initial.Transition / nonPenaltySum(&initial)
	newRatio := w.Transition / nonPenaltySum(&w)

	if newRatio <= initialRatio-epsilon {
		t.Errorf("accepted feature ratio did not increase: before=%v, after=%v",
			initialRatio, newRatio)
	}
}

func TestPairwiseUpdateDecreasesNegativeFeature(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	initial := DefaultWeights()
	learner := NewLearner(&initial, cfg, nil)

	// Negative signal for Task feature (dismissed candidate was strong on Task).
	fPos := FeatureVector{Task: 0.1}
	fNeg := FeatureVector{Task: 0.9}

	learner.Update(context.Background(), "test", &fPos, &fNeg)

	w := learner.Weights()
	initialRatio := initial.Task / nonPenaltySum(&initial)
	newRatio := w.Task / nonPenaltySum(&w)

	if newRatio >= initialRatio+epsilon {
		t.Errorf("dismissed feature ratio did not decrease: before=%v, after=%v",
			initialRatio, newRatio)
	}
}

// -----------------------------------------------------------------------
// Per-scope isolation tests
// -----------------------------------------------------------------------

func TestPerScopeIsolation(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	cfg := DefaultConfig()
	cfg.MinSamples = 0
	ctx := context.Background()

	// Create two learners without store to avoid async goroutine races.
	// We persist via the store directly to test scope isolation.
	dwA := DefaultWeights()
	learnerA := NewLearner(&dwA, cfg, nil)
	dwB := DefaultWeights()
	learnerB := NewLearner(&dwB, cfg, nil)

	// Update scope A only.
	fPos := FeatureVector{Transition: 1.0}
	fNeg := FeatureVector{}
	learnerA.Update(ctx, "repo:alpha", &fPos, &fNeg)

	// Save both scopes synchronously to the store.
	wA := learnerA.Weights()
	scA := learnerA.SampleCount()
	err := store.SaveWeights(ctx, "repo:alpha", &wA, scA, learnerA.Eta())
	if err != nil {
		t.Fatal(err)
	}

	wB := learnerB.Weights()
	scB := learnerB.SampleCount()
	err = store.SaveWeights(ctx, "repo:beta", &wB, scB, learnerB.Eta())
	if err != nil {
		t.Fatal(err)
	}

	// Load and verify isolation.
	pA, err := store.LoadWeights(ctx, "repo:alpha")
	if err != nil {
		t.Fatal(err)
	}
	pB, err := store.LoadWeights(ctx, "repo:beta")
	if err != nil {
		t.Fatal(err)
	}

	if pA == nil || pB == nil {
		t.Fatal("expected both profiles to exist")
	}

	// A should have been updated, B should still have defaults.
	if pA.SampleCount != 1 {
		t.Errorf("scope A sample_count = %v, want 1", pA.SampleCount)
	}
	if pB.SampleCount != 0 {
		t.Errorf("scope B sample_count = %v, want 0", pB.SampleCount)
	}

	// The weight vectors should differ.
	if math.Abs(pA.Weights.Transition-pB.Weights.Transition) < epsilon {
		t.Error("expected scope A and B to have different Transition weights")
	}
}

// -----------------------------------------------------------------------
// Store round-trip tests
// -----------------------------------------------------------------------

func TestStoreRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	original := Weights{
		Transition:          0.25,
		Frequency:           0.18,
		Success:             0.12,
		Prefix:              0.14,
		Affinity:            0.09,
		Task:                0.07,
		Feedback:            0.11,
		ProjectTypeAffinity: 0.06,
		FailureRecovery:     0.10,
		RiskPenalty:         0.35,
	}

	err := store.SaveWeights(ctx, "global", &original, 42, 0.015)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadWeights(ctx, "global")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected loaded profile, got nil")
	}

	if loaded.SampleCount != 42 {
		t.Errorf("sample_count = %v, want 42", loaded.SampleCount)
	}
	if math.Abs(loaded.LearningRate-0.015) > epsilon {
		t.Errorf("learning_rate = %v, want 0.015", loaded.LearningRate)
	}

	// Check each weight field.
	checkWeight := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > epsilon {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	checkWeight("Transition", loaded.Weights.Transition, original.Transition)
	checkWeight("Frequency", loaded.Weights.Frequency, original.Frequency)
	checkWeight("Success", loaded.Weights.Success, original.Success)
	checkWeight("Prefix", loaded.Weights.Prefix, original.Prefix)
	checkWeight("Affinity", loaded.Weights.Affinity, original.Affinity)
	checkWeight("Task", loaded.Weights.Task, original.Task)
	checkWeight("Feedback", loaded.Weights.Feedback, original.Feedback)
	checkWeight("ProjectTypeAffinity", loaded.Weights.ProjectTypeAffinity, original.ProjectTypeAffinity)
	checkWeight("FailureRecovery", loaded.Weights.FailureRecovery, original.FailureRecovery)
	checkWeight("RiskPenalty", loaded.Weights.RiskPenalty, original.RiskPenalty)
}

func TestStoreLoadNonexistentScope(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)

	p, err := store.LoadWeights(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Errorf("expected nil profile for nonexistent scope, got %+v", p)
	}
}

func TestStoreUpsertOverwrites(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	w1 := DefaultWeights()
	err := store.SaveWeights(ctx, "global", &w1, 10, 0.02)
	if err != nil {
		t.Fatal(err)
	}

	w2 := DefaultWeights()
	w2.Transition = 0.50
	err = store.SaveWeights(ctx, "global", &w2, 20, 0.01)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadWeights(ctx, "global")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected profile")
	}
	if loaded.SampleCount != 20 {
		t.Errorf("sample_count = %v, want 20", loaded.SampleCount)
	}
	if math.Abs(loaded.Weights.Transition-0.50) > epsilon {
		t.Errorf("Transition = %v, want 0.50", loaded.Weights.Transition)
	}
}

// -----------------------------------------------------------------------
// Concurrent safety tests
// -----------------------------------------------------------------------

func TestConcurrentSafety(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	dw := DefaultWeights()
	learner := NewLearner(&dw, cfg, nil)

	var wg sync.WaitGroup
	const goroutines = 20
	const updatesPerGoroutine = 50

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < updatesPerGoroutine; i++ {
				fPos := FeatureVector{Transition: 0.7, Frequency: 0.3}
				fNeg := FeatureVector{Task: 0.4}
				learner.Update(context.Background(), "global", &fPos, &fNeg)
			}
		}()
	}

	// Also read concurrently.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < updatesPerGoroutine; i++ {
				_ = learner.Weights()
				_ = learner.SampleCount()
				_ = learner.Eta()
			}
		}()
	}

	wg.Wait()

	if learner.SampleCount() != goroutines*updatesPerGoroutine {
		t.Errorf("sample count = %v, want %v",
			learner.SampleCount(), goroutines*updatesPerGoroutine)
	}

	// Weights should still be in valid ranges.
	w := learner.Weights()
	if w.Transition < 0 || w.Transition > cfg.WeightMax+epsilon {
		t.Errorf("Transition out of range: %v", w.Transition)
	}
	if w.RiskPenalty < cfg.WeightRiskMin-epsilon || w.RiskPenalty > cfg.WeightRiskMax+epsilon {
		t.Errorf("RiskPenalty out of range: %v", w.RiskPenalty)
	}
}

// -----------------------------------------------------------------------
// DefaultWeights / DefaultConfig tests
// -----------------------------------------------------------------------

func TestDefaultWeightsMatchSpec(t *testing.T) {
	w := DefaultWeights()

	// Spec Section 7.1 default weights.
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"Transition", w.Transition, 0.30},
		{"Frequency", w.Frequency, 0.20},
		{"Success", w.Success, 0.10},
		{"Prefix", w.Prefix, 0.15},
		{"Affinity", w.Affinity, 0.10},
		{"Task", w.Task, 0.05},
		{"Feedback", w.Feedback, 0.15},
		{"ProjectTypeAffinity", w.ProjectTypeAffinity, 0.08},
		{"FailureRecovery", w.FailureRecovery, 0.12},
		{"RiskPenalty", w.RiskPenalty, 0.20},
	}

	for _, c := range checks {
		if math.Abs(c.got-c.want) > epsilon {
			t.Errorf("DefaultWeights().%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestDefaultConfigMatchesSpec(t *testing.T) {
	cfg := DefaultConfig()

	if math.Abs(cfg.EtaInitial-0.02) > epsilon {
		t.Errorf("EtaInitial = %v, want 0.02", cfg.EtaInitial)
	}
	if math.Abs(cfg.EtaDecayConstant-500) > epsilon {
		t.Errorf("EtaDecayConstant = %v, want 500", cfg.EtaDecayConstant)
	}
	if math.Abs(cfg.EtaFloor-0.001) > epsilon {
		t.Errorf("EtaFloor = %v, want 0.001", cfg.EtaFloor)
	}
	if cfg.MinSamples != 30 {
		t.Errorf("MinSamples = %v, want 30", cfg.MinSamples)
	}
}

// -----------------------------------------------------------------------
// Integration: LoadFromStore
// -----------------------------------------------------------------------

func TestLoadFromStore(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	cfg := DefaultConfig()

	// Save a profile.
	w := DefaultWeights()
	w.Transition = 0.45
	err := store.SaveWeights(ctx, "repo:myrepo", &w, 100, 0.008)
	if err != nil {
		t.Fatal(err)
	}

	// Create a learner and load.
	dw := DefaultWeights()
	learner := NewLearner(&dw, cfg, store)
	found, err := learner.LoadFromStore(ctx, "repo:myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected to find persisted profile")
	}

	if math.Abs(learner.Weights().Transition-0.45) > epsilon {
		t.Errorf("loaded Transition = %v, want 0.45", learner.Weights().Transition)
	}
	if learner.SampleCount() != 100 {
		t.Errorf("loaded sample_count = %v, want 100", learner.SampleCount())
	}
}

func TestLoadFromStoreNotFound(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	cfg := DefaultConfig()

	dw := DefaultWeights()
	learner := NewLearner(&dw, cfg, store)
	found, err := learner.LoadFromStore(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected not found for nonexistent scope")
	}

	// Weights should remain at defaults.
	w := learner.Weights()
	if math.Abs(w.Transition-DefaultWeights().Transition) > epsilon {
		t.Error("weights should remain at defaults when scope not found")
	}
}

func TestLoadFromStoreNilStore(t *testing.T) {
	cfg := DefaultConfig()
	dw := DefaultWeights()
	learner := NewLearner(&dw, cfg, nil)

	found, err := learner.LoadFromStore(context.Background(), "global")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected not found when store is nil")
	}
}

// -----------------------------------------------------------------------
// Edge case: repeated updates converge
// -----------------------------------------------------------------------

func TestRepeatedUpdatesConverge(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	dw := DefaultWeights()
	learner := NewLearner(&dw, cfg, nil)

	// Apply the same update 1000 times. Due to eta decay, changes should diminish.
	fPos := FeatureVector{Transition: 0.8}
	fNeg := FeatureVector{Transition: 0.2}

	var prevW Weights
	for i := 0; i < 1000; i++ {
		prevW = learner.Weights()
		learner.Update(context.Background(), "test", &fPos, &fNeg)
	}

	w := learner.Weights()
	// After many updates with decaying eta, changes should be very small.
	delta := math.Abs(w.Transition - prevW.Transition)
	if delta > 0.001 {
		t.Errorf("expected convergence: last delta = %v", delta)
	}

	// All weights should still be in valid range.
	if w.Transition < cfg.WeightMin-epsilon || w.Transition > cfg.WeightMax+epsilon {
		t.Errorf("Transition out of range after convergence: %v", w.Transition)
	}
}

// -----------------------------------------------------------------------
// Edge case: no-op update when fPos == fNeg
// -----------------------------------------------------------------------

func TestNoOpUpdate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 0

	initial := DefaultWeights()
	learner := NewLearner(&initial, cfg, nil)

	fSame := FeatureVector{Transition: 0.5, Frequency: 0.3}
	learner.Update(context.Background(), "test", &fSame, &fSame)

	w := learner.Weights()
	// With identical feature vectors, the delta is zero so weights should
	// not change (renormalization is a no-op when sum is unchanged).
	if math.Abs(w.Transition-initial.Transition) > epsilon {
		t.Errorf("weights changed on no-op update: Transition=%v, want %v",
			w.Transition, initial.Transition)
	}
}
