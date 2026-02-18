package normalize

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Template represents a normalized command template stored in the database.
type Template struct {
	TemplateID  string   `json:"template_id"`
	CmdNorm     string   `json:"cmd_norm"`
	Tags        []string `json:"tags,omitempty"`
	SlotCount   int      `json:"slot_count"`
	FirstSeenMs int64    `json:"first_seen_ms"`
	LastSeenMs  int64    `json:"last_seen_ms"`
	UseCount    int      `json:"use_count,omitempty"`
}

// TemplateStore provides persistence for command templates.
type TemplateStore struct {
	db *sql.DB
}

// NewTemplateStore creates a TemplateStore with the given database connection.
func NewTemplateStore(db *sql.DB) *TemplateStore {
	return &TemplateStore{db: db}
}

// Upsert inserts or updates a command template.
// On conflict, it updates last_seen_ms to the MAX of old and new values,
// preserving the earliest first_seen_ms.
func (s *TemplateStore) Upsert(ctx context.Context, t Template) error {
	tagsJSON := "null"
	if len(t.Tags) > 0 {
		b, err := json.Marshal(t.Tags)
		if err != nil {
			return fmt.Errorf("marshal tags: %w", err)
		}
		tagsJSON = string(b)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO command_template (template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(template_id) DO UPDATE SET
			last_seen_ms = MAX(command_template.last_seen_ms, excluded.last_seen_ms),
			tags = excluded.tags,
			slot_count = excluded.slot_count
	`, t.TemplateID, t.CmdNorm, tagsJSON, t.SlotCount, t.FirstSeenMs, t.LastSeenMs)
	if err != nil {
		return fmt.Errorf("upsert template: %w", err)
	}
	return nil
}

// Get retrieves a template by its ID.
// Returns nil if the template does not exist.
func (s *TemplateStore) Get(ctx context.Context, templateID string) (*Template, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT template_id, cmd_norm, tags, slot_count, first_seen_ms, last_seen_ms
		FROM command_template
		WHERE template_id = ?
	`, templateID)

	var t Template
	var tagsJSON sql.NullString
	err := row.Scan(&t.TemplateID, &t.CmdNorm, &tagsJSON, &t.SlotCount, &t.FirstSeenMs, &t.LastSeenMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}

	if tagsJSON.Valid && tagsJSON.String != "null" && tagsJSON.String != "" {
		if err := json.Unmarshal([]byte(tagsJSON.String), &t.Tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags: %w", err)
		}
	}

	return &t, nil
}

// ComputeTemplateID computes the sha256 hex digest of a normalized command.
func ComputeTemplateID(cmdNorm string) string {
	h := sha256.Sum256([]byte(cmdNorm))
	return fmt.Sprintf("%x", h)
}

// CountSlots counts the number of placeholder slots in a normalized command.
// It counts both the new pre-normalization placeholders (<PATH>, <UUID>, <URL>, <NUM>)
// and the legacy slot types (<path>, <num>, <sha>, <url>, <msg>, <arg>).
func CountSlots(cmdNorm string) int {
	placeholders := []string{
		// New pre-normalization placeholders
		PlaceholderPath, PlaceholderUUID, PlaceholderURL, PlaceholderNum,
		// Legacy slot types
		SlotPath, SlotNum, SlotSHA, SlotURL, SlotMsg, SlotArg,
	}

	count := 0
	for _, p := range placeholders {
		count += strings.Count(cmdNorm, p)
	}
	return count
}
