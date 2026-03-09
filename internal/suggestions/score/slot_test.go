package score

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

// createSlotTestDB creates a temporary SQLite database for slot store testing.
func createSlotTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-slot-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create slot_value table
	_, err = db.Exec(`
		CREATE TABLE slot_value (
			scope     TEXT NOT NULL,
			cmd_norm  TEXT NOT NULL,
			slot_idx  INTEGER NOT NULL,
			value     TEXT NOT NULL,
			count     REAL NOT NULL,
			last_ts   INTEGER NOT NULL,
			PRIMARY KEY (scope, cmd_norm, slot_idx, value)
		);
		CREATE INDEX idx_slot_value_lookup ON slot_value(scope, cmd_norm, slot_idx);
	`)
	require.NoError(t, err)

	return db
}

func TestSlotStore_DefaultOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultSlotOptions()
	assert.Equal(t, int64(DefaultSlotTauMs), opts.TauMs)
	assert.Equal(t, DefaultTopK, opts.TopK)
}

func TestSlotStore_NewWithDefaults(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, SlotOptions{})
	require.NoError(t, err)
	defer ss.Close()

	assert.Equal(t, int64(DefaultSlotTauMs), ss.TauMs())
	assert.Equal(t, DefaultTopK, ss.TopK())
}

func TestSlotStore_NewClampsValues(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)

	t.Run("tau below minimum", func(t *testing.T) {
		ss, err := NewSlotStore(db, SlotOptions{TauMs: 1})
		require.NoError(t, err)
		defer ss.Close()
		assert.Equal(t, int64(MinTauMs), ss.TauMs())
	})

	t.Run("topK below minimum", func(t *testing.T) {
		ss, err := NewSlotStore(db, SlotOptions{TopK: 0})
		require.NoError(t, err)
		defer ss.Close()
		assert.Equal(t, DefaultTopK, ss.TopK())
	})

	t.Run("topK above maximum", func(t *testing.T) {
		ss, err := NewSlotStore(db, SlotOptions{TopK: 1000})
		require.NoError(t, err)
		defer ss.Close()
		assert.Equal(t, MaxTopK, ss.TopK())
	})
}

func TestSlotStore_Update_FirstOccurrence(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
	require.NoError(t, err)

	// Verify count is 1.0
	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, "main", values[0].Value)
	assert.InDelta(t, 1.0, values[0].Count, 0.001)
}

func TestSlotStore_Update_AccumulatesCount(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Update same value multiple times at same timestamp
	for i := 0; i < 5; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
		require.NoError(t, err)
	}

	// Count should be 5.0 (no decay since same timestamp)
	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.InDelta(t, 5.0, values[0].Count, 0.001)
}

func TestSlotStore_Update_WithDecay(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	// Use 1 hour tau for easier testing
	ss, err := NewSlotStore(db, SlotOptions{TauMs: 3600 * 1000, TopK: 20})
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	t0 := int64(1000000) // Start at a non-zero time to avoid edge cases
	tauMs := ss.TauMs()

	// First update at t0
	err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", t0)
	require.NoError(t, err)

	// Second update at t0 + tau (one time constant)
	t1 := t0 + tauMs
	err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", t1)
	require.NoError(t, err)

	// At t1, stored count should be: 1.0 * exp(-1) + 1.0 ≈ 1.368
	// When queried at t1, no additional decay is applied (same timestamp)
	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, t1)
	require.NoError(t, err)
	require.Len(t, values, 1)
	// The stored count is 1*exp(-1)+1 ≈ 1.368, and since query is at same time as last_ts, no decay
	assert.InDelta(t, 1.368, values[0].Count, 0.01)
}

func TestSlotStore_UpdateBoth(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)
	repoKey := "repo:/home/user/project"

	err = ss.UpdateBoth(ctx, "git checkout {}", 0, "feature", repoKey, nowMs)
	require.NoError(t, err)

	// Check global scope
	globalValues, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, globalValues, 1)
	assert.Equal(t, "feature", globalValues[0].Value)

	// Check repo scope
	repoValues, err := ss.GetTopValuesAt(ctx, repoKey, "git checkout {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, repoValues, 1)
	assert.Equal(t, "feature", repoValues[0].Value)
}

func TestSlotStore_UpdateBoth_NoRepoKey(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Empty repo key should only update global
	err = ss.UpdateBoth(ctx, "git checkout {}", 0, "main", "", nowMs)
	require.NoError(t, err)

	// Check global scope
	globalValues, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, globalValues, 1)
}

