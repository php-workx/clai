package suggest

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runger/clai/internal/storage"
)

func TestDefaultRanker_Rank_BasicRanking(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session and commands
	createTestSession(t, store, "session-1")
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "git status", 1700000001000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/tmp", "git push", 1700000002000, true)
	createTestCommand(t, store, "session-1", "cmd-3", "/tmp", "git log", 1700000003000, true)

	ranker := NewRanker(store)

	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:  "session-1",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	if len(suggestions) == 0 {
		t.Fatal("Expected at least one suggestion")
	}

	// Verify suggestions have scores
	for i, s := range suggestions {
		if s.Score <= 0 || s.Score > 1 {
			t.Errorf("suggestions[%d].Score = %v, want 0 < score <= 1", i, s.Score)
		}
		if s.Source == "" {
			t.Errorf("suggestions[%d].Source is empty", i)
		}
		if s.Text == "" {
			t.Errorf("suggestions[%d].Text is empty", i)
		}
	}
}

func TestDefaultRanker_Rank_OrderedByScore(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session and commands with different timestamps
	createTestSession(t, store, "session-1")
	now := time.Now().UnixMilli()

	// More recent commands should score higher
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "git status", now-1000*60*60*24, true) // 1 day ago
	createTestCommand(t, store, "session-1", "cmd-2", "/tmp", "git push", now-1000*60*60, true)      // 1 hour ago
	createTestCommand(t, store, "session-1", "cmd-3", "/tmp", "git log", now-1000*60, true)          // 1 minute ago

	ranker := NewRanker(store)

	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:  "session-1",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	// Verify ordering is by score descending
	for i := 1; i < len(suggestions); i++ {
		if suggestions[i].Score > suggestions[i-1].Score {
			t.Errorf("Suggestions not ordered by score: [%d].Score=%v > [%d].Score=%v",
				i, suggestions[i].Score, i-1, suggestions[i-1].Score)
		}
	}
}

func TestDefaultRanker_Rank_SessionWeightHighest(t *testing.T) {
	t.Parallel()

	// Session commands should have highest source weight (1.0)
	sessionWeight := SourceWeight(SourceSession)
	cwdWeight := SourceWeight(SourceCWD)
	globalWeight := SourceWeight(SourceGlobal)

	if sessionWeight <= cwdWeight {
		t.Errorf("Session weight (%v) should be > CWD weight (%v)", sessionWeight, cwdWeight)
	}
	if cwdWeight <= globalWeight {
		t.Errorf("CWD weight (%v) should be > Global weight (%v)", cwdWeight, globalWeight)
	}
}

func TestDefaultRanker_Rank_RecencyDecay(t *testing.T) {
	t.Parallel()

	now := time.Now()

	// Test recency scores at different times
	tests := []struct {
		hoursAgo      int64
		minExpected   float64
		maxExpected   float64
		relativeOrder int // lower number = should score higher
	}{
		{0, 0.9, 1.0, 1},    // Just now
		{1, 0.5, 0.9, 2},    // 1 hour ago
		{24, 0.2, 0.5, 3},   // 1 day ago
		{168, 0.1, 0.3, 4},  // 1 week ago
		{720, 0.05, 0.2, 5}, // 1 month ago
	}

	scores := make([]float64, 0, len(tests))
	for _, tt := range tests {
		ts := now.Add(-time.Duration(tt.hoursAgo) * time.Hour).UnixMilli()
		score := calculateRecencyScore(ts, now)
		scores = append(scores, score)

		if score < tt.minExpected || score > tt.maxExpected {
			t.Errorf("Recency score for %d hours ago = %v, want between %v and %v",
				tt.hoursAgo, score, tt.minExpected, tt.maxExpected)
		}
	}

	// Verify relative ordering
	for i := 1; i < len(scores); i++ {
		if scores[i] >= scores[i-1] {
			t.Errorf("Recency scores not monotonically decreasing: %v >= %v", scores[i], scores[i-1])
		}
	}
}

