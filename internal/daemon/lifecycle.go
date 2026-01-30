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

// Run starts the daemon and blocks until shutdown.
// It handles SIGTERM and SIGINT for graceful shutdown.
func Run(ctx context.Context, cfg *ServerConfig) error {
	server, err := NewServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Create context that cancels on signals
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		select {
		case sig := <-sigChan:
			server.logger.Info("received signal", "signal", sig)
			server.Shutdown()
			cancel()
		case <-ctx.Done():
			return
		}
	}()

	// Start server (blocking)
	return server.Start(ctx)
}

// IsRunning checks if the daemon is currently running.
func IsRunning() bool {
	return IsRunningWithPaths(config.DefaultPaths())
}

// IsRunningWithPaths checks if the daemon is running using the given paths.
func IsRunningWithPaths(paths *config.Paths) bool {
	pid, err := ReadPID(paths.PIDFile())
	if err != nil {
		return false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	err = process.Signal(syscall.Signal(0))
	return err == nil
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
	if err != nil {
		return fmt.Errorf("failed to read PID: %w", err)
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
	socketPath := paths.SocketFile()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("socket not available after %v", timeout)
}
