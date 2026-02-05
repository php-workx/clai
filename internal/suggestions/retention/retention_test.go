package retention

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-retention-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create command_event table
	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL
		);
		CREATE INDEX idx_event_ts ON command_event(ts);
	`)
	require.NoError(t, err)

	return db
}

// insertEvent inserts a test event into the database.
func insertEvent(t *testing.T, db *sql.DB, ts int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO command_event (session_id, ts, cmd_raw, cmd_norm, cwd)
		VALUES ('session1', ?, 'test command', 'test command', '/home/user')
	`, ts)
	require.NoError(t, err)
}

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	assert.Equal(t, DefaultRetentionDays, policy.RetentionDays)
	assert.True(t, policy.AutoVacuum)
}

func TestNewPurger_ClampsRetentionDays(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	t.Run("below minimum", func(t *testing.T) {
		p := NewPurger(db, &Policy{RetentionDays: 0})
		assert.Equal(t, MinRetentionDays, p.GetRetentionDays())
	})

	t.Run("above maximum", func(t *testing.T) {
		p := NewPurger(db, &Policy{RetentionDays: 10000})
		assert.Equal(t, MaxRetentionDays, p.GetRetentionDays())
	})

	t.Run("within range", func(t *testing.T) {
		p := NewPurger(db, &Policy{RetentionDays: 30})
		assert.Equal(t, 30, p.GetRetentionDays())
	})
}

func TestPurger_Purge(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)

	// Set up test data
	// nowMs = 100 days in milliseconds
	nowMs := int64(100 * 24 * 60 * 60 * 1000)

	// Insert events at different times
	// Event 1: 150 days ago (should be deleted with 90-day retention)
	insertEvent(t, db, nowMs-150*24*60*60*1000)
	// Event 2: 50 days ago (should be kept)
	insertEvent(t, db, nowMs-50*24*60*60*1000)
	// Event 3: today (should be kept)
	insertEvent(t, db, nowMs)

	ctx := context.Background()
	p := NewPurger(db, &Policy{
		RetentionDays: 90,
		AutoVacuum:    false, // Disable for test
	})

	result, err := p.PurgeAt(ctx, nowMs)
	require.NoError(t, err)

	assert.Equal(t, int64(1), result.DeletedEvents)
	assert.False(t, result.Vacuumed)

	// Verify remaining events
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestPurger_Purge_NoOldData(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	nowMs := int64(100 * 24 * 60 * 60 * 1000)

	// Insert only recent events
	insertEvent(t, db, nowMs-10*24*60*60*1000)
	insertEvent(t, db, nowMs)

	ctx := context.Background()
	p := NewPurger(db, &Policy{
		RetentionDays: 90,
		AutoVacuum:    false,
	})

	result, err := p.PurgeAt(ctx, nowMs)
	require.NoError(t, err)

	assert.Equal(t, int64(0), result.DeletedEvents)
}

func TestPurger_Purge_EmptyDatabase(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	nowMs := int64(100 * 24 * 60 * 60 * 1000)

	ctx := context.Background()
	p := NewPurger(db, DefaultPolicy())

	result, err := p.PurgeAt(ctx, nowMs)
	require.NoError(t, err)

	assert.Equal(t, int64(0), result.DeletedEvents)
}

func TestPurger_EstimatePurgeCount(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	nowMs := int64(100 * 24 * 60 * 60 * 1000)

	// Insert events
	insertEvent(t, db, nowMs-150*24*60*60*1000) // Old
	insertEvent(t, db, nowMs-100*24*60*60*1000) // Old
	insertEvent(t, db, nowMs-50*24*60*60*1000)  // Recent
	insertEvent(t, db, nowMs)                   // Today

	ctx := context.Background()
	p := NewPurger(db, &Policy{RetentionDays: 90})

	count, err := p.EstimatePurgeCountAt(ctx, nowMs)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestPurger_ShortRetention(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	nowMs := int64(10 * 24 * 60 * 60 * 1000) // 10 days in ms

	// Insert events
	insertEvent(t, db, nowMs-5*24*60*60*1000) // 5 days ago
	insertEvent(t, db, nowMs-2*24*60*60*1000) // 2 days ago
	insertEvent(t, db, nowMs)                 // Today

	ctx := context.Background()
	p := NewPurger(db, &Policy{
		RetentionDays: 3, // Only keep 3 days
		AutoVacuum:    false,
	})

	result, err := p.PurgeAt(ctx, nowMs)
	require.NoError(t, err)

	assert.Equal(t, int64(1), result.DeletedEvents)

	// Verify remaining events
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM command_event").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestPurger_RebuildAggregates(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	ctx := context.Background()
	p := NewPurger(db, DefaultPolicy())

	// Should not error (even though not fully implemented)
	err := p.RebuildAggregates(ctx)
	require.NoError(t, err)
}

func TestConstants(t *testing.T) {
	t.Parallel()

	// Verify sensible defaults
	assert.Equal(t, 90, DefaultRetentionDays)
	assert.Equal(t, 1, MinRetentionDays)
	assert.Equal(t, 3650, MaxRetentionDays) // 10 years
	assert.Equal(t, 10000, VacuumThreshold)
}