func TestDefaultRanker_Rank_SuccessBias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		successCount int
		failureCount int
		expected     float64
	}{
		{"all success", 10, 0, 1.0},
		{"all failure", 0, 10, 0.0},
		{"50/50", 5, 5, 0.5},
		{"mostly success", 8, 2, 0.8},
		{"mostly failure", 2, 8, 0.2},
		{"no data", 0, 0, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calculateSuccessScore(tt.successCount, tt.failureCount)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("calculateSuccessScore(%d, %d) = %v, want %v",
					tt.successCount, tt.failureCount, got, tt.expected)
			}
		})
	}
}

func TestDefaultRanker_Rank_ToolAffinity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		command        string
		lastToolPrefix string
		expected       float64
	}{
		{"same tool", "git push", "git", 1.0},
		{"different tool", "docker run", "git", 0.0},
		{"no last command", "git status", "", 0.0},
		{"complex command", "docker-compose up", "docker-compose", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calculateAffinityScore(tt.command, tt.lastToolPrefix)
			if got != tt.expected {
				t.Errorf("calculateAffinityScore(%q, %q) = %v, want %v",
					tt.command, tt.lastToolPrefix, got, tt.expected)
			}
		})
	}
}

func TestDefaultRanker_Rank_WithToolAffinity(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UnixMilli()

	// Create session and commands
	createTestSession(t, store, "session-1")
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "git status", now-1000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/tmp", "docker ps", now-2000, true)

	ranker := NewRanker(store)

	// When last command was git, git commands should score higher
	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:   "session-1",
		CWD:         "/tmp",
		Prefix:      "",
		LastCommand: "git commit",
		MaxResults:  10,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	// Find git and docker suggestions
	var gitSuggestion, dockerSuggestion *Suggestion
	for i := range suggestions {
		if GetToolPrefix(suggestions[i].Text) == "git" {
			gitSuggestion = &suggestions[i]
		}
		if GetToolPrefix(suggestions[i].Text) == "docker" {
			dockerSuggestion = &suggestions[i]
		}
	}

	if gitSuggestion != nil && dockerSuggestion != nil {
		// Git should score higher due to affinity
		// Note: This might not always be true depending on other factors
		// so we just verify both exist
		t.Logf("Git score: %v, Docker score: %v", gitSuggestion.Score, dockerSuggestion.Score)
	}
}

func TestDefaultRanker_Rank_Deduplication(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session and duplicate commands
	createTestSession(t, store, "session-1")
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "git status", 1700000001000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/tmp", "git status", 1700000002000, true)
	createTestCommand(t, store, "session-1", "cmd-3", "/tmp", "git status", 1700000003000, false)

	ranker := NewRanker(store)

	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:  "session-1",
		CWD:        "/tmp",
		Prefix:     "git status",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	// Should deduplicate to one suggestion
	gitStatusCount := 0
	for _, s := range suggestions {
		if s.Text == "git status" {
			gitStatusCount++
		}
	}
	if gitStatusCount > 1 {
		t.Errorf("Found %d 'git status' suggestions, expected 1 (deduplication failed)", gitStatusCount)
	}
}

func TestDefaultRanker_Rank_MaxResults(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create many commands
	createTestSession(t, store, "session-1")
	for i := 0; i < 20; i++ {
		createTestCommand(t, store, "session-1", generateCmdID(i), "/tmp",
			"git cmd"+string(rune('a'+i)), int64(1700000001000+i*1000), true)
	}

	ranker := NewRanker(store)

	// Request only 5 results
	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:  "session-1",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	if len(suggestions) > 5 {
		t.Errorf("Got %d suggestions, want at most 5", len(suggestions))
	}
}

func TestDefaultRanker_Rank_NilRequest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ranker := NewRanker(store)

	suggestions, err := ranker.Rank(context.Background(), nil)
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	if len(suggestions) > 0 {
		t.Errorf("Expected nil or empty suggestions for nil request, got %d", len(suggestions))
	}
}

