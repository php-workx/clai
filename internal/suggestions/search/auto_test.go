package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func TestAutoService_Search_MergesFTSAndDescribe(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	describeSvc := NewDescribeService(db, DescribeConfig{})
	autoSvc := NewAutoService(nil, describeSvc, DefaultAutoConfig())
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-docker-1", "docker run <arg>", "docker run nginx",
		[]string{"container", "docker"}, "", "/home/user", 1000)
	insertDescribeTestData(t, db, "tmpl-git-1", "git commit -m <msg>", "git commit -m 'fix'",
		[]string{"vcs", "git"}, "", "/home/user", 2000)

	results, err := autoSvc.Search(ctx, "containers", SearchOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, BackendAuto, results[0].Backend)
}

func TestAutoService_Search_EmptyQuery(t *testing.T) {
	t.Parallel()
	autoSvc := NewAutoService(nil, nil, DefaultAutoConfig())
	ctx := context.Background()

	results, err := autoSvc.Search(ctx, "", SearchOptions{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestAutoService_Search_NoResults(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	describeSvc := NewDescribeService(db, DescribeConfig{})
	autoSvc := NewAutoService(nil, describeSvc, DefaultAutoConfig())
	ctx := context.Background()

	results, err := autoSvc.Search(ctx, "xyzzy nonsense", SearchOptions{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestAutoService_Search_DescribeOnlyWhenFTSNil(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	describeSvc := NewDescribeService(db, DescribeConfig{})
	autoSvc := NewAutoService(nil, describeSvc, DefaultAutoConfig())
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-npm-1", "npm install <arg>", "npm install express",
		[]string{"js", "package"}, "", "/home/user", 1000)

	results, err := autoSvc.Search(ctx, "install packages", SearchOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, BackendAuto, results[0].Backend)
	assert.NotEmpty(t, results[0].MatchedTags)
}

func TestAutoService_Search_Limit(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	describeSvc := NewDescribeService(db, DescribeConfig{})
	autoSvc := NewAutoService(nil, describeSvc, DefaultAutoConfig())
	ctx := context.Background()

	for i := 0; i < 30; i++ {
		id := "tmpl-git-" + string(rune('a'+i))
		insertDescribeTestData(t, db, id, "git status", "git status",
			[]string{"vcs", "git"}, "", "/home/user", int64(1000+i))
	}

	results, err := autoSvc.Search(ctx, "version control", SearchOptions{Limit: 5})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 5)
}

func TestAutoConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := DefaultAutoConfig()
	assert.Equal(t, 0.6, cfg.FTSWeight)
	assert.Equal(t, 0.4, cfg.DescribeWeight)
}

func TestAutoService_WeightNormalization(t *testing.T) {
	t.Parallel()
	svc := NewAutoService(nil, nil, AutoConfig{
		FTSWeight:      3.0,
		DescribeWeight: 7.0,
	})
	assert.InDelta(t, 0.3, svc.ftsWeight, 0.001)
	assert.InDelta(t, 0.7, svc.describeWeight, 0.001)
}

func TestAutoService_DefaultWeights(t *testing.T) {
	t.Parallel()
	svc := NewAutoService(nil, nil, AutoConfig{})
	assert.InDelta(t, 0.6, svc.ftsWeight, 0.001)
	assert.InDelta(t, 0.4, svc.describeWeight, 0.001)
}

func TestDedupKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		result   SearchResult
		expected string
	}{
		{"with template ID", SearchResult{TemplateID: "abc123", CmdRaw: "git status"}, "t:abc123"},
		{"without template ID", SearchResult{CmdRaw: "git status"}, "r:git status"},
		{"empty template ID", SearchResult{TemplateID: "", CmdRaw: "ls -la"}, "r:ls -la"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, dedupKey(tt.result))
		})
	}
}

func TestMaxScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		results  []SearchResult
		expected float64
	}{
		{"empty", nil, 0},
		{"single", []SearchResult{{Score: -5.0}}, -5.0},
		{"multiple negative", []SearchResult{{Score: -3.0}, {Score: -7.0}, {Score: -1.0}}, -7.0},
		{"all positive", []SearchResult{{Score: 3.0}, {Score: 7.0}, {Score: 1.0}}, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, maxScore(tt.results))
		})
	}
}

func TestMergeResults_Deduplication(t *testing.T) {
	t.Parallel()
	svc := NewAutoService(nil, nil, DefaultAutoConfig())

	ftsResults := []SearchResult{
		{ID: 1, CmdRaw: "git status", TemplateID: "tmpl-1", Score: -3.0},
		{ID: 2, CmdRaw: "docker run nginx", TemplateID: "tmpl-2", Score: -5.0},
	}

	descResults := []SearchResult{
		{ID: 1, CmdRaw: "git status", TemplateID: "tmpl-1", Score: 0.8,
			Tags: []string{"vcs", "git"}, MatchedTags: []string{"vcs"}},
		{ID: 3, CmdRaw: "kubectl get pods", TemplateID: "tmpl-3", Score: 0.5,
			Tags: []string{"k8s", "container"}, MatchedTags: []string{"container"}},
	}

	merged := svc.mergeResults(ftsResults, descResults)
	assert.Len(t, merged, 3)
	for _, r := range merged {
		assert.Equal(t, BackendAuto, r.Backend)
	}
}

func TestMergeResults_EnrichesFTSWithTags(t *testing.T) {
	t.Parallel()
	svc := NewAutoService(nil, nil, DefaultAutoConfig())

	ftsResults := []SearchResult{
		{ID: 1, CmdRaw: "git status", TemplateID: "tmpl-1", Score: -3.0},
	}
	descResults := []SearchResult{
		{ID: 1, CmdRaw: "git status", TemplateID: "tmpl-1", Score: 0.8,
			Tags: []string{"vcs", "git"}, MatchedTags: []string{"vcs"},
			CmdNorm: "git status"},
	}

	merged := svc.mergeResults(ftsResults, descResults)
	require.Len(t, merged, 1)
	assert.Equal(t, []string{"vcs", "git"}, merged[0].Tags)
	assert.Equal(t, []string{"vcs"}, merged[0].MatchedTags)
	assert.Equal(t, "git status", merged[0].CmdNorm)
}

func TestBackendConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Backend("fts"), BackendFTS)
	assert.Equal(t, Backend("describe"), BackendDescribe)
	assert.Equal(t, Backend("auto"), BackendAuto)
	assert.Equal(t, Backend("fallback"), BackendFallback)
}
