//go:build windows

package workflow

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

type windowsProcessController struct{}

func newPlatformProcessController() ProcessController {
	return &windowsProcessController{}
}

// Start configures the command with CREATE_NEW_PROCESS_GROUP and starts it.
// Matches the existing pattern in internal/ipc/spawn_windows.go.
func (w *windowsProcessController) Start(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd.Start()
}

// Interrupt sends CTRL_BREAK_EVENT to the process group via GenerateConsoleCtrlEvent.
func (w *windowsProcessController) Interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

// Kill forcefully terminates the process.
func (w *windowsProcessController) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	return cmd.Process.Kill()
}

// Wait waits for the process to exit. If ctx is cancelled, it sends Interrupt,
// waits up to gracePeriod for the process to exit, then sends Kill.
func (w *windowsProcessController) Wait(ctx context.Context, cmd *exec.Cmd, gracePeriod time.Duration) error {
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = w.Interrupt(cmd)

		select {
		case err := <-done:
			return err
		case <-time.After(gracePeriod):
			_ = w.Kill(cmd)
			return <-done
		}
	}
}
