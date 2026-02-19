//go:build !windows

package hook

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/transport"
)

// shortTempDir creates a temp directory with a short path suitable for Unix sockets.
// Unix sockets have a path length limit (~104-108 chars on macOS).
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "clai-h")
	if err != nil {
		t.Fatalf("failed to create short temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// testServer creates a test server that accepts connections and optionally reads events.
type testServer struct {
	listener  net.Listener
	transport *transport.UnixTransport
	stopCh    chan struct{}
	events    []*event.CommandEvent
	wg        sync.WaitGroup
	mu        sync.Mutex
}

func newTestServer(t *testing.T, socketPath string) *testServer {
	t.Helper()
	tr := transport.NewUnixTransport(socketPath)
	listener, err := tr.Listen()
	require.NoError(t, err)

	s := &testServer{
		listener:  listener,
		transport: tr,
		events:    make([]*event.CommandEvent, 0),
		stopCh:    make(chan struct{}),
	}

	return s
}

func (s *testServer) acceptOne() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the event (NDJSON format)
		reader := bufio.NewReader(conn)
		line, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return
		}

		if len(line) > 0 {
			var ev event.CommandEvent
			if json.Unmarshal(line, &ev) == nil {
				s.mu.Lock()
				s.events = append(s.events, &ev)
				s.mu.Unlock()
			}
		}
	}()
}

func (s *testServer) acceptAndBlock() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Block - don't read anything, simulating a slow server
		select {
		case <-s.stopCh:
		case <-time.After(5 * time.Second):
		}
	}()
}

func (s *testServer) getEvents() []*event.CommandEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*event.CommandEvent, len(s.events))
	copy(result, s.events)
	return result
}

func (s *testServer) close() {
	close(s.stopCh)
	s.listener.Close()
	s.transport.Close()
	s.wg.Wait()
}

func TestNewSender(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "test.sock")
	tr := transport.NewUnixTransport(socketPath)

	sender := NewSender(tr)

	assert.NotNil(t, sender)
	assert.Equal(t, DefaultConnectTimeout, sender.ConnectTimeout())
	assert.Equal(t, DefaultWriteTimeout, sender.WriteTimeout())
}

func TestSender_Send_Success(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "send.sock")

	// Start test server
	server := newTestServer(t, socketPath)
	defer server.close()
	server.acceptOne()

	// Create sender
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	// Use longer timeouts for test reliability
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 100 * time.Millisecond

	// Create and send event
	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		TS:        1730000000123,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user",
		CmdRaw:    "git status",
		ExitCode:  0,
		Ephemeral: false,
	}

	result := sender.Send(ev)
	assert.True(t, result)

	// Wait for server to process
	server.wg.Wait()

	// Verify event was received
	events := server.getEvents()
	require.Len(t, events, 1)
	assert.Equal(t, ev.SessionID, events[0].SessionID)
	assert.Equal(t, ev.CmdRaw, events[0].CmdRaw)
	assert.Equal(t, ev.Shell, events[0].Shell)
}

func TestSender_Send_ConnectTimeout_NoServer(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "noserver.sock")

	// No server listening - should fail fast
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)

	ev := event.NewCommandEvent()
	ev.SessionID = "test"
	ev.CmdRaw = "echo test"
	ev.Cwd = "/tmp"
	ev.Shell = event.ShellBash

	start := time.Now()
	result := sender.Send(ev)
	elapsed := time.Since(start)

	assert.False(t, result)
	// Should fail quickly since socket doesn't exist
	assert.Less(t, elapsed, 50*time.Millisecond)
}

func TestSender_Send_WriteTimeout(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "slow.sock")

	// Start server that accepts but doesn't read
	server := newTestServer(t, socketPath)
	defer server.close()
	server.acceptAndBlock()

	// Create sender with very short write timeout
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 1 * time.Millisecond // Very short

	// Create a large event to trigger write timeout more reliably
	ev := event.NewCommandEvent()
	ev.SessionID = "test"
	ev.CmdRaw = string(make([]byte, 64*1024)) // 64KB command
	ev.Cwd = "/tmp"
	ev.Shell = event.ShellBash

	start := time.Now()
	result := sender.Send(ev)
	elapsed := time.Since(start)

	// Should complete within reasonable time (not hang)
	assert.Less(t, elapsed, 500*time.Millisecond)
	// Result could be true or false depending on kernel buffer
	_ = result
}

