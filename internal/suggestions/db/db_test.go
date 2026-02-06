package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// V1 Database Tests (backward compatibility)
// =============================================================================

func TestOpen_CreatesDatabase(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true, // Skip lock for parallel tests
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestOpen_CreatesDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "nested", "test.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Verify directory was created
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Database directory was not created")
	}
}

func TestOpen_RunsMigrations(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Verify all V1 tables exist
	for _, table := range AllTables {
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE type='table' AND name=?
		`, table).Scan(&name)

		if err != nil {
			t.Errorf("Table %s does not exist: %v", table, err)
		}
	}

	// Verify all V1 indexes exist
	for _, index := range AllIndexes {
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE type='index' AND name=?
		`, index).Scan(&name)

		if err != nil {
			t.Errorf("Index %s does not exist: %v", index, err)
		}
	}
}

func TestOpen_WALModeEnabled(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	var journalMode string
	err := db.QueryRowContext(context.Background(),
		"PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to check journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Journal mode = %s, want wal", journalMode)
	}
}

func TestOpen_ForeignKeysEnabled(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	var foreignKeys int
	err := db.QueryRowContext(context.Background(),
		"PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("Failed to check foreign_keys: %v", err)
	}

	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestDB_SchemaVersion(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()
	version, err := db.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	if version != V1SchemaVersion {
		t.Errorf("Version() = %d, want %d", version, V1SchemaVersion)
	}
}

func TestDB_Validate(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	err := db.Validate(context.Background())
	if err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestDB_Close(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)

	// Close should not error
	if err := db.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Second close should be safe
	if err := db.Close(); err != nil {
		t.Errorf("Second Close() error = %v", err)
	}
}

func TestDB_PrepareStatement(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Prepare a statement
	stmt1, err := db.PrepareStatement(ctx, "test_query", "SELECT 1")
	if err != nil {
		t.Fatalf("PrepareStatement() error = %v", err)
	}

	// Get the same statement again (should be cached)
	stmt2, err := db.PrepareStatement(ctx, "test_query", "SELECT 1")
	if err != nil {
		t.Fatalf("PrepareStatement() second call error = %v", err)
	}

	if stmt1 != stmt2 {
		t.Error("PrepareStatement() did not return cached statement")
	}
}

func TestMigrations_RefuseNewerVersion(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create initial V1 database
	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Manually update schema version to a future version
	_, err = db.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO schema_migrations (version, applied_ts)
		VALUES (?, ?)
	`, V1SchemaVersion+10, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("Failed to insert future version: %v", err)
	}
	db.Close()

	// Try to open again - should fail
	_, err = Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
		UseV1:    true,
	})
	if err == nil {
		t.Fatal("Open() should fail with newer schema version")
	}

	// Verify it's the right error
	if !contains(err.Error(), "newer than supported") {
		t.Errorf("Expected 'newer than supported' error, got: %v", err)
	}
}

func TestMigrations_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open and close multiple times
	for i := 0; i < 3; i++ {
		db, err := Open(context.Background(), Options{
			Path:     dbPath,
			SkipLock: true,
			UseV1:    true,
		})
		if err != nil {
			t.Fatalf("Open() iteration %d error = %v", i, err)
		}

		// Verify tables exist
		if err := db.Validate(context.Background()); err != nil {
			t.Errorf("Validate() iteration %d error = %v", i, err)
		}

		db.Close()
	}
}

func TestLock_PreventsMultipleOpens(t *testing.T) {
	// Don't run in parallel since we're testing locking behavior
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// First open should succeed
	db1, err := Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 100 * time.Millisecond,
		UseV1:       true,
	})
	if err != nil {
		t.Fatalf("First Open() error = %v", err)
	}
	defer db1.Close()

	// Second open should fail (lock held)
	_, err = Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 100 * time.Millisecond,
		UseV1:       true,
	})
	if err == nil {
		t.Fatal("Second Open() should fail due to lock")
	}
}

func TestLock_ReleasedOnClose(t *testing.T) {
	// Don't run in parallel
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open and close
	db1, err := Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 100 * time.Millisecond,
		UseV1:       true,
	})
	if err != nil {
		t.Fatalf("First Open() error = %v", err)
	}
	db1.Close()

	// Should be able to open again
	db2, err := Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 100 * time.Millisecond,
		UseV1:       true,
	})
	if err != nil {
		t.Fatalf("Second Open() after close error = %v", err)
	}
	db2.Close()
}

func TestLock_ConcurrentStartup(t *testing.T) {
	// Don't run in parallel
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	const numGoroutines = 10
	var wg sync.WaitGroup
	successCount := 0
	var successMu sync.Mutex
	var successDB *DB

	// Try to open from multiple goroutines simultaneously
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(context.Background(), Options{
				Path:        dbPath,
				LockTimeout: 100 * time.Millisecond,
				UseV1:       true,
			})
			if err == nil {
				successMu.Lock()
				successCount++
				successDB = db
				successMu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Exactly one should succeed
	if successCount != 1 {
		t.Errorf("Expected exactly 1 success, got %d", successCount)
	}

	if successDB != nil {
		successDB.Close()
	}
}

func TestLock_TimeoutBehavior(t *testing.T) {
	// Don't run in parallel
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// First open
	db1, err := Open(context.Background(), Options{
		Path:  dbPath,
		UseV1: true,
	})
	if err != nil {
		t.Fatalf("First Open() error = %v", err)
	}
	defer db1.Close()

	// Try to open with short timeout
	start := time.Now()
	_, err = Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 200 * time.Millisecond,
		UseV1:       true,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Second Open() should fail")
	}

	// Verify we actually waited for the timeout
	if elapsed < 150*time.Millisecond {
		t.Errorf("Lock timeout too fast: %v", elapsed)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("Lock timeout too slow: %v", elapsed)
	}
}

func TestIsLocked(t *testing.T) {
	// Don't run in parallel
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	dbDir := filepath.Dir(dbPath)

	// Initially not locked
	if IsLocked(dbDir) {
		t.Error("IsLocked() = true before any open")
	}

	// Open and check
	db, err := Open(context.Background(), Options{
		Path:  dbPath,
		UseV1: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if !IsLocked(dbDir) {
		t.Error("IsLocked() = false while db is open")
	}

	// Close and check
	db.Close()

	// Note: On some systems the lock file may still exist but be unlocked
	// This test is less reliable so we just check it doesn't panic
	_ = IsLocked(dbDir)
}

func TestGetLockHolderPID(t *testing.T) {
	// Don't run in parallel
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	dbDir := filepath.Dir(dbPath)

	// No lock file yet
	pid := GetLockHolderPID(dbDir)
	if pid != 0 {
		t.Errorf("GetLockHolderPID() = %d before any open, want 0", pid)
	}

	// Open
	db, err := Open(context.Background(), Options{
		Path:  dbPath,
		UseV1: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Should return our PID
	pid = GetLockHolderPID(dbDir)
	if pid != os.Getpid() {
		t.Errorf("GetLockHolderPID() = %d, want %d", pid, os.Getpid())
	}
}

func TestOpen_ReadOnly(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// First create the database with write access
	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("Initial Open() error = %v", err)
	}
	db.Close()

	// Open in read-only mode
	roDb, err := Open(context.Background(), Options{
		Path:     dbPath,
		ReadOnly: true,
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("ReadOnly Open() error = %v", err)
	}
	defer roDb.Close()

	// Should be able to read
	version, err := roDb.Version(context.Background())
	if err != nil {
		t.Errorf("Version() error = %v", err)
	}
	if version != V1SchemaVersion {
		t.Errorf("Version() = %d, want %d", version, V1SchemaVersion)
	}

	// Write should fail
	_, err = roDb.ExecContext(context.Background(), `
		INSERT INTO session (id, created_at, shell) VALUES ('test', 1000, 'zsh')
	`)
	if err == nil {
		t.Error("Write to read-only database should fail")
	}
}

func TestDB_ConcurrentReads(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Insert some test data
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, created_at, shell) VALUES ('test-session', 1000, 'zsh')
	`)
	if err != nil {
		t.Fatalf("Insert error = %v", err)
	}

	// Run concurrent reads
	const numReaders = 20
	var wg sync.WaitGroup
	errCh := make(chan error, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var id string
			err := db.QueryRowContext(ctx, `
				SELECT id FROM session WHERE id = ?
			`, "test-session").Scan(&id)
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent read error: %v", err)
	}
}

