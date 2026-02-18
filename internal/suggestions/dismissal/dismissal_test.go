package dismissal

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Create the dismissal_pattern table (from V2 schema).
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS dismissal_pattern (
			scope                   TEXT NOT NULL,
			context_template_id     TEXT NOT NULL,
			dismissed_template_id   TEXT NOT NULL,
			dismissal_count         INTEGER NOT NULL,
			last_dismissed_ms       INTEGER NOT NULL,
			suppression_level       TEXT NOT NULL,
			PRIMARY KEY(scope, context_template_id, dismissed_template_id)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func newStore(t *testing.T, threshold int) (*Store, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	cfg := Config{LearnedThreshold: threshold}
	store := NewStore(db, cfg, nil)
	return store, db
}

// --- State.IsValid ---

func TestState_IsValid(t *testing.T) {
	valid := []State{StateNone, StateTemporary, StateLearned, StatePermanent}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("expected %q to be valid", s)
		}
	}
	invalid := []State{"", "bogus", "NONE", "Temporary"}
	for _, s := range invalid {
		if s.IsValid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

// --- NewStore ---

func TestNewStore_NilLogger(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.logger == nil {
		t.Error("expected non-nil logger even when nil was passed")
	}
}

func TestNewStore_InvalidThreshold(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, Config{LearnedThreshold: 0}, nil)
	if store.cfg.LearnedThreshold != DefaultConfig().LearnedThreshold {
		t.Errorf("expected threshold to be corrected to default %d, got %d",
			DefaultConfig().LearnedThreshold, store.cfg.LearnedThreshold)
	}

	store = NewStore(db, Config{LearnedThreshold: -5}, nil)
	if store.cfg.LearnedThreshold != DefaultConfig().LearnedThreshold {
		t.Errorf("expected negative threshold to be corrected to default %d, got %d",
			DefaultConfig().LearnedThreshold, store.cfg.LearnedThreshold)
	}
}

// --- State transitions: NONE -> TEMPORARY -> LEARNED ---

func TestDismissal_NoneToTemporary(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Initially NONE.
	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE, got %q", state)
	}

	// First dismiss: NONE -> TEMPORARY.
	err = store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	if err != nil {
		t.Fatalf("RecordDismissal: %v", err)
	}

	state, err = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateTemporary {
		t.Errorf("expected TEMPORARY after 1 dismiss, got %q", state)
	}
}

func TestDismissal_TemporaryStaysTemporary(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Dismiss twice (threshold is 3).
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateTemporary {
		t.Errorf("expected TEMPORARY after 2 dismissals (threshold=3), got %q", state)
	}

	// Verify count.
	rec, err := store.GetRecord(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec.DismissalCount != 2 {
		t.Errorf("expected count 2, got %d", rec.DismissalCount)
	}
}

func TestDismissal_TemporaryToLearned(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Dismiss 3 times (threshold is 3).
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 3000)

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateLearned {
		t.Errorf("expected LEARNED after 3 dismissals (threshold=3), got %q", state)
	}

	rec, err := store.GetRecord(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec.DismissalCount != 3 {
		t.Errorf("expected count 3, got %d", rec.DismissalCount)
	}
	if rec.LastDismissedMs != 3000 {
		t.Errorf("expected last_dismissed_ms 3000, got %d", rec.LastDismissedMs)
	}
}

func TestDismissal_LearnedStaysLearned(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Get to LEARNED.
	for i := 0; i < 3; i++ {
		store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", int64(1000+i))
	}

	// Dismiss again; should stay LEARNED.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 5000)

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateLearned {
		t.Errorf("expected LEARNED to persist, got %q", state)
	}

	rec, err := store.GetRecord(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec.DismissalCount != 4 {
		t.Errorf("expected count 4, got %d", rec.DismissalCount)
	}
}

// --- Threshold = 1 edge case ---

func TestDismissal_ThresholdOne(t *testing.T) {
	store, _ := newStore(t, 1)
	ctx := context.Background()

	// With threshold 1, a single dismiss should go straight to LEARNED.
	err := store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	if err != nil {
		t.Fatalf("RecordDismissal: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateLearned {
		t.Errorf("expected LEARNED with threshold=1 after 1 dismiss, got %q", state)
	}
}

// --- Acceptance resets suppression ---

func TestAcceptance_ResetsTemporary(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Dismiss once -> TEMPORARY.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)

	// Accept -> NONE.
	err := store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordAcceptance: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE after acceptance, got %q", state)
	}
}

func TestAcceptance_ResetsLearned(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Get to LEARNED.
	for i := 0; i < 3; i++ {
		store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", int64(1000+i))
	}

	// Accept -> NONE.
	err := store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordAcceptance: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE after acceptance, got %q", state)
	}

	// Record should be gone.
	rec, err := store.GetRecord(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil record after acceptance, got %+v", rec)
	}
}

