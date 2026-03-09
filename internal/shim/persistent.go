package shim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/runger/clai/internal/ipc"
)

// BackoffDelays are the retry delays for reconnection attempts.
var BackoffDelays = [2]time.Duration{100 * time.Millisecond, 500 * time.Millisecond}

// Dispatcher is the interface for sending events to the daemon.
// This abstracts the IPC client for testability.
type Dispatcher interface {
	// Dispatch sends a single event to the daemon.
	// Returns an error if the send fails (e.g., broken connection).
	Dispatch(ev *ShimEvent) error
	// Close closes the underlying connection.
	Close() error
}

// ClientDispatcher wraps an ipc.Client to implement Dispatcher.
type ClientDispatcher struct {
	client  *ipc.Client
	version string
}

// NewClientDispatcher creates a Dispatcher backed by an ipc.Client.
func NewClientDispatcher(client *ipc.Client, version string) *ClientDispatcher {
	return &ClientDispatcher{client: client, version: version}
}

// Dispatch dispatches a ShimEvent to the appropriate gRPC method.
func (d *ClientDispatcher) Dispatch(ev *ShimEvent) error {
	switch ev.Type {
	case EventSessionStart:
		info := ipc.DefaultClientInfo(d.version)
		if ev.Shell != "" {
			info.Shell = ev.Shell
		}
		d.client.SessionStart(ev.SessionID, ev.Cwd, info)
		return nil

	case EventSessionEnd:
		d.client.SessionEnd(ev.SessionID)
		return nil

	case EventCommandStart:
		cmdCtx := &ipc.CommandContext{
			GitBranch:     ev.GitBranch,
			GitRepoName:   ev.GitRepoName,
			GitRepoRoot:   ev.GitRepoRoot,
			PrevCommandID: ev.PrevCommandID,
		}
		d.client.LogStartWithContext(ev.SessionID, ev.CommandID, ev.Cwd, ev.Command, cmdCtx)
		return nil

	case EventCommandEnd:
		d.client.LogEnd(ev.SessionID, ev.CommandID, ev.ExitCode, ev.DurationMs)
		return nil

	default:
		return fmt.Errorf("unknown event type: %q", ev.Type)
	}
}

// Close closes the underlying IPC client connection.
func (d *ClientDispatcher) Close() error {
	return d.client.Close()
}

// DialFunc creates a new Dispatcher connection to the daemon.
// This is called for initial connection and reconnection attempts.
type DialFunc func() (Dispatcher, error)

// DefaultDialFunc returns a DialFunc that creates real ipc.Client connections.
func DefaultDialFunc(version string) DialFunc {
	return func() (Dispatcher, error) {
		client, err := ipc.NewClient()
		if err != nil {
			return nil, err
		}
		return NewClientDispatcher(client, version), nil
	}
}

// Runner manages the persistent NDJSON stdin loop with a single gRPC connection.
type Runner struct {
	dispatcher Dispatcher
	dial       DialFunc
	buf        *RingBuffer[*ShimEvent]
	version    string
	mu         sync.Mutex
	oneshot    bool
}

// NewRunner creates a new persistent mode Runner.
// The dialFn is used to create gRPC connections (for testability).
func NewRunner(dialFn DialFunc, version string) *Runner {
	return &Runner{
		dial:    dialFn,
		buf:     NewRingBuffer[*ShimEvent](DefaultRingCapacity),
		version: version,
	}
}

// Run reads NDJSON from the reader and dispatches events to the daemon.
// It maintains a single gRPC connection, reconnecting with backoff on failure.
// The context controls the lifecycle; when cancelled, Run returns.
//
// This is the main entry point for persistent mode. It reads from stdin
// (or any io.Reader) and sends events to the daemon, buffering up to 16
// events during temporary connection loss.
func (r *Runner) Run(ctx context.Context, reader io.Reader) error {
	// Establish initial connection
	if err := r.connect(); err != nil {
		// Initial connection failed; start in oneshot mode
		r.mu.Lock()
		r.oneshot = true
		r.mu.Unlock()
	}

	scanner := bufio.NewScanner(reader)
	// Set a generous max line size for long commands
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			r.drainAndClose()
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		ev, err := ParseShimEvent(line)
		if err != nil {
			// Skip invalid events silently (fire-and-forget semantics)
			continue
		}

		r.handleEvent(ev)
	}

	// Scanner finished (stdin closed / EOF)
	r.drainAndClose()

	if err := scanner.Err(); err != nil {
		if isBrokenPipe(err) {
			return nil // Expected on shell exit
		}
		return err
	}
	return nil
}

