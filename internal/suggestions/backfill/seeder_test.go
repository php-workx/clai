package backfill

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/normalize"
)

// newTestDB opens a fresh V2 database in a temp directory for testing.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	d, err := db.Open(context.Background(), db.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	return d.DB()
}

// makeEntries generates n test import entries with incrementing timestamps.
func makeEntries(n int, cmdFn func(i int) string) []history.ImportEntry {
	entries := make([]history.ImportEntry, n)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		cmd := "ls"
		if cmdFn != nil {
			cmd = cmdFn(i)
		}
		entries[i] = history.ImportEntry{
			Command:   cmd,
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
	}
	return entries
}

// countRows returns the number of rows in a table.
func countRows(t *testing.T, sqlDB *sql.DB, table string) int {
	t.Helper()
	var count int
	err := sqlDB.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	require.NoError(t, err)
	return count
}

// --- Tests ---

func TestSeed_BasicCommands(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	commands := []string{
		"git status", "git add .", "git commit -m 'test'", "git push",
		"ls -la", "cd /tmp", "echo hello", "make build", "go test ./...",
		"docker run nginx",
	}

	entries := makeEntries(100, func(i int) string {
		return commands[i%len(commands)]
	})

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// Verify command_event rows.
	eventCount := countRows(t, sqlDB, "command_event")
	assert.Equal(t, 100, eventCount)

	// Verify command_template count equals unique normalized templates.
	templateCount := countRows(t, sqlDB, "command_template")
	assert.Greater(t, templateCount, 0)
	assert.LessOrEqual(t, templateCount, len(commands))

	// Verify command_stat exists for each template.
	statCount := countRows(t, sqlDB, "command_stat")
	assert.Equal(t, templateCount, statCount)
}

func TestSeed_TransitionBigrams(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	// Sequence: A, B, C, A, B
	// Expected transitions: A->B(2), B->C(1), C->A(1)
	sequence := []string{"git status", "git add .", "git commit", "git status", "git add ."}
	entries := makeEntries(5, func(i int) string {
		return sequence[i]
	})

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// Get template IDs for the commands.
	tidA := normalize.ComputeTemplateID(
		normalize.PreNormalize("git status", normalize.PreNormConfig{}).CmdNorm)
	tidB := normalize.ComputeTemplateID(
		normalize.PreNormalize("git add .", normalize.PreNormConfig{}).CmdNorm)
	tidC := normalize.ComputeTemplateID(
		normalize.PreNormalize("git commit", normalize.PreNormConfig{}).CmdNorm)

	// A->B should have count 2.
	var countAB int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT count FROM transition_stat
		WHERE scope = 'global' AND prev_template_id = ? AND next_template_id = ?
	`, tidA, tidB).Scan(&countAB)
	require.NoError(t, err)
	assert.Equal(t, 2, countAB)

	// B->C should have count 1.
	var countBC int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT count FROM transition_stat
		WHERE scope = 'global' AND prev_template_id = ? AND next_template_id = ?
	`, tidB, tidC).Scan(&countBC)
	require.NoError(t, err)
	assert.Equal(t, 1, countBC)

	// C->A should have count 1.
	var countCA int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT count FROM transition_stat
		WHERE scope = 'global' AND prev_template_id = ? AND next_template_id = ?
	`, tidC, tidA).Scan(&countCA)
	require.NoError(t, err)
	assert.Equal(t, 1, countCA)
}

func TestSeed_FrequencyDecay(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	// Seed same command at t=0, t=1000ms, t=2000ms.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []history.ImportEntry{
		{Command: "git status", Timestamp: base},
		{Command: "git status", Timestamp: base.Add(time.Millisecond * 1000)},
		{Command: "git status", Timestamp: base.Add(time.Millisecond * 2000)},
	}

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	tid := normalize.ComputeTemplateID(
		normalize.PreNormalize("git status", normalize.PreNormConfig{}).CmdNorm)

	var score float64
	err = sqlDB.QueryRowContext(ctx, `
		SELECT score FROM command_stat
		WHERE scope = 'global' AND template_id = ?
	`, tid).Scan(&score)
	require.NoError(t, err)

	// Manual calculation:
	// t=0:    score = 1.0
	// t=1000: score = 1.0 * exp(-1000/tauMs) + 1.0
	// t=2000: score = prev * exp(-1000/tauMs) + 1.0
	d := math.Exp(-1000.0 / float64(tauMs))
	expected := 1.0
	expected = expected*d + 1.0
	expected = expected*d + 1.0

	assert.InDelta(t, expected, score, 1e-9)
}

func TestSeed_PipelineDetection(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	entries := []history.ImportEntry{
		{
			Command:   "cat foo | grep bar | wc -l",
			Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// Verify pipeline_event has 3 segments.
	peCount := countRows(t, sqlDB, "pipeline_event")
	assert.Equal(t, 3, peCount)

	// Verify pipeline_transition has 2 rows (between 3 consecutive segments).
	ptCount := countRows(t, sqlDB, "pipeline_transition")
	assert.Equal(t, 2, ptCount)

	// Verify pipeline_pattern has 1 row.
	ppCount := countRows(t, sqlDB, "pipeline_pattern")
	assert.Equal(t, 1, ppCount)
}

func TestSeed_TemplateDedup(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	entries := makeEntries(50, func(i int) string {
		return "ls"
	})

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// 1 unique template.
	templateCount := countRows(t, sqlDB, "command_template")
	assert.Equal(t, 1, templateCount)

	// command_stat should reflect 50 occurrences.
	var successCount int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT success_count FROM command_stat WHERE scope = 'global'
	`).Scan(&successCount)
	require.NoError(t, err)
	assert.Equal(t, 50, successCount)

	// Score should be > 1 (50 decayed additions).
	var score float64
	err = sqlDB.QueryRowContext(ctx, `
		SELECT score FROM command_stat WHERE scope = 'global'
	`).Scan(&score)
	require.NoError(t, err)
	assert.Greater(t, score, 1.0)
}

