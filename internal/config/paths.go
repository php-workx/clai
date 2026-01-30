// Package config provides configuration management for clai.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths holds all the path configurations for clai.
type Paths struct {
	// ConfigDir is the directory for configuration files (~/.config/clai)
	ConfigDir string

	// DataDir is the directory for data files (~/.local/share/clai)
	DataDir string

	// CacheDir is the directory for cache files (~/.cache/clai)
	CacheDir string

	// RuntimeDir is the directory for runtime files like sockets and PID files
	RuntimeDir string
}

// DefaultPaths returns the default paths based on XDG Base Directory spec.
// On Windows, it uses %APPDATA% instead.
func DefaultPaths() *Paths {
	home := homeDir()

	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}

		return &Paths{
			ConfigDir:  filepath.Join(appData, "clai"),
			DataDir:    filepath.Join(localAppData, "clai"),
			CacheDir:   filepath.Join(localAppData, "clai", "cache"),
			RuntimeDir: filepath.Join(localAppData, "clai", "run"),
		}
	}

	// Unix-like systems follow XDG Base Directory spec
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}

	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(home, ".cache")
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		// Fallback to ~/.clai/run for runtime files
		runtimeDir = filepath.Join(home, ".clai", "run")
	} else {
		runtimeDir = filepath.Join(runtimeDir, "clai")
	}

	return &Paths{
		ConfigDir:  filepath.Join(configHome, "clai"),
		DataDir:    filepath.Join(dataHome, "clai"),
		CacheDir:   filepath.Join(cacheHome, "clai"),
		RuntimeDir: runtimeDir,
	}
}

// ConfigFile returns the path to the main configuration file.
func (p *Paths) ConfigFile() string {
	return filepath.Join(p.ConfigDir, "config.yaml")
}

// DatabaseFile returns the path to the SQLite database.
func (p *Paths) DatabaseFile() string {
	return filepath.Join(p.DataDir, "state.db")
}

// SocketFile returns the path to the Unix domain socket.
func (p *Paths) SocketFile() string {
	return filepath.Join(p.RuntimeDir, "clai.sock")
}

// PIDFile returns the path to the daemon PID file.
func (p *Paths) PIDFile() string {
	return filepath.Join(p.RuntimeDir, "clai.pid")
}

// LogDir returns the path to the log directory.
func (p *Paths) LogDir() string {
	return filepath.Join(p.DataDir, "logs")
}

// LogFile returns the path to the daemon log file.
func (p *Paths) LogFile() string {
	return filepath.Join(p.LogDir(), "daemon.log")
}

// HooksDir returns the path to the hooks directory.
func (p *Paths) HooksDir() string {
	return filepath.Join(p.DataDir, "hooks")
}

// SuggestionFile returns the path to the suggestion cache file.
func (p *Paths) SuggestionFile() string {
	return filepath.Join(p.CacheDir, "suggestion")
}

// LastOutputFile returns the path to the last output cache file.
func (p *Paths) LastOutputFile() string {
	return filepath.Join(p.CacheDir, "last_output")
}

// EnsureDirectories creates all necessary directories.
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.ConfigDir,
		p.DataDir,
		p.CacheDir,
		p.RuntimeDir,
		p.LogDir(),
		p.HooksDir(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
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

// LegacyConfigDir returns the legacy config directory (~/.clai).
// Used for migration from older versions.
func LegacyConfigDir() string {
	return filepath.Join(homeDir(), ".clai")
}

// LegacyCacheDir returns the legacy cache directory (~/.cache/clai).
func LegacyCacheDir() string {
	return filepath.Join(homeDir(), ".cache", "clai")
}