func TestSchemaValidity_SessionTable(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Test inserting a valid session
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, created_at, shell, host, user)
		VALUES ('sess-1', 1000, 'zsh', 'localhost', 'testuser')
	`)
	if err != nil {
		t.Errorf("Insert session error = %v", err)
	}

	// Test querying
	var id string
	var shell string
	err = db.QueryRowContext(ctx, `
		SELECT id, shell FROM session WHERE id = ?
	`, "sess-1").Scan(&id, &shell)
	if err != nil {
		t.Errorf("Query session error = %v", err)
	}
	if id != "sess-1" || shell != "zsh" {
		t.Errorf("Got id=%s, shell=%s, want sess-1, zsh", id, shell)
	}
}

func TestSchemaValidity_CommandEventTable(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Create session first (foreign key)
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, created_at, shell) VALUES ('sess-1', 1000, 'zsh')
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}

	// Insert command event
	_, err = db.ExecContext(ctx, `
		INSERT INTO command_event (session_id, ts, duration_ms, exit_code, cwd, repo_key, branch, cmd_raw, cmd_norm)
		VALUES ('sess-1', 2000, 100, 0, '/home/user', 'abc123', 'main', 'git status', 'git status')
	`)
	if err != nil {
		t.Errorf("Insert command_event error = %v", err)
	}

	// Test index usage (query by ts)
	rows, err := db.QueryContext(ctx, `
		SELECT id FROM command_event WHERE ts >= 1000 ORDER BY ts
	`)
	if err != nil {
		t.Errorf("Query by ts error = %v", err)
	}
	rows.Close()

	// Test index usage (query by repo_key, ts)
	rows, err = db.QueryContext(ctx, `
		SELECT id FROM command_event WHERE repo_key = 'abc123' ORDER BY ts
	`)
	if err != nil {
		t.Errorf("Query by repo_key error = %v", err)
	}
	rows.Close()
}

func TestSchemaValidity_TransitionTable(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Insert transition
	_, err := db.ExecContext(ctx, `
		INSERT INTO transition (scope, prev_norm, next_norm, count, last_ts)
		VALUES ('global', 'git add', 'git commit', 5, 1000)
	`)
	if err != nil {
		t.Errorf("Insert transition error = %v", err)
	}

	// Test upsert (primary key conflict)
	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO transition (scope, prev_norm, next_norm, count, last_ts)
		VALUES ('global', 'git add', 'git commit', 10, 2000)
	`)
	if err != nil {
		t.Errorf("Upsert transition error = %v", err)
	}

	// Verify count was updated
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT count FROM transition WHERE scope='global' AND prev_norm='git add' AND next_norm='git commit'
	`).Scan(&count)
	if err != nil {
		t.Errorf("Query transition error = %v", err)
	}
	if count != 10 {
		t.Errorf("count = %d, want 10", count)
	}
}

func TestSchemaValidity_CommandScoreTable(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Insert scores
	_, err := db.ExecContext(ctx, `
		INSERT INTO command_score (scope, cmd_norm, score, last_ts) VALUES
		('global', 'git status', 5.5, 1000),
		('global', 'git add', 3.2, 1000),
		('repo-123', 'make test', 10.0, 1000)
	`)
	if err != nil {
		t.Errorf("Insert command_score error = %v", err)
	}

	// Test index usage (query by scope, score desc)
	rows, err := db.QueryContext(ctx, `
		SELECT cmd_norm, score FROM command_score WHERE scope = 'global' ORDER BY score DESC LIMIT 10
	`)
	if err != nil {
		t.Errorf("Query by scope/score error = %v", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var cmdNorm string
		var score float64
		if err := rows.Scan(&cmdNorm, &score); err != nil {
			t.Errorf("Scan error = %v", err)
		}
		results = append(results, cmdNorm)
	}

	if len(results) != 2 {
		t.Errorf("Got %d results, want 2", len(results))
	}
	if results[0] != "git status" {
		t.Errorf("First result = %s, want git status", results[0])
	}
}

func TestSchemaValidity_SlotValueTable(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Insert slot values
	_, err := db.ExecContext(ctx, `
		INSERT INTO slot_value (scope, cmd_norm, slot_idx, value, count, last_ts) VALUES
		('global', 'kubectl get pods -n <arg>', 0, 'default', 10.0, 1000),
		('global', 'kubectl get pods -n <arg>', 0, 'kube-system', 5.0, 1000),
		('repo-123', 'kubectl get pods -n <arg>', 0, 'production', 15.0, 1000)
	`)
	if err != nil {
		t.Errorf("Insert slot_value error = %v", err)
	}

	// Test index usage (query for top values)
	rows, err := db.QueryContext(ctx, `
		SELECT value, count FROM slot_value
		WHERE scope = 'global' AND cmd_norm = 'kubectl get pods -n <arg>' AND slot_idx = 0
		ORDER BY count DESC
		LIMIT 10
	`)
	if err != nil {
		t.Errorf("Query slot_value error = %v", err)
	}
	defer rows.Close()

	var topValue string
	var topCount float64
	if rows.Next() {
		if err := rows.Scan(&topValue, &topCount); err != nil {
			t.Errorf("Scan error = %v", err)
		}
	}

	if topValue != "default" {
		t.Errorf("Top value = %s, want default", topValue)
	}
}

func TestSchemaValidity_ProjectTaskTable(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Insert project tasks
	_, err := db.ExecContext(ctx, `
		INSERT INTO project_task (repo_key, kind, name, command, description, discovered_ts) VALUES
		('repo-123', 'npm', 'test', 'npm test', 'Run tests', 1000),
		('repo-123', 'npm', 'build', 'npm run build', 'Build project', 1000),
		('repo-123', 'make', 'lint', 'make lint', NULL, 1000)
	`)
	if err != nil {
		t.Errorf("Insert project_task error = %v", err)
	}

	// Query tasks for repo
	rows, err := db.QueryContext(ctx, `
		SELECT name, command FROM project_task WHERE repo_key = 'repo-123'
	`)
	if err != nil {
		t.Errorf("Query project_task error = %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var name, command string
		if err := rows.Scan(&name, &command); err != nil {
			t.Errorf("Scan error = %v", err)
		}
	}

	if count != 3 {
		t.Errorf("Got %d tasks, want 3", count)
	}
}

func TestSchemaValidity_ForeignKeyEnforced(t *testing.T) {
	t.Parallel()

	db := newTestV1DB(t)
	defer db.Close()

	ctx := context.Background()

	// Try to insert command_event without session (should fail)
	_, err := db.ExecContext(ctx, `
		INSERT INTO command_event (session_id, ts, cwd, cmd_raw, cmd_norm)
		VALUES ('nonexistent-session', 1000, '/tmp', 'ls', 'ls')
	`)

	if err == nil {
		t.Error("Insert with invalid session_id should fail due to foreign key constraint")
	}
}

// =============================================================================
// V2 Database Tests
// =============================================================================

func TestV2Open_CreatesDatabase(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("V2 database file was not created")
	}
}

func TestV2Open_SchemaVersion(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()

	ctx := context.Background()
	version, err := db.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	if version != SchemaVersion {
		t.Errorf("Version() = %d, want %d", version, SchemaVersion)
	}
}

func TestV2Open_ValidateAll23Tables(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()

	ctx := context.Background()

	err := db.ValidateV2(ctx)
	if err != nil {
		t.Fatalf("ValidateV2() error = %v", err)
	}

	// Also verify the exact count of tables (23)
	if len(V2AllTables) != 23 {
		t.Errorf("V2AllTables has %d entries, want 23", len(V2AllTables))
	}
}

func TestV2Open_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Open and close multiple times
	for i := 0; i < 3; i++ {
		db, err := Open(context.Background(), Options{
			Path:     dbPath,
			SkipLock: true,
		})
		if err != nil {
			t.Fatalf("Open() iteration %d error = %v", i, err)
		}

		if err := db.ValidateV2(context.Background()); err != nil {
			t.Errorf("ValidateV2() iteration %d error = %v", i, err)
		}

		db.Close()
	}
}

func TestV2Open_RefuseNewerVersion(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Create initial V2 database
	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Manually update schema version to a future version
	_, err = db.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO schema_migrations (version, applied_ms)
		VALUES (?, ?)
	`, SchemaVersion+10, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("Failed to insert future version: %v", err)
	}
	db.Close()

	// Try to open again - should fail
	_, err = Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err == nil {
		t.Fatal("Open() should fail with newer schema version")
	}

	if !contains(err.Error(), "newer than supported") {
		t.Errorf("Expected 'newer than supported' error, got: %v", err)
	}
}

