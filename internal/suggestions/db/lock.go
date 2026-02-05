package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrLockAcquireFailed is returned when the daemon lock cannot be acquired.
var ErrLockAcquireFailed = errors.New("failed to acquire daemon lock")

// ErrLockTimeout is returned when the lock cannot be acquired within the timeout.
var ErrLockTimeout = errors.New("lock acquisition timed out")

// LockFile represents an advisory file lock used to prevent concurrent
// daemon starts. The lock is held for the lifetime of the daemon.
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

// AcquireLock attempts to acquire an exclusive advisory lock on the lock file.
// The lock prevents concurrent daemon starts and ensures only one process
// runs migrations at a time.
//
// The caller must call Release() when done with the lock.
func AcquireLock(dbDir string, opts LockOptions) (*LockFile, error) {
	lockPath := LockPath(dbDir)

	// Ensure the directory exists
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Set default retry interval
	if opts.RetryInterval == 0 {
		opts.RetryInterval = 100 * time.Millisecond
	}

	deadline := time.Now().Add(opts.Timeout)

	for {
		lf, err := tryAcquireLock(lockPath)
		if err == nil {
			return lf, nil
		}

		// Check if this is a "would block" error
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %v", ErrLockAcquireFailed, err)
		}

		// If no timeout, fail immediately
		if opts.Timeout == 0 {
			return nil, fmt.Errorf("%w: lock held by another process", ErrLockAcquireFailed)
		}

		// Check if we've exceeded the timeout
		if time.Now().After(deadline) {
			return nil, ErrLockTimeout
		}

		// Wait and retry
		time.Sleep(opts.RetryInterval)
	}
}

// tryAcquireLock makes a single attempt to acquire the lock.
func tryAcquireLock(lockPath string) (*LockFile, error) {
	// Open or create the lock file
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire an exclusive lock (non-blocking)
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		return nil, err
	}

	// Write our PID to the file for debugging
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to truncate lock file: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to seek lock file: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to write PID to lock file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
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

	// Release the lock
	if err := syscall.Flock(int(lf.file.Fd()), syscall.LOCK_UN); err != nil {
		// Continue with cleanup even if unlock fails
		_ = lf.file.Close()
		lf.file = nil
		return fmt.Errorf("failed to release lock: %w", err)
	}

	// Close the file
	if err := lf.file.Close(); err != nil {
		lf.file = nil
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	lf.file = nil

	// Remove the lock file (best effort)
	_ = os.Remove(lf.path)

	return nil
}

// Path returns the path to the lock file.
func (lf *LockFile) Path() string {
	return lf.path
}

// IsLocked checks if a lock is currently held on the given database directory.
// This is useful for status commands to report if the daemon is running.
func IsLocked(dbDir string) bool {
	lockPath := LockPath(dbDir)

	file, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
	if err != nil {
		return false
	}
	defer file.Close()

	// Try to acquire lock non-blocking
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Could not acquire - someone else has it
		return true
	}

	// We got the lock - release it immediately
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

// GetLockHolderPID attempts to read the PID from the lock file.
// Returns 0 if the PID cannot be determined.
func GetLockHolderPID(dbDir string) int {
	lockPath := LockPath(dbDir)

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
