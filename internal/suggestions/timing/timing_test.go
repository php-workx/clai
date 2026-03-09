package timing

import (
	"sync"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.FastThresholdMs != 100 {
		t.Errorf("FastThresholdMs = %d, want 100", cfg.FastThresholdMs)
	}
	if cfg.PauseThresholdMs != 300 {
		t.Errorf("PauseThresholdMs = %d, want 300", cfg.PauseThresholdMs)
	}
	if cfg.IdleTimeoutMs != 2000 {
		t.Errorf("IdleTimeoutMs = %d, want 2000", cfg.IdleTimeoutMs)
	}
}

func TestApplyDefaults(t *testing.T) {
	// Zero config should get all defaults
	cfg := Config{}.applyDefaults()
	want := DefaultConfig()
	if cfg != want {
		t.Errorf("applyDefaults() = %+v, want %+v", cfg, want)
	}

	// Partial config should fill in missing values
	cfg = Config{FastThresholdMs: 50}.applyDefaults()
	if cfg.FastThresholdMs != 50 {
		t.Errorf("FastThresholdMs = %d, want 50", cfg.FastThresholdMs)
	}
	if cfg.PauseThresholdMs != want.PauseThresholdMs {
		t.Errorf("PauseThresholdMs = %d, want %d", cfg.PauseThresholdMs, want.PauseThresholdMs)
	}

	// Negative values should get defaults
	cfg = Config{FastThresholdMs: -1}.applyDefaults()
	if cfg.FastThresholdMs != want.FastThresholdMs {
		t.Errorf("negative FastThresholdMs should get default, got %d", cfg.FastThresholdMs)
	}
}

func TestNewMachine(t *testing.T) {
	m := NewMachine(DefaultConfig())
	if m.State() != IDLE {
		t.Errorf("initial state = %v, want IDLE", m.State())
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		want  string
		state State
	}{
		{"IDLE", IDLE},
		{"TYPING", TYPING},
		{"FAST_TYPING", FastTyping},
		{"PAUSED", PAUSED},
		{"UNKNOWN", State(99)},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestIdleToTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// First keystroke: IDLE -> TYPING, no request
	state, shouldRequest := m.OnKeystroke(1000)
	if state != TYPING {
		t.Errorf("state = %v, want TYPING", state)
	}
	if shouldRequest {
		t.Error("shouldRequest = true, want false (debounce)")
	}
}

func TestTypingToFastTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING
	m.OnKeystroke(1000)

	// TYPING -> FastTyping (delta 50ms < 100ms threshold)
	state, shouldRequest := m.OnKeystroke(1050)
	if state != FastTyping {
		t.Errorf("state = %v, want FastTyping", state)
	}
	if shouldRequest {
		t.Error("shouldRequest = true, want false (suppress during fast typing)")
	}
}

func TestTypingToPaused(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING
	m.OnKeystroke(1000)

	// TYPING -> PAUSED (delta 400ms > 300ms threshold)
	state, shouldRequest := m.OnKeystroke(1400)
	if state != PAUSED {
		t.Errorf("state = %v, want PAUSED", state)
	}
	if !shouldRequest {
		t.Error("shouldRequest = false, want true (pause triggers request)")
	}
}

func TestTypingStaysTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING
	m.OnKeystroke(1000)

	// TYPING -> TYPING (delta 200ms: between 100ms and 300ms)
	state, shouldRequest := m.OnKeystroke(1200)
	if state != TYPING {
		t.Errorf("state = %v, want TYPING", state)
	}
	if shouldRequest {
		t.Error("shouldRequest = true, want false (normal typing, no request)")
	}
}

func TestFastTypingStaysFast(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING -> FastTyping
	m.OnKeystroke(1000)
	m.OnKeystroke(1050)

	// FastTyping -> FastTyping (still fast, delta 40ms)
	state, shouldRequest := m.OnKeystroke(1090)
	if state != FastTyping {
		t.Errorf("state = %v, want FastTyping", state)
	}
	if shouldRequest {
		t.Error("shouldRequest = true, want false (still fast typing)")
	}
}

func TestFastTypingToPaused(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING -> FastTyping
	m.OnKeystroke(1000)
	m.OnKeystroke(1050)

	// FastTyping -> PAUSED (delta 400ms > 300ms)
	state, shouldRequest := m.OnKeystroke(1450)
	if state != PAUSED {
		t.Errorf("state = %v, want PAUSED", state)
	}
	if !shouldRequest {
		t.Error("shouldRequest = false, want true (pause after fast typing)")
	}
}

func TestPausedToTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING -> PAUSED
	m.OnKeystroke(1000)
	m.OnKeystroke(1400)

	// PAUSED -> TYPING (resumed)
	state, shouldRequest := m.OnKeystroke(1600)
	if state != TYPING {
		t.Errorf("state = %v, want TYPING", state)
	}
	if shouldRequest {
		t.Error("shouldRequest = true, want false (resumed typing, debounce)")
	}
}

func TestFullLifecycle(t *testing.T) {
	// Test the full: IDLE -> TYPING -> FastTyping -> PAUSED -> IDLE
	m := NewMachine(DefaultConfig())

	// IDLE -> TYPING
	state, req := m.OnKeystroke(1000)
	if state != TYPING || req {
		t.Fatalf("step 1: state=%v req=%v, want TYPING/false", state, req)
	}

	// TYPING -> FastTyping
	state, req = m.OnKeystroke(1050)
	if state != FastTyping || req {
		t.Fatalf("step 2: state=%v req=%v, want FastTyping/false", state, req)
	}

	// FastTyping continues
	state, req = m.OnKeystroke(1090)
	if state != FastTyping || req {
		t.Fatalf("step 3: state=%v req=%v, want FastTyping/false", state, req)
	}

	// FastTyping -> PAUSED
	state, req = m.OnKeystroke(1500)
	if state != PAUSED || !req {
		t.Fatalf("step 4: state=%v req=%v, want PAUSED/true", state, req)
	}

	// PAUSED -> IDLE (via idle timeout at 2s)
	state, req = m.OnIdle(3600)
	if state != IDLE || req {
		t.Fatalf("step 5: state=%v req=%v, want IDLE/false", state, req)
	}
}

func TestIdleTimeoutFromFastTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Get to FastTyping
	m.OnKeystroke(1000)
	m.OnKeystroke(1050)

	if m.State() != FastTyping {
		t.Fatalf("setup: state = %v, want FastTyping", m.State())
	}

	// Not yet timed out (1500ms since last keystroke, need 2000ms)
	state, req := m.OnIdle(2550)
	if state != FastTyping {
		t.Errorf("before timeout: state = %v, want FastTyping", state)
	}
	if req {
		t.Error("before timeout: shouldRequest = true, want false")
	}

	// Now timed out (2100ms since last keystroke at 1050)
	state, req = m.OnIdle(3050)
	if state != IDLE {
		t.Errorf("after timeout: state = %v, want IDLE", state)
	}
	if req {
		t.Error("after timeout: shouldRequest = true, want false")
	}
}

func TestIdleTimeoutFromPaused(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Get to PAUSED
	m.OnKeystroke(1000)
	m.OnKeystroke(1400)

	if m.State() != PAUSED {
		t.Fatalf("setup: state = %v, want PAUSED", m.State())
	}

	// Idle timeout from PAUSED: 2000ms after last keystroke at 1400
	state, req := m.OnIdle(3400)
	if state != IDLE {
		t.Errorf("state = %v, want IDLE", state)
	}
	if req {
		t.Error("shouldRequest = true, want false")
	}
}

func TestOnIdleWhenAlreadyIdle(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Already IDLE
	state, req := m.OnIdle(99999)
	if state != IDLE {
		t.Errorf("state = %v, want IDLE", state)
	}
	if req {
		t.Error("shouldRequest = true, want false")
	}
}

func TestOnIdleFromTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Get to TYPING
	m.OnKeystroke(1000)

	// 2000ms later, should go IDLE
	state, req := m.OnIdle(3000)
	if state != IDLE {
		t.Errorf("state = %v, want IDLE", state)
	}
	if req {
		t.Error("shouldRequest = true, want false")
	}
}

func TestReset(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Get to some non-IDLE state
	m.OnKeystroke(1000)
	m.OnKeystroke(1050)
	if m.State() != FastTyping {
		t.Fatalf("setup: state = %v, want FastTyping", m.State())
	}

	m.Reset()
	if m.State() != IDLE {
		t.Errorf("after Reset: state = %v, want IDLE", m.State())
	}

	// After reset, first keystroke should be IDLE -> TYPING again
	state, req := m.OnKeystroke(5000)
	if state != TYPING {
		t.Errorf("after Reset + keystroke: state = %v, want TYPING", state)
	}
	if req {
		t.Error("after Reset + keystroke: shouldRequest = true, want false")
	}
}

