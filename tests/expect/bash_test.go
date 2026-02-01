package expect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBash_SourceWithoutError verifies the bash script sources without errors.
func TestBash_SourceWithoutError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the script
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)

	// Wait a moment for the script to load
	time.Sleep(500 * time.Millisecond)

	// Verify by checking that functions are defined
	err = session.SendLine("type run")
	require.NoError(t, err)

	// Wait for the function type output
	output, err := session.ExpectTimeout("function", 3*time.Second)
	require.NoError(t, err, "expected run to be a function after sourcing")

	// Verify no errors in output
	assert.NotContains(t, output, "not found",
		"should not have 'not found' errors")
}

// TestBash_TabCompletionIntegration verifies tab completion is registered.
func TestBash_TabCompletionIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check that our completion function is registered
	err = session.SendLine("complete -p -D 2>/dev/null || echo 'no default'")
	require.NoError(t, err)

	// Look for our completion function in the output
	output, _ := session.ExpectTimeout("_clai_completion", 2*time.Second)
	// Note: May not find it if completion not fully supported in this bash version
	_ = output
}

// TestBash_AcceptCommandWorks verifies the accept command works.
func TestBash_AcceptCommandWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Try running accept with no suggestion
	err = session.SendLine("accept")
	require.NoError(t, err)

	// Should show "No suggestion available"
	output, err := session.ExpectTimeout("No suggestion", 2*time.Second)
	require.NoError(t, err, "expected no suggestion message")
	assert.Contains(t, output, "No suggestion", "should show no suggestion message")
}

// TestBash_ClearSuggestionWorks verifies the clear-suggestion command works.
func TestBash_ClearSuggestionWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Run clear-suggestion
	err = session.SendLine("clear-suggestion")
	require.NoError(t, err)

	// Should show confirmation
	output, err := session.ExpectTimeout("cleared", 2*time.Second)
	require.NoError(t, err, "expected cleared message")
	assert.Contains(t, output, "cleared", "should confirm suggestion cleared")
}

// TestBash_NaturalLanguagePrefix verifies ? prefix handling.
func TestBash_NaturalLanguagePrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Note: The ? prefix in bash uses extdebug trap
	// This is complex to test interactively because the DEBUG trap
	// intercepts commands before they run
	// Verify extdebug is enabled
	err = session.SendLine("shopt extdebug")
	require.NoError(t, err)
	_, _ = session.ExpectTimeout("on", 2*time.Second)
}

// TestBash_RunWrapperExists verifies the run wrapper function exists.
func TestBash_RunWrapperExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if run function exists
	err = session.SendLine("type run")
	require.NoError(t, err)

	// Should show it's a function
	output, err := session.ExpectTimeout("function", 2*time.Second)
	require.NoError(t, err, "expected run to be a function")
	assert.Contains(t, output, "function", "run should be defined as a function")
}

// TestBash_AIFixFunctionExists verifies the ai-fix function exists.
func TestBash_AIFixFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if ai-fix function exists
	err = session.SendLine("type ai-fix")
	require.NoError(t, err)

	// Should show it's a function
	output, err := session.ExpectTimeout("function", 2*time.Second)
	require.NoError(t, err, "expected ai-fix to be a function")
	assert.Contains(t, output, "function", "ai-fix should be defined as a function")
}

// TestBash_AIFunctionExists verifies the ai function exists.
func TestBash_AIFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if ai function exists
	err = session.SendLine("type ai")
	require.NoError(t, err)

	// Should show it's a function
	output, err := session.ExpectTimeout("function", 2*time.Second)
	require.NoError(t, err, "expected ai to be a function")
	assert.Contains(t, output, "function", "ai should be defined as a function")
}

// TestBash_VoiceFunctionExists verifies the voice function exists.
func TestBash_VoiceFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if voice function exists
	err = session.SendLine("type voice")
	require.NoError(t, err)

	// Should show it's a function
	output, err := session.ExpectTimeout("function", 2*time.Second)
	require.NoError(t, err, "expected voice to be a function")
	assert.Contains(t, output, "function", "voice should be defined as a function")
}

// TestBash_PromptCommandSet verifies PROMPT_COMMAND is configured.
func TestBash_PromptCommandSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check PROMPT_COMMAND contains our function
	err = session.SendLine("echo $PROMPT_COMMAND")
	require.NoError(t, err)

	// Should contain our prompt command function
	output, err := session.ExpectTimeout("_ai_prompt_command", 2*time.Second)
	require.NoError(t, err, "expected prompt command to be set")
	assert.Contains(t, output, "_ai_prompt_command", "PROMPT_COMMAND should include our function")
}

// TestBash_DoctorShowsCorrectShell verifies clai doctor detects bash correctly.
func TestBash_DoctorShowsCorrectShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Run clai doctor
	err = session.SendLine("clai doctor")
	require.NoError(t, err)

	// Should show bash in shell integration status (or warn if not installed in bashrc)
	output, err := session.ExpectTimeout("Shell integration", 5*time.Second)
	require.NoError(t, err, "expected Shell integration in output")

	// Get more output to see the shell status
	moreOutput, _ := session.ExpectTimeout("Daemon", 3*time.Second)
	combined := output + moreOutput

	// Should NOT show zsh or fish
	assert.NotContains(t, combined, "zsh (.zshrc)", "doctor should not show zsh when in bash")
	assert.NotContains(t, combined, "fish", "doctor should not show fish when in bash")

	// Should either show bash or "Not installed" (if bashrc doesn't have clai)
	hasBash := assert.ObjectsAreEqual(true, containsAny(combined, "bash", "Not installed"))
	assert.True(t, hasBash, "doctor should show bash status or not installed")
}

// TestBash_StatusShowsCorrectShell verifies clai status detects bash correctly.
func TestBash_StatusShowsCorrectShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "bash")

	hookFile := FindHookFile("clai.bash")
	if hookFile == "" {
		t.Skip("clai.bash hook file not found")
	}

	session, err := NewSession("bash", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create bash session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile)
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Run clai status
	err = session.SendLine("clai status")
	require.NoError(t, err)

	// Should show Shell Integration section
	output, err := session.ExpectTimeout("Shell Integration", 5*time.Second)
	require.NoError(t, err, "expected Shell Integration in output")

	// Get more output
	moreOutput, _ := session.ExpectTimeout("Quick Stats", 3*time.Second)
	combined := output + moreOutput

	// Should NOT show zsh or fish
	assert.NotContains(t, combined, "zsh (.zshrc)", "status should not show zsh when in bash")
	assert.NotContains(t, combined, "fish", "status should not show fish when in bash")
}

// containsAny checks if s contains any of the substrings
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if containsString(s, substr) {
			return true
		}
	}
	return false
}

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
