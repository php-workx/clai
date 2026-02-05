package discovery

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// makefileTargetRegex matches Makefile target definitions.
// Matches lines like: "target: dependencies" or "target:"
// Excludes lines starting with . (internal targets) and tabs (recipe lines).
var makefileTargetRegex = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*)\s*:`)

// phonyTargets are common .PHONY targets that should still be suggested.
var commonTargets = map[string]string{
	"all":      "Build all targets",
	"build":    "Build the project",
	"clean":    "Remove build artifacts",
	"test":     "Run tests",
	"install":  "Install the project",
	"run":      "Run the project",
	"fmt":      "Format code",
	"lint":     "Run linters",
	"check":    "Run checks",
	"dev":      "Run development server",
	"help":     "Show help",
	"dist":     "Create distribution",
	"release":  "Create release",
	"deploy":   "Deploy the project",
	"docker":   "Build Docker image",
	"coverage": "Run test coverage",
	"bench":    "Run benchmarks",
	"docs":     "Generate documentation",
	"proto":    "Generate protobuf code",
	"generate": "Run code generation",
	"vendor":   "Vendor dependencies",
	"tidy":     "Tidy dependencies",
	"update":   "Update dependencies",
}

// discoverMakefile discovers tasks from Makefile targets.
// Per spec Section 10.1, this uses "Mode A heuristic" - parsing the Makefile directly.
func (s *Service) discoverMakefile(ctx context.Context, repoRoot string, nowMs int64) error {
	// Check common Makefile names
	var makefilePath string
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if fileExists(repoRoot, name) {
			makefilePath = filepath.Join(repoRoot, name)
			break
		}
	}

	if makefilePath == "" {
		return nil // Not an error, just no Makefile
	}

	// Read Makefile
	file, err := os.Open(makefilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Parse targets
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), int(s.opts.MaxOutputBytes))

	seenTargets := make(map[string]bool)
	var tasks []Task
	var bytesRead int64

	for scanner.Scan() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		bytesRead += int64(len(line)) + 1 // +1 for newline

		// Respect output size limit
		if bytesRead > s.opts.MaxOutputBytes {
			s.opts.Logger.Warn("Makefile too large, truncating",
				"bytes_read", bytesRead,
				"limit", s.opts.MaxOutputBytes,
				"repo", repoRoot,
			)
			break
		}

		// Skip empty lines and comments
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Skip lines starting with tab (recipe lines)
		if strings.HasPrefix(line, "\t") {
			continue
		}

		// Skip internal targets (starting with .)
		if strings.HasPrefix(trimmed, ".") {
			continue
		}

		// Match target definition
		matches := makefileTargetRegex.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}

		target := matches[1]

		// Skip if already seen
		if seenTargets[target] {
			continue
		}
		seenTargets[target] = true

		// Get description from common targets, if available
		description := commonTargets[target]

		tasks = append(tasks, Task{
			RepoKey:     repoRoot,
			Kind:        KindMakefile,
			Name:        target,
			Command:     "make " + target,
			Description: description,
		})
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Sort for deterministic ordering
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Name < tasks[j].Name
	})

	// Save to database
	return s.saveTasks(ctx, repoRoot, KindMakefile, tasks, nowMs)
}
