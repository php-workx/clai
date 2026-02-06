package projecttype

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestDir creates a temporary directory for testing.
func createTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "clai-projecttype-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// touchFile creates an empty file at the given path.
func touchFile(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(path, []byte{}, 0644))
}

// mkDir creates a directory at the given path.
func mkDir(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0755))
}

func TestNewDetector_Defaults(t *testing.T) {
	t.Parallel()

	d := NewDetector(DetectorOptions{})
	assert.Equal(t, DefaultCacheTTL, d.opts.CacheTTL)
	assert.Equal(t, DefaultMaxScanDepth, d.opts.MaxScanDepth)
	assert.GreaterOrEqual(t, len(d.markers), len(builtinMarkers))
}

func TestNewDetector_CustomOptions(t *testing.T) {
	t.Parallel()

	d := NewDetector(DetectorOptions{
		CacheTTL:     30 * time.Second,
		MaxScanDepth: 5,
		ExtraMarkers: []Marker{
			{Name: "deno.json", ProjectType: "deno"},
		},
	})
	assert.Equal(t, 30*time.Second, d.opts.CacheTTL)
	assert.Equal(t, 5, d.opts.MaxScanDepth)
	assert.Equal(t, len(builtinMarkers)+1, len(d.markers))
}

func TestDetect_EmptyCwd(t *testing.T) {
	t.Parallel()

	d := NewDetector(DetectorOptions{})
	types := d.Detect("")
	assert.Nil(t, types)
}

func TestDetect_EmptyDir(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Nil(t, types)
}

func TestDetect_GoProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "go")
}

func TestDetect_RustProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "Cargo.toml"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "rust")
}

func TestDetect_NodeProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "package.json"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "node")
}

func TestDetect_PythonProject_Pyproject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "pyproject.toml"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "python")
}

func TestDetect_PythonProject_SetupPy(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "setup.py"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "python")
}

func TestDetect_RubyProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "Gemfile"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "ruby")
}

func TestDetect_JavaProject_Maven(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "pom.xml"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "java")
}

func TestDetect_JavaProject_Gradle(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "build.gradle"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "java")
}

func TestDetect_JavaProject_GradleKts(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "build.gradle.kts"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "java")
}

func TestDetect_CppProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "CMakeLists.txt"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "cpp")
}

func TestDetect_MakeProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "Makefile"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "make")
}

func TestDetect_DockerProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "Dockerfile"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "docker")
}

func TestDetect_TerraformProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	mkDir(t, filepath.Join(dir, ".terraform"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "terraform")
}

func TestDetect_NixProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "flake.nix"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "nix")
}

func TestDetect_HaskellProject(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "myproject.cabal"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Contains(t, types, "haskell")
}

func TestDetect_MultipleTypes(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))
	touchFile(t, filepath.Join(dir, "Makefile"))
	touchFile(t, filepath.Join(dir, "Dockerfile"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Len(t, types, 3)
	assert.Contains(t, types, "go")
	assert.Contains(t, types, "make")
	assert.Contains(t, types, "docker")
}

func TestDetect_DuplicateTypesDeduped(t *testing.T) {
	t.Parallel()

	// Both pyproject.toml and setup.py map to "python"
	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "pyproject.toml"))
	touchFile(t, filepath.Join(dir, "setup.py"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)

	pythonCount := 0
	for _, tp := range types {
		if tp == "python" {
			pythonCount++
		}
	}
	assert.Equal(t, 1, pythonCount, "python should appear only once")
}

func TestDetect_ScanUpward(t *testing.T) {
	t.Parallel()

	root := createTestDir(t)
	touchFile(t, filepath.Join(root, "go.mod"))

	// Create a subdirectory; detection from subdir should find go.mod in parent
	subdir := filepath.Join(root, "cmd", "myapp")
	mkDir(t, subdir)

	d := NewDetector(DetectorOptions{})
	types := d.Detect(subdir)
	assert.Contains(t, types, "go")
}

func TestDetect_ScanUpwardMaxDepth(t *testing.T) {
	t.Parallel()

	root := createTestDir(t)
	touchFile(t, filepath.Join(root, "go.mod"))

	// Create nested subdirectory deeper than MaxScanDepth=2
	deep := filepath.Join(root, "a", "b", "c")
	mkDir(t, deep)

	d := NewDetector(DetectorOptions{MaxScanDepth: 2})
	types := d.Detect(deep)
	// With MaxScanDepth=2, we scan deep and deep/.. (root/a/b and root/a)
	// but not root, so go.mod should NOT be found
	assert.NotContains(t, types, "go")
}

