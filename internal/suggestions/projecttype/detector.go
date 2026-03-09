// Package projecttype provides marker-based project type detection for the
// clai suggestions engine. It scans upward from cwd for marker files/directories
// to determine active project types (e.g., "go", "rust", "docker").
//
// Detection results are cached with a configurable TTL to avoid re-scanning
// on every command. A .clai/project.yaml override file can be used to manually
// specify project types.
//
// See spec appendix 20.1 for project type detection details.
package projecttype

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultCacheTTL is the default time-to-live for cached detection results.
const DefaultCacheTTL = 60 * time.Second

// DefaultMaxScanDepth limits how many parent directories we scan upward.
const DefaultMaxScanDepth = 10

// Marker defines a filesystem marker that indicates a project type.
type Marker struct {
	// Name is the filename or directory name to look for.
	Name string

	// ProjectType is the type string to return when this marker is found.
	ProjectType string

	// IsDir indicates this marker is a directory, not a file.
	IsDir bool

	// IsGlob indicates the Name contains a glob pattern (e.g., "*.cabal").
	IsGlob bool
}

// builtinMarkers defines the default set of marker files per spec appendix 20.1.
// Order does not matter; all markers are checked and multiple types can be detected.
var builtinMarkers = []Marker{
	{Name: "go.mod", ProjectType: "go"},
	{Name: "Cargo.toml", ProjectType: "rust"},
	{Name: "package.json", ProjectType: "node"},
	{Name: "pyproject.toml", ProjectType: "python"},
	{Name: "setup.py", ProjectType: "python"},
	{Name: "Gemfile", ProjectType: "ruby"},
	{Name: "pom.xml", ProjectType: "java"},
	{Name: "build.gradle", ProjectType: "java"},
	{Name: "build.gradle.kts", ProjectType: "java"},
	{Name: "CMakeLists.txt", ProjectType: "cpp"},
	{Name: "Makefile", ProjectType: "make"},
	{Name: "Dockerfile", ProjectType: "docker"},
	{Name: ".terraform", ProjectType: "terraform", IsDir: true},
	{Name: "flake.nix", ProjectType: "nix"},
	{Name: "*.cabal", ProjectType: "haskell", IsGlob: true},
}

// overrideConfig represents the .clai/project.yaml override file.
type overrideConfig struct {
	ProjectTypes []string `yaml:"project_types"`
}

// cacheEntry holds a cached detection result.
type cacheEntry struct {
	expiresAt time.Time
	types     []string
}

// DetectorOptions configures the project type detector.
type DetectorOptions struct {
	ExtraMarkers []Marker
	CacheTTL     time.Duration
	MaxScanDepth int
}

// Detector detects project types by scanning for marker files.
// It is safe for concurrent use.
type Detector struct {
	cache   map[string]*cacheEntry
	nowFunc func() time.Time
	markers []Marker
	opts    DetectorOptions
	mu      sync.RWMutex
}

// NewDetector creates a new project type detector.
func NewDetector(opts DetectorOptions) *Detector {
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = DefaultCacheTTL
	}
	if opts.MaxScanDepth <= 0 {
		opts.MaxScanDepth = DefaultMaxScanDepth
	}

	markers := make([]Marker, len(builtinMarkers), len(builtinMarkers)+len(opts.ExtraMarkers))
	copy(markers, builtinMarkers)
	markers = append(markers, opts.ExtraMarkers...)

	return &Detector{
		opts:    opts,
		markers: markers,
		cache:   make(map[string]*cacheEntry),
		nowFunc: time.Now,
	}
}

// Detect returns the detected project types for the given directory.
// Results are cached; subsequent calls within the TTL return cached results.
func (d *Detector) Detect(cwd string) []string {
	if cwd == "" {
		return nil
	}

	// Check cache first
	d.mu.RLock()
	entry, ok := d.cache[cwd]
	d.mu.RUnlock()

	if ok && d.nowFunc().Before(entry.expiresAt) {
		return entry.types
	}

	// Compute fresh result
	types := d.detect(cwd)

	// Store in cache
	d.mu.Lock()
	d.cache[cwd] = &cacheEntry{
		types:     types,
		expiresAt: d.nowFunc().Add(d.opts.CacheTTL),
	}
	d.mu.Unlock()

	return types
}

// detect performs the actual detection (uncached).
func (d *Detector) detect(cwd string) []string {
	// Check for .clai/project.yaml override first
	if override := d.readOverride(cwd); len(override) > 0 {
		return override
	}

	// Scan upward from cwd for marker files
	seen := make(map[string]bool)
	var types []string

	dir := filepath.Clean(cwd)
	for depth := 0; depth < d.opts.MaxScanDepth; depth++ {
		for _, marker := range d.markers {
			if seen[marker.ProjectType] {
				continue
			}

			if d.markerExists(dir, marker) {
				seen[marker.ProjectType] = true
				types = append(types, marker.ProjectType)
			}
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break // Reached filesystem root
		}
		dir = parent
	}

	return types
}

// markerExists checks whether a marker exists in the given directory.
func (d *Detector) markerExists(dir string, marker Marker) bool {
	if marker.IsGlob {
		pattern := filepath.Join(dir, marker.Name)
		matches, err := filepath.Glob(pattern)
		return err == nil && len(matches) > 0
	}

	path := filepath.Join(dir, marker.Name)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	if marker.IsDir {
		return info.IsDir()
	}
	return !info.IsDir()
}

// readOverride reads the .clai/project.yaml override file from the given directory
// and its parents (up to MaxScanDepth).
func (d *Detector) readOverride(cwd string) []string {
	dir := filepath.Clean(cwd)
	for depth := 0; depth < d.opts.MaxScanDepth; depth++ {
		overridePath := filepath.Join(dir, ".clai", "project.yaml")
		data, err := os.ReadFile(overridePath)
		if err == nil {
			types := parseOverride(data)
			if len(types) > 0 {
				return types
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

// parseOverride parses a .clai/project.yaml file.
func parseOverride(data []byte) []string {
	var cfg overrideConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	// Validate: only return non-empty, trimmed strings
	var types []string
	seen := make(map[string]bool)
	for _, t := range cfg.ProjectTypes {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}
	return types
}

// Invalidate removes the cached result for the given directory.
func (d *Detector) Invalidate(cwd string) {
	d.mu.Lock()
	delete(d.cache, cwd)
	d.mu.Unlock()
}

// InvalidateAll clears the entire cache.
func (d *Detector) InvalidateAll() {
	d.mu.Lock()
	d.cache = make(map[string]*cacheEntry)
	d.mu.Unlock()
}

// Cleanup removes expired entries from the cache.
func (d *Detector) Cleanup() {
	now := d.nowFunc()
	d.mu.Lock()
	for k, v := range d.cache {
		if now.After(v.expiresAt) {
			delete(d.cache, k)
		}
	}
	d.mu.Unlock()
}

// Size returns the number of entries in the cache.
func (d *Detector) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.cache)
}

// BuiltinMarkers returns a copy of the built-in markers for inspection/testing.
func BuiltinMarkers() []Marker {
	result := make([]Marker, len(builtinMarkers))
	copy(result, builtinMarkers)
	return result
}

// FormatProjectTypes formats a slice of project types as a pipe-separated
// string suitable for storage in the session table (e.g., "go|docker").
func FormatProjectTypes(types []string) string {
	return strings.Join(types, "|")
}

// ParseProjectTypes parses a pipe-separated project types string back
// into a slice.
func ParseProjectTypes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
