package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	// Save original
	origShell := os.Getenv("SHELL")
	defer os.Setenv("SHELL", origShell)

	tests := []struct {
		shell    string
		expected string
	}{
		{"/bin/zsh", "zsh"},
		{"/bin/bash", "bash"},
		{"/usr/bin/zsh", "zsh"},
		{"/usr/local/bin/bash", "bash"},
		{"/bin/fish", "fish"},
		{"/usr/local/bin/fish", "fish"},
		{"/bin/sh", ""}, // sh not supported
		{"", ""},        // empty
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			os.Setenv("SHELL", tt.shell)
			got := detectShell()
			if got != tt.expected {
				t.Errorf("detectShell() with SHELL=%q = %q, want %q", tt.shell, got, tt.expected)
			}
		})
	}
}

func TestGetRCFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot get home directory")
	}

	tests := []struct {
		shell    string
		contains string
	}{
		{"zsh", ".zshrc"},
		{"bash", ".bash"},
		{"fish", "config.fish"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			got := getRCFile(tt.shell)
			if tt.contains == "" {
				if got != "" {
					t.Errorf("getRCFile(%q) = %q, want empty", tt.shell, got)
				}
			} else {
				if !strings.Contains(got, tt.contains) {
					t.Errorf("getRCFile(%q) = %q, should contain %q", tt.shell, got, tt.contains)
				}
				if !strings.HasPrefix(got, home) {
					t.Errorf("getRCFile(%q) = %q, should be in home dir", tt.shell, got)
				}
			}
		})
	}
}

func TestGetHookContent(t *testing.T) {
	tests := []string{"zsh", "bash", "fish"}

	for _, shell := range tests {
		t.Run(shell, func(t *testing.T) {
			content, err := getHookContent(shell)
			if err != nil {
				t.Fatalf("getHookContent(%q) error: %v", shell, err)
			}
			if content == "" {
				t.Errorf("getHookContent(%q) returned empty content", shell)
			}
			if len(content) < 100 {
				t.Errorf("getHookContent(%q) content too short: %d bytes", shell, len(content))
			}
		})
	}
}

func TestGetHookContent_InvalidShell(t *testing.T) {
	_, err := getHookContent("invalid")
	if err == nil {
		t.Error("getHookContent(invalid) should fail")
	}
}

func TestIsInstalled(t *testing.T) {
	// Create temp file
	tmpDir, err := os.MkdirTemp("", "clai-install-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rcFile := filepath.Join(tmpDir, ".zshrc")
	hookFile := filepath.Join(tmpDir, "hooks", "clai.zsh")

	tests := []struct {
		name      string
		content   string
		hookPath  string
		shell     string
		installed bool
	}{
		{
			name:      "empty file",
			content:   "",
			installed: false,
		},
		{
			name:      "no clai",
			content:   "export PATH=$PATH:/usr/local/bin\nalias ll='ls -la'\n",
			installed: false,
		},
		{
			name:      "with source hook",
			content:   "# My config\nsource \"" + hookFile + "\"\n",
			hookPath:  hookFile,
			installed: true,
		},
		{
			name:      "with eval init",
			content:   "# Config\neval \"$(clai init zsh)\"\n",
			shell:     "zsh",
			installed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write content
			if err := os.WriteFile(rcFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			hookPath := tt.hookPath
			if hookPath == "" {
				hookPath = hookFile
			}
			shell := tt.shell
			if shell == "" {
				shell = "zsh"
			}

			installed, _, err := isInstalled(rcFile, hookPath, shell)
			if err != nil {
				t.Fatalf("isInstalled error: %v", err)
			}
			if installed != tt.installed {
				t.Errorf("isInstalled() = %v, want %v", installed, tt.installed)
			}
		})
	}
}

func TestIsInstalled_NonExistentFile(t *testing.T) {
	installed, _, err := isInstalled("/nonexistent/file", "/hooks/clai.zsh", "zsh")
	if err != nil {
		t.Fatalf("isInstalled should not error for nonexistent file: %v", err)
	}
	if installed {
		t.Error("isInstalled should return false for nonexistent file")
	}
}
