// Package backfill provides bulk seeding of V2 suggestion tables from imported
// shell history. It normalizes commands in parallel, deduplicates templates in
// memory, and writes all aggregates in a single atomic transaction.
package backfill

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/suggestions/normalize"
)

// tauMs is the decay time constant (7 days in milliseconds), matching the
// WritePath default from the ingest package.
const tauMs = 7 * 24 * 60 * 60 * 1000

// scopeGlobal is the global scope key for aggregate tables.
const scopeGlobal = "global"

// normalizedEntry holds the result of parallel normalization for one history entry.
type normalizedEntry struct {
	cmdRaw  string
	preNorm normalize.PreNormResult
	tsMs    int64
	index   int // original position in sorted entries
}

// templateInfo tracks aggregate data for a unique command template during the
// in-memory dedup phase.
type templateInfo struct {
	cmdNorm   string
	tags      []string
	slotCount int

	firstSeenMs     int64
	lastSeenMs      int64
	occurrenceCount int

	// timestamps of every occurrence (chronological) for decay calculation
	timestamps []int64
}

// transitionKey identifies a directed bigram between two command templates.
type transitionKey struct {
	prevTemplateID string
	nextTemplateID string
}

// pipelineSegmentInfo holds pipeline normalization info for a single segment.
type pipelineSegmentInfo struct {
	raw        string
	norm       string
	templateID string
	operator   string
}

// Seed bulk-inserts imported shell history into the V2 suggestion tables.
// It is idempotent: if a backfill session for the given shell already exists,
// it returns nil without modifying the database.
//
// The algorithm runs in four phases:
//  1. Parallel normalize (CPU-bound workers)
//  2. Deduplicate templates (in-memory maps)
//  3. Batch SQL (single BEGIN IMMEDIATE transaction)
//  4. Commit
func Seed(ctx context.Context, db *sql.DB, entries []history.ImportEntry, shell string) error {
	if len(entries) == 0 {
		return nil
	}

	sessionID := "backfill-" + shell
	seeded, err := isBackfillSeeded(ctx, db, sessionID)
	if err != nil {
		return fmt.Errorf("idempotency check: %w", err)
	}
	if seeded {
		return nil
	}

	sorted := sortImportEntries(entries)
	normalized := parallelNormalize(ctx, sorted)
	templates := buildTemplateAggregates(normalized)
	transitions, transitionLastMs := buildTransitionAggregates(normalized)
	return seedNormalizedEntries(ctx, db, shell, sessionID, normalized, templates, transitions, transitionLastMs)
}