func TestCustomConfig(t *testing.T) {
	cfg := Config{
		FastThresholdMs:  50,
		PauseThresholdMs: 200,
		IdleTimeoutMs:    1000,
	}
	m := NewMachine(cfg)

	// IDLE -> TYPING
	m.OnKeystroke(1000)

	// With custom threshold of 50ms: delta 40ms should be FastTyping
	state, _ := m.OnKeystroke(1040)
	if state != FastTyping {
		t.Errorf("state = %v, want FastTyping (custom 50ms threshold)", state)
	}

	// With custom pause of 200ms: delta 250ms should be PAUSED
	state, req := m.OnKeystroke(1290)
	if state != PAUSED {
		t.Errorf("state = %v, want PAUSED (custom 200ms threshold)", state)
	}
	if !req {
		t.Error("shouldRequest = false, want true")
	}

	// With custom idle of 1000ms
	m.OnKeystroke(2000) // PAUSED -> TYPING
	state, _ = m.OnIdle(3000)
	if state != IDLE {
		t.Errorf("idle timeout: state = %v, want IDLE (custom 1000ms)", state)
	}
}

func TestFastTypingSuppression(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Simulate rapid typing: 10 keystrokes at 50ms intervals
	ts := int64(1000)
	m.OnKeystroke(ts) // IDLE -> TYPING

	for i := 0; i < 10; i++ {
		ts += 50 // 50ms intervals (fast)
		state, shouldRequest := m.OnKeystroke(ts)
		if shouldRequest {
			t.Errorf("keystroke %d: shouldRequest = true, want false during fast typing", i)
		}
		if i > 0 && state != FastTyping {
			t.Errorf("keystroke %d: state = %v, want FastTyping", i, state)
		}
	}
}

func TestPauseTriggersSuggestion(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Type fast, then pause
	m.OnKeystroke(1000)
	m.OnKeystroke(1050) // FastTyping
	m.OnKeystroke(1100) // Still FastTyping

	// Pause for 400ms
	state, shouldRequest := m.OnKeystroke(1500)
	if state != PAUSED {
		t.Errorf("state = %v, want PAUSED", state)
	}
	if !shouldRequest {
		t.Error("shouldRequest = false, want true (pause should trigger suggestion)")
	}
}

func TestHintFastTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Get to FastTyping
	m.OnKeystroke(1000)
	m.OnKeystroke(1050)

	hint := m.Hint()
	if hint.UserSpeedClass != "fast" {
		t.Errorf("UserSpeedClass = %q, want %q", hint.UserSpeedClass, "fast")
	}
	if hint.SuggestedPauseThresholdMs != 500 {
		t.Errorf("SuggestedPauseThresholdMs = %d, want 500", hint.SuggestedPauseThresholdMs)
	}
}

func TestHintPaused(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Get to PAUSED
	m.OnKeystroke(1000)
	m.OnKeystroke(1400)

	hint := m.Hint()
	if hint.UserSpeedClass != "moderate" {
		t.Errorf("UserSpeedClass = %q, want %q", hint.UserSpeedClass, "moderate")
	}
	if hint.SuggestedPauseThresholdMs != 150 {
		t.Errorf("SuggestedPauseThresholdMs = %d, want 150", hint.SuggestedPauseThresholdMs)
	}
}

func TestHintIdle(t *testing.T) {
	m := NewMachine(DefaultConfig())

	hint := m.Hint()
	if hint.UserSpeedClass != "exploratory" {
		t.Errorf("UserSpeedClass = %q, want %q", hint.UserSpeedClass, "exploratory")
	}
	if hint.SuggestedPauseThresholdMs != 0 {
		t.Errorf("SuggestedPauseThresholdMs = %d, want 0", hint.SuggestedPauseThresholdMs)
	}
}

func TestHintTyping(t *testing.T) {
	m := NewMachine(DefaultConfig())

	m.OnKeystroke(1000)

	hint := m.Hint()
	if hint.UserSpeedClass != "moderate" {
		t.Errorf("UserSpeedClass = %q, want %q", hint.UserSpeedClass, "moderate")
	}
	if hint.SuggestedPauseThresholdMs != 200 {
		t.Errorf("SuggestedPauseThresholdMs = %d, want 200", hint.SuggestedPauseThresholdMs)
	}
}

