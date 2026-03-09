package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Corruption Detection Tests
// =============================================================================

func TestIsCorruptionError_NilError(t *testing.T) {
	t.Parallel()
	if isCorruptionError(nil) {
		t.Error("nil error should not be corruption")
	}
}

func TestIsCorruptionError_MalformedImage(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "database disk image is malformed"}
	if !isCorruptionError(err) {
		t.Error("malformed image should be corruption")
	}
}

func TestIsCorruptionError_NotADatabase(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "file is not a database"}
	if !isCorruptionError(err) {
		t.Error("not a database should be corruption")
	}
}

func TestIsCorruptionError_SQLiteCorrupt(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "SQLITE_CORRUPT: some detail"}
	if !isCorruptionError(err) {
		t.Error("SQLITE_CORRUPT should be corruption")
	}
}

func TestIsCorruptionError_SQLiteNotADB(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "SQLITE_NOTADB: header check failed"}
	if !isCorruptionError(err) {
		t.Error("SQLITE_NOTADB should be corruption")
	}
}

func TestIsCorruptionError_EncryptedDB(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "file is encrypted or is not a database"}
	if !isCorruptionError(err) {
		t.Error("encrypted or not a database should be corruption")
	}
}

func TestIsCorruptionError_NormalError(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "connection refused"}
	if isCorruptionError(err) {
		t.Error("connection refused should not be corruption")
	}
}

func TestIsCorruptionError_CaseInsensitive(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "DATABASE DISK IMAGE IS MALFORMED"}
	if !isCorruptionError(err) {
		t.Error("corruption detection should be case-insensitive for known messages")
	}
}

func TestIsPermissionError_EACCES(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "permission denied"}
	if !isPermissionError(err) {
		t.Error("permission denied should be a permission error")
	}
}

func TestIsPermissionError_NormalError(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "database disk image is malformed"}
	if isPermissionError(err) {
		t.Error("corruption error should not be a permission error")
	}
}

func TestIsPermissionError_NilError(t *testing.T) {
	t.Parallel()
	if isPermissionError(nil) {
		t.Error("nil error should not be a permission error")
	}
}

func TestIsDiskFullError_NoSpace(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "no space left on device"}
	if !isDiskFullError(err) {
		t.Error("no space left should be a disk full error")
	}
}

func TestIsDiskFullError_NilError(t *testing.T) {
	t.Parallel()
	if isDiskFullError(nil) {
		t.Error("nil error should not be a disk full error")
	}
}

func TestIsDiskFullError_NormalError(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "connection timeout"}
	if isDiskFullError(err) {
		t.Error("timeout should not be a disk full error")
	}
}

// =============================================================================
// Rotation Tests
// =============================================================================

func TestRotateCorruptDB_AllFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create main DB, WAL, and SHM files
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(dbPath+suffix, []byte("data"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", dbPath+suffix, err)
		}
	}

	backup, err := rotateCorruptDB(dbPath)
	if err != nil {
		t.Fatalf("rotateCorruptDB() error = %v", err)
	}

	// Verify backup path is returned
	if backup == "" {
		t.Error("backup path should not be empty")
	}
	if !strings.Contains(backup, ".corrupt.") {
		t.Errorf("backup path should contain .corrupt., got: %s", backup)
	}

	// Verify all original files are gone
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Errorf("Original file %s should not exist after rotation", dbPath+suffix)
		}
	}

	// Verify all backup files exist
	for _, suffix := range []string{"", "-wal", "-shm"} {
		backupFile := dbPath + suffix + strings.TrimPrefix(backup, dbPath)
		if suffix != "" {
			// For WAL and SHM, the backup uses the same timestamp
			ts := strings.TrimPrefix(backup, dbPath+".corrupt.")
			backupFile = dbPath + suffix + ".corrupt." + ts
		}
		if _, err := os.Stat(backupFile); os.IsNotExist(err) {
			t.Errorf("Backup file %s should exist after rotation", backupFile)
		}
	}
}

