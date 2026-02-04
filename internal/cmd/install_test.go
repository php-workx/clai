package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	// Save originals
	origShell := os.Getenv("SHELL")
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	origSessionID := os.Getenv("CLAI_SESSION_ID")
	defer func() {
		restoreEnvInstall("SHELL", origShell)
		restoreEnvInstall("CLAI_CURRENT_SHELL", origClaiShell)
		restoreEnvInstall("CLAI_SESSION_ID", origSessionID)
	}()

	// Clear env vars for tests
	os.Unsetenv("CLAI_CURRENT_SHELL")
	os.Unsetenv("CLAI_SESSION_ID")

	tests := []struct {
		name            string
		shell           string
		expected        string
		expectConfident bool
	}{
		// SHELL fallback tests (not confident - parent process detection returns "go" in test)
		{"shell_zsh", "/bin/zsh", "zsh", false},
		{"shell_bash", "/bin/bash", "bash", false},
		{"shell_fish", "/bin/fish", "fish", false},
		{"shell_sh", "/bin/sh", "", false},
		{"shell_empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shell != "" {
				os.Setenv("SHELL", tt.shell)
			} else {
				os.Unsetenv("SHELL")
			}

			got := DetectShell()
			if got.Shell != tt.expected {
				t.Errorf("DetectShell() shell = %q, want %q", got.Shell, tt.expected)
			}
			if got.Confident != tt.expectConfident {
				t.Errorf("DetectShell() confident = %v, want %v", got.Confident, tt.expectConfident)
			}
		})
	}
}

// restoreEnvInstall restores an environment variable to its original value
func restoreEnvInstall(key, value string) {
	if value != "" {
		os.Setenv(key, value)
	} else {
		os.Unsetenv(key)
	}
}

func TestGetRCFiles(t *testing.T) {
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
			got := getRCFiles(tt.shell)
			if tt.contains == "" {
				if len(got) != 0 {
					t.Errorf("getRCFiles(%q) = %v, want empty", tt.shell, got)
				}
			} else {
				found := false
				for _, f := range got {
					if strings.Contains(f, tt.contains) {
						found = true
					}
					if !strings.HasPrefix(f, home) {
						t.Errorf("getRCFiles(%q) returned %q, should be in home dir", tt.shell, f)
					}
				}
				if !found {
					t.Errorf("getRCFiles(%q) = %v, should contain entry with %q", tt.shell, got, tt.contains)
				}
			}
		})
	}
}

func TestGetRCFiles_DarwinBash(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-specific test")
	}

	got := getRCFiles("bash")
	if len(got) != 2 {
		t.Fatalf("getRCFiles(bash) on macOS returned %d files, want 2 (.bash_profile and .bashrc)", len(got))
	}
	hasProfile, hasRC := false, false
	for _, f := range got {
		if strings.Contains(f, ".bash_profile") {
			hasProfile = true
		}
		if strings.Contains(f, ".bashrc") {
			hasRC = true
		}
	}
	if !hasProfile {
		t.Errorf("getRCFiles(bash) = %v, missing .bash_profile", got)
	}
	if !hasRC {
		t.Errorf("getRCFiles(bash) = %v, missing .bashrc", got)
	}
}

func TestGetHookContent(t *testing.T) {
	tests := []struct {
		shell    string
		contains string
	}{
		{"zsh", `eval "$(clai init zsh)"`},
		{"bash", `eval "$(clai init bash)"`},
		{"fish", "clai init fish | source"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			content, err := getHookContent(tt.shell)
			if err != nil {
				t.Fatalf("getHookContent(%q) error: %v", tt.shell, err)
			}
			if !strings.Contains(content, tt.contains) {
				t.Errorf("getHookContent(%q) = %q, want it to contain %q", tt.shell, content, tt.contains)
			}
		})
	}
}

func TestGetHookContent_NoRawPlaceholders(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		t.Run(shell, func(t *testing.T) {
			content, err := getHookContent(shell)
			if err != nil {
				t.Fatalf("getHookContent(%q) error: %v", shell, err)
			}
			if strings.Contains(content, "{{CLAI_SESSION_ID}}") {
				t.Errorf("getHookContent(%q) contains raw {{CLAI_SESSION_ID}} placeholder", shell)
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

func TestDetectShell_ParentProcessFallback(t *testing.T) {
	// This test verifies that when SHELL is set to unsupported shell,
	// we still try parent process detection first

	// Save and clear all detection variables
	origShell := os.Getenv("SHELL")
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	defer func() {
		restoreEnvInstall("SHELL", origShell)
		restoreEnvInstall("CLAI_CURRENT_SHELL", origClaiShell)
	}()

	// Clear all variables to force parent process detection
	os.Unsetenv("CLAI_CURRENT_SHELL")
	os.Setenv("SHELL", "/bin/sh") // Set to unsupported shell

	// The detection should either:
	// 1. Detect parent process (if running from zsh/bash/fish) - confident=true
	// 2. Fall back to SHELL=/bin/sh which is unsupported - shell="", confident=false
	detection := DetectShell()

	// We can't know for sure what the test runner's parent is, but we can verify
	// the function doesn't crash and returns sensible values
	t.Logf("DetectShell with cleared vars: shell=%q, confident=%v", detection.Shell, detection.Confident)

	// If we got a shell, it should be one of the supported ones
	if detection.Shell != "" {
		switch detection.Shell {
		case "zsh", "bash", "fish":
			// Valid
		default:
			t.Errorf("detected unsupported shell: %q", detection.Shell)
		}
	}
}