func TestV2_SessionTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert a V2 session with new column names
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms, project_types, host, user_name)
		VALUES ('sess-v2', 'zsh', 1000, 'go|docker', 'localhost', 'testuser')
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}

	var id, shell, projectTypes string
	err = db.QueryRowContext(ctx, `
		SELECT id, shell, project_types FROM session WHERE id = 'sess-v2'
	`).Scan(&id, &shell, &projectTypes)
	if err != nil {
		t.Fatalf("Query session error = %v", err)
	}
	if id != "sess-v2" || shell != "zsh" || projectTypes != "go|docker" {
		t.Errorf("Got id=%s, shell=%s, project_types=%s", id, shell, projectTypes)
	}
}

func TestV2_CommandEventTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert session first
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('sess-v2', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}

	// Insert command event with V2 columns
	_, err = db.ExecContext(ctx, `
		INSERT INTO command_event (session_id, ts_ms, cwd, repo_key, branch, cmd_raw, cmd_norm, cmd_truncated, template_id, exit_code, duration_ms)
		VALUES ('sess-v2', 2000, '/home/user', 'abc123', 'main', 'git status', 'git status', 0, 'tpl-git-status', 0, 100)
	`)
	if err != nil {
		t.Fatalf("Insert command_event error = %v", err)
	}

	var templateID string
	err = db.QueryRowContext(ctx, `
		SELECT template_id FROM command_event WHERE session_id = 'sess-v2'
	`).Scan(&templateID)
	if err != nil {
		t.Fatalf("Query command_event error = %v", err)
	}
	if templateID != "tpl-git-status" {
		t.Errorf("template_id = %s, want tpl-git-status", templateID)
	}
}

