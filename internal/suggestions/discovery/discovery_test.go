package discovery

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-discovery-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create project_task table
	_, err = db.Exec(`
		CREATE TABLE project_task (
			repo_key      TEXT NOT NULL,
			kind          TEXT NOT NULL,
			name          TEXT NOT NULL,
			command       TEXT NOT NULL,
			description   TEXT,
			discovered_ts INTEGER NOT NULL,
			PRIMARY KEY(repo_key, kind, name)
		);
		CREATE INDEX idx_project_task_repo ON project_task(repo_key);
	`)
	require.NoError(t, err)

	return db
}

// createTestRepo creates a temporary directory for a test repository.
func createTestRepo(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-test-repo-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	return dir
}

func TestService_NewService(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultOptions())
	require.NoError(t, err)
	defer svc.Close()

	assert.Equal(t, DefaultTimeout, svc.opts.Timeout)
	assert.Equal(t, int64(DefaultMaxOutputBytes), svc.opts.MaxOutputBytes)
	assert.Equal(t, DefaultTTL, svc.opts.TTL)
}

func TestService_NewService_CustomOptions(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	opts := Options{
		Timeout:        100 * time.Millisecond,
		MaxOutputBytes: 512 * 1024,
		TTL:            1 * time.Minute,
	}

	svc, err := NewService(db, opts)
	require.NoError(t, err)
	defer svc.Close()

	assert.Equal(t, 100*time.Millisecond, svc.opts.Timeout)
	assert.Equal(t, int64(512*1024), svc.opts.MaxOutputBytes)
	assert.Equal(t, 1*time.Minute, svc.TTL())
}

func TestService_DiscoverPackageJSON(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Millisecond})
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)

	// Create package.json
	packageJSON := `{
		"name": "test-project",
		"scripts": {
			"test": "jest",
			"build": "tsc",
			"lint": "eslint ."
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(packageJSON), 0644))

	// Run discovery
	ctx := context.Background()
	discovered, err := svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.True(t, discovered)

	// Get tasks
	tasks, err := svc.GetTasks(ctx, repoRoot)
	require.NoError(t, err)
	assert.Len(t, tasks, 3)

	// Verify tasks are sorted by name
	assert.Equal(t, "build", tasks[0].Name)
	assert.Equal(t, "npm run build", tasks[0].Command)
	assert.Equal(t, KindPackageJSON, tasks[0].Kind)

	assert.Equal(t, "lint", tasks[1].Name)
	assert.Equal(t, "test", tasks[2].Name)
}

func TestService_DiscoverMakefile(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Millisecond})
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)

	// Create Makefile
	makefile := `
.PHONY: all build test clean

all: build

build:
	go build ./...

test:
	go test ./...

clean:
	rm -rf bin/
