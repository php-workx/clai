package signal

import (
	"context"
	"errors"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetup_NilConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, h := Setup(ctx, nil)
	defer h.Stop()

	// Should not panic with nil config
	assert.NotNil(t, h)
}

func TestSetup_CancelsContextOnShutdown(t *testing.T) {
	// Skip in parallel since we're sending signals to the process
	// This test sends SIGTERM which is safe in a controlled test environment

	ctx := context.Background()
	ctx, h := Setup(ctx, &Config{})

	// Context should not be canceled yet
	select {
	case <-ctx.Done():
		t.Fatal("context should not be canceled before signal")
	default:
		// Good, context is not canceled
	}

	// Send SIGTERM to ourselves
	err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	require.NoError(t, err)

	// Context should be canceled within a reasonable time
	select {
	case <-ctx.Done():
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("context was not canceled after SIGTERM")
	}

	h.Wait()
}

func TestSetup_CallsShutdownFn(t *testing.T) {
	ctx := context.Background()

	var shutdownCalled atomic.Bool
	var receivedCtx context.Context

	_, h := Setup(ctx, &Config{
		ShutdownFn: func(ctx context.Context) error {
			shutdownCalled.Store(true)
			receivedCtx = ctx
			return nil
		},
	})

	// Send SIGTERM
	err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	require.NoError(t, err)

	// Wait for handler to finish
	h.Wait()

	assert.True(t, shutdownCalled.Load(), "shutdown function should have been called")
	assert.NotNil(t, receivedCtx, "shutdown function should receive context")
}

func TestSetup_ShutdownFnError(t *testing.T) {
	ctx := context.Background()

	ctx, h := Setup(ctx, &Config{
		ShutdownFn: func(ctx context.Context) error {
			return errors.New("shutdown error")
		},
	})

	// Send SIGTERM
	err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	require.NoError(t, err)

	// Wait for handler to finish - should not panic on error
	h.Wait()

	// Context should still be canceled
	select {
	case <-ctx.Done():
		// Success
	default:
		t.Fatal("context should be canceled even if shutdown function errors")
	}
}

func TestSetup_ReloadConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var reloadCount atomic.Int32

	_, h := Setup(ctx, &Config{
		ReloadFn: func() error {
			reloadCount.Add(1)
			return nil
		},
	})
	defer h.Stop()

	// Send SIGHUP to trigger reload
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	require.NoError(t, err)

	// Wait for reload to be processed
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(1), reloadCount.Load(), "reload function should have been called once")

	// Send another SIGHUP
	err = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(2), reloadCount.Load(), "reload function should have been called twice")
}

func TestSetup_ReloadFnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var reloadCalled atomic.Bool

	_, h := Setup(ctx, &Config{
		ReloadFn: func() error {
			reloadCalled.Store(true)
			return errors.New("reload error")
		},
	})
	defer h.Stop()

	// Send SIGHUP - should not panic on error
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.True(t, reloadCalled.Load(), "reload function should have been called")
}

func TestSetup_NoReloadFn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// No reload function configured
	_, h := Setup(ctx, &Config{})
	defer h.Stop()

	// Send SIGHUP - should be ignored without panic
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	require.NoError(t, err)

	// Just ensure it doesn't panic or hang
	time.Sleep(100 * time.Millisecond)
}

func TestHandler_Stop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx, h := Setup(ctx, &Config{})

	// Stop should cancel the context
	h.Stop()

	select {
	case <-ctx.Done():
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("context was not canceled after Stop")
	}
}

func TestSetup_ExternalContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	_, h := Setup(ctx, &Config{})

	// Cancel the parent context
	cancel()

	// Handler should finish
	select {
	case <-h.shutdownDone:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish after context cancellation")
	}
}

func TestShutdownTimeout(t *testing.T) {
	t.Parallel()

	// Verify the constant is set correctly
	assert.Equal(t, 5*time.Second, ShutdownTimeout)
}
