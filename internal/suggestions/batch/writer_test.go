package batch

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/ingest"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-batch-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create minimal schema for testing (matches V2 command_event columns)
	_, err = db.Exec(`
		CREATE TABLE command_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			ts_ms INTEGER NOT NULL,
			cwd TEXT NOT NULL,
			repo_key TEXT,
			branch TEXT,
			cmd_raw TEXT NOT NULL,
			cmd_norm TEXT NOT NULL,
			cmd_truncated INTEGER NOT NULL DEFAULT 0,
			template_id TEXT,
			exit_code INTEGER,
			duration_ms INTEGER,
			ephemeral INTEGER NOT NULL DEFAULT 0
		)
	`)
	require.NoError(t, err)

	return db
}

func TestNewWriter(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	t.Run("default options", func(t *testing.T) {
		w := NewWriter(db, DefaultOptions())
		assert.NotNil(t, w)
		assert.Equal(t, DefaultFlushInterval, w.opts.FlushInterval)
		assert.Equal(t, DefaultMaxBatchSize, w.opts.MaxBatchSize)
		assert.Equal(t, DefaultQueueSize, w.opts.QueueSize)
	})

	t.Run("custom options", func(t *testing.T) {
		opts := Options{
			FlushInterval: 50 * time.Millisecond,
			MaxBatchSize:  50,
			QueueSize:     200,
		}
		w := NewWriter(db, opts)
		assert.Equal(t, 50*time.Millisecond, w.opts.FlushInterval)
		assert.Equal(t, 50, w.opts.MaxBatchSize)
		assert.Equal(t, 200, w.opts.QueueSize)
	})

	t.Run("zero options use defaults", func(t *testing.T) {
		w := NewWriter(db, Options{})
		assert.Equal(t, DefaultFlushInterval, w.opts.FlushInterval)
		assert.Equal(t, DefaultMaxBatchSize, w.opts.MaxBatchSize)
		assert.Equal(t, DefaultQueueSize, w.opts.QueueSize)
	})
}

func TestWriter_EnqueueAndFlush(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 1 * time.Hour, // Long interval - rely on explicit flush
		MaxBatchSize:  100,           // Large batch - won't auto-flush
		QueueSize:     100,
	})
	w.Start()
	defer w.Stop()

	// Enqueue some events
	for i := 0; i < 5; i++ {
		ev := &event.CommandEvent{
			Version:   1,
			Type:      event.EventTypeCommandEnd,
			Ts:        time.Now().UnixMilli(),
			SessionID: "test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    "echo test",
			ExitCode:  0,
			Ephemeral: false,
		}
		ok := w.Enqueue(ev)
		assert.True(t, ok)
	}

	// Give write loop time to receive all events
	time.Sleep(10 * time.Millisecond)

	// Force flush
	w.Flush()
	time.Sleep(100 * time.Millisecond) // Give time for flush to complete

	// Check that events were written
	stats := w.Stats()
	assert.Equal(t, int64(5), stats.EventsWritten)
	assert.Equal(t, int64(1), stats.BatchesWritten)
	assert.Equal(t, int64(0), stats.EventsDropped)

	// Verify in database
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestWriter_BatchSizeFlush(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 1 * time.Hour, // Long interval, rely on batch size
		MaxBatchSize:  5,
		QueueSize:     100,
	})
	w.Start()
	defer w.Stop()

	// Enqueue exactly MaxBatchSize events to trigger flush
	for i := 0; i < 5; i++ {
		ev := &event.CommandEvent{
			Version:   1,
			Type:      event.EventTypeCommandEnd,
			Ts:        time.Now().UnixMilli(),
			SessionID: "test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    "echo batch",
			ExitCode:  0,
			Ephemeral: false,
		}
		w.Enqueue(ev)
	}

	// Wait for batch size trigger
	time.Sleep(50 * time.Millisecond)

	stats := w.Stats()
	assert.Equal(t, int64(5), stats.EventsWritten)
	assert.GreaterOrEqual(t, stats.BatchesWritten, int64(1))
}

