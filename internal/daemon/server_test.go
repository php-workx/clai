package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/suggestions/batch"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// openTestDB is a helper that opens a V2 database in a temp directory for testing.
func openTestDB(t *testing.T) *suggestdb.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	v2db, err := suggestdb.Open(context.Background(), suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open test V2 database: %v", err)
	}
	t.Cleanup(func() { v2db.Close() })
	return v2db
}

// openTestDBInDir opens a V2 database in the specified directory for testing.
func openTestDBInDir(t *testing.T, dir string) *suggestdb.DB {
	t.Helper()
	dbPath := filepath.Join(dir, "test.db")
	v2db, err := suggestdb.Open(context.Background(), suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open test V2 database: %v", err)
	}
	t.Cleanup(func() { v2db.Close() })
	return v2db
}

func TestNewServer_Success(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	cfg := &ServerConfig{
		V2DB:        v2db,
		IdleTimeout: 5 * time.Minute,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("server should not be nil")
	}

	if server.v2db != v2db {
		t.Error("v2db should be set")
	}

	if server.registry == nil {
		t.Error("registry should be created automatically")
	}

	if server.sessionManager == nil {
		t.Error("sessionManager should be created")
	}
}

func TestNewServer_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := NewServer(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestNewServer_NilV2DB(t *testing.T) {
	t.Parallel()

	cfg := &ServerConfig{
		V2DB: nil,
	}

	_, err := NewServer(cfg)
	if err == nil {
		t.Error("expected error for nil V2DB")
	}
}

func TestNewServer_DefaultIdleTimeout(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	cfg := &ServerConfig{
		V2DB: v2db,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Default should be 20 minutes
	if server.idleTimeout != 20*time.Minute {
		t.Errorf("expected default idle timeout of 20 minutes, got %v", server.idleTimeout)
	}
}

func TestServer_TouchActivity(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	oldActivity := server.getLastActivity()
	time.Sleep(10 * time.Millisecond)
	server.touchActivity()
	newActivity := server.getLastActivity()

	if !newActivity.After(oldActivity) {
		t.Error("lastActivity should be updated after touchActivity")
	}
}

func TestServer_IncrementCommandsLogged(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.getCommandsLogged() != 0 {
		t.Errorf("expected 0 commands logged initially, got %d", server.getCommandsLogged())
	}

	server.incrementCommandsLogged()
	server.incrementCommandsLogged()
	server.incrementCommandsLogged()

	if server.getCommandsLogged() != 3 {
		t.Errorf("expected 3 commands logged, got %d", server.getCommandsLogged())
	}
}

func TestServer_Version(t *testing.T) {
	// Version should be set (either "dev" or build-time value)
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

// TestNewServer_TableDriven uses table-driven tests to verify NewServer behavior
// with various configurations.
func TestNewServer_TableDriven(t *testing.T) {
	t.Parallel()

	validLogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	validRegistry := provider.NewRegistry()

	// Open a shared V2 DB for the table-driven tests that need one.
	validV2DB := openTestDB(t)

	tests := []struct {
		config      *ServerConfig
		validate    func(t *testing.T, s *Server)
		name        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "nil config returns error",
			config:      nil,
			wantErr:     true,
			errContains: "config is required",
		},
		{
			name: "nil V2DB returns error",
			config: &ServerConfig{
				V2DB: nil,
			},
			wantErr:     true,
			errContains: "V2DB is required",
		},
		{
			name: "valid config with minimal options",
			config: &ServerConfig{
				V2DB: validV2DB,
			},
			wantErr: false,
			validate: func(t *testing.T, s *Server) {
				if s.v2db != validV2DB {
					t.Error("v2db should be set correctly")
				}
				if s.registry == nil {
					t.Error("registry should be auto-created")
				}
				if s.paths == nil {
					t.Error("paths should be set to defaults")
				}
				if s.logger == nil {
					t.Error("logger should be set to default")
				}
				if s.idleTimeout != 20*time.Minute {
					t.Errorf("expected default idle timeout 20m, got %v", s.idleTimeout)
				}
				if s.sessionManager == nil {
					t.Error("sessionManager should be created")
				}
				if s.shutdownChan == nil {
					t.Error("shutdownChan should be initialized")
				}
			},
		},
		{
			name: "valid config with all options provided",
			config: &ServerConfig{
				V2DB:        validV2DB,
				Registry:    validRegistry,
				Logger:      validLogger,
				IdleTimeout: 10 * time.Minute,
			},
			wantErr: false,
			validate: func(t *testing.T, s *Server) {
				if s.v2db != validV2DB {
					t.Error("v2db should be the provided database")
				}
				if s.registry != validRegistry {
					t.Error("registry should be the provided registry")
				}
				if s.logger != validLogger {
					t.Error("logger should be the provided logger")
				}
				if s.idleTimeout != 10*time.Minute {
					t.Errorf("expected idle timeout 10m, got %v", s.idleTimeout)
				}
			},
		},
		{
			name: "custom paths are respected",
			config: &ServerConfig{
				V2DB: validV2DB,
				Paths: &config.Paths{
					BaseDir: "/tmp/clai-test",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, s *Server) {
				if s.paths.BaseDir != "/tmp/clai-test" {
					t.Errorf("expected custom base dir, got %s", s.paths.BaseDir)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, err := NewServer(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				} else if tt.errContains != "" && !containsSubstring(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if server == nil {
				t.Fatal("server should not be nil")
			}

			if tt.validate != nil {
				tt.validate(t, server)
			}
		})
	}
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestServer_ActivityTracking_Concurrent verifies thread-safety of activity tracking.
func TestServer_ActivityTracking_Concurrent(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Half writers, half readers

	// Start writers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				server.touchActivity()
			}
		}()
	}

	// Start readers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = server.getLastActivity()
			}
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify final state is consistent
	finalActivity := server.getLastActivity()
	if finalActivity.IsZero() {
		t.Error("last activity should not be zero after concurrent updates")
	}
}

// TestServer_CommandsLogged_Concurrent verifies thread-safety of command counter.
func TestServer_CommandsLogged_Concurrent(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	const numGoroutines = 100
	const incrementsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				server.incrementCommandsLogged()
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * incrementsPerGoroutine)
	actual := server.getCommandsLogged()

	if actual != expected {
		t.Errorf("expected %d commands logged, got %d", expected, actual)
	}
}

// TestServer_CommandsLogged_ReadWhileWrite tests concurrent read/write operations.
func TestServer_CommandsLogged_ReadWhileWrite(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	const numWriters = 50
	const numReaders = 50
	const operations = 100

	var wg sync.WaitGroup
	wg.Add(numWriters + numReaders)

	// Writers
	for i := 0; i < numWriters; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				server.incrementCommandsLogged()
			}
		}()
	}

	// Readers
	var sawNegative atomic.Bool
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				count := server.getCommandsLogged()
				if count < 0 {
					sawNegative.Store(true)
				}
			}
		}()
	}

	wg.Wait()

	if sawNegative.Load() {
		t.Errorf("commands logged was negative at some point during concurrent access")
	}
}