func TestDetect_ScanUpwardExactDepth(t *testing.T) {
	t.Parallel()

	root := createTestDir(t)
	touchFile(t, filepath.Join(root, "go.mod"))

	// Create nested subdirectory at exactly MaxScanDepth
	sub := filepath.Join(root, "a", "b")
	mkDir(t, sub)

	d := NewDetector(DetectorOptions{MaxScanDepth: 3})
	types := d.Detect(sub)
	// With MaxScanDepth=3, we scan sub, root/a, root -> should find go.mod
	assert.Contains(t, types, "go")
}

func TestDetect_OverrideFile(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	// Create go.mod (would normally detect "go")
	touchFile(t, filepath.Join(dir, "go.mod"))

	// Create .clai/project.yaml override
	overrideDir := filepath.Join(dir, ".clai")
	mkDir(t, overrideDir)
	overrideContent := "project_types:\n  - custom_go\n  - monorepo\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(overrideDir, "project.yaml"),
		[]byte(overrideContent),
		0644,
	))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	// Override should replace marker detection
	assert.Equal(t, []string{"custom_go", "monorepo"}, types)
	assert.NotContains(t, types, "go")
}

func TestDetect_OverrideFileInParent(t *testing.T) {
	t.Parallel()

	root := createTestDir(t)
	// Create override in root
	overrideDir := filepath.Join(root, ".clai")
	mkDir(t, overrideDir)
	overrideContent := "project_types:\n  - parent_type\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(overrideDir, "project.yaml"),
		[]byte(overrideContent),
		0644,
	))

	// Create subdirectory
	subdir := filepath.Join(root, "sub")
	mkDir(t, subdir)

	d := NewDetector(DetectorOptions{})
	types := d.Detect(subdir)
	assert.Equal(t, []string{"parent_type"}, types)
}

func TestDetect_OverrideFileEmpty(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))

	// Create .clai/project.yaml with empty project_types list
	overrideDir := filepath.Join(dir, ".clai")
	mkDir(t, overrideDir)
	overrideContent := "project_types: []\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(overrideDir, "project.yaml"),
		[]byte(overrideContent),
		0644,
	))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	// Empty override should fall through to marker detection
	assert.Contains(t, types, "go")
}

func TestDetect_OverrideFileInvalid(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))

	// Create invalid .clai/project.yaml
	overrideDir := filepath.Join(dir, ".clai")
	mkDir(t, overrideDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(overrideDir, "project.yaml"),
		[]byte("not: valid: yaml: {{{{"),
		0644,
	))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	// Invalid override should fall through to marker detection
	assert.Contains(t, types, "go")
}

func TestDetect_OverrideDeduplication(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	overrideDir := filepath.Join(dir, ".clai")
	mkDir(t, overrideDir)
	overrideContent := "project_types:\n  - go\n  - go\n  - rust\n  - rust\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(overrideDir, "project.yaml"),
		[]byte(overrideContent),
		0644,
	))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.Equal(t, []string{"go", "rust"}, types)
}

func TestDetect_CacheTTL(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))

	now := time.Now()
	d := NewDetector(DetectorOptions{CacheTTL: 1 * time.Hour})
	d.nowFunc = func() time.Time { return now }

	// First call: detects types
	types1 := d.Detect(dir)
	assert.Contains(t, types1, "go")
	assert.Equal(t, 1, d.Size())

	// Remove marker file
	os.Remove(filepath.Join(dir, "go.mod"))

	// Second call within TTL: returns cached result
	types2 := d.Detect(dir)
	assert.Contains(t, types2, "go") // Still cached

	// Advance time past TTL
	d.nowFunc = func() time.Time { return now.Add(2 * time.Hour) }

	// Third call after TTL: re-scans
	types3 := d.Detect(dir)
	assert.NotContains(t, types3, "go") // File removed, not cached anymore
}

func TestDetect_CacheInvalidate(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))

	d := NewDetector(DetectorOptions{CacheTTL: 1 * time.Hour})

	types := d.Detect(dir)
	assert.Contains(t, types, "go")
	assert.Equal(t, 1, d.Size())

	// Invalidate cache
	d.Invalidate(dir)
	assert.Equal(t, 0, d.Size())

	// Remove marker
	os.Remove(filepath.Join(dir, "go.mod"))

	// Re-detect: should not find go anymore
	types = d.Detect(dir)
	assert.NotContains(t, types, "go")
}

func TestDetect_InvalidateAll(t *testing.T) {
	t.Parallel()

	dir1 := createTestDir(t)
	dir2 := createTestDir(t)
	touchFile(t, filepath.Join(dir1, "go.mod"))
	touchFile(t, filepath.Join(dir2, "Cargo.toml"))

	d := NewDetector(DetectorOptions{})

	d.Detect(dir1)
	d.Detect(dir2)
	assert.Equal(t, 2, d.Size())

	d.InvalidateAll()
	assert.Equal(t, 0, d.Size())
}

