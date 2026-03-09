package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfPreCommit skips the test if running inside a pre-commit hook.
// Pre-commit hooks hold git locks that interfere with our git tests.
func skipIfPreCommit(t *testing.T) {
	t.Helper()
	// Check for common pre-commit environment indicators
	if os.Getenv("PRE_COMMIT") != "" ||
		os.Getenv("GIT_DIR") != "" ||
		os.Getenv("GIT_INDEX_FILE") != "" {
		t.Skip("skipping: test doesn't work reliably during pre-commit hooks")
	}
}

// createTestRepo creates a temporary git repository for testing.
// The repository is fully isolated from the parent git environment.
func createTestRepo(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-git-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	// Initialize git repo with explicit env to isolate from parent repos
	runCmdWithEnv(t, dir, nil, "git", "init")
	runCmdWithEnv(t, dir, nil, "git", "config", "user.email", "test@test.com")
	runCmdWithEnv(t, dir, nil, "git", "config", "user.name", "Test User")

	// Create initial commit
	testFile := filepath.Join(dir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)
	runCmdWithEnv(t, dir, nil, "git", "add", ".")
	runCmdWithEnv(t, dir, nil, "git", "commit", "-m", "initial")

	return dir
}

// runCmd runs a command and fails the test if it errors.
func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	runCmdWithEnv(t, dir, nil, name, args...)
}

// runCmdWithEnv runs a command with optional environment variables.
// It sets GIT_DIR and GIT_WORK_TREE to isolate git operations from parent repos.
func runCmdWithEnv(t *testing.T, dir string, extraEnv []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Set environment to isolate git from parent repository
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+filepath.Join(dir, ".git"),
		"GIT_WORK_TREE="+dir,
		"GIT_CONFIG_NOSYSTEM=1",                      // Don't read system config
		"HOME="+dir,                                  // Don't read user config from real home
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir), // Don't look above temp dir
	)
	if extraEnv != nil {
		cmd.Env = append(cmd.Env, extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Skip test if git lock is held (e.g., during pre-commit hooks)
		if strings.Contains(string(out), "index.lock") || strings.Contains(string(out), "config: File exists") {
			t.Skipf("skipping: git lock held (likely pre-commit hook running): %s", out)
		}
		t.Fatalf("command %q failed: %v\noutput: %s", name, err, out)
	}
}

func TestComputeContext_GitRepo(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)

	ctx := computeContext(dir)

	assert.True(t, ctx.IsRepo)
	// RepoRoot is canonicalized, so compare canonical paths
	canonicalDir := canonicalizePath(dir)
	assert.Equal(t, canonicalDir, ctx.RepoRoot)
	assert.NotEmpty(t, ctx.Branch)
	assert.False(t, ctx.Dirty)
	assert.NotEmpty(t, ctx.RepoKey)
}

func TestComputeContext_NotGitRepo(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir, err := os.MkdirTemp("", "clai-nogit-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// Set GIT_CEILING_DIRECTORIES to prevent git from finding parent repos
	origCeiling := os.Getenv("GIT_CEILING_DIRECTORIES")
	os.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	defer func() {
		if origCeiling != "" {
			os.Setenv("GIT_CEILING_DIRECTORIES", origCeiling)
		} else {
			os.Unsetenv("GIT_CEILING_DIRECTORIES")
		}
	}()

	ctx := computeContext(dir)

	assert.False(t, ctx.IsRepo)
	assert.Empty(t, ctx.RepoRoot)
	assert.Empty(t, ctx.Branch)
	assert.Empty(t, ctx.RepoKey)
}

func TestComputeContext_DirtyRepo(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)

	// Create an uncommitted change
	testFile := filepath.Join(dir, "dirty.txt")
	err := os.WriteFile(testFile, []byte("dirty content"), 0644)
	require.NoError(t, err)

	ctx := computeContext(dir)

	assert.True(t, ctx.IsRepo)
	assert.True(t, ctx.Dirty)
}

func TestComputeContext_WithRemote(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)

	// Add a remote
	runCmd(t, dir, "git", "remote", "add", "origin", "https://github.com/test/repo.git")

	ctx := computeContext(dir)

	assert.True(t, ctx.IsRepo)
	assert.Equal(t, "https://github.com/test/repo.git", ctx.RemoteURL)
	assert.NotEmpty(t, ctx.RepoKey)
}

func TestComputeRepoKey(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		repoRoot  string
	}{
		{
			name:      "with remote",
			remoteURL: "https://github.com/test/repo.git",
			repoRoot:  "/home/user/project",
		},
		{
			name:      "without remote",
			remoteURL: "",
			repoRoot:  "/home/user/local-project",
		},
		{
			name:      "ssh remote",
			remoteURL: "git@github.com:test/repo.git",
			repoRoot:  "/home/user/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := computeRepoKey(tt.remoteURL, tt.repoRoot)

			assert.NotEmpty(t, key)
			assert.Len(t, key, 64) // SHA256 hex = 64 chars

			// Same inputs should produce same key (deterministic)
			key2 := computeRepoKey(tt.remoteURL, tt.repoRoot)
			assert.Equal(t, key, key2)

			// Different inputs should produce different keys
			key3 := computeRepoKey(tt.remoteURL+"x", tt.repoRoot)
			assert.NotEqual(t, key, key3)
		})
	}
}

