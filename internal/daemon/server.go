// Package daemon implements the clai gRPC daemon server.
// It handles all AI operations, session tracking, and command logging.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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
	"github.com/runger/clai/internal/suggestions/alias"
	"github.com/runger/clai/internal/suggestions/api"
	"github.com/runger/clai/internal/suggestions/batch"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/ingest"
	"github.com/runger/clai/internal/suggestions/learning"
	"github.com/runger/clai/internal/suggestions/maintenance"
	"github.com/runger/clai/internal/suggestions/projecttype"
	search2 "github.com/runger/clai/internal/suggestions/search"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
	"github.com/runger/clai/internal/suggestions/workflow"
)

// Version is set at build time
var Version = "dev"

// LLMQuerier abstracts LLM queries for testability.
type LLMQuerier interface {
	Query(ctx context.Context, prompt string) (string, error)
}

type suggestSnapshot struct {
	Context     suggest2.SuggestContext
	Suggestions []suggest2.Suggestion
	ShownAtMs   int64
}

// Server is the main daemon server that handles all gRPC requests.
type Server struct {
	pb.UnimplementedClaiServiceServer

	// Dependencies
	store    storage.Store
	v2db     *suggestdb.DB // V2 suggestions database (optional, enables V2 features)
	ranker   suggest.Ranker
	registry *provider.Registry
	llm      LLMQuerier

	// Server state
	grpcServer     *grpc.Server
	listener       net.Listener
	diagHTTPServer *http.Server
	diagListener   net.Listener
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
	feedbackStore  *feedback.Store
	dismissalStore *dismissal.Store

	// Maintenance
	maintenanceRunner *maintenance.Runner
	diagnosticsMux    *http.ServeMux

	// V2 batch writer (nil if V2 disabled)
	batchWriter *batch.Writer

	// V2 scorer (nil if V2 disabled)
	v2Scorer *suggest2.Scorer

	// Scorer version: "v1" (default), "v2", or "blend"
	scorerVersion string

	// V2 runtime enrichers
	projectDetector      *projecttype.Detector
	aliasStore           *alias.Store
	workflowMiner        *workflow.Miner
	workflowMineInterval time.Duration
	learningStore        *learning.Store
	learner              *learning.Learner
	lastSuggestSnapshots map[string]suggestSnapshot
	snapshotMu           sync.RWMutex

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

	// LLM is the LLM querier for workflow analysis (optional).
	LLM LLMQuerier

	// Paths is the path configuration (optional, uses defaults if nil)
	Paths *config.Paths

	// Logger is the structured logger (optional, uses default if nil)
	Logger *slog.Logger

	// IdleTimeout is the duration after which the daemon exits if idle
	// Default: 20 minutes
	IdleTimeout time.Duration

	// FeedbackStore is the suggestion feedback store (optional)
	FeedbackStore *feedback.Store

	// DismissalStore tracks learned dismissal states (optional).
	DismissalStore *dismissal.Store

	// MaintenanceRunner is the background maintenance goroutine (optional).
	// If non-nil, the runner is started with the server and notified on each
	// ingested command event for activity tracking.
	MaintenanceRunner *maintenance.Runner

	// V2DB is the V2 suggestions database (optional, enables V2 features).
	// If nil, V2 features are disabled and the daemon operates with V1 only.
	V2DB *suggestdb.DB

	// BatchWriter is the V2 batch event writer (optional).
	// If nil and V2DB is non-nil, a default batch writer is created.
	BatchWriter *batch.Writer

	// V2Scorer is the V2 suggestion scorer (optional).
	// If nil, V2 scoring is not available until dependencies are initialized
	// (see the separate scorer dependency initialization).
	V2Scorer *suggest2.Scorer

	// ScorerVersion is ignored in V2-only mode and retained only for compatibility.
	ScorerVersion string

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
	diagMux := resolveDiagnosticsMux(v2scorer, cfg.V2DB, logger)
	projectDetector := projecttype.NewDetector(projecttype.DetectorOptions{})
	aliasStore := resolveAliasStore(cfg.V2DB)
	feedbackStore := resolveFeedbackStore(cfg.FeedbackStore, cfg.V2DB, logger)
	dismissalStore := resolveDismissalStore(cfg.DismissalStore, cfg.V2DB, logger)
	learningStore := resolveLearningStore(cfg.V2DB)
	learner := resolveLearner(learningStore)
	workflowMiner, workflowInterval := resolveWorkflowMiner(cfg.V2DB)

	now := time.Now()
	return &Server{
		store:                cfg.Store,
		v2db:                 cfg.V2DB,
		ranker:               ranker,
		registry:             registry,
		llm:                  cfg.LLM,
		paths:                paths,
		logger:               logger,
		sessionManager:       NewSessionManager(),
		feedbackStore:        feedbackStore,
		dismissalStore:       dismissalStore,
		startTime:            now,
		lastActivity:         now,
		idleTimeout:          idleTimeout,
		shutdownChan:         make(chan struct{}),
		maintenanceRunner:    cfg.MaintenanceRunner,
		diagnosticsMux:       diagMux,
		batchWriter:          bw,
		v2Scorer:             v2scorer,
		scorerVersion:        scorerVersion,
		projectDetector:      projectDetector,
		aliasStore:           aliasStore,
		workflowMiner:        workflowMiner,
		workflowMineInterval: workflowInterval,
		learningStore:        learningStore,
		learner:              learner,
		lastSuggestSnapshots: make(map[string]suggestSnapshot),
		ingestionQueue:       ingestQueue,
		circuitBreaker:       cb,
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
			version = "blend"
		} else {
			version = "v1"
		}
	}
	if (version == "v2" || version == "blend") && v2scorer == nil {
		logger.Warn("scorer_version requires V2 scorer but V2 is unavailable; falling back to v1",
			"requested", version,
		)
		return "v1"
	}
	return version
}

