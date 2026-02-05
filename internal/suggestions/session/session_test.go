//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestSessionFilePath tests path resolution logic.
// These tests manipulate environment variables and cannot run in parallel.
func TestSessionFilePath(t *testing.T) {
	// Save and restore original env vars
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer func() {
		if origXDG != "" {
			os.Setenv("XDG_RUNTIME_DIR", origXDG)
		} else {
			os.Unsetenv("XDG_RUNTIME_DIR")
		}
	}()

	t.Run("XDG_RUNTIME_DIR takes priority", func(t *testing.T) {
		os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

		path := SessionFilePath(12345)
		expected := "/run/user/1000/clai/session.12345"

		if path != expected {
			t.Errorf("SessionFilePath(12345) = %q, want %q", path, expected)
		}
	})

	t.Run("fallback to /tmp when XDG not set", func(t *testing.T) {
		os.Unsetenv("XDG_RUNTIME_DIR")

		path := SessionFilePath(12345)
		uid := strconv.Itoa(os.Getuid())
		expected := "/tmp/clai-" + uid + "/session.12345"

		if path != expected {
			t.Errorf("SessionFilePath(12345) = %q, want %q", path, expected)
		}
	})

	t.Run("UID is included in fallback path", func(t *testing.T) {
		os.Unsetenv("XDG_RUNTIME_DIR")

		path := SessionFilePath(99999)
		uid := strconv.Itoa(os.Getuid())

		if !strings.Contains(path, uid) {
			t.Errorf("SessionFilePath() = %q, expected to contain UID %s", path, uid)
		}
	})

	t.Run("PID is included in path", func(t *testing.T) {
		pid := 54321
		path := SessionFilePath(pid)

		if !strings.HasSuffix(path, ".54321") {
			t.Errorf("SessionFilePath(%d) = %q, expected to end with .%d", pid, path, pid)
		}
	})
}

