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
	"google.golang.org/grpc/status"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/storage"
	"github.com/runger/clai/internal/suggest"
	"github.com/runger/clai/internal/suggestions/batch"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/ingest"
	"github.com/runger/clai/internal/suggestions/maintenance"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
)

// Version is set at build time
var Version = "dev"

// LLMQuerier abstracts LLM queries for testability.
type LLMQuerier interface {
	Query(ctx context.Context, prompt string) (string, error)
}

// Server is the main daemon server that handles all gRPC requests.
type Server struct {
	pb.UnimplementedClaiServiceServer
	lastActivity      time.Time
	startTime         time.Time
	listener          net.Listener
	store             storage.Store
	ranker            suggest.Ranker
	llm               LLMQuerier
	grpcServer        *grpc.Server
	v2Scorer          *suggest2.Scorer
	logger            *slog.Logger
	sessionManager    *SessionManager
	registry          *provider.Registry
	v2db              *suggestdb.DB
	circuitBreaker    *CircuitBreaker
	shutdownChan      chan struct{}
	ingestionQueue    *IngestionQueue
	paths             *config.Paths
	feedbackStore     *feedback.Store
	maintenanceRunner *maintenance.Runner
	batchWriter       *batch.Writer
	scorerVersion     string
	wg                sync.WaitGroup
	idleTimeout       time.Duration
	commandsLogged    int64
	mu                sync.RWMutex
	shutdownOnce      sync.Once
}

// ServerConfig contains configuration options for the daemon server.
type ServerConfig struct {
	LLM               LLMQuerier
	Ranker            suggest.Ranker
	Store             storage.Store
	V2DB              *suggestdb.DB
	Paths             *config.Paths
	Logger            *slog.Logger
	FeedbackStore     *feedback.Store
	MaintenanceRunner *maintenance.Runner
	Registry          *provider.Registry
	BatchWriter       *batch.Writer
	V2Scorer          *suggest2.Scorer
	ReloadFn          ReloadFunc
	ScorerVersion     string
	IdleTimeout       time.Duration
}

// NewServer creates a new daemon server with the given configuration.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}

	paths := defaultPaths(cfg.Paths)
	logger := defaultLogger(cfg.Logger)
	ranker := defaultRanker(cfg.Ranker, cfg.Store)
	registry := defaultRegistry(cfg.Registry)
	idleTimeout := defaultIdleTimeout(cfg.IdleTimeout)

	// Create ingestion queue with default capacity (8192)
	ingestQueue := NewIngestionQueue(0, logger)

	// Create circuit breaker with defaults
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		Logger: logger,
	})

	bw := resolveBatchWriter(cfg.BatchWriter, cfg.V2DB)
	v2scorer := resolveV2Scorer(cfg.V2Scorer, cfg.V2DB, logger)
	scorerVersion := resolveScorerVersion(cfg.ScorerVersion, v2scorer, logger)

	now := time.Now()
	return &Server{
		store:             cfg.Store,
		v2db:              cfg.V2DB,
		ranker:            ranker,
		registry:          registry,
		llm:               cfg.LLM,
		paths:             paths,
		logger:            logger,
		sessionManager:    NewSessionManager(),
		feedbackStore:     cfg.FeedbackStore,
		startTime:         now,
		lastActivity:      now,
		idleTimeout:       idleTimeout,
		shutdownChan:      make(chan struct{}),
		maintenanceRunner: cfg.MaintenanceRunner,
		batchWriter:       bw,
		v2Scorer:          v2scorer,
		scorerVersion:     scorerVersion,
		ingestionQueue:    ingestQueue,
		circuitBreaker:    cb,
	}, nil
}

func defaultPaths(paths *config.Paths) *config.Paths {
	if paths == nil {
		return config.DefaultPaths()
	}
	return paths
}

func defaultLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func defaultRanker(ranker suggest.Ranker, store storage.Store) suggest.Ranker {
	if ranker == nil {
		return suggest.NewRanker(store)
	}
	return ranker
}

func defaultRegistry(registry *provider.Registry) *provider.Registry {
	if registry == nil {
		return provider.NewRegistry()
	}
	return registry
}

func defaultIdleTimeout(timeout time.Duration) time.Duration {
	if timeout == 0 {
		return 20 * time.Minute
	}
	return timeout
}

func resolveBatchWriter(override *batch.Writer, v2db *suggestdb.DB) *batch.Writer {
	if override != nil {
		return override
	}
	if v2db == nil {
		return nil
	}
	opts := batch.DefaultOptions()
	opts.WritePathConfig = &ingest.WritePathConfig{}
	return batch.NewWriter(v2db.DB(), opts)
}

func resolveV2Scorer(override *suggest2.Scorer, v2db *suggestdb.DB, logger *slog.Logger) *suggest2.Scorer {
	if override != nil {
		return override
	}
	if v2db == nil {
		return nil
	}
	return initV2Scorer(v2db.DB(), logger)
}

func resolveScorerVersion(requested string, v2scorer *suggest2.Scorer, logger *slog.Logger) string {
	version := requested
	if version == "" {
		if v2scorer != nil {
			version = "v2"
		} else {
			version = "v1"
		}
	}
	if version == "v2" && v2scorer == nil {
		logger.Warn("scorer_version requires V2 scorer but V2 is unavailable; falling back to v1",
			"requested", version,
		)
		return "v1"
	}
	return version
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
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Create gRPC server
	s.grpcServer = grpc.NewServer(grpc.ChainUnaryInterceptor(s.accessLogUnaryInterceptor()))
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

	// Start maintenance runner (if configured)
	if s.maintenanceRunner != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.maintenanceRunner.Run(ctx, s.shutdownChan)
		}()
	}

	// Start V2 batch writer (if configured)
	if s.batchWriter != nil {
		s.batchWriter.Start()
	}

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

func (s *Server) accessLogUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		// "Web server"-style access log line, but structured. Do not log request bodies
		// (buffers/commands) here.
		s.logger.Info("rpc",
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration_ms", time.Since(start).Milliseconds(),
		)

		return resp, err
	}
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.logger.Info("daemon shutting down")

		// Signal shutdown
		close(s.shutdownChan)

		// Stop V2 batch writer (flushes pending events)
		if s.batchWriter != nil {
			s.batchWriter.Stop()
		}

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
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o600)
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

// incrementCommandsLogged safely increments the commands logged counter
// and notifies the maintenance runner (if configured) about the new event.
func (s *Server) incrementCommandsLogged() {
	s.mu.Lock()
	s.commandsLogged++
	s.mu.Unlock()

	if s.maintenanceRunner != nil {
		s.maintenanceRunner.RecordEvent()
	}
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