func TestWriter_EphemeralEventsNotPersisted(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 100 * time.Millisecond,
		MaxBatchSize:  10,
		QueueSize:     100,
	})
	w.Start()
	defer w.Stop()

	// Enqueue ephemeral events (should not be persisted)
	for i := 0; i < 5; i++ {
		ev := &event.CommandEvent{
			Version:   1,
			Type:      event.EventTypeCommandEnd,
			Ts:        time.Now().UnixMilli(),
			SessionID: "test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    "secret command",
			ExitCode:  0,
			Ephemeral: true, // Ephemeral!
		}
		ok := w.Enqueue(ev)
		assert.True(t, ok, "Enqueue should return true for ephemeral events")
	}

	// Flush and wait
	w.Flush()
	time.Sleep(50 * time.Millisecond)

	// Check that NO events were written (ephemeral are not persisted)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Ephemeral events should not be persisted")
}

func TestWriter_NilEventIgnored(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, DefaultOptions())
	w.Start()
	defer w.Stop()

	// Enqueue nil event
	ok := w.Enqueue(nil)
	assert.True(t, ok, "Enqueue should return true for nil event")

	// Flush and check
	w.Flush()
	time.Sleep(50 * time.Millisecond)

	stats := w.Stats()
	assert.Equal(t, int64(0), stats.EventsWritten)
}

func TestWriter_QueueFull(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 1 * time.Hour, // Never flush automatically
		MaxBatchSize:  1000,          // High batch size
		QueueSize:     5,             // Very small queue
	})
	// Don't start the writer - simulate slow consumer
	// The write loop won't drain the queue

	// Enqueue more events than queue can hold
	for i := 0; i < 10; i++ {
		ev := &event.CommandEvent{
			Version:   1,
			Type:      event.EventTypeCommandEnd,
			Ts:        time.Now().UnixMilli(),
			SessionID: "test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    "echo overflow",
			ExitCode:  0,
		}
		w.Enqueue(ev)
	}

	// Should have dropped some events
	stats := w.Stats()
	assert.Greater(t, stats.EventsDropped, int64(0))
}

func TestWriter_GracefulShutdown(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 1 * time.Hour, // Long interval
		MaxBatchSize:  100,
		QueueSize:     100,
	})
	w.Start()

	// Enqueue events
	for i := 0; i < 10; i++ {
		ev := &event.CommandEvent{
			Version:   1,
			Type:      event.EventTypeCommandEnd,
			Ts:        time.Now().UnixMilli(),
			SessionID: "test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    "echo shutdown",
			ExitCode:  0,
		}
		w.Enqueue(ev)
	}

	// Stop should flush pending events
	w.Stop()

	// Verify events were written on shutdown
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count, "All events should be flushed on shutdown")
}

func TestWriter_MultipleStops(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, DefaultOptions())
	w.Start()

	// Stop multiple times should not panic
	w.Stop()
	w.Stop()
	w.Stop()
}

func TestWriter_TimerFlush(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 30 * time.Millisecond, // Short interval
		MaxBatchSize:  100,
		QueueSize:     100,
	})
	w.Start()
	defer w.Stop()

	// Enqueue one event (not enough to trigger batch size flush)
	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        time.Now().UnixMilli(),
		SessionID: "test-session",
		Shell:     event.ShellBash,
		Cwd:       "/home/user",
		CmdRaw:    "echo timer",
		ExitCode:  0,
	}
	w.Enqueue(ev)

	// Wait for timer flush
	time.Sleep(100 * time.Millisecond)

	stats := w.Stats()
	assert.Equal(t, int64(1), stats.EventsWritten)
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	assert.Equal(t, DefaultFlushInterval, opts.FlushInterval)
	assert.Equal(t, DefaultMaxBatchSize, opts.MaxBatchSize)
	assert.Equal(t, DefaultQueueSize, opts.QueueSize)
}

