package shim

import (
	"testing"
)

func TestParseShimEvent(t *testing.T) {
	t.Run("valid session_start", func(t *testing.T) {
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
	})

	t.Run("valid session_end", func(t *testing.T) {
		data := []byte(`{"type":"session_end","session_id":"s1"}`)
		ev, err := ParseShimEvent(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Type != EventSessionEnd {
			t.Errorf("type = %q, want %q", ev.Type, EventSessionEnd)
		}
	})

	t.Run("valid command_start", func(t *testing.T) {
		data := []byte(`{"type":"command_start","session_id":"s1","command_id":"c1","cwd":"/tmp","command":"ls -la","git_branch":"main","git_repo_name":"myrepo","git_repo_root":"/repo","prev_command_id":"c0"}`)
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
	})

	t.Run("valid command_end", func(t *testing.T) {
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
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := ParseShimEvent([]byte{})
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ParseShimEvent([]byte(`{not json}`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("missing type", func(t *testing.T) {
		_, err := ParseShimEvent([]byte(`{"session_id":"s1"}`))
		if err == nil {
			t.Fatal("expected error for missing type")
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		_, err := ParseShimEvent([]byte(`{"type":"unknown","session_id":"s1"}`))
		if err == nil {
			t.Fatal("expected error for unknown type")
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		_, err := ParseShimEvent([]byte(`{"type":"session_start"}`))
		if err == nil {
			t.Fatal("expected error for missing session_id")
		}
	})
}
