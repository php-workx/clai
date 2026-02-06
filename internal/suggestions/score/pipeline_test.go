package score

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/db"
)

func newPipelineTestDB(t *testing.T) *db.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "suggestions_v2.db")

	d, err := db.Open(context.Background(), db.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	// Insert a session for foreign key references
	_, err = d.DB().ExecContext(context.Background(), `
		INSERT INTO session (id, shell, started_at_ms) VALUES ('test-session', 'zsh', 1000)
	`)
	require.NoError(t, err)

	return d
}

func TestPipelineStore_GetNextSegments_Empty(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	results, err := ps.GetNextSegments(ctx, "global", "nonexistent-template", "|", 5)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestPipelineStore_GetNextSegments(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	// Insert pipeline transitions
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
		VALUES
			('global', 'tpl-grep', 'tpl-sort', '|', 5.0, 5, 1000),
			('global', 'tpl-grep', 'tpl-wc', '|', 3.0, 3, 900),
			('global', 'tpl-grep', 'tpl-head', '|', 1.0, 1, 800)
	`)
	require.NoError(t, err)

	// Insert command templates for the cmd_norm lookup
	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES
			('tpl-sort', 'sort', 'null', 0, 1000, 1000),
			('tpl-wc', 'wc -l', 'null', 0, 1000, 1000),
			('tpl-head', 'head -n <NUM>', 'null', 1, 1000, 1000)
	`)
	require.NoError(t, err)

	results, err := ps.GetNextSegments(ctx, "global", "tpl-grep", "|", 5)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Results should be sorted by weight descending
	assert.Equal(t, "tpl-sort", results[0].NextTemplateID)
	assert.Equal(t, "sort", results[0].NextCmdNorm)
	assert.Equal(t, 5.0, results[0].Weight)
	assert.Equal(t, 5, results[0].Count)

	assert.Equal(t, "tpl-wc", results[1].NextTemplateID)
	assert.Equal(t, "wc -l", results[1].NextCmdNorm)
}

func TestPipelineStore_GetNextSegments_FilterByOperator(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	// Insert transitions with different operators
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
		VALUES
			('global', 'tpl-make', 'tpl-test', '&&', 5.0, 5, 1000),
			('global', 'tpl-make', 'tpl-echo', '|', 2.0, 2, 900)
	`)
	require.NoError(t, err)

	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES
			('tpl-test', 'make test', 'null', 0, 1000, 1000),
			('tpl-echo', 'echo <arg>', 'null', 1, 1000, 1000)
	`)
	require.NoError(t, err)

	// Only "&&" transitions
	results, err := ps.GetNextSegments(ctx, "global", "tpl-make", "&&", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tpl-test", results[0].NextTemplateID)
	assert.Equal(t, "&&", results[0].Operator)
}

func TestPipelineStore_GetNextSegments_AllOperators(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
		VALUES
			('global', 'tpl-make', 'tpl-test', '&&', 5.0, 5, 1000),
			('global', 'tpl-make', 'tpl-echo', '|', 2.0, 2, 900)
	`)
	require.NoError(t, err)

	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES
			('tpl-test', 'make test', 'null', 0, 1000, 1000),
			('tpl-echo', 'echo <arg>', 'null', 1, 1000, 1000)
	`)
	require.NoError(t, err)

	// Empty operator string means all operators
	results, err := ps.GetNextSegments(ctx, "global", "tpl-make", "", 5)
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestPipelineStore_GetTopPipelinePatterns(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	// Insert pipeline patterns
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO pipeline_pattern (pattern_hash, template_chain, operator_chain, scope, count, last_seen_ms, cmd_norm_display)
		VALUES
			('hash1', 'tpl-grep|tpl-sort', '|', 'global', 10, 2000, 'grep <arg> | sort'),
			('hash2', 'tpl-cat|tpl-grep|tpl-wc', '|,|', 'global', 5, 1500, 'cat <PATH> | grep <arg> | wc -l'),
			('hash3', 'tpl-make|tpl-test', '&&', 'global', 1, 1000, 'make build && make test')
	`)
	require.NoError(t, err)

	// Get all patterns with count >= 1
	results, err := ps.GetTopPipelinePatterns(ctx, "global", 1, 10)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Should be sorted by count DESC
	assert.Equal(t, "hash1", results[0].PatternHash)
	assert.Equal(t, 10, results[0].Count)
	assert.Equal(t, "grep <arg> | sort", results[0].CmdNormDisplay)

	// With minCount >= 5, should exclude the last one
	results, err = ps.GetTopPipelinePatterns(ctx, "global", 5, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestPipelineStore_GetPipelinePatternsStartingWith(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	// Insert pipeline patterns
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO pipeline_pattern (pattern_hash, template_chain, operator_chain, scope, count, last_seen_ms, cmd_norm_display)
		VALUES
			('hash1', 'tpl-grep|tpl-sort', '|', 'global', 10, 2000, 'grep <arg> | sort'),
			('hash2', 'tpl-grep|tpl-wc', '|', 'global', 5, 1500, 'grep <arg> | wc -l'),
			('hash3', 'tpl-cat|tpl-grep', '|', 'global', 3, 1000, 'cat <PATH> | grep <arg>')
	`)
	require.NoError(t, err)

	// Get patterns starting with tpl-grep
	results, err := ps.GetPipelinePatternsStartingWith(ctx, "global", "tpl-grep", 1, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, "hash1", results[0].PatternHash)
	assert.Equal(t, "hash2", results[1].PatternHash)
}

func TestPipelineStore_GetPipelineSegmentCount(t *testing.T) {
	t.Parallel()
	d := newPipelineTestDB(t)
	ps := NewPipelineStore(d.DB())
	ctx := context.Background()

	// Insert a command event
	_, err := d.DB().ExecContext(ctx, `
		INSERT INTO command_event (session_id, ts_ms, cwd, cmd_raw, cmd_norm, template_id, exit_code)
		VALUES ('test-session', 1000, '/tmp', 'cat file | grep x', 'cat <PATH> | grep <arg>', 'tpl-pipe', 0)
	`)
	require.NoError(t, err)

	// Insert pipeline event rows
	_, err = d.DB().ExecContext(ctx, `
		INSERT INTO pipeline_event (command_event_id, position, operator, cmd_raw, cmd_norm, template_id)
		VALUES
			(1, 0, '|', 'cat file', 'cat <PATH>', 'tpl-cat'),
			(1, 1, NULL, 'grep x', 'grep <arg>', 'tpl-grep')
	`)
	require.NoError(t, err)

	count, err := ps.GetPipelineSegmentCount(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Non-existent event ID
	count, err = ps.GetPipelineSegmentCount(ctx, 999)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
