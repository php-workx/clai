package cache

import (
	"os"
	"path/filepath"
	"strings"
)

// Dir returns the cache directory path
func Dir() string {
	if dir := os.Getenv("CLAI_CACHE"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "clai")
}

// EnsureDir creates the cache directory if it doesn't exist
func EnsureDir() error {
	return os.MkdirAll(Dir(), 0o755)
}

// SuggestionFile returns the path to the suggestion cache file
func SuggestionFile() string {
	return filepath.Join(Dir(), "suggestion")
}

// LastOutputFile returns the path to the last output cache file
func LastOutputFile() string {
	return filepath.Join(Dir(), "last_output")
}

// OffFile returns the path to the session disable flag.
func OffFile() string {
	return filepath.Join(Dir(), "off")
}

// SessionOff reports whether session suggestions are disabled.
func SessionOff() bool {
	_, err := os.Stat(OffFile())
	return err == nil
}

// SetSessionOff toggles the session disable flag.
func SetSessionOff(off bool) error {
	if off {
		if err := EnsureDir(); err != nil {
			return err
		}
		return os.WriteFile(OffFile(), []byte("1"), 0o644)
	}
	if err := os.Remove(OffFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadSuggestion reads the current suggestion from cache
func ReadSuggestion() (string, error) {
	data, err := os.ReadFile(SuggestionFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteSuggestion writes a suggestion to cache
func WriteSuggestion(suggestion string) error {
	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(SuggestionFile(), []byte(suggestion), 0o644)
}

// ClearSuggestion clears the suggestion file
func ClearSuggestion() error {
	return WriteSuggestion("")
}

// ReadLastOutput reads the last command output from cache (up to n lines)
func ReadLastOutput(maxLines int) (string, error) {
	data, err := os.ReadFile(LastOutputFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "(no output captured)", nil
		}
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}

// WriteLastOutput writes command output to cache
func WriteLastOutput(output string) error {
	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(LastOutputFile(), []byte(output), 0o644)
}
