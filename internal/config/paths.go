// Package config provides configuration management for clai.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths holds all the path configurations for clai.
// All paths are relative to the base directory (~/.clai on Unix, %APPDATA%\clai on Windows).
type Paths struct {
	// BaseDir is the root directory for all clai files (~/.clai)
	BaseDir string
}

// DefaultPaths returns the default paths.
// Unix: ~/.clai
// Windows: %APPDATA%\clai
func DefaultPaths() *Paths {
	// Check for CLAI_HOME override first (works on all platforms)
	if claiHome := os.Getenv("CLAI_HOME"); claiHome != "" {
		return &Paths{
			BaseDir: claiHome,
		}
	}

	home := homeDir()

	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return &Paths{
			BaseDir: filepath.Join(appData, "clai"),
		}
	}

	return &Paths{
		BaseDir: filepath.Join(home, ".clai"),
	}
}

// ConfigFile returns the path to the main configuration file.
func (p *Paths) ConfigFile() string {
	return filepath.Join(p.BaseDir, "config.yaml")
}

// DatabaseFile returns the path to the SQLite database.
func (p *Paths) DatabaseFile() string {
	return filepath.Join(p.BaseDir, "state.db")
}

// SocketFile returns the path to the Unix domain socket.
func (p *Paths) SocketFile() string {
	return filepath.Join(p.BaseDir, "clai.sock")
}

// PIDFile returns the path to the daemon PID file.
func (p *Paths) PIDFile() string {
	return filepath.Join(p.BaseDir, "clai.pid")
}

// LogDir returns the path to the log directory.
func (p *Paths) LogDir() string {
	return filepath.Join(p.BaseDir, "logs")
}

// LogFile returns the path to the daemon log file.
func (p *Paths) LogFile() string {
	return filepath.Join(p.LogDir(), "daemon.log")
}

// HooksDir returns the path to the hooks directory.
func (p *Paths) HooksDir() string {
	return filepath.Join(p.BaseDir, "hooks")
}

// WorkflowLogDir returns the path to the workflow log directory.
// Creates the directory if it doesn't exist.
func (p *Paths) WorkflowLogDir() string {
	dir := filepath.Join(p.BaseDir, "workflow-logs")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// CacheDir returns the path to the cache directory.
func (p *Paths) CacheDir() string {
	return filepath.Join(p.BaseDir, "cache")
}

// SuggestionFile returns the path to the suggestion cache file.
func (p *Paths) SuggestionFile() string {
	return filepath.Join(p.CacheDir(), "suggestion")
}

// LastOutputFile returns the path to the last output cache file.
func (p *Paths) LastOutputFile() string {
	return filepath.Join(p.CacheDir(), "last_output")
}

// EnsureDirectories creates all necessary directories.
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.BaseDir,
		p.LogDir(),
		p.HooksDir(),
		p.CacheDir(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}

// homeDir returns the user's home directory.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback
		if runtime.GOOS == "windows" {
			return os.Getenv("USERPROFILE")
		}
		return os.Getenv("HOME")
	}
	return home
}

// Deprecated compatibility methods - these now all return paths under BaseDir

// ConfigDir returns the base directory (for backward compatibility).
func (p *Paths) ConfigDir() string {
	return p.BaseDir
}

// DataDir returns the base directory (for backward compatibility).
func (p *Paths) DataDir() string {
	return p.BaseDir
}

// RuntimeDir returns the base directory (for backward compatibility).
func (p *Paths) RuntimeDir() string {
	return p.BaseDir
}