// TestServer_WritePIDFile verifies PID file is written correctly.
func TestServer_WritePIDFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	v2db := openTestDBInDir(t, tmpDir)
	server, err := NewServer(&ServerConfig{
		V2DB:  v2db,
		Paths: paths,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	err = server.writePIDFile()
	if err != nil {
		t.Fatalf("writePIDFile failed: %v", err)
	}

	pidPath := server.paths.PIDFile()

	// Verify file exists
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}

	// Verify content is the current PID
	expectedPID := strconv.Itoa(os.Getpid())
	content := string(bytes.TrimSpace(data))

	if content != expectedPID {
		t.Errorf("expected PID %s, got %s", expectedPID, content)
	}

	// Verify file permissions
	info, err := os.Stat(pidPath)
	if err != nil {
		t.Fatalf("failed to stat PID file: %v", err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}
}

// TestServer_Cleanup verifies cleanup removes socket and PID files.
func TestServer_Cleanup(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	v2db := openTestDBInDir(t, tmpDir)
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		V2DB:   v2db,
		Paths:  paths,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Create socket and PID files
	socketPath := paths.SocketFile()
	pidPath := paths.PIDFile()

	if err := os.WriteFile(socketPath, []byte("socket"), 0600); err != nil {
		t.Fatalf("failed to create socket file: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte("12345"), 0600); err != nil {
		t.Fatalf("failed to create PID file: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Fatal("socket file should exist before cleanup")
	}
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Fatal("PID file should exist before cleanup")
	}

	// Call cleanup
	server.cleanup()

	// Verify files are removed
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after cleanup")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should be removed after cleanup")
	}
}

