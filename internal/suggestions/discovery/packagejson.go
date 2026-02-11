package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// packageJSON represents the relevant parts of a package.json file.
type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// discoverPackageJSON discovers tasks from package.json scripts.
func (s *Service) discoverPackageJSON(ctx context.Context, repoRoot string, nowMs int64) error {
	packagePath := filepath.Join(repoRoot, "package.json")

	// Check if package.json exists
	if !fileExists(repoRoot, "package.json") {
		return nil // Not an error, just no package.json
	}

	// Read and parse package.json
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return err
	}

	// Respect output size limit
	if int64(len(data)) > s.opts.MaxOutputBytes {
		s.opts.Logger.Warn("package.json too large, skipping",
			"size", len(data),
			"limit", s.opts.MaxOutputBytes,
			"repo", repoRoot,
		)
		return nil
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return err
	}

	// Convert scripts to tasks
	tasks := make([]Task, 0, len(pkg.Scripts))
	for name, script := range pkg.Scripts {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		tasks = append(tasks, Task{
			RepoKey:     repoRoot,
			Kind:        KindPackageJSON,
			Name:        name,
			Command:     "npm run " + name,
			Description: script, // Use the script content as description
		})
	}

	// Sort for deterministic ordering
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Name < tasks[j].Name
	})

	// Save to database
	return s.saveTasks(ctx, repoRoot, KindPackageJSON, tasks, nowMs)
}