func resolveDiagnosticsMux(v2scorer *suggest2.Scorer, v2db *suggestdb.DB, logger *slog.Logger) *http.ServeMux {
	if v2scorer == nil || v2db == nil {
		return nil
	}
	searchSvc, err := search2.NewService(v2db.DB(), search2.Config{
		Logger:         logger,
		EnableFallback: true,
	})
	if err != nil {
		logger.Debug("diagnostics search service unavailable", "error", err)
	}
	handler := api.NewHandler(api.HandlerDependencies{
		Scorer:    v2scorer,
		SearchSvc: searchSvc,
		Logger:    logger,
	})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func resolveAliasStore(v2db *suggestdb.DB) *alias.Store {
	if v2db == nil {
		return nil
	}
	return alias.NewStore(v2db.DB())
}

func resolveFeedbackStore(existing *feedback.Store, v2db *suggestdb.DB, logger *slog.Logger) *feedback.Store {
	if existing != nil {
		return existing
	}
	if v2db == nil {
		return nil
	}
	return feedback.NewStore(v2db.DB(), feedback.DefaultConfig(), logger)
}

func resolveDismissalStore(existing *dismissal.Store, v2db *suggestdb.DB, logger *slog.Logger) *dismissal.Store {
	if existing != nil {
		return existing
	}
	if v2db == nil {
		return nil
	}
	return dismissal.NewStore(v2db.DB(), dismissal.DefaultConfig(), logger)
}

func resolveLearningStore(v2db *suggestdb.DB) *learning.Store {
	if v2db == nil {
		return nil
	}
	return learning.NewStore(v2db.DB())
}

func resolveLearner(store *learning.Store) *learning.Learner {
	if store == nil {
		return nil
	}
	l := learning.NewLearner(learning.DefaultWeights(), learning.DefaultConfig(), store)
	_, _ = l.LoadFromStore(context.Background(), "global")
	return l
}

func resolveWorkflowMiner(v2db *suggestdb.DB) (*workflow.Miner, time.Duration) {
	if v2db == nil {
		return nil, 0
	}
	cfg := workflow.DefaultMinerConfig()
	return workflow.NewMiner(v2db.DB(), cfg), time.Duration(cfg.MineIntervalMs) * time.Millisecond
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

	if s.diagnosticsMux != nil {
		diagListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.logger.Warn("failed to start diagnostics http listener", "error", err)
		} else {
			s.diagListener = diagListener
			s.diagHTTPServer = &http.Server{
				Handler:           s.diagnosticsMux,
				ReadHeaderTimeout: 2 * time.Second,
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				if serveErr := s.diagHTTPServer.Serve(diagListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					s.logger.Warn("diagnostics http server failed", "error", serveErr)
				}
			}()
			s.logger.Info("diagnostics api listening", "addr", diagListener.Addr().String())
		}
	}

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

	// Start workflow miner and pattern refresher.
	if s.workflowMiner != nil {
		s.workflowMiner.Start()
		s.wg.Add(1)
		go s.refreshWorkflowPatternsLoop(ctx)
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
		if s.workflowMiner != nil {
			s.workflowMiner.Stop()
		}
		if s.diagHTTPServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.diagHTTPServer.Shutdown(ctx)
			cancel()
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
		if s.diagListener != nil {
			s.diagListener.Close()
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

func (s *Server) refreshWorkflowPatternsLoop(ctx context.Context) {
	defer s.wg.Done()
	interval := s.workflowMineInterval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	load := func() {
		if s.v2db == nil || s.v2Scorer == nil {
			return
		}
		patterns, err := workflow.LoadPromotedPatterns(ctx, s.v2db.DB(), workflow.DefaultMinerConfig().MinOccurrences)
		if err != nil {
			s.logger.Warn("failed to refresh workflow patterns", "error", err)
			return
		}
		s.v2Scorer.SetWorkflowPatterns(patterns)
	}

	load()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			load()
		}
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
