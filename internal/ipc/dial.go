// Package ipc provides gRPC client functionality for communicating with the clai daemon.
package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Default timeouts for different operation types
const (
	// FireAndForgetTimeout is used for logging operations that should not block
	FireAndForgetTimeout = 10 * time.Millisecond

	// SuggestTimeout is used for suggestion requests
	SuggestTimeout = 50 * time.Millisecond

	// InteractiveTimeout is used for longer operations like text-to-command
	InteractiveTimeout = 5 * time.Second

	// DialTimeout is the maximum time to wait for initial connection
	DialTimeout = 50 * time.Millisecond
)

// SocketPath returns the path to the daemon Unix socket
func SocketPath() string {
	// Allow override via environment variable
	if path := os.Getenv("CLAI_SOCKET"); path != "" {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".clai", "run", "clai.sock")
}

// RunDir returns the directory containing runtime files (socket, pid)
func RunDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".clai", "run")
}

// SocketExists checks if the daemon socket file exists
func SocketExists() bool {
	_, err := os.Stat(SocketPath())
	return err == nil
}

// Dial connects to the daemon with the specified timeout.
// Returns a gRPC connection or an error if connection fails.
func Dial(timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return DialContext(ctx)
}

// DialContext connects to the daemon using the provided context for timeout/cancellation.
func DialContext(ctx context.Context) (*grpc.ClientConn, error) {
	sockPath := SocketPath()

	// Check if socket exists before attempting connection
	if !SocketExists() {
		return nil, fmt.Errorf("socket not found: %s", sockPath)
	}

	// Create a custom dialer for Unix sockets
	// The dialer receives the target address, but we use sockPath directly
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sockPath)
	}

	//nolint:staticcheck // Using deprecated DialContext for blocking connection behavior
	conn, err := grpc.DialContext(
		ctx,
		"passthrough:///"+sockPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	return conn, nil
}

// QuickDial attempts a fast connection to the daemon.
// It uses a short timeout suitable for fire-and-forget operations.
func QuickDial() (*grpc.ClientConn, error) {
	return Dial(DialTimeout)
}
