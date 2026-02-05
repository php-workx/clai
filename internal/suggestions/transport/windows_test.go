//go:build windows

package transport

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestDefaultWindowsPipePath verifies the pipe path format.
func TestDefaultWindowsPipePath(t *testing.T) {
	t.Parallel()

	path := DefaultWindowsPipePath()

	// Should start with \\.\pipe\
	if !strings.HasPrefix(path, `\\.\pipe\`) {
		t.Errorf("DefaultWindowsPipePath() = %q, should start with \\\\.\\pipe\\", path)
	}

	// Should contain clai
	if !strings.Contains(path, "clai") {
		t.Errorf("DefaultWindowsPipePath() = %q, should contain 'clai'", path)
	}

	// Should end with -daemon
	if !strings.HasSuffix(path, "-daemon") {
		t.Errorf("DefaultWindowsPipePath() = %q, should end with '-daemon'", path)
	}
}

// TestNewWindowsTransport verifies transport creation.
func TestNewWindowsTransport(t *testing.T) {
	t.Parallel()

	t.Run("custom path", func(t *testing.T) {
		t.Parallel()

		customPath := `\\.\pipe\custom-test`
		transport := NewWindowsTransport(customPath)

		if transport.SocketPath() != customPath {
			t.Errorf("SocketPath() = %q, want %q", transport.SocketPath(), customPath)
		}
	})

	t.Run("default path", func(t *testing.T) {
		t.Parallel()

		transport := NewWindowsTransport("")
		defaultPath := DefaultWindowsPipePath()

		if transport.SocketPath() != defaultPath {
			t.Errorf("SocketPath() = %q, want %q", transport.SocketPath(), defaultPath)
		}
	})
}

// TestWindowsTransport_Listen_NotImplemented verifies Listen returns error.
func TestWindowsTransport_Listen_NotImplemented(t *testing.T) {
	t.Parallel()

	transport := NewWindowsTransport("")

	listener, err := transport.Listen()
	if listener != nil {
		t.Error("Listen() should return nil listener")
	}

	if err == nil {
		t.Fatal("Listen() should return error")
	}

	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Listen() error = %v, should wrap ErrNotImplemented", err)
	}
}

// TestWindowsTransport_Dial_NotImplemented verifies Dial returns error.
func TestWindowsTransport_Dial_NotImplemented(t *testing.T) {
	t.Parallel()

	transport := NewWindowsTransport("")

	conn, err := transport.Dial(100 * time.Millisecond)
	if conn != nil {
		t.Error("Dial() should return nil connection")
	}

	if err == nil {
		t.Fatal("Dial() should return error")
	}

	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Dial() error = %v, should wrap ErrNotImplemented", err)
	}
}

// TestWindowsTransport_Close_NotImplemented verifies Close returns error.
func TestWindowsTransport_Close_NotImplemented(t *testing.T) {
	t.Parallel()

	transport := NewWindowsTransport("")

	err := transport.Close()
	if err == nil {
		t.Fatal("Close() should return error")
	}

	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Close() error = %v, should wrap ErrNotImplemented", err)
	}
}

// TestWindowsTransportInterface verifies WindowsTransport satisfies Transport interface.
func TestWindowsTransportInterface(t *testing.T) {
	t.Parallel()

	var _ Transport = (*WindowsTransport)(nil)
}
