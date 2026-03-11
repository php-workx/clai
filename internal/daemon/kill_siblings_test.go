//go:build !windows

package daemon

import (
	"os"
	"testing"
)

func TestFindDaemonPIDs_ReturnsValidPIDs(t *testing.T) {
	// findDaemonPIDs uses pgrep which may or may not find claid processes.
	// This test just verifies it doesn't panic and returns valid PIDs.
	pids := findDaemonPIDs()
	for _, pid := range pids {
		if pid <= 0 {
			t.Errorf("findDaemonPIDs returned invalid PID: %d", pid)
		}
	}
}

func TestKillSiblingDaemons_SkipsSelf(t *testing.T) {
	// Verify our own process survives killSiblingDaemons.
	// This is a smoke test — it won't find real claid siblings in test,
	// but ensures the self-exclusion logic doesn't kill us.
	killSiblingDaemons()

	// If we're still alive, the self-PID exclusion works.
	if os.Getpid() <= 0 {
		t.Fatal("unreachable")
	}
}