func TestSlotStore_GetTopValues_OrdersByCount(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Insert values with different counts
	for i := 0; i < 5; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
		require.NoError(t, err)
	}
	for i := 0; i < 3; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "develop", nowMs)
		require.NoError(t, err)
	}
	err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "feature", nowMs)
	require.NoError(t, err)

	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, values, 3)

	// Should be ordered by count descending
	assert.Equal(t, "main", values[0].Value)
	assert.InDelta(t, 5.0, values[0].Count, 0.001)
	assert.Equal(t, "develop", values[1].Value)
	assert.InDelta(t, 3.0, values[1].Count, 0.001)
	assert.Equal(t, "feature", values[2].Value)
	assert.InDelta(t, 1.0, values[2].Count, 0.001)
}

func TestSlotStore_GetTopValues_RespectsLimit(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Insert 5 different values
	branches := []string{"main", "develop", "feature", "hotfix", "release"}
	for _, branch := range branches {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, branch, nowMs)
		require.NoError(t, err)
	}

	// Request only 3
	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 3, nowMs)
	require.NoError(t, err)
	assert.Len(t, values, 3)
}

func TestSlotStore_GetTopValues_DecaysAtQueryTime(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, SlotOptions{TauMs: 3600 * 1000, TopK: 20})
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	t0 := int64(1000000)
	tauMs := ss.TauMs()

	// Insert at t0
	err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", t0)
	require.NoError(t, err)

	// Query at t0 + tau
	t1 := t0 + tauMs
	values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "git checkout {}", 0, 10, t1)
	require.NoError(t, err)
	require.Len(t, values, 1)

	// Count should be decayed: 1.0 * exp(-1) ≈ 0.368
	assert.InDelta(t, 0.368, values[0].Count, 0.01)
}

func TestSlotStore_GetBestValue_PrefersRepoScope(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)
	repoKey := "repo:/home/user/project"

	// Add to global scope with higher count
	for i := 0; i < 10; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
		require.NoError(t, err)
	}

	// Add to repo scope with lower count but should still be preferred
	for i := 0; i < 5; i++ {
		err = ss.Update(ctx, repoKey, "git checkout {}", 0, "feature", nowMs)
		require.NoError(t, err)
	}

	// Should prefer repo-scoped value
	best, err := ss.GetBestValueAt(ctx, "git checkout {}", 0, repoKey, nowMs)
	require.NoError(t, err)
	assert.Equal(t, "feature", best)
}

func TestSlotStore_GetBestValue_FallsBackToGlobal(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)
	repoKey := "repo:/home/user/project"

	// Only add to global scope
	for i := 0; i < 5; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
		require.NoError(t, err)
	}

	// Should fall back to global
	best, err := ss.GetBestValueAt(ctx, "git checkout {}", 0, repoKey, nowMs)
	require.NoError(t, err)
	assert.Equal(t, "main", best)
}

func TestSlotStore_GetBestValue_RequiresConfidence(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add two values with similar counts (no clear winner)
	for i := 0; i < 3; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "develop", nowMs)
		require.NoError(t, err)
	}

	// 3 is not >= 2*2=4, so no confident value
	best, err := ss.GetBestValueAt(ctx, "git checkout {}", 0, "", nowMs)
	require.NoError(t, err)
	assert.Equal(t, "", best)
}

func TestSlotStore_GetBestValue_SingleValueIsConfident(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Single value with any count
	err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
	require.NoError(t, err)

	best, err := ss.GetBestValueAt(ctx, "git checkout {}", 0, "", nowMs)
	require.NoError(t, err)
	assert.Equal(t, "main", best)
}

func TestSlotStore_GetBestValue_HighConfidence(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Add values where first is >= 2x second
	for i := 0; i < 10; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "main", nowMs)
		require.NoError(t, err)
	}
	for i := 0; i < 4; i++ {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, "develop", nowMs)
		require.NoError(t, err)
	}

	// 10 >= 2*4=8, so main is confident
	best, err := ss.GetBestValueAt(ctx, "git checkout {}", 0, "", nowMs)
	require.NoError(t, err)
	assert.Equal(t, "main", best)
}

func TestSlotStore_GetBestValue_NoValues(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// No values at all
	best, err := ss.GetBestValueAt(ctx, "git checkout {}", 0, "", nowMs)
	require.NoError(t, err)
	assert.Equal(t, "", best)
}

