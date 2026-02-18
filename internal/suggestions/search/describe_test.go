package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func createDescribeTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-describe-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts_ms         INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL,
			repo_key      TEXT,
			template_id   TEXT,
			ephemeral     INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX idx_event_ts ON command_event(ts_ms);
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE command_template (
			template_id     TEXT PRIMARY KEY,
			cmd_norm        TEXT NOT NULL,
			tags            TEXT,
			slot_count      INTEGER NOT NULL,
			first_seen_ms   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL
		);
	`)
	require.NoError(t, err)

	return db
}

func insertDescribeTestData(t *testing.T, db *sql.DB, templateID, cmdNorm, cmdRaw string,
	tags []string, repoKey, cwd string, tsMs int64) {
	t.Helper()

	var tagsJSON string
	if len(tags) > 0 {
		b, err := json.Marshal(tags)
		require.NoError(t, err)
		tagsJSON = string(b)
	} else {
		tagsJSON = "null"
	}

	_, err := db.Exec(`
		INSERT OR IGNORE INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, 0, ?, ?)
	`, templateID, cmdNorm, tagsJSON, tsMs, tsMs)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO command_event (session_id, ts_ms, cmd_raw, cmd_norm, cwd, repo_key, template_id, ephemeral)
		VALUES ('session1', ?, ?, ?, ?, ?, ?, 0)
	`, tsMs, cmdRaw, cmdNorm, cwd, repoKey, templateID)
	require.NoError(t, err)
}

func TestDescribeService_Search_MatchesByTags(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-git-1", "git commit -m <msg>", "git commit -m 'fix bug'",
		[]string{"vcs", "git"}, "", "/home/user/project", 1000)
	insertDescribeTestData(t, db, "tmpl-docker-1", "docker run <arg>", "docker run nginx",
		[]string{"container", "docker"}, "", "/home/user/project", 2000)

	results, err := svc.Search(ctx, "version control commit", SearchOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, "git commit -m 'fix bug'", results[0].CmdRaw)
	assert.Equal(t, BackendDescribe, results[0].Backend)
	assert.NotEmpty(t, results[0].Tags)
	assert.NotEmpty(t, results[0].MatchedTags)
}

func TestDescribeService_Search_ContainerQuery(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-docker-1", "docker run <arg>", "docker run nginx",
		[]string{"container", "docker"}, "", "/home/user", 1000)
	insertDescribeTestData(t, db, "tmpl-k8s-1", "kubectl get pods", "kubectl get pods",
		[]string{"k8s", "container"}, "", "/home/user", 2000)
	insertDescribeTestData(t, db, "tmpl-git-1", "git status", "git status",
		[]string{"vcs", "git"}, "", "/home/user", 3000)

	results, err := svc.Search(ctx, "containers", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		assert.Contains(t, r.MatchedTags, "container")
	}
}

func TestDescribeService_Search_EmptyQuery(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	results, err := svc.Search(ctx, "", SearchOptions{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestDescribeService_Search_NoTagMatch(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-git-1", "git status", "git status",
		[]string{"vcs", "git"}, "", "/home/user", 1000)

	results, err := svc.Search(ctx, "xyzzy foobar", SearchOptions{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestDescribeService_Search_RepoFilter(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-git-1", "git commit -m <msg>", "git commit -m 'fix'",
		[]string{"vcs", "git"}, "repo1", "/path/repo1", 1000)
	insertDescribeTestData(t, db, "tmpl-git-2", "git push <arg> <arg>", "git push origin main",
		[]string{"vcs", "git"}, "repo2", "/path/repo2", 2000)

	results, err := svc.Search(ctx, "commit", SearchOptions{RepoKey: "repo1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "repo1", results[0].RepoKey)
}

func TestDescribeService_Search_ScoreByOverlap(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-pytest", "pytest <path>", "pytest tests/",
		[]string{"python", "test"}, "", "/home/user", 1000)
	insertDescribeTestData(t, db, "tmpl-go-test", "go test <path>", "go test ./...",
		[]string{"go", "test"}, "", "/home/user", 2000)
	insertDescribeTestData(t, db, "tmpl-python", "python <path>", "python script.py",
		[]string{"python"}, "", "/home/user", 3000)

	results, err := svc.Search(ctx, "python test", SearchOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, "pytest tests/", results[0].CmdRaw)
	assert.Equal(t, 1.0, results[0].Score)
}

func TestDescribeService_Search_CustomTagMapper(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)

	customMapper := func(desc string) []string {
		if desc == "my containers" {
			return []string{"container"}
		}
		return nil
	}

	svc := NewDescribeService(db, DescribeConfig{TagMapper: customMapper})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-docker-1", "docker run <arg>", "docker run nginx",
		[]string{"container", "docker"}, "", "/home/user", 1000)

	results, err := svc.Search(ctx, "my containers", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "docker run nginx", results[0].CmdRaw)
}

func TestDescribeService_Search_NetworkHTTP(t *testing.T) {
	t.Parallel()
	db := createDescribeTestDB(t)
	svc := NewDescribeService(db, DescribeConfig{})
	ctx := context.Background()

	insertDescribeTestData(t, db, "tmpl-curl", "curl <url>", "curl https://api.example.com",
		[]string{"network", "http"}, "", "/home/user", 1000)
	insertDescribeTestData(t, db, "tmpl-ssh", "ssh <arg>", "ssh user@host",
		[]string{"network", "remote"}, "", "/home/user", 2000)

	results, err := svc.Search(ctx, "http request", SearchOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].MatchedTags, "http")
}

func TestComputeMatchedTags(t *testing.T) {
	t.Parallel()
	queryTags := map[string]bool{"vcs": true, "git": true, "container": true}

	tests := []struct {
		name     string
		tags     []string
		expected []string
	}{
		{"full overlap", []string{"vcs", "git"}, []string{"git", "vcs"}},
		{"partial overlap", []string{"vcs", "python"}, []string{"vcs"}},
		{"no overlap", []string{"python", "test"}, nil},
		{"empty tags", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := computeMatchedTags(tt.tags, queryTags)
			assert.Equal(t, tt.expected, matched)
		})
	}
}

func TestDefaultDescriptionToTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		desc      string
		expectNil bool
	}{
		{"empty", "", true},
		{"unknown words", "xyzzy foobar baz", true},
		{"direct tag match", "git container", false},
		{"synonym mapping", "commit containers", false},
		{"with punctuation", "find, files!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := defaultDescriptionToTags(tt.desc)
			if tt.expectNil {
				assert.Nil(t, tags)
			} else {
				assert.NotNil(t, tags)
			}
		})
	}
}