func TestDetect_Cleanup(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))

	now := time.Now()
	d := NewDetector(DetectorOptions{CacheTTL: 1 * time.Second})
	d.nowFunc = func() time.Time { return now }

	d.Detect(dir)
	assert.Equal(t, 1, d.Size())

	// Advance time past TTL
	d.nowFunc = func() time.Time { return now.Add(5 * time.Second) }
	d.Cleanup()
	assert.Equal(t, 0, d.Size())
}

func TestDetect_ExtraMarkers(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "deno.json"))

	d := NewDetector(DetectorOptions{
		ExtraMarkers: []Marker{
			{Name: "deno.json", ProjectType: "deno"},
		},
	})
	types := d.Detect(dir)
	assert.Contains(t, types, "deno")
}

func TestDetect_TerraformDirNotFile(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	// Create .terraform as a file (not a directory) - should NOT detect terraform
	touchFile(t, filepath.Join(dir, ".terraform"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.NotContains(t, types, "terraform")
}

func TestDetect_DirectoryMarkerNotMatchedAsFile(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	// Create go.mod as a directory (not a file) - should NOT detect go
	mkDir(t, filepath.Join(dir, "go.mod"))

	d := NewDetector(DetectorOptions{})
	types := d.Detect(dir)
	assert.NotContains(t, types, "go")
}

func TestFormatProjectTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "go|docker|make", FormatProjectTypes([]string{"go", "docker", "make"}))
	assert.Equal(t, "go", FormatProjectTypes([]string{"go"}))
	assert.Equal(t, "", FormatProjectTypes(nil))
	assert.Equal(t, "", FormatProjectTypes([]string{}))
}

func TestParseProjectTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"go", "docker", "make"}, ParseProjectTypes("go|docker|make"))
	assert.Equal(t, []string{"go"}, ParseProjectTypes("go"))
	assert.Nil(t, ParseProjectTypes(""))
}

func TestParseProjectTypes_WithWhitespace(t *testing.T) {
	t.Parallel()

	result := ParseProjectTypes("go | docker | make")
	assert.Equal(t, []string{"go", "docker", "make"}, result)
}

func TestBuiltinMarkers(t *testing.T) {
	t.Parallel()

	markers := BuiltinMarkers()
	assert.Equal(t, len(builtinMarkers), len(markers))

	// Verify all expected project types are present
	typeSet := make(map[string]bool)
	for _, m := range markers {
		typeSet[m.ProjectType] = true
	}

	expected := []string{
		"go", "rust", "node", "python", "ruby", "java", "cpp",
		"make", "docker", "terraform", "nix", "haskell",
	}
	for _, e := range expected {
		assert.True(t, typeSet[e], "missing built-in marker for project type: %s", e)
	}
}

func TestDetect_StableOrder(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))
	touchFile(t, filepath.Join(dir, "Makefile"))
	touchFile(t, filepath.Join(dir, "Dockerfile"))
	touchFile(t, filepath.Join(dir, "package.json"))

	d := NewDetector(DetectorOptions{})

	// Detect multiple times, results should be stable
	types1 := d.Detect(dir)
	d.InvalidateAll()
	types2 := d.Detect(dir)

	sort.Strings(types1)
	sort.Strings(types2)
	assert.Equal(t, types1, types2)
}

func TestDetect_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	dir := createTestDir(t)
	touchFile(t, filepath.Join(dir, "go.mod"))
	touchFile(t, filepath.Join(dir, "Makefile"))

	d := NewDetector(DetectorOptions{})

	// Run concurrent detections
	done := make(chan []string, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- d.Detect(dir)
		}()
	}

	// All results should be the same
	var results [][]string
	for i := 0; i < 10; i++ {
		r := <-done
		results = append(results, r)
	}

	for _, r := range results {
		assert.Contains(t, r, "go")
		assert.Contains(t, r, "make")
	}
}

func TestParseOverride_ValidYAML(t *testing.T) {
	t.Parallel()

	data := []byte("project_types:\n  - go\n  - docker\n")
	types := parseOverride(data)
	assert.Equal(t, []string{"go", "docker"}, types)
}

func TestParseOverride_EmptyTypes(t *testing.T) {
	t.Parallel()

	data := []byte("project_types: []\n")
	types := parseOverride(data)
	assert.Nil(t, types)
}

func TestParseOverride_InvalidYAML(t *testing.T) {
	t.Parallel()

	data := []byte("{{invalid yaml}}")
	types := parseOverride(data)
	assert.Nil(t, types)
}

func TestParseOverride_WhitespaceInTypes(t *testing.T) {
	t.Parallel()

	data := []byte("project_types:\n  - \"  go  \"\n  - \" docker\"\n")
	types := parseOverride(data)
	assert.Equal(t, []string{"go", "docker"}, types)
}

func TestParseOverride_EmptyStringEntries(t *testing.T) {
	t.Parallel()

	data := []byte("project_types:\n  - go\n  - \"\"\n  - docker\n  - \"  \"\n")
	types := parseOverride(data)
	assert.Equal(t, []string{"go", "docker"}, types)
}
