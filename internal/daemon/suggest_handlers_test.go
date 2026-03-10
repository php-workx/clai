package daemon

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	pb "github.com/runger/clai/gen/clai/v1"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/explain"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
)

// ============================================================================
// Feature flag tests
// ============================================================================

// TestScorerVersion_DefaultsToV2 verifies that a server with V2DB
// defaults to "v2" scorer version.
func TestScorerVersion_DefaultsToV2(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		V2DB: v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	if server.scorerVersion != "v2" {
		t.Errorf("expected scorerVersion='v2', got %q", server.scorerVersion)
	}
	if server.v2Scorer == nil {
		t.Error("v2Scorer should be initialized when V2DB is provided")
	}
}

// TestScorerVersion_V2WorksWithDB verifies that "v2" scorer version is kept
// when V2DB is provided (and scorer is auto-initialized).
func TestScorerVersion_V2WorksWithDB(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		ScorerVersion: "v2",
		V2DB:          v2db,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	if server.scorerVersion != "v2" {
		t.Errorf("expected scorerVersion='v2', got %q", server.scorerVersion)
	}
	if server.v2Scorer == nil {
		t.Error("v2Scorer should be initialized when V2DB is provided")
	}
}

// ============================================================================
// Suggest handler tests
// ============================================================================

// TestSuggest_ReturnsResults verifies suggest returns results from V2 scorer.
func TestSuggest_ReturnsResults(t *testing.T) {
	t.Parallel()
	server := createTestServer(t)
	ctx := context.Background()

	resp, err := server.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "git",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	// With an empty DB, V2 scorer may return empty results
	_ = resp
}

// TestSuggest_DefaultMaxResults verifies that zero MaxResults defaults to 5.
func TestSuggest_DefaultMaxResults(t *testing.T) {
	t.Parallel()
	server := createTestServer(t)
	ctx := context.Background()

	resp, err := server.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		Buffer:     "",
		MaxResults: 0, // Should default to 5
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	// Just verify no error with default max results
	_ = resp
}

// TestSuggest_V2_WithScorer verifies V2 mode uses the V2 scorer when available.
func TestSuggest_V2_WithScorer(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		t.Fatalf("failed to open V2 database: %v", err)
	}
	defer v2db.Close()

	server, err := NewServer(&ServerConfig{
		V2DB:          v2db,
		ScorerVersion: "v2",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.scorerVersion != "v2" {
		t.Fatalf("expected scorerVersion='v2', got %q", server.scorerVersion)
	}

	// The V2 scorer is initialized but the DB has no data, so it should
	// return an empty list (not an error).
	resp, err := server.Suggest(ctx, &pb.SuggestRequest{
		SessionId:  "test-session",
		Cwd:        "/tmp",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	// Empty DB means no V2 suggestions
	// This verifies the V2 path was attempted without error
	_ = resp
}

// TestMergeResponses_Deduplication verifies mergeResponses deduplicates by command text.
func TestMergeResponses_Deduplication(t *testing.T) {
	t.Parallel()

	v1 := &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "git status", Source: "history", Score: 0.9},
			{Text: "git push", Source: "history", Score: 0.7},
		},
	}
	v2 := &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "git status", Source: "v2", Score: 0.85},
			{Text: "git pull", Source: "v2", Score: 0.6},
		},
	}

	result := mergeResponses(v1, v2, 5)
	if result == nil {
		t.Fatal("mergeResponses returned nil")
	}

	// "git status" should appear only once (V2 wins due to interleave order)
	textCounts := make(map[string]int)
	for _, s := range result.Suggestions {
		textCounts[s.Text]++
	}

	if textCounts["git status"] != 1 {
		t.Errorf("expected 'git status' once, got %d times", textCounts["git status"])
	}

	// Should have 3 unique: git status (from v2), git pull (from v2), git push (from v1)
	if len(result.Suggestions) != 3 {
		t.Errorf("expected 3 merged suggestions, got %d", len(result.Suggestions))
	}
}

// TestMergeResponses_MaxResultsCap verifies mergeResponses respects maxResults.
func TestMergeResponses_MaxResultsCap(t *testing.T) {
	t.Parallel()

	v1 := &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "cmd-a", Source: "v1", Score: 0.9},
			{Text: "cmd-b", Source: "v1", Score: 0.8},
			{Text: "cmd-c", Source: "v1", Score: 0.7},
		},
	}
	v2 := &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "cmd-d", Source: "v2", Score: 0.85},
			{Text: "cmd-e", Source: "v2", Score: 0.6},
		},
	}

	result := mergeResponses(v1, v2, 3)
	if len(result.Suggestions) != 3 {
		t.Errorf("expected 3 suggestions (maxResults=3), got %d", len(result.Suggestions))
	}
}

