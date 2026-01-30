package provider

import (
	"strings"
	"testing"
)

func TestNewContextBuilder(t *testing.T) {
	cmds := []CommandContext{
		{Command: "ls -la", ExitCode: 0},
	}
	builder := NewContextBuilder("linux", "bash", "/home/user", cmds)

	if builder == nil {
		t.Fatal("NewContextBuilder() returned nil")
	}
	if builder.os != "linux" {
		t.Errorf("builder.os = %q, want %q", builder.os, "linux")
	}
	if builder.shell != "bash" {
		t.Errorf("builder.shell = %q, want %q", builder.shell, "bash")
	}
	if builder.cwd != "/home/user" {
		t.Errorf("builder.cwd = %q, want %q", builder.cwd, "/home/user")
	}
	if len(builder.recentCmds) != 1 {
		t.Errorf("len(builder.recentCmds) = %d, want %d", len(builder.recentCmds), 1)
	}
}

func TestContextBuilder_BuildTextToCommandPrompt(t *testing.T) {
	cmds := []CommandContext{
		{Command: "git status", ExitCode: 0},
		{Command: "git add .", ExitCode: 0},
	}
	builder := NewContextBuilder("darwin", "zsh", "/project", cmds)

	prompt := builder.BuildTextToCommandPrompt("list all files")

	// Check that it contains required elements
	checks := []string{
		"command-line assistant",
		"OS: darwin",
		"Shell: zsh",
		"Working Directory: /project",
		"Recent commands:",
		"git status",
		"exit 0",
		"git add .",
		"User request: list all files",
		"1-3 shell commands",
		"No explanations",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("BuildTextToCommandPrompt() missing %q\nPrompt:\n%s", check, prompt)
		}
	}
}

func TestContextBuilder_BuildTextToCommandPrompt_NoRecentCmds(t *testing.T) {
	builder := NewContextBuilder("linux", "bash", "/home", nil)

	prompt := builder.BuildTextToCommandPrompt("find large files")

	// Should not contain recent commands section
	if strings.Contains(prompt, "Recent commands:") {
		t.Error("BuildTextToCommandPrompt() should not include 'Recent commands:' when empty")
	}

	// But should contain other required elements
	if !strings.Contains(prompt, "OS: linux") {
		t.Error("BuildTextToCommandPrompt() missing OS")
	}
	if !strings.Contains(prompt, "find large files") {
		t.Error("BuildTextToCommandPrompt() missing user request")
	}
}

func TestContextBuilder_BuildNextStepPrompt(t *testing.T) {
	cmds := []CommandContext{
		{Command: "cd /project", ExitCode: 0},
		{Command: "git clone repo", ExitCode: 0},
	}
	builder := NewContextBuilder("darwin", "zsh", "/project", cmds)

	prompt := builder.BuildNextStepPrompt("git status", 0)

	checks := []string{
		"predicting the next command",
		"OS: darwin",
		"Shell: zsh",
		"Working Directory: /project",
		"Last command: git status",
		"Exit code: 0",
		"Previous commands:",
		"Predict 1-3 likely next commands",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("BuildNextStepPrompt() missing %q\nPrompt:\n%s", check, prompt)
		}
	}
}

func TestContextBuilder_BuildNextStepPrompt_FailedCommand(t *testing.T) {
	builder := NewContextBuilder("linux", "bash", "/", nil)

	prompt := builder.BuildNextStepPrompt("npm install", 1)

	if !strings.Contains(prompt, "Exit code: 1") {
		t.Error("BuildNextStepPrompt() should show exit code 1")
	}
	if !strings.Contains(prompt, "npm install") {
		t.Error("BuildNextStepPrompt() should show last command")
	}
}

func TestContextBuilder_BuildDiagnosePrompt(t *testing.T) {
	cmds := []CommandContext{
		{Command: "cd myproject", ExitCode: 0},
	}
	builder := NewContextBuilder("darwin", "zsh", "/project", cmds)

	prompt := builder.BuildDiagnosePrompt("npm install", 1, "ENOENT: no such file")

	checks := []string{
		"diagnosing a failed command",
		"OS: darwin",
		"Shell: zsh",
		"Working Directory: /project",
		"Failed command: npm install",
		"Exit code: 1",
		"Error output:",
		"ENOENT: no such file",
		"Recent command history:",
		"brief explanation",
		"1-3 fix commands",
		"$ ",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("BuildDiagnosePrompt() missing %q\nPrompt:\n%s", check, prompt)
		}
	}
}

func TestContextBuilder_BuildDiagnosePrompt_NoStderr(t *testing.T) {
	builder := NewContextBuilder("linux", "bash", "/", nil)

	prompt := builder.BuildDiagnosePrompt("command", 127, "")

	// Should not include error output section when empty
	if strings.Contains(prompt, "Error output:") {
		t.Error("BuildDiagnosePrompt() should not include 'Error output:' when stderr is empty")
	}
}

func TestTrimRecentCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    []CommandContext
		expected int
	}{
		{
			name:     "empty list",
			input:    nil,
			expected: 0,
		},
		{
			name:     "under limit",
			input:    make([]CommandContext, 5),
			expected: 5,
		},
		{
			name:     "at limit",
			input:    make([]CommandContext, MaxRecentCommands),
			expected: MaxRecentCommands,
		},
		{
			name:     "over limit",
			input:    make([]CommandContext, MaxRecentCommands+5),
			expected: MaxRecentCommands,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TrimRecentCommands(tt.input)
			if len(result) != tt.expected {
				t.Errorf("TrimRecentCommands() returned %d items, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestTrimRecentCommands_KeepsLatest(t *testing.T) {
	cmds := make([]CommandContext, 15)
	for i := range cmds {
		cmds[i] = CommandContext{Command: string(rune('a' + i)), ExitCode: i}
	}

	result := TrimRecentCommands(cmds)

	if len(result) != MaxRecentCommands {
		t.Fatalf("TrimRecentCommands() returned %d items, want %d", len(result), MaxRecentCommands)
	}

	// Should keep the last 10 commands (indices 5-14)
	expectedFirst := CommandContext{Command: "f", ExitCode: 5}
	if result[0] != expectedFirst {
		t.Errorf("TrimRecentCommands() first item = %+v, want %+v", result[0], expectedFirst)
	}

	expectedLast := CommandContext{Command: "o", ExitCode: 14}
	if result[len(result)-1] != expectedLast {
		t.Errorf("TrimRecentCommands() last item = %+v, want %+v", result[len(result)-1], expectedLast)
	}
}

func TestMaxRecentCommands(t *testing.T) {
	if MaxRecentCommands != 10 {
		t.Errorf("MaxRecentCommands = %d, want %d", MaxRecentCommands, 10)
	}
}