// handleEvent dispatches a single event, handling connection failures,
// buffering, and reconnection.
func (r *Runner) handleEvent(ev *ShimEvent) {
	r.mu.Lock()
	oneshot := r.oneshot
	disp := r.dispatcher
	r.mu.Unlock()

	if oneshot {
		r.sendOneshot(ev)
		return
	}

	// Try to send on persistent connection
	if disp != nil {
		if err := disp.Dispatch(ev); err != nil {
			if isBrokenPipe(err) {
				r.handleBrokenPipe(ev)
				return
			}
			// Other error: buffer and try reconnect
			r.buf.Push(ev)
			r.tryReconnect()
			return
		}
		return
	}

	// No dispatcher: buffer and try reconnect
	r.buf.Push(ev)
	r.tryReconnect()
}

// connect establishes the initial gRPC connection.
func (r *Runner) connect() error {
	disp, err := r.dial()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.dispatcher = disp
	r.mu.Unlock()
	return nil
}

// tryReconnect attempts to reconnect with exponential backoff.
// On success, drains the ring buffer. On failure, falls back to oneshot mode.
func (r *Runner) tryReconnect() {
	// Close old connection
	r.mu.Lock()
	if r.dispatcher != nil {
		_ = r.dispatcher.Close()
		r.dispatcher = nil
	}
	r.mu.Unlock()

	for _, delay := range BackoffDelays {
		time.Sleep(delay)

		disp, err := r.dial()
		if err != nil {
			continue
		}

		r.mu.Lock()
		r.dispatcher = disp
		r.mu.Unlock()

		// Drain buffered events
		r.drainBuffer()
		return
	}

	// All retries exhausted: fall back to oneshot mode
	r.mu.Lock()
	r.oneshot = true
	r.mu.Unlock()

	// Drain buffer via oneshot
	r.drainBufferOneshot()
}

// handleBrokenPipe handles a broken pipe by falling back to oneshot mode.
func (r *Runner) handleBrokenPipe(ev *ShimEvent) {
	r.mu.Lock()
	if r.dispatcher != nil {
		_ = r.dispatcher.Close()
		r.dispatcher = nil
	}
	r.oneshot = true
	r.mu.Unlock()

	// Drain any buffered events via oneshot, then send the current one
	r.drainBufferOneshot()
	r.sendOneshot(ev)
}

// drainBuffer sends all buffered events via the persistent connection.
func (r *Runner) drainBuffer() {
	events := r.buf.DrainAll()
	r.mu.Lock()
	disp := r.dispatcher
	r.mu.Unlock()

	if disp == nil {
		return
	}

	for _, ev := range events {
		_ = disp.Dispatch(ev)
	}
}

// drainBufferOneshot sends all buffered events via individual oneshot connections.
func (r *Runner) drainBufferOneshot() {
	events := r.buf.DrainAll()
	for _, ev := range events {
		r.sendOneshot(ev)
	}
}

// sendOneshot sends a single event using a fresh connection (one per event).
func (r *Runner) sendOneshot(ev *ShimEvent) {
	disp, err := r.dial()
	if err != nil {
		return // Silent failure, consistent with shim behavior
	}
	defer disp.Close()
	_ = disp.Dispatch(ev)
}

// drainAndClose sends any buffered events and closes the connection.
func (r *Runner) drainAndClose() {
	r.mu.Lock()
	disp := r.dispatcher
	oneshot := r.oneshot
	r.mu.Unlock()

	if oneshot {
		r.drainBufferOneshot()
		return
	}

	if disp != nil {
		r.drainBuffer()
		_ = disp.Close()
	} else {
		r.drainBufferOneshot()
	}

	r.mu.Lock()
	r.dispatcher = nil
	r.mu.Unlock()
}

// isBrokenPipe returns true if the error represents a broken pipe.
func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	// Check for EPIPE directly
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	// Check for common broken pipe error strings from gRPC
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "transport is closing")
}
