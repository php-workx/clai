package dirscope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeScopeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cwd      string
		repoRoot string
		maxDepth int
		wantKey  bool // true if we expect a non-empty key
	}{
		{
			name:     "repo root returns key",
			cwd:      "/home/user/project",
			repoRoot: "/home/user/project",
			maxDepth: 3,
			wantKey:  true,
		},
		{
			name:     "subdirectory returns key",
			cwd:      "/home/user/project/src/main",
			repoRoot: "/home/user/project",
			maxDepth: 3,
			wantKey:  true,
		},
		{
			name:     "deep path is truncated",
			cwd:      "/home/user/project/a/b/c/d/e",
			repoRoot: "/home/user/project",
			maxDepth: 3,
			wantKey:  true,
		},
		{
			name:     "same truncated path gives same key",
			cwd:      "/home/user/project/a/b/c/d",
			repoRoot: "/home/user/project",
			maxDepth: 3,
			wantKey:  true,
		},
		{
			name:     "outside repo returns empty",
			cwd:      "/home/other",
			repoRoot: "/home/user/project",
			maxDepth: 3,
			wantKey:  false,
		},
		{
			name:     "empty cwd returns empty",
			cwd:      "",
			repoRoot: "/home/user/project",
			maxDepth: 3,
			wantKey:  false,
		},
		{
			name:     "empty repoRoot returns empty",
			cwd:      "/home/user/project",
			repoRoot: "",
			maxDepth: 3,
			wantKey:  false,
		},
		{
			name:     "zero maxDepth uses default",
			cwd:      "/home/user/project/src",
			repoRoot: "/home/user/project",
			maxDepth: 0,
			wantKey:  true,
		},
		{
			name:     "negative maxDepth uses default",
			cwd:      "/home/user/project/src",
			repoRoot: "/home/user/project",
			maxDepth: -1,
			wantKey:  true,
		},
		{
			name:     "maxDepth 1 truncates to one level",
			cwd:      "/home/user/project/src/main/java",
			repoRoot: "/home/user/project",
			maxDepth: 1,
			wantKey:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := ComputeScopeKey(tt.cwd, tt.repoRoot, tt.maxDepth)
			if tt.wantKey {
				assert.NotEmpty(t, key)
				assert.True(t, IsDirScope(key), "key should have dir: prefix")
			} else {
				assert.Empty(t, key)
			}
		})
	}

	// Test that same truncated path gives same key
	t.Run("deterministic for same truncated path", func(t *testing.T) {
		t.Parallel()
		key1 := ComputeScopeKey("/home/user/project/a/b/c/d", "/home/user/project", 3)
		key2 := ComputeScopeKey("/home/user/project/a/b/c/e", "/home/user/project", 3)
		assert.Equal(t, key1, key2, "paths truncated to same prefix should have same key")
	})

	// Test that different paths give different keys
	t.Run("different paths give different keys", func(t *testing.T) {
		t.Parallel()
		key1 := ComputeScopeKey("/home/user/project/src", "/home/user/project", 3)
		key2 := ComputeScopeKey("/home/user/project/test", "/home/user/project", 3)
		assert.NotEqual(t, key1, key2, "different paths should have different keys")
	})

	// Test that root and subdir give different keys
	t.Run("root and subdir give different keys", func(t *testing.T) {
		t.Parallel()
		key1 := ComputeScopeKey("/home/user/project", "/home/user/project", 3)
		key2 := ComputeScopeKey("/home/user/project/src", "/home/user/project", 3)
		assert.NotEqual(t, key1, key2, "root and subdir should have different keys")
	})
}

func TestTruncatePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		relPath  string
		expected string
		maxDepth int
	}{
		{name: "empty", relPath: "", maxDepth: 3, expected: ""},
		{name: "dot", relPath: ".", maxDepth: 3, expected: ""},
		{name: "single level", relPath: "src", maxDepth: 3, expected: "src"},
		{name: "two levels", relPath: "src/main", maxDepth: 3, expected: "src/main"},
		{name: "three levels", relPath: "src/main/java", maxDepth: 3, expected: "src/main/java"},
		{name: "four levels truncated", relPath: "src/main/java/com", maxDepth: 3, expected: "src/main/java"},
		{name: "max depth 1", relPath: "src/main/java", maxDepth: 1, expected: "src"},
		{name: "max depth 10", relPath: "a/b/c", maxDepth: 10, expected: "a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := truncatePath(tt.relPath, tt.maxDepth)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDirScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scope    string
		expected bool
	}{
		{name: "dir scope", scope: "dir:abc123", expected: true},
		{name: "global scope", scope: "global", expected: false},
		{name: "repo scope", scope: "sha256:abc", expected: false},
		{name: "empty", scope: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, IsDirScope(tt.scope))
		})
	}
}
