//go:build darwin

package workflow

import "syscall"

// setPdeathsig is a no-op on macOS; Pdeathsig is not supported on Darwin.
func setPdeathsig(_ *syscall.SysProcAttr) {
	// No-op: Darwin does not support Pdeathsig (PR_SET_PDEATHSIG is Linux-only).
}
