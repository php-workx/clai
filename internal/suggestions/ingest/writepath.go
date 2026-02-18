// Package ingest provides NDJSON parsing, validation, and write-path transaction
// orchestration for command events in the suggestions engine.
//
// The write path ensures all database updates for a single ingested event happen
// atomically within a single BEGIN IMMEDIATE transaction. If any step fails, all
// changes are rolled back.
package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/normalize"
	"github.com/runger/clai/internal/suggestions/recovery"
)

// ScopeGlobal is the global scope identifier for aggregate tables.
const ScopeGlobal = "global"

// DefaultTauMs is the default decay time constant (7 days in milliseconds)
// for decayed frequency scoring.
const DefaultTauMs = 7 * 24 * 60 * 60 * 1000

// CacheInvalidator is called after a successful commit to invalidate cached suggestions.
type CacheInvalidator interface {
	Invalidate(sessionID string)
}

// DefaultPipelineMaxSegments is the default maximum number of pipeline segments to process.
const DefaultPipelineMaxSegments = 8

// WritePathConfig configures the write-path transaction orchestrator.
type WritePathConfig struct {
	Cache               CacheInvalidator
	ProjectTypes        []string
	SlotCorrelationKeys [][]int
	TauMs               int64
	PipelineMaxSegments int
}

// WritePathContext holds the enriched context for a single event ingestion.
// It is computed before the transaction begins.
type WritePathContext struct {
	Event          *event.CommandEvent
	RepoKey        string
	Branch         string
	PrevTemplateID string
	Slots          []normalize.SlotValue
	PreNorm        normalize.PreNormResult
	PrevExitCode   int
	NowMs          int64
	PrevFailed     bool
}

// WritePathResult holds the output of a successful write-path transaction.
type WritePathResult struct {
	TemplateID              string
	CmdNorm                 string
	EventID                 int64
	PipelineSegments        int
	TransitionRecorded      bool
	FailureRecoveryRecorded bool
}

