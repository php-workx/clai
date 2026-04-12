//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// killSiblingDaemons finds and terminates any other claid processes.
// This handles legacy daemons that predate the lockfile mechanism and would
// otherwise accumulate as zombies invisible to IsRunning().
func killSiblingDaemons() {
	myPID := os.Getpid()

	pids := findDaemonPIDs()
	if len(pids) == 0 {
		return
	}

	for _, pid := range pids {
		if pid == myPID || pid <= 0 {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		// Verify the process is alive before sending signal.
		if proc.Signal(syscall.Signal(0)) != nil {
			continue
		}
		_ = proc.Signal(syscall.SIGTERM)
	}

	// Give them a moment to exit gracefully.
	time.Sleep(300 * time.Millisecond)

	// Escalate any survivors to SIGKILL.
	for _, pid := range pids {
		if pid == myPID || pid <= 0 {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if proc.Signal(syscall.Signal(0)) == nil {
			_ = proc.Kill()
		}
	}
}

// findDaemonPIDs returns PIDs of running claid processes via pgrep.
// Returns nil if pgrep is unavailable or finds nothing.
func findDaemonPIDs() []int {
	out, err := exec.Command("pgrep", "-x", "claid").Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
