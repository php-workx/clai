package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestResolveLogFile_PrimaryPreferred(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{BaseDir: tmpDir}

	if err := os.MkdirAll(paths.LogDir(), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	primary := paths.LogFile()
	if err := os.WriteFile(primary, []byte("primary"), 0o644); err != nil {
		t.Fatalf("WriteFile(primary) error = %v", err)
	}
	legacy := filepath.Join(tmpDir, "clai.log")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}

	got, ok := resolveLogFile(paths)
	if !ok {
		t.Fatal("resolveLogFile() ok = false, want true")
	}
	if got != primary {
		t.Fatalf("resolveLogFile() = %q, want %q", got, primary)
	}
}

func TestResolveLogFile_FallbackLegacy(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{BaseDir: tmpDir}
	legacy := filepath.Join(tmpDir, "clai.log")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}

	got, ok := resolveLogFile(paths)
	if !ok {
		t.Fatal("resolveLogFile() ok = false, want true")
	}
	if got != legacy {
		t.Fatalf("resolveLogFile() = %q, want %q", got, legacy)
	}
}

func TestResolveLogFile_NotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{BaseDir: tmpDir}

	got, ok := resolveLogFile(paths)
	if ok {
		t.Fatal("resolveLogFile() ok = true, want false")
	}
	if got != paths.LogFile() {
		t.Fatalf("resolveLogFile() = %q, want %q", got, paths.LogFile())
	}
}
