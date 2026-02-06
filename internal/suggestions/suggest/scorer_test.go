package suggest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/score"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t testing.TB) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-scorer-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create all required tables
	_, err = db.Exec(`
		-- Command event table (for transition store's getPrevStmt)
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL,
			repo_key      TEXT
		);

		-- Command score table
		CREATE TABLE command_score (
			scope         TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			score         REAL NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, cmd_norm)
		);

		-- Transition table
		CREATE TABLE transition (
			scope         TEXT NOT NULL,
			prev_norm     TEXT NOT NULL,
			next_norm     TEXT NOT NULL,
			count         INTEGER NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_norm, next_norm)
		);

		-- Project task table
		CREATE TABLE project_task (
			repo_key      TEXT NOT NULL,
			kind          TEXT NOT NULL,
			name          TEXT NOT NULL,
			command       TEXT NOT NULL,
			description   TEXT,
			discovered_ts INTEGER NOT NULL,
			PRIMARY KEY(repo_key, kind, name)
		);
	`)
	require.NoError(t, err)

	return db
}

func TestScorer_NewScorer(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{DB: db}, DefaultScorerConfig())
	require.NoError(t, err)
	assert.NotNil(t, scorer)
	assert.Equal(t, DefaultTopK, scorer.TopK())
}

func TestScorer_NewScorer_CustomConfig(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	cfg := ScorerConfig{
		TopK: 5,
		Weights: Weights{
			RepoTransition: 100,
		},
	}

	scorer, err := NewScorer(ScorerDependencies{DB: db}, cfg)
	require.NoError(t, err)
	assert.Equal(t, 5, scorer.TopK())
	assert.Equal(t, float64(100), scorer.Weights().RepoTransition)
}

func TestScorer_Suggest_Empty(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{DB: db}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{})
	require.NoError(t, err)
	assert.Empty(t, suggestions)
}

func TestScorer_Suggest_WithFrequency(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create frequency store
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	// Add some commands
	ctx := context.Background()
	nowMs := int64(1000000)
	for i := 0; i < 5; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git commit", nowMs))
	}

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)

	// First suggestion should be git status (higher frequency)
	assert.Equal(t, "git status", suggestions[0].Command)
	assert.Contains(t, suggestions[0].Reasons, ReasonGlobalFrequency)
}

func TestScorer_Suggest_WithTransitions(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create transition store
	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	// Add transitions
	ctx := context.Background()
	nowMs := int64(1000000)
	for i := 0; i < 5; i++ {
		require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git add .", "git commit", nowMs))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git add .", "git status", nowMs))
	}

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		TransitionStore: transStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastCmd: "git add .",
		NowMs:   nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)

	// First suggestion should be git commit (higher transition count)
	assert.Equal(t, "git commit", suggestions[0].Command)
	assert.Contains(t, suggestions[0].Reasons, ReasonGlobalTransition)
}

