package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/picker"
)

// --- Query sanitization tests ---

func TestSanitizeQuery_Empty(t *testing.T) {
	result, err := sanitizeQuery("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestSanitizeQuery_PlainText(t *testing.T) {
	result, err := sanitizeQuery("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", result)
	}
}

func TestSanitizeQuery_StripControlChars(t *testing.T) {
	// 0x01 (SOH), 0x02 (STX), 0x07 (BEL) should be stripped
	input := "hello\x01\x02\x07world"
	result, err := sanitizeQuery(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "helloworld" {
		t.Fatalf("expected %q, got %q", "helloworld", result)
	}
}

func TestSanitizeQuery_PreserveTab(t *testing.T) {
	input := "hello\tworld"
	result, err := sanitizeQuery(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello\tworld" {
		t.Fatalf("expected tab to be preserved, got %q", result)
	}
}

func TestSanitizeQuery_RejectNewline(t *testing.T) {
	_, err := sanitizeQuery("hello\nworld")
	if err == nil {
		t.Fatal("expected error for newline in query")
	}
	if !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected error about newlines, got: %v", err)
	}
}

func TestSanitizeQuery_RejectCarriageReturn(t *testing.T) {
	_, err := sanitizeQuery("hello\rworld")
	if err == nil {
		t.Fatal("expected error for carriage return in query")
	}
	if !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected error about newlines, got: %v", err)
	}
}