// withTempSessionDir sets up a temporary directory for session file tests
// and returns a cleanup function. It overrides the sessionFilePathFunc
// to use the temp directory.
func withTempSessionDir(t *testing.T) (tmpDir string, cleanup func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "clai-session-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Save original func
	origFunc := sessionFilePathFunc

	// Override to use temp dir
	sessionFilePathFunc = func(pid int) string {
		return filepath.Join(tmpDir, "clai", "session."+strconv.Itoa(pid))
	}

	cleanup = func() {
		sessionFilePathFunc = origFunc
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

// TestWriteSessionFile tests the writeSessionFile function.
func TestWriteSessionFile(t *testing.T) {
	t.Run("creates directory with correct permissions", func(t *testing.T) {
		tmpDir, cleanup := withTempSessionDir(t)
		defer cleanup()

		pid := 99999
		sessionID := "abc123def456"

		err := writeSessionFile(pid, sessionID)
		if err != nil {
			t.Fatalf("writeSessionFile() error = %v", err)
		}

		// Verify directory was created with correct permissions
		dir := filepath.Join(tmpDir, "clai")
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("directory should exist: %v", err)
		}

		if info.Mode().Perm() != 0700 {
			t.Errorf("directory permissions = %o, want 0700", info.Mode().Perm())
		}
	})

	t.Run("creates file with secure permissions", func(t *testing.T) {
		_, cleanup := withTempSessionDir(t)
		defer cleanup()

		pid := 88888
		sessionID := "testid12345678"

		err := writeSessionFile(pid, sessionID)
		if err != nil {
			t.Fatalf("writeSessionFile() error = %v", err)
		}

		path := SessionFilePath(pid)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("session file should exist: %v", err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
		}
	})
}

// TestReadWriteSessionFile tests round-trip read/write.
func TestReadWriteSessionFile(t *testing.T) {
	_, cleanup := withTempSessionDir(t)
	defer cleanup()

	pid := 77777
	sessionID := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"

	// Write
	err := writeSessionFile(pid, sessionID)
	if err != nil {
		t.Fatalf("writeSessionFile() error = %v", err)
	}

	// Read back
	readID, err := readSessionFile(pid)
	if err != nil {
		t.Fatalf("readSessionFile() error = %v", err)
	}

	if readID != sessionID {
		t.Errorf("readSessionFile() = %q, want %q", readID, sessionID)
	}
}

// TestReadSessionFile_NotExists verifies reading non-existent file returns empty.
func TestReadSessionFile_NotExists(t *testing.T) {
	_, cleanup := withTempSessionDir(t)
	defer cleanup()

	// Read non-existent file
	readID, err := readSessionFile(66666)
	if err != nil {
		t.Errorf("readSessionFile() should not error for missing file, got %v", err)
	}

	if readID != "" {
		t.Errorf("readSessionFile() = %q, want empty string for missing file", readID)
	}
}

// TestCleanupSessionFile verifies session file removal.
func TestCleanupSessionFile(t *testing.T) {
	_, cleanup := withTempSessionDir(t)
	defer cleanup()

	pid := 55555
	sessionID := "cleanup-test-id"

	// Create session file
	err := writeSessionFile(pid, sessionID)
	if err != nil {
		t.Fatalf("writeSessionFile() error = %v", err)
	}

	// Verify it exists
	path := SessionFilePath(pid)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should exist before cleanup: %v", err)
	}

	// Cleanup
	err = CleanupSessionFile(pid)
	if err != nil {
		t.Errorf("CleanupSessionFile() error = %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("session file should be removed after cleanup")
	}
}

// TestCleanupSessionFile_NotExists verifies cleanup of non-existent file doesn't error.
func TestCleanupSessionFile_NotExists(t *testing.T) {
	_, cleanup := withTempSessionDir(t)
	defer cleanup()

	// Cleanup non-existent file should not error
	err := CleanupSessionFile(44444)
	if err != nil {
		t.Errorf("CleanupSessionFile() for non-existent file should not error, got %v", err)
	}
}

// TestGenerateLocalSessionID tests local session ID generation.
func TestGenerateLocalSessionID(t *testing.T) {
	t.Parallel()

	t.Run("correct length", func(t *testing.T) {
		t.Parallel()

		sessionID := generateLocalSessionID()

		if len(sessionID) != SessionIDLength {
			t.Errorf("generateLocalSessionID() length = %d, want %d", len(sessionID), SessionIDLength)
		}
	})

	t.Run("is valid hex", func(t *testing.T) {
		t.Parallel()

		sessionID := generateLocalSessionID()

		for _, c := range sessionID {
			isDigit := c >= '0' && c <= '9'
			isHexLetter := c >= 'a' && c <= 'f'
			if !isDigit && !isHexLetter {
				t.Errorf("generateLocalSessionID() contains non-hex character: %c", c)
			}
		}
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		t.Parallel()

		ids := make(map[string]bool)
		const numIDs = 100

		for i := 0; i < numIDs; i++ {
			id := generateLocalSessionID()
			if ids[id] {
				t.Errorf("generateLocalSessionID() generated duplicate ID: %s", id)
			}
			ids[id] = true
		}
	})
}

// TestGenerateLocalSessionIDWithInputs tests deterministic generation.
func TestGenerateLocalSessionIDWithInputs(t *testing.T) {
	t.Parallel()

	t.Run("is deterministic", func(t *testing.T) {
		t.Parallel()

		hostname := "testhost"
		pid := 12345
		timestamp := int64(1700000000000)
		random := []byte("1234567890abcdef")

		id1 := GenerateLocalSessionIDWithInputs(hostname, pid, timestamp, random)
		id2 := GenerateLocalSessionIDWithInputs(hostname, pid, timestamp, random)

		if id1 != id2 {
			t.Errorf("GenerateLocalSessionIDWithInputs() not deterministic: %s != %s", id1, id2)
		}
	})

	t.Run("different inputs produce different IDs", func(t *testing.T) {
		t.Parallel()

		baseHostname := "testhost"
		basePid := 12345
		baseTimestamp := int64(1700000000000)
		baseRandom := []byte("1234567890abcdef")

		baseID := GenerateLocalSessionIDWithInputs(baseHostname, basePid, baseTimestamp, baseRandom)

		tests := []struct {
			name      string
			hostname  string
			pid       int
			timestamp int64
			random    []byte
		}{
			{"different hostname", "otherhost", basePid, baseTimestamp, baseRandom},
			{"different pid", baseHostname, 99999, baseTimestamp, baseRandom},
			{"different timestamp", baseHostname, basePid, baseTimestamp + 1, baseRandom},
			{"different random", baseHostname, basePid, baseTimestamp, []byte("abcdef1234567890")},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				id := GenerateLocalSessionIDWithInputs(tt.hostname, tt.pid, tt.timestamp, tt.random)
				if id == baseID {
					t.Errorf("GenerateLocalSessionIDWithInputs() with %s should produce different ID", tt.name)
				}
			})
		}
	})

	t.Run("correct length", func(t *testing.T) {
		t.Parallel()

		id := GenerateLocalSessionIDWithInputs("host", 1, 1, []byte("rand"))

		if len(id) != SessionIDLength {
			t.Errorf("GenerateLocalSessionIDWithInputs() length = %d, want %d", len(id), SessionIDLength)
		}
	})
}

