package discover

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writePlaybook writes a tasks.yaml file to the given directory and returns the path.
func writePlaybook(t *testing.T, dir, content string) string {
	t.Helper()
	claiDir := filepath.Join(dir, ".clai")
	require.NoError(t, os.MkdirAll(claiDir, 0755))
	path := filepath.Join(claiDir, "tasks.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestDiscoverWithPlaybook(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pbPath := writePlaybook(t, dir, `
tasks:
  - name: "build"
    command: "make build"
    priority: high
    tags: ["go"]
  - name: "test"
    command: "make test"
    tags: ["go", "testing"]
`)

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		PlaybookPath: pbPath,
		Limit:        10,
	})

	require.True(t, len(candidates) >= 2, "should have at least 2 playbook candidates")

	// Playbook candidates should come first
	assert.Equal(t, SourcePlaybook, candidates[0].Source)
	assert.Equal(t, SourcePlaybook, candidates[1].Source)

	// Verify playbook commands are present
	cmds := make(map[string]bool)
	for _, c := range candidates {
		cmds[c.Command] = true
	}
	assert.True(t, cmds["make build"])
	assert.True(t, cmds["make test"])
}

func TestDiscoverWithProjectTypes(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		ProjectTypes: []string{"go"},
		Limit:        10,
	})

	require.True(t, len(candidates) > 0)

	// First candidates should be from project_type
	assert.Equal(t, SourceProjectType, candidates[0].Source)

	// Check that Go-specific commands are present
	cmds := make(map[string]bool)
	for _, c := range candidates {
		cmds[c.Command] = true
	}
	assert.True(t, cmds["go test ./..."])
	assert.True(t, cmds["go build ./..."])
}

func TestDiscoverWithToolCommonFallback(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		Limit: 10,
	})

	require.True(t, len(candidates) > 0)

	// All candidates should be from tool_common
	for _, c := range candidates {
		assert.Equal(t, SourceToolCommon, c.Source)
	}

	// Check universal commands
	cmds := make(map[string]bool)
	for _, c := range candidates {
		cmds[c.Command] = true
	}
	assert.True(t, cmds["git status"])
	assert.True(t, cmds["ls"])
	assert.True(t, cmds["pwd"])
}

func TestDiscoverCooldownEnforcement(t *testing.T) {
	t.Parallel()

	engine := NewEngine()

	// Set a fixed time for deterministic testing
	var currentTime int64 = 1000000
	engine.nowFn = func() int64 { return currentTime }

	config := DiscoverConfig{
		ProjectTypes: []string{"go"},
		Limit:        10,
		CooldownMs:   5000, // 5 second cooldown
	}

	// First call should return candidates
	first := engine.Discover(context.Background(), config)
	require.True(t, len(first) > 0)

	// Immediately request again -- all should be filtered by cooldown
	second := engine.Discover(context.Background(), config)
	assert.Len(t, second, 0, "all candidates should be filtered by cooldown")

	// Advance time past cooldown
	currentTime += 6000 // 6 seconds later
	third := engine.Discover(context.Background(), config)
	assert.True(t, len(third) > 0, "candidates should be available after cooldown expires")
}

func TestDiscoverLimitEnforcement(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		ProjectTypes: []string{"go", "node", "python"},
		Limit:        3,
	})

	assert.Len(t, candidates, 3)
}

func TestDiscoverEmptySession(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		// No project types, no playbook
		Limit: 10,
	})

	// Should still return tool-common commands
	require.True(t, len(candidates) > 0)
	for _, c := range candidates {
		assert.Equal(t, SourceToolCommon, c.Source)
	}
}

func TestDiscoverSourcePriority(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pbPath := writePlaybook(t, dir, `
tasks:
  - name: "deploy"
    command: "kubectl apply"
    tags: ["k8s"]
`)

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		PlaybookPath: pbPath,
		ProjectTypes: []string{"go"},
		Limit:        20,
	})

	require.True(t, len(candidates) > 2)

	// Verify source ordering: playbook first, then project_type, then tool_common
	lastSource := SourcePlaybook
	for _, c := range candidates {
		if c.Source != lastSource {
			// Source changed -- verify it only goes forward in priority
			assert.True(t, sourceOrder(c.Source) >= sourceOrder(lastSource),
				"source ordering should be playbook -> project_type -> tool_common, got %s after %s",
				c.Source, lastSource)
			lastSource = c.Source
		}
	}
}

func TestDiscoverDeduplication(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pbPath := writePlaybook(t, dir, `
tasks:
  - name: "test"
    command: "make test"
  - name: "build"
    command: "make build"
`)

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		PlaybookPath: pbPath,
		ProjectTypes: []string{"make"}, // "make" priors also include "make test" and "make build"
		Limit:        20,
	})

	// Count occurrences of each command
	cmdCounts := make(map[string]int)
	for _, c := range candidates {
		cmdCounts[c.Command]++
	}

	// No duplicates
	for cmd, count := range cmdCounts {
		assert.Equal(t, 1, count, "command %q should appear exactly once", cmd)
	}
}

func TestDiscoverDefaultLimit(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		ProjectTypes: []string{"go", "node", "python", "rust", "docker"},
		// Limit not set (zero) -> should default to 5
	})

	assert.LessOrEqual(t, len(candidates), 5)
}

func TestDiscoverResetCooldowns(t *testing.T) {
	t.Parallel()

	engine := NewEngine()

	var currentTime int64 = 1000000
	engine.nowFn = func() int64 { return currentTime }

	config := DiscoverConfig{
		ProjectTypes: []string{"go"},
		Limit:        10,
		CooldownMs:   60000, // Very long cooldown
	}

	// First call populates cooldowns
	first := engine.Discover(context.Background(), config)
	require.True(t, len(first) > 0)

	// Cooldown blocks second call
	second := engine.Discover(context.Background(), config)
	assert.Len(t, second, 0)

	// Reset cooldowns
	engine.ResetCooldowns()

	// Should be able to discover again
	third := engine.Discover(context.Background(), config)
	assert.True(t, len(third) > 0)
}

func TestDiscoverInvalidPlaybookPath(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		PlaybookPath: "/nonexistent/path/tasks.yaml",
		ProjectTypes: []string{"go"},
		Limit:        10,
	})

	// Should still return project-type and tool-common candidates
	require.True(t, len(candidates) > 0)

	// No playbook candidates
	for _, c := range candidates {
		assert.NotEqual(t, SourcePlaybook, c.Source)
	}
}

func TestDiscoverMultipleProjectTypes(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	candidates := engine.Discover(context.Background(), DiscoverConfig{
		ProjectTypes: []string{"go", "docker"},
		Limit:        20,
	})

	// Should have commands from both Go and Docker
	cmds := make(map[string]bool)
	for _, c := range candidates {
		cmds[c.Command] = true
	}

	assert.True(t, cmds["go test ./..."], "should include Go commands")
	assert.True(t, cmds["docker ps"], "should include Docker commands")
}

func TestProjectTypePriorsCompleteness(t *testing.T) {
	t.Parallel()

	// Verify all expected project types have priors
	expectedTypes := []string{"go", "node", "python", "rust", "docker"}
	for _, pt := range expectedTypes {
		priors, ok := projectTypePriors[pt]
		assert.True(t, ok, "should have priors for %q", pt)
		assert.True(t, len(priors) > 0, "priors for %q should not be empty", pt)
	}
}

func TestToolCommonCommandsNotEmpty(t *testing.T) {
	t.Parallel()

	assert.True(t, len(toolCommonCommands) > 0, "tool-common commands should not be empty")
}

func TestDiscoverConcurrentSafety(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	config := DiscoverConfig{
		ProjectTypes: []string{"go"},
		Limit:        5,
		CooldownMs:   100,
	}

	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = engine.Discover(context.Background(), config)
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
