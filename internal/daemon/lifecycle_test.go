package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runger/clai/internal/config"
)

func TestReadPID(t *testing.T) {
	t.Parallel()

	// Create temp directory
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write a valid PID
	if err := os.WriteFile(pidFile, []byte("12345\n"), 0600); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	pid, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}

	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}
}

func TestReadPID_InvalidPID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write an invalid PID
	if err := os.WriteFile(pidFile, []byte("not-a-number\n"), 0600); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	_, err := ReadPID(pidFile)
	if err == nil {
		t.Error("expected error for invalid PID")
	}
}

func TestReadPID_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadPID("/nonexistent/path/file.pid")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestIsRunningWithPaths_NotRunning(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		RuntimeDir: tmpDir,
	}

	// No PID file exists
	if IsRunningWithPaths(paths) {
		t.Error("expected IsRunning to return false when no PID file exists")
	}
}

func TestIsRunningWithPaths_StalePID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		RuntimeDir: tmpDir,
	}

	// Write a stale PID (unlikely to be a running process)
	pidFile := paths.PIDFile()
	if err := os.WriteFile(pidFile, []byte("999999999\n"), 0600); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	// Should return false because the process doesn't exist
	if IsRunningWithPaths(paths) {
		t.Error("expected IsRunning to return false for stale PID")
	}
}

func TestCleanupStaleWithPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		RuntimeDir: tmpDir,
	}

	// Create socket and PID files
	socketFile := paths.SocketFile()
	pidFile := paths.PIDFile()

	if err := os.WriteFile(socketFile, []byte("socket"), 0600); err != nil {
		t.Fatalf("failed to create socket file: %v", err)
	}
	if err := os.WriteFile(pidFile, []byte("12345\n"), 0600); err != nil {
		t.Fatalf("failed to create PID file: %v", err)
	}

	// Cleanup
	err := CleanupStaleWithPaths(paths)
	if err != nil {
		t.Fatalf("CleanupStale failed: %v", err)
	}

	// Verify files are gone
	if _, err := os.Stat(socketFile); !os.IsNotExist(err) {
		t.Error("socket file should be removed")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("PID file should be removed")
	}
}

func TestWaitForSocketWithPaths_Exists(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		RuntimeDir: tmpDir,
	}

	// Create the socket file
	socketFile := paths.SocketFile()
	if err := os.WriteFile(socketFile, []byte("socket"), 0600); err != nil {
		t.Fatalf("failed to create socket file: %v", err)
	}

	// Should succeed immediately
	err := WaitForSocketWithPaths(paths, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForSocket failed: %v", err)
	}
}

func TestWaitForSocketWithPaths_Timeout(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		RuntimeDir: tmpDir,
	}

	// Don't create the socket file
	err := WaitForSocketWithPaths(paths, 100*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestWaitForSocketWithContext_Cancelled(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		RuntimeDir: tmpDir,
	}

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// Should return context.Canceled error
	err := WaitForSocketWithContext(ctx, paths, 5*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
