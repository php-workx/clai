package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// CorruptionEvent records details about a database corruption incident.
type CorruptionEvent struct {
	Timestamp         time.Time `json:"timestamp"`
	OriginalPath      string    `json:"original_path"`
	CorruptBackup     string    `json:"corrupt_backup"`
	Reason            string    `json:"reason"`
	OriginalSizeBytes int64     `json:"original_size_bytes"`
	RecoverySuccess   bool      `json:"recovery_success"`
}

// CorruptionHistory holds the history of corruption events.
type CorruptionHistory struct {
	Events []CorruptionEvent `json:"events"`
}

// corruptionHistoryFilename is the name of the JSON file storing corruption events.
const corruptionHistoryFilename = "corruption_history.json"

// isCorruptionError checks if an error indicates SQLite database corruption.
// It detects SQLITE_CORRUPT (11), SQLITE_NOTADB (26), and related error messages.
func isCorruptionError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	// Check for known corruption error messages
	corruptionMessages := []string{
		"database disk image is malformed",
		"file is not a database",
		"file is encrypted or is not a database",
		"disk I/O error",
	}

	for _, cm := range corruptionMessages {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(cm)) {
			return true
		}
	}

	// Check for SQLite error codes embedded in the error message.
	// modernc.org/sqlite wraps errors with the code number.
	// SQLITE_CORRUPT = 11, SQLITE_NOTADB = 26
	if strings.Contains(msg, "SQLITE_CORRUPT") || strings.Contains(msg, "sqlite3: corrupt") {
		return true
	}
	if strings.Contains(msg, "SQLITE_NOTADB") || strings.Contains(msg, "not a database") {
		return true
	}

	return false
}

// isPermissionError checks if an error is a permission-related error.
// Permission errors should NOT trigger rotation.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for EACCES and EPERM
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return true
	}

	// Check error message for permission strings
	msg := err.Error()
	if strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted") {
		return true
	}

	return false
}

// isDiskFullError checks if an error indicates the disk is full.
// Disk full errors should NOT trigger rotation.
func isDiskFullError(err error) bool {
	if err == nil {
		return false
	}

	// Check for ENOSPC
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}

	msg := err.Error()
	if strings.Contains(msg, "no space left on device") || strings.Contains(msg, "disk full") {
		return true
	}

	return false
}

// rotateCorruptDB renames all database files (main, WAL, SHM) with a corruption
// timestamp suffix. Returns the backup path of the main DB file.
func rotateCorruptDB(dbPath string) (string, error) {
	ts := time.Now().Unix()
	suffix := fmt.Sprintf(".corrupt.%d", ts)

	// Files to rotate: main db, WAL, SHM
	files := []string{
		dbPath,
		dbPath + "-wal",
		dbPath + "-shm",
	}

	var mainBackup string
	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			continue // File doesn't exist, skip
		}
		backup := f + suffix
		if err := os.Rename(f, backup); err != nil {
			return "", fmt.Errorf("failed to rotate %s: %w", f, err)
		}
		if f == dbPath {
			mainBackup = backup
		}
	}

	return mainBackup, nil
}

// RunIntegrityCheck runs PRAGMA integrity_check on the database.
// Returns nil if the database is healthy, or an error describing the corruption.
func RunIntegrityCheck(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("failed to run integrity check: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("failed to scan integrity check result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("integrity check rows error: %w", err)
	}

	// A healthy database returns exactly one row: "ok"
	if len(results) == 1 && results[0] == "ok" {
		return nil
	}

	return fmt.Errorf("integrity check failed: %s", strings.Join(results, "; "))
}

// RecoverOptions configures database recovery behavior.
type RecoverOptions struct {
	Logger            *slog.Logger
	RunIntegrityCheck bool
}

// recoverAndReopen attempts to recover from a corrupted database by:
// 1. Closing the corrupted connection (if any)
// 2. Rotating all DB files to .corrupt.<timestamp>
// 3. Opening a fresh database with the full schema
//
// It returns a new *sql.DB or an error if recovery fails.
func recoverAndReopen(ctx context.Context, dbPath string, corruptDB *sql.DB, reason string, logger *slog.Logger) (*sql.DB, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Get original file size for the corruption event record
	var originalSize int64
	if info, err := os.Stat(dbPath); err == nil {
		originalSize = info.Size()
	}

	// Step 1: Close the corrupted connection
	if corruptDB != nil {
		_ = corruptDB.Close()
	}

	// Step 2: Rotate corrupt files
	backupPath, err := rotateCorruptDB(dbPath)
	if err != nil {
		logger.Error("failed to rotate corrupt database",
			"path", dbPath,
			"error", err,
		)
		return nil, fmt.Errorf("failed to rotate corrupt database: %w", err)
	}

	logger.Error("database corruption detected; rotated and reinitializing",
		"path", dbPath,
		"backup", backupPath,
		"reason", reason,
		"original_size_bytes", originalSize,
	)

	// Step 3: Open a fresh database
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	newDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open fresh database after recovery: %w", err)
	}

	newDB.SetMaxOpenConns(1)
	newDB.SetMaxIdleConns(1)
	newDB.SetConnMaxLifetime(0)

	if err := newDB.PingContext(ctx); err != nil {
		newDB.Close()
		return nil, fmt.Errorf("failed to ping fresh database after recovery: %w", err)
	}

	// Step 4: Run V2 migrations on the fresh database
	if err := RunV2Migrations(ctx, newDB); err != nil {
		newDB.Close()
		return nil, fmt.Errorf("failed to run migrations on fresh database: %w", err)
	}

	// Step 5: Record the corruption event
	event := &CorruptionEvent{
		Timestamp:         time.Now(),
		OriginalPath:      dbPath,
		OriginalSizeBytes: originalSize,
		CorruptBackup:     backupPath,
		Reason:            reason,
		RecoverySuccess:   true,
	}
	if err := recordCorruptionEvent(dbPath, event); err != nil {
		// Non-fatal: log but continue
		logger.Warn("failed to record corruption event",
			"error", err,
		)
	}

	return newDB, nil
}

// recordCorruptionEvent appends a corruption event to the history file.
func recordCorruptionEvent(dbPath string, event *CorruptionEvent) error {
	historyPath := corruptionHistoryPath(dbPath)

	history, err := LoadCorruptionHistory(historyPath)
	if err != nil {
		// Start fresh if we can't load existing history
		history = &CorruptionHistory{}
	}

	history.Events = append(history.Events, *event)

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal corruption history: %w", err)
	}

	if err := os.WriteFile(historyPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write corruption history: %w", err)
	}

	return nil
}

// LoadCorruptionHistory loads the corruption history from the given path.
func LoadCorruptionHistory(path string) (*CorruptionHistory, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reads user-specified path
	if err != nil {
		if os.IsNotExist(err) {
			return &CorruptionHistory{}, nil
		}
		return nil, fmt.Errorf("failed to read corruption history: %w", err)
	}

	var history CorruptionHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to parse corruption history: %w", err)
	}

	return &history, nil
}

// corruptionHistoryPath returns the path to the corruption history file
// for a given database path.
func corruptionHistoryPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), corruptionHistoryFilename)
}

// CorruptionHistoryPath returns the path to the corruption history file
// for the default database directory.
func CorruptionHistoryPath() (string, error) {
	dbPath, err := DefaultDBPath()
	if err != nil {
		return "", err
	}
	return corruptionHistoryPath(dbPath), nil
}
