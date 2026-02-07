// clai-shim is a thin client that communicates with the clai daemon via gRPC.
// It is designed for minimal startup time and silent failure behavior.
//
// Subcommands:
//   - session-start: Notify daemon of new shell session
//   - session-end: Notify daemon of shell session ending
//   - log-start: Log command start
//   - log-end: Log command completion
//   - suggest: Get command suggestions
//   - text-to-command: Convert natural language to commands
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/runger/clai/internal/ipc"
)

func signalAwareContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// Version info - injected at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Flag name constants to avoid duplication
const (
	flagSessionID     = "session-id"
	flagCommandID     = "command-id"
	flagCwd           = "cwd"
	flagShell         = "shell"
	flagCommand       = "command"
	flagBuffer        = "buffer"
	flagCursor        = "cursor"
	flagPrompt        = "prompt"
	flagExitCode      = "exit-code"
	flagDuration      = "duration"
	flagGitBranch     = "git-branch"
	flagGitRepoName   = "git-repo-name"
	flagGitRepoRoot   = "git-repo-root"
	flagPrevCommandID = "prev-command-id"
	flagHistoryPath   = "history-path"
	flagIfNotExists   = "if-not-exists"
	flagForce         = "force"
)

func main() {
	// Silent exit on any panic
	defer func() {
		if r := recover(); r != nil {
			os.Exit(0)
		}
	}()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	cmd := os.Args[1]

	switch cmd {
	case "session-start":
		runSessionStart()
	case "session-end":
		runSessionEnd()
	case "log-start":
		runLogStart()
	case "log-end":
		runLogEnd()
	case "suggest":
		runSuggest()
	case "text-to-command":
		runTextToCommand()
	case "ping":
		runPing()
	case "status":
		runStatus()
	case "import-history":
		runImportHistory()
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		printUsage()
	}

	// Always exit 0 for silent failure
	os.Exit(0)
}

func printVersion() {
	fmt.Printf("clai-shim %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
}

func printUsage() {
	fmt.Println(`clai-shim - Thin client for clai daemon

Usage: clai-shim <command> [flags...]

Commands:
  session-start --session-id=ID --cwd=PATH --shell=SHELL  Notify new shell session
  session-end --session-id=ID                              Notify session ending
  log-start --session-id=ID --command-id=ID --cwd=PATH --command="CMD"
            [--git-branch=BRANCH] [--git-repo-name=NAME] [--git-repo-root=PATH]
            [--prev-command-id=ID]                        Log command start
  log-end --session-id=ID --command-id=ID --exit-code=N --duration=MS   Log command end
  suggest --session-id=ID --cwd=PATH --buffer="TEXT" [--cursor=N] [--limit=N]  Get suggestions
  text-to-command --session-id=ID --cwd=PATH --prompt="TEXT"            Text to command
  import-history [--shell=SHELL] [--if-not-exists] [--force]            Import shell history
  ping                                        Check daemon connectivity
  status                                      Get daemon status
  version                                     Print version info
  help                                        Print this help

Environment:
  CLAI_SOCKET       Override daemon socket path
  CLAI_DAEMON_PATH  Override daemon binary path`)
}

// parseFlags parses flag-style arguments (--key=value or --key value)
// Returns a map of key -> value
func parseFlags(args []string) map[string]string {
	result := make(map[string]string)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if idx := strings.Index(key, "="); idx >= 0 {
				// --key=value format
				result[key[:idx]] = key[idx+1:]
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				// --key value format
				result[key] = args[i+1]
				i++
			} else {
				// --key (boolean flag)
				result[key] = "true"
			}
		}
	}
	return result
}

// runSessionStart handles the session-start command
// Flags: --session-id, --cwd, --shell
func runSessionStart() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags[flagSessionID]
	cwd := flags[flagCwd]
	shell := flags[flagShell]

	if sessionID == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	info := ipc.DefaultClientInfo(Version)
	if shell != "" {
		info.Shell = shell
	}

	client.SessionStart(sessionID, cwd, info)
}

// runSessionEnd handles the session-end command
// Flags: --session-id
func runSessionEnd() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags[flagSessionID]
	if sessionID == "" {
		return // Silent failure
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	client.SessionEnd(sessionID)
}

// runLogStart handles the log-start command
// Flags: --session-id, --command-id, --cwd, --command
//
//	--git-branch, --git-repo-name, --git-repo-root, --prev-command-id
func runLogStart() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags[flagSessionID]
	commandID := flags[flagCommandID]
	cwd := flags[flagCwd]
	command := flags[flagCommand]

	if sessionID == "" || commandID == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	// Build command context with git info if provided
	ctx := &ipc.CommandContext{
		GitBranch:     flags[flagGitBranch],
		GitRepoName:   flags[flagGitRepoName],
		GitRepoRoot:   flags[flagGitRepoRoot],
		PrevCommandID: flags[flagPrevCommandID],
	}

	client.LogStartWithContext(sessionID, commandID, cwd, command, ctx)
}

