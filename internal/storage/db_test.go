package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSQLiteStore_CreatesDatabase(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestNewSQLiteStore_CreatesDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "nested", "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	// Verify directory was created
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Database directory was not created")
	}
}

func TestSQLiteStore_Migration_CreatesSchema(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	// Verify tables exist by querying them
	tables := []string{"schema_meta", "sessions", "commands", "ai_cache"}
	for _, table := range tables {
		_, err := store.DB().ExecContext(context.Background(),
			"SELECT 1 FROM "+table+" LIMIT 1")
		if err != nil {
			t.Errorf("Table %s does not exist: %v", table, err)
		}
	}
}

func TestSQLiteStore_WALMode_Enabled(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	var journalMode string
	err := store.DB().QueryRowContext(context.Background(),
		"PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to check journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Journal mode = %s, want wal", journalMode)
	}
}

func TestSQLiteStore_ForeignKeys_Enabled(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	var foreignKeys int
	err := store.DB().QueryRowContext(context.Background(),
		"PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("Failed to check foreign_keys: %v", err)
	}

	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestSQLiteStore_Close(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Close should not error
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Second close should be safe (ignore result - behavior varies by driver)
	_ = store.Close()
}

func TestSQLiteStore_ConcurrentWrites_Safe(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a session first
	session := &Session{
		SessionID:       "test-concurrent-session",
		StartedAtUnixMs: 1000,
		Shell:           "zsh",
		OS:              "darwin",
		InitialCWD:      "/tmp",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Run concurrent writes
	const numWriters = 10
	const writesPerWriter = 10

	// Channel capacity matches exactly the number of goroutines (one result per goroutine)
	errCh := make(chan error, numWriters)

	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			for j := 0; j < writesPerWriter; j++ {
				cmd := &Command{
					CommandID:     generateTestID(writerID, j),
					SessionID:     "test-concurrent-session",
					TsStartUnixMs: int64(1000 + writerID*100 + j),
					CWD:           "/tmp",
					Command:       "echo test",
					IsSuccess:     boolPtr(true),
				}
				if err := store.CreateCommand(ctx, cmd); err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}(i)
	}

	// Wait for all writers (exactly numWriters results expected)
	var errs []error
	for i := 0; i < numWriters; i++ {
		if err := <-errCh; err != nil {
			errs = append(errs, err)
		}
	}
	for _, err := range errs {
		t.Errorf("Concurrent write error: %v", err)
	}

	// Verify all commands were written (only if no errors)
	if len(errs) == 0 {
		cmds, err := store.QueryCommands(ctx, CommandQuery{
			SessionID: strPtr("test-concurrent-session"),
			Limit:     numWriters * writesPerWriter,
		})
		if err != nil {
			t.Fatalf("QueryCommands() error = %v", err)
		}

		if len(cmds) != numWriters*writesPerWriter {
			t.Errorf("Got %d commands, want %d", len(cmds), numWriters*writesPerWriter)
		}
	}
}

func TestDefaultDBPath(t *testing.T) {
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

	if !strings.HasSuffix(path, "state.db") {
		t.Errorf("DefaultDBPath() = %s does not end with state.db", path)
	}
}

// Helper functions

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}

	return store
}

func generateTestID(writerID, writeNum int) string {
	return fmt.Sprintf("cmd-%c-%c", 'a'+writerID, '0'+writeNum)
}

func strPtr(s string) *string {
	return &s
}
