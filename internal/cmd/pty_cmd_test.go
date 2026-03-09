package cmd

import (
	"strings"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestSetPTYEnabled_PersistentConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAI_HOME", home)

	if err := setPTYEnabled(false); err != nil {
		t.Fatalf("setPTYEnabled(false) error: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load config error: %v", err)
	}
	if cfg.PTY.Enabled {
		t.Fatal("expected pty.enabled to be false")
	}

	if err := setPTYEnabled(true); err != nil {
		t.Fatalf("setPTYEnabled(true) error: %v", err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load config error: %v", err)
	}
	if !cfg.PTY.Enabled {
		t.Fatal("expected pty.enabled to be true")
	}
}

func TestPtyStatusCommandOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAI_HOME", home)
	t.Setenv("CLAI_WRAP", "1")

	if err := setPTYEnabled(true); err != nil {
		t.Fatalf("setPTYEnabled(true) error: %v", err)
	}

	out := captureStdout(t, func() {
		if err := ptyStatusCmd.RunE(ptyStatusCmd, nil); err != nil {
			t.Fatalf("pty status command failed: %v", err)
		}
	})

	required := []string{
		"pty.enabled = on",
		"running inside clai-wrap",
		"applies to new shell sessions",
	}
	for _, s := range required {
		if !strings.Contains(out, s) {
			t.Errorf("status output missing %q\noutput:\n%s", s, out)
		}
	}
}