func TestSender_Send_SilentDropOnError(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "drop.sock")

	// No server - connection will fail
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)

	ev := event.NewCommandEvent()
	ev.SessionID = "test"
	ev.CmdRaw = "echo test"
	ev.Cwd = "/tmp"
	ev.Shell = event.ShellBash

	// Should return false (dropped) without panic or error logging
	result := sender.Send(ev)
	assert.False(t, result)

	// Verify no panic when sending nil
	result = sender.Send(nil)
	assert.False(t, result)
}

func TestSender_Send_NilEvent(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "nil.sock")

	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)

	result := sender.Send(nil)
	assert.False(t, result)
}

func TestSender_SetConnectTimeout(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "timeout.sock")
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)

	// Test within valid range
	sender.SetConnectTimeout(12 * time.Millisecond)
	assert.Equal(t, 12*time.Millisecond, sender.ConnectTimeout())

	// Test below minimum - should clamp
	sender.SetConnectTimeout(5 * time.Millisecond)
	assert.Equal(t, MinConnectTimeout, sender.ConnectTimeout())

	// Test above maximum - should clamp
	sender.SetConnectTimeout(50 * time.Millisecond)
	assert.Equal(t, MaxConnectTimeout, sender.ConnectTimeout())
}

func TestSender_SetWriteTimeout(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "write.sock")
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)

	sender.SetWriteTimeout(25 * time.Millisecond)
	assert.Equal(t, 25*time.Millisecond, sender.WriteTimeout())

	// Negative should use default
	sender.SetWriteTimeout(-1 * time.Millisecond)
	assert.Equal(t, DefaultWriteTimeout, sender.WriteTimeout())
}

func TestSender_EnvironmentVariableParsing(t *testing.T) {
	// Save and restore environment
	origValue := os.Getenv(EnvConnectTimeoutMs)
	defer os.Setenv(EnvConnectTimeoutMs, origValue)

	tmpDir := shortTempDir(t)

	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "valid value within range",
			envValue: "12",
			expected: 12 * time.Millisecond,
		},
		{
			name:     "value at minimum",
			envValue: "10",
			expected: 10 * time.Millisecond,
		},
		{
			name:     "value at maximum",
			envValue: "20",
			expected: 20 * time.Millisecond,
		},
		{
			name:     "value below minimum - use default",
			envValue: "5",
			expected: DefaultConnectTimeout,
		},
		{
			name:     "value above maximum - use default",
			envValue: "50",
			expected: DefaultConnectTimeout,
		},
		{
			name:     "invalid value - use default",
			envValue: "abc",
			expected: DefaultConnectTimeout,
		},
		{
			name:     "empty value - use default",
			envValue: "",
			expected: DefaultConnectTimeout,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(EnvConnectTimeoutMs)
			} else {
				os.Setenv(EnvConnectTimeoutMs, tt.envValue)
			}

			socketPath := filepath.Join(tmpDir, fmt.Sprintf("env%d.sock", i))
			tr := transport.NewUnixTransport(socketPath)
			sender := NewSender(tr)

			assert.Equal(t, tt.expected, sender.ConnectTimeout())
		})
	}
}