func TestV2_CommandTemplateTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES ('tpl-git-status', 'git status', 'git|vcs|status', 0, 1000, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert command_template error = %v", err)
	}

	var tags string
	var slotCount int
	err = db.QueryRowContext(ctx, `
		SELECT tags, slot_count FROM command_template WHERE template_id = 'tpl-git-status'
	`).Scan(&tags, &slotCount)
	if err != nil {
		t.Fatalf("Query command_template error = %v", err)
	}
	if tags != "git|vcs|status" || slotCount != 0 {
		t.Errorf("Got tags=%s, slot_count=%d", tags, slotCount)
	}
}

func TestV2_TransitionStatTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO transition_stat (scope, prev_template_id, next_template_id, weight, count, last_seen_ms)
		VALUES ('global', 'tpl-git-add', 'tpl-git-commit', 0.8, 15, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert transition_stat error = %v", err)
	}

	var weight float64
	err = db.QueryRowContext(ctx, `
		SELECT weight FROM transition_stat
		WHERE scope = 'global' AND prev_template_id = 'tpl-git-add'
	`).Scan(&weight)
	if err != nil {
		t.Fatalf("Query transition_stat error = %v", err)
	}
	if weight != 0.8 {
		t.Errorf("weight = %f, want 0.8", weight)
	}
}

