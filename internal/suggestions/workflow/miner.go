// Package workflow provides workflow detection and session activation for clai.
// It mines command history for recurring multi-step sequences and activates
// matching workflows during user sessions to provide next-step suggestions.
//
// Per spec appendix 20.4:
//   - Background miner detects repeated command sequences (3-6 steps)
//   - Supports contiguous and non-contiguous sequences (max gap of 2 commands)
//   - Stores patterns in workflow_pattern / workflow_step tables
//   - Promotes patterns above configurable occurrence threshold
package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MinerConfig holds configuration for the workflow miner.
type MinerConfig struct {
	// MinSteps is the minimum number of steps in a workflow (default: 3).
	MinSteps int

	// MaxSteps is the maximum number of steps in a workflow (default: 6).
	MaxSteps int

	// MinOccurrences is the minimum number of times a pattern must appear
	// before it is stored/promoted (default: 3).
	MinOccurrences int

	// MaxGap is the maximum number of non-matching commands between
	// workflow steps in non-contiguous matching (default: 2).
	MaxGap int

	// MineIntervalMs is the interval between mining runs in milliseconds.
	MineIntervalMs int

	// Scope is the scope to use for storing patterns (e.g., "global" or a repo key).
	Scope string
}

// DefaultMinerConfig returns the default miner configuration.
func DefaultMinerConfig() MinerConfig {
	return MinerConfig{
		MinSteps:       3,
		MaxSteps:       6,
		MinOccurrences: 3,
		MaxGap:         2,
		MineIntervalMs: 600000, // 10 minutes
		Scope:          "global",
	}
}

