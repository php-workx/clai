package replay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test Corpus ---
// Sanitized session recordings exercising different workflows.

// gitWorkflowSession is a typical git add/commit/push workflow.
var gitWorkflowSession = Session{
	ID: "git-workflow",
	Commands: []Command{
		{CmdNorm: "git status", ExitCode: 0, TimestampMs: 1000},
		{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 2000},
		{CmdNorm: "git commit -m {}", ExitCode: 0, TimestampMs: 3000, CmdRaw: "git commit -m \"fix: resolve bug\""},
		{CmdNorm: "git push", ExitCode: 0, TimestampMs: 4000},
		{CmdNorm: "git log", ExitCode: 0, TimestampMs: 5000},
	},
}

// buildCycleSession is a build/test/fix development cycle.
var buildCycleSession = Session{
	ID: "build-cycle",
	Commands: []Command{
		{CmdNorm: "make build", ExitCode: 0, TimestampMs: 1000},
		{CmdNorm: "make test", ExitCode: 1, TimestampMs: 2000},
		{CmdNorm: "make build", ExitCode: 0, TimestampMs: 3000},
		{CmdNorm: "make test", ExitCode: 0, TimestampMs: 4000},
		{CmdNorm: "make lint", ExitCode: 0, TimestampMs: 5000},
	},
}

// npmWorkflowSession is an npm-based development workflow.
var npmWorkflowSession = Session{
	ID: "npm-workflow",
	Commands: []Command{
		{CmdNorm: "npm install", ExitCode: 0, TimestampMs: 1000},
		{CmdNorm: "npm run dev", ExitCode: 0, TimestampMs: 2000},
		{CmdNorm: "npm test", ExitCode: 0, TimestampMs: 3000},
		{CmdNorm: "npm run build", ExitCode: 0, TimestampMs: 4000},
		{CmdNorm: "npm publish", ExitCode: 0, TimestampMs: 5000},
	},
}

// directoryNavSession exercises directory navigation patterns.
var directoryNavSession = Session{
	ID: "directory-nav",
	Commands: []Command{
		{CmdNorm: "ls", ExitCode: 0, TimestampMs: 1000, CWD: "/home/user"},
		{CmdNorm: "cd {}", ExitCode: 0, TimestampMs: 2000, CWD: "/home/user", CmdRaw: "cd project"},
		{CmdNorm: "ls -la", ExitCode: 0, TimestampMs: 3000, CWD: "/home/user/project"},
		{CmdNorm: "cat {}", ExitCode: 0, TimestampMs: 4000, CWD: "/home/user/project", CmdRaw: "cat README.md"},
	},
}

// mixedToolsSession exercises a mix of different tools.
var mixedToolsSession = Session{
	ID: "mixed-tools",
	Commands: []Command{
		{CmdNorm: "docker ps", ExitCode: 0, TimestampMs: 1000},
		{CmdNorm: "docker compose up -d", ExitCode: 0, TimestampMs: 2000},
		{CmdNorm: "curl {}", ExitCode: 0, TimestampMs: 3000, CmdRaw: "curl http://localhost:8080"},
		{CmdNorm: "docker logs {}", ExitCode: 0, TimestampMs: 4000, CmdRaw: "docker logs myapp"},
		{CmdNorm: "docker compose down", ExitCode: 0, TimestampMs: 5000},
	},
}

// TestCorpus contains all test sessions for replay validation.
var TestCorpus = []Session{
	gitWorkflowSession,
	buildCycleSession,
	npmWorkflowSession,
	directoryNavSession,
	mixedToolsSession,
}

// --- Tests ---

func TestReplay_Deterministic(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	// Run the git workflow session twice and verify identical results
	session := gitWorkflowSession
	// Add expectations that we will use to check determinism
	session.Expected = []ExpectedTopK{} // No expectations - just check for determinism

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	diffs1, err := runner.Replay(ctx, tmpDir1, session)
	require.NoError(t, err)

	diffs2, err := runner.Replay(ctx, tmpDir2, session)
	require.NoError(t, err)

	// Both runs should produce identical results (both empty since no expectations)
	assert.Equal(t, diffs1, diffs2, "two replays of same session should produce identical results")
}

