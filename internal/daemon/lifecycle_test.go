package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
		BaseDir: tmpDir,
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
		BaseDir: tmpDir,
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

func TestIsRunningWithPaths_LockHeldPIDFallback(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Hold the daemon lock in this process, but do not create a PID file.
	lockPath := LockFilePath(paths.BaseDir)
	lock := NewLockFile(lockPath)
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire lock failed: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	if !IsRunningWithPaths(paths) {
		t.Error("expected IsRunningWithPaths to return true when lock is held by a live process")
	}
}

func TestCleanupStaleWithPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		BaseDir: tmpDir,
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
		BaseDir: tmpDir,
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
		BaseDir: tmpDir,
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
		BaseDir: tmpDir,
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

func TestResolveRunPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{BaseDir: tmpDir}
	got, err := resolveRunPaths(&ServerConfig{Paths: paths})
	if err != nil {
		t.Fatalf("resolveRunPaths() error = %v", err)
	}
	if got != paths {
		t.Fatalf("resolveRunPaths() returned unexpected paths pointer")
	}
}

func TestHandleLifecycleSignal(t *testing.T) {
	t.Parallel()

	makeServer := func(baseDir string) *Server {
		return &Server{
			logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
			shutdownChan: make(chan struct{}),
			paths:        &config.Paths{BaseDir: baseDir},
		}
	}

	t.Run("SIGTERM triggers shutdown", func(t *testing.T) {
		srv := makeServer(t.TempDir())
		cancelled := false
		stop := handleLifecycleSignal(syscall.SIGTERM, func() { cancelled = true }, &ServerConfig{}, srv, NewLockFile(filepath.Join(t.TempDir(), "lock")))
		if !stop {
			t.Fatalf("handleLifecycleSignal(SIGTERM) stop = false, want true")
		}
		if !cancelled {
			t.Fatalf("cancel should have been called")
		}
		select {
		case <-srv.shutdownChan:
		default:
			t.Fatalf("shutdown channel should be closed")
		}
	})

	t.Run("SIGHUP with reload function", func(t *testing.T) {
		srv := makeServer(t.TempDir())
		calls := 0
		cfg := &ServerConfig{
			ReloadFn: func() error {
				calls++
				return nil
			},
		}
		stop := handleLifecycleSignal(syscall.SIGHUP, func() {}, cfg, srv, nil)
		if stop {
			t.Fatalf("handleLifecycleSignal(SIGHUP) stop = true, want false")
		}
		if calls != 1 {
			t.Fatalf("reload calls = %d, want 1", calls)
		}
	})

	t.Run("unknown signal ignored", func(t *testing.T) {
		srv := makeServer(t.TempDir())
		stop := handleLifecycleSignal(syscall.SIGWINCH, func() {}, &ServerConfig{}, srv, nil)
		if stop {
			t.Fatalf("handleLifecycleSignal(SIGWINCH) stop = true, want false")
		}
	})
}

func TestResolveStopPID(t *testing.T) {
	t.Parallel()

	t.Run("uses live pid file first", func(t *testing.T) {
		paths := &config.Paths{BaseDir: t.TempDir()}
		if err := os.WriteFile(paths.PIDFile(), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		pid, err := resolveStopPID(paths)
		if err != nil {
			t.Fatalf("resolveStopPID() error = %v", err)
		}
		if pid != os.Getpid() {
			t.Fatalf("resolveStopPID() pid = %d, want %d", pid, os.Getpid())
		}
	})

	t.Run("falls back to lock pid", func(t *testing.T) {
		paths := &config.Paths{BaseDir: t.TempDir()}
		if err := os.WriteFile(paths.PIDFile(), []byte("999999999\n"), 0600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		lock := NewLockFile(LockFilePath(paths.BaseDir))
		if err := lock.Acquire(); err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		t.Cleanup(func() { _ = lock.Release() })

		pid, err := resolveStopPID(paths)
		if err != nil {
			t.Fatalf("resolveStopPID() error = %v", err)
		}
		if pid != os.Getpid() {
			t.Fatalf("resolveStopPID() pid = %d, want %d", pid, os.Getpid())
		}
	})
}

func TestStopWithPaths_NotRunning(t *testing.T) {
	t.Parallel()
	paths := &config.Paths{BaseDir: t.TempDir()}
	err := StopWithPaths(paths)
	if err == nil {
		t.Fatalf("StopWithPaths() expected error when daemon is not running")
	}
}

func TestStopWithPaths_SignalsProcess(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "sleep 5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	paths := &config.Paths{BaseDir: t.TempDir()}
	if err := os.WriteFile(paths.PIDFile(), []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := StopWithPaths(paths); err != nil {
		t.Fatalf("StopWithPaths() error = %v", err)
	}
}

func TestProcessExists(t *testing.T) {
	t.Parallel()
	if !processExists(os.Getpid()) {
		t.Fatalf("processExists(current pid) = false, want true")
	}
	if processExists(999999999) {
		t.Fatalf("processExists(nonexistent) = true, want false")
	}
}

func TestWaitForProcessExit(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "sleep 5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if err := waitForProcessExit(cmd.Process, 100*time.Millisecond); err != nil {
		t.Fatalf("waitForProcessExit() error = %v", err)
	}
}

func TestWaitForSocket_DefaultWrapper(t *testing.T) {
	tmpDir := t.TempDir()
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		_ = os.Setenv("HOME", oldHome)
	})
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Setenv("HOME", tmpDir)

	paths := config.DefaultPaths()
	if err := os.MkdirAll(filepath.Dir(paths.SocketFile()), 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(paths.SocketFile(), []byte("socket"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := WaitForSocket(50 * time.Millisecond); err != nil {
		t.Fatalf("WaitForSocket() error = %v", err)
	}
}

func TestCleanupStale_DefaultWrapper(t *testing.T) {
	tmpDir := t.TempDir()
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		_ = os.Setenv("HOME", oldHome)
	})
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Setenv("HOME", tmpDir)

	paths := config.DefaultPaths()
	if err := os.MkdirAll(filepath.Dir(paths.SocketFile()), 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(paths.SocketFile(), []byte("socket"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(paths.PIDFile(), []byte("999999999\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := CleanupStale(); err != nil {
		t.Fatalf("CleanupStale() error = %v", err)
	}
}