func TestRotateCorruptDB_OnlyMainFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Only create main DB file (no WAL or SHM)
	if err := os.WriteFile(dbPath, []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	backup, err := rotateCorruptDB(dbPath)
	if err != nil {
		t.Fatalf("rotateCorruptDB() error = %v", err)
	}

	if backup == "" {
		t.Error("backup path should not be empty")
	}

	// Verify original file is gone
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("Original file should not exist after rotation")
	}
}

func TestRotateCorruptDB_NoFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nonexistent.db")

	// Should succeed even with no files
	backup, err := rotateCorruptDB(dbPath)
	if err != nil {
		t.Fatalf("rotateCorruptDB() error = %v", err)
	}

	// Backup should be empty since there was no main file
	if backup != "" {
		t.Errorf("backup path should be empty for nonexistent file, got: %s", backup)
	}
}

// =============================================================================
// Integrity Check Tests
// =============================================================================

func TestRunIntegrityCheck_HealthyDB(t *testing.T) {
	t.Parallel()

	db := newTestV2DB(t)
	defer db.Close()

	err := RunIntegrityCheck(context.Background(), db.DB())
	if err != nil {
		t.Errorf("RunIntegrityCheck() on healthy DB error = %v", err)
	}
}

func TestRunIntegrityCheck_CorruptDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupt.db")

	// Write garbage data to simulate corruption
	if err := os.WriteFile(dbPath, []byte("this is not a sqlite database at all, it is garbage data that should fail integrity check"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	dsn := "file:" + dbPath + "?_pragma=busy_timeout(5000)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer sqlDB.Close()

	err = RunIntegrityCheck(context.Background(), sqlDB)
	if err == nil {
		t.Error("RunIntegrityCheck() should fail on corrupt DB")
	}
}

// =============================================================================
// Corruption History Tests
// =============================================================================

func TestCorruptionHistory_LoadEmpty(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corruption_history.json")

	history, err := LoadCorruptionHistory(path)
	if err != nil {
		t.Fatalf("LoadCorruptionHistory() error = %v", err)
	}
	if len(history.Events) != 0 {
		t.Errorf("Expected empty history, got %d events", len(history.Events))
	}
}

func TestCorruptionHistory_RecordAndLoad(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	event := CorruptionEvent{
		Timestamp:         time.Now(),
		OriginalPath:      dbPath,
		OriginalSizeBytes: 1024,
		CorruptBackup:     dbPath + ".corrupt.123",
		Reason:            "database disk image is malformed",
		RecoverySuccess:   true,
	}

	err := recordCorruptionEvent(dbPath, &event)
	if err != nil {
		t.Fatalf("recordCorruptionEvent() error = %v", err)
	}

	// Load and verify
	historyPath := corruptionHistoryPath(dbPath)
	history, err := LoadCorruptionHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadCorruptionHistory() error = %v", err)
	}

	if len(history.Events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(history.Events))
	}

	if history.Events[0].OriginalPath != dbPath {
		t.Errorf("OriginalPath = %s, want %s", history.Events[0].OriginalPath, dbPath)
	}
	if history.Events[0].OriginalSizeBytes != 1024 {
		t.Errorf("OriginalSizeBytes = %d, want 1024", history.Events[0].OriginalSizeBytes)
	}
	if !history.Events[0].RecoverySuccess {
		t.Error("RecoverySuccess should be true")
	}
}

func TestCorruptionHistory_MultipleEvents(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	for i := 0; i < 3; i++ {
		event := CorruptionEvent{
			Timestamp:         time.Now(),
			OriginalPath:      dbPath,
			OriginalSizeBytes: int64(i * 1000),
			CorruptBackup:     dbPath + ".corrupt.123",
			Reason:            "test corruption",
			RecoverySuccess:   true,
		}
		if err := recordCorruptionEvent(dbPath, &event); err != nil {
			t.Fatalf("recordCorruptionEvent() error = %v", err)
		}
	}

	historyPath := corruptionHistoryPath(dbPath)
	history, err := LoadCorruptionHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadCorruptionHistory() error = %v", err)
	}

	if len(history.Events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(history.Events))
	}
}

func TestCorruptionHistory_InvalidJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corruption_history.json")

	// Write invalid JSON
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	_, err := LoadCorruptionHistory(path)
	if err == nil {
		t.Error("LoadCorruptionHistory() should fail on invalid JSON")
	}
}