func TestAcceptance_ResetsPermanent(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Set to PERMANENT.
	store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 1000)

	// Accept -> NONE (acceptance overrides everything).
	err := store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordAcceptance: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE after acceptance of permanent, got %q", state)
	}
}

func TestAcceptance_NoExistingRecord(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Accept on non-existent record should be a no-op (no error).
	err := store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordAcceptance on non-existent: %v", err)
	}
}

// --- Acceptance resets count (dismiss after accept starts fresh) ---

func TestAcceptance_ResetsCount(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Dismiss twice.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)

	// Accept.
	store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")

	// Dismiss once more -- should be count=1, TEMPORARY (not LEARNED).
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 3000)

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateTemporary {
		t.Errorf("expected TEMPORARY after accept+1 dismiss, got %q", state)
	}

	rec, err := store.GetRecord(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec.DismissalCount != 1 {
		t.Errorf("expected count 1 after accept+1 dismiss, got %d", rec.DismissalCount)
	}
}

// --- Never action ---

func TestNever_FromNone(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	err := store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 1000)
	if err != nil {
		t.Fatalf("RecordNever: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StatePermanent {
		t.Errorf("expected PERMANENT, got %q", state)
	}
}

func TestNever_FromTemporary(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)

	err := store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 2000)
	if err != nil {
		t.Fatalf("RecordNever: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StatePermanent {
		t.Errorf("expected PERMANENT from temporary, got %q", state)
	}
}

func TestNever_FromLearned(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", int64(1000+i))
	}

	err := store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 5000)
	if err != nil {
		t.Fatalf("RecordNever: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StatePermanent {
		t.Errorf("expected PERMANENT from learned, got %q", state)
	}
}

// --- Dismiss does not downgrade PERMANENT ---

func TestDismissal_DoesNotDowngradePermanent(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 1000)

	// Dismiss should not change PERMANENT state.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StatePermanent {
		t.Errorf("expected PERMANENT to persist after dismiss, got %q", state)
	}

	// Count should not increment for permanent.
	rec, err := store.GetRecord(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec.DismissalCount != 1 {
		t.Errorf("expected permanent count to remain 1, got %d", rec.DismissalCount)
	}
}

// --- Unblock action ---

func TestUnblock_FromPermanent(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 1000)

	err := store.RecordUnblock(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordUnblock: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE after unblock, got %q", state)
	}
}

func TestUnblock_OnlyAffectsPermanent(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Set to TEMPORARY.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)

	// Unblock should not affect non-PERMANENT records.
	err := store.RecordUnblock(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordUnblock: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateTemporary {
		t.Errorf("expected TEMPORARY to persist after unblock of non-permanent, got %q", state)
	}
}

func TestUnblock_OnlyAffectsLearned_NotModified(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Get to LEARNED.
	for i := 0; i < 3; i++ {
		store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", int64(1000+i))
	}

	// Unblock should not affect LEARNED.
	err := store.RecordUnblock(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordUnblock: %v", err)
	}

	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateLearned {
		t.Errorf("expected LEARNED to persist after unblock, got %q", state)
	}
}