func isBackfillSeeded(ctx context.Context, db *sql.DB, sessionID string) (bool, error) {
	var existingCount int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM command_event WHERE session_id = ?`, sessionID,
	).Scan(&existingCount)
	if err != nil {
		return false, err
	}
	return existingCount > 0, nil
}

func sortImportEntries(entries []history.ImportEntry) []history.ImportEntry {
	sorted := make([]history.ImportEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	return sorted
}

func buildTemplateAggregates(normalized []normalizedEntry) map[string]*templateInfo {
	templates := make(map[string]*templateInfo)
	for _, ne := range normalized {
		tid := ne.preNorm.TemplateID
		info, exists := templates[tid]
		if !exists {
			info = &templateInfo{
				cmdNorm:     ne.preNorm.CmdNorm,
				tags:        ne.preNorm.Tags,
				slotCount:   ne.preNorm.SlotCount,
				firstSeenMs: ne.tsMs,
				lastSeenMs:  ne.tsMs,
			}
			templates[tid] = info
		}
		info.occurrenceCount++
		info.timestamps = append(info.timestamps, ne.tsMs)
		if ne.tsMs < info.firstSeenMs {
			info.firstSeenMs = ne.tsMs
		}
		if ne.tsMs > info.lastSeenMs {
			info.lastSeenMs = ne.tsMs
		}
	}
	return templates
}

func buildTransitionAggregates(normalized []normalizedEntry) (map[transitionKey]int, map[transitionKey]int64) {
	transitions := make(map[transitionKey]int)
	transitionLastMs := make(map[transitionKey]int64)
	for i := 1; i < len(normalized); i++ {
		prev := normalized[i-1].preNorm.TemplateID
		next := normalized[i].preNorm.TemplateID
		key := transitionKey{prevTemplateID: prev, nextTemplateID: next}
		transitions[key]++
		transitionLastMs[key] = normalized[i].tsMs
	}
	return transitions, transitionLastMs
}

func seedNormalizedEntries(
	ctx context.Context,
	db *sql.DB,
	shell string,
	sessionID string,
	normalized []normalizedEntry,
	templates map[string]*templateInfo,
	transitions map[transitionKey]int,
	transitionLastMs map[transitionKey]int64,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := insertBackfillSession(ctx, tx, sessionID, shell, normalized[0].tsMs); err != nil {
		return err
	}
	eventIDs, err := insertCommandEvents(ctx, tx, normalized, sessionID)
	if err != nil {
		return fmt.Errorf("insert command_events: %w", err)
	}
	if err := insertCommandTemplates(ctx, tx, templates); err != nil {
		return fmt.Errorf("insert command_templates: %w", err)
	}
	if err := insertCommandStats(ctx, tx, templates); err != nil {
		return fmt.Errorf("insert command_stats: %w", err)
	}
	if err := insertTransitionStats(ctx, tx, transitions, transitionLastMs); err != nil {
		return fmt.Errorf("insert transition_stats: %w", err)
	}
	if err := insertPipelineData(ctx, tx, normalized, eventIDs); err != nil {
		return fmt.Errorf("insert pipeline data: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func insertBackfillSession(ctx context.Context, tx *sql.Tx, sessionID, shell string, startedAtMs int64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO session (id, shell, started_at_ms) VALUES (?, ?, ?)`,
		sessionID, shell, startedAtMs,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// parallelNormalize normalizes all entries using runtime.NumCPU() workers.
// Each worker gets its own Normalizer to avoid contention.
func parallelNormalize(ctx context.Context, entries []history.ImportEntry) []normalizedEntry {
	n := len(entries)
	result := make([]normalizedEntry, n)

	numWorkers := runtime.NumCPU()
	if numWorkers > n {
		numWorkers = n
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	var wg sync.WaitGroup
	chunkSize := (n + numWorkers - 1) / numWorkers

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > n {
			end = n
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()

			for i := start; i < end; i++ {
				entry := entries[i]

				// Sanitize malformed UTF-8 to prevent panics.
				cmdRaw := sanitizeUTF8(entry.Command)

				preNorm := normalize.PreNormalize(cmdRaw, normalize.PreNormConfig{})

				tsMs := entry.Timestamp.UnixMilli()
				if entry.Timestamp.IsZero() {
					tsMs = 0
				}

				result[i] = normalizedEntry{
					cmdRaw:  cmdRaw,
					preNorm: preNorm,
					tsMs:    tsMs,
					index:   i,
				}
			}
		}(start, end)
	}

	wg.Wait()
	return result
}

// sanitizeUTF8 replaces invalid UTF-8 sequences with the Unicode replacement
// character, matching the behavior of ingest.ToLossyUTF8.
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune('\uFFFD')
			i++
		} else {
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

// insertCommandEvents inserts all command_event rows and returns a slice of
// their auto-generated IDs (parallel to normalized entries).
func insertCommandEvents(ctx context.Context, tx *sql.Tx, entries []normalizedEntry, sessionID string) ([]int64, error) {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO command_event (
			session_id, ts_ms, cwd, repo_key, branch,
			cmd_raw, cmd_norm, cmd_truncated, template_id,
			exit_code, duration_ms, ephemeral
		) VALUES (?, ?, ?, NULL, NULL, ?, ?, ?, ?, NULL, NULL, 0)
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	ids := make([]int64, len(entries))
	for i, ne := range entries {
		truncated := 0
		if ne.preNorm.Truncated {
			truncated = 1
		}

		res, err := stmt.ExecContext(ctx,
			sessionID,
			ne.tsMs,
			"/",
			ne.cmdRaw,
			ne.preNorm.CmdNorm,
			truncated,
			ne.preNorm.TemplateID,
		)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("row %d last_insert_id: %w", i, err)
		}
		ids[i] = id
	}
	return ids, nil
}