func TestSender_NDJSONFormat(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "ndjson.sock")

	// Start test server
	server := newTestServer(t, socketPath)
	defer server.close()
	server.acceptOne()

	// Create sender
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 100 * time.Millisecond

	duration := int64(1500)
	ev := &event.CommandEvent{
		Version:    1,
		Type:       event.EventTypeCommandEnd,
		TS:         1730000000123,
		SessionID:  "ndjson-test",
		Shell:      event.ShellFish,
		Cwd:        "/home/user/project",
		CmdRaw:     "npm test",
		ExitCode:   0,
		DurationMs: &duration,
		Ephemeral:  true,
	}

	result := sender.Send(ev)
	assert.True(t, result)

	// Wait for server to process
	server.wg.Wait()

	// Verify event was received with all fields
	events := server.getEvents()
	require.Len(t, events, 1)
	received := events[0]

	assert.Equal(t, ev.Version, received.Version)
	assert.Equal(t, ev.Type, received.Type)
	assert.Equal(t, ev.TS, received.TS)
	assert.Equal(t, ev.SessionID, received.SessionID)
	assert.Equal(t, ev.Shell, received.Shell)
	assert.Equal(t, ev.Cwd, received.Cwd)
	assert.Equal(t, ev.CmdRaw, received.CmdRaw)
	assert.Equal(t, ev.ExitCode, received.ExitCode)
	require.NotNil(t, received.DurationMs)
	assert.Equal(t, *ev.DurationMs, *received.DurationMs)
	assert.Equal(t, ev.Ephemeral, received.Ephemeral)
}

func TestSender_MultipleEvents(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "multi.sock")

	// Start test server
	server := newTestServer(t, socketPath)
	defer server.close()

	// Accept multiple connections
	for i := 0; i < 3; i++ {
		server.acceptOne()
	}

	// Create sender
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 100 * time.Millisecond

	// Send multiple events
	for i := 0; i < 3; i++ {
		ev := event.NewCommandEvent()
		ev.SessionID = "multi-test"
		ev.CmdRaw = "echo " + string(rune('a'+i))
		ev.Cwd = "/tmp"
		ev.Shell = event.ShellBash

		result := sender.Send(ev)
		assert.True(t, result)
	}

	// Wait for server to process
	server.wg.Wait()

	// Verify all events were received
	events := server.getEvents()
	assert.Len(t, events, 3)
}

func TestSender_SpecialCharactersInCommand(t *testing.T) {
	t.Parallel()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "special.sock")

	// Start test server
	server := newTestServer(t, socketPath)
	defer server.close()
	server.acceptOne()

	// Create sender
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 100 * time.Millisecond

	// Command with special characters that need JSON escaping
	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		TS:        1730000000123,
		SessionID: "special-test",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user",
		CmdRaw:    `git commit -m "fix: \"quoted\" work"` + "\n\ttabbed",
		ExitCode:  0,
		Ephemeral: false,
	}

	result := sender.Send(ev)
	assert.True(t, result)

	// Wait for server to process
	server.wg.Wait()

	// Verify event was received correctly
	events := server.getEvents()
	require.Len(t, events, 1)
	assert.Equal(t, ev.CmdRaw, events[0].CmdRaw)
}

func TestSender_IncognitoNoRecord(t *testing.T) {
	// Save and restore environment
	origValue := os.Getenv(EnvNoRecord)
	defer func() {
		if origValue != "" {
			os.Setenv(EnvNoRecord, origValue)
		} else {
			os.Unsetenv(EnvNoRecord)
		}
	}()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "norecord.sock")

	// Start test server
	server := newTestServer(t, socketPath)
	defer server.close()
	server.acceptOne()

	// Create sender
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 100 * time.Millisecond

	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		TS:        1730000000123,
		SessionID: "norecord-test",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user",
		CmdRaw:    "secret command",
		ExitCode:  0,
		Ephemeral: false,
	}

	// Enable no-record mode
	os.Setenv(EnvNoRecord, "1")

	// Send should succeed without actually sending
	result := sender.Send(ev)
	assert.True(t, result, "Send should return true even though event was dropped")

	// Give server a moment to receive (it shouldn't)
	time.Sleep(50 * time.Millisecond)

	// Verify NO event was received
	events := server.getEvents()
	assert.Len(t, events, 0, "No events should be received in no-record mode")
}

