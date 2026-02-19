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
	makefilePath := findMakefilePath(repoRoot)
	if makefilePath == "" {
		return nil // Not an error, just no Makefile
	}

	// Read Makefile
	file, err := os.Open(makefilePath) //nolint:gosec // reads user-specified path
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), int(s.opts.MaxOutputBytes))
	tasks, err := s.parseMakefileTargets(ctx, repoRoot, scanner)
	if err != nil {
		return err
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Name < tasks[j].Name
	})
	return s.saveTasks(ctx, repoRoot, KindMakefile, tasks, nowMs)
}

func findMakefilePath(repoRoot string) string {
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if fileExists(repoRoot, name) {
			return filepath.Join(repoRoot, name)
		}
	}
	return ""
}

func (s *Service) parseMakefileTargets(ctx context.Context, repoRoot string, scanner *bufio.Scanner) ([]Task, error) {
	seenTargets := make(map[string]bool)
	var tasks []Task
	var bytesRead int64

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line := scanner.Text()
		bytesRead += int64(len(line)) + 1 // +1 for newline

		if bytesRead > s.opts.MaxOutputBytes {
			s.opts.Logger.Warn("Makefile too large, truncating",
				"bytes_read", bytesRead,
				"limit", s.opts.MaxOutputBytes,
				"repo", repoRoot,
			)
			break
		}
		target, ok := parseMakefileTarget(line)
		if !ok || seenTargets[target] {
			continue
		}
		seenTargets[target] = true
		tasks = append(tasks, Task{
			RepoKey:     repoRoot,
			Kind:        KindMakefile,
			Name:        target,
			Command:     "make " + target,
			Description: commonTargets[target],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func parseMakefileTarget(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if strings.HasPrefix(line, "\t") || strings.HasPrefix(trimmed, ".") {
		return "", false
	}
	matches := makefileTargetRegex.FindStringSubmatch(line)
	if len(matches) < 2 {
		return "", false
	}
	return matches[1], true
}