func TestV2_CommandStatTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO command_stat (scope, template_id, score, success_count, failure_count, last_seen_ms)
		VALUES ('global', 'tpl-git-status', 5.5, 100, 2, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert command_stat error = %v", err)
	}

	var successCount, failureCount int
	err = db.QueryRowContext(ctx, `
		SELECT success_count, failure_count FROM command_stat
		WHERE scope = 'global' AND template_id = 'tpl-git-status'
	`).Scan(&successCount, &failureCount)
	if err != nil {
		t.Fatalf("Query command_stat error = %v", err)
	}
	if successCount != 100 || failureCount != 2 {
		t.Errorf("Got success=%d, failure=%d", successCount, failureCount)
	}
}

func TestV2_SlotStatTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO slot_stat (scope, template_id, slot_index, value, weight, count, last_seen_ms)
		VALUES ('global', 'tpl-kubectl-get', 0, 'default', 0.9, 10, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert slot_stat error = %v", err)
	}

	var weight float64
	err = db.QueryRowContext(ctx, `
		SELECT weight FROM slot_stat
		WHERE scope = 'global' AND template_id = 'tpl-kubectl-get' AND slot_index = 0 AND value = 'default'
	`).Scan(&weight)
	if err != nil {
		t.Fatalf("Query slot_stat error = %v", err)
	}
	if weight != 0.9 {
		t.Errorf("weight = %f, want 0.9", weight)
	}
}

func TestV2_SlotCorrelationTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO slot_correlation (scope, template_id, slot_key, tuple_hash, tuple_value_json, weight, count, last_seen_ms)
		VALUES ('global', 'tpl-docker-run', '0:1', 'abc123', '["nginx","8080"]', 0.7, 5, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert slot_correlation error = %v", err)
	}
}

func TestV2_ProjectTypeStatTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO project_type_stat (project_type, template_id, score, count, last_seen_ms)
		VALUES ('go', 'tpl-go-test', 8.5, 50, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert project_type_stat error = %v", err)
	}
}

func TestV2_ProjectTypeTransitionTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO project_type_transition (project_type, prev_template_id, next_template_id, weight, count, last_seen_ms)
		VALUES ('go', 'tpl-go-build', 'tpl-go-test', 0.7, 30, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert project_type_transition error = %v", err)
	}
}

func TestV2_PipelineEventTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert session and command event first
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('sess-v2', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO command_event (id, session_id, ts_ms, cwd, cmd_raw, cmd_norm)
		VALUES (1, 'sess-v2', 2000, '/tmp', 'cat file | grep pattern', 'cat {} | grep {}')
	`)
	if err != nil {
		t.Fatalf("Insert command_event error = %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO pipeline_event (command_event_id, position, operator, cmd_raw, cmd_norm, template_id)
		VALUES (1, 0, NULL, 'cat file', 'cat {}', 'tpl-cat'),
		       (1, 1, '|', 'grep pattern', 'grep {}', 'tpl-grep')
	`)
	if err != nil {
		t.Fatalf("Insert pipeline_event error = %v", err)
	}

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipeline_event WHERE command_event_id = 1`).Scan(&count)
	if err != nil {
		t.Fatalf("Query pipeline_event error = %v", err)
	}
	if count != 2 {
		t.Errorf("Got %d pipeline events, want 2", count)
	}
}

func TestV2_PipelineTransitionTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
		VALUES ('global', 'tpl-cat', 'tpl-grep', '|', 0.9, 20, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert pipeline_transition error = %v", err)
	}
}

func TestV2_PipelinePatternTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO pipeline_pattern (pattern_hash, template_chain, operator_chain, scope, count, last_seen_ms, cmd_norm_display)
		VALUES ('hash123', 'tpl-cat|tpl-grep', '|', 'global', 15, 2000, 'cat {} | grep {}')
	`)
	if err != nil {
		t.Fatalf("Insert pipeline_pattern error = %v", err)
	}
}

