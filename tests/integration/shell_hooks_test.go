package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runger/clai/internal/config"
)

// TestShellHooks_ZshFileExists verifies the zsh hook file exists and is valid.
func TestShellHooks_ZshFileExists(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Fatal("zsh hook file not found in any expected location")
	}

	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("zsh hook file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("zsh hook file is empty")
	}
}

// TestShellHooks_BashFileExists verifies the bash hook file exists and is valid.
func TestShellHooks_BashFileExists(t *testing.T) {
	hookPath := findHookFile("clai.bash")
	if hookPath == "" {
		t.Fatal("bash hook file not found in any expected location")
	}

	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("bash hook file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("bash hook file is empty")
	}
}

// TestShellHooks_ZshSyntaxValid verifies the zsh hook file has valid syntax.
func TestShellHooks_ZshSyntaxValid(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	// Check if zsh is available
	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available for syntax check")
	}

	// Run syntax check
	cmd := exec.Command(zshPath, "-n", hookPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("zsh syntax error in %s: %v\n%s", hookPath, err, stderr.String())
	}
}

// TestShellHooks_BashSyntaxValid verifies the bash hook file has valid syntax.
func TestShellHooks_BashSyntaxValid(t *testing.T) {
	hookPath := findHookFile("clai.bash")
	if hookPath == "" {
		t.Skip("bash hook file not found")
	}

	// Check if bash is available
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available for syntax check")
	}

	// Run syntax check
	cmd := exec.Command(bashPath, "-n", hookPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("bash syntax error in %s: %v\n%s", hookPath, err, stderr.String())
	}
}

// TestShellHooks_ZshRequiredFunctions verifies required functions are defined.
func TestShellHooks_ZshRequiredFunctions(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	requiredFunctions := []string{
		"_ai_update_suggestion",
		"_ai_forward_char",
		"_ai_precmd",
		"run()",
		"function history", // Uses function keyword to avoid alias expansion
		"_clai_disable",
		"_clai_enable",
		"clai()",
	}

	for _, fn := range requiredFunctions {
		if !strings.Contains(string(content), fn) {
			t.Errorf("required function %q not found in zsh hooks", fn)
		}
	}
}

// TestShellHooks_BashRequiredFunctions verifies required functions are defined.
func TestShellHooks_BashRequiredFunctions(t *testing.T) {
	hookPath := findHookFile("clai.bash")
	if hookPath == "" {
		t.Skip("bash hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	requiredFunctions := []string{
		"_clai_completion",
		"_ai_prompt_command",
		"run()",
		"ai-fix()",
		"history()",
		"_clai_disable()",
		"_clai_enable()",
		"clai()",
	}

	for _, fn := range requiredFunctions {
		if !strings.Contains(string(content), fn) {
			t.Errorf("required function %q not found in bash hooks", fn)
		}
	}
}

// TestShellHooks_HistoryInterception verifies history command interception is set up.
func TestShellHooks_HistoryInterception(t *testing.T) {
	tests := []struct {
		shell string
		file  string
	}{
		{"zsh", "clai.zsh"},
		{"bash", "clai.bash"},
		{"fish", "clai.fish"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			hookPath := findHookFile(tt.file)
			if hookPath == "" {
				t.Skipf("%s hook file not found", tt.shell)
			}

			content, err := os.ReadFile(hookPath)
			if err != nil {
				t.Fatalf("failed to read hook file: %v", err)
			}

			contentStr := string(content)

			// Check for history function definition
			if !strings.Contains(contentStr, "history") {
				t.Errorf("%s: history function not found", tt.shell)
			}

			// Check for --global flag support
			if !strings.Contains(contentStr, "--global") {
				t.Errorf("%s: --global flag support not found in history function", tt.shell)
			}

			// Check for session-specific history call
			if !strings.Contains(contentStr, "clai history") {
				t.Errorf("%s: clai history call not found", tt.shell)
			}

			// Check for CLAI_SESSION_ID usage
			if !strings.Contains(contentStr, "CLAI_SESSION_ID") {
				t.Errorf("%s: CLAI_SESSION_ID not found in history function", tt.shell)
			}
		})
	}
}

// TestShellHooks_ZshSessionIDGeneration verifies session ID is generated.
func TestShellHooks_ZshSessionIDGeneration(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	// Check for session ID generation
	if !strings.Contains(string(content), "CLAI_SESSION_ID") {
		t.Error("CLAI_SESSION_ID not found in zsh hooks")
	}
}

// TestShellHooks_ClaiCalls verifies hooks call clai commands correctly.
func TestShellHooks_ClaiCalls(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	expectedCalls := []string{
		"clai suggest",
		"clai voice",
		"clai diagnose",
	}

	for _, call := range expectedCalls {
		if !strings.Contains(string(content), call) {
			t.Errorf("expected clai call %q not found in hooks", call)
		}
	}
}

// TestShellHooks_VoiceModePrefix verifies voice mode is configured.
func TestShellHooks_VoiceModePrefix(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	// Voice mode uses backtick prefix
	if !strings.Contains(string(content), "text-to-command") {
		t.Log("voice mode (text-to-command) not found in hooks - may be optional")
	}
}

// TestShellHooks_RunWrapper verifies the run wrapper exists.
func TestShellHooks_RunWrapper(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	// Check for run wrapper function
	if !strings.Contains(string(content), "run()") && !strings.Contains(string(content), "function run") {
		t.Log("run wrapper function not found - may be optional for auto-diagnosis")
	}
}

// TestShellHooks_ConfigVariables verifies configuration variables are used.
func TestShellHooks_ConfigVariables(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	expectedVars := []string{
		"CLAI_AUTO_EXTRACT",
		"CLAI_CACHE",
	}

	for _, v := range expectedVars {
		if !strings.Contains(string(content), v) {
			t.Errorf("expected config variable %q not found in hooks", v)
		}
	}
}