func TestUnblock_NoExistingRecord(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Unblock on non-existent record should be a no-op.
	err := store.RecordUnblock(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("RecordUnblock on non-existent: %v", err)
	}
}

// --- FilterCandidates ---

func TestFilterCandidates_RemovesLearnedAndPermanent(t *testing.T) {
	store, _ := newStore(t, 2)
	ctx := context.Background()

	// tmpl-a: LEARNED (dismiss twice with threshold 2).
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)

	// tmpl-b: PERMANENT.
	store.RecordNever(ctx, "global", "ctx-1", "tmpl-b", 3000)

	// tmpl-c: TEMPORARY (dismiss once).
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-c", 4000)

	// tmpl-d: NONE (no dismissals).

	candidates := []Candidate{
		{TemplateID: "tmpl-a"},
		{TemplateID: "tmpl-b"},
		{TemplateID: "tmpl-c"},
		{TemplateID: "tmpl-d"},
	}

	filtered, err := store.FilterCandidates(ctx, "global", "ctx-1", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}

	// tmpl-a (LEARNED) and tmpl-b (PERMANENT) should be removed.
	// tmpl-c (TEMPORARY) and tmpl-d (NONE) should remain.
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered candidates, got %d", len(filtered))
	}
	if filtered[0].TemplateID != "tmpl-c" {
		t.Errorf("expected tmpl-c, got %q", filtered[0].TemplateID)
	}
	if filtered[1].TemplateID != "tmpl-d" {
		t.Errorf("expected tmpl-d, got %q", filtered[1].TemplateID)
	}
}

func TestFilterCandidates_EmptyCandidates(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	filtered, err := store.FilterCandidates(ctx, "global", "ctx-1", nil)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if filtered != nil {
		t.Errorf("expected nil for nil input, got %v", filtered)
	}

	filtered, err = store.FilterCandidates(ctx, "global", "ctx-1", []Candidate{})
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 0 {
		t.Errorf("expected empty for empty input, got %v", filtered)
	}
}

func TestFilterCandidates_EmptyScope(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	candidates := []Candidate{{TemplateID: "tmpl-a"}}
	filtered, err := store.FilterCandidates(ctx, "", "ctx-1", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected all candidates returned for empty scope, got %d", len(filtered))
	}
}

func TestFilterCandidates_NoSuppressed(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	candidates := []Candidate{
		{TemplateID: "tmpl-a"},
		{TemplateID: "tmpl-b"},
	}

	filtered, err := store.FilterCandidates(ctx, "global", "ctx-1", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("expected all 2 candidates, got %d", len(filtered))
	}
}

func TestFilterCandidates_AllSuppressed(t *testing.T) {
	store, _ := newStore(t, 1)
	ctx := context.Background()

	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	store.RecordNever(ctx, "global", "ctx-1", "tmpl-b", 2000)

	candidates := []Candidate{
		{TemplateID: "tmpl-a"},
		{TemplateID: "tmpl-b"},
	}

	filtered, err := store.FilterCandidates(ctx, "global", "ctx-1", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 0 {
		t.Errorf("expected 0 candidates when all suppressed, got %d", len(filtered))
	}
}

// --- IsSuppressed ---

func TestIsSuppressed(t *testing.T) {
	store, _ := newStore(t, 2)
	ctx := context.Background()

	// NONE -> not suppressed.
	suppressed, err := store.IsSuppressed(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	}
	if suppressed {
		t.Error("expected not suppressed for NONE")
	}

	// TEMPORARY -> not suppressed.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	suppressed, err = store.IsSuppressed(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	}
	if suppressed {
		t.Error("expected not suppressed for TEMPORARY")
	}

	// LEARNED -> suppressed.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)
	suppressed, err = store.IsSuppressed(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	}
	if !suppressed {
		t.Error("expected suppressed for LEARNED")
	}

	// PERMANENT -> suppressed.
	store.RecordNever(ctx, "global", "ctx-1", "tmpl-b", 3000)
	suppressed, err = store.IsSuppressed(ctx, "global", "ctx-1", "tmpl-b")
	if err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	}
	if !suppressed {
		t.Error("expected suppressed for PERMANENT")
	}
}

