package cmd

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

//go:embed shell/zsh/clai.zsh
//go:embed shell/bash/clai.bash
//go:embed shell/fish/clai.fish
var shellScripts embed.FS

var initCmd = &cobra.Command{
	Use:     "init <shell>",
	Short:   "Output shell integration script",
	GroupID: groupSetup,
	Long: `Output the shell integration script for your shell.

Add this to your shell configuration file:

  # For Zsh (~/.zshrc):
  eval "$(clai init zsh)"

  # For Bash (~/.bashrc or ~/.bash_profile on macOS):
  eval "$(clai init bash)"

  # For Fish (~/.config/fish/config.fish):
  clai init fish | source`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash", "fish"},
	RunE:      runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	shell := args[0]

	// NOTE: We do NOT start the daemon here to avoid blocking shell startup.
	// The daemon is started lazily by clai-shim when it first connects,
	// and shell scripts run clai-shim in the background anyway.

	var filename string
	switch shell {
	case "zsh":
		filename = "shell/zsh/clai.zsh"
	case "bash":
		filename = "shell/bash/clai.bash"
	case "fish":
		filename = "shell/fish/clai.fish"
	default:
		return fmt.Errorf("unsupported shell: %s (supported: zsh, bash, fish)", shell)
	}

	content, err := shellScripts.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read shell script: %w", err)
	}

	// Generate or reuse session ID
	// If CLAI_SESSION_ID is already set (re-sourcing), preserve it
	sessionID := os.Getenv("CLAI_SESSION_ID")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Load config to inject settings into the shell script.
	cfg, _ := config.Load() // Ignore errors; defaults are fine.
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Replace placeholders with actual values.
	// Use a single-pass replacer to avoid repeated full-script copies (helps keep init fast).
	replacer := strings.NewReplacer(
		"{{CLAI_SESSION_ID}}", sessionID,
		"{{CLAI_UP_ARROW_HISTORY}}", strconv.FormatBool(cfg.History.UpArrowOpensHistory),
		"{{CLAI_UP_ARROW_TRIGGER}}", cfg.History.UpArrowTrigger,
		"{{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}}", strconv.Itoa(cfg.History.UpArrowDoubleWindowMs),
	)
	fmt.Print(replacer.Replace(string(content)))
	return nil
}