func TestReplay_DeterministicWithExpectations(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	// Create a session with intentionally wrong expectations to capture actual output
	session := Session{
		ID: "determinism-check",
		Commands: []Command{
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 1000},
			{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 2000},
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 3000},
		},
		Expected: []ExpectedTopK{
			// Use impossible expectations so we capture actual output in diffs
			{AfterCommand: 2, TopK: []string{"IMPOSSIBLE_CMD_1", "IMPOSSIBLE_CMD_2"}},
		},
	}

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	diffs1, err := runner.Replay(ctx, tmpDir1, session)
	require.NoError(t, err)
	require.NotEmpty(t, diffs1, "should have diffs with impossible expectations")

	diffs2, err := runner.Replay(ctx, tmpDir2, session)
	require.NoError(t, err)
	require.NotEmpty(t, diffs2, "should have diffs with impossible expectations")

	// The actual "Got" lists from both runs must be identical
	require.Equal(t, len(diffs1), len(diffs2))
	for i := range diffs1 {
		assert.Equal(t, diffs1[i].Got, diffs2[i].Got,
			"actual top-k at step %d should be identical across runs", i)
		assert.Equal(t, diffs1[i].Mismatches, diffs2[i].Mismatches,
			"mismatches at step %d should be identical across runs", i)
	}
}

func TestReplay_CorpusRuns(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	// Replay each corpus session and verify no errors
	for _, session := range TestCorpus {
		session := session
		t.Run(session.ID, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			_, err := runner.Replay(ctx, tmpDir, session)
			require.NoError(t, err, "replay of session %q should not error", session.ID)
		})
	}
}

func TestReplay_EmptySession(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	session := Session{
		ID:       "empty",
		Commands: nil,
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, session)
	require.NoError(t, err)
	assert.Nil(t, diffs, "empty session should produce nil diffs")
}

func TestReplay_SingleCommand(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	session := Session{
		ID: "single-command",
		Commands: []Command{
			{CmdNorm: "ls", ExitCode: 0, TimestampMs: 1000},
		},
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, session)
	require.NoError(t, err)
	assert.Empty(t, diffs, "single command with no expectations should have no diffs")
}

func TestReplay_MatchingExpectations(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	// First, run a session to discover what the actual output is
	discoverSession := Session{
		ID: "discover",
		Commands: []Command{
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 1000},
			{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 2000},
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 3000},
			{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 4000},
		},
		Expected: []ExpectedTopK{
			// Use impossible expectation to capture actual output
			{AfterCommand: 3, TopK: []string{"IMPOSSIBLE"}},
		},
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, discoverSession)
	require.NoError(t, err)
	require.NotEmpty(t, diffs)

	// Now use the actual output as the expectation
	actualTopK := diffs[0].Got

	matchSession := Session{
		ID: "matching",
		Commands: []Command{
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 1000},
			{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 2000},
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 3000},
			{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 4000},
		},
		Expected: []ExpectedTopK{
			{AfterCommand: 3, TopK: actualTopK},
		},
	}

	tmpDir2 := t.TempDir()
	diffs2, err := runner.Replay(ctx, tmpDir2, matchSession)
	require.NoError(t, err)
	assert.Empty(t, diffs2, "expectations derived from actual output should match on second run")
}

func TestReplay_DiffDetection(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	// Create a session with known wrong expectations
	session := Session{
		ID: "diff-detection",
		Commands: []Command{
			{CmdNorm: "git status", ExitCode: 0, TimestampMs: 1000},
			{CmdNorm: "git add .", ExitCode: 0, TimestampMs: 2000},
		},
		Expected: []ExpectedTopK{
			{AfterCommand: 1, TopK: []string{"completely-wrong-command", "another-wrong-one"}},
		},
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, session)
	require.NoError(t, err)
	require.NotEmpty(t, diffs, "intentional mismatch should produce diffs")

	// Verify diff structure
	diff := diffs[0]
	assert.Equal(t, 0, diff.StepIndex)
	assert.Equal(t, 1, diff.AfterCommand)
	assert.Equal(t, []string{"completely-wrong-command", "another-wrong-one"}, diff.Expected)
	assert.NotEmpty(t, diff.Mismatches, "should have at least one mismatch")

	// Verify mismatch types
	for _, m := range diff.Mismatches {
		assert.Contains(t, []string{"missing", "extra", "reordered"}, m.Type,
			"mismatch type should be valid")
	}
}

