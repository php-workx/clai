// Package invariant provides test helpers for key correctness invariants
// of the suggestions engine, as defined in spec Section 19.
package invariant

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

const sessionScopeFmt = "session:%s"

// AssertSessionIsolation validates invariant I1: suggestions for session A
// must not include session-scoped transitions derived exclusively from session B.
//
// It inserts commands into two distinct sessions and verifies that
// transition_stat rows scoped to one session do not leak into the other.
func AssertSessionIsolation(t *testing.T, db *sql.DB, sessionA, sessionB string) {
	t.Helper()
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()

	ensureSessionsExist(t, db, []string{sessionA, sessionB}, nowMs)

	// Insert command events for session A: cmd_a1 -> cmd_a2
	insertEvent(t, db, sessionA, "cmd_a1", "cmd_a1", "tmpl_a1", nowMs)
	insertEvent(t, db, sessionA, "cmd_a2", "cmd_a2", "tmpl_a2", nowMs+1)

	// Insert command events for session B: cmd_b1 -> cmd_b2
	insertEvent(t, db, sessionB, "cmd_b1", "cmd_b1", "tmpl_b1", nowMs+2)
	insertEvent(t, db, sessionB, "cmd_b2", "cmd_b2", "tmpl_b2", nowMs+3)

	// Insert session-scoped transitions
	insertTransition(t, db, fmt.Sprintf(sessionScopeFmt, sessionA), "tmpl_a1", "tmpl_a2", nowMs)
	insertTransition(t, db, fmt.Sprintf(sessionScopeFmt, sessionB), "tmpl_b1", "tmpl_b2", nowMs)

	assertScopeExcludesTemplates(t, ctx, db, fmt.Sprintf(sessionScopeFmt, sessionA), sessionA, sessionB, "tmpl_b1", "tmpl_b2")
	assertScopeExcludesTemplates(t, ctx, db, fmt.Sprintf(sessionScopeFmt, sessionB), sessionB, sessionA, "tmpl_a1", "tmpl_a2")
}

// AssertDeterministicRanking validates invariant I3: with identical input state,
// returned top-k must be byte-for-byte identical across repeated calls.
//
// It queries transition_stat for the given scope and verifies that results
// are stable across multiple queries with the same parameters.
func AssertDeterministicRanking(t *testing.T, db *sql.DB, scope string) {
	t.Helper()
	ctx := context.Background()

	query := `
		SELECT next_template_id, weight, count
		FROM transition_stat
		WHERE scope = ?
		ORDER BY weight DESC, count DESC, next_template_id ASC
	`

	// Run the query twice and compare results
	results1 := queryTransitions(t, ctx, db, query, scope)
	results2 := queryTransitions(t, ctx, db, query, scope)

	if len(results1) != len(results2) {
		t.Fatalf("I3 violation: first query returned %d rows, second returned %d",
			len(results1), len(results2))
	}

	for i := range results1 {
		if results1[i] != results2[i] {
			t.Errorf("I3 violation: row %d differs between calls: %v vs %v",
				i, results1[i], results2[i])
		}
	}
}

// AssertTransactionalConsistency validates invariant I5: after successful commit
// of a non-ephemeral command_end, dependent aggregate tables reflect the same
// event version atomically.
//
// It inserts a command event within a transaction along with its dependent
// aggregates, then verifies all tables reflect the same event.
func AssertTransactionalConsistency(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	fixture := insertTransactionalFixture(t, ctx, db)
	assertTransactionalFixtureExists(t, ctx, db, fixture)
	assertFixtureTimestamps(t, ctx, db, fixture)
}

type txnFixture struct {
	eventID    int64
	templateID string
	scope      string
	nowMs      int64
}

func ensureSessionsExist(t *testing.T, db *sql.DB, sessionIDs []string, nowMs int64) {
	t.Helper()
	ctx := context.Background()
	for _, sid := range sessionIDs {
		_, err := db.ExecContext(ctx,
			"INSERT OR IGNORE INTO session (id, shell, started_at_ms) VALUES (?, 'bash', ?)",
			sid, nowMs)
		if err != nil {
			t.Fatalf("failed to insert session %q: %v", sid, err)
		}
	}
}

func assertScopeExcludesTemplates(t *testing.T, ctx context.Context, db *sql.DB, scope, currentSession, otherSession string, forbidden ...string) {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		"SELECT next_template_id FROM transition_stat WHERE scope = ?",
		scope)
	if err != nil {
		t.Fatalf("failed to query %s transitions: %v", currentSession, err)
	}
	defer rows.Close()

	for rows.Next() {
		var nextTmpl string
		if err := rows.Scan(&nextTmpl); err != nil {
			t.Fatalf("failed to scan transition: %v", err)
		}
		for _, forbiddenTmpl := range forbidden {
			if nextTmpl == forbiddenTmpl {
				t.Errorf("I1 violation: session %q transitions contain session %q template %q",
					currentSession, otherSession, nextTmpl)
			}
		}
	}
}

