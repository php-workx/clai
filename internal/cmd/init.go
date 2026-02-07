package cmd

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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
		sessionID = generateSessionID()
	}

	// Load config to inject settings into the shell script.
	cfg, _ := config.Load() // Ignore errors; defaults are fine.
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Replace placeholders with actual values.
	script := strings.ReplaceAll(string(content), "{{CLAI_SESSION_ID}}", sessionID)
	script = strings.ReplaceAll(script, "{{CLAI_UP_ARROW_HISTORY}}", strconv.FormatBool(cfg.History.UpArrowOpensHistory))

	fmt.Print(script)
	return nil
}

// generateSessionID returns a UUID-v4 shaped ID without using crypto/rand so
// shell startup does not depend on entropy availability.
func generateSessionID() string {
	hostname, _ := os.Hostname()
	seed := strings.Join([]string{
		hostname,
		strconv.FormatInt(time.Now().UnixNano(), 10),
		strconv.Itoa(os.Getpid()),
		strconv.Itoa(os.Getppid()),
	}, ":")

	sum := sha256.Sum256([]byte(seed))
	id := make([]byte, 16)
	copy(id, sum[:16])

	// Set UUID v4 version and variant bits for format compatibility.
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	hexID := hex.EncodeToString(id)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexID[0:8],
		hexID[8:12],
		hexID[12:16],
		hexID[16:20],
		hexID[20:32],
	)
}