func TestSender_IncognitoEphemeral(t *testing.T) {
	// Save and restore environment
	origValue := os.Getenv(EnvEphemeral)
	defer func() {
		if origValue != "" {
			os.Setenv(EnvEphemeral, origValue)
		} else {
			os.Unsetenv(EnvEphemeral)
		}
	}()
	// Also ensure CLAI_NO_RECORD is not set
	origNoRecord := os.Getenv(EnvNoRecord)
	os.Unsetenv(EnvNoRecord)
	defer func() {
		if origNoRecord != "" {
			os.Setenv(EnvNoRecord, origNoRecord)
		}
	}()

	tmpDir := shortTempDir(t)
	socketPath := filepath.Join(tmpDir, "ephemeral.sock")

	// Start test server
	server := newTestServer(t, socketPath)
	defer server.close()
	server.acceptOne()

	// Create sender
	tr := transport.NewUnixTransport(socketPath)
	sender := NewSender(tr)
	sender.connectTimeout = 100 * time.Millisecond
	sender.writeTimeout = 100 * time.Millisecond

	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		TS:        1730000000123,
		SessionID: "ephemeral-test",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user",
		CmdRaw:    "private command",
		ExitCode:  0,
		Ephemeral: false, // Initially false
	}

	// Enable ephemeral mode
	os.Setenv(EnvEphemeral, "1")

	// Send should succeed
	result := sender.Send(ev)
	assert.True(t, result)

	// Wait for server to process
	server.wg.Wait()

	// Verify event was received with Ephemeral=true
	events := server.getEvents()
	require.Len(t, events, 1)
	assert.True(t, events[0].Ephemeral, "Event should have Ephemeral=true when CLAI_EPHEMERAL is set")
	assert.Equal(t, "private command", events[0].CmdRaw)
}

func TestIsNoRecord(t *testing.T) {
	origValue := os.Getenv(EnvNoRecord)
	defer func() {
		if origValue != "" {
			os.Setenv(EnvNoRecord, origValue)
		} else {
			os.Unsetenv(EnvNoRecord)
		}
	}()

	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{"not set", "", false},
		{"set to 1", "1", true},
		{"set to 0", "0", false},
		{"set to true", "true", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(EnvNoRecord)
			} else {
				os.Setenv(EnvNoRecord, tt.envValue)
			}
			assert.Equal(t, tt.want, IsNoRecord())
		})
	}
}

func TestIsEphemeral(t *testing.T) {
	origValue := os.Getenv(EnvEphemeral)
	defer func() {
		if origValue != "" {
			os.Setenv(EnvEphemeral, origValue)
		} else {
			os.Unsetenv(EnvEphemeral)
		}
	}()

	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{"not set", "", false},
		{"set to 1", "1", true},
		{"set to 0", "0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(EnvEphemeral)
			} else {
				os.Setenv(EnvEphemeral, tt.envValue)
			}
			assert.Equal(t, tt.want, IsEphemeral())
		})
	}
}

func TestIsIncognito(t *testing.T) {
	origNoRecord := os.Getenv(EnvNoRecord)
	origEphemeral := os.Getenv(EnvEphemeral)
	defer func() {
		if origNoRecord != "" {
			os.Setenv(EnvNoRecord, origNoRecord)
		} else {
			os.Unsetenv(EnvNoRecord)
		}
		if origEphemeral != "" {
			os.Setenv(EnvEphemeral, origEphemeral)
		} else {
			os.Unsetenv(EnvEphemeral)
		}
	}()

	tests := []struct {
		name      string
		noRecord  string
		ephemeral string
		want      bool
	}{
		{"neither set", "", "", false},
		{"no_record set", "1", "", true},
		{"ephemeral set", "", "1", true},
		{"both set", "1", "1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.noRecord == "" {
				os.Unsetenv(EnvNoRecord)
			} else {
				os.Setenv(EnvNoRecord, tt.noRecord)
			}
			if tt.ephemeral == "" {
				os.Unsetenv(EnvEphemeral)
			} else {
				os.Setenv(EnvEphemeral, tt.ephemeral)
			}
			assert.Equal(t, tt.want, IsIncognito())
		})
	}
}