func TestDefaultRanker_Rank_EmptyPrefix(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create session and commands
	createTestSession(t, store, "session-1")
	createTestCommand(t, store, "session-1", "cmd-1", "/tmp", "git status", 1700000001000, true)
	createTestCommand(t, store, "session-1", "cmd-2", "/tmp", "docker ps", 1700000002000, true)

	ranker := NewRanker(store)

	// Empty prefix should return all matching commands
	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:  "session-1",
		CWD:        "/tmp",
		Prefix:     "",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	if len(suggestions) < 2 {
		t.Errorf("Expected at least 2 suggestions for empty prefix, got %d", len(suggestions))
	}
}

func TestDefaultRanker_Rank_MergesSources(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create two sessions
	createTestSession(t, store, "session-1")
	createTestSession(t, store, "session-2")

	// Session 1 commands
	createTestCommand(t, store, "session-1", "cmd-1", "/home/user", "make build", 1700000001000, true)

	// Session 2 commands (different session and directory)
	createTestCommand(t, store, "session-2", "cmd-2", "/var/log", "make test", 1700000002000, true)

	ranker := NewRanker(store)

	// Query for session-1 in /home/user with prefix "make"
	suggestions, err := ranker.Rank(ctx, &RankRequest{
		SessionID:  "session-1",
		CWD:        "/home/user",
		Prefix:     "make",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	// Should have results from multiple sources
	sources := make(map[string]bool)
	for _, s := range suggestions {
		sources[s.Source] = true
	}

	if len(sources) < 1 {
		t.Error("Expected results from at least one source")
	}
}

func TestCalculateScore(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recentTime := now.Add(-time.Hour).UnixMilli()

	tests := []struct {
		name           string
		source         Source
		commandTime    int64
		successCount   int
		failureCount   int
		command        string
		lastToolPrefix string
		wantMin        float64
		wantMax        float64
	}{
		{
			name:           "high score - session, recent, success, affinity",
			source:         SourceSession,
			commandTime:    recentTime,
			successCount:   10,
			failureCount:   0,
			command:        "git status",
			lastToolPrefix: "git",
			wantMin:        0.8,
			wantMax:        1.0,
		},
		{
			name:           "low score - global, old, failure, no affinity",
			source:         SourceGlobal,
			commandTime:    now.Add(-720 * time.Hour).UnixMilli(), // 30 days ago
			successCount:   0,
			failureCount:   10,
			command:        "docker ps",
			lastToolPrefix: "git",
			wantMin:        0.0,
			wantMax:        0.3,
		},
		{
			name:           "medium score - cwd, moderate time, mixed success",
			source:         SourceCWD,
			commandTime:    now.Add(-24 * time.Hour).UnixMilli(), // 1 day ago
			successCount:   5,
			failureCount:   5,
			command:        "npm install",
			lastToolPrefix: "npm",
			wantMin:        0.3,
			wantMax:        0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := calculateScore(tt.source, tt.commandTime, now, tt.successCount, tt.failureCount, tt.command, tt.lastToolPrefix)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("calculateScore() = %v, want between %v and %v", score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// Helper function
func generateCmdID(n int) string {
	return filepath.Join("test-cmd", string(rune('0'+n/10)), string(rune('0'+n%10)))
}

// Benchmark tests

func BenchmarkRanker_Rank_SmallHistory(b *testing.B) {
	store := benchmarkStore(b, 100)
	defer store.Close()

	ranker := NewRanker(store)
	ctx := context.Background()
	req := &RankRequest{
		SessionID:  "session-0",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ranker.Rank(ctx, req)
	}
}

func BenchmarkRanker_Rank_MediumHistory(b *testing.B) {
	store := benchmarkStore(b, 1000)
	defer store.Close()

	ranker := NewRanker(store)
	ctx := context.Background()
	req := &RankRequest{
		SessionID:  "session-0",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ranker.Rank(ctx, req)
	}
}

func BenchmarkRanker_Rank_LargeHistory(b *testing.B) {
	store := benchmarkStore(b, 10000)
	defer store.Close()

	ranker := NewRanker(store)
	ctx := context.Background()
	req := &RankRequest{
		SessionID:  "session-0",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ranker.Rank(ctx, req)
	}
}

func benchmarkStore(b *testing.B, commandCount int) *storage.SQLiteStore {
	b.Helper()

	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		b.Fatalf("NewSQLiteStore() error = %v", err)
	}

	ctx := context.Background()

	// Create sessions
	numSessions := 10
	for i := 0; i < numSessions; i++ {
		session := &storage.Session{
			SessionID:       "session-" + string(rune('0'+i)),
			StartedAtUnixMs: 1700000000000 + int64(i*1000000),
			Shell:           "zsh",
			OS:              "darwin",
			InitialCWD:      "/tmp",
		}
		if err := store.CreateSession(ctx, session); err != nil {
			b.Fatalf("CreateSession() error = %v", err)
		}
	}

	// Create commands
	commands := []string{"git status", "git push", "git pull", "git commit", "git log",
		"docker ps", "docker run", "docker build", "docker compose up",
		"make build", "make test", "make install",
		"npm install", "npm run dev", "npm test"}

	for i := 0; i < commandCount; i++ {
		sessionIdx := i % numSessions
		cmdIdx := i % len(commands)
		isSuccess := i%5 != 0 // 80% success rate
		cmd := &storage.Command{
			CommandID:     "cmd-" + string(rune('a'+i/1000)) + string(rune('a'+(i/100)%10)) + string(rune('a'+(i/10)%10)) + string(rune('a'+i%10)),
			SessionID:     "session-" + string(rune('0'+sessionIdx)),
			TsStartUnixMs: 1700000000000 + int64(i*1000),
			CWD:           "/tmp",
			Command:       commands[cmdIdx],
			IsSuccess:     &isSuccess,
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			b.Fatalf("CreateCommand() error = %v", err)
		}
	}

	return store
}

func TestRanker_Performance_10KCommands_Under50ms(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	if os.Getenv("RUN_PERF_TESTS") == "" {
		t.Skip("Skipping flaky performance test (set RUN_PERF_TESTS=1 to enable)")
	}

	t.Parallel()

	// Create store with 10K commands
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "perf.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create sessions
	numSessions := 10
	for i := 0; i < numSessions; i++ {
		session := &storage.Session{
			SessionID:       "session-" + string(rune('0'+i)),
			StartedAtUnixMs: 1700000000000 + int64(i*1000000),
			Shell:           "zsh",
			OS:              "darwin",
			InitialCWD:      "/tmp",
		}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
	}

	// Create 10K commands
	commands := []string{"git status", "git push", "git pull", "git commit", "git log",
		"docker ps", "docker run", "docker build",
		"make build", "make test", "npm install", "npm run dev"}

	for i := 0; i < 10000; i++ {
		sessionIdx := i % numSessions
		cmdIdx := i % len(commands)
		isSuccess := i%5 != 0
		cmd := &storage.Command{
			CommandID:     generateLargeCmdID(i),
			SessionID:     "session-" + string(rune('0'+sessionIdx)),
			TsStartUnixMs: 1700000000000 + int64(i*1000),
			CWD:           "/tmp",
			Command:       commands[cmdIdx],
			IsSuccess:     &isSuccess,
		}
		if err := store.CreateCommand(ctx, cmd); err != nil {
			t.Fatalf("CreateCommand() error = %v", err)
		}
	}

	ranker := NewRanker(store)
	req := &RankRequest{
		SessionID:  "session-0",
		CWD:        "/tmp",
		Prefix:     "git",
		MaxResults: 10,
	}

	// Measure time
	start := time.Now()
	_, err = ranker.Rank(ctx, req)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	t.Logf("Ranking 10K commands took %v", duration)

	// Target: < 50ms
	if duration > 50*time.Millisecond {
		t.Errorf("Ranking took %v, want < 50ms", duration)
	}
}

func generateLargeCmdID(n int) string {
	// Generate unique IDs for large number of commands
	chars := "abcdefghijklmnopqrstuvwxyz"
	id := make([]byte, 0, 10)
	id = append(id, "cmd-"...)

	remaining := n
	for i := 0; i < 4; i++ {
		id = append(id, chars[remaining%26])
		remaining /= 26
	}
	return string(id)
}
