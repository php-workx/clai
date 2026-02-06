package maintenance

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// testSchema creates the minimal V2 schema needed for maintenance tests.
const testSchema = `
CREATE TABLE IF NOT EXISTS command_event (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id      TEXT NOT NULL,
  ts_ms           INTEGER NOT NULL,
  cwd             TEXT NOT NULL,
  repo_key        TEXT,
  branch          TEXT,
  cmd_raw         TEXT NOT NULL,
  cmd_norm        TEXT NOT NULL,
  cmd_truncated   INTEGER NOT NULL DEFAULT 0,
  template_id     TEXT,
  exit_code       INTEGER,
  duration_ms     INTEGER,
  ephemeral       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_event_ts ON command_event(ts_ms);

CREATE TABLE IF NOT EXISTS command_template (
  template_id     TEXT PRIMARY KEY,
  cmd_norm        TEXT NOT NULL,
  tags            TEXT,
  slot_count      INTEGER NOT NULL,
  first_seen_ms   INTEGER NOT NULL,
  last_seen_ms    INTEGER NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS command_event_fts USING fts5(
  cmd_raw,
  cmd_norm,
  repo_key UNINDEXED,
  session_id UNINDEXED,
  content='command_event',
  content_rowid='id',
  tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS command_event_ai AFTER INSERT ON command_event
WHEN NEW.ephemeral = 0
BEGIN
  INSERT INTO command_event_fts(rowid, cmd_raw, cmd_norm, repo_key, session_id)
  VALUES (NEW.id, NEW.cmd_raw, NEW.cmd_norm, NEW.repo_key, NEW.session_id);
END;

CREATE TRIGGER IF NOT EXISTS command_event_ad AFTER DELETE ON command_event
BEGIN
  INSERT INTO command_event_fts(command_event_fts, rowid, cmd_raw, cmd_norm, repo_key, session_id)
  VALUES ('delete', OLD.id, OLD.cmd_raw, OLD.cmd_norm, OLD.repo_key, OLD.session_id);
END;
`

// openTestDB creates an in-memory test database with the V2 schema subset.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(testSchema); err != nil {
		db.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// openTestDBOnDisk creates a test database on disk for size-related tests.
