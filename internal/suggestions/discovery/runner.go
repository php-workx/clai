package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"
)

// Runner errors.
var (
	ErrRunnerTimeout     = errors.New("discovery runner timeout")
	ErrRunnerOutputLimit = errors.New("discovery runner output exceeded limit")
	ErrRunnerRootUser    = errors.New("discovery runner cannot run as root")
	ErrRunnerNonZeroExit = errors.New("discovery runner exited with non-zero status")
)

// RunnerConfig configures the command runner safety constraints.
type RunnerConfig struct {
	Logger         *slog.Logger
	WorkingDir     string
	Timeout        time.Duration
	MaxOutputBytes int64
	AllowRoot      bool
}

// DefaultRunnerConfig returns the default runner configuration.
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		Timeout:        500 * time.Millisecond,
		MaxOutputBytes: 1 << 20, // 1MB
		AllowRoot:      false,
		Logger:         slog.Default(),
	}
}

// RunnerResult contains the result of a runner execution.
type RunnerResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// Runner executes discovery commands with safety constraints.
// Per spec Section 10.2.1.
type Runner struct {
	cfg RunnerConfig
}

// NewRunner creates a new safe command runner.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 500 * time.Millisecond
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 1 << 20
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Check if running as root
	if !cfg.AllowRoot {
		currentUser, err := user.Current()
		if err == nil && currentUser.Uid == "0" {
			return nil, ErrRunnerRootUser
		}
	}

	return &Runner{cfg: cfg}, nil
}

// Run executes a command with safety constraints.
// Per spec Section 10.2.1:
// - working directory = repo root
// - timeout enforced
// - output cap enforced
// - no stdin
// - environment sanitized
func (r *Runner) Run(ctx context.Context, command string, args ...string) (*RunnerResult, error) {
	start := time.Now()

	// Create context with timeout
	runCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(runCtx, command, args...) //nolint:gosec // command and args are controlled by caller

	// Set working directory
	if r.cfg.WorkingDir != "" {
		cmd.Dir = r.cfg.WorkingDir
	}

	// No stdin - close stdin immediately
	cmd.Stdin = nil

	// Create limited writers for stdout/stderr
	stdout := &limitedBuffer{limit: r.cfg.MaxOutputBytes}
	stderr := &limitedBuffer{limit: r.cfg.MaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Sanitize environment - only pass safe variables
	cmd.Env = sanitizeEnvironment()

	// Run command
	err := cmd.Run()
	result := &RunnerResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: time.Since(start),
	}

	// Check for output limit exceeded
	if stdout.exceeded || stderr.exceeded {
		r.cfg.Logger.Warn("discovery runner output exceeded limit",
			"command", command,
			"args", args,
			"limit", r.cfg.MaxOutputBytes,
		)
		return result, ErrRunnerOutputLimit
	}

	// Check for timeout
	if runCtx.Err() == context.DeadlineExceeded {
		r.cfg.Logger.Warn("discovery runner timeout",
			"command", command,
			"args", args,
			"timeout", r.cfg.Timeout,
		)
		return result, ErrRunnerTimeout
	}

	// Check for non-zero exit
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			r.cfg.Logger.Warn("discovery runner non-zero exit",
				"command", command,
				"args", args,
				"exit_code", result.ExitCode,
				"stderr", string(result.Stderr),
			)
			return result, fmt.Errorf("%w: exit code %d", ErrRunnerNonZeroExit, result.ExitCode)
		}
		return result, err
	}

	return result, nil
}

// RunShell executes a shell command with safety constraints.
func (r *Runner) RunShell(ctx context.Context, shellCmd string) (*RunnerResult, error) {
	return r.Run(ctx, "sh", "-c", shellCmd)
}