// runLogEnd handles the log-end command
// Flags: --session-id, --command-id, --exit-code, --duration
func runLogEnd() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags[flagSessionID]
	commandID := flags[flagCommandID]
	exitCodeStr := flags[flagExitCode]
	durationStr := flags[flagDuration]

	if sessionID == "" || commandID == "" {
		return // Silent failure
	}

	exitCode, _ := strconv.Atoi(exitCodeStr)
	durationMs, _ := strconv.ParseInt(durationStr, 10, 64)

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	client.LogEnd(sessionID, commandID, exitCode, durationMs)
}

// runSuggest handles the suggest command
// Flags: --session-id, --cwd, --buffer, --cursor, --limit
// Output: suggestions (one per line)
func runSuggest() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags[flagSessionID]
	cwd := flags[flagCwd]
	buffer := flags[flagBuffer]
	cursorStr := flags[flagCursor]
	limitStr := flags["limit"]

	if sessionID == "" {
		// Try without flags for backwards compatibility
		if len(os.Args) >= 5 {
			sessionID = os.Args[2]
			cwd = os.Args[3]
			buffer = os.Args[4]
		}
	}

	if sessionID == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	cursorPos := len(buffer)
	if cursorStr != "" {
		cursorPos, _ = strconv.Atoi(cursorStr)
	}

	limit := 1
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	ctx, stop := signalAwareContext()
	defer stop()

	suggestions := client.Suggest(ctx, sessionID, cwd, buffer, cursorPos, false, limit)
	if len(suggestions) == 0 {
		return
	}

	// Output suggestions (one per line)
	for _, s := range suggestions {
		fmt.Println(s.Text)
	}
}

// runTextToCommand handles the text-to-command command
// Flags: --session-id, --cwd, --prompt
// Output: First suggestion or JSON if multiple
func runTextToCommand() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags[flagSessionID]
	cwd := flags[flagCwd]
	prompt := flags[flagPrompt]

	if sessionID == "" {
		// Try without flags for backwards compatibility
		if len(os.Args) >= 5 {
			sessionID = os.Args[2]
			cwd = os.Args[3]
			prompt = os.Args[4]
		}
	}

	if sessionID == "" || prompt == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	ctx, stop := signalAwareContext()
	defer stop()

	resp, err := client.TextToCommand(ctx, sessionID, prompt, cwd, 3)
	if err != nil || resp == nil || len(resp.Suggestions) == 0 {
		return
	}

	// Output first suggestion for shell integration
	fmt.Println(resp.Suggestions[0].Text)
}

// runPing checks daemon connectivity
func runPing() {
	client, err := ipc.NewClient()
	if err != nil {
		fmt.Println("not connected")
		return
	}
	defer client.Close()

	if client.Ping() {
		fmt.Println("ok")
	} else {
		fmt.Println("not responding")
	}
}

// runStatus prints daemon status
func runStatus() {
	client, err := ipc.NewClient()
	if err != nil {
		fmt.Println(`{"error": "not connected"}`)
		return
	}
	defer client.Close()

	status, err := client.GetStatus()
	if err != nil {
		fmt.Println(`{"error": "failed to get status"}`)
		return
	}

	output := map[string]interface{}{
		"version":         status.Version,
		"active_sessions": status.ActiveSessions,
		"uptime_seconds":  status.UptimeSeconds,
		"commands_logged": status.CommandsLogged,
	}

	data, _ := json.Marshal(output)
	fmt.Println(string(data))
}

// runImportHistory handles the import-history command
// Flags: --shell, --history-path, --if-not-exists, --force
// Output: JSON with import results
func runImportHistory() {
	flags := parseFlags(os.Args[2:])

	shell := flags[flagShell]
	historyPath := flags[flagHistoryPath]
	ifNotExists := flags[flagIfNotExists] == "true"
	force := flags[flagForce] == "true"

	// Default shell to "auto" if not specified
	if shell == "" {
		shell = "auto"
	}

	client, err := ipc.NewClient()
	if err != nil {
		fmt.Println(`{"error": "not connected"}`)
		return
	}
	defer client.Close()

	// Create signal-aware context for graceful cancellation
	ctx, stop := signalAwareContext()
	defer stop()

	resp, err := client.ImportHistory(ctx, shell, historyPath, ifNotExists, force)
	if err != nil {
		output := map[string]interface{}{
			"error": err.Error(),
		}
		data, _ := json.Marshal(output)
		fmt.Println(string(data))
		return
	}

	output := map[string]interface{}{
		"imported_count": resp.ImportedCount,
		"skipped":        resp.Skipped,
	}
	if resp.Error != "" {
		output["error"] = resp.Error
	}

	data, _ := json.Marshal(output)
	fmt.Println(string(data))
}
