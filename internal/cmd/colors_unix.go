//go:build !windows

package cmd

import (
	"os"

	"golang.org/x/sys/unix"
)

// getTermWidthIoctl returns the terminal width via ioctl, or 0 if unavailable.
func getTermWidthIoctl() int {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 {
		return 0
	}
	return int(ws.Col)
}
