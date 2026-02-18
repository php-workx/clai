package expect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBash_HookMissingGracefulHandling(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	pathWithoutShim := removeBinaryDirFromPath(t, "clai-shim")
	session, err := NewSession("bash",
		WithTimeout(10*time.Second),
		WithEnv("PATH="+pathWithoutShim),
	)
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	require.NoError(t, session.SendLine("source "+hookFile))
	time.Sleep(300 * time.Millisecond)

	require.NoError(t, session.SendLine("echo HOOK_MISSING_OK"))
	output, err := session.ExpectTimeout("HOOK_MISSING_OK", 3*time.Second)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(output), "command not found")
}

func TestBash_WorksWithoutDaemon(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	shimDir := createFailingShim(t)
	session, err := NewSession("bash",
		WithTimeout(10*time.Second),
		WithEnv("PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH")),
	)
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	require.NoError(t, session.SendLine("source "+hookFile))
	time.Sleep(300 * time.Millisecond)

	require.NoError(t, session.SendLine("echo NO_DAEMON_OK"))
	output, err := session.ExpectTimeout("NO_DAEMON_OK", 3*time.Second)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(output), "command not found")
}

func TestZsh_SessionCleanupOnExit(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "zsh")

	hookFile := FindHookFile("clai.zsh")
	if hookFile == "" {
		t.Skip("clai.zsh hook file not found")
	}

	shimDir, logPath := createLoggingShim(t)
	session, err := NewSession("zsh",
		WithTimeout(10*time.Second),
		WithEnv("PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH")),
		WithEnv("CLAI_SHIM_LOG="+logPath),
	)
	require.NoError(t, err, "failed to create zsh session")
	defer func() { _ = session.Close() }()

	require.NoError(t, session.SendLine("source "+hookFile))
	_, err = session.ExpectTimeout("clai [", 5*time.Second)
	require.NoError(t, err, "expected startup message")

	require.NoError(t, session.SendLine("exit"))
	_, _ = session.ExpectEOF()
	waitForShimLine(t, logPath, "session-end", 3*time.Second)

	lines := readShimLogLines(t, logPath)
	startLine := findShimLine(lines, "session-start")
	endLine := findShimLine(lines, "session-end")
	require.NotEmpty(t, startLine, "expected session-start call")
	require.NotEmpty(t, endLine, "expected session-end call")

	startID := shimArgValue(startLine, "--session-id=")
	endID := shimArgValue(endLine, "--session-id=")
	require.NotEmpty(t, startID)
	require.Equal(t, startID, endID, "session-end must use the active session ID")
}

func TestBash_GitContextRefreshOnCD(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	repoA := filepath.Join(t.TempDir(), "repo-a")
	repoB := filepath.Join(t.TempDir(), "repo-b")
	require.NoError(t, os.MkdirAll(filepath.Join(repoA, ".git"), 0750))
	require.NoError(t, os.MkdirAll(filepath.Join(repoB, ".git"), 0750))

	shimDir, logPath := createLoggingShim(t)
	session, err := NewSession("bash",
		WithTimeout(10*time.Second),
		WithEnv("PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH")),
		WithEnv("CLAI_SHIM_LOG="+logPath),
	)
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	require.NoError(t, session.SendLine("source "+hookFile))
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, os.WriteFile(logPath, nil, 0644))

	require.NoError(t, session.SendLine("cd "+repoA))
	require.NoError(t, session.SendLine("echo PWD_A_DONE"))
	_, err = session.ExpectTimeout("PWD_A_DONE", 3*time.Second)
	require.NoError(t, err)

	require.NoError(t, session.SendLine("cd "+repoB))
	require.NoError(t, session.SendLine("echo PWD_B_DONE"))
	_, err = session.ExpectTimeout("PWD_B_DONE", 3*time.Second)
	require.NoError(t, err)

	getContextCwds := func() []string {
		lines := readShimLogLines(t, logPath)
		contextCwds := make([]string, 0, 2)
		for _, line := range lines {
			if !strings.HasPrefix(line, "log-start\t") {
				continue
			}
			if !strings.Contains(line, "\t--command=echo PWD_") {
				continue
			}
			cwd := shimArgValue(line, "--cwd=")
			if cwd != "" {
				contextCwds = append(contextCwds, cwd)
			}
		}
		return contextCwds
	}

	var contextCwds []string
	require.Eventually(t, func() bool {
		contextCwds = getContextCwds()
		if len(contextCwds) < 2 {
			return false
		}
		foundA := false
		foundB := false
		for _, cwd := range contextCwds {
			if cwd == repoA {
				foundA = true
			}
			if cwd == repoB {
				foundB = true
			}
		}
		return foundA && foundB
	}, 3*time.Second, 50*time.Millisecond, "expected command context in each repo after cd; got %v", contextCwds)

	assert.Contains(t, contextCwds, repoA)
	assert.Contains(t, contextCwds, repoB)
}

