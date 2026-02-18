// Package signal provides signal handling for the suggestions daemon.
// It implements the signal handling specified in tech_suggestions_v3.md Section 13.
package signal

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ShutdownTimeout is the maximum time to wait for graceful shutdown.
const ShutdownTimeout = 5 * time.Second

// Handler manages signal handling for the suggestions daemon.
type Handler struct {
	logger       *slog.Logger
	sigCh        chan os.Signal
	cancel       context.CancelFunc
	reloadFn     func() error
	shutdownFn   func(context.Context) error
	shutdownDone chan struct{}
}

// Config configures the signal handler.
type Config struct {
	// Logger for logging signal events (optional, uses slog.Default if nil).
	Logger *slog.Logger

	// ReloadFn is called on SIGHUP to reload configuration.
	// If nil, SIGHUP is ignored.
	ReloadFn func() error

	// ShutdownFn is called during graceful shutdown to perform cleanup.
	// The context has ShutdownTimeout deadline.
	// If nil, only context cancellation is performed.
	ShutdownFn func(context.Context) error
}

// Setup creates a signal handler and returns a context that is canceled on shutdown signals.
// The returned cancel function should be called to clean up resources.
//
// Per spec Section 13:
//   - SIGTERM: Graceful shutdown
//   - SIGINT: Graceful shutdown (allows Ctrl+C during dev)
//   - SIGHUP: Reload config
//   - SIGPIPE: Ignore (prevents crash on client disconnect)
func Setup(ctx context.Context, cfg *Config) (context.Context, *Handler) {
	if cfg == nil {
		cfg = &Config{}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Create cancellable context for shutdown
	ctx, cancel := context.WithCancel(ctx)

	h := &Handler{
		logger:       logger,
		sigCh:        make(chan os.Signal, 1),
		cancel:       cancel,
		reloadFn:     cfg.ReloadFn,
		shutdownFn:   cfg.ShutdownFn,
		shutdownDone: make(chan struct{}),
	}

	// Ignore SIGPIPE to prevent crash when client disconnects mid-write
	signal.Ignore(syscall.SIGPIPE)

	// Register for shutdown and reload signals
	signal.Notify(h.sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	// Start signal handler goroutine
	go h.run(ctx)

	return ctx, h
}

// run processes incoming signals.
func (h *Handler) run(ctx context.Context) {
	defer close(h.shutdownDone)

	for {
		select {
		case <-ctx.Done():
			// Context was canceled externally
			h.logger.Debug("signal handler: context canceled")
			signal.Stop(h.sigCh)
			return

		case sig := <-h.sigCh:
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				h.logger.Info("shutdown signal received", "signal", sig)
				h.initiateShutdown()
				signal.Stop(h.sigCh)
				return

			case syscall.SIGHUP:
				h.logger.Info("reload signal received")
				h.handleReload()
			}
		}
	}
}

// initiateShutdown performs the graceful shutdown sequence.
// Per spec Section 13.3:
//  1. Stop accepting new connections (handled by caller)
//  2. Wait for in-flight requests (with timeout)
//  3. Flush pending SQLite batch
//  4. Close database connection
//  5. Remove socket file (Unix)
//  6. Exit 0
func (h *Handler) initiateShutdown() {
	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Call shutdown function if provided
	if h.shutdownFn != nil {
		h.logger.Debug("executing shutdown function")
		if err := h.shutdownFn(shutdownCtx); err != nil {
			h.logger.Warn("shutdown function error", "error", err)
		}
	}

	// Cancel the main context to stop all operations
	h.cancel()

	h.logger.Debug("shutdown complete")
}

// handleReload calls the reload function to refresh configuration.
func (h *Handler) handleReload() {
	if h.reloadFn == nil {
		h.logger.Debug("no reload function configured, ignoring SIGHUP")
		return
	}

	if err := h.reloadFn(); err != nil {
		h.logger.Error("failed to reload configuration", "error", err)
	} else {
		h.logger.Info("configuration reloaded")
	}
}

// Wait blocks until the signal handler has finished processing.
// This should be called after the main context is done to ensure
// clean shutdown.
func (h *Handler) Wait() {
	<-h.shutdownDone
}

// Stop stops the signal handler and releases resources.
// This should be called during cleanup if shutdown wasn't initiated
// via signal.
func (h *Handler) Stop() {
	signal.Stop(h.sigCh)
	h.cancel()
}