func TestWriter_Stats(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	w := NewWriter(db, Options{
		FlushInterval: 1 * time.Hour, // Long interval - rely on explicit flush
		MaxBatchSize:  100,           // Large batch - won't auto-flush
		QueueSize:     100,
	})
	w.Start()
	defer w.Stop()

	// Initial stats
	stats := w.Stats()
	assert.Equal(t, int64(0), stats.EventsWritten)
	assert.Equal(t, int64(0), stats.BatchesWritten)
	assert.Equal(t, int64(0), stats.EventsDropped)
	assert.True(t, stats.LastFlushTime.IsZero())

	// Enqueue events
	for i := 0; i < 3; i++ {
		ev := &event.CommandEvent{
			Version:   1,
			Type:      event.EventTypeCommandEnd,
			Ts:        time.Now().UnixMilli(),
			SessionID: "test-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user",
			CmdRaw:    "echo stats",
			ExitCode:  0,
		}
		w.Enqueue(ev)
	}

	// Give write loop time to receive all events, then flush
	time.Sleep(10 * time.Millisecond)
	w.Flush()
	time.Sleep(100 * time.Millisecond)

	// Check updated stats
	stats = w.Stats()
	assert.Equal(t, int64(3), stats.EventsWritten)
	assert.Equal(t, int64(1), stats.BatchesWritten)
	assert.Equal(t, 3, stats.LastBatchSize)
	assert.False(t, stats.LastFlushTime.IsZero())
}

// --- WritePath V2 integration tests ---

// createTestV2DB creates a V2 SQLite database with the full schema for WritePath tests.
func createTestV2DB(t *testing.T) *sql.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	d, err := suggestdb.Open(context.Background(), suggestdb.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	// Ensure session exists for tests (command_event references session_id)
	_, err = d.DB().ExecContext(context.Background(), `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('test-session', 'zsh', 1000)
	`)
	require.NoError(t, err)

	return d.DB()
}

func TestBatchWriter_CallsWritePath(t *testing.T) {
	t.Parallel()

	db := createTestV2DB(t)
	w := NewWriter(db, Options{
		FlushInterval:   1 * time.Hour, // Rely on explicit flush
		MaxBatchSize:    100,
		QueueSize:       100,
		WritePathConfig: &ingest.WritePathConfig{},
	})
	w.Start()
	defer w.Stop()

	// Enqueue an event
	ev := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1700000000000,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user/project",
		CmdRaw:    "git status",
		ExitCode:  0,
	}
	ok := w.Enqueue(ev)
	require.True(t, ok)

	// Give write loop time to pick up event, then flush
	time.Sleep(10 * time.Millisecond)
	w.Flush()
	time.Sleep(200 * time.Millisecond)

	// Verify command_event was inserted
	var eventCount int
	err := db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&eventCount)
	require.NoError(t, err)
	assert.Equal(t, 1, eventCount, "command_event should have 1 row")

	// Verify command_template was populated by WritePath
	var templateCount int
	err = db.QueryRow("SELECT COUNT(*) FROM command_template").Scan(&templateCount)
	require.NoError(t, err)
	assert.Equal(t, 1, templateCount, "command_template should have 1 row from WritePath")

	// Verify command_stat was populated by WritePath
	var statCount int
	err = db.QueryRow("SELECT COUNT(*) FROM command_stat WHERE scope = 'global'").Scan(&statCount)
	require.NoError(t, err)
	assert.Equal(t, 1, statCount, "command_stat should have 1 global row from WritePath")

	// Verify the template matches the normalized command
	var cmdNorm string
	err = db.QueryRow("SELECT cmd_norm FROM command_template LIMIT 1").Scan(&cmdNorm)
	require.NoError(t, err)
	assert.Equal(t, "git status", cmdNorm)
}

