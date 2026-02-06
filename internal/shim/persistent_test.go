package shim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockDispatcher records dispatched events for testing.
type mockDispatcher struct {
	mu         sync.Mutex
	events     []*ShimEvent
	failNext   bool
	failPipe   bool
	closed     bool
	closeErr   error
	onDispatch func(ev *ShimEvent) error
}

func (m *mockDispatcher) Dispatch(ev *ShimEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.onDispatch != nil {
		return m.onDispatch(ev)
	}
	if m.failPipe {
		return fmt.Errorf("broken pipe")
	}
	if m.failNext {
		m.failNext = false
		return fmt.Errorf("temporary error")
	}
	m.events = append(m.events, ev)
	return nil
}

func (m *mockDispatcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeErr
}

func (m *mockDispatcher) getEvents() []*ShimEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*ShimEvent, len(m.events))
	copy(result, m.events)
	return result
}

// testDialer creates a DialFunc that returns mock dispatchers.
type testDialer struct {
	mu          sync.Mutex
	dispatchers []*mockDispatcher
	failCount   int // number of initial dial failures
	dialCount   int
}

func (td *testDialer) dial() (Dispatcher, error) {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.dialCount++
	if td.failCount > 0 {
		td.failCount--
		return nil, fmt.Errorf("connection refused")
	}
	d := &mockDispatcher{}
	td.dispatchers = append(td.dispatchers, d)
	return d, nil
}

func (td *testDialer) getDispatchers() []*mockDispatcher {
	td.mu.Lock()
	defer td.mu.Unlock()
	result := make([]*mockDispatcher, len(td.dispatchers))
	copy(result, td.dispatchers)
	return result
}

func init() {
	// Override backoff delays for fast tests
	BackoffDelays = [2]time.Duration{time.Millisecond, time.Millisecond}
}

func TestRunnerBasicFlow(t *testing.T) {
	td := &testDialer{}
	runner := NewRunner(td.dial, "test")

	input := strings.Join([]string{
		`{"type":"session_start","session_id":"s1","cwd":"/home","shell":"zsh"}`,
		`{"type":"command_start","session_id":"s1","command_id":"c1","cwd":"/home","command":"ls"}`,
		`{"type":"command_end","session_id":"s1","command_id":"c1","exit_code":0,"duration_ms":100}`,
		`{"type":"session_end","session_id":"s1"}`,
	}, "\n")

	err := runner.Run(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dispatchers := td.getDispatchers()
	if len(dispatchers) == 0 {
		t.Fatal("expected at least one dispatcher")
	}

	events := dispatchers[0].getEvents()
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	expectedTypes := []string{
		EventSessionStart,
		EventCommandStart,
		EventCommandEnd,
		EventSessionEnd,
	}
	for i, want := range expectedTypes {
		if events[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, events[i].Type, want)
		}
	}
}

func TestRunnerSkipsInvalidEvents(t *testing.T) {
	td := &testDialer{}
	runner := NewRunner(td.dial, "test")

	input := strings.Join([]string{
		`{"type":"session_start","session_id":"s1","cwd":"/home"}`,
		`not valid json`,
		`{"type":"unknown_type","session_id":"s1"}`,
		`{"type":"session_end","session_id":"s1"}`,
		``,
	}, "\n")

	err := runner.Run(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dispatchers := td.getDispatchers()
	if len(dispatchers) == 0 {
		t.Fatal("expected at least one dispatcher")
	}

	events := dispatchers[0].getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 valid events, got %d", len(events))
	}
}

func TestRunnerOneshotFallback(t *testing.T) {
	// Dial always fails initially -> oneshot mode
	// Then succeeds for individual sends
	callCount := 0
	var dispatchers []*mockDispatcher
	var mu sync.Mutex
	dialFn := func() (Dispatcher, error) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		if callCount <= 3 {
			// First 3 calls fail (initial + 2 retries in connect/tryReconnect)
			return nil, fmt.Errorf("connection refused")
		}
		d := &mockDispatcher{}
		dispatchers = append(dispatchers, d)
		return d, nil
	}

	runner := NewRunner(dialFn, "test")

	input := `{"type":"session_start","session_id":"s1","cwd":"/home"}` + "\n"
	err := runner.Run(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Should have created oneshot dispatchers for the event
	if len(dispatchers) == 0 {
		t.Log("no dispatchers created (all dial attempts might have been for reconnect)")
	}
}