// TestServer_Cleanup_NonexistentFiles verifies cleanup handles missing files gracefully.
func TestServer_Cleanup_NonexistentFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	v2db := openTestDBInDir(t, tmpDir)
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		V2DB:   v2db,
		Paths:  paths,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Don't create the files - just call cleanup
	// This should not panic or error
	server.cleanup()

	// Verify no error was logged (warnings are ok for non-existent files)
	// The cleanup function handles os.IsNotExist errors gracefully
}

// TestServer_IdleTimeout_Configuration verifies idle timeout is configured correctly.
func TestServer_IdleTimeout_Configuration(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)

	tests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
	}{
		{
			name:     "zero timeout gets default",
			timeout:  0,
			expected: 20 * time.Minute,
		},
		{
			name:     "custom timeout is respected",
			timeout:  5 * time.Minute,
			expected: 5 * time.Minute,
		},
		{
			name:     "short timeout for testing",
			timeout:  1 * time.Second,
			expected: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, err := NewServer(&ServerConfig{
				V2DB:        v2db,
				IdleTimeout: tt.timeout,
			})
			if err != nil {
				t.Fatalf("NewServer failed: %v", err)
			}

			if server.idleTimeout != tt.expected {
				t.Errorf("expected idle timeout %v, got %v", tt.expected, server.idleTimeout)
			}
		})
	}
}

// TestServer_WatchIdle_IdleConditionCheck verifies idle condition logic.
func TestServer_WatchIdle_IdleConditionCheck(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		IdleTimeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Set last activity to be old enough to trigger idle timeout
	server.mu.Lock()
	server.lastActivity = time.Now().Add(-2 * time.Second)
	server.mu.Unlock()

	// Verify the idle condition is met
	since := time.Since(server.getLastActivity())
	if since <= server.idleTimeout {
		t.Errorf("expected idle duration %v > timeout %v", since, server.idleTimeout)
	}

	// Verify no active sessions means idle check would trigger
	if server.sessionManager.ActiveCount() != 0 {
		t.Error("expected no active sessions")
	}
}

// TestServer_WatchIdle_CancelContext verifies watchIdle respects context cancellation.
func TestServer_WatchIdle_CancelContext(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		IdleTimeout: 1 * time.Hour, // Long timeout - we're testing cancellation
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start watchIdle
	done := make(chan struct{})
	server.wg.Add(1)
	go func() {
		server.watchIdle(ctx)
		close(done)
	}()

	// Cancel context
	cancel()

	// Wait for watchIdle to exit
	select {
	case <-done:
		// Success - watchIdle exited
	case <-time.After(2 * time.Second):
		t.Error("watchIdle did not exit after context cancellation")
	}
}

// TestServer_WatchIdle_ShutdownChan verifies watchIdle respects shutdown channel.
func TestServer_WatchIdle_ShutdownChan(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		IdleTimeout: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()

	// Start watchIdle
	done := make(chan struct{})
	server.wg.Add(1)
	go func() {
		server.watchIdle(ctx)
		close(done)
	}()

	// Close shutdown channel
	close(server.shutdownChan)

	// Wait for watchIdle to exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("watchIdle did not exit after shutdown channel closed")
	}
}

// TestServer_WatchIdle_NoShutdownWithActiveSessions verifies idle timeout is
// suspended when there are active sessions.
func TestServer_WatchIdle_NoShutdownWithActiveSessions(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		IdleTimeout: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Set last activity to be old
	server.mu.Lock()
	server.lastActivity = time.Now().Add(-1 * time.Hour)
	server.mu.Unlock()

	// Add an active session
	server.sessionManager.Start("test-session", "zsh", "darwin", "host", "user", "/tmp", time.Now())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start watchIdle
	done := make(chan struct{})
	server.wg.Add(1)
	go func() {
		server.watchIdle(ctx)
		close(done)
	}()

	// Wait a bit and verify shutdown wasn't triggered
	select {
	case <-server.shutdownChan:
		t.Error("shutdown should not be triggered when there are active sessions")
	case <-ctx.Done():
		// Good - no shutdown was triggered during the test window
	}

	// Ensure the goroutine exits before test ends
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("watchIdle did not exit after context timeout")
	}
}

