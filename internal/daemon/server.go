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
	"github.com/runger/clai/internal/suggestions/alias"
	"github.com/runger/clai/internal/suggestions/api"
	"github.com/runger/clai/internal/suggestions/batch"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/ingest"
	"github.com/runger/clai/internal/suggestions/learning"
	"github.com/runger/clai/internal/suggestions/maintenance"
	"github.com/runger/clai/internal/suggestions/ops"
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
	Suggestions []suggest2.Suggestion
	Context     suggest2.SuggestContext
	ShownAtMs   int64
}

type capturedEntry struct {
	createdAt time.Time
	size      int64
}

// Server is the main daemon server that handles all gRPC requests.
type Server struct {
	pb.UnimplementedClaiServiceServer
	lastActivity         time.Time
	startTime            time.Time
	diagListener         net.Listener
	llm                  LLMQuerier
	listener             net.Listener
	jsonListener         net.Listener
	maintenanceRunner    *maintenance.Runner
	searchSvc            *search2.Service
	logger               *slog.Logger
	sessionManager       *SessionManager
	grpcServer           *grpc.Server
	registry             *provider.Registry
	circuitBreaker       *CircuitBreaker
	shutdownChan         chan struct{}
	ingestionQueue       *IngestionQueue
	lastSuggestSnapshots map[string]suggestSnapshot
	weightsCache         map[string]cachedWeights
	feedbackStore        *feedback.Store
	dismissalStore       *dismissal.Store
	batchWriter          *batch.Writer
	diagnosticsMux       *http.ServeMux
	diagHTTPServer       *http.Server
	scorer               *suggest2.Scorer
	paths                *config.Paths
	learner              *learning.Learner
	projectDetector      *projecttype.Detector
	db                   *suggestdb.DB
	workflowMiner        *workflow.Miner
	learningStore        *learning.Store
	capturedSize         map[string]capturedEntry
	aliasStore           *alias.Store
	wg                   sync.WaitGroup
	idleTimeout          time.Duration
	commandsLogged       int64
	workflowMineInterval time.Duration
	weightsCacheMu       sync.RWMutex
	snapshotMu           sync.RWMutex
	mu                   sync.RWMutex
	shutdownOnce         sync.Once
	captureMu            sync.Mutex
}

// ServerConfig contains configuration options for the daemon server.
type ServerConfig struct {
	LLM               LLMQuerier
	MaintenanceRunner *maintenance.Runner
	Paths             *config.Paths
	Logger            *slog.Logger
	FeedbackStore     *feedback.Store
	DismissalStore    *dismissal.Store
	Registry          *provider.Registry
	DB                *suggestdb.DB
	BatchWriter       *batch.Writer
	Scorer            *suggest2.Scorer
	ReloadFn          ReloadFunc
	IdleTimeout       time.Duration
}

