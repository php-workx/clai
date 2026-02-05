package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_DefaultConfig(t *testing.T) {
	t.Parallel()

	logger := New(nil)
	assert.NotNil(t, logger)
}

func TestNew_JSONOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelInfo,
	})

	logger.Info("test message", "key", "value")

	// Verify output is valid JSON
	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	// Check required fields
	assert.Contains(t, logEntry, "ts")
	assert.Contains(t, logEntry, "level")
	assert.Contains(t, logEntry, "msg")
	assert.Equal(t, "test message", logEntry["msg"])
	assert.Equal(t, "value", logEntry["key"])
}

func TestNew_DebugLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Debug:  true,
	})

	logger.Debug("debug message")

	assert.Contains(t, buf.String(), "debug message")
}

func TestNew_InfoLevel_HidesDebug(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelInfo,
	})

	logger.Debug("debug message")

	assert.NotContains(t, buf.String(), "debug message")
}

func TestLogStartup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelInfo,
	})

	info := StartupInfo{
		Version:       "1.2.0",
		GitCommit:     "abc123",
		ConfigPath:    "/etc/clai/config.yaml",
		DatabasePath:  "/var/lib/clai/suggestions.db",
		SchemaVersion: 1,
		SocketPath:    "/tmp/clai/daemon.sock",
		FTS5Available: true,
		PID:           12345,
	}

	LogStartup(logger, info)

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "daemon started", logEntry["msg"])
	assert.Equal(t, "1.2.0", logEntry["version"])
	assert.Equal(t, "abc123", logEntry["git_commit"])
	assert.Equal(t, "/etc/clai/config.yaml", logEntry["config_path"])
	assert.Equal(t, "/var/lib/clai/suggestions.db", logEntry["database_path"])
	assert.Equal(t, float64(1), logEntry["schema_version"])
	assert.Equal(t, "/tmp/clai/daemon.sock", logEntry["socket_path"])
	assert.Equal(t, true, logEntry["fts5_available"])
	assert.Equal(t, float64(12345), logEntry["pid"])
}

func TestLogShutdown(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelInfo,
	})

	LogShutdown(logger, "idle timeout")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "daemon shutting down", logEntry["msg"])
	assert.Equal(t, "idle timeout", logEntry["reason"])
}

func TestLogConfigReload(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelInfo,
	})

	LogConfigReload(logger, "/etc/clai/config.yaml")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "configuration reloaded", logEntry["msg"])
	assert.Equal(t, "/etc/clai/config.yaml", logEntry["config_path"])
}

func TestLogEventDropped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelWarn,
	})

	LogEventDropped(logger, "timeout")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "WARN", logEntry["level"])
	assert.Equal(t, "event dropped", logEntry["msg"])
	assert.Equal(t, "timeout", logEntry["reason"])
}

func TestLogDiscoveryTimeout(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelWarn,
	})

	LogDiscoveryTimeout(logger, "just", 500)

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "discovery runner timeout", logEntry["msg"])
	assert.Equal(t, "just", logEntry["kind"])
	assert.Equal(t, float64(500), logEntry["timeout_ms"])
}

func TestLogSQLiteError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelError,
	})

	LogSQLiteError(logger, "insert", assert.AnError)

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "ERROR", logEntry["level"])
	assert.Equal(t, "sqlite error", logEntry["msg"])
	assert.Equal(t, "insert", logEntry["operation"])
}

func TestLogFTS5Unavailable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelWarn,
	})

	LogFTS5Unavailable(logger)

	assert.Contains(t, buf.String(), "FTS5 not available")
}

func TestTimestampFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&Config{
		Output: &buf,
		Level:  slog.LevelInfo,
	})

	logger.Info("test")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	// Verify timestamp field is named "ts" per spec
	assert.Contains(t, logEntry, "ts")
	assert.NotContains(t, logEntry, "time")

	// Verify timestamp is in RFC3339 format
	ts, ok := logEntry["ts"].(string)
	require.True(t, ok)
	assert.True(t, strings.Contains(ts, "T"), "timestamp should be in ISO format")
}