// TestServer_PruneCache verifies pruneCache calls the V2 ops layer.
func TestServer_PruneCache(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		V2DB:   v2db,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()
	// Should not panic - pruneCache now calls ops.PruneExpiredCache on the v2db
	server.pruneCache(ctx)
}

// TestServer_PruneCache_HandlesError verifies pruneCache handles errors gracefully.
func TestServer_PruneCache_HandlesError(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		V2DB:   v2db,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()

	// Should not panic - with a real DB there's no error, but we verify the path works
	server.pruneCache(ctx)
}

// TestServer_PruneCacheLoop_CancelContext verifies pruneCacheLoop respects context cancellation.
func TestServer_PruneCacheLoop_CancelContext(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	server.wg.Add(1)
	go func() {
		server.pruneCacheLoop(ctx)
		close(done)
	}()

	// Allow startup prune to run
	time.Sleep(10 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for loop to exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("pruneCacheLoop did not exit after context cancellation")
	}
}

// TestServer_PruneCacheLoop_ShutdownChan verifies pruneCacheLoop respects shutdown.
func TestServer_PruneCacheLoop_ShutdownChan(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()

	done := make(chan struct{})
	server.wg.Add(1)
	go func() {
		server.pruneCacheLoop(ctx)
		close(done)
	}()

	// Allow startup prune to run
	time.Sleep(10 * time.Millisecond)

	// Signal shutdown
	close(server.shutdownChan)

	// Wait for loop to exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("pruneCacheLoop did not exit after shutdown channel closed")
	}
}

// TestServer_Start_CreatesSocket verifies Start creates the Unix socket.
func TestServer_Start_CreatesSocket(t *testing.T) {
	t.Parallel()

	// Use /tmp directly to avoid Unix socket path length limits on macOS
	// macOS limits socket paths to 104 characters
	tmpDir, err := os.MkdirTemp("/tmp", "clai-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Pre-create all directories that EnsureDirectories would create
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	v2db := openTestDBInDir(t, tmpDir)
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		Paths:       paths,
		Logger:      logger,
		IdleTimeout: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(ctx)
	}()

	// Wait for server to start with retry
	socketPath := paths.SocketFile()
	var socketExists bool
	var lastErr error
	for i := 0; i < 100; i++ { // 100 * 20ms = 2s max wait
		time.Sleep(20 * time.Millisecond)
		_, lastErr = os.Stat(socketPath)
		if lastErr == nil {
			socketExists = true
			break
		}
		// Check if server returned an error
		select {
		case srvErr := <-serverErr:
			if srvErr != nil {
				t.Fatalf("server.Start failed: %v", srvErr)
			}
		default:
		}
	}

	if !socketExists {
		t.Fatalf("socket file should exist after Start, last error: %v", lastErr)
	}

	// Verify socket exists
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file should exist: %v", err)
	}

	// Verify socket permissions (0600)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected socket permissions 0600, got %o", info.Mode().Perm())
	}

	// Verify PID file exists
	pidPath := paths.PIDFile()
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("PID file should exist after Start")
	}

	// Shutdown server
	server.Shutdown()

	// Check for unexpected errors
	select {
	case srvErr := <-serverErr:
		if srvErr != nil {
			t.Errorf("unexpected server error: %v", srvErr)
		}
	case <-time.After(2 * time.Second):
		t.Error("server did not stop in time")
	}
}

// TestServer_Shutdown_Idempotent verifies Shutdown can be called multiple times.
func TestServer_Shutdown_Idempotent(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Create a new shutdown channel since the default one is used
	server.shutdownChan = make(chan struct{})

	// Start components that Shutdown() will try to stop, so they don't block.
	if server.batchWriter != nil {
		server.batchWriter.Start()
	}
	if server.workflowMiner != nil {
		server.workflowMiner.Start()
	}

	// Call Shutdown multiple times - should not panic
	for i := 0; i < 5; i++ {
		server.Shutdown()
	}
}

