// Package git provides git context computation for the clai suggestions engine.
// Git context (repo_root, branch, dirty status) is computed by the daemon
// and cached to avoid fork overhead in shell hooks.
//
// See spec Section 7 for details on repo identification and caching.
package git

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultCacheTTL is the default time-to-live for cached git context.
// Per spec Section 7.3: 1-3 seconds.
const DefaultCacheTTL = 2 * time.Second

// Context represents the git context for a directory.
type Context struct {
	RepoRoot  string `json:"repo_root,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"`
	Branch    string `json:"branch,omitempty"`
	RepoKey   string `json:"repo_key,omitempty"`
	Dirty     bool   `json:"dirty,omitempty"`
	IsRepo    bool   `json:"is_repo"`
}

// cacheEntry stores a cached git context with expiration.
type cacheEntry struct {
	context   *Context
	expiresAt time.Time
}

// ContextCache provides caching for git context lookups.
// It's safe for concurrent use.
type ContextCache struct {
	cache   map[string]*cacheEntry
	nowFunc func() time.Time
	ttl     time.Duration
	mu      sync.RWMutex
}

// NewContextCache creates a new git context cache with the given TTL.
func NewContextCache(ttl time.Duration) *ContextCache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &ContextCache{
		cache:   make(map[string]*cacheEntry),
		ttl:     ttl,
		nowFunc: time.Now,
	}
}

// Get returns the git context for the given directory.
// It returns a cached result if available and not expired.
// If forceRefresh is true, the cache is bypassed.
func (c *ContextCache) Get(cwd string, forceRefresh bool) *Context {
	if !forceRefresh {
		c.mu.RLock()
		entry, ok := c.cache[cwd]
		c.mu.RUnlock()

		if ok && c.nowFunc().Before(entry.expiresAt) {
			return entry.context
		}
	}

	// Compute fresh context
	ctx := computeContext(cwd)

	// Store in cache
	c.mu.Lock()
	c.cache[cwd] = &cacheEntry{
		context:   ctx,
		expiresAt: c.nowFunc().Add(c.ttl),
	}
	c.mu.Unlock()

	return ctx
}

// Invalidate removes the cached context for the given directory.
// Call this when a git-related command is executed.
func (c *ContextCache) Invalidate(cwd string) {
	c.mu.Lock()
	delete(c.cache, cwd)
	c.mu.Unlock()
}

// InvalidateAll clears the entire cache.
func (c *ContextCache) InvalidateAll() {
	c.mu.Lock()
	c.cache = make(map[string]*cacheEntry)
	c.mu.Unlock()
}

// Cleanup removes expired entries from the cache.
// This can be called periodically to prevent unbounded growth.
func (c *ContextCache) Cleanup() {
	now := c.nowFunc()
	c.mu.Lock()
	for k, v := range c.cache {
		if now.After(v.expiresAt) {
			delete(c.cache, k)
		}
	}
	c.mu.Unlock()
}

// Size returns the number of entries in the cache.
func (c *ContextCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// computeContext computes the git context for a directory.
// This is the non-cached computation that actually runs git commands.
func computeContext(cwd string) *Context {
	ctx := &Context{
		IsRepo: false,
	}

	// Get repo root
	repoRoot, err := gitRevParse(cwd, "--show-toplevel")
	if err != nil {
		return ctx // Not a git repo
	}

	ctx.IsRepo = true
	ctx.RepoRoot = canonicalizePath(repoRoot)

	// Get remote URL
	remoteURL, _ := gitConfig(cwd, "remote.origin.url")
	ctx.RemoteURL = strings.TrimSpace(remoteURL)

	// Get current branch
	branch, err := gitRevParse(cwd, "--abbrev-ref", "HEAD")
	if err == nil {
		ctx.Branch = strings.TrimSpace(branch)
	}

	// Check if dirty
	ctx.Dirty = isRepoDirty(cwd)

	// Compute repo key
	ctx.RepoKey = computeRepoKey(ctx.RemoteURL, ctx.RepoRoot)

	return ctx
}

// computeRepoKey computes the repo_key hash per spec Section 7.2.
// repo_key = SHA256(lower(remote_url) + "|" + canonical(repo_root))
// If no remote: SHA256("local|" + canonical(repo_root))
func computeRepoKey(remoteURL, repoRoot string) string {
	var input string
	if remoteURL != "" {
		input = strings.ToLower(remoteURL) + "|" + repoRoot
	} else {
		input = "local|" + repoRoot
	}

	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// gitRevParse runs `git rev-parse` with the given arguments.
func gitRevParse(cwd string, args ...string) (string, error) {
	allArgs := append([]string{"rev-parse"}, args...)
	return runGitCommand(cwd, allArgs...)
}

// gitConfig runs `git config` to get a config value.
func gitConfig(cwd, key string) (string, error) {
	return runGitCommand(cwd, "config", "--get", key)
}

// isRepoDirty returns true if the working tree has uncommitted changes.
// Uses `git status --porcelain` for efficiency.
func isRepoDirty(cwd string) bool {
	output, err := runGitCommand(cwd, "status", "--porcelain")
	if err != nil {
		return false // Assume clean on error
	}
	return strings.TrimSpace(output) != ""
}

// runGitCommand runs a git command in the specified directory.
func runGitCommand(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // git args are controlled by caller
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// canonicalizePath returns the canonical (physical) path.
// This resolves symlinks to avoid split history.
func canonicalizePath(path string) string {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return canonical
}

// IsGitCommand returns true if the command is a git-related command
// that should trigger cache invalidation.
func IsGitCommand(cmd string) bool {
	// Extract first word (the command)
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}

	firstWord := filepath.Base(fields[0])
	if firstWord == "git" {
		return true
	}

	// Also check for gh (GitHub CLI) which affects git state
	if firstWord == "gh" {
		return true
	}

	return false
}
