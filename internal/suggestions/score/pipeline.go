// Package score provides scoring and aggregate tracking for the clai suggestions engine.
// PipelineStore implements pipeline-aware suggestion retrieval per spec Section 20.2.
package score

import (
	"context"
	"database/sql"
	"errors"
)

// PipelineCompletion represents a suggested next segment in a pipeline.
type PipelineCompletion struct {
	// NextTemplateID is the template ID of the next pipeline segment.
	NextTemplateID string

	// NextCmdNorm is the normalized command of the next segment.
	NextCmdNorm string

	// Operator is the pipeline operator connecting to the next segment (e.g., "|", "&&").
	Operator string

	// Weight is the decayed frequency weight.
	Weight float64

	// Count is the raw occurrence count.
	Count int
}

// PipelinePattern represents a full pipeline pattern with its display form.
type PipelinePattern struct {
	// PatternHash uniquely identifies this pipeline pattern.
	PatternHash string

	// TemplateChain is the pipe-separated list of template IDs.
	TemplateChain string

	// OperatorChain is the comma-separated list of operators.
	OperatorChain string

	// CmdNormDisplay is the human-readable normalized pipeline command.
	CmdNormDisplay string

	// Count is the occurrence count.
	Count int

	// LastSeenMs is the last time this pattern was seen.
	LastSeenMs int64
}

// PipelineStore provides query access to pipeline awareness tables.
type PipelineStore struct {
	db *sql.DB
}

// NewPipelineStore creates a new PipelineStore with the given database connection.
func NewPipelineStore(db *sql.DB) *PipelineStore {
	return &PipelineStore{db: db}
}

// GetNextSegments retrieves the most common next segments that follow a given template
// in pipeline contexts. This enables pipeline completion suggestions.
//
// For example, if the user typed "grep pattern", this might return "sort" and "wc -l"
// as common next segments that follow grep in pipelines.
func (ps *PipelineStore) GetNextSegments(ctx context.Context, scope, prevTemplateID, operator string, limit int) ([]PipelineCompletion, error) {
	if limit <= 0 {
		limit = 5
	}

	query := `
		SELECT pt.next_template_id, pt.operator, pt.weight, pt.count,
		       COALESCE(ct.cmd_norm, '') as cmd_norm
		FROM pipeline_transition pt
		LEFT JOIN command_template ct ON ct.template_id = pt.next_template_id
		WHERE pt.scope = ? AND pt.prev_template_id = ?
	`
	args := []interface{}{scope, prevTemplateID}

	if operator != "" {
		query += ` AND pt.operator = ?`
		args = append(args, operator)
	}

	query += ` ORDER BY pt.weight DESC LIMIT ?`
	args = append(args, limit)

	rows, err := ps.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PipelineCompletion
	for rows.Next() {
		var pc PipelineCompletion
		if err := rows.Scan(&pc.NextTemplateID, &pc.Operator, &pc.Weight, &pc.Count, &pc.NextCmdNorm); err != nil {
			return nil, err
		}
		results = append(results, pc)
	}

	return results, rows.Err()
}

// GetTopPipelinePatterns retrieves the most common full pipeline patterns
// in the given scope. These are complete pipeline commands that can be
// suggested as whole-command completions.
func (ps *PipelineStore) GetTopPipelinePatterns(ctx context.Context, scope string, minCount, limit int) ([]PipelinePattern, error) {
	if limit <= 0 {
		limit = 10
	}
	if minCount <= 0 {
		minCount = 1
	}

	rows, err := ps.db.QueryContext(ctx, `
		SELECT pattern_hash, template_chain, operator_chain, cmd_norm_display, count, last_seen_ms
		FROM pipeline_pattern
		WHERE scope = ? AND count >= ?
		ORDER BY count DESC, last_seen_ms DESC
		LIMIT ?
	`, scope, minCount, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PipelinePattern
	for rows.Next() {
		var pp PipelinePattern
		if err := rows.Scan(&pp.PatternHash, &pp.TemplateChain, &pp.OperatorChain, &pp.CmdNormDisplay, &pp.Count, &pp.LastSeenMs); err != nil {
			return nil, err
		}
		results = append(results, pp)
	}

	return results, rows.Err()
}

// GetPipelinePatternsStartingWith retrieves pipeline patterns where the first
// segment matches the given template ID. This is useful for suggesting pipeline
// completions when the user has typed the first command.
func (ps *PipelineStore) GetPipelinePatternsStartingWith(ctx context.Context, scope, firstTemplateID string, minCount, limit int) ([]PipelinePattern, error) {
	if limit <= 0 {
		limit = 5
	}
	if minCount <= 0 {
		minCount = 1
	}

	// template_chain starts with the first template ID followed by "|"
	prefix := firstTemplateID + "|"

	rows, err := ps.db.QueryContext(ctx, `
		SELECT pattern_hash, template_chain, operator_chain, cmd_norm_display, count, last_seen_ms
		FROM pipeline_pattern
		WHERE scope = ? AND template_chain LIKE ? AND count >= ?
		ORDER BY count DESC, last_seen_ms DESC
		LIMIT ?
	`, scope, prefix+"%", minCount, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PipelinePattern
	for rows.Next() {
		var pp PipelinePattern
		if err := rows.Scan(&pp.PatternHash, &pp.TemplateChain, &pp.OperatorChain, &pp.CmdNormDisplay, &pp.Count, &pp.LastSeenMs); err != nil {
			return nil, err
		}
		results = append(results, pp)
	}

	return results, rows.Err()
}

// GetPipelineSegmentCount returns the total number of pipeline events recorded
// for a given command event ID. Returns 0 if the event has no pipeline segments.
func (ps *PipelineStore) GetPipelineSegmentCount(ctx context.Context, commandEventID int64) (int, error) {
	var count int
	err := ps.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pipeline_event WHERE command_event_id = ?
	`, commandEventID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}
