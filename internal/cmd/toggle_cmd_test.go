package cmd

import (
	"testing"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/config"
)

func TestToggleSuggestions_SessionOnly(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("CLAI_CACHE", cacheDir)

	if err := toggleSuggestions(false, true); err != nil {
		t.Fatalf("toggleSuggestions disable session error: %v", err)
	}
	if !cache.SessionOff() {
		t.Fatal("expected session off flag to be set")
	}

	if err := toggleSuggestions(true, true); err != nil {
		t.Fatalf("toggleSuggestions enable session error: %v", err)
	}
	if cache.SessionOff() {
		t.Fatal("expected session off flag to be cleared")
	}
}

func TestToggleSuggestions_PersistentConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAI_HOME", home)

	if err := toggleSuggestions(false, false); err != nil {
		t.Fatalf("toggleSuggestions disable error: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load config error: %v", err)
	}
	if cfg.Suggestions.Enabled {
		t.Fatal("expected suggestions.enabled to be false")
	}

	if err := toggleSuggestions(true, false); err != nil {
		t.Fatalf("toggleSuggestions enable error: %v", err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load config error: %v", err)
	}
	if !cfg.Suggestions.Enabled {
		t.Fatal("expected suggestions.enabled to be true")
	}
}
