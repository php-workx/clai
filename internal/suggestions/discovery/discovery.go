// Package discovery provides task discovery from project files.
// It implements the discovery specified in tech_suggestions_v3.md Section 10.1.
package discovery

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskKind identifies the type of task source.
type TaskKind string

// Built-in task kinds per spec Section 10.1.
const (
	KindPackageJSON TaskKind = "package.json"
	KindMakefile    TaskKind = "makefile"
)

// Default discovery configuration.
const (
	// DefaultTimeout is the maximum time allowed for discovery operations.
	DefaultTimeout = 500 * time.Millisecond

	// DefaultMaxOutputBytes is the maximum output size from discovery commands.
	DefaultMaxOutputBytes = 1 << 20 // 1MB

	// DefaultTTL is how long before re-discovery is triggered.
	DefaultTTL = 5 * time.Minute
)

// Task represents a discovered project task.
type Task struct {
	RepoKey     string   // Repository identifier (git root path)
	Kind        TaskKind // Source type (package.json, makefile, etc.)
	Name        string   // Task name (e.g., "test", "build")
	Command     string   // Full command to execute
	Description string   // Optional description
}

// Options configures the discovery service.
type Options struct {
	// Timeout for discovery operations.
	Timeout time.Duration

	// MaxOutputBytes limits the output from discovery commands.
	MaxOutputBytes int64

	// TTL is how long before re-discovery is triggered for a repo.
	TTL time.Duration

	// Logger for discovery operations.
	Logger *slog.Logger
}

// DefaultOptions returns the default discovery options.
func DefaultOptions() Options {
	return Options{
		Timeout:        DefaultTimeout,
		MaxOutputBytes: DefaultMaxOutputBytes,
		TTL:            DefaultTTL,
		Logger:         slog.Default(),
	}
}

// Service manages task discovery for repositories.
type Service struct {
	db   *sql.DB
	opts Options

	// Prepared statements
	insertStmt *sql.Stmt
	selectStmt *sql.Stmt
	deleteStmt *sql.Stmt
	lastTsStmt *sql.Stmt

	// Discovery cache to avoid excessive re-scanning
	cacheMu sync.RWMutex
	cache   map[string]time.Time // repo_key -> last_discovery_time
}

// NewService creates a new discovery service.
func NewService(db *sql.DB, opts Options) (*Service, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	s := &Service{
		db:    db,
		opts:  opts,
		cache: make(map[string]time.Time),
	}

	if err := s.prepareStatements(); err != nil {
		return nil, err
	}

	return s, nil
}

