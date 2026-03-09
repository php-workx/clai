package cmd

import (
	"strings"
	"testing"
)

func TestShellScripts_RecursionGuard_Bash(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "_clai_log_command_start") {
		t.Fatalf("bash script missing log start hook")
	}
	if !strings.Contains(text, "_ai_*|_clai_*|_AI_*|_CLAI_*") {
		t.Fatalf("bash script missing recursion guard for internal clai functions")
	}
}