func openTestDBOnDisk(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("failed to open test db on disk: %v", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(testSchema); err != nil {
		db.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db, dbPath
}

// insertEvent inserts a test command event.
func insertEvent(t *testing.T, db *sql.DB, tsMs int64, cmd string, templateID *string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id)
		VALUES ('test-session', ?, '/tmp', ?, ?, ?)
	`, tsMs, cmd, cmd, templateID)
	if err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}
}

// insertTemplate inserts a test command template.
func insertTemplate(t *testing.T, db *sql.DB, templateID, cmdNorm string) {
	t.Helper()
	now := time.Now().UnixMilli()
	_, err := db.Exec(`
		INSERT INTO command_template (template_id, cmd_norm, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, 0, ?, ?)
	`, templateID, cmdNorm, now, now)
	if err != nil {
		t.Fatalf("failed to insert template: %v", err)
	}
}

// countEvents returns the number of rows in command_event.
func countEvents(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var count int64
	if err := db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count); err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	return count
}

// countTemplates returns the number of rows in command_template.
func countTemplates(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var count int64
	if err := db.QueryRow("SELECT COUNT(*) FROM command_template").Scan(&count); err != nil {
		t.Fatalf("failed to count templates: %v", err)
	}
	return count
}

// --- Config tests ---

func TestConfig_ApplyDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()

	if cfg.Interval != DefaultInterval {
		t.Errorf("Interval: got %v, want %v", cfg.Interval, DefaultInterval)
	}
	// RetentionDays=0 stays 0 (disabled); only negative triggers default
	if cfg.RetentionDays != 0 {
		t.Errorf("RetentionDays: got %d, want 0 (disabled)", cfg.RetentionDays)
	}
	if cfg.PruneBatchSize != DefaultPruneBatchSize {
		t.Errorf("PruneBatchSize: got %d, want %d", cfg.PruneBatchSize, DefaultPruneBatchSize)
	}
	if cfg.PruneYieldDuration != DefaultPruneYieldDuration {
		t.Errorf("PruneYieldDuration: got %v, want %v", cfg.PruneYieldDuration, DefaultPruneYieldDuration)
	}
	if cfg.LowActivityThreshold != DefaultLowActivityThreshold {
		t.Errorf("LowActivityThreshold: got %d, want %d", cfg.LowActivityThreshold, DefaultLowActivityThreshold)
	}
	if cfg.VacuumGrowthRatio != DefaultVacuumGrowthRatio {
		t.Errorf("VacuumGrowthRatio: got %f, want %f", cfg.VacuumGrowthRatio, DefaultVacuumGrowthRatio)
	}
	if cfg.Logger == nil {
		t.Error("Logger should be set to default")
	}
}

func TestConfig_ApplyDefaults_NegativeRetentionDays(t *testing.T) {
	cfg := Config{RetentionDays: -1}
	cfg.applyDefaults()

	if cfg.RetentionDays != DefaultRetentionDays {
		t.Errorf("RetentionDays: got %d, want %d", cfg.RetentionDays, DefaultRetentionDays)
	}
}

func TestConfig_PreservesUserValues(t *testing.T) {
	cfg := Config{
		Interval:             10 * time.Minute,
		RetentionDays:        30,
		PruneBatchSize:       500,
		PruneYieldDuration:   200 * time.Millisecond,
		LowActivityThreshold: 10,
		VacuumGrowthRatio:    3.0,
	}
	cfg.applyDefaults()

	if cfg.Interval != 10*time.Minute {
		t.Errorf("Interval: got %v, want 10m", cfg.Interval)
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays: got %d, want 30", cfg.RetentionDays)
	}
	if cfg.PruneBatchSize != 500 {
		t.Errorf("PruneBatchSize: got %d, want 500", cfg.PruneBatchSize)
	}
	if cfg.PruneYieldDuration != 200*time.Millisecond {
		t.Errorf("PruneYieldDuration: got %v, want 200ms", cfg.PruneYieldDuration)
	}
	if cfg.LowActivityThreshold != 10 {
		t.Errorf("LowActivityThreshold: got %d, want 10", cfg.LowActivityThreshold)
	}
	if cfg.VacuumGrowthRatio != 3.0 {
		t.Errorf("VacuumGrowthRatio: got %f, want 3.0", cfg.VacuumGrowthRatio)
	}
}

// --- Constructor tests ---

func TestNewRunner(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{RetentionDays: 90})

	if r.db != db {
		t.Error("db not set")
	}
	if r.cfg.Interval != DefaultInterval {
		t.Errorf("Interval: got %v, want %v", r.cfg.Interval, DefaultInterval)
	}
	if r.cfg.RetentionDays != 90 {
		t.Errorf("RetentionDays: got %d, want 90", r.cfg.RetentionDays)
	}
}

// --- WAL checkpoint tests ---

func TestWALCheckpoint_LowActivity(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	r.walCheckpoint(ctx, true)

	stats := r.GetStats()
	if stats.WALCheckpoints != 1 {
		t.Errorf("WALCheckpoints: got %d, want 1", stats.WALCheckpoints)
	}
}

func TestWALCheckpoint_HighActivity(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	r.walCheckpoint(ctx, false)

	stats := r.GetStats()
	if stats.WALCheckpoints != 1 {
		t.Errorf("WALCheckpoints: got %d, want 1", stats.WALCheckpoints)
	}
}

// --- Retention prune tests ---

func TestRetentionPrune_DeletesOldRows(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{RetentionDays: 90})
	ctx := context.Background()

	now := time.Now().UnixMilli()
	oldTs := now - 100*24*60*60*1000 // 100 days ago
	newTs := now - 10*24*60*60*1000  // 10 days ago

	for i := 0; i < 5; i++ {
		insertEvent(t, db, oldTs, fmt.Sprintf("old-cmd-%d", i), nil)
	}
	for i := 0; i < 3; i++ {
		insertEvent(t, db, newTs, fmt.Sprintf("new-cmd-%d", i), nil)
	}

	if count := countEvents(t, db); count != 8 {
		t.Fatalf("expected 8 events before prune, got %d", count)
	}

	deleted := r.retentionPrune(ctx)

	if deleted != 5 {
		t.Errorf("deleted: got %d, want 5", deleted)
	}
	if count := countEvents(t, db); count != 3 {
		t.Errorf("remaining events: got %d, want 3", count)
	}
}

func TestRetentionPrune_BatchProcessing(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{
		RetentionDays:      90,
		PruneBatchSize:     3,
		PruneYieldDuration: time.Millisecond, // fast for testing
	})
	ctx := context.Background()

	now := time.Now().UnixMilli()
	oldTs := now - 100*24*60*60*1000

	// Insert 10 old events (will require multiple batches of 3)
	for i := 0; i < 10; i++ {
		insertEvent(t, db, oldTs, fmt.Sprintf("cmd-%d", i), nil)
	}

	deleted := r.retentionPrune(ctx)

	if deleted != 10 {
		t.Errorf("deleted: got %d, want 10", deleted)
	}
	if count := countEvents(t, db); count != 0 {
		t.Errorf("remaining events: got %d, want 0", count)
	}
}

func TestRetentionPrune_DisabledWhenZero(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{RetentionDays: 0})
	ctx := context.Background()

	now := time.Now().UnixMilli()
	oldTs := now - 1000*24*60*60*1000 // Very old

	insertEvent(t, db, oldTs, "old-cmd", nil)

	// tick should not prune because RetentionDays=0
	r.tick(ctx)

	if count := countEvents(t, db); count != 1 {
		t.Errorf("events should not be pruned when RetentionDays=0, got %d", count)
	}
}

func TestRetentionPrune_NoOldRows(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{RetentionDays: 90})
	ctx := context.Background()

	now := time.Now().UnixMilli()
	insertEvent(t, db, now, "recent-cmd", nil)

	deleted := r.retentionPrune(ctx)

	if deleted != 0 {
		t.Errorf("deleted: got %d, want 0", deleted)
	}
}

func TestRetentionPrune_CancelledContext(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{
		RetentionDays:      90,
		PruneBatchSize:     2,
		PruneYieldDuration: time.Millisecond,
	})

	now := time.Now().UnixMilli()
	oldTs := now - 100*24*60*60*1000

	for i := 0; i < 10; i++ {
		insertEvent(t, db, oldTs, fmt.Sprintf("cmd-%d", i), nil)
	}

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return quickly; may have partially deleted
	deleted := r.retentionPrune(ctx)
	remaining := countEvents(t, db)

	// Total should add up
	if deleted+remaining != 10 {
		t.Errorf("deleted=%d + remaining=%d should equal 10", deleted, remaining)
	}
}

func TestRetentionPrune_EmptyDB(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{RetentionDays: 90})
	ctx := context.Background()

	deleted := r.retentionPrune(ctx)

	if deleted != 0 {
		t.Errorf("deleted: got %d, want 0", deleted)
	}
}

// --- Orphan cleanup tests ---

func TestCleanOrphanedTemplates_RemovesOrphans(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	// Create templates
	insertTemplate(t, db, "tmpl-used", "git status")
	insertTemplate(t, db, "tmpl-orphan", "rm -rf /")

	// Only reference tmpl-used in an event
	tmplUsed := "tmpl-used"
	insertEvent(t, db, time.Now().UnixMilli(), "git status", &tmplUsed)

	if count := countTemplates(t, db); count != 2 {
		t.Fatalf("expected 2 templates, got %d", count)
	}

	deleted := r.cleanOrphanedTemplates(ctx)

	if deleted != 1 {
		t.Errorf("deleted: got %d, want 1", deleted)
	}
	if count := countTemplates(t, db); count != 1 {
		t.Errorf("remaining templates: got %d, want 1", count)
	}
}

func TestCleanOrphanedTemplates_NoOrphans(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	insertTemplate(t, db, "tmpl-1", "git status")
	tmpl := "tmpl-1"
	insertEvent(t, db, time.Now().UnixMilli(), "git status", &tmpl)

	deleted := r.cleanOrphanedTemplates(ctx)

	if deleted != 0 {
		t.Errorf("deleted: got %d, want 0", deleted)
	}
}

func TestCleanOrphanedTemplates_AllOrphans(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	insertTemplate(t, db, "tmpl-1", "cmd1")
	insertTemplate(t, db, "tmpl-2", "cmd2")

	// No events reference any template
	deleted := r.cleanOrphanedTemplates(ctx)

	if deleted != 2 {
		t.Errorf("deleted: got %d, want 2", deleted)
	}
}

// --- FTS optimize tests ---

func TestFTSOptimize(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	// Insert some data so FTS has something to work with
	insertEvent(t, db, time.Now().UnixMilli(), "git status", nil)
	insertEvent(t, db, time.Now().UnixMilli(), "make build", nil)

	r.ftsOptimize(ctx)

	stats := r.GetStats()
	if stats.FTSOptimizations != 1 {
		t.Errorf("FTSOptimizations: got %d, want 1", stats.FTSOptimizations)
	}
}

// --- VACUUM tests ---

func TestMaybeVacuum_NoPath(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{DBPath: ""})
	ctx := context.Background()

	r.maybeVacuum(ctx)

	stats := r.GetStats()
	if stats.VacuumsPerformed != 0 {
		t.Error("should not vacuum without DBPath")
	}
}

func TestMaybeVacuum_BelowThreshold(t *testing.T) {
	db, dbPath := openTestDBOnDisk(t)
	r := NewRunner(db, Config{
		DBPath:            dbPath,
		VacuumGrowthRatio: 2.0,
	})
	ctx := context.Background()

	// First call records initial size
	r.maybeVacuum(ctx)
	stats := r.GetStats()
	if stats.VacuumsPerformed != 0 {
		t.Error("should not vacuum on first check")
	}
	if stats.LastVacuumSizeBytes == 0 {
		t.Error("should have recorded initial size")
	}

	// Second call: DB hasn't grown enough
	r.maybeVacuum(ctx)
	stats = r.GetStats()
	if stats.VacuumsPerformed != 0 {
		t.Error("should not vacuum below threshold")
	}
}

func TestMaybeVacuum_AboveThreshold(t *testing.T) {
	db, dbPath := openTestDBOnDisk(t)
	r := NewRunner(db, Config{
		DBPath:            dbPath,
		VacuumGrowthRatio: 2.0,
	})
	ctx := context.Background()

	// Record a small initial size via the mutex
	r.mu.Lock()
	r.stats.LastVacuumSizeBytes = 1024 // 1KB baseline
	r.mu.Unlock()

	// Insert enough data to exceed 2KB
	for i := 0; i < 200; i++ {
		insertEvent(t, db, time.Now().UnixMilli(), fmt.Sprintf("command-with-long-text-%d-padding-padding-padding", i), nil)
	}

	// Force WAL checkpoint so data is in main file
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if info.Size() < 2048 {
		t.Skipf("DB file too small for threshold test: %d bytes", info.Size())
	}

	r.maybeVacuum(ctx)

	stats := r.GetStats()
	if stats.VacuumsPerformed != 1 {
		t.Errorf("VacuumsPerformed: got %d, want 1", stats.VacuumsPerformed)
	}
}

func TestMaybeVacuum_FirstRun(t *testing.T) {
	db, dbPath := openTestDBOnDisk(t)
	r := NewRunner(db, Config{DBPath: dbPath})
	ctx := context.Background()

	stats := r.GetStats()
	if stats.LastVacuumSizeBytes != 0 {
		t.Error("initial size should be 0")
	}

	r.maybeVacuum(ctx)

	stats = r.GetStats()
	if stats.LastVacuumSizeBytes == 0 {
		t.Error("should have recorded size after first check")
	}
	if stats.VacuumsPerformed != 0 {
		t.Error("should not vacuum on first run")
	}
}

// --- Tick tests ---

func TestTick_LowActivity(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{RetentionDays: 90})
	ctx := context.Background()

	// No events recorded = low activity
	r.tick(ctx)

	stats := r.GetStats()
	if stats.Ticks != 1 {
		t.Errorf("Ticks: got %d, want 1", stats.Ticks)
	}
	if stats.WALCheckpoints != 1 {
		t.Errorf("WALCheckpoints: got %d, want 1", stats.WALCheckpoints)
	}
	// FTS optimize should run during low activity
	if stats.FTSOptimizations != 1 {
		t.Errorf("FTSOptimizations: got %d, want 1", stats.FTSOptimizations)
	}
}

func TestTick_HighActivity(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{
		RetentionDays:        90,
		LowActivityThreshold: 5,
	})
	ctx := context.Background()

	// Record enough events to exceed threshold
	for i := 0; i < 10; i++ {
		r.RecordEvent()
	}

	r.tick(ctx)

	stats := r.GetStats()
	if stats.Ticks != 1 {
		t.Errorf("Ticks: got %d, want 1", stats.Ticks)
	}
	// WAL should still checkpoint (PASSIVE mode)
	if stats.WALCheckpoints != 1 {
		t.Errorf("WALCheckpoints: got %d, want 1", stats.WALCheckpoints)
	}
	// FTS optimize should NOT run during high activity
	if stats.FTSOptimizations != 0 {
		t.Errorf("FTSOptimizations: got %d, want 0 (high activity)", stats.FTSOptimizations)
	}
}

func TestTick_ResetsEventCounter(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})
	ctx := context.Background()

	r.RecordEvent()
	r.RecordEvent()
	r.RecordEvent()

	if r.events.Load() != 3 {
		t.Fatalf("events before tick: got %d, want 3", r.events.Load())
	}

	r.tick(ctx)

	if r.events.Load() != 0 {
		t.Errorf("events after tick: got %d, want 0", r.events.Load())
	}
}

// --- Run loop tests ---

func TestRun_ContextCancel(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{Interval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	stopCh := make(chan struct{})

	done := make(chan struct{})
	go func() {
		r.Run(ctx, stopCh)
		close(done)
	}()

	// Let it tick a few times
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}

	stats := r.GetStats()
	if stats.Ticks == 0 {
		t.Error("expected at least one tick")
	}
}

func TestRun_StopChannel(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{Interval: 10 * time.Millisecond})

	ctx := context.Background()
	stopCh := make(chan struct{})

	done := make(chan struct{})
	go func() {
		r.Run(ctx, stopCh)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	close(stopCh)

	select {
	case <-done:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after stopCh closed")
	}
}

func TestRun_MultipleTicks(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{Interval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	stopCh := make(chan struct{})

	go r.Run(ctx, stopCh)

	// Wait for several ticks
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Allow Run to exit
	time.Sleep(50 * time.Millisecond)

	stats := r.GetStats()
	if stats.Ticks < 2 {
		t.Errorf("expected >= 2 ticks, got %d", stats.Ticks)
	}
}

// --- Integration tests ---

func TestIntegration_FullMaintenancePass(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{
		RetentionDays:  90,
		PruneBatchSize: 10,
	})
	ctx := context.Background()

	now := time.Now().UnixMilli()
	oldTs := now - 100*24*60*60*1000

	// Insert old events with template references
	tmpl := "tmpl-old"
	insertTemplate(t, db, tmpl, "old-cmd")
	for i := 0; i < 5; i++ {
		insertEvent(t, db, oldTs, fmt.Sprintf("old-cmd-%d", i), &tmpl)
	}

	// Insert recent event with different template
	tmplNew := "tmpl-new"
	insertTemplate(t, db, tmplNew, "new-cmd")
	insertEvent(t, db, now, "new-cmd", &tmplNew)

	// Run a full tick
	r.tick(ctx)

	// Old events should be pruned
	if count := countEvents(t, db); count != 1 {
		t.Errorf("events after tick: got %d, want 1", count)
	}

	// Orphaned old template should be cleaned
	if count := countTemplates(t, db); count != 1 {
		t.Errorf("templates after tick: got %d, want 1", count)
	}

	// Stats should reflect work done
	stats := r.GetStats()
	if stats.EventsPruned != 5 {
		t.Errorf("EventsPruned: got %d, want 5", stats.EventsPruned)
	}
	if stats.OrphansCleaned != 1 {
		t.Errorf("OrphansCleaned: got %d, want 1", stats.OrphansCleaned)
	}
}

// --- RecordEvent tests ---

func TestRecordEvent_ThreadSafety(t *testing.T) {
	db := openTestDB(t)
	r := NewRunner(db, Config{})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.RecordEvent()
		}()
	}
	wg.Wait()

	if got := r.events.Load(); got != 100 {
		t.Errorf("events: got %d, want 100", got)
	}
}

// --- Constants tests ---

func TestConstants(t *testing.T) {
	if DefaultInterval != 5*time.Minute {
		t.Errorf("DefaultInterval: got %v, want 5m", DefaultInterval)
	}
	if DefaultRetentionDays != 90 {
		t.Errorf("DefaultRetentionDays: got %d, want 90", DefaultRetentionDays)
	}
	if DefaultPruneBatchSize != 1000 {
		t.Errorf("DefaultPruneBatchSize: got %d, want 1000", DefaultPruneBatchSize)
	}
	if DefaultPruneYieldDuration != 100*time.Millisecond {
		t.Errorf("DefaultPruneYieldDuration: got %v, want 100ms", DefaultPruneYieldDuration)
	}
	if DefaultLowActivityThreshold != 5 {
		t.Errorf("DefaultLowActivityThreshold: got %d, want 5", DefaultLowActivityThreshold)
	}
	if DefaultVacuumGrowthRatio != 2.0 {
		t.Errorf("DefaultVacuumGrowthRatio: got %f, want 2.0", DefaultVacuumGrowthRatio)
	}
}