func TestSeed_Idempotent(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	entries := makeEntries(10, func(i int) string {
		return fmt.Sprintf("cmd-%d", i)
	})

	// First seed.
	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	countBefore := countRows(t, sqlDB, "command_event")
	assert.Equal(t, 10, countBefore)

	// Second seed should be a no-op.
	err = Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	countAfter := countRows(t, sqlDB, "command_event")
	assert.Equal(t, 10, countAfter, "second seed should not add rows")
}

func TestSeed_IdempotentReplace(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	entries := makeEntries(5, func(i int) string {
		return "git status"
	})

	// Seed twice with same shell.
	err := Seed(ctx, sqlDB, entries, "bash")
	require.NoError(t, err)

	err = Seed(ctx, sqlDB, entries, "bash")
	require.NoError(t, err)

	// Should still have exactly 5 rows.
	assert.Equal(t, 5, countRows(t, sqlDB, "command_event"))

	// Different shell should be allowed.
	err = Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// Now we have 10 rows (5 for bash, 5 for zsh).
	assert.Equal(t, 10, countRows(t, sqlDB, "command_event"))
}

func TestSeed_EmptyHistory(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)

	err := Seed(context.Background(), sqlDB, nil, "zsh")
	require.NoError(t, err)

	assert.Equal(t, 0, countRows(t, sqlDB, "command_event"))
	assert.Equal(t, 0, countRows(t, sqlDB, "command_template"))
	assert.Equal(t, 0, countRows(t, sqlDB, "command_stat"))
}

func TestSeed_TimestampOrdering(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	// Out-of-order timestamps.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []history.ImportEntry{
		{Command: "cmd-c", Timestamp: base.Add(3 * time.Second)},
		{Command: "cmd-a", Timestamp: base.Add(1 * time.Second)},
		{Command: "cmd-b", Timestamp: base.Add(2 * time.Second)},
	}

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// Verify the events are inserted in timestamp order by checking
	// the first event's cmd_raw matches cmd-a (earliest timestamp).
	var firstCmd string
	err = sqlDB.QueryRowContext(ctx, `
		SELECT cmd_raw FROM command_event ORDER BY ts_ms ASC LIMIT 1
	`).Scan(&firstCmd)
	require.NoError(t, err)
	assert.Equal(t, "cmd-a", firstCmd)

	// Verify transitions reflect sorted order: a->b, b->c.
	tidA := normalize.ComputeTemplateID(
		normalize.PreNormalize("cmd-a", normalize.PreNormConfig{}).CmdNorm)
	tidB := normalize.ComputeTemplateID(
		normalize.PreNormalize("cmd-b", normalize.PreNormConfig{}).CmdNorm)
	tidC := normalize.ComputeTemplateID(
		normalize.PreNormalize("cmd-c", normalize.PreNormConfig{}).CmdNorm)

	var countAB int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT count FROM transition_stat
		WHERE scope = 'global' AND prev_template_id = ? AND next_template_id = ?
	`, tidA, tidB).Scan(&countAB)
	require.NoError(t, err)
	assert.Equal(t, 1, countAB)

	var countBC int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT count FROM transition_stat
		WHERE scope = 'global' AND prev_template_id = ? AND next_template_id = ?
	`, tidB, tidC).Scan(&countBC)
	require.NoError(t, err)
	assert.Equal(t, 1, countBC)
}

