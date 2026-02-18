package typo

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-typo-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create command_score table
	_, err = db.Exec(`
		CREATE TABLE command_score (
			scope     TEXT NOT NULL,
			cmd_norm  TEXT NOT NULL,
			score     REAL NOT NULL,
			last_ts   INTEGER NOT NULL,
			PRIMARY KEY(scope, cmd_norm)
		)
	`)
	require.NoError(t, err)

	return db
}

func TestLevenshteinDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},        // substitution
		{"abc", "abcd", 1},       // insertion
		{"abcd", "abc", 1},       // deletion
		{"kitten", "sitting", 3}, // classic example
		{"gti", "git", 2},        // typo example
	}

	for _, tc := range tests {
		got := LevenshteinDistance(tc.a, tc.b)
		assert.Equal(t, tc.want, got, "LevenshteinDistance(%q, %q)", tc.a, tc.b)
	}
}

func TestDamerauLevenshteinDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},        // substitution
		{"gti", "git", 1},        // transposition
		{"ab", "ba", 1},          // transposition
		{"abc", "bac", 1},        // transposition at start
		{"kitten", "sitting", 3}, // complex
	}

	for _, tc := range tests {
		got := DamerauLevenshteinDistance(tc.a, tc.b)
		assert.Equal(t, tc.want, got, "DamerauLevenshteinDistance(%q, %q)", tc.a, tc.b)
	}
}

func TestSimilarity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b    string
		wantMin float64
		wantMax float64
	}{
		{"abc", "abc", 1.0, 1.0},
		{"gti", "git", 0.6, 0.7}, // transposition
		{"", "", 1.0, 1.0},
		{"abc", "xyz", 0.0, 0.1},
	}

	for _, tc := range tests {
		got := Similarity(tc.a, tc.b)
		assert.GreaterOrEqual(t, got, tc.wantMin, "Similarity(%q, %q) >= %f", tc.a, tc.b, tc.wantMin)
		assert.LessOrEqual(t, got, tc.wantMax, "Similarity(%q, %q) <= %f", tc.a, tc.b, tc.wantMax)
	}
}

func TestCorrector_NewCorrector(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	corrector, err := NewCorrector(db, DefaultCorrectorConfig())
	require.NoError(t, err)
	defer corrector.Close()

	assert.NotNil(t, corrector)
}

func TestCorrector_NewCorrector_DefaultsInvalidConfig(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Invalid config values should be defaulted
	cfg := CorrectorConfig{
		SimilarityThreshold: -1,
		TopPercent:          0,
		CandidateLimit:      0,
		MaxSuggestions:      0,
	}

	corrector, err := NewCorrector(db, cfg)
	require.NoError(t, err)
	defer corrector.Close()

	assert.Equal(t, DefaultSimilarityThreshold, corrector.similarityThreshold)
	assert.Equal(t, DefaultTopPercent, corrector.topPercent)
}

func TestShouldCorrect(t *testing.T) {
	t.Parallel()

	assert.True(t, ShouldCorrect(127))
	assert.False(t, ShouldCorrect(0))
	assert.False(t, ShouldCorrect(1))
	assert.False(t, ShouldCorrect(128))
}

func TestCorrector_Correct_SimpleTypo(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Insert candidate commands
	_, err := db.Exec(`
		INSERT INTO command_score (scope, cmd_norm, score, last_ts) VALUES
		('__global__', 'git status', 100, 1000),
		('__global__', 'git commit', 80, 1000),
		('__global__', 'git push', 60, 1000)
	`)
	require.NoError(t, err)

	// Use lower threshold since "gti" vs "git" has similarity ~0.66
	corrector, err := NewCorrector(db, CorrectorConfig{
		SimilarityThreshold: 0.6,
		TopPercent:          100, // Consider all candidates
		CandidateLimit:      100,
		MaxSuggestions:      3,
	})
	require.NoError(t, err)
	defer corrector.Close()

	ctx := context.Background()

	// "gti" is a common typo for "git"
	corrections, err := corrector.Correct(ctx, "gti status", "")
	require.NoError(t, err)
	require.NotEmpty(t, corrections)

	// Should suggest "git status"
	found := false
	for _, c := range corrections {
		if c.Suggested == "git status" {
			found = true
			break
		}
	}
	assert.True(t, found, "should suggest 'git status'")
}

