package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPidPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	expected := filepath.Join(home, ".clai", "run", "clai.pid")

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
	expected := filepath.Join(home, ".clai", "run", "clai.log")

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
