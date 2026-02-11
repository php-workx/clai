package ipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPidPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	// Must match config.Paths.PIDFile()
	expected := filepath.Join(home, ".clai", "clai.pid")

	path := PidPath()
	if path != expected {
		t.Errorf("PidPath() = %q, want %q", path, expected)
	}
}

func TestLogPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	// Must match where daemon log file goes
	expected := filepath.Join(home, ".clai", "logs", "daemon.log")

	path := LogPath()
	if path != expected {
		t.Errorf("LogPath() = %q, want %q", path, expected)
	}
}

func TestFindDaemonBinaryFromEnv(t *testing.T) {
	// Create a temp file to act as the daemon binary
	tmpFile, err := os.CreateTemp("", "claid-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Make temp file executable
	if err := os.Chmod(tmpFile.Name(), 0o755); err != nil {
		t.Fatalf("Failed to chmod temp file: %v", err)
	}

	// Set environment variable
	os.Setenv("CLAI_DAEMON_PATH", tmpFile.Name())
	defer os.Unsetenv("CLAI_DAEMON_PATH")

	path, err := findDaemonBinary()
	if err != nil {
		t.Errorf("findDaemonBinary() error = %v", err)
	}
	if path != tmpFile.Name() {
		t.Errorf("findDaemonBinary() = %q, want %q", path, tmpFile.Name())
	}
}

func TestFindDaemonBinaryNotFound(t *testing.T) {
	// Ensure no env override
	os.Unsetenv("CLAI_DAEMON_PATH")

	// Use a non-existent path prefix to ensure binary isn't found
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", oldPath)

	// Set HOME to temp directory to avoid finding binary in ~/go/bin
	tmpDir, err := os.MkdirTemp("", "clai-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	_, err = findDaemonBinary()
	if err == nil {
		t.Error("findDaemonBinary() should fail when binary not found")
	}
}

func TestIsDaemonRunningNoSocket(t *testing.T) {
	os.Setenv("CLAI_SOCKET", "/tmp/nonexistent-clai-daemon-test.sock")
	defer os.Unsetenv("CLAI_SOCKET")

	if IsDaemonRunning() {
		t.Error("IsDaemonRunning() = true for non-existent socket")
	}
}

func TestDaemonBinaryName(t *testing.T) {
	if DaemonBinaryName == "" {
		t.Error("DaemonBinaryName should not be empty")
	}
	if DaemonBinaryName != "claid" {
		t.Errorf("DaemonBinaryName = %q, want %q", DaemonBinaryName, "claid")
	}
}

func TestSpawnDaemonMissingBinary(t *testing.T) {
	// Ensure daemon binary isn't found
	os.Unsetenv("CLAI_DAEMON_PATH")
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", oldPath)

	// Use a temp directory for run dir to avoid creating dirs under real user home
	tmpDir, err := os.MkdirTemp("", "clai-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set XDG_RUNTIME_DIR to temp directory to isolate test
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	defer os.Setenv("XDG_RUNTIME_DIR", oldXDG)

	// Set HOME to temp directory to avoid finding binary in ~/go/bin
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// This should fail because daemon binary doesn't exist
	err = SpawnDaemon()
	if err == nil {
		t.Error("SpawnDaemon() should fail when daemon binary not found")
	}
}

func TestSpawnDaemonContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := SpawnDaemonContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SpawnDaemonContext() error = %v, want context.Canceled", err)
	}
}

func TestSpawnAndWaitContextCanceledWhileWaiting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-spawn-cancel-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	daemonPath := filepath.Join(tmpDir, "claid-test")
	if err := os.WriteFile(daemonPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldSocket := os.Getenv("CLAI_SOCKET")
	oldDaemonPath := os.Getenv("CLAI_DAEMON_PATH")
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("CLAI_SOCKET", oldSocket)
		_ = os.Setenv("CLAI_DAEMON_PATH", oldDaemonPath)
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		_ = os.Setenv("HOME", oldHome)
	})

	_ = os.Setenv("CLAI_SOCKET", filepath.Join(tmpDir, "clai.sock"))
	_ = os.Setenv("CLAI_DAEMON_PATH", daemonPath)
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Setenv("HOME", tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err = SpawnAndWaitContext(ctx, 2*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SpawnAndWaitContext() error = %v, want context.Canceled", err)
	}
}

func TestSpawnAndWaitContextTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-spawn-timeout-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	daemonPath := filepath.Join(tmpDir, "claid-test")
	if err := os.WriteFile(daemonPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldSocket := os.Getenv("CLAI_SOCKET")
	oldDaemonPath := os.Getenv("CLAI_DAEMON_PATH")
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("CLAI_SOCKET", oldSocket)
		_ = os.Setenv("CLAI_DAEMON_PATH", oldDaemonPath)
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		_ = os.Setenv("HOME", oldHome)
	})

	_ = os.Setenv("CLAI_SOCKET", filepath.Join(tmpDir, "clai.sock"))
	_ = os.Setenv("CLAI_DAEMON_PATH", daemonPath)
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Setenv("HOME", tmpDir)

	err = SpawnAndWaitContext(context.Background(), 40*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not start within") {
		t.Fatalf("SpawnAndWaitContext() error = %v, want timeout error", err)
	}
}

func TestRemoveStaleSocketRetriesBeforeDelete(t *testing.T) {
	oldQuickDial := quickDialFn
	oldSocketExists := socketExistsFn
	oldSocketPath := socketPathFn
	oldRemove := removeFileFn
	oldDaemonLock := daemonLockFn
	oldAttempts := staleSocketDialAttempts
	oldDelay := staleSocketRetryDelay
	t.Cleanup(func() {
		quickDialFn = oldQuickDial
		socketExistsFn = oldSocketExists
		socketPathFn = oldSocketPath
		removeFileFn = oldRemove
		daemonLockFn = oldDaemonLock
		staleSocketDialAttempts = oldAttempts
		staleSocketRetryDelay = oldDelay
	})

	socketExistsFn = func() bool { return true }
	socketPathFn = func() string { return "/tmp/fake-clai.sock" }
	daemonLockFn = func() (int, bool, error) { return 0, false, nil }
	staleSocketDialAttempts = 3
	staleSocketRetryDelay = 0

	dialAttempts := 0
	quickDialFn = func() (io.Closer, error) {
		dialAttempts++
		if dialAttempts < 3 {
			return nil, errors.New("transient dial failure")
		}
		return io.NopCloser(strings.NewReader("")), nil
	}

	removeCalls := 0
	removeFileFn = func(path string) error {
		removeCalls++
		return nil
	}

	if err := removeStaleSocket(context.Background()); err != nil {
		t.Fatalf("removeStaleSocket() error = %v", err)
	}
	if removeCalls != 0 {
		t.Fatalf("removeStaleSocket() remove calls = %d, want 0", removeCalls)
	}
}

func TestRemoveStaleSocketDeleteError(t *testing.T) {
	oldQuickDial := quickDialFn
	oldSocketExists := socketExistsFn
	oldSocketPath := socketPathFn
	oldRemove := removeFileFn
	oldDaemonLock := daemonLockFn
	oldAttempts := staleSocketDialAttempts
	oldDelay := staleSocketRetryDelay
	t.Cleanup(func() {
		quickDialFn = oldQuickDial
		socketExistsFn = oldSocketExists
		socketPathFn = oldSocketPath
		removeFileFn = oldRemove
		daemonLockFn = oldDaemonLock
		staleSocketDialAttempts = oldAttempts
		staleSocketRetryDelay = oldDelay
	})

	socketExistsFn = func() bool { return true }
	socketPathFn = func() string { return "/tmp/fake-clai.sock" }
	daemonLockFn = func() (int, bool, error) { return 0, false, nil }
	staleSocketDialAttempts = 2
	staleSocketRetryDelay = 0
	quickDialFn = func() (io.Closer, error) {
		return nil, errors.New("connection refused")
	}
	removeFileFn = func(path string) error {
		return fmt.Errorf("permission denied")
	}

	err := removeStaleSocket(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to remove stale socket") {
		t.Fatalf("removeStaleSocket() error = %v, want wrapped delete error", err)
	}
}

