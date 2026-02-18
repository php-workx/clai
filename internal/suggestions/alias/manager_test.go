package alias

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_SetAndExpand(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
	})

	mgr.SetAliases(AliasMap{
		"gs": "git status",
		"gp": "git push",
	})

	result, expanded := mgr.Expand("gs --short")
	assert.True(t, expanded)
	assert.Equal(t, "git status --short", result)

	result, expanded = mgr.Expand("docker run")
	assert.False(t, expanded)
	assert.Equal(t, "docker run", result)
}

func TestManager_SetAndRender(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
	})

	mgr.SetAliases(AliasMap{
		"gs": "git status",
		"gp": "git push",
	})

	result := mgr.Render("git status --short")
	assert.Equal(t, "gs --short", result)

	result = mgr.Render("git push origin main")
	assert.Equal(t, "gp origin main", result)

	result = mgr.Render("docker run nginx")
	assert.Equal(t, "docker run nginx", result)
}

func TestManager_Aliases(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
	})

	original := AliasMap{
		"gs": "git status",
		"gp": "git push",
	}
	mgr.SetAliases(original)

	// Aliases() returns a copy
	aliases := mgr.Aliases()
	assert.Equal(t, original, aliases)

	// Mutating the copy shouldn't affect the manager
	aliases["new"] = "new value"
	assert.NotContains(t, mgr.Aliases(), "new")
}

func TestManager_SaveAndLoad(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create a manager and save aliases
	mgr1 := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
	})
	mgr1.SetAliases(AliasMap{
		"gs": "git status",
		"ll": "ls -la",
	})
	err := mgr1.store.SaveAliases(ctx, "test-sess", mgr1.Aliases())
	require.NoError(t, err)

	// Create a new manager and load from store
	mgr2 := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
	})
	err = mgr2.LoadFromStore(ctx)
	require.NoError(t, err)

	// Should have the same aliases
	assert.Equal(t, mgr1.Aliases(), mgr2.Aliases())

	// Should be able to expand
	result, expanded := mgr2.Expand("gs --short")
	assert.True(t, expanded)
	assert.Equal(t, "git status --short", result)

	// Should be able to render
	rendered := mgr2.Render("git status --short")
	assert.Equal(t, "gs --short", rendered)
}

func TestManager_ChainedExpansion(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID:         "test-sess",
		Shell:             "bash",
		DB:                db,
		MaxExpansionDepth: 5,
	})

	mgr.SetAliases(AliasMap{
		"l":  "ll",
		"ll": "ls -la",
	})

	result, expanded := mgr.Expand("l /tmp")
	assert.True(t, expanded)
	assert.Equal(t, "ls -la /tmp", result)
}

func TestManager_CycleDetection(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID:         "test-sess",
		Shell:             "bash",
		DB:                db,
		MaxExpansionDepth: 5,
	})

	mgr.SetAliases(AliasMap{
		"a": "b args",
		"b": "a args",
	})

	// Should not infinite loop
	result, expanded := mgr.Expand("a")
	assert.True(t, expanded)
	assert.Contains(t, result, "args")
}

func TestManager_EmptyInitialState(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
	})

	// Before any aliases are set, expand and render should be no-ops
	result, expanded := mgr.Expand("git status")
	assert.False(t, expanded)
	assert.Equal(t, "git status", result)

	rendered := mgr.Render("git status")
	assert.Equal(t, "git status", rendered)
}

func TestManager_DefaultMaxDepth(t *testing.T) {
	db := setupTestDB(t)
	mgr := NewManager(ManagerConfig{
		SessionID: "test-sess",
		Shell:     "bash",
		DB:        db,
		// MaxExpansionDepth defaults to 0 -> normalize.DefaultMaxAliasDepth (5)
	})

	mgr.SetAliases(AliasMap{
		"a": "b",
		"b": "c",
		"c": "d",
		"d": "echo hello",
	})

	result, expanded := mgr.Expand("a")
	assert.True(t, expanded)
	assert.Equal(t, "echo hello", result)
}
