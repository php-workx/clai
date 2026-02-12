package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")

// errSessionIDRequired is the validation message for a missing session_id.
const errSessionIDRequired = "session_id is required"

// CreateSession creates a new session record.
func (s *SQLiteStore) CreateSession(ctx context.Context, session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	if session.SessionID == "" {
		return errors.New(errSessionIDRequired)
	}
	if session.Shell == "" {
		return errors.New("shell is required")
	}
	if session.OS == "" {
		return errors.New("os is required")
	}
	if session.InitialCWD == "" {
		return errors.New("initial_cwd is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (
			session_id, started_at_unix_ms, ended_at_unix_ms,
			shell, os, hostname, username, initial_cwd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.SessionID,
		session.StartedAtUnixMs,
		session.EndedAtUnixMs,
		session.Shell,
		session.OS,
		nullableString(session.Hostname),
		nullableString(session.Username),
		session.InitialCWD,
	)
	if err != nil {
		// Check for duplicate key error
		if isDuplicateKeyError(err) {
			return fmt.Errorf("session with id %s already exists", session.SessionID)
		}
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

// EndSession updates a session's end time.
func (s *SQLiteStore) EndSession(ctx context.Context, sessionID string, endTime int64) error {
	if sessionID == "" {
		return errors.New(errSessionIDRequired)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET ended_at_unix_ms = ? WHERE session_id = ?
	`, endTime, sessionID)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// GetSession retrieves a session by ID.
func (s *SQLiteStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, errors.New(errSessionIDRequired)
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT session_id, started_at_unix_ms, ended_at_unix_ms,
		       shell, os, hostname, username, initial_cwd
		FROM sessions WHERE session_id = ?
	`, sessionID)

	var session Session
	var endedAt sql.NullInt64
	var hostname, username sql.NullString

	err := row.Scan(
		&session.SessionID,
		&session.StartedAtUnixMs,
		&endedAt,
		&session.Shell,
		&session.OS,
		&hostname,
		&username,
		&session.InitialCWD,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if endedAt.Valid {
		session.EndedAtUnixMs = &endedAt.Int64
	}
	if hostname.Valid {
		session.Hostname = hostname.String
	}
	if username.Valid {
		session.Username = username.String
	}

	return &session, nil
}

// ErrAmbiguousSession is returned when a prefix matches multiple sessions.
var ErrAmbiguousSession = errors.New("ambiguous session prefix")

// GetSessionByPrefix retrieves a session by ID prefix.
// Returns ErrSessionNotFound if no session matches.
// Returns ErrAmbiguousSession if multiple sessions match.
func (s *SQLiteStore) GetSessionByPrefix(ctx context.Context, prefix string) (*Session, error) {
	if prefix == "" {
		return nil, errors.New("prefix is required")
	}

	// Query for sessions matching the prefix
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, started_at_unix_ms, ended_at_unix_ms,
		       shell, os, hostname, username, initial_cwd
		FROM sessions WHERE session_id LIKE ? || '%'
		ORDER BY started_at_unix_ms DESC
		LIMIT 2
	`, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		var endedAt sql.NullInt64
		var hostname, username sql.NullString

		err := rows.Scan(
			&session.SessionID,
			&session.StartedAtUnixMs,
			&endedAt,
			&session.Shell,
			&session.OS,
			&hostname,
			&username,
			&session.InitialCWD,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		if endedAt.Valid {
			session.EndedAtUnixMs = &endedAt.Int64
		}
		if hostname.Valid {
			session.Hostname = hostname.String
		}
		if username.Valid {
			session.Username = username.String
		}

		sessions = append(sessions, session)
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

// nullableString converts an empty string to a nil sql.NullString.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// isDuplicateKeyError checks if the error is a duplicate key constraint violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "UNIQUE constraint failed") ||
		contains(errStr, "duplicate key") ||
		contains(errStr, "already exists")
}
