package expect

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Performance thresholds for shell startup overhead.
// These values represent the maximum acceptable time clai integration adds to shell startup.
const (
	// MaxIntegrationOverhead is the max time clai integration should add to shell startup.
	// This includes sourcing the shell script and running backgrounded shim calls.
	// Note: The expect test framework adds artificial delays (~200ms for RC sourcing),
	// so we measure source time directly for more accurate results.
	MaxIntegrationOverhead = 300 * time.Millisecond

	// MaxSourceTime is the max time to source the clai shell script itself.
	// This should be very fast as it's just loading shell functions.
	// All IPC calls are backgrounded so they don't add to this time.
	MaxSourceTime = 100 * time.Millisecond
)

// measureShellStartup measures the time from shell start to prompt ready.
func measureShellStartup(t *testing.T, shell string, withClai bool) time.Duration {
	t.Helper()

	var hookFile string
	if withClai {
		var filename string
		switch shell {
		case "zsh":
			filename = "clai.zsh"
		case "bash":
			filename = "clai.bash"
		case "fish":
			filename = "clai.fish"
		}
		hookFile = FindHookFile(filename)
		if hookFile == "" {
			t.Skipf("clai.%s hook file not found", shell)
		}
	}

	var opts []SessionOption
	opts = append(opts, WithTimeout(10*time.Second))
	if withClai {
		opts = append(opts, WithRCFile(hookFile))
	}

	start := time.Now()

	session, err := NewSession(shell, opts...)
	require.NoError(t, err, "failed to create %s session", shell)
	defer session.Close()

	if withClai {
		// With clai, wait for the startup message which indicates integration is loaded
		// Note: Fish's `status is-interactive` may return false in PTY test environment,
		// so we also check for the prompt in that case.
		if shell == "fish" {
			// Fish may not output startup message in PTY, wait for prompt instead
			_, err = session.ExpectRegexTimeout(`(clai \[|>)`, 5*time.Second)
		} else {
			_, err = session.ExpectTimeout("clai [", 5*time.Second)
		}
		require.NoError(t, err, "expected clai startup message or prompt")
	} else {
		// Without clai, wait for a prompt
		err = session.WaitForPrompt()
		require.NoError(t, err, "expected shell prompt")
	}

	return time.Since(start)
}

// TestPerformance_ZshIntegrationOverhead measures zsh startup overhead from clai.
func TestPerformance_ZshIntegrationOverhead(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	SkipIfShellMissing(t, "zsh")

	// Measure baseline (shell only)
	baseline := measureShellStartup(t, "zsh", false)
	t.Logf("zsh baseline startup: %v", baseline)

	// Measure with clai integration
	withClai := measureShellStartup(t, "zsh", true)
	t.Logf("zsh with clai startup: %v", withClai)

	// Calculate overhead
	overhead := withClai - baseline
	t.Logf("zsh clai overhead: %v", overhead)

	// Assert overhead is within acceptable limits
	assert.Less(t, overhead, MaxIntegrationOverhead,
		"clai integration overhead (%v) exceeds threshold (%v)", overhead, MaxIntegrationOverhead)
}

// TestPerformance_BashIntegrationOverhead measures bash startup overhead from clai.
func TestPerformance_BashIntegrationOverhead(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	// Measure baseline (shell only)
	baseline := measureShellStartup(t, "bash", false)
	t.Logf("bash baseline startup: %v", baseline)

	// Measure with clai integration
	withClai := measureShellStartup(t, "bash", true)
	t.Logf("bash with clai startup: %v", withClai)

	// Calculate overhead
	overhead := withClai - baseline
	t.Logf("bash clai overhead: %v", overhead)

	// Assert overhead is within acceptable limits
	assert.Less(t, overhead, MaxIntegrationOverhead,
		"clai integration overhead (%v) exceeds threshold (%v)", overhead, MaxIntegrationOverhead)
}

// TestPerformance_FishIntegrationOverhead measures fish startup overhead from clai.
// Note: Fish has different PTY behavior, so we measure source time directly instead
// of comparing baseline vs with-clai, which requires reliable prompt detection.
func TestPerformance_FishIntegrationOverhead(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	// Start fish session
	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Wait for shell to be ready
	time.Sleep(200 * time.Millisecond)

	// Measure time to source the clai script
	start := time.Now()

	err = session.SendLine(fmt.Sprintf("source %s 2>/dev/null", hookFile))
	require.NoError(t, err)

	// Verify script loaded by checking for a function
	err = session.SendLine("functions -q run && echo CLAI_READY")
	require.NoError(t, err)

	_, err = session.ExpectTimeout("CLAI_READY", 5*time.Second)
	require.NoError(t, err, "expected clai functions to be defined")

	sourceTime := time.Since(start)
	t.Logf("fish clai source time: %v", sourceTime)

	// Source time should be fast (all IPC calls are backgrounded)
	assert.Less(t, sourceTime, MaxIntegrationOverhead,
		"fish clai source time (%v) exceeds threshold (%v)", sourceTime, MaxIntegrationOverhead)
}

