//go:build !windows

package workflow

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

type unixProcessController struct{}

func newPlatformProcessController() ProcessController {
	return &unixProcessController{}
}

// Start configures the command with a new process group and starts it.
// Sets Setpgid to create a new process group (matching internal/ipc/spawn_unix.go).
// Note: Pdeathsig (MR-C) is Linux-only and not available on macOS at the struct level,
// so it is set conditionally via setPdeathsig in process_linux.go.
func (u *unixProcessController) Start(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	setPdeathsig(cmd.SysProcAttr)
	return cmd.Start()
}

// Interrupt sends SIGINT to the process group (negative PID targets the group).
func (u *unixProcessController) Interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return errors.New(errProcessNotStarted)
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}

// Kill sends SIGKILL to the process group.
func (u *unixProcessController) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return errors.New(errProcessNotStarted)
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// Wait waits for the process to exit. If ctx is cancelled, it sends Interrupt,
// waits up to gracePeriod for the process to exit, then sends Kill.
func (u *unixProcessController) Wait(ctx context.Context, cmd *exec.Cmd, gracePeriod time.Duration) error {
	if cmd.Process == nil {
		return errors.New(errProcessNotStarted)
	}

	// Channel to receive the result of cmd.Wait().
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Context cancelled: begin graceful shutdown.
		_ = u.Interrupt(cmd)

		select {
		case err := <-done:
			return err
		case <-time.After(gracePeriod):
			// Grace period expired, force kill.
			_ = u.Kill(cmd)
			return <-done
		}
	}
}
