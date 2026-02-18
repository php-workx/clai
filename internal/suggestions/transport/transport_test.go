//go:build !windows

package transport

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// shortTempDir creates a temp directory with a short path suitable for Unix sockets.
// Unix sockets have a path length limit (~104-108 chars on macOS), and Go's t.TempDir()
// creates paths that are often too long for socket tests.
func shortTempDir(t *testing.T) string {
	t.Helper()
	// Use /tmp directly with a short name to stay under socket path limits
	dir, err := os.MkdirTemp("/tmp", "clai-t")
	if err != nil {
		t.Fatalf("failed to create short temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestDefaultUnixSocketPath verifies the path priority logic.
func TestDefaultUnixSocketPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		xdgRuntimeDir  string
		tmpdir         string
		expectedSuffix string
	}{
		{
			name:           "XDG_RUNTIME_DIR takes priority",
			xdgRuntimeDir:  "/run/user/1000",
			tmpdir:         "/var/tmp",
			expectedSuffix: "/run/user/1000/clai/daemon.sock",
		},
		{
			name:           "TMPDIR fallback when XDG not set",
			xdgRuntimeDir:  "",
			tmpdir:         "/var/tmp",
			expectedSuffix: "/var/tmp/clai-",
		},
		{
			name:           "tmp fallback when both unset",
			xdgRuntimeDir:  "",
			tmpdir:         "",
			expectedSuffix: "/tmp/clai-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original values
			origXDG := os.Getenv("XDG_RUNTIME_DIR")
			origTmp := os.Getenv("TMPDIR")
			defer func() {
				os.Setenv("XDG_RUNTIME_DIR", origXDG)
				os.Setenv("TMPDIR", origTmp)
			}()

			// Set test values
			if tt.xdgRuntimeDir != "" {
				os.Setenv("XDG_RUNTIME_DIR", tt.xdgRuntimeDir)
			} else {
				os.Unsetenv("XDG_RUNTIME_DIR")
			}

			if tt.tmpdir != "" {
				os.Setenv("TMPDIR", tt.tmpdir)
			} else {
				os.Unsetenv("TMPDIR")
			}

			path := DefaultUnixSocketPath()

			if !strings.Contains(path, tt.expectedSuffix) {
				t.Errorf("DefaultUnixSocketPath() = %q, expected to contain %q", path, tt.expectedSuffix)
			}

			// Verify path ends with daemon.sock
			if !strings.HasSuffix(path, "daemon.sock") {
				t.Errorf("DefaultUnixSocketPath() = %q, expected to end with daemon.sock", path)
			}
		})
	}
}

// TestDefaultUnixSocketPath_ContainsUID verifies UID is included when needed.
func TestDefaultUnixSocketPath_ContainsUID(t *testing.T) {
	t.Parallel()

	// Save and clear XDG_RUNTIME_DIR to test TMPDIR/tmp fallback
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	os.Unsetenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", origXDG)

	path := DefaultUnixSocketPath()
	uid := strconv.Itoa(os.Getuid())

	if !strings.Contains(path, uid) {
		t.Errorf("DefaultUnixSocketPath() = %q, expected to contain UID %s", path, uid)
	}
}

// TestNewUnixTransport verifies transport creation.
func TestNewUnixTransport(t *testing.T) {
	t.Parallel()

	t.Run("custom path", func(t *testing.T) {
		t.Parallel()

		customPath := "/tmp/custom-test.sock"
		transport := NewUnixTransport(customPath)

		if transport.SocketPath() != customPath {
			t.Errorf("SocketPath() = %q, want %q", transport.SocketPath(), customPath)
		}
	})

	t.Run("default path", func(t *testing.T) {
		t.Parallel()

		transport := NewUnixTransport("")
		defaultPath := DefaultUnixSocketPath()

		if transport.SocketPath() != defaultPath {
			t.Errorf("SocketPath() = %q, want %q", transport.SocketPath(), defaultPath)
		}
	})
}

// TestUnixTransport_Listen creates and listens on a socket.
func TestUnixTransport_Listen(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "test.sock")

	transport := NewUnixTransport(socketPath)

	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	defer transport.Close()

	// Verify socket file exists
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file should exist: %v", err)
	}

	// Verify permissions (0600)
	if info.Mode().Perm() != 0600 {
		t.Errorf("socket permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify listener is usable
	if listener.Addr().Network() != "unix" {
		t.Errorf("listener network = %q, want unix", listener.Addr().Network())
	}
}

// TestUnixTransport_Listen_CreatesDirectory verifies parent directory creation.
func TestUnixTransport_Listen_CreatesDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "nested", "dirs", "test.sock")

	transport := NewUnixTransport(socketPath)

	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	defer transport.Close()

	// Verify directory exists with proper permissions
	dir := filepath.Dir(socketPath)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory should exist: %v", err)
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("directory permissions = %o, want 0700", info.Mode().Perm())
	}
}

