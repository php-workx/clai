package cmd

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestCheckFeedbackStats(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE suggestion_feedback(action TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO suggestion_feedback(action) VALUES
		('accepted'),
		('accepted'),
		('dismissed'),
		('edited'),
		('ignored')
	`); err != nil {
		t.Fatalf("INSERT error = %v", err)
	}

	stats := checkFeedbackStats(context.Background(), db)
	if stats.Status != "ok" {
		t.Fatalf("status = %q, want ok", stats.Status)
	}
	if stats.Accepted != 2 || stats.Dismissed != 1 || stats.Edited != 1 {
		t.Fatalf("unexpected counts: %#v", stats)
	}
	if stats.Total != 5 {
		t.Fatalf("total = %d, want 5", stats.Total)
	}
}

func TestCheckFeedbackStats_QueryError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	stats := checkFeedbackStats(context.Background(), db)
	if !strings.Contains(stats.Status, "error:") {
		t.Fatalf("status = %q, want error status", stats.Status)
	}
}
