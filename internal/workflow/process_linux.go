//go:build linux

package workflow

import "syscall"

// setPdeathsig sets Pdeathsig on Linux to ensure children are killed if the parent dies (MR-C).
func setPdeathsig(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