func TestScorer_Suggest_WithProjectTasks(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create discovery service
	discSvc, err := discovery.NewService(db, discovery.Options{})
	require.NoError(t, err)
	defer discSvc.Close()

	// Insert a project task directly
	_, err = db.Exec(`
		INSERT INTO project_task (repo_key, kind, name, command, description, discovered_ts)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "/test/repo", "makefile", "test", "make test", "Run tests", 1000000)
	require.NoError(t, err)

	scorer, err := NewScorer(ScorerDependencies{
		DB:               db,
		DiscoveryService: discSvc,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		RepoKey: "/test/repo",
		NowMs:   1000000,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)
	assert.Equal(t, "make test", suggestions[0].Command)
	assert.Contains(t, suggestions[0].Reasons, ReasonProjectTask)
}

func BenchmarkScorer_Suggest_Latency(b *testing.B) {
	db := createTestDB(b)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(b, err)
	b.Cleanup(func() { freqStore.Close() })

	transStore, err := score.NewTransitionStore(db)
	require.NoError(b, err)
	b.Cleanup(func() { transStore.Close() })

	ctx := context.Background()
	nowMs := time.Now().UnixMilli()

	// Populate frequency data
	for i := 0; i < 200; i++ {
		cmd := "git status"
		if i%3 == 0 {
			cmd = "npm install"
		}
		require.NoError(b, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
	}

	// Populate transition data
	for i := 0; i < 100; i++ {
		require.NoError(b, transStore.RecordTransition(ctx, score.ScopeGlobal, "git status", "git commit", nowMs))
	}

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, DefaultScorerConfig())
	require.NoError(b, err)

	suggestCtx := SuggestContext{
		LastCmd: "git status",
		NowMs:   nowMs,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := scorer.Suggest(ctx, suggestCtx)
		if err != nil {
			b.Fatalf("Suggest error: %v", err)
		}
	}

	b.StopTimer()
	avg := time.Duration(int64(b.Elapsed()) / int64(b.N))
	b.ReportMetric(float64(avg.Microseconds()), "us/op")
	if avg > 20*time.Millisecond {
		b.Fatalf("average suggest latency %v exceeds 20ms target", avg)
	}
}

func TestScorer_Suggest_Deduplication(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create stores
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	// Add same command via different sources
	ctx := context.Background()
	nowMs := int64(1000000)

	// Via frequency
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))

	// Via transition
	require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git add .", "git status", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastCmd: "git add .",
		NowMs:   nowMs,
	})
	require.NoError(t, err)

	// Should have git status only once but with multiple reasons
	gitStatusCount := 0
	var gitStatusSug *Suggestion
	for i := range suggestions {
		if suggestions[i].Command == "git status" {
			gitStatusCount++
			gitStatusSug = &suggestions[i]
		}
	}
	assert.Equal(t, 1, gitStatusCount)
	assert.NotNil(t, gitStatusSug)
	assert.GreaterOrEqual(t, len(gitStatusSug.Reasons), 2)
}

func TestScorer_Suggest_DangerousPenalty(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create frequency store
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	// Add dangerous command with high frequency
	ctx := context.Background()
	nowMs := int64(1000000)
	for i := 0; i < 10; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "rm -rf /", nowMs))
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "ls -la", nowMs))
	}

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)

	// Find the dangerous command
	var dangerousSug *Suggestion
	for i := range suggestions {
		if suggestions[i].Command == "rm -rf /" {
			dangerousSug = &suggestions[i]
			break
		}
	}

	// Should have dangerous tag
	if dangerousSug != nil {
		assert.Contains(t, dangerousSug.Reasons, ReasonDangerous)
	}

	// ls -la should rank higher than rm -rf / due to penalty
	if len(suggestions) >= 2 {
		// First suggestion should not be the dangerous one
		assert.NotEqual(t, "rm -rf /", suggestions[0].Command)
	}
}

func TestScorer_Suggest_RepoScope(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create frequency store
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add repo-specific commands
	repoKey := "/test/repo"
	require.NoError(t, freqStore.Update(ctx, repoKey, "npm test", nowMs))

	// Add global commands
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		RepoKey: repoKey,
		NowMs:   nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)

	// Both repo and global commands should appear
	commands := make([]string, len(suggestions))
	for i, s := range suggestions {
		commands[i] = s.Command
	}
	assert.Contains(t, commands, "npm test")
	assert.Contains(t, commands, "git status")
}

func TestScorer_Suggest_TopK(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create frequency store
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add many commands
	commands := []string{"cmd1", "cmd2", "cmd3", "cmd4", "cmd5", "cmd6", "cmd7", "cmd8", "cmd9", "cmd10"}
	for _, cmd := range commands {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs))
	}

	// Create scorer with TopK=3
	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, ScorerConfig{TopK: 3})
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)
	assert.Len(t, suggestions, 3)
}

func TestScorer_Confidence(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{DB: db}, DefaultScorerConfig())
	require.NoError(t, err)

	// Test confidence calculation
	sug := &Suggestion{
		Score: 100,
		scores: scoreInfo{
			repoTransition:   50,
			globalTransition: 30,
			repoFrequency:    20,
		},
	}

	confidence := scorer.calculateConfidence(sug)
	assert.Greater(t, confidence, 0.0)
	assert.LessOrEqual(t, confidence, 1.0)
}

func TestDefaultWeights(t *testing.T) {
	t.Parallel()

	weights := DefaultWeights()
	assert.Equal(t, float64(DefaultWeightRepoTransition), weights.RepoTransition)
	assert.Equal(t, float64(DefaultWeightGlobalTransition), weights.GlobalTransition)
	assert.Equal(t, float64(DefaultWeightRepoFrequency), weights.RepoFrequency)
	assert.Equal(t, float64(DefaultWeightProjectTask), weights.ProjectTask)
	assert.Equal(t, float64(DefaultWeightDangerous), weights.DangerousPenalty)
}

func TestDefaultScorerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultScorerConfig()
	assert.Equal(t, DefaultTopK, cfg.TopK)
	assert.NotNil(t, cfg.Logger)
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 80, DefaultWeightRepoTransition)
	assert.Equal(t, 60, DefaultWeightGlobalTransition)
	assert.Equal(t, 30, DefaultWeightRepoFrequency)
	assert.Equal(t, 20, DefaultWeightProjectTask)
	assert.Equal(t, -50, DefaultWeightDangerous)
	assert.Equal(t, 3, DefaultTopK)
	assert.Equal(t, 10, MaxTopK)
}