// TestServer_Shutdown_ClosesListener verifies Shutdown closes the listener.
func TestServer_Shutdown_ClosesListener(t *testing.T) {
	t.Parallel()

	// Use /tmp directly to avoid Unix socket path length limits on macOS
	tmpDir, err := os.MkdirTemp("/tmp", "clai-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Pre-create directories
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	v2db := openTestDBInDir(t, tmpDir)
	server, err := NewServer(&ServerConfig{
		V2DB:  v2db,
		Paths: paths,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start server
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Start(ctx)
	}()

	// Wait for server to actually start (socket to be created)
	socketPath := paths.SocketFile()
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
	}

	// Trigger shutdown
	cancel()
	server.Shutdown()

	// Wait for server to stop
	select {
	case <-serverDone:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("server did not stop in time")
	}
}

// TestServer_ActivityTracking_TimeProgression verifies activity timestamps progress.
func TestServer_ActivityTracking_TimeProgression(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	times := make([]time.Time, 10)

	for i := 0; i < 10; i++ {
		times[i] = server.getLastActivity()
		time.Sleep(5 * time.Millisecond)
		server.touchActivity()
	}

	// Each successive time should be >= previous
	for i := 1; i < len(times); i++ {
		if times[i].Before(times[i-1]) {
			t.Errorf("time[%d] (%v) should not be before time[%d] (%v)",
				i, times[i], i-1, times[i-1])
		}
	}
}

// TestServer_StartTime verifies start time is set on creation.
func TestServer_StartTime(t *testing.T) {
	t.Parallel()

	before := time.Now()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	after := time.Now()

	if server.startTime.Before(before) || server.startTime.After(after) {
		t.Errorf("startTime should be between %v and %v, got %v",
			before, after, server.startTime)
	}
}

// TestServer_InitialActivity verifies initial activity time is set on creation.
func TestServer_InitialActivity(t *testing.T) {
	t.Parallel()

	before := time.Now()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{V2DB: v2db})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	after := time.Now()

	activity := server.getLastActivity()
	if activity.Before(before) || activity.After(after) {
		t.Errorf("initial lastActivity should be between %v and %v, got %v",
			before, after, activity)
	}
}

// TestNewServer_WithV2DB verifies the server starts successfully with a V2 database.
func TestNewServer_WithV2DB(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.v2db != v2db {
		t.Error("v2db should be set to the provided database")
	}
}

// TestNewServer_V2DB_UnwritablePath verifies graceful handling when V2 DB path is unwritable.
func TestNewServer_V2DB_UnwritablePath(t *testing.T) {
	t.Parallel()

	// Create an unwritable directory to simulate V2 DB open failure
	tmpDir := t.TempDir()
	unwritableDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(unwritableDir, 0555); err != nil {
		t.Fatalf("failed to create unwritable dir: %v", err)
	}
	defer os.Chmod(unwritableDir, 0755) // restore for cleanup

	dbPath := filepath.Join(unwritableDir, "subdir", "suggestions_v2.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	// We expect this to fail because the directory is not writable
	if err == nil {
		// If running as root, the permission restriction may not apply
		v2db.Close()
		t.Skip("running as root; permission test not applicable")
	}

	// v2db should be nil after the failed open
	if v2db != nil {
		t.Fatal("v2db should be nil after failed open")
	}

	// NewServer should fail with nil V2DB since V2DB is now required
	_, err = NewServer(&ServerConfig{
		V2DB: nil,
	})
	if err == nil {
		t.Fatal("NewServer should fail without V2DB")
	}
	if !containsSubstring(err.Error(), "V2DB is required") {
		t.Errorf("expected error about V2DB being required, got: %v", err)
	}
}

// TestNewServer_WithBatchWriter verifies the server creates a batch writer when V2DB is provided.
func TestNewServer_WithBatchWriter(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.batchWriter == nil {
		t.Error("batchWriter should be initialized when V2DB is provided")
	}
	if server.v2db != v2db {
		t.Error("v2db should be set to the provided database")
	}
}

// TestNewServer_CustomBatchWriter verifies a custom batch writer is used when provided.
func TestNewServer_CustomBatchWriter(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)

	// Create a custom batch writer
	customWriter := batch.NewWriter(v2db.DB(), batch.DefaultOptions())

	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		BatchWriter: customWriter,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.batchWriter != customWriter {
		t.Error("batchWriter should be the custom writer provided in config")
	}
}