// NewServer creates a new daemon server with the given configuration.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.DB == nil {
		return nil, fmt.Errorf("DB is required")
	}

	paths := defaultPaths(cfg.Paths)
	logger := defaultLogger(cfg.Logger)
	registry := defaultRegistry(cfg.Registry)
	idleTimeout := defaultIdleTimeout(cfg.IdleTimeout)

	// Create ingestion queue with default capacity (8192)
	ingestQueue := NewIngestionQueue(0, logger)

	// Create circuit breaker with defaults
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		Logger: logger,
	})

	bw := resolveBatchWriter(cfg.BatchWriter, cfg.DB)
	scorer := resolveScorer(cfg.Scorer, cfg.DB, logger)
	searchSvc := resolveSearchSvc(cfg.DB, logger)
	diagMux := resolveDiagnosticsMux(scorer, searchSvc, logger)
	projectDetector := projecttype.NewDetector(projecttype.DetectorOptions{})
	aliasStore := resolveAliasStore(cfg.DB)
	feedbackStore := resolveFeedbackStore(cfg.FeedbackStore, cfg.DB, logger)
	dismissalStore := resolveDismissalStore(cfg.DismissalStore, cfg.DB, logger)
	learningStore := resolveLearningStore(cfg.DB)
	learner := resolveLearner(learningStore)
	workflowMiner, workflowInterval := resolveWorkflowMiner(cfg.DB)

	now := time.Now()
	return &Server{
		db:                   cfg.DB,
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
		capturedSize:         make(map[string]capturedEntry),
		maintenanceRunner:    cfg.MaintenanceRunner,
		diagnosticsMux:       diagMux,
		batchWriter:          bw,
		scorer:               scorer,
		searchSvc:            searchSvc,
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

func resolveBatchWriter(override *batch.Writer, db *suggestdb.DB) *batch.Writer {
	if override != nil {
		return override
	}
	if db == nil {
		return nil
	}
	opts := batch.DefaultOptions()
	opts.WritePathConfig = &ingest.WritePathConfig{}
	return batch.NewWriter(db.DB(), opts)
}

func resolveScorer(override *suggest2.Scorer, db *suggestdb.DB, logger *slog.Logger) *suggest2.Scorer {
	if override != nil {
		return override
	}
	if db == nil {
		return nil
	}
	return initScorer(db.DB(), logger)
}

func resolveDiagnosticsMux(scorer *suggest2.Scorer, searchSvc *search2.Service, logger *slog.Logger) *http.ServeMux {
	if scorer == nil {
		return nil
	}
	handler := api.NewHandler(api.HandlerDependencies{
		Scorer:    scorer,
		SearchSvc: searchSvc,
		Logger:    logger,
	})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func resolveSearchSvc(db *suggestdb.DB, logger *slog.Logger) *search2.Service {
	if db == nil {
		return nil
	}
	svc, err := search2.NewService(db.DB(), search2.Config{
		Logger:         logger,
		EnableFallback: true,
	})
	if err != nil {
		logger.Debug("search service init failed", "error", err)
		return nil
	}
	return svc
}

func resolveAliasStore(db *suggestdb.DB) *alias.Store {
	if db == nil {
		return nil
	}
	return alias.NewStore(db.DB())
}

func resolveFeedbackStore(existing *feedback.Store, db *suggestdb.DB, logger *slog.Logger) *feedback.Store {
	if existing != nil {
		return existing
	}
	if db == nil {
		return nil
	}
	return feedback.NewStore(db.DB(), feedback.DefaultConfig(), logger)
}

func resolveDismissalStore(existing *dismissal.Store, db *suggestdb.DB, logger *slog.Logger) *dismissal.Store {
	if existing != nil {
		return existing
	}
	if db == nil {
		return nil
	}
	return dismissal.NewStore(db.DB(), dismissal.DefaultConfig(), logger)
}

func resolveLearningStore(db *suggestdb.DB) *learning.Store {
	if db == nil {
		return nil
	}
	return learning.NewStore(db.DB())
}

func resolveLearner(store *learning.Store) *learning.Learner {
	if store == nil {
		return nil
	}
	w := learning.DefaultWeights()
	l := learning.NewLearner(&w, learning.DefaultConfig(), store)
	if _, err := l.LoadFromStore(context.Background(), "global"); err != nil {
		slog.Warn("failed to load learning weights from store, using defaults", "error", err)
	}
	return l
}

func resolveWorkflowMiner(db *suggestdb.DB) (*workflow.Miner, time.Duration) {
	if db == nil {
		return nil, 0
	}
	cfg := workflow.DefaultMinerConfig()
	return workflow.NewMiner(db.DB(), cfg), time.Duration(cfg.MineIntervalMs) * time.Millisecond
}

// Start starts the gRPC server and listens on the Unix socket.
//
//nolint:funlen // Startup orchestration is inherently sequential; splitting fragments the flow.
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
	if chmodErr := os.Chmod(socketPath, 0o600); chmodErr != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", chmodErr)
	}

	// Create gRPC server
	s.grpcServer = grpc.NewServer(grpc.ChainUnaryInterceptor(s.accessLogUnaryInterceptor()))
	pb.RegisterClaiServiceServer(s.grpcServer, s)

	// Create JSON-RPC listener for clai-wrap phase-2 protocol.
	jsonListener, err := s.startJSONRPCListener()
	if err != nil {
		listener.Close()
		return err
	}
	s.jsonListener = jsonListener

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
		jsonListener.Close()
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

	// Start batch writer (if configured)
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
	errChan := make(chan error, 2)
	go func() {
		if err := s.grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errChan <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()
	go s.serveJSONRPC(ctx, jsonListener, errChan)

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		s.Shutdown()
		return nil
	case <-s.shutdownChan:
		return nil
	case err := <-errChan:
		s.Shutdown()
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

		// Stop batch writer (flushes pending events)
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

		if s.searchSvc != nil {
			s.searchSvc.Close()
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
		if s.jsonListener != nil {
			s.jsonListener.Close()
		}

		// Cleanup PID file and socket
		s.cleanup()

		s.logger.Info("daemon stopped")
	})
}

