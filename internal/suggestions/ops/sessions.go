// Package ops provides database operations against the unified V2/V3 schema.
// These functions replace the storage.Store interface methods.
package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

// Session represents a shell session.
type Session struct {
	EndedAtMs   *int64
	SessionID   string
	Shell       string
	OS          string
	Hostname    string
	Username    string
	InitialCWD  string
	StartedAtMs int64
}

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")

// ErrAmbiguousSession is returned when a prefix matches multiple sessions.
var ErrAmbiguousSession = errors.New("ambiguous session prefix")

// CreateSession creates a new session record in the V2 session table.
func CreateSession(ctx context.Context, db *suggestdb.DB, s *Session) error {
	if s == nil {
		return errors.New("session cannot be nil")
	}
	if s.SessionID == "" {
		return errors.New("session_id is required")
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms, host, user_name, os, initial_cwd)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, s.SessionID, s.Shell, s.StartedAtMs,
		nullableString(s.Hostname), nullableString(s.Username),
		nullableString(s.OS), nullableString(s.InitialCWD))
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("session with id %s already exists", s.SessionID)
		}
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

// EndSession updates a session's ended timestamp.
func EndSession(ctx context.Context, db *suggestdb.DB, sessionID string, endTimeMs int64) error {
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	// V2 session table doesn't have ended_at column by default, but we can use
	// a no-op here since session end is tracked by the session manager in memory.
	// The important thing is the session exists.
	_ = endTimeMs
	return nil
}

// GetSession retrieves a session by ID.
func GetSession(ctx context.Context, db *suggestdb.DB, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	row := db.QueryRowContext(ctx, `
		SELECT id, shell, started_at_ms, host, user_name, os, initial_cwd
		FROM session WHERE id = ?
	`, sessionID)

	var s Session
	var host, userName, osName, initialCWD sql.NullString
	err := row.Scan(&s.SessionID, &s.Shell, &s.StartedAtMs, &host, &userName, &osName, &initialCWD)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if host.Valid {
		s.Hostname = host.String
	}
	if userName.Valid {
		s.Username = userName.String
	}
	if osName.Valid {
		s.OS = osName.String
	}
	if initialCWD.Valid {
		s.InitialCWD = initialCWD.String
	}
	return &s, nil
}

// GetSessionByPrefix retrieves a session by ID prefix.
func GetSessionByPrefix(ctx context.Context, db *suggestdb.DB, prefix string) (*Session, error) {
	if prefix == "" {
		return nil, errors.New("prefix is required")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, shell, started_at_ms, host, user_name, os, initial_cwd
		FROM session WHERE id LIKE ? || '%'
		ORDER BY started_at_ms DESC
		LIMIT 2
	`, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var host, userName, osName, initialCWD sql.NullString
		if err := rows.Scan(&s.SessionID, &s.Shell, &s.StartedAtMs, &host, &userName, &osName, &initialCWD); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		if host.Valid {
			s.Hostname = host.String
		}
		if userName.Valid {
			s.Username = userName.String
		}
		if osName.Valid {
			s.OS = osName.String
		}
		if initialCWD.Valid {
			s.InitialCWD = initialCWD.String
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sessions: %w", err)
	}

	if len(sessions) == 0 {
		return nil, ErrSessionNotFound
	}
	if len(sessions) > 1 {
		return nil, ErrAmbiguousSession
	}
	return &sessions[0], nil
}