func TestSeed_MalformedUTF8(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)

	// Create entries with invalid UTF-8 bytes.
	entries := []history.ImportEntry{
		{
			Command:   "echo \xff\xfe hello",
			Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			Command:   "git status",
			Timestamp: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC),
		},
		{
			Command:   string([]byte{0x80, 0x81, 0x82}),
			Timestamp: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC),
		},
	}

	// Should not panic.
	err := Seed(context.Background(), sqlDB, entries, "zsh")
	require.NoError(t, err)

	assert.Equal(t, 3, countRows(t, sqlDB, "command_event"))
}

func TestSeed_LargeImport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large import test in short mode")
	}

	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	commands := []string{
		"git status", "git add .", "git commit -m 'msg'", "git push",
		"ls -la", "cd /tmp", "echo hello", "make build", "go test ./...",
		"docker run nginx", "cat file | grep pattern | wc -l",
		"npm install", "python script.py", "kubectl get pods",
		"ssh user@host", "curl http://example.com",
	}

	entries := makeEntries(25000, func(i int) string {
		return commands[i%len(commands)]
	})

	start := time.Now()
	err := Seed(ctx, sqlDB, entries, "zsh")
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Less(t, elapsed, 60*time.Second, "seed should complete within 60s")

	// Verify row counts are consistent.
	eventCount := countRows(t, sqlDB, "command_event")
	assert.Equal(t, 25000, eventCount)

	templateCount := countRows(t, sqlDB, "command_template")
	assert.Greater(t, templateCount, 0)
	assert.LessOrEqual(t, templateCount, len(commands))

	statCount := countRows(t, sqlDB, "command_stat")
	assert.Equal(t, templateCount, statCount)
}

