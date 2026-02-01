package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestDetectShell_Fallback(t *testing.T) {
	tests := []struct {
		name             string
		shell            string
		claiCurrentShell string
		sessionID        string
		wantShell        string
		wantActive       bool
	}{
		{
			name:       "falls back to SHELL env var",
			shell:      "/bin/bash",
			wantShell:  "bash",
			wantActive: false,
		},
		{
			name:       "extracts shell name from full path",
			shell:      "/usr/local/bin/zsh",
			wantShell:  "zsh",
			wantActive: false,
		},
		{
			name:             "active when CLAI_CURRENT_SHELL matches SHELL",
			shell:            "/bin/zsh",
			claiCurrentShell: "zsh",
			sessionID:        "test-session",
			wantShell:        "zsh",
			wantActive:       true,
		},
		{
			name:             "not active when CLAI_CURRENT_SHELL differs from detected shell",
			shell:            "/bin/bash",
			claiCurrentShell: "zsh",
			sessionID:        "test-session",
			wantShell:        "bash",
			wantActive:       false, // CLAI_CURRENT_SHELL=zsh doesn't match detected shell=bash
		},
		{
			name:             "not active without session ID",
			shell:            "/bin/zsh",
			claiCurrentShell: "zsh",
			sessionID:        "",
			wantShell:        "zsh",
			wantActive:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			origShell := os.Getenv("SHELL")
			origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
			origSessionID := os.Getenv("CLAI_SESSION_ID")
			defer func() {
				restoreEnv("SHELL", origShell)
				restoreEnv("CLAI_CURRENT_SHELL", origClaiShell)
				restoreEnv("CLAI_SESSION_ID", origSessionID)
			}()

			// Set test env
			if tt.shell != "" {
				os.Setenv("SHELL", tt.shell)
			} else {
				os.Unsetenv("SHELL")
			}
			if tt.claiCurrentShell != "" {
				os.Setenv("CLAI_CURRENT_SHELL", tt.claiCurrentShell)
			} else {
				os.Unsetenv("CLAI_CURRENT_SHELL")
			}
			if tt.sessionID != "" {
				os.Setenv("CLAI_SESSION_ID", tt.sessionID)
			} else {
				os.Unsetenv("CLAI_SESSION_ID")
			}

			// Note: In test environment, parent process detection returns "go" not a shell,
			// so we're testing the $SHELL fallback path
			got := DetectShell()
			if got.Shell != tt.wantShell {
				t.Errorf("DetectShell().Shell = %q, want %q", got.Shell, tt.wantShell)
			}
			if got.Active != tt.wantActive {
				t.Errorf("DetectShell().Active = %v, want %v", got.Active, tt.wantActive)
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
		name       string
		shell      string // SHELL env var - DetectShell() falls back to this in tests
		wantShells []string
		wantEmpty  bool
	}{
		{
			name:       "zsh only checks zsh",
			shell:      "/bin/zsh",
			wantShells: []string{"zsh"},
		},
		{
			name:       "bash only checks bash",
			shell:      "/bin/bash",
			wantShells: []string{"bash"},
		},
		{
			name:       "fish only checks fish",
			shell:      "/bin/fish",
			wantShells: []string{"fish"},
		},
		{
			name:       "unknown shell checks all shells",
			shell:      "/bin/ksh",
			wantShells: []string{"zsh", "bash", "fish"}, // When shell unknown, check all
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and set env
			origShell := os.Getenv("SHELL")
			origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
			origSessionID := os.Getenv("CLAI_SESSION_ID")
			origHome := os.Getenv("HOME")
			defer func() {
				restoreEnv("SHELL", origShell)
				restoreEnv("CLAI_CURRENT_SHELL", origClaiShell)
				restoreEnv("CLAI_SESSION_ID", origSessionID)
				os.Setenv("HOME", origHome)
			}()

			// Set SHELL for detection (parent process detection returns "go" in tests)
			os.Setenv("SHELL", tt.shell)
			// Clear clai env vars to test RC file detection
			os.Unsetenv("CLAI_CURRENT_SHELL")
			os.Unsetenv("CLAI_SESSION_ID")
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
	origSessionID := os.Getenv("CLAI_SESSION_ID")
	origShell := os.Getenv("SHELL")
	origHome := os.Getenv("HOME")
	defer func() {
		restoreEnv("CLAI_CURRENT_SHELL", origClaiShell)
		restoreEnv("CLAI_SESSION_ID", origSessionID)
		restoreEnv("SHELL", origShell)
		os.Setenv("HOME", origHome)
	}()

	os.Unsetenv("CLAI_CURRENT_SHELL")
	os.Unsetenv("CLAI_SESSION_ID")
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
	// When SHELL is zsh, CLAI_CURRENT_SHELL=zsh, and CLAI_SESSION_ID is set,
	// should detect as active session.
	// Note: In test environment, parent process detection won't work (parent is "go"),
	// so we rely on $SHELL fallback.
	tmpDir := t.TempDir()
	home := tmpDir

	// No rc files exist
	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Save and set env
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	origSessionID := os.Getenv("CLAI_SESSION_ID")
	origShell := os.Getenv("SHELL")
	origHome := os.Getenv("HOME")
	defer func() {
		restoreEnv("CLAI_CURRENT_SHELL", origClaiShell)
		restoreEnv("CLAI_SESSION_ID", origSessionID)
		restoreEnv("SHELL", origShell)
		os.Setenv("HOME", origHome)
	}()

	// Simulate being in zsh with active clai session
	os.Setenv("SHELL", "/bin/zsh")
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

func TestCheckShellIntegrationWithPaths_InheritedSessionNotActive(t *testing.T) {
	// When in bash (detected via SHELL) but CLAI_CURRENT_SHELL=zsh (inherited from parent),
	// should NOT report active session - the clai session is not active in bash
	tmpDir := t.TempDir()
	home := tmpDir

	paths := &config.Paths{
		BaseDir: tmpDir,
	}

	// Save and set env
	origShell := os.Getenv("SHELL")
	origClaiShell := os.Getenv("CLAI_CURRENT_SHELL")
	origSessionID := os.Getenv("CLAI_SESSION_ID")
	origHome := os.Getenv("HOME")
	defer func() {
		restoreEnv("SHELL", origShell)
		restoreEnv("CLAI_CURRENT_SHELL", origClaiShell)
		restoreEnv("CLAI_SESSION_ID", origSessionID)
		os.Setenv("HOME", origHome)
	}()

	// Simulate being in bash with inherited zsh clai session
	// In tests, parent process detection returns "go", so we rely on $SHELL
	os.Setenv("SHELL", "/bin/bash")
	os.Setenv("CLAI_CURRENT_SHELL", "zsh") // Inherited from parent - doesn't match bash
	os.Setenv("CLAI_SESSION_ID", "test-session-123")
	os.Setenv("HOME", home)

	result := checkShellIntegrationWithPaths(paths)

	// Should return empty - clai is not active in bash, and no RC files exist
	if len(result) != 0 {
		t.Errorf("expected empty result when inherited session doesn't match shell, got %v", result)
	}
}

// restoreEnv restores an environment variable to its original value
func restoreEnv(key, value string) {
	if value != "" {
		os.Setenv(key, value)
	} else {
		os.Unsetenv(key)
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
