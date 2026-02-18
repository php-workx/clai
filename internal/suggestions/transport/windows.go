//go:build windows

// Package transport provides IPC transport abstractions for the clai suggestions daemon.
package transport

import (
	"errors"
	"fmt"
	"net"
	"os/user"
	"time"
)

// ErrNotImplemented is returned when Windows named pipe support is not yet implemented.
var ErrNotImplemented = errors.New("windows named pipe transport not implemented")

// WindowsTransport implements Transport using Windows named pipes.
// This is currently a stub implementation that returns ErrNotImplemented.
type WindowsTransport struct {
	pipePath string
}

// NewWindowsTransport creates a new Windows named pipe transport.
// If pipePath is empty, it uses the default path: \\.\pipe\clai-<SID>-daemon
func NewWindowsTransport(pipePath string) *WindowsTransport {
	if pipePath == "" {
		pipePath = DefaultWindowsPipePath()
	}
	return &WindowsTransport{
		pipePath: pipePath,
	}
}

// DefaultWindowsPipePath returns the default named pipe path for the current user.
// Format: \\.\pipe\clai-<SID>-daemon
func DefaultWindowsPipePath() string {
	sid := getCurrentUserSID()
	return fmt.Sprintf(`\\.\pipe\clai-%s-daemon`, sid)
}

// getCurrentUserSID returns the current user's SID or a fallback identifier.
func getCurrentUserSID() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	// On Windows, u.Uid contains the SID
	return u.Uid
}

// Listen creates and returns a listener for the named pipe.
// This is a stub implementation that returns ErrNotImplemented.
func (t *WindowsTransport) Listen() (net.Listener, error) {
	return nil, fmt.Errorf("listen: %w", ErrNotImplemented)
}

// Dial connects to the named pipe with the specified timeout.
// This is a stub implementation that returns ErrNotImplemented.
func (t *WindowsTransport) Dial(timeout time.Duration) (net.Conn, error) {
	return nil, fmt.Errorf("dial: %w", ErrNotImplemented)
}

// Close releases resources held by the transport.
// This is a stub implementation that returns ErrNotImplemented.
func (t *WindowsTransport) Close() error {
	return fmt.Errorf("close: %w", ErrNotImplemented)
}

// SocketPath returns the named pipe path.
func (t *WindowsTransport) SocketPath() string {
	return t.pipePath
}

// Ensure WindowsTransport implements Transport interface.
var _ Transport = (*WindowsTransport)(nil)
