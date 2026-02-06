package suggest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/recovery"
	"github.com/runger/clai/internal/suggestions/score"
	"github.com/runger/clai/internal/suggestions/workflow"

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

		-- Pipeline transition table
		CREATE TABLE pipeline_transition (
			scope             TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			operator          TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_template_id, next_template_id, operator)
		);

		-- Command template table
		CREATE TABLE command_template (
			template_id     TEXT PRIMARY KEY,
			cmd_norm        TEXT NOT NULL,
			tags            TEXT,
			slot_count      INTEGER NOT NULL,
			first_seen_ms   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL
		);

		-- Failure recovery table
		CREATE TABLE failure_recovery (
			scope                 TEXT NOT NULL,
			failed_template_id    TEXT NOT NULL,
			exit_code_class       TEXT NOT NULL,
			recovery_template_id  TEXT NOT NULL,
			weight                REAL NOT NULL,
			count                 INTEGER NOT NULL,
			success_rate          REAL NOT NULL,
			last_seen_ms          INTEGER NOT NULL,
			source                TEXT NOT NULL DEFAULT 'learned',
			PRIMARY KEY(scope, failed_template_id, exit_code_class, recovery_template_id)
		);

		-- Dismissal pattern table
		CREATE TABLE dismissal_pattern (
			scope                   TEXT NOT NULL,
			context_template_id     TEXT NOT NULL,
			dismissed_template_id   TEXT NOT NULL,
			dismissal_count         INTEGER NOT NULL,
			last_dismissed_ms       INTEGER NOT NULL,
			suppression_level       TEXT NOT NULL,
			PRIMARY KEY(scope, context_template_id, dismissed_template_id)
		);

		-- Suggestion feedback table
		CREATE TABLE suggestion_feedback (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT NOT NULL,
			ts_ms           INTEGER NOT NULL,
			prompt_prefix   TEXT,
			suggested_text  TEXT NOT NULL,
			action          TEXT NOT NULL,
			executed_text   TEXT,
			latency_ms      INTEGER
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

func TestScorer_Suggest_WithDirScope(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Create stores
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)
	dirKey := "dir:testscope123"

	// Add dir-scoped frequency
	require.NoError(t, freqStore.Update(ctx, dirKey, "npm run dev", nowMs))

	// Add dir-scoped transition
	require.NoError(t, transStore.RecordTransition(ctx, dirKey, "git pull", "npm run dev", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastCmd:     "git pull",
		DirScopeKey: dirKey,
		NowMs:       nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)

	// npm run dev should appear with dir scope reasons
	var found *Suggestion
	for i := range suggestions {
		if suggestions[i].Command == "npm run dev" {
			found = &suggestions[i]
			break
		}
	}
	assert.NotNil(t, found, "npm run dev should be suggested")
	if found != nil {
		assert.Contains(t, found.Reasons, ReasonDirTransition)
		assert.Contains(t, found.Reasons, ReasonDirFrequency)
	}
}

func TestScorer_Suggest_DirScopeEmpty(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add global frequency only
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "ls -la", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// No DirScopeKey set - should still work
	suggestions, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)
	assert.Equal(t, "ls -la", suggestions[0].Command)
}

func TestDefaultWeights(t *testing.T) {
	t.Parallel()

	weights := DefaultWeights()
	assert.Equal(t, float64(DefaultWeightRepoTransition), weights.RepoTransition)
	assert.Equal(t, float64(DefaultWeightGlobalTransition), weights.GlobalTransition)
	assert.Equal(t, float64(DefaultWeightRepoFrequency), weights.RepoFrequency)
	assert.Equal(t, float64(DefaultWeightProjectTask), weights.ProjectTask)
	assert.Equal(t, float64(DefaultWeightDangerous), weights.DangerousPenalty)
	assert.Equal(t, float64(DefaultWeightDirTransition), weights.DirTransition)
	assert.Equal(t, float64(DefaultWeightDirFrequency), weights.DirFrequency)
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
	assert.Equal(t, 90, DefaultWeightDirTransition)
	assert.Equal(t, 40, DefaultWeightDirFrequency)
	assert.Equal(t, 3, DefaultTopK)
	assert.Equal(t, 10, MaxTopK)
}

func TestAmplifierConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1.5, DefaultWorkflowBoostFactor)
	assert.Equal(t, 0.3, DefaultDismissalPenalty)
	assert.Equal(t, 0.0, DefaultPermanentPenalty)
	assert.Equal(t, 2.0, DefaultRecoveryBoostFactor)
	assert.Equal(t, 50.0, DefaultPipelineConfWeight)
	assert.Equal(t, int64(7*24*60*60*1000), int64(DefaultRecencyDecayTauMs))
	assert.Equal(t, 1.3, DefaultPlaybookBoostFactor)
}

func TestDefaultAmplifierConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultAmplifierConfig()
	assert.Equal(t, DefaultWorkflowBoostFactor, cfg.WorkflowBoostFactor)
	assert.Equal(t, DefaultDismissalPenalty, cfg.DismissalPenaltyFactor)
	assert.Equal(t, DefaultPermanentPenalty, cfg.PermanentPenaltyFactor)
	assert.Equal(t, DefaultRecoveryBoostFactor, cfg.RecoveryBoostFactor)
	assert.Equal(t, DefaultPipelineConfWeight, cfg.PipelineConfidenceWeight)
	assert.Equal(t, int64(DefaultRecencyDecayTauMs), cfg.RecencyDecayTauMs)
	assert.Equal(t, DefaultPlaybookBoostFactor, cfg.PlaybookBoostFactor)
}

func TestReasonConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "workflow_boost", ReasonWorkflowBoost)
	assert.Equal(t, "pipeline_conf", ReasonPipelineConf)
	assert.Equal(t, "dismissal_penalty", ReasonDismissalPenalty)
	assert.Equal(t, "recovery_boost", ReasonRecoveryBoost)
}

func TestScorer_DefaultScorerConfig_HasAmplifiers(t *testing.T) {
	t.Parallel()

	cfg := DefaultScorerConfig()
	assert.Equal(t, DefaultWorkflowBoostFactor, cfg.Amplifiers.WorkflowBoostFactor)
	assert.Equal(t, DefaultRecoveryBoostFactor, cfg.Amplifiers.RecoveryBoostFactor)
	assert.Equal(t, DefaultDismissalPenalty, cfg.Amplifiers.DismissalPenaltyFactor)
}

func TestEditDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "ab", 1},
		{"abc", "abcd", 1},
		{"kitten", "sitting", 3},
		{"git", "gti", 2},
		{"git", "git", 0},
	}

	for _, tt := range tests {
		got := editDistance(tt.a, tt.b)
		assert.Equal(t, tt.want, got, "editDistance(%q, %q)", tt.a, tt.b)
	}
}

func TestSuppressNearDuplicates(t *testing.T) {
	t.Parallel()

	suggestions := []Suggestion{
		{Command: "git commit -m foo", TemplateID: "tmpl:git_commit", Score: 100},
		{Command: "git commit -m bar", TemplateID: "tmpl:git_commit", Score: 80},
		{Command: "git push", TemplateID: "tmpl:git_push", Score: 90},
		{Command: "git status", TemplateID: "", Score: 50},  // No template ID, always kept
		{Command: "git status2", TemplateID: "", Score: 40}, // No template ID, always kept
	}

	result := suppressNearDuplicates(suggestions)

	// Should keep highest-scored git commit variant
	assert.Len(t, result, 4)
	var commitSug *Suggestion
	for i := range result {
		if result[i].TemplateID == "tmpl:git_commit" {
			commitSug = &result[i]
			break
		}
	}
	require.NotNil(t, commitSug)
	assert.Equal(t, float64(100), commitSug.Score)
	assert.Equal(t, "git commit -m foo", commitSug.Command)
}

func TestSuppressNearDuplicates_Empty(t *testing.T) {
	t.Parallel()

	result := suppressNearDuplicates(nil)
	assert.Nil(t, result)

	result = suppressNearDuplicates([]Suggestion{})
	assert.Empty(t, result)
}

func TestScorer_WorkflowBoost(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add some commands to frequency
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git commit", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git push", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "npm test", nowMs))

	// Create workflow tracker with a pattern: git add -> git commit -> git push
	patterns := []workflow.Pattern{
		{
			PatternID:    "wf:git-flow",
			TemplateIDs:  []string{"tmpl:git_add", "tmpl:git_commit", "tmpl:git_push"},
			DisplayNames: []string{"git add .", "git commit", "git push"},
			Scope:        "global",
			StepCount:    3,
		},
	}
	tracker := workflow.NewTracker(patterns, workflow.DefaultTrackerConfig())

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		WorkflowTracker: tracker,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// Simulate being at step 0 (just ran "git add .") by using the template ID
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastCmd:        "git add .",
		LastTemplateID: "tmpl:git_add",
		NowMs:          nowMs,
	})
	require.NoError(t, err)

	// "git commit" should be boosted due to workflow
	var found bool
	for _, sug := range suggestions {
		if sug.Command == "git commit" {
			found = true
			assert.Contains(t, sug.Reasons, ReasonWorkflowBoost)
			break
		}
	}
	assert.True(t, found, "git commit should appear with workflow boost")
}