// cleanup removes the socket and PID file.
func (s *Server) cleanup() {
	socketPath := s.paths.SocketFile()
	jsonSocketPath := s.paths.JSONRPCSocketFile()
	pidPath := s.paths.PIDFile()

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to remove socket", "path", socketPath, "error", err)
	}
	if err := os.Remove(jsonSocketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to remove json-rpc socket", "path", jsonSocketPath, "error", err)
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
		if s.db == nil || s.scorer == nil {
			return
		}
		patterns, err := workflow.LoadPromotedPatterns(ctx, s.db.DB(), workflow.DefaultMinerConfig().MinOccurrences)
		if err != nil {
			s.logger.Warn("failed to refresh workflow patterns", "error", err)
			return
		}
		s.scorer.SetWorkflowPatterns(patterns)
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

const maxCapturedEntryAge = 2 * time.Hour

// pruneCache removes expired cache entries.
func (s *Server) pruneCache(ctx context.Context) {
	pruned, err := ops.PruneExpiredCache(ctx, s.db)
	if err != nil {
		s.logger.Warn("failed to prune cache", "error", err)
	} else if pruned > 0 {
		s.logger.Info("pruned expired cache entries", "count", pruned)
	}

	outputPruned, err := ops.PruneExpiredCommandOutput(ctx, s.db)
	if err != nil {
		s.logger.Warn("failed to prune command output", "error", err)
	} else if outputPruned > 0 {
		s.logger.Info("pruned expired command output rows", "count", outputPruned)
	}

	// Prune stale suggest snapshots for crashed/orphaned sessions.
	nowMs := time.Now().UnixMilli()
	cutoffMs := nowMs - maxSuggestSnapshotAge.Milliseconds()
	s.snapshotMu.Lock()
	for sid := range s.lastSuggestSnapshots {
		if s.lastSuggestSnapshots[sid].ShownAtMs < cutoffMs {
			delete(s.lastSuggestSnapshots, sid)
		}
	}
	s.snapshotMu.Unlock()

	// Prune orphaned capturedSize entries (command.start without command.end).
	capturedCutoff := time.Now().Add(-maxCapturedEntryAge)
	s.captureMu.Lock()
	for cid, entry := range s.capturedSize {
		if entry.createdAt.Before(capturedCutoff) {
			delete(s.capturedSize, cid)
		}
	}
	s.captureMu.Unlock()
}

func (s *Server) setCapturedBytes(commandID string, value int64) {
	s.captureMu.Lock()
	defer s.captureMu.Unlock()
	s.capturedSize[commandID] = capturedEntry{size: value, createdAt: time.Now()}
}

func (s *Server) addCapturedBytes(commandID string, delta int64) {
	s.captureMu.Lock()
	defer s.captureMu.Unlock()
	entry := s.capturedSize[commandID]
	entry.size += delta
	if entry.createdAt.IsZero() {
		entry.createdAt = time.Now()
	}
	s.capturedSize[commandID] = entry
}

func (s *Server) popCapturedBytes(commandID string) int64 {
	s.captureMu.Lock()
	defer s.captureMu.Unlock()
	value := s.capturedSize[commandID].size
	delete(s.capturedSize, commandID)
	return value
}