func TestConcurrentSafety(t *testing.T) {
	m := NewMachine(DefaultConfig())

	var wg sync.WaitGroup
	const goroutines = 100

	// Concurrent OnKeystroke calls
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			ts := int64(1000 + i*10)
			m.OnKeystroke(ts)
		}(i)
	}
	wg.Wait()

	// Machine should be in a valid state
	state := m.State()
	if state < IDLE || state > PAUSED {
		t.Errorf("invalid state after concurrent writes: %v", state)
	}

	// Concurrent OnIdle calls
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			m.OnIdle(int64(5000 + i*10))
		}(i)
	}
	wg.Wait()

	// Concurrent State/Hint reads while writing
	wg.Add(goroutines * 3)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			m.OnKeystroke(int64(10000 + i*5))
		}(i)
		go func() {
			defer wg.Done()
			_ = m.State()
		}()
		go func() {
			defer wg.Done()
			_ = m.Hint()
		}()
	}
	wg.Wait()

	// Concurrent Reset
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.Reset()
		}()
	}
	wg.Wait()

	if m.State() != IDLE {
		t.Errorf("after concurrent Reset: state = %v, want IDLE", m.State())
	}
}

func TestBoundaryValues(t *testing.T) {
	cfg := Config{
		FastThresholdMs:  100,
		PauseThresholdMs: 300,
		IdleTimeoutMs:    2000,
	}
	m := NewMachine(cfg)

	// Exactly at fast threshold (delta == 100ms): NOT fast (need < 100)
	m.OnKeystroke(1000)
	state, _ := m.OnKeystroke(1100)
	if state != TYPING {
		t.Errorf("delta=100ms (exactly at fast threshold): state = %v, want TYPING", state)
	}

	// Exactly at pause threshold (delta == 300ms): NOT paused (need > 300)
	m.Reset()
	m.OnKeystroke(1000)
	state, req := m.OnKeystroke(1300)
	if state != TYPING {
		t.Errorf("delta=300ms (exactly at pause threshold): state = %v, want TYPING", state)
	}
	if req {
		t.Error("delta=300ms: shouldRequest = true, want false")
	}

	// Just above pause threshold (delta == 301ms): PAUSED
	m.Reset()
	m.OnKeystroke(1000)
	state, req = m.OnKeystroke(1301)
	if state != PAUSED {
		t.Errorf("delta=301ms: state = %v, want PAUSED", state)
	}
	if !req {
		t.Error("delta=301ms: shouldRequest = false, want true")
	}

	// Just below fast threshold (delta == 99ms): FastTyping
	m.Reset()
	m.OnKeystroke(1000)
	state, _ = m.OnKeystroke(1099)
	if state != FastTyping {
		t.Errorf("delta=99ms: state = %v, want FastTyping", state)
	}
}

func TestIdleTimeoutBoundary(t *testing.T) {
	m := NewMachine(DefaultConfig())

	m.OnKeystroke(1000)
	// Exactly at idle timeout boundary (2000ms)
	state, _ := m.OnIdle(3000)
	if state != IDLE {
		t.Errorf("exactly at idle timeout: state = %v, want IDLE", state)
	}

	// Just below idle timeout (1999ms since last keystroke)
	m.Reset()
	m.OnKeystroke(1000)
	state, _ = m.OnIdle(2999)
	if state != TYPING {
		t.Errorf("just below idle timeout: state = %v, want TYPING", state)
	}
}

func TestMultiplePauseCycles(t *testing.T) {
	m := NewMachine(DefaultConfig())

	// Cycle 1: type, pause, get suggestion
	m.OnKeystroke(1000)               // IDLE -> TYPING
	state, req := m.OnKeystroke(1400) // TYPING -> PAUSED
	if state != PAUSED || !req {
		t.Fatalf("cycle 1 pause: state=%v req=%v", state, req)
	}

	// Resume typing
	m.OnKeystroke(1600) // PAUSED -> TYPING

	// Cycle 2: type fast, then pause
	m.OnKeystroke(1650)              // TYPING -> FastTyping
	state, req = m.OnKeystroke(2050) // FastTyping -> PAUSED
	if state != PAUSED || !req {
		t.Fatalf("cycle 2 pause: state=%v req=%v", state, req)
	}

	// Cycle 3: resume and pause again
	m.OnKeystroke(2200)              // PAUSED -> TYPING
	state, req = m.OnKeystroke(2600) // TYPING -> PAUSED
	if state != PAUSED || !req {
		t.Fatalf("cycle 3 pause: state=%v req=%v", state, req)
	}
}
