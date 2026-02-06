package shim

import (
	"testing"
)

func TestParseShimEvent_SessionStart(t *testing.T) {
	data := []byte(`{"type":"session_start","session_id":"s1","cwd":"/tmp","shell":"zsh"}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("ParseShimEvent() error = %v", err)
	}
	if ev.Type != EventSessionStart {
		t.Errorf("Type = %q, want %q", ev.Type, EventSessionStart)
	}
	if ev.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "s1")
	}
	if ev.Cwd != "/tmp" {
		t.Errorf("Cwd = %q, want %q", ev.Cwd, "/tmp")
	}
	if ev.Shell != "zsh" {
		t.Errorf("Shell = %q, want %q", ev.Shell, "zsh")
	}
}

func TestParseShimEvent_CommandStart(t *testing.T) {
	data := []byte(`{"type":"command_start","session_id":"s1","command_id":"c1","cwd":"/home","command":"ls -la","git_branch":"main","git_repo_name":"clai","git_repo_root":"/home/clai","prev_command_id":"c0"}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("ParseShimEvent() error = %v", err)
	}
	if ev.Type != EventCommandStart {
		t.Errorf("Type = %q, want %q", ev.Type, EventCommandStart)
	}
	if ev.CommandID != "c1" {
		t.Errorf("CommandID = %q, want %q", ev.CommandID, "c1")
	}
	if ev.Command != "ls -la" {
		t.Errorf("Command = %q, want %q", ev.Command, "ls -la")
	}
	if ev.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want %q", ev.GitBranch, "main")
	}
	if ev.GitRepoName != "clai" {
		t.Errorf("GitRepoName = %q, want %q", ev.GitRepoName, "clai")
	}
	if ev.GitRepoRoot != "/home/clai" {
		t.Errorf("GitRepoRoot = %q, want %q", ev.GitRepoRoot, "/home/clai")
	}
	if ev.PrevCommandID != "c0" {
		t.Errorf("PrevCommandID = %q, want %q", ev.PrevCommandID, "c0")
	}
}

func TestParseShimEvent_CommandEnd(t *testing.T) {
	data := []byte(`{"type":"command_end","session_id":"s1","command_id":"c1","exit_code":1,"duration_ms":1500}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("ParseShimEvent() error = %v", err)
	}
	if ev.Type != EventCommandEnd {
		t.Errorf("Type = %q, want %q", ev.Type, EventCommandEnd)
	}
	if ev.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", ev.ExitCode)
	}
	if ev.DurationMs != 1500 {
		t.Errorf("DurationMs = %d, want 1500", ev.DurationMs)
	}
}

func TestParseShimEvent_SessionEnd(t *testing.T) {
	data := []byte(`{"type":"session_end","session_id":"s1"}`)
	ev, err := ParseShimEvent(data)
	if err != nil {
		t.Fatalf("ParseShimEvent() error = %v", err)
	}
	if ev.Type != EventSessionEnd {
		t.Errorf("Type = %q, want %q", ev.Type, EventSessionEnd)
	}
	if ev.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "s1")
	}
}

func TestParseShimEvent_EmptyData(t *testing.T) {
	_, err := ParseShimEvent([]byte{})
	if err == nil {
		t.Fatal("ParseShimEvent(empty) should return error")
	}
}

func TestParseShimEvent_InvalidJSON(t *testing.T) {
	_, err := ParseShimEvent([]byte(`{not json}`))
	if err == nil {
		t.Fatal("ParseShimEvent(invalid) should return error")
	}
}

func TestParseShimEvent_MissingType(t *testing.T) {
	_, err := ParseShimEvent([]byte(`{"session_id":"s1"}`))
	if err == nil {
		t.Fatal("ParseShimEvent(no type) should return error")
	}
}

func TestParseShimEvent_UnknownType(t *testing.T) {
	_, err := ParseShimEvent([]byte(`{"type":"foobar","session_id":"s1"}`))
	if err == nil {
		t.Fatal("ParseShimEvent(unknown type) should return error")
	}
}

func TestParseShimEvent_MissingSessionID(t *testing.T) {
	_, err := ParseShimEvent([]byte(`{"type":"session_start"}`))
	if err == nil {
		t.Fatal("ParseShimEvent(no session_id) should return error")
	}
}