func TestBatchWriter_WritePath_ErrorSkipsEvent(t *testing.T) {
	t.Parallel()

	db := createTestV2DB(t)
	w := NewWriter(db, Options{
		FlushInterval:   1 * time.Hour,
		MaxBatchSize:    100,
		QueueSize:       100,
		WritePathConfig: &ingest.WritePathConfig{},
	})
	w.Start()
	defer w.Stop()

	// Enqueue a valid event first
	ev1 := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1700000000000,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user/project",
		CmdRaw:    "git status",
		ExitCode:  0,
	}
	w.Enqueue(ev1)

	// Enqueue an event with an empty command. WritePath should still
	// handle this, but even if it errors, the batch should continue.
	ev2 := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1700000001000,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user/project",
		CmdRaw:    "", // empty command
		ExitCode:  0,
	}
	w.Enqueue(ev2)

	// Enqueue another valid event after the potentially problematic one
	ev3 := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1700000002000,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user/project",
		CmdRaw:    "make build",
		ExitCode:  0,
	}
	w.Enqueue(ev3)

	// Flush and wait
	time.Sleep(10 * time.Millisecond)
	w.Flush()
	time.Sleep(200 * time.Millisecond)

	// The batch should not have failed entirely. At minimum, event 1 and event 3
	// should be written. Event 2 may or may not succeed (empty cmd is still valid
	// input to WritePath). The point is the batch did not fail.
	stats := w.Stats()
	assert.Equal(t, int64(3), stats.EventsWritten, "batch should report all 3 events written")
	assert.Equal(t, int64(0), stats.WriteErrors, "no batch-level write errors expected")

	// Verify command_events were inserted
	var eventCount int
	err := db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&eventCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, eventCount, 2, "at least 2 events should be in command_event")
}

func TestBatchWriter_TransitionsAcrossBatches(t *testing.T) {
	t.Parallel()

	db := createTestV2DB(t)
	w := NewWriter(db, Options{
		FlushInterval:   1 * time.Hour,
		MaxBatchSize:    100,
		QueueSize:       100,
		WritePathConfig: &ingest.WritePathConfig{},
	})
	w.Start()
	defer w.Stop()

	// Batch 1: enqueue first event
	ev1 := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1700000000000,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user/project",
		CmdRaw:    "git status",
		ExitCode:  0,
	}
	w.Enqueue(ev1)

	// Flush batch 1 and wait
	time.Sleep(10 * time.Millisecond)
	w.Flush()
	time.Sleep(200 * time.Millisecond)

	// Verify no transitions yet (only one event so far)
	var transCount int
	err := db.QueryRow("SELECT COUNT(*) FROM transition_stat").Scan(&transCount)
	require.NoError(t, err)
	assert.Equal(t, 0, transCount, "no transitions after first event")

	// Batch 2: enqueue second event (different command, same session)
	ev2 := &event.CommandEvent{
		Version:   1,
		Type:      event.EventTypeCommandEnd,
		Ts:        1700000001000,
		SessionID: "test-session",
		Shell:     event.ShellZsh,
		Cwd:       "/home/user/project",
		CmdRaw:    "git add .",
		ExitCode:  0,
	}
	w.Enqueue(ev2)

	// Flush batch 2 and wait
	time.Sleep(10 * time.Millisecond)
	w.Flush()
	time.Sleep(200 * time.Millisecond)

	// Verify transition was recorded across batch boundaries
	err = db.QueryRow("SELECT COUNT(*) FROM transition_stat WHERE scope = 'global'").Scan(&transCount)
	require.NoError(t, err)
	assert.Equal(t, 1, transCount, "transition should be recorded across batch boundary")

	// Verify the transition links the correct templates
	var prevTemplateID, nextTemplateID string
	err = db.QueryRow(`
		SELECT prev_template_id, next_template_id
		FROM transition_stat
		WHERE scope = 'global'
		LIMIT 1
	`).Scan(&prevTemplateID, &nextTemplateID)
	require.NoError(t, err)
	assert.NotEmpty(t, prevTemplateID, "prev_template_id should be set")
	assert.NotEmpty(t, nextTemplateID, "next_template_id should be set")
	assert.NotEqual(t, prevTemplateID, nextTemplateID, "template IDs should differ for different commands")

	// Verify two command_template rows exist (one per unique normalized command)
	var templateCount int
	err = db.QueryRow("SELECT COUNT(*) FROM command_template").Scan(&templateCount)
	require.NoError(t, err)
	assert.Equal(t, 2, templateCount, "two distinct command templates should exist")
}
