package daemon

import (
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
}

// SessionManager tracks active sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionInfo
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
	copy := *info
	return &copy, true
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
		copy := *info
		infos = append(infos, &copy)
	}
	return infos
}