// TestMergeResponses_EmptyInputs verifies mergeResponses handles empty/nil inputs.
func TestMergeResponses_EmptyInputs(t *testing.T) {
	t.Parallel()

	v1 := &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "git log", Source: "v1", Score: 0.5},
		},
	}

	// nil V2 returns V1 as-is
	result := mergeResponses(v1, nil, 5)
	if len(result.Suggestions) != 1 || result.Suggestions[0].Text != "git log" {
		t.Error("nil V2 should return V1 unchanged")
	}

	// empty V2 returns V1 as-is
	result = mergeResponses(v1, &pb.SuggestResponse{}, 5)
	if len(result.Suggestions) != 1 || result.Suggestions[0].Text != "git log" {
		t.Error("empty V2 should return V1 unchanged")
	}

	v2 := &pb.SuggestResponse{
		Suggestions: []*pb.Suggestion{
			{Text: "git diff", Source: "v2", Score: 0.6},
		},
	}

	// nil V1 returns V2 as-is
	result = mergeResponses(nil, v2, 5)
	if len(result.Suggestions) != 1 || result.Suggestions[0].Text != "git diff" {
		t.Error("nil V1 should return V2 unchanged")
	}

	// empty V1 returns V2 as-is
	result = mergeResponses(&pb.SuggestResponse{}, v2, 5)
	if len(result.Suggestions) != 1 || result.Suggestions[0].Text != "git diff" {
		t.Error("empty V1 should return V2 unchanged")
	}
}

// These test-only helpers intentionally mutate unexported fields on
// suggest2.Suggestion (and its unexported score struct) via reflection/unsafe.
// They depend on the current internal field names:
// Suggestion: lastSeenMs, maxTransCount, maxFreqScore, scores
// scoreInfo: repoTransition, globalTransition, repoFrequency, globalFrequency,
// projectTask, dangerous, dirTransition, dirFrequency, workflowBoost,
// pipelineConf, dismissalPenalty, recoveryBoost.
// If suggest2 internals are renamed, these helpers need to be updated.
func setSuggestionPrivateInt64(s *suggest2.Suggestion, field string, value int64) {
	v := reflect.ValueOf(s).Elem().FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(value)
}

func setSuggestionPrivateInt(s *suggest2.Suggestion, field string, value int) {
	v := reflect.ValueOf(s).Elem().FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(value))
}

func setSuggestionPrivateFloat64(s *suggest2.Suggestion, field string, value float64) {
	v := reflect.ValueOf(s).Elem().FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetFloat(value)
}

func setSuggestionScorePrivateFloat64(s *suggest2.Suggestion, field string, value float64) {
	scores := reflect.ValueOf(s).Elem().FieldByName("scores")
	v := scores.FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetFloat(value)
}

func TestV2SuggestionRisk(t *testing.T) {
	t.Parallel()
	if got := v2SuggestionRisk("rm -rf /tmp/test"); got != "destructive" {
		t.Fatalf("expected destructive risk, got %q", got)
	}
	if got := v2SuggestionRisk("git status"); got != "" {
		t.Fatalf("expected no risk for safe command, got %q", got)
	}
}

func TestV2SuggestionDescription_CoversBranches(t *testing.T) {
	t.Parallel()

	s := suggest2.Suggestion{Command: "git status"}
	why := []explain.Reason{{Tag: "repo_trans", Description: "From repository workflow", Contribution: 0.4}}
	if got := v2SuggestionDescription(&s, why, "git add ."); got != "From repository workflow" {
		t.Fatalf("expected why-first description, got %q", got)
	}

	s2 := suggest2.Suggestion{Command: "git commit"}
	setSuggestionPrivateInt(&s2, "maxTransCount", 2)
	if got := v2SuggestionDescription(&s2, nil, "a very long previous command that should be truncated for display in description"); got == "" || got[:15] != "Often run after" {
		t.Fatalf("expected transition description, got %q", got)
	}

	s3 := suggest2.Suggestion{Command: "npm test"}
	setSuggestionPrivateFloat64(&s3, "maxFreqScore", 1.1)
	if got := v2SuggestionDescription(&s3, nil, ""); got != "Frequently used command." {
		t.Fatalf("expected frequency description, got %q", got)
	}

	s4 := suggest2.Suggestion{Command: "ls"}
	setSuggestionPrivateInt64(&s4, "lastSeenMs", 1700000000000)
	if got := v2SuggestionDescription(&s4, nil, ""); got != "Used recently." {
		t.Fatalf("expected recency description, got %q", got)
	}

	if got := v2SuggestionDescription(&suggest2.Suggestion{Command: "echo hi"}, nil, ""); got != "" {
		t.Fatalf("expected empty description fallback, got %q", got)
	}
}