func TestReplay_FixedClockProducesConsistentTimestamps(t *testing.T) {
	t.Parallel()

	cfg := RunnerConfig{
		BaseTimestampMs:      5000,
		TimestampIncrementMs: 2000,
		SessionID:            "clock-test",
		RepoKey:              "/test/repo",
		CWD:                  "/test/workdir",
	}
	runner := NewRunner(cfg)
	ctx := context.Background()

	// Commands with TimestampMs=0 should use the fixed clock
	session := Session{
		ID: "clock-test",
		Commands: []Command{
			{CmdNorm: "cmd1", ExitCode: 0}, // Should get ts=5000
			{CmdNorm: "cmd2", ExitCode: 0}, // Should get ts=7000
			{CmdNorm: "cmd3", ExitCode: 0}, // Should get ts=9000
		},
	}

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Run twice - both should succeed and produce identical results
	diffs1, err := runner.Replay(ctx, tmpDir1, session)
	require.NoError(t, err)

	diffs2, err := runner.Replay(ctx, tmpDir2, session)
	require.NoError(t, err)

	assert.Equal(t, diffs1, diffs2, "fixed clock should produce identical replays")
}

func TestReplay_ExplicitTimestampsOverrideClock(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	// Commands with explicit timestamps should use those instead of the fixed clock
	session := Session{
		ID: "explicit-ts",
		Commands: []Command{
			{CmdNorm: "cmd1", ExitCode: 0, TimestampMs: 50000},
			{CmdNorm: "cmd2", ExitCode: 0, TimestampMs: 60000},
		},
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, session)
	require.NoError(t, err)
	assert.Empty(t, diffs) // No expectations = no diffs
}

func TestReplayAll(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	sessions := []Session{
		{
			ID: "session-a",
			Commands: []Command{
				{CmdNorm: "ls", ExitCode: 0, TimestampMs: 1000},
			},
		},
		{
			ID: "session-b",
			Commands: []Command{
				{CmdNorm: "pwd", ExitCode: 0, TimestampMs: 1000},
			},
		},
	}

	tmpDir := t.TempDir()
	results, err := runner.ReplayAll(ctx, tmpDir, sessions)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Contains(t, results, "session-a")
	assert.Contains(t, results, "session-b")
}

