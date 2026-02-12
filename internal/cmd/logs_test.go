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

func TestReadChunkLines_NoNulBytesInOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644)
	if err != nil {
		t.Fatalf("write test file: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open test file: %v", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("stat test file: %v", err)
	}
	offset := stat.Size()
	lines, _, readErr := readChunkLines(f, &offset, 4096, "")
	if readErr != nil {
		t.Fatalf("readChunkLines returned error: %v", readErr)
	}

	for _, line := range lines {
		for _, ch := range []byte(line) {
			if ch == 0 {
				t.Fatalf("unexpected NUL byte in line %q", line)
			}
		}
	}
}

func TestCollectTailLines_PropagatesReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	err := os.WriteFile(path, []byte("hello\n"), 0o644)
	if err != nil {
		t.Fatalf("write test file: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open test file: %v", err)
	}
	_ = f.Close() // force read error

	_, gotErr := collectTailLines(f, 6, 1)
	if gotErr == nil {
		t.Fatal("expected error from closed file")
	}
}
