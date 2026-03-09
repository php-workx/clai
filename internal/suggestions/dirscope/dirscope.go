// Package dirscope computes directory-scoped aggregate keys for the clai
// suggestions engine. It maps a working directory to a stable scope identifier
// by hashing the repo name and truncated relative path.
package dirscope

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// ScopePrefix is the prefix for directory scope identifiers.
const ScopePrefix = "dir:"

// DefaultMaxDepth is the default maximum directory depth for scope keys.
const DefaultMaxDepth = 3

// ComputeScopeKey computes a directory scope key from cwd relative to the git
// repo root. The key format is: dir:sha256(repoName/truncated_path).
// Returns "" if the cwd is outside the repo root or inputs are empty.
func ComputeScopeKey(cwd, repoRoot string, maxDepth int) string {
	if cwd == "" || repoRoot == "" {
		return ""
	}
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}

	cwd = filepath.Clean(cwd)
	repoRoot = filepath.Clean(repoRoot)

	relPath, err := filepath.Rel(repoRoot, cwd)
	if err != nil {
		return ""
	}

	// Reject paths outside the repo root
	if strings.HasPrefix(relPath, "..") {
		return ""
	}

	// Normalize "." to empty
	if relPath == "." {
		relPath = ""
	}

	truncated := truncatePath(relPath, maxDepth)
	repoName := filepath.Base(repoRoot)

	var hashInput string
	if truncated == "" {
		hashInput = repoName
	} else {
		hashInput = repoName + "/" + truncated
	}

	hash := sha256.Sum256([]byte(hashInput))
	return ScopePrefix + hex.EncodeToString(hash[:])
}

// truncatePath truncates a relative path to at most maxDepth components.
func truncatePath(relPath string, maxDepth int) string {
	if relPath == "" || relPath == "." {
		return ""
	}

	normalized := filepath.ToSlash(relPath)
	parts := strings.Split(normalized, "/")

	var filtered []string
	for _, p := range parts {
		if p != "" && p != "." {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) > maxDepth {
		filtered = filtered[:maxDepth]
	}
	return strings.Join(filtered, "/")
}

// IsDirScope returns true if the scope string is a directory scope identifier.
func IsDirScope(scope string) bool {
	return strings.HasPrefix(scope, ScopePrefix)
}