func TestRemoveStaleSocketDoesNotDeleteForUnknownDialError(t *testing.T) {
	oldQuickDial := quickDialFn
	oldSocketExists := socketExistsFn
	oldSocketPath := socketPathFn
	oldRemove := removeFileFn
	oldDaemonLock := daemonLockFn
	oldAttempts := staleSocketDialAttempts
	oldDelay := staleSocketRetryDelay
	t.Cleanup(func() {
		quickDialFn = oldQuickDial
		socketExistsFn = oldSocketExists
		socketPathFn = oldSocketPath
		removeFileFn = oldRemove
		daemonLockFn = oldDaemonLock
		staleSocketDialAttempts = oldAttempts
		staleSocketRetryDelay = oldDelay
	})

	socketExistsFn = func() bool { return true }
	socketPathFn = func() string { return "/tmp/fake-clai.sock" }
	daemonLockFn = func() (int, bool, error) { return 0, false, nil }
	staleSocketDialAttempts = 2
	staleSocketRetryDelay = 0
	quickDialFn = func() (io.Closer, error) {
		// Something that's not clearly stale: we should not delete the socket.
		return nil, errors.New("permission denied")
	}

	removeCalls := 0
	removeFileFn = func(path string) error {
		removeCalls++
		return nil
	}

	err := removeStaleSocket(context.Background())
	if err == nil || !strings.Contains(err.Error(), "socket exists but dial failed") {
		t.Fatalf("removeStaleSocket() error = %v, want non-stale dial error", err)
	}
	if removeCalls != 0 {
		t.Fatalf("removeStaleSocket() remove calls = %d, want 0", removeCalls)
	}
}

