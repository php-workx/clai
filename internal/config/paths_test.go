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

	if paths.BaseDir == "" {
		t.Error("BaseDir is empty")
	}

	// BaseDir should be absolute
	if !filepath.IsAbs(paths.BaseDir) {
		t.Errorf("BaseDir should be absolute: %s", paths.BaseDir)
	}

	// BaseDir should contain "clai"
	if !strings.Contains(paths.BaseDir, "clai") {
		t.Errorf("BaseDir should contain 'clai': %s", paths.BaseDir)
	}
}

func TestDefaultPaths_CLAIHome(t *testing.T) {
	// Save original env var
	origClaiHome := os.Getenv("CLAI_HOME")

	defer func() {
		if origClaiHome != "" {
			os.Setenv("CLAI_HOME", origClaiHome)
		} else {
			os.Unsetenv("CLAI_HOME")
		}
	}()

	// Set custom CLAI_HOME
	os.Setenv("CLAI_HOME", "/custom/clai/home")

	paths := DefaultPaths()

	if paths.BaseDir != "/custom/clai/home" {
		t.Errorf("BaseDir should respect CLAI_HOME: %s", paths.BaseDir)
	}
}

func TestPaths_DerivedDirs(t *testing.T) {
	paths := &Paths{BaseDir: "/test/clai"}

	tests := []struct {
		name     string
		got      string
		wantBase string
	}{
		{"CacheDir", paths.CacheDir(), "/test/clai/cache"},
		{"LogDir", paths.LogDir(), "/test/clai/logs"},
		{"HooksDir", paths.HooksDir(), "/test/clai/hooks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.wantBase {
				t.Errorf("%s = %s, want %s", tt.name, tt.got, tt.wantBase)
			}
		})
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
		BaseDir: filepath.Join(tmpDir, "clai"),
	}

	// Ensure directories
	err = paths.EnsureDirectories()
	if err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	// Check directories exist
	dirs := []string{
		paths.BaseDir,
		paths.LogDir(),
		paths.HooksDir(),
		paths.CacheDir(),
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

func TestDefaultPaths_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	paths := DefaultPaths()

	// On Windows, should use APPDATA
	if !strings.Contains(paths.BaseDir, "AppData") && !strings.Contains(paths.BaseDir, "Roaming") {
		t.Errorf("On Windows, BaseDir should be in AppData: %s", paths.BaseDir)
	}
}
