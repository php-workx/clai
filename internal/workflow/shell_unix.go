//go:build !windows

package workflow

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/google/shlex"
)

type unixShellAdapter struct{}

func newPlatformShellAdapter() ShellAdapter {
	return &unixShellAdapter{}
}

func (a *unixShellAdapter) BuildCommand(ctx context.Context, step *StepDef, workDir string, env []string, outputFile string) (*exec.Cmd, error) {
	if step.Run == "" {
		return nil, fmt.Errorf("step run field is empty")
	}

	env = append(env, "CLAI_OUTPUT="+outputFile)

	var cmd *exec.Cmd
	if step.Shell == "" {
		// Argv mode (default): split using POSIX shlex, no shell involved (D8).
		argv, err := shlex.Split(step.Run)
		if err != nil {
			return nil, fmt.Errorf("splitting command: %w", err)
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("command produced empty argv")
		}
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
	} else {
		// Shell mode: wrap in shell interpreter.
		shellPath := "/bin/sh"
		if step.Shell != "true" {
			shellPath = step.Shell
		}
		cmd = exec.CommandContext(ctx, shellPath, "-c", step.Run)
	}

	cmd.Dir = workDir
	cmd.Env = env
	return cmd, nil
}
