package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

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
