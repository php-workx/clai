// Package search provides full-text search for command history via SQLite FTS5.
// It implements the search feature specified in tech_suggestions_v3.md Section 12.
package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrFTS5Unavailable indicates that FTS5 is not available in the SQLite build.
var ErrFTS5Unavailable = errors.New("FTS5 not available; history search disabled")

// FTS5 table and schema definitions.
const (
	// FTS5TableName is the name of the FTS5 virtual table.
	FTS5TableName = "command_fts"

	// createFTS5Table creates the FTS5 virtual table for command search.
	// Per spec Section 12.2: content='command_event', content_rowid='id'
	createFTS5Table = `
		CREATE VIRTUAL TABLE IF NOT EXISTS command_fts
		USING fts5(cmd_raw, repo_key, cwd, content='command_event', content_rowid='id')
	`

	// DefaultLimit is the default number of search results.
	DefaultLimit = 20

	// MaxLimit is the maximum allowed search results.
	MaxLimit = 100
)

// SearchResult represents a single search result.
type SearchResult struct {
	ID        int64   // Command event ID
	CmdRaw    string  // Raw command
	RepoKey   string  // Repository key (may be empty)
	Cwd       string  // Working directory
	Timestamp int64   // Event timestamp (ms)
	Score     float64 // BM25 score
}

// SearchOptions configures search behavior.
type SearchOptions struct {
	// RepoKey filters results to a specific repository.
	RepoKey string

	// Cwd filters results to a specific working directory.
	Cwd string

	// Limit is the maximum number of results.
	Limit int

	// IncludeRecency weights results by recency in addition to BM25.
	IncludeRecency bool
}

// Service provides full-text search capabilities.
type Service struct {
	db     *sql.DB
	logger *slog.Logger

	// FTS5 availability
	fts5Available bool

	// Fallback mode (LIKE-based search)
	fallbackEnabled bool

	// Prepared statements
	searchStmt   *sql.Stmt
	insertStmt   *sql.Stmt
	deleteStmt   *sql.Stmt
	fallbackStmt *sql.Stmt
}

// Config configures the search service.
type Config struct {
	// Logger for search operations.
	Logger *slog.Logger

	// EnableFallback enables LIKE-based search when FTS5 is unavailable.
	EnableFallback bool
}

// DefaultConfig returns the default search configuration.
func DefaultConfig() Config {
	return Config{
		Logger:         slog.Default(),
		EnableFallback: false,
	}
}

// NewService creates a new search service.
func NewService(db *sql.DB, cfg Config) (*Service, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Service{
		db:              db,
		logger:          cfg.Logger,
		fallbackEnabled: cfg.EnableFallback,
	}

	// Check FTS5 availability and initialize
	if err := s.initFTS5(); err != nil {
		if errors.Is(err, ErrFTS5Unavailable) {
			s.fts5Available = false
			s.logger.Warn("FTS5 not available; history search disabled")
		} else {
			return nil, err
		}
	} else {
		s.fts5Available = true
	}

	if err := s.prepareStatements(); err != nil {
		return nil, err
	}

	return s, nil
}