func TestSanitizeQuery_TruncateLong(t *testing.T) {
	input := strings.Repeat("a", 5000)
	result, err := sanitizeQuery(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != maxQueryLen {
		t.Fatalf("expected length %d, got %d", maxQueryLen, len(result))
	}
}

func TestSanitizeQuery_ExactMaxLen(t *testing.T) {
	input := strings.Repeat("b", maxQueryLen)
	result, err := sanitizeQuery(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != maxQueryLen {
		t.Fatalf("expected length %d, got %d", maxQueryLen, len(result))
	}
}

func TestSanitizeQuery_TruncateLongUtf8Safe(t *testing.T) {
	// 4094 bytes + 3-byte rune + 1 byte => 4098 bytes total.
	// Truncation at 4096 would split the rune unless boundary-safe.
	input := strings.Repeat("a", 4094) + "界" + "x"
	result, err := sanitizeQuery(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !utf8.ValidString(result) {
		t.Fatalf("expected valid UTF-8 after truncation, got invalid string")
	}
	if len(result) > maxQueryLen {
		t.Fatalf("expected len <= %d, got %d", maxQueryLen, len(result))
	}
}

// --- Flag validation tests ---

func TestParseHistoryFlags_ValidFlags(t *testing.T) {
	args := []string{"--tabs", "session,global", "--limit", "50", "--query", "hello", "--session", "abc123", "--output", "plain", "--cwd", "/tmp"}
	opts, err := parseHistoryFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.tabs != "session,global" {
		t.Errorf("tabs: expected %q, got %q", "session,global", opts.tabs)
	}
	if opts.limit != 50 {
		t.Errorf("limit: expected 50, got %d", opts.limit)
	}
	if opts.query != "hello" {
		t.Errorf("query: expected %q, got %q", "hello", opts.query)
	}
	if opts.session != "abc123" {
		t.Errorf("session: expected %q, got %q", "abc123", opts.session)
	}
	if opts.output != "plain" {
		t.Errorf("output: expected %q, got %q", "plain", opts.output)
	}
	if opts.cwd != "/tmp" {
		t.Errorf("cwd: expected %q, got %q", "/tmp", opts.cwd)
	}
}

func TestParseHistoryFlags_NoFlags(t *testing.T) {
	opts, err := parseHistoryFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.tabs != "" {
		t.Errorf("expected empty tabs, got %q", opts.tabs)
	}
	if opts.limit != 0 {
		t.Errorf("expected limit 0, got %d", opts.limit)
	}
}

func TestParseHistoryFlags_UnknownFlag(t *testing.T) {
	_, err := parseHistoryFlags([]string{"--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseHistoryFlags_InvalidLimit(t *testing.T) {
	_, err := parseHistoryFlags([]string{"--limit", "-5"})
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
}

func TestParseHistoryFlags_InvalidOutput(t *testing.T) {
	_, err := parseHistoryFlags([]string{"--output", "json"})
	if err == nil {
		t.Fatal("expected error for invalid output format")
	}
}

func TestParseHistoryFlags_QueryWithNewline(t *testing.T) {
	_, err := parseHistoryFlags([]string{"--query", "hello\nworld"})
	if err == nil {
		t.Fatal("expected error for query with newline")
	}
}

func TestParseHistoryFlags_QueryStripsControlChars(t *testing.T) {
	opts, err := parseHistoryFlags([]string{"--query", "hello\x01world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.query != "helloworld" {
		t.Errorf("expected control chars stripped, got %q", opts.query)
	}
}

func TestParseHistoryFlags_UnexpectedPositionalArg(t *testing.T) {
	_, err := parseHistoryFlags([]string{"extra"})
	if err == nil {
		t.Fatal("expected error for unexpected positional argument")
	}
}

func TestSanitizeQuery_StripANSI(t *testing.T) {
	// ANSI escape codes contain 0x1B (ESC) which is a control char < 0x20,
	// so sanitizeQuery strips it byte-by-byte. The '[31m' chars are printable
	// and preserved. This verifies the function handles ANSI-like input.
	input := "\x1b[31mhello\x1b[0m"
	result, err := sanitizeQuery(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ESC (0x1B) is stripped as a control char. The rest are printable.
	if strings.Contains(result, "\x1b") {
		t.Fatalf("expected ESC bytes stripped, got %q", result)
	}
}

func TestParseHistoryFlags_LimitZeroIsValid(t *testing.T) {
	opts, err := parseHistoryFlags([]string{"--limit", "0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.limit != 0 {
		t.Errorf("expected limit 0, got %d", opts.limit)
	}
}

func TestParseHistoryFlags_OutputPlainIsValid(t *testing.T) {
	opts, err := parseHistoryFlags([]string{"--output", "plain"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.output != "plain" {
		t.Errorf("expected output %q, got %q", "plain", opts.output)
	}
}

func TestParseHistoryFlags_OutputEmptyIsValid(t *testing.T) {
	opts, err := parseHistoryFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.output != "" {
		t.Errorf("expected empty output, got %q", opts.output)
	}
}

// --- Color profile tests ---

// TestSetColorProfile_PipeDetectsAscii verifies that lipgloss detects Ascii
// (no color) when output goes to a pipe, which is the root cause of the
// no-color-in-zsh bug (stdout is a pipe in $(clai-picker ...) subshells).
func TestSetColorProfile_PipeDetectsAscii(t *testing.T) {
	// Create a pipe — lipgloss should detect no color capabilities.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	output := termenv.NewOutput(w)
	profile := output.ColorProfile()
	if profile != termenv.Ascii {
		t.Errorf("expected Ascii profile for pipe, got %v", profile)
	}
}

// TestSetColorProfile_ModifiesDefaultRenderer verifies that
// lipgloss.SetColorProfile modifies the existing default renderer in-place,
// so package-level styles pick up the change. This is the fix for colors
// not appearing when styles are created at init time.
func TestSetColorProfile_ModifiesDefaultRenderer(t *testing.T) {
	// Save and restore the original profile after the test.
	origProfile := lipgloss.DefaultRenderer().ColorProfile()
	defer lipgloss.SetColorProfile(origProfile)

	// Force Ascii (no color) to simulate pipe detection.
	lipgloss.SetColorProfile(termenv.Ascii)

	// A style created now should produce plain text (no ANSI codes).
	s := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	plainOutput := s.Render("hello")

	// Now switch to ANSI256 (simulating our tty detection fix).
	lipgloss.SetColorProfile(termenv.ANSI256)

	// The same style object should now produce colored output.
	colorOutput := s.Render("hello")

	// In Ascii mode, Render should return plain "hello" (no escape codes).
	if strings.Contains(plainOutput, "\x1b[") {
		t.Errorf("Ascii profile should not produce ANSI codes, got %q", plainOutput)
	}

	// In ANSI256 mode, Render should include ANSI escape codes.
	if !strings.Contains(colorOutput, "\x1b[") {
		t.Errorf("ANSI256 profile should produce ANSI codes, got %q", colorOutput)
	}
}

// TestSetColorProfile_TtyDetectsColor verifies that a real tty (/dev/tty)
// is detected as having color support, which is the core of the fix.
func TestSetColorProfile_TtyDetectsColor(t *testing.T) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no /dev/tty available (CI environment)")
	}
	defer tty.Close()

	output := termenv.NewOutput(tty)
	profile := output.ColorProfile()

	// A real terminal should support at least ANSI colors.
	if profile == termenv.Ascii {
		t.Errorf("expected color support from /dev/tty, got Ascii")
	}
}

// --- Backend dispatch tests ---

func TestDispatch_BuiltinIsDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.History.PickerBackend != "builtin" {
		t.Errorf("expected default backend %q, got %q", "builtin", cfg.History.PickerBackend)
	}
}

func TestDispatchBackend_FzfNotFoundWithEmptyPath(t *testing.T) {
	// Verify fzf lookup fails with an empty PATH.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer func() { os.Setenv("PATH", origPath) }()

	_, err := exec.LookPath("fzf")
	if err == nil {
		t.Fatal("expected fzf to not be found with empty PATH")
	}
}

// --- Tab resolution tests ---

func TestResolveTabs_AllTabs(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{tabs: ""}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != len(cfg.History.PickerTabs) {
		t.Errorf("expected %d tabs, got %d", len(cfg.History.PickerTabs), len(tabs))
	}
}

func TestResolveTabs_FilterByID(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{tabs: "session"}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].ID != "session" {
		t.Errorf("expected tab ID %q, got %q", "session", tabs[0].ID)
	}
}

func TestResolveTabs_MultipleTabs(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{tabs: "session,global"}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
}

func TestResolveTabs_UnknownFallsBackToAll(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{tabs: "nonexistent"}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != len(cfg.History.PickerTabs) {
		t.Errorf("expected fallback to all %d tabs, got %d", len(cfg.History.PickerTabs), len(tabs))
	}
}

// --- Socket path tests ---

func TestSocketPath_DefaultWhenEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Daemon.SocketPath = ""

	path := socketPath(cfg)
	expected := config.DefaultPaths().SocketFile()
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestSocketPath_CustomOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Daemon.SocketPath = "/tmp/test.sock"

	path := socketPath(cfg)
	if path != "/tmp/test.sock" {
		t.Errorf("expected %q, got %q", "/tmp/test.sock", path)
	}
}

// --- Variable substitution tests ---

func TestResolveTabs_SubstitutesSessionID(t *testing.T) {
	cfg := config.DefaultConfig()
	// Default config has "session" tab with Args: {"session": "$CLAI_SESSION_ID"}
	opts := &pickerOpts{
		tabs:    "session",
		session: "my-actual-session-123",
	}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}

	// Verify the $CLAI_SESSION_ID was substituted with the actual session ID.
	if sid, ok := tabs[0].Args["session"]; !ok {
		t.Error("expected 'session' key in Args")
	} else if sid != "my-actual-session-123" {
		t.Errorf("expected session substituted to 'my-actual-session-123', got %q", sid)
	}
}

func TestResolveTabs_NoSubstitutionWithEmptySession(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{
		tabs:    "session",
		session: "", // Empty session ID
	}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}

	// When session is empty, $CLAI_SESSION_ID should remain as-is.
	if sid, ok := tabs[0].Args["session"]; !ok {
		t.Error("expected 'session' key in Args")
	} else if sid != "$CLAI_SESSION_ID" {
		t.Errorf("expected literal '$CLAI_SESSION_ID' when session is empty, got %q", sid)
	}
}

func TestResolveTabs_PreservesOtherArgs(t *testing.T) {
	cfg := config.DefaultConfig()
	// Add a custom tab with mixed Args.
	cfg.History.PickerTabs = append(cfg.History.PickerTabs, config.TabDef{
		ID:    "custom",
		Label: "Custom",
		Args: map[string]string{
			"session":   "$CLAI_SESSION_ID",
			"other_key": "static_value",
		},
	})

	opts := &pickerOpts{
		tabs:    "custom",
		session: "sess-456",
	}

	tabs := resolveTabs(cfg, opts)
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}

	// Verify session was substituted.
	if tabs[0].Args["session"] != "sess-456" {
		t.Errorf("expected session 'sess-456', got %q", tabs[0].Args["session"])
	}

	// Verify other_key was preserved.
	if tabs[0].Args["other_key"] != "static_value" {
		t.Errorf("expected other_key 'static_value', got %q", tabs[0].Args["other_key"])
	}
}

func TestResolveTabs_DoesNotModifyOriginalConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{
		tabs:    "session",
		session: "modified-session",
	}

	// Get the original value before calling resolveTabs.
	var originalSessionArg string
	for _, tab := range cfg.History.PickerTabs {
		if tab.ID == "session" {
			originalSessionArg = tab.Args["session"]
			break
		}
	}

	_ = resolveTabs(cfg, opts)

	// Verify the original config was not modified.
	for _, tab := range cfg.History.PickerTabs {
		if tab.ID == "session" {
			if tab.Args["session"] != originalSessionArg {
				t.Errorf("original config was modified: expected %q, got %q",
					originalSessionArg, tab.Args["session"])
			}
			break
		}
	}
}

func TestNewSuggestModel_UsesBottomUpLayout(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{
		session: "sess-123",
		cwd:     "/tmp",
		query:   "git st",
	}

	m := newSuggestModel(cfg, opts)
	if m.Layout() != picker.LayoutBottomUp {
		t.Fatalf("expected LayoutBottomUp, got %v", m.Layout())
	}
}

func restoreMainHooks() func() {
	origCheckTTY := checkTTYFn
	origCheckTERM := checkTERMFn
	origCheckTermWidth := checkTermWidthFn
	origMkdirAll := mkdirAllFn
	origDefaultPaths := defaultPathsFn
	origAcquireLock := acquireLockFn
	origReleaseLock := releaseLockFn
	origLoadConfig := loadConfigFn
	origDispatchHistory := dispatchHistoryFn
	origDispatchSuggest := dispatchSuggestFn
	origDispatchBuiltin := dispatchBuiltinFn
	origDispatchFzf := dispatchFzfFn
	origRunTUI := runTUIFn
	origLookPath := lookPathFn
	origNewHistoryProvider := newHistoryProviderFn
	origRunFzfCommand := runFzfCommandOutputFn

	return func() {
		checkTTYFn = origCheckTTY
		checkTERMFn = origCheckTERM
		checkTermWidthFn = origCheckTermWidth
		mkdirAllFn = origMkdirAll
		defaultPathsFn = origDefaultPaths
		acquireLockFn = origAcquireLock
		releaseLockFn = origReleaseLock
		loadConfigFn = origLoadConfig
		dispatchHistoryFn = origDispatchHistory
		dispatchSuggestFn = origDispatchSuggest
		dispatchBuiltinFn = origDispatchBuiltin
		dispatchFzfFn = origDispatchFzf
		runTUIFn = origRunTUI
		lookPathFn = origLookPath
		newHistoryProviderFn = origNewHistoryProvider
		runFzfCommandOutputFn = origRunFzfCommand
	}
}

func captureStdoutStderr(t *testing.T, fn func()) (string, string) {
	t.Helper()
	origStdout := os.Stdout
	origStderr := os.Stderr

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW
	fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr

	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)
	_ = stdoutR.Close()
	_ = stderrR.Close()
	return string(stdoutBytes), string(stderrBytes)
}

