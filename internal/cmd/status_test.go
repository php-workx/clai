package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestDetectCurrentShell(t *testing.T) {
	tests := []struct {
		name             string
		claiCurrentShell string
		shell            string
		want             string
	}{
		{
			name:             "CLAI_CURRENT_SHELL takes precedence",
			claiCurrentShell: "fish",
			shell:            "/bin/zsh",
			want:             "fish",
		},
		{
			name:             "falls back to SHELL when CLAI_CURRENT_SHELL unset",
			claiCurrentShell: "",
			shell:            "/bin/bash",
			want:             "bash",
		},
		{
			name:             "extracts shell name from full path",
			claiCurrentShell: "",
			shell:            "/usr/local/bin/zsh",
			want:             "zsh",
		},
		{
			name:             "returns empty when both unset",
			claiCurrentShell: "",
			shell:            "",
			want:             "",
		},
		{
			name:             "CLAI_CURRENT_SHELL is already just the name",
			claiCurrentShell: "zsh",
			shell:            "/bin/bash",
			want:             "zsh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
			origShell := os.Getenv("SHELL")
			defer func() {
				os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
				os.Setenv("SHELL", origShell)
			}()

			// Set test env
			if tt.claiCurrentShell != "" {
				os.Setenv("CLAI_CURRENT_SHELL", tt.claiCurrentShell)
			} else {
				os.Unsetenv("CLAI_CURRENT_SHELL")
			}
			if tt.shell != "" {
				os.Setenv("SHELL", tt.shell)
			} else {
				os.Unsetenv("SHELL")
			}

			got := detectCurrentShell()
			if got != tt.want {
				t.Errorf("detectCurrentShell() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckShellIntegrationWithPaths_ShellSpecific(t *testing.T) {
	// Create a temp directory structure with mock rc files
	tmpDir := t.TempDir()
	home := tmpDir

	// Create mock rc files with clai init lines
	zshrc := filepath.Join(home, ".zshrc")
	bashrc := filepath.Join(home, ".bashrc")
	fishConfig := filepath.Join(home, ".config", "fish")

	// Create directories
	os.MkdirAll(fishConfig, 0755)

	// Write mock rc files with clai init
	os.WriteFile(zshrc, []byte(`# zshrc
eval "$(clai init zsh)"
`), 0644)
	os.WriteFile(bashrc, []byte(`# bashrc
eval "$(clai init bash)"
`), 0644)
	os.WriteFile(filepath.Join(fishConfig, "config.fish"), []byte(`# config.fish
clai init fish | source
`), 0644)

	// Create mock paths
	hooksDir := filepath.Join(tmpDir, "hooks")
	os.MkdirAll(hooksDir, 0755)
	os.WriteFile(filepath.Join(hooksDir, "clai.zsh"), []byte("# zsh hooks"), 0644)
	os.WriteFile(filepath.Join(hooksDir, "clai.bash"), []byte("# bash hooks"), 0644)
	os.WriteFile(filepath.Join(hooksDir, "clai.fish"), []byte("# fish hooks"), 0644)

	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	tests := []struct {
		name             string
		claiCurrentShell string
		wantShells       []string
		wantEmpty        bool
	}{
		{
			name:             "zsh only checks zsh",
			claiCurrentShell: "zsh",
			wantShells:       []string{"zsh"},
		},
		{
			name:             "bash only checks bash",
			claiCurrentShell: "bash",
			wantShells:       []string{"bash"},
		},
		{
			name:             "fish only checks fish",
			claiCurrentShell: "fish",
			wantShells:       []string{"fish"},
		},
		{
			name:             "unknown shell returns empty",
			claiCurrentShell: "ksh",
			wantEmpty:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and set env
			origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
			origHome := os.Getenv("HOME")
			defer func() {
				os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
				os.Setenv("HOME", origHome)
			}()

			os.Setenv("CLAI_CURRENT_SHELL", tt.claiCurrentShell)
			os.Setenv("HOME", home)

			result := checkShellIntegrationWithPaths(paths)

			if tt.wantEmpty {
				if len(result) != 0 {
					t.Errorf("expected empty result, got %v", result)
				}
				return
			}

			// Check that only the expected shell(s) are in the result
			for _, wantShell := range tt.wantShells {
				found := false
				for _, r := range result {
					if contains(r, wantShell) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %q in result, got %v", wantShell, result)
				}
			}

			// Check that other shells are NOT in the result
			allShells := []string{"zsh", "bash", "fish"}
			for _, shell := range allShells {
				isWanted := false
				for _, w := range tt.wantShells {
					if w == shell {
						isWanted = true
						break
					}
				}
				if !isWanted {
					for _, r := range result {
						if contains(r, shell) {
							t.Errorf("did not expect %q in result, got %v", shell, result)
						}
					}
				}
			}
		})
	}
}

func TestCheckShellIntegrationWithPaths_NoCurrentShell(t *testing.T) {
	// When CLAI_CURRENT_SHELL and SHELL are both unset, should check all shells
	tmpDir := t.TempDir()
	home := tmpDir

	// Create zshrc with clai init
	zshrc := filepath.Join(home, ".zshrc")
	os.WriteFile(zshrc, []byte(`eval "$(clai init zsh)"`), 0644)

	// Create mock paths
	hooksDir := filepath.Join(tmpDir, "hooks")
	os.MkdirAll(hooksDir, 0755)
	os.WriteFile(filepath.Join(hooksDir, "clai.zsh"), []byte("# zsh hooks"), 0644)

	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Save and unset env
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	origShell := os.Getenv("SHELL")
	origHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
		os.Setenv("SHELL", origShell)
		os.Setenv("HOME", origHome)
	}()

	os.Unsetenv("CLAI_CURRENT_SHELL")
	os.Unsetenv("SHELL")
	os.Setenv("HOME", home)

	result := checkShellIntegrationWithPaths(paths)

	// Should find zsh since we're checking all shells
	found := false
	for _, r := range result {
		if contains(r, "zsh") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find zsh when checking all shells, got %v", result)
	}
}

func TestCheckShellIntegrationWithPaths_ActiveSession(t *testing.T) {
	// When CLAI_CURRENT_SHELL and CLAI_SESSION_ID are set, should detect as active session
	// without checking RC files (used when running via eval "$(clai init zsh)")
	tmpDir := t.TempDir()
	home := tmpDir

	// No rc files exist
	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Save and set env
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	origSessionID := os.Getenv("CLAI_SESSION_ID")
	origHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
		if origSessionID != "" {
			os.Setenv("CLAI_SESSION_ID", origSessionID)
		} else {
			os.Unsetenv("CLAI_SESSION_ID")
		}
		os.Setenv("HOME", origHome)
	}()

	os.Setenv("CLAI_CURRENT_SHELL", "zsh")
	os.Setenv("CLAI_SESSION_ID", "test-session-123")
	os.Setenv("HOME", home)

	result := checkShellIntegrationWithPaths(paths)

	// Should find "zsh (active session)"
	if len(result) != 1 {
		t.Errorf("expected 1 result for active session, got %v", result)
	}
	if len(result) > 0 && result[0] != "zsh (active session)" {
		t.Errorf("expected 'zsh (active session)', got %q", result[0])
	}
}

func TestCheckShellIntegrationWithPaths_SessionIDWithoutShell(t *testing.T) {
	// When only CLAI_SESSION_ID is set (no CLAI_CURRENT_SHELL), should check RC files
	tmpDir := t.TempDir()
	home := tmpDir

	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Save and set env
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	origSessionID := os.Getenv("CLAI_SESSION_ID")
	origHome := os.Getenv("HOME")
	defer func() {
		if origClaiShell != "" {
			os.Setenv("CLAI_CURRENT_SHELL", origClaiShell)
		} else {
			os.Unsetenv("CLAI_CURRENT_SHELL")
		}
		if origSessionID != "" {
			os.Setenv("CLAI_SESSION_ID", origSessionID)
		} else {
			os.Unsetenv("CLAI_SESSION_ID")
		}
		os.Setenv("HOME", origHome)
	}()

	os.Unsetenv("CLAI_CURRENT_SHELL")
	os.Setenv("CLAI_SESSION_ID", "test-session-123")
	os.Setenv("HOME", home)

	result := checkShellIntegrationWithPaths(paths)

	// Should return empty since no RC files and no CLAI_CURRENT_SHELL
	if len(result) != 0 {
		t.Errorf("expected empty result when CLAI_CURRENT_SHELL unset, got %v", result)
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}