func TestRemoveStaleSocketHonorsCancellation(t *testing.T) {
	oldQuickDial := quickDialFn
	oldSocketExists := socketExistsFn
	oldDaemonLock := daemonLockFn
	oldAttempts := staleSocketDialAttempts
	oldDelay := staleSocketRetryDelay
	t.Cleanup(func() {
		quickDialFn = oldQuickDial
		socketExistsFn = oldSocketExists
		daemonLockFn = oldDaemonLock
		staleSocketDialAttempts = oldAttempts
		staleSocketRetryDelay = oldDelay
	})

	socketExistsFn = func() bool { return true }
	daemonLockFn = func() (int, bool, error) { return 0, false, nil }
	staleSocketDialAttempts = 3
	staleSocketRetryDelay = 20 * time.Millisecond
	quickDialFn = func() (io.Closer, error) {
		return nil, errors.New("dial failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := removeStaleSocket(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("removeStaleSocket() error = %v, want context.Canceled", err)
	}
}

func TestFindDaemonBinaryEnvPathIsAbsolute(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-daemon-env-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	daemonPath := filepath.Join(tmpDir, "claid")
	if err := os.WriteFile(daemonPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	old := os.Getenv("CLAI_DAEMON_PATH")
	defer os.Setenv("CLAI_DAEMON_PATH", old)
	_ = os.Setenv("CLAI_DAEMON_PATH", daemonPath)

	got, err := findDaemonBinary()
	if err != nil {
		t.Fatalf("findDaemonBinary() error = %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("findDaemonBinary() = %q, want absolute path", got)
	}
}

func TestSpawnDaemonWritesPIDFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clai-spawn-pid-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	daemonPath := filepath.Join(tmpDir, "claid-test")
	if err := os.WriteFile(daemonPath, []byte("#!/bin/sh\nsleep 1\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldDaemonPath := os.Getenv("CLAI_DAEMON_PATH")
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("CLAI_DAEMON_PATH", oldDaemonPath)
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		_ = os.Setenv("HOME", oldHome)
	})

	_ = os.Setenv("CLAI_DAEMON_PATH", daemonPath)
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Setenv("HOME", tmpDir)

	if err := SpawnDaemon(); err != nil {
		t.Fatalf("SpawnDaemon() error = %v", err)
	}

	pidData, err := os.ReadFile(PidPath())
	if err != nil {
		t.Fatalf("ReadFile(PidPath) error = %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || pid <= 0 {
		t.Fatalf("pid file content = %q, want positive integer", string(pidData))
	}
	proc, err := os.FindProcess(pid)
	if err == nil && proc != nil {
		_ = proc.Kill()
	}
}

func TestSpawnAndWait_MissingBinary(t *testing.T) {
	oldPath := os.Getenv("PATH")
	oldHome := os.Getenv("HOME")
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Setenv("HOME", oldHome)
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
	})

	tmpDir := t.TempDir()
	_ = os.Setenv("PATH", "/nonexistent")
	_ = os.Setenv("HOME", tmpDir)
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Unsetenv("CLAI_DAEMON_PATH")

	if err := SpawnAndWait(50 * time.Millisecond); err == nil {
		t.Fatalf("SpawnAndWait() expected error when daemon binary is missing")
	}
}

func TestIsSocketDialable(t *testing.T) {
	oldQuickDial := quickDialFn
	t.Cleanup(func() { quickDialFn = oldQuickDial })

	quickDialFn = func() (io.Closer, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}
	if !isSocketDialable() {
		t.Fatalf("isSocketDialable() = false, want true")
	}

	quickDialFn = func() (io.Closer, error) {
		return nil, errors.New("dial failed")
	}
	if isSocketDialable() {
		t.Fatalf("isSocketDialable() = true, want false")
	}
}

func TestEnsureHealthySocket(t *testing.T) {
	t.Run("no socket", func(t *testing.T) {
		oldSocketExists := socketExistsFn
		t.Cleanup(func() { socketExistsFn = oldSocketExists })
		socketExistsFn = func() bool { return false }

		ready, err := ensureHealthySocket()
		if err != nil {
			t.Fatalf("ensureHealthySocket() error = %v", err)
		}
		if ready {
			t.Fatalf("ensureHealthySocket() ready = true, want false")
		}
	})

	t.Run("socket dialable", func(t *testing.T) {
		oldSocketExists := socketExistsFn
		oldQuickDial := quickDialFn
		t.Cleanup(func() {
			socketExistsFn = oldSocketExists
			quickDialFn = oldQuickDial
		})
		socketExistsFn = func() bool { return true }
		quickDialFn = func() (io.Closer, error) {
			return io.NopCloser(strings.NewReader("")), nil
		}

		ready, err := ensureHealthySocket()
		if err != nil {
			t.Fatalf("ensureHealthySocket() error = %v", err)
		}
		if !ready {
			t.Fatalf("ensureHealthySocket() ready = false, want true")
		}
	})

	t.Run("stale removal error", func(t *testing.T) {
		oldQuickDial := quickDialFn
		oldSocketExists := socketExistsFn
		oldSocketPath := socketPathFn
		oldRemove := removeFileFn
		oldDaemonLock := daemonLockFn
		oldAttempts := staleSocketDialAttempts
		oldDelay := staleSocketRetryDelay
		t.Cleanup(func() {
			quickDialFn = oldQuickDial
			socketExistsFn = oldSocketExists
			socketPathFn = oldSocketPath
			removeFileFn = oldRemove
			daemonLockFn = oldDaemonLock
			staleSocketDialAttempts = oldAttempts
			staleSocketRetryDelay = oldDelay
		})

		socketExistsFn = func() bool { return true }
		socketPathFn = func() string { return "/tmp/fake-clai.sock" }
		daemonLockFn = func() (int, bool, error) { return 0, false, nil }
		staleSocketDialAttempts = 1
		staleSocketRetryDelay = 0
		quickDialFn = func() (io.Closer, error) {
			return nil, errors.New("connection refused")
		}
		removeFileFn = func(path string) error { return fmt.Errorf("permission denied") }

		_, err := ensureHealthySocket()
		if err == nil || !strings.Contains(err.Error(), "failed to remove stale socket") {
			t.Fatalf("ensureHealthySocket() error = %v, want stale socket removal error", err)
		}
	})
}

func TestRecoverMissingSocketDaemon(t *testing.T) {
	t.Run("kill stale daemon when socket not published", func(t *testing.T) {
		oldSocketExists := socketExistsFn
		oldDaemonLock := daemonLockFn
		oldKill := killPIDFn
		oldQuickDial := quickDialFn
		t.Cleanup(func() {
			socketExistsFn = oldSocketExists
			daemonLockFn = oldDaemonLock
			killPIDFn = oldKill
			quickDialFn = oldQuickDial
		})

		socketExistsFn = func() bool { return false }
		daemonLockFn = func() (int, bool, error) { return 321, true, nil }
		quickDialFn = func() (io.Closer, error) { return nil, errors.New("dial failed") }

		killedPID := 0
		killPIDFn = func(pid int, timeout time.Duration) error {
			killedPID = pid
			return nil
		}

		recoverMissingSocketDaemon()
		if killedPID != 321 {
			t.Fatalf("killed PID = %d, want 321", killedPID)
		}
	})

	t.Run("skip kill when socket is published quickly", func(t *testing.T) {
		oldSocketExists := socketExistsFn
		oldDaemonLock := daemonLockFn
		oldKill := killPIDFn
		oldQuickDial := quickDialFn
		t.Cleanup(func() {
			socketExistsFn = oldSocketExists
			daemonLockFn = oldDaemonLock
			killPIDFn = oldKill
			quickDialFn = oldQuickDial
		})

		socketChecks := 0
		socketExistsFn = func() bool {
			socketChecks++
			return socketChecks > 1
		}
		daemonLockFn = func() (int, bool, error) { return 123, true, nil }
		quickDialFn = func() (io.Closer, error) { return io.NopCloser(strings.NewReader("")), nil }

		killCalls := 0
		killPIDFn = func(pid int, timeout time.Duration) error {
			killCalls++
			return nil
		}

		recoverMissingSocketDaemon()
		if killCalls != 0 {
			t.Fatalf("killPID calls = %d, want 0", killCalls)
		}
	})
}

func TestWaitWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitWithContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitWithContext() error = %v, want context.Canceled", err)
	}

	if err := waitWithContext(context.Background(), 1*time.Millisecond); err != nil {
		t.Fatalf("waitWithContext() error = %v, want nil", err)
	}
}