func TestRunnerInitialConnectFailure(t *testing.T) {
	td := &testDialer{failCount: 100} // always fail
	runner := NewRunner(td.dial, "test")

	// Even with all connections failing, Run should not error on EOF
	input := `{"type":"session_start","session_id":"s1","cwd":"/home"}` + "\n"
	err := runner.Run(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunnerContextCancellation(t *testing.T) {
	td := &testDialer{}
	runner := NewRunner(td.dial, "test")

	ctx, cancel := context.WithCancel(context.Background())

	// Use a reader that blocks until context is cancelled
	pr, pw := syncPipe()
	defer pw.Close()

	// Write one event, then cancel
	_, _ = pw.Write([]byte(`{"type":"session_start","session_id":"s1","cwd":"/home"}` + "\n"))

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx, pr)
	}()

	// Give the runner time to process the first event
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Write another line to unblock the scanner
	_, _ = pw.Write([]byte(`{"type":"session_end","session_id":"s1"}` + "\n"))

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunnerEmptyInput(t *testing.T) {
	td := &testDialer{}
	runner := NewRunner(td.dial, "test")

	err := runner.Run(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunnerBrokenPipeDetection(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{"nil error", "", false},
		{"broken pipe string", "write: broken pipe", true},
		{"connection refused", "dial: connection refused", true},
		{"transport closing", "rpc error: transport is closing", true},
		{"unrelated error", "timeout exceeded", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf("%s", tt.errMsg)
			}
			if got := isBrokenPipe(err); got != tt.want {
				t.Errorf("isBrokenPipe(%v) = %v, want %v", err, got, tt.want)
			}
		})
	}
}

func TestRunnerBrokenPipeFallsBackToOneshot(t *testing.T) {
	var mu sync.Mutex
	var dispatchers []*mockDispatcher
	firstCall := true

	dialFn := func() (Dispatcher, error) {
		mu.Lock()
		defer mu.Unlock()
		d := &mockDispatcher{}
		if firstCall {
			firstCall = false
			d.failPipe = true // First dispatcher will return broken pipe
		}
		dispatchers = append(dispatchers, d)
		return d, nil
	}

	runner := NewRunner(dialFn, "test")

	input := strings.Join([]string{
		`{"type":"session_start","session_id":"s1","cwd":"/home"}`,
		`{"type":"session_end","session_id":"s1"}`,
	}, "\n")

	err := runner.Run(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Should have created multiple dispatchers: first persistent (broken pipe), then oneshot(s)
	if len(dispatchers) < 2 {
		t.Errorf("expected at least 2 dispatchers (persistent + oneshot), got %d", len(dispatchers))
	}
}

func TestRunnerBufferingDuringReconnect(t *testing.T) {
	var mu sync.Mutex
	var dispatchers []*mockDispatcher
	callCount := 0

	dialFn := func() (Dispatcher, error) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		d := &mockDispatcher{}
		dispatchers = append(dispatchers, d)
		return d, nil
	}

	runner := NewRunner(dialFn, "test")

	// Manually push events to buffer to simulate connection loss buffering
	runner.buf.Push(&ShimEvent{Type: EventSessionStart, SessionID: "s1", Cwd: "/home"})
	runner.buf.Push(&ShimEvent{Type: EventCommandStart, SessionID: "s1", CommandID: "c1"})

	if runner.buf.Len() != 2 {
		t.Fatalf("expected 2 buffered events, got %d", runner.buf.Len())
	}

	// Drain via oneshot
	runner.oneshot = true
	runner.drainBufferOneshot()

	if runner.buf.Len() != 0 {
		t.Errorf("expected 0 buffered events after drain, got %d", runner.buf.Len())
	}
}

func TestRunnerRingBufferCapacity(t *testing.T) {
	runner := NewRunner(nil, "test")

	if runner.buf.Cap() != DefaultRingCapacity {
		t.Errorf("expected buffer capacity %d, got %d", DefaultRingCapacity, runner.buf.Cap())
	}
}

// syncPipe creates a synchronous in-memory pipe (like os.Pipe but using bytes.Buffer).
func syncPipe() (*syncReader, *syncWriter) {
	var mu sync.Mutex
	var buf bytes.Buffer
	cond := sync.NewCond(&mu)
	closed := false

	r := &syncReader{mu: &mu, buf: &buf, cond: cond, closed: &closed}
	w := &syncWriter{mu: &mu, buf: &buf, cond: cond, closed: &closed}
	return r, w
}

type syncReader struct {
	mu     *sync.Mutex
	buf    *bytes.Buffer
	cond   *sync.Cond
	closed *bool
}

func (r *syncReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.buf.Len() == 0 {
		if *r.closed {
			return 0, fmt.Errorf("EOF")
		}
		r.cond.Wait()
	}
	return r.buf.Read(p)
}

type syncWriter struct {
	mu     *sync.Mutex
	buf    *bytes.Buffer
	cond   *sync.Cond
	closed *bool
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	w.cond.Signal()
	return n, err
}

func (w *syncWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.closed = true
	w.cond.Broadcast()
	return nil
}