func TestCorrector_Correct_NoMatch(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Insert candidate commands
	_, err := db.Exec(`
		INSERT INTO command_score (scope, cmd_norm, score, last_ts) VALUES
		('__global__', 'git status', 100, 1000)
	`)
	require.NoError(t, err)

	corrector, err := NewCorrector(db, DefaultCorrectorConfig())
	require.NoError(t, err)
	defer corrector.Close()

	ctx := context.Background()

	// "xyz" is too different from "git"
	corrections, err := corrector.Correct(ctx, "xyz status", "")
	require.NoError(t, err)
	assert.Empty(t, corrections)
}

func TestCorrector_Correct_EmptyCommand(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	corrector, err := NewCorrector(db, DefaultCorrectorConfig())
	require.NoError(t, err)
	defer corrector.Close()

	ctx := context.Background()

	corrections, err := corrector.Correct(ctx, "", "")
	require.NoError(t, err)
	assert.Empty(t, corrections)
}

func TestCorrector_Correct_RepoScope(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Insert repo-specific and global commands
	_, err := db.Exec(`
		INSERT INTO command_score (scope, cmd_norm, score, last_ts) VALUES
		('/test/repo', 'npm test', 100, 1000),
		('__global__', 'git status', 100, 1000)
	`)
	require.NoError(t, err)

	// Use lower threshold since "npn" vs "npm" has similarity ~0.66
	corrector, err := NewCorrector(db, CorrectorConfig{
		SimilarityThreshold: 0.6,
		TopPercent:          100, // Consider all candidates
		CandidateLimit:      100,
		MaxSuggestions:      3,
	})
	require.NoError(t, err)
	defer corrector.Close()

	ctx := context.Background()

	// "npn" is a typo for "npm"
	corrections, err := corrector.Correct(ctx, "npn test", "/test/repo")
	require.NoError(t, err)
	require.NotEmpty(t, corrections)

	// Should suggest "npm test"
	assert.Equal(t, "npm test", corrections[0].Suggested)
}

func TestCorrector_Correct_MaxSuggestions(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Insert many similar commands
	_, err := db.Exec(`
		INSERT INTO command_score (scope, cmd_norm, score, last_ts) VALUES
		('__global__', 'git status', 100, 1000),
		('__global__', 'git stash', 90, 1000),
		('__global__', 'git stage', 80, 1000),
		('__global__', 'git stats', 70, 1000),
		('__global__', 'git start', 60, 1000)
	`)
	require.NoError(t, err)

	corrector, err := NewCorrector(db, CorrectorConfig{
		SimilarityThreshold: 0.5, // Lower threshold to match more
		TopPercent:          100, // Consider all
		CandidateLimit:      100,
		MaxSuggestions:      2,
	})
	require.NoError(t, err)
	defer corrector.Close()

	ctx := context.Background()

	corrections, err := corrector.Correct(ctx, "gti statu", "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(corrections), 2)
}

func TestCorrector_Correct_NoCandidates(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	// Empty database

	corrector, err := NewCorrector(db, DefaultCorrectorConfig())
	require.NoError(t, err)
	defer corrector.Close()

	ctx := context.Background()

	corrections, err := corrector.Correct(ctx, "gti status", "")
	require.NoError(t, err)
	assert.Empty(t, corrections)
}

func TestExtractFirstToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"git", "git"},
		{"git status", "git"},
		{"  git status  ", "git"},
		{"git\tstatus", "git"},
		{"single", "single"},
	}

	for _, tc := range tests {
		got := extractFirstToken(tc.input)
		assert.Equal(t, tc.want, got, "extractFirstToken(%q)", tc.input)
	}
}

func TestDefaultCorrectorConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultCorrectorConfig()
	assert.Equal(t, DefaultSimilarityThreshold, cfg.SimilarityThreshold)
	assert.Equal(t, DefaultTopPercent, cfg.TopPercent)
	assert.Equal(t, DefaultCandidateLimit, cfg.CandidateLimit)
	assert.Equal(t, DefaultMaxSuggestions, cfg.MaxSuggestions)
	assert.NotNil(t, cfg.Logger)
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0.7, DefaultSimilarityThreshold)
	assert.Equal(t, 10, DefaultTopPercent)
	assert.Equal(t, 500, DefaultCandidateLimit)
	assert.Equal(t, 3, DefaultMaxSuggestions)
	assert.Equal(t, 127, ExitCodeNotFound)
}
