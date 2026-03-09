package workflow

import (
	"bytes"
	"testing"
)

func TestSecretMasker_SingleSecret(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "my-secret-value")

	m := NewSecretMasker([]SecretDef{
		{Name: "SECRET_TOKEN", From: "env"},
	})

	got := m.Mask("the token is my-secret-value here")
	want := "the token is *** here"
	if got != want {
		t.Errorf("Mask() = %q, want %q", got, want)
	}
}

func TestSecretMasker_MultipleSecrets_OverlappingSubstrings(t *testing.T) {
	t.Setenv("DEPLOY_TOKEN", "abc")
	t.Setenv("DEPLOY_TOKEN_LONG", "abcdef")

	m := NewSecretMasker([]SecretDef{
		{Name: "DEPLOY_TOKEN", From: "env"},
		{Name: "DEPLOY_TOKEN_LONG", From: "env"},
	})

	got := m.Mask("value is abcdef end")
	want := "value is *** end"
	if got != want {
		t.Errorf("Mask() = %q, want %q", got, want)
	}
}

func TestSecretMasker_EmptyEnvValue(t *testing.T) {
	t.Setenv("EMPTY_SECRET", "")

	m := NewSecretMasker([]SecretDef{
		{Name: "EMPTY_SECRET", From: "env"},
	})

	input := "nothing to mask here"
	got := m.Mask(input)
	if got != input {
		t.Errorf("Mask() = %q, want %q (unchanged)", got, input)
	}
}

func TestSecretMasker_NilMasker(t *testing.T) {
	var m *SecretMasker

	input := "should pass through"
	got := m.Mask(input)
	if got != input {
		t.Errorf("nil Mask() = %q, want %q", got, input)
	}

	gotBytes := m.MaskBytes([]byte(input))
	if string(gotBytes) != input {
		t.Errorf("nil MaskBytes() = %q, want %q", gotBytes, input)
	}

	if names := m.SecretNames(); names != nil {
		t.Errorf("nil SecretNames() = %v, want nil", names)
	}
}

func TestSecretMasker_NoSecrets(t *testing.T) {
	m := NewSecretMasker(nil)

	input := "should pass through"
	got := m.Mask(input)
	if got != input {
		t.Errorf("Mask() = %q, want %q", got, input)
	}
}

func TestSecretMasker_EmptySlice(t *testing.T) {
	m := NewSecretMasker([]SecretDef{})

	input := "should pass through"
	got := m.Mask(input)
	if got != input {
		t.Errorf("Mask() = %q, want %q", got, input)
	}
}

func TestSecretMasker_MultipleOccurrences(t *testing.T) {
	t.Setenv("REPEATED_SECRET", "s3cr3t")

	m := NewSecretMasker([]SecretDef{
		{Name: "REPEATED_SECRET", From: "env"},
	})

	got := m.Mask("first s3cr3t then s3cr3t again")
	want := "first *** then *** again"
	if got != want {
		t.Errorf("Mask() = %q, want %q", got, want)
	}
}

func TestSecretMasker_MaskBytes(t *testing.T) {
	t.Setenv("BYTE_SECRET", "hidden")

	m := NewSecretMasker([]SecretDef{
		{Name: "BYTE_SECRET", From: "env"},
	})

	got := m.MaskBytes([]byte("the hidden value"))
	want := []byte("the *** value")
	if !bytes.Equal(got, want) {
		t.Errorf("MaskBytes() = %q, want %q", got, want)
	}
}

func TestSecretMasker_NonEnvSourceSkipped(t *testing.T) {
	t.Setenv("FILE_SECRET", "should-not-mask")

	m := NewSecretMasker([]SecretDef{
		{Name: "FILE_SECRET", From: "file", Path: "/some/path"},
	})

	input := "should-not-mask stays"
	got := m.Mask(input)
	if got != input {
		t.Errorf("Mask() = %q, want %q (non-env source should be skipped)", got, input)
	}
}

func TestSecretMasker_SecretNames(t *testing.T) {
	t.Setenv("NAME_A", "val-a")
	t.Setenv("NAME_B", "val-b")

	m := NewSecretMasker([]SecretDef{
		{Name: "NAME_A", From: "env"},
		{Name: "NAME_B", From: "env"},
	})

	names := m.SecretNames()
	if len(names) != 2 {
		t.Fatalf("SecretNames() returned %d names, want 2", len(names))
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["NAME_A"] || !found["NAME_B"] {
		t.Errorf("SecretNames() = %v, want [NAME_A, NAME_B]", names)
	}
}

func TestSecretMasker_SortedValuesKeepNameAlignment(t *testing.T) {
	t.Setenv("SHORT_SECRET", "abc")
	t.Setenv("LONG_SECRET", "abcdefgh")

	m := NewSecretMasker([]SecretDef{
		{Name: "SHORT_SECRET", From: "env"},
		{Name: "LONG_SECRET", From: "env"},
	})

	if len(m.values) != 2 || len(m.names) != 2 {
		t.Fatalf("unexpected masker entries: values=%d names=%d", len(m.values), len(m.names))
	}

	if m.values[0] != "abcdefgh" || m.names[0] != "LONG_SECRET" {
		t.Fatalf("entry 0 misaligned: value=%q name=%q", m.values[0], m.names[0])
	}
	if m.values[1] != "abc" || m.names[1] != "SHORT_SECRET" {
		t.Fatalf("entry 1 misaligned: value=%q name=%q", m.values[1], m.names[1])
	}
}

func TestSecretMasker_UnsetEnvSkipped(t *testing.T) {
	// Use a name extremely unlikely to be set in any environment.
	m := NewSecretMasker([]SecretDef{
		{Name: "CLAI_TEST_UNSET_VAR_9f8e7d6c", From: "env"},
	})

	input := "nothing here"
	got := m.Mask(input)
	if got != input {
		t.Errorf("Mask() = %q, want %q (unset env should be skipped)", got, input)
	}
}