// TestUnixTransport_Listen_CleansStaleSocket verifies stale socket cleanup.
func TestUnixTransport_Listen_CleansStaleSocket(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "stale.sock")

	// Create a stale socket file (not a real socket, just a file)
	if err := os.WriteFile(socketPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("failed to create stale socket file: %v", err)
	}

	transport := NewUnixTransport(socketPath)

	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	defer transport.Close()

	// Verify socket was replaced
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file should exist: %v", err)
	}

	// Should be a socket now, not a regular file
	if info.Mode()&os.ModeSocket == 0 {
		t.Error("socket file should be a socket after cleanup")
	}
}

// TestUnixTransport_Listen_FailsOnActiveSocket verifies error when socket is active.
func TestUnixTransport_Listen_FailsOnActiveSocket(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "active.sock")

	// Create first transport and start listening
	transport1 := NewUnixTransport(socketPath)
	listener1, err := transport1.Listen()
	if err != nil {
		t.Fatalf("first Listen() error = %v", err)
	}
	defer listener1.Close()
	defer transport1.Close()

	// Try to create second transport on same socket
	transport2 := NewUnixTransport(socketPath)
	listener2, err := transport2.Listen()

	if err == nil {
		listener2.Close()
		t.Fatal("second Listen() should fail on active socket")
	}

	if !strings.Contains(err.Error(), "another daemon may be running") {
		t.Errorf("error = %v, should mention another daemon", err)
	}
}

// TestUnixTransport_Dial connects to a listening socket.
func TestUnixTransport_Dial(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "dial.sock")

	transport := NewUnixTransport(socketPath)

	// Start listening
	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	defer transport.Close()

	// Accept connections in background
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- conn
	}()

	// Dial to the socket
	conn, err := transport.Dial(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	// Verify connection works
	select {
	case serverConn := <-accepted:
		serverConn.Close()
	case <-time.After(1 * time.Second):
		t.Fatal("server did not accept connection")
	}
}

// TestUnixTransport_Dial_NonexistentSocket verifies error on missing socket.
func TestUnixTransport_Dial_NonexistentSocket(t *testing.T) {
	t.Parallel()

	transport := NewUnixTransport("/tmp/nonexistent-clai-test-socket.sock")

	conn, err := transport.Dial(50 * time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("Dial() should fail on nonexistent socket")
	}

	if !strings.Contains(err.Error(), "socket not found") {
		t.Errorf("error = %v, should mention socket not found", err)
	}
}

// TestUnixTransport_Dial_Timeout verifies dial respects timeout.
func TestUnixTransport_Dial_Timeout(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "timeout.sock")

	transport := NewUnixTransport(socketPath)

	// Start listening but don't accept connections
	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	defer transport.Close()

	// Create a new transport instance for dialing
	clientTransport := NewUnixTransport(socketPath)

	start := time.Now()
	conn, err := clientTransport.Dial(50 * time.Millisecond)
	elapsed := time.Since(start)

	// Connection should succeed (socket exists and we can connect)
	// but if we had a backlog issue, it would timeout
	if err == nil {
		conn.Close()
	}

	// Verify timeout is respected (with some tolerance)
	if elapsed > 500*time.Millisecond {
		t.Errorf("Dial took %v, expected to respect timeout", elapsed)
	}
}

// TestUnixTransport_Close removes socket file.
func TestUnixTransport_Close(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "close.sock")

	transport := NewUnixTransport(socketPath)

	_, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	// Don't close listener manually - let transport.Close() handle it

	// Close transport (closes listener and removes socket)
	if err := transport.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify socket file is removed
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after Close()")
	}
}

// TestUnixTransport_Close_Idempotent verifies Close can be called multiple times.
func TestUnixTransport_Close_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "idempotent.sock")

	transport := NewUnixTransport(socketPath)

	_, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	// Don't close listener manually - let transport.Close() handle it

	// Call Close multiple times - should not panic
	for i := 0; i < 3; i++ {
		_ = transport.Close()
		// First close may succeed, subsequent may return error for missing file
		// but should not panic
	}
}

