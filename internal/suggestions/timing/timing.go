// Package timing implements an adaptive suggestion cadence state machine.
//
// The state machine tracks typing activity to suppress suggestions during
// fast typing and request them when the user pauses. This reduces noise
// for fast typists while keeping suggestions responsive for exploratory users.
//
// States:
//   - IDLE: No typing activity. Suggestions can be requested immediately.
//   - TYPING: User has started typing. Short debounce window.
//   - FastTyping: Rapid keystrokes detected (inter-keystroke < FastThresholdMs).
//     Suggestions are suppressed.
//   - PAUSED: User paused after typing (inter-keystroke > PauseThresholdMs).
//     Suggestions should be requested.
package timing

import "sync"

// State represents the current typing state.
type State int

const (
	// IDLE means no typing activity. Suggestions can be requested immediately.
	IDLE State = iota
	// TYPING means the user has started typing. Short debounce window.
	TYPING
	// FastTyping means rapid keystrokes detected. Suppress suggestions.
	FastTyping
	// PAUSED means the user paused after typing. Request suggestion.
	PAUSED
)

// String returns the human-readable name of the state.
func (s State) String() string {
	switch s {
	case IDLE:
		return "IDLE"
	case TYPING:
		return "TYPING"
	case FastTyping:
		return "FAST_TYPING"
	case PAUSED:
		return "PAUSED"
	default:
		return "UNKNOWN"
	}
}

// Config holds the configurable thresholds for the timing state machine.
type Config struct {
	// FastThresholdMs is the maximum inter-keystroke interval in milliseconds
	// to be classified as fast typing. Keystrokes arriving faster than this
	// threshold transition the machine to FastTyping.
	// Default: 100ms.
	FastThresholdMs int64

	// PauseThresholdMs is the minimum inter-keystroke interval in milliseconds
	// to be classified as a pause. When a keystroke arrives after this interval,
	// the machine transitions to PAUSED and a suggestion request is triggered.
	// Default: 300ms.
	PauseThresholdMs int64

	// IdleTimeoutMs is the inactivity duration in milliseconds after which the
	// machine returns to IDLE from FastTyping or PAUSED.
	// Default: 2000ms.
	IdleTimeoutMs int64
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		FastThresholdMs:  100,
		PauseThresholdMs: 300,
		IdleTimeoutMs:    2000,
	}
}

// applyDefaults fills in zero-valued fields with defaults.
func (c Config) applyDefaults() Config {
	d := DefaultConfig()
	if c.FastThresholdMs <= 0 {
		c.FastThresholdMs = d.FastThresholdMs
	}
	if c.PauseThresholdMs <= 0 {
		c.PauseThresholdMs = d.PauseThresholdMs
	}
	if c.IdleTimeoutMs <= 0 {
		c.IdleTimeoutMs = d.IdleTimeoutMs
	}
	return c
}

// TimingHint is returned alongside suggestions to tell the shell integration
// how long to wait before the next suggestion request.
type TimingHint struct {
	// UserSpeedClass categorizes the user's typing speed.
	// Values: "fast", "moderate", "exploratory".
	UserSpeedClass string `json:"user_speed_class"`

	// SuggestedPauseThresholdMs is the recommended wait in milliseconds before
	// the shell should issue the next suggestion request.
	SuggestedPauseThresholdMs int64 `json:"suggested_pause_threshold_ms"`
}

// Machine is the adaptive typing cadence state machine. It is safe for
// concurrent use.
type Machine struct {
	mu              sync.Mutex
	config          Config
	state           State
	lastKeystrokeMs int64
}

func (m *Machine) transitionToPaused() (State, bool) {
	m.state = PAUSED
	return PAUSED, true
}

// NewMachine creates a new timing state machine with the given config.
// Zero-valued config fields are replaced with defaults.
func NewMachine(config Config) *Machine {
	return &Machine{
		config: config.applyDefaults(),
		state:  IDLE,
	}
}

// OnKeystroke processes a keystroke event at the given timestamp (Unix ms).
// It returns the new state and whether a suggestion should be requested.
//
// Transition rules:
//   - IDLE -> TYPING: first keystroke, no request (debounce)
//   - TYPING -> FastTyping: inter-keystroke < FastThresholdMs, suppress request
//   - TYPING -> PAUSED: inter-keystroke > PauseThresholdMs, request
//   - FastTyping -> FastTyping: still fast, suppress
//   - FastTyping -> PAUSED: paused after fast, request
//   - PAUSED -> TYPING: resumed typing, no request (debounce)
func (m *Machine) OnKeystroke(nowMs int64) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delta := nowMs - m.lastKeystrokeMs
	m.lastKeystrokeMs = nowMs

	if m.state != IDLE && m.state != TYPING && m.state != FastTyping && m.state != PAUSED {
		m.state = IDLE
		return IDLE, false
	}

	switch m.state {
	case IDLE, PAUSED:
		// First keystroke or resumed typing after pause.
		m.state = TYPING
		return TYPING, false

	case TYPING, FastTyping:
		if delta > m.config.PauseThresholdMs {
			return m.transitionToPaused()
		}
		if m.state == TYPING {
			if delta < m.config.FastThresholdMs {
				m.state = FastTyping
				return FastTyping, false
			}
			// Still in normal typing range — stay in TYPING, no request
			return TYPING, false
		}
		// Still fast typing — suppress
		return FastTyping, false
	}

	// Defensive fallback for unexpected state transitions.
	m.state = IDLE
	return IDLE, false
}

// OnIdle checks whether the machine should transition to IDLE due to
// inactivity. nowMs is the current timestamp in Unix ms. Returns the
// new state and whether a suggestion should be requested (always false
// for idle transitions).
func (m *Machine) OnIdle(nowMs int64) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == IDLE {
		return IDLE, false
	}

	delta := nowMs - m.lastKeystrokeMs
	if delta >= m.config.IdleTimeoutMs {
		m.state = IDLE
		return IDLE, false
	}

	return m.state, false
}

// State returns the current state. Safe for concurrent use.
func (m *Machine) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Reset returns the machine to the IDLE state and clears the last
// keystroke timestamp.
func (m *Machine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = IDLE
	m.lastKeystrokeMs = 0
}

// Hint returns a TimingHint based on the current state. The hint tells
// the shell integration how long to wait before issuing the next
// suggestion request.
//
// Timing hints by state:
//   - FastTyping: 500ms wait (longer, to let fast typing settle)
//   - PAUSED: 150ms wait (normal, responsive)
//   - IDLE: 0ms (immediate)
//   - TYPING: 200ms wait (moderate debounce)
func (m *Machine) Hint() TimingHint {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state {
	case FastTyping:
		return TimingHint{
			UserSpeedClass:            "fast",
			SuggestedPauseThresholdMs: 500,
		}
	case PAUSED:
		return TimingHint{
			UserSpeedClass:            "moderate",
			SuggestedPauseThresholdMs: 150,
		}
	case TYPING:
		return TimingHint{
			UserSpeedClass:            "moderate",
			SuggestedPauseThresholdMs: 200,
		}
	default: // IDLE
		return TimingHint{
			UserSpeedClass:            "exploratory",
			SuggestedPauseThresholdMs: 0,
		}
	}
}
