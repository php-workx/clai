package discovery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ParsedTask represents a task discovered by a parser.
type ParsedTask struct {
	Name        string
	Command     string
	Description string
}

// Parser parses runner output into tasks.
type Parser interface {
	Parse(output []byte) ([]ParsedTask, error)
}

// NewParser creates a parser based on the config.
func NewParser(cfg ParserConfig, kind string) (Parser, error) {
	switch cfg.Type {
	case ParserTypeJSONKeys:
		return &JSONKeysParser{path: cfg.Path, kind: kind}, nil
	case ParserTypeJSONArray:
		return &JSONArrayParser{path: cfg.Path, kind: kind}, nil
	case ParserTypeRegexLines:
		var regex *regexp.Regexp
		if cfg.compiledRegex != nil {
			regex = cfg.compiledRegex
		} else {
			var err error
			regex, err = regexp.Compile(cfg.Pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid regex: %w", err)
			}
		}
		return &RegexLinesParser{regex: regex, kind: kind}, nil
	case ParserTypeMakeQP:
		return &MakeQPParser{}, nil
	default:
		return nil, fmt.Errorf("unknown parser type: %s", cfg.Type)
	}
}

// JSONKeysParser extracts keys from a JSON object at a path.
// Per spec Section 10.2.2.
type JSONKeysParser struct {
	path string
	kind string
}

// Parse extracts keys from JSON object at the configured path.
func (p *JSONKeysParser) Parse(output []byte) ([]ParsedTask, error) {
	var data interface{}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Navigate to the path
	obj, err := navigateJSONPath(data, p.path)
	if err != nil {
		return nil, err
	}

	// Extract keys
	objMap, ok := obj.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("value at path %q is not an object", p.path)
	}

	tasks := make([]ParsedTask, 0, len(objMap))
	for key, val := range objMap {
		task := ParsedTask{
			Name:    key,
			Command: fmt.Sprintf("%s %s", p.kind, key),
		}
		// Try to extract description if value is a string
		if desc, ok := val.(string); ok {
			task.Description = desc
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// JSONArrayParser extracts strings from a JSON array at a path.
// Per spec Section 10.2.2.
type JSONArrayParser struct {
	path string
	kind string
}

// Parse extracts strings from JSON array at the configured path.
func (p *JSONArrayParser) Parse(output []byte) ([]ParsedTask, error) {
	var data interface{}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Navigate to the path
	obj, err := navigateJSONPath(data, p.path)
	if err != nil {
		return nil, err
	}

	// Extract array elements
	arr, ok := obj.([]interface{})
	if !ok {
		return nil, fmt.Errorf("value at path %q is not an array", p.path)
	}

	var tasks []ParsedTask
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			tasks = append(tasks, ParsedTask{
				Name:    v,
				Command: fmt.Sprintf("%s %s", p.kind, v),
			})
		case map[string]interface{}:
			// Try to extract name and optionally description
			name, _ := v["name"].(string)
			if name == "" {
				continue
			}
			task := ParsedTask{
				Name:    name,
				Command: fmt.Sprintf("%s %s", p.kind, name),
			}
			if desc, ok := v["description"].(string); ok {
				task.Description = desc
			}
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// RegexLinesParser applies a regex to each line of output.
// Per spec Section 10.2.2.
type RegexLinesParser struct {
	regex *regexp.Regexp
	kind  string
}

// Parse applies regex to each line and extracts task names from capture group.
func (p *RegexLinesParser) Parse(output []byte) ([]ParsedTask, error) {
	var tasks []ParsedTask

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		matches := p.regex.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}

		// First capture group is the task name
		name := matches[1]
		if name == "" {
			continue
		}

		task := ParsedTask{
			Name:    name,
			Command: fmt.Sprintf("%s %s", p.kind, name),
		}

		// If there's a second capture group, use it as description
		if len(matches) >= 3 && matches[2] != "" {
			task.Description = matches[2]
		}

		tasks = append(tasks, task)
	}

	return tasks, scanner.Err()
}

// MakeQPParser parses `make -qp` output.
// Per spec Section 10.2.2.
type MakeQPParser struct{}

// Parse extracts targets from `make -qp` output.
func (p *MakeQPParser) Parse(output []byte) ([]ParsedTask, error) {
	var tasks []ParsedTask

	// Regex to match target lines (not starting with . or #)
	targetRegex := regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*)\s*:`)

	// Track which targets are internal/phony
	internalTargets := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	inDatabase := false

	for scanner.Scan() {
		line := scanner.Text()

		// Track when we enter the database section
		if strings.Contains(line, "# Files") {
			inDatabase = true
			continue
		}

		if !inDatabase {
			continue
		}

		// Skip special targets
		if strings.HasPrefix(line, ".") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Check for "Not a target:" markers
		if strings.Contains(line, "# Not a target:") {
			continue
		}

		matches := targetRegex.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}

		name := matches[1]
		if internalTargets[name] {
			continue
		}

		tasks = append(tasks, ParsedTask{
			Name:    name,
			Command: fmt.Sprintf("make %s", name),
		})
	}

	return tasks, scanner.Err()
}

// navigateJSONPath navigates to a path in JSON data.
// Path is dot-separated (e.g., "recipes" or "scripts.test").
func navigateJSONPath(data interface{}, path string) (interface{}, error) {
	if path == "" || path == "." {
		return data, nil
	}

	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			var ok bool
			current, ok = v[part]
			if !ok {
				return nil, fmt.Errorf("path %q not found", path)
			}
		default:
			return nil, fmt.Errorf("cannot navigate path %q: not an object", path)
		}
	}

	return current, nil
}
