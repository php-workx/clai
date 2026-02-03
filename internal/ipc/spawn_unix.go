//go:build !windows

package ipc

import (
	"os/exec"
	"syscall"
)

// setProcAttr sets process attributes for Unix systems to detach from parent process group.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
