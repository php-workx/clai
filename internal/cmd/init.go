package cmd

import (
	"embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed shell/zsh/ai-terminal.zsh
//go:embed shell/bash/ai-terminal.bash
//go:embed shell/fish/ai-terminal.fish
var shellScripts embed.FS

var initCmd = &cobra.Command{
	Use:   "init <shell>",
	Short: "Output shell integration script",
	Long: `Output the shell integration script for your shell.

Add this to your shell configuration file:

  # For Zsh (~/.zshrc):
  eval "$(ai-terminal init zsh)"

  # For Bash (~/.bashrc):
  eval "$(ai-terminal init bash)"

  # For Fish (~/.config/fish/config.fish):
  ai-terminal init fish | source`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash", "fish"},
	RunE:      runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	shell := args[0]

	var filename string
	switch shell {
	case "zsh":
		filename = "shell/zsh/ai-terminal.zsh"
	case "bash":
		filename = "shell/bash/ai-terminal.bash"
	case "fish":
		filename = "shell/fish/ai-terminal.fish"
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
