package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadChunkLines_ReadsActualBytesOnShortRead(t *testing.T) {
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

	var offset int64 = 11
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
