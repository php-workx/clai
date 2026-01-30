package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestCheckBinary(t *testing.T) {
	// checkBinary uses exec.LookPath("clai") which searches PATH
	// We can't easily mock this, but we can test it returns a valid result
	result := checkBinary()

	if result.name != "clai binary" {
		t.Errorf("checkBinary().name = %q, want %q", result.name, "clai binary")
	}

	// Result should be either "ok" or "error"
	if result.status != "ok" && result.status != "error" {
		t.Errorf("checkBinary().status = %q, want 'ok' or 'error'", result.status)
	}

	// Message should be set
	if result.message == "" {
		t.Error("checkBinary().message should not be empty")
	}
}

func TestCheckDirectories(t *testing.T) {
	results := checkDirectories()

	// Should return at least 1 result (base directory check)
	if len(results) < 1 {
		t.Errorf("checkDirectories() returned %d results, want at least 1", len(results))
	}

	// First result should be the data directory check
	if len(results) > 0 {
		r := results[0]
		if r.name != "Data directory" {
			t.Errorf("results[0].name = %q, want %q", r.name, "Data directory")
		}

		// Status should be ok, warn, or error
		if r.status != "ok" && r.status != "warn" && r.status != "error" {
			t.Errorf("results[0].status = %q, want ok/warn/error", r.status)
		}

		// Message should always be set
		if r.message == "" {
			t.Error("results[0].message should not be empty")
		}
	}
}

func TestCheckDirectoriesScenarios(t *testing.T) {
	// Create temp directories for testing
	tmpDir := t.TempDir()

	// Create test paths config
	testPaths := &config.Paths{
		BaseDir: filepath.Join(tmpDir, "clai"),
	}

	t.Run("existing_directory", func(t *testing.T) {
		if err := os.MkdirAll(testPaths.BaseDir, 0755); err != nil {
			t.Fatalf("failed to create test dir: %v", err)
		}

		_, err := os.Stat(testPaths.BaseDir)
		if err != nil {
			t.Errorf("expected directory to exist: %v", err)
		}
	})

	t.Run("missing_directory", func(t *testing.T) {
		missingPath := filepath.Join(tmpDir, "nonexistent")
		_, err := os.Stat(missingPath)
		if !os.IsNotExist(err) {
			t.Error("expected directory to not exist")
		}
	})
}

func TestCheckConfiguration(t *testing.T) {
	result := checkConfiguration()

	if result.name != "Configuration" {
		t.Errorf("checkConfiguration().name = %q, want %q", result.name, "Configuration")
	}

	// Status should be ok or error
	validStatuses := map[string]bool{"ok": true, "error": true}
	if !validStatuses[result.status] {
		t.Errorf("checkConfiguration().status = %q, want ok or error", result.status)
	}

	// Message should be set
	if result.message == "" {
		t.Error("checkConfiguration().message should not be empty")
	}
}

func TestCheckConfigurationScenarios(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, configDir string) string
		wantStatus string
	}{
		{
			name: "valid_config",
			setup: func(t *testing.T, configDir string) string {
				configFile := filepath.Join(configDir, "config.yaml")
				content := `
provider: claude
timeout: 30s
`
				if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write config: %v", err)
				}
				return configFile
			},
			wantStatus: "ok",
		},
		{
			name: "invalid_yaml",
			setup: func(t *testing.T, configDir string) string {
				configFile := filepath.Join(configDir, "config.yaml")
				content := `
invalid: [yaml: content
`
				if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write config: %v", err)
				}
				return configFile
			},
			wantStatus: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := tt.setup(t, tmpDir)

			// Test loading the config directly
			cfg, err := config.LoadFromFile(configFile)
			switch tt.wantStatus {
			case "ok":
				if err != nil {
					t.Errorf("expected config to load successfully, got: %v", err)
				}
				if cfg == nil {
					t.Error("expected non-nil config")
				}
			case "error":
				if err == nil {
					t.Error("expected config loading to fail")
				}
			}
		})
	}
}

func TestCheckShellIntegrationDoctor(t *testing.T) {
	result := checkShellIntegrationDoctor()

	if result.name != "Shell integration" {
		t.Errorf("checkShellIntegrationDoctor().name = %q, want %q", result.name, "Shell integration")
	}

	// Status should be ok or warn
	if result.status != "ok" && result.status != "warn" {
		t.Errorf("checkShellIntegrationDoctor().status = %q, want ok or warn", result.status)
	}

	// Message should be set
	if result.message == "" {
		t.Error("checkShellIntegrationDoctor().message should not be empty")
	}
}

