package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSocketPath(t *testing.T) {
	// Test default path
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".clai", "run", "clai.sock")

	path := SocketPath()
	if path != expected {
		t.Errorf("SocketPath() = %q, want %q", path, expected)
	}
}

func TestSocketPathEnvOverride(t *testing.T) {
	// Set environment variable
	customPath := "/tmp/custom-clai.sock"
	os.Setenv("CLAI_SOCKET", customPath)
	defer os.Unsetenv("CLAI_SOCKET")

	path := SocketPath()
	if path != customPath {
		t.Errorf("SocketPath() with CLAI_SOCKET = %q, want %q", path, customPath)
	}
}

func TestRunDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".clai", "run")

	dir := RunDir()
	if dir != expected {
		t.Errorf("RunDir() = %q, want %q", dir, expected)
	}
}

func TestSocketExists(t *testing.T) {
	// Test with non-existent socket
	os.Setenv("CLAI_SOCKET", "/tmp/nonexistent-clai-test.sock")
	defer os.Unsetenv("CLAI_SOCKET")

	if SocketExists() {
		t.Error("SocketExists() = true for non-existent socket")
	}

	// Create a temporary file to act as socket
	tmpFile, err := os.CreateTemp("", "clai-test-sock-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	os.Setenv("CLAI_SOCKET", tmpFile.Name())

	if !SocketExists() {
		t.Error("SocketExists() = false for existing file")
	}
}

func TestDialNonExistentSocket(t *testing.T) {
	os.Setenv("CLAI_SOCKET", "/tmp/nonexistent-clai-test.sock")
	defer os.Unsetenv("CLAI_SOCKET")

	conn, err := QuickDial()
	if err == nil {
		conn.Close()
		t.Error("QuickDial() succeeded for non-existent socket")
	}
}

func TestDialTimeout(t *testing.T) {
	// Verify timeout constants are reasonable
	if FireAndForgetTimeout <= 0 {
		t.Error("FireAndForgetTimeout should be positive")
	}

	if SuggestTimeout <= 0 {
		t.Error("SuggestTimeout should be positive")
	}

	if InteractiveTimeout <= 0 {
		t.Error("InteractiveTimeout should be positive")
	}

	if DialTimeout <= 0 {
		t.Error("DialTimeout should be positive")
	}

	// Fire-and-forget should be shorter than suggestion timeout
	if FireAndForgetTimeout >= SuggestTimeout {
		t.Error("FireAndForgetTimeout should be less than SuggestTimeout")
	}

	// Suggestion timeout should be shorter than interactive timeout
	if SuggestTimeout >= InteractiveTimeout {
		t.Error("SuggestTimeout should be less than InteractiveTimeout")
	}
}