// initFTS5 checks FTS5 availability and creates the table if supported.
func (s *Service) initFTS5() error {
	// Check if FTS5 is available by attempting to create a test table
	_, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS _fts5_test USING fts5(test)`)
	if err != nil {
		if strings.Contains(err.Error(), "no such module") ||
			strings.Contains(err.Error(), "fts5") {
			return ErrFTS5Unavailable
		}
		return err
	}

	// Drop the test table
	_, _ = s.db.Exec(`DROP TABLE IF EXISTS _fts5_test`)

	// Create the actual FTS5 table
	_, err = s.db.Exec(createFTS5Table)
	return err
}

// prepareStatements prepares SQL statements.
func (s *Service) prepareStatements() error {
	var err error

	if s.fts5Available {
		// FTS5 search query with BM25 scoring
		// Per spec Section 12.3: top K results ordered by bm25 score and recency
		s.searchStmt, err = s.db.Prepare(`
			SELECT ce.id, ce.cmd_raw, COALESCE(ce.repo_key, ''), ce.cwd, ce.ts,
			       bm25(command_fts, 1.0, 0.5, 0.3) as score
			FROM command_fts
			JOIN command_event ce ON command_fts.rowid = ce.id
			WHERE command_fts MATCH ?
			  AND (? = '' OR ce.repo_key = ?)
			  AND (? = '' OR ce.cwd = ?)
			  AND ce.ephemeral = 0
			ORDER BY score, ce.ts DESC
			LIMIT ?
		`)
		if err != nil {
			return err
		}

		// Insert into FTS5 index
		// Per spec Section 12.4: Do not index ephemeral events
		s.insertStmt, err = s.db.Prepare(`
			INSERT INTO command_fts(rowid, cmd_raw, repo_key, cwd)
			SELECT id, cmd_raw, COALESCE(repo_key, ''), cwd
			FROM command_event
			WHERE id = ? AND ephemeral = 0
		`)
		if err != nil {
			s.searchStmt.Close()
			return err
		}

		// Delete from FTS5 index
		s.deleteStmt, err = s.db.Prepare(`
			INSERT INTO command_fts(command_fts, rowid, cmd_raw, repo_key, cwd)
			VALUES('delete', ?, ?, ?, ?)
		`)
		if err != nil {
			s.searchStmt.Close()
			s.insertStmt.Close()
			return err
		}
	}

	// Fallback LIKE-based search
	if s.fallbackEnabled || !s.fts5Available {
		s.fallbackStmt, err = s.db.Prepare(`
			SELECT id, cmd_raw, COALESCE(repo_key, ''), cwd, ts, 0.0 as score
			FROM command_event
			WHERE cmd_raw LIKE ?
			  AND (? = '' OR repo_key = ?)
			  AND (? = '' OR cwd = ?)
			  AND ephemeral = 0
			ORDER BY ts DESC
			LIMIT ?
		`)
		if err != nil {
			if s.searchStmt != nil {
				s.searchStmt.Close()
			}
			if s.insertStmt != nil {
				s.insertStmt.Close()
			}
			if s.deleteStmt != nil {
				s.deleteStmt.Close()
			}
			return err
		}
	}

	return nil
}

// Close releases resources held by the service.
func (s *Service) Close() error {
	if s.searchStmt != nil {
		s.searchStmt.Close()
	}
	if s.insertStmt != nil {
		s.insertStmt.Close()
	}
	if s.deleteStmt != nil {
		s.deleteStmt.Close()
	}
	if s.fallbackStmt != nil {
		s.fallbackStmt.Close()
	}
	return nil
}

// Search performs a full-text search on command history.
func (s *Service) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	// Try FTS5 search first
	if s.fts5Available {
		return s.searchFTS5(ctx, query, opts, limit)
	}

	// Fall back to LIKE search if enabled
	if s.fallbackEnabled {
		return s.searchLike(ctx, query, opts, limit)
	}

	return nil, ErrFTS5Unavailable
}

// searchFTS5 performs FTS5-based search.
func (s *Service) searchFTS5(ctx context.Context, query string, opts SearchOptions, limit int) ([]SearchResult, error) {
	// Prepare FTS5 query - escape special characters
	ftsQuery := escapeFTS5Query(query)

	rows, err := s.searchStmt.QueryContext(ctx,
		ftsQuery,
		opts.RepoKey, opts.RepoKey,
		opts.Cwd, opts.Cwd,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanResults(rows)
}

// searchLike performs LIKE-based fallback search.
func (s *Service) searchLike(ctx context.Context, query string, opts SearchOptions, limit int) ([]SearchResult, error) {
	// Convert query to LIKE pattern
	likePattern := "%" + escapeLikePattern(query) + "%"

	rows, err := s.fallbackStmt.QueryContext(ctx,
		likePattern,
		opts.RepoKey, opts.RepoKey,
		opts.Cwd, opts.Cwd,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanResults(rows)
}

// scanResults scans search result rows.
func (s *Service) scanResults(rows *sql.Rows) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.CmdRaw, &r.RepoKey, &r.Cwd, &r.Timestamp, &r.Score); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// IndexEvent indexes a command event for search.
// This should be called during event ingestion.
// Per spec Section 12.4: Do not index ephemeral events.
func (s *Service) IndexEvent(ctx context.Context, eventID int64) error {
	if !s.fts5Available {
		return nil // Silently skip if FTS5 unavailable
	}

	_, err := s.insertStmt.ExecContext(ctx, eventID)
	return err
}

// IndexEventBatch indexes multiple events efficiently.
func (s *Service) IndexEventBatch(ctx context.Context, eventIDs []int64) error {
	if !s.fts5Available || len(eventIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt := tx.StmtContext(ctx, s.insertStmt)
	for _, id := range eventIDs {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// RemoveEvent removes an event from the search index.
func (s *Service) RemoveEvent(ctx context.Context, eventID int64, cmdRaw, repoKey, cwd string) error {
	if !s.fts5Available {
		return nil
	}

	_, err := s.deleteStmt.ExecContext(ctx, eventID, cmdRaw, repoKey, cwd)
	return err
}

// RebuildIndex rebuilds the FTS5 index from scratch.
// This is useful for recovery or after bulk data changes.
func (s *Service) RebuildIndex(ctx context.Context) error {
	if !s.fts5Available {
		return ErrFTS5Unavailable
	}

	s.logger.Info("rebuilding FTS5 index")
	start := time.Now()

	// Drop and recreate the FTS5 table
	_, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS command_fts`)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, createFTS5Table)
	if err != nil {
		return err
	}

	// Rebuild content
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO command_fts(rowid, cmd_raw, repo_key, cwd)
		SELECT id, cmd_raw, COALESCE(repo_key, ''), cwd
		FROM command_event
		WHERE ephemeral = 0
	`)
	if err != nil {
		return err
	}

	s.logger.Info("FTS5 index rebuilt", "duration", time.Since(start))
	return nil
}

// FTS5Available returns whether FTS5 is available.
func (s *Service) FTS5Available() bool {
	return s.fts5Available
}

// FallbackEnabled returns whether LIKE fallback is enabled.
func (s *Service) FallbackEnabled() bool {
	return s.fallbackEnabled
}

// escapeFTS5Query escapes special characters for FTS5 queries.
func escapeFTS5Query(query string) string {
	// FTS5 special characters: " * ^ : ( ) -
	// For simple substring matching, we wrap in quotes
	escaped := strings.ReplaceAll(query, `"`, `""`)
	return fmt.Sprintf(`"%s"`, escaped)
}

// escapeLikePattern escapes special characters for LIKE queries.
func escapeLikePattern(pattern string) string {
	// Escape % and _ which are wildcards in LIKE
	pattern = strings.ReplaceAll(pattern, `%`, `\%`)
	pattern = strings.ReplaceAll(pattern, `_`, `\_`)
	return pattern
}