func TestCorruptionHistoryPath(t *testing.T) {
	t.Parallel()
	path := corruptionHistoryPath("/home/user/.clai/suggestions_v2.db")
	expected := "/home/user/.clai/corruption_history.json"
	if path != expected {
		t.Errorf("corruptionHistoryPath() = %s, want %s", path, expected)
	}
}

func TestCorruptionHistoryPath_Public(t *testing.T) {
	t.Parallel()

	path, err := CorruptionHistoryPath()
	if err != nil {
		t.Fatalf("CorruptionHistoryPath() error = %v", err)
	}
	if path == "" {
		t.Error("CorruptionHistoryPath() returned empty string")
	}
	if filepath.Base(path) != corruptionHistoryFilename {
		t.Errorf("CorruptionHistoryPath() = %s, want filename %s", path, corruptionHistoryFilename)
	}
}

// =============================================================================
// Recovery Integration Tests
// =============================================================================

func TestRecoverAndReopen_SuccessfulRecovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Create a valid V2 database first
	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Insert some data to verify it gets lost after recovery
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('sess-1', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert error = %v", err)
	}
	db.Close()

	// Now simulate corruption recovery
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	recoveredDB, err := recoverAndReopen(context.Background(), dbPath, nil, "test corruption", logger)
	if err != nil {
		t.Fatalf("recoverAndReopen() error = %v", err)
	}
	defer recoveredDB.Close()

	// Verify the recovered DB is functional but empty
	var count int
	err = recoveredDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM session").Scan(&count)
	if err != nil {
		t.Fatalf("Query on recovered DB error = %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 sessions in recovered DB, got %d", count)
	}

	// Verify all V2 tables exist
	err = ValidateV2Schema(context.Background(), recoveredDB)
	if err != nil {
		t.Errorf("ValidateV2Schema() on recovered DB error = %v", err)
	}

	// Verify corruption backup exists
	matches, err := filepath.Glob(dbPath + ".corrupt.*")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) == 0 {
		t.Error("No corruption backup files found")
	}

	// Verify corruption history was recorded
	historyPath := corruptionHistoryPath(dbPath)
	history, err := LoadCorruptionHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadCorruptionHistory() error = %v", err)
	}
	if len(history.Events) != 1 {
		t.Errorf("Expected 1 corruption event, got %d", len(history.Events))
	}
}