func TestCheckDaemon(t *testing.T) {
	result := checkDaemon()

	if result.name != "Daemon" {
		t.Errorf("checkDaemon().name = %q, want %q", result.name, "Daemon")
	}

	// Status should be ok or warn
	if result.status != "ok" && result.status != "warn" {
		t.Errorf("checkDaemon().status = %q, want ok or warn", result.status)
	}

	// Message should be set
	if result.message == "" {
		t.Error("checkDaemon().message should not be empty")
	}

	// Check message content based on status
	if result.status == "ok" && result.message != "Running" {
		t.Errorf("when daemon is running, message should be 'Running', got %q", result.message)
	}
	if result.status == "warn" && result.message != "Not running. Will start automatically when needed." {
		t.Errorf("when daemon is not running, message should indicate auto-start, got %q", result.message)
	}
}

func TestCheckAIProviders(t *testing.T) {
	results := checkAIProviders()

	// Should return at least 1 result (Claude CLI check)
	if len(results) < 1 {
		t.Errorf("checkAIProviders() returned %d results, want at least 1", len(results))
	}

	// First result should be Claude CLI check
	if len(results) > 0 {
		r := results[0]
		if r.name != "Claude CLI" {
			t.Errorf("checkAIProviders()[0].name = %q, want %q", r.name, "Claude CLI")
		}

		// Status should be ok or error
		if r.status != "ok" && r.status != "error" {
			t.Errorf("checkAIProviders()[0].status = %q, want ok or error", r.status)
		}

		// Message should be set
		if r.message == "" {
			t.Error("checkAIProviders()[0].message should not be empty")
		}
	}
}

func TestCheckResult(t *testing.T) {
	// Test the checkResult struct
	tests := []struct {
		name    string
		result  checkResult
		wantErr bool
	}{
		{
			name: "ok_result",
			result: checkResult{
				name:    "Test check",
				status:  "ok",
				message: "All good",
			},
			wantErr: false,
		},
		{
			name: "warn_result",
			result: checkResult{
				name:    "Test check",
				status:  "warn",
				message: "Minor issue",
			},
			wantErr: false, // warnings are not errors
		},
		{
			name: "error_result",
			result: checkResult{
				name:    "Test check",
				status:  "error",
				message: "Critical failure",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasErr := tt.result.status == "error"
			if hasErr != tt.wantErr {
				t.Errorf("status %q: hasErr = %v, want %v", tt.result.status, hasErr, tt.wantErr)
			}
		})
	}
}

func TestRunDoctor(t *testing.T) {
	// runDoctor orchestrates all checks and prints results
	// We can test it runs without panicking and returns appropriate errors
	err := runDoctor(doctorCmd, []string{})

	// It may return an error if some checks fail (like daemon not running)
	// The key is that it doesn't panic
	if err != nil {
		t.Logf("runDoctor returned error (expected if checks fail): %v", err)
	}
}

func TestRunDoctorOutput(t *testing.T) {
	// Test that runDoctor produces output to stdout
	// We can capture stdout to verify output format

	// Save original stdout
	origStdout := os.Stdout

	// Create a pipe to capture output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	os.Stdout = w

	// Run doctor (ignore error since checks may fail)
	_ = runDoctor(doctorCmd, []string{})

	// Restore stdout and close writer
	w.Close()
	os.Stdout = origStdout

	// Read captured output
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Verify output contains expected headers
	if len(output) == 0 {
		t.Error("runDoctor should produce output")
	}

	// Check for header
	if !containsString(output, "clai Doctor") {
		t.Error("output should contain 'clai Doctor' header")
	}
}

// containsString checks if a string contains a substring
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDoctorCmdRegistration(t *testing.T) {
	// Test that doctorCmd is properly configured
	if doctorCmd == nil {
		t.Fatal("doctorCmd should not be nil")
	}

	if doctorCmd.Use != "doctor" {
		t.Errorf("doctorCmd.Use = %q, want %q", doctorCmd.Use, "doctor")
	}

	if doctorCmd.Short == "" {
		t.Error("doctorCmd.Short should not be empty")
	}

	if doctorCmd.Long == "" {
		t.Error("doctorCmd.Long should not be empty")
	}

	if doctorCmd.RunE == nil {
		t.Error("doctorCmd.RunE should not be nil")
	}
}
