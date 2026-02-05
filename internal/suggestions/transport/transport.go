// Package transport provides IPC transport abstractions for the clai suggestions daemon.
// It supports Unix domain sockets on macOS/Linux and named pipes on Windows.
package transport

import (
	"net"
	"time"
)

// Transport defines the interface for daemon IPC communication.
// Implementations provide platform-specific transport mechanisms
// (Unix sockets, Windows named pipes).
type Transport interface {
	// Listen creates and returns a listener for the transport.
	// The implementation is responsible for creating any necessary
	// directories and cleaning up stale sockets/pipes.
	Listen() (net.Listener, error)

	// Dial connects to the transport with the specified timeout.
	// Returns a connection or an error if the connection fails.
	Dial(timeout time.Duration) (net.Conn, error)

	// Close releases any resources held by the transport.
	// This includes removing socket files on Unix systems.
	Close() error

	// SocketPath returns the path to the socket file or pipe name.
	// On Unix: /path/to/daemon.sock
	// On Windows: \\.\pipe\clai-<SID>-daemon
	SocketPath() string
}
