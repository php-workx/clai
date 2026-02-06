// Package daemon implements the clai gRPC daemon server.
// It handles all AI operations, session tracking, and command logging.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/storage"
	"github.com/runger/clai/internal/suggest"
	"github.com/runger/clai/internal/suggestions/feedback"
)

// Version is set at build time
var Version = "dev"

// Server is the main daemon server that handles all gRPC requests.
type Server struct {
	pb.UnimplementedClaiServiceServer

	// Dependencies
	store    storage.Store
	ranker   suggest.Ranker
	registry *provider.Registry

	// Server state
	grpcServer     *grpc.Server
	listener       net.Listener
	paths          *config.Paths
	logger         *slog.Logger
	sessionManager *SessionManager

	// Lifecycle
	startTime    time.Time
	lastActivity time.Time
	idleTimeout  time.Duration
	shutdownChan chan struct{}
	shutdownOnce sync.Once
	wg           sync.WaitGroup

	// Feedback
	feedbackStore *feedback.Store

	// Backpressure
	ingestionQueue *IngestionQueue
	circuitBreaker *CircuitBreaker

	// Metrics
	mu             sync.RWMutex
	commandsLogged int64
}

// ServerConfig contains configuration options for the daemon server.
type ServerConfig struct {
	// Store is the storage backend (required)
	Store storage.Store

	// Ranker is the suggestion ranker (optional, created if nil)
	Ranker suggest.Ranker

	// Registry is the provider registry (optional, created if nil)
	Registry *provider.Registry

	// Paths is the path configuration (optional, uses defaults if nil)
	Paths *config.Paths

	// Logger is the structured logger (optional, uses default if nil)
	Logger *slog.Logger

	// IdleTimeout is the duration after which the daemon exits if idle
	// Default: 20 minutes
	IdleTimeout time.Duration

	// FeedbackStore is the suggestion feedback store (optional)
	FeedbackStore *feedback.Store

	// ReloadFn is called on SIGHUP to reload configuration.
	// If nil, SIGHUP is ignored.
	ReloadFn ReloadFunc
}

// NewServer creates a new daemon server with the given configuration.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}

	// Set defaults
	paths := cfg.Paths
	if paths == nil {
		paths = config.DefaultPaths()
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	ranker := cfg.Ranker
	if ranker == nil {
		ranker = suggest.NewRanker(cfg.Store)
	}

	registry := cfg.Registry
	if registry == nil {
		registry = provider.NewRegistry()
	}

	idleTimeout := cfg.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = 20 * time.Minute
	}

	// Create ingestion queue with default capacity (8192)
	ingestQueue := NewIngestionQueue(0, logger)

	// Create circuit breaker with defaults
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		Logger: logger,
	})

	now := time.Now()
	return &Server{
		store:          cfg.Store,
		ranker:         ranker,
		registry:       registry,
		paths:          paths,
		logger:         logger,
		sessionManager: NewSessionManager(),
		feedbackStore:  cfg.FeedbackStore,
		startTime:      now,
		lastActivity:   now,
		idleTimeout:    idleTimeout,
		shutdownChan:   make(chan struct{}),
		ingestionQueue: ingestQueue,
		circuitBreaker: cb,
	}, nil
}

// Start starts the gRPC server and listens on the Unix socket.
func (s *Server) Start(ctx context.Context) error {
	// Ensure runtime directory exists
	if err := s.paths.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Clean up stale socket
	socketPath := s.paths.SocketFile()
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to remove stale socket", "path", socketPath, "error", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions (readable/writable by owner only)
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Create gRPC server
	s.grpcServer = grpc.NewServer()
	pb.RegisterClaiServiceServer(s.grpcServer, s)

	// Write PID file
	if err := s.writePIDFile(); err != nil {
		listener.Close()
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	s.logger.Info("daemon starting",
		"socket", socketPath,
		"pid", os.Getpid(),
		"version", Version,
	)

	// Start idle watcher
	s.wg.Add(1)
	go s.watchIdle(ctx)

	// Start cache pruning
	s.wg.Add(1)
	go s.pruneCacheLoop(ctx)

	// Serve gRPC requests in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errChan <- fmt.Errorf("gRPC server error: %w", err)
		} else {
			errChan <- nil
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		s.Shutdown()
		// Wait for server to finish
		<-errChan
		return nil
	case err := <-errChan:
		return err
	}
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.logger.Info("daemon shutting down")

		// Signal shutdown
		close(s.shutdownChan)

		// Stop gRPC server
		if s.grpcServer != nil {
			s.grpcServer.GracefulStop()
		}

		// Wait for goroutines
		s.wg.Wait()

		// Close listener
		if s.listener != nil {
			s.listener.Close()
		}

		// Cleanup PID file and socket
		s.cleanup()

		s.logger.Info("daemon stopped")
	})
}

// cleanup removes the socket and PID file.
func (s *Server) cleanup() {
	socketPath := s.paths.SocketFile()
	pidPath := s.paths.PIDFile()

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to remove socket", "path", socketPath, "error", err)
	}

	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to remove PID file", "path", pidPath, "error", err)
	}
}

// writePIDFile writes the current process ID to the PID file.
func (s *Server) writePIDFile() error {
	pidPath := s.paths.PIDFile()
	pid := os.Getpid()
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0600)
}

// touchActivity updates the last activity timestamp.
func (s *Server) touchActivity() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

// getLastActivity returns the last activity timestamp.
func (s *Server) getLastActivity() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastActivity
}

// incrementCommandsLogged safely increments the commands logged counter.
func (s *Server) incrementCommandsLogged() {
	s.mu.Lock()
	s.commandsLogged++
	s.mu.Unlock()
}

// getCommandsLogged returns the number of commands logged.
func (s *Server) getCommandsLogged() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.commandsLogged
}

// watchIdle monitors for idle timeout and initiates shutdown.
func (s *Server) watchIdle(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			if s.sessionManager.ActiveCount() == 0 {
				since := time.Since(s.getLastActivity())
				if since > s.idleTimeout {
					s.logger.Info("idle timeout reached",
						"idle_duration", since,
						"timeout", s.idleTimeout,
					)
					go s.Shutdown()
					return
				}
			}
		}
	}
}

// pruneCacheLoop periodically prunes expired cache entries.
func (s *Server) pruneCacheLoop(ctx context.Context) {
	defer s.wg.Done()

	// Prune on startup
	s.pruneCache(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			s.pruneCache(ctx)
		}
	}
}

// pruneCache removes expired cache entries.
func (s *Server) pruneCache(ctx context.Context) {
	pruned, err := s.store.PruneExpiredCache(ctx)
	if err != nil {
		s.logger.Warn("failed to prune cache", "error", err)
		return
	}
	if pruned > 0 {
		s.logger.Info("pruned expired cache entries", "count", pruned)
	}
}
