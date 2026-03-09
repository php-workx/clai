package discovery

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunner_NewRunner(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(DefaultRunnerConfig())
	require.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestRunner_NewRunner_DefaultValues(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{})
	require.NoError(t, err)

	assert.Equal(t, 500*time.Millisecond, runner.cfg.Timeout)
	assert.Equal(t, int64(1<<20), runner.cfg.MaxOutputBytes)
}

func TestRunner_Run_Success(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(DefaultRunnerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	result, err := runner.Run(ctx, "echo", "hello world")
	require.NoError(t, err)

	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, string(result.Stdout), "hello world")
	assert.True(t, result.Duration > 0)
}

func TestRunner_Run_NonZeroExit(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(DefaultRunnerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	result, err := runner.Run(ctx, "sh", "-c", "exit 42")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRunnerNonZeroExit)
	assert.Equal(t, 42, result.ExitCode)
}

func TestRunner_Run_Timeout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(RunnerConfig{
		Timeout: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = runner.Run(ctx, "sleep", "5")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRunnerTimeout)
}

func TestRunner_Run_OutputLimit(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(RunnerConfig{
		MaxOutputBytes: 10, // Very small limit
		Timeout:        5 * time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := runner.Run(ctx, "sh", "-c", "yes | head -1000")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRunnerOutputLimit)
	assert.LessOrEqual(t, len(result.Stdout), 10)
}

func TestRunner_Run_WorkingDir(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(RunnerConfig{
		WorkingDir: "/tmp",
	})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := runner.Run(ctx, "pwd")
	require.NoError(t, err)
	assert.Contains(t, string(result.Stdout), "/tmp") // May be /private/tmp on macOS
}

func TestRunner_Run_NoStdin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(DefaultRunnerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	// This should not hang waiting for stdin
	result, err := runner.Run(ctx, "cat")
	require.NoError(t, err)
	assert.Empty(t, result.Stdout)
}

func TestRunner_RunShell(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(DefaultRunnerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	result, err := runner.RunShell(ctx, "echo hello && echo world")
	require.NoError(t, err)
	assert.Contains(t, string(result.Stdout), "hello")
	assert.Contains(t, string(result.Stdout), "world")
}

func TestRunner_ContextCancellation(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	runner, err := NewRunner(RunnerConfig{
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = runner.Run(ctx, "sleep", "5")
	require.Error(t, err)
}

func TestSanitizeEnvironment(t *testing.T) {
	t.Parallel()

	env := sanitizeEnvironment()

	// Should contain PATH
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			found = true
			break
		}
	}
	assert.True(t, found, "PATH should be in sanitized environment")

	// Should not contain any secrets
	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		upperKey := strings.ToUpper(key)
		assert.NotContains(t, upperKey, "SECRET")
		assert.NotContains(t, upperKey, "PASSWORD")
		assert.NotContains(t, upperKey, "TOKEN")
		assert.NotContains(t, upperKey, "PRIVATE_KEY")
	}
}

func TestLimitedBuffer_Write(t *testing.T) {
	t.Parallel()

	buf := &limitedBuffer{limit: 10}

	// Write within limit
	n, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.False(t, buf.exceeded)

	// Write to reach limit
	n, err = buf.Write([]byte("world"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.False(t, buf.exceeded)

	// Write over limit
	n, err = buf.Write([]byte("extra"))
	require.NoError(t, err)
	assert.Equal(t, 5, n) // Reports success but discards
	assert.True(t, buf.exceeded)

	// Buffer should only contain first 10 bytes
	assert.Equal(t, "helloworld", buf.String())
}

func TestDiscoveryErrorTracker(t *testing.T) {
	t.Parallel()

	tracker := NewDiscoveryErrorTracker(5)
	assert.Equal(t, 0, tracker.Count())

	// Record some errors
	tracker.Record("makefile", "make -qp", "parse error", "/repo1")
	tracker.Record("package.json", "cat package.json", "file not found", "/repo2")
	tracker.Record("justfile", "just --list", "timeout", "/repo3")

	assert.Equal(t, 3, tracker.Count())

	// Get recent errors
	recent := tracker.GetRecent(2)
	assert.Len(t, recent, 2)
	assert.Equal(t, "justfile", recent[0].Kind) // Most recent first
	assert.Equal(t, "package.json", recent[1].Kind)

	// Get all errors
	all := tracker.GetRecent(0)
	assert.Len(t, all, 3)

	// Clear errors
	tracker.Clear()
	assert.Equal(t, 0, tracker.Count())
}

func TestDiscoveryErrorTracker_RingBuffer(t *testing.T) {
	t.Parallel()

	tracker := NewDiscoveryErrorTracker(3)

	// Fill the buffer
	tracker.Record("kind1", "cmd1", "err1", "/repo1")
	tracker.Record("kind2", "cmd2", "err2", "/repo2")
	tracker.Record("kind3", "cmd3", "err3", "/repo3")

	// Add one more to trigger ring buffer
	tracker.Record("kind4", "cmd4", "err4", "/repo4")

	assert.Equal(t, 3, tracker.Count())

	// Most recent should be kind4, oldest (kind1) should be overwritten
	recent := tracker.GetRecent(3)
	kinds := make([]string, len(recent))
	for i, r := range recent {
		kinds[i] = r.Kind
	}
	assert.Contains(t, kinds, "kind4")
	assert.Contains(t, kinds, "kind3")
	assert.Contains(t, kinds, "kind2")
	assert.NotContains(t, kinds, "kind1") // Overwritten
}

func TestDefaultRunnerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultRunnerConfig()
	assert.Equal(t, 500*time.Millisecond, cfg.Timeout)
	assert.Equal(t, int64(1<<20), cfg.MaxOutputBytes)
	assert.False(t, cfg.AllowRoot)
	assert.NotNil(t, cfg.Logger)
}
