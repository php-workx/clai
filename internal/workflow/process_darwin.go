//go:build darwin

package workflow

import "syscall"

// setPdeathsig is a no-op on macOS; Pdeathsig is not supported on Darwin.
func setPdeathsig(_ *syscall.SysProcAttr) {}