func TestEnsureSocketNotOwnedByLiveDaemon(t *testing.T) {
	oldDaemonLock := daemonLockFn
	t.Cleanup(func() { daemonLockFn = oldDaemonLock })

	daemonLockFn = nil
	if err := ensureSocketNotOwnedByLiveDaemon(errors.New("dial failed")); err != nil {
		t.Fatalf("ensureSocketNotOwnedByLiveDaemon() error = %v, want nil when no lock fn", err)
	}

	daemonLockFn = func() (int, bool, error) { return 77, true, nil }
	err := ensureSocketNotOwnedByLiveDaemon(errors.New("dial failed"))
	if err == nil || !strings.Contains(err.Error(), "lock is held") {
		t.Fatalf("ensureSocketNotOwnedByLiveDaemon() error = %v, want lock-held error", err)
	}
}

func TestIsLikelyStaleSocketDialError(t *testing.T) {
	if isLikelyStaleSocketDialError(nil) {
		t.Fatalf("expected false for nil error")
	}
	if !isLikelyStaleSocketDialError(syscall.ECONNREFUSED) {
		t.Fatalf("expected true for ECONNREFUSED")
	}
	if !isLikelyStaleSocketDialError(errors.New("transport: no such file or directory")) {
		t.Fatalf("expected true for ENOENT message")
	}
	if isLikelyStaleSocketDialError(errors.New("permission denied")) {
		t.Fatalf("expected false for non-stale dial error")
	}
}

func TestDaemonLockHeldPID_NoLockFile(t *testing.T) {
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		_ = os.Setenv("HOME", oldHome)
	})

	tmpDir := t.TempDir()
	_ = os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	_ = os.Setenv("HOME", tmpDir)

	pid, held, err := daemonLockHeldPID()
	if err != nil {
		t.Fatalf("daemonLockHeldPID() error = %v", err)
	}
	if held || pid != 0 {
		t.Fatalf("daemonLockHeldPID() = (pid=%d, held=%v), want (0,false)", pid, held)
	}
}

func TestTerminatePID(t *testing.T) {
	if err := terminatePID(0, 10*time.Millisecond); err != nil {
		t.Fatalf("terminatePID(0) error = %v, want nil", err)
	}

	if err := terminatePID(999999999, 10*time.Millisecond); err != nil {
		t.Fatalf("terminatePID(nonexistent) error = %v, want nil, got %v", err, err)
	}
}
