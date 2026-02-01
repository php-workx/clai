package integration

import (
	"os/exec"
	"testing"
	"time"

	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/ipc"
)

// TestInitIsFast verifies that clai init completes quickly and doesn't block on daemon.
// This is critical for shell startup time - init should take <50ms even with stale socket.
func TestInitIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	claiPath, err := exec.LookPath("clai")
	if err != nil {
		t.Skip("clai binary not found in PATH")
	}

	// Test init timing - should complete in <50ms even in worst case
	start := time.Now()
	cmd := exec.Command(claiPath, "init", "zsh")
	output, err := cmd.Output()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("clai init zsh failed: %v", err)
	}

	// Should output shell script
	if len(output) < 100 {
		t.Error("clai init zsh output too short, expected shell script content")
	}

	// Should complete quickly (< 100ms even with cold start overhead)
	if elapsed > 100*time.Millisecond {
		t.Errorf("clai init took %v, should complete in <100ms to not block shell startup", elapsed)
	}

	t.Logf("clai init completed in %v", elapsed)
}

// TestDaemonStartsViaShim verifies that the daemon starts when clai-shim connects.
// This is the expected startup path: shell script backgrounds clai-shim calls.
func TestDaemonStartsViaShim(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon lifecycle test in short mode")
	}

	// Find required binaries
	shimPath, err := exec.LookPath("clai-shim")
	if err != nil {
		t.Skip("clai-shim binary not found in PATH")
	}

	_, err = exec.LookPath("claid")
	if err != nil {
		t.Skip("claid binary not found in PATH")
	}

	// Check initial daemon state
	wasRunning := daemon.IsRunning()
	t.Logf("Daemon was initially running: %v", wasRunning)

	// Call clai-shim (which internally calls EnsureDaemon via NewClient)
	cmd := exec.Command(shimPath, "suggest",
		"--session-id=test-session",
		"--cwd=/tmp",
		"--buffer=git",
		"--cursor=3")
	_, _ = cmd.Output() // Ignore output, just trigger connection

	// Give daemon time to start if it wasn't running
	if !wasRunning {
		time.Sleep(500 * time.Millisecond)
	}

	// Verify daemon is now running
	if !daemon.IsRunning() && !ipc.IsDaemonRunning() {
		t.Error("daemon should be running after clai-shim call")
	}
}

// TestInitOutputsShellScript verifies clai init outputs valid shell scripts.
func TestInitOutputsShellScript(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	claiPath, err := exec.LookPath("clai")
	if err != nil {
		t.Skip("clai binary not found in PATH")
	}

	tests := []struct {
		shell    string
		contains string
	}{
		{"zsh", "CLAI_SESSION_ID"},
		{"bash", "CLAI_SESSION_ID"},
		{"fish", "CLAI_SESSION_ID"},
	}

	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			cmd := exec.Command(claiPath, "init", tc.shell)
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("clai init %s failed: %v", tc.shell, err)
			}

			if len(output) == 0 {
				t.Errorf("clai init %s returned empty output", tc.shell)
			}

			// Check for expected content
			outputStr := string(output)
			if !containsSubstring(outputStr, tc.contains) {
				t.Errorf("clai init %s output missing expected content %q", tc.shell, tc.contains)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