// sanitizeEnvironment returns a sanitized environment for command execution.
// Only includes safe variables, excludes secrets and sensitive data.
func sanitizeEnvironment() []string {
	// Safe environment variables to pass through
	safeVars := []string{
		"PATH",
		"HOME",
		"USER",
		"SHELL",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"TERM",
		"TMPDIR",
		"TMP",
		"TEMP",
		// Build tool variables
		"GOPATH",
		"GOROOT",
		"NODE_PATH",
		"NPM_CONFIG_PREFIX",
		"CARGO_HOME",
		"RUSTUP_HOME",
	}

	// Prefixes of variables to exclude
	excludePrefixes := []string{
		"AWS_",
		"AZURE_",
		"GCP_",
		"GOOGLE_",
		"GITHUB_TOKEN",
		"GITLAB_TOKEN",
		"NPM_TOKEN",
		"API_KEY",
		"SECRET",
		"PASSWORD",
		"PRIVATE_KEY",
		"CREDENTIALS",
		"AUTH_",
	}

	env := os.Environ()
	sanitized := make([]string, 0, len(safeVars))

	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]

		// Check if in safe list
		isSafe := false
		for _, safe := range safeVars {
			if key == safe {
				isSafe = true
				break
			}
		}

		// Check if has excluded prefix
		isExcluded := false
		for _, prefix := range excludePrefixes {
			if strings.HasPrefix(strings.ToUpper(key), strings.ToUpper(prefix)) {
				isExcluded = true
				break
			}
		}

		if isSafe && !isExcluded {
			sanitized = append(sanitized, e)
		}
	}

	return sanitized
}

// limitedBuffer is a bytes.Buffer that limits the amount of data written.
type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int64
	exceeded bool
	mu       sync.Mutex
}

func (lb *limitedBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.exceeded {
		return len(p), nil // Discard but report success
	}

	remaining := lb.limit - int64(lb.buf.Len())
	if remaining <= 0 {
		lb.exceeded = true
		return len(p), nil
	}

	if int64(len(p)) > remaining {
		p = p[:remaining]
		lb.exceeded = true
	}

	return lb.buf.Write(p)
}

func (lb *limitedBuffer) Bytes() []byte {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.buf.Bytes()
}

func (lb *limitedBuffer) String() string {
	return string(lb.Bytes())
}

// Ensure limitedBuffer implements io.Writer
var _ io.Writer = (*limitedBuffer)(nil)

// DiscoveryErrorTracker tracks recent discovery errors for debugging.
// Per spec Section 10.2.1: "Debug endpoint /debug/discovery-errors shows recent failures"
type DiscoveryErrorTracker struct {
	errors   []DiscoveryError
	maxSize  int
	position int
	mu       sync.RWMutex
}

// DiscoveryError represents a discovery failure.
type DiscoveryError struct {
	Timestamp time.Time
	Kind      string
	Command   string
	Error     string
	RepoKey   string
}

// NewDiscoveryErrorTracker creates a new error tracker.
func NewDiscoveryErrorTracker(maxSize int) *DiscoveryErrorTracker {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &DiscoveryErrorTracker{
		errors:  make([]DiscoveryError, 0, maxSize),
		maxSize: maxSize,
	}
}

// Record records a discovery error.
func (t *DiscoveryErrorTracker) Record(kind, command, errMsg, repoKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	err := DiscoveryError{
		Timestamp: time.Now(),
		Kind:      kind,
		Command:   command,
		Error:     errMsg,
		RepoKey:   repoKey,
	}

	if len(t.errors) < t.maxSize {
		t.errors = append(t.errors, err)
	} else {
		// Ring buffer - overwrite oldest
		t.errors[t.position] = err
		t.position = (t.position + 1) % t.maxSize
	}
}

// GetRecent returns the most recent discovery errors.
func (t *DiscoveryErrorTracker) GetRecent(limit int) []DiscoveryError {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.errors) {
		limit = len(t.errors)
	}

	// Return in reverse chronological order
	result := make([]DiscoveryError, 0, limit)
	n := len(t.errors)
	for i := 0; i < limit && i < n; i++ {
		idx := (t.position - 1 - i + n) % n
		if idx < 0 {
			idx += n
		}
		if idx < len(t.errors) {
			result = append(result, t.errors[idx])
		}
	}

	return result
}

// Clear clears all recorded errors.
func (t *DiscoveryErrorTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errors = t.errors[:0]
	t.position = 0
}

// Count returns the number of recorded errors.
func (t *DiscoveryErrorTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.errors)
}
