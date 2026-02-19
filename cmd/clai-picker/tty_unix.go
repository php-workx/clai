//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// checkTTY verifies that /dev/tty is openable.
func checkTTY() error {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return fmt.Errorf("no TTY available: %w", err)
	}
	f.Close()
	return nil
}

// checkTERM verifies that the TERM environment variable is not "dumb".
func checkTERM() error {
	if os.Getenv("TERM") == "dumb" {
		return fmt.Errorf("TERM=dumb is not supported")
	}
	return nil
}

// checkTermWidth verifies that the terminal is at least 20 columns wide.
func checkTermWidth() error {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return fmt.Errorf("cannot check terminal width: %w", err)
	}
	defer f.Close()

	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)), //nolint:gosec // G103: unsafe.Pointer required for ioctl syscall
	)
	if errno != 0 {
		return fmt.Errorf("cannot get terminal size: %w", errno)
	}

	if ws.Col < 20 {
		return fmt.Errorf("terminal too narrow (%d columns, need at least 20)", ws.Col)
	}

	return nil
}

// acquireLock acquires an advisory file lock using flock.
// Returns the file descriptor (kept open for the duration of the process).
func acquireLock(path string) (int, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR, 0o600)
	if err != nil {
		return -1, fmt.Errorf("cannot open lock file: %w", err)
	}

	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("another instance of clai-picker is running")
	}

	return fd, nil
}

// releaseLock releases the advisory file lock.
func releaseLock(fd int) {
	if fd >= 0 {
		syscall.Flock(fd, syscall.LOCK_UN)
		syscall.Close(fd)
	}
}