func TestScorer_WorkflowBoost_NilTracker(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))

	// No workflow tracker provided
	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:git_add",
		NowMs:          nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions) // Should still work without tracker
}

func TestScorer_PipelineConfidence(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Insert pipeline transition data
	_, err := db.Exec(`
		INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "global", "tmpl:grep", "tmpl:sort", "|", 0.8, 10, 1000000)
	require.NoError(t, err)

	// Insert the command template so the join resolves
	_, err = db.Exec(`
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "tmpl:sort", "sort", "", 0, 1000000, 1000000)
	require.NoError(t, err)

	pipelineStore := score.NewPipelineStore(db)

	scorer, err := NewScorer(ScorerDependencies{
		DB:            db,
		PipelineStore: pipelineStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:grep",
		Scope:          "global",
		NowMs:          1000000,
	})
	require.NoError(t, err)

	// Should include "sort" with pipeline_conf reason
	var found bool
	for _, sug := range suggestions {
		if sug.Command == "sort" {
			found = true
			assert.Contains(t, sug.Reasons, ReasonPipelineConf)
			break
		}
	}
	assert.True(t, found, "sort should appear from pipeline confidence")
}

func TestScorer_PipelineConfidence_NilStore(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{
		DB: db,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:grep",
		Scope:          "global",
		NowMs:          1000000,
	})
	require.NoError(t, err)
	assert.Empty(t, suggestions)
}

func TestScorer_RecoveryBoost(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Insert a failure recovery pattern
	_, err := db.Exec(`
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "global", "tmpl:npm_install", "class:general", "bootstrap:npm cache clean --force", 1.0, 5, 0.8, 1000000, "bootstrap")
	require.NoError(t, err)

	// Insert command template for the recovery
	_, err = db.Exec(`
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "bootstrap:npm cache clean --force", "npm cache clean --force", "", 0, 1000000, 1000000)
	require.NoError(t, err)

	classifier := recovery.NewClassifier(nil)
	safety := recovery.NewSafetyGate(recovery.DefaultSafetyConfig())
	recoveryEngine, err := recovery.NewEngine(db, classifier, safety, recovery.DefaultEngineConfig())
	require.NoError(t, err)

	scorer, err := NewScorer(ScorerDependencies{
		DB:             db,
		RecoveryEngine: recoveryEngine,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:npm_install",
		LastExitCode:   1,
		LastFailed:     true,
		Scope:          "global",
		NowMs:          1000000,
	})
	require.NoError(t, err)

	// Should include recovery suggestion with recovery_boost reason
	var found bool
	for _, sug := range suggestions {
		if sug.Command == "npm cache clean --force" {
			found = true
			assert.Contains(t, sug.Reasons, ReasonRecoveryBoost)
			assert.Greater(t, sug.Score, 0.0)
			break
		}
	}
	assert.True(t, found, "npm cache clean --force should appear as recovery suggestion")
}

func TestScorer_RecoveryBoost_NotTriggeredOnSuccess(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	classifier := recovery.NewClassifier(nil)
	safety := recovery.NewSafetyGate(recovery.DefaultSafetyConfig())
	recoveryEngine, err := recovery.NewEngine(db, classifier, safety, recovery.DefaultEngineConfig())
	require.NoError(t, err)

	scorer, err := NewScorer(ScorerDependencies{
		DB:             db,
		RecoveryEngine: recoveryEngine,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:npm_install",
		LastExitCode:   0,
		LastFailed:     false, // Not failed
		Scope:          "global",
		NowMs:          1000000,
	})
	require.NoError(t, err)
	assert.Empty(t, suggestions)
}

func TestScorer_RecoveryBoost_NilEngine(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{
		DB: db,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	ctx := context.Background()
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastFailed: true,
		NowMs:      1000000,
	})
	require.NoError(t, err)
	assert.Empty(t, suggestions) // No crash with nil engine
}

