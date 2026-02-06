package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runger/clai/internal/config"
)

func TestDaemonStartCmd_AlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("CLAI_HOME", tempDir)

	paths := config.DefaultPaths()
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories error: %v", err)
	}

	pidPath := filepath.Join(paths.BaseDir, "clai.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		t.Fatalf("WriteFile pid error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := daemonStartCmd.RunE(daemonStartCmd, nil); err != nil {
			t.Fatalf("daemon start error: %v", err)
		}
	})

	if !strings.Contains(output, "already running") {
		t.Fatalf("expected already running output, got: %s", output)
	}
}
