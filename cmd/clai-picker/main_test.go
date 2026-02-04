package main

import (
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

func TestDispatch_BuiltinBackend(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.History.PickerBackend = "builtin"
	opts := &pickerOpts{}

	code := dispatch(cfg, opts)
	if code != exitSuccess {
		t.Errorf("expected exit code %d, got %d", exitSuccess, code)
	}
}

func TestDispatch_ClaiBackend(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.History.PickerBackend = "clai"
	opts := &pickerOpts{}

	code := dispatch(cfg, opts)
	if code != exitSuccess {
		t.Errorf("expected exit code %d, got %d", exitSuccess, code)
	}
}

func TestDispatch_EmptyBackendDefaultsToBuiltin(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.History.PickerBackend = ""
	opts := &pickerOpts{}

	code := dispatch(cfg, opts)
	if code != exitSuccess {
		t.Errorf("expected exit code %d, got %d", exitSuccess, code)
	}
}

func TestDispatchBackend_FzfFallsBackWhenMissing(t *testing.T) {
	// fzf is likely not on PATH in test environments, so this should fall back.
	// Even if fzf IS on PATH, both branches return exitSuccess.
	opts := &pickerOpts{}
	code := dispatchBackend("fzf", opts)
	if code != exitSuccess {
		t.Errorf("expected exit code %d, got %d", exitSuccess, code)
	}
}

func TestDispatchBackend_UnknownFallsBackToBuiltin(t *testing.T) {
	opts := &pickerOpts{}
	code := dispatchBackend("unknown_backend", opts)
	if code != exitSuccess {
		t.Errorf("expected exit code %d, got %d", exitSuccess, code)
	}
}
