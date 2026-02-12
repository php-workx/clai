package workflow

import (
	"os"
	"sort"
	"strings"
)

// SecretMasker replaces secret values with "***" in arbitrary strings.
type SecretMasker struct {
	values []string // sorted longest-first for greedy matching
	names  []string // secret names for debugging
}

// NewSecretMasker creates a masker from the workflow's SecretDef list.
// In Tier 0, only "env" source is supported — reads from os.Getenv.
func NewSecretMasker(secrets []SecretDef) *SecretMasker {
	if len(secrets) == 0 {
		return &SecretMasker{}
	}

	values := make([]string, 0, len(secrets))
	names := make([]string, 0, len(secrets))

	for _, s := range secrets {
		if s.From != "env" {
			continue
		}
		val := os.Getenv(s.Name)
		if val == "" {
			continue
		}
		values = append(values, val)
		names = append(names, s.Name)
	}

	// Sort longest-first so longer secrets mask before shorter ones
	// that may be substrings.
	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})

	return &SecretMasker{
		values: values,
		names:  names,
	}
}

// Mask replaces all known secret values in the input with "***".
func (m *SecretMasker) Mask(input string) string {
	if m == nil || len(m.values) == 0 {
		return input
	}
	for _, v := range m.values {
		input = strings.ReplaceAll(input, v, "***")
	}
	return input
}

// MaskBytes replaces all known secret values in the input bytes.
func (m *SecretMasker) MaskBytes(input []byte) []byte {
	if m == nil || len(m.values) == 0 {
		return input
	}
	for _, v := range m.values {
		input = []byte(strings.ReplaceAll(string(input), v, "***"))
	}
	return input
}

// SecretNames returns the list of secret names (not values) for debugging.
func (m *SecretMasker) SecretNames() []string {
	if m == nil {
		return nil
	}
	return m.names
}