`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "Makefile"), []byte(makefile), 0644))

	// Run discovery
	ctx := context.Background()
	discovered, err := svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.True(t, discovered)

	// Get tasks
	tasks, err := svc.GetTasksByKind(ctx, repoRoot, KindMakefile)
	require.NoError(t, err)
	assert.Len(t, tasks, 4) // all, build, test, clean

	// Find build task
	var buildTask *Task
	for i := range tasks {
		if tasks[i].Name == "build" {
			buildTask = &tasks[i]
			break
		}
	}
	require.NotNil(t, buildTask)
	assert.Equal(t, "make build", buildTask.Command)
	assert.Equal(t, "Build the project", buildTask.Description) // From commonTargets
}

func TestService_DiscoverBoth(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Millisecond})
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)

	// Create both package.json and Makefile
	packageJSON := `{"scripts": {"start": "node app.js"}}`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(packageJSON), 0644))

	makefile := "build:\n\tgo build ./..."
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "Makefile"), []byte(makefile), 0644))

	// Run discovery
	ctx := context.Background()
	_, err = svc.Discover(ctx, repoRoot)
	require.NoError(t, err)

	// Get all tasks
	tasks, err := svc.GetTasks(ctx, repoRoot)
	require.NoError(t, err)
	assert.Len(t, tasks, 2) // 1 from package.json, 1 from Makefile

	// Check kinds
	kindCounts := make(map[TaskKind]int)
	for _, t := range tasks {
		kindCounts[t.Kind]++
	}
	assert.Equal(t, 1, kindCounts[KindPackageJSON])
	assert.Equal(t, 1, kindCounts[KindMakefile])
}

func TestService_DiscoverEmptyRepo(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Millisecond})
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)

	// Run discovery on empty repo
	ctx := context.Background()
	discovered, err := svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.True(t, discovered) // Discovery ran, just found nothing

	tasks, err := svc.GetTasks(ctx, repoRoot)
	require.NoError(t, err)
	assert.Len(t, tasks, 0)
}

func TestService_DiscoverNoRepo(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, DefaultOptions())
	require.NoError(t, err)
	defer svc.Close()

	ctx := context.Background()

	// Empty repo root
	discovered, err := svc.Discover(ctx, "")
	require.NoError(t, err)
	assert.False(t, discovered)
}

func TestService_TTLCaching(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Hour}) // Long TTL
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)
	makefile := "build:\n\tgo build ./..."
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "Makefile"), []byte(makefile), 0644))

	ctx := context.Background()

	// First discovery
	discovered, err := svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.True(t, discovered)

	// Second discovery should be cached (in-memory)
	discovered, err = svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.False(t, discovered) // Cache hit

	// Invalidate in-memory cache only
	svc.InvalidateCache(repoRoot)

	// Third discovery should still be cached (from DB timestamp, TTL not expired)
	discovered, err = svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.False(t, discovered) // DB cache hit
}

func TestService_InvalidPackageJSON(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Millisecond})
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)

	// Create invalid package.json
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte("not json"), 0644))

	// Discovery should not fail, just skip this source
	ctx := context.Background()
	discovered, err := svc.Discover(ctx, repoRoot)
	require.NoError(t, err)
	assert.True(t, discovered)

	tasks, err := svc.GetTasks(ctx, repoRoot)
	require.NoError(t, err)
	assert.Len(t, tasks, 0)
}

func TestService_MakefileTargetRegex(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		line     string
		expected string
	}{
		{"build:", "build"},
		{"test: deps", "test"},
		{"all: build test", "all"},
		{"my-target:", "my-target"},
		{"my_target:", "my_target"},
		{"target123:", "target123"},
		{".PHONY: all", ""}, // Starts with .
		{"\tcommand", ""},   // Tab-indented
		{"# comment:", ""},  // Comment
		{"VAR = value", ""}, // Variable assignment
		{":=", ""},          // Empty target
		{"123target:", ""},  // Starts with number
		{"-target:", ""},    // Starts with hyphen
	}

	for _, tc := range testCases {
		t.Run(tc.line, func(t *testing.T) {
			matches := makefileTargetRegex.FindStringSubmatch(tc.line)
			if tc.expected == "" {
				assert.Empty(t, matches)
			} else {
				require.Len(t, matches, 2)
				assert.Equal(t, tc.expected, matches[1])
			}
		})
	}
}

func TestService_ContextCancellation(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	svc, err := NewService(db, Options{TTL: 1 * time.Millisecond})
	require.NoError(t, err)
	defer svc.Close()

	repoRoot := createTestRepo(t)

	// Create large Makefile
	var content string
	for i := 0; i < 1000; i++ {
		content += "target" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + ":\n\techo test\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "Makefile"), []byte(content), 0644))

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Discovery should complete (errors are logged but not propagated)
	// This is per spec: "Discovery errors must never fail daemon startup or crash the daemon"
	discovered, err := svc.Discover(ctx, repoRoot)
	// Either it returns an error or it succeeds with discovery=true
	// The behavior depends on timing - the context may be checked at different points
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	} else {
		assert.True(t, discovered) // Discovery ran (even if sources failed)
	}
}

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	assert.Equal(t, DefaultTimeout, opts.Timeout)
	assert.Equal(t, int64(DefaultMaxOutputBytes), opts.MaxOutputBytes)
	assert.Equal(t, DefaultTTL, opts.TTL)
	assert.NotNil(t, opts.Logger)
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, TaskKind("package.json"), KindPackageJSON)
	assert.Equal(t, TaskKind("makefile"), KindMakefile)
	assert.Equal(t, 500*time.Millisecond, DefaultTimeout)
	assert.Equal(t, 1<<20, DefaultMaxOutputBytes) // 1MB
	assert.Equal(t, 5*time.Minute, DefaultTTL)
}
