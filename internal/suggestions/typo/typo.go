// Package typo provides typo correction for failed commands.
// Per spec Section 11.6: "Did You Mean?" functionality.
package typo

import (
	"context"
	"database/sql"
	"log/slog"
	"sort"
	"strings"
)

// Configuration defaults per spec Section 11.6.3.
const (
	// DefaultSimilarityThreshold is the minimum similarity for a suggestion.
	// Per spec: 0.7 Damerau-Levenshtein similarity.
	DefaultSimilarityThreshold = 0.7

	// DefaultTopPercent is the minimum frequency percentile for candidates.
	// Per spec: top 10% by score.
	DefaultTopPercent = 10

	// DefaultCandidateLimit is the maximum candidates to retrieve.
	// Per spec: N=200-1000.
	DefaultCandidateLimit = 500

	// DefaultMaxSuggestions is the maximum corrections to return.
	DefaultMaxSuggestions = 3

	// ExitCodeNotFound is the "command not found" exit code.
	// Per spec Section 11.6.1.
	ExitCodeNotFound = 127
)

// Corrector provides typo correction for failed commands.
type Corrector struct {
	db                  *sql.DB
	similarityThreshold float64
	topPercent          int
	candidateLimit      int
	maxSuggestions      int
	logger              *slog.Logger

	// Prepared statements
	stmtGetTopCommands *sql.Stmt
}

// CorrectorConfig configures the typo corrector.
type CorrectorConfig struct {
	// SimilarityThreshold is the minimum similarity score (0-1).
	// Default: 0.7
	SimilarityThreshold float64

	// TopPercent is the minimum frequency percentile for candidates.
	// Default: 10 (top 10%)
	TopPercent int

	// CandidateLimit is the maximum candidates to consider.
	// Default: 500
	CandidateLimit int

	// MaxSuggestions is the maximum corrections to return.
	// Default: 3
	MaxSuggestions int

	// Logger for corrector operations.
	Logger *slog.Logger
}

// DefaultCorrectorConfig returns the default corrector configuration.
func DefaultCorrectorConfig() CorrectorConfig {
	return CorrectorConfig{
		SimilarityThreshold: DefaultSimilarityThreshold,
		TopPercent:          DefaultTopPercent,
		CandidateLimit:      DefaultCandidateLimit,
		MaxSuggestions:      DefaultMaxSuggestions,
		Logger:              slog.Default(),
	}
}

// Correction represents a typo correction suggestion.
type Correction struct {
	// Original is the misspelled command/token.
	Original string

	// Suggested is the corrected command/token.
	Suggested string

	// Similarity is the similarity score (0-1).
	Similarity float64

	// Score is the frequency score of the suggested command.
	Score float64
}