// TestPerformance_InitCommandFast verifies clai init command completes quickly.
// This is critical because `eval "$(clai init zsh)"` runs synchronously in shell startup.
func TestPerformance_InitCommandFast(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	// Skip in containers - absolute timing is meaningless due to container overhead.
	// The relative overhead tests (IntegrationOverhead, SourceScriptFast) still run
	// and catch real performance issues since container overhead cancels out.
	if isRunningInContainer() {
		t.Skip("skipping absolute timing test in container - use relative overhead tests instead")
	}

	claiPath, err := exec.LookPath("clai")
	if err != nil {
		t.Skip("clai binary not found in PATH")
	}

	threshold := 50 * time.Millisecond
	// macOS tends to have higher process startup + filesystem overhead even for
	// tiny commands; keep this strict but non-flaky.
	if runtime.GOOS == "darwin" {
		threshold = 80 * time.Millisecond
	}

	shells := []string{"zsh", "bash", "fish"}

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			// Run multiple times to get consistent measurement
			var totalDuration time.Duration
			const iterations = 5

			for i := 0; i < iterations; i++ {
				start := time.Now()
				cmd := exec.Command(claiPath, "init", shell)
				output, err := cmd.Output()
				elapsed := time.Since(start)
				totalDuration += elapsed

				require.NoError(t, err, "clai init %s failed", shell)
				require.Greater(t, len(output), 100, "expected shell script output")
			}

			avgDuration := totalDuration / iterations
			t.Logf("clai init %s average time: %v (over %d runs)", shell, avgDuration, iterations)

			// Should complete very quickly - just reading embedded file
			assert.Less(t, avgDuration, threshold,
				"clai init %s took %v, should be <%v", shell, avgDuration, threshold)
		})
	}
}

// isRunningInContainer detects if we're running inside a Docker container.
func isRunningInContainer() bool {
	// Check for /.dockerenv file (Docker-specific)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Check cgroup for docker/lxc indicators
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "lxc") {
			return true
		}
	}
	return false
}

// TestPerformance_SourceScriptFast measures time to source the shell script directly.
// This isolates the shell parsing overhead from the clai binary execution.
func TestPerformance_SourceScriptFast(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	// Test zsh and bash which have reliable prompt detection
	shells := []struct {
		name     string
		hookFile string
	}{
		{"zsh", "clai.zsh"},
		{"bash", "clai.bash"},
	}

	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			SkipIfShellMissing(t, shell.name)

			hookFile := FindHookFile(shell.hookFile)
			if hookFile == "" {
				t.Skipf("%s hook file not found", shell.hookFile)
			}

			// Start shell without RC
			session, err := NewSession(shell.name, WithTimeout(10*time.Second))
			require.NoError(t, err)
			defer session.Close()

			// Wait for shell to be ready
			time.Sleep(200 * time.Millisecond)

			// Measure time to source the script
			start := time.Now()

			err = session.SendLine(fmt.Sprintf("source %s", hookFile))
			require.NoError(t, err)

			// Wait for the startup message
			_, err = session.ExpectTimeout("clai [", 5*time.Second)
			require.NoError(t, err, "expected clai startup message")

			sourceTime := time.Since(start)
			t.Logf("%s source time: %v", shell.name, sourceTime)

			// Source time should be fast (backgrounded operations don't count)
			threshold := MaxSourceTime
			if runtime.GOOS == "darwin" {
				// zsh can be noticeably slower to source large scripts on macOS due to
				// filesystem and process scheduling variance in PTYs.
				threshold = 150 * time.Millisecond
			}
			assert.Less(t, sourceTime, threshold,
				"%s source took %v, should be <%v", shell.name, sourceTime, threshold)
		})
	}

	// Fish uses different detection method due to PTY behavior
	t.Run("fish", func(t *testing.T) {
		SkipIfShellMissing(t, "fish")

		hookFile := FindHookFile("clai.fish")
		if hookFile == "" {
			t.Skip("clai.fish hook file not found")
		}

		session, err := NewSession("fish", WithTimeout(10*time.Second))
		require.NoError(t, err)
		defer session.Close()

		time.Sleep(200 * time.Millisecond)

		start := time.Now()

		err = session.SendLine(fmt.Sprintf("source %s 2>/dev/null", hookFile))
		require.NoError(t, err)

		// Verify script loaded by checking for a function
		err = session.SendLine("functions -q run && echo SOURCE_DONE")
		require.NoError(t, err)

		_, err = session.ExpectTimeout("SOURCE_DONE", 5*time.Second)
		require.NoError(t, err, "expected clai functions to be defined")

		sourceTime := time.Since(start)
		t.Logf("fish source time: %v", sourceTime)

		assert.Less(t, sourceTime, MaxSourceTime,
			"fish source took %v, should be <%v", sourceTime, MaxSourceTime)
	})
}

