package daemon

import (
	"testing"
	"time"
)

func TestNewServer_Success(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	cfg := &ServerConfig{
		Store:       store,
		IdleTimeout: 5 * time.Minute,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("server should not be nil")
	}

	if server.store != store {
		t.Error("store should be set")
	}

	if server.ranker == nil {
		t.Error("ranker should be created automatically")
	}

	if server.registry == nil {
		t.Error("registry should be created automatically")
	}

	if server.sessionManager == nil {
		t.Error("sessionManager should be created")
	}
}

func TestNewServer_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := NewServer(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestNewServer_NilStore(t *testing.T) {
	t.Parallel()

	cfg := &ServerConfig{
		Store: nil,
	}

	_, err := NewServer(cfg)
	if err == nil {
		t.Error("expected error for nil store")
	}
}

func TestNewServer_DefaultIdleTimeout(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	cfg := &ServerConfig{
		Store: store,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Default should be 20 minutes
	if server.idleTimeout != 20*time.Minute {
		t.Errorf("expected default idle timeout of 20 minutes, got %v", server.idleTimeout)
	}
}

func TestServer_TouchActivity(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	server, err := NewServer(&ServerConfig{Store: store})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	oldActivity := server.getLastActivity()
	time.Sleep(10 * time.Millisecond)
	server.touchActivity()
	newActivity := server.getLastActivity()

	if !newActivity.After(oldActivity) {
		t.Error("lastActivity should be updated after touchActivity")
	}
}

func TestServer_IncrementCommandsLogged(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	server, err := NewServer(&ServerConfig{Store: store})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.getCommandsLogged() != 0 {
		t.Errorf("expected 0 commands logged initially, got %d", server.getCommandsLogged())
	}

	server.incrementCommandsLogged()
	server.incrementCommandsLogged()
	server.incrementCommandsLogged()

	if server.getCommandsLogged() != 3 {
		t.Errorf("expected 3 commands logged, got %d", server.getCommandsLogged())
	}
}

func TestServer_Version(t *testing.T) {
	// Version should be set (either "dev" or build-time value)
	if Version == "" {
		t.Error("Version should not be empty")
	}
}