func TestBash_NonInteractiveBashC_DoesNotRunHooks(t *testing.T) {
	t.Parallel()
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	shimDir, logPath := createLoggingShim(t)
	cmd := exec.Command("bash", "--norc", "--noprofile", "-c", //nolint:gosec // G204: test launches known binary with test-controlled args
		fmt.Sprintf("source %q; echo NON_INTERACTIVE_OK", hookFile))
	cmd.Env = append(os.Environ(),
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAI_SHIM_LOG="+logPath,
	)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "bash -c should succeed")
	assert.Contains(t, string(out), "NON_INTERACTIVE_OK")

	lines := readShimLogLines(t, logPath)
	assert.Len(t, lines, 0, "hooks must not run in non-interactive bash -c")
}

func TestBash_NonInteractiveScript_DoesNotRunHooks(t *testing.T) {
	t.Parallel()
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	shimDir, logPath := createLoggingShim(t)
	scriptPath := filepath.Join(t.TempDir(), "noninteractive.sh")
	script := fmt.Sprintf("#!/usr/bin/env bash\nsource %q\necho SCRIPT_MODE_OK\n", hookFile)
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0755))

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(),
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAI_SHIM_LOG="+logPath,
	)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "script execution should succeed")
	assert.Contains(t, string(out), "SCRIPT_MODE_OK")

	lines := readShimLogLines(t, logPath)
	assert.Len(t, lines, 0, "hooks must not run while executing scripts")
}

func createLoggingShim(t *testing.T) (dir string, logPath string) {
	t.Helper()

	dir = t.TempDir()
	logPath = filepath.Join(dir, "shim.log")
	scriptPath := filepath.Join(dir, "clai-shim")

	script := fmt.Sprintf(`#!/bin/sh
log="${CLAI_SHIM_LOG:-%s}"
cmd="$1"
shift
tab=$(printf '\t')
line="$cmd"
for arg in "$@"; do
  line="${line}${tab}${arg}"
done
printf "%%s\n" "$line" >> "$log"
exit 0
`, logPath)

	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0755))
	return dir, logPath
}

func createFailingShim(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "clai-shim")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1\n"), 0755))
	return dir
}

func readShimLogLines(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read shim log: %v", err)
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func findShimLine(lines []string, cmd string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, cmd+"\t") || line == cmd || strings.Contains(line, cmd+"\t") {
			return line
		}
	}
	return ""
}

func waitForShimLine(t *testing.T, logPath, cmd string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		lines := readShimLogLines(t, logPath)
		if findShimLine(lines, cmd) != "" {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func shimArgValue(line, prefix string) string {
	for _, part := range strings.Split(line, "\t") {
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix)
		}
	}
	return ""
}

func removeBinaryDirFromPath(t *testing.T, binary string) string {
	t.Helper()

	pathEnv := os.Getenv("PATH")
	binPath, err := exec.LookPath(binary)
	if err != nil {
		return pathEnv
	}
	binDir := filepath.Clean(filepath.Dir(binPath))

	filtered := make([]string, 0)
	for _, part := range filepath.SplitList(pathEnv) {
		if filepath.Clean(part) == binDir {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, string(os.PathListSeparator))
}
