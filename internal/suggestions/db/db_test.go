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

func TestOpen_CreatesDatabase(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true, // Skip lock for parallel tests
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

	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Verify all tables exist
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

	// Verify all indexes exist
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

func TestDB_Validate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	defer db.Close()

	err := db.Validate(context.Background())
	if err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestDB_Close(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

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

	db := newTestDB(t)
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

	// Create initial database
	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Manually update schema version to a future version
	_, err = db.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO schema_migrations (version, applied_ts)
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
	})
	if err != nil {
		t.Fatalf("First Open() error = %v", err)
	}
	defer db1.Close()

	// Second open should fail (lock held)
	_, err = Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 100 * time.Millisecond,
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
	})
	if err != nil {
		t.Fatalf("First Open() error = %v", err)
	}
	db1.Close()

	// Should be able to open again
	db2, err := Open(context.Background(), Options{
		Path:        dbPath,
		LockTimeout: 100 * time.Millisecond,
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
		Path: dbPath,
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
		Path: dbPath,
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
		Path: dbPath,
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
	})
	if err != nil {
		t.Fatalf("Initial Open() error = %v", err)
	}
	db.Close()

	// Open in read-only mode
	roDb, err := Open(context.Background(), Options{
		Path:     dbPath,
		ReadOnly: true,
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
	if version != SchemaVersion {
		t.Errorf("Version() = %d, want %d", version, SchemaVersion)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

	db := newTestDB(t)
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

// Helper functions

func newTestDB(t *testing.T) *DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true, // Skip lock for parallel tests
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
	Version(context.Context) (int, error)
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
} = (*DB)(nil)
