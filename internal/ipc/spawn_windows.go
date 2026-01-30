//go:build windows

package ipc

import (
	"os/exec"
	"syscall"
)

// setProcAttr sets process attributes for Windows systems.
// On Windows, we use CREATE_NEW_PROCESS_GROUP to detach from parent.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