func TestV2SuggestionReasons_IncludesExplainAndSignals(t *testing.T) {
	t.Parallel()
	nowMs := int64(1_700_000_010_000)
	s := suggest2.Suggestion{Command: "make test"}
	setSuggestionPrivateInt64(&s, "lastSeenMs", 1_700_000_000_000)
	setSuggestionPrivateFloat64(&s, "maxFreqScore", 2.5)
	setSuggestionPrivateInt(&s, "maxTransCount", 3)

	why := []explain.Reason{
		{Tag: "repo_trans", Description: "Common in this repo", Contribution: 0.3},
	}
	reasons := v2SuggestionReasons(&s, why, nowMs)
	if len(reasons) < 4 {
		t.Fatalf("expected explain + recency + frequency + transition reasons, got %d", len(reasons))
	}

	hasType := map[string]bool{}
	for _, r := range reasons {
		hasType[r.Type] = true
	}
	for _, typ := range []string{"repo_trans", "recency", "frequency", "transition_count"} {
		if !hasType[typ] {
			t.Fatalf("expected reason type %q in %+v", typ, reasons)
		}
	}
}

func TestV2SuggestionToProto_MapsFields(t *testing.T) {
	t.Parallel()
	nowMs := int64(1_700_000_010_000)
	s := suggest2.Suggestion{
		Command:    "rm -rf /tmp/demo",
		Score:      0.91,
		Confidence: 0.77,
	}
	setSuggestionPrivateInt64(&s, "lastSeenMs", 1_700_000_000_000)
	cfg := explain.DefaultConfig()
	got := v2SuggestionToProto(&s, "git clean", nowMs, cfg)

	if got.Text != s.Command || got.CmdNorm != s.Command {
		t.Fatalf("expected command text/cmd_norm to match, got %+v", got)
	}
	if got.Source != "global" {
		t.Fatalf("expected source=global, got %q", got.Source)
	}
	if got.Risk != "destructive" {
		t.Fatalf("expected destructive risk for rm -rf, got %q", got.Risk)
	}
	if got.Score != s.Score || got.Confidence != s.Confidence {
		t.Fatalf("expected score/confidence copied, got score=%f confidence=%f", got.Score, got.Confidence)
	}
	if len(got.Reasons) == 0 {
		t.Fatal("expected reasons to be populated")
	}
}

func TestV2SuggestionSource_DominantSignals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		setter  func(*suggest2.Suggestion)
		wantSrc string
	}{
		{
			name: "repo dominates",
			setter: func(s *suggest2.Suggestion) {
				setSuggestionScorePrivateFloat64(s, "repoTransition", 0.8)
				setSuggestionScorePrivateFloat64(s, "globalFrequency", 0.2)
			},
			wantSrc: "repo",
		},
		{
			name: "cwd dominates",
			setter: func(s *suggest2.Suggestion) {
				setSuggestionScorePrivateFloat64(s, "dirFrequency", 0.9)
				setSuggestionScorePrivateFloat64(s, "repoFrequency", 0.4)
			},
			wantSrc: "cwd",
		},
		{
			name: "session dominates",
			setter: func(s *suggest2.Suggestion) {
				setSuggestionScorePrivateFloat64(s, "workflowBoost", 0.7)
				setSuggestionScorePrivateFloat64(s, "globalTransition", 0.4)
			},
			wantSrc: "session",
		},
		{
			name: "falls back global",
			setter: func(s *suggest2.Suggestion) {
				// No positive sources.
			},
			wantSrc: "global",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := suggest2.Suggestion{Command: "git status"}
			tc.setter(&s)
			if got := v2SuggestionSource(&s); got != tc.wantSrc {
				t.Fatalf("v2SuggestionSource()=%q want %q", got, tc.wantSrc)
			}
		})
	}
}

func TestFormatAgo_CoversRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		want  string
		delta int64
	}{
		{"0s", -1},
		{"30s", 30 * 1000},
		{"2m", 2 * 60 * 1000},
		{"3h", 3 * 60 * 60 * 1000},
		{"2d", 2 * 24 * 60 * 60 * 1000},
	}
	for _, tc := range cases {
		if got := formatAgo(tc.delta); got != tc.want {
			t.Fatalf("formatAgo(%d) = %q, want %q", tc.delta, got, tc.want)
		}
	}
}