// --- Scope isolation ---

func TestDismissal_ScopeIsolation(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Dismiss in "repo-a" scope.
	store.RecordDismissal(ctx, "repo-a", "ctx-1", "tmpl-a", 1000)

	// Should not affect "global" scope.
	state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE in global scope, got %q", state)
	}

	// Should be TEMPORARY in "repo-a" scope.
	state, err = store.GetState(ctx, "repo-a", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateTemporary {
		t.Errorf("expected TEMPORARY in repo-a scope, got %q", state)
	}
}

// --- Context isolation ---

func TestDismissal_ContextIsolation(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// Dismiss when context is "ctx-1".
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)

	// Should not affect context "ctx-2".
	state, err := store.GetState(ctx, "global", "ctx-2", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE for different context, got %q", state)
	}
}

// --- Validation of required fields ---

func TestRecordDismissal_RequiredFields(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	cases := []struct {
		name      string
		scope     string
		ctxTmpl   string
		dismissed string
	}{
		{"empty scope", "", "ctx-1", "tmpl-a"},
		{"empty context_template_id", "global", "", "tmpl-a"},
		{"empty dismissed_template_id", "global", "ctx-1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.RecordDismissal(ctx, tc.scope, tc.ctxTmpl, tc.dismissed, 1000)
			if err == nil {
				t.Error("expected error for missing required field")
			}
		})
	}
}

func TestRecordAcceptance_RequiredFields(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	cases := []struct {
		name      string
		scope     string
		ctxTmpl   string
		dismissed string
	}{
		{"empty scope", "", "ctx-1", "tmpl-a"},
		{"empty context_template_id", "global", "", "tmpl-a"},
		{"empty dismissed_template_id", "global", "ctx-1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.RecordAcceptance(ctx, tc.scope, tc.ctxTmpl, tc.dismissed)
			if err == nil {
				t.Error("expected error for missing required field")
			}
		})
	}
}

func TestRecordNever_RequiredFields(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	cases := []struct {
		name      string
		scope     string
		ctxTmpl   string
		dismissed string
	}{
		{"empty scope", "", "ctx-1", "tmpl-a"},
		{"empty context_template_id", "global", "", "tmpl-a"},
		{"empty dismissed_template_id", "global", "ctx-1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.RecordNever(ctx, tc.scope, tc.ctxTmpl, tc.dismissed, 1000)
			if err == nil {
				t.Error("expected error for missing required field")
			}
		})
	}
}

func TestRecordUnblock_RequiredFields(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	cases := []struct {
		name      string
		scope     string
		ctxTmpl   string
		dismissed string
	}{
		{"empty scope", "", "ctx-1", "tmpl-a"},
		{"empty context_template_id", "global", "", "tmpl-a"},
		{"empty dismissed_template_id", "global", "ctx-1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.RecordUnblock(ctx, tc.scope, tc.ctxTmpl, tc.dismissed)
			if err == nil {
				t.Error("expected error for missing required field")
			}
		})
	}
}

// --- GetState with empty fields returns NONE ---

func TestGetState_EmptyFields(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	state, err := store.GetState(ctx, "", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateNone {
		t.Errorf("expected NONE for empty scope, got %q", state)
	}
}

// --- GetRecord with empty fields returns nil ---

func TestGetRecord_EmptyFields(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	rec, err := store.GetRecord(ctx, "", "ctx-1", "tmpl-a")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil for empty scope, got %+v", rec)
	}
}

// --- DefaultConfig ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LearnedThreshold != 3 {
		t.Errorf("expected default threshold 3, got %d", cfg.LearnedThreshold)
	}
}

// --- Full lifecycle test ---

