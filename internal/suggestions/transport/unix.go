//go:build !windows

// Package transport provides IPC transport abstractions for the clai suggestions daemon.
package transport

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// UnixTransport implements Transport using Unix domain sockets.
type UnixTransport struct {
	socketPath string
	listener   net.Listener
	mu         sync.Mutex
}

// NewUnixTransport creates a new Unix socket transport.
// If socketPath is empty, it uses the default path resolution:
//  1. $XDG_RUNTIME_DIR/clai/daemon.sock (preferred)
//  2. $TMPDIR/clai-$UID/daemon.sock
//  3. /tmp/clai-$UID/daemon.sock (fallback)
func NewUnixTransport(socketPath string) *UnixTransport {
	if socketPath == "" {
		socketPath = DefaultUnixSocketPath()
	}
	return &UnixTransport{
		socketPath: socketPath,
	}
}

// DefaultUnixSocketPath returns the default socket path following XDG and security conventions.
func DefaultUnixSocketPath() string {
	// Priority 1: XDG_RUNTIME_DIR
	if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
		return filepath.Join(xdgRuntime, "clai", "daemon.sock")
	}

	uid := strconv.Itoa(os.Getuid())

	// Priority 2: TMPDIR
	if tmpdir := os.Getenv("TMPDIR"); tmpdir != "" {
		return filepath.Join(tmpdir, "clai-"+uid, "daemon.sock")
	}

	// Priority 3: /tmp fallback
	return filepath.Join("/tmp", "clai-"+uid, "daemon.sock")
}

// Listen creates and returns a listener for the Unix socket.
// It ensures the parent directory exists with proper permissions (0700),
// and cleans up any stale socket files before listening.
func (t *UnixTransport) Listen() (net.Listener, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Ensure parent directory exists with secure permissions
	dir := filepath.Dir(t.socketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Clean up stale socket if it exists
	if err := t.cleanupStaleSocket(); err != nil {
		return nil, fmt.Errorf("failed to cleanup stale socket: %w", err)
	}

	// Create the listener
	listener, err := net.Listen("unix", t.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Set socket permissions (owner read/write only)
	if err := os.Chmod(t.socketPath, 0600); err != nil {
		listener.Close()
		os.Remove(t.socketPath)
		return nil, fmt.Errorf("failed to set socket permissions: %w", err)
	}

	t.listener = listener
	return listener, nil
}

// cleanupStaleSocket removes a socket file if it exists and is not responsive.
// This handles the case where a previous daemon crashed without cleanup.
func (t *UnixTransport) cleanupStaleSocket() error {
	// Check if socket file exists
	_, err := os.Stat(t.socketPath)
	if os.IsNotExist(err) {
		return nil // No socket to clean up
	}
	if err != nil {
		return fmt.Errorf("failed to stat socket: %w", err)
	}

	// Try to connect to check if it's alive
	conn, err := net.DialTimeout("unix", t.socketPath, 100*time.Millisecond)
	if err == nil {
		// Socket is alive - another daemon is running
		conn.Close()
		return fmt.Errorf("socket is active (another daemon may be running)")
	}

	// Socket exists but is not responsive - remove it
	if err := os.Remove(t.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}

	return nil
}

// Dial connects to the Unix socket with the specified timeout.
func (t *UnixTransport) Dial(timeout time.Duration) (net.Conn, error) {
	// Check if socket exists first
	if _, err := os.Stat(t.socketPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("socket not found: %s", t.socketPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", t.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %w", err)
	}

	return conn, nil
}

// Close releases resources and removes the socket file.
func (t *UnixTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var errs []error

	// Close the listener if it exists
	if t.listener != nil {
		if err := t.listener.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close listener: %w", err))
		}
		t.listener = nil
	}

	// Remove the socket file
	if err := os.Remove(t.socketPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove socket: %w", err))
	}

	if len(errs) > 0 {
		return errs[0] // Return first error
	}
	return nil
}

// SocketPath returns the path to the Unix socket file.
func (t *UnixTransport) SocketPath() string {
	return t.socketPath
}

// Ensure UnixTransport implements Transport interface.
var _ Transport = (*UnixTransport)(nil)
