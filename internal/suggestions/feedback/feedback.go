// Package feedback manages suggestion feedback storage.
package feedback

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// FeedbackAction represents the type of feedback on a suggestion.
type FeedbackAction string

const (
	ActionAccepted  FeedbackAction = "accepted"
	ActionDismissed FeedbackAction = "dismissed"
	ActionEdited    FeedbackAction = "edited"
	ActionNever     FeedbackAction = "never"
	ActionUnblock   FeedbackAction = "unblock"
	ActionIgnored   FeedbackAction = "ignored"
	ActionTimeout   FeedbackAction = "timeout"
)

// MatchMethod describes how an implicit acceptance was detected.
type MatchMethod string

const (
	MatchExplicit       MatchMethod = "explicit"
	MatchImplicitExact  MatchMethod = "implicit_exact"
	MatchImplicitPrefix MatchMethod = "implicit_prefix"
)

// FeedbackRecord represents a stored feedback entry.
type FeedbackRecord struct {
	SessionID     string
	PromptPrefix  string
	SuggestedText string
	ExecutedText  string
	Action        FeedbackAction
	MatchMethod   MatchMethod
	ID            int64
	TSMs          int64
	LatencyMs     int64
}

// RecentSuggestion tracks a suggestion shown to the user.
type RecentSuggestion struct {
	SessionID     string
	SuggestedText string
	PromptPrefix  string
	ShownAtMs     int64
}

// Config holds feedback system configuration.
type Config struct {
	MatchWindowMs  int64
	BoostAccept    float64
	PenaltyDismiss float64
}

// DefaultConfig returns the default feedback configuration.
func DefaultConfig() Config {
	return Config{MatchWindowMs: 5000, BoostAccept: 0.10, PenaltyDismiss: 0.08}
}

// Store manages suggestion feedback persistence and querying.
type Store struct {
	db                *sql.DB
	logger            *slog.Logger
	recentSuggestions []RecentSuggestion
	cfg               Config
}

// NewStore creates a new feedback store.
func NewStore(db *sql.DB, cfg Config, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{db: db, cfg: cfg, logger: logger, recentSuggestions: make([]RecentSuggestion, 0, 64)}
}

// RecordFeedback stores a feedback record.
func (s *Store) RecordFeedback(ctx context.Context, rec *FeedbackRecord) (int64, error) {
	if rec == nil {
		return 0, fmt.Errorf("feedback record is nil")
	}
	if rec.SessionID == "" {
		return 0, fmt.Errorf("session_id is required")
	}
	if rec.SuggestedText == "" {
		return 0, fmt.Errorf("suggested_text is required")
	}
	if !isValidAction(rec.Action) {
		return 0, fmt.Errorf("invalid feedback action: %q", rec.Action)
	}
	if rec.TSMs == 0 {
		rec.TSMs = time.Now().UnixMilli()
	}
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO suggestion_feedback (session_id, ts_ms, prompt_prefix, suggested_text, action, executed_text, latency_ms) VALUES (?, ?, ?, ?, ?, ?, ?)",
		rec.SessionID, rec.TSMs, nullStr(rec.PromptPrefix), rec.SuggestedText, string(rec.Action), nullStr(rec.ExecutedText), rec.LatencyMs)
	if err != nil {
		return 0, fmt.Errorf("failed to insert feedback: %w", err)
	}
	id, _ := result.LastInsertId()
	s.logger.Debug("recorded feedback", "id", id, "session_id", rec.SessionID, "action", rec.Action)
	return id, nil
}

// TrackSuggestion records that a suggestion was shown.
func (s *Store) TrackSuggestion(sessionID, suggestedText, promptPrefix string, shownAtMs int64) {
	if shownAtMs == 0 {
		shownAtMs = time.Now().UnixMilli()
	}
	s.recentSuggestions = append(s.recentSuggestions, RecentSuggestion{sessionID, suggestedText, promptPrefix, shownAtMs})
	s.pruneRecentSuggestions(shownAtMs)
}