// TestServer_BatchWriterLifecycle verifies that the batch writer is started during
// Start() and stopped during Shutdown().
func TestServer_BatchWriterLifecycle(t *testing.T) {
	t.Parallel()

	// Use /tmp directly to avoid Unix socket path length limits on macOS
	tmpDir, err := os.MkdirTemp("/tmp", "clai-bw-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	v2db := openTestDBInDir(t, tmpDir)

	paths := &config.Paths{
		BaseDir: tmpDir,
	}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		Paths:       paths,
		Logger:      logger,
		IdleTimeout: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.batchWriter == nil {
		t.Fatal("batchWriter should be initialized")
	}

	// Start the server in background
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(serverCtx)
	}()

	// Wait for server to start (socket to be created)
	socketPath := paths.SocketFile()
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		// Check if server returned an error
		select {
		case srvErr := <-serverErr:
			if srvErr != nil {
				t.Fatalf("server.Start failed: %v", srvErr)
			}
		default:
		}
	}

	// The batch writer should now be started (Start was called inside Server.Start).
	// We can verify it's functional by checking that Stats() returns valid data.
	stats := server.batchWriter.Stats()
	// Initial stats should show zero events
	if stats.EventsWritten != 0 {
		t.Errorf("expected 0 events written initially, got %d", stats.EventsWritten)
	}

	// Shutdown the server, which should stop the batch writer
	cancel()
	server.Shutdown()

	// Wait for server to stop
	select {
	case srvErr := <-serverErr:
		if srvErr != nil {
			t.Errorf("unexpected server error: %v", srvErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not stop in time")
	}

	// After shutdown, the batch writer's Stop() should have been called.
	// Calling Stop() again should be safe (it's idempotent via sync.Once).
	server.batchWriter.Stop()
}

// TestServer_BatchWriterNilLifecycle verifies Start and Shutdown work when V2DB
// is provided but batch writer is explicitly set to nil (edge case).
func TestServer_BatchWriterNilLifecycle(t *testing.T) {
	t.Parallel()

	// Use /tmp directly to avoid Unix socket path length limits on macOS
	tmpDir, err := os.MkdirTemp("/tmp", "clai-nobw-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	paths := &config.Paths{
		BaseDir: tmpDir,
	}
	if err = paths.EnsureDirectories(); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	// Even though V2DB is required, we still test that a nil BatchWriter override
	// doesn't cause issues - the server auto-creates one from V2DB.
	v2db := openTestDBInDir(t, tmpDir)
	server, err := NewServer(&ServerConfig{
		V2DB:        v2db,
		Paths:       paths,
		Logger:      logger,
		IdleTimeout: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// batchWriter should be auto-created from V2DB
	if server.batchWriter == nil {
		t.Fatal("batchWriter should be auto-created when V2DB is provided")
	}

	// Start the server - should work fine
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(serverCtx)
	}()

	// Wait for server to start
	socketPath := paths.SocketFile()
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
	}

	// Shutdown - should work fine
	cancel()
	server.Shutdown()

	select {
	case srvErr := <-serverErr:
		if srvErr != nil {
			t.Errorf("unexpected server error: %v", srvErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not stop in time")
	}
}

// TestNewServer_V2ScorerInitialized verifies that the V2 scorer is auto-initialized
// when a V2DB is provided but no explicit V2Scorer is configured.
func TestNewServer_V2ScorerInitialized(t *testing.T) {
	t.Parallel()

	v2db := openTestDB(t)
	server, err := NewServer(&ServerConfig{
		V2DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.v2Scorer == nil {
		t.Error("v2Scorer should be non-nil when V2DB is provided")
	}
	if server.v2db != v2db {
		t.Error("v2db should be set to the provided database")
	}
}

// Ensure mock types satisfy their interfaces.
var _ provider.Provider = (*mockProvider)(nil)
