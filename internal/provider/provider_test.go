package provider

import (
	"testing"
)

func TestSuggestion(t *testing.T) {
	s := Suggestion{
		Text:        "ls -la",
		Description: "List all files",
		Source:      "ai",
		Score:       0.9,
		Risk:        "safe",
	}

	if s.Text != "ls -la" {
		t.Errorf("Suggestion.Text = %q, want %q", s.Text, "ls -la")
	}
	if s.Description != "List all files" {
		t.Errorf("Suggestion.Description = %q, want %q", s.Description, "List all files")
	}
	if s.Source != "ai" {
		t.Errorf("Suggestion.Source = %q, want %q", s.Source, "ai")
	}
	if s.Score != 0.9 {
		t.Errorf("Suggestion.Score = %f, want %f", s.Score, 0.9)
	}
	if s.Risk != "safe" {
		t.Errorf("Suggestion.Risk = %q, want %q", s.Risk, "safe")
	}
}

func TestCommandContext(t *testing.T) {
	ctx := CommandContext{
		Command:  "git status",
		ExitCode: 0,
	}

	if ctx.Command != "git status" {
		t.Errorf("CommandContext.Command = %q, want %q", ctx.Command, "git status")
	}
	if ctx.ExitCode != 0 {
		t.Errorf("CommandContext.ExitCode = %d, want %d", ctx.ExitCode, 0)
	}
}

func TestTextToCommandRequest(t *testing.T) {
	req := &TextToCommandRequest{
		Prompt: "list files",
		CWD:    "/home/user",
		OS:     "linux",
		Shell:  "bash",
		RecentCmds: []CommandContext{
			{Command: "cd /home/user", ExitCode: 0},
		},
	}

	if req.Prompt != "list files" {
		t.Errorf("TextToCommandRequest.Prompt = %q, want %q", req.Prompt, "list files")
	}
	if req.CWD != "/home/user" {
		t.Errorf("TextToCommandRequest.CWD = %q, want %q", req.CWD, "/home/user")
	}
	if req.OS != "linux" {
		t.Errorf("TextToCommandRequest.OS = %q, want %q", req.OS, "linux")
	}
	if req.Shell != "bash" {
		t.Errorf("TextToCommandRequest.Shell = %q, want %q", req.Shell, "bash")
	}
	if len(req.RecentCmds) != 1 {
		t.Errorf("len(TextToCommandRequest.RecentCmds) = %d, want %d", len(req.RecentCmds), 1)
	}
}

func TestTextToCommandResponse(t *testing.T) {
	resp := &TextToCommandResponse{
		Suggestions: []Suggestion{
			{Text: "ls -la", Source: "ai", Score: 1.0, Risk: "safe"},
		},
		ProviderName: "anthropic",
		LatencyMs:    150,
	}

	if len(resp.Suggestions) != 1 {
		t.Errorf("len(TextToCommandResponse.Suggestions) = %d, want %d", len(resp.Suggestions), 1)
	}
	if resp.ProviderName != "anthropic" {
		t.Errorf("TextToCommandResponse.ProviderName = %q, want %q", resp.ProviderName, "anthropic")
	}
	if resp.LatencyMs != 150 {
		t.Errorf("TextToCommandResponse.LatencyMs = %d, want %d", resp.LatencyMs, 150)
	}
}

func TestNextStepRequest(t *testing.T) {
	req := &NextStepRequest{
		SessionID:    "session-123",
		LastCommand:  "git add .",
		LastExitCode: 0,
		CWD:          "/project",
		OS:           "darwin",
		Shell:        "zsh",
	}

	if req.SessionID != "session-123" {
		t.Errorf("NextStepRequest.SessionID = %q, want %q", req.SessionID, "session-123")
	}
	if req.LastCommand != "git add ." {
		t.Errorf("NextStepRequest.LastCommand = %q, want %q", req.LastCommand, "git add .")
	}
	if req.LastExitCode != 0 {
		t.Errorf("NextStepRequest.LastExitCode = %d, want %d", req.LastExitCode, 0)
	}
	if req.CWD != "/project" {
		t.Errorf("NextStepRequest.CWD = %q, want %q", req.CWD, "/project")
	}
	if req.OS != "darwin" {
		t.Errorf("NextStepRequest.OS = %q, want %q", req.OS, "darwin")
	}
	if req.Shell != "zsh" {
		t.Errorf("NextStepRequest.Shell = %q, want %q", req.Shell, "zsh")
	}
}

func TestDiagnoseRequest(t *testing.T) {
	req := &DiagnoseRequest{
		SessionID: "session-123",
		Command:   "npm install",
		ExitCode:  1,
		CWD:       "/project",
		OS:        "linux",
		Shell:     "bash",
		StdErr:    "ENOENT: no such file or directory",
	}

	if req.Command != "npm install" {
		t.Errorf("DiagnoseRequest.Command = %q, want %q", req.Command, "npm install")
	}
	if req.ExitCode != 1 {
		t.Errorf("DiagnoseRequest.ExitCode = %d, want %d", req.ExitCode, 1)
	}
	if req.StdErr != "ENOENT: no such file or directory" {
		t.Errorf("DiagnoseRequest.StdErr = %q, want %q", req.StdErr, "ENOENT: no such file or directory")
	}
	if req.SessionID != "session-123" {
		t.Errorf("DiagnoseRequest.SessionID = %q, want %q", req.SessionID, "session-123")
	}
	if req.CWD != "/project" {
		t.Errorf("DiagnoseRequest.CWD = %q, want %q", req.CWD, "/project")
	}
	if req.OS != "linux" {
		t.Errorf("DiagnoseRequest.OS = %q, want %q", req.OS, "linux")
	}
	if req.Shell != "bash" {
		t.Errorf("DiagnoseRequest.Shell = %q, want %q", req.Shell, "bash")
	}
}

func TestDiagnoseResponse(t *testing.T) {
	resp := &DiagnoseResponse{
		Explanation: "The npm install command failed because package.json is missing.",
		Fixes: []Suggestion{
			{Text: "npm init -y", Source: "ai", Score: 1.0, Risk: "safe"},
		},
		ProviderName: "anthropic",
		LatencyMs:    200,
	}

	if resp.Explanation == "" {
		t.Error("DiagnoseResponse.Explanation is empty")
	}
	if len(resp.Fixes) != 1 {
		t.Errorf("len(DiagnoseResponse.Fixes) = %d, want %d", len(resp.Fixes), 1)
	}
	if resp.ProviderName != "anthropic" {
		t.Errorf("DiagnoseResponse.ProviderName = %q, want %q", resp.ProviderName, "anthropic")
	}
	if resp.LatencyMs != 200 {
		t.Errorf("DiagnoseResponse.LatencyMs = %d, want %d", resp.LatencyMs, 200)
	}
}

func TestDefaultTimeout(t *testing.T) {
	if DefaultTimeout.Seconds() != 10 {
		t.Errorf("DefaultTimeout = %v, want 10s", DefaultTimeout)
	}
}
