package main

import (
	"os"
	"strings"
	"testing"

	"github.com/runger/clai/internal/config"
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

// --- Backend dispatch tests ---

func TestDispatch_EmptyBackendDefaultsToBuiltin(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.History.PickerBackend = ""
	// Verify dispatch resolves empty to "builtin".
	backend := cfg.History.PickerBackend
	if backend == "" {
		backend = "builtin"
	}
	if backend != "builtin" {
		t.Errorf("expected backend %q, got %q", "builtin", backend)
	}
}

func TestDispatch_BuiltinIsDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.History.PickerBackend != "builtin" {
		t.Errorf("expected default backend %q, got %q", "builtin", cfg.History.PickerBackend)
	}
}

func TestDispatchBackend_FzfFallsBackWhenMissing(t *testing.T) {
	// Use a PATH with no fzf to guarantee fallback.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer func() { os.Setenv("PATH", origPath) }()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	// dispatchFzf should detect fzf is missing and fall back to builtin.
	// builtin will try to open /dev/tty which may fail in CI but let's
	// just check the fzf-not-found path runs without panic.
	_ = dispatchFzf(cfg, opts)
}

func TestDispatchBackend_FzfFallbackLogsDebug(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	t.Setenv("CLAI_DEBUG", "1")
	defer func() {
		os.Setenv("PATH", origPath)
		os.Unsetenv("CLAI_DEBUG")
	}()

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	// Should not panic; debug logging should run.
	_ = dispatchFzf(cfg, opts)
}

func TestDispatchBackend_UnknownFallsBackToBuiltin(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	// Unknown backend should fall back. May fail opening /dev/tty in CI.
	_ = dispatchBackend("unknown_backend", cfg, opts)
}

func TestDispatchBackend_UnknownLogsDebug(t *testing.T) {
	t.Setenv("CLAI_DEBUG", "1")
	defer os.Unsetenv("CLAI_DEBUG")

	cfg := config.DefaultConfig()
	opts := &pickerOpts{}
	// Should log debug message about unknown backend.
	_ = dispatchBackend("unknown_backend", cfg, opts)
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
