package daemon

import (
	"maps"
	"sync"
	"time"
)

// SessionInfo contains metadata about an active session.
type SessionInfo struct {
	SessionID    string
	Shell        string
	OS           string
	Hostname     string
	Username     string
	CWD          string
	StartedAt    time.Time
	LastActivity time.Time

	// Stashed command data from CommandStarted for CommandEnded to read.
	LastCmdRaw     string // Raw command from CommandStarted
	LastCmdCWD     string // CWD from CommandStarted
	LastGitRepo    string // Git repo name from CommandStarted
	LastGitRoot    string // Git repo root from CommandStarted
	LastGitBranch  string // Git branch from CommandStarted
	LastCmdID      string // Command ID from CommandStarted
	LastTemplateID string
	ProjectTypes   []string
	Aliases        map[string]string
}

// SessionManager tracks active sessions.
type SessionManager struct {
	sessions map[string]*SessionInfo
	mu       sync.RWMutex
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionInfo),
	}
}

// Start registers a new session.
func (m *SessionManager) Start(sessionID, shell, os, hostname, username, cwd string, startedAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[sessionID] = &SessionInfo{
		SessionID:    sessionID,
		Shell:        shell,
		OS:           os,
		Hostname:     hostname,
		Username:     username,
		CWD:          cwd,
		StartedAt:    startedAt,
		LastActivity: time.Now(),
		Aliases:      make(map[string]string),
	}
}

// End removes a session.
func (m *SessionManager) End(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, sessionID)
}

// Get returns session info if the session exists.
func (m *SessionManager) Get(sessionID string) (*SessionInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[sessionID]
	if !ok {
		return nil, false
	}

	// Return a copy to avoid data races
	infoCopy := *info
	if len(info.ProjectTypes) > 0 {
		infoCopy.ProjectTypes = append([]string(nil), info.ProjectTypes...)
	}
	if info.Aliases != nil {
		infoCopy.Aliases = maps.Clone(info.Aliases)
	}
	return &infoCopy, true
}

// Touch updates the last activity time for a session.
func (m *SessionManager) Touch(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[sessionID]; ok {
		info.LastActivity = time.Now()
	}
}

// UpdateCWD updates the current working directory for a session.
func (m *SessionManager) UpdateCWD(sessionID, cwd string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[sessionID]; ok {
		info.CWD = cwd
		info.LastActivity = time.Now()
	}
}

// StashCommand stores command data from CommandStarted for later retrieval by CommandEnded.
func (m *SessionManager) StashCommand(sessionID, cmdID, cmdRaw, cwd, gitRepo, gitRoot, gitBranch string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[sessionID]; ok {
		info.LastCmdRaw = cmdRaw
		info.LastCmdCWD = cwd
		info.LastGitRepo = gitRepo
		info.LastGitRoot = gitRoot
		info.LastGitBranch = gitBranch
		info.LastCmdID = cmdID
		info.LastActivity = time.Now()
	}
}

func (m *SessionManager) SetLastTemplateID(sessionID, templateID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.sessions[sessionID]; ok {
		info.LastTemplateID = templateID
		info.LastActivity = time.Now()
	}
}

func (m *SessionManager) SetProjectTypes(sessionID string, projectTypes []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.sessions[sessionID]; ok {
		info.ProjectTypes = append([]string(nil), projectTypes...)
		info.LastActivity = time.Now()
	}
}

func (m *SessionManager) SetAliases(sessionID string, aliases map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.sessions[sessionID]; ok {
		if aliases == nil {
			info.Aliases = make(map[string]string)
		} else {
			info.Aliases = maps.Clone(aliases)
		}
		info.LastActivity = time.Now()
	}
}

// Exists checks if a session exists.
func (m *SessionManager) Exists(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.sessions[sessionID]
	return ok
}

// ActiveCount returns the number of active sessions.
func (m *SessionManager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.sessions)
}

// List returns a list of all active session IDs.
func (m *SessionManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// GetAll returns a copy of all session info.
func (m *SessionManager) GetAll() []*SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]*SessionInfo, 0, len(m.sessions))
	for _, info := range m.sessions {
		infoCopy := *info
		infos = append(infos, &infoCopy)
	}
	return infos
}