func TestScorer_DismissalPenalty_Learned(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add two commands with equal frequency
	for i := 0; i < 5; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git log", nowMs))
	}

	// Create dismissal store and record learned dismissal for "git status"
	dismissalStore := dismissal.NewStore(db, dismissal.DefaultConfig(), nil)

	// Record enough dismissals to reach LEARNED state (threshold = 3)
	for i := 0; i < 4; i++ {
		require.NoError(t, dismissalStore.RecordDismissal(ctx, "global", "tmpl:prev_cmd", "git status", nowMs))
	}

	// Verify state is learned
	state, err := dismissalStore.GetState(ctx, "global", "tmpl:prev_cmd", "git status")
	require.NoError(t, err)
	assert.Equal(t, dismissal.StateLearned, state)

	scorer, err := NewScorer(ScorerDependencies{
		DB:             db,
		FreqStore:      freqStore,
		DismissalStore: dismissalStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:prev_cmd",
		Scope:          "global",
		NowMs:          nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)

	// Find both suggestions and check scores
	var statusScore, logScore float64
	for _, sug := range suggestions {
		if sug.Command == "git status" {
			statusScore = sug.Score
		}
		if sug.Command == "git log" {
			logScore = sug.Score
		}
	}

	// git status should have lower score due to dismissal penalty
	if statusScore > 0 && logScore > 0 {
		assert.Less(t, statusScore, logScore, "dismissed suggestion should have lower score")
	}
}

func TestScorer_DismissalPenalty_Permanent(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	for i := 0; i < 5; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
	}

	// Create permanent dismissal (never feedback)
	dismissalStore := dismissal.NewStore(db, dismissal.DefaultConfig(), nil)
	require.NoError(t, dismissalStore.RecordNever(ctx, "global", "tmpl:prev_cmd", "git status", nowMs))

	state, err := dismissalStore.GetState(ctx, "global", "tmpl:prev_cmd", "git status")
	require.NoError(t, err)
	assert.Equal(t, dismissal.StatePermanent, state)

	scorer, err := NewScorer(ScorerDependencies{
		DB:             db,
		FreqStore:      freqStore,
		DismissalStore: dismissalStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:prev_cmd",
		Scope:          "global",
		NowMs:          nowMs,
	})
	require.NoError(t, err)

	// "git status" should have zero score (permanent penalty factor = 0.0)
	for _, sug := range suggestions {
		if sug.Command == "git status" {
			assert.Equal(t, 0.0, sug.Score, "permanent dismissal should zero the score")
			assert.Contains(t, sug.Reasons, ReasonDismissalPenalty)
		}
	}
}

func TestScorer_DismissalPenalty_NilStore(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))

	// No dismissal store
	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastTemplateID: "tmpl:prev_cmd",
		Scope:          "global",
		NowMs:          nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions) // Should still work without dismissal store
}

func TestScorer_PrefixFiltering_ExactMatch(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git commit", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "npm install", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		Prefix: "git",
		NowMs:  nowMs,
	})
	require.NoError(t, err)

	// Only git commands should be included
	for _, sug := range suggestions {
		assert.True(t, strings.HasPrefix(strings.ToLower(sug.Command), "git"),
			"suggestion %q should start with 'git'", sug.Command)
	}
}

func TestScorer_PrefixFiltering_EmptyPrefix(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "npm install", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// Empty prefix: all suggestions should be returned
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		Prefix: "",
		NowMs:  nowMs,
	})
	require.NoError(t, err)
	assert.Len(t, suggestions, 2)
}

func TestScorer_PrefixFiltering_FuzzyTolerance(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git status", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "npm test", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// "gti" has edit distance 2 from "git" (swap), but "git" prefix on "gti" is 3 chars
	// With our current fuzzy logic: editDistance("gti", "git") = 2 which exceeds 1
	// But the first word match: "git" HasPrefix "gti" -> false
	// So "gti" should NOT match "git status" via basic fuzzy, unless the typo tolerance is
	// within range. Let's test with a single-char typo instead.
	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		Prefix: "gis", // edit distance 1 from "git"
		NowMs:  nowMs,
	})
	require.NoError(t, err)

	// "gis" vs "git" = edit distance 1 (within tolerance for len <= 5)
	var hasGitCmd bool
	for _, sug := range suggestions {
		if strings.HasPrefix(strings.ToLower(sug.Command), "git") {
			hasGitCmd = true
			break
		}
	}
	assert.True(t, hasGitCmd, "fuzzy tolerance should match 'gis' to 'git' commands")
}

