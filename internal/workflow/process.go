package workflow

import (
	"context"
	"os/exec"
	"time"
)

// DefaultGracePeriod is the time to wait between interrupt and kill signals.
const DefaultGracePeriod = 5 * time.Second

// ProcessController manages subprocess lifecycle with platform-appropriate signals.
type ProcessController interface {
	// Start configures platform-specific process group settings and starts the command.
	Start(cmd *exec.Cmd) error

	// Interrupt sends a graceful interrupt signal to the process group.
	// On Unix: SIGINT to pgid. On Windows: GenerateConsoleCtrlEvent.
	Interrupt(cmd *exec.Cmd) error

	// Kill forcefully terminates the process group.
	Kill(cmd *exec.Cmd) error

	// Wait waits for the process to complete with cancellation support.
	// If ctx is cancelled, sends Interrupt, waits gracePeriod, then Kill.
	Wait(ctx context.Context, cmd *exec.Cmd, gracePeriod time.Duration) error
}

// NewProcessController creates a platform-appropriate ProcessController.
func NewProcessController() ProcessController {
	return newPlatformProcessController()
}