func TestV2_FailureRecoveryTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES ('global', 'tpl-make-build', 'code:2', 'tpl-make-clean', 0.8, 10, 0.75, 2000, 'learned')
	`)
	if err != nil {
		t.Fatalf("Insert failure_recovery error = %v", err)
	}
}

func TestV2_WorkflowPatternTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO workflow_pattern (pattern_id, template_chain, display_chain, scope, step_count, occurrence_count, last_seen_ms, avg_duration_ms)
		VALUES ('wf-1', 'tpl-git-add|tpl-git-commit|tpl-git-push', 'git add|git commit|git push', 'global', 3, 25, 2000, 5000)
	`)
	if err != nil {
		t.Fatalf("Insert workflow_pattern error = %v", err)
	}
}

func TestV2_WorkflowStepTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert workflow pattern first
	_, err := db.ExecContext(ctx, `
		INSERT INTO workflow_pattern (pattern_id, template_chain, display_chain, scope, step_count, occurrence_count, last_seen_ms)
		VALUES ('wf-1', 'tpl-a|tpl-b|tpl-c', 'a|b|c', 'global', 3, 10, 2000)
	`)
	if err != nil {
		t.Fatalf("Insert workflow_pattern error = %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO workflow_step (pattern_id, step_index, template_id) VALUES
		('wf-1', 0, 'tpl-a'),
		('wf-1', 1, 'tpl-b'),
		('wf-1', 2, 'tpl-c')
	`)
	if err != nil {
		t.Fatalf("Insert workflow_step error = %v", err)
	}

	// Verify index on template_id works
	var patternID string
	err = db.QueryRowContext(ctx, `
		SELECT pattern_id FROM workflow_step WHERE template_id = 'tpl-b'
	`).Scan(&patternID)
	if err != nil {
		t.Fatalf("Query workflow_step error = %v", err)
	}
	if patternID != "wf-1" {
		t.Errorf("pattern_id = %s, want wf-1", patternID)
	}
}

func TestV2_TaskCandidateTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO task_candidate (repo_key, kind, name, command_text, description, source, priority_boost, source_checksum, discovered_ms)
		VALUES ('repo-123', 'npm', 'test', 'npm test', 'Run tests', 'auto', 0.5, 'sha256:abc', 2000)
	`)
	if err != nil {
		t.Fatalf("Insert task_candidate error = %v", err)
	}

	var source string
	var boost float64
	err = db.QueryRowContext(ctx, `
		SELECT source, priority_boost FROM task_candidate WHERE repo_key = 'repo-123' AND name = 'test'
	`).Scan(&source, &boost)
	if err != nil {
		t.Fatalf("Query task_candidate error = %v", err)
	}
	if source != "auto" || boost != 0.5 {
		t.Errorf("Got source=%s, boost=%f", source, boost)
	}
}

func TestV2_SuggestionCacheTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO suggestion_cache (cache_key, session_id, context_hash, suggestions_json, created_ms, ttl_ms)
		VALUES ('key-1', 'sess-1', 'ctx-hash', '[]', 2000, 5000)
	`)
	if err != nil {
		t.Fatalf("Insert suggestion_cache error = %v", err)
	}

	var hitCount int
	err = db.QueryRowContext(ctx, `
		SELECT hit_count FROM suggestion_cache WHERE cache_key = 'key-1'
	`).Scan(&hitCount)
	if err != nil {
		t.Fatalf("Query suggestion_cache error = %v", err)
	}
	if hitCount != 0 {
		t.Errorf("hit_count = %d, want 0", hitCount)
	}
}

func TestV2_SuggestionFeedbackTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO suggestion_feedback (session_id, ts_ms, prompt_prefix, suggested_text, action, executed_text, latency_ms)
		VALUES ('sess-1', 2000, 'git ', 'git status', 'accepted', 'git status', 50)
	`)
	if err != nil {
		t.Fatalf("Insert suggestion_feedback error = %v", err)
	}
}

