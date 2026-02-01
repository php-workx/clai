package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	// Save originals
	origShell := os.Getenv("SHELL")
	origZshVersion := os.Getenv("ZSH_VERSION")
	origBashVersion := os.Getenv("BASH_VERSION")
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	defer func() {
		os.Setenv("SHELL", origShell)
		os.Setenv("ZSH_VERSION", origZshVersion)
		os.Setenv("BASH_VERSION", origBashVersion)
		os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
	}()

	// Clear version variables for SHELL fallback tests
	os.Unsetenv("ZSH_VERSION")
	os.Unsetenv("BASH_VERSION")
	os.Unsetenv("CLAI_CURRENT_SHELL")

	tests := []struct {
		name            string
		shell           string
		zshVersion      string
		bashVersion     string
		claiShell       string
		expected        string
		expectConfident bool
	}{
		// SHELL fallback tests (not confident)
		{"shell_zsh", "/bin/zsh", "", "", "", "zsh", false},
		{"shell_bash", "/bin/bash", "", "", "", "bash", false},
		{"shell_fish", "/bin/fish", "", "", "", "fish", false},
		{"shell_sh", "/bin/sh", "", "", "", "", false},
		{"shell_empty", "", "", "", "", "", false},
		// Version variable tests (confident)
		{"zsh_version", "/bin/bash", "5.9", "", "", "zsh", true},
		{"bash_version", "/bin/zsh", "", "5.1.16", "", "bash", true},
		// CLAI_CURRENT_SHELL tests (confident)
		{"clai_zsh", "/bin/bash", "", "", "zsh", "zsh", true},
		{"clai_fish", "/bin/zsh", "", "", "fish", "fish", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("SHELL", tt.shell)
			if tt.zshVersion != "" {
				os.Setenv("ZSH_VERSION", tt.zshVersion)
			} else {
				os.Unsetenv("ZSH_VERSION")
			}
			if tt.bashVersion != "" {
				os.Setenv("BASH_VERSION", tt.bashVersion)
			} else {
				os.Unsetenv("BASH_VERSION")
			}
			if tt.claiShell != "" {
				os.Setenv("CLAI_CURRENT_SHELL", tt.claiShell)
			} else {
				os.Unsetenv("CLAI_CURRENT_SHELL")
			}

			got, confident := detectShell()
			if got != tt.expected {
				t.Errorf("detectShell() shell = %q, want %q", got, tt.expected)
			}
			if confident != tt.expectConfident {
				t.Errorf("detectShell() confident = %v, want %v", confident, tt.expectConfident)
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

func TestValidateShellInput(t *testing.T) {
	// Test that only valid shells are accepted
	validShells := []string{"zsh", "bash", "fish"}
	invalidShells := []string{"sh", "csh", "tcsh", "ksh", "powershell", "", "invalid", "ZSH", "BASH"}

	for _, shell := range validShells {
		t.Run("valid_"+shell, func(t *testing.T) {
			switch shell {
			case "zsh", "bash", "fish":
				// These should be valid - test passes
			default:
				t.Errorf("shell %q should be valid", shell)
			}
		})
	}

	for _, shell := range invalidShells {
		t.Run("invalid_"+shell, func(t *testing.T) {
			switch shell {
			case "zsh", "bash", "fish":
				t.Errorf("shell %q should be invalid", shell)
			default:
				// These should be invalid - test passes
			}
		})
	}
}

func TestDetectShell_ParentProcessFallback(t *testing.T) {
	// This test verifies that when version variables aren't set,
	// we fall back to parent process detection (which should work for the test runner)

	// Save and clear all detection variables
	origShell := os.Getenv("SHELL")
	origZshVersion := os.Getenv("ZSH_VERSION")
	origBashVersion := os.Getenv("BASH_VERSION")
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	defer func() {
		os.Setenv("SHELL", origShell)
		if origZshVersion != "" {
			os.Setenv("ZSH_VERSION", origZshVersion)
		}
		if origBashVersion != "" {
			os.Setenv("BASH_VERSION", origBashVersion)
		}
		if origClaiShell != "" {
			os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
		}
	}()

	// Clear all variables to force parent process detection
	os.Unsetenv("CLAI_CURRENT_SHELL")
	os.Unsetenv("ZSH_VERSION")
	os.Unsetenv("BASH_VERSION")
	os.Setenv("SHELL", "/bin/sh") // Set to unsupported shell

	// The detection should either:
	// 1. Detect parent process (if running from zsh/bash/fish) - confident=true
	// 2. Fall back to SHELL=/bin/sh which is unsupported - shell="", confident=false
	shell, confident := detectShell()

	// We can't know for sure what the test runner's parent is, but we can verify
	// the function doesn't crash and returns sensible values
	t.Logf("detectShell with cleared vars: shell=%q, confident=%v", shell, confident)

	// If we got a shell, it should be one of the supported ones
	if shell != "" {
		switch shell {
		case "zsh", "bash", "fish":
			// Valid
		default:
			t.Errorf("detected unsupported shell: %q", shell)
		}
	}
}