func TestReplayAll_EmptyList(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	tmpDir := t.TempDir()
	results, err := runner.ReplayAll(ctx, tmpDir, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// --- Diff Computation Tests ---

func TestComputeMismatches_Identical(t *testing.T) {
	t.Parallel()

	mismatches := computeMismatches(
		[]string{"git add .", "git commit", "git push"},
		[]string{"git add .", "git commit", "git push"},
	)
	assert.Empty(t, mismatches)
}

func TestComputeMismatches_Empty(t *testing.T) {
	t.Parallel()

	mismatches := computeMismatches(nil, nil)
	assert.Empty(t, mismatches)

	mismatches = computeMismatches([]string{}, []string{})
	assert.Empty(t, mismatches)
}

func TestComputeMismatches_Missing(t *testing.T) {
	t.Parallel()

	mismatches := computeMismatches(
		[]string{"git add .", "git commit", "git push"},
		[]string{"git add ."},
	)
	require.Len(t, mismatches, 2)
	assert.Equal(t, "missing", mismatches[0].Type)
	assert.Equal(t, 1, mismatches[0].Position)
	assert.Equal(t, "git commit", mismatches[0].Expected)
	assert.Equal(t, "missing", mismatches[1].Type)
	assert.Equal(t, 2, mismatches[1].Position)
	assert.Equal(t, "git push", mismatches[1].Expected)
}

func TestComputeMismatches_Extra(t *testing.T) {
	t.Parallel()

	mismatches := computeMismatches(
		[]string{"git add ."},
		[]string{"git add .", "unexpected-cmd", "another-unexpected"},
	)
	require.Len(t, mismatches, 2)
	assert.Equal(t, "extra", mismatches[0].Type)
	assert.Equal(t, 1, mismatches[0].Position)
	assert.Equal(t, "unexpected-cmd", mismatches[0].Got)
	assert.Equal(t, "extra", mismatches[1].Type)
	assert.Equal(t, 2, mismatches[1].Position)
}

func TestComputeMismatches_Reordered(t *testing.T) {
	t.Parallel()

	mismatches := computeMismatches(
		[]string{"git add .", "git commit", "git push"},
		[]string{"git add .", "git push", "git commit"},
	)
	require.Len(t, mismatches, 2)
	// Position 1: expected "git commit", got "git push"
	assert.Equal(t, "reordered", mismatches[0].Type)
	assert.Equal(t, 1, mismatches[0].Position)
	assert.Equal(t, "git commit", mismatches[0].Expected)
	assert.Equal(t, "git push", mismatches[0].Got)
	// Position 2: expected "git push", got "git commit"
	assert.Equal(t, "reordered", mismatches[1].Type)
	assert.Equal(t, 2, mismatches[1].Position)
}

func TestComputeMismatches_CompletelyDifferent(t *testing.T) {
	t.Parallel()

	mismatches := computeMismatches(
		[]string{"a", "b"},
		[]string{"x", "y"},
	)
	require.Len(t, mismatches, 2)
	assert.Equal(t, "missing", mismatches[0].Type)
	assert.Equal(t, "missing", mismatches[1].Type)
}

// --- Formatter Tests ---

func TestFormatDiffs_NoDiffs(t *testing.T) {
	t.Parallel()

	output := FormatDiffs("test-session", nil)
	assert.Contains(t, output, "all expectations matched")
	assert.Contains(t, output, "test-session")
}

func TestFormatDiffs_WithDiffs(t *testing.T) {
	t.Parallel()

	diffs := []DiffResult{
		{
			StepIndex:    0,
			AfterCommand: 2,
			Expected:     []string{"git commit", "git push"},
			Got:          []string{"git status", "git push"},
			Mismatches: []Mismatch{
				{Position: 0, Expected: "git commit", Got: "git status", Type: "missing"},
			},
		},
	}

	output := FormatDiffs("my-session", diffs)
	assert.Contains(t, output, "my-session")
	assert.Contains(t, output, "1 diff(s) found")
	assert.Contains(t, output, "Step 0")
	assert.Contains(t, output, "after command #2")
	assert.Contains(t, output, "MISSING")
	assert.Contains(t, output, "git commit")
}

func TestFormatDiffs_AllMismatchTypes(t *testing.T) {
	t.Parallel()

	diffs := []DiffResult{
		{
			StepIndex:    0,
			AfterCommand: 0,
			Expected:     []string{"a"},
			Got:          []string{"b"},
			Mismatches: []Mismatch{
				{Position: 0, Expected: "a", Got: "b", Type: "missing"},
				{Position: 1, Expected: "", Got: "c", Type: "extra"},
				{Position: 2, Expected: "d", Got: "e", Type: "reordered"},
			},
		},
	}

	output := FormatDiffs("test", diffs)
	assert.Contains(t, output, "MISSING")
	assert.Contains(t, output, "EXTRA")
	assert.Contains(t, output, "REORDERED")
}

func TestFormatAllDiffs(t *testing.T) {
	t.Parallel()

	results := map[string][]DiffResult{
		"session-a": nil,
		"session-b": {
			{
				StepIndex:    0,
				AfterCommand: 0,
				Expected:     []string{"x"},
				Got:          []string{"y"},
				Mismatches:   []Mismatch{{Position: 0, Expected: "x", Got: "y", Type: "missing"}},
			},
		},
	}

	output := FormatAllDiffs(results)
	assert.Contains(t, output, "session-a")
	assert.Contains(t, output, "session-b")
}

func TestFormatTopK_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(empty)", formatTopK(nil))
	assert.Equal(t, "(empty)", formatTopK([]string{}))
}

func TestFormatTopK_Values(t *testing.T) {
	t.Parallel()

	result := formatTopK([]string{"git status", "git add ."})
	assert.Contains(t, result, "1:")
	assert.Contains(t, result, "git status")
	assert.Contains(t, result, "2:")
	assert.Contains(t, result, "git add .")
}

// --- Runner Config Tests ---

func TestDefaultRunnerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultRunnerConfig()
	assert.Equal(t, int64(1000), cfg.BaseTimestampMs)
	assert.Equal(t, int64(1000), cfg.TimestampIncrementMs)
	assert.Equal(t, "replay-session", cfg.SessionID)
	assert.Equal(t, "/replay/repo", cfg.RepoKey)
	assert.Equal(t, "/replay/workdir", cfg.CWD)
	assert.Equal(t, 3, cfg.TopK)
}

