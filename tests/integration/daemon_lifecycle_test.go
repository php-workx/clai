package integration

import (
	"os/exec"
	"testing"
	"time"

	"github.com/runger/clai/internal/daemon"
)

// TestDaemonStartsOnInit verifies that the clai daemon starts when clai init is called.
// This tests the complete flow: clai init → EnsureDaemon → claid spawns
func TestDaemonStartsOnInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon lifecycle test in short mode")
	}

	// Find clai binary
	claiPath, err := exec.LookPath("clai")
	if err != nil {
		t.Skip("clai binary not found in PATH, skipping test")
	}

	// Find claid binary (required for daemon to spawn)
	_, err = exec.LookPath("claid")
	if err != nil {
		t.Skip("claid binary not found in PATH, skipping test")
	}

	// Check initial daemon state
	wasRunning := daemon.IsRunning()
	t.Logf("Daemon was initially running: %v", wasRunning)

	// Run clai init zsh (output is the shell script, we discard it)
	cmd := exec.Command(claiPath, "init", "zsh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clai init zsh failed: %v\nOutput: %s", err, output)
	}

	// Verify output contains expected shell script content
	if len(output) < 100 {
		t.Errorf("clai init zsh output too short, expected shell script content")
	}

	// Give daemon time to start (EnsureDaemon spawns it in background)
	time.Sleep(500 * time.Millisecond)

	// Verify daemon is now running
	if !daemon.IsRunning() {
		t.Error("daemon should be running after clai init, but daemon.IsRunning() returned false")
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
