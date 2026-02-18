// Package hook provides the client-side event sender for clai shell hooks.
// It implements fire-and-forget event sending with configurable timeouts
// to ensure shell hooks never block the user's terminal prompt.
package hook

import (
	"encoding/json"
	"os"
	"strconv"
	"time"

	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/transport"
)

// Default timeout values per spec sections 3.3 and 6.4.
const (
	// DefaultConnectTimeout is the default timeout for connecting to the daemon socket.
	// Range: 10-20ms as per spec.
	DefaultConnectTimeout = 15 * time.Millisecond

	// DefaultWriteTimeout is the default timeout for writing events to the socket.
	// Range: 10-20ms as per spec.
	DefaultWriteTimeout = 15 * time.Millisecond

	// MinConnectTimeout is the minimum allowed connect timeout.
	MinConnectTimeout = 10 * time.Millisecond

	// MaxConnectTimeout is the maximum allowed connect timeout.
	MaxConnectTimeout = 20 * time.Millisecond
)

// EnvConnectTimeoutMs is the environment variable for configuring connect timeout.
const EnvConnectTimeoutMs = "CLAI_CONNECT_TIMEOUT_MS"

// Environment variables for incognito mode per spec Section 6.10.
const (
	// EnvNoRecord skips ingestion entirely when set to "1".
	EnvNoRecord = "CLAI_NO_RECORD"

	// EnvEphemeral marks events as ephemeral (not persisted) when set to "1".
	EnvEphemeral = "CLAI_EPHEMERAL"
)

// Sender sends command events to the daemon using fire-and-forget semantics.
// It connects to the daemon socket, writes the event, and immediately closes
// the connection without waiting for any acknowledgment.
//
// All errors are silently dropped to ensure the shell prompt is never blocked.
type Sender struct {
	transport      transport.Transport
	connectTimeout time.Duration
	writeTimeout   time.Duration
}

// NewSender creates a new Sender with the given transport.
// It uses default timeouts which can be overridden with SetConnectTimeout
// and SetWriteTimeout, or via the CLAI_CONNECT_TIMEOUT_MS environment variable.
func NewSender(t transport.Transport) *Sender {
	s := &Sender{
		transport:      t,
		connectTimeout: DefaultConnectTimeout,
		writeTimeout:   DefaultWriteTimeout,
	}

	// Check for environment variable override
	if envTimeout := os.Getenv(EnvConnectTimeoutMs); envTimeout != "" {
		if ms, err := strconv.Atoi(envTimeout); err == nil {
			timeout := time.Duration(ms) * time.Millisecond
			// Clamp to valid range
			if timeout >= MinConnectTimeout && timeout <= MaxConnectTimeout {
				s.connectTimeout = timeout
			}
		}
	}

	return s
}

// Send attempts to send an event to the daemon.
// Returns true if the event was successfully written to the socket,
// false if any error occurred (connection failed, write failed, etc.).
//
// If CLAI_NO_RECORD=1, the event is silently dropped without sending.
// If CLAI_EPHEMERAL=1, the event's Ephemeral field is set to true.
//
// This method is fire-and-forget: it does NOT read or wait for any
// acknowledgment from the daemon. Events are silently dropped on any error.
func (s *Sender) Send(ev *event.CommandEvent) bool {
	if ev == nil {
		return false
	}

	// Check for no-record mode (skip ingestion entirely)
	if os.Getenv(EnvNoRecord) == "1" {
		return true // Silently succeed without sending
	}

	// Check for ephemeral mode (send but mark as ephemeral)
	if os.Getenv(EnvEphemeral) == "1" {
		ev.Ephemeral = true
	}

	// Connect to daemon with timeout
	conn, err := s.transport.Dial(s.connectTimeout)
	if err != nil {
		return false
	}
	defer conn.Close()

	// Set write deadline
	if err := conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return false
	}

	// Serialize event to JSON
	data, err := json.Marshal(ev)
	if err != nil {
		return false
	}

	// Write JSON + newline (NDJSON format)
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err == nil
}

// SetConnectTimeout sets the timeout for connecting to the daemon socket.
// The timeout is clamped to the valid range (10-20ms).
func (s *Sender) SetConnectTimeout(d time.Duration) {
	if d < MinConnectTimeout {
		d = MinConnectTimeout
	}
	if d > MaxConnectTimeout {
		d = MaxConnectTimeout
	}
	s.connectTimeout = d
}

// SetWriteTimeout sets the timeout for writing events to the socket.
func (s *Sender) SetWriteTimeout(d time.Duration) {
	if d < 0 {
		d = DefaultWriteTimeout
	}
	s.writeTimeout = d
}

// ConnectTimeout returns the current connect timeout.
func (s *Sender) ConnectTimeout() time.Duration {
	return s.connectTimeout
}

// WriteTimeout returns the current write timeout.
func (s *Sender) WriteTimeout() time.Duration {
	return s.writeTimeout
}

// IsNoRecord returns true if CLAI_NO_RECORD is set to "1".
// When true, events should not be sent to the daemon.
func IsNoRecord() bool {
	return os.Getenv(EnvNoRecord) == "1"
}

// IsEphemeral returns true if CLAI_EPHEMERAL is set to "1".
// When true, events should be marked as ephemeral and not persisted.
func IsEphemeral() bool {
	return os.Getenv(EnvEphemeral) == "1"
}

// IsIncognito returns true if either CLAI_NO_RECORD or CLAI_EPHEMERAL is set.
func IsIncognito() bool {
	return IsNoRecord() || IsEphemeral()
}