// CheckImplicitAcceptance checks if an executed command matches a recent suggestion.
func (s *Store) CheckImplicitAcceptance(ctx context.Context, sessionID, executedCmd string, executedAtMs int64) (MatchMethod, error) {
	if executedCmd == "" || sessionID == "" {
		return "", nil
	}
	if executedAtMs == 0 {
		executedAtMs = time.Now().UnixMilli()
	}
	windowStart := executedAtMs - s.cfg.MatchWindowMs
	executedLower := strings.ToLower(strings.TrimSpace(executedCmd))
	var bestMatch *RecentSuggestion
	var bestMethod MatchMethod
	for i := range s.recentSuggestions {
		rs := &s.recentSuggestions[i]
		if rs.SessionID != sessionID || rs.ShownAtMs < windowStart {
			continue
		}
		sugLower := strings.ToLower(strings.TrimSpace(rs.SuggestedText))
		if executedLower == sugLower {
			bestMatch, bestMethod = rs, MatchImplicitExact
			break
		}
		if strings.HasPrefix(executedLower, sugLower) && bestMethod != MatchImplicitExact {
			bestMatch, bestMethod = rs, MatchImplicitPrefix
		}
	}
	if bestMatch == nil {
		return "", nil
	}
	latMs := executedAtMs - bestMatch.ShownAtMs
	_, err := s.RecordFeedback(ctx, &FeedbackRecord{
		SessionID: sessionID, TSMs: executedAtMs, PromptPrefix: bestMatch.PromptPrefix,
		SuggestedText: bestMatch.SuggestedText, Action: ActionAccepted, ExecutedText: executedCmd,
		LatencyMs: latMs, MatchMethod: bestMethod,
	})
	if err != nil {
		return bestMethod, fmt.Errorf("failed to record implicit acceptance: %w", err)
	}
	return bestMethod, nil
}

// UpdateSlotCorrelation updates the slot_correlation table.
func (s *Store) UpdateSlotCorrelation(ctx context.Context, scope, templateID, slotKey, tupleHash, tupleValueJSON string, nowMs int64) error {
	if templateID == "" || slotKey == "" || tupleHash == "" {
		return nil
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	const upsertSlotCorrelationQuery = `
		INSERT INTO slot_correlation
			(scope, template_id, slot_key, tuple_hash, tuple_value_json, weight, count, last_seen_ms)
		VALUES
			(?, ?, ?, ?, ?, 1.0, 1, ?)
		ON CONFLICT(scope, template_id, slot_key, tuple_hash) DO UPDATE SET
			weight = weight + 1.0,
			count = count + 1,
			last_seen_ms = excluded.last_seen_ms
	`
	_, err := s.db.ExecContext(ctx,
		upsertSlotCorrelationQuery,
		scope, templateID, slotKey, tupleHash, tupleValueJSON, nowMs)
	if err != nil {
		return fmt.Errorf("failed to update slot correlation: %w", err)
	}
	return nil
}

// QueryFeedback returns feedback records for a session.
func (s *Store) QueryFeedback(ctx context.Context, sessionID string, limit int) ([]FeedbackRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	const queryFeedbackBySession = `
		SELECT id, session_id, ts_ms, prompt_prefix, suggested_text, action, executed_text, latency_ms
		FROM suggestion_feedback
		WHERE session_id = ?
		ORDER BY ts_ms DESC
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx,
		queryFeedbackBySession,
		sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query feedback: %w", err)
	}
	defer rows.Close()
	var recs []FeedbackRecord
	for rows.Next() {
		var r FeedbackRecord
		var pp, et sql.NullString
		if err := rows.Scan(&r.ID, &r.SessionID, &r.TSMs, &pp, &r.SuggestedText, &r.Action, &et, &r.LatencyMs); err != nil {
			return nil, err
		}
		if pp.Valid {
			r.PromptPrefix = pp.String
		}
		if et.Valid {
			r.ExecutedText = et.String
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

// CountByAction returns feedback counts grouped by action.
func (s *Store) CountByAction(ctx context.Context, sessionID string) (map[FeedbackAction]int, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT action, COUNT(*) FROM suggestion_feedback WHERE session_id = ? GROUP BY action", sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[FeedbackAction]int)
	for rows.Next() {
		var a string
		var c int
		if err := rows.Scan(&a, &c); err != nil {
			return nil, err
		}
		m[FeedbackAction(a)] = c
	}
	return m, rows.Err()
}

func (s *Store) pruneRecentSuggestions(nowMs int64) {
	ws := nowMs - s.cfg.MatchWindowMs
	cut := 0
	for i, rs := range s.recentSuggestions {
		if rs.ShownAtMs >= ws {
			cut = i
			break
		}
		if i == len(s.recentSuggestions)-1 {
			cut = len(s.recentSuggestions)
		}
	}
	if cut > 0 {
		copy(s.recentSuggestions, s.recentSuggestions[cut:])
		s.recentSuggestions = s.recentSuggestions[:len(s.recentSuggestions)-cut]
	}
}

func isValidAction(a FeedbackAction) bool {
	switch a {
	case ActionAccepted, ActionDismissed, ActionEdited, ActionNever, ActionUnblock, ActionIgnored, ActionTimeout:
		return true
	}
	return false
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
