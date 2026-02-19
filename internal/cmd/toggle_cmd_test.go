package cmd

import (
	"testing"

	"github.com/runger/clai/internal/cache"
	"github.com/runger/clai/internal/config"
)

func TestToggleIntegration_SessionOnly(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("CLAI_CACHE", cacheDir)

	if err := toggleIntegration(false, true); err != nil {
		t.Fatalf("toggleIntegration disable session error: %v", err)
	}
	if !cache.SessionOff() {
		t.Fatal("expected session off flag to be set")
	}

	if err := toggleIntegration(true, true); err != nil {
		t.Fatalf("toggleIntegration enable session error: %v", err)
	}
	if cache.SessionOff() {
		t.Fatal("expected session off flag to be cleared")
	}
}

func TestToggleIntegration_PersistentConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAI_HOME", home)

	if err := toggleIntegration(false, false); err != nil {
		t.Fatalf("toggleIntegration disable error: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load config error: %v", err)
	}
	if cfg.Suggestions.Enabled {
		t.Fatal("expected suggestions.enabled to be false")
	}

	if toggleErr := toggleIntegration(true, false); toggleErr != nil {
		t.Fatalf("toggleIntegration enable error: %v", toggleErr)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load config error: %v", err)
	}
	if !cfg.Suggestions.Enabled {
		t.Fatal("expected suggestions.enabled to be true")
	}
}
