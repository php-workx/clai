package workflow

import (
	"bufio"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// validKeyRe matches output keys: starts with a letter or underscore,
// followed by letters, digits, or underscores.
var validKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseOutputFile reads and parses KEY=value pairs from the given file path.
// Returns a map of key->value. Malformed lines are skipped with a warning log.
// Missing file returns empty map (not an error -- step may not write outputs).
func ParseOutputFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip blank lines and comments.
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Split on first '='.
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			slog.Warn("output: skipping malformed line (no '=')", "path", path, "line", lineNum)
			continue
		}

		key := line[:idx]
		value := line[idx+1:]

		if !validKeyRe.MatchString(key) {
			slog.Warn("output: skipping invalid key", "path", path, "line", lineNum, "key", key)
			continue
		}

		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
