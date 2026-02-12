package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckNotRoot_NotRoot(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("root check not applicable on Windows")
	}

	// Test process should not be running as root in CI
	if os.Geteuid() == 0 {
		t.Skip("test is running as root - skipping non-root check")
	}

	err := CheckNotRoot()
	if err != nil {
		t.Errorf("expected no error for non-root user, got: %v", err)
	}
}

func TestCheckNotRoot_ReturnsCorrectError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("root check not applicable on Windows")
	}

	// We can only test the error type, not the actual root check
	// since tests shouldn't run as root
	if os.Geteuid() == 0 {
		err := CheckNotRoot()
		if !errors.Is(err, ErrRunningAsRoot) {
			t.Errorf("expected ErrRunningAsRoot, got: %v", err)
		}
	}
}

func TestValidateDirectoryPermissions_Secure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	secureDir := filepath.Join(tmpDir, "secure")

	err := os.Mkdir(secureDir, 0700)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	err = ValidateDirectoryPermissions(secureDir)
	if err != nil {
		t.Errorf("expected no error for 0700 directory, got: %v", err)
	}
}

func TestValidateDirectoryPermissions_WorldReadable(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	insecureDir := filepath.Join(tmpDir, "insecure")

	err := os.Mkdir(insecureDir, 0755)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	err = ValidateDirectoryPermissions(insecureDir)
	if err == nil {
		t.Error("expected error for world-readable directory")
	}

	if !errors.Is(err, ErrInsecureDirectory) {
		t.Errorf("expected ErrInsecureDirectory, got: %v", err)
	}
}

func TestValidateDirectoryPermissions_GroupWritable(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	insecureDir := filepath.Join(tmpDir, "group-writable")

	err := os.Mkdir(insecureDir, 0770)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	err = ValidateDirectoryPermissions(insecureDir)
	if err == nil {
		t.Error("expected error for group-writable directory")
	}

	if !errors.Is(err, ErrInsecureDirectory) {
		t.Errorf("expected ErrInsecureDirectory, got: %v", err)
	}
}

func TestValidateDirectoryPermissions_NonExistent(t *testing.T) {
	t.Parallel()

	err := ValidateDirectoryPermissions("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Errorf("expected nil for non-existent directory (will be created later), got: %v", err)
	}
}

func TestValidateDirectoryPermissions_NotADirectory(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "not-a-dir")

	err := os.WriteFile(filePath, []byte("test"), 0600)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	err = ValidateDirectoryPermissions(filePath)
	if err == nil {
		t.Error("expected error for non-directory path")
	}
}

func TestEnsureSecureDirectory_Creates(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	secureDir := filepath.Join(tmpDir, "new-dir")

	err := EnsureSecureDirectory(secureDir)
	if err != nil {
		t.Fatalf("EnsureSecureDirectory failed: %v", err)
	}

	info, err := os.Stat(secureDir)
	if err != nil {
		t.Fatalf("directory should exist: %v", err)
	}

	if !info.IsDir() {
		t.Error("should be a directory")
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("expected permissions 0700, got %o", info.Mode().Perm())
	}
}

func TestEnsureSecureDirectory_FixesPermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	insecureDir := filepath.Join(tmpDir, "insecure-dir")

	// Create with insecure permissions
	err := os.Mkdir(insecureDir, 0755)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// EnsureSecureDirectory should fix permissions
	err = EnsureSecureDirectory(insecureDir)
	if err != nil {
		t.Fatalf("EnsureSecureDirectory failed: %v", err)
	}

	info, err := os.Stat(insecureDir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("expected permissions 0700 after fix, got %o", info.Mode().Perm())
	}
}

func TestEnsureSecureDirectory_AlreadySecure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	secureDir := filepath.Join(tmpDir, "already-secure")

	err := os.Mkdir(secureDir, 0700)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Should succeed without needing to fix anything
	err = EnsureSecureDirectory(secureDir)
	if err != nil {
		t.Fatalf("EnsureSecureDirectory failed: %v", err)
	}
}

func TestEnsureSecureDirectory_NotADirectory(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "not-a-dir")

	err := os.WriteFile(filePath, []byte("test"), 0600)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	err = EnsureSecureDirectory(filePath)
	if err == nil {
		t.Error("expected error when path is a file, not directory")
	}
}

func TestEnsureSecureDirectory_CreatesNested(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")

	err := EnsureSecureDirectory(nestedDir)
	if err != nil {
		t.Fatalf("EnsureSecureDirectory failed: %v", err)
	}

	info, err := os.Stat(nestedDir)
	if err != nil {
		t.Fatalf("directory should exist: %v", err)
	}

	if !info.IsDir() {
		t.Error("should be a directory")
	}
}

func TestValidateDirectoryPermissions_ModeTable(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}

	tests := []struct {
		name    string
		mode    os.FileMode
		wantErr bool
	}{
		{"0700 (secure)", 0700, false},
		{"0600 (missing owner execute)", 0600, true},
		{"0755 (group+world read/exec)", 0755, true},
		{"0750 (group read/exec)", 0750, true},
		{"0701 (world exec)", 0701, true},
		{"0710 (group exec)", 0710, true},
		{"0777 (all access)", 0777, true},
		{"0770 (group all)", 0770, true},
		{"0707 (world all)", 0707, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testDir := filepath.Join(tmpDir, "test")

			err := os.Mkdir(testDir, tt.mode)
			if err != nil {
				t.Fatalf("failed to create directory: %v", err)
			}

			err = ValidateDirectoryPermissions(testDir)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for mode %o, got nil", tt.mode)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error for mode %o, got: %v", tt.mode, err)
			}
		})
	}
}