// prepareStatements creates prepared SQL statements.
func (s *Service) prepareStatements() error {
	var err error

	s.insertStmt, err = s.db.Prepare(`
		INSERT OR REPLACE INTO project_task (repo_key, kind, name, command, description, discovered_ts)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}

	s.selectStmt, err = s.db.Prepare(`
		SELECT kind, name, command, description FROM project_task
		WHERE repo_key = ?
		ORDER BY kind, name
	`)
	if err != nil {
		s.insertStmt.Close()
		return err
	}

	s.deleteStmt, err = s.db.Prepare(`
		DELETE FROM project_task WHERE repo_key = ? AND kind = ?
	`)
	if err != nil {
		s.insertStmt.Close()
		s.selectStmt.Close()
		return err
	}

	s.lastTsStmt, err = s.db.Prepare(`
		SELECT MAX(discovered_ts) FROM project_task WHERE repo_key = ?
	`)
	if err != nil {
		s.insertStmt.Close()
		s.selectStmt.Close()
		s.deleteStmt.Close()
		return err
	}

	return nil
}

// Close releases resources held by the service.
func (s *Service) Close() error {
	if s.insertStmt != nil {
		s.insertStmt.Close()
	}
	if s.selectStmt != nil {
		s.selectStmt.Close()
	}
	if s.deleteStmt != nil {
		s.deleteStmt.Close()
	}
	if s.lastTsStmt != nil {
		s.lastTsStmt.Close()
	}
	return nil
}

// Discover runs all built-in discovery for a repository if TTL has expired.
// It returns true if discovery was performed, false if cached.
func (s *Service) Discover(ctx context.Context, repoRoot string) (bool, error) {
	if repoRoot == "" {
		return false, nil
	}

	// Check cache first
	s.cacheMu.RLock()
	lastDiscovery, ok := s.cache[repoRoot]
	s.cacheMu.RUnlock()

	if ok && time.Since(lastDiscovery) < s.opts.TTL {
		return false, nil // Cache hit
	}

	// Check database for last discovery timestamp
	var lastTs sql.NullInt64
	if err := s.lastTsStmt.QueryRow(repoRoot).Scan(&lastTs); err != nil {
		return false, err
	}

	if lastTs.Valid {
		lastDiscoveryDB := time.UnixMilli(lastTs.Int64)
		if time.Since(lastDiscoveryDB) < s.opts.TTL {
			// Update cache
			s.cacheMu.Lock()
			s.cache[repoRoot] = lastDiscoveryDB
			s.cacheMu.Unlock()
			return false, nil
		}
	}

	// Run discovery with timeout
	discoverCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout*2) // 2x for all sources
	defer cancel()

	nowMs := time.Now().UnixMilli()

	// Discover package.json scripts
	if err := s.discoverPackageJSON(discoverCtx, repoRoot, nowMs); err != nil {
		s.opts.Logger.Warn("package.json discovery failed", "error", err, "repo", repoRoot)
		// Continue with other discovery sources
	}

	// Discover Makefile targets
	if err := s.discoverMakefile(discoverCtx, repoRoot, nowMs); err != nil {
		s.opts.Logger.Warn("Makefile discovery failed", "error", err, "repo", repoRoot)
		// Continue with other discovery sources
	}

	// Update cache
	s.cacheMu.Lock()
	s.cache[repoRoot] = time.Now()
	s.cacheMu.Unlock()

	return true, nil
}

// DiscoverIfNeeded checks if discovery is needed and runs it.
// This is a convenience method for use in the suggestion pipeline.
func (s *Service) DiscoverIfNeeded(ctx context.Context, repoRoot string) error {
	_, err := s.Discover(ctx, repoRoot)
	return err
}

// GetTasks retrieves all discovered tasks for a repository.
func (s *Service) GetTasks(ctx context.Context, repoRoot string) ([]Task, error) {
	rows, err := s.selectStmt.QueryContext(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var desc sql.NullString
		if err := rows.Scan(&t.Kind, &t.Name, &t.Command, &desc); err != nil {
			return nil, err
		}
		t.RepoKey = repoRoot
		t.Description = desc.String
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// GetTasksByKind retrieves tasks of a specific kind for a repository.
func (s *Service) GetTasksByKind(ctx context.Context, repoRoot string, kind TaskKind) ([]Task, error) {
	tasks, err := s.GetTasks(ctx, repoRoot)
	if err != nil {
		return nil, err
	}

	var filtered []Task
	for _, t := range tasks {
		if t.Kind == kind {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// saveTasks saves discovered tasks to the database.
func (s *Service) saveTasks(ctx context.Context, repoRoot string, kind TaskKind, tasks []Task, nowMs int64) error {
	// Delete existing tasks of this kind for the repo
	if _, err := s.deleteStmt.ExecContext(ctx, repoRoot, kind); err != nil {
		return err
	}

	// Insert new tasks
	for _, t := range tasks {
		_, err := s.insertStmt.ExecContext(ctx, repoRoot, t.Kind, t.Name, t.Command, t.Description, nowMs)
		if err != nil {
			return err
		}
	}

	return nil
}

// fileExists checks if a file exists in the given directory.
func fileExists(dir, filename string) bool {
	path := filepath.Join(dir, filename)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// InvalidateCache clears the discovery cache for a repository.
// This should be called when the repository changes (e.g., git pull).
func (s *Service) InvalidateCache(repoRoot string) {
	s.cacheMu.Lock()
	delete(s.cache, repoRoot)
	s.cacheMu.Unlock()
}

// InvalidateAll clears the entire discovery cache.
func (s *Service) InvalidateAll() {
	s.cacheMu.Lock()
	s.cache = make(map[string]time.Time)
	s.cacheMu.Unlock()
}

// TTL returns the configured TTL.
func (s *Service) TTL() time.Duration {
	return s.opts.TTL
}
