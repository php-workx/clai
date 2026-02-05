package score

import (
	"context"
	"database/sql"
	"math"
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

	dir, err := os.MkdirTemp("", "clai-score-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create minimal schema for testing
	_, err = db.Exec(`
		CREATE TABLE command_score (
			scope         TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			score         REAL NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, cmd_norm)
		);
		CREATE INDEX idx_command_score_scope ON command_score(scope, score DESC);
	`)
	require.NoError(t, err)

	return db
}

func TestNewFrequencyStore(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	t.Run("default options", func(t *testing.T) {
		fs, err := NewFrequencyStore(db, DefaultFrequencyOptions())
		require.NoError(t, err)
		defer fs.Close()

		assert.Equal(t, int64(DefaultTauMs), fs.TauMs())
	})

	t.Run("custom tau", func(t *testing.T) {
		customTau := int64(14 * 24 * 60 * 60 * 1000) // 14 days
		fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: customTau})
		require.NoError(t, err)
		defer fs.Close()

		assert.Equal(t, customTau, fs.TauMs())
	})

	t.Run("tau below minimum is clamped", func(t *testing.T) {
		fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: 1000}) // 1 second
		require.NoError(t, err)
		defer fs.Close()

		assert.Equal(t, int64(MinTauMs), fs.TauMs())
	})

	t.Run("zero tau uses default", func(t *testing.T) {
		fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: 0})
		require.NoError(t, err)
		defer fs.Close()

		assert.Equal(t, int64(DefaultTauMs), fs.TauMs())
	})
}

func TestFrequencyStore_Update(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: DefaultTauMs})
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	t.Run("first occurrence sets score to 1", func(t *testing.T) {
		err := fs.Update(ctx, ScopeGlobal, "git status", 1000)
		require.NoError(t, err)

		score, err := fs.GetScoreAt(ctx, ScopeGlobal, "git status", 1000)
		require.NoError(t, err)
		assert.Equal(t, 1.0, score)
	})

	t.Run("immediate repeat increments to 2", func(t *testing.T) {
		// Update again at the same time
		err := fs.Update(ctx, ScopeGlobal, "git status", 1000)
		require.NoError(t, err)

		score, err := fs.GetScoreAt(ctx, ScopeGlobal, "git status", 1000)
		require.NoError(t, err)
		// score = 1 * exp(0) + 1 = 1 + 1 = 2
		assert.Equal(t, 2.0, score)
	})

	t.Run("score decays over time", func(t *testing.T) {
		// New command
		err := fs.Update(ctx, ScopeGlobal, "npm test", 0)
		require.NoError(t, err)

		// Get score at time 0
		scoreAt0, err := fs.GetScoreAt(ctx, ScopeGlobal, "npm test", 0)
		require.NoError(t, err)
		assert.Equal(t, 1.0, scoreAt0)

		// Get score after one tau (7 days)
		scoreAfterTau, err := fs.GetScoreAt(ctx, ScopeGlobal, "npm test", DefaultTauMs)
		require.NoError(t, err)
		// score = 1 * exp(-tau/tau) = exp(-1) ≈ 0.368
		expected := math.Exp(-1.0)
		assert.InDelta(t, expected, scoreAfterTau, 0.001)
	})
}

func TestFrequencyStore_UpdateBoth(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: DefaultTauMs})
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	t.Run("updates both global and repo scopes", func(t *testing.T) {
		repoKey := "sha256:testrepo"
		err := fs.UpdateBoth(ctx, "make build", repoKey, 5000)
		require.NoError(t, err)

		// Check global scope
		globalScore, err := fs.GetScoreAt(ctx, ScopeGlobal, "make build", 5000)
		require.NoError(t, err)
		assert.Equal(t, 1.0, globalScore)

		// Check repo scope
		repoScore, err := fs.GetScoreAt(ctx, repoKey, "make build", 5000)
		require.NoError(t, err)
		assert.Equal(t, 1.0, repoScore)
	})

	t.Run("empty repo key only updates global", func(t *testing.T) {
		err := fs.UpdateBoth(ctx, "ls -la", "", 6000)
		require.NoError(t, err)

		// Check global scope
		globalScore, err := fs.GetScoreAt(ctx, ScopeGlobal, "ls -la", 6000)
		require.NoError(t, err)
		assert.Equal(t, 1.0, globalScore)
	})
}

func TestFrequencyStore_GetTopCommands(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: DefaultTauMs})
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()
	baseTs := int64(1000000)

	// Insert commands with different frequencies
	commands := []string{"git status", "git commit", "make build", "npm test"}
	for i, cmd := range commands {
		// More recent commands get lower multiplier (to test decay effects)
		for j := 0; j <= i; j++ {
			require.NoError(t, fs.Update(ctx, ScopeGlobal, cmd, baseTs))
		}
	}

	t.Run("returns top commands by score", func(t *testing.T) {
		top, err := fs.GetTopCommandsAt(ctx, ScopeGlobal, 3, baseTs)
		require.NoError(t, err)

		assert.Len(t, top, 3)
		// npm test has highest count (4)
		assert.Equal(t, "npm test", top[0].CmdNorm)
		// make build has second highest (3)
		assert.Equal(t, "make build", top[1].CmdNorm)
	})

	t.Run("respects limit", func(t *testing.T) {
		top, err := fs.GetTopCommandsAt(ctx, ScopeGlobal, 2, baseTs)
		require.NoError(t, err)

		assert.Len(t, top, 2)
	})

	t.Run("returns empty for nonexistent scope", func(t *testing.T) {
		top, err := fs.GetTopCommandsAt(ctx, "nonexistent", 10, baseTs)
		require.NoError(t, err)

		assert.Empty(t, top)
	})
}

func TestFrequencyStore_GetScore_Nonexistent(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	fs, err := NewFrequencyStore(db, FrequencyOptions{TauMs: DefaultTauMs})
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	score, err := fs.GetScoreAt(ctx, ScopeGlobal, "nonexistent", 1000)
	require.NoError(t, err)
	assert.Equal(t, 0.0, score)
}

func TestDecayFormula(t *testing.T) {
	t.Parallel()

	// Test the decay formula independently
	tests := []struct {
		name     string
		score    float64
		elapsed  int64
		tauMs    int64
		expected float64
	}{
		{
			name:     "no decay at t=0",
			score:    10.0,
			elapsed:  0,
			tauMs:    DefaultTauMs,
			expected: 10.0,
		},
		{
			name:     "half-life decay",
			score:    10.0,
			elapsed:  DefaultTauMs, // one tau
			tauMs:    DefaultTauMs,
			expected: 10.0 * math.Exp(-1.0), // ~3.68
		},
		{
			name:     "full decay after many tau",
			score:    10.0,
			elapsed:  10 * DefaultTauMs, // 10 tau
			tauMs:    DefaultTauMs,
			expected: 10.0 * math.Exp(-10.0), // ~0.00045
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decay := math.Exp(-float64(tt.elapsed) / float64(tt.tauMs))
			result := tt.score * decay
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}