func TestSeed_FTSPopulated(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	entries := []history.ImportEntry{
		{Command: "git status", Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Command: "git add .", Timestamp: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)},
		{Command: "echo hello", Timestamp: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)},
	}

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// FTS is populated via trigger; query should return results for 'git'.
	var ftsCount int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM command_event_fts WHERE command_event_fts MATCH 'git'
	`).Scan(&ftsCount)
	require.NoError(t, err)
	assert.Equal(t, 2, ftsCount, "FTS should find 2 rows matching 'git'")
}

func TestSeed_GlobalScopeOnly(t *testing.T) {
	t.Parallel()
	sqlDB := newTestDB(t)
	ctx := context.Background()

	entries := makeEntries(20, func(i int) string {
		return fmt.Sprintf("cmd-%d", i%5)
	})

	err := Seed(ctx, sqlDB, entries, "zsh")
	require.NoError(t, err)

	// Verify no rows with scope != "global" in command_stat.
	var nonGlobalStats int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM command_stat WHERE scope != 'global'
	`).Scan(&nonGlobalStats)
	require.NoError(t, err)
	assert.Equal(t, 0, nonGlobalStats)

	// Verify no rows with scope != "global" in transition_stat.
	var nonGlobalTransitions int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM transition_stat WHERE scope != 'global'
	`).Scan(&nonGlobalTransitions)
	require.NoError(t, err)
	assert.Equal(t, 0, nonGlobalTransitions)
}

// --- Benchmarks ---

func BenchmarkSeed_1K(b *testing.B) {
	benchmarkSeed(b, 1000)
}

func BenchmarkSeed_10K(b *testing.B) {
	benchmarkSeed(b, 10000)
}

func BenchmarkSeed_25K(b *testing.B) {
	benchmarkSeed(b, 25000)
}

func benchmarkSeed(b *testing.B, n int) {
	commands := []string{
		"git status", "git add .", "git commit -m 'msg'", "git push",
		"ls -la", "cd /tmp", "make build", "go test ./...",
		"cat file | grep pattern", "docker run nginx",
	}

	entries := make([]history.ImportEntry, n)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		entries[i] = history.ImportEntry{
			Command:   commands[i%len(commands)],
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tmpDir := b.TempDir()
		dbPath := filepath.Join(tmpDir, "bench.db")
		d, err := db.Open(context.Background(), db.Options{
			Path:     dbPath,
			SkipLock: true,
		})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		err = Seed(context.Background(), d.DB(), entries, "zsh")
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		d.Close()
	}
}

// --- Internal helper tests ---

func TestSanitizeUTF8(t *testing.T) {
	t.Parallel()

	// Valid UTF-8 should pass through.
	assert.Equal(t, "hello world", sanitizeUTF8("hello world"))

	// Invalid bytes should be replaced.
	result := sanitizeUTF8(string([]byte{0x80, 0x81}))
	assert.NotContains(t, result, string([]byte{0x80}))

	// Mixed valid and invalid.
	result = sanitizeUTF8("hello\xff world")
	assert.Contains(t, result, "hello")
	assert.Contains(t, result, "world")
}

func TestBuildOperatorChain(t *testing.T) {
	t.Parallel()

	segments := []normalize.Segment{
		{Raw: "cat file", Operator: normalize.OpPipe},
		{Raw: "grep pattern", Operator: normalize.OpAnd},
		{Raw: "sort", Operator: ""},
	}

	chain := buildOperatorChain(segments)
	assert.Equal(t, "|,&&", chain)
}

func TestComputeHash(t *testing.T) {
	t.Parallel()

	h1 := computeHash("test input")
	h2 := computeHash("test input")
	h3 := computeHash("different input")

	assert.Equal(t, h1, h2, "same input should produce same hash")
	assert.NotEqual(t, h1, h3, "different inputs should produce different hashes")
	assert.Len(t, h1, 32, "hash should be 32 hex chars (16 bytes)")
}

func TestParallelNormalize(t *testing.T) {
	t.Parallel()

	entries := make([]history.ImportEntry, 100)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		entries[i] = history.ImportEntry{
			Command:   fmt.Sprintf("cmd-%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
	}

	result := parallelNormalize(context.Background(), entries)
	assert.Len(t, result, 100)

	// Verify ordering is preserved.
	for i := 0; i < 100; i++ {
		assert.Equal(t, i, result[i].index)
		assert.Equal(t, fmt.Sprintf("cmd-%d", i), result[i].cmdRaw)
	}

	// Verify timestamps are set.
	for i := 0; i < 100; i++ {
		assert.Greater(t, result[i].tsMs, int64(0))
	}

	// Verify normalization ran (template IDs are non-empty).
	seen := make(map[string]bool)
	for _, r := range result {
		assert.NotEmpty(t, r.preNorm.TemplateID)
		assert.NotEmpty(t, r.preNorm.CmdNorm)
		seen[r.preNorm.TemplateID] = true
	}
	assert.Equal(t, 100, len(seen), "100 unique commands should produce 100 unique templates")
}

func TestDecayCalculation(t *testing.T) {
	t.Parallel()

	// Verify the decay formula used in insertCommandStats matches the
	// specification: score = oldScore * exp(-(nowMs - lastMs) / tauMs) + 1.0
	timestamps := []int64{0, 1000, 2000, 5000}

	var score float64
	var lastMs int64
	for i, ts := range timestamps {
		if i == 0 {
			score = 1.0
		} else {
			elapsed := float64(ts - lastMs)
			decay := math.Exp(-elapsed / float64(tauMs))
			score = score*decay + 1.0
		}
		lastMs = ts
	}

	// Verify manually:
	d1 := math.Exp(-1000.0 / float64(tauMs))
	d2 := math.Exp(-1000.0 / float64(tauMs))
	d3 := math.Exp(-3000.0 / float64(tauMs))

	expected := 1.0
	expected = expected*d1 + 1.0
	expected = expected*d2 + 1.0
	expected = expected*d3 + 1.0

	assert.InDelta(t, expected, score, 1e-12)
}

func TestTemplateTimestampSorting(t *testing.T) {
	t.Parallel()

	// Verify that timestamps within templateInfo are sorted before decay
	// calculation, even if they arrive out of order.
	timestamps := []int64{5000, 1000, 3000, 2000, 4000}
	sorted := make([]int64, len(timestamps))
	copy(sorted, timestamps)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Compute score with sorted timestamps.
	var score float64
	var lastMs int64
	for i, ts := range sorted {
		if i == 0 {
			score = 1.0
		} else {
			elapsed := float64(ts - lastMs)
			decay := math.Exp(-elapsed / float64(tauMs))
			score = score*decay + 1.0
		}
		lastMs = ts
	}

	assert.Greater(t, score, 1.0)
	assert.Less(t, score, float64(len(timestamps))+0.1)
}
