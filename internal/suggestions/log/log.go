// Package log provides JSON-lines structured logging for the suggestions daemon.
// It implements the logging format specified in tech_suggestions_v3.md Section 20.4.
package log

import (
	"io"
	"log/slog"
	"os"
)

// Config configures the structured logger.
type Config struct {
	// Output is the writer for log output (default: os.Stderr)
	Output io.Writer

	// Level is the minimum log level (default: LevelInfo)
	Level slog.Level

	// Debug enables debug level logging (overrides Level)
	Debug bool
}

// DefaultConfig returns the default logging configuration.
func DefaultConfig() *Config {
	return &Config{
		Output: os.Stderr,
		Level:  slog.LevelInfo,
		Debug:  false,
	}
}

// New creates a new JSON-lines structured logger.
// Per spec Section 20.4, log format is:
//
//	{"ts":"2024-01-15T10:30:00Z","level":"info","msg":"daemon started","version":"1.2.0","pid":12345}
//
// Log levels:
//   - debug: Verbose (enabled via CLAI_DEBUG=1)
//   - info: Startup, shutdown, config reload
//   - warn: Non-fatal issues (discovery failures, dropped events)
//   - error: Fatal issues requiring attention
func New(cfg *Config) *slog.Logger {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	output := cfg.Output
	if output == nil {
		output = os.Stderr
	}

	level := cfg.Level
	if cfg.Debug {
		level = slog.LevelDebug
	}

	// Create JSON handler with timestamp formatted as "ts"
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename "time" to "ts" for spec compliance
			if a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			// Rename "msg" to keep consistency
			if a.Key == slog.MessageKey {
				a.Key = "msg"
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(output, opts)
	return slog.New(handler)
}

// NewFromEnv creates a logger configured from environment variables.
// CLAI_DEBUG=1 enables debug logging.
func NewFromEnv() *slog.Logger {
	cfg := DefaultConfig()
	if os.Getenv("CLAI_DEBUG") == "1" {
		cfg.Debug = true
	}
	return New(cfg)
}

// StartupInfo holds information to log at daemon startup.
// Per spec Section 20.4, startup message includes:
//   - Version and git commit
//   - Config file path loaded
//   - Database path and schema version
//   - Socket path
//   - FTS5 availability
type StartupInfo struct {
	Version       string
	GitCommit     string
	ConfigPath    string
	DatabasePath  string
	SchemaVersion int
	SocketPath    string
	FTS5Available bool
	PID           int
}

// LogStartup logs daemon startup information.
func LogStartup(logger *slog.Logger, info StartupInfo) {
	logger.Info("daemon started",
		"version", info.Version,
		"git_commit", info.GitCommit,
		"config_path", info.ConfigPath,
		"database_path", info.DatabasePath,
		"schema_version", info.SchemaVersion,
		"socket_path", info.SocketPath,
		"fts5_available", info.FTS5Available,
		"pid", info.PID,
	)
}

// LogShutdown logs daemon shutdown.
func LogShutdown(logger *slog.Logger, reason string) {
	logger.Info("daemon shutting down", "reason", reason)
}

// LogConfigReload logs configuration reload.
func LogConfigReload(logger *slog.Logger, configPath string) {
	logger.Info("configuration reloaded", "config_path", configPath)
}

// LogEventDropped logs when an event is dropped.
func LogEventDropped(logger *slog.Logger, reason string) {
	logger.Warn("event dropped", "reason", reason)
}

// LogDiscoveryTimeout logs when a discovery runner times out.
func LogDiscoveryTimeout(logger *slog.Logger, kind string, timeoutMs int64) {
	logger.Warn("discovery runner timeout", "kind", kind, "timeout_ms", timeoutMs)
}

// LogSQLiteError logs SQLite errors.
func LogSQLiteError(logger *slog.Logger, operation string, err error) {
	logger.Error("sqlite error", "operation", operation, "error", err)
}

// LogFTS5Unavailable logs when FTS5 is not available.
func LogFTS5Unavailable(logger *slog.Logger) {
	logger.Warn("FTS5 not available; history search disabled")
}

// LogCorruptionDetected logs when database corruption is detected.
func LogCorruptionDetected(logger *slog.Logger, dbPath string, reason string) {
	logger.Error("database corruption detected",
		"database_path", dbPath,
		"reason", reason,
	)
}

// LogCorruptionRecovered logs successful corruption recovery.
func LogCorruptionRecovered(logger *slog.Logger, dbPath string, backupPath string) {
	logger.Info("database corruption recovered",
		"database_path", dbPath,
		"backup_path", backupPath,
	)
}

// LogCorruptionRecoveryFailed logs when corruption recovery fails.
func LogCorruptionRecoveryFailed(logger *slog.Logger, dbPath string, err error) {
	logger.Error("database corruption recovery failed",
		"database_path", dbPath,
		"error", err,
	)
}

// LogIntegrityCheckPassed logs when an integrity check passes.
func LogIntegrityCheckPassed(logger *slog.Logger, dbPath string) {
	logger.Debug("database integrity check passed",
		"database_path", dbPath,
	)
}

// LogIntegrityCheckFailed logs when an integrity check fails.
func LogIntegrityCheckFailed(logger *slog.Logger, dbPath string, err error) {
	logger.Error("database integrity check failed",
		"database_path", dbPath,
		"error", err,
	)
}
