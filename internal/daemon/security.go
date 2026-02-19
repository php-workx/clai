package daemon

import (
	"fmt"
	"os"
	"runtime"
)

// ErrRunningAsRoot is returned when the daemon detects it is running as root.
var ErrRunningAsRoot = fmt.Errorf("refusing to run as root (UID 0): running the clai daemon as root is a security risk")

// ErrInsecureDirectory is returned when a runtime directory has insecure permissions.
var ErrInsecureDirectory = fmt.Errorf("runtime directory has insecure permissions")

// CheckNotRoot verifies the daemon is not running as root (effective UID 0).
// Returns nil if not root, ErrRunningAsRoot if running with effective root privileges.
// On Windows, this check is skipped.
func CheckNotRoot() error {
	if runtime.GOOS == "windows" {
		return nil
	}

	if os.Geteuid() == 0 {
		return ErrRunningAsRoot
	}

	return nil
}

// ValidateDirectoryPermissions checks that the given directory has secure permissions.
// On Unix-like systems, the directory must exist and be exactly mode 0o700
// (owner read/write/execute only).
// Returns nil if permissions are acceptable, error otherwise.
func ValidateDirectoryPermissions(dirPath string) error {
	if runtime.GOOS == "windows" {
		return nil // Windows uses ACLs, not Unix permissions
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist yet - will be created with correct perms
		}
		return fmt.Errorf("failed to stat directory %s: %w", dirPath, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dirPath)
	}

	perm := info.Mode().Perm()
	if perm != 0o700 {
		return fmt.Errorf("%w: %s has mode %o; expected exactly 0700",
			ErrInsecureDirectory, dirPath, perm)
	}

	return nil
}

// EnsureSecureDirectory creates a directory with mode 0o700 if it doesn't exist,
// or validates permissions if it does exist.
func EnsureSecureDirectory(dirPath string) error {
	if runtime.GOOS == "windows" {
		return os.MkdirAll(dirPath, 0o700)
	}

	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		// Create with secure permissions
		return os.MkdirAll(dirPath, 0o700)
	}
	if err != nil {
		return fmt.Errorf("failed to stat directory %s: %w", dirPath, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s exists but is not a directory", dirPath)
	}

	// Fix permissions if too open
	perm := info.Mode().Perm()
	if perm != 0o700 {
		if err := os.Chmod(dirPath, 0o700); err != nil { //nolint:gosec // G302: 0700 is appropriate for daemon runtime directory
			return fmt.Errorf("failed to fix permissions on %s: %w", dirPath, err)
		}
	}

	return nil
}
