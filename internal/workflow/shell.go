package workflow

import (
	"context"
	"os/exec"
)

// ShellAdapter builds exec.Cmd for workflow step execution.
type ShellAdapter interface {
	// BuildCommand creates an exec.Cmd for the given step.
	// workDir is the working directory.
	// env is the merged environment (step > job > workflow precedence).
	// outputFile is the path to the $CLAI_OUTPUT temp file.
	BuildCommand(ctx context.Context, step *StepDef, workDir string, env []string, outputFile string) (*exec.Cmd, error)
}

// NewShellAdapter creates a platform-appropriate ShellAdapter.
func NewShellAdapter() ShellAdapter {
	return newPlatformShellAdapter()
}
