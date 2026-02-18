package daemon

import (
	"testing"
	"time"
)

func TestSessionManager_StartAndGet(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("session-1", "zsh", "darwin", "host1", "user1", "/tmp", now)

	info, ok := m.Get("session-1")
	if !ok {
		t.Fatal("session not found")
	}

	if info.SessionID != "session-1" {
		t.Errorf("expected session ID 'session-1', got %s", info.SessionID)
	}
	if info.Shell != "zsh" {
		t.Errorf("expected shell 'zsh', got %s", info.Shell)
	}
	if info.OS != "darwin" {
		t.Errorf("expected OS 'darwin', got %s", info.OS)
	}
	if info.Hostname != "host1" {
		t.Errorf("expected hostname 'host1', got %s", info.Hostname)
	}
	if info.Username != "user1" {
		t.Errorf("expected username 'user1', got %s", info.Username)
	}
	if info.CWD != "/tmp" {
		t.Errorf("expected CWD '/tmp', got %s", info.CWD)
	}
}

func TestSessionManager_End(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("session-2", "bash", "linux", "", "", "/home/user", now)

	if !m.Exists("session-2") {
		t.Error("session should exist after start")
	}

	m.End("session-2")

	if m.Exists("session-2") {
		t.Error("session should not exist after end")
	}
}

func TestSessionManager_ActiveCount(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	if m.ActiveCount() != 0 {
		t.Errorf("expected 0 active sessions, got %d", m.ActiveCount())
	}

	m.Start("s1", "zsh", "darwin", "", "", "/tmp", now)
	if m.ActiveCount() != 1 {
		t.Errorf("expected 1 active session, got %d", m.ActiveCount())
	}

	m.Start("s2", "bash", "linux", "", "", "/home", now)
	if m.ActiveCount() != 2 {
		t.Errorf("expected 2 active sessions, got %d", m.ActiveCount())
	}

	m.End("s1")
	if m.ActiveCount() != 1 {
		t.Errorf("expected 1 active session after end, got %d", m.ActiveCount())
	}
}

func TestSessionManager_Touch(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	startTime := time.Now().Add(-1 * time.Hour)

	m.Start("session-3", "zsh", "darwin", "", "", "/tmp", startTime)

	info1, _ := m.Get("session-3")
	oldActivity := info1.LastActivity

	time.Sleep(10 * time.Millisecond)
	m.Touch("session-3")

	info2, _ := m.Get("session-3")
	if !info2.LastActivity.After(oldActivity) {
		t.Error("LastActivity should be updated after Touch")
	}
}

func TestSessionManager_UpdateCWD(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("session-4", "zsh", "darwin", "", "", "/tmp", now)

	m.UpdateCWD("session-4", "/home/user/project")

	info, _ := m.Get("session-4")
	if info.CWD != "/home/user/project" {
		t.Errorf("expected CWD '/home/user/project', got %s", info.CWD)
	}
}

func TestSessionManager_List(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("a", "zsh", "darwin", "", "", "/", now)
	m.Start("b", "bash", "linux", "", "", "/", now)
	m.Start("c", "fish", "freebsd", "", "", "/", now)

	list := m.List()
	if len(list) != 3 {
		t.Errorf("expected 3 sessions in list, got %d", len(list))
	}

	// Check all sessions are in the list
	found := make(map[string]bool)
	for _, id := range list {
		found[id] = true
	}
	for _, expected := range []string{"a", "b", "c"} {
		if !found[expected] {
			t.Errorf("session %s not found in list", expected)
		}
	}
}

func TestSessionManager_GetAll(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("x", "zsh", "darwin", "host-x", "user-x", "/x", now)
	m.Start("y", "bash", "linux", "host-y", "user-y", "/y", now)

	all := m.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(all))
	}
}

func TestSessionManager_GetNonexistent(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()

	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("expected ok to be false for nonexistent session")
	}
}

func TestSessionManager_TouchNonexistent(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()

	// Should not panic
	m.Touch("nonexistent")
}

func TestSessionManager_UpdateCWDNonexistent(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()

	// Should not panic
	m.UpdateCWD("nonexistent", "/new/path")
}

func TestSessionManager_StashCommandInfo(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("sess-stash", "zsh", "darwin", "host1", "user1", "/tmp", now)

	m.StashCommand("sess-stash", "cmd-1", "git status", "/home/user/repo", "myrepo", "/home/user/repo", "main")

	info, ok := m.Get("sess-stash")
	if !ok {
		t.Fatal("session not found")
	}

	if info.LastCmdID != "cmd-1" {
		t.Errorf("expected LastCmdID 'cmd-1', got %s", info.LastCmdID)
	}
	if info.LastCmdRaw != "git status" {
		t.Errorf("expected LastCmdRaw 'git status', got %s", info.LastCmdRaw)
	}
	if info.LastCmdCWD != "/home/user/repo" {
		t.Errorf("expected LastCmdCWD '/home/user/repo', got %s", info.LastCmdCWD)
	}
	if info.LastGitRepo != "myrepo" {
		t.Errorf("expected LastGitRepo 'myrepo', got %s", info.LastGitRepo)
	}
	if info.LastGitBranch != "main" {
		t.Errorf("expected LastGitBranch 'main', got %s", info.LastGitBranch)
	}
}

func TestSessionManager_StashOverwrites(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("sess-overwrite", "bash", "linux", "", "", "/tmp", now)

	m.StashCommand("sess-overwrite", "cmd-1", "ls -la", "/first", "repo1", "/first", "dev")
	m.StashCommand("sess-overwrite", "cmd-2", "cat file.txt", "/second", "repo2", "/second", "feature")

	info, ok := m.Get("sess-overwrite")
	if !ok {
		t.Fatal("session not found")
	}

	if info.LastCmdID != "cmd-2" {
		t.Errorf("expected LastCmdID 'cmd-2', got %s", info.LastCmdID)
	}
	if info.LastCmdRaw != "cat file.txt" {
		t.Errorf("expected LastCmdRaw 'cat file.txt', got %s", info.LastCmdRaw)
	}
	if info.LastCmdCWD != "/second" {
		t.Errorf("expected LastCmdCWD '/second', got %s", info.LastCmdCWD)
	}
	if info.LastGitRepo != "repo2" {
		t.Errorf("expected LastGitRepo 'repo2', got %s", info.LastGitRepo)
	}
	if info.LastGitBranch != "feature" {
		t.Errorf("expected LastGitBranch 'feature', got %s", info.LastGitBranch)
	}
}

func TestSessionManager_StashClearedOnEnd(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	m.Start("sess-clear", "zsh", "darwin", "", "", "/tmp", now)
	m.StashCommand("sess-clear", "cmd-1", "make build", "/project", "proj", "/project", "main")

	m.End("sess-clear")

	_, ok := m.Get("sess-clear")
	if ok {
		t.Error("expected session to not exist after End")
	}
}

func TestSessionManager_StashOnNonexistentSession(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()

	// Should not panic
	m.StashCommand("nonexistent", "cmd-1", "echo hello", "/tmp", "repo", "/tmp", "main")

	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("expected session to not exist for nonexistent session")
	}
}

func TestSessionManager_Concurrent(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	now := time.Now()

	// Start multiple goroutines accessing the session manager
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func(id int) {
			sessionID := "concurrent-" + string(rune('0'+id))
			m.Start(sessionID, "zsh", "darwin", "", "", "/tmp", now)
			m.Touch(sessionID)
			m.Get(sessionID)
			m.ActiveCount()
			m.List()
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		go func() {
			m.ActiveCount()
			m.List()
			m.GetAll()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