func TestParseRunInputs_CoversHistorySuggestAndErrors(t *testing.T) {
	cmd, opts, exitCode, showUsage, err := parseRunInputs([]string{"history", "--limit", "3"})
	if err != nil {
		t.Fatalf("history parse failed: %v", err)
	}
	if cmd != cmdHistory || opts.limit != 3 || exitCode != 0 || showUsage {
		t.Fatalf("unexpected history parse result: cmd=%q limit=%d exit=%d usage=%v", cmd, opts.limit, exitCode, showUsage)
	}

	cmd, opts, exitCode, showUsage, err = parseRunInputs([]string{"suggest", "--limit", "2", "--query", "git"})
	if err != nil {
		t.Fatalf("suggest parse failed: %v", err)
	}
	if cmd != cmdSuggest || opts.limit != 2 || opts.query != "git" || exitCode != 0 || showUsage {
		t.Fatalf("unexpected suggest parse result: cmd=%q limit=%d query=%q exit=%d usage=%v", cmd, opts.limit, opts.query, exitCode, showUsage)
	}

	cmd, opts, exitCode, showUsage, err = parseRunInputs([]string{"nope"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if cmd != cmdUnknown || opts != nil || exitCode != exitFallback || !showUsage {
		t.Fatalf("unexpected unknown parse result: cmd=%q opts=%v exit=%d usage=%v", cmd, opts, exitCode, showUsage)
	}
}

func TestParseSuggestFlags_Validation(t *testing.T) {
	opts, err := parseSuggestFlags([]string{"--limit", "4", "--query", "x", "--output", "plain", "--session", "s", "--cwd", "/tmp"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.limit != 4 || opts.query != "x" || opts.output != "plain" || opts.session != "s" || opts.cwd != "/tmp" {
		t.Fatalf("unexpected parsed options: %+v", opts)
	}

	if _, err := parseSuggestFlags([]string{"--limit", "-1"}); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := parseSuggestFlags([]string{"--output", "json"}); err == nil {
		t.Fatal("expected output error")
	}
	if _, err := parseSuggestFlags([]string{"--query", "a\nb"}); err == nil {
		t.Fatal("expected query newline error")
	}
}

func TestApplyRunDefaults_CoversHistoryAndSuggest(t *testing.T) {
	cfg := config.DefaultConfig()

	historyOpts := &pickerOpts{}
	applyRunDefaults(cmdHistory, cfg, historyOpts)
	if historyOpts.limit != cfg.History.PickerPageSize {
		t.Fatalf("expected history limit default %d, got %d", cfg.History.PickerPageSize, historyOpts.limit)
	}
	if historyOpts.tabs == "" {
		t.Fatal("expected history tabs default to be set")
	}

	suggestOpts := &pickerOpts{}
	applyRunDefaults(cmdSuggest, cfg, suggestOpts)
	if suggestOpts.limit != cfg.Suggestions.MaxResults {
		t.Fatalf("expected suggest limit default %d, got %d", cfg.Suggestions.MaxResults, suggestOpts.limit)
	}
}

func TestDispatchRunCommand_Delegates(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	dispatchHistoryFn = func(_ *config.Config, _ *pickerOpts) int { return 11 }
	dispatchSuggestFn = func(_ *config.Config, _ *pickerOpts) int { return 22 }

	if got := dispatchRunCommand(cmdHistory, cfg, opts); got != 11 {
		t.Fatalf("history dispatch = %d, want 11", got)
	}
	if got := dispatchRunCommand(cmdSuggest, cfg, opts); got != 22 {
		t.Fatalf("suggest dispatch = %d, want 22", got)
	}
}

func TestDispatchBackend_CoversBranches(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	dispatchBuiltinFn = func(_ *config.Config, _ *pickerOpts) int { return 3 }
	dispatchFzfFn = func(_ *config.Config, _ *pickerOpts) int { return 4 }

	if got := dispatchBackend("builtin", cfg, opts); got != 3 {
		t.Fatalf("builtin dispatch = %d, want 3", got)
	}
	if got := dispatchBackend("clai", cfg, opts); got != 3 {
		t.Fatalf("clai dispatch = %d, want 3", got)
	}
	if got := dispatchBackend("fzf", cfg, opts); got != 4 {
		t.Fatalf("fzf dispatch = %d, want 4", got)
	}
	if got := dispatchBackend("unknown", cfg, opts); got != 3 {
		t.Fatalf("unknown backend fallback = %d, want 3", got)
	}
}

func TestDispatchHistory_UsesConfiguredBackend(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	dispatchBuiltinFn = func(_ *config.Config, _ *pickerOpts) int { return 9 }
	dispatchFzfFn = func(_ *config.Config, _ *pickerOpts) int { return 8 }

	cfg.History.PickerBackend = "builtin"
	if got := dispatchHistory(cfg, opts); got != 9 {
		t.Fatalf("expected builtin backend result 9, got %d", got)
	}

	cfg.History.PickerBackend = "fzf"
	if got := dispatchHistory(cfg, opts); got != 8 {
		t.Fatalf("expected fzf backend result 8, got %d", got)
	}
}

func TestDispatchSuggest_SuccessAndFallback(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	runTUIFn = func(_ picker.Model) (int, string) { return exitSuccess, "echo hi" }
	stdout, stderr := captureStdoutStderr(t, func() {
		if got := dispatchSuggest(cfg, opts); got != exitSuccess {
			t.Fatalf("dispatchSuggest success code = %d", got)
		}
	})
	if !strings.Contains(stdout, "echo hi") {
		t.Fatalf("expected stdout to contain result, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}

	runTUIFn = func(_ picker.Model) (int, string) { return exitFallback, "boom" }
	_, stderr = captureStdoutStderr(t, func() {
		if got := dispatchSuggest(cfg, opts); got != exitFallback {
			t.Fatalf("dispatchSuggest fallback code = %d", got)
		}
	})
	if !strings.Contains(stderr, "boom") {
		t.Fatalf("expected fallback error on stderr, got %q", stderr)
	}
}

func TestDispatchBuiltin_SuccessAndFallback(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	runTUIFn = func(_ picker.Model) (int, string) { return exitSuccess, "git status" }
	stdout, _ := captureStdoutStderr(t, func() {
		if got := dispatchBuiltin(cfg, opts); got != exitSuccess {
			t.Fatalf("dispatchBuiltin success code = %d", got)
		}
	})
	if !strings.Contains(stdout, "git status") {
		t.Fatalf("expected stdout to contain selection, got %q", stdout)
	}

	runTUIFn = func(_ picker.Model) (int, string) { return exitFallback, "picker failure" }
	_, stderr := captureStdoutStderr(t, func() {
		if got := dispatchBuiltin(cfg, opts); got != exitFallback {
			t.Fatalf("dispatchBuiltin fallback code = %d", got)
		}
	})
	if !strings.Contains(stderr, "picker failure") {
		t.Fatalf("expected fallback error on stderr, got %q", stderr)
	}
}

func TestDispatchFzf_CoversBranches(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	dispatchBuiltinFn = func(_ *config.Config, _ *pickerOpts) int { return 7 }

	lookPathFn = func(string) (string, error) { return "", errors.New("missing") }
	if got := dispatchFzf(cfg, opts); got != 7 {
		t.Fatalf("expected builtin fallback when fzf missing, got %d", got)
	}

	lookPathFn = func(string) (string, error) { return "/usr/bin/fzf", nil }
	runFzfCommandOutputFn = func(_ []string, _ string) ([]byte, error) { return nil, errors.New("fzf failed") }
	newHistoryProviderFn = func(string) picker.Provider {
		return &fakeHistoryProvider{
			resp: []picker.Response{{Items: []picker.Item{{Value: "git status"}}, AtEnd: true}},
		}
	}
	if got := dispatchFzf(cfg, opts); got != exitFallback {
		t.Fatalf("expected exitFallback when fzf errors, got %d", got)
	}

	runFzfCommandOutputFn = func(_ []string, _ string) ([]byte, error) {
		cmd := exec.Command("sh", "-c", "exit 1")
		return nil, cmd.Run()
	}
	newHistoryProviderFn = func(string) picker.Provider {
		return &fakeHistoryProvider{
			resp: []picker.Response{{Items: []picker.Item{{Value: "git status"}}, AtEnd: true}},
		}
	}
	if got := dispatchFzf(cfg, opts); got != exitCancelled {
		t.Fatalf("expected exitCancelled on fzf no-match, got %d", got)
	}

	// Empty output from backend means no selection, treat as cancel.
	runFzfCommandOutputFn = func(_ []string, _ string) ([]byte, error) { return []byte(""), nil }
	newHistoryProviderFn = func(string) picker.Provider {
		return &fakeHistoryProvider{
			resp: []picker.Response{{Items: []picker.Item{{Value: "git status"}}, AtEnd: true}},
		}
	}
	if got := dispatchFzf(cfg, opts); got != exitCancelled {
		t.Fatalf("expected exitCancelled on empty fzf selection, got %d", got)
	}
}

type fakeHistoryProvider struct {
	err  error
	resp []picker.Response
}

func (f *fakeHistoryProvider) Fetch(_ context.Context, _ picker.Request) (picker.Response, error) {
	if f.err != nil {
		return picker.Response{}, f.err
	}
	if len(f.resp) == 0 {
		return picker.Response{AtEnd: true}, nil
	}
	out := f.resp[0]
	f.resp = f.resp[1:]
	return out, nil
}

func TestRunFzfBackend_PaginatesAndReturnsSelection(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	cfg := config.DefaultConfig()
	cfg.History.PickerPageSize = 1
	opts := &pickerOpts{query: "git"}

	prov := &fakeHistoryProvider{
		resp: []picker.Response{
			{Items: []picker.Item{{Value: "git status"}}, AtEnd: false},
			{Items: []picker.Item{{Value: "git diff"}}, AtEnd: true},
		},
	}
	newHistoryProviderFn = func(string) picker.Provider { return prov }

	var gotArgs []string
	var gotInput string
	runFzfCommandOutputFn = func(args []string, input string) ([]byte, error) {
		gotArgs = append([]string{}, args...)
		gotInput = input
		return []byte("git diff\n"), nil
	}

	out, err := runFzfBackend(cfg, opts)
	if err != nil {
		t.Fatalf("runFzfBackend failed: %v", err)
	}
	if out != "git diff" {
		t.Fatalf("expected selected output, got %q", out)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "--query git") {
		t.Fatalf("expected query args to include --query git, got %v", gotArgs)
	}
	if !strings.Contains(gotInput, "git status") || !strings.Contains(gotInput, "git diff") {
		t.Fatalf("expected fzf input to include fetched history items, got %q", gotInput)
	}
}

func TestRun_CoversEarlyFailureAndSuccessPath(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	checkTTYFn = func() error { return errors.New("no tty") }
	if got := run([]string{"history"}); got != exitFallback {
		t.Fatalf("expected fallback on tty error, got %d", got)
	}

	checkTTYFn = func() error { return nil }
	checkTERMFn = func() error { return nil }
	checkTermWidthFn = func() error { return nil }
	mkdirAllFn = func(string, os.FileMode) error { return nil }
	defaultPathsFn = config.DefaultPaths
	acquireLockFn = func(string) (int, error) { return -1, nil }
	releaseLockFn = func(int) {}
	loadConfigFn = func() (*config.Config, error) { return config.DefaultConfig(), nil }
	dispatchHistoryFn = func(_ *config.Config, _ *pickerOpts) int { return exitSuccess }

	if got := run([]string{"history"}); got != exitSuccess {
		t.Fatalf("expected run success, got %d", got)
	}
}

func TestRun_HelpPathsReturnSuccess(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	checkTTYFn = func() error { return nil }
	checkTERMFn = func() error { return nil }
	checkTermWidthFn = func() error { return nil }
	mkdirAllFn = func(string, os.FileMode) error { return nil }
	defaultPathsFn = config.DefaultPaths
	acquireLockFn = func(string) (int, error) { return -1, nil }
	releaseLockFn = func(int) {}
	loadConfigFn = func() (*config.Config, error) {
		t.Fatal("loadConfigFn should not be called for help/version early exits")
		return nil, errors.New("unexpected call")
	}

	if got := run([]string{"--help"}); got != exitSuccess {
		t.Fatalf("run(--help) = %d, want %d", got, exitSuccess)
	}
	if got := run([]string{"--version"}); got != exitSuccess {
		t.Fatalf("run(--version) = %d, want %d", got, exitSuccess)
	}
	if got := run([]string{"history", "-h"}); got != exitSuccess {
		t.Fatalf("run(history -h) = %d, want %d", got, exitSuccess)
	}
	if got := run([]string{"suggest", "--help"}); got != exitSuccess {
		t.Fatalf("run(suggest --help) = %d, want %d", got, exitSuccess)
	}
}

func TestDebugLogPrintUsageAndPrintVersion(t *testing.T) {
	restore := restoreMainHooks()
	defer restore()

	t.Setenv("CLAI_DEBUG", "1")
	_, stderr := captureStdoutStderr(t, func() {
		debugLog("hello %s", "world")
	})
	if !strings.Contains(stderr, "hello world") {
		t.Fatalf("expected debug log output, got %q", stderr)
	}

	_, stderr = captureStdoutStderr(t, printUsage)
	if !strings.Contains(stderr, "Usage: clai-picker") {
		t.Fatalf("expected usage output, got %q", stderr)
	}

	origVersion, origCommit, origBuildDate := Version, GitCommit, BuildDate
	defer func() {
		Version, GitCommit, BuildDate = origVersion, origCommit, origBuildDate
	}()
	Version, GitCommit, BuildDate = "1.2.3", "abc123", "2026-02-11"
	stdout, _ := captureStdoutStderr(t, printVersion)
	if !strings.Contains(stdout, "clai-picker 1.2.3") || !strings.Contains(stdout, "abc123") {
		t.Fatalf("expected version output, got %q", stdout)
	}
}
