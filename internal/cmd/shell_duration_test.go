package cmd

import (
	"strings"
	"testing"
)

func TestShellScripts_DurationCapture_Bash(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "date +%s%N") {
		t.Fatalf("bash script missing high-precision timestamp (date +%%s%%N)")
	}
	if !strings.Contains(text, "1000000") {
		t.Fatalf("bash script missing millisecond conversion")
	}
	if !strings.Contains(text, "--duration=") {
		t.Fatalf("bash script missing duration logging")
	}
}

func TestShellScripts_DurationCapture_Zsh(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	text := string(content)

	// zsh uses seconds * 1000 for millisecond precision (macOS date lacks %N)
	if !strings.Contains(text, "date +%s") {
		t.Fatalf("zsh script missing timestamp capture (date +%%s)")
	}
	if !strings.Contains(text, "* 1000") {
		t.Fatalf("zsh script missing millisecond conversion")
	}
	if !strings.Contains(text, "--duration=") {
		t.Fatalf("zsh script missing duration logging")
	}
}

func TestShellScripts_DurationCapture_Fish(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "command date +%s%N") {
		t.Fatalf("fish script missing high-precision timestamp (date +%%s%%N)")
	}
	if !strings.Contains(text, "/ 1000000") {
		t.Fatalf("fish script missing millisecond conversion")
	}
	if !strings.Contains(text, "--duration=") {
		t.Fatalf("fish script missing duration logging")
	}
}
