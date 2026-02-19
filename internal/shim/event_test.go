package shim

import (
	"testing"
)

func testSessionStart(t *testing.T) {
	t.Helper()
	data := []byte(`{"type":"session_start","session_id":"s1","cwd":"/home","shell":"zsh"}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != EventSessionStart {
		t.Errorf("type = %q, want %q", ev.Type, EventSessionStart)
	}
	if ev.SessionID != "s1" {
		t.Errorf("session_id = %q, want %q", ev.SessionID, "s1")
	}
	if ev.Cwd != "/home" {
		t.Errorf("cwd = %q, want %q", ev.Cwd, "/home")
	}
	if ev.Shell != "zsh" {
		t.Errorf("shell = %q, want %q", ev.Shell, "zsh")
	}
}

func testSessionEnd(t *testing.T) {
	t.Helper()
	data := []byte(`{"type":"session_end","session_id":"s1"}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != EventSessionEnd {
		t.Errorf("type = %q, want %q", ev.Type, EventSessionEnd)
	}
	if ev.SessionID != "s1" {
		t.Errorf("session_id = %q, want %q", ev.SessionID, "s1")
	}
}

func testCommandStart(t *testing.T) {
	t.Helper()
	data := []byte(`{"type":"command_start","session_id":"s1","command_id":"c1","cwd":"/tmp",` +
		`"command":"ls -la","git_branch":"main","git_repo_name":"myrepo",` +
		`"git_repo_root":"/repo","prev_command_id":"c0"}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != EventCommandStart {
		t.Errorf("type = %q, want %q", ev.Type, EventCommandStart)
	}
	if ev.CommandID != "c1" {
		t.Errorf("command_id = %q, want %q", ev.CommandID, "c1")
	}
	if ev.Command != "ls -la" {
		t.Errorf("command = %q, want %q", ev.Command, "ls -la")
	}
	if ev.GitBranch != "main" {
		t.Errorf("git_branch = %q, want %q", ev.GitBranch, "main")
	}
	if ev.GitRepoName != "myrepo" {
		t.Errorf("git_repo_name = %q, want %q", ev.GitRepoName, "myrepo")
	}
	if ev.GitRepoRoot != "/repo" {
		t.Errorf("git_repo_root = %q, want %q", ev.GitRepoRoot, "/repo")
	}
	if ev.PrevCommandID != "c0" {
		t.Errorf("prev_command_id = %q, want %q", ev.PrevCommandID, "c0")
	}
}

func testCommandEnd(t *testing.T) {
	t.Helper()
	data := []byte(`{"type":"command_end","session_id":"s1","command_id":"c1","exit_code":1,"duration_ms":500}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != EventCommandEnd {
		t.Errorf("type = %q, want %q", ev.Type, EventCommandEnd)
	}
	if ev.ExitCode != 1 {
		t.Errorf("exit_code = %d, want %d", ev.ExitCode, 1)
	}
	if ev.DurationMs != 500 {
		t.Errorf("duration_ms = %d, want %d", ev.DurationMs, 500)
	}
}

func testParseErrors(t *testing.T) {
	t.Helper()
	tests := []struct {
		name string
		data []byte
	}{
		{"empty data", []byte{}},
		{"invalid JSON", []byte(`{not json}`)},
		{"missing type", []byte(`{"session_id":"s1"}`)},
		{"unknown type", []byte(`{"type":"unknown","session_id":"s1"}`)},
		{"missing session_id", []byte(`{"type":"session_start"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseShimEvent(tt.data)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

func TestParseShimEvent(t *testing.T) {
	t.Run("valid session_start", testSessionStart)
	t.Run("valid session_end", testSessionEnd)
	t.Run("valid command_start", testCommandStart)
	t.Run("valid command_end", testCommandEnd)
	t.Run("parse errors", testParseErrors)
}