func TestV2_SessionAliasTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO session_alias (session_id, alias_key, expansion) VALUES
		('sess-1', 'gs', 'git status'),
		('sess-1', 'ga', 'git add')
	`)
	if err != nil {
		t.Fatalf("Insert session_alias error = %v", err)
	}

	var expansion string
	err = db.QueryRowContext(ctx, `
		SELECT expansion FROM session_alias WHERE session_id = 'sess-1' AND alias_key = 'gs'
	`).Scan(&expansion)
	if err != nil {
		t.Fatalf("Query session_alias error = %v", err)
	}
	if expansion != "git status" {
		t.Errorf("expansion = %s, want 'git status'", expansion)
	}
}

func TestV2_DismissalPatternTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO dismissal_pattern (scope, context_template_id, dismissed_template_id, dismissal_count, last_dismissed_ms, suppression_level)
		VALUES ('global', 'tpl-git-add', 'tpl-rm-rf', 5, 2000, 'LEARNED')
	`)
	if err != nil {
		t.Fatalf("Insert dismissal_pattern error = %v", err)
	}
}

func TestV2_RankWeightProfileTable(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO rank_weight_profile (
			profile_key, scope, updated_ms,
			w_transition, w_frequency, w_success, w_prefix, w_affinity, w_task, w_feedback,
			w_project_type_affinity, w_failure_recovery, w_risk_penalty,
			sample_count, learning_rate
		) VALUES (
			'default', 'global', 2000,
			0.25, 0.20, 0.15, 0.10, 0.10, 0.05, 0.05,
			0.08, 0.12, 0.10,
			1000, 0.01
		)
	`)
	if err != nil {
		t.Fatalf("Insert rank_weight_profile error = %v", err)
	}
}

func TestV2_FTS5_InsertTrigger(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert session
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('sess-fts', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}

	// Insert non-ephemeral command event (should trigger FTS insert)
	_, err = db.ExecContext(ctx, `
		INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, ephemeral)
		VALUES ('sess-fts', 2000, '/tmp', 'git status --porcelain', 'git status --porcelain', 0)
	`)
	if err != nil {
		t.Fatalf("Insert command_event error = %v", err)
	}

	// Search via FTS
	var cmdRaw string
	err = db.QueryRowContext(ctx, `
		SELECT cmd_raw FROM command_event_fts WHERE command_event_fts MATCH 'porcelain'
	`).Scan(&cmdRaw)
	if err != nil {
		t.Fatalf("FTS search error = %v", err)
	}
	if cmdRaw != "git status --porcelain" {
		t.Errorf("FTS result = %s, want 'git status --porcelain'", cmdRaw)
	}
}

func TestV2_FTS5_EphemeralNotIndexed(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert session
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('sess-eph', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}

	// Insert ephemeral command event (should NOT trigger FTS insert)
	_, err = db.ExecContext(ctx, `
		INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, ephemeral)
		VALUES ('sess-eph', 2000, '/tmp', 'secret-command --key=abc', 'secret-command --key=abc', 1)
	`)
	if err != nil {
		t.Fatalf("Insert ephemeral command_event error = %v", err)
	}

	// FTS search should find nothing
	var cmdRaw string
	err = db.QueryRowContext(ctx, `
		SELECT cmd_raw FROM command_event_fts WHERE command_event_fts MATCH 'secret'
	`).Scan(&cmdRaw)
	if err == nil {
		t.Errorf("FTS should not contain ephemeral commands, found: %s", cmdRaw)
	}
}

func TestV2_FTS5_DeleteTrigger(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert session
	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('sess-del', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert session error = %v", err)
	}

	// Insert and then delete command event
	_, err = db.ExecContext(ctx, `
		INSERT INTO command_event (id, session_id, ts_ms, cwd, cmd_raw, cmd_norm, ephemeral)
		VALUES (999, 'sess-del', 2000, '/tmp', 'unique-deletable-command', 'unique-deletable-command', 0)
	`)
	if err != nil {
		t.Fatalf("Insert command_event error = %v", err)
	}

	// Verify it's in FTS
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM command_event_fts WHERE command_event_fts MATCH 'deletable'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("FTS count error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 FTS result before delete, got %d", count)
	}

	// Delete the command event
	_, err = db.ExecContext(ctx, `DELETE FROM command_event WHERE id = 999`)
	if err != nil {
		t.Fatalf("Delete command_event error = %v", err)
	}

	// FTS should no longer find it
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM command_event_fts WHERE command_event_fts MATCH 'deletable'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("FTS count after delete error = %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 FTS results after delete, got %d", count)
	}
}

func TestV2_SeparateFromV1(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v1Path := filepath.Join(tmpDir, "suggestions.db")
	v2Path := filepath.Join(tmpDir, "suggestions_v2.db")

	// Create V1 database
	v1DB, err := Open(context.Background(), Options{
		Path:     v1Path,
		SkipLock: true,
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("V1 Open() error = %v", err)
	}
	defer v1DB.Close()

	// Create V2 database
	v2DB, err := Open(context.Background(), Options{
		Path:     v2Path,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("V2 Open() error = %v", err)
	}
	defer v2DB.Close()

	// Both should exist as separate files
	if _, err := os.Stat(v1Path); os.IsNotExist(err) {
		t.Error("V1 database file does not exist")
	}
	if _, err := os.Stat(v2Path); os.IsNotExist(err) {
		t.Error("V2 database file does not exist")
	}

	// V1 should have V1 tables
	if err := v1DB.Validate(context.Background()); err != nil {
		t.Errorf("V1 Validate() error = %v", err)
	}

	// V2 should have V2 tables
	if err := v2DB.ValidateV2(context.Background()); err != nil {
		t.Errorf("V2 ValidateV2() error = %v", err)
	}

	// V1 should NOT have V2-only tables
	var name string
	err = v1DB.QueryRowContext(context.Background(), `
		SELECT name FROM sqlite_master WHERE type='table' AND name='command_template'
	`).Scan(&name)
	if err == nil {
		t.Error("V1 database should not have command_template table")
	}

	// V2 should NOT have V1-only tables
	err = v2DB.QueryRowContext(context.Background(), `
		SELECT name FROM sqlite_master WHERE type='table' AND name='transition'
	`).Scan(&name)
	if err == nil {
		t.Error("V2 database should not have V1 transition table")
	}
}

func TestV2_SchemaCountIs23(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()
	ctx := context.Background()

	// Count all user tables (excluding sqlite internal tables)
	rows, err := db.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		t.Fatalf("Query tables error = %v", err)
	}
	defer rows.Close()

	var tableCount int
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan error = %v", err)
		}
		tableCount++
	}
	t.Logf("Found %d user tables in database", tableCount)

	// command_event_fts creates multiple internal tables (command_event_fts,
	// command_event_fts_config, command_event_fts_content, etc.)
	// We count the logical tables from V2AllTables.
	// Check that each V2AllTables entry exists in the DB.
	for _, expected := range V2AllTables {
		found := false
		// Check tables
		var name string
		err := db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master
			WHERE name=? AND (type='table' OR type='view')
		`, expected).Scan(&name)
		if err == nil {
			found = true
		}
		if !found {
			t.Errorf("Expected table %q not found in database", expected)
		}
	}

	// Verify V2AllTables has exactly 23 entries
	if len(V2AllTables) != 23 {
		t.Errorf("V2AllTables has %d entries, want 23", len(V2AllTables))
	}
}