func TestComputeRepoKey_CaseInsensitive(t *testing.T) {
	// Remote URLs should be case-insensitive per spec
	key1 := computeRepoKey("https://github.com/Test/Repo.git", "/home/user/project")
	key2 := computeRepoKey("https://github.com/test/repo.git", "/home/user/project")

	assert.Equal(t, key1, key2, "repo keys should be case-insensitive for remote URL")
}

func TestContextCache_Get(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)
	cache := NewContextCache(DefaultCacheTTL)

	// First call should compute
	ctx1 := cache.Get(dir, false)
	assert.True(t, ctx1.IsRepo)

	// Second call should return cached value
	ctx2 := cache.Get(dir, false)
	assert.Equal(t, ctx1, ctx2)
	assert.Equal(t, 1, cache.Size())
}

func TestContextCache_Expiration(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)

	// Create cache with very short TTL
	cache := NewContextCache(1 * time.Millisecond)

	// Set up time control
	now := time.Now()
	cache.nowFunc = func() time.Time { return now }

	// Get first value
	ctx1 := cache.Get(dir, false)
	assert.True(t, ctx1.IsRepo)

	// Advance time past TTL
	now = now.Add(10 * time.Millisecond)

	// Should recompute
	ctx2 := cache.Get(dir, false)
	assert.True(t, ctx2.IsRepo)
}

func TestContextCache_ForceRefresh(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)
	cache := NewContextCache(1 * time.Hour) // Long TTL

	// First call
	ctx1 := cache.Get(dir, false)
	assert.True(t, ctx1.IsRepo)
	assert.False(t, ctx1.Dirty)

	// Make repo dirty
	testFile := filepath.Join(dir, "dirty.txt")
	err := os.WriteFile(testFile, []byte("dirty"), 0644)
	require.NoError(t, err)

	// Without force refresh, should return cached (clean)
	ctx2 := cache.Get(dir, false)
	assert.False(t, ctx2.Dirty)

	// With force refresh, should return fresh (dirty)
	ctx3 := cache.Get(dir, true)
	assert.True(t, ctx3.Dirty)
}

func TestContextCache_Invalidate(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)
	cache := NewContextCache(1 * time.Hour)

	// Populate cache
	cache.Get(dir, false)
	assert.Equal(t, 1, cache.Size())

	// Invalidate
	cache.Invalidate(dir)
	assert.Equal(t, 0, cache.Size())
}

func TestContextCache_InvalidateAll(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir1 := createTestRepo(t)
	dir2 := createTestRepo(t)
	cache := NewContextCache(1 * time.Hour)

	// Populate cache with multiple entries
	cache.Get(dir1, false)
	cache.Get(dir2, false)
	assert.Equal(t, 2, cache.Size())

	// Invalidate all
	cache.InvalidateAll()
	assert.Equal(t, 0, cache.Size())
}

func TestContextCache_Cleanup(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir1 := createTestRepo(t)
	dir2 := createTestRepo(t)

	cache := NewContextCache(100 * time.Millisecond)

	// Set up time control
	now := time.Now()
	cache.nowFunc = func() time.Time { return now }

	// Add first entry
	cache.Get(dir1, false)

	// Advance time
	now = now.Add(50 * time.Millisecond)

	// Add second entry (will expire later)
	cache.Get(dir2, false)

	// Advance time past first entry's expiration
	now = now.Add(100 * time.Millisecond)

	// Cleanup should remove first entry but keep second
	cache.Cleanup()
	assert.Equal(t, 1, cache.Size())
}

func TestContextCache_ConcurrentAccess(t *testing.T) {
	skipIfPreCommit(t)
	t.Parallel()

	dir := createTestRepo(t)
	cache := NewContextCache(DefaultCacheTTL)

	// Run concurrent gets
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			ctx := cache.Get(dir, false)
			assert.True(t, ctx.IsRepo)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestIsGitCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git status", true},
		{"git commit -m 'test'", true},
		{"git push origin main", true},
		{"/usr/bin/git log", true},
		{"gh pr create", true},
		{"gh issue list", true},
		{"ls -la", false},
		{"npm install", false},
		{"make build", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := IsGitCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("IsGitCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestNewContextCache_DefaultTTL(t *testing.T) {
	cache := NewContextCache(0)
	assert.Equal(t, DefaultCacheTTL, cache.ttl)

	cache2 := NewContextCache(-1)
	assert.Equal(t, DefaultCacheTTL, cache2.ttl)
}
