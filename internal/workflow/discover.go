package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/runger/clai/internal/config"
)

// WorkflowSearchDirs returns the directories to search for workflow files,
// in priority order: project-local (.clai/workflows/) then user-global.
func WorkflowSearchDirs() []string {
	return []string{
		filepath.Join(".clai", "workflows"),
		filepath.Join(config.DefaultPaths().BaseDir, "workflows"),
	}
}

// DiscoverWorkflow resolves a workflow name to a file path.
// It searches for <name>.yaml and <name>.yml in the standard directories.
// If the name already ends in .yaml or .yml, it is searched as-is.
// Returns the resolved path or an error if not found.
func DiscoverWorkflow(name string) (string, error) {
	// If name is already a file path that exists, use it directly.
	if _, err := os.Stat(name); err == nil {
		return name, nil
	}

	// Build candidate filenames.
	var candidates []string
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		candidates = []string{name}
	} else {
		candidates = []string{name + ".yaml", name + ".yml"}
	}

	// Search each directory.
	for _, dir := range WorkflowSearchDirs() {
		for _, candidate := range candidates {
			path := filepath.Join(dir, candidate)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("workflow %q not found in search paths: %s",
		name, strings.Join(WorkflowSearchDirs(), ", "))
}