func TestSlotStore_IsConfidentValue(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	tests := []struct {
		name     string
		values   []SlotValue
		expected bool
	}{
		{
			name:     "empty",
			values:   []SlotValue{},
			expected: false,
		},
		{
			name:     "single with count",
			values:   []SlotValue{{Value: "a", Count: 1.0}},
			expected: true,
		},
		{
			name:     "single with zero count",
			values:   []SlotValue{{Value: "a", Count: 0.0}},
			expected: false,
		},
		{
			name:     "two values - confident",
			values:   []SlotValue{{Value: "a", Count: 10.0}, {Value: "b", Count: 4.0}},
			expected: true,
		},
		{
			name:     "two values - not confident",
			values:   []SlotValue{{Value: "a", Count: 5.0}, {Value: "b", Count: 4.0}},
			expected: false,
		},
		{
			name:     "two values - exact 2x",
			values:   []SlotValue{{Value: "a", Count: 8.0}, {Value: "b", Count: 4.0}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ss.isConfidentValue(tt.values)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSlotStore_MultipleSlotIndices(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, DefaultSlotOptions())
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Update slot 0
	err = ss.Update(ctx, ScopeGlobal, "docker run {} {}", 0, "nginx", nowMs)
	require.NoError(t, err)

	// Update slot 1
	err = ss.Update(ctx, ScopeGlobal, "docker run {} {}", 1, "8080:80", nowMs)
	require.NoError(t, err)

	// Get slot 0 values
	slot0Values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "docker run {} {}", 0, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, slot0Values, 1)
	assert.Equal(t, "nginx", slot0Values[0].Value)

	// Get slot 1 values
	slot1Values, err := ss.GetTopValuesAt(ctx, ScopeGlobal, "docker run {} {}", 1, 10, nowMs)
	require.NoError(t, err)
	require.Len(t, slot1Values, 1)
	assert.Equal(t, "8080:80", slot1Values[0].Value)
}

func TestSlotStore_PruneSlot(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, SlotOptions{TopK: 3})
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Insert 5 values
	branches := []string{"main", "develop", "feature", "hotfix", "release"}
	for i, branch := range branches {
		// Give each a different count so ordering is deterministic
		for j := 0; j < 5-i; j++ {
			err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, branch, nowMs)
			require.NoError(t, err)
		}
	}

	// Verify we have 5 values before pruning
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ?
	`, ScopeGlobal, "git checkout {}", 0).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count)

	// Prune to top 3
	deleted, err := ss.PruneSlot(ctx, ScopeGlobal, "git checkout {}", 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	// Verify only 3 remain
	err = db.QueryRow(`
		SELECT COUNT(*) FROM slot_value
		WHERE scope = ? AND cmd_norm = ? AND slot_idx = ?
	`, ScopeGlobal, "git checkout {}", 0).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestSlotStore_PruneSlot_NothingToPrune(t *testing.T) {
	t.Parallel()

	db := createSlotTestDB(t)
	ss, err := NewSlotStore(db, SlotOptions{TopK: 10})
	require.NoError(t, err)
	defer ss.Close()

	ctx := context.Background()
	nowMs := int64(1000000)

	// Insert only 3 values
	branches := []string{"main", "develop", "feature"}
	for _, branch := range branches {
		err = ss.Update(ctx, ScopeGlobal, "git checkout {}", 0, branch, nowMs)
		require.NoError(t, err)
	}

	// Prune with topK=10 should delete nothing
	deleted, err := ss.PruneSlot(ctx, ScopeGlobal, "git checkout {}", 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestSlotStore_Constants(t *testing.T) {
	t.Parallel()

	// Verify constants match spec Section 9.3.3
	assert.Equal(t, 20, DefaultTopK)
	assert.Equal(t, 1, MinTopK)
	assert.Equal(t, 100, MaxTopK)
	assert.Equal(t, DefaultTauMs, DefaultSlotTauMs)
}

func TestSortSlotValues(t *testing.T) {
	t.Parallel()

	values := []SlotValue{
		{Value: "c", Count: 1.0},
		{Value: "a", Count: 5.0},
		{Value: "b", Count: 3.0},
	}

	sortSlotValues(values)

	assert.Equal(t, "a", values[0].Value)
	assert.Equal(t, "b", values[1].Value)
	assert.Equal(t, "c", values[2].Value)
}
