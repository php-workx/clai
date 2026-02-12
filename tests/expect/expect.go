// Package expect provides shell session testing utilities using go-expect.
//
// It wraps the Netflix go-expect library to provide easy-to-use interactive
// shell testing for zsh, bash, and fish shells.
package expect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	expect "github.com/Netflix/go-expect"
)

// ContainerTestSem limits concurrent tests in containers to reduce resource contention.
// In container environments (Docker), running all tests in parallel causes CPU contention
// that leads to timing-related test failures. This semaphore limits concurrency to 2.
var ContainerTestSem = make(chan struct{}, 2)

// AcquireTestSlot limits parallelism in container environments.
// Call this after t.Parallel() in tests that are timing-sensitive.
// In containers, this blocks until a slot is available (max 2 concurrent tests).
// On local machines, this is a no-op.
func AcquireTestSlot(t *testing.T) {
	if IsRunningInContainer() {
		ContainerTestSem <- struct{}{}
		t.Cleanup(func() { <-ContainerTestSem })
	}
}

// IsRunningInContainer detects if we're running inside a Docker container.
func IsRunningInContainer() bool {
	// Check for /.dockerenv file (Docker-specific)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Check cgroup for docker/lxc indicators
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "lxc") {
			return true
		}
	}
	return false
}

// IsRunningOnAlpine detects if we're running on Alpine Linux (musl libc).
// Alpine has different performance characteristics due to musl vs glibc.
func IsRunningOnAlpine() bool {
	if _, err := os.Stat("/etc/alpine-release"); err == nil {
		return true
	}
	return false
}

// Key constants for special keys (ANSI escape sequences)
const (
	KeyRight     = "\x1b[C"
	KeyLeft      = "\x1b[D"
	KeyUp        = "\x1b[A"
	KeyDown      = "\x1b[B"
	KeyEscape    = "\x1b"
	KeyEnter     = "\r"
	KeyTab       = "\t"
	KeyCtrlC     = "\x03"
	KeyCtrlD     = "\x04"
	KeyCtrlSpace = "\x00"
	KeyCtrlX     = "\x18"
	KeyCtrlV     = "\x16"
	KeyAltEnter  = "\x1b\r"
)

// ShellSession wraps go-expect for interactive shell testing.
type ShellSession struct {
	Console *expect.Console
	Shell   string
	Timeout time.Duration
	cmd     *exec.Cmd
}

// SessionOption configures a ShellSession.
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	timeout    time.Duration
	env        []string
	showOutput bool
	rcFile     string
	claiInit   bool // Use eval "$(clai init <shell>)" instead of sourcing RC file
}

// WithTimeout sets the default timeout for expect operations.
func WithTimeout(d time.Duration) SessionOption {
	return func(c *sessionConfig) {
		c.timeout = d
	}
}

// WithEnv adds environment variables to the shell session.
func WithEnv(env ...string) SessionOption {
	return func(c *sessionConfig) {
		c.env = append(c.env, env...)
	}
}

// WithOutput enables output to stdout for debugging.
func WithOutput(show bool) SessionOption {
	return func(c *sessionConfig) {
		c.showOutput = show
	}
}

// WithRCFile sources a specific RC file on startup.
func WithRCFile(path string) SessionOption {
	return func(c *sessionConfig) {
		c.rcFile = path
	}
}

// WithClaiInit uses eval "$(clai init <shell>)" to load shell integration.
// This is preferred over WithRCFile for tests that need proper session ID generation.
func WithClaiInit() SessionOption {
	return func(c *sessionConfig) {
		c.claiInit = true
	}
}

