// Package storage provides SQLite-based persistent storage for clai.
// It handles sessions, commands, and AI response caching.
package storage

import "context"

// Store defines the interface for all storage operations.
// The daemon is the single writer; clai-shim never opens the DB directly.
type Store interface {
	// Sessions
	CreateSession(ctx context.Context, s *Session) error
	EndSession(ctx context.Context, sessionID string, endTime int64) error
	GetSession(ctx context.Context, sessionID string) (*Session, error)

	// Commands
	CreateCommand(ctx context.Context, c *Command) error
	UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error
	QueryCommands(ctx context.Context, q CommandQuery) ([]Command, error)

	// AI Cache
	GetCached(ctx context.Context, key string) (*CacheEntry, error)
	SetCached(ctx context.Context, entry *CacheEntry) error
	PruneExpiredCache(ctx context.Context) (int64, error)

	// Lifecycle
	Close() error
}

// Session represents a shell session.
type Session struct {
	SessionID       string
	StartedAtUnixMs int64
	EndedAtUnixMs   *int64
	Shell           string
	OS              string
	Hostname        string
	Username        string
	InitialCWD      string
}

// Command represents a command executed in a session.
type Command struct {
	ID            int64
	CommandID     string
	SessionID     string
	TsStartUnixMs int64
	TsEndUnixMs   *int64
	DurationMs    *int64
	CWD           string
	Command       string
	CommandNorm   string
	CommandHash   string
	ExitCode      *int
	IsSuccess     *bool // nil = unknown (treated as success), false = failure, true = success
}

// CommandQuery defines parameters for querying commands.
type CommandQuery struct {
	SessionID *string
	CWD       *string
	Prefix    string
	Limit     int
}

// CacheEntry represents a cached AI response.
type CacheEntry struct {
	CacheKey        string
	ResponseJSON    string
	Provider        string
	CreatedAtUnixMs int64
	ExpiresAtUnixMs int64
	HitCount        int64
}
