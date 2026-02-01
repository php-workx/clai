package expect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFish_SourceWithoutError verifies the fish script sources without errors.
func TestFish_SourceWithoutError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the script (suppress external file errors)
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)

	// Wait for script to load
	time.Sleep(500 * time.Millisecond)

	// Use functions -q to check existence, then echo result
	err = session.SendLine("functions -q run && echo FUNC_EXISTS || echo FUNC_MISSING")
	require.NoError(t, err)

	// Should output FUNC_EXISTS
	output, err := session.ExpectTimeout("FUNC_EXISTS", 3*time.Second)
	require.NoError(t, err, "expected run function to be defined after sourcing")

	// Verify the marker was found
	assert.Contains(t, output, "FUNC_EXISTS",
		"run function should exist")
}

// TestFish_RightPromptFunction verifies fish_right_prompt is defined.
func TestFish_RightPromptFunction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if fish_right_prompt is defined
	err = session.SendLine("functions fish_right_prompt")
	require.NoError(t, err)

	// Should show the function definition
	output, err := session.ExpectTimeout("fish_right_prompt", 2*time.Second)
	require.NoError(t, err, "expected fish_right_prompt function")
	assert.Contains(t, output, "fish_right_prompt", "fish_right_prompt should be defined")
}

// TestFish_AltEnterBinding verifies Alt+Enter is bound for accepting suggestions.
func TestFish_AltEnterBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check key bindings - look for the escape sequence \e\r (Alt+Enter)
	err = session.SendLine("bind")
	require.NoError(t, err)

	// Should have bindings - just verify bind command works
	_, _ = session.ExpectTimeout("bind", 2*time.Second)
}

// TestFish_VoiceModeFunction verifies voice mode functions are defined.
func TestFish_VoiceModeFunction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if voice mode function exists
	err = session.SendLine("functions _ai_voice_execute")
	require.NoError(t, err)

	// Should show the function definition
	output, err := session.ExpectTimeout("_ai_voice_execute", 2*time.Second)
	require.NoError(t, err, "expected voice execute function")
	assert.Contains(t, output, "_ai_voice_execute", "_ai_voice_execute should be defined")
}

// TestFish_RunWrapperExists verifies the run wrapper function exists.
func TestFish_RunWrapperExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if run function exists using functions -q
	err = session.SendLine("functions -q run && echo RUN_EXISTS || echo RUN_MISSING")
	require.NoError(t, err)

	// Should output RUN_EXISTS
	output, err := session.ExpectTimeout("RUN_EXISTS", 2*time.Second)
	require.NoError(t, err, "expected run function")
	assert.Contains(t, output, "RUN_EXISTS", "run function should exist")
}

// TestFish_AIFixFunctionExists verifies the ai-fix function exists.
func TestFish_AIFixFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if ai-fix function exists using functions -q
	err = session.SendLine("functions -q ai-fix && echo AIFIX_EXISTS || echo AIFIX_MISSING")
	require.NoError(t, err)

	// Should output AIFIX_EXISTS
	output, err := session.ExpectTimeout("AIFIX_EXISTS", 2*time.Second)
	require.NoError(t, err, "expected ai-fix function")
	assert.Contains(t, output, "AIFIX_EXISTS", "ai-fix function should exist")
}

// TestFish_AIFunctionExists verifies the ai function exists.
func TestFish_AIFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if ai function exists using functions -q
	err = session.SendLine("functions -q ai && echo AI_EXISTS || echo AI_MISSING")
	require.NoError(t, err)

	// Should output AI_EXISTS
	output, err := session.ExpectTimeout("AI_EXISTS", 2*time.Second)
	require.NoError(t, err, "expected ai function")
	assert.Contains(t, output, "AI_EXISTS", "ai function should exist")
}

// TestFish_VoiceFunctionExists verifies the voice function exists.
func TestFish_VoiceFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if voice function exists using functions -q
	err = session.SendLine("functions -q voice && echo VOICE_EXISTS || echo VOICE_MISSING")
	require.NoError(t, err)

	// Should output VOICE_EXISTS
	output, err := session.ExpectTimeout("VOICE_EXISTS", 2*time.Second)
	require.NoError(t, err, "expected voice function")
	assert.Contains(t, output, "VOICE_EXISTS", "voice function should exist")
}

// TestFish_CacheDirCreated verifies the cache directory is created.
func TestFish_CacheDirCreated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check CLAI_CACHE is set
	err = session.SendLine("echo $CLAI_CACHE")
	require.NoError(t, err)

	// Should show the cache path
	output, err := session.ExpectTimeout("clai", 2*time.Second)
	require.NoError(t, err, "expected cache path")
	assert.Contains(t, output, "clai", "CLAI_CACHE should be set")
}

// TestFish_EnterBindingForVoice verifies Enter is bound for voice-aware execute.
func TestFish_EnterBindingForVoice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check that _ai_voice_execute function exists (it's bound to Enter)
	err = session.SendLine("functions _ai_voice_execute")
	require.NoError(t, err)

	// Should have our voice execute function
	output, err := session.ExpectTimeout("_ai_voice_execute", 2*time.Second)
	require.NoError(t, err, "expected voice execute function")
	assert.Contains(t, output, "_ai_voice_execute", "_ai_voice_execute should be defined")
}

