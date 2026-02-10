package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/runger/clai/internal/config"
)

// ReloadFunc is a function called on SIGHUP to reload configuration.
type ReloadFunc func() error

// Run starts the daemon and blocks until shutdown.
// It handles signals for lifecycle management:
//   - SIGTERM/SIGINT: graceful shutdown (drain queues, close DB, remove lock file)
//   - SIGHUP: reload configuration from disk
//   - SIGUSR1: graceful re-exec (exec self with same args after cleanup)
//   - SIGPIPE: ignore (prevent crashes on broken pipe)
func Run(ctx context.Context, cfg *ServerConfig) error {
	// Check privilege safety
	if err := CheckNotRoot(); err != nil {
		return err
	}

	// Validate and ensure secure directory permissions
	paths := cfg.Paths
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if err := EnsureSecureDirectory(paths.BaseDir); err != nil {
		return fmt.Errorf("failed to ensure secure base directory: %w", err)
	}

	// Acquire lock file to prevent double-start
	lockPath := LockFilePath(paths.BaseDir)
	lockFile := NewLockFile(lockPath)
	if err := lockFile.Acquire(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer lockFile.Release()

	server, err := NewServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Create context that cancels on signals
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ignore SIGPIPE to prevent crash on broken pipe
	signal.Ignore(syscall.SIGPIPE)

	// Handle signals
	sigChan := make(chan os.Signal, 4)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGUSR1)
	defer signal.Stop(sigChan)

	go func() {
		for {
			select {
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGTERM, syscall.SIGINT:
					server.logger.Info("received shutdown signal", "signal", sig)
					server.Shutdown()
					cancel()
					return

				case syscall.SIGHUP:
					server.logger.Info("received SIGHUP, reloading configuration")
					if cfg.ReloadFn != nil {
						if err := cfg.ReloadFn(); err != nil {
							server.logger.Error("failed to reload configuration", "error", err)
						} else {
							server.logger.Info("configuration reloaded successfully")
						}
					} else {
						server.logger.Debug("no reload function configured, ignoring SIGHUP")
					}

				case syscall.SIGUSR1:
					server.logger.Info("received SIGUSR1, initiating graceful re-exec")
					server.Shutdown()
					lockFile.Release()
					reExec()
					// reExec calls syscall.Exec which replaces the process,
					// so we should not reach here on success.
					// If we do reach here, it means re-exec failed.
					server.logger.Error("re-exec failed, shutting down")
					cancel()
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// Start server (blocking)
	return server.Start(ctx)
}

// reExec replaces the current process with a fresh copy of itself.
func reExec() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// syscall.Exec replaces the current process
	_ = syscall.Exec(exe, os.Args, os.Environ())
}

// IsRunning checks if the daemon is currently running.
func IsRunning() bool {
	return IsRunningWithPaths(config.DefaultPaths())
}

// IsRunningWithPaths checks if the daemon is running using the given paths.
func IsRunningWithPaths(paths *config.Paths) bool {
	pid, err := ReadPID(paths.PIDFile())
	if err != nil {
		// PID file missing/stale; fall through to lock-based detection.
		pid = 0
	}

	// Check if process exists
	if pid > 0 {
		process, err := os.FindProcess(pid)
		if err == nil {
			// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
			if process.Signal(syscall.Signal(0)) == nil {
				return true
			}
		}
	}

	// If the PID file is wrong, fall back to the held lock PID. This handles
	// cases where the daemon is alive but the PID file was overwritten by a
	// failed spawn attempt.
	lockPID, held, err := ReadHeldPID(LockFilePath(paths.BaseDir))
	if err != nil || !held || lockPID <= 0 {
		return false
	}

	process, err := os.FindProcess(lockPID)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// ReadPID reads the PID from the PID file.
func ReadPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID: %w", err)
	}

	return pid, nil
}

// Stop stops the running daemon by sending SIGTERM.
func Stop() error {
	return StopWithPaths(config.DefaultPaths())
}

// StopWithPaths stops the running daemon using the given paths.
func StopWithPaths(paths *config.Paths) error {
	pid, err := ReadPID(paths.PIDFile())
	if err != nil || pid <= 0 {
		pid = 0
	}

	// If PID file is stale, use the held lock PID.
	if pid > 0 {
		if proc, ferr := os.FindProcess(pid); ferr == nil {
			if proc.Signal(syscall.Signal(0)) != nil {
				pid = 0
			}
		} else {
			pid = 0
		}
	}
	if pid == 0 {
		lockPID, held, lerr := ReadHeldPID(LockFilePath(paths.BaseDir))
		if lerr != nil {
			return fmt.Errorf("failed to read PID and lock PID: %w", lerr)
		}
		if !held || lockPID <= 0 {
			return fmt.Errorf("daemon not running")
		}
		pid = lockPID
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit (with timeout)
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Force kill if graceful shutdown didn't work
			process.Kill()
			return nil
		case <-ticker.C:
			// Check if process is still running
			if err := process.Signal(syscall.Signal(0)); err != nil {
				// Process is gone
				return nil
			}
		}
	}
}

// CleanupStale removes stale socket and PID files.
// Call this when the daemon is known to not be running.
func CleanupStale() error {
	return CleanupStaleWithPaths(config.DefaultPaths())
}

// CleanupStaleWithPaths removes stale files using the given paths.
func CleanupStaleWithPaths(paths *config.Paths) error {
	// Only cleanup if daemon is not running
	if IsRunningWithPaths(paths) {
		return fmt.Errorf("daemon is still running")
	}

	// Remove socket
	socketPath := paths.SocketFile()
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket: %w", err)
	}

	// Remove PID file
	pidPath := paths.PIDFile()
	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}

	return nil
}

// WaitForSocket waits for the daemon socket to become available.
// Returns an error if the socket doesn't become available within the timeout.
func WaitForSocket(timeout time.Duration) error {
	return WaitForSocketWithPaths(config.DefaultPaths(), timeout)
}

// WaitForSocketWithPaths waits for the socket using the given paths.
func WaitForSocketWithPaths(paths *config.Paths, timeout time.Duration) error {
	return WaitForSocketWithContext(context.Background(), paths, timeout)
}

// WaitForSocketWithContext waits for the socket using context for cancellation.
func WaitForSocketWithContext(ctx context.Context, paths *config.Paths, timeout time.Duration) error {
	socketPath := paths.SocketFile()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("socket not available after %v", timeout)
			}
			return ctx.Err()
		default:
			if _, err := os.Stat(socketPath); err == nil {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("socket not available after %v", timeout)
			}
			return ctx.Err()
		case <-ticker.C:
			// Continue to next iteration
		}
	}
}
