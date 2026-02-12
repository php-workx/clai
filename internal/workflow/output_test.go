package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOutputFile_SingleValid(t *testing.T) {
	path := writeTempFile(t, "FOO=bar\n")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", got["FOO"], "bar")
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestParseOutputFile_Multiple(t *testing.T) {
	path := writeTempFile(t, "A=1\nB=2\nC=3\n")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"A": "1", "B": "2", "C": "3"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d", len(got), len(want))
	}
}

func TestParseOutputFile_ValueWithEquals(t *testing.T) {
	path := writeTempFile(t, "URL=https://host?a=1\n")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["URL"] != "https://host?a=1" {
		t.Errorf("URL = %q, want %q", got["URL"], "https://host?a=1")
	}
}

func TestParseOutputFile_EmptyValue(t *testing.T) {
	path := writeTempFile(t, "EMPTY=\n")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got["EMPTY"]
	if !ok {
		t.Fatal("key EMPTY not found")
	}
	if v != "" {
		t.Errorf("EMPTY = %q, want empty string", v)
	}
}

func TestParseOutputFile_InvalidKeyStartsWithDigit(t *testing.T) {
	path := writeTempFile(t, "1BAD=val\n")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseOutputFile_MissingFile(t *testing.T) {
	got, err := ParseOutputFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseOutputFile_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseOutputFile_BlankLinesAndComments(t *testing.T) {
	content := "# header comment\n\nFOO=bar\n\n# another comment\nBAZ=qux\n\n"
	path := writeTempFile(t, content)
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", got["FOO"], "bar")
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", got["BAZ"], "qux")
	}
}

func TestParseOutputFile_MixedValidInvalid(t *testing.T) {
	content := "GOOD=yes\n1BAD=no\nnoequals\nALSO_GOOD=yep\n"
	path := writeTempFile(t, content)
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got["GOOD"] != "yes" {
		t.Errorf("GOOD = %q, want %q", got["GOOD"], "yes")
	}
	if got["ALSO_GOOD"] != "yep" {
		t.Errorf("ALSO_GOOD = %q, want %q", got["ALSO_GOOD"], "yep")
	}
}

func TestParseOutputFile_UnderscoreKey(t *testing.T) {
	path := writeTempFile(t, "_private=secret\n")
	got, err := ParseOutputFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["_private"] != "secret" {
		t.Errorf("_private = %q, want %q", got["_private"], "secret")
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "output")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