func TestFullLifecycle(t *testing.T) {
	store, _ := newStore(t, 3)
	ctx := context.Background()

	// 1. NONE: no record.
	state, _ := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateNone {
		t.Fatalf("step 1: expected NONE, got %q", state)
	}

	// 2. Dismiss 1x: TEMPORARY.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 1000)
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateTemporary {
		t.Fatalf("step 2: expected TEMPORARY, got %q", state)
	}

	// 3. Dismiss 2x: still TEMPORARY.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 2000)
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateTemporary {
		t.Fatalf("step 3: expected TEMPORARY, got %q", state)
	}

	// 4. Accept: back to NONE.
	store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateNone {
		t.Fatalf("step 4: expected NONE, got %q", state)
	}

	// 5. Dismiss 3x: TEMPORARY -> TEMPORARY -> LEARNED.
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 3000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 4000)
	store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", 5000)
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateLearned {
		t.Fatalf("step 5: expected LEARNED, got %q", state)
	}

	// 6. NEVER: PERMANENT.
	store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 6000)
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StatePermanent {
		t.Fatalf("step 6: expected PERMANENT, got %q", state)
	}

	// 7. Unblock: back to NONE.
	store.RecordUnblock(ctx, "global", "ctx-1", "tmpl-a")
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateNone {
		t.Fatalf("step 7: expected NONE, got %q", state)
	}

	// 8. Never from NONE: straight to PERMANENT.
	store.RecordNever(ctx, "global", "ctx-1", "tmpl-a", 7000)
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StatePermanent {
		t.Fatalf("step 8: expected PERMANENT, got %q", state)
	}

	// 9. Accept from PERMANENT: resets to NONE.
	store.RecordAcceptance(ctx, "global", "ctx-1", "tmpl-a")
	state, _ = store.GetState(ctx, "global", "ctx-1", "tmpl-a")
	if state != StateNone {
		t.Fatalf("step 9: expected NONE, got %q", state)
	}
}

// --- FilterCandidates respects scope and context ---

func TestFilterCandidates_ScopeAndContextIsolation(t *testing.T) {
	store, _ := newStore(t, 1)
	ctx := context.Background()

	// Suppress tmpl-a in (repo-x, ctx-1).
	store.RecordDismissal(ctx, "repo-x", "ctx-1", "tmpl-a", 1000)

	candidates := []Candidate{{TemplateID: "tmpl-a"}}

	// Should NOT be filtered in different scope.
	filtered, err := store.FilterCandidates(ctx, "global", "ctx-1", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 candidate in global scope, got %d", len(filtered))
	}

	// Should NOT be filtered in different context.
	filtered, err = store.FilterCandidates(ctx, "repo-x", "ctx-2", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 candidate for different context, got %d", len(filtered))
	}

	// Should be filtered in matching scope+context.
	filtered, err = store.FilterCandidates(ctx, "repo-x", "ctx-1", candidates)
	if err != nil {
		t.Fatalf("FilterCandidates: %v", err)
	}
	if len(filtered) != 0 {
		t.Errorf("expected 0 candidates for matching scope+context, got %d", len(filtered))
	}
}

// --- Configurable threshold ---

func TestConfigurableThreshold(t *testing.T) {
	tests := []struct {
		threshold     int
		dismissals    int
		expectedState State
	}{
		{threshold: 1, dismissals: 1, expectedState: StateLearned},
		{threshold: 2, dismissals: 1, expectedState: StateTemporary},
		{threshold: 2, dismissals: 2, expectedState: StateLearned},
		{threshold: 5, dismissals: 4, expectedState: StateTemporary},
		{threshold: 5, dismissals: 5, expectedState: StateLearned},
		{threshold: 5, dismissals: 6, expectedState: StateLearned},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("threshold=%d_dismissals=%d", tc.threshold, tc.dismissals), func(t *testing.T) {
			store, _ := newStore(t, tc.threshold)
			ctx := context.Background()

			for i := 0; i < tc.dismissals; i++ {
				store.RecordDismissal(ctx, "global", "ctx-1", "tmpl-a", int64(1000+i))
			}

			state, err := store.GetState(ctx, "global", "ctx-1", "tmpl-a")
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			if state != tc.expectedState {
				t.Errorf("expected %q, got %q", tc.expectedState, state)
			}
		})
	}
}