func TestV2_WALModeEnabled(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()

	var journalMode string
	err := db.QueryRowContext(context.Background(),
		"PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to check journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Journal mode = %s, want wal", journalMode)
	}
}

func TestV2_DefaultDBPath(t *testing.T) {
	t.Parallel()

	path, err := DefaultDBPath()
	if err != nil {
		t.Fatalf("DefaultDBPath() error = %v", err)
	}

	if path == "" {
		t.Error("DefaultDBPath() returned empty string")
	}

	if !filepath.IsAbs(path) {
		t.Errorf("DefaultDBPath() = %s is not absolute", path)
	}

	if filepath.Base(path) != "suggestions_v2.db" {
		t.Errorf("DefaultDBPath() = %s does not end with suggestions_v2.db", path)
	}
}

func TestV1_DefaultDBPath(t *testing.T) {
	t.Parallel()

	path, err := DefaultV1DBPath()
	if err != nil {
		t.Fatalf("DefaultV1DBPath() error = %v", err)
	}

	if path == "" {
		t.Error("DefaultV1DBPath() returned empty string")
	}

	if filepath.Base(path) != "suggestions.db" {
		t.Errorf("DefaultV1DBPath() = %s does not end with suggestions.db", path)
	}
}

// =============================================================================
// Helper functions
// =============================================================================

// newTestV1DB creates a V1 test database.
func newTestV1DB(t *testing.T) *DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
		UseV1:    true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return db
}

// newTestV2DB creates a V2 test database.
func newTestV2DB(t *testing.T) *DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return db
}

// contains is a simple string contains check.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Compile-time check that DB has expected methods
var _ interface {
	DB() *sql.DB
	Close() error
	Validate(context.Context) error
	ValidateV2(context.Context) error
	Version(context.Context) (int, error)
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
} = (*DB)(nil)
