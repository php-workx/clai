package cmd

import (
	"embed"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/ipc"
)

//go:embed shell/zsh/clai.zsh
//go:embed shell/bash/clai.bash
//go:embed shell/fish/clai.fish
var shellScripts embed.FS

var initCmd = &cobra.Command{
	Use:   "init <shell>",
	Short: "Output shell integration script",
	Long: `Output the shell integration script for your shell.

Add this to your shell configuration file:

  # For Zsh (~/.zshrc):
  eval "$(clai init zsh)"

  # For Bash (~/.bashrc):
  eval "$(clai init bash)"

  # For Fish (~/.config/fish/config.fish):
  clai init fish | source`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash", "fish"},
	RunE:      runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	shell := args[0]

	// Ensure the clai daemon is running (starts in background if needed)
	// This is done silently - any errors are ignored since the daemon
	// will be started lazily when needed anyway
	_ = ipc.EnsureDaemon()

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

	fmt.Print(string(content))
	return nil
}