// NewCorrector creates a new typo corrector.
func NewCorrector(db *sql.DB, cfg CorrectorConfig) (*Corrector, error) {
	if cfg.SimilarityThreshold <= 0 || cfg.SimilarityThreshold > 1 {
		cfg.SimilarityThreshold = DefaultSimilarityThreshold
	}
	if cfg.TopPercent <= 0 || cfg.TopPercent > 100 {
		cfg.TopPercent = DefaultTopPercent
	}
	if cfg.CandidateLimit <= 0 {
		cfg.CandidateLimit = DefaultCandidateLimit
	}
	if cfg.MaxSuggestions <= 0 {
		cfg.MaxSuggestions = DefaultMaxSuggestions
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	c := &Corrector{
		db:                  db,
		similarityThreshold: cfg.SimilarityThreshold,
		topPercent:          cfg.TopPercent,
		candidateLimit:      cfg.CandidateLimit,
		maxSuggestions:      cfg.MaxSuggestions,
		logger:              cfg.Logger,
	}

	// Prepare statements
	var err error
	c.stmtGetTopCommands, err = db.Prepare(`
		SELECT cmd_norm, score
		FROM command_score
		WHERE scope = ?
		ORDER BY score DESC
		LIMIT ?
	`)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// Close releases resources.
func (c *Corrector) Close() error {
	if c.stmtGetTopCommands != nil {
		c.stmtGetTopCommands.Close()
	}
	return nil
}

// ShouldCorrect returns true if the exit code indicates a typo.
// Per spec Section 11.6.1.
func ShouldCorrect(exitCode int) bool {
	return exitCode == ExitCodeNotFound
}

// Correct attempts to correct a failed command.
// Per spec Section 11.6.
func (c *Corrector) Correct(ctx context.Context, failedCmd string, repoKey string) ([]Correction, error) {
	// Extract the first token (command name) for comparison
	failedToken := extractFirstToken(failedCmd)
	if failedToken == "" {
		return nil, nil
	}

	// Get candidate commands
	candidates, err := c.getCandidates(ctx, repoKey)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Apply top percent filter
	// Per spec: only suggest when candidate is in top 10% by score
	topCount := (len(candidates) * c.topPercent) / 100
	if topCount < 1 {
		topCount = 1
	}
	candidates = candidates[:topCount]

	// Find matches above threshold
	var corrections []Correction
	for _, cand := range candidates {
		candToken := extractFirstToken(cand.cmdNorm)
		if candToken == "" {
			continue
		}

		// Skip if tokens are identical (not a typo)
		if candToken == failedToken {
			continue
		}

		similarity := Similarity(failedToken, candToken)
		if similarity >= c.similarityThreshold {
			corrections = append(corrections, Correction{
				Original:   failedToken,
				Suggested:  cand.cmdNorm,
				Similarity: similarity,
				Score:      cand.score,
			})
		}
	}

	// Sort by similarity (highest first), then by score
	sort.Slice(corrections, func(i, j int) bool {
		if corrections[i].Similarity != corrections[j].Similarity {
			return corrections[i].Similarity > corrections[j].Similarity
		}
		return corrections[i].Score > corrections[j].Score
	})

	// Limit results
	if len(corrections) > c.maxSuggestions {
		corrections = corrections[:c.maxSuggestions]
	}

	return corrections, nil
}

// candidate represents a command candidate from the database.
type candidate struct {
	cmdNorm string
	score   float64
}

// getCandidates retrieves high-frequency commands for comparison.
// Per spec Section 11.6.2: repo-scoped first, then global.
func (c *Corrector) getCandidates(ctx context.Context, repoKey string) ([]candidate, error) {
	var candidates []candidate

	// Try repo-scoped first
	if repoKey != "" {
		repoCandidates, err := c.queryTopCommands(ctx, repoKey)
		if err != nil {
			c.logger.Debug("repo candidates query failed", "error", err)
		} else {
			candidates = append(candidates, repoCandidates...)
		}
	}

	// Add global candidates
	globalCandidates, err := c.queryTopCommands(ctx, "__global__")
	if err != nil {
		c.logger.Debug("global candidates query failed", "error", err)
	} else {
		candidates = append(candidates, globalCandidates...)
	}

	// De-duplicate by cmd_norm, keeping highest score
	seen := make(map[string]float64)
	var deduped []candidate
	for _, cand := range candidates {
		if existingScore, ok := seen[cand.cmdNorm]; !ok || cand.score > existingScore {
			if !ok {
				deduped = append(deduped, cand)
			}
			seen[cand.cmdNorm] = cand.score
		}
	}

	// Re-sort by score descending
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].score > deduped[j].score
	})

	return deduped, nil
}

// queryTopCommands queries top commands for a scope.
func (c *Corrector) queryTopCommands(ctx context.Context, scope string) ([]candidate, error) {
	rows, err := c.stmtGetTopCommands.QueryContext(ctx, scope, c.candidateLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []candidate
	for rows.Next() {
		var cand candidate
		if err := rows.Scan(&cand.cmdNorm, &cand.score); err != nil {
			return nil, err
		}
		candidates = append(candidates, cand)
	}

	return candidates, rows.Err()
}

// extractFirstToken extracts the first word from a command.
func extractFirstToken(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}

	// Find end of first token
	for i, ch := range cmd {
		if ch == ' ' || ch == '\t' {
			return cmd[:i]
		}
	}

	return cmd
}
