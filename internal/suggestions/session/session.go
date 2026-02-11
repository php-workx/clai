// Package session provides session ID generation and caching for the clai suggestions engine.
// Session IDs are used to track shell sessions across command events.
//
// See spec Section 6.5 for details on session ID strategies.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runger/clai/internal/suggestions/transport"
)

const (
	// SessionIDLength is the length of generated session IDs in hex characters (16-32 range).
	SessionIDLength = 32
)

// sessionFilePathFunc is the function used to resolve session file paths.
// It can be overridden in tests.
var sessionFilePathFunc = defaultSessionFilePath

// GetSessionID returns a session ID for the current shell process.
// It first tries to read an existing session ID from the session file.
// If not found, it attempts Strategy A (daemon-assigned via transport),
// falling back to Strategy B (local generation) if the daemon is unavailable.
//
// The transport parameter can be nil to skip daemon communication and use local generation.
func GetSessionID(t transport.Transport) (string, error) {
	pid := os.Getpid()

	// Try to read existing session ID from file
	if sessionID, err := readSessionFile(pid); err == nil && sessionID != "" {
		return sessionID, nil
	}

	var sessionID string

	// Strategy A: Request from daemon (if transport available)
	if t != nil {
		var err error
		sessionID, err = requestDaemonSessionID(t)
		if err == nil && sessionID != "" {
			// Successfully got session ID from daemon, write to file
			// Ignore write error - session file is optional, we still have a valid session ID
			_ = writeSessionFile(pid, sessionID)
			return sessionID, nil
		}
		// Fall through to Strategy B if daemon unavailable
	}

	// Strategy B: Generate locally
	sessionID = generateLocalSessionID()

	// Write to session file for future reads
	// Ignore write error - session file is optional
	_ = writeSessionFile(pid, sessionID)

	return sessionID, nil
}

// generateLocalSessionID generates a session ID locally using SHA256.
// The ID is derived from: hostname + PID + timestamp + random bytes + container fingerprint.
// This provides a stable, unique identifier per shell instance.
// Random seed uses 16 bytes (128 bits) from crypto/rand, exceeding the 64-bit minimum.
func generateLocalSessionID() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	pid := os.Getpid()
	timestamp := time.Now().UnixNano()

	// Add random bytes for uniqueness (128 bits from crypto/rand)
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to less random but still unique data
		randomBytes = []byte(fmt.Sprintf("%d%d", timestamp, pid))
	}

	// Add container fingerprint for disambiguation in containerized environments
	containerFP := containerFingerprint()

	// Combine all components
	input := fmt.Sprintf("%s|%d|%d|%s|%s", hostname, pid, timestamp, hex.EncodeToString(randomBytes), containerFP)

	// Hash with SHA256
	hash := sha256.Sum256([]byte(input))

	// Return first SessionIDLength/2 bytes as hex (SessionIDLength hex chars)
	return hex.EncodeToString(hash[:SessionIDLength/2])
}

// containerFingerprint returns a string identifying the container environment,
// or an empty string if not running in a container. This helps disambiguate
// session IDs when hostname and PID may collide across containers.
func containerFingerprint() string {
	if fp, ok := dockerFingerprint(); ok {
		return fp
	}
	if fp, ok := kubernetesFingerprint(); ok {
		return fp
	}
	return genericContainerFingerprint()
}

func dockerFingerprint() (string, bool) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return "", false
	}
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		if id := extractContainerIDFromCgroup(string(data)); id != "" {
			return "docker:" + id, true
		}
	}
	return "docker:unknown", true
}

func extractContainerIDFromCgroup(cgroup string) string {
	lines := strings.Split(cgroup, "\n")
	for _, line := range lines {
		if idx := strings.LastIndex(line, "/"); idx >= 0 {
			id := line[idx+1:]
			if len(id) >= 12 {
				return id[:12]
			}
		}
	}
	return ""
}

func kubernetesFingerprint() (string, bool) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return "", false
	}
	podName := os.Getenv("HOSTNAME")
	if podName == "" {
		podName = "unknown"
	}
	return "k8s:" + podName, true
}

func genericContainerFingerprint() string {
	if val := os.Getenv("container"); val != "" {
		return "container:" + val
	}
	return ""
}

// GenerateLocalSessionIDWithInputs generates a session ID from specific inputs.
// This is exposed for testing to allow deterministic generation.
func GenerateLocalSessionIDWithInputs(hostname string, pid int, timestamp int64, random []byte) string {
	input := fmt.Sprintf("%s|%d|%d|%s", hostname, pid, timestamp, hex.EncodeToString(random))
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:SessionIDLength/2])
}

// requestDaemonSessionID attempts to get a session ID from the daemon.
// This is Strategy A from the spec - daemon-assigned session IDs.
// Returns empty string and error if daemon is not available.
func requestDaemonSessionID(t transport.Transport) (string, error) {
	// Try to connect with a short timeout (15ms as per spec)
	conn, err := t.Dial(15 * time.Millisecond)
	if err != nil {
		return "", fmt.Errorf("daemon not available: %w", err)
	}
	defer conn.Close()

	// TODO: Implement the actual protocol for requesting session ID from daemon
	// For now, return empty to trigger fallback to local generation
	// The daemon would need to implement a "session-start" handler that returns
	// a unique session ID.
	return "", fmt.Errorf("daemon session ID request not yet implemented")
}

// SessionFilePath returns the path to the session file for the given PID.
// Path follows spec Section 6.5:
//   - $XDG_RUNTIME_DIR/clai/session.$PID (preferred)
//   - /tmp/clai-$UID/session.$PID (fallback)
func SessionFilePath(pid int) string {
	return sessionFilePathFunc(pid)
}

// defaultSessionFilePath is the default implementation of session file path resolution.
func defaultSessionFilePath(pid int) string {
	// Priority 1: XDG_RUNTIME_DIR
	if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
		return filepath.Join(xdgRuntime, "clai", fmt.Sprintf("session.%d", pid))
	}

	// Priority 2: /tmp with UID
	uid := strconv.Itoa(os.Getuid())
	return filepath.Join("/tmp", "clai-"+uid, fmt.Sprintf("session.%d", pid))
}

// readSessionFile reads the session ID from the session file for the given PID.
// Returns empty string and nil error if file doesn't exist.
// Returns error only for actual read failures.
func readSessionFile(pid int) (string, error) {
	path := SessionFilePath(pid)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read session file: %w", err)
	}

	sessionID := string(data)
	if sessionID == "" {
		return "", nil
	}

	return sessionID, nil
}

// writeSessionFile writes the session ID to the session file for the given PID.
// Creates the parent directory with 0700 permissions if it doesn't exist.
func writeSessionFile(pid int, sessionID string) error {
	path := SessionFilePath(pid)

	// Ensure parent directory exists with secure permissions (0700)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	// Write session file with secure permissions (0600)
	if err := os.WriteFile(path, []byte(sessionID), 0600); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

// CleanupSessionFile removes the session file for the given PID.
// This should be called when the shell session ends.
func CleanupSessionFile(pid int) error {
	path := SessionFilePath(pid)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove session file: %w", err)
	}

	return nil
}