// insertCommandTemplates inserts unique command_template rows.
func insertCommandTemplates(ctx context.Context, tx *sql.Tx, templates map[string]*templateInfo) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO command_template (
			template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for tid, info := range templates {
		tagsJSON := "null"
		if len(info.tags) > 0 {
			b, err := json.Marshal(info.tags)
			if err != nil {
				return fmt.Errorf("marshal tags for %s: %w", tid, err)
			}
			tagsJSON = string(b)
		}

		_, err := stmt.ExecContext(ctx,
			tid,
			info.cmdNorm,
			tagsJSON,
			info.slotCount,
			info.firstSeenMs,
			info.lastSeenMs,
		)
		if err != nil {
			return fmt.Errorf("template %s: %w", tid, err)
		}
	}
	return nil
}

// insertCommandStats inserts command_stat rows with decayed frequency scores.
// For each template, the score is computed by iterating occurrences chronologically:
//
//	score = score * exp(-(t_new - t_old) / tauMs) + 1.0
func insertCommandStats(ctx context.Context, tx *sql.Tx, templates map[string]*templateInfo) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO command_stat (
			scope, template_id, score, success_count, failure_count, last_seen_ms
		) VALUES (?, ?, ?, ?, 0, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for tid, info := range templates {
		// Sort timestamps to compute decay correctly.
		ts := info.timestamps
		sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })

		var score float64
		var lastMs int64
		for i, t := range ts {
			if i == 0 {
				score = 1.0
			} else {
				elapsed := float64(t - lastMs)
				decay := math.Exp(-elapsed / float64(tauMs))
				score = score*decay + 1.0
			}
			lastMs = t
		}

		_, err := stmt.ExecContext(ctx,
			scopeGlobal,
			tid,
			score,
			info.occurrenceCount,
			info.lastSeenMs,
		)
		if err != nil {
			return fmt.Errorf("stat for %s: %w", tid, err)
		}
	}
	return nil
}

// insertTransitionStats inserts transition_stat rows for command bigrams.
func insertTransitionStats(ctx context.Context, tx *sql.Tx, transitions map[transitionKey]int, lastMs map[transitionKey]int64) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO transition_stat (
			scope, prev_template_id, next_template_id, weight, count, last_seen_ms
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, count := range transitions {
		_, err := stmt.ExecContext(ctx,
			scopeGlobal,
			key.prevTemplateID,
			key.nextTemplateID,
			float64(count), // weight = count for bulk backfill
			count,
			lastMs[key],
		)
		if err != nil {
			return fmt.Errorf("transition %s->%s: %w", key.prevTemplateID, key.nextTemplateID, err)
		}
	}
	return nil
}

// insertPipelineData inserts pipeline_event, pipeline_transition, and
// pipeline_pattern rows for commands that contain pipes or compound operators.
func insertPipelineData(ctx context.Context, tx *sql.Tx, entries []normalizedEntry, eventIDs []int64) error {
	stmts, err := preparePipelineStatements(ctx, tx)
	if err != nil {
		return err
	}
	defer stmts.close()

	normalizer := normalize.NewNormalizer()

	for i, ne := range entries {
		segments := ne.preNorm.Segments
		if len(segments) <= 1 {
			continue
		}
		segInfos := buildPipelineSegmentInfos(normalizer, segments)
		if err := insertPipelineEventRows(ctx, stmts.eventStmt, eventIDs[i], i, segInfos); err != nil {
			return err
		}
		if err := insertPipelineTransitionRows(ctx, stmts.transStmt, i, ne.tsMs, segInfos); err != nil {
			return err
		}
		if err := insertPipelinePatternRow(ctx, stmts.patternStmt, i, ne, segments, segInfos); err != nil {
			return err
		}
	}

	return nil
}

type pipelineStatements struct {
	eventStmt   *sql.Stmt
	transStmt   *sql.Stmt
	patternStmt *sql.Stmt
}

