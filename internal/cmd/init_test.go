package cmd

import (
	"strings"
	"testing"
)

func TestRunInit_Zsh(t *testing.T) {
	// Capture stdout by reading the embedded file directly
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	// Verify essential Zsh-specific content
	requiredContent := []string{
		"clai.zsh",
		"CLAI_CACHE",
		"zle -N",
		"bindkey",
		"add-zsh-hook",
		"precmd",
		"clai diagnose",
		"clai extract",
		"clai ask",
		"clai suggest",
		"POSTDISPLAY",
		"pipestatus",
		"ai-fix",
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
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}

	output := string(content)

	requiredContent := []string{
		"clai.bash",
		"CLAI_CACHE",
		"PROMPT_COMMAND",
		"DEBUG",
		"trap",
		"clai diagnose",
		"clai extract",
		"clai ask",
		"PIPESTATUS",
		"history",
		"accept",
		"ai-fix",
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
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}

	output := string(content)

	requiredContent := []string{
		"clai.fish",
		"CLAI_CACHE",
		"set -gx",
		"function",
		"fish_right_prompt",
		"commandline",
		"clai diagnose",
		"clai extract",
		"clai ask",
		"status is-interactive",
		"pipestatus",
		"function ai-fix",
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
		"zsh":  "shell/zsh/clai.zsh",
		"bash": "shell/bash/clai.bash",
		"fish": "shell/fish/clai.fish",
	}

	commonFeatures := []string{
		"CLAI_AUTO_EXTRACT",
		"CLAI_CACHE",
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
		"shell/zsh/clai.zsh",
		"shell/bash/clai.bash",
		"shell/fish/clai.fish",
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