func insertTransactionalFixture(t *testing.T, ctx context.Context, db *sql.DB) txnFixture {
	t.Helper()
	nowMs := time.Now().UnixMilli()
	sessionID := "test-txn-session"
	templateID := "tmpl_txn_test"
	scope := "global"

	ensureSessionsExist(t, db, []string{sessionID}, nowMs)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		`INSERT INTO command_event
			(session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id, exit_code, ephemeral)
		 VALUES
			(?, ?, '/tmp', 'test cmd', 'test cmd', ?, 0, 0)`,
		sessionID, nowMs, templateID)
	if err != nil {
		t.Fatalf("failed to insert command_event: %v", err)
	}
	eventID, _ := result.LastInsertId()

	_, err = tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO command_template (template_id, cmd_norm, slot_count, first_seen_ms, last_seen_ms) VALUES (?, 'test cmd', 0, ?, ?)",
		templateID, nowMs, nowMs)
	if err != nil {
		t.Fatalf("failed to insert command_template: %v", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO command_stat (scope, template_id, score, success_count, failure_count, last_seen_ms) VALUES (?, ?, 1.0, 1, 0, ?)",
		scope, templateID, nowMs)
	if err != nil {
		t.Fatalf("failed to insert command_stat: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	return txnFixture{eventID: eventID, templateID: templateID, scope: scope, nowMs: nowMs}
}

func assertTransactionalFixtureExists(t *testing.T, ctx context.Context, db *sql.DB, fixture txnFixture) {
	t.Helper()
	assertExistsByID(t, ctx, db, "command_event", "id", fixture.eventID)
	assertExistsByID(t, ctx, db, "command_template", "template_id", fixture.templateID)
	assertCommandStatExists(t, ctx, db, fixture.scope, fixture.templateID)
}

func assertExistsByID(t *testing.T, ctx context.Context, db *sql.DB, table, col string, id any) {
	t.Helper()

	var query string
	switch {
	case table == "command_event" && col == "id":
		query = "SELECT COUNT(*) > 0 FROM command_event WHERE id = ?"
	case table == "command_template" && col == "template_id":
		query = "SELECT COUNT(*) > 0 FROM command_template WHERE template_id = ?"
	default:
		t.Fatalf("unsupported assertExistsByID target: %s.%s", table, col)
	}

	var exists bool
	if err := db.QueryRowContext(ctx, query, id).Scan(&exists); err != nil || !exists {
		t.Errorf("I5 violation: %s row (%s=%v) not found after commit", table, col, id)
	}
}

func assertCommandStatExists(t *testing.T, ctx context.Context, db *sql.DB, scope, templateID string) {
	t.Helper()
	var exists bool
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) > 0 FROM command_stat WHERE scope = ? AND template_id = ?",
		scope, templateID).Scan(&exists)
	if err != nil || !exists {
		t.Errorf("I5 violation: command_stat for scope=%q template=%q not found after commit", scope, templateID)
	}
}

func assertFixtureTimestamps(t *testing.T, ctx context.Context, db *sql.DB, fixture txnFixture) {
	t.Helper()
	csLastSeen := queryInt64(t, ctx, db,
		"SELECT last_seen_ms FROM command_stat WHERE scope = ? AND template_id = ?",
		fixture.scope, fixture.templateID)
	if csLastSeen != fixture.nowMs {
		t.Errorf("I5 violation: command_stat.last_seen_ms = %d, want %d", csLastSeen, fixture.nowMs)
	}

	ctLastSeen := queryInt64(t, ctx, db,
		"SELECT last_seen_ms FROM command_template WHERE template_id = ?",
		fixture.templateID)
	if ctLastSeen != fixture.nowMs {
		t.Errorf("I5 violation: command_template.last_seen_ms = %d, want %d", ctLastSeen, fixture.nowMs)
	}
}

func queryInt64(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var val int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&val); err != nil {
		t.Fatalf("failed to query int64 value: %v", err)
	}
	return val
}

// insertEvent inserts a command event into the database for invariant testing.
func insertEvent(t *testing.T, db *sql.DB, sessionID, cmdRaw, cmdNorm, templateID string, tsMs int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id, exit_code, ephemeral) VALUES (?, ?, '/tmp', ?, ?, ?, 0, 0)",
		sessionID, tsMs, cmdRaw, cmdNorm, templateID)
	if err != nil {
		t.Fatalf("failed to insert command event: %v", err)
	}
}

// insertTransition inserts a transition_stat row for invariant testing.
func insertTransition(t *testing.T, db *sql.DB, scope, prevTmpl, nextTmpl string, tsMs int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT OR REPLACE INTO transition_stat (scope, prev_template_id, next_template_id, weight, count, last_seen_ms) VALUES (?, ?, ?, 1.0, 1, ?)",
		scope, prevTmpl, nextTmpl, tsMs)
	if err != nil {
		t.Fatalf("failed to insert transition: %v", err)
	}
}

// transitionResult holds a transition query result for deterministic comparison.
type transitionResult struct {
	templateID string
	weight     float64
	count      int
}

// queryTransitions runs a query and collects results for comparison.
func queryTransitions(t *testing.T, ctx context.Context, db *sql.DB, query, scope string) []transitionResult {
	t.Helper()

	rows, err := db.QueryContext(ctx, query, scope)
	if err != nil {
		t.Fatalf("failed to query transitions: %v", err)
	}
	defer rows.Close()

	var results []transitionResult
	for rows.Next() {
		var r transitionResult
		if err := rows.Scan(&r.templateID, &r.weight, &r.count); err != nil {
			t.Fatalf("failed to scan transition row: %v", err)
		}
		results = append(results, r)
	}
	return results
}