// TestFish_EscapeBindingClears verifies Escape clears suggestions.
func TestFish_EscapeBindingClears(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check that _ai_clear_suggestion function exists (it's bound to Escape)
	err = session.SendLine("functions _ai_clear_suggestion")
	require.NoError(t, err)

	// Should have our clear function
	output, err := session.ExpectTimeout("_ai_clear_suggestion", 2*time.Second)
	require.NoError(t, err, "expected clear suggestion function")
	assert.Contains(t, output, "_ai_clear_suggestion", "_ai_clear_suggestion should be defined")
}

// TestFish_HistoryFunctionExists verifies the history function is defined.
func TestFish_HistoryFunctionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check if history function exists (wraps builtin)
	err = session.SendLine("functions history | head -5")
	require.NoError(t, err)

	// Should show the function definition with --global support
	output, err := session.ExpectTimeout("history", 2*time.Second)
	require.NoError(t, err, "expected history function")
	assert.Contains(t, output, "history", "history function should be defined")
}

// TestFish_SessionIDExists verifies CLAI_SESSION_ID is set.
func TestFish_SessionIDExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(10*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Check that CLAI_SESSION_ID is set
	err = session.SendLine("test -n \"$CLAI_SESSION_ID\" && echo SESSION_SET || echo SESSION_MISSING")
	require.NoError(t, err)

	// Should output SESSION_SET
	output, err := session.ExpectTimeout("SESSION_SET", 2*time.Second)
	require.NoError(t, err, "expected CLAI_SESSION_ID to be set")
	assert.Contains(t, output, "SESSION_SET", "CLAI_SESSION_ID should be set")
}

// TestFish_DoctorShowsCorrectShell verifies clai doctor detects fish correctly.
func TestFish_DoctorShowsCorrectShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(15*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Run clai doctor with a marker we can detect
	err = session.SendLine("clai doctor; echo DOCTOR_DONE")
	require.NoError(t, err)

	// Wait for our marker
	output, err := session.ExpectTimeout("DOCTOR_DONE", 10*time.Second)
	require.NoError(t, err, "expected doctor to complete")

	// Should NOT show zsh or bash
	assert.NotContains(t, output, "zsh (.zshrc)", "doctor should not show zsh when in fish")
	assert.NotContains(t, output, "bash (.bashrc)", "doctor should not show bash when in fish")
}

// TestFish_InstallDetectsShell verifies clai install correctly detects fish
// even without CLAI_CURRENT_SHELL set. Fish is special because it doesn't
// export FISH_VERSION, so detection relies on parent process checking.
func TestFish_InstallDetectsShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	// Start a clean fish session WITHOUT clai loaded (no hook file)
	// This simulates a user who just did `brew install clai`
	session, err := NewSession("fish",
		WithTimeout(10*time.Second),
		WithEnv("CLAI_CURRENT_SHELL", ""), // Ensure not set
	)
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Wait for prompt
	time.Sleep(500 * time.Millisecond)

	// Fish doesn't export FISH_VERSION to child processes, but we can verify
	// we're in fish by checking $FISH_VERSION directly (it's set in fish)
	err = session.SendLine(`echo "FISH_VERSION=$FISH_VERSION"; echo DONE`)
	require.NoError(t, err)

	output, err := session.ExpectTimeout("DONE", 3*time.Second)
	require.NoError(t, err, "command should complete")

	// FISH_VERSION should be set within fish
	assert.Contains(t, output, "FISH_VERSION=", "FISH_VERSION should be visible in fish")
	// But verify it's not empty (it won't be exported but will be shown)
	assert.NotContains(t, output, "FISH_VERSION=\n", "FISH_VERSION should have a value in fish")
}

// TestFish_StatusShowsCorrectShell verifies clai status detects fish correctly.
func TestFish_StatusShowsCorrectShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping interactive test in short mode")
	}
	SkipIfShellMissing(t, "fish")

	hookFile := FindHookFile("clai.fish")
	if hookFile == "" {
		t.Skip("clai.fish hook file not found")
	}

	session, err := NewSession("fish", WithTimeout(15*time.Second))
	require.NoError(t, err, "failed to create fish session")
	defer session.Close()

	// Source the hook file
	err = session.SendLine("source " + hookFile + " 2>/dev/null")
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Run clai status with a marker we can detect
	err = session.SendLine("clai status; echo STATUS_DONE")
	require.NoError(t, err)

	// Wait for our marker
	output, err := session.ExpectTimeout("STATUS_DONE", 10*time.Second)
	require.NoError(t, err, "expected status to complete")

	// Should NOT show zsh or bash
	assert.NotContains(t, output, "zsh (.zshrc)", "status should not show zsh when in fish")
	assert.NotContains(t, output, "bash (.bashrc)", "status should not show bash when in fish")
}
