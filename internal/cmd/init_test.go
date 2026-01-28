package cmd

import (
	"strings"
	"testing"
)

func TestRunInit_Zsh(t *testing.T) {
	// Capture stdout by reading the embedded file directly
	content, err := shellScripts.ReadFile("shell/zsh/ai-terminal.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	// Verify essential Zsh-specific content
	requiredContent := []string{
		"ai-terminal.zsh",
		"AI_TERMINAL_AUTO_DIAGNOSE",
		"AI_TERMINAL_CACHE",
		"zle -N",
		"bindkey",
		"add-zsh-hook",
		"preexec",
		"precmd",
		"ai-terminal diagnose",
		"ai-terminal extract",
		"ai-terminal ask",
		"RPROMPT",
		"pipestatus",
		"ai-fix",
		"ai-toggle",
		"run()",
	}

	for _, req := range requiredContent {
		if !strings.Contains(output, req) {
			t.Errorf("zsh script missing %q", req)
		}
	}

	if len(output) < 1000 {
		t.Errorf("zsh script too short (%d bytes)", len(output))
	}
}

func TestRunInit_Bash(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/ai-terminal.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}

	output := string(content)

	requiredContent := []string{
		"ai-terminal.bash",
		"AI_TERMINAL_AUTO_DIAGNOSE",
		"AI_TERMINAL_CACHE",
		"PROMPT_COMMAND",
		"DEBUG",
		"trap",
		"ai-terminal diagnose",
		"ai-terminal extract",
		"ai-terminal ask",
		"PIPESTATUS",
		"history",
		"accept",
		"ai-fix",
		"ai-toggle",
		"run()",
	}

	for _, req := range requiredContent {
		if !strings.Contains(output, req) {
			t.Errorf("bash script missing %q", req)
		}
	}

	if len(output) < 1000 {
		t.Errorf("bash script too short (%d bytes)", len(output))
	}
}

func TestRunInit_Fish(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/ai-terminal.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}

	output := string(content)

	requiredContent := []string{
		"ai-terminal.fish",
		"AI_TERMINAL_AUTO_DIAGNOSE",
		"AI_TERMINAL_CACHE",
		"set -gx",
		"function",
		"--on-event",
		"fish_preexec",
		"fish_postexec",
		"fish_right_prompt",
		"commandline",
		"ai-terminal diagnose",
		"ai-terminal extract",
		"ai-terminal ask",
		"status is-interactive",
		"pipestatus",
		"function ai-fix",
		"function ai-toggle",
		"function run",
	}

	for _, req := range requiredContent {
		if !strings.Contains(output, req) {
			t.Errorf("fish script missing %q", req)
		}
	}

	if len(output) < 1000 {
		t.Errorf("fish script too short (%d bytes)", len(output))
	}
}

func TestRunInit_UnsupportedShell(t *testing.T) {
	err := runInit(initCmd, []string{"powershell"})
	if err == nil {
		t.Error("init powershell should have failed")
	}

	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("Error should mention unsupported shell, got: %v", err)
	}
}

func TestShellScripts_AllHaveCommonFeatures(t *testing.T) {
	shells := map[string]string{
		"zsh":  "shell/zsh/ai-terminal.zsh",
		"bash": "shell/bash/ai-terminal.bash",
		"fish": "shell/fish/ai-terminal.fish",
	}

	commonFeatures := []string{
		"AI_TERMINAL_AUTO_DIAGNOSE",
		"AI_TERMINAL_AUTO_EXTRACT",
		"AI_TERMINAL_CACHE",
	}

	for name, path := range shells {
		t.Run(name, func(t *testing.T) {
			content, err := shellScripts.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s script: %v", name, err)
			}

			output := string(content)

			for _, feature := range commonFeatures {
				if !strings.Contains(output, feature) {
					t.Errorf("%s script missing common feature: %q", name, feature)
				}
			}
		})
	}
}

func TestShellScripts_Embedded(t *testing.T) {
	// Verify all shell scripts are properly embedded
	shells := []string{
		"shell/zsh/ai-terminal.zsh",
		"shell/bash/ai-terminal.bash",
		"shell/fish/ai-terminal.fish",
	}

	for _, path := range shells {
		t.Run(path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read embedded file %s: %v", path, err)
			}

			if len(content) == 0 {
				t.Errorf("Embedded file %s is empty", path)
			}
		})
	}
}