// TestGetSessionID tests the main GetSessionID function.
func TestGetSessionID(t *testing.T) {
	t.Run("reads existing file", func(t *testing.T) {
		_, cleanup := withTempSessionDir(t)
		defer cleanup()

		// Pre-create session file
		pid := os.Getpid()
		existingID := "existing-session-id-12345678"
		err := writeSessionFile(pid, existingID)
		if err != nil {
			t.Fatalf("failed to write existing session file: %v", err)
		}

		// GetSessionID should return existing ID
		sessionID, err := GetSessionID(nil)
		if err != nil {
			t.Errorf("GetSessionID() error = %v", err)
		}

		if sessionID != existingID {
			t.Errorf("GetSessionID() = %q, want %q (existing ID)", sessionID, existingID)
		}
	})

	t.Run("generates new ID when file doesn't exist", func(t *testing.T) {
		_, cleanup := withTempSessionDir(t)
		defer cleanup()

		// GetSessionID should generate new ID
		sessionID, err := GetSessionID(nil)
		if err != nil {
			t.Errorf("GetSessionID() error = %v", err)
		}

		if sessionID == "" {
			t.Error("GetSessionID() returned empty session ID")
		}

		if len(sessionID) != SessionIDLength {
			t.Errorf("GetSessionID() length = %d, want %d", len(sessionID), SessionIDLength)
		}
	})

	t.Run("writes new ID to file", func(t *testing.T) {
		_, cleanup := withTempSessionDir(t)
		defer cleanup()

		// Generate new ID
		sessionID, err := GetSessionID(nil)
		if err != nil {
			t.Fatalf("GetSessionID() error = %v", err)
		}

		// Verify it was written to file
		readID, err := readSessionFile(os.Getpid())
		if err != nil {
			t.Fatalf("readSessionFile() error = %v", err)
		}

		if readID != sessionID {
			t.Errorf("written session ID = %q, want %q", readID, sessionID)
		}
	})
}

// TestSessionIDConstant verifies SessionIDLength is in valid range (16-32).
func TestSessionIDConstant(t *testing.T) {
	t.Parallel()

	if SessionIDLength < 16 || SessionIDLength > 32 {
		t.Errorf("SessionIDLength = %d, should be 16-32", SessionIDLength)
	}
}

// TestConcurrentGetSessionID tests that GetSessionID returns consistent IDs
// when the session file already exists (simulating subsequent calls after initial setup).
func TestConcurrentGetSessionID(t *testing.T) {
	_, cleanup := withTempSessionDir(t)
	defer cleanup()

	// First, establish a session ID by writing it to file (simulates shell startup)
	pid := os.Getpid()
	initialID := "established-session-id-abc123"
	err := writeSessionFile(pid, initialID)
	if err != nil {
		t.Fatalf("failed to write initial session file: %v", err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	ids := make(chan string, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			id, err := GetSessionID(nil)
			if err != nil {
				errors <- err
				return
			}
			ids <- id
		}()
	}

	wg.Wait()
	close(ids)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent GetSessionID() error: %v", err)
	}

	// All IDs should be the same as the initially established ID
	for id := range ids {
		if id != initialID {
			t.Errorf("concurrent GetSessionID() returned wrong ID: got %q, want %q", id, initialID)
		}
	}
}