func TestScorer_DeterministicTieBreaking(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add commands with same frequency to get same score
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "alpha-cmd", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "beta-cmd", nowMs))
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "gamma-cmd", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// Run twice to verify deterministic ordering
	suggestions1, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)
	suggestions2, err := scorer.Suggest(ctx, SuggestContext{NowMs: nowMs})
	require.NoError(t, err)

	require.Equal(t, len(suggestions1), len(suggestions2))
	for i := range suggestions1 {
		assert.Equal(t, suggestions1[i].Command, suggestions2[i].Command,
			"deterministic ordering: position %d should match", i)
	}
}

func TestScorer_MaxTopK_Clamped(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{DB: db}, ScorerConfig{TopK: 999})
	require.NoError(t, err)
	assert.Equal(t, MaxTopK, scorer.TopK())
}

func TestScorer_SuggestContext_Defaults(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	ctx := context.Background()
	nowMs := time.Now().UnixMilli()
	require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "ls", nowMs))

	scorer, err := NewScorer(ScorerDependencies{
		DB:        db,
		FreqStore: freqStore,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	// With empty NowMs and Scope, defaults should be applied
	suggestions, err := scorer.Suggest(ctx, SuggestContext{})
	require.NoError(t, err)
	// Should still work with defaulted NowMs and Scope
	assert.NotEmpty(t, suggestions)
}

func TestScorer_ConfidenceWithNewFeatures(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	scorer, err := NewScorer(ScorerDependencies{DB: db}, DefaultScorerConfig())
	require.NoError(t, err)

	// Test confidence with all 10 features active
	sug := &Suggestion{
		Score: 200,
		scores: scoreInfo{
			repoTransition:   50,
			globalTransition: 30,
			repoFrequency:    20,
			globalFrequency:  10,
			projectTask:      10,
			dirTransition:    30,
			dirFrequency:     20,
			workflowBoost:    15,
			pipelineConf:     10,
			recoveryBoost:    5,
		},
	}

	confidence := scorer.calculateConfidence(sug)
	assert.Greater(t, confidence, 0.5, "high multi-source suggestion should have high confidence")
	assert.LessOrEqual(t, confidence, 1.0)

	// Test confidence with dismissal penalty (negative) - should not count as source
	sugWithPenalty := &Suggestion{
		Score: 50,
		scores: scoreInfo{
			repoTransition:   50,
			dismissalPenalty: -20, // Negative: not a contributing source
		},
	}

	confWithPenalty := scorer.calculateConfidence(sugWithPenalty)
	assert.Greater(t, confWithPenalty, 0.0)
	assert.LessOrEqual(t, confWithPenalty, 1.0)
}

func TestScorer_Suggest_CombinedFeatures(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Set up all stores
	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(t, err)
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(db)
	require.NoError(t, err)
	defer transStore.Close()

	pipelineStore := score.NewPipelineStore(db)

	ctx := context.Background()
	nowMs := int64(1000000)

	// Frequency data
	for i := 0; i < 5; i++ {
		require.NoError(t, freqStore.Update(ctx, score.ScopeGlobal, "git commit", nowMs))
	}

	// Transition data
	for i := 0; i < 3; i++ {
		require.NoError(t, transStore.RecordTransition(ctx, score.ScopeGlobal, "git add .", "git commit", nowMs))
	}

	// Pipeline data
	_, err = db.Exec(`
		INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "global", "tmpl:git_add", "tmpl:git_commit", "&&", 0.9, 5, nowMs)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "tmpl:git_commit", "git commit", "", 0, nowMs, nowMs)
	require.NoError(t, err)

	// Workflow tracker
	patterns := []workflow.Pattern{
		{
			PatternID:    "wf:git-flow",
			TemplateIDs:  []string{"tmpl:git_add", "tmpl:git_commit"},
			DisplayNames: []string{"git add .", "git commit"},
			Scope:        "global",
			StepCount:    2,
		},
	}
	tracker := workflow.NewTracker(patterns, workflow.DefaultTrackerConfig())

	scorer, err := NewScorer(ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
		PipelineStore:   pipelineStore,
		WorkflowTracker: tracker,
	}, DefaultScorerConfig())
	require.NoError(t, err)

	suggestions, err := scorer.Suggest(ctx, SuggestContext{
		LastCmd:        "git add .",
		LastTemplateID: "tmpl:git_add",
		Scope:          "global",
		NowMs:          nowMs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, suggestions)

	// "git commit" should be the top suggestion with multiple reason sources
	assert.Equal(t, "git commit", suggestions[0].Command)
	reasons := suggestions[0].Reasons
	assert.GreaterOrEqual(t, len(reasons), 2, "should have multiple contributing reasons")
}