// Miner detects recurring multi-step command sequences from history.
// It runs periodically in the background, scanning command_event rows
// for repeated template_id sequences.
type Miner struct {
	db     *sql.DB
	cfg    MinerConfig
	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewMiner creates a new workflow miner.
func NewMiner(db *sql.DB, cfg MinerConfig) *Miner {
	return &Miner{
		db:     db,
		cfg:    cfg,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start begins periodic background mining. Call Stop to terminate.
func (m *Miner) Start() {
	go m.run()
}

// Stop halts the background miner and waits for it to finish.
func (m *Miner) Stop() {
	close(m.stopCh)
	<-m.doneCh
}

// run is the main mining loop.
func (m *Miner) run() {
	defer close(m.doneCh)

	// Run once immediately on start.
	m.mineOnce(context.Background())

	interval := time.Duration(m.cfg.MineIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.mineOnce(context.Background())
		}
	}
}

// MineOnce runs a single mining pass. Exported for testing.
func (m *Miner) MineOnce(ctx context.Context) {
	m.mineOnce(ctx)
}

// mineOnce performs one mining pass over recent command history.
func (m *Miner) mineOnce(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Fetch recent sessions with enough commands for workflow detection.
	sessions, err := m.fetchSessions(ctx)
	if err != nil {
		return
	}

	// For each session, extract template_id sequences and find patterns.
	patternCounts := make(map[string]*candidatePattern)
	for _, sess := range sessions {
		templates, err := m.fetchSessionTemplates(ctx, sess)
		if err != nil {
			continue
		}
		if len(templates) < m.cfg.MinSteps {
			continue
		}

		// Extract contiguous subsequences.
		m.extractContiguous(templates, patternCounts)

		// Extract non-contiguous subsequences (with gap tolerance).
		m.extractNonContiguous(templates, patternCounts)
	}

	// Store/update patterns that meet the occurrence threshold.
	for _, cp := range patternCounts {
		if cp.count >= m.cfg.MinOccurrences {
			if err := m.storePattern(ctx, cp); err != nil {
				continue
			}
		}
	}
}

// templateEntry is a single command event with its template_id and display text.
type templateEntry struct {
	templateID string
	cmdNorm    string
	tsMs       int64
}

// candidatePattern is a workflow pattern candidate being counted.
type candidatePattern struct {
	templateIDs []string
	cmdNorms    []string
	count       int
	lastSeenMs  int64
}

// patternKey computes a unique key for a sequence of template IDs.
func patternKey(templateIDs []string) string {
	return strings.Join(templateIDs, "|")
}

// patternHash computes a stable hash for a workflow pattern.
func patternHash(templateIDs []string) string {
	h := sha256.Sum256([]byte(patternKey(templateIDs)))
	return fmt.Sprintf("%x", h[:16]) // Use first 16 bytes (32 hex chars)
}

// fetchSessions returns session IDs that have enough commands for mining.
func (m *Miner) fetchSessions(ctx context.Context) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT session_id, COUNT(*) as cnt
		FROM command_event
		WHERE template_id IS NOT NULL AND template_id != ''
		GROUP BY session_id
		HAVING cnt >= ?
		ORDER BY MAX(ts_ms) DESC
		LIMIT 100
	`, m.cfg.MinSteps)
	if err != nil {
		return nil, fmt.Errorf("fetch sessions: %w", err)
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var sid string
		var cnt int
		if err := rows.Scan(&sid, &cnt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, sid)
	}
	return sessions, rows.Err()
}

// fetchSessionTemplates returns the template sequence for a session.
func (m *Miner) fetchSessionTemplates(ctx context.Context, sessionID string) ([]templateEntry, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT template_id, cmd_norm, ts_ms
		FROM command_event
		WHERE session_id = ? AND template_id IS NOT NULL AND template_id != ''
		ORDER BY ts_ms ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("fetch templates: %w", err)
	}
	defer rows.Close()

	var entries []templateEntry
	for rows.Next() {
		var e templateEntry
		if err := rows.Scan(&e.templateID, &e.cmdNorm, &e.tsMs); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// extractContiguous finds contiguous subsequences of length minSteps..maxSteps.
func (m *Miner) extractContiguous(entries []templateEntry, counts map[string]*candidatePattern) {
	for length := m.cfg.MinSteps; length <= m.cfg.MaxSteps; length++ {
		for start := 0; start+length <= len(entries); start++ {
			sub := entries[start : start+length]
			ids := make([]string, length)
			norms := make([]string, length)
			for i, e := range sub {
				ids[i] = e.templateID
				norms[i] = e.cmdNorm
			}

			key := patternKey(ids)
			if cp, ok := counts[key]; ok {
				cp.count++
				if sub[length-1].tsMs > cp.lastSeenMs {
					cp.lastSeenMs = sub[length-1].tsMs
				}
			} else {
				counts[key] = &candidatePattern{
					templateIDs: ids,
					cmdNorms:    norms,
					count:       1,
					lastSeenMs:  sub[length-1].tsMs,
				}
			}
		}
	}
}

// extractNonContiguous finds non-contiguous subsequences where gaps between
// matching steps are at most maxGap commands.
func (m *Miner) extractNonContiguous(entries []templateEntry, counts map[string]*candidatePattern) {
	// Use a sliding window approach. For each starting position, try to build
	// subsequences by allowing gaps.
	n := len(entries)

	for start := 0; start < n; start++ {
		// Build subsequences starting at this position.
		m.buildNonContiguousFrom(entries, start, []int{start}, counts)
	}
}

// buildNonContiguousFrom recursively builds non-contiguous subsequences.
func (m *Miner) buildNonContiguousFrom(
	entries []templateEntry,
	lastIdx int,
	indices []int,
	counts map[string]*candidatePattern,
) {
	// If we have enough steps, record this pattern.
	if len(indices) >= m.cfg.MinSteps {
		ids := make([]string, len(indices))
		norms := make([]string, len(indices))
		for i, idx := range indices {
			ids[i] = entries[idx].templateID
			norms[i] = entries[idx].cmdNorm
		}

		key := patternKey(ids)
		if cp, ok := counts[key]; ok {
			cp.count++
			lastTs := entries[indices[len(indices)-1]].tsMs
			if lastTs > cp.lastSeenMs {
				cp.lastSeenMs = lastTs
			}
		} else {
			counts[key] = &candidatePattern{
				templateIDs: ids,
				cmdNorms:    norms,
				count:       1,
				lastSeenMs:  entries[indices[len(indices)-1]].tsMs,
			}
		}
	}

	// If we have reached max steps, stop extending.
	if len(indices) >= m.cfg.MaxSteps {
		return
	}

	// Try extending by looking at positions after lastIdx, within gap tolerance.
	// The gap is the number of skipped positions (not matching our pattern).
	maxNext := lastIdx + m.cfg.MaxGap + 1
	if maxNext >= len(entries) {
		maxNext = len(entries) - 1
	}

	for next := lastIdx + 1; next <= maxNext; next++ {
		// Skip if it would be contiguous AND same template (already counted above).
		if next == lastIdx+1 && len(indices) >= m.cfg.MinSteps {
			// Contiguous extensions of already-counted patterns are fine for
			// building longer sequences; they won't create duplicates because
			// the key includes all template IDs.
		}
		m.buildNonContiguousFrom(entries, next, append(indices, next), counts)
	}
}

// storePattern persists a workflow pattern and its steps to the database.
func (m *Miner) storePattern(ctx context.Context, cp *candidatePattern) error {
	pid := patternHash(cp.templateIDs)

	templateChainJSON, err := json.Marshal(cp.templateIDs)
	if err != nil {
		return fmt.Errorf("marshal template chain: %w", err)
	}

	displayChainJSON, err := json.Marshal(cp.cmdNorms)
	if err != nil {
		return fmt.Errorf("marshal display chain: %w", err)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Upsert workflow_pattern.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workflow_pattern (pattern_id, template_chain, display_chain, scope, step_count, occurrence_count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pattern_id) DO UPDATE SET
			occurrence_count = excluded.occurrence_count,
			last_seen_ms = MAX(workflow_pattern.last_seen_ms, excluded.last_seen_ms)
	`, pid, string(templateChainJSON), string(displayChainJSON), m.cfg.Scope,
		len(cp.templateIDs), cp.count, cp.lastSeenMs)
	if err != nil {
		return fmt.Errorf("upsert pattern: %w", err)
	}

	// Upsert workflow_step rows.
	for i, tid := range cp.templateIDs {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO workflow_step (pattern_id, step_index, template_id)
			VALUES (?, ?, ?)
			ON CONFLICT(pattern_id, step_index) DO UPDATE SET
				template_id = excluded.template_id
		`, pid, i, tid)
		if err != nil {
			return fmt.Errorf("upsert step %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// LoadPromotedPatterns loads all workflow patterns that meet the min occurrence threshold.
func LoadPromotedPatterns(ctx context.Context, db *sql.DB, minOccurrences int) ([]Pattern, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT pattern_id, template_chain, display_chain, scope, step_count, occurrence_count, last_seen_ms
		FROM workflow_pattern
		WHERE occurrence_count >= ?
		ORDER BY occurrence_count DESC
	`, minOccurrences)
	if err != nil {
		return nil, fmt.Errorf("load patterns: %w", err)
	}
	defer rows.Close()

	var patterns []Pattern
	for rows.Next() {
		var p Pattern
		var templateChainJSON, displayChainJSON string
		if err := rows.Scan(&p.PatternID, &templateChainJSON, &displayChainJSON,
			&p.Scope, &p.StepCount, &p.OccurrenceCount, &p.LastSeenMs); err != nil {
			return nil, fmt.Errorf("scan pattern: %w", err)
		}

		if err := json.Unmarshal([]byte(templateChainJSON), &p.TemplateIDs); err != nil {
			return nil, fmt.Errorf("unmarshal template chain: %w", err)
		}
		if err := json.Unmarshal([]byte(displayChainJSON), &p.DisplayNames); err != nil {
			return nil, fmt.Errorf("unmarshal display chain: %w", err)
		}

		patterns = append(patterns, p)
	}

	return patterns, rows.Err()
}

// Pattern is a detected workflow pattern stored in the database.
type Pattern struct {
	PatternID       string   `json:"pattern_id"`
	TemplateIDs     []string `json:"template_ids"`
	DisplayNames    []string `json:"display_names"`
	Scope           string   `json:"scope"`
	StepCount       int      `json:"step_count"`
	OccurrenceCount int      `json:"occurrence_count"`
	LastSeenMs      int64    `json:"last_seen_ms"`
}