// TestPerformance_NoBlockingIPCOnStartup verifies startup doesn't block on IPC.
// The shell script should background all clai-shim calls.
func TestPerformance_NoBlockingIPCOnStartup(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	// This test verifies that even if the daemon is not running,
	// shell startup is still fast because IPC calls are backgrounded.
	// Test zsh and bash which have reliable prompt/message detection.

	shells := []struct {
		name     string
		hookFile string
	}{
		{"zsh", "clai.zsh"},
		{"bash", "clai.bash"},
	}

	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			SkipIfShellMissing(t, shell.name)

			hookFile := FindHookFile(shell.hookFile)
			if hookFile == "" {
				t.Skipf("%s hook file not found", shell.hookFile)
			}

			// Start multiple sessions in sequence to verify consistent timing
			var durations []time.Duration

			for i := 0; i < 3; i++ {
				start := time.Now()

				session, err := NewSession(shell.name,
					WithTimeout(10*time.Second),
					WithRCFile(hookFile),
				)
				if err != nil {
					t.Fatalf("iteration %d: failed to create session: %v", i, err)
				}

				_, err = session.ExpectTimeout("clai [", 5*time.Second)
				session.Close()

				if err != nil {
					t.Fatalf("iteration %d: startup message not received: %v", i, err)
				}

				duration := time.Since(start)
				durations = append(durations, duration)
				t.Logf("%s iteration %d: %v", shell.name, i, duration)
			}

			// All iterations should have similar timing (no random blocking)
			// Calculate variance - if IPC was blocking, we'd see high variance
			var total time.Duration
			for _, d := range durations {
				total += d
			}
			avg := total / time.Duration(len(durations))

			var maxDeviation time.Duration
			for _, d := range durations {
				dev := d - avg
				if dev < 0 {
					dev = -dev
				}
				if dev > maxDeviation {
					maxDeviation = dev
				}
			}

			t.Logf("%s average: %v, max deviation: %v", shell.name, avg, maxDeviation)

			// Max deviation should be small if startup is consistent
			assert.Less(t, maxDeviation, 100*time.Millisecond,
				"%s startup timing is inconsistent (deviation: %v), may indicate blocking IPC",
				shell.name, maxDeviation)
		})
	}

	// Fish uses different detection method due to PTY behavior
	t.Run("fish", func(t *testing.T) {
		SkipIfShellMissing(t, "fish")

		hookFile := FindHookFile("clai.fish")
		if hookFile == "" {
			t.Skip("clai.fish hook file not found")
		}

		var durations []time.Duration

		for i := 0; i < 3; i++ {
			session, err := NewSession("fish", WithTimeout(10*time.Second))
			if err != nil {
				t.Fatalf("iteration %d: failed to create session: %v", i, err)
			}

			time.Sleep(200 * time.Millisecond)

			start := time.Now()

			session.SendLine(fmt.Sprintf("source %s 2>/dev/null", hookFile))
			session.SendLine("functions -q run && echo ITER_DONE")

			_, err = session.ExpectTimeout("ITER_DONE", 5*time.Second)
			session.Close()

			if err != nil {
				t.Fatalf("iteration %d: clai not loaded: %v", i, err)
			}

			duration := time.Since(start)
			durations = append(durations, duration)
			t.Logf("fish iteration %d: %v", i, duration)
		}

		var total time.Duration
		for _, d := range durations {
			total += d
		}
		avg := total / time.Duration(len(durations))

		var maxDeviation time.Duration
		for _, d := range durations {
			dev := d - avg
			if dev < 0 {
				dev = -dev
			}
			if dev > maxDeviation {
				maxDeviation = dev
			}
		}

		t.Logf("fish average: %v, max deviation: %v", avg, maxDeviation)

		assert.Less(t, maxDeviation, 100*time.Millisecond,
			"fish startup timing is inconsistent (deviation: %v), may indicate blocking IPC",
			maxDeviation)
	})
}
