package ipc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// PidPath returns the path to the daemon PID file
func PidPath() string {
	return filepath.Join(RunDir(), "clai.pid")
}

// LogPath returns the path to the daemon log file
func LogPath() string {
	return filepath.Join(RunDir(), "clai.log")
}

// DaemonBinaryName is the name of the daemon executable
const DaemonBinaryName = "claid"

// EnsureDaemon ensures the daemon is running, spawning it if necessary.
// Returns nil if daemon is available, error otherwise.
func EnsureDaemon() error {
	// Fast path: socket exists and is connectable
	if SocketExists() {
		conn, err := QuickDial()
		if err == nil {
			conn.Close()
			return nil
		}
		// Socket exists but can't connect - might be stale
		// Remove it and try to spawn
		_ = os.Remove(SocketPath())
	}

	// Try to spawn the daemon
	return SpawnDaemon()
}

// SpawnDaemon starts the daemon process in the background.
// It does not wait for the daemon to be ready.
func SpawnDaemon() error {
	return SpawnDaemonContext(context.Background())
}

// SpawnDaemonContext starts the daemon process in the background and supports
// cancellation before process creation.
func SpawnDaemonContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Ensure run directory exists
	if err := os.MkdirAll(RunDir(), 0755); err != nil {
		return fmt.Errorf("failed to create run dir: %w", err)
	}

	if err := removeStaleSocket(); err != nil {
		return err
	}

	// Find daemon binary
	daemonPath, err := findDaemonBinary()
	if err != nil {
		return err
	}

	// Create log file
	logFile, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Log file creation failed, use /dev/null
		logFile, _ = os.Open(os.DevNull)
	}
	defer logFile.Close()

	// Start daemon process
	cmd := exec.Command(daemonPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Detach from parent process group (platform-specific)
	setProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Write PID file (non-fatal if it fails)
	_ = os.WriteFile(PidPath(), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)

	// Detach from child - let it run independently
	// We don't call cmd.Wait() so the process continues after shim exits

	return nil
}

// SpawnAndWait spawns the daemon and waits for it to become available.
// timeout specifies how long to wait for the daemon to start.
func SpawnAndWait(timeout time.Duration) error {
	return SpawnAndWaitContext(context.Background(), timeout)
}

// SpawnAndWaitContext spawns the daemon and waits for readiness with
// cancellation support.
func SpawnAndWaitContext(ctx context.Context, timeout time.Duration) error {
	if err := SpawnDaemonContext(ctx); err != nil {
		return err
	}

	// Wait for socket to become available
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("daemon did not start within %v", timeout)
		case <-ticker.C:
			if SocketExists() {
				conn, err := QuickDial()
				if err == nil {
					conn.Close()
					return nil
				}
			}
		}
	}
}

// findDaemonBinary locates the daemon executable
func findDaemonBinary() (string, error) {
	// Check CLAI_DAEMON_PATH environment variable
	if path := os.Getenv("CLAI_DAEMON_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Check same directory as current executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		daemonPath := filepath.Join(dir, DaemonBinaryName)
		if _, err := os.Stat(daemonPath); err == nil {
			return daemonPath, nil
		}
	}

	// Check PATH
	if path, err := exec.LookPath(DaemonBinaryName); err == nil {
		return path, nil
	}

	// Check common install locations
	commonPaths := []string{
		"/usr/local/bin/" + DaemonBinaryName,
		"/usr/bin/" + DaemonBinaryName,
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		commonPaths = append(commonPaths,
			filepath.Join(home, ".local", "bin", DaemonBinaryName),
			filepath.Join(home, "go", "bin", DaemonBinaryName),
		)
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("daemon binary '%s' not found", DaemonBinaryName)
}

// IsDaemonRunning checks if the daemon process is running
func IsDaemonRunning() bool {
	if !SocketExists() {
		return false
	}

	conn, err := QuickDial()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func removeStaleSocket() error {
	if !SocketExists() {
		return nil
	}
	conn, err := QuickDial()
	if err == nil {
		conn.Close()
		return nil
	}
	if err := os.Remove(SocketPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}
	return nil
}