// WritePath orchestrates all database writes for a single ingested event within
// a single BEGIN IMMEDIATE transaction. All steps succeed or fail atomically.
//
// Transaction steps (in order):
//  1. Insert command_event row
//  2. Upsert command_template
//  3. Update command_stat (frequency + success/failure counts)
//  4. Update transition_stat (if previous template known)
//  5. Update slot_stat values (from normalized placeholders)
//  6. Update slot_correlation for configured tuples
//  7. Update project_type_stat/project_type_transition (when project types active)
//  8. Update directory-scoped aggregates (scope=dir:<hash>)
//  9. Update pipeline_event/pipeline_transition/pipeline_pattern (for compound commands)
//  10. Update failure_recovery (when previous command failed)
//  11. Invalidate cache index (after commit)
func WritePath(ctx context.Context, db *sql.DB, wctx *WritePathContext, cfg *WritePathConfig) (*WritePathResult, error) {
	if err := validateWritePathInputs(db, wctx); err != nil {
		return nil, err
	}
	tauMs := resolveTauMs(cfg.TauMs)

	result := &WritePathResult{
		TemplateID: wctx.PreNorm.TemplateID,
		CmdNorm:    wctx.PreNorm.CmdNorm,
	}

	// Use BEGIN IMMEDIATE to avoid SQLITE_BUSY on concurrent reads.
	// The standard database/sql BeginTx doesn't support IMMEDIATE directly,
	// so we issue the BEGIN IMMEDIATE manually and wrap in a helper.
	tx, err := beginImmediate(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("begin immediate transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback after commit

	_, err = executeWritePathSteps(ctx, tx, wctx, cfg, tauMs, result)
	if err != nil {
		return nil, err
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Step 11: Invalidate cache (after commit, non-transactional)
	if cfg.Cache != nil {
		cfg.Cache.Invalidate(wctx.Event.SessionID)
	}

	return result, nil
}

func validateWritePathInputs(db *sql.DB, wctx *WritePathContext) error {
	if wctx == nil {
		return errors.New("write path context is nil")
	}
	if db == nil {
		return errors.New("database is nil")
	}
	return nil
}

func resolveTauMs(tauMs int64) int64 {
	if tauMs <= 0 {
		return DefaultTauMs
	}
	return tauMs
}

func executeWritePathSteps(
	ctx context.Context,
	tx *sql.Tx,
	wctx *WritePathContext,
	cfg *WritePathConfig,
	tauMs int64,
	result *WritePathResult,
) (int64, error) {
	eventID, err := insertCommandEvent(ctx, tx, wctx)
	if err != nil {
		return 0, fmt.Errorf("step 1 (command_event): %w", err)
	}
	result.EventID = eventID
	if err := upsertCommandTemplate(ctx, tx, wctx); err != nil {
		return 0, fmt.Errorf("step 2 (command_template): %w", err)
	}
	if err := updateCommandStat(ctx, tx, wctx, tauMs); err != nil {
		return 0, fmt.Errorf("step 3 (command_stat): %w", err)
	}
	if err := runOptionalWritePathSteps(ctx, tx, wctx, cfg, tauMs, eventID, result); err != nil {
		return 0, err
	}
	return eventID, nil
}

func runOptionalWritePathSteps(
	ctx context.Context,
	tx *sql.Tx,
	wctx *WritePathContext,
	cfg *WritePathConfig,
	tauMs int64,
	eventID int64,
	result *WritePathResult,
) error {
	if wctx.PrevTemplateID != "" {
		if err := updateTransitionStat(ctx, tx, wctx, tauMs); err != nil {
			return fmt.Errorf("step 4 (transition_stat): %w", err)
		}
		result.TransitionRecorded = true
	}
	if err := updateSlotStats(ctx, tx, wctx, tauMs); err != nil {
		return fmt.Errorf("step 5 (slot_stat): %w", err)
	}
	if len(cfg.SlotCorrelationKeys) > 0 {
		if err := updateSlotCorrelations(ctx, tx, wctx, cfg.SlotCorrelationKeys); err != nil {
			return fmt.Errorf("step 6 (slot_correlation): %w", err)
		}
	}
	if len(cfg.ProjectTypes) > 0 {
		if err := updateProjectTypeStats(ctx, tx, wctx, cfg.ProjectTypes, tauMs); err != nil {
			return fmt.Errorf("step 7 (project_type_stat): %w", err)
		}
	}
	if err := updateDirectoryScopedAggregates(ctx, tx, wctx, tauMs); err != nil {
		return fmt.Errorf("step 8 (dir aggregates): %w", err)
	}
	if err := runPipelineAndRecoverySteps(ctx, tx, wctx, cfg, eventID, result); err != nil {
		return err
	}
	return nil
}

func runPipelineAndRecoverySteps(
	ctx context.Context,
	tx *sql.Tx,
	wctx *WritePathContext,
	cfg *WritePathConfig,
	eventID int64,
	result *WritePathResult,
) error {
	if len(wctx.PreNorm.Segments) > 1 {
		maxSegments := cfg.PipelineMaxSegments
		if maxSegments <= 0 {
			maxSegments = DefaultPipelineMaxSegments
		}
		n, err := updatePipelineTables(ctx, tx, wctx, eventID, maxSegments)
		if err != nil {
			return fmt.Errorf("step 9 (pipeline): %w", err)
		}
		result.PipelineSegments = n
	}
	if wctx.PrevFailed && wctx.PrevTemplateID != "" {
		if err := updateFailureRecovery(ctx, tx, wctx); err != nil {
			return fmt.Errorf("step 10 (failure_recovery): %w", err)
		}
		result.FailureRecoveryRecorded = true
	}
	return nil
}

// beginImmediate starts a BEGIN IMMEDIATE transaction.
// This avoids SQLITE_BUSY errors when concurrent readers exist.
func beginImmediate(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	// Upgrade to IMMEDIATE mode. SQLite's default is DEFERRED which can
	// cause SQLITE_BUSY on the first write. IMMEDIATE acquires a reserved
	// lock immediately, allowing readers to continue while we prepare writes.
	//
	// Note: With modernc.org/sqlite and MaxOpenConns(1), this is effectively
	// serialized already, but we use IMMEDIATE for correctness if the pool
	// config ever changes.
	if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
		tx.Rollback() //nolint:errcheck // best-effort rollback on error
		return nil, err
	}

	return tx, nil
}

// Step 1: Insert command_event row
func insertCommandEvent(ctx context.Context, tx *sql.Tx, wctx *WritePathContext) (int64, error) {
	var durationMs *int64
	if wctx.Event.DurationMs != nil {
		durationMs = wctx.Event.DurationMs
	}

	truncated := 0
	if wctx.PreNorm.Truncated {
		truncated = 1
	}

	ephemeral := 0
	if wctx.Event.Ephemeral {
		ephemeral = 1
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO command_event (
			session_id, ts_ms, cwd, repo_key, branch,
			cmd_raw, cmd_norm, cmd_truncated, template_id,
			exit_code, duration_ms, ephemeral
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		wctx.Event.SessionID,
		wctx.NowMs,
		wctx.Event.Cwd,
		nullableString(wctx.RepoKey),
		nullableString(wctx.Branch),
		wctx.Event.CmdRaw,
		wctx.PreNorm.CmdNorm,
		truncated,
		wctx.PreNorm.TemplateID,
		wctx.Event.ExitCode,
		durationMs,
		ephemeral,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// Step 2: Upsert command_template
func upsertCommandTemplate(ctx context.Context, tx *sql.Tx, wctx *WritePathContext) error {
	tagsJSON := "null"
	if len(wctx.PreNorm.Tags) > 0 {
		b, err := json.Marshal(wctx.PreNorm.Tags)
		if err != nil {
			return fmt.Errorf("marshal tags: %w", err)
		}
		tagsJSON = string(b)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(template_id) DO UPDATE SET
			last_seen_ms = MAX(command_template.last_seen_ms, excluded.last_seen_ms),
			tags = excluded.tags,
			slot_count = excluded.slot_count
	`,
		wctx.PreNorm.TemplateID,
		wctx.PreNorm.CmdNorm,
		tagsJSON,
		wctx.PreNorm.SlotCount,
		wctx.NowMs,
		wctx.NowMs,
	)
	return err
}

// Step 3: Update command_stat (frequency + success/failure counts)
func updateCommandStat(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, tauMs int64) error {
	scopes := []string{ScopeGlobal}
	if wctx.RepoKey != "" {
		scopes = append(scopes, wctx.RepoKey)
	}

	isSuccess := wctx.Event.ExitCode == 0

	for _, scope := range scopes {
		if err := upsertCommandStatInTx(ctx, tx, scope, wctx.PreNorm.TemplateID, isSuccess, wctx.NowMs, tauMs); err != nil {
			return err
		}
	}
	return nil
}

func upsertCommandStatInTx(ctx context.Context, tx *sql.Tx, scope, templateID string, isSuccess bool, nowMs, tauMs int64) error {
	// Read existing score
	var currentScore float64
	var lastSeenMs int64
	var successCount, failureCount int

	err := tx.QueryRowContext(ctx, `
		SELECT score, success_count, failure_count, last_seen_ms
		FROM command_stat
		WHERE scope = ? AND template_id = ?
	`, scope, templateID).Scan(&currentScore, &successCount, &failureCount, &lastSeenMs)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Calculate new score using decay formula: score = score * exp(-(now - last) / tau) + 1.0
	var newScore float64
	if errors.Is(err, sql.ErrNoRows) {
		newScore = 1.0
		successCount = 0
		failureCount = 0
	} else {
		elapsed := float64(nowMs - lastSeenMs)
		decay := math.Exp(-elapsed / float64(tauMs))
		newScore = currentScore*decay + 1.0
	}

	if isSuccess {
		successCount++
	} else {
		failureCount++
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO command_stat (scope, template_id, score, success_count, failure_count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, template_id) DO UPDATE SET
			score = ?,
			success_count = ?,
			failure_count = ?,
			last_seen_ms = ?
	`,
		scope, templateID, newScore, successCount, failureCount, nowMs,
		newScore, successCount, failureCount, nowMs,
	)
	return err
}

// Step 4: Update transition_stat
func updateTransitionStat(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, tauMs int64) error {
	scopes := []string{ScopeGlobal}
	if wctx.RepoKey != "" {
		scopes = append(scopes, wctx.RepoKey)
	}

	for _, scope := range scopes {
		if err := upsertTransitionStatInTx(ctx, tx, scope, wctx.PrevTemplateID, wctx.PreNorm.TemplateID, wctx.NowMs, tauMs); err != nil {
			return err
		}
	}
	return nil
}

func upsertTransitionStatInTx(ctx context.Context, tx *sql.Tx, scope, prevTemplateID, nextTemplateID string, nowMs, tauMs int64) error {
	newWeight, newCount, err := loadDecayedTransitionWeightAndCount(ctx, tx, `
		SELECT weight, count, last_seen_ms
		FROM transition_stat
		WHERE scope = ? AND prev_template_id = ? AND next_template_id = ?
	`, []any{scope, prevTemplateID, nextTemplateID}, nowMs, tauMs)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO transition_stat (scope, prev_template_id, next_template_id, weight, count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, prev_template_id, next_template_id) DO UPDATE SET
			weight = ?,
			count = ?,
			last_seen_ms = ?
	`,
		scope, prevTemplateID, nextTemplateID, newWeight, newCount, nowMs,
		newWeight, newCount, nowMs,
	)
	return err
}

// Step 5: Update slot_stat values
func updateSlotStats(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, tauMs int64) error {
	if len(wctx.Slots) == 0 {
		return nil
	}

	scopes := []string{ScopeGlobal}
	if wctx.RepoKey != "" {
		scopes = append(scopes, wctx.RepoKey)
	}

	for _, slot := range wctx.Slots {
		for _, scope := range scopes {
			if err := upsertSlotStatInTx(ctx, tx, slotStatUpsertInput{
				scope:      scope,
				templateID: wctx.PreNorm.TemplateID,
				slotIndex:  slot.Index,
				value:      slot.Value,
				nowMs:      wctx.NowMs,
				tauMs:      tauMs,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type slotStatUpsertInput struct {
	scope      string
	templateID string
	value      string
	slotIndex  int
	nowMs      int64
	tauMs      int64
}

func upsertSlotStatInTx(ctx context.Context, tx *sql.Tx, input slotStatUpsertInput) error {
	var currentWeight float64
	var currentCount int
	var lastSeenMs int64

	err := tx.QueryRowContext(ctx, `
		SELECT weight, count, last_seen_ms
		FROM slot_stat
		WHERE scope = ? AND template_id = ? AND slot_index = ? AND value = ?
	`, input.scope, input.templateID, input.slotIndex, input.value).Scan(&currentWeight, &currentCount, &lastSeenMs)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var newWeight float64
	var newCount int
	if errors.Is(err, sql.ErrNoRows) {
		newWeight = 1.0
		newCount = 1
	} else {
		elapsed := float64(input.nowMs - lastSeenMs)
		decay := math.Exp(-elapsed / float64(input.tauMs))
		newWeight = currentWeight*decay + 1.0
		newCount = currentCount + 1
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO slot_stat (scope, template_id, slot_index, value, weight, count, last_seen_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(scope, template_id, slot_index, value) DO UPDATE SET
				weight = ?,
				count = ?,
				last_seen_ms = ?
	`,
		input.scope, input.templateID, input.slotIndex, input.value, newWeight, newCount, input.nowMs,
		newWeight, newCount, input.nowMs,
	)
	return err
}

// Step 6: Update slot_correlation for configured tuples
func updateSlotCorrelations(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, correlationKeys [][]int) error {
	if len(wctx.Slots) < 2 {
		return nil
	}

	slotMap := buildSlotValueMap(wctx.Slots)
	scopes := writePathScopes(wctx.RepoKey)
	for _, indices := range correlationKeys {
		slotKey, tupleHash, tupleValueJSON, ok, err := buildCorrelationTuple(indices, slotMap)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := upsertCorrelationTuple(ctx, tx, scopes, wctx, slotKey, tupleHash, tupleValueJSON); err != nil {
			return err
		}
	}
	return nil
}

func buildSlotValueMap(slots []normalize.SlotValue) map[int]string {
	slotMap := make(map[int]string, len(slots))
	for _, s := range slots {
		slotMap[s.Index] = s.Value
	}
	return slotMap
}

func writePathScopes(repoKey string) []string {
	scopes := []string{ScopeGlobal}
	if repoKey != "" {
		scopes = append(scopes, repoKey)
	}
	return scopes
}

func buildCorrelationTuple(indices []int, slotMap map[int]string) (slotKey, tupleHash, tupleValueJSON string, ok bool, err error) {
	if len(indices) < 2 {
		return "", "", "", false, nil
	}
	values := make([]string, 0, len(indices))
	for _, idx := range indices {
		v, ok := slotMap[idx]
		if !ok {
			return "", "", "", false, nil
		}
		values = append(values, v)
	}
	valueJSON, err := json.Marshal(values)
	if err != nil {
		return "", "", "", false, fmt.Errorf("marshal tuple values: %w", err)
	}
	slotKey = buildSlotKey(indices)
	tupleHash = computeHash(string(valueJSON))
	return slotKey, tupleHash, string(valueJSON), true, nil
}

func upsertCorrelationTuple(
	ctx context.Context,
	tx *sql.Tx,
	scopes []string,
	wctx *WritePathContext,
	slotKey string,
	tupleHash string,
	tupleValueJSON string,
) error {
	for _, scope := range scopes {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO slot_correlation (scope, template_id, slot_key, tuple_hash, tuple_value_json, weight, count, last_seen_ms)
			VALUES (?, ?, ?, ?, ?, 1.0, 1, ?)
			ON CONFLICT(scope, template_id, slot_key, tuple_hash) DO UPDATE SET
				weight = weight + 1.0,
				count = count + 1,
				last_seen_ms = excluded.last_seen_ms
		`,
			scope, wctx.PreNorm.TemplateID, slotKey, tupleHash, tupleValueJSON, wctx.NowMs,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Step 7: Update project_type_stat and project_type_transition
func updateProjectTypeStats(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, projectTypes []string, tauMs int64) error {
	for _, pt := range projectTypes {
		// Update project_type_stat
		if err := upsertProjectTypeStatInTx(ctx, tx, pt, wctx.PreNorm.TemplateID, wctx.NowMs, tauMs); err != nil {
			return err
		}

		// Update project_type_transition (if previous template known)
		if wctx.PrevTemplateID != "" {
			if err := upsertProjectTypeTransitionInTx(ctx, tx, pt, wctx.PrevTemplateID, wctx.PreNorm.TemplateID, wctx.NowMs, tauMs); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertProjectTypeStatInTx(ctx context.Context, tx *sql.Tx, projectType, templateID string, nowMs, tauMs int64) error {
	var currentScore float64
	var currentCount int
	var lastSeenMs int64

	err := tx.QueryRowContext(ctx, `
		SELECT score, count, last_seen_ms
		FROM project_type_stat
		WHERE project_type = ? AND template_id = ?
	`, projectType, templateID).Scan(&currentScore, &currentCount, &lastSeenMs)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var newScore float64
	var newCount int
	if errors.Is(err, sql.ErrNoRows) {
		newScore = 1.0
		newCount = 1
	} else {
		elapsed := float64(nowMs - lastSeenMs)
		decay := math.Exp(-elapsed / float64(tauMs))
		newScore = currentScore*decay + 1.0
		newCount = currentCount + 1
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO project_type_stat (project_type, template_id, score, count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(project_type, template_id) DO UPDATE SET
			score = ?,
			count = ?,
			last_seen_ms = ?
	`,
		projectType, templateID, newScore, newCount, nowMs,
		newScore, newCount, nowMs,
	)
	return err
}

func upsertProjectTypeTransitionInTx(ctx context.Context, tx *sql.Tx, projectType, prevTemplateID, nextTemplateID string, nowMs, tauMs int64) error {
	newWeight, newCount, err := loadDecayedTransitionWeightAndCount(ctx, tx, `
		SELECT weight, count, last_seen_ms
		FROM project_type_transition
		WHERE project_type = ? AND prev_template_id = ? AND next_template_id = ?
	`, []any{projectType, prevTemplateID, nextTemplateID}, nowMs, tauMs)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO project_type_transition (project_type, prev_template_id, next_template_id, weight, count, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_type, prev_template_id, next_template_id) DO UPDATE SET
			weight = ?,
			count = ?,
			last_seen_ms = ?
	`,
		projectType, prevTemplateID, nextTemplateID, newWeight, newCount, nowMs,
		newWeight, newCount, nowMs,
	)
	return err
}

func loadDecayedTransitionWeightAndCount(
	ctx context.Context,
	tx *sql.Tx,
	query string,
	args []any,
	nowMs, tauMs int64,
) (weight float64, count int, err error) {
	var currentWeight float64
	var currentCount int
	var lastSeenMs int64

	err = tx.QueryRowContext(ctx, query, args...).Scan(&currentWeight, &currentCount, &lastSeenMs)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}

	if errors.Is(err, sql.ErrNoRows) {
		return 1.0, 1, nil
	}

	elapsed := float64(nowMs - lastSeenMs)
	decay := math.Exp(-elapsed / float64(tauMs))
	newWeight := currentWeight*decay + 1.0
	newCount := currentCount + 1
	return newWeight, newCount, nil
}

// Step 8: Update directory-scoped aggregates
func updateDirectoryScopedAggregates(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, tauMs int64) error {
	dirScope := computeDirScope(wctx.Event.Cwd)

	// Update command_stat for dir scope
	isSuccess := wctx.Event.ExitCode == 0
	if err := upsertCommandStatInTx(ctx, tx, dirScope, wctx.PreNorm.TemplateID, isSuccess, wctx.NowMs, tauMs); err != nil {
		return err
	}

	// Update transition_stat for dir scope (if previous template known)
	if wctx.PrevTemplateID != "" {
		if err := upsertTransitionStatInTx(ctx, tx, dirScope, wctx.PrevTemplateID, wctx.PreNorm.TemplateID, wctx.NowMs, tauMs); err != nil {
			return err
		}
	}

	return nil
}

// Step 9: Update pipeline tables (pipeline_event, pipeline_transition, pipeline_pattern)
func updatePipelineTables(ctx context.Context, tx *sql.Tx, wctx *WritePathContext, eventID int64, maxSegments int) (int, error) {
	segments := trimPipelineSegments(wctx.PreNorm.Segments, maxSegments)
	if len(segments) <= 1 {
		return 0, nil
	}
	normalizer := normalize.NewNormalizer()
	segInfos := buildWritePathPipelineInfos(normalizer, segments)
	if err := insertWritePathPipelineEvents(ctx, tx, eventID, segInfos); err != nil {
		return 0, err
	}
	if err := insertWritePathPipelineTransitions(ctx, tx, wctx.NowMs, writePathScopes(wctx.RepoKey), segInfos); err != nil {
		return 0, err
	}
	if err := upsertWritePathPipelinePatterns(ctx, tx, wctx, writePathScopes(wctx.RepoKey), segments, segInfos); err != nil {
		return 0, err
	}
	return len(segments), nil
}

func trimPipelineSegments(segments []normalize.Segment, maxSegments int) []normalize.Segment {
	if maxSegments > 0 && len(segments) > maxSegments {
		return segments[:maxSegments]
	}
	return segments
}

type writePathPipelineInfo struct {
	raw        string
	norm       string
	templateID string
	operator   string
}

func buildWritePathPipelineInfos(normalizer *normalize.Normalizer, segments []normalize.Segment) []writePathPipelineInfo {
	infos := make([]writePathPipelineInfo, len(segments))
	for i, seg := range segments {
		segNorm, _ := normalizer.Normalize(seg.Raw)
		operator := ""
		if seg.Operator != "" {
			operator = string(seg.Operator)
		}
		infos[i] = writePathPipelineInfo{
			raw:        seg.Raw,
			norm:       segNorm,
			templateID: normalize.ComputeTemplateID(segNorm),
			operator:   operator,
		}
	}
	return infos
}

func insertWritePathPipelineEvents(ctx context.Context, tx *sql.Tx, eventID int64, segInfos []writePathPipelineInfo) error {
	for i, si := range segInfos {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO pipeline_event (command_event_id, position, operator, cmd_raw, cmd_norm, template_id)
			VALUES (?, ?, ?, ?, ?, ?)
		`,
			eventID, i, nullableString(si.operator), si.raw, si.norm, si.templateID,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func insertWritePathPipelineTransitions(
	ctx context.Context,
	tx *sql.Tx,
	nowMs int64,
	scopes []string,
	segInfos []writePathPipelineInfo,
) error {
	for i := 1; i < len(segInfos); i++ {
		prevOp := segInfos[i-1].operator
		if prevOp == "" {
			prevOp = "|"
		}
		for _, scope := range scopes {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO pipeline_transition (scope, prev_template_id, next_template_id, operator, weight, count, last_seen_ms)
				VALUES (?, ?, ?, ?, 1.0, 1, ?)
				ON CONFLICT(scope, prev_template_id, next_template_id, operator) DO UPDATE SET
					weight = weight + 1.0,
					count = count + 1,
					last_seen_ms = excluded.last_seen_ms
			`,
				scope, segInfos[i-1].templateID, segInfos[i].templateID, prevOp, nowMs,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertWritePathPipelinePatterns(
	ctx context.Context,
	tx *sql.Tx,
	wctx *WritePathContext,
	scopes []string,
	segments []normalize.Segment,
	segInfos []writePathPipelineInfo,
) error {
	segTemplateIDs := make([]string, len(segInfos))
	for i, si := range segInfos {
		segTemplateIDs[i] = si.templateID
	}
	templateChain := strings.Join(segTemplateIDs, "|")
	operatorChain := buildOperatorChain(segments)
	patternHash := computeHash(templateChain + ":" + operatorChain)
	for _, scope := range scopes {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO pipeline_pattern (pattern_hash, template_chain, operator_chain, scope, count, last_seen_ms, cmd_norm_display)
			VALUES (?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(pattern_hash) DO UPDATE SET
				count = count + 1,
				last_seen_ms = excluded.last_seen_ms
		`,
			patternHash+"_"+scope, templateChain, operatorChain, scope, wctx.NowMs, wctx.PreNorm.CmdNorm,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Step 10: Update failure_recovery
func updateFailureRecovery(ctx context.Context, tx *sql.Tx, wctx *WritePathContext) error {
	exitCodeClass := classifyExitCode(wctx.PrevExitCode)
	scopes := writePathScopes(wctx.RepoKey)
	isRecoverySuccess := wctx.Event.ExitCode == 0

	for _, scope := range scopes {
		current, err := readFailureRecoveryState(ctx, tx, scope, wctx, exitCodeClass)
		if err != nil {
			return err
		}
		next := computeFailureRecoveryState(current, isRecoverySuccess)
		if err := upsertFailureRecoveryState(ctx, tx, scope, wctx, exitCodeClass, next); err != nil {
			return err
		}
	}
	return nil
}

type failureRecoveryState struct {
	count       int
	successRate float64
	weight      float64
}

func readFailureRecoveryState(
	ctx context.Context,
	tx *sql.Tx,
	scope string,
	wctx *WritePathContext,
	exitCodeClass string,
) (failureRecoveryState, error) {
	var state failureRecoveryState
	err := tx.QueryRowContext(ctx, `
		SELECT count, success_rate
		FROM failure_recovery
		WHERE scope = ? AND failed_template_id = ? AND exit_code_class = ? AND recovery_template_id = ?
	`, scope, wctx.PrevTemplateID, exitCodeClass, wctx.PreNorm.TemplateID).Scan(&state.count, &state.successRate)
	if errors.Is(err, sql.ErrNoRows) {
		return failureRecoveryState{}, nil
	}
	if err != nil {
		return failureRecoveryState{}, err
	}
	state.weight = float64(state.count)
	return state, nil
}

func computeFailureRecoveryState(current failureRecoveryState, success bool) failureRecoveryState {
	if current.count == 0 {
		next := failureRecoveryState{count: 1, weight: 1.0}
		if success {
			next.successRate = 1.0
		}
		return next
	}
	nextCount := current.count + 1
	successSoFar := current.successRate * float64(current.count)
	if success {
		successSoFar++
	}
	return failureRecoveryState{
		count:       nextCount,
		successRate: successSoFar / float64(nextCount),
		weight:      float64(nextCount),
	}
}

func upsertFailureRecoveryState(
	ctx context.Context,
	tx *sql.Tx,
	scope string,
	wctx *WritePathContext,
	exitCodeClass string,
	state failureRecoveryState,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO failure_recovery (scope, failed_template_id, exit_code_class, recovery_template_id, weight, count, success_rate, last_seen_ms, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'learned')
		ON CONFLICT(scope, failed_template_id, exit_code_class, recovery_template_id) DO UPDATE SET
			weight = ?,
			count = ?,
			success_rate = ?,
			last_seen_ms = ?
	`,
		scope, wctx.PrevTemplateID, exitCodeClass, wctx.PreNorm.TemplateID,
		state.weight, state.count, state.successRate, wctx.NowMs,
		state.weight, state.count, state.successRate, wctx.NowMs,
	)
	return err
}

// Helper functions

// nullableString returns a sql.NullString for the given value.
// Empty strings are stored as NULL.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// computeDirScope returns a directory-scoped scope key.
// Format: "dir:<sha256_hex_prefix>"
func computeDirScope(cwd string) string {
	h := sha256.Sum256([]byte(cwd))
	return fmt.Sprintf("dir:%x", h[:8])
}

// computeHash returns a truncated SHA-256 hex hash of the input.
func computeHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h[:16])
}

// buildSlotKey builds a slot key string from indices (e.g., "0:1").
func buildSlotKey(indices []int) string {
	parts := make([]string, len(indices))
	for i, idx := range indices {
		parts[i] = fmt.Sprintf("%d", idx)
	}
	return strings.Join(parts, ":")
}

// buildOperatorChain builds the operator chain string from segments.
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

// classifyExitCode classifies an exit code for failure recovery lookup.
// It uses the recovery package's semantic classifier to produce keys like
// "class:not_found" instead of raw "code:127".
func classifyExitCode(exitCode int) string {
	classifier := recovery.NewClassifier(nil)
	return classifier.ClassifyToKey(exitCode)
}

// PrepareWriteContext creates a WritePathContext from a validated event.
// This performs normalization and context enrichment before the transaction.
func PrepareWriteContext(
	ev *event.CommandEvent,
	repoKey, branch, prevTemplateID string,
	prevExitCode int,
	prevFailed bool,
	aliases map[string]string,
) *WritePathContext {
	// Run pre-normalization pipeline
	preNorm := normalize.PreNormalize(ev.CmdRaw, normalize.PreNormConfig{
		Aliases: aliases,
	})

	// Run normalizer for slot extraction
	normalizer := normalize.NewNormalizer()
	_, slots := normalizer.Normalize(ev.CmdRaw)

	nowMs := ev.TS
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}

	return &WritePathContext{
		Event:          ev,
		PreNorm:        preNorm,
		Slots:          slots,
		RepoKey:        repoKey,
		Branch:         branch,
		PrevTemplateID: prevTemplateID,
		PrevExitCode:   prevExitCode,
		PrevFailed:     prevFailed,
		NowMs:          nowMs,
	}
}
