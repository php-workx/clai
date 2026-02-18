package feedback

import (
	"context"
	"database/sql"
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

	// Create the V2 schema tables needed for tests
	_, err = db.Exec(`
		CREATE TABLE suggestion_feedback (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			ts_ms INTEGER NOT NULL,
			prompt_prefix TEXT,
			suggested_text TEXT NOT NULL,
			action TEXT NOT NULL,
			executed_text TEXT,
			latency_ms INTEGER DEFAULT 0
		);
		CREATE TABLE slot_correlation (
			scope TEXT NOT NULL DEFAULT '',
			template_id TEXT NOT NULL,
			slot_key TEXT NOT NULL,
			tuple_hash TEXT NOT NULL,
			tuple_value_json TEXT NOT NULL DEFAULT '{}',
			weight REAL NOT NULL DEFAULT 1.0,
			count INTEGER NOT NULL DEFAULT 1,
			last_seen_ms INTEGER NOT NULL,
			PRIMARY KEY (scope, template_id, slot_key, tuple_hash)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecordFeedback(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	id, err := store.RecordFeedback(ctx, &FeedbackRecord{
		SessionID:     "sess-1",
		SuggestedText: "git status",
		Action:        ActionAccepted,
		TSMs:          1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestRecordFeedback_AllActions(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	actions := []FeedbackAction{
		ActionAccepted, ActionDismissed, ActionEdited,
		ActionNever, ActionUnblock, ActionIgnored, ActionTimeout,
	}
	for _, action := range actions {
		_, err := store.RecordFeedback(ctx, &FeedbackRecord{
			SessionID:     "sess-1",
			SuggestedText: "cmd-" + string(action),
			Action:        action,
			TSMs:          1000,
		})
		if err != nil {
			t.Errorf("action %q: unexpected error: %v", action, err)
		}
	}
}

func TestRecordFeedback_MissingSessionID(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	_, err := store.RecordFeedback(ctx, &FeedbackRecord{
		SuggestedText: "git status",
		Action:        ActionAccepted,
	})
	if err == nil {
		t.Error("expected error for missing session_id")
	}
}

func TestRecordFeedback_MissingSuggestedText(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	_, err := store.RecordFeedback(ctx, &FeedbackRecord{
		SessionID: "sess-1",
		Action:    ActionAccepted,
	})
	if err == nil {
		t.Error("expected error for missing suggested_text")
	}
}

func TestRecordFeedback_InvalidAction(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	_, err := store.RecordFeedback(ctx, &FeedbackRecord{
		SessionID:     "sess-1",
		SuggestedText: "git status",
		Action:        "bogus",
	})
	if err == nil {
		t.Error("expected error for invalid action")
	}
}

func TestCheckImplicitAcceptance_ExactMatch(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	now := int64(10000)
	store.TrackSuggestion("sess-1", "git status", "git ", now)

	method, err := store.CheckImplicitAcceptance(ctx, "sess-1", "git status", now+100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != MatchImplicitExact {
		t.Errorf("expected %q, got %q", MatchImplicitExact, method)
	}
}

func TestCheckImplicitAcceptance_PrefixMatch(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	now := int64(10000)
	store.TrackSuggestion("sess-1", "git status", "git ", now)

	method, err := store.CheckImplicitAcceptance(ctx, "sess-1", "git status --short", now+100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != MatchImplicitPrefix {
		t.Errorf("expected %q, got %q", MatchImplicitPrefix, method)
	}
}

func TestCheckImplicitAcceptance_OutsideWindow(t *testing.T) {
	db := setupTestDB(t)
	cfg := DefaultConfig()
	cfg.MatchWindowMs = 1000
	store := NewStore(db, cfg, nil)
	ctx := context.Background()

	now := int64(10000)
	store.TrackSuggestion("sess-1", "git status", "git ", now)

	method, err := store.CheckImplicitAcceptance(ctx, "sess-1", "git status", now+2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "" {
		t.Errorf("expected empty method outside window, got %q", method)
	}
}

func TestCheckImplicitAcceptance_WrongSession(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	now := int64(10000)
	store.TrackSuggestion("sess-1", "git status", "git ", now)

	method, err := store.CheckImplicitAcceptance(ctx, "sess-OTHER", "git status", now+100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "" {
		t.Errorf("expected empty method for wrong session, got %q", method)
	}
}

func TestCheckImplicitAcceptance_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	now := int64(10000)
	store.TrackSuggestion("sess-1", "git status", "git ", now)

	method, err := store.CheckImplicitAcceptance(ctx, "sess-1", "docker ps", now+100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "" {
		t.Errorf("expected empty method for no match, got %q", method)
	}
}

func TestCheckImplicitAcceptance_EmptyInputs(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	method, err := store.CheckImplicitAcceptance(ctx, "", "cmd", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "" {
		t.Errorf("expected empty for empty session")
	}

	method, err = store.CheckImplicitAcceptance(ctx, "sess-1", "", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "" {
		t.Errorf("expected empty for empty command")
	}
}

func TestUpdateSlotCorrelation(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	err := store.UpdateSlotCorrelation(ctx, "global", "tmpl-1", "slot-a", "hash1", `{"k":"v"}`, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Upsert: same key should increment weight and count
	err = store.UpdateSlotCorrelation(ctx, "global", "tmpl-1", "slot-a", "hash1", `{"k":"v"}`, 2000)
	if err != nil {
		t.Fatalf("unexpected error on upsert: %v", err)
	}

	var weight float64
	var count int
	err = db.QueryRow("SELECT weight, count FROM slot_correlation WHERE tuple_hash = 'hash1'").Scan(&weight, &count)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	if weight != 2.0 {
		t.Errorf("expected weight 2.0, got %f", weight)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestUpdateSlotCorrelation_EmptyFields(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	// Should be a no-op when required fields are empty
	err := store.UpdateSlotCorrelation(ctx, "global", "", "slot-a", "hash1", `{}`, 1000)
	if err != nil {
		t.Errorf("expected nil error for empty templateID, got: %v", err)
	}
	err = store.UpdateSlotCorrelation(ctx, "global", "tmpl-1", "", "hash1", `{}`, 1000)
	if err != nil {
		t.Errorf("expected nil error for empty slotKey, got: %v", err)
	}
	err = store.UpdateSlotCorrelation(ctx, "global", "tmpl-1", "slot-a", "", `{}`, 1000)
	if err != nil {
		t.Errorf("expected nil error for empty tupleHash, got: %v", err)
	}
}

func TestQueryFeedback(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	// Insert some records
	for i := 0; i < 3; i++ {
		_, err := store.RecordFeedback(ctx, &FeedbackRecord{
			SessionID:     "sess-1",
			SuggestedText: "cmd",
			Action:        ActionAccepted,
			TSMs:          int64(1000 + i),
		})
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	recs, err := store.QueryFeedback(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("expected 3 records, got %d", len(recs))
	}
}

func TestCountByAction(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db, DefaultConfig(), nil)
	ctx := context.Background()

	store.RecordFeedback(ctx, &FeedbackRecord{SessionID: "sess-1", SuggestedText: "a", Action: ActionAccepted, TSMs: 1000})
	store.RecordFeedback(ctx, &FeedbackRecord{SessionID: "sess-1", SuggestedText: "b", Action: ActionAccepted, TSMs: 1001})
	store.RecordFeedback(ctx, &FeedbackRecord{SessionID: "sess-1", SuggestedText: "c", Action: ActionDismissed, TSMs: 1002})

	counts, err := store.CountByAction(ctx, "sess-1")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if counts[ActionAccepted] != 2 {
		t.Errorf("expected 2 accepted, got %d", counts[ActionAccepted])
	}
	if counts[ActionDismissed] != 1 {
		t.Errorf("expected 1 dismissed, got %d", counts[ActionDismissed])
	}
}

func TestIsValidAction(t *testing.T) {
	valid := []FeedbackAction{ActionAccepted, ActionDismissed, ActionEdited, ActionNever, ActionUnblock, ActionIgnored, ActionTimeout}
	for _, a := range valid {
		if !isValidAction(a) {
			t.Errorf("expected %q to be valid", a)
		}
	}
	if isValidAction("bogus") {
		t.Error("expected 'bogus' to be invalid")
	}
	if isValidAction("") {
		t.Error("expected empty to be invalid")
	}
}

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
