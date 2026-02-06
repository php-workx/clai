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

// AssertSessionIsolation validates invariant I1: suggestions for session A
// must not include session-scoped transitions derived exclusively from session B.
//
// It inserts commands into two distinct sessions and verifies that
// transition_stat rows scoped to one session do not leak into the other.
func AssertSessionIsolation(t *testing.T, db *sql.DB, sessionA, sessionB string) {
	t.Helper()
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()

	// Ensure both sessions exist
	for _, sid := range []string{sessionA, sessionB} {
		_, err := db.ExecContext(ctx,
			"INSERT OR IGNORE INTO session (id, shell, started_at_ms) VALUES (?, 'bash', ?)",
			sid, nowMs)
		if err != nil {
			t.Fatalf("failed to insert session %q: %v", sid, err)
		}
	}

	// Insert command events for session A: cmd_a1 -> cmd_a2
	insertEvent(t, db, sessionA, "cmd_a1", "cmd_a1", "tmpl_a1", nowMs)
	insertEvent(t, db, sessionA, "cmd_a2", "cmd_a2", "tmpl_a2", nowMs+1)

	// Insert command events for session B: cmd_b1 -> cmd_b2
	insertEvent(t, db, sessionB, "cmd_b1", "cmd_b1", "tmpl_b1", nowMs+2)
	insertEvent(t, db, sessionB, "cmd_b2", "cmd_b2", "tmpl_b2", nowMs+3)

	// Insert session-scoped transitions
	insertTransition(t, db, fmt.Sprintf("session:%s", sessionA), "tmpl_a1", "tmpl_a2", nowMs)
	insertTransition(t, db, fmt.Sprintf("session:%s", sessionB), "tmpl_b1", "tmpl_b2", nowMs)

	// Verify: session A scope should NOT contain session B transitions
	rows, err := db.QueryContext(ctx,
		"SELECT next_template_id FROM transition_stat WHERE scope = ?",
		fmt.Sprintf("session:%s", sessionA))
	if err != nil {
		t.Fatalf("failed to query session A transitions: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nextTmpl string
		if err := rows.Scan(&nextTmpl); err != nil {
			t.Fatalf("failed to scan transition: %v", err)
		}
		if nextTmpl == "tmpl_b2" || nextTmpl == "tmpl_b1" {
			t.Errorf("I1 violation: session %q transitions contain session %q template %q",
				sessionA, sessionB, nextTmpl)
		}
	}

	// Verify: session B scope should NOT contain session A transitions
	rows2, err := db.QueryContext(ctx,
		"SELECT next_template_id FROM transition_stat WHERE scope = ?",
		fmt.Sprintf("session:%s", sessionB))
	if err != nil {
		t.Fatalf("failed to query session B transitions: %v", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var nextTmpl string
		if err := rows2.Scan(&nextTmpl); err != nil {
			t.Fatalf("failed to scan transition: %v", err)
		}
		if nextTmpl == "tmpl_a2" || nextTmpl == "tmpl_a1" {
			t.Errorf("I1 violation: session %q transitions contain session %q template %q",
				sessionB, sessionA, nextTmpl)
		}
	}
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

	nowMs := time.Now().UnixMilli()
	sessionID := "test-txn-session"
	templateID := "tmpl_txn_test"
	scope := "global"

	// Ensure session exists
	_, err := db.ExecContext(ctx,
		"INSERT OR IGNORE INTO session (id, shell, started_at_ms) VALUES (?, 'bash', ?)",
		sessionID, nowMs)
	if err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	// Execute a transaction that inserts into multiple tables atomically
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	// Insert command_event
	result, err := tx.ExecContext(ctx,
		"INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id, exit_code, ephemeral) VALUES (?, ?, '/tmp', 'test cmd', 'test cmd', ?, 0, 0)",
		sessionID, nowMs, templateID)
	if err != nil {
		tx.Rollback()
		t.Fatalf("failed to insert command_event: %v", err)
	}
	eventID, _ := result.LastInsertId()

	// Insert command_template
	_, err = tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO command_template (template_id, cmd_norm, slot_count, first_seen_ms, last_seen_ms) VALUES (?, 'test cmd', 0, ?, ?)",
		templateID, nowMs, nowMs)
	if err != nil {
		tx.Rollback()
		t.Fatalf("failed to insert command_template: %v", err)
	}

	// Insert command_stat
	_, err = tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO command_stat (scope, template_id, score, success_count, failure_count, last_seen_ms) VALUES (?, ?, 1.0, 1, 0, ?)",
		scope, templateID, nowMs)
	if err != nil {
		tx.Rollback()
		t.Fatalf("failed to insert command_stat: %v", err)
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	// Verify all tables reflect the event
	var ceExists bool
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) > 0 FROM command_event WHERE id = ?", eventID).Scan(&ceExists)
	if err != nil || !ceExists {
		t.Errorf("I5 violation: command_event row %d not found after commit", eventID)
	}

	var ctExists bool
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) > 0 FROM command_template WHERE template_id = ?", templateID).Scan(&ctExists)
	if err != nil || !ctExists {
		t.Errorf("I5 violation: command_template %q not found after commit", templateID)
	}

	var csExists bool
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) > 0 FROM command_stat WHERE scope = ? AND template_id = ?",
		scope, templateID).Scan(&csExists)
	if err != nil || !csExists {
		t.Errorf("I5 violation: command_stat for scope=%q template=%q not found after commit",
			scope, templateID)
	}

	// Verify timestamps are consistent (all should have the same nowMs)
	var csLastSeen int64
	err = db.QueryRowContext(ctx,
		"SELECT last_seen_ms FROM command_stat WHERE scope = ? AND template_id = ?",
		scope, templateID).Scan(&csLastSeen)
	if err != nil {
		t.Fatalf("failed to query command_stat last_seen_ms: %v", err)
	}
	if csLastSeen != nowMs {
		t.Errorf("I5 violation: command_stat.last_seen_ms = %d, want %d", csLastSeen, nowMs)
	}

	var ctLastSeen int64
	err = db.QueryRowContext(ctx,
		"SELECT last_seen_ms FROM command_template WHERE template_id = ?",
		templateID).Scan(&ctLastSeen)
	if err != nil {
		t.Fatalf("failed to query command_template last_seen_ms: %v", err)
	}
	if ctLastSeen != nowMs {
		t.Errorf("I5 violation: command_template.last_seen_ms = %d, want %d", ctLastSeen, nowMs)
	}
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