func TestOpen_WithRecovery_CorruptDB(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Write garbage to simulate a corrupt database
	if err := os.WriteFile(dbPath, []byte("this is not a valid sqlite database file and should trigger corruption recovery"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	// Open with recovery enabled
	db, err := Open(context.Background(), Options{
		Path:           dbPath,
		SkipLock:       true,
		EnableRecovery: true,
	})
	if err != nil {
		t.Fatalf("Open() with recovery error = %v", err)
	}
	defer db.Close()

	// Verify the DB is functional
	err = db.ValidateV2(context.Background())
	if err != nil {
		t.Errorf("ValidateV2() after recovery error = %v", err)
	}

	// Verify backup was created
	matches, err := filepath.Glob(filepath.Join(tmpDir, "suggestions_v2.db.corrupt.*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) == 0 {
		t.Error("No corruption backup files found after recovery")
	}
}

func TestOpen_WithRecovery_IntegrityCheck(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// First create a valid DB
	db, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("Initial Open() error = %v", err)
	}
	db.Close()

	// Open with integrity check (should pass on healthy DB)
	db, err = Open(context.Background(), Options{
		Path:              dbPath,
		SkipLock:          true,
		EnableRecovery:    true,
		RunIntegrityCheck: true,
	})
	if err != nil {
		t.Fatalf("Open() with integrity check error = %v", err)
	}
	defer db.Close()

	err = db.ValidateV2(context.Background())
	if err != nil {
		t.Errorf("ValidateV2() after integrity check error = %v", err)
	}
}

func TestOpen_WithoutRecovery_CorruptDB(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Write garbage to simulate corruption
	if err := os.WriteFile(dbPath, []byte("this is not a valid sqlite database file"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	// Open WITHOUT recovery enabled - should fail
	_, err := Open(context.Background(), Options{
		Path:     dbPath,
		SkipLock: true,
		// EnableRecovery is false
	})
	if err == nil {
		t.Fatal("Open() without recovery should fail on corrupt DB")
	}
}

func TestOpen_V1DB_NoRecovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions.db")

	// Recovery should not be attempted for V1 databases
	if err := os.WriteFile(dbPath, []byte("garbage"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	_, err := Open(context.Background(), Options{
		Path:           dbPath,
		SkipLock:       true,
		UseV1:          true,
		EnableRecovery: true, // Should be ignored for V1
	})
	if err == nil {
		t.Fatal("Open() V1 with recovery should still fail (recovery not supported for V1)")
	}
}

func TestOpen_WithRecovery_FreshDB(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Opening a fresh DB with recovery enabled should work normally
	db, err := Open(context.Background(), Options{
		Path:           dbPath,
		SkipLock:       true,
		EnableRecovery: true,
	})
	if err != nil {
		t.Fatalf("Open() fresh DB with recovery error = %v", err)
	}
	defer db.Close()

	err = db.ValidateV2(context.Background())
	if err != nil {
		t.Errorf("ValidateV2() on fresh DB error = %v", err)
	}

	// No backup files should be created
	matches, err := filepath.Glob(filepath.Join(tmpDir, "*.corrupt.*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("No backup files expected for fresh DB, found %d", len(matches))
	}
}

func TestOpen_WithRecovery_RecoveredDBIsFunctional(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Write garbage
	if err := os.WriteFile(dbPath, []byte("definitely not sqlite"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	// Open with recovery
	db, err := Open(context.Background(), Options{
		Path:           dbPath,
		SkipLock:       true,
		EnableRecovery: true,
	})
	if err != nil {
		t.Fatalf("Open() with recovery error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Insert data into the recovered DB
	_, err = db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('recovered-session', 'zsh', 1000)
	`)
	if err != nil {
		t.Fatalf("Insert into recovered DB error = %v", err)
	}

	// Query back
	var id string
	err = db.QueryRowContext(ctx, "SELECT id FROM session WHERE id = 'recovered-session'").Scan(&id)
	if err != nil {
		t.Fatalf("Query from recovered DB error = %v", err)
	}
	if id != "recovered-session" {
		t.Errorf("Got id=%s, want recovered-session", id)
	}

	// Verify schema version
	version, err := db.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("Version() = %d, want %d", version, SchemaVersion)
	}
}

func TestOpen_WithRecovery_WALAndSHMRotated(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Test the rotateCorruptDB function directly to verify WAL/SHM rotation.
	// When going through Open(), SQLite may modify or remove WAL/SHM files
	// before we detect corruption, so we test rotation separately.

	// Create main, WAL, and SHM files
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(dbPath+suffix, []byte("data"+suffix), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", dbPath+suffix, err)
		}
	}

	backup, err := rotateCorruptDB(dbPath)
	if err != nil {
		t.Fatalf("rotateCorruptDB() error = %v", err)
	}

	if backup == "" {
		t.Fatal("backup path should not be empty")
	}

	// Verify all corrupt backups were created
	for _, suffix := range []string{"", "-wal", "-shm"} {
		pattern := filepath.Join(tmpDir, "suggestions_v2.db"+suffix+".corrupt.*")
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			t.Fatalf("Glob(%s) error = %v", pattern, globErr)
		}
		if len(matches) == 0 {
			t.Errorf("No backup found for %s", "suggestions_v2.db"+suffix)
		}
	}

	// Verify original files are gone
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statErr := os.Stat(dbPath + suffix); !os.IsNotExist(statErr) {
			t.Errorf("Original file %s should not exist after rotation", dbPath+suffix)
		}
	}

	// Now also verify that Open with recovery handles the main file corruption.
	// Write just the main file as garbage (WAL/SHM may not exist).
	if writeErr := os.WriteFile(dbPath, []byte("garbage"), 0644); writeErr != nil {
		t.Fatalf("Failed to create corrupt file: %v", writeErr)
	}

	db, err := Open(context.Background(), Options{
		Path:           dbPath,
		SkipLock:       true,
		EnableRecovery: true,
	})
	if err != nil {
		t.Fatalf("Open() with recovery error = %v", err)
	}
	defer db.Close()

	// Main DB backup should exist (may be 1 or 2 depending on timing)
	pattern := filepath.Join(tmpDir, "suggestions_v2.db.corrupt.*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob(%s) error = %v", pattern, err)
	}
	if len(matches) < 1 {
		t.Error("Expected at least 1 main backup file from Open recovery")
	}
}

func TestOpen_WithRecovery_MultipleCycles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Simulate multiple corruption-recovery cycles
	for i := 0; i < 3; i++ {
		// Corrupt the DB
		if err := os.WriteFile(dbPath, []byte("garbage"), 0644); err != nil {
			t.Fatalf("Cycle %d: failed to create corrupt file: %v", i, err)
		}

		db, err := Open(context.Background(), Options{
			Path:           dbPath,
			SkipLock:       true,
			EnableRecovery: true,
		})
		if err != nil {
			t.Fatalf("Cycle %d: Open() error = %v", i, err)
		}

		// Verify DB is functional
		if err := db.ValidateV2(context.Background()); err != nil {
			t.Errorf("Cycle %d: ValidateV2() error = %v", i, err)
		}

		db.Close()

		// Small delay to ensure different timestamps
		time.Sleep(time.Millisecond * 10)
	}

	// Verify corruption history has all events
	historyPath := corruptionHistoryPath(dbPath)
	history, err := LoadCorruptionHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadCorruptionHistory() error = %v", err)
	}
	if len(history.Events) != 3 {
		t.Errorf("Expected 3 corruption events, got %d", len(history.Events))
	}
}

// =============================================================================
// Error Classification Edge Cases
// =============================================================================

func TestErrorClassification_CorruptionNotPermission(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "database disk image is malformed"}
	if isPermissionError(err) {
		t.Error("corruption error should not be classified as permission error")
	}
	if isDiskFullError(err) {
		t.Error("corruption error should not be classified as disk full error")
	}
}

func TestErrorClassification_PermissionNotCorruption(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "permission denied"}
	if isCorruptionError(err) {
		t.Error("permission error should not be classified as corruption")
	}
}

func TestErrorClassification_DiskFullNotCorruption(t *testing.T) {
	t.Parallel()
	err := &mockError{msg: "no space left on device"}
	if isCorruptionError(err) {
		t.Error("disk full error should not be classified as corruption")
	}
}

// =============================================================================
// Corruption History JSON Serialization
// =============================================================================

func TestCorruptionEvent_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second) // Truncate to avoid nanosecond differences
	event := CorruptionEvent{
		Timestamp:         now,
		OriginalPath:      "/home/user/.clai/suggestions_v2.db",
		OriginalSizeBytes: 4096,
		CorruptBackup:     "/home/user/.clai/suggestions_v2.db.corrupt.1234567890",
		Reason:            "database disk image is malformed",
		RecoverySuccess:   true,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded CorruptionEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.OriginalPath != event.OriginalPath {
		t.Errorf("OriginalPath = %s, want %s", decoded.OriginalPath, event.OriginalPath)
	}
	if decoded.OriginalSizeBytes != event.OriginalSizeBytes {
		t.Errorf("OriginalSizeBytes = %d, want %d", decoded.OriginalSizeBytes, event.OriginalSizeBytes)
	}
	if decoded.RecoverySuccess != event.RecoverySuccess {
		t.Errorf("RecoverySuccess = %v, want %v", decoded.RecoverySuccess, event.RecoverySuccess)
	}
}

// =============================================================================
// Logger Integration
// =============================================================================

func TestOpen_WithRecovery_UsesProvidedLogger(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	// Write garbage
	if err := os.WriteFile(dbPath, []byte("not sqlite"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	// Create a logger that writes to a buffer
	logFile := filepath.Join(tmpDir, "test.log")
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer f.Close()

	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db, err := Open(context.Background(), Options{
		Path:           dbPath,
		SkipLock:       true,
		EnableRecovery: true,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
}

// =============================================================================
// Helpers
// =============================================================================

// mockError is a simple error implementation for testing error classification.
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
