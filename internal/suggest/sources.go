package suggest

import (
	"context"

	"github.com/runger/clai/internal/storage"
)

// Source represents the origin of a suggestion.
type Source string

const (
	// SourceSession indicates commands from the current session.
	SourceSession Source = "session"
	// SourceCWD indicates commands run in the current directory.
	SourceCWD Source = "cwd"
	// SourceGlobal indicates commands from global history.
	SourceGlobal Source = "global"
	// SourceAI indicates AI-generated suggestions.
	SourceAI Source = "ai"
)

// SourceWeight returns the weight for a given source.
// Higher weights indicate more relevant sources.
func SourceWeight(s Source) float64 {
	switch s {
	case SourceSession:
		return 1.0
	case SourceCWD:
		return 0.7
	case SourceGlobal:
		return 0.4
	case SourceAI:
		return 0.5 // AI suggestions get moderate weight
	default:
		return 0.0
	}
}

// CommandSource wraps storage.Store to provide suggestion-oriented queries.
type CommandSource struct {
	store storage.Store
}

// NewCommandSource creates a new CommandSource.
func NewCommandSource(store storage.Store) *CommandSource {
	return &CommandSource{store: store}
}

// QueryResult holds the commands from a query along with their source.
type QueryResult struct {
	Commands []storage.Command
	Source   Source
}

// QuerySession retrieves commands from the current session.
func (s *CommandSource) QuerySession(ctx context.Context, sessionID, prefix string, limit int) (*QueryResult, error) {
	if sessionID == "" {
		return &QueryResult{Commands: nil, Source: SourceSession}, nil
	}

	cmds, err := s.store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Prefix:    prefix,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Commands: cmds,
		Source:   SourceSession,
	}, nil
}

// QueryCWD retrieves commands from the current working directory within the current session.
// This ensures session isolation while providing CWD-specific suggestions.
func (s *CommandSource) QueryCWD(ctx context.Context, sessionID, cwd, prefix string, limit int) (*QueryResult, error) {
	if cwd == "" || sessionID == "" {
		return &QueryResult{Commands: nil, Source: SourceCWD}, nil
	}

	cmds, err := s.store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		CWD:       &cwd,
		Prefix:    prefix,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Commands: cmds,
		Source:   SourceCWD,
	}, nil
}

// QueryGlobal retrieves commands from the current session across all directories.
// This provides session-wide history while maintaining session isolation.
func (s *CommandSource) QueryGlobal(ctx context.Context, sessionID, prefix string, limit int) (*QueryResult, error) {
	if sessionID == "" {
		return &QueryResult{Commands: nil, Source: SourceGlobal}, nil
	}

	cmds, err := s.store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Prefix:    prefix,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Commands: cmds,
		Source:   SourceGlobal,
	}, nil
}

// QueryAllScopes queries all three scopes and returns the combined results.
// Results from higher-priority sources are returned first.
// All scopes are filtered by session ID to ensure session isolation.
// Partial failures are tolerated: if one scope fails, results from other scopes are still returned.
func (s *CommandSource) QueryAllScopes(ctx context.Context, sessionID, cwd, prefix string, limitPerScope int) ([]*QueryResult, error) {
	results := make([]*QueryResult, 0, 3)

	// Session scope (highest priority) - current session, any directory
	sessionResult, err := s.QuerySession(ctx, sessionID, prefix, limitPerScope)
	if err == nil && len(sessionResult.Commands) > 0 {
		results = append(results, sessionResult)
	}
	// Continue on error - partial results are acceptable

	// CWD scope - current session, current directory only
	cwdResult, err := s.QueryCWD(ctx, sessionID, cwd, prefix, limitPerScope)
	if err == nil && len(cwdResult.Commands) > 0 {
		results = append(results, cwdResult)
	}
	// Continue on error - partial results are acceptable

	// Global scope (lowest priority) - current session, any directory
	// Note: With session filtering, this overlaps with Session scope but may have different ranking
	globalResult, err := s.QueryGlobal(ctx, sessionID, prefix, limitPerScope)
	if err == nil && len(globalResult.Commands) > 0 {
		results = append(results, globalResult)
	}
	// Continue on error - partial results are acceptable

	return results, nil
}