func preparePipelineStatements(ctx context.Context, tx *sql.Tx) (*pipelineStatements, error) {
	eventStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO pipeline_event (
			command_event_id, position, operator, cmd_raw, cmd_norm, template_id
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	transStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO pipeline_transition (
			scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms
		) VALUES (?, ?, ?, ?, 1.0, 1, ?)
		ON CONFLICT(scope, prev_template_id, next_template_id, operator) DO UPDATE SET
			weight = weight + 1.0,
			count = count + 1,
			last_seen_ms = MAX(pipeline_transition.last_seen_ms, excluded.last_seen_ms)
	`)
	if err != nil {
		eventStmt.Close()
		return nil, err
	}
	patternStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO pipeline_pattern (
			pattern_hash, template_chain, operator_chain, scope, count, last_seen_ms, cmd_norm_display
		) VALUES (?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(pattern_hash) DO UPDATE SET
			count = count + 1,
			last_seen_ms = MAX(pipeline_pattern.last_seen_ms, excluded.last_seen_ms)
	`)
	if err != nil {
		eventStmt.Close()
		transStmt.Close()
		return nil, err
	}
	return &pipelineStatements{
		eventStmt:   eventStmt,
		transStmt:   transStmt,
		patternStmt: patternStmt,
	}, nil
}

func (s *pipelineStatements) close() {
	if s == nil {
		return
	}
	s.eventStmt.Close()
	s.transStmt.Close()
	s.patternStmt.Close()
}

func buildPipelineSegmentInfos(normalizer *normalize.Normalizer, segments []normalize.Segment) []pipelineSegmentInfo {
	segInfos := make([]pipelineSegmentInfo, len(segments))
	for j, seg := range segments {
		segNorm, _ := normalizer.Normalize(seg.Raw)
		operator := ""
		if seg.Operator != "" {
			operator = string(seg.Operator)
		}
		segInfos[j] = pipelineSegmentInfo{
			raw:        seg.Raw,
			norm:       segNorm,
			templateID: normalize.ComputeTemplateID(segNorm),
			operator:   operator,
		}
	}
	return segInfos
}

func insertPipelineEventRows(ctx context.Context, stmt *sql.Stmt, eventID int64, entryIndex int, segInfos []pipelineSegmentInfo) error {
	for j, si := range segInfos {
		opVal := sql.NullString{}
		if si.operator != "" {
			opVal = sql.NullString{String: si.operator, Valid: true}
		}
		_, err := stmt.ExecContext(ctx, eventID, j, opVal, si.raw, si.norm, si.templateID)
		if err != nil {
			return fmt.Errorf("pipeline_event entry %d seg %d: %w", entryIndex, j, err)
		}
	}
	return nil
}

func insertPipelineTransitionRows(ctx context.Context, stmt *sql.Stmt, entryIndex int, tsMs int64, segInfos []pipelineSegmentInfo) error {
	for j := 1; j < len(segInfos); j++ {
		prevOp := segInfos[j-1].operator
		if prevOp == "" {
			prevOp = "|"
		}
		_, err := stmt.ExecContext(ctx,
			scopeGlobal,
			segInfos[j-1].templateID,
			segInfos[j].templateID,
			prevOp,
			tsMs,
		)
		if err != nil {
			return fmt.Errorf("pipeline_transition entry %d seg %d: %w", entryIndex, j, err)
		}
	}
	return nil
}

func insertPipelinePatternRow(
	ctx context.Context,
	stmt *sql.Stmt,
	entryIndex int,
	ne normalizedEntry,
	segments []normalize.Segment,
	segInfos []pipelineSegmentInfo,
) error {
	tids := make([]string, len(segInfos))
	for j, si := range segInfos {
		tids[j] = si.templateID
	}
	templateChain := strings.Join(tids, "|")
	operatorChain := buildOperatorChain(segments)
	patternHash := computeHash(templateChain+":"+operatorChain) + "_" + scopeGlobal
	_, err := stmt.ExecContext(ctx,
		patternHash,
		templateChain,
		operatorChain,
		scopeGlobal,
		ne.tsMs,
		ne.preNorm.CmdNorm,
	)
	if err != nil {
		return fmt.Errorf("pipeline_pattern entry %d: %w", entryIndex, err)
	}
	return nil
}

// buildOperatorChain builds the operator chain string from segments,
// matching the format used by the ingest write path.
func buildOperatorChain(segments []normalize.Segment) string {
	ops := make([]string, 0, len(segments)-1)
	for i := 0; i < len(segments)-1; i++ {
		op := string(segments[i].Operator)
		if op == "" {
			op = "|"
		}
		ops = append(ops, op)
	}
	return strings.Join(ops, ",")
}

// computeHash returns a truncated SHA-256 hex hash of the input,
// matching the format used by the ingest write path.
func computeHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h[:16])
}
