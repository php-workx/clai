package workflow

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func longRunningCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "ping", "-n", "60", "127.0.0.1")
	}
	return exec.Command("sleep", "60")
}

func TestNewProcessController(t *testing.T) {
	pc := NewProcessController()
	if pc == nil {
		t.Fatal("NewProcessController returned nil")
	}
}

func TestStartConfiguresProcessGroup(t *testing.T) {
	pc := NewProcessController()
	cmd := longRunningCmd()
	if err := pc.Start(cmd); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = pc.Kill(cmd) }()

	if cmd.Process == nil {
		t.Fatal("process should be running after Start")
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should be set after Start")
	}
}

func TestWaitNormalCompletion(t *testing.T) {
	pc := NewProcessController()

	// Use a command that exits quickly.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "echo", "hello")
	} else {
		cmd = exec.Command("echo", "hello")
	}

	if err := pc.Start(cmd); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	ctx := context.Background()
	err := pc.Wait(ctx, cmd, DefaultGracePeriod)
	if err != nil {
		t.Fatalf("Wait returned unexpected error: %v", err)
	}
}

func TestWaitContextCancellation(t *testing.T) {
	pc := NewProcessController()
	cmd := longRunningCmd()

	if err := pc.Start(cmd); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel context after a short delay.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := pc.Wait(ctx, cmd, 1*time.Second)
	elapsed := time.Since(start)

	// The process should have been interrupted and exited.
	// err will be non-nil because sleep was killed.
	if err == nil {
		t.Fatal("Wait should return an error when process is killed")
	}

	// Should complete well before the sleep would have finished (60s).
	if elapsed > 10*time.Second {
		t.Fatalf("Wait took too long: %v (expected < 10s)", elapsed)
	}
}

func TestInterruptWithoutStart(t *testing.T) {
	pc := NewProcessController()
	cmd := longRunningCmd()
	// Don't start the command.
	err := pc.Interrupt(cmd)
	if err == nil {
		t.Fatal("Interrupt on unstarted process should return an error")
	}
}

func TestKillWithoutStart(t *testing.T) {
	pc := NewProcessController()
	cmd := longRunningCmd()
	err := pc.Kill(cmd)
	if err == nil {
		t.Fatal("Kill on unstarted process should return an error")
	}
}

func TestWaitWithoutStart(t *testing.T) {
	pc := NewProcessController()
	cmd := longRunningCmd()
	err := pc.Wait(context.Background(), cmd, DefaultGracePeriod)
	if err == nil {
		t.Fatal("Wait on unstarted process should return an error")
	}
}

func TestKillTerminatesProcess(t *testing.T) {
	pc := NewProcessController()
	cmd := longRunningCmd()

	if err := pc.Start(cmd); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := pc.Kill(cmd); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	// Wait should return an error since the process was killed.
	err := cmd.Wait()
	if err == nil {
		t.Fatal("expected error after Kill, got nil")
	}
}