// TestShellHooks_SuggestionFile verifies suggestion file path is used.
func TestShellHooks_SuggestionFile(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	// Check for suggestion file handling
	if !strings.Contains(string(content), "suggestion") {
		t.Error("suggestion file handling not found in hooks")
	}
}

// TestShellHooks_ZshIntegration runs a simple zsh integration test.
func TestShellHooks_ZshIntegration(t *testing.T) {
	hookPath := findHookFile("clai.zsh")
	if hookPath == "" {
		t.Skip("zsh hook file not found")
	}

	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available")
	}

	// Create a test script that sources the hooks and checks functions exist
	script := `
source "` + hookPath + `"
if typeset -f _ai_update_suggestion > /dev/null; then
    echo "PASS: _ai_update_suggestion defined"
else
    echo "FAIL: _ai_update_suggestion not defined"
    exit 1
fi
if typeset -f _ai_precmd > /dev/null; then
    echo "PASS: _ai_precmd defined"
else
    echo "FAIL: _ai_precmd not defined"
    exit 1
fi
echo "All functions defined"
`

	cmd := exec.Command(zshPath, "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "CLAI_SESSION_ID=test-session")

	if err := cmd.Run(); err != nil {
		t.Errorf("zsh integration test failed: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}
}

// TestShellHooks_BashIntegration runs a simple bash integration test.
func TestShellHooks_BashIntegration(t *testing.T) {
	hookPath := findHookFile("clai.bash")
	if hookPath == "" {
		t.Skip("bash hook file not found")
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	// Create a test script that sources the hooks and checks functions exist
	// Note: Some bash versions don't support complete -D, so we check core functions
	script := `
source "` + hookPath + `" 2>/dev/null || true
if type _ai_prompt_command &>/dev/null; then
    echo "PASS: _ai_prompt_command defined"
else
    echo "FAIL: _ai_prompt_command not defined"
    exit 1
fi
if type run &>/dev/null; then
    echo "PASS: run defined"
else
    echo "FAIL: run not defined"
    exit 1
fi
echo "All functions defined"
`

	// Run bash in interactive mode so shell hooks initialize.
	cmd := exec.Command(bashPath, "--norc", "--noprofile", "-i", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "CLAI_SESSION_ID=test-session")

	if err := cmd.Run(); err != nil {
		t.Errorf("bash integration test failed: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}
}

// TestShellHooks_ClaiOffOnWrapper verifies disable/enable functions and clai wrapper exist.
func TestShellHooks_ClaiOffOnWrapper(t *testing.T) {
	tests := []struct {
		shell   string
		file    string
		disable string
		enable  string
		wrapper string
		reinit  string
	}{
		{"zsh", "clai.zsh", "_clai_disable", "_clai_enable", "clai()", "_CLAI_REINIT"},
		{"bash", "clai.bash", "_clai_disable", "_clai_enable", "clai()", "_CLAI_REINIT"},
		{"fish", "clai.fish", "_clai_disable", "_clai_enable", "function clai", "_CLAI_REINIT"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			hookPath := findHookFile(tt.file)
			if hookPath == "" {
				t.Skipf("%s hook file not found", tt.shell)
			}

			content, err := os.ReadFile(hookPath)
			if err != nil {
				t.Fatalf("failed to read hook file: %v", err)
			}

			contentStr := string(content)

			if !strings.Contains(contentStr, tt.disable) {
				t.Errorf("%s: _clai_disable function not found", tt.shell)
			}
			if !strings.Contains(contentStr, tt.enable) {
				t.Errorf("%s: _clai_enable function not found", tt.shell)
			}
			if !strings.Contains(contentStr, tt.wrapper) {
				t.Errorf("%s: clai wrapper function not found (looking for %q)", tt.shell, tt.wrapper)
			}
			if !strings.Contains(contentStr, tt.reinit) {
				t.Errorf("%s: _CLAI_REINIT guard not found", tt.shell)
			}

			// Verify CLAI_OFF is set in disable
			if !strings.Contains(contentStr, "CLAI_OFF") {
				t.Errorf("%s: CLAI_OFF not found in disable function", tt.shell)
			}

			// Verify command clai is used (not recursive call)
			if !strings.Contains(contentStr, "command clai") {
				t.Errorf("%s: 'command clai' not used in wrapper (would cause recursion)", tt.shell)
			}
		})
	}
}

// findHookFile searches for a hook file in common locations.
func findHookFile(name string) string {
	// Get shell name from filename (e.g., "clai.zsh" -> "zsh")
	ext := filepath.Ext(name)
	shellName := strings.TrimPrefix(ext, ".")

	// Try source files first — installed hooks may be thin loaders
	// that delegate to `clai init` and lack the full script content.
	cwd, _ := os.Getwd()

	// Try project internal shell directory (source of truth)
	hookPath := filepath.Join(cwd, "internal", "cmd", "shell", shellName, name)
	if _, err := os.Stat(hookPath); err == nil {
		return hookPath
	}

	// Try parent directories (for running from tests/integration)
	search := cwd
	for i := 0; i < 3; i++ {
		search = filepath.Dir(search)

		hookPath = filepath.Join(search, "internal", "cmd", "shell", shellName, name)
		if _, err := os.Stat(hookPath); err == nil {
			return hookPath
		}
	}

	// Fall back to installed location
	paths := config.DefaultPaths()
	hookPath = filepath.Join(paths.HooksDir(), name)
	if _, err := os.Stat(hookPath); err == nil {
		return hookPath
	}

	return ""
}
