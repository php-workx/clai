package alias

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"

	"github.com/runger/clai/internal/suggestions/normalize"
)

// Manager coordinates alias capture, storage, expansion, and rendering
// for a single session. It is safe for concurrent use.
type Manager struct {
	store     *Store
	sessionID string
	shell     string
	logger    *slog.Logger

	mu         sync.RWMutex
	aliases    AliasMap
	reverseMap []ReverseEntry
	expander   *normalize.AliasExpander
	maxDepth   int
}

// ManagerConfig holds configuration for the alias Manager.
type ManagerConfig struct {
	// SessionID is the current session identifier.
	SessionID string

	// Shell is the shell type (bash, zsh, fish).
	Shell string

	// DB is the database connection for persistence.
	DB *sql.DB

	// MaxExpansionDepth is the maximum alias expansion depth.
	// If zero, uses normalize.DefaultMaxAliasDepth.
	MaxExpansionDepth int

	// Logger is the structured logger. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// NewManager creates a new alias Manager for the given session.
func NewManager(cfg ManagerConfig) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxDepth := cfg.MaxExpansionDepth
	if maxDepth <= 0 {
		maxDepth = normalize.DefaultMaxAliasDepth
	}

	return &Manager{
		store:     NewStore(cfg.DB),
		sessionID: cfg.SessionID,
		shell:     cfg.Shell,
		logger:    logger,
		aliases:   make(AliasMap),
		maxDepth:  maxDepth,
	}
}

// CaptureAndStore captures aliases from the current shell and stores them.
// This should be called at session start and after alias-modifying commands.
func (m *Manager) CaptureAndStore(ctx context.Context) error {
	aliases, err := Capture(ctx, m.shell)
	if err != nil {
		m.logger.Warn("alias capture failed",
			"session_id", m.sessionID,
			"shell", m.shell,
			"error", err,
		)
		// Non-fatal: continue with empty alias map
		aliases = make(AliasMap)
	}

	// Store in database
	if err := m.store.SaveAliases(ctx, m.sessionID, aliases); err != nil {
		return err
	}

	m.setAliases(aliases)

	m.logger.Debug("aliases captured",
		"session_id", m.sessionID,
		"shell", m.shell,
		"count", len(aliases),
	)
	return nil
}

// LoadFromStore loads aliases from the database for this session.
// This is used when the manager is created for an existing session.
func (m *Manager) LoadFromStore(ctx context.Context) error {
	aliases, err := m.store.LoadAliases(ctx, m.sessionID)
	if err != nil {
		return err
	}

	m.setAliases(aliases)
	return nil
}

// SetAliases sets the alias map directly without capture or persistence.
// Useful for testing or when aliases are provided externally.
func (m *Manager) SetAliases(aliases AliasMap) {
	m.setAliases(aliases)
}

// setAliases updates the internal alias state (map, expander, reverse map).
func (m *Manager) setAliases(aliases AliasMap) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.aliases = aliases
	m.expander = &normalize.AliasExpander{
		Aliases:  aliases,
		MaxDepth: m.maxDepth,
	}
	m.reverseMap = BuildReverseMap(aliases)
}

// Expand expands aliases in the first token of a command.
// Returns the expanded command and whether expansion occurred.
func (m *Manager) Expand(cmd string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.expander == nil {
		return cmd, false
	}
	return m.expander.Expand(cmd)
}

// Render rewrites a command to use aliases where possible.
// Returns the alias-rendered command.
func (m *Manager) Render(cmd string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return RenderWithAliases(cmd, m.reverseMap)
}

// Aliases returns a copy of the current alias map.
func (m *Manager) Aliases() AliasMap {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(AliasMap, len(m.aliases))
	for k, v := range m.aliases {
		result[k] = v
	}
	return result
}

// HandleCommand checks if a command should trigger alias re-capture.
// If the command is an alias/unalias/abbr command, it re-captures aliases.
func (m *Manager) HandleCommand(ctx context.Context, cmd string) {
	if ShouldResnapshot(cmd) {
		if err := m.CaptureAndStore(ctx); err != nil {
			m.logger.Warn("alias re-snapshot failed",
				"session_id", m.sessionID,
				"command", cmd,
				"error", err,
			)
		}
	}
}