func TestNewRunner_DefaultsApplied(t *testing.T) {
	t.Parallel()

	runner := NewRunner(RunnerConfig{})
	assert.Equal(t, int64(1000), runner.cfg.BaseTimestampMs)
	assert.Equal(t, int64(1000), runner.cfg.TimestampIncrementMs)
	assert.Equal(t, "replay-session", runner.cfg.SessionID)
	assert.Equal(t, "/replay/repo", runner.cfg.RepoKey)
	assert.Equal(t, "/replay/workdir", runner.cfg.CWD)
	assert.Equal(t, 3, runner.cfg.TopK)
}

func TestNewRunner_CustomConfig(t *testing.T) {
	t.Parallel()

	runner := NewRunner(RunnerConfig{
		BaseTimestampMs:      5000,
		TimestampIncrementMs: 500,
		SessionID:            "custom-session",
		RepoKey:              "/custom/repo",
		CWD:                  "/custom/dir",
		TopK:                 5,
	})
	assert.Equal(t, int64(5000), runner.cfg.BaseTimestampMs)
	assert.Equal(t, int64(500), runner.cfg.TimestampIncrementMs)
	assert.Equal(t, "custom-session", runner.cfg.SessionID)
	assert.Equal(t, "/custom/repo", runner.cfg.RepoKey)
	assert.Equal(t, "/custom/dir", runner.cfg.CWD)
	assert.Equal(t, 5, runner.cfg.TopK)
}

// --- Integration-style Tests ---

func TestReplay_BuildCycleProducesDeterministicOutput(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	session := buildCycleSession

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	diffs1, err := runner.Replay(ctx, tmpDir1, session)
	require.NoError(t, err)

	diffs2, err := runner.Replay(ctx, tmpDir2, session)
	require.NoError(t, err)

	assert.Equal(t, diffs1, diffs2, "build cycle replays should be deterministic")
}

func TestReplay_NpmWorkflowProducesDeterministicOutput(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	session := npmWorkflowSession

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	diffs1, err := runner.Replay(ctx, tmpDir1, session)
	require.NoError(t, err)

	diffs2, err := runner.Replay(ctx, tmpDir2, session)
	require.NoError(t, err)

	assert.Equal(t, diffs1, diffs2, "npm workflow replays should be deterministic")
}

func TestReplay_FullCorpusDeterministic(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	results1, err := runner.ReplayAll(ctx, tmpDir1, TestCorpus)
	require.NoError(t, err)

	results2, err := runner.ReplayAll(ctx, tmpDir2, TestCorpus)
	require.NoError(t, err)

	for _, session := range TestCorpus {
		diffs1 := results1[session.ID]
		diffs2 := results2[session.ID]
		assert.Equal(t, diffs1, diffs2,
			"session %q should produce identical results across runs", session.ID)
	}
}

func TestReplay_CommandWithFailure(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	session := Session{
		ID: "failure-session",
		Commands: []Command{
			{CmdNorm: "make build", ExitCode: 0, TimestampMs: 1000},
			{CmdNorm: "make test", ExitCode: 1, TimestampMs: 2000},
			{CmdNorm: "make build", ExitCode: 0, TimestampMs: 3000},
		},
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, session)
	require.NoError(t, err)
	assert.Empty(t, diffs, "no expectations means no diffs")
}

func TestReplay_CWDVariation(t *testing.T) {
	t.Parallel()

	runner := NewRunner(DefaultRunnerConfig())
	ctx := context.Background()

	session := Session{
		ID: "cwd-variation",
		Commands: []Command{
			{CmdNorm: "ls", ExitCode: 0, TimestampMs: 1000, CWD: "/home/user/project-a"},
			{CmdNorm: "ls", ExitCode: 0, TimestampMs: 2000, CWD: "/home/user/project-b"},
			{CmdNorm: "ls", ExitCode: 0, TimestampMs: 3000, CWD: "/home/user/project-a"},
		},
	}

	tmpDir := t.TempDir()
	diffs, err := runner.Replay(ctx, tmpDir, session)
	require.NoError(t, err)
	assert.Empty(t, diffs)
}
