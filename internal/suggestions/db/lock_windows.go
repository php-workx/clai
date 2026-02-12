//go:build windows

package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

const windowsStillActive = 259

// ErrLockAcquireFailed is returned when the daemon lock cannot be acquired.
var ErrLockAcquireFailed = errors.New("failed to acquire daemon lock")

// ErrLockTimeout is returned when the lock cannot be acquired within the timeout.
var ErrLockTimeout = errors.New("lock acquisition timed out")

// LockFile represents an advisory file lock used to prevent concurrent daemon starts.
type LockFile struct {
	path string
	file *os.File
}

// LockOptions configures lock acquisition behavior.
type LockOptions struct {
	// Timeout is the maximum time to wait for the lock.
	// If zero, the lock attempt is non-blocking.
	Timeout time.Duration

	// RetryInterval is how often to retry acquiring the lock.
	// If zero, defaults to 100ms.
	RetryInterval time.Duration
}

// DefaultLockOptions returns sensible default options.
func DefaultLockOptions() LockOptions {
	return LockOptions{
		Timeout:       5 * time.Second,
		RetryInterval: 100 * time.Millisecond,
	}
}

// LockPath returns the path to the daemon lock file for a given database directory.
func LockPath(dbDir string) string {
	return filepath.Join(dbDir, ".daemon.lock")
}

// AcquireLock attempts to acquire an exclusive lock by atomically creating
// the lock file.
func AcquireLock(dbDir string, opts LockOptions) (*LockFile, error) {
	lockPath := LockPath(dbDir)

	// Ensure the directory exists.
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	if opts.RetryInterval == 0 {
		opts.RetryInterval = 100 * time.Millisecond
	}

	deadline := time.Now().Add(opts.Timeout)

	for {
		lf, err := tryAcquireLock(lockPath)
		if err == nil {
			return lf, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %w", ErrLockAcquireFailed, err)
		}

		holderPID := readLockPID(lockPath)
		if holderPID > 0 && !processExists(holderPID) {
			// Best-effort stale lock cleanup.
			_ = os.Remove(lockPath)
			continue
		}

		if opts.Timeout == 0 {
			return nil, fmt.Errorf("%w: lock held by another process", ErrLockAcquireFailed)
		}
		if time.Now().After(deadline) {
			return nil, ErrLockTimeout
		}

		time.Sleep(opts.RetryInterval)
	}
}

// tryAcquireLock makes a single attempt to acquire the lock.
func tryAcquireLock(lockPath string) (*LockFile, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, os.ErrExist
		}
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Write our PID to the file for diagnostics.
	if err := file.Truncate(0); err != nil {
		file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to truncate lock file: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to seek lock file: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to write PID to lock file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to sync lock file: %w", err)
	}

	return &LockFile{
		path: lockPath,
		file: file,
	}, nil
}

// Release releases the lock and removes the lock file.
// It is safe to call Release multiple times.
func (lf *LockFile) Release() error {
	if lf.file == nil {
		return nil
	}

	if err := lf.file.Close(); err != nil {
		lf.file = nil
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	lf.file = nil
	_ = os.Remove(lf.path)
	return nil
}

// Path returns the path to the lock file.
func (lf *LockFile) Path() string {
	return lf.path
}

// IsLocked checks if a lock is currently held on the given database directory.
func IsLocked(dbDir string) bool {
	lockPath := LockPath(dbDir)
	if _, err := os.Stat(lockPath); err != nil {
		return false
	}

	pid := readLockPID(lockPath)
	if pid > 0 && !processExists(pid) {
		// Best-effort stale lock cleanup.
		_ = os.Remove(lockPath)
		return false
	}
	return true
}

// GetLockHolderPID attempts to read the PID from the lock file.
// Returns 0 if the PID cannot be determined.
func GetLockHolderPID(dbDir string) int {
	return readLockPID(LockPath(dbDir))
}

func readLockPID(lockPath string) int {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0
	}
	return pid
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == windowsStillActive
}
