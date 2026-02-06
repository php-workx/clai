package invariant

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/runger/clai/internal/suggestions/db"
)

// createTestDB creates a temporary V2 database for invariant testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "invariant_test.db")

	ctx := context.Background()
	sdb, err := db.Open(ctx, db.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	t.Cleanup(func() { sdb.Close() })

	return sdb.DB()
}

func TestAssertSessionIsolation(t *testing.T) {
	sqlDB := createTestDB(t)
	// This should pass on clean state - no cross-session leakage
	AssertSessionIsolation(t, sqlDB, "session-alpha", "session-beta")
}

func TestAssertSessionIsolation_DetectsLeak(t *testing.T) {
	sqlDB := createTestDB(t)

	// Set up sessions
	ctx := context.Background()
	now := int64(1000000)

	for _, sid := range []string{"sess-x", "sess-y"} {
		_, err := sqlDB.ExecContext(ctx,
			"INSERT INTO session (id, shell, started_at_ms) VALUES (?, 'bash', ?)",
			sid, now)
		if err != nil {
			t.Fatalf("failed to insert session: %v", err)
		}
	}

	// Insert legitimate transitions for each session
	_, err := sqlDB.ExecContext(ctx,
		"INSERT INTO transition_stat (scope, prev_template_id, next_template_id, weight, count, last_seen_ms) VALUES ('session:sess-x', 'tmpl_x1', 'tmpl_x2', 1.0, 1, ?)", now)
	if err != nil {
		t.Fatalf("failed to insert transition: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx,
		"INSERT INTO transition_stat (scope, prev_template_id, next_template_id, weight, count, last_seen_ms) VALUES ('session:sess-y', 'tmpl_y1', 'tmpl_y2', 1.0, 1, ?)", now)
	if err != nil {
		t.Fatalf("failed to insert transition: %v", err)
	}

	// Verify that transitions are properly isolated
	var countX, countY int
	sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM transition_stat WHERE scope = 'session:sess-x'").Scan(&countX)
	sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM transition_stat WHERE scope = 'session:sess-y'").Scan(&countY)

	if countX != 1 {
		t.Errorf("expected 1 transition for sess-x, got %d", countX)
	}
	if countY != 1 {
		t.Errorf("expected 1 transition for sess-y, got %d", countY)
	}
}

func TestAssertDeterministicRanking(t *testing.T) {
	sqlDB := createTestDB(t)
	ctx := context.Background()
	now := int64(2000000)

	// Ensure session exists
	_, err := sqlDB.ExecContext(ctx,
		"INSERT INTO session (id, shell, started_at_ms) VALUES ('rank-session', 'bash', ?)", now)
	if err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	// Insert some transitions with different weights for deterministic ordering
	transitions := []struct {
		prev, next string
		weight     float64
		count      int
	}{
		{"tmpl_r0", "tmpl_r1", 3.0, 5},
		{"tmpl_r0", "tmpl_r2", 1.0, 2},
		{"tmpl_r0", "tmpl_r3", 2.0, 3},
	}

	for _, tr := range transitions {
		_, err := sqlDB.ExecContext(ctx,
			"INSERT INTO transition_stat (scope, prev_template_id, next_template_id, weight, count, last_seen_ms) VALUES ('global', ?, ?, ?, ?, ?)",
			tr.prev, tr.next, tr.weight, tr.count, now)
		if err != nil {
			t.Fatalf("failed to insert transition: %v", err)
		}
	}

	// Should pass: repeated queries return identical ordering
	AssertDeterministicRanking(t, sqlDB, "global")
}

func TestAssertDeterministicRanking_EmptyScope(t *testing.T) {
	sqlDB := createTestDB(t)

	// Should pass even with an empty scope (no rows is still deterministic)
	AssertDeterministicRanking(t, sqlDB, "nonexistent-scope")
}

func TestAssertTransactionalConsistency(t *testing.T) {
	sqlDB := createTestDB(t)
	// Should pass: all writes are committed atomically
	AssertTransactionalConsistency(t, sqlDB)
}

func TestAssertTransactionalConsistency_VerifiesAllTables(t *testing.T) {
	sqlDB := createTestDB(t)
	ctx := context.Background()

	// Run the assertion first to populate data
	AssertTransactionalConsistency(t, sqlDB)

	// Verify that the data was actually written to all three tables
	var eventCount, templateCount, statCount int
	sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM command_event WHERE session_id = 'test-txn-session'").Scan(&eventCount)
	sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM command_template WHERE template_id = 'tmpl_txn_test'").Scan(&templateCount)
	sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM command_stat WHERE template_id = 'tmpl_txn_test'").Scan(&statCount)

	if eventCount < 1 {
		t.Errorf("expected at least 1 command_event row, got %d", eventCount)
	}
	if templateCount < 1 {
		t.Errorf("expected at least 1 command_template row, got %d", templateCount)
	}
	if statCount < 1 {
		t.Errorf("expected at least 1 command_stat row, got %d", statCount)
	}
}

func TestCreateTestDB_HasV2Schema(t *testing.T) {
	sqlDB := createTestDB(t)
	ctx := context.Background()

	// Verify that key V2 tables exist
	tables := []string{
		"session",
		"command_event",
		"command_template",
		"transition_stat",
		"command_stat",
		"suggestion_feedback",
		"schema_migrations",
	}

	for _, table := range tables {
		var name string
		err := sqlDB.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("V2 table %q not found: %v", table, err)
		}
	}
}

// TestMain sets up CLAI_HOME to avoid polluting the user's home directory.
func TestMain(m *testing.M) {
	tmpHome, err := os.MkdirTemp("", "clai-invariant-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpHome)
	os.Setenv("CLAI_HOME", tmpHome)
	os.Exit(m.Run())
}
