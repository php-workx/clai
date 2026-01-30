package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	paths := DefaultPaths()

	if paths.ConfigDir == "" {
		t.Error("ConfigDir is empty")
	}
	if paths.DataDir == "" {
		t.Error("DataDir is empty")
	}
	if paths.CacheDir == "" {
		t.Error("CacheDir is empty")
	}
	if paths.RuntimeDir == "" {
		t.Error("RuntimeDir is empty")
	}

	// All paths should be absolute
	if !filepath.IsAbs(paths.ConfigDir) {
		t.Errorf("ConfigDir should be absolute: %s", paths.ConfigDir)
	}
	if !filepath.IsAbs(paths.DataDir) {
		t.Errorf("DataDir should be absolute: %s", paths.DataDir)
	}
}

func TestDefaultPaths_XDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG test not applicable on Windows")
	}

	// Save original env vars
	origConfigHome := os.Getenv("XDG_CONFIG_HOME")
	origDataHome := os.Getenv("XDG_DATA_HOME")
	origCacheHome := os.Getenv("XDG_CACHE_HOME")

	defer func() {
		os.Setenv("XDG_CONFIG_HOME", origConfigHome)
		os.Setenv("XDG_DATA_HOME", origDataHome)
		os.Setenv("XDG_CACHE_HOME", origCacheHome)
	}()

	// Set custom XDG paths
	os.Setenv("XDG_CONFIG_HOME", "/custom/config")
	os.Setenv("XDG_DATA_HOME", "/custom/data")
	os.Setenv("XDG_CACHE_HOME", "/custom/cache")

	paths := DefaultPaths()

	if !strings.HasPrefix(paths.ConfigDir, "/custom/config") {
		t.Errorf("ConfigDir should respect XDG_CONFIG_HOME: %s", paths.ConfigDir)
	}
	if !strings.HasPrefix(paths.DataDir, "/custom/data") {
		t.Errorf("DataDir should respect XDG_DATA_HOME: %s", paths.DataDir)
	}
	if !strings.HasPrefix(paths.CacheDir, "/custom/cache") {
		t.Errorf("CacheDir should respect XDG_CACHE_HOME: %s", paths.CacheDir)
	}
}

func TestPaths_ConfigFile(t *testing.T) {
	paths := DefaultPaths()
	configFile := paths.ConfigFile()

	if !strings.HasSuffix(configFile, "config.yaml") {
		t.Errorf("ConfigFile should end with config.yaml: %s", configFile)
	}
	if !strings.Contains(configFile, "clai") {
		t.Errorf("ConfigFile should contain 'clai': %s", configFile)
	}
}

func TestPaths_DatabaseFile(t *testing.T) {
	paths := DefaultPaths()
	dbFile := paths.DatabaseFile()

	if !strings.HasSuffix(dbFile, "state.db") {
		t.Errorf("DatabaseFile should end with state.db: %s", dbFile)
	}
}

func TestPaths_SocketFile(t *testing.T) {
	paths := DefaultPaths()
	socketFile := paths.SocketFile()

	if !strings.HasSuffix(socketFile, "clai.sock") {
		t.Errorf("SocketFile should end with clai.sock: %s", socketFile)
	}
}

func TestPaths_PIDFile(t *testing.T) {
	paths := DefaultPaths()
	pidFile := paths.PIDFile()

	if !strings.HasSuffix(pidFile, "clai.pid") {
		t.Errorf("PIDFile should end with clai.pid: %s", pidFile)
	}
}

func TestPaths_LogDir(t *testing.T) {
	paths := DefaultPaths()
	logDir := paths.LogDir()

	if !strings.Contains(logDir, "logs") {
		t.Errorf("LogDir should contain 'logs': %s", logDir)
	}
}

func TestPaths_LogFile(t *testing.T) {
	paths := DefaultPaths()
	logFile := paths.LogFile()

	if !strings.HasSuffix(logFile, "daemon.log") {
		t.Errorf("LogFile should end with daemon.log: %s", logFile)
	}
}

func TestPaths_HooksDir(t *testing.T) {
	paths := DefaultPaths()
	hooksDir := paths.HooksDir()

	if !strings.Contains(hooksDir, "hooks") {
		t.Errorf("HooksDir should contain 'hooks': %s", hooksDir)
	}
}

func TestPaths_SuggestionFile(t *testing.T) {
	paths := DefaultPaths()
	suggFile := paths.SuggestionFile()

	if !strings.HasSuffix(suggFile, "suggestion") {
		t.Errorf("SuggestionFile should end with 'suggestion': %s", suggFile)
	}
}

func TestPaths_EnsureDirectories(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "clai-paths-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create custom paths pointing to temp directory
	paths := &Paths{
		ConfigDir:  filepath.Join(tmpDir, "config", "clai"),
		DataDir:    filepath.Join(tmpDir, "data", "clai"),
		CacheDir:   filepath.Join(tmpDir, "cache", "clai"),
		RuntimeDir: filepath.Join(tmpDir, "run", "clai"),
	}

	// Ensure directories
	err = paths.EnsureDirectories()
	if err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	// Check directories exist
	dirs := []string{
		paths.ConfigDir,
		paths.DataDir,
		paths.CacheDir,
		paths.RuntimeDir,
		paths.LogDir(),
		paths.HooksDir(),
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("Directory should exist: %s", dir)
		} else if !info.IsDir() {
			t.Errorf("Should be a directory: %s", dir)
		}
	}
}

func TestHomeDir(t *testing.T) {
	home := homeDir()

	if home == "" {
		t.Error("homeDir returned empty string")
	}
	if !filepath.IsAbs(home) {
		t.Errorf("homeDir should return absolute path: %s", home)
	}
}

func TestLegacyConfigDir(t *testing.T) {
	legacyDir := LegacyConfigDir()

	if !strings.HasSuffix(legacyDir, ".clai") {
		t.Errorf("LegacyConfigDir should end with .clai: %s", legacyDir)
	}
}

func TestLegacyCacheDir(t *testing.T) {
	legacyDir := LegacyCacheDir()

	if !strings.Contains(legacyDir, ".cache") || !strings.Contains(legacyDir, "clai") {
		t.Errorf("LegacyCacheDir should contain .cache and clai: %s", legacyDir)
	}
}