// NewSession starts a new interactive shell session.
//
// The shell parameter should be "zsh", "bash", or "fish".
// The session is started with no RC files (-f flag for zsh/bash, --no-config for fish)
// to ensure a clean environment.
func NewSession(shell string, opts ...SessionOption) (*ShellSession, error) {
	cfg := &sessionConfig{
		timeout: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	shellPath, err := exec.LookPath(shell)
	if err != nil {
		return nil, fmt.Errorf("shell %q not found: %w", shell, err)
	}

	// Build console options
	var consoleOpts []expect.ConsoleOpt
	consoleOpts = append(consoleOpts, expect.WithDefaultTimeout(cfg.timeout))
	if cfg.showOutput {
		consoleOpts = append(consoleOpts, expect.WithStdout(os.Stdout))
	}

	console, err := expect.NewConsole(consoleOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create console: %w", err)
	}

	// Build shell command with appropriate flags
	var args []string
	switch shell {
	case "zsh":
		// -f: no rc files, -i: interactive
		args = []string{"-f", "-i"}
	case "bash":
		// --norc: no rc files, --noprofile: no profile, -i: interactive
		args = []string{"--norc", "--noprofile", "-i"}
	case "fish":
		// --no-config: no config files, --interactive: interactive
		args = []string{"--no-config", "--interactive"}
	default:
		args = []string{"-i"}
	}

	cmd := exec.Command(shellPath, args...) //nolint:gosec // G204: shellPath is from test config
	cmd.Stdin = console.Tty()
	cmd.Stdout = console.Tty()
	cmd.Stderr = console.Tty()

	// Set environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, cfg.env...)
	// Ensure TERM is set for proper terminal handling
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	if err := cmd.Start(); err != nil {
		console.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	session := &ShellSession{
		Console: console,
		Shell:   shell,
		Timeout: cfg.timeout,
		cmd:     cmd,
	}

	// Wait for shell to be ready (look for prompt or timeout)
	time.Sleep(100 * time.Millisecond)

	// Load shell integration
	if cfg.claiInit {
		// Use eval "$(clai init <shell>)" for proper session ID generation
		var initCmd string
		switch shell {
		case "fish":
			initCmd = "clai init fish | source"
		default:
			initCmd = fmt.Sprintf(`eval "$(clai init %s)"`, shell)
		}
		session.SendLine(initCmd)
		time.Sleep(300 * time.Millisecond)
	} else if cfg.rcFile != "" {
		// Source RC file directly (legacy, doesn't replace placeholders)
		absPath, err := filepath.Abs(cfg.rcFile)
		if err != nil {
			session.Close()
			return nil, fmt.Errorf("failed to resolve rc file path: %w", err)
		}
		if _, err := os.Stat(absPath); err != nil {
			session.Close()
			return nil, fmt.Errorf("rc file not found: %w", err)
		}
		session.SendLine(fmt.Sprintf("source %q", absPath))
		time.Sleep(200 * time.Millisecond)
	}

	return session, nil
}

// Send sends text to the shell without a newline.
func (s *ShellSession) Send(text string) error {
	_, err := s.Console.Send(text)
	return err
}

// SendLine sends text followed by a newline.
func (s *ShellSession) SendLine(text string) error {
	_, err := s.Console.SendLine(text)
	return err
}

// SendKey sends a special key (use Key* constants).
func (s *ShellSession) SendKey(key string) error {
	_, err := s.Console.Send(key)
	return err
}

// Expect waits for an exact string match in the output.
func (s *ShellSession) Expect(str string) (string, error) {
	return s.Console.ExpectString(str)
}

// ExpectTimeout waits for an exact string match with a specific timeout.
func (s *ShellSession) ExpectTimeout(str string, timeout time.Duration) (string, error) {
	return s.Console.Expect(expect.String(str), expect.WithTimeout(timeout))
}

// ExpectRegex waits for a regex pattern match in the output.
func (s *ShellSession) ExpectRegex(pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	return s.Console.Expect(expect.Regexp(re))
}

// ExpectRegexTimeout waits for a regex pattern match with a specific timeout.
func (s *ShellSession) ExpectRegexTimeout(pattern string, timeout time.Duration) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	return s.Console.Expect(expect.Regexp(re), expect.WithTimeout(timeout))
}

// ExpectEOF waits for the shell to close.
func (s *ShellSession) ExpectEOF() (string, error) {
	return s.Console.ExpectEOF()
}

// Close terminates the shell session.
func (s *ShellSession) Close() error {
	// Send exit command
	s.SendLine("exit")

	// Close the console (this closes the pty)
	if err := s.Console.Close(); err != nil {
		return err
	}

	// Wait for the process to exit
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}

	return nil
}

// WaitForPrompt waits for a shell prompt to appear.
// This is a heuristic that works for most default prompts.
func (s *ShellSession) WaitForPrompt() error {
	// Most prompts end with $, >, or #
	_, err := s.ExpectRegexTimeout(`[$>#%]\s*$`, 2*time.Second)
	return err
}

// ClearBuffer sends Ctrl+C to clear any pending input.
func (s *ShellSession) ClearBuffer() error {
	return s.SendKey(KeyCtrlC)
}

// GetOutput reads any pending output without waiting.
func (s *ShellSession) GetOutput() string {
	// Try to read with a very short timeout
	output, _ := s.ExpectRegexTimeout(".*", 100*time.Millisecond)
	return output
}

// FindHookFile searches for a clai hook file in common locations.
func FindHookFile(name string) string {
	// Get shell name from filename (e.g., "clai.zsh" -> "zsh")
	ext := filepath.Ext(name)
	shellName := strings.TrimPrefix(ext, ".")

	// Try relative paths first (for running from project root)
	paths := []string{
		filepath.Join("internal", "cmd", "shell", shellName, name),
		filepath.Join("hooks", name),
	}

	// Get current working directory
	cwd, _ := os.Getwd()

	// Try current directory and parents
	for i := 0; i < 5; i++ {
		for _, p := range paths {
			fullPath := filepath.Join(cwd, p)
			if _, err := os.Stat(fullPath); err == nil {
				return fullPath
			}
		}
		cwd = filepath.Dir(cwd)
	}

	return ""
}

// SkipIfShellMissing skips the test if the specified shell is not available.
func SkipIfShellMissing(t interface{ Skip(args ...interface{}) }, shell string) {
	if _, err := exec.LookPath(shell); err != nil {
		t.Skip(fmt.Sprintf("%s not available, skipping", shell))
	}
}

// SkipIfClaiMissing skips the test if clai binary is not available.
func SkipIfClaiMissing(t interface{ Skip(args ...interface{}) }) {
	if _, err := exec.LookPath("clai"); err != nil {
		t.Skip("clai not available, skipping")
	}
}

// SkipIfShort skips the test if running in short mode.
func SkipIfShort(t interface {
	Skip(args ...interface{})
	Short() bool
}, reason string) {
	if t.Short() {
		t.Skip("skipping in short mode: " + reason)
	}
}