// TestUnixTransport_DataTransfer verifies data can be sent over the transport.
func TestUnixTransport_DataTransfer(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "data.sock")

	transport := NewUnixTransport(socketPath)

	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer transport.Close()

	testData := []byte("hello, clai daemon!")

	// Server goroutine
	serverDone := make(chan struct{})
	var serverErr error
	var receivedData []byte

	go func() {
		defer close(serverDone)
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			serverErr = err
			return
		}
		defer conn.Close()

		receivedData, serverErr = io.ReadAll(conn)
	}()

	// Client connection
	conn, err := transport.Dial(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	conn.Close()

	// Wait for server
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not finish")
	}

	if serverErr != nil {
		t.Fatalf("server error = %v", serverErr)
	}

	if string(receivedData) != string(testData) {
		t.Errorf("received = %q, want %q", receivedData, testData)
	}
}

// TestUnixTransport_ConcurrentConnections verifies multiple clients can connect.
func TestUnixTransport_ConcurrentConnections(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "concurrent.sock")

	transport := NewUnixTransport(socketPath)

	listener, err := transport.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer transport.Close()

	const numClients = 10
	var wg sync.WaitGroup
	wg.Add(numClients * 2) // clients + server handlers

	// Accept connections in background
	go func() {
		for i := 0; i < numClients; i++ {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	// Start clients concurrently
	errors := make(chan error, numClients)
	for i := 0; i < numClients; i++ {
		go func() {
			defer wg.Done()
			clientTransport := NewUnixTransport(socketPath)
			conn, err := clientTransport.Dial(500 * time.Millisecond)
			if err != nil {
				errors <- err
				return
			}
			conn.Write([]byte("test"))
			conn.Close()
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent connections did not complete in time")
	}

	close(errors)
	for err := range errors {
		t.Errorf("client error: %v", err)
	}

	listener.Close()
}

// TestUnixTransport_PermissionDenied verifies error on permission denied.
func TestUnixTransport_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	t.Parallel()

	// Try to create socket in a directory we can't write to
	transport := NewUnixTransport("/root/clai-test.sock")

	_, err := transport.Listen()
	if err == nil {
		t.Fatal("Listen() should fail on permission denied")
	}
}

// TestUnixSocketPath_XDGPriority verifies XDG_RUNTIME_DIR has highest priority.
func TestUnixSocketPath_XDGPriority(t *testing.T) {
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	origTmp := os.Getenv("TMPDIR")
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", origXDG)
		os.Setenv("TMPDIR", origTmp)
	}()

	os.Setenv("XDG_RUNTIME_DIR", "/custom/xdg/runtime")
	os.Setenv("TMPDIR", "/custom/tmp")

	path := DefaultUnixSocketPath()
	expected := "/custom/xdg/runtime/clai/daemon.sock"

	if path != expected {
		t.Errorf("DefaultUnixSocketPath() = %q, want %q", path, expected)
	}
}

// TestUnixSocketPath_TMPDIRFallback verifies TMPDIR fallback works.
func TestUnixSocketPath_TMPDIRFallback(t *testing.T) {
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	origTmp := os.Getenv("TMPDIR")
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", origXDG)
		os.Setenv("TMPDIR", origTmp)
	}()

	os.Unsetenv("XDG_RUNTIME_DIR")
	os.Setenv("TMPDIR", "/custom/tmpdir")

	path := DefaultUnixSocketPath()
	uid := strconv.Itoa(os.Getuid())
	expected := "/custom/tmpdir/clai-" + uid + "/daemon.sock"

	if path != expected {
		t.Errorf("DefaultUnixSocketPath() = %q, want %q", path, expected)
	}
}

// TestUnixSocketPath_TmpFallback verifies /tmp fallback works.
func TestUnixSocketPath_TmpFallback(t *testing.T) {
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	origTmp := os.Getenv("TMPDIR")
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", origXDG)
		os.Setenv("TMPDIR", origTmp)
	}()

	os.Unsetenv("XDG_RUNTIME_DIR")
	os.Unsetenv("TMPDIR")

	path := DefaultUnixSocketPath()
	uid := strconv.Itoa(os.Getuid())
	expected := "/tmp/clai-" + uid + "/daemon.sock"

	if path != expected {
		t.Errorf("DefaultUnixSocketPath() = %q, want %q", path, expected)
	}
}

// TestTransportInterface verifies UnixTransport satisfies Transport interface.
func TestTransportInterface(t *testing.T) {
	t.Parallel()

	var _ Transport = (*UnixTransport)(nil)
}
